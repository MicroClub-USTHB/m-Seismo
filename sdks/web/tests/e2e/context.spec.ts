import { expect, test } from '@playwright/test';

const RECEIVER = 'http://localhost:4010';

async function setMode(
  request: import('@playwright/test').APIRequestContext,
  mode: string,
): Promise<void> {
  const response = await request.post(`${RECEIVER}/__mode`, { data: { mode } });
  expect(response.ok()).toBe(true);
}

interface StoredContext {
  url: string;
  timestamp: string;
  browser: { name: string; version: string };
  platform: string;
  viewport: { width: number; height: number };
  application: Record<string, unknown>;
  userAgent?: unknown;
}

interface StoredReport {
  context: StoredContext;
  userAgent?: unknown;
}

async function storedReports(
  request: import('@playwright/test').APIRequestContext,
): Promise<{ count: number; reports: StoredReport[] }> {
  return (await request.get(`${RECEIVER}/__reports`)).json();
}

test.beforeEach(async ({ page, request }) => {
  await request.post(`${RECEIVER}/__reset`);
  await page.goto('/');
});

test('A: a report carries the full automatic context', async ({ page, request }) => {
  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText(
    'send test report: ok status=201',
  );

  const stored = await storedReports(request);
  expect(stored.count).toBe(1);
  const report = stored.reports[0]!;
  const context = report.context;

  expect(context.url).toContain('localhost:5173');
  expect(Number.isNaN(Date.parse(context.timestamp))).toBe(false);
  expect(context.browser.name).toBeTruthy();
  expect(context.browser.version).toBeTruthy();
  expect(context.platform).toBeTruthy();
  expect(context.viewport.width).toBeGreaterThan(0);
  expect(context.viewport.height).toBeGreaterThan(0);
  expect(context.application).toEqual({ version: '0.0.0', environment: 'test-app' });

  // The raw user agent must never leak, at the top level or inside context.
  expect(report.userAgent).toBeUndefined();
  expect(context.userAgent).toBeUndefined();
});

test('B: URL secrets are redacted while ordinary query params survive', async ({
  page,
  request,
}) => {
  await page.getByTestId('context-lab-navigate').click();
  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText(
    'send test report: ok status=201',
  );

  const stored = await storedReports(request);
  const url: string = stored.reports[0]!.context.url;

  expect(url).toContain('token=[REDACTED]');
  expect(url).toContain('email=[REDACTED]');
  expect(url).toContain('utm_source=docs');
  // The fragment and the raw secret values are gone.
  expect(url).not.toContain('fake-secret-123');
  expect(url).not.toContain('a@b.c');
  expect(url).not.toContain('#section');
});

test('C: application metadata is present with config and omitted without it', async ({
  page,
  request,
}) => {
  // Initial init (page load) configured appVersion/environment.
  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText('ok status=201');

  // Re-init without any application metadata.
  await page.getByTestId('context-lab-reinit-no-meta').click();
  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText(
    'send test report: ok status=201',
  );

  const stored = await storedReports(request);
  expect(stored.count).toBe(2);
  expect(stored.reports[0]!.context.application).toEqual({
    version: '0.0.0',
    environment: 'test-app',
  });
  expect(stored.reports[1]!.context.application).toEqual({});
});

test('D: context is collected fresh at submit time, after init', async ({
  page,
  request,
}) => {
  // The page was already initialised by load; update the URL afterwards.
  await page.getByTestId('context-lab-navigate').click();
  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText('ok status=201');

  const stored = await storedReports(request);
  // If context were captured at init, the token-bearing URL would be missing.
  expect(stored.reports[0]!.context.url).toContain('token=[REDACTED]');
});

test('E: the viewport reflects the current window size', async ({ page, request }) => {
  await page.setViewportSize({ width: 1024, height: 640 });
  const expected = await page.evaluate(() => ({
    width: window.innerWidth,
    height: window.innerHeight,
  }));

  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText('ok status=201');

  const stored = await storedReports(request);
  expect(stored.reports[0]!.context.viewport).toEqual(expected);
});

test('F: a retry keeps the reportId and re-collects the context', async ({
  page,
  request,
}) => {
  await setMode(request, 'fail500');

  await page.getByTestId('context-lab-navigate').click();
  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-description').fill('retry with fresh context');
  await page.getByTestId('mseismo-send').click();
  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:failure');

  await setMode(request, 'ok');
  // Change the URL between attempts to prove context is re-sampled on retry.
  // (The modal overlay blocks page buttons, so drive history directly.)
  await page.evaluate(() => history.replaceState(null, '', location.pathname));
  await page.getByTestId('mseismo-send').click();
  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:success');

  const logText = await page.getByTestId('event-log').innerText();
  const ids = [...logText.matchAll(/submit:start id=([0-9a-f-]+)/gi)].map((m) => m[1]);
  expect(ids.length).toBeGreaterThanOrEqual(2);
  expect(ids[1]).toBe(ids[0]);

  const stored = await storedReports(request);
  expect(stored.count).toBe(1);
  const url: string = stored.reports[0]!.context.url;
  expect(url).toContain('localhost:5173');
  expect(url).not.toContain('token');
});
