/**
 * Acceptance E — the backend is unavailable or rejects the report. The user
 * must see a clear message, keep their text, and be able to retry.
 */
import { expect, test } from '@playwright/test';
import {
  getReports,
  openForm,
  resetReceiver,
  setMode,
  submitDescription,
  submitStartIds,
  waitForFormClosed,
  waitForSuccess,
} from './helpers';

test.beforeEach(async ({ page, request }) => {
  await resetReceiver(request);
  await page.goto('/');
});

test('E: an unreachable backend keeps the form open and a retry succeeds', async ({
  page,
  request,
}) => {
  await setMode(request, 'offline');

  await openForm(page);
  await submitDescription(page, 'Please do not lose this text');

  await expect(page.getByTestId('mseismo-message')).toContainText(
    'Could not reach the server',
  );
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
  await expect(page.getByTestId('mseismo-send')).toHaveText('Try again');
  await expect(page.getByTestId('mseismo-description')).toHaveValue(
    'Please do not lose this text',
  );
  expect(await getReports(request)).toHaveLength(0);

  await setMode(request, 'ok');
  await page.getByTestId('mseismo-send').click();
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const reports = await getReports(request);
  expect(reports).toHaveLength(1);
  expect(reports[0]!.description).toBe('Please do not lose this text');

  const ids = await submitStartIds(page);
  expect(ids).toHaveLength(2);
  expect(ids[1]).toBe(ids[0]);
});

for (const [mode, status] of [
  ['fail413', 413],
  ['fail500', 500],
] as const) {
  test(`E: HTTP ${status} is reported with its status and nothing is stored`, async ({
    page,
    request,
  }) => {
    await setMode(request, mode);

    await openForm(page);
    await submitDescription(page, `server says ${status}`);

    await expect(page.getByTestId('mseismo-message')).toContainText(`HTTP ${status}`);
    await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
    await expect(page.getByTestId('mseismo-send')).toHaveText('Try again');

    expect(await getReports(request)).toHaveLength(0);
  });
}

test('E: a rate-limited response (429) surfaces the status', async ({ page, request }) => {
  await setMode(request, 'rateLimited');

  await openForm(page);
  await submitDescription(page, 'too many reports');

  await expect(page.getByTestId('mseismo-message')).toContainText('HTTP 429');
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
  expect(await getReports(request)).toHaveLength(0);
});
