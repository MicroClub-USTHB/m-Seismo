/**
 * Diagnostic event schema and shared constants (Phase 5.2).
 *
 * One event shape is used for every captured problem so a later phase (5.3) can
 * add new `kind`s and attach these events to reports without reshaping them.
 *
 * This module is types + constants only; it has no behaviour and is SSR-safe.
 */

/** A JSON-serializable value. Context is constrained to this so it can be
 * serialized into a report later without surprises. */
export type JsonValue =
  | string
  | number
  | boolean
  | null
  | JsonValue[]
  | { [key: string]: JsonValue };

/**
 * The kind of diagnostic. Extensible in a later phase:
 * - `error`               — an uncaught runtime error (window `error` event)
 * - `unhandledrejection`  — an unhandled promise rejection
 * - `captured`            — reported explicitly via `captureException()`
 */
export type DiagnosticKind = 'error' | 'unhandledrejection' | 'captured';

/** One level of `Error.cause`. Deeper `cause` chains are not followed. */
export interface DiagnosticCause {
  readonly type: string;
  readonly message: string;
}

/** Source location, when the browser provides it. */
export interface DiagnosticSource {
  readonly filename?: string;
  readonly line?: number;
  readonly column?: number;
}

/** The normalized error shape shared by all three capture paths. */
export interface DiagnosticError {
  /** `'TypeError'`, `'DOMException'`, `'string'`, `'object'`, `'null'`, … */
  readonly type: string;
  readonly message: string;
  readonly stack?: string;
  readonly cause?: DiagnosticCause;
  readonly source?: DiagnosticSource;
}

/** A single captured diagnostic event. */
export interface DiagnosticEvent {
  /** Unique per event. */
  readonly id: string;
  readonly kind: DiagnosticKind;
  /** ISO 8601 timestamp, UTC. */
  readonly timestamp: string;
  readonly error: DiagnosticError;
  /** `false` for automatic capture, `true` for `captureException()`. */
  readonly handled: boolean;
  /** Extra fields from `captureException()` only; structurally sanitized. */
  readonly context?: Record<string, JsonValue>;
  /** Machine-readable markers, e.g. `'cross-origin-script-error'`. */
  readonly flags?: readonly string[];
}

// --- Flags ------------------------------------------------------------------

/** The browser hid the error details (cross-origin script without CORS). */
export const FLAG_CROSS_ORIGIN_SCRIPT_ERROR = 'cross-origin-script-error';
/** No usable stack was available. */
export const FLAG_STACK_MISSING = 'stack-missing';
/** At least one string field was truncated to its limit. */
export const FLAG_TRUNCATED = 'truncated';
/** A rejection reason that is not `Error`-like. */
export const FLAG_NON_ERROR_REJECTION = 'non-error-rejection';

// --- Limits -----------------------------------------------------------------

/** Max length of a normalized error message. */
export const MAX_MESSAGE_LENGTH = 1000;
/** Max length of a normalized stack. */
export const MAX_STACK_LENGTH = 8000;
/** Max length of a single sanitized context string value. */
export const CONTEXT_MAX_STRING_LENGTH = 500;
/** Max keys kept in a context object (per object level). */
export const CONTEXT_MAX_KEYS = 20;
/** Max nesting depth for context values. */
export const CONTEXT_MAX_DEPTH = 3;
/** Marker appended to any truncated string. */
export const TRUNCATION_MARKER = '…[truncated]';

// --- Buffer -----------------------------------------------------------------

/** Default number of events kept in the ring buffer. */
export const DEFAULT_MAX_EVENTS = 50;
/** Hard upper bound; larger configured values are clamped to this. */
export const MAX_EVENTS_CEILING = 200;
