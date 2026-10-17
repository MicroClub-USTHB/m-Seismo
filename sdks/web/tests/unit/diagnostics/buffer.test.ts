import { describe, expect, it } from 'vitest';
import { DiagnosticBuffer, clampMaxEvents } from '@sdk/diagnostics/buffer';
import {
  DEFAULT_MAX_EVENTS,
  MAX_EVENTS_CEILING,
} from '@sdk/diagnostics/types';
import type { DiagnosticEvent } from '@sdk/diagnostics/types';

function event(id: number): DiagnosticEvent {
  return {
    id: `e-${id}`,
    kind: 'error',
    timestamp: new Date(0).toISOString(),
    error: { type: 'Error', message: `m-${id}` },
    handled: false,
  };
}

describe('clampMaxEvents', () => {
  it('defaults non-positive and non-integer values', () => {
    expect(clampMaxEvents(undefined)).toBe(DEFAULT_MAX_EVENTS);
    expect(clampMaxEvents(0)).toBe(DEFAULT_MAX_EVENTS);
    expect(clampMaxEvents(-5)).toBe(DEFAULT_MAX_EVENTS);
    expect(clampMaxEvents(1.5)).toBe(DEFAULT_MAX_EVENTS);
    expect(clampMaxEvents(Number.NaN)).toBe(DEFAULT_MAX_EVENTS);
  });

  it('clamps to the ceiling', () => {
    expect(clampMaxEvents(10_000)).toBe(MAX_EVENTS_CEILING);
  });

  it('accepts a valid positive integer', () => {
    expect(clampMaxEvents(7)).toBe(7);
  });
});

describe('DiagnosticBuffer', () => {
  it('keeps events in insertion order', () => {
    const buffer = new DiagnosticBuffer(5);
    buffer.push(event(1));
    buffer.push(event(2));
    expect(buffer.snapshot().map((e) => e.id)).toEqual(['e-1', 'e-2']);
    expect(buffer.size()).toBe(2);
  });

  it('evicts the oldest when over capacity', () => {
    const buffer = new DiagnosticBuffer(2);
    buffer.push(event(1));
    buffer.push(event(2));
    buffer.push(event(3));
    expect(buffer.snapshot().map((e) => e.id)).toEqual(['e-2', 'e-3']);
  });

  it('snapshot returns deep copies (mutation-safe)', () => {
    const buffer = new DiagnosticBuffer();
    buffer.push({
      ...event(1),
      context: { nested: { a: 1 } },
    });
    const first = buffer.snapshot();
    (first[0]!.context!.nested as { a: number }).a = 99;
    const second = buffer.snapshot();
    expect((second[0]!.context!.nested as { a: number }).a).toBe(1);
  });

  it('clear empties the buffer', () => {
    const buffer = new DiagnosticBuffer();
    buffer.push(event(1));
    buffer.clear();
    expect(buffer.size()).toBe(0);
    expect(buffer.snapshot()).toEqual([]);
  });

  it('resize applies a smaller capacity immediately', () => {
    const buffer = new DiagnosticBuffer(5);
    buffer.push(event(1));
    buffer.push(event(2));
    buffer.push(event(3));
    buffer.resize(1);
    expect(buffer.snapshot().map((e) => e.id)).toEqual(['e-3']);
  });
});
