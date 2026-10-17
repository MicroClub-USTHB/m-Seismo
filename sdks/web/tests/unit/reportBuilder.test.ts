import { afterEach, describe, expect, it, vi } from 'vitest';
import { SDK_VERSION, buildReport, createReportId } from '@m-seismo/web';
import type { ReportContext } from '@m-seismo/web';
import { resetConfig } from '@sdk/core/config';

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function makeContext(overrides: Partial<ReportContext> = {}): ReportContext {
  return {
    timestamp: new Date('2026-01-02T03:04:05.000Z').toISOString(),
    application: {},
    ...overrides,
  };
}

afterEach(() => {
  resetConfig();
  vi.unstubAllGlobals();
});

describe('report builder (assembly only)', () => {
  it('builds a minimal manual report with error: null', () => {
    const report = buildReport({ description: 'manual report', context: makeContext() });
    expect(report.schemaVersion).toBe(1);
    expect(report.sdkVersion).toBe(SDK_VERSION);
    expect(report.description).toBe('manual report');
    expect(report.error).toBeNull();
    expect(report.screenshot).toBeNull();
    expect(report.breadcrumbs).toEqual([]);
    expect(report.networkFailures).toEqual([]);
  });

  it('produces a UUID reportId and carries the provided context verbatim', () => {
    const context = makeContext({
      url: 'https://app.test/page',
      browser: { name: 'Chrome', version: '120' },
      platform: 'Linux',
      viewport: { width: 800, height: 600 },
      application: { version: '1.2.3', environment: 'prod' },
    });
    const report = buildReport({ description: 'x', context });
    expect(report.reportId).toMatch(UUID_RE);
    expect(report.context).toBe(context);
    expect(Number.isNaN(Date.parse(report.context.timestamp))).toBe(false);
  });

  it('keeps a captured error object', () => {
    const report = buildReport({
      description: 'boom',
      context: makeContext(),
      error: { type: 'TypeError', message: 'bad', stack: 'at x' },
    });
    expect(report.error).toEqual({ type: 'TypeError', message: 'bad', stack: 'at x' });
  });

  it('honours the reportId override (idempotency)', () => {
    const report = buildReport(
      { description: 'x', context: makeContext() },
      { reportId: 'fixed-id' },
    );
    expect(report.reportId).toBe('fixed-id');
  });

  it('is SSR-safe: importing and building touches no globals', () => {
    const report = buildReport({ description: 'x', context: makeContext() });
    expect(typeof globalThis.window).toBe('undefined');
    expect(report.context.application).toEqual({});
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
