/**
 * Unhandled promise rejection collector (Phase 5.2).
 *
 * Registers ONE `unhandledrejection` listener via `addEventListener`. It reads
 * `event.reason`, normalizes it defensively, and records an event. It never
 * calls `preventDefault()` (so the browser still logs the rejection), never
 * throws, and never changes host behaviour.
 *
 * Known limit: a rejection that is handled later (`rejectionhandled`) is not
 * tracked in this phase.
 */
import { normalizeError } from '../errors/normalize';
import { createDiagnosticEvent } from '../diagnostics/event';
import { FLAG_NON_ERROR_REJECTION } from '../diagnostics/types';
import type { DiagnosticEvent } from '../diagnostics/types';

/** Narrow structural view of the `PromiseRejectionEvent` we consume. */
interface RejectionEventLike {
  readonly reason?: unknown;
}

function addFlag(flags: string[], flag: string): void {
  if (!flags.includes(flag)) flags.push(flag);
}

function handleRejection(event: RejectionEventLike): DiagnosticEvent {
  const normalized = normalizeError(event.reason);
  const flags = [...normalized.flags];
  if (!normalized.errorLike) addFlag(flags, FLAG_NON_ERROR_REJECTION);
  return createDiagnosticEvent({
    kind: 'unhandledrejection',
    error: normalized.error,
    handled: false,
    flags,
  });
}

/**
 * Installs the collector. Returns a disposer that removes the listener. The
 * handler is fully wrapped so it can never throw into the host.
 */
export function installRejectionCollector(
  host: EventTarget,
  record: (event: DiagnosticEvent) => void,
): () => void {
  const listener = (event: Event): void => {
    try {
      record(handleRejection(event as unknown as RejectionEventLike));
    } catch {
      // Never propagate; never record a failure of our own collector.
    }
  };
  host.addEventListener('unhandledrejection', listener);
  return () => host.removeEventListener('unhandledrejection', listener);
}
