/**
 * Per-event size bounding (Phase 5.3).
 *
 * A single diagnostic event must not blow up the buffer or a later report, so
 * every event that goes through {@link boundEventSize} is trimmed field-by-field
 * until its serialized size is at most {@link EVENT_MAX_BYTES}. Trimming always
 * sets the `truncated` flag; an event is never silently dropped.
 *
 * Order is deterministic: the largest, least-important fields are reduced first
 * (stacks, then long messages, then context), so the event still carries its
 * kind, timestamp and the headline facts.
 */
import { truncateString } from './jsonSafe';
import { EVENT_MAX_BYTES, FLAG_TRUNCATED } from './types';
import type {
  DiagnosticEvent,
  ErrorDiagnosticEvent,
  InteractionDiagnosticEvent,
  JsonValue,
  NavigationDiagnosticEvent,
  NetworkDiagnosticEvent,
  SdkDiagnosticEvent,
} from './types';

/** Serialized size of a value in UTF-8 bytes, never throwing. */
export function byteSize(value: unknown): number {
  let json: string;
  try {
    json = JSON.stringify(value) ?? '';
  } catch {
    return Number.MAX_SAFE_INTEGER;
  }
  try {
    return new TextEncoder().encode(json).length;
  } catch {
    return json.length;
  }
}

function addTruncatedFlag(flags: readonly string[] | undefined): string[] {
  const next = flags !== undefined ? [...flags] : [];
  if (!next.includes(FLAG_TRUNCATED)) next.push(FLAG_TRUNCATED);
  return next;
}

function boundError(
  event: ErrorDiagnosticEvent,
  maxBytes: number,
): ErrorDiagnosticEvent {
  let message = event.error.message;
  let stack = event.error.stack;
  let context: Record<string, JsonValue> | undefined = event.context;

  const make = (): ErrorDiagnosticEvent => ({
    id: event.id,
    kind: event.kind,
    timestamp: event.timestamp,
    error: {
      type: event.error.type,
      message,
      ...(stack !== undefined ? { stack } : {}),
      ...(event.error.cause !== undefined ? { cause: event.error.cause } : {}),
      ...(event.error.source !== undefined ? { source: event.error.source } : {}),
    },
    handled: event.handled,
    ...(context !== undefined ? { context } : {}),
    flags: addTruncatedFlag(event.flags),
  });

  const steps: Array<() => void> = [
    () => {
      if (stack !== undefined) stack = truncateString(stack, 1000);
    },
    () => {
      if (stack !== undefined) stack = truncateString(stack, 300);
    },
    () => {
      stack = undefined;
    },
    () => {
      message = truncateString(message, 200);
    },
    () => {
      message = truncateString(message, 80);
    },
    () => {
      context = undefined;
    },
    () => {
      message = '';
    },
  ];
  for (const step of steps) {
    step();
    if (byteSize(make()) <= maxBytes) break;
  }
  return make();
}

function boundNetwork(
  event: NetworkDiagnosticEvent,
  maxBytes: number,
): NetworkDiagnosticEvent {
  let url = event.data.url;
  const make = (): NetworkDiagnosticEvent => ({
    ...event,
    data: { ...event.data, url },
    flags: addTruncatedFlag(event.flags),
  });
  const steps: Array<() => void> = [
    () => {
      url = truncateString(url, 512);
    },
    () => {
      url = truncateString(url, 128);
    },
  ];
  for (const step of steps) {
    step();
    if (byteSize(make()) <= maxBytes) break;
  }
  return make();
}

function boundNavigation(
  event: NavigationDiagnosticEvent,
  maxBytes: number,
): NavigationDiagnosticEvent {
  const make = (to: string, from: string | undefined): NavigationDiagnosticEvent => ({
    ...event,
    data: { trigger: event.data.trigger, to, ...(from !== undefined ? { from } : {}) },
    flags: addTruncatedFlag(event.flags),
  });
  const candidates: NavigationDiagnosticEvent[] = [
    make(
      truncateString(event.data.to, 512),
      event.data.from !== undefined ? truncateString(event.data.from, 512) : undefined,
    ),
    make(
      truncateString(event.data.to, 256),
      event.data.from !== undefined ? truncateString(event.data.from, 256) : undefined,
    ),
  ];
  for (const candidate of candidates) {
    if (byteSize(candidate) <= maxBytes) return candidate;
  }
  return candidates[candidates.length - 1]!;
}

function boundInteraction(
  event: InteractionDiagnosticEvent,
  maxBytes: number,
): InteractionDiagnosticEvent {
  const target = event.data.target;
  const label = target.label !== undefined ? truncateString(target.label, 20) : undefined;
  const bounded: InteractionDiagnosticEvent = {
    ...event,
    data: {
      action: event.data.action,
      target: { ...target, ...(label !== undefined ? { label } : {}) },
    },
    flags: addTruncatedFlag(event.flags),
  };
  return byteSize(bounded) <= maxBytes ? bounded : event;
}

function boundSdk(event: SdkDiagnosticEvent): SdkDiagnosticEvent {
  return {
    ...event,
    data: { name: truncateString(event.data.name, 64) },
    flags: addTruncatedFlag(event.flags),
  };
}

/**
 * Returns a copy of `event` bounded to `maxBytes`. If the event already fits it
 * is returned unchanged; otherwise fields are trimmed and the `truncated` flag
 * is added. Never throws.
 */
export function boundEventSize(
  event: DiagnosticEvent,
  maxBytes: number = EVENT_MAX_BYTES,
): DiagnosticEvent {
  if (byteSize(event) <= maxBytes) return event;

  switch (event.kind) {
    case 'error':
    case 'unhandledrejection':
    case 'captured':
      return boundError(event, maxBytes);
    case 'network':
      return boundNetwork(event, maxBytes);
    case 'navigation':
      return boundNavigation(event, maxBytes);
    case 'interaction':
      return boundInteraction(event, maxBytes);
    case 'sdk':
      return boundSdk(event);
  }
}
