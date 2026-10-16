import { describe, expect, it } from 'vitest';
import { buildReport, sanitize } from '@m-seismo/web';
import type { SanitizedReport } from '@m-seismo/web';

describe('sanitize (privacy boundary stub)', () => {
  it('is a pass-through in this phase', () => {
    const report = buildReport({ description: 'x' });
    const sanitized = sanitize(report);
    expect(sanitized).toBe(report);
  });

  it('returns the branded SanitizedReport type', () => {
    // Compile-time assertion: only sanitize() may produce a SanitizedReport.
    const sanitized: SanitizedReport = sanitize(buildReport({ description: 'x' }));
    expect(sanitized.description).toBe('x');
  });
});
