/**
 * Acceptance D — privacy boundary.
 *
 * Verifies what the shipped (baseline) scrubber *does* protect, and pins the
 * known Phase 6 gaps as expected failures so they cannot silently regress into
 * "passing" privacy claims.
 */
import { expect, test } from '@playwright/test';
import { FAKE_SECRETS } from '../../../test-app/src/lab/fixtures/secrets';
import {
  getReports,
  openForm,
  RECEIVER,
  resetReceiver,
  submitDescription,
  waitForFormClosed,
  waitForSuccess,
} from './helpers';

test.beforeEach(async ({ page, request }) => {
  await resetReceiver(request);
  await page.goto('/');
});

test('D: URL query secrets are redacted in context and network failures', async ({
  page,
  request,
}) => {
  // 1. Put a secret in the page URL (kept out of the report).
  await page.getByTestId('context-lab-navigate').click();

  // 2. Produce a failed cross-origin request that also carries a secret.
  await page.evaluate(async (receiver) => {
    const response = await fetch(
      `${receiver}/api/status/500?token=fake-secret-123&api_key=abcd1234efgh5678ijkl9012mnop3456`,
    );
    return response.status;
  }, RECEIVER);

  await openForm(page);
  await submitDescription(page, 'privacy check');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const report = (await getReports(request))[0]!;

  // The URL keeps the non-sensitive parameters but hides sensitive ones.
  expect(report.context.url).toBeDefined();
  expect(report.context.url).not.toContain('fake-secret-123');
  expect(report.context.url).toContain('token=');
  expect(report.context.url).toContain('utm_source=docs');

  // Network failures are recorded with sanitized URLs only.
  expect(report.networkFailures.length).toBeGreaterThan(0);
  for (const failure of report.networkFailures) {
    expect(failure.url).not.toContain('fake-secret-123');
    expect(failure.url).not.toContain('abcd1234efgh5678ijkl9012mnop3456');
    expect(failure.url).toContain('[REDACTED]');
  }

  // Nothing anywhere in the payload carries the raw URL secret.
  expect(JSON.stringify(report)).not.toContain('fake-secret-123');
});

test('D: a known token shape in an error message is scrubbed', async ({ page, request }) => {
  const pageError = page.waitForEvent('pageerror');
  await page.evaluate((secret) => {
    setTimeout(() => {
      throw new Error(`request failed token=${secret}`);
    }, 0);
  }, FAKE_SECRETS.longToken);
  await pageError;

  await openForm(page);
  await submitDescription(page, 'the request with the token failed');
  await waitForSuccess(page);
  await waitForFormClosed(page);

  const report = (await getReports(request))[0]!;
  expect(report.type).toBe('manual_with_error');
  expect(report.error?.message).not.toContain(FAKE_SECRETS.longToken);
  expect(JSON.stringify(report)).not.toContain(FAKE_SECRETS.longToken);
});

test.fail(
  'D: KNOWN GAP (Phase 6) — a JWT in the free-text description is sent verbatim',
  async ({ page, request }) => {
    await openForm(page);
    await submitDescription(page, `our token is ${FAKE_SECRETS.jwt} and it leaked`);
    await waitForSuccess(page);
    await waitForFormClosed(page);

    const report = (await getReports(request))[0]!;
    expect(report.description).toContain('our token is');
    expect(JSON.stringify(report)).not.toContain(FAKE_SECRETS.jwt);
  },
);

test.fail(
  'D: KNOWN GAP (Phase 6) — a prose password without a separator is not scrubbed',
  async ({ page, request }) => {
    await openForm(page);
    await submitDescription(page, `it broke after I typed: ${FAKE_SECRETS.prosePassword}`);
    await waitForSuccess(page);
    await waitForFormClosed(page);

    const report = (await getReports(request))[0]!;
    expect(JSON.stringify(report)).not.toContain(FAKE_SECRETS.prosePassword);
  },
);
