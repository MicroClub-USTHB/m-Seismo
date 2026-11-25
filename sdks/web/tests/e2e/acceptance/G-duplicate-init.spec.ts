/**
 * Acceptance G — `init()` is idempotent. Calling it repeatedly must not
 * duplicate the launcher, the shortcut listener, the form, or the report.
 */
import { expect, test } from '@playwright/test';
import {
  getReports,
  openForm,
  resetReceiver,
  submitDescription,
  waitForFormClosed,
  waitForSuccess,
} from './helpers';

test.beforeEach(async ({ page, request }) => {
  await resetReceiver(request);
  await page.goto('/');
});

test('G: repeated init() keeps exactly one launcher and one dialog', async ({
  page,
  request,
}) => {
  await page.getByTestId('init-twice').click();
  await page.getByTestId('init-twice').click();

  await expect(page.getByTestId('mseismo-launcher')).toHaveCount(1);

  await page.getByTestId('mseismo-launcher').click();
  await expect(page.getByTestId('mseismo-dialog')).toHaveCount(1);
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();

  await submitDescription(page, 'one report, not three');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  expect(await getReports(request)).toHaveLength(1);
});

test('G: the shortcut opens exactly one dialog after repeated init()', async ({ page }) => {
  await page.getByTestId('init-twice').click();
  await page.getByRole('heading', { level: 1 }).click();
  await page.keyboard.press('Control+Enter');

  await expect(page.getByTestId('mseismo-dialog')).toHaveCount(1);
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
});

test('G: destroy() removes the launcher, and a later init() restores just one', async ({
  page,
}) => {
  await page.getByTestId('destroy').click();
  await expect(page.getByTestId('mseismo-launcher')).toHaveCount(0);

  await page.getByTestId('init-twice').click();
  await expect(page.getByTestId('mseismo-launcher')).toHaveCount(1);
});

test('G: re-init after destroy() leaves exactly one working instance', async ({ page, request }) => {
  await page.getByTestId('destroy').click();
  await expect(page.getByTestId('mseismo-launcher')).toHaveCount(0);

  await page.getByTestId('init-twice').click();
  await page.getByTestId('init-twice').click();
  await expect(page.getByTestId('mseismo-launcher')).toHaveCount(1);

  await openForm(page);
  await expect(page.getByTestId('mseismo-dialog')).toHaveCount(1);
  await submitDescription(page, 'one report after a destroy/init cycle');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  expect(await getReports(request)).toHaveLength(1);
});
