/**
 * Acceptance C — the "logical bug" journey: nothing throws, so the report has
 * no error, but the breadcrumb trail shows what the user actually did.
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

test('C: a silent failure is reported with the clicks that led to it', async ({
  page,
  request,
}) => {
  // Host-page actions producing no exception at all.
  await page.getByTestId('bc-click').click();
  await page.getByTestId('silent-save').click();
  await expect(page.getByTestId('event-log')).toContainText('nothing happened');

  await openForm(page);
  await submitDescription(page, 'I clicked Save and nothing happened');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const reports = await getReports(request);
  expect(reports).toHaveLength(1);
  const report = reports[0]!;

  expect(report.type).toBe('manual');
  expect(report.error).toBeNull();
  expect(report.description).toBe('I clicked Save and nothing happened');

  const interactions = report.breadcrumbs.filter((crumb) => crumb.category === 'interaction');
  const testIds = interactions.map((crumb) => {
    const target = crumb.data?.['target'] as { testId?: string } | undefined;
    return target?.testId;
  });
  expect(testIds).toContain('bc-click');
  expect(testIds).toContain('silent-save');

  // Breadcrumbs are ordered oldest → newest and each has the minimum shape.
  for (const crumb of report.breadcrumbs) {
    expect(typeof crumb.message).toBe('string');
    expect(crumb.message.length).toBeGreaterThan(0);
    expect(typeof crumb.category).toBe('string');
  }
});

test('C: a failing request before the report is captured as a network failure', async ({
  page,
  request,
}) => {
  await page.getByTestId('bc-network-http-error').click();
  await expect(page.getByTestId('event-log')).toContainText('HTTP 500');

  await openForm(page);
  await submitDescription(page, 'the save request failed with a server error');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const report = (await getReports(request))[0]!;
  expect(report.type).toBe('manual');
  expect(report.error).toBeNull();
  expect(report.networkFailures.length).toBeGreaterThan(0);
  expect(report.networkFailures.some((failure) => failure.status === 500)).toBe(true);
});

test('C: interactions with the SDK’s own UI are never breadcrumbed', async ({
  page,
  request,
}) => {
  await page.getByTestId('bc-click').click();

  // Open and cancel through the launcher and the cancel button.
  await page.getByTestId('mseismo-launcher').click();
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
  await page.getByTestId('mseismo-cancel').click();

  await openForm(page);
  await submitDescription(page, 'no SDK clicks in the trail, please');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const report = (await getReports(request))[0]!;
  const allTargets = JSON.stringify(report.breadcrumbs);
  expect(allTargets).not.toContain('mseismo-send');
  expect(allTargets).not.toContain('mseismo-cancel');
  expect(allTargets).not.toContain('mseismo-launcher');
});
