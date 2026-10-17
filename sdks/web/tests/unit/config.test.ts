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
      shortcut: { key: 'enter', alt: false, ctrl: true, meta: false, shift: false },
      launcher: { enabled: false },
      screenshot: true,
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
      shortcut: { key: 'enter', alt: false, ctrl: true, meta: false, shift: false },
      launcher: { enabled: false },
      screenshot: true,
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

  it('defaults the shortcut to Ctrl+Enter', () => {
    configure({ endpoint: 'https://a.test/reports' });
    expect(getConfig()?.shortcut).toEqual({
      key: 'enter',
      alt: false,
      ctrl: true,
      meta: false,
      shift: false,
    });
  });

  it('accepts a custom shortcut string', () => {
    configure({ endpoint: 'https://a.test/reports', shortcut: 'Alt+Shift+K' });
    expect(getConfig()?.shortcut).toEqual({
      key: 'k',
      alt: true,
      ctrl: false,
      meta: false,
      shift: true,
    });
  });

  it('shortcut: false disables the shortcut without warning', () => {
    configure({ endpoint: 'https://a.test/reports', shortcut: false });
    expect(getConfig()?.shortcut).toBe(false);
    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('an invalid shortcut warns once and disables the shortcut', () => {
    expect(
      configure({ endpoint: 'https://a.test/reports', shortcut: 'Ctrl+Shift' }),
    ).toBe(true);
    expect(getConfig()?.shortcut).toBe(false);
    expect(warnSpy).toHaveBeenCalledTimes(1);
  });

  it('launcher: true uses the default label and position', () => {
    configure({ endpoint: 'https://a.test/reports', launcher: true });
    expect(getConfig()?.launcher).toEqual({
      enabled: true,
      label: 'Report a bug',
      position: 'bottom-right',
    });
  });

  it('launcher options customise label and position', () => {
    configure({
      endpoint: 'https://a.test/reports',
      launcher: { label: 'Found a bug?', position: 'top-left' },
    });
    expect(getConfig()?.launcher).toEqual({
      enabled: true,
      label: 'Found a bug?',
      position: 'top-left',
    });
  });

  it('screenshot defaults to true and can be disabled', () => {
    configure({ endpoint: 'https://a.test/reports' });
    expect(getConfig()?.screenshot).toBe(true);
    configure({ endpoint: 'https://a.test/reports', screenshot: false });
    expect(getConfig()?.screenshot).toBe(false);
  });

  it('stores an onEvent listener only when it is a function', () => {
    const onEvent = (): void => {};
    configure({ endpoint: 'https://a.test/reports', onEvent });
    expect(getConfig()?.onEvent).toBe(onEvent);
  });

  it('omits sensitiveQueryParams by default', () => {
    configure({ endpoint: 'https://a.test/reports' });
    expect(getConfig()?.sensitiveQueryParams).toBeUndefined();
  });

  it('stores a valid privacy.sensitiveQueryParams list (trimmed)', () => {
    configure({
      endpoint: 'https://a.test/reports',
      privacy: { sensitiveQueryParams: [' workspace ', 'tenant'] },
    });
    expect(getConfig()?.sensitiveQueryParams).toEqual(['workspace', 'tenant']);
    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('ignores non-string entries with one warning', () => {
    configure({
      endpoint: 'https://a.test/reports',
      privacy: { sensitiveQueryParams: ['ok', 5 as unknown as string, ''] },
    });
    expect(getConfig()?.sensitiveQueryParams).toEqual(['ok']);
    expect(warnSpy).toHaveBeenCalledTimes(1);
  });

  it('ignores a non-array sensitiveQueryParams with one warning', () => {
    configure({
      endpoint: 'https://a.test/reports',
      privacy: { sensitiveQueryParams: 'nope' as unknown as string[] },
    });
    expect(getConfig()?.sensitiveQueryParams).toBeUndefined();
    expect(warnSpy).toHaveBeenCalledTimes(1);
  });
});
