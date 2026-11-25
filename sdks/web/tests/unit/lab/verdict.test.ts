import { describe, expect, it } from 'vitest';
import { accepts, fail, gapVerdict, info, pass, safeVerify } from '@app/lab/verdict';

describe('lab verdict helpers', () => {
  it('builds the three verdict shapes', () => {
    expect(pass('ok')).toEqual({ status: 'pass', observed: 'ok' });
    expect(info('note', { a: 1 })).toEqual({ status: 'info', observed: 'note', evidence: { a: 1 } });
    expect(fail('bad').status).toBe('fail');
  });

  it('accepts only the documented status for strict categories', () => {
    for (const category of ['DETECTED_AUTO', 'MANUAL_ONLY', 'NOT_SUPPORTED'] as const) {
      expect(accepts(category, 'pass')).toBe(true);
      expect(accepts(category, 'info')).toBe(false);
      expect(accepts(category, 'fail')).toBe(false);
    }
  });

  it('accepts pass or info for LIMITED and KNOWN_GAP', () => {
    for (const category of ['LIMITED', 'KNOWN_GAP'] as const) {
      expect(accepts(category, 'pass')).toBe(true);
      expect(accepts(category, 'info')).toBe(true);
      expect(accepts(category, 'fail')).toBe(false);
    }
  });

  it('never softens a detected problem to a pass', () => {
    expect(gapVerdict(true, 'leak').status).toBe('info');
    expect(gapVerdict(false, 'clean').status).toBe('pass');
  });

  it('turns a thrown verifier into a fail', async () => {
    const verdict = await safeVerify(() => {
      throw new Error('boom');
    });
    expect(verdict.status).toBe('fail');
    expect(verdict.observed).toContain('boom');
  });

  it('turns an undefined verdict into a fail', async () => {
    const verdict = await safeVerify(() => undefined as never);
    expect(verdict.status).toBe('fail');
  });
});
