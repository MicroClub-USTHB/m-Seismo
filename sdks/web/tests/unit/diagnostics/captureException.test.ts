// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { captureException, destroy, init } from '@m-seismo/web';
import {
  getDiagnosticsCount,
  getDiagnosticsSnapshot,
  resetDiagnostics,
} from '@sdk/internal';
import type { ErrorDiagnosticEvent } from '@sdk/diagnostics/types';

function firstError(): ErrorDiagnosticEvent {
  const event = getDiagnosticsSnapshot().find((e) => e.kind === 'captured');
  return event as ErrorDiagnosticEvent;
}

const ENDPOINT = 'https://receiver.test/api/reports';

beforeEach(() => {
  init({
    endpoint: ENDPOINT,
    shortcut: false,
    launcher: false,
    diagnostics: {
      captureNavigation: false,
      captureInteractions: false,
      captureNetwork: false,
    },
  });
});

afterEach(() => {
  destroy();
  resetDiagnostics();
  vi.restoreAllMocks();
});

describe('captureException', () => {
  it('records a captured Error with handled:true', () => {
    captureException(new TypeError('caught'));
    const events = getDiagnosticsSnapshot();
    expect(events).toHaveLength(1);
    const event = firstError();
    expect(event.kind).toBe('captured');
    expect(event.handled).toBe(true);
    expect(event.error.type).toBe('TypeError');
    expect(event.error.message).toBe('caught');
  });

  it('records non-Error values without throwing', () => {
    expect(() => captureException('just a string')).not.toThrow();
    const event = firstError();
    expect(event.error.type).toBe('string');
    expect(event.error.message).toBe('just a string');
  });

  it('sanitizes context and never mutates the caller object', () => {
    const context: Record<string, unknown> = {
      userId: 'u-1',
      nested: { ok: true, fn: () => undefined, sym: Symbol('s') },
    };
    captureException(new Error('with context'), context);
    const event = firstError();
    expect(event.context).toEqual({ userId: 'u-1', nested: { ok: true } });
    expect(typeof (context.nested as { fn: unknown }).fn).toBe('function');
  });

  it('is a no-op with a warning before init', () => {
    destroy();
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    captureException(new Error('too early'));
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(getDiagnosticsCount()).toBe(0);
  });

  it('is a no-op after destroy', () => {
    destroy();
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    captureException(new Error('too late'));
    expect(warnSpy).toHaveBeenCalled();
    expect(getDiagnosticsSnapshot()).toEqual([]);
  });

  it('never throws even for a hostile value', () => {
    const hostile = Object.defineProperty({}, 'name', {
      get() {
        throw new Error('boom');
      },
    });
    expect(() => captureException(hostile)).not.toThrow();
  });
});
