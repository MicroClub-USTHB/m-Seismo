/**
 * Acceptance B — an exception happened shortly before the user opened the form.
 * The manual report must carry that error (typed `manual_with_error`) without
 * the user having to reproduce it.
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

test('B: a recent uncaught exception is attached to the manual report', async ({
  page,
  request,
}) => {
  const pageError = page.waitForEvent('pageerror');
  await page.getByTestId('diag-uncaught-typeerror').click();
  const error = await pageError;
  expect(error.name).toBe('TypeError');

  await openForm(page);
  await page.getByTestId('mseismo-description').fill('I saw this error just before reporting');
  await page.getByTestId('mseismo-send').click();

  await waitForSuccess(page);
  await waitForFormClosed(page);

  const reports = await getReports(request);
  expect(reports).toHaveLength(1);
  const report = reports[0]!;

  expect(report.type).toBe('manual_with_error');
  expect(report.trigger).toBe('form');
  expect(report.description).toBe('I saw this error just before reporting');

  expect(report.error).not.toBeNull();
  expect(report.error?.type).toBe('TypeError');
  expect(report.error?.message).toContain('diagnostics TypeError');
  expect(report.error?.fingerprint).toMatch(/^[0-9a-f]{16}$/);
  expect(report.error?.occurrences).toBe(1);

  expect(report.breadcrumbs.some((crumb) => crumb.category === 'error')).toBe(true);
});

test('B: an exception after opening the form is still attached', async ({ page, request }) => {
  await openForm(page);

  const pageError = page.waitForEvent('pageerror');
  await page.evaluate(() => {
    setTimeout(() => {
      throw new Error('thrown while the form was open');
    }, 0);
  });
  await pageError;

  await submitDescription(page, 'and then it threw while typing');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const reports = await getReports(request);
  expect(reports[0]!.type).toBe('manual_with_error');
  expect(reports[0]!.error?.message).toContain('thrown while the form was open');
});

test('B: with no recent error the report stays a plain manual report', async ({
  page,
  request,
}) => {
  await openForm(page);
  await submitDescription(page, 'nothing crashed, this is a feature request');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const reports = await getReports(request);
  expect(reports[0]!.type).toBe('manual');
  expect(reports[0]!.error).toBeNull();
});
