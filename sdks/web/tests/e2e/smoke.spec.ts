import { expect, test } from '@playwright/test';

const BUTTON_TESTIDS = [
  'open-form',
  'throw-sync',
  'unhandled-rejection',
  'fetch-500',
  'fetch-200',
  'silent-save',
  'theme-toggle',
] as const;

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('test app loads and exposes every scenario button', async ({ page }) => {
  await expect(
    page.getByRole('heading', { name: 'm-Seismo Web SDK — test app' }),
  ).toBeVisible();

  for (const testid of BUTTON_TESTIDS) {
    await expect(page.getByTestId(testid)).toBeVisible();
  }
  await expect(page.getByTestId('ctrl-enter-input')).toBeVisible();
  await expect(page.getByTestId('event-log')).toBeVisible();
});

test('theme toggle flips the data-theme attribute', async ({ page }) => {
  const root = page.locator('html');
  const initial = await root.getAttribute('data-theme');
  expect(['light', 'dark']).toContain(initial);

  await page.getByTestId('theme-toggle').click();
  const flipped = await root.getAttribute('data-theme');
  expect(flipped).not.toBe(initial);

  await page.getByTestId('theme-toggle').click();
  await expect(root).toHaveAttribute('data-theme', initial ?? 'light');
});

test('HTTP 500 button results in a logged 500', async ({ page }) => {
  await page.getByTestId('fetch-500').click();
  await expect(page.getByTestId('event-log')).toContainText('HTTP 500 received from /api/fail');
});

test('HTTP 200 button results in a logged 200', async ({ page }) => {
  await page.getByTestId('fetch-200').click();
  await expect(page.getByTestId('event-log')).toContainText('HTTP 200 received from /api/ok');
});

test('Ctrl+Enter updates the indicator and logs the shortcut', async ({ page }) => {
  await page.getByTestId('ctrl-enter-input').click();
  await page.keyboard.press('Control+Enter');
  await expect(page.getByTestId('ctrl-enter-indicator')).toContainText('Ctrl+Enter pressed');
  await expect(page.getByTestId('event-log')).toContainText('Ctrl+Enter pressed');
});

test('uncaught exception and unhandled rejection do not crash the page', async ({
  page,
}) => {
  await page.getByTestId('throw-sync').click();
  await page.getByTestId('unhandled-rejection').click();
  await page.getByTestId('silent-save').click();

  await expect(page.getByTestId('event-log')).toContainText('uncaught exception thrown');
  await expect(page.getByTestId('event-log')).toContainText(
    'unhandled promise rejection triggered',
  );
  await expect(page.getByTestId('event-log')).toContainText('nothing happened');

  // Page is still alive and interactive after the errors.
  await page.getByTestId('open-form').click();
  await expect(page.getByTestId('event-log')).toContainText('openReportForm() stub called');
});
