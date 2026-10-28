// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { captureException, captureMessage, destroy, init } from '@m-seismo/web';
import { processInput } from '@sdk/reporting/runtime';
import { handleAutomaticDiagnostic } from '@sdk/reporting/runtime';
import { resetOriginalFetch } from '@sdk/network/originalFetch';
import { getReportProcessorStats, resetDiagnostics } from '@sdk/internal';
import type { ErrorDiagnosticEvent } from '@sdk/diagnostics/types';

const ENDPOINT = 'https://receiver.test/api/reports';

function errorEvent(message = 'boom'): ErrorDiagnosticEvent {
  return {
    kind: 'error',
    id: `e-${message}`,
    timestamp: new Date().toISOString(),
    handled: false,
    error: { type: 'TypeError', message, stack: `TypeError: ${message}\n    at f (/app/a.js:1:1)` },
  };
}

function okResponse(): Response {
  return new Response(JSON.stringify({ id: 'srv-1' }), {
    status: 201,
    headers: { 'content-type': 'application/json' },
  });
}

let fetchSpy: ReturnType<typeof vi.fn>;

function start(reporting: Record<string, unknown> = {}): void {
  fetchSpy = vi.fn(async () => okResponse());
  vi.stubGlobal('fetch', fetchSpy);
  resetOriginalFetch();
  init({
    endpoint: ENDPOINT,
    shortcut: false,
    launcher: false,
    diagnostics: {
      captureNavigation: false,
      captureInteractions: false,
      captureNetwork: false,
    },
    reporting,
  });
}

function lastBody(): Record<string, unknown> {
  const calls = fetchSpy.mock.calls;
  const call = calls[calls.length - 1] as [unknown, { body?: string } | undefined] | undefined;
  const init_ = call?.[1];
  return JSON.parse(init_?.body ?? '{}') as Record<string, unknown>;
}

afterEach(() => {
  destroy();
  resetDiagnostics();
  resetOriginalFetch();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('runtime automatic reporting', () => {
  it('does not send when autoReport is off, but still records', async () => {
    start({ autoReport: 'off' });
    handleAutomaticDiagnostic(errorEvent());
    await new Promise((r) => setTimeout(r, 5));
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('sends an automatic_error report when autoReport is errors', async () => {
    start({ autoReport: 'errors' });
    handleAutomaticDiagnostic(errorEvent());
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    const body = lastBody();
    expect(body['type']).toBe('automatic_error');
    expect(body['trigger']).toBe('automatic');
    expect(body['description']).toBeNull();
  });

  it('does not start a second automatic report while one is in flight', async () => {
    let resolveFetch: ((value: Response) => void) | undefined;
    start({ autoReport: 'errors' });
    fetchSpy.mockImplementation(
      () => new Promise<Response>((resolve) => { resolveFetch = resolve; }),
    );

    handleAutomaticDiagnostic(errorEvent('first'));
    handleAutomaticDiagnostic(errorEvent('second'));

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    resolveFetch?.(okResponse());
    await vi.waitFor(() => expect(getReportProcessorStats().processing).toBe(false));
  });
});

describe('runtime captured reporting', () => {
  it('sends a captured_exception report from captureException', async () => {
    start({ autoReport: 'off' });
    captureException(new Error('handled'));
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    const body = lastBody();
    expect(body['type']).toBe('captured_exception');
    expect(body['trigger']).toBe('api');
  });

  it('sends a captured_message report from captureMessage', async () => {
    start({ autoReport: 'off' });
    captureMessage('checkout stalled');
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    const body = lastBody();
    expect(body['type']).toBe('captured_message');
    expect((body['error'] as { message: string }).message).toBe('checkout stalled');
  });

  it('does not send captured reports when sendCapturedExceptions is false', async () => {
    start({ autoReport: 'off', sendCapturedExceptions: false });
    captureException(new Error('handled'));
    captureMessage('note');
    await new Promise((r) => setTimeout(r, 5));
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('still sends a manual report when captured sending is disabled', async () => {
    start({ sendCapturedExceptions: false });
    const result = await processInput({ source: 'form', description: 'user report' });
    expect(result.status).toBe('sent');
    expect(lastBody()['type']).toBe('manual');
  });
});

describe('runtime state lifecycle', () => {
  it('clears dedup state on destroy', async () => {
    start({ autoReport: 'errors' });
    handleAutomaticDiagnostic(errorEvent());
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    expect(getReportProcessorStats().fingerprints).toBe(1);
    destroy();
    expect(getReportProcessorStats()).toEqual({
      fingerprints: 0,
      sessionSends: 0,
      minuteSends: 0,
      suppressed: { duplicate: 0, rate_limited: 0 },
      processing: false,
    });
  });
});
