import { expect, test } from '@playwright/test';

const RECEIVER = 'http://localhost:4010';
const ENDPOINT = `${RECEIVER}/api/reports`;
const APP_ORIGIN = 'http://localhost:5173';

test.beforeEach(async ({ page, request }) => {
  await request.post(`${RECEIVER}/__reset`);
  await page.goto('/');
});

test('A: sending a test report stores a valid report on the receiver', async ({
  page,
  request,
}) => {
  // Reset assertion baseline.
  const before = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(before.count).toBe(0);

  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText(
    'send test report: ok status=201',
  );

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(1);
  const report = stored.reports[0];
  expect(report.schemaVersion).toBe(2);
  expect(report.type).toBe('manual');
  expect(report.trigger).toBe('form');
  expect(typeof report.sdkVersion).toBe('string');
  expect(typeof report.reportId).toBe('string');
  expect(report.description).toContain('m-Seismo test app');
  expect(report.error).toBeNull();
  expect(report.screenshot).toBeNull();
  expect(Array.isArray(report.breadcrumbs)).toBe(true);
  expect(report.context.url).toContain('localhost:5173');
  expect(Number.isNaN(Date.parse(report.context.timestamp))).toBe(false);
});

test('B: fail500 mode is logged and the page stays usable', async ({
  page,
  request,
}) => {
  const mode = await request.post(`${RECEIVER}/__mode`, {
    data: { mode: 'fail500' },
  });
  expect(mode.ok()).toBe(true);

  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText(
    'send test report: failed kind=http status=500',
  );

  // Nothing was stored, and the page is still interactive.
  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(0);

  await page.getByTestId('theme-toggle').click();
  await expect(page.getByTestId('event-log')).toContainText('theme set to');
});

test('C: the browser sends a real cross-origin request (preflight handled)', async ({
  page,
  request,
}) => {
  const [response] = await Promise.all([
    page.waitForResponse(
      (r) => r.url() === ENDPOINT && r.request().method() === 'POST',
    ),
    page.getByTestId('send-report').click(),
  ]);

  expect(response.status()).toBe(201);
  // The CORS header proves the cross-origin response was accepted by the browser.
  expect(response.headers()['access-control-allow-origin']).toBe(APP_ORIGIN);

  // The receiver actually observed an OPTIONS preflight.
  const stats = await (await request.get(`${RECEIVER}/__stats`)).json();
  expect(stats.preflightCount).toBeGreaterThanOrEqual(1);
  expect(stats.storedCount).toBe(1);
});

test('D: duplicate reportId is not stored twice (idempotency)', async ({
  request,
}) => {
  const report = {
    schemaVersion: 1,
    sdkVersion: '0.0.0',
    reportId: 'dup-1',
    description: 'duplicate check',
    error: null,
    context: {
      url: 'https://app.test/',
      timestamp: new Date().toISOString(),
      application: {},
    },
    breadcrumbs: [],
    networkFailures: [],
    screenshot: null,
  };

  const first = await request.post(ENDPOINT, { data: report });
  expect(first.status()).toBe(201);
  const second = await request.post(ENDPOINT, { data: report });
  expect(second.status()).toBe(201);

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(1);
});

test('E: the report endpoint rejects a wrong method with 405 (not a bare 404)', async ({
  request,
}) => {
  const response = await request.get(ENDPOINT);
  expect(response.status()).toBe(405);
  expect(response.headers()['allow']).toBe('POST, OPTIONS');
});
