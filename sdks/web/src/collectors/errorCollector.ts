/**
 * Uncaught runtime error collector (Phase 5.2).
 *
 * Registers ONE bubble-phase `error` listener on the host window via
 * `addEventListener` — never `window.onerror`, never replacing or wrapping a
 * host handler, and never in the capture phase. As a result it observes only
 * genuine runtime script errors (whose target is the window); resource load
 * failures (`<img>`, `<script src>`, `<link>`) neither bubble nor are listened
 * for in the capture phase, so they are not captured. They are additionally
 * rejected defensively by the target/message checks below.
 *
 * The handler never calls `preventDefault()`/`stopPropagation()`, never
 * rethrows, and never changes console output: host behaviour is identical with
 * and without the SDK.
 */
import { normalizeError } from '../errors/normalize';
import { createDiagnosticEvent } from '../diagnostics/event';
import { isDomNode } from '../diagnostics/jsonSafe';
import {
  FLAG_CROSS_ORIGIN_SCRIPT_ERROR,
  FLAG_STACK_MISSING,
} from '../diagnostics/types';
import type { DiagnosticError, DiagnosticEvent, DiagnosticSource } from '../diagnostics/types';

/** Narrow structural view of the DOM `ErrorEvent` we consume. */
interface ErrorEventLike {
  readonly message?: unknown;
  readonly filename?: unknown;
  readonly lineno?: unknown;
  readonly colno?: unknown;
  readonly error?: unknown;
  readonly target?: unknown;
}

/** Cross-origin scripts without CORS surface as this bare message. */
const SCRIPT_ERROR_PATTERN = /^script error\.?$/i;

function addFlag(flags: string[], flag: string): void {
  if (!flags.includes(flag)) flags.push(flag);
}

function toPositiveInt(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? Math.trunc(value)
    : undefined;
}

function sourceFrom(event: ErrorEventLike): DiagnosticSource | undefined {
  const filename =
    typeof event.filename === 'string' && event.filename.length > 0
      ? event.filename
      : undefined;
  const line = toPositiveInt(event.lineno);
  const column = toPositiveInt(event.colno);
  if (filename === undefined && line === undefined && column === undefined) {
    return undefined;
  }
  return {
    ...(filename !== undefined ? { filename } : {}),
    ...(line !== undefined ? { line } : {}),
    ...(column !== undefined ? { column } : {}),
  };
}

/**
 * True for a genuine runtime script error. Resource-load errors are rejected:
 * their `target` is an element (not the window) and they carry no `message`
 * and no `error` property.
 */
function isRuntimeError(event: ErrorEventLike): boolean {
  const target = event.target;
  // A DOM element target means a resource load failure (`<img>`, `<script src>`).
  if (isDomNode(target)) return false;
  const hasMessage = typeof event.message === 'string';
  const hasError = 'error' in (event as object);
  return hasMessage || hasError;
}

function handleError(event: ErrorEventLike): DiagnosticEvent | null {
  if (!isRuntimeError(event)) return null;

  const flags: string[] = [];
  const message = typeof event.message === 'string' ? event.message : undefined;
  const rawError = event.error;

  let error: DiagnosticError;
  if (rawError !== undefined && rawError !== null) {
    const normalized = normalizeError(rawError);
    for (const flag of normalized.flags) addFlag(flags, flag);
    error = normalized.error;
    if (error.message.length === 0 && message !== undefined) {
      error = { ...error, message };
    }
  } else {
    error = { type: 'string', message: message ?? '' };
    addFlag(flags, FLAG_STACK_MISSING);
  }

  const source = sourceFrom(event);
  if (source !== undefined) error = { ...error, source };

  if (SCRIPT_ERROR_PATTERN.test(error.message.trim())) {
    addFlag(flags, FLAG_CROSS_ORIGIN_SCRIPT_ERROR);
  }

  return createDiagnosticEvent({ kind: 'error', error, handled: false, flags });
}

/**
 * Installs the collector. Returns a disposer that removes the listener.
 * The handler is fully wrapped so it can never throw into the host.
 */
export function installErrorCollector(
  host: EventTarget,
  record: (event: DiagnosticEvent) => void,
): () => void {
  const listener = (event: Event): void => {
    try {
      const diagnostic = handleError(event as unknown as ErrorEventLike);
      if (diagnostic !== null) record(diagnostic);
    } catch {
      // Never propagate; never record a failure of our own collector.
    }
  };
  host.addEventListener('error', listener);
  return () => host.removeEventListener('error', listener);
}
