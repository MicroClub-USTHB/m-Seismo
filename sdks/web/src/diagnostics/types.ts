/**
 * Diagnostic event schema and shared constants (Phase 5.2, extended Phase 5.3).
 *
 * One event shape is used for every captured problem so Phase 5.3 can add new
 * `kind`s (navigation, interaction, network, sdk) and attach these events to
 * reports without reshaping them. {@link DiagnosticEvent} is a discriminated
 * union on `kind`:
 *
 * - error-like kinds (`error`, `unhandledrejection`, `captured`) carry an
 *   {@link DiagnosticError} plus `handled`/`context`/`flags`;
 * - the behavioural kinds (`navigation`, `interaction`, `network`, `sdk`) carry
 *   a typed `data` payload and no error.
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

/** Kind of an error-like diagnostic event. */
export type ErrorDiagnosticKind = 'error' | 'unhandledrejection' | 'captured';

/**
 * The kind of diagnostic.
 * - `error`               — an uncaught runtime error (window `error` event)
 * - `unhandledrejection`  — an unhandled promise rejection
 * - `captured`            — reported explicitly via `captureException()`
 * - `navigation`          — a route change (Phase 5.3)
 * - `interaction`         — an allow-listed click/submit (Phase 5.3)
 * - `network`             — a failed (or opaque) `fetch` (Phase 5.3)
 * - `sdk`                 — an SDK lifecycle breadcrumb, e.g. form opened
 */
export type DiagnosticKind =
  | ErrorDiagnosticKind
  | 'navigation'
  | 'interaction'
  | 'network'
  | 'sdk';

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

// --- Behavioural payloads (Phase 5.3) --------------------------------------

/** How a navigation was detected. */
export type NavigationTrigger =
  | 'pushState'
  | 'replaceState'
  | 'popstate'
  | 'hashchange'
  | 'load';

/** Navigation breadcrumb payload. `from` is omitted for the initial `load`. */
export interface NavigationData {
  readonly from?: string;
  readonly to: string;
  readonly trigger: NavigationTrigger;
}

/** Which user action produced an interaction breadcrumb. */
export type InteractionAction = 'click' | 'submit';

/**
 * Allow-listed, low-cardinality target description. Never carries input values,
 * link destinations, or (unless `captureElementText` is enabled) visible text.
 */
export interface InteractionTarget {
  readonly tag: string;
  readonly id?: string;
  readonly role?: string;
  readonly testId?: string;
  readonly type?: string;
  readonly name?: string;
  readonly label?: string;
}

/** Interaction breadcrumb payload. */
export interface InteractionData {
  readonly action: InteractionAction;
  readonly target: InteractionTarget;
}

/**
 * Why a `fetch` produced a diagnostic.
 * - `http_error`      — response received with status ≥ 400
 * - `network_error`   — the promise rejected with a `TypeError` (DNS/CORS/offline)
 * - `aborted`         — the promise rejected with an `AbortError`
 * - `other_error`     — any other rejection
 * - `opaque_response` — a response with status 0 and `type: 'opaque'` (NOT a failure)
 */
export type NetworkCategory =
  | 'http_error'
  | 'network_error'
  | 'aborted'
  | 'other_error'
  | 'opaque_response';

/** Network breadcrumb payload. Bodies, headers, cookies and credentials are never read. */
export interface NetworkData {
  readonly method: string;
  readonly url: string;
  readonly status?: number;
  readonly durationMs: number;
  readonly category: NetworkCategory;
  readonly aborted?: boolean;
}

/** SDK lifecycle breadcrumb payload, e.g. `{ name: 'report-form-opened' }`. */
export interface SdkData {
  readonly name: string;
}

// --- Events ----------------------------------------------------------------

interface DiagnosticEventBase {
  /** Unique per event. */
  readonly id: string;
  /** ISO 8601 timestamp, UTC. */
  readonly timestamp: string;
  /** Machine-readable markers, e.g. `'truncated'`. */
  readonly flags?: readonly string[];
}

/** An uncaught error, unhandled rejection, or explicitly captured exception. */
export interface ErrorDiagnosticEvent extends DiagnosticEventBase {
  readonly kind: ErrorDiagnosticKind;
  readonly error: DiagnosticError;
  /** `false` for automatic capture, `true` for `captureException()`. */
  readonly handled: boolean;
  /** Extra fields from `captureException()` only; structurally sanitized. */
  readonly context?: Record<string, JsonValue>;
}

/** A navigation breadcrumb. */
export interface NavigationDiagnosticEvent extends DiagnosticEventBase {
  readonly kind: 'navigation';
  readonly data: NavigationData;
}

/** An interaction breadcrumb. */
export interface InteractionDiagnosticEvent extends DiagnosticEventBase {
  readonly kind: 'interaction';
  readonly data: InteractionData;
}

/** A network breadcrumb. */
export interface NetworkDiagnosticEvent extends DiagnosticEventBase {
  readonly kind: 'network';
  readonly data: NetworkData;
}

/** An SDK lifecycle breadcrumb. */
export interface SdkDiagnosticEvent extends DiagnosticEventBase {
  readonly kind: 'sdk';
  readonly data: SdkData;
}

/** Any captured diagnostic event. */
export type DiagnosticEvent =
  | ErrorDiagnosticEvent
  | NavigationDiagnosticEvent
  | InteractionDiagnosticEvent
  | NetworkDiagnosticEvent
  | SdkDiagnosticEvent;

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

/**
 * Default number of events kept in the ring buffer. Raised from 50 in Phase 5.2
 * to 100 in Phase 5.3 to make room for navigation/interaction/network/sdk
 * breadcrumbs sharing one buffer.
 */
export const DEFAULT_MAX_EVENTS = 100;
/** Hard upper bound; larger configured values are clamped to this. */
export const MAX_EVENTS_CEILING = 200;

/**
 * Default maximum age of a buffered event, in milliseconds (10 minutes). Events
 * older than this are pruned lazily on insert and excluded at report time.
 */
export const DEFAULT_MAX_AGE_MS = 10 * 60 * 1000;
/** Lower clamp for `diagnostics.maxAgeMs` (10 seconds). */
export const MIN_MAX_AGE_MS = 10 * 1000;
/** Upper clamp for `diagnostics.maxAgeMs` (1 hour). */
export const MAX_MAX_AGE_MS = 60 * 60 * 1000;

/**
 * Soft cap for a single serialized event, in bytes (~2 KB). Events larger than
 * this are trimmed field-by-field and flagged `truncated`.
 */
export const EVENT_MAX_BYTES = 2048;
/** Max length of a sanitized URL inside a navigation/network breadcrumb. */
export const BREADCRUMB_URL_MAX_LENGTH = 1024;
/** Max length of an interaction element label when `captureElementText` is on. */
export const INTERACTION_LABEL_MAX_LENGTH = 40;

// --- Report attachment ------------------------------------------------------

/** Most recent breadcrumbs attached to a report. */
export const MAX_REPORT_BREADCRUMBS = 50;
/** Most recent network failures attached to a report. */
export const MAX_REPORT_NETWORK_FAILURES = 20;

// --- Interactions -----------------------------------------------------------

/** Identical consecutive clicks within this window are collapsed to one. */
export const INTERACTION_THROTTLE_MS = 300;
