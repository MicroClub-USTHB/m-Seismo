// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { captureException, destroy, init } from '@m-seismo/web';
import {
  getDiagnosticsCount,
  getDiagnosticsSnapshot,
  resetDiagnostics,
} from '@sdk/internal';

const ENDPOINT = 'https://receiver.test/api/reports';

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
    init({ endpoint: ENDPOINT, shortcut: false, launcher: false });
    init({ endpoint: ENDPOINT, shortcut: false, launcher: false });
    window.dispatchEvent(runtimeError());
    expect(getDiagnosticsCount()).toBe(1);
  });

  it('keeps events across repeated init and applies the new size', () => {
    init({ endpoint: ENDPOINT, shortcut: false, launcher: false, diagnostics: { maxEvents: 1 } });
    captureException(new Error('a'));
    captureException(new Error('b'));
    expect(getDiagnosticsCount()).toBe(1);

    init({ endpoint: ENDPOINT, shortcut: false, launcher: false, diagnostics: { maxEvents: 5 } });
    expect(getDiagnosticsCount()).toBe(1);
    captureException(new Error('c'));
    expect(getDiagnosticsCount()).toBe(2);
  });

  it('destroy clears the buffer and stops capture', () => {
    init({ endpoint: ENDPOINT, shortcut: false, launcher: false });
    captureException(new Error('a'));
    expect(getDiagnosticsCount()).toBe(1);
    destroy();
    expect(getDiagnosticsSnapshot()).toEqual([]);
    window.dispatchEvent(runtimeError());
    expect(getDiagnosticsCount()).toBe(0);
  });

  it('respects captureUncaughtErrors:false but still captures rejections', () => {
    init({
      endpoint: ENDPOINT,
      shortcut: false,
      launcher: false,
      diagnostics: { captureUncaughtErrors: false },
    });
    window.dispatchEvent(runtimeError());
    expect(getDiagnosticsCount()).toBe(0);

    const rej = new Event('unhandledrejection');
    Object.defineProperty(rej, 'reason', { value: new Error('r') });
    window.dispatchEvent(rej);
    expect(getDiagnosticsCount()).toBe(1);
  });

  it('respects captureUnhandledRejections:false', () => {
    init({
      endpoint: ENDPOINT,
      shortcut: false,
      launcher: false,
      diagnostics: { captureUnhandledRejections: false },
    });
    const rej = new Event('unhandledrejection');
    Object.defineProperty(rej, 'reason', { value: new Error('r') });
    window.dispatchEvent(rej);
    expect(getDiagnosticsCount()).toBe(0);
  });
});
