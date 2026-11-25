/**
 * Payload limits: per-event size bounding order, the per-event byte ceiling,
 * and the report-level size guard (screenshot first, then breadcrumbs/network
 * failures — required fields are never dropped).
 */
import { describe, expect, it, vi } from 'vitest';
import { boundEventSize, byteSize } from '@sdk/diagnostics/bound';
import { FLAG_TRUNCATED, EVENT_MAX_BYTES, MAX_REPORT_BREADCRUMBS } from '@sdk/diagnostics/types';
import type { ErrorDiagnosticEvent, NetworkDiagnosticEvent } from '@sdk/diagnostics/types';
import { MAX_REPORT_BYTES, processReport } from '@sdk/reporting/processor';
import type { ReportProcessorDeps } from '@sdk/reporting/processor';
import { ReportLimiter } from '@sdk/reporting/limiter';
import type { DiagnosticsSelection } from '@sdk/diagnostics/select';
import type { ReportContext, ReportBreadcrumb } from '@sdk/core/types';
import type { SdkConfig } from '@sdk/core/config';
import type { SanitizedReport } from '@sdk/privacy/sanitize';
import type { SendResult } from '@sdk/transport/types';

const ISO = '2026-10-19T12:00:00.000Z';
const NOW = Date.parse(ISO);

function errorEvent(overrides: Partial<ErrorDiagnosticEvent> = {}): ErrorDiagnosticEvent {
  return {
    kind: 'error',
    id: 'e-1',
    timestamp: ISO,
    handled: false,
    error: { type: 'TypeError', message: 'boom', stack: 'TypeError: boom' },
    ...overrides,
  };
}

function context(): ReportContext {
  return { url: 'https://app.test/', timestamp: ISO, application: {} };
}

function config(): SdkConfig {
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
    reporting: {
      autoReport: 'off',
      sendCapturedExceptions: true,
      dedupWindowMs: 60_000,
      maxAutoReportsPerMinute: 5,
      maxAutoReportsPerSession: 50,
    },
  };
}

function makeDeps(selection: DiagnosticsSelection) {
  const sent: SanitizedReport[] = [];
  const send = vi.fn(async (report: SanitizedReport): Promise<SendResult> => {
    sent.push(report);
    return { ok: true, status: 201 };
  });
  const deps: ReportProcessorDeps = {
    now: () => NOW,
    createId: () => 'r-limit',
    getConfig: config,
    collectContext: () => context(),
    selectDiagnostics: () => selection,
    send,
    limiter: new ReportLimiter({ dedupWindowMs: 60_000, maxPerMinute: 5, maxPerSession: 50 }),
    warn: vi.fn(),
  };
  return { deps, sent, send };
}

describe('per-event size bounding', () => {
  it('returns a small event unchanged (no truncated flag)', () => {
    const event = errorEvent();
    expect(boundEventSize(event)).toBe(event);
  });

  it('keeps the stack under control first, leaving message and context intact', () => {
    const event = errorEvent({
      error: {
        type: 'Error',
        message: 'short message',
        stack: `Error: short message\n${'at frame (/app/very-long-path.js:1:1)\n'.repeat(500)}`,
      },
      context: { detail: 'still here' },
    });
    expect(byteSize(event)).toBeGreaterThan(EVENT_MAX_BYTES);

    const bounded = boundEventSize(event);
    expect(bounded.flags).toContain(FLAG_TRUNCATED);
    if (bounded.kind !== 'error') throw new Error('expected an error event');
    expect(bounded.error.message).toBe('short message');
    expect(bounded.context).toEqual({ detail: 'still here' });
    expect((bounded.error.stack ?? '').length).toBeLessThan(
      (event.error.stack ?? '').length,
    );
    expect(byteSize(bounded)).toBeLessThanOrEqual(EVENT_MAX_BYTES);
  });

  it('drops context before the message when the stack alone cannot fit', () => {
    const event = errorEvent({
      error: {
        type: 'Error',
        message: `message ${'m'.repeat(400)}`,
        stack: `Error: boom\n${'at frame (/app/very-long-path.js:1:1)\n'.repeat(500)}`,
      },
      context: { blob: 'z'.repeat(4000) },
    });
    const bounded = boundEventSize(event);
    expect(bounded.flags).toContain(FLAG_TRUNCATED);
    if (bounded.kind !== 'error') throw new Error('expected an error event');
    expect(bounded.context).toBeUndefined();
    expect(bounded.error.message.startsWith('message')).toBe(true);
    expect(bounded.error.stack).toBeUndefined();
    expect(byteSize(bounded)).toBeLessThanOrEqual(EVENT_MAX_BYTES);
  });

  it('trims an oversized network URL and flags the event', () => {
    const event: NetworkDiagnosticEvent = {
      kind: 'network',
      id: 'n-1',
      timestamp: ISO,
      data: {
        method: 'GET',
        url: `https://app.test/${'p'.repeat(4000)}`,
        durationMs: 12,
        category: 'http_error',
        status: 500,
      },
    };
    const bounded = boundEventSize(event);
    expect(bounded.flags).toContain(FLAG_TRUNCATED);
    if (bounded.kind !== 'network') throw new Error('expected a network event');
    expect(bounded.data.url.length).toBeLessThan(event.data.url.length);
    expect(byteSize(bounded)).toBeLessThanOrEqual(EVENT_MAX_BYTES);
  });
});

describe('report size guard', () => {
  it('drops the screenshot first and keeps breadcrumbs', async () => {
    const { deps, sent } = makeDeps({
      breadcrumbs: [{ timestamp: ISO, category: 'sdk', message: 'tiny' }],
      networkFailures: [],
      error: null,
    });
    const result = await processReport(
      {
        source: 'form',
        description: 'oversize',
        screenshot: {
          name: 'huge.png',
          mimeType: 'image/png',
          sizeBytes: MAX_REPORT_BYTES + 1,
          dataBase64: 'A'.repeat(MAX_REPORT_BYTES + 1),
        },
      },
      deps,
    );
    expect(result.status).toBe('sent');
    expect(sent[0]!.screenshot).toBeNull();
    expect(sent[0]!.breadcrumbs).toHaveLength(1);
    expect(sent[0]!.description).toBe('oversize');
  });

  it('drops breadcrumbs and network failures only when still too large', async () => {
    const filler = 'sec1 '.repeat(22_000); // ~110 KB, shaped so scrubbing stays cheap
    const breadcrumbs: ReportBreadcrumb[] = Array.from({ length: MAX_REPORT_BREADCRUMBS }, (
      _,
      i,
    ) => ({
      timestamp: ISO,
      category: 'navigation',
      message: `${i} ${filler}`,
      level: 'info',
    }));
    const { deps, sent } = makeDeps({
      breadcrumbs,
      networkFailures: [
        {
          timestamp: ISO,
          method: 'GET',
          url: `https://app.test/big${filler}`,
          durationMs: 5,
          category: 'http_error',
          status: 500,
        },
      ],
      error: null,
    });

    const result = await processReport({ source: 'form', description: 'very big' }, deps);
    expect(result.status).toBe('sent');
    expect(sent[0]!.breadcrumbs).toEqual([]);
    expect(sent[0]!.networkFailures).toEqual([]);
    // Required fields survive.
    expect(sent[0]!.description).toBe('very big');
    expect(sent[0]!.reportId).toBe('r-limit');
  });
});
