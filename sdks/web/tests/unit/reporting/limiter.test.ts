import { describe, expect, it } from 'vitest';
import { ReportLimiter, MAX_TRACKED_FINGERPRINTS } from '@sdk/reporting/limiter';

const BASE = 1_700_000_000_000;

function makeLimiter(overrides: Partial<ConstructorParameters<typeof ReportLimiter>[0]> = {}) {
  return new ReportLimiter({
    dedupWindowMs: 60_000,
    maxPerMinute: 5,
    maxPerSession: 50,
    ...overrides,
  });
}

describe('ReportLimiter', () => {
  it('sends the first occurrence and suppresses repeats inside the window', () => {
    const limiter = makeLimiter();
    expect(limiter.decide('fp-a', BASE)).toEqual({ action: 'send', occurrences: 1 });
    expect(limiter.decide('fp-a', BASE + 1)).toEqual({ action: 'duplicate', occurrences: 2 });
    expect(limiter.decide('fp-a', BASE + 2)).toEqual({ action: 'duplicate', occurrences: 3 });
    expect(limiter.stats().suppressed.duplicate).toBe(2);
  });

  it('reports distinct fingerprints independently', () => {
    const limiter = makeLimiter();
    expect(limiter.decide('fp-a', BASE).action).toBe('send');
    expect(limiter.decide('fp-b', BASE).action).toBe('send');
    expect(limiter.decide('fp-b', BASE + 1).action).toBe('duplicate');
  });

  it('sends again after the window closes, with the accumulated count', () => {
    const limiter = makeLimiter({ dedupWindowMs: 1_000 });
    limiter.decide('fp-a', BASE);
    limiter.decide('fp-a', BASE + 500);
    limiter.decide('fp-a', BASE + 900);
    expect(limiter.decide('fp-a', BASE + 1_000)).toEqual({ action: 'send', occurrences: 4 });
  });

  it('bounds sends per minute and resumes in the next minute', () => {
    const limiter = makeLimiter({ maxPerMinute: 2 });
    expect(limiter.decide('a', BASE).action).toBe('send');
    expect(limiter.decide('b', BASE).action).toBe('send');
    expect(limiter.decide('c', BASE).action).toBe('rate_limited');
    expect(limiter.stats().suppressed.rate_limited).toBe(1);
    expect(limiter.decide('d', BASE + 60_000).action).toBe('send');
  });

  it('bounds sends per session', () => {
    const limiter = makeLimiter({ maxPerSession: 1, maxPerMinute: 100 });
    expect(limiter.decide('a', BASE).action).toBe('send');
    expect(limiter.decide('b', BASE + 120_000).action).toBe('rate_limited');
  });

  it('keeps the fingerprint table bounded, evicting the oldest', () => {
    const limiter = makeLimiter({ maxFingerprints: 3, maxPerMinute: 1_000 });
    for (let i = 0; i < 10; i += 1) limiter.decide(`fp-${i}`, BASE + i);
    expect(limiter.stats().fingerprints).toBe(3);
    expect(MAX_TRACKED_FINGERPRINTS).toBe(100);
  });

  it('rolls the minute window lazily without timers', () => {
    const limiter = makeLimiter({ maxPerMinute: 1 });
    limiter.decide('a', BASE);
    limiter.decide('b', BASE + 30_000);
    expect(limiter.decide('c', BASE + 60_000).action).toBe('send');
    expect(limiter.stats().minuteSends).toBe(1);
  });

  it('occurrencesFor reads the current count without mutating', () => {
    const limiter = makeLimiter();
    expect(limiter.occurrencesFor('fp-a')).toBe(1);
    limiter.decide('fp-a', BASE);
    limiter.decide('fp-a', BASE + 1);
    expect(limiter.occurrencesFor('fp-a')).toBe(2);
    expect(limiter.stats().suppressed.duplicate).toBe(1);
  });

  it('clear() resets every counter', () => {
    const limiter = makeLimiter();
    limiter.decide('fp-a', BASE);
    limiter.decide('fp-a', BASE + 1);
    limiter.clear();
    expect(limiter.stats()).toEqual({
      fingerprints: 0,
      sessionSends: 0,
      minuteSends: 0,
      suppressed: { duplicate: 0, rate_limited: 0 },
    });
  });

  it('configure() changes limits but keeps history', () => {
    const limiter = makeLimiter();
    limiter.decide('fp-a', BASE);
    limiter.configure({ dedupWindowMs: 1, maxPerMinute: 5, maxPerSession: 50 });
    expect(limiter.decide('fp-a', BASE + 10)).toEqual({ action: 'send', occurrences: 2 });
  });
});
