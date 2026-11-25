import { describe, expect, it } from 'vitest';
import {
  DEFAULT_SENSITIVE_QUERY_PARAMS,
  MAX_URL_LENGTH,
  isSensitiveKey,
  looksLikeSecretValue,
  sanitizeUrl,
} from '@sdk/context/sanitizeUrl';

describe('sanitizeUrl', () => {
  it('removes the fragment', () => {
    expect(sanitizeUrl('https://a.test/p?x=1#frag')).toBe('https://a.test/p?x=1');
    expect(sanitizeUrl('https://a.test/p#only-fragment')).toBe('https://a.test/p');
  });

  it('removes userinfo entirely', () => {
    expect(sanitizeUrl('https://user:pass@a.test/p?x=1')).toBe('https://a.test/p?x=1');
    expect(sanitizeUrl('https://token@a.test/')).toBe('https://a.test/');
  });

  it.each(DEFAULT_SENSITIVE_QUERY_PARAMS)(
    'redacts the default sensitive key "%s"',
    (key) => {
      const result = sanitizeUrl(`https://a.test/?${key}=supersecretvalue`);
      expect(result).toBe(`https://a.test/?${key}=[REDACTED]`);
    },
  );

  it('is case-insensitive for sensitive keys', () => {
    expect(sanitizeUrl('https://a.test/?TOKEN=abc')).toBe('https://a.test/?TOKEN=[REDACTED]');
    expect(sanitizeUrl('https://a.test/?Api_Key=abc')).toBe(
      'https://a.test/?Api_Key=[REDACTED]',
    );
  });

  it('redacts key-name-contains matches without absurd false positives', () => {
    expect(sanitizeUrl('https://a.test/?refresh_token=abc')).toBe(
      'https://a.test/?refresh_token=[REDACTED]',
    );
    expect(sanitizeUrl('https://a.test/?client_secret=abc')).toBe(
      'https://a.test/?client_secret=[REDACTED]',
    );
    expect(sanitizeUrl('https://a.test/?user_password=abc')).toBe(
      'https://a.test/?user_password=[REDACTED]',
    );
    // "monkey", "keywords", "hockey" must NOT be treated as sensitive.
    expect(sanitizeUrl('https://a.test/?monkey=1')).toBe('https://a.test/?monkey=1');
    expect(sanitizeUrl('https://a.test/?keywords=shoes')).toBe(
      'https://a.test/?keywords=shoes',
    );
    // but api_key / public_key should be.
    expect(sanitizeUrl('https://a.test/?public_key=abc')).toBe(
      'https://a.test/?public_key=[REDACTED]',
    );
  });

  it('redacts JWT-looking values regardless of key name', () => {
    const jwt =
      'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c';
    expect(sanitizeUrl(`https://a.test/?data=${jwt}`)).toBe(
      'https://a.test/?data=[REDACTED]',
    );
  });

  it('redacts long hex / long opaque values regardless of key name', () => {
    const hex = 'a'.repeat(40).replace('a', 'f'); // 40 hex chars
    expect(sanitizeUrl(`https://a.test/?h=${hex}`)).toBe('https://a.test/?h=[REDACTED]');
    const opaque = 'A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6';
    expect(sanitizeUrl(`https://a.test/?v=${opaque}`)).toBe(
      'https://a.test/?v=[REDACTED]',
    );
    // A long ordinary lowercase word is kept.
    const word = 'abcdefghijklmnopqrstuvwxyzabcdef';
    expect(sanitizeUrl(`https://a.test/?w=${word}`)).toBe(
      `https://a.test/?w=${word}`,
    );
  });

  it('preserves non-sensitive params and key order', () => {
    expect(
      sanitizeUrl('https://a.test/p?utm_source=docs&token=x&page=2'),
    ).toBe('https://a.test/p?utm_source=docs&token=[REDACTED]&page=2');
  });

  it('preserves repeated keys', () => {
    expect(sanitizeUrl('https://a.test/?a=1&token=x&a=2')).toBe(
      'https://a.test/?a=1&token=[REDACTED]&a=2',
    );
  });

  it('handles relative URLs and keeps them relative', () => {
    expect(sanitizeUrl('/page?token=x&x=1')).toBe('/page?token=[REDACTED]&x=1');
    expect(sanitizeUrl('page?token=x')).toBe('/page?token=[REDACTED]');
  });

  it('returns undefined for invalid input instead of throwing', () => {
    expect(sanitizeUrl('http://')).toBeUndefined();
    expect(sanitizeUrl('not a url')).toBeUndefined();
    expect(sanitizeUrl('')).toBeUndefined();
    expect(sanitizeUrl(undefined)).toBeUndefined();
    expect(sanitizeUrl(null)).toBeUndefined();
    expect(sanitizeUrl(42)).toBeUndefined();
    expect(sanitizeUrl({})).toBeUndefined();
  });

  it('omits forbidden schemes', () => {
    expect(sanitizeUrl('javascript:alert(1)')).toBeUndefined();
    expect(sanitizeUrl('data:text/html,<h1>x</h1>')).toBeUndefined();
    expect(sanitizeUrl('blob:https://a.test/uuid')).toBeUndefined();
  });

  it('truncates very long URLs, marking the cut', () => {
    const long = `https://a.test/p?q=${'x'.repeat(5000)}`;
    const result = sanitizeUrl(long);
    expect(result).toBeDefined();
    expect(result!.length).toBeLessThanOrEqual(MAX_URL_LENGTH);
    expect(result!.endsWith('...[truncated]')).toBe(true);
  });

  it('supports a custom sensitiveQueryParams list', () => {
    expect(
      sanitizeUrl('https://a.test/?workspace=secret&ok=1', {
        sensitiveQueryParams: ['workspace'],
      }),
    ).toBe('https://a.test/?workspace=[REDACTED]&ok=1');
  });

  it('never throws on garbage input', () => {
    const garbage = [
      '%%%%',
      'https://a.test/?=',
      'https://a.test/?a',
      'https://a.test/?#',
      'https://[::1]/x',
      '\u0000\u0001',
    ];
    for (const value of garbage) {
      expect(() => sanitizeUrl(value)).not.toThrow();
    }
  });
});

describe('isSensitiveKey / looksLikeSecretValue helpers', () => {
  it('detects whole-word "key" only', () => {
    expect(isSensitiveKey('key', undefined)).toBe(true);
    expect(isSensitiveKey('api_key', undefined)).toBe(true);
    expect(isSensitiveKey('monkey', undefined)).toBe(false);
    expect(isSensitiveKey('keywords', undefined)).toBe(false);
  });

  it('recognises secret-looking values', () => {
    expect(looksLikeSecretValue('a'.repeat(40))).toBe(true);
    expect(looksLikeSecretValue('short')).toBe(false);
  });
});
