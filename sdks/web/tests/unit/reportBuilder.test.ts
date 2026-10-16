import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  SDK_VERSION,
  buildReport,
  createReportId,
  init,
} from '@m-seismo/web';
import { resetConfig } from '@sdk/core/config';

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

afterEach(() => {
  resetConfig();
  vi.unstubAllGlobals();
});

describe('report builder', () => {
  it('builds a minimal manual report with error: null', () => {
    const report = buildReport({ description: 'manual report' });
    expect(report.schemaVersion).toBe(1);
    expect(report.sdkVersion).toBe(SDK_VERSION);
    expect(report.description).toBe('manual report');
    expect(report.error).toBeNull();
    expect(report.screenshot).toBeNull();
    expect(report.breadcrumbs).toEqual([]);
    expect(report.networkFailures).toEqual([]);
  });

  it('produces a UUID reportId and an ISO timestamp', () => {
    const report = buildReport({ description: 'x' });
    expect(report.reportId).toMatch(UUID_RE);
    expect(Number.isNaN(Date.parse(report.context.timestamp))).toBe(false);
    expect(new Date(report.context.timestamp).toISOString()).toBe(
      report.context.timestamp,
    );
  });

  it('keeps a captured error object', () => {
    const report = buildReport({
      description: 'boom',
      error: { type: 'TypeError', message: 'bad', stack: 'at x' },
    });
    expect(report.error).toEqual({ type: 'TypeError', message: 'bad', stack: 'at x' });
  });

  it('pulls application metadata from the configured SDK', () => {
    init({
      endpoint: 'https://example.test/api/reports',
      appVersion: '9.9.9',
      environment: 'staging',
    });
    const report = buildReport({ description: 'x' });
    expect(report.context.application).toEqual({
      version: '9.9.9',
      environment: 'staging',
    });
  });

  it('is SSR-safe: no location -> empty url, application {} by default', () => {
    const report = buildReport({ description: 'x' });
    expect(typeof globalThis.window).toBe('undefined');
    expect(report.context.url).toBe('');
    expect(report.context.application).toEqual({});
  });

  it('honours overrides for url, application, now and reportId', () => {
    const now = new Date('2026-01-02T03:04:05.000Z');
    const report = buildReport(
      { description: 'x' },
      {
        url: 'https://app.test/page',
        application: { version: '1', environment: 'test' },
        now,
        reportId: 'fixed-id',
      },
    );
    expect(report.context.url).toBe('https://app.test/page');
    expect(report.context.application).toEqual({ version: '1', environment: 'test' });
    expect(report.context.timestamp).toBe(now.toISOString());
    expect(report.reportId).toBe('fixed-id');
  });
});

describe('createReportId fallback', () => {
  it('returns a UUID using crypto.randomUUID when available', () => {
    expect(createReportId()).toMatch(UUID_RE);
  });

  it('falls back to getRandomValues when randomUUID is missing', () => {
    vi.stubGlobal('crypto', {
      getRandomValues: (arr: Uint8Array) => {
        for (let i = 0; i < arr.length; i += 1) arr[i] = (i * 7 + 3) % 256;
        return arr;
      },
    });
    const id = createReportId();
    expect(id).toMatch(UUID_RE);
  });

  it('falls back to Math.random when no crypto is present', () => {
    vi.stubGlobal('crypto', undefined);
    const id = createReportId();
    expect(id).toMatch(UUID_RE);
  });
});
