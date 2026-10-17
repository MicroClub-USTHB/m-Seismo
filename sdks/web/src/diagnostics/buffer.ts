/**
 * Bounded in-memory diagnostic buffer (Phase 5.2).
 *
 * A ring buffer that keeps the most recent events, oldest first. When full, the
 * oldest event is evicted. `snapshot()` returns deep copies so callers can never
 * mutate the buffer's contents.
 *
 * Internal only: not exported from the public API.
 */
import { DEFAULT_MAX_EVENTS, MAX_EVENTS_CEILING } from './types';
import type { DiagnosticEvent, JsonValue } from './types';

/** Clamps a configured max to a positive integer no larger than the ceiling. */
export function clampMaxEvents(value: unknown): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0) {
    return DEFAULT_MAX_EVENTS;
  }
  return Math.min(value, MAX_EVENTS_CEILING);
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
    ...(event.flags !== undefined ? { flags: [...event.flags] } : {}),
  };
}

export class DiagnosticBuffer {
  private events: DiagnosticEvent[] = [];
  private max: number;

  constructor(maxEvents: number = DEFAULT_MAX_EVENTS) {
    this.max = clampMaxEvents(maxEvents);
  }

  /** Appends an event, evicting the oldest when over capacity. */
  push(event: DiagnosticEvent): void {
    this.events.push(event);
    const overflow = this.events.length - this.max;
    if (overflow > 0) this.events.splice(0, overflow);
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
  resize(maxEvents: number): void {
    this.max = clampMaxEvents(maxEvents);
    const overflow = this.events.length - this.max;
    if (overflow > 0) this.events.splice(0, overflow);
  }

  /** Current capacity. */
  get maxEvents(): number {
    return this.max;
  }
}
