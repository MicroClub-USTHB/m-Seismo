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
 *   GET  /__stats          {preflightCount, requestCount, storedCount, mode}
 *   POST /__reset          clear reports + counters, mode -> "ok"
 *   POST /__mode           {mode} where mode is ok|fail500|slow|offline|rateLimited
 *   POST /api/reports      validate + store a report (201 {id})
 *
 * Config via env:
 *   PORT            (default 4010)
 *   ALLOWED_ORIGIN  (default http://localhost:5173)
 *   MAX_BYTES       (default 262144)
 *   MAX_REPORTS     (default 100)
 */
import http from 'node:http';

const PORT = Number(process.env.PORT ?? 4010);
const ALLOWED_ORIGIN = process.env.ALLOWED_ORIGIN ?? 'http://localhost:5173';
const MAX_BYTES = Number(process.env.MAX_BYTES ?? 262_144);
const MAX_REPORTS = Number(process.env.MAX_REPORTS ?? 100);
const SLOW_DELAY_MS = Number(process.env.SLOW_DELAY_MS ?? 15_000);

const VALID_MODES = new Set(['ok', 'fail500', 'slow', 'offline', 'rateLimited']);

/** @type {Map<string, object>} */
const reports = new Map();
let mode = 'ok';
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

  const context = report.context;
  if (typeof context !== 'object' || context === null) return 'context must be an object';
  if (typeof context.url !== 'string') return 'context.url must be a string';
  if (!isNonEmptyString(context.timestamp)) return 'context.timestamp must be a non-empty string';
  if (Number.isNaN(Date.parse(context.timestamp))) return 'context.timestamp must be an ISO date';
  if (typeof context.application !== 'object' || context.application === null) {
    return 'context.application must be an object';
  }

  if (!Array.isArray(report.breadcrumbs)) return 'breadcrumbs must be an array';
  if (!Array.isArray(report.networkFailures)) return 'networkFailures must be an array';
  if (report.screenshot !== null) return 'screenshot must be null in this contract version';
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
    await new Promise((resolve) => setTimeout(resolve, SLOW_DELAY_MS));
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

  if (method === 'GET' && pathname === '/__reports') {
    sendJson(res, 200, {
      count: reports.size,
      reports: Array.from(reports.values()),
    });
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
        mode = body.mode;
        sendJson(res, 200, { ok: true, mode });
      })
      .catch(() => sendJson(res, 400, { message: 'could not read request body' }));
    return;
  }

  if (method === 'POST' && pathname === '/api/reports') {
    void handleIngest(req, res);
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
