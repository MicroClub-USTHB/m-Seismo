import { expect, test, type APIRequestContext } from '@playwright/test';

const RECEIVER = 'http://localhost:4010';

interface StoredReport {
  schemaVersion: number;
  reportId: string;
  type: string;
  trigger: string;
  description: string | null;
  error: {
    type: string;
    message: string;
    flags?: string[];
    fingerprint?: string;
    occurrences?: number;
    context?: Record<string, unknown>;
  } | null;
}

interface ReportsResponse {
  count: number;
  reports: StoredReport[];
}

async function reports(request: APIRequestContext): Promise<ReportsResponse> {
  return (await (await request.get(`${RECEIVER}/__reports`)).json()) as ReportsResponse;
}

async function stats(request: APIRequestContext): Promise<{
  storedCount: number;
  byType: Record<string, number>;
  byFingerprint: Record<string, number>;
}> {
  return (await (await request.get(`${RECEIVER}/__stats`)).json()) as {
    storedCount: number;
    byType: Record<string, number>;
    byFingerprint: Record<string, number>;
  };
}

async function submitForm(page: import('@playwright/test').Page, text: string): Promise<void> {
  await page.getByTestId('mseismo-description').fill(text);
  await page.getByTestId('mseismo-send').click();
  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:success');
}

test.beforeEach(async ({ page, request }) => {
  await request.post(`${RECEIVER}/__reset`);
  await page.goto('/');
});

test('A: a manual form report is typed manual and carries schemaVersion 2', async ({
  page,
  request,
}) => {
  await page.getByTestId('rp-open-form').click();
  await submitForm(page, 'processor lab manual report');

  const stored = await reports(request);
  expect(stored.count).toBe(1);
  const report = stored.reports[0]!;
  expect(report.schemaVersion).toBe(2);
  expect(report.type).toBe('manual');
  expect(report.trigger).toBe('form');
  expect(report.description).toBe('processor lab manual report');
  expect(report.error).toBeNull();
});

test('B: a manual report made after an error becomes manual_with_error', async ({
  page,
  request,
}) => {
  // Disable captured sending so only the manual report is stored.
  await page.getByTestId('rp-reinit-no-capture').click();
  await page.getByTestId('rp-seed-error-then-form').click();
  await submitForm(page, 'it broke right before I reported');

  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const report = (await reports(request)).reports[0]!;
  expect(report.type).toBe('manual_with_error');
  expect(report.trigger).toBe('form');
  expect(report.error?.fingerprint).toMatch(/^[0-9a-f]{16}$/);
  expect(report.error?.occurrences).toBeGreaterThanOrEqual(1);
});

test('C: autoReport=errors sends an uncaught error as automatic_error', async ({
  page,
  request,
}) => {
  await page.getByTestId('rp-reinit-auto-errors').click();
  await page.getByTestId('rp-uncaught').click();

  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const report = (await reports(request)).reports[0]!;
  expect(report.type).toBe('automatic_error');
  expect(report.trigger).toBe('automatic');
  expect(report.description).toBeNull();
  expect(report.error?.message).toContain('automatic uncaught error');
});

test('D: autoReport=off does not send automatically', async ({ page, request }) => {
  await page.getByTestId('rp-uncaught').click();
  await page.waitForTimeout(500);
  expect((await reports(request)).count).toBe(0);
});

test('E: captureException is sent as captured_exception with secrets scrubbed', async ({
  page,
  request,
}) => {
  await page.getByTestId('rp-capture-exception').click();

  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const report = (await reports(request)).reports[0]!;
  expect(report.type).toBe('captured_exception');
  expect(report.trigger).toBe('api');
  expect(report.error?.fingerprint).toMatch(/^[0-9a-f]{16}$/);
  expect(report.error?.context?.['feature']).toBe('checkout');
  expect(report.error?.context?.['token']).toBe('[REDACTED]');
  expect(JSON.stringify(report.error)).not.toContain('super-secret-token-value-1234567890');
});

test('F: captureMessage is sent as captured_message', async ({ page, request }) => {
  await page.getByTestId('rp-capture-message').click();

  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const report = (await reports(request)).reports[0]!;
  expect(report.type).toBe('captured_message');
  expect(report.error?.type).toBe('Message');
  expect(report.error?.message).toContain('captured message');
});

test('G: repeats inside the window are deduped, then counted on the next send', async ({
  page,
  request,
}) => {
  // Tiny window (5s) + generous caps so the second burst re-sends.
  await page.getByTestId('rp-reinit-small-limits').click();

  await page.getByTestId('rp-repeat-10').click();
  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const first = (await reports(request)).reports[0]!;
  expect(first.error?.occurrences).toBe(1);

  // After the window closes, the next occurrence is sent and reports the
  // accumulated count (10 previous + this one).
  await page.waitForTimeout(5_500);
  await page.getByTestId('rp-repeat-10').click();
  await expect.poll(async () => (await reports(request)).count).toBe(2);
  const second = (await reports(request)).reports[1]!;
  expect(second.error?.fingerprint).toBe(first.error?.fingerprint);
  expect(second.error?.occurrences).toBe(11);
});

test('H: the same message from two code paths yields two fingerprints', async ({
  page,
  request,
}) => {
  await page.getByTestId('rp-two-paths').click();

  await expect.poll(async () => (await reports(request)).count).toBe(2);
  const fingerprints = (await reports(request)).reports.map((r) => r.error?.fingerprint);
  expect(fingerprints[0]).not.toBe(fingerprints[1]);
  expect(fingerprints[0]).toMatch(/^[0-9a-f]{16}$/);
});

test('I: five different messages produce five reports', async ({ page, request }) => {
  await page.getByTestId('rp-different-messages').click();

  await expect.poll(async () => (await reports(request)).count).toBe(5);
  const types = new Set((await reports(request)).reports.map((r) => r.type));
  expect(types).toEqual(new Set(['captured_message']));
});

test('J: an error without a stack gets a weak-fingerprint flag', async ({ page, request }) => {
  await page.getByTestId('rp-weak-no-stack').click();

  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const report = (await reports(request)).reports[0]!;
  expect(report.error?.fingerprint).toMatch(/^[0-9a-f]{16}$/);
  expect(report.error?.flags).toContain('weak-fingerprint');
});

test('K: a cross-origin script error is auto-reported with a weak fingerprint', async ({
  page,
  request,
}) => {
  await page.getByTestId('rp-reinit-auto-errors').click();
  await page.getByTestId('rp-cross-origin').click();

  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const report = (await reports(request)).reports[0]!;
  expect(report.type).toBe('automatic_error');
  expect(report.error?.fingerprint).toMatch(/^[0-9a-f]{16}$/);

  // Chrome masks a cross-origin script error to "Script error." and the
  // collector flags it (which forces a weak fingerprint). Firefox can expose a
  // real stack instead, so only assert the weak flag when it was flagged.
  if (report.error?.flags?.includes('cross-origin-script-error')) {
    expect(report.error?.flags).toContain('weak-fingerprint');
  }
});

test('L: tiny limits rate-limit automatic/captured sends', async ({ page, request }) => {
  await page.getByTestId('rp-reinit-small-limits').click();
  await page.getByTestId('rp-different-messages').click();

  await expect.poll(async () => (await reports(request)).count).toBe(2);
  await page.waitForTimeout(300);
  const summary = await stats(request);
  expect(summary.storedCount).toBe(2);
  expect(summary.byType['captured_message']).toBe(2);
});

test('M: beforeSend dropping automatic reports keeps manual ones', async ({
  page,
  request,
}) => {
  await page.getByTestId('rp-reinit-beforesend-drop').click();
  await page.getByTestId('rp-uncaught').click();
  await page.waitForTimeout(500);
  expect((await reports(request)).count).toBe(0);

  // The manual "send test report" button bypasses the automatic path.
  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText('send test report: ok');
  expect((await reports(request)).count).toBe(1);
});

test('N: a throwing beforeSend still sends the report unmodified', async ({
  page,
  request,
}) => {
  await page.getByTestId('rp-reinit-beforesend-throw').click();
  await page.getByTestId('rp-uncaught').click();

  await expect.poll(async () => (await reports(request)).count).toBe(1);
  const report = (await reports(request)).reports[0]!;
  expect(report.type).toBe('automatic_error');
  expect(report.error?.message).toContain('automatic uncaught error');
});

test('O: receiver stats group stored reports by type and fingerprint', async ({
  page,
  request,
}) => {
  await page.getByTestId('send-report').click();
  await expect(page.getByTestId('event-log')).toContainText('send test report: ok');
  await page.getByTestId('rp-capture-exception').click();
  await page.getByTestId('rp-capture-message').click();

  await expect.poll(async () => (await reports(request)).count).toBe(3);
  const summary = await stats(request);
  expect(summary.byType['manual']).toBe(1);
  expect(summary.byType['captured_exception']).toBe(1);
  expect(summary.byType['captured_message']).toBe(1);
  expect(Object.keys(summary.byFingerprint).length).toBeGreaterThanOrEqual(1);
});
