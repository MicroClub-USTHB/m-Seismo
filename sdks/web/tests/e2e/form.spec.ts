import { expect, test } from '@playwright/test';

const RECEIVER = 'http://localhost:4010';
const ENDPOINT = `${RECEIVER}/api/reports`;

async function setMode(
  request: import('@playwright/test').APIRequestContext,
  mode: string,
  delayMs?: number,
): Promise<void> {
  const response = await request.post(`${RECEIVER}/__mode`, {
    data: delayMs === undefined ? { mode } : { mode, delayMs },
  });
  expect(response.ok()).toBe(true);
}

test.beforeEach(async ({ page, request }) => {
  await request.post(`${RECEIVER}/__reset`);
  await page.goto('/');
});

test('A: opens via the launcher-independent button with all fields', async ({ page }) => {
  await page.getByTestId('open-form').click();

  const dialog = page.getByTestId('mseismo-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute('role', 'dialog');
  await expect(dialog).toHaveAttribute('aria-modal', 'true');
  await expect(page.getByTestId('mseismo-description')).toBeVisible();
  await expect(page.getByTestId('mseismo-screenshot')).toBeVisible();
  await expect(page.getByTestId('mseismo-send')).toBeVisible();
  await expect(page.getByTestId('mseismo-cancel')).toBeVisible();
});

test('B: opens via the floating launcher button', async ({ page }) => {
  await expect(page.getByTestId('mseismo-launcher')).toBeVisible();
  await page.getByTestId('mseismo-launcher').click();
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
});

test('C: Ctrl+Enter opens the form from a non-typing context', async ({ page }) => {
  await page.getByRole('heading', { level: 1 }).click();
  await page.keyboard.press('Control+Enter');
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
});

test('D: an empty description is rejected and nothing is sent', async ({
  page,
  request,
}) => {
  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-send').click();

  await expect(page.getByTestId('mseismo-message')).toContainText('describe');
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(0);
});

test('E: a valid report is stored and the form closes', async ({
  page,
  request,
}) => {
  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-description').fill('The export button does nothing');
  await page.getByTestId('mseismo-send').click();

  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:success');
  await expect(page.getByTestId('mseismo-dialog')).toBeHidden();

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(1);
  const report = stored.reports[0];
  expect(report.description).toBe('The export button does nothing');
  expect(report.error).toBeNull();
  expect(report.screenshot).toBeNull();
  expect(report.context.url).toContain('localhost:5173');
});

test('F: a picked screenshot is attached and stored', async ({ page, request }) => {
  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-description').fill('screenshot attached');
  await page
    .getByTestId('mseismo-screenshot')
    .setInputFiles({
      name: 'shot.png',
      mimeType: 'image/png',
      buffer: Buffer.from([137, 80, 78, 71]),
    });
  await expect(page.getByTestId('mseismo-file-info')).toContainText('shot.png');

  await page.getByTestId('mseismo-send').click();
  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:success');

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(1);
  const screenshot = stored.reports[0].screenshot;
  expect(screenshot.mimeType).toBe('image/png');
  expect(screenshot.name).toBe('shot.png');
  expect(screenshot.sizeBytes).toBe(4);
  expect(screenshot.dataBase64Length).toBeGreaterThan(0);

  const full = await (await request.get(`${RECEIVER}/__reports?full=1`)).json();
  expect(typeof full.reports[0].screenshot.dataBase64).toBe('string');
  expect(full.reports[0].screenshot.dataBase64.length).toBeGreaterThan(0);
});

test('G: a failure keeps the form open and retry reuses the reportId', async ({
  page,
  request,
}) => {
  await setMode(request, 'fail500');

  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-description').fill('retry me');
  await page.getByTestId('mseismo-send').click();

  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:failure');
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
  await expect(page.getByTestId('mseismo-send')).toHaveText('Try again');

  await setMode(request, 'ok');
  await page.getByTestId('mseismo-send').click();
  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:success');

  const logText = await page.getByTestId('event-log').innerText();
  const ids = [...logText.matchAll(/submit:start id=([0-9a-f-]+)/gi)].map((m) => m[1]);
  expect(ids.length).toBeGreaterThanOrEqual(2);
  expect(ids[1]).toBe(ids[0]);

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(1);
});

test('H: cancel discards the typed text', async ({ page }) => {
  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-description').fill('discard this');
  await page.getByTestId('mseismo-cancel').click();
  await expect(page.getByTestId('mseismo-dialog')).toBeHidden();

  await page.getByTestId('open-form').click();
  await expect(page.getByTestId('mseismo-description')).toHaveValue('');
});

test('I: Escape closes the form', async ({ page }) => {
  await page.getByTestId('open-form').click();
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('mseismo-dialog')).toBeHidden();
});

test('J: the form is isolated from hostile page CSS', async ({ page }) => {
  await page.getByTestId('open-form').click();
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();

  await page.addStyleTag({ content: '* { color: rgb(255, 0, 0) !important; }' });

  const titleColor = await page.getByTestId('mseismo-dialog').evaluate((dialog) => {
    const title = dialog.querySelector('.title');
    return title ? getComputedStyle(title).color : null;
  });

  expect(titleColor).not.toBeNull();
  expect(titleColor).not.toBe('rgb(255, 0, 0)');
});

test('K: a slow-but-successful server still stores the report', async ({
  page,
  request,
}) => {
  // Delay is below the SDK timeout (10s), so the report should succeed.
  await setMode(request, 'slow', 2000);

  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-description').fill('slow but fine');

  const started = Date.now();
  await page.getByTestId('mseismo-send').click();
  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:success');
  expect(Date.now() - started).toBeGreaterThanOrEqual(2000);

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(1);
  expect(stored.reports[0].description).toBe('slow but fine');
});

test('L: a server slower than the timeout fails with kind=timeout and stays open', async ({
  page,
  request,
}) => {
  test.setTimeout(30_000);
  // Delay exceeds the SDK timeout (10s): the transport must abort and report a
  // timeout, and the form must remain usable for a retry.
  await setMode(request, 'slow', 12_000);

  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-description').fill('too slow');
  await page.getByTestId('mseismo-send').click();

  await expect(page.getByTestId('event-log')).toContainText(
    'sdk event: submit:failure id=',
    { timeout: 15_000 },
  );
  await expect(page.getByTestId('event-log')).toContainText('kind=timeout');
  await expect(page.getByTestId('mseismo-message')).toContainText('timed out');
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
  await expect(page.getByTestId('mseismo-send')).toHaveText('Try again');

  const stored = await (await request.get(`${RECEIVER}/__reports`)).json();
  expect(stored.count).toBe(0);
});
