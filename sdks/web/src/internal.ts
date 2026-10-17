/**
 * Internal, test-only inspection surface (Phase 5.2).
 *
 * NOT part of the public API and NOT exported from `src/index.ts`, so it is not
 * included in the built library. It exists so the test app and Playwright can
 * read the diagnostic buffer without widening the public surface.
 */
import { isEnabled } from './core/config';
import {
  deactivateDiagnostics,
  getDiagnosticsBuffer,
  isRecordingDiagnostic,
} from './diagnostics/store';
import type { DiagnosticEvent } from './diagnostics/types';

/** Snapshot of buffered diagnostics, or `[]` when the SDK is not enabled. */
export function getDiagnosticsSnapshot(): DiagnosticEvent[] {
  if (!isEnabled()) return [];
  return getDiagnosticsBuffer()?.snapshot() ?? [];
}

/** Number of buffered diagnostics, or `0` when the SDK is not enabled. */
export function getDiagnosticsCount(): number {
  if (!isEnabled()) return 0;
  return getDiagnosticsBuffer()?.size() ?? 0;
}

/** Clears the buffered diagnostics (test-only). */
export function clearDiagnostics(): void {
  getDiagnosticsBuffer()?.clear();
}

/** Whether a diagnostic push is currently in progress (test-only). */
export function isRecordingDiagnostics(): boolean {
  return isRecordingDiagnostic();
}

/** Fully detaches (and clears) the diagnostics store (test-only). */
export function resetDiagnostics(): void {
  deactivateDiagnostics(true);
}
