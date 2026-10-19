import { describe, expect, it } from 'vitest';
import { buildReport, sanitize } from '@m-seismo/web';
import type { ReportBreadcrumb, ReportContext, SanitizedReport } from '@m-seismo/web';

const context: ReportContext = {
  timestamp: new Date('2026-01-02T03:04:05.000Z').toISOString(),
  application: {},
};

describe('sanitize (privacy boundary + baseline scrubber)', () => {
  it('returns a structurally identical copy when there is nothing to scrub', () => {
    const report = buildReport({ type: 'manual', trigger: 'form', description: 'x', context });
    const sanitized = sanitize(report);
    expect(sanitized).toEqual(report);
  });

  it('returns the branded SanitizedReport type', () => {
    // Compile-time assertion: only sanitize() may produce a SanitizedReport.
    const sanitized: SanitizedReport = sanitize(
      buildReport({ type: 'manual', trigger: 'form', description: 'x', context }),
    );
    expect(sanitized.description).toBe('x');
  });

  it('redacts a Bearer token in a breadcrumb message', () => {
    const breadcrumb: ReportBreadcrumb = {
      timestamp: context.timestamp,
      category: 'sdk',
      message: 'Authorization: Bearer fake-secret-token-value',
      level: 'info',
    };
    const report = buildReport({
      type: 'manual',
      trigger: 'form',
      description: 'x',
      context,
      breadcrumbs: [breadcrumb],
    });
    const sanitized = sanitize(report);
    expect(sanitized.breadcrumbs[0]!.message).toContain('[REDACTED]');
    expect(sanitized.breadcrumbs[0]!.message).not.toContain('fake-secret-token-value');
  });

  it('redacts a JWT-shaped string in an error message', () => {
    const report = buildReport({
      type: 'manual_with_error',
      trigger: 'form',
      description: 'x',
      context,
      error: {
        type: 'Error',
        message: 'failed with eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature',
      },
    });
    const sanitized = sanitize(report);
    expect(sanitized.error?.message).not.toContain('eyJhbGciOiJIUzI1NiJ9');
    expect(sanitized.error?.message).toContain('[REDACTED]');
  });

  it('never mutates the input report', () => {
    const report = buildReport({
      type: 'manual_with_error',
      trigger: 'form',
      description: 'x',
      context,
      error: { type: 'Error', message: 'token=supersecretvalue' },
    });
    sanitize(report);
    expect(report.error?.message).toBe('token=supersecretvalue');
  });
});
