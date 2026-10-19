import { describe, expect, it, vi } from 'vitest';
import { MAX_REPORT_BYTES, processReport } from '@sdk/reporting/processor';
import type { ProcessReportInput, ReportProcessorDeps } from '@sdk/reporting/processor';
import { ReportLimiter } from '@sdk/reporting/limiter';
import type { BeforeSendHook, ReportingConfig, SdkConfig } from '@sdk/core/config';
import type { DiagnosticsSelection } from '@sdk/diagnostics/select';
import type { ErrorDiagnosticEvent } from '@sdk/diagnostics/types';
import type { ReportContext } from '@sdk/core/types';
import type { SanitizedReport } from '@sdk/privacy/sanitize';
import type { SendResult } from '@sdk/transport/types';

const ISO = '2026-10-19T12:00:00.000Z';
const NOW = Date.parse(ISO);

function makeConfig(opts: { beforeSend?: BeforeSendHook } = {}): SdkConfig {
  const reporting: ReportingConfig = {
    autoReport: 'off',
    sendCapturedExceptions: true,
    dedupWindowMs: 60_000,
    maxAutoReportsPerMinute: 5,
    maxAutoReportsPerSession: 50,
    ...(opts.beforeSend !== undefined ? { beforeSend: opts.beforeSend } : {}),
  };
  return {
    endpoint: 'https://receiver.test/api/reports',
    timeoutMs: 10_000,
    shortcut: false,
    launcher: { enabled: false },
    screenshot: true,
    diagnostics: {
      maxEvents: 100,
      maxAgeMs: 600_000,
      captureUncaughtErrors: true,
      captureUnhandledRejections: true,
      captureNavigation: true,
      captureInteractions: true,
      captureNetwork: true,
      captureElementText: false,
      includeInReports: true,
    },
    reporting,
  };
}

function context(): ReportContext {
  return { url: 'https://app.test/checkout', timestamp: ISO, application: {} };
}

const EMPTY: DiagnosticsSelection = { breadcrumbs: [], networkFailures: [], error: null };

function makeEvent(overrides: Partial<ErrorDiagnosticEvent> = {}): ErrorDiagnosticEvent {
  return {
    kind: 'error',
    id: 'e-1',
    timestamp: ISO,
    handled: false,
    error: {
      type: 'TypeError',
      message: 'boom',
      stack: 'TypeError: boom\n    at save (/app/checkout.js:10:5)',
    },
    ...overrides,
  };
}

function makeDeps(overrides: Partial<ReportProcessorDeps> = {}) {
  const sent: SanitizedReport[] = [];
  const send = vi.fn(async (report: SanitizedReport): Promise<SendResult> => {
    sent.push(report);
    return { ok: true, status: 201, id: 'srv-1' };
  });
  const deps: ReportProcessorDeps = {
    now: () => NOW,
    createId: () => 'r-1',
    getConfig: () => makeConfig(),
    collectContext: () => context(),
    selectDiagnostics: () => EMPTY,
    send,
    limiter: new ReportLimiter({ dedupWindowMs: 60_000, maxPerMinute: 5, maxPerSession: 50 }),
    warn: vi.fn(),
    ...overrides,
  };
  return { deps, sent, send };
}

const SELECT_ERROR: DiagnosticsSelection = {
  breadcrumbs: [],
  networkFailures: [],
  error: {
    type: 'Error',
    message: 'recent failure',
    stack: 'Error: recent failure\n    at submit (/app/form.js:3:1)',
    handled: false,
    timestamp: ISO,
  },
};

describe('processReport — classification', () => {
  it('classifies a plain form submission as manual', async () => {
    const { deps, sent } = makeDeps();
    const result = await processReport({ source: 'form', description: '  broken  ' }, deps);
    expect(result).toMatchObject({ status: 'sent', type: 'manual', trigger: 'form' });
    expect(sent[0]!.description).toBe('broken');
    expect(sent[0]!.error).toBeNull();
    expect(sent[0]!.type).toBe('manual');
    expect(sent[0]!.trigger).toBe('form');
  });

  it('classifies a form submission with a recent error as manual_with_error', async () => {
    const { deps, sent } = makeDeps({ selectDiagnostics: () => SELECT_ERROR });
    const result = await processReport({ source: 'form', description: 'it broke' }, deps);
    expect(result).toMatchObject({ status: 'sent', type: 'manual_with_error' });
    expect(sent[0]!.error?.fingerprint).toMatch(/^[0-9a-f]{16}$/);
    expect(sent[0]!.error?.occurrences).toBe(1);
  });

  it('classifies an automatic error', async () => {
    const { deps, sent } = makeDeps();
    const result = await processReport({ source: 'automatic', event: makeEvent() }, deps);
    expect(result).toMatchObject({ status: 'sent', type: 'automatic_error', trigger: 'automatic' });
    expect(sent[0]!.description).toBeNull();
  });

  it('classifies captureMessage as captured_message', async () => {
    const { deps, sent } = makeDeps();
    const result = await processReport({ source: 'api', message: 'checkout stalled' }, deps);
    expect(result).toMatchObject({ status: 'sent', type: 'captured_message', trigger: 'api' });
    expect(sent[0]!.error?.type).toBe('Message');
    expect(sent[0]!.error?.message).toBe('checkout stalled');
  });

  it('classifies captureException (event) as captured_exception', async () => {
    const { deps } = makeDeps();
    const result = await processReport(
      { source: 'api', event: makeEvent({ handled: true }) },
      deps,
    );
    expect(result).toMatchObject({ status: 'sent', type: 'captured_exception', trigger: 'api' });
  });

  it('classifies captureException (raw value) as captured_exception', async () => {
    const { deps, sent } = makeDeps();
    const result = await processReport(
      { source: 'api', error: new RangeError('out of range') },
      deps,
    );
    expect(result).toMatchObject({ status: 'sent', type: 'captured_exception' });
    expect(sent[0]!.error?.type).toBe('RangeError');
    expect(sent[0]!.error?.message).toBe('out of range');
  });
});

describe('processReport — suppression', () => {
  it('suppresses an empty form description as invalid_input', async () => {
    const { deps, send } = makeDeps();
    const result = await processReport({ source: 'form', description: '   ' }, deps);
    expect(result).toMatchObject({ status: 'suppressed', reason: 'invalid_input', type: 'manual' });
    expect(send).not.toHaveBeenCalled();
  });

  it('suppresses everything when the SDK is not configured', async () => {
    const { deps } = makeDeps({ getConfig: () => null });
    const result = await processReport({ source: 'api', message: 'x' }, deps);
    expect(result).toMatchObject({ status: 'suppressed', reason: 'disabled' });
  });

  it('suppresses an automatic report without an event', async () => {
    const { deps } = makeDeps();
    const result = await processReport({ source: 'automatic' }, deps);
    expect(result).toMatchObject({ status: 'suppressed', reason: 'invalid_input', type: 'automatic_error' });
  });
});

describe('processReport — dedup', () => {
  it('sends the first automatic occurrence and suppresses the next as duplicate', async () => {
    const { deps, sent } = makeDeps();
    const first = await processReport({ source: 'automatic', event: makeEvent() }, deps);
    const second = await processReport({ source: 'automatic', event: makeEvent() }, deps);
    expect(first).toMatchObject({ status: 'sent', occurrences: 1 });
    expect(second).toMatchObject({ status: 'suppressed', reason: 'duplicate', occurrences: 2 });
    expect(sent).toHaveLength(1);
  });

  it('does not merge different stack frames for the same message', async () => {
    const { deps, sent } = makeDeps();
    const a = makeEvent({ error: { type: 'Error', message: 'x', stack: 'Error: x\n    at a (/app/a.js:1:1)' } });
    const b = makeEvent({ error: { type: 'Error', message: 'x', stack: 'Error: x\n    at b (/app/b.js:2:2)' } });
    expect((await processReport({ source: 'automatic', event: a }, deps)).status).toBe('sent');
    expect((await processReport({ source: 'automatic', event: b }, deps)).status).toBe('sent');
    expect(sent).toHaveLength(2);
  });

  it('never deduplicates manual reports', async () => {
    const { deps, sent } = makeDeps({ selectDiagnostics: () => SELECT_ERROR });
    const a = await processReport({ source: 'form', description: 'first' }, deps);
    const b = await processReport({ source: 'form', description: 'second' }, deps);
    expect(a.status).toBe('sent');
    expect(b.status).toBe('sent');
    expect(sent).toHaveLength(2);
  });

  it('reports the read-only occurrence count on a manual report', async () => {
    const shared = {
      breadcrumbs: [],
      networkFailures: [],
      error: {
        type: 'TypeError',
        message: 'boom',
        stack: 'TypeError: boom\n    at save (/app/checkout.js:10:5)',
        handled: false,
        timestamp: ISO,
      },
    } satisfies DiagnosticsSelection;
    const { deps } = makeDeps({ selectDiagnostics: () => shared });
    await processReport({ source: 'automatic', event: makeEvent() }, deps);
    await processReport({ source: 'automatic', event: makeEvent() }, deps);
    const manual = await processReport({ source: 'form', description: 'and it happened again' }, deps);
    expect(manual).toMatchObject({ status: 'sent', type: 'manual_with_error', occurrences: 2 });
  });
});

describe('processReport — beforeSend', () => {
  it('drops the report when the hook returns null', async () => {
    const { deps, send } = makeDeps({
      getConfig: () => makeConfig({ beforeSend: () => null }),
    });
    const result = await processReport({ source: 'form', description: 'drop me' }, deps);
    expect(result).toMatchObject({ status: 'suppressed', reason: 'disabled' });
    expect(send).not.toHaveBeenCalled();
  });

  it('sends unmodified and warns when the hook throws', async () => {
    const warn = vi.fn();
    const { deps, sent } = makeDeps({
      warn,
      getConfig: () =>
        makeConfig({
          beforeSend: () => {
            throw new Error('hook bug');
          },
        }),
    });
    const result = await processReport({ source: 'form', description: 'keep me' }, deps);
    expect(result.status).toBe('sent');
    expect(warn).toHaveBeenCalledTimes(1);
    expect(sent[0]!.description).toBe('keep me');
  });

  it('receives a frozen, already-sanitized report', async () => {
    let seen: SanitizedReport | null = null;
    const { deps } = makeDeps({
      getConfig: () =>
        makeConfig({
          beforeSend: (report) => {
            seen = report as SanitizedReport;
            return report;
          },
        }),
    });
    await processReport(
      {
        source: 'api',
        error: { name: 'Error', message: 'request failed token=supersecretvalue123', stack: '' },
      },
      deps,
    );
    expect(seen).not.toBeNull();
    expect(Object.isFrozen(seen)).toBe(true);
    expect(seen!.error?.message).not.toContain('supersecretvalue123');
    expect(seen!.error?.message).toContain('[REDACTED]');
  });
});

describe('processReport — transport + limits', () => {
  it('maps an HTTP failure to a failed result', async () => {
    const { deps } = makeDeps({
      send: async (): Promise<SendResult> => ({
        ok: false,
        kind: 'http',
        status: 500,
        message: 'HTTP 500',
      }),
    });
    const result = await processReport({ source: 'form', description: 'x' }, deps);
    expect(result).toMatchObject({ status: 'failed', kind: 'http', httpStatus: 500 });
  });

  it('maps a throwing transport to a network failure', async () => {
    const { deps } = makeDeps({
      send: async (): Promise<SendResult> => {
        throw new Error('offline');
      },
    });
    const result = await processReport({ source: 'form', description: 'x' }, deps);
    expect(result).toMatchObject({ status: 'failed', kind: 'network', message: 'offline' });
  });

  it('rate-limits automatic reports past the per-minute cap', async () => {
    const { deps } = makeDeps({
      limiter: new ReportLimiter({ dedupWindowMs: 0, maxPerMinute: 1, maxPerSession: 100 }),
    });
    await processReport({ source: 'automatic', event: makeEvent({ error: { type: 'E', message: 'a' } }) }, deps);
    const second = await processReport(
      { source: 'automatic', event: makeEvent({ error: { type: 'E', message: 'b' } }) },
      deps,
    );
    expect(second).toMatchObject({ status: 'suppressed', reason: 'rate_limited' });
  });

  it('drops an oversized screenshot before sending', async () => {
    const { deps, sent } = makeDeps();
    const screenshot = {
      name: 'huge.png',
      mimeType: 'image/png' as const,
      sizeBytes: 6_000_000,
      dataBase64: 'A'.repeat(MAX_REPORT_BYTES + 1),
    };
    const result = await processReport({ source: 'form', description: 'big', screenshot }, deps);
    expect(result.status).toBe('sent');
    expect(sent[0]!.screenshot).toBeNull();
  });
});
