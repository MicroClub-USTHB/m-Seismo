import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { configure, getConfig, isEnabled, resetConfig } from '@sdk/core/config';

let warnSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  resetConfig();
  warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  resetConfig();
  warnSpy.mockRestore();
});

describe('configure / init validation', () => {
  it('rejects a missing endpoint: one warning, no throw, disabled', () => {
    const result = configure({ endpoint: '' });
    expect(result).toBe(false);
    expect(isEnabled()).toBe(false);
    expect(getConfig()).toBeNull();
    expect(warnSpy).toHaveBeenCalledTimes(1);
  });

  it('rejects a non-http(s) scheme', () => {
    expect(configure({ endpoint: 'ftp://example.test/ingest' })).toBe(false);
    expect(isEnabled()).toBe(false);
    expect(warnSpy).toHaveBeenCalledTimes(1);
  });

  it('rejects a non-URL string', () => {
    expect(configure({ endpoint: 'not-a-url' })).toBe(false);
    expect(isEnabled()).toBe(false);
  });

  it('accepts an absolute https endpoint', () => {
    expect(configure({ endpoint: 'https://example.test/api/reports' })).toBe(true);
    expect(isEnabled()).toBe(true);
    expect(getConfig()).toEqual({
      endpoint: 'https://example.test/api/reports',
      timeoutMs: 10_000,
    });
  });

  it('accepts http (for local dev) and stores optional fields', () => {
    expect(
      configure({
        endpoint: 'http://localhost:4010/api/reports',
        appVersion: '1.2.3',
        environment: 'test',
        projectKey: 'pk_123',
        timeoutMs: 2500,
      }),
    ).toBe(true);
    expect(getConfig()).toEqual({
      endpoint: 'http://localhost:4010/api/reports',
      appVersion: '1.2.3',
      environment: 'test',
      projectKey: 'pk_123',
      timeoutMs: 2500,
    });
  });

  it('a later valid init replaces the previous config (no duplicate state)', () => {
    configure({ endpoint: 'https://a.test/reports' });
    configure({ endpoint: 'https://b.test/reports', environment: 'prod' });
    const config = getConfig();
    expect(config?.endpoint).toBe('https://b.test/reports');
    expect(config?.environment).toBe('prod');
  });

  it('a later invalid init disables the SDK', () => {
    configure({ endpoint: 'https://a.test/reports' });
    expect(isEnabled()).toBe(true);
    expect(configure({ endpoint: 'nope' })).toBe(false);
    expect(isEnabled()).toBe(false);
  });

  it('falls back to the default timeout for invalid timeoutMs', () => {
    configure({ endpoint: 'https://a.test/reports', timeoutMs: -5 });
    expect(getConfig()?.timeoutMs).toBe(10_000);
    configure({ endpoint: 'https://a.test/reports', timeoutMs: Number.NaN });
    expect(getConfig()?.timeoutMs).toBe(10_000);
  });
});
