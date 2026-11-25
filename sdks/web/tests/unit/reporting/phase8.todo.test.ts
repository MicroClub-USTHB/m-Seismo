/**
 * Rate limiting: what exists today vs. what Phase 8 must add.
 *
 * Today the SDK only has *report-level* dedup/rate limiting (the
 * {@link ReportLimiter} used by the automatic/captured paths). Manual reports
 * are never suppressed. There is NO transport-level rate limiting, backoff or
 * retry queue — those are Phase 8 and are listed as `it.todo` so the gap is
 * visible in the test output.
 */
import { describe, expect, it } from 'vitest';
import { ReportLimiter } from '@sdk/reporting/limiter';

describe('rate limiting implemented today', () => {
  it('caps automatic sends per session and never suppresses manually', () => {
    const limiter = new ReportLimiter({ dedupWindowMs: 1_000, maxPerMinute: 100, maxPerSession: 2 });
    expect(limiter.decide('a', 0).action).toBe('send');
    expect(limiter.decide('b', 1).action).toBe('send');
    expect(limiter.decide('c', 2).action).toBe('rate_limited');
    // `occurrencesFor` (the manual path) reads a count and never blocks.
    expect(limiter.occurrencesFor('never-seen')).toBe(1);
  });
});

describe('NOT IMPLEMENTED (Phase 8)', () => {
  it.todo('rate-limits transport sends (e.g. max uploads per minute)');
  it.todo('retries a failed send with exponential backoff');
  it.todo('persists an offline queue and flushes it when connectivity returns');
});
