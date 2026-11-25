import { afterEach, describe, expect, it, vi } from 'vitest';
import { getContext } from '@sdk/context/getContext';
import type { ContextEnvironment } from '@sdk/context/env';

const NOW = new Date('2026-01-02T03:04:05.000Z');

const CHROME_WIN =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('getContext — full environment', () => {
  it('collects every field', () => {
    const { context } = getContext({
      reportId: 'rid-1',
      appVersion: '1.2.3',
      environment: 'prod',
      env: {
        navigator: { userAgent: CHROME_WIN },
        location: { href: 'https://app.test/page?utm_source=docs&token=abc#top' },
        window: { innerWidth: 1280, innerHeight: 720 },
        now: NOW,
      },
    });

    expect(context).toEqual({
      url: 'https://app.test/page?utm_source=docs&token=[REDACTED]',
      timestamp: NOW.toISOString(),
      browser: { name: 'Chrome', version: '120' },
      platform: 'Windows',
      viewport: { width: 1280, height: 720 },
      application: { version: '1.2.3', environment: 'prod' },
    });
  });
});

describe('getContext — browser and platform detection table', () => {
  const cases: ReadonlyArray<{
    readonly label: string;
    readonly ua: string;
    readonly browser?: { readonly name?: string; readonly version?: string };
    readonly platform?: string;
  }> = [
    {
      label: 'Chrome on Windows',
      ua: CHROME_WIN,
      browser: { name: 'Chrome', version: '120' },
      platform: 'Windows',
    },
    {
      label: 'Edge on Windows',
      ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91',
      browser: { name: 'Edge', version: '120' },
      platform: 'Windows',
    },
    {
      label: 'Firefox on macOS',
      ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:121.0) Gecko/20100101 Firefox/121.0',
      browser: { name: 'Firefox', version: '121' },
      platform: 'macOS',
    },
    {
      label: 'Safari on macOS',
      ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Safari/605.1.15',
      browser: { name: 'Safari', version: '17' },
      platform: 'macOS',
    },
    {
      label: 'Safari on iOS',
      ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1',
      browser: { name: 'Safari', version: '17' },
      platform: 'iOS',
    },
    {
      label: 'Chrome on iOS (CriOS)',
      ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0.6099.119 Mobile/15E148 Safari/604.1',
      browser: { name: 'Chrome', version: '120' },
      platform: 'iOS',
    },
    {
      label: 'Firefox on iOS (FxiOS)',
      ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/121.0 Mobile/15E148 Safari/605.1.15',
      browser: { name: 'Firefox', version: '121' },
      platform: 'iOS',
    },
    {
      label: 'Opera on Windows',
      ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36 OPR/105.0.0.0',
      browser: { name: 'Opera', version: '105' },
      platform: 'Windows',
    },
    {
      label: 'Samsung Internet on Android',
      ua: 'Mozilla/5.0 (Linux; Android 13; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36',
      browser: { name: 'Samsung Internet', version: '23' },
      platform: 'Android',
    },
    {
      label: 'Chrome on Android',
      ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36',
      browser: { name: 'Chrome', version: '120' },
      platform: 'Android',
    },
    {
      label: 'Firefox on Android',
      ua: 'Mozilla/5.0 (Android 13; Mobile; rv:121.0) Gecko/121.0 Firefox/121.0',
      browser: { name: 'Firefox', version: '121' },
      platform: 'Android',
    },
    {
      label: 'Chrome on ChromeOS',
      ua: 'Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
      browser: { name: 'Chrome', version: '120' },
      platform: 'ChromeOS',
    },
    {
      label: 'unknown UA (bot)',
      ua: 'Mozilla/5.0 (compatible; SomeBot/1.0)',
    },
    {
      label: 'empty UA',
      ua: '',
    },
  ];

  it.each(cases)('$label', ({ ua, browser, platform }) => {
    const { context } = getContext({ env: { navigator: { userAgent: ua } } });
    if (browser === undefined) {
      expect(context.browser).toBeUndefined();
    } else {
      expect(context.browser).toEqual(browser);
    }
    expect(context.platform).toBe(platform);
  });
});

describe('getContext — userAgentData (client hints) takes priority', () => {
  it('prefers brands over the UA and ignores GREASE entries', () => {
    const { context } = getContext({
      env: {
        navigator: {
          userAgent: CHROME_WIN,
          userAgentData: {
            brands: [
              { brand: 'Not A Brand', version: '99' },
              { brand: 'Chromium', version: '120' },
              { brand: 'Google Chrome', version: '120.0.6099.109' },
            ],
            platform: 'Windows',
          },
        },
      },
    });
    expect(context.browser).toEqual({ name: 'Chrome', version: '120' });
    expect(context.platform).toBe('Windows');
  });

  it('detects Edge from brands even when the UA says Chrome', () => {
    const { context } = getContext({
      env: {
        navigator: {
          userAgent: CHROME_WIN,
          userAgentData: {
            brands: [
              { brand: 'Not_A Brand', version: '8' },
              { brand: 'Chromium', version: '120' },
              { brand: 'Microsoft Edge', version: '120.0.2210.91' },
            ],
          },
        },
      },
    });
    expect(context.browser).toEqual({ name: 'Edge', version: '120' });
  });

  it('falls back to the UA when brands are only GREASE', () => {
    const { context } = getContext({
      env: {
        navigator: {
          userAgent:
            'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:121.0) Gecko/20100101 Firefox/121.0',
          userAgentData: { brands: [{ brand: 'Not A Brand', version: '99' }] },
        },
      },
    });
    expect(context.browser).toEqual({ name: 'Firefox', version: '121' });
  });

  it('prefers userAgentData.platform over the UA platform', () => {
    const { context } = getContext({
      env: {
        navigator: { userAgent: CHROME_WIN, userAgentData: { platform: 'macOS' } },
      },
    });
    expect(context.platform).toBe('macOS');
  });

  it('falls back to navigator.platform when the UA is silent', () => {
    const { context } = getContext({
      env: { navigator: { userAgent: 'Mozilla/5.0', platform: 'MacIntel' } },
    });
    expect(context.platform).toBe('macOS');
  });
});

describe('getContext — missing fields are omitted, never faked', () => {
  it('collects only timestamp and application with an empty environment', () => {
    const { context } = getContext({ env: {}, now: NOW });
    expect(context).toEqual({ timestamp: NOW.toISOString(), application: {} });
    expect(context.url).toBeUndefined();
    expect(context.browser).toBeUndefined();
    expect(context.platform).toBeUndefined();
    expect(context.viewport).toBeUndefined();
  });

  it('omits browser/platform when there is no navigator', () => {
    const { context } = getContext({ env: { location: { href: 'https://a.test/' } } });
    expect(context.url).toBe('https://a.test/');
    expect(context.browser).toBeUndefined();
    expect(context.platform).toBeUndefined();
  });

  it('omits url when there is no location', () => {
    const { context } = getContext({ env: { navigator: { userAgent: CHROME_WIN } } });
    expect(context.url).toBeUndefined();
  });

  it('omits viewport when dimensions are missing or non-positive', () => {
    expect(getContext({ env: {} }).context.viewport).toBeUndefined();
    expect(
      getContext({ env: { window: { innerWidth: 0, innerHeight: 600 } } }).context.viewport,
    ).toBeUndefined();
    expect(
      getContext({ env: { window: { innerWidth: 800 } } }).context.viewport,
    ).toBeUndefined();
    expect(
      getContext({ env: { window: { innerWidth: Number.NaN, innerHeight: 600 } } }).context
        .viewport,
    ).toBeUndefined();
  });

  it('uses the url override when provided', () => {
    const { context } = getContext({
      url: 'https://override.test/?secret=abc',
      env: { location: { href: 'https://ignored.test/' } },
    });
    expect(context.url).toBe('https://override.test/?secret=[REDACTED]');
  });
});

describe('getContext — application metadata comes from input only', () => {
  it('includes version and environment when provided', () => {
    const { context } = getContext({ appVersion: '9.9.9', environment: 'staging' });
    expect(context.application).toEqual({ version: '9.9.9', environment: 'staging' });
  });

  it('omits both when not provided', () => {
    expect(getContext({}).context.application).toEqual({});
  });

  it('omits empty strings', () => {
    expect(getContext({ appVersion: '', environment: '' }).context.application).toEqual({});
  });
});

describe('getContext — resilience and safety', () => {
  it('a throwing collector omits that field and leaves the rest intact', () => {
    const throwingWindow = {
      get innerWidth(): number {
        throw new Error('boom');
      },
      innerHeight: 600,
    };
    const { context } = getContext({
      appVersion: '1.0.0',
      env: {
        navigator: { userAgent: CHROME_WIN },
        location: { href: 'https://a.test/' },
        window: throwingWindow,
        now: NOW,
      },
    });
    expect(context.viewport).toBeUndefined();
    // Everything else survives.
    expect(context.url).toBe('https://a.test/');
    expect(context.browser).toEqual({ name: 'Chrome', version: '120' });
    expect(context.platform).toBe('Windows');
    expect(context.timestamp).toBe(NOW.toISOString());
    expect(context.application).toEqual({ version: '1.0.0' });
  });

  it('is read-only: never writes to the injected environment or host state', () => {
    let writes = 0;
    const guard = <T extends object>(target: T): T =>
      new Proxy(target, {
        set: () => {
          writes += 1;
          return true;
        },
        defineProperty: () => {
          writes += 1;
          return true;
        },
        deleteProperty: () => {
          writes += 1;
          return true;
        },
      });

    const env: ContextEnvironment = {
      navigator: guard({ userAgent: CHROME_WIN }),
      location: guard({ href: 'https://a.test/?token=abc' }),
      window: guard({ innerWidth: 800, innerHeight: 600 }),
      now: NOW,
    };

    // Any access to storage or document would throw and fail the test.
    const hostile = new Proxy(
      {},
      {
        get() {
          throw new Error('storage/document must not be touched');
        },
        set() {
          throw new Error('storage/document must not be written');
        },
      },
    );
    vi.stubGlobal('localStorage', hostile);
    vi.stubGlobal('sessionStorage', hostile);
    vi.stubGlobal('document', hostile);

    const { context } = getContext({ env });
    expect(context.browser).toEqual({ name: 'Chrome', version: '120' });
    expect(writes).toBe(0);
  });

  it('is SSR-safe: with no window/navigator it returns only the minimal subset', () => {
    vi.stubGlobal('window', undefined);
    vi.stubGlobal('navigator', undefined);
    vi.stubGlobal('location', undefined);

    const { context } = getContext({ now: NOW });
    expect(context).toEqual({ timestamp: NOW.toISOString(), application: {} });
  });
});

describe('getContext — report id', () => {
  it('echoes the provided report id and never generates one', () => {
    expect(getContext({ reportId: 'rid-42' }).reportId).toBe('rid-42');
    expect(getContext({}).reportId).toBeUndefined();
  });

  it('returns the same id when called again (reused, not regenerated)', () => {
    const first = getContext({ reportId: 'stable' }).reportId;
    const second = getContext({ reportId: 'stable' }).reportId;
    expect(first).toBe('stable');
    expect(second).toBe(first);
  });
});
