/**
 * Acceptance A — the primary user journey: a person opens the form, describes a
 * problem, optionally attaches a screenshot, and the report reaches the backend.
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

test.beforeEach(async ({ page, request }) => {
  await resetReceiver(request);
  await page.goto('/');
});

test('A: a manual report with a screenshot is stored exactly once', async ({ page, request }) => {
  await openForm(page);

  await page.getByTestId('mseismo-description').fill('  The export button does nothing  ');
  await page
    .getByTestId('mseismo-screenshot')
    .setInputFiles({ name: 'shot.png', mimeType: 'image/png', buffer: pngBuffer() });
  await expect(page.getByTestId('mseismo-file-info')).toContainText('shot.png');

  await page.getByTestId('mseismo-send').click();

  await expect(page.getByTestId('mseismo-message')).toContainText('Thanks! Your report was sent.');
  await waitForSuccess(page);
  await expect(page.getByTestId('mseismo-dialog')).toBeHidden();

  const reports = await getReports(request);
  expect(reports).toHaveLength(1);
  const report = reports[0]!;

  expect(report.schemaVersion).toBe(2);
  expect(report.type).toBe('manual');
  expect(report.trigger).toBe('form');
  expect(report.description).toBe('The export button does nothing');
  expect(report.error).toBeNull();
  expect(report.reportId).toMatch(/^[0-9a-f-]{36}$/i);
  expect(report.context.url).toContain('localhost:5173');
  expect(report.context.timestamp).toBeTruthy();

  expect(report.screenshot).toMatchObject({
    name: 'shot.png',
    mimeType: 'image/png',
    sizeBytes: pngBuffer().length,
  });
  expect(report.screenshot?.dataBase64Length).toBeGreaterThan(0);
});

test('A: a manual report without a screenshot is still accepted', async ({ page, request }) => {
  await openForm(page);
  await submitDescription(page, 'No screenshot, just words');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const reports = await getReports(request);
  expect(reports).toHaveLength(1);
  expect(reports[0]!.screenshot).toBeNull();
  expect(reports[0]!.description).toBe('No screenshot, just words');
});

test('A: two manual reports are never deduplicated', async ({ page, request }) => {
  for (let index = 0; index < 2; index += 1) {
    await openForm(page);
    await submitDescription(page, 'the same problem twice');
    await waitForSuccess(page);
    await waitForFormClosed(page);
  }

  const reports = await getReports(request);
  expect(reports).toHaveLength(2);
  expect(reports.every((report) => report.type === 'manual')).toBe(true);
  expect(reports[0]!.reportId).not.toBe(reports[1]!.reportId);
});

test('A: cancelling discards the description and sends nothing', async ({ page, request }) => {
  await openForm(page);
  await page.getByTestId('mseismo-description').fill('this must never leave the browser');
  await page.getByTestId('mseismo-cancel').click();
  await expect(page.getByTestId('mseismo-dialog')).toBeHidden();

  await openForm(page);
  await expect(page.getByTestId('mseismo-description')).toHaveValue('');

  expect(await getReports(request)).toHaveLength(0);
});
