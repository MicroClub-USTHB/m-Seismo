/**
 * Small shared assertions over SDK buffer state and receiver storage.
 * Keeps the scenario files free of copy-pasted polling logic.
 */
import type { DiagnosticEvent } from '@sdk/diagnostics/types';
import type { LabContext } from '../types';
import type { StoredReport } from './receiver';
import { waitFor } from './waiters';

/** Waits until the receiver has stored at least `count` reports. */
export async function waitForStoredCount(
  ctx: LabContext,
  count: number,
  timeoutMs = 5_000,
): Promise<boolean> {
  return waitFor(async () => (await ctx.receiver.getReports()).count >= count, timeoutMs);
}

/** Fetches all stored reports (metadata form; screenshot base64 redacted). */
export async function storedReports(ctx: LabContext, full = false): Promise<StoredReport[]> {
  return (await ctx.receiver.getReports({ full })).reports;
}

/** Waits for a buffered event matching `predicate`. Returns it or undefined. */
export async function waitForEvent(
  ctx: LabContext,
  predicate: (event: DiagnosticEvent) => boolean,
  timeoutMs = 5_000,
): Promise<DiagnosticEvent | undefined> {
  let found: DiagnosticEvent | undefined;
  await waitFor(() => {
    found = ctx.snapshot().find(predicate);
    return found !== undefined;
  }, timeoutMs);
  return found;
}

/** Deep JSON text of a value (never throws). */
export function json(value: unknown): string {
  try {
    return JSON.stringify(value) ?? String(value);
  } catch {
    return String(value);
  }
}
