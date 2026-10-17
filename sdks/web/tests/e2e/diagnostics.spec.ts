import { expect, test, type Page } from '@playwright/test';

const RECEIVER = 'http://localhost:4010';

interface DiagnosticEvent {
  id: string;
  kind: 'error' | 'unhandledrejection' | 'captured';
  timestamp: string;
  error: { type: string; message: string; stack?: string; cause?: unknown };
  handled: boolean;
  context?: Record<string, unknown>;
  flags?: string[];
}

async function collect(page: Page): Promise<DiagnosticEvent[]> {
  await page.getByTestId('diag-show-buffer').click();
  const text = await page.getByTestId('diagnostics-buffer').innerText();
  return JSON.parse(text) as DiagnosticEvent[];
}

test.beforeEach(async ({ page, request }) => {
  await request.post(`${RECEIVER}/__reset`);
  await page.goto('/');
});

test('A: an uncaught TypeError is captured automatically', async ({ page }) => {
  await page.getByTestId('diag-uncaught-typeerror').click();

  await expect.poll(async () => (await collect(page)).length).toBe(1);
  const [event] = await collect(page);
  expect(event!.kind).toBe('error');
  expect(event!.handled).toBe(false);
  expect(event!.error.type).toBe('TypeError');
  expect(event!.error.message).toContain('diagnostics TypeError');
  expect(typeof event!.error.stack).toBe('string');
});

test('B: captureException records a handled event with sanitized context', async ({
  page,
}) => {
  await page.getByTestId('diag-capture-exception').click();

  await expect.poll(async () => (await collect(page)).length).toBe(1);
  const [event] = await collect(page);
  expect(event!.kind).toBe('captured');
  expect(event!.handled).toBe(true);
  expect(event!.error.type).toBe('Error');
  expect(event!.context).toEqual({
    feature: 'checkout',
    userId: 'user-42',
    nested: { step: 3, retries: 1 },
  });
});

test('C: a non-Error rejection is captured and flagged', async ({ page }) => {
  await page.getByTestId('diag-reject-string').click();

  await expect.poll(async () => (await collect(page)).length).toBe(1);
  const [event] = await collect(page);
  expect(event!.kind).toBe('unhandledrejection');
  expect(event!.handled).toBe(false);
  expect(event!.flags).toContain('non-error-rejection');
});

test('D: resource load failures are not captured', async ({ page }) => {
  await page.getByTestId('diag-broken-image').click();
  await page.getByTestId('diag-broken-script').click();
  await page.waitForTimeout(300);

  expect((await collect(page)).length).toBe(0);
});

test('E: a cross-origin script error is captured without crashing', async ({ page }) => {
  await page.getByTestId('diag-cross-origin').click();

  await expect.poll(async () => (await collect(page)).length).toBe(1);
  const [event] = await collect(page);
  expect(event!.kind).toBe('error');

  // Chrome masks cross-origin script errors down to "Script error." (so we flag
  // it); Firefox and Safari expose the real message. Either is acceptable — the
  // point is that we capture the event and never fabricate hidden detail.
  if (event!.flags?.includes('cross-origin-script-error')) {
    expect(event!.error.message.toLowerCase()).toContain('script error');
  } else {
    expect(event!.error.message).toContain('cross-origin script failure');
  }
});

test('F: flooding errors is capped by the default buffer size', async ({ page }) => {
  await page.getByTestId('diag-flood').click();

  await expect.poll(async () => (await collect(page)).length).toBe(50);
});

test('G: re-init keeps events and applies a smaller maxEvents', async ({ page }) => {
  for (let i = 0; i < 10; i += 1) {
    await page.getByTestId('diag-uncaught-typeerror').click();
  }
  await expect.poll(async () => (await collect(page)).length).toBe(10);

  await page.getByTestId('diag-reinit-small').click();
  expect((await collect(page)).length).toBe(5);
});

test('H: re-init with capture disabled stops recording', async ({ page }) => {
  await page.getByTestId('diag-capture-exception').click();
  await expect.poll(async () => (await collect(page)).length).toBe(1);

  await page.getByTestId('diag-reinit-disabled').click();
  await page.getByTestId('diag-uncaught-typeerror').click();
  await page.getByTestId('diag-reject-error').click();
  await page.waitForTimeout(200);

  expect((await collect(page)).length).toBe(1);
});

test('I: clearing the buffer empties it', async ({ page }) => {
  await page.getByTestId('diag-capture-exception').click();
  await expect.poll(async () => (await collect(page)).length).toBe(1);

  await page.getByTestId('diag-clear-buffer').click();
  await page.getByTestId('diag-show-buffer').click();
  const text = await page.getByTestId('diagnostics-buffer').innerText();
  expect(JSON.parse(text)).toEqual([]);
});
