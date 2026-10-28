/**
 * Privacy: BASELINE coverage + explicit KNOWN GAPS.
 *
 * The scrubber in `src/privacy/scrub.ts` is a best-effort baseline (Phase 5.3),
 * NOT the full privacy phase. This file:
 *   1. proves the shapes the baseline DOES redact (table-driven), and
 *   2. lists, as `it.fails`, the secrets the baseline does NOT catch, so Phase 6
 *      turning them green flips the expected failures into normal passes.
 *
 * A `KNOWN GAPS` test must never be written as a plain passing assertion that
 * claims coverage the scrubber does not provide.
 */
import { describe, expect, it } from 'vitest';
import { buildReport, sanitize } from '@m-seismo/web';
import { scrubReport, scrubText } from '@sdk/privacy/scrub';
import type { ReportContext, ReportPayload } from '@sdk/core/types';
import {
  FAKE_SECRETS,
  UNSCRUBBED_SECRETS,
} from '@app/lab/fixtures/secrets';

function context(): ReportContext {
  return {
    url: 'https://app.test/',
    timestamp: '2026-01-01T00:00:00.000Z',
    application: {},
  };
}

function reportWith(description: string | null, errorMessage: string): ReportPayload {
  return buildReport(
    {
      type: description === null ? 'automatic_error' : 'manual',
      trigger: description === null ? 'automatic' : 'form',
      description,
      context: context(),
      error: { type: 'Error', message: errorMessage },
    },
    { reportId: 'r-gap' },
  );
}

describe('baseline scrubber — covered shapes (table-driven)', () => {
  // Each secret is presented in the carrier shape the baseline is documented to
  // catch (a bare token in prose is deliberately NOT always redacted — see the
  // KNOWN GAPS below).
  it.each<
    readonly [string, string, string]
  >([
    ['long token', FAKE_SECRETS.longToken, `request failed: ${FAKE_SECRETS.longToken}`],
    ['JWT', FAKE_SECRETS.jwt, FAKE_SECRETS.jwt],
    ['long hex', FAKE_SECRETS.longHex, `stack seed ${FAKE_SECRETS.longHex}`],
    ['key=value pair', 'abcd1234efgh5678ijkl9012mnop3456', FAKE_SECRETS.keyValue],
    [
      'bearer token',
      'sk-live-abcdefghijklmnopqrstuvwxyz012345',
      `Authorization: ${FAKE_SECRETS.bearer}`,
    ],
  ])('redacts the %s', (_label, rawValue, carrier) => {
    const out = scrubText(carrier);
    expect(out).toContain('[REDACTED]');
    expect(out).not.toContain(rawValue);
  });

  it('redacts a bearer token and a JWT', () => {
    expect(scrubText(`Authorization: ${FAKE_SECRETS.bearer}`)).not.toContain(
      'sk-live-abcdefghijklmnopqrstuvwxyz012345',
    );
    expect(scrubText(FAKE_SECRETS.jwt)).toBe('[REDACTED]');
  });

  it('redacts a sensitive key=value pair', () => {
    const out = scrubText(FAKE_SECRETS.keyValue);
    expect(out).toContain('api_key=[REDACTED]');
    expect(out).not.toContain('abcd1234efgh5678ijkl9012mnop3456');
  });

  it('scrubs error messages through sanitize()', () => {
    const scrubbed = sanitize(reportWith('manual note', `boom ${FAKE_SECRETS.longToken}`));
    expect(scrubbed.error?.message).not.toContain(FAKE_SECRETS.longToken);
  });
});

describe('KNOWN GAPS (Phase 6 must handle)', () => {
  // Each of these is EXPECTED TO FAIL today. When Phase 6 lands, remove the
  // `.fails` (the assertions already describe the desired behaviour).

  it.fails('redacts a free-text password (no separator) in an error message', () => {
    const out = scrubText(UNSCRUBBED_SECRETS[0]!);
    expect(out).not.toContain('correct-horse-battery-staple');
  });

  it.fails('scrubs the user-written description', () => {
    const report = reportWith(`my api_key=${FAKE_SECRETS.longToken}`, 'ok');
    const scrubbed = sanitize(report);
    expect(scrubbed.description).not.toContain(FAKE_SECRETS.longToken);
  });

  it.fails('redacts a secret under a sensitive key name in an error context', () => {
    const report: ReportPayload = {
      ...reportWith(null, 'context gap'),
      error: { type: 'Error', message: 'context gap', context: { password: 'hunter2' } },
    };
    const scrubbed = scrubReport(report);
    expect(scrubbed.error?.context?.['password']).toBe('[REDACTED]');
  });

  it.fails('redacts a long lowercase-only token with no digits or uppercase', () => {
    // LONG_B64 requires a digit AND an uppercase/underscore, so this survives.
    const token = 'abcdefghijklmnopqrstuvwxyzabcdefghijkl';
    expect(scrubText(`key is ${token}`)).not.toContain(token);
  });

  it.fails('redacts personal data (an email address) from a breadcrumb message', () => {
    const out = scrubText('reported by jane.doe@example.com');
    expect(out).not.toContain('jane.doe@example.com');
  });
});
