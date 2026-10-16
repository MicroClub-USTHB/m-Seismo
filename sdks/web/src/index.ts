/**
 * Public API surface of the m-Seismo Web SDK.
 *
 * Phase 3 adds the built-in bug-reporting form: a shadow-DOM dialog opened by a
 * keyboard shortcut, a floating launcher button, or `openReportForm()`. The form
 * collects a description plus an optional screenshot, builds a report, passes it
 * through the privacy boundary, and sends it with the Phase 2 transport.
 *
 * The module is SSR-safe: importing it touches no `window`/`document`, and DOM
 * work only happens inside `init()`/`openReportForm()` when a browser exists.
 *
 * Anything backend-shaped is **ASSUMED** — see `docs/backend-contract.md`.
 */
import { initSdk, openForm, destroySdk } from './core/sdk';
import type { InitOptions } from './core/config';

export { SDK_VERSION } from './core/version';
export {
  SCHEMA_VERSION,
  DEFAULT_TIMEOUT_MS,
  DEFAULT_SHORTCUT,
  DEFAULT_LAUNCHER_LABEL,
  getConfig,
  isEnabled,
  buildReport,
  createReportId,
  parseShortcut,
  matchesShortcut,
} from './core/index';
export type {
  SchemaVersion,
  ReportError,
  ReportApplication,
  ReportContext,
  ReportPayload,
  ScreenshotAttachment,
  InitOptions,
  SdkConfig,
  LauncherOptions,
  LauncherPosition,
  ShortcutSpec,
  SdkEvent,
  SdkEventListener,
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
 * Initialise the SDK. Validates and stores config and builds the DOM side
 * effects (shortcut listener, launcher, form). Invalid config logs one warning,
 * does not throw, and leaves the SDK disabled. Safe to call more than once:
 * the previous instance is torn down first.
 *
 * @returns `true` when the config was accepted.
 */
export function init(options: InitOptions): boolean {
  return initSdk(options);
}

/**
 * Open the bug-reporting form. No-op (with a warning) before a successful
 * {@link init}.
 *
 * @param _context reserved for pre-filling report context in a later phase.
 */
export function openReportForm(_context?: CaptureContext): void {
  openForm();
}

/**
 * Tear down all DOM side effects (form, launcher, shortcut) and disable the SDK.
 * Call {@link init} again to re-enable.
 */
export function destroy(): void {
  destroySdk();
}

/**
 * Capture an exception. Phase 3: no-op stub.
 */
export function captureException(
  _error: unknown,
  _context?: CaptureContext,
): void {
  // no-op until the collectors phase
}

/**
 * Capture a free-form message. Phase 3: no-op stub.
 */
export function captureMessage(
  _message: string,
  _context?: CaptureContext,
): void {
  // no-op until the collectors phase
}
