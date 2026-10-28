/**
 * Public `captureException()` and `captureMessage()` (Phase 5.2, reports 5.3/7).
 *
 * Both let the host report something it caught itself. They never throw, accept
 * any value, and are no-ops (with one warning) before a successful `init()`,
 * while the SDK is disabled, or after `destroy()`.
 *
 * Each records a `captured` diagnostic event into the buffer FIRST, then — when
 * `reporting.sendCapturedExceptions` is true (default) — asks the unified
 * processor for a `captured_exception` / `captured_message` report. Sending is
 * fire-and-forget: the call returns immediately.
 *
 * TODO Phase 6: the context is structurally sanitized only; string contents are
 * not fully redacted yet.
 */
import { getConfig, isEnabled } from '../core/config';
import { warn } from '../core/log';
import { normalizeError } from '../errors/normalize';
import { sanitizeContext } from './jsonSafe';
import { createDiagnosticEvent } from './event';
import { getDiagnosticsBuffer, recordDiagnostic } from './store';
import type { ErrorDiagnosticEvent, JsonValue } from './types';
import { processInput } from '../reporting/runtime';

function sanitizeOptionalContext(
  context: Record<string, unknown> | undefined,
): Record<string, JsonValue> | undefined {
  if (context === undefined || context === null || typeof context !== 'object') {
    return undefined;
  }
  try {
    return sanitizeContext(context);
  } catch {
    return undefined;
  }
}

function dispatch(event: ErrorDiagnosticEvent, message?: string): void {
  recordDiagnostic(event);
  const config = getConfig();
  if (config === null || !config.reporting.sendCapturedExceptions) return;
  void processInput({
    source: 'api',
    event,
    ...(message !== undefined ? { message } : {}),
  }).catch(() => {
    // processInput() never rejects; guard anyway.
  });
}

/** Records a caught error as a `captured` event and (by default) a report. */
export function captureException(
  error: unknown,
  context?: Record<string, unknown>,
): void {
  if (!isEnabled()) {
    warn('captureException() ignored: the SDK is not initialized (or was destroyed).');
    return;
  }
  if (getDiagnosticsBuffer() === null) {
    warn('captureException() ignored: diagnostics are not active.');
    return;
  }

  try {
    const normalized = normalizeError(error);
    const sanitizedContext = sanitizeOptionalContext(context);
    const event = createDiagnosticEvent({
      kind: 'captured',
      error: normalized.error,
      handled: true,
      flags: normalized.flags,
      ...(sanitizedContext !== undefined ? { context: sanitizedContext } : {}),
    });
    dispatch(event);
  } catch {
    // captureException() must never throw into the host.
  }
}

/**
 * Records a free-form message as a `captured` event and (by default) a
 * `captured_message` report. Never throws.
 */
export function captureMessage(
  message: string,
  context?: Record<string, unknown>,
): void {
  if (!isEnabled()) {
    warn('captureMessage() ignored: the SDK is not initialized (or was destroyed).');
    return;
  }
  if (getDiagnosticsBuffer() === null) {
    warn('captureMessage() ignored: diagnostics are not active.');
    return;
  }
  if (typeof message !== 'string') {
    warn('captureMessage() ignored: "message" must be a string.');
    return;
  }

  try {
    const sanitizedContext = sanitizeOptionalContext(context);
    const event = createDiagnosticEvent({
      kind: 'captured',
      error: { type: 'Message', message },
      handled: true,
      ...(sanitizedContext !== undefined ? { context: sanitizedContext } : {}),
    });
    dispatch(event, message);
  } catch {
    // captureMessage() must never throw into the host.
  }
}
