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
 *   GET  /__stats          {preflightCount, requestCount, storedCount, mode}
 *   POST /__reset          clear reports + counters, mode -> "ok"
 *   POST /__mode           {mode, delayMs?} where mode is
 *                          ok|fail500|slow|offline|rateLimited; delayMs overrides
 *                          the slow-mode delay for this session (reset restores
 *                          the default)
 *   POST /api/reports      validate + store a report (201 {id})
 *                          Other methods => 405 (Allow: POST, OPTIONS)
 *   GET  /cross-origin-script.js
 *                          JS that throws on load, served WITHOUT CORS headers so
 *                          a cross-origin <script> load yields only a generic
 *                          "Script error." in the host page (Phase 5.2 test)
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

const PORT = Number(process.env.PORT ?? 4010);
const ALLOWED_ORIGIN = process.env.ALLOWED_ORIGIN ?? 'http://localhost:5173';
const MAX_BYTES = Number(process.env.MAX_BYTES ?? 4_194_304);
const MAX_REPORTS = Number(process.env.MAX_REPORTS ?? 100);
const SLOW_DELAY_MS = Number(process.env.SLOW_DELAY_MS ?? 7_000);

const VALID_MODES = new Set(['ok', 'fail500', 'slow', 'offline', 'rateLimited']);

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

/** Validates against the ASSUMED draft contract. Returns an error string or null. */
function validateReport(report) {
  if (typeof report !== 'object' || report === null) return 'body must be a JSON object';
  if (report.schemaVersion !== 1) return 'schemaVersion must be 1';
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

  // A raw userAgent field is never sent by the SDK. Store it if it arrives, but
  // flag it so an accidental leak is obvious in the receiver log.
  if (report.userAgent !== undefined) {
    // eslint-disable-next-line no-console -- deliberate accidental-leak warning.
    console.warn(
      '[mock-receiver] WARNING: report carried a raw "userAgent" field; the SDK must not send this.',
    );
  }
  return null;
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
    const stored = Array.from(reports.values()).map((report) =>
      full ? report : redactScreenshot(report),
    );
    sendJson(res, 200, { count: reports.size, reports: stored });
    return;
  }

  if (method === 'GET' && pathname === '/__stats') {
    sendJson(res, 200, {
      mode,
      preflightCount,
      requestCount,
      storedCount: reports.size,
    });
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
