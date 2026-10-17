/**
 * Active diagnostics store (Phase 5.2).
 *
 * Owns the single in-memory {@link DiagnosticBuffer} for the current SDK
 * lifetime. Kept out of the form/launcher lifecycle on purpose:
 *
 * - a **repeated `init()`** keeps existing events (they are real history) but
 *   applies the new `diagnostics.maxEvents` via `resize()`;
 * - **`destroy()`** clears the buffer and detaches it.
 *
 * `recordDiagnostic()` has a re-entrancy guard so a failure while recording can
 * never start a second capture.
 */
import { DiagnosticBuffer } from './buffer';
import type { DiagnosticEvent } from './types';

let active: DiagnosticBuffer | null = null;
let recording = false;

/**
 * Ensures a buffer exists and applies `maxEvents`. Existing events are kept;
 * only the capacity changes.
 */
export function activateDiagnostics(maxEvents: number): DiagnosticBuffer {
  if (active === null) {
    active = new DiagnosticBuffer(maxEvents);
  } else {
    active.resize(maxEvents);
  }
  return active;
}

/** The active buffer, or `null` when the SDK is not initialised. */
export function getDiagnosticsBuffer(): DiagnosticBuffer | null {
  return active;
}

/**
 * Pushes an event unless a push is already in progress (re-entrancy guard).
 * Never throws.
 */
export function recordDiagnostic(event: DiagnosticEvent): void {
  if (recording) return;
  const buffer = active;
  if (buffer === null) return;
  recording = true;
  try {
    buffer.push(event);
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
