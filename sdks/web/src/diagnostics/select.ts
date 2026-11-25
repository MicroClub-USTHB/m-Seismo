/**
 * `selectDiagnostics()` (Phase 5.3) — the pure bridge from the diagnostic buffer
 * to a report payload.
 *
 * Given a buffer snapshot, the current wall-clock time and the relevant config,
 * it picks the events that belong on a report:
 *
 * - events older than `maxAgeMs` are excluded;
 * - `network` events (except `opaque_response`, which is not a failure) become
 *   `networkFailures`, newest-first capped to {@link MAX_REPORT_NETWORK_FAILURES}
 *   then re-ordered oldest-first;
 * - every other kind becomes a breadcrumb, newest-first capped to
 *   {@link MAX_REPORT_BREADCRUMBS} then re-ordered oldest-first;
 * - the top-level `error` is the most recent error-like event within the window.
 *
 * It is a pure function (no globals, never throws). `now` is injected so tests
 * are deterministic.
 */
import {
  MAX_REPORT_BREADCRUMBS,
  MAX_REPORT_NETWORK_FAILURES,
} from './types';
import type {
  DiagnosticEvent,
  ErrorDiagnosticEvent,
  InteractionDiagnosticEvent,
  JsonValue,
  NavigationDiagnosticEvent,
  NetworkDiagnosticEvent,
} from './types';
import type {
  ReportBreadcrumb,
  ReportError,
  ReportNetworkFailure,
} from '../core/types';

export interface SelectDiagnosticsOptions {
  /** When false, all three outputs are empty. */
  readonly includeInReports: boolean;
  /** Only events newer than this (ms) relative to `now` are included. */
  readonly maxAgeMs: number;
  /** Override the breadcrumb cap (defaults to {@link MAX_REPORT_BREADCRUMBS}). */
  readonly maxBreadcrumbs?: number;
  /** Override the network-failure cap (defaults to {@link MAX_REPORT_NETWORK_FAILURES}). */
  readonly maxNetworkFailures?: number;
}

export interface DiagnosticsSelection {
  readonly breadcrumbs: ReportBreadcrumb[];
  readonly networkFailures: ReportNetworkFailure[];
  readonly error: ReportError | null;
}

/** True when an event is within the age window. */
function withinAge(event: DiagnosticEvent, now: number, maxAgeMs: number): boolean {
  const time = Date.parse(event.timestamp);
  if (Number.isNaN(time)) return true;
  return now - time <= maxAgeMs;
}

function isErrorEvent(event: DiagnosticEvent): event is ErrorDiagnosticEvent {
  return (
    event.kind === 'error' ||
    event.kind === 'unhandledrejection' ||
    event.kind === 'captured'
  );
}

function targetData(target: InteractionDiagnosticEvent['data']['target']): Record<string, JsonValue> {
  const data: Record<string, JsonValue> = { tag: target.tag };
  if (target.id !== undefined) data['id'] = target.id;
  if (target.role !== undefined) data['role'] = target.role;
  if (target.testId !== undefined) data['testId'] = target.testId;
  if (target.type !== undefined) data['type'] = target.type;
  if (target.name !== undefined) data['name'] = target.name;
  if (target.label !== undefined) data['label'] = target.label;
  return data;
}

function networkData(data: NetworkDiagnosticEvent['data']): Record<string, JsonValue> {
  const out: Record<string, JsonValue> = {
    method: data.method,
    url: data.url,
    durationMs: data.durationMs,
    category: data.category,
  };
  if (data.status !== undefined) out['status'] = data.status;
  return out;
}

function navigationData(data: NavigationDiagnosticEvent['data']): Record<string, JsonValue> {
  const out: Record<string, JsonValue> = {
    trigger: data.trigger,
    to: data.to,
  };
  if (data.from !== undefined) out['from'] = data.from;
  return out;
}

function toBreadcrumb(event: DiagnosticEvent): ReportBreadcrumb {
  if (isErrorEvent(event)) {
    const data: Record<string, JsonValue> = { type: event.error.type };
    if (event.error.source !== undefined) {
      const source: Record<string, JsonValue> = {};
      if (event.error.source.filename !== undefined) source['filename'] = event.error.source.filename;
      if (event.error.source.line !== undefined) source['line'] = event.error.source.line;
      if (event.error.source.column !== undefined) source['column'] = event.error.source.column;
      data['source'] = source;
    }
    return {
      timestamp: event.timestamp,
      category: event.kind,
      message: event.error.message.length > 0 ? event.error.message : event.error.type,
      level: 'error',
      data,
    };
  }

  switch (event.kind) {
    case 'navigation':
      return {
        timestamp: event.timestamp,
        category: 'navigation',
        message: `navigation ${event.data.trigger}: ${event.data.from ?? '(initial)'} → ${event.data.to}`,
        level: 'info',
        data: navigationData(event.data),
      };
    case 'interaction':
      return {
        timestamp: event.timestamp,
        category: 'interaction',
        message: `${event.data.action} on <${event.data.target.tag}>`,
        level: 'info',
        data: { action: event.data.action, target: targetData(event.data.target) },
      };
    case 'sdk':
      return {
        timestamp: event.timestamp,
        category: 'sdk',
        message: event.data.name,
        level: 'info',
        data: { name: event.data.name },
      };
    case 'network':
      return {
        timestamp: event.timestamp,
        category: 'sdk',
        message: `fetch ${event.data.method} ${event.data.url} (opaque)`,
        level: 'info',
        data: networkData(event.data),
      };
  }
}

function toNetworkFailure(event: NetworkDiagnosticEvent): ReportNetworkFailure {
  return {
    timestamp: event.timestamp,
    method: event.data.method,
    url: event.data.url,
    ...(event.data.status !== undefined ? { status: event.data.status } : {}),
    durationMs: event.data.durationMs,
    category: event.data.category,
  };
}

function toReportError(event: ErrorDiagnosticEvent): ReportError {
  return {
    type: event.error.type,
    message: event.error.message,
    ...(event.error.stack !== undefined ? { stack: event.error.stack } : {}),
    handled: event.handled,
    timestamp: event.timestamp,
    ...(event.error.source !== undefined ? { source: event.error.source } : {}),
    ...(event.flags !== undefined ? { flags: event.flags } : {}),
    ...(event.context !== undefined ? { context: event.context } : {}),
  };
}

/** Last `cap` items, re-ordered oldest-first. */
function tail<T>(items: readonly T[], cap: number): T[] {
  return items.slice(Math.max(0, items.length - cap));
}

export function selectDiagnostics(
  events: readonly DiagnosticEvent[],
  now: number,
  options: SelectDiagnosticsOptions,
): DiagnosticsSelection {
  if (!options.includeInReports) {
    return { breadcrumbs: [], networkFailures: [], error: null };
  }

  const maxBreadcrumbs = options.maxBreadcrumbs ?? MAX_REPORT_BREADCRUMBS;
  const maxNetworkFailures = options.maxNetworkFailures ?? MAX_REPORT_NETWORK_FAILURES;

  const fresh = events.filter((event) => withinAge(event, now, options.maxAgeMs));

  const breadcrumbEvents: DiagnosticEvent[] = [];
  const networkFailureEvents: NetworkDiagnosticEvent[] = [];

  let latestError: ErrorDiagnosticEvent | null = null;

  for (const event of fresh) {
    if (isErrorEvent(event)) {
      latestError = event;
      breadcrumbEvents.push(event);
      continue;
    }
    if (event.kind === 'network') {
      if (event.data.category === 'opaque_response') {
        breadcrumbEvents.push(event);
      } else {
        networkFailureEvents.push(event);
      }
      continue;
    }
    breadcrumbEvents.push(event);
  }

  return {
    breadcrumbs: tail(breadcrumbEvents, maxBreadcrumbs).map(toBreadcrumb),
    networkFailures: tail(networkFailureEvents, maxNetworkFailures).map(toNetworkFailure),
    error: latestError !== null ? toReportError(latestError) : null,
  };
}
