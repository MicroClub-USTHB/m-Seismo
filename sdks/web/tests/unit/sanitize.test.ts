import { describe, expect, it } from 'vitest';
import { buildReport, sanitize } from '@m-seismo/web';
import type { ReportContext, SanitizedReport } from '@m-seismo/web';

const context: ReportContext = {
  timestamp: new Date('2026-01-02T03:04:05.000Z').toISOString(),
  application: {},
};

describe('sanitize (privacy boundary stub)', () => {
  it('is a pass-through in this phase', () => {
    const report = buildReport({ description: 'x', context });
    const sanitized = sanitize(report);
    expect(sanitized).toBe(report);
  });

  it('returns the branded SanitizedReport type', () => {
    // Compile-time assertion: only sanitize() may produce a SanitizedReport.
    const sanitized: SanitizedReport = sanitize(
      buildReport({ description: 'x', context }),
    );
    expect(sanitized.description).toBe('x');
  });
});
