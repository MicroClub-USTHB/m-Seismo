/**
 * Public API surface of the m-Seismo Web SDK.
 *
 * Phase 1 only exposes typed no-op stubs so the test application and the unit
 * tests have something stable to import. None of these functions touch the DOM
 * or `window`/`document` yet, which keeps the module import SSR-safe.
 */

export const SDK_VERSION = '0.0.0';

export interface InitOptions {
  /** Endpoint that receives unified reports. Contract defined in a later phase. */
  readonly endpoint?: string;
}

export interface CaptureContext {
  /** Arbitrary structured context attached to a report. */
  readonly [key: string]: unknown;
}

/**
 * Initialise the SDK. Phase 1: no-op stub.
 */
export function init(_options: InitOptions = {}): void {
  // no-op in Phase 1
}

/**
 * Open the bug-reporting form. Phase 1: no-op stub.
 */
export function openReportForm(_context?: CaptureContext): void {
  // no-op in Phase 1
}

/**
 * Capture an exception and send it as part of a report. Phase 1: no-op stub.
 */
export function captureException(
  _error: unknown,
  _context?: CaptureContext,
): void {
  // no-op in Phase 1
}

/**
 * Capture a free-form message. Phase 1: no-op stub.
 */
export function captureMessage(
  _message: string,
  _context?: CaptureContext,
): void {
  // no-op in Phase 1
}
