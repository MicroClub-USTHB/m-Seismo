// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest';
import { installErrorCollector } from '@sdk/collectors/errorCollector';
import { FLAG_CROSS_ORIGIN_SCRIPT_ERROR } from '@sdk/diagnostics/types';
import type { DiagnosticEvent, ErrorDiagnosticEvent } from '@sdk/diagnostics/types';

function record(): { events: DiagnosticEvent[]; fn: (e: DiagnosticEvent) => void } {
  const events: DiagnosticEvent[] = [];
  return { events, fn: (e) => events.push(e) };
}

describe('installErrorCollector', () => {
  it('records a genuine runtime error', () => {
    const { events, fn } = record();
    const dispose = installErrorCollector(window, fn);

    const err = new TypeError('boom');
    window.dispatchEvent(
      new ErrorEvent('error', {
        message: 'boom',
        error: err,
        filename: 'https://app.test/main.js',
        lineno: 12,
        colno: 4,
      }),
    );

    expect(events).toHaveLength(1);
    const event = events[0]! as ErrorDiagnosticEvent;
    expect(event.kind).toBe('error');
    expect(event.handled).toBe(false);
    expect(event.error.type).toBe('TypeError');
    expect(event.error.message).toBe('boom');
    expect(event.error.source).toEqual({
      filename: 'https://app.test/main.js',
      line: 12,
      column: 4,
    });
    dispose();
  });

  it('records a cross-origin "Script error." with the flag', () => {
    const { events, fn } = record();
    const dispose = installErrorCollector(window, fn);
    window.dispatchEvent(new ErrorEvent('error', { message: 'Script error.' }));
    expect(events).toHaveLength(1);
    expect(events[0]!.flags).toContain(FLAG_CROSS_ORIGIN_SCRIPT_ERROR);
    dispose();
  });

  it('ignores a resource-style event whose target is an element', () => {
    const { events, fn } = record();
    const dispose = installErrorCollector(window, fn);
    const img = document.createElement('img');
    img.dispatchEvent(new Event('error', { bubbles: true }));
    expect(events).toHaveLength(0);
    dispose();
  });

  it('ignores an error event with no message and no error', () => {
    const { events, fn } = record();
    const dispose = installErrorCollector(window, fn);
    window.dispatchEvent(new Event('error'));
    expect(events).toHaveLength(0);
    dispose();
  });

  it('removes the listener on dispose', () => {
    const { events, fn } = record();
    const dispose = installErrorCollector(window, fn);
    dispose();
    window.dispatchEvent(new ErrorEvent('error', { message: 'x', error: new Error('x') }));
    expect(events).toHaveLength(0);
  });

  it('never throws when the record callback throws', () => {
    const spy = vi.fn(() => {
      throw new Error('record failed');
    });
    const dispose = installErrorCollector(window, spy);
    expect(() => {
      window.dispatchEvent(new ErrorEvent('error', { message: 'x', error: new Error('x') }));
    }).not.toThrow();
    dispose();
  });
});
