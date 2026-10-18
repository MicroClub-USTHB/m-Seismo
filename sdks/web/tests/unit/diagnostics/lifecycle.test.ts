// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { captureException, destroy, init } from '@m-seismo/web';
import type { DiagnosticsOptions, InitOptions } from '@m-seismo/web';
import {
  getDiagnosticsCount,
  getDiagnosticsSnapshot,
  resetDiagnostics,
} from '@sdk/internal';

const ENDPOINT = 'https://receiver.test/api/reports';

/**
 * Initialises with the Phase 5.3 collectors off so these lifecycle tests count
 * only the error/rejection events they dispatch. Navigation especially adds an
 * initial `load` breadcrumb that would otherwise inflate the counts.
 */
function initDiag(diagnostics: DiagnosticsOptions = {}): void {
  const options: InitOptions = {
    endpoint: ENDPOINT,
    shortcut: false,
    launcher: false,
    diagnostics: {
      captureNavigation: false,
      captureInteractions: false,
      captureNetwork: false,
      ...diagnostics,
    },
  };
  init(options);
}

function runtimeError(): Event {
  return new ErrorEvent('error', { message: 'boom', error: new Error('boom') });
}

afterEach(() => {
  destroy();
  resetDiagnostics();
  vi.restoreAllMocks();
});

describe('diagnostics lifecycle', () => {
  it('registers exactly one listener per type across repeated init', () => {
    initDiag();
    initDiag();
    window.dispatchEvent(runtimeError());
    expect(getDiagnosticsCount()).toBe(1);
  });

  it('keeps events across repeated init and applies the new size', () => {
    initDiag({ maxEvents: 1 });
    captureException(new Error('a'));
    captureException(new Error('b'));
    expect(getDiagnosticsCount()).toBe(1);

    initDiag({ maxEvents: 5 });
    expect(getDiagnosticsCount()).toBe(1);
    captureException(new Error('c'));
    expect(getDiagnosticsCount()).toBe(2);
  });

  it('destroy clears the buffer and stops capture', () => {
    initDiag();
    captureException(new Error('a'));
    expect(getDiagnosticsCount()).toBe(1);
    destroy();
    expect(getDiagnosticsSnapshot()).toEqual([]);
    window.dispatchEvent(runtimeError());
    expect(getDiagnosticsCount()).toBe(0);
  });

  it('respects captureUncaughtErrors:false but still captures rejections', () => {
    initDiag({ captureUncaughtErrors: false });
    window.dispatchEvent(runtimeError());
    expect(getDiagnosticsCount()).toBe(0);

    const rej = new Event('unhandledrejection');
    Object.defineProperty(rej, 'reason', { value: new Error('r') });
    window.dispatchEvent(rej);
    expect(getDiagnosticsCount()).toBe(1);
  });

  it('respects captureUnhandledRejections:false', () => {
    initDiag({ captureUnhandledRejections: false });
    const rej = new Event('unhandledrejection');
    Object.defineProperty(rej, 'reason', { value: new Error('r') });
    window.dispatchEvent(rej);
    expect(getDiagnosticsCount()).toBe(0);
  });
});
