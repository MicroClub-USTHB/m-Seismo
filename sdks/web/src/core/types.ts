/**
 * Report payload types.
 *
 * The shape below is the SDK's implementation of the **ASSUMED** draft contract
 * in `docs/backend-contract.md`. It has NOT been verified against the real Go
 * backend; field names and nesting may change.
 */
import type {
  DiagnosticSource,
  JsonValue,
  NetworkCategory,
} from '../diagnostics/types';

/**
 * Schema version carried on every report. Bumped only on breaking changes.
 *
 * Phase 7 bumps `1` → `2`: the payload gained `type`, `trigger`, an optional
 * `error.fingerprint`/`error.occurrences`, and `description` became nullable for
 * automatic/captured reports. See `docs/report.schema.json`.
 */
export const SCHEMA_VERSION = 2;

export type SchemaVersion = typeof SCHEMA_VERSION;

/**
 * How a report came to exist (Phase 7).
 *
 * - `manual`              — user submitted the form, no error attached
 * - `manual_with_error`   — user submitted the form with a recent error
 * - `automatic_error`     — SDK reported an uncaught error / unhandled rejection
 * - `captured_exception`  — host called `captureException()`
 * - `captured_message`    — host called `captureMessage()`
 */
export type ReportType =
  | 'manual'
  | 'manual_with_error'
  | 'automatic_error'
  | 'captured_exception'
  | 'captured_message';

/** What caused the report to be produced (Phase 7). */
export type ReportTrigger = 'form' | 'automatic' | 'api';

/**
 * The most recent error attached to a report. `type`/`message` are required;
 * every other field is optional and additive over the Phase 5.1 shape.
 *
 * Phase 7 adds `fingerprint` (stable identity of this class of error) and
 * `occurrences` (how many times this fingerprint occurred since the last report
 * was sent for it). Both are present only for reports that carry an error.
 */
export interface ReportError {
  readonly type: string;
  readonly message: string;
  readonly stack?: string;
  /** `false` for automatic capture, `true` for `captureException()`. */
  readonly handled?: boolean;
  /** ISO 8601 timestamp of when the error was captured. */
  readonly timestamp?: string;
  readonly source?: DiagnosticSource;
  readonly flags?: readonly string[];
  readonly context?: Record<string, JsonValue>;
  /** Stable identity of this error class (Phase 7). Lowercase hex, bounded. */
  readonly fingerprint?: string;
  /** Times this fingerprint occurred since the last report sent for it (≥ 1). */
  readonly occurrences?: number;
}

export interface ReportApplication {
  readonly version?: string;
  readonly environment?: string;
}

/** Detected browser name/version. Both fields are optional and may be omitted. */
export interface ReportBrowser {
  readonly name?: string;
  readonly version?: string;
}

/** Viewport size in integer pixels. */
export interface ReportViewport {
  readonly width: number;
  readonly height: number;
}

/**
 * Automatic context captured with every report (Phase 5.1).
 *
 * Every field except `timestamp` and `application` is optional and is omitted
 * when it cannot be collected. `url` is already sanitized (fragment, userinfo
 * and sensitive query values removed) before it reaches this type.
 */
export interface ReportContext {
  /** Sanitized page URL. Omitted when there is no `location` or it is unusable. */
  readonly url?: string;
  /** ISO 8601 timestamp, UTC. Always present. */
  readonly timestamp: string;
  /** Detected browser, or omitted when it cannot be determined. */
  readonly browser?: ReportBrowser;
  /** Normalized platform/OS, or omitted when it cannot be determined. */
  readonly platform?: string;
  /** Viewport in integer pixels, or omitted when unavailable. */
  readonly viewport?: ReportViewport;
  readonly application: ReportApplication;
}

/**
 * A user-supplied screenshot, embedded inline as base64.
 *
 * **ASSUMED** shape (see `docs/backend-contract.md`): inline base64 in the JSON
 * body, not multipart or a separate upload. `dataBase64` carries no data-URL
 * prefix (it is the raw base64 of the file bytes).
 */
export interface ScreenshotAttachment {
  /** Original file name, e.g. `screen.png`. */
  readonly name: string;
  /** IANA media type; one of `image/png`, `image/jpeg`, `image/webp`. */
  readonly mimeType: string;
  /** Size of the decoded image in bytes. */
  readonly sizeBytes: number;
  /** Base64-encoded file bytes (no `data:` prefix). */
  readonly dataBase64: string;
}

/** Category of a report breadcrumb (mirrors the diagnostic `kind`). */
export type ReportBreadcrumbCategory =
  | 'error'
  | 'unhandledrejection'
  | 'captured'
  | 'navigation'
  | 'interaction'
  | 'sdk';

/**
 * A single breadcrumb attached to a report (Phase 5.3). Older-first order.
 * `level` is `'error'` for error-like breadcrumbs and `'info'` otherwise.
 */
export interface ReportBreadcrumb {
  readonly timestamp: string;
  readonly category: ReportBreadcrumbCategory;
  readonly message: string;
  readonly level?: 'error' | 'info';
  readonly data?: Record<string, JsonValue>;
}

/** A failed (or opaque) network call attached to a report (Phase 5.3). */
export interface ReportNetworkFailure {
  readonly timestamp: string;
  readonly method: string;
  readonly url: string;
  readonly status?: number;
  readonly durationMs: number;
  readonly category: NetworkCategory;
}

export interface ReportPayload {
  readonly schemaVersion: SchemaVersion;
  readonly sdkVersion: string;
  readonly reportId: string;
  /** How this report was produced (Phase 7). */
  readonly type: ReportType;
  /** What caused this report to be produced (Phase 7). */
  readonly trigger: ReportTrigger;
  /**
   * User description. A non-empty string is required for `manual` /
   * `manual_with_error`; `null` is used for automatic and captured reports.
   */
  readonly description: string | null;
  /** `null` is valid for `manual`, and for any report with no error attached. */
  readonly error: ReportError | null;
  readonly context: ReportContext;
  /** Recent breadcrumbs, oldest first (empty when diagnostics are disabled). */
  readonly breadcrumbs: readonly ReportBreadcrumb[];
  /** Recent network failures, oldest first (empty when disabled). */
  readonly networkFailures: readonly ReportNetworkFailure[];
  /** Optional screenshot attachment, or `null` when none was provided. */
  readonly screenshot: ScreenshotAttachment | null;
}
