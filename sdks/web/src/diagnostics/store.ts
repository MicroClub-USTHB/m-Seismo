/**
 * Active diagnostics store (Phase 5.2, age-aware in Phase 5.3).
 *
 * Owns the single in-memory {@link DiagnosticBuffer} for the current SDK
 * lifetime. Kept out of the form/launcher lifecycle on purpose:
 *
 * - a **repeated `init()`** keeps existing events (they are real history) but
 *   applies the new `diagnostics.maxEvents`/`maxAgeMs` via `resize()`;
 * - **`destroy()`** clears the buffer and detaches it.
 *
 * `recordDiagnostic()` bounds each event's serialized size and has a re-entrancy
 * guard so a failure while recording can never start a second capture.
 */
import { boundEventSize } from './bound';
import { DiagnosticBuffer } from './buffer';
import { DEFAULT_MAX_AGE_MS, DEFAULT_MAX_EVENTS } from './types';
import type { DiagnosticEvent } from './types';

let active: DiagnosticBuffer | null = null;
let recording = false;

/**
 * Ensures a buffer exists and applies `maxEvents`/`maxAgeMs`. Existing events
 * are kept; only the capacity/age window changes.
 */
export function activateDiagnostics(
  maxEvents: number = DEFAULT_MAX_EVENTS,
  maxAgeMs: number = DEFAULT_MAX_AGE_MS,
  now: () => number = Date.now,
): DiagnosticBuffer {
  if (active === null) {
    active = new DiagnosticBuffer({ maxEvents, maxAgeMs, now });
  } else {
    active.resize(maxEvents, maxAgeMs);
  }
  return active;
}

/** The active buffer, or `null` when the SDK is not initialised. */
export function getDiagnosticsBuffer(): DiagnosticBuffer | null {
  return active;
}

/**
 * Pushes an event unless a push is already in progress (re-entrancy guard). The
 * event is size-bounded first. Never throws.
 */
export function recordDiagnostic(event: DiagnosticEvent): void {
  if (recording) return;
  const buffer = active;
  if (buffer === null) return;
  recording = true;
  try {
    buffer.push(boundEventSize(event));
  } catch {
    // Recording must never affect the host.
  } finally {
    recording = false;
  }
}

/** Whether a diagnostic push is currently in progress. */
export function isRecordingDiagnostic(): boolean {
  return recording;
}

/**
 * Detaches the active buffer. When `clear` is true the events are discarded
 * (used by `destroy()`); otherwise they are kept but no longer active.
 */
export function deactivateDiagnostics(clear: boolean): void {
  if (clear) active?.clear();
  active = null;
}
