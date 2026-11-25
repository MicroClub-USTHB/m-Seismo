/**
 * Bounded in-memory diagnostic buffer (Phase 5.2, age-aware in Phase 5.3).
 *
 * A ring buffer that keeps the most recent events, oldest first. When full, the
 * oldest event is evicted. Events older than `maxAgeMs` are pruned lazily on
 * insert (and can be pruned on demand via {@link DiagnosticBuffer.prune}).
 * `snapshot()` returns deep copies so callers can never mutate the buffer.
 *
 * The clock is injectable (`now`) so age pruning is deterministic in tests.
 *
 * Internal only: not exported from the public API.
 */
import {
  DEFAULT_MAX_AGE_MS,
  DEFAULT_MAX_EVENTS,
  MAX_EVENTS_CEILING,
  MAX_MAX_AGE_MS,
  MIN_MAX_AGE_MS,
} from './types';
import type { DiagnosticEvent, JsonValue } from './types';

/** Clamps a configured max to a positive integer no larger than the ceiling. */
export function clampMaxEvents(value: unknown): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0) {
    return DEFAULT_MAX_EVENTS;
  }
  return Math.min(value, MAX_EVENTS_CEILING);
}

/** Clamps a configured max age to the supported [10s, 1h] range. */
export function clampMaxAgeMs(value: unknown): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return DEFAULT_MAX_AGE_MS;
  }
  return Math.min(Math.max(value, MIN_MAX_AGE_MS), MAX_MAX_AGE_MS);
}

function cloneJson(value: JsonValue): JsonValue {
  if (Array.isArray(value)) return value.map(cloneJson);
  if (value !== null && typeof value === 'object') {
    const out: Record<string, JsonValue> = {};
    for (const key of Object.keys(value)) {
      out[key] = cloneJson(value[key]!);
    }
    return out;
  }
  return value;
}

function cloneContext(
  context: Record<string, JsonValue>,
): Record<string, JsonValue> {
  return cloneJson(context) as Record<string, JsonValue>;
}

function cloneEvent(event: DiagnosticEvent): DiagnosticEvent {
  const flags =
    event.flags !== undefined ? { flags: [...event.flags] } : {};
  switch (event.kind) {
    case 'error':
    case 'unhandledrejection':
    case 'captured':
      return {
        id: event.id,
        kind: event.kind,
        timestamp: event.timestamp,
        error: {
          type: event.error.type,
          message: event.error.message,
          ...(event.error.stack !== undefined ? { stack: event.error.stack } : {}),
          ...(event.error.cause !== undefined ? { cause: { ...event.error.cause } } : {}),
          ...(event.error.source !== undefined ? { source: { ...event.error.source } } : {}),
        },
        handled: event.handled,
        ...(event.context !== undefined ? { context: cloneContext(event.context) } : {}),
        ...flags,
      };
    case 'interaction':
      return {
        id: event.id,
        kind: event.kind,
        timestamp: event.timestamp,
        data: {
          action: event.data.action,
          target: { ...event.data.target },
        },
        ...flags,
      };
    case 'navigation':
      return {
        id: event.id,
        kind: event.kind,
        timestamp: event.timestamp,
        data: { ...event.data },
        ...flags,
      };
    case 'network':
      return {
        id: event.id,
        kind: event.kind,
        timestamp: event.timestamp,
        data: { ...event.data },
        ...flags,
      };
    case 'sdk':
      return {
        id: event.id,
        kind: event.kind,
        timestamp: event.timestamp,
        data: { ...event.data },
        ...flags,
      };
  }
}

export interface DiagnosticBufferOptions {
  /** Maximum number of buffered events. */
  readonly maxEvents?: number;
  /** Maximum age of a buffered event in milliseconds. */
  readonly maxAgeMs?: number;
  /** Injectable wall clock (ms since epoch). Defaults to `Date.now`. */
  readonly now?: () => number;
}

export class DiagnosticBuffer {
  private events: DiagnosticEvent[] = [];
  private max: number;
  private maxAgeMs: number;
  private readonly now: () => number;

  constructor(options: DiagnosticBufferOptions | number = {}) {
    const opts: DiagnosticBufferOptions =
      typeof options === 'number' ? { maxEvents: options } : options;
    this.max = clampMaxEvents(opts.maxEvents);
    this.maxAgeMs = clampMaxAgeMs(opts.maxAgeMs);
    this.now = opts.now ?? Date.now;
  }

  /** Appends an event, evicting the oldest when over capacity, then prunes. */
  push(event: DiagnosticEvent): void {
    this.events.push(event);
    const overflow = this.events.length - this.max;
    if (overflow > 0) this.events.splice(0, overflow);
    this.prune();
  }

  /** Removes events older than {@link maxAgeMs} relative to the clock. */
  prune(): void {
    if (this.events.length === 0) return;
    const cutoff = this.now() - this.maxAgeMs;
    let drop = 0;
    while (drop < this.events.length) {
      const event = this.events[drop]!;
      const time = Date.parse(event.timestamp);
      if (Number.isNaN(time) || time >= cutoff) break;
      drop += 1;
    }
    if (drop > 0) this.events.splice(0, drop);
  }

  /** Deep copies of the buffered events, in insertion order (oldest first). */
  snapshot(): DiagnosticEvent[] {
    return this.events.map(cloneEvent);
  }

  /** Removes every buffered event. */
  clear(): void {
    this.events = [];
  }

  /** Current number of buffered events. */
  size(): number {
    return this.events.length;
  }

  /** Applies a new capacity, evicting the oldest events if it shrank. */
  resize(maxEvents: number, maxAgeMs?: number): void {
    this.max = clampMaxEvents(maxEvents);
    if (maxAgeMs !== undefined) this.maxAgeMs = clampMaxAgeMs(maxAgeMs);
    const overflow = this.events.length - this.max;
    if (overflow > 0) this.events.splice(0, overflow);
    this.prune();
  }

  /** Current capacity. */
  get maxEvents(): number {
    return this.max;
  }

  /** Current max age in milliseconds. */
  get maxAge(): number {
    return this.maxAgeMs;
  }
}
