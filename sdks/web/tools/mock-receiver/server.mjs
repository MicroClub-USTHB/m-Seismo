/**
 * Local mock ingest receiver — Phase 2.
 *
 * A tiny, dependency-free Node HTTP server that behaves like the **ASSUMED**
 * backend contract in `docs/backend-contract.md`. It exists ONLY so the browser
 * can make real cross-origin requests during development and e2e tests. It is
 * NOT the real backend and encodes unverified assumptions.
 *
 * Endpoints:
 *   OPTIONS *              CORS preflight (counted)
 *   GET  /__health         200 {ok:true}             (used by Playwright webServer)
 *   GET  /__reports        {count, reports:[...]}    (inspect stored reports)
 *                          Add ?full=1 to include screenshot base64 data;
 *                          by default screenshot.dataBase64 is replaced with
 *                          screenshot.dataBase64Length to keep responses small.
 *                          Add ?type=manual_with_error to filter by report type.
 *   GET  /__stats          {preflightCount, requestCount, storedCount, mode,
 *                           byType, byFingerprint}
 *   GET  /__viewer         a self-contained HTML page that renders the stored
 *                          reports readably (no external assets). Screenshot
 *                          base64 is shown as metadata only. Presentation of the
 *                          payload, never an edit of it.
 *   POST /__reset          clear reports + counters, mode -> "ok"
 *   POST /__mode           {mode, delayMs?} where mode is
 *                          ok|fail500|fail413|slow|offline|rateLimited; delayMs overrides
 *                          the slow-mode delay for this session (reset restores
 *                          the default)
 *   POST /api/reports      validate + store a report (201 {id})
 *                          schemaVersion 2 is validated against
 *                          docs/report.schema.json (single source of truth, via
 *                          ajv, a devDependency). schemaVersion 1 is still
 *                          accepted and validated with the legacy checker.
 *                          Other methods => 405 (Allow: POST, OPTIONS)
 *   GET  /cross-origin-script.js
 *                          JS that throws on load, served WITHOUT CORS headers so
 *                          a cross-origin <script> load yields only a generic
 *                          "Script error." in the host page (Phase 5.2 test)
 *
 * Phase 5.3 network-diagnostics helper routes (all CORS-enabled unless noted):
 *   GET  /api/status/:code  respond with the given HTTP status (e.g. /api/status/500)
 *   GET|POST /api/echo      echo the request method + JSON body back (200)
 *   GET  /api/slow          delay `?ms=` ms (default 1200), then 200 {ok:true}
 *   GET  /api/no-cors       served WITHOUT CORS headers (triggers a fetch
 *                           network_error, or an opaque_response with mode:'no-cors')
 *   GET  /api/drop          destroy the socket with no response (network_error)
 *
 * Config via env:
 *   PORT            (default 4010)
 *   ALLOWED_ORIGIN  (default http://localhost:5173)
 *   MAX_BYTES       (default 4194304, i.e. 4 MiB, to allow inline screenshots)
 *   MAX_REPORTS     (default 100)
 *   SLOW_DELAY_MS   (default 7000, deliberately below the SDK's default 10s
 *                    timeout so slow-mode is a *delayed success*; pass a larger
 *                    delayMs to /__mode to exercise the timeout path)
 */
import http from 'node:http';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import Ajv from 'ajv';

const PORT = Number(process.env.PORT ?? 4010);
const ALLOWED_ORIGIN = process.env.ALLOWED_ORIGIN ?? 'http://localhost:5173';
const MAX_BYTES = Number(process.env.MAX_BYTES ?? 4_194_304);
const MAX_REPORTS = Number(process.env.MAX_REPORTS ?? 100);
const SLOW_DELAY_MS = Number(process.env.SLOW_DELAY_MS ?? 7_000);

const VALID_MODES = new Set(['ok', 'fail500', 'fail413', 'slow', 'offline', 'rateLimited']);

/** @type {Map<string, object>} */
const reports = new Map();
let mode = 'ok';
let slowDelayMs = SLOW_DELAY_MS;
let preflightCount = 0;
let requestCount = 0;

function setCors(res) {
  res.setHeader('Access-Control-Allow-Origin', ALLOWED_ORIGIN);
  res.setHeader('Access-Control-Allow-Methods', 'POST, OPTIONS');
  res.setHeader(
    'Access-Control-Allow-Headers',
    'Content-Type, X-MSeismo-Key',
  );
  res.setHeader('Access-Control-Max-Age', '600');
  res.setHeader('Vary', 'Origin');
}

function sendJson(res, status, body) {
  setCors(res);
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json');
  res.end(JSON.stringify(body));
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    let tooLarge = false;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > MAX_BYTES) {
        tooLarge = true;
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      if (tooLarge) {
        reject(Object.assign(new Error('payload too large'), { code: 413 }));
        return;
      }
      resolve(Buffer.concat(chunks).toString('utf8'));
    });
    req.on('error', reject);
  });
}

function isNonEmptyString(value) {
  return typeof value === 'string' && value.length > 0;
}

const ALLOWED_SCREENSHOT_MIME = new Set(['image/png', 'image/jpeg', 'image/webp']);

/** Validates an inline screenshot attachment. Returns an error string or null. */
function validateScreenshot(screenshot) {
  if (typeof screenshot !== 'object' || screenshot === null) {
    return 'screenshot must be null or an object';
  }
  if (!isNonEmptyString(screenshot.name)) return 'screenshot.name must be a non-empty string';
  if (!ALLOWED_SCREENSHOT_MIME.has(screenshot.mimeType)) {
    return 'screenshot.mimeType must be image/png, image/jpeg, or image/webp';
  }
  if (typeof screenshot.sizeBytes !== 'number' || screenshot.sizeBytes < 0) {
    return 'screenshot.sizeBytes must be a non-negative number';
  }
  if (!isNonEmptyString(screenshot.dataBase64)) {
    return 'screenshot.dataBase64 must be a non-empty base64 string';
  }
  return null;
}

/**
 * Returns a copy of the report with the screenshot's base64 payload replaced by
 * its length, so `/__reports` stays readable. Use `/__reports?full=1` for the raw
 * stored object.
 */
function redactScreenshot(report) {
  const screenshot = report?.screenshot;
  if (typeof screenshot !== 'object' || screenshot === null) return report;
  const { dataBase64, ...rest } = screenshot;
  return {
    ...report,
    screenshot: {
      ...rest,
      dataBase64Length: typeof dataBase64 === 'string' ? dataBase64.length : 0,
    },
  };
}

/** Validates an optional context sub-object against the ASSUMED contract. */
function validateContext(context) {
  if (typeof context !== 'object' || context === null) return 'context must be an object';

  // url: optional; when present must be a string.
  if (context.url !== undefined && typeof context.url !== 'string') {
    return 'context.url must be a string when present';
  }
  if (!isNonEmptyString(context.timestamp)) {
    return 'context.timestamp must be a non-empty string';
  }
  if (Number.isNaN(Date.parse(context.timestamp))) {
    return 'context.timestamp must be an ISO date';
  }

  // browser: optional; wrong types are rejected, unknown sub-fields accepted.
  if (context.browser !== undefined) {
    const browser = context.browser;
    if (typeof browser !== 'object' || browser === null) {
      return 'context.browser must be an object when present';
    }
    if (browser.name !== undefined && typeof browser.name !== 'string') {
      return 'context.browser.name must be a string when present';
    }
    if (browser.version !== undefined && typeof browser.version !== 'string') {
      return 'context.browser.version must be a string when present';
    }
  }

  // platform: optional string.
  if (context.platform !== undefined && typeof context.platform !== 'string') {
    return 'context.platform must be a string when present';
  }

  // viewport: optional { width, height } of finite positive numbers.
  if (context.viewport !== undefined) {
    const viewport = context.viewport;
    if (typeof viewport !== 'object' || viewport === null) {
      return 'context.viewport must be an object when present';
    }
    if (
      typeof viewport.width !== 'number' ||
      !Number.isFinite(viewport.width) ||
      viewport.width <= 0 ||
      typeof viewport.height !== 'number' ||
      !Number.isFinite(viewport.height) ||
      viewport.height <= 0
    ) {
      return 'context.viewport must have positive numeric width and height';
    }
  }

  // application: required object; unknown sub-fields accepted.
  if (typeof context.application !== 'object' || context.application === null) {
    return 'context.application must be an object';
  }
  return null;
}

// --- Schema validation (schemaVersion 2) ------------------------------------
// docs/report.schema.json is the single source of truth. ajv is a devDependency
// of the SDK workspace; the receiver is a dev tool, never shipped.
const SCHEMA_PATH = new URL('../../docs/report.schema.json', import.meta.url);
let validateV2 = null;
try {
  const schema = JSON.parse(readFileSync(fileURLToPath(SCHEMA_PATH), 'utf8'));
  validateV2 = new Ajv({ allErrors: true, strict: false }).compile(schema);
} catch (error) {
  // eslint-disable-next-line no-console -- startup problem must be visible.
  console.warn(
    `[mock-receiver] WARNING: could not load docs/report.schema.json (${error?.message ?? error}); schemaVersion 2 will use the legacy checks only.`,
  );
}

/** Top-level keys the SDK sends; anything else is logged as unknown. */
const KNOWN_TOP_LEVEL_KEYS = new Set([
  'schemaVersion',
  'sdkVersion',
  'reportId',
  'type',
  'trigger',
  'description',
  'error',
  'context',
  'breadcrumbs',
  'networkFailures',
  'screenshot',
]);

function logUnknownFields(report) {
  const unknown = Object.keys(report).filter((key) => !KNOWN_TOP_LEVEL_KEYS.has(key));
  if (unknown.length > 0) {
    // eslint-disable-next-line no-console -- tolerated-but-logged per contract.
    console.warn(`[mock-receiver] report ${report.reportId} carried unknown fields: ${unknown.join(', ')}`);
  }
}

/** Legacy validation for schemaVersion 1 payloads (kept for compatibility). */
function validateV1(report) {
  if (!isNonEmptyString(report.sdkVersion)) return 'sdkVersion must be a non-empty string';
  if (!isNonEmptyString(report.reportId)) return 'reportId must be a non-empty string';
  if (!isNonEmptyString(report.description)) return 'description must be a non-empty string';

  if (report.error !== null) {
    if (typeof report.error !== 'object') return 'error must be null or an object';
    if (!isNonEmptyString(report.error.type)) return 'error.type must be a non-empty string';
    if (!isNonEmptyString(report.error.message)) return 'error.message must be a non-empty string';
  }

  const contextError = validateContext(report.context);
  if (contextError) return contextError;

  if (!Array.isArray(report.breadcrumbs)) return 'breadcrumbs must be an array';
  if (!Array.isArray(report.networkFailures)) return 'networkFailures must be an array';
  if (report.screenshot !== null) {
    const screenshotError = validateScreenshot(report.screenshot);
    if (screenshotError) return screenshotError;
  }
  return null;
}

/**
 * Validates a report. schemaVersion 2 is checked against docs/report.schema.json;
 * schemaVersion 1 uses the legacy checker. Returns an error string or null.
 */
function validateReport(report) {
  if (typeof report !== 'object' || report === null) return 'body must be a JSON object';

  // A raw userAgent field is never sent by the SDK. Store it if it arrives, but
  // flag it so an accidental leak is obvious in the receiver log.
  if (report.userAgent !== undefined) {
    // eslint-disable-next-line no-console -- deliberate accidental-leak warning.
    console.warn(
      '[mock-receiver] WARNING: report carried a raw "userAgent" field; the SDK must not send this.',
    );
  }

  if (report.schemaVersion === 1) {
    return validateV1(report);
  }

  if (report.schemaVersion === 2) {
    // docs/report.schema.json is the single source of truth for v2. The legacy
    // checker is only a fallback when the schema could not be compiled.
    if (validateV2 === null) return validateV1(report);
    if (!validateV2(report)) {
      const first = validateV2.errors?.[0];
      const where = first?.instancePath && first.instancePath.length > 0 ? first.instancePath : '/';
      return `schema: ${where} ${first?.message ?? 'is invalid'}`;
    }
    logUnknownFields(report);
    return null;
  }

  return `schemaVersion must be 1 or 2 (got ${JSON.stringify(report.schemaVersion)})`;
}

async function handleIngest(req, res) {
  requestCount += 1;

  if (mode === 'offline') {
    req.socket.destroy();
    return;
  }
  if (mode === 'fail500') {
    sendJson(res, 500, { message: 'simulated server error' });
    return;
  }
  if (mode === 'fail413') {
    sendJson(res, 413, { message: 'payload too large (simulated)' });
    return;
  }
  if (mode === 'rateLimited') {
    res.setHeader('Retry-After', '1');
    sendJson(res, 429, { message: 'rate limited (simulated)' });
    return;
  }
  if (mode === 'slow') {
    await new Promise((resolve) => setTimeout(resolve, slowDelayMs));
  }

  let raw;
  try {
    raw = await readBody(req);
  } catch (error) {
    if (error && error.code === 413) {
      sendJson(res, 413, { message: 'payload too large' });
      return;
    }
    sendJson(res, 400, { message: 'could not read request body' });
    return;
  }

  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch {
    sendJson(res, 400, { message: 'body must be valid JSON' });
    return;
  }

  const validationError = validateReport(parsed);
  if (validationError) {
    sendJson(res, 400, { message: validationError });
    return;
  }

  // Idempotency: duplicate reportId returns the same result without storing twice.
  if (reports.has(parsed.reportId)) {
    sendJson(res, 201, { id: parsed.reportId });
    return;
  }

  if (reports.size >= MAX_REPORTS) {
    sendJson(res, 429, { message: 'too many stored reports (mock limit)' });
    return;
  }

  reports.set(parsed.reportId, parsed);
  sendJson(res, 201, { id: parsed.reportId });
}

// --- /__viewer: a self-contained, read-only HTML view of stored reports -----
// Every value is HTML-escaped before it reaches the page, so a report whose
// description contains a <script> tag is rendered as inert text. No external
// assets are referenced; the styling is inlined so the page works offline.

function escapeHtml(value) {
  return String(value).replace(
    /[&<>"']/g,
    (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
  );
}

function presentType(type) {
  const labels = {
    manual: 'Manual report',
    manual_with_error: 'Manual report with error',
    automatic_error: 'Automatic error',
    captured_exception: 'Captured exception',
    captured_message: 'Captured message',
  };
  return typeof type === 'string' && type in labels ? labels[type] : `Report (${String(type)})`;
}

function renderReportSection(report) {
  const error = report?.error;
  const context = report?.context ?? {};
  const breadcrumbs = Array.isArray(report?.breadcrumbs) ? report.breadcrumbs : [];
  const network = Array.isArray(report?.networkFailures) ? report.networkFailures : [];
  const screenshot = report?.screenshot;

  const kv = (rows) =>
    `<dl class="kv">${rows
      .map(
        ([key, value]) =>
          `<dt>${escapeHtml(key)}</dt><dd>${value === undefined || value === null || value === '' ? '<span class="absent">not available</span>' : escapeHtml(value)}</dd>`,
      )
      .join('')}</dl>`;

  const crumbs =
    breadcrumbs.length === 0
      ? '<p class="absent">No breadcrumbs.</p>'
      : `<ol class="crumbs">${breadcrumbs
          .map(
            (crumb) =>
              `<li><span class="cat">${escapeHtml(crumb?.category ?? '?')}</span> ${escapeHtml(crumb?.message ?? '')}</li>`,
          )
          .join('')}</ol>`;

  const net =
    network.length === 0
      ? '<p class="absent">No network failures.</p>'
      : `<table class="net"><thead><tr><th>Method</th><th>URL</th><th>Status</th><th>ms</th><th>Category</th></tr></thead><tbody>${network
          .map(
            (failure) =>
              `<tr><td>${escapeHtml(failure?.method ?? '')}</td><td class="mono">${escapeHtml(failure?.url ?? '')}</td><td>${escapeHtml(failure?.status ?? '—')}</td><td>${escapeHtml(failure?.durationMs ?? '—')}</td><td>${escapeHtml(failure?.category ?? '')}</td></tr>`,
          )
          .join('')}</tbody></table>`;

  const screenshotBlock =
    typeof screenshot !== 'object' || screenshot === null
      ? '<p class="absent">No screenshot attached.</p>'
      : kv([
          ['Name', screenshot.name],
          ['MIME type', screenshot.mimeType],
          ['Size', `${screenshot.sizeBytes ?? 0} bytes`],
          ['Inline base64', screenshot.dataBase64 === undefined ? 'stripped' : 'present'],
        ]);

  return `
  <article class="report">
    <header><span class="badge">${escapeHtml(presentType(report?.type))}</span> <strong>${escapeHtml(report?.reportId ?? '(no id)')}</strong> <span class="absent">${escapeHtml(report?.context?.timestamp ?? '')}</span></header>
    <h3>Description</h3>
    <p>${report?.description ? escapeHtml(report.description) : '<span class="absent">(no description)</span>'}</p>
    <h3>Error</h3>
    ${
      error === null || error === undefined
        ? '<p class="absent">No error attached.</p>'
        : kv([
            ['Type', error.type],
            ['Message', error.message],
            ['Fingerprint', error.fingerprint],
            ['Occurrences', error.occurrences],
          ]) + (error.stack ? `<pre class="stack">${escapeHtml(error.stack)}</pre>` : '')
    }
    <h3>Context</h3>
    ${kv([
      ['URL', context.url],
      ['Browser', context.browser ? `${context.browser.name ?? ''} ${context.browser.version ?? ''}`.trim() : undefined],
      ['Platform', context.platform],
      ['Viewport', context.viewport ? `${context.viewport.width} × ${context.viewport.height}` : undefined],
      ['App version', context.application?.version],
      ['Environment', context.application?.environment],
    ])}
    <h3>Breadcrumbs (${breadcrumbs.length})</h3>
    ${crumbs}
    <h3>Network failures (${network.length})</h3>
    ${net}
    <h3>Screenshot</h3>
    ${screenshotBlock}
  </article>`;
}

function renderViewerPage(stored, currentMode) {
  const body =
    stored.length === 0
      ? '<p class="absent">No reports stored yet. Send one from the test app, then reload.</p>'
      : stored.map(renderReportSection).join('');
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>m-Seismo mock receiver — stored reports (${stored.length})</title>
<style>
:root{--bg:#f5f6f8;--panel:#fff;--fg:#14161a;--muted:#5b6470;--border:#d7dbe2;--accent:#2f6fed;--code:#0f1115;--codefg:#d6dae0;}
@media (prefers-color-scheme: dark){:root{--bg:#14161a;--panel:#1d2026;--fg:#eef1f5;--muted:#9aa4b2;--border:#333a44;--accent:#6da1ff;--code:#0b0d11;}}
*{box-sizing:border-box;}body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;}
header.page{padding:16px 20px;background:var(--panel);border-bottom:1px solid var(--border);display:flex;flex-wrap:wrap;gap:8px;align-items:center;}
header.page h1{font-size:16px;margin:0;}
main{max-width:900px;margin:0 auto;padding:20px;}
.report{background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:16px;margin-bottom:16px;}
.report header{display:flex;flex-wrap:wrap;gap:8px;align-items:baseline;margin-bottom:8px;}
.report h3{margin:16px 0 6px;font-size:13px;text-transform:uppercase;letter-spacing:.03em;color:var(--muted);}
.badge{background:#dbe7fb;color:#1a3f7a;border-radius:999px;padding:1px 8px;font-size:12px;font-weight:700;}
.absent{color:var(--muted);font-style:italic;}
.kv{display:grid;grid-template-columns:minmax(120px,max-content) 1fr;gap:2px 16px;margin:0;}
.kv dt{color:var(--muted);}.kv dd{margin:0;word-break:break-word;}
.mono,pre{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;}
pre.stack{background:var(--code);color:var(--codefg);padding:10px;border-radius:6px;font-size:12px;overflow:auto;max-height:260px;white-space:pre-wrap;word-break:break-word;}
.crumbs{margin:0;padding-left:18px;}.crumbs .cat{font-weight:700;color:var(--accent);}
table.net{border-collapse:collapse;width:100%;font-size:13px;}table.net th,table.net td{border:1px solid var(--border);padding:4px 6px;text-align:left;}
a{color:var(--accent);}
</style></head>
<body>
<header class="page"><h1>Mock receiver — stored reports</h1><span class="badge">${stored.length} stored</span><span class="absent">mode: ${escapeHtml(currentMode)}</span><a href="/__reports?full=1" style="margin-left:auto">Raw JSON</a></header>
<main>${body}</main>
</body></html>`;
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://localhost');
  const { pathname } = url;
  const method = req.method ?? 'GET';

  // CORS preflight for every path.
  if (method === 'OPTIONS') {
    preflightCount += 1;
    setCors(res);
    res.statusCode = 204;
    res.end();
    return;
  }

  if (method === 'GET' && pathname === '/__health') {
    sendJson(res, 200, { ok: true });
    return;
  }

  // Deliberately served WITHOUT any CORS headers so that a cross-origin
  // <script src> load that throws produces only a generic "Script error."
  // message in the host page. Used by the Phase 5.2 diagnostics test.
  if (method === 'GET' && pathname === '/cross-origin-script.js') {
    res.statusCode = 200;
    res.setHeader('Content-Type', 'application/javascript');
    res.end(
      'throw new Error("m-seismo cross-origin script failure");\n',
    );
    return;
  }

  if (method === 'GET' && pathname === '/__reports') {
    const full = url.searchParams.get('full') === '1';
    const typeFilter = url.searchParams.get('type');
    let stored = Array.from(reports.values());
    if (typeFilter !== null) {
      stored = stored.filter((report) => report.type === typeFilter);
    }
    stored = stored.map((report) => (full ? report : redactScreenshot(report)));
    sendJson(res, 200, { count: stored.length, reports: stored });
    return;
  }

  if (method === 'GET' && pathname === '/__stats') {
    const byType = {};
    const byFingerprint = {};
    for (const report of reports.values()) {
      const type = typeof report.type === 'string' ? report.type : `v${report.schemaVersion ?? '?'}`;
      byType[type] = (byType[type] ?? 0) + 1;
      const fingerprint = report?.error?.fingerprint;
      if (typeof fingerprint === 'string') {
        byFingerprint[fingerprint] = (byFingerprint[fingerprint] ?? 0) + 1;
      }
    }
    sendJson(res, 200, {
      mode,
      preflightCount,
      requestCount,
      storedCount: reports.size,
      byType,
      byFingerprint,
    });
    return;
  }

  if (method === 'GET' && pathname === '/__viewer') {
    res.statusCode = 200;
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    res.end(renderViewerPage(Array.from(reports.values()), mode));
    return;
  }

  if (method === 'POST' && pathname === '/__reset') {
    reports.clear();
    preflightCount = 0;
    requestCount = 0;
    mode = 'ok';
    slowDelayMs = SLOW_DELAY_MS;
    sendJson(res, 200, { ok: true, mode });
    return;
  }

  if (method === 'POST' && pathname === '/__mode') {
    void readBody(req)
      .then((raw) => {
        let body;
        try {
          body = JSON.parse(raw);
        } catch {
          sendJson(res, 400, { message: 'body must be valid JSON' });
          return;
        }
        if (!VALID_MODES.has(body?.mode)) {
          sendJson(res, 400, {
            message: `mode must be one of: ${Array.from(VALID_MODES).join(', ')}`,
          });
          return;
        }
        if (body.delayMs !== undefined) {
          if (typeof body.delayMs !== 'number' || !Number.isFinite(body.delayMs) || body.delayMs < 0) {
            sendJson(res, 400, { message: 'delayMs must be a non-negative number' });
            return;
          }
          slowDelayMs = body.delayMs;
        }
        mode = body.mode;
        sendJson(res, 200, { ok: true, mode, slowDelayMs });
      })
      .catch(() => sendJson(res, 400, { message: 'could not read request body' }));
    return;
  }

  if (method === 'POST' && pathname === '/api/reports') {
    void handleIngest(req, res);
    return;
  }

  // --- Phase 5.3 network-diagnostics helper routes ------------------------

  // /api/status/:code — reply with the requested status code.
  const statusMatch = /^\/api\/status\/(\d{3})$/.exec(pathname);
  if (statusMatch && (method === 'GET' || method === 'POST')) {
    const code = Number(statusMatch[1]);
    sendJson(res, code, { requestedStatus: code, ok: code < 400 });
    return;
  }

  // /api/echo — reflect the request back (used to prove a successful call is
  // NOT recorded as a failure).
  if ((method === 'GET' || method === 'POST') && pathname === '/api/echo') {
    void readBody(req)
      .then((raw) => {
        let body = null;
        if (raw.length > 0) {
          try {
            body = JSON.parse(raw);
          } catch {
            body = raw;
          }
        }
        sendJson(res, 200, { ok: true, method, body });
      })
      .catch(() => sendJson(res, 200, { ok: true, method, body: null }));
    return;
  }

  // /api/slow — delay, then respond. `?ms=` overrides the default delay;
  // `?status=` (100–599) overrides the response status so a *slow failing*
  // request can be exercised (used by the E11 lab scenario).
  if (method === 'GET' && pathname === '/api/slow') {
    const requested = Number(url.searchParams.get('ms'));
    const delay = Number.isFinite(requested) && requested >= 0 ? requested : 1_200;
    const requestedStatus = Number(url.searchParams.get('status'));
    const status =
      Number.isInteger(requestedStatus) && requestedStatus >= 100 && requestedStatus <= 599
        ? requestedStatus
        : 200;
    void new Promise((resolve) => setTimeout(resolve, delay)).then(() => {
      sendJson(res, status, { ok: status < 400, delayedMs: delay, status });
    });
    return;
  }

  // /api/no-cors — success status, but deliberately NO CORS headers. A normal
  // cross-origin fetch rejects with a TypeError (network_error); a
  // mode:'no-cors' fetch yields an opaque response (status 0).
  if (method === 'GET' && pathname === '/api/no-cors') {
    res.statusCode = 200;
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ ok: true }));
    return;
  }

  // /api/drop — accept the connection then destroy it with no response, so the
  // browser surfaces a network error (TypeError).
  if (method === 'GET' && pathname === '/api/drop') {
    req.socket.destroy();
    return;
  }

  // ------------------------------------------------------------------------

  if (pathname === '/api/reports') {
    // The report endpoint is POST-only. Answer a wrong method explicitly so a
    // direct browser visit (`GET`) is self-explanatory rather than a bare 404.
    res.setHeader('Allow', 'POST, OPTIONS');
    sendJson(res, 405, { message: `method ${method} not allowed; use POST` });
    return;
  }

  sendJson(res, 404, { message: `no route for ${method} ${pathname}` });
});

server.listen(PORT, () => {
  // eslint-disable-next-line no-console -- server startup notice.
  console.log(
    `[mock-receiver] listening on http://localhost:${PORT} (origin ${ALLOWED_ORIGIN})`,
  );
});
