/**
 * Unified report processor (Phase 7).
 *
 * Every report the SDK sends flows through {@link processReport}: the manual
 * form, `captureException()`, `captureMessage()`, and the automatic
 * uncaught-error/rejection path. Nothing else assembles a final payload.
 *
 * The pipeline, in order:
 *   1. collect   — fresh context + the diagnostics selection (injected);
 *   2. classify  — pick the {@link ReportType} from the source + inputs;
 *   3. normalize — one {@link ReportError} shape for every error path;
 *   4. fingerprint + dedup — a stable identity, plus automatic rate limiting;
 *   5. sanitize  — the ONLY route to the transport (BASELINE scrubbing);
 *   6. beforeSend — optional last-chance hook (frozen copy, may drop);
 *   7. size guard — drop oversized optional parts;
 *   8. send      — the transport (never throws).
 *
 * It is a pure function with every dependency injected (clock, id generator,
 * config accessor, context/diagnostics collectors, transport, limiter, logger),
 * so it can be unit-tested without a browser or a network and never reads
 * globals directly.
 */
import type { SdkConfig } from '../core/config';
import { buildReport, createReportId } from '../core/reportBuilder';
import type {
  ReportBreadcrumb,
  ReportContext,
  ReportError,
  ReportNetworkFailure,
  ReportPayload,
  ReportTrigger,
  ReportType,
  ScreenshotAttachment,
} from '../core/types';
import { normalizeError } from '../errors/normalize';
import { sanitize } from '../privacy/sanitize';
import type { SanitizedReport } from '../privacy/sanitize';
import { validateDescription } from '../form/validation';
import { FLAG_CROSS_ORIGIN_SCRIPT_ERROR } from '../diagnostics/types';
import type { DiagnosticError, ErrorDiagnosticEvent } from '../diagnostics/types';
import type { SendFailureKind, SendResult } from '../transport/types';
import type { DiagnosticsSelection } from '../diagnostics/select';
import { computeFingerprint, FLAG_WEAK_FINGERPRINT } from './fingerprint';
import type { ReportLimiter } from './limiter';

/** Why a report was suppressed (never sent). */
export type SuppressReason =
  | 'duplicate'
  | 'rate_limited'
  | 'disabled'
  | 'invalid_input';

export interface SentResult {
  readonly status: 'sent';
  readonly reportId: string;
  readonly type: ReportType;
  readonly trigger: ReportTrigger;
  readonly httpStatus: number;
  readonly id?: string;
  readonly fingerprint?: string;
  readonly occurrences?: number;
}

export interface SuppressedResult {
  readonly status: 'suppressed';
  readonly reportId: string;
  readonly type: ReportType;
  readonly trigger: ReportTrigger;
  readonly reason: SuppressReason;
  readonly fingerprint?: string;
  readonly occurrences?: number;
}

export interface FailedResult {
  readonly status: 'failed';
  readonly reportId: string;
  readonly type: ReportType;
  readonly trigger: ReportTrigger;
  readonly kind: SendFailureKind;
  readonly httpStatus?: number;
  readonly message: string;
  readonly fingerprint?: string;
  readonly occurrences?: number;
}

export type ProcessResult = SentResult | SuppressedResult | FailedResult;

export interface ProcessReportInput {
  /** Which entry point produced this report. */
  readonly source: ReportTrigger;
  /** Form description (required for `form`), or an optional developer note. */
  readonly description?: string | null;
  /** Reuse an id (form retries); a new one is generated when omitted. */
  readonly reportId?: string;
  /** Screenshot attached from the manual form. */
  readonly screenshot?: ScreenshotAttachment | null;
  /**
   * The normalized diagnostic event for `automatic` reports, and for
   * `api` reports produced by `captureException()` / `captureMessage()`.
   */
  readonly event?: ErrorDiagnosticEvent;
  /** Raw value for a directly-supplied `captureException()` error (fallback). */
  readonly error?: unknown;
  /** Present for `captureMessage()`; selects `captured_message`. */
  readonly message?: string;
}

export interface ReportProcessorDeps {
  readonly now: () => number;
  readonly createId: () => string;
  readonly getConfig: () => SdkConfig | null;
  readonly collectContext: (reportId: string, config: SdkConfig) => ReportContext;
  readonly selectDiagnostics: (
    now: number,
    config: SdkConfig,
  ) => DiagnosticsSelection;
  readonly send: (
    report: SanitizedReport,
    config: SdkConfig,
  ) => Promise<SendResult>;
  readonly limiter: ReportLimiter;
  readonly warn: (message: string) => void;
}

/** Max serialized report size before optional parts are dropped (~5 MB). */
export const MAX_REPORT_BYTES = 5 * 1024 * 1024;

function flagsInclude(event: ErrorDiagnosticEvent, flag: string): boolean {
  return event.flags !== undefined && event.flags.includes(flag);
}

/** Builds a {@link ReportError} from a normalized diagnostic event. */
function reportErrorFromEvent(event: ErrorDiagnosticEvent): ReportError {
  const error: DiagnosticError = event.error;
  return {
    type: error.type,
    message: error.message,
    ...(error.stack !== undefined ? { stack: error.stack } : {}),
    handled: event.handled,
    timestamp: event.timestamp,
    ...(error.source !== undefined ? { source: error.source } : {}),
    ...(event.flags !== undefined ? { flags: [...event.flags] } : {}),
    ...(event.context !== undefined ? { context: event.context } : {}),
  };
}

/** Builds a {@link ReportError} from a raw value via {@link normalizeError}. */
function reportErrorFromValue(value: unknown): ReportError {
  const normalized = normalizeError(value);
  const error = normalized.error;
  return {
    type: error.type,
    message: error.message,
    ...(error.stack !== undefined ? { stack: error.stack } : {}),
    handled: true,
    timestamp: new Date().toISOString(),
    ...(error.source !== undefined ? { source: error.source } : {}),
    ...(normalized.flags.length > 0 ? { flags: normalized.flags } : {}),
  };
}

/** Best-effort `pathname` for a sanitized page URL; `''` when unusable. */
function pathFromUrl(url: string | undefined): string {
  if (url === undefined || url.length === 0) return '';
  try {
    return new URL(url).pathname;
  } catch {
    const noScheme = url.replace(/^[a-z]+:\/\/[^/]*/i, '');
    const path = noScheme.split(/[?#]/)[0] ?? '';
    return path.startsWith('/') ? path : '';
  }
}

function cloneJson<T>(value: T): T {
  try {
    if (typeof structuredClone === 'function') return structuredClone(value);
  } catch {
    // fall through
  }
  try {
    return JSON.parse(JSON.stringify(value)) as T;
  } catch {
    return value;
  }
}

function deepFreeze<T>(value: T): T {
  if (value !== null && typeof value === 'object') {
    for (const key of Object.keys(value as Record<string, unknown>)) {
      deepFreeze((value as Record<string, unknown>)[key]);
    }
    Object.freeze(value);
  }
  return value;
}

/**
 * Drops optional parts until the serialized report fits `MAX_REPORT_BYTES`.
 * The screenshot goes first, then breadcrumbs/network failures. Required fields
 * are never dropped.
 */
function enforceSizeLimit(report: ReportPayload): ReportPayload {
  const size = (value: ReportPayload): number => {
    try {
      return JSON.stringify(value).length;
    } catch {
      return 0;
    }
  };
  if (size(report) <= MAX_REPORT_BYTES) return report;
  let next: ReportPayload = { ...report, screenshot: null };
  if (size(next) <= MAX_REPORT_BYTES) return next;
  next = { ...next, breadcrumbs: [], networkFailures: [] };
  return next;
}

function withFingerprint(
  error: ReportError,
  fingerprint: string,
  weak: boolean,
  occurrences: number,
): ReportError {
  const flags = error.flags !== undefined ? [...error.flags] : [];
  if (weak && !flags.includes(FLAG_WEAK_FINGERPRINT)) flags.push(FLAG_WEAK_FINGERPRINT);
  return {
    ...error,
    fingerprint,
    occurrences,
    ...(flags.length > 0 ? { flags } : {}),
  };
}

/** The report source resolves to, for the suppressed/invalid paths. */
function baseTypeFor(source: ReportTrigger): ReportType {
  if (source === 'form') return 'manual';
  if (source === 'automatic') return 'automatic_error';
  return 'captured_exception';
}

export async function processReport(
  input: ProcessReportInput,
  deps: ReportProcessorDeps,
): Promise<ProcessResult> {
  const reportId = input.reportId ?? deps.createId();
  const trigger = input.source;

  const config = deps.getConfig();
  if (config === null) {
    return { status: 'suppressed', reportId, type: baseTypeFor(trigger), trigger, reason: 'disabled' };
  }

  const now = deps.now();
  const isoNow = new Date(now).toISOString();
  const context = deps.collectContext(reportId, config);
  let type: ReportType;
  let description: string | null = null;
  let error: ReportError | null = null;
  let breadcrumbs: readonly ReportBreadcrumb[] = [];
  let networkFailures: readonly ReportNetworkFailure[] = [];

  // --- 1. collect + 2. classify + 3. normalize ------------------------------
  if (input.source === 'form') {
    const validation = validateDescription(
      typeof input.description === 'string' ? input.description : '',
    );
    if (!validation.ok) {
      return { status: 'suppressed', reportId, type: 'manual', trigger, reason: 'invalid_input' };
    }
    description = validation.value;
    const selection = deps.selectDiagnostics(now, config);
    breadcrumbs = selection.breadcrumbs;
    networkFailures = selection.networkFailures;
    error = selection.error;
    type = error !== null ? 'manual_with_error' : 'manual';
  } else if (input.source === 'automatic') {
    if (input.event === undefined) {
      return { status: 'suppressed', reportId, type: 'automatic_error', trigger, reason: 'invalid_input' };
    }
    const selection = deps.selectDiagnostics(now, config);
    breadcrumbs = selection.breadcrumbs;
    networkFailures = selection.networkFailures;
    error = reportErrorFromEvent(input.event);
    type = 'automatic_error';
  } else {
    const selection = deps.selectDiagnostics(now, config);
    breadcrumbs = selection.breadcrumbs;
    networkFailures = selection.networkFailures;
    if (input.message !== undefined) {
      error = { type: 'Message', message: input.message, handled: true, timestamp: isoNow };
      type = 'captured_message';
    } else if (input.event !== undefined) {
      error = reportErrorFromEvent(input.event);
      type = 'captured_exception';
    } else {
      error = { ...reportErrorFromValue(input.error), timestamp: isoNow };
      type = 'captured_exception';
    }
    description = typeof input.description === 'string' ? input.description : null;
  }

  // --- 4. fingerprint + dedup ----------------------------------------------
  let fingerprint: string | undefined;
  let occurrences: number | undefined;

  if (error !== null) {
    const crossOrigin =
      error.flags !== undefined && error.flags.includes(FLAG_CROSS_ORIGIN_SCRIPT_ERROR);
    const pagePath = pathFromUrl(context.url);
    const fp = computeFingerprint({
      type: error.type,
      message: error.message,
      ...(error.stack !== undefined ? { stack: error.stack } : {}),
      crossOrigin,
      pagePath,
    });
    fingerprint = fp.fingerprint;

    if (type === 'manual' || type === 'manual_with_error') {
      // Manual reports are NEVER deduplicated; just report the current count.
      occurrences = deps.limiter.occurrencesFor(fp.fingerprint);
    } else {
      const decision = deps.limiter.decide(fp.fingerprint, now);
      if (decision.action === 'duplicate') {
        return { status: 'suppressed', reportId, type, trigger, reason: 'duplicate', fingerprint, occurrences: decision.occurrences };
      }
      if (decision.action === 'rate_limited') {
        return { status: 'suppressed', reportId, type, trigger, reason: 'rate_limited', fingerprint, occurrences: decision.occurrences };
      }
      occurrences = decision.occurrences;
    }

    error = withFingerprint(error, fp.fingerprint, fp.weak, occurrences);
  }

  // --- assemble -------------------------------------------------------------
  const payload = buildReport(
    {
      type,
      trigger,
      description,
      context,
      error,
      breadcrumbs,
      networkFailures,
      screenshot: input.screenshot ?? null,
    },
    { reportId },
  );

  // --- 5. sanitize ----------------------------------------------------------
  let report: SanitizedReport = sanitize(payload);

  // --- 6. beforeSend --------------------------------------------------------
  const hook = config.reporting.beforeSend;
  if (hook !== undefined) {
    const frozen = deepFreeze(cloneJson(report));
    let hookResult: ReportPayload | null;
    try {
      hookResult = hook(frozen);
    } catch {
      deps.warn(
        'reporting.beforeSend threw; sending the report unmodified.',
      );
      hookResult = frozen;
    }
    if (hookResult === null) {
      return { status: 'suppressed', reportId, type, trigger, reason: 'disabled', ...(fingerprint !== undefined ? { fingerprint } : {}), ...(occurrences !== undefined ? { occurrences } : {}) };
    }
    // The hook's return value is intentionally NOT re-sanitized.
    report = hookResult as SanitizedReport;
  }

  // --- 7. size guard --------------------------------------------------------
  report = enforceSizeLimit(report) as SanitizedReport;

  // --- 8. send --------------------------------------------------------------
  let result: SendResult;
  try {
    result = await deps.send(report, config);
  } catch (error_) {
    return {
      status: 'failed',
      reportId,
      type,
      trigger,
      kind: 'network',
      message: error_ instanceof Error ? error_.message : String(error_),
      ...(fingerprint !== undefined ? { fingerprint } : {}),
      ...(occurrences !== undefined ? { occurrences } : {}),
    };
  }

  if (!result.ok) {
    return {
      status: 'failed',
      reportId,
      type,
      trigger,
      kind: result.kind,
      ...(result.status !== undefined ? { httpStatus: result.status } : {}),
      message: result.message,
      ...(fingerprint !== undefined ? { fingerprint } : {}),
      ...(occurrences !== undefined ? { occurrences } : {}),
    };
  }

  return {
    status: 'sent',
    reportId,
    type,
    trigger,
    httpStatus: result.status,
    ...(result.id !== undefined ? { id: result.id } : {}),
    ...(fingerprint !== undefined ? { fingerprint } : {}),
    ...(occurrences !== undefined ? { occurrences } : {}),
  };
}

/** Default id generator, exported so the runtime and tests share one. */
export const generateReportId = createReportId;
