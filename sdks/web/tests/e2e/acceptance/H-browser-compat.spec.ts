/**
 * Acceptance H — browser compatibility.
 *
 * Runs the core journey once per Playwright project (Chromium, Firefox,
 * WebKit). If a browser is missing required OS libraries the project cannot
 * launch at all; that is reported as "not run", never as passing.
 */
import { expect, test } from '@playwright/test';
import {
  getReports,
  openForm,
  pngBuffer,
  resetReceiver,
  submitDescription,
  waitForFormClosed,
  waitForSuccess,
} from './helpers';

const BROWSER_TOKEN: Record<string, string> = {
  chromium: 'chrome',
  firefox: 'firefox',
  webkit: 'safari',
};

test.beforeEach(async ({ page, request }) => {
  await resetReceiver(request);
  await page.goto('/');
});

test('H: the full journey works in this browser without page errors', async ({
  page,
  request,
  browserName,
}) => {
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

  // Shadow DOM + async file reading + cross-origin fetch all at once.
  await openForm(page);
  await expect(page.getByTestId('mseismo-dialog')).toHaveAttribute('aria-modal', 'true');
  await page.getByTestId('mseismo-description').fill(`compat run on ${browserName}`);
  await page
    .getByTestId('mseismo-screenshot')
    .setInputFiles({ name: 'compat.png', mimeType: 'image/png', buffer: pngBuffer() });
  await expect(page.getByTestId('mseismo-file-info')).toContainText('compat.png');

  await page.getByTestId('mseismo-send').click();
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const reports = await getReports(request);
  expect(reports).toHaveLength(1);
  const report = reports[0]!;

  expect(report.description).toBe(`compat run on ${browserName}`);

  // The SDK reports the real browser and a sane viewport.
  expect(report.context.browser?.name).toBeTruthy();
  expect(report.context.browser?.name?.toLowerCase()).toContain(
    BROWSER_TOKEN[browserName] ?? browserName,
  );
  expect(report.context.viewport?.width).toBeGreaterThan(0);
  expect(report.context.viewport?.height).toBeGreaterThan(0);

  // The page must not have thrown anything unexpected during the journey.
  expect(pageErrors).toEqual([]);
});

test('H: the keyboard shortcut and Escape work in this browser', async ({ page }) => {
  await page.getByRole('heading', { level: 1 }).click();
  await page.keyboard.press('Control+Enter');
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(page.getByTestId('mseismo-dialog')).toBeHidden();
});

test('H: the launcher and form are reachable by role and name', async ({ page }) => {
  const launcher = page.getByTestId('mseismo-launcher');
  await expect(launcher).toBeVisible();
  await expect(launcher).toHaveAttribute('aria-label', 'Report a bug');
  await page.getByRole('button', { name: 'Report a bug' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('textbox')).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Send report' })).toBeVisible();
});
