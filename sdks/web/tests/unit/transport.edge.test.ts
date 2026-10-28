/**
 * Transport edge cases not already covered by `transport.test.ts`:
 * 413 mapping, the missing-`fetch` environment, hostile success bodies, and the
 * "never logs the payload" guarantee.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildReport, sanitize, sendReport } from '@m-seismo/web';
import type { DiagnosticsConfig, ReportingConfig, SdkConfig } from '@m-seismo/web';
import { resetOriginalFetch } from '@sdk/network/originalFetch';

function diagnostics(): DiagnosticsConfig {
  return {
    maxEvents: 100,
    maxAgeMs: 600_000,
    captureUncaughtErrors: true,
    captureUnhandledRejections: true,
    captureNavigation: true,
    captureInteractions: true,
    captureNetwork: true,
    captureElementText: false,
    includeInReports: true,
  };
}

function reporting(): ReportingConfig {
  return {
    autoReport: 'off',
    sendCapturedExceptions: true,
    dedupWindowMs: 60_000,
    maxAutoReportsPerMinute: 5,
    maxAutoReportsPerSession: 50,
  };
}

function config(): SdkConfig {
  return {
    endpoint: 'https://receiver.test/api/reports',
    timeoutMs: 10_000,
    shortcut: false,
    launcher: { enabled: false },
    screenshot: true,
    diagnostics: diagnostics(),
    reporting: reporting(),
  };
}

function report() {
  return sanitize(
    buildReport(
      {
        type: 'manual',
        trigger: 'form',
        description: 'transport edge',
        context: { url: 'https://app.test/', timestamp: '2026-01-01T00:00:00.000Z', application: {} },
      },
      { reportId: 'edge-1' },
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetOriginalFetch();
});

describe('sendReport edge cases', () => {
  it('maps HTTP 413 (payload too large) to kind http with the status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('{}', { status: 413 }))),
    );
    const result = await sendReport(report(), config());
    expect(result).toMatchObject({ ok: false, kind: 'http', status: 413 });
  });

  it('returns a network failure when fetch is not available, without throwing', async () => {
    vi.stubGlobal('fetch', undefined);
    resetOriginalFetch();
    const result = await sendReport(report(), config());
    expect(result).toMatchObject({ ok: false, kind: 'network' });
  });

  it('returns invalid-response when a 2xx body cannot be parsed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: () => Promise.reject(new Error('not json')),
        } as unknown as Response),
      ),
    );
    const result = await sendReport(report(), config());
    expect(result).toMatchObject({ ok: false, kind: 'invalid-response', status: 201 });
  });

  it('never throws even when fetch itself throws synchronously', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => {
        throw new Error('sync boom');
      }),
    );
    await expect(sendReport(report(), config())).resolves.toMatchObject({
      ok: false,
      kind: 'network',
    });
  });

  it('never logs the report payload', async () => {
    const spies = [
      vi.spyOn(console, 'log').mockImplementation(() => {}),
      vi.spyOn(console, 'warn').mockImplementation(() => {}),
      vi.spyOn(console, 'error').mockImplementation(() => {}),
      vi.spyOn(console, 'info').mockImplementation(() => {}),
      vi.spyOn(console, 'debug').mockImplementation(() => {}),
    ];
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(JSON.stringify({ id: 'x' }), { status: 201 }))),
    );
    await sendReport(report(), config());

    for (const spy of spies) {
      for (const call of spy.mock.calls) {
        expect(JSON.stringify(call)).not.toContain('transport edge');
        expect(JSON.stringify(call)).not.toContain('edge-1');
      }
    }
  });
});
