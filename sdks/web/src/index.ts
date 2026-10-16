/**
 * Public API surface of the m-Seismo Web SDK.
 *
 * Phase 2 adds the pieces needed to talk to a backend: config/`init`, a minimal
 * report builder, the `sanitize()` privacy boundary, and the HTTP transport.
 * `openReportForm`, `captureException`, and `captureMessage` remain stubs.
 *
 * The whole module is SSR-safe: importing it touches no `window`/`document`.
 *
 * Anything backend-shaped is **ASSUMED** — see `docs/backend-contract.md`.
 */
import { configure } from './core/config';
import type { InitOptions } from './core/config';

export { SDK_VERSION } from './core/version';
export { SCHEMA_VERSION, DEFAULT_TIMEOUT_MS, getConfig, isEnabled, buildReport, createReportId } from './core/index';
export type {
  SchemaVersion,
  ReportError,
  ReportApplication,
  ReportContext,
  ReportPayload,
  InitOptions,
  SdkConfig,
  BuildReportInput,
  BuildReportOptions,
} from './core/index';

export { sanitize } from './privacy/index';
export type { SanitizedReport } from './privacy/index';

export { sendReport } from './transport/index';
export type { SendFailure, SendFailureKind, SendResult, SendSuccess } from './transport/index';

export interface CaptureContext {
  /** Arbitrary structured context attached to a report. */
  readonly [key: string]: unknown;
}

/**
 * Initialise the SDK. Validates and stores config. Invalid config logs one
 * warning, does not throw, and leaves the SDK disabled.
 *
 * @returns `true` when the config was accepted.
 */
export function init(options: InitOptions): boolean {
  return configure(options);
}

/**
 * Open the bug-reporting form. Phase 2: no-op stub.
 */
export function openReportForm(_context?: CaptureContext): void {
  // no-op until the form phase
}

/**
 * Capture an exception. Phase 2: no-op stub.
 */
export function captureException(
  _error: unknown,
  _context?: CaptureContext,
): void {
  // no-op until the collectors phase
}

/**
 * Capture a free-form message. Phase 2: no-op stub.
 */
export function captureMessage(
  _message: string,
  _context?: CaptureContext,
): void {
  // no-op until the collectors phase
}
