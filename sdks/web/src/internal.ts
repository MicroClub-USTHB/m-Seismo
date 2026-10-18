/**
 * Internal, test-only inspection surface (Phase 5.2).
 *
 * NOT part of the public API and NOT exported from `src/index.ts`, so it is not
 * included in the built library. It exists so the test app and Playwright can
 * read the diagnostic buffer without widening the public surface.
 */
import { getConfig, isEnabled } from './core/config';
import { selectDiagnostics } from './diagnostics/select';
import type { DiagnosticsSelection } from './diagnostics/select';
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

/** What would be attached to a report right now (test-only). */
export function getDiagnosticsSelection(): DiagnosticsSelection {
  const config = getConfig();
  if (!isEnabled() || config === null) {
    return { breadcrumbs: [], networkFailures: [], error: null };
  }
  return selectDiagnostics(getDiagnosticsBuffer()?.snapshot() ?? [], Date.now(), {
    includeInReports: config.diagnostics.includeInReports,
    maxAgeMs: config.diagnostics.maxAgeMs,
  });
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
