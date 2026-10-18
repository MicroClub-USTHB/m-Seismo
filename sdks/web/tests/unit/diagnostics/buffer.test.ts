import { describe, expect, it } from 'vitest';
import { DiagnosticBuffer, clampMaxAgeMs, clampMaxEvents } from '@sdk/diagnostics/buffer';
import {
  DEFAULT_MAX_AGE_MS,
  DEFAULT_MAX_EVENTS,
  MAX_EVENTS_CEILING,
  MAX_MAX_AGE_MS,
  MIN_MAX_AGE_MS,
} from '@sdk/diagnostics/types';
import type { ErrorDiagnosticEvent } from '@sdk/diagnostics/types';

const NOW = Date.parse('2026-01-02T03:04:05.000Z');

function event(id: number): ErrorDiagnosticEvent {
  return {
    id: `e-${id}`,
    kind: 'error',
    timestamp: new Date(NOW).toISOString(),
    error: { type: 'Error', message: `m-${id}` },
    handled: false,
  };
}

function makeBuffer(maxEvents?: number, maxAgeMs?: number): DiagnosticBuffer {
  return new DiagnosticBuffer({
    ...(maxEvents !== undefined ? { maxEvents } : {}),
    ...(maxAgeMs !== undefined ? { maxAgeMs } : {}),
    now: () => NOW,
  });
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

describe('clampMaxAgeMs', () => {
  it('defaults non-positive/non-finite values', () => {
    expect(clampMaxAgeMs(undefined)).toBe(DEFAULT_MAX_AGE_MS);
    expect(clampMaxAgeMs(0)).toBe(DEFAULT_MAX_AGE_MS);
    expect(clampMaxAgeMs(Number.NaN)).toBe(DEFAULT_MAX_AGE_MS);
  });

  it('clamps to [10s, 1h]', () => {
    expect(clampMaxAgeMs(1)).toBe(MIN_MAX_AGE_MS);
    expect(clampMaxAgeMs(10 * 60 * 60 * 1000)).toBe(MAX_MAX_AGE_MS);
    expect(clampMaxAgeMs(60_000)).toBe(60_000);
  });
});

describe('DiagnosticBuffer', () => {
  it('keeps events in insertion order', () => {
    const buffer = makeBuffer(5);
    buffer.push(event(1));
    buffer.push(event(2));
    expect(buffer.snapshot().map((e) => e.id)).toEqual(['e-1', 'e-2']);
    expect(buffer.size()).toBe(2);
  });

  it('evicts the oldest when over capacity', () => {
    const buffer = makeBuffer(2);
    buffer.push(event(1));
    buffer.push(event(2));
    buffer.push(event(3));
    expect(buffer.snapshot().map((e) => e.id)).toEqual(['e-2', 'e-3']);
  });

  it('prunes events older than maxAgeMs', () => {
    const buffer = makeBuffer(10, 60_000);
    buffer.push({ ...event(1), timestamp: new Date(NOW - 120_000).toISOString() });
    buffer.push({ ...event(2), timestamp: new Date(NOW - 30_000).toISOString() });
    expect(buffer.snapshot().map((e) => e.id)).toEqual(['e-2']);
  });

  it('snapshot returns deep copies (mutation-safe)', () => {
    const buffer = makeBuffer(10);
    buffer.push({
      ...event(1),
      context: { nested: { a: 1 } },
    });
    const first = buffer.snapshot();
    ((first[0]! as ErrorDiagnosticEvent).context!.nested as { a: number }).a = 99;
    const second = buffer.snapshot();
    expect(((second[0]! as ErrorDiagnosticEvent).context!.nested as { a: number }).a).toBe(1);
  });

  it('clear empties the buffer', () => {
    const buffer = makeBuffer(10);
    buffer.push(event(1));
    buffer.clear();
    expect(buffer.size()).toBe(0);
    expect(buffer.snapshot()).toEqual([]);
  });

  it('resize applies a smaller capacity immediately', () => {
    const buffer = makeBuffer(5);
    buffer.push(event(1));
    buffer.push(event(2));
    buffer.push(event(3));
    buffer.resize(1);
    expect(buffer.snapshot().map((e) => e.id)).toEqual(['e-3']);
  });
});
