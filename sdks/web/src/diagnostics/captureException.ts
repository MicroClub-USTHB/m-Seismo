/**
 * Public `captureException()` (Phase 5.2).
 *
 * Lets the host application report an error it caught itself. It never throws
 * and accepts any value as `error`. Before a successful `init()`, while the SDK
 * is disabled, or after `destroy()`, it is a no-op with one warning and the
 * error is NOT recorded. It never logs the error to the console.
 *
 * TODO Phase 6: the context is structurally sanitized only; string contents are
 * not redacted yet.
 */
import { isEnabled } from '../core/config';
import { warn } from '../core/log';
import { normalizeError } from '../errors/normalize';
import { sanitizeContext } from './jsonSafe';
import { createDiagnosticEvent } from './event';
import { getDiagnosticsBuffer, recordDiagnostic } from './store';
import type { JsonValue } from './types';

/** Records a caught error as a `captured` diagnostic event. Never throws. */
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

    let sanitizedContext: Record<string, JsonValue> | undefined;
    if (context !== undefined && context !== null && typeof context === 'object') {
      sanitizedContext = sanitizeContext(context);
    }

    const event = createDiagnosticEvent({
      kind: 'captured',
      error: normalized.error,
      handled: true,
      flags: normalized.flags,
      ...(sanitizedContext !== undefined ? { context: sanitizedContext } : {}),
    });
    recordDiagnostic(event);
  } catch {
    // captureException() must never throw into the host.
  }
}
