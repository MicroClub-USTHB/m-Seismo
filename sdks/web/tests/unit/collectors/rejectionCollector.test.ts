// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest';
import { installRejectionCollector } from '@sdk/collectors/rejectionCollector';
import { FLAG_NON_ERROR_REJECTION } from '@sdk/diagnostics/types';
import type { DiagnosticEvent } from '@sdk/diagnostics/types';

function record(): { events: DiagnosticEvent[]; fn: (e: DiagnosticEvent) => void } {
  const events: DiagnosticEvent[] = [];
  return { events, fn: (e) => events.push(e) };
}

function reject(reason: unknown): Event {
  const event = new Event('unhandledrejection');
  Object.defineProperty(event, 'reason', { value: reason, configurable: true });
  return event;
}

describe('installRejectionCollector', () => {
  it('records a rejection with an Error reason', () => {
    const { events, fn } = record();
    const dispose = installRejectionCollector(window, fn);
    window.dispatchEvent(reject(new Error('async boom')));
    expect(events).toHaveLength(1);
    const event = events[0]!;
    expect(event.kind).toBe('unhandledrejection');
    expect(event.handled).toBe(false);
    expect(event.error.type).toBe('Error');
    expect(event.error.message).toBe('async boom');
    expect(event.flags ?? []).not.toContain(FLAG_NON_ERROR_REJECTION);
    dispose();
  });

  it.each([['string', 'oof'], ['number', 7], ['undefined', undefined]])(
    'normalizes a %s rejection reason',
    (_label, reason) => {
      const { events, fn } = record();
      const dispose = installRejectionCollector(window, fn);
      window.dispatchEvent(reject(reason));
      expect(events).toHaveLength(1);
      expect(events[0]!.flags).toContain(FLAG_NON_ERROR_REJECTION);
      dispose();
    },
  );

  it('removes the listener on dispose', () => {
    const { events, fn } = record();
    const dispose = installRejectionCollector(window, fn);
    dispose();
    window.dispatchEvent(reject(new Error('x')));
    expect(events).toHaveLength(0);
  });
});
