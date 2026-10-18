import { describe, expect, it } from 'vitest';
import { scrubText, scrubReport, SCRUB_MARKER } from '@sdk/privacy/scrub';
import { buildReport } from '@m-seismo/web';
import type { ReportBreadcrumb, ReportContext } from '@m-seismo/web';

const context: ReportContext = {
  timestamp: new Date('2026-01-02T03:04:05.000Z').toISOString(),
  application: {},
};

describe('scrubText', () => {
  it('redacts a Bearer token', () => {
    const out = scrubText('Authorization: Bearer fake-secret-token-value');
    expect(out).toContain(SCRUB_MARKER);
    expect(out).not.toContain('fake-secret-token-value');
  });

  it('redacts a JWT', () => {
    const jwt = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abc123signature';
    expect(scrubText(`token ${jwt}`)).not.toContain(jwt);
  });

  it('redacts token/secret/password key-value pairs', () => {
    expect(scrubText('token=abcd1234efgh')).toContain(SCRUB_MARKER);
    expect(scrubText('password: "hunter2"')).toContain(SCRUB_MARKER);
    expect(scrubText('api_key=abcdef123456')).toContain(SCRUB_MARKER);
  });

  it('redacts long hex strings', () => {
    expect(scrubText('hash a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6')).toContain(SCRUB_MARKER);
  });

  it('leaves ordinary text alone', () => {
    expect(scrubText('TypeError: cannot read properties of undefined')).toBe(
      'TypeError: cannot read properties of undefined',
    );
  });
});

describe('scrubReport', () => {
  it('scrubs breadcrumb messages and network URLs', () => {
    const breadcrumb: ReportBreadcrumb = {
      timestamp: context.timestamp,
      category: 'sdk',
      message: 'token=abcd1234',
      level: 'info',
    };
    const report = buildReport({
      description: 'user text token=abcd1234',
      context,
      breadcrumbs: [breadcrumb],
      networkFailures: [
        {
          timestamp: context.timestamp,
          method: 'GET',
          url: 'https://api.test/x?access_token=abcd1234',
          durationMs: 1,
          category: 'http_error',
          status: 500,
        },
      ],
    });
    const scrubbed = scrubReport(report);
    expect(scrubbed.breadcrumbs[0]!.message).toContain(SCRUB_MARKER);
    expect(scrubbed.networkFailures[0]!.url).toContain(SCRUB_MARKER);
    // The user description is intentionally NOT scrubbed in this baseline.
    expect(scrubbed.description).toBe('user text token=abcd1234');
  });

  it('never mutates the input', () => {
    const report = buildReport({
      description: 'x',
      context,
      error: { type: 'Error', message: 'token=abcd1234' },
    });
    scrubReport(report);
    expect(report.error?.message).toBe('token=abcd1234');
  });
});
