import { describe, expect, it } from 'vitest';
import {
  computeFingerprint,
  normalizeMessage,
  topFrame,
  FLAG_WEAK_FINGERPRINT,
} from '@sdk/reporting/fingerprint';

const HEX16 = /^[0-9a-f]{16}$/;

function fp(overrides: Partial<Parameters<typeof computeFingerprint>[0]> = {}) {
  return computeFingerprint({
    type: 'TypeError',
    message: 'boom',
    crossOrigin: false,
    pagePath: '/checkout',
    ...overrides,
  });
}

describe('normalizeMessage', () => {
  it('collapses whitespace', () => {
    expect(normalizeMessage('  a   b\n\tc  ')).toBe('a b c');
  });

  it('replaces volatile tokens', () => {
    expect(normalizeMessage('id 550e8400-e29b-41d4-a716-446655440000')).toContain('<uuid>');
    expect(normalizeMessage('at 1234567 ms')).toContain('<number>');
    expect(normalizeMessage('hash deadbeefdeadbeef')).toContain('<hex>');
    expect(normalizeMessage('at 2026-10-19T12:00:00.000Z')).toContain('<timestamp>');
  });

  it('scrubs secrets so they do not appear in the normalized text', () => {
    const out = normalizeMessage('Bearer fake-secret-token-value');
    expect(out).not.toContain('fake-secret-token-value');
    expect(out).toContain('[REDACTED]');
  });
});

describe('topFrame', () => {
  it('extracts function + path from V8 frames', () => {
    const stack =
      'TypeError: boom\n    at save (/app/checkout.js:10:5)\n    at onClick (/app/main.js:1:1)';
    expect(topFrame(stack)?.frame).toBe('save@/app/checkout.js');
  });

  it('extracts from bracketed variants', () => {
    const stack = 'Error\n    at Object.<anonymous> (/app/index.js:3:9)';
    expect(topFrame(stack)?.frame).toBe('Object.<anonymous>@/app/index.js');
  });

  it('extracts from Firefox-style frames', () => {
    const stack = 'save@/app/checkout.js:10:5\nonClick@/app/main.js:1:1';
    expect(topFrame(stack)?.frame).toBe('save@/app/checkout.js');
  });

  it('strips line/column, query string and fragment', () => {
    const stack = 'Error\n    at save (/app/a.js?v=2#x:10:5)';
    expect(topFrame(stack)?.frame).toBe('save@/app/a.js');
  });
});

describe('computeFingerprint', () => {
  it('is deterministic for the same inputs', () => {
    expect(fp().fingerprint).toBe(fp().fingerprint);
    expect(fp().fingerprint).toMatch(HEX16);
  });

  it('changes with the type', () => {
    expect(fp({ type: 'TypeError' }).fingerprint).not.toBe(fp({ type: 'RangeError' }).fingerprint);
  });

  it('changes with the message', () => {
    expect(fp({ message: 'a' }).fingerprint).not.toBe(fp({ message: 'b' }).fingerprint);
  });

  it('is stable across volatile message tokens', () => {
    const a = fp({ message: 'timeout after 1234 ms for 550e8400-e29b-41d4-a716-446655440000' });
    const b = fp({ message: 'timeout after 9876 ms for 11111111-2222-3333-4444-555555555555' });
    expect(a.fingerprint).toBe(b.fingerprint);
  });

  it('does NOT merge identical messages from different stack frames', () => {
    const first = fp({ stack: 'Error: x\n    at first (/a.js:1:1)' });
    const second = fp({ stack: 'Error: x\n    at second (/b.js:2:2)' });
    expect(first.fingerprint).not.toBe(second.fingerprint);
  });

  it('flags a weak fingerprint when no stack is available', () => {
    expect(fp({ stack: '' }).weak).toBe(true);
    expect(fp({ stack: '' }).fingerprint).toMatch(HEX16);
    expect(FLAG_WEAK_FINGERPRINT).toBe('weak-fingerprint');
  });

  it('does not merge the same bare cross-origin message across page paths', () => {
    const a = fp({ message: 'Script error.', crossOrigin: true, pagePath: '/a' });
    const b = fp({ message: 'Script error.', crossOrigin: true, pagePath: '/b' });
    expect(a.weak).toBe(true);
    expect(a.fingerprint).not.toBe(b.fingerprint);
  });

  it('a secret in the message cannot influence the fingerprint text', () => {
    const result = fp({ message: 'token=supersecretvalue123' });
    expect(result.normalizedMessage).not.toContain('supersecretvalue123');
    expect(result.fingerprint).toMatch(HEX16);
  });
});
