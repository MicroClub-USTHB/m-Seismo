/**
 * Builds {@link DiagnosticEvent}s with a consistent id/timestamp.
 *
 * Uses the same id generator as reports so ids are uniform across the SDK.
 */
import { createReportId } from '../core/reportBuilder';
import type { DiagnosticError, DiagnosticEvent, DiagnosticKind, JsonValue } from './types';

export interface DiagnosticEventInput {
  readonly kind: DiagnosticKind;
  readonly error: DiagnosticError;
  readonly handled: boolean;
  readonly flags?: readonly string[];
  readonly context?: Record<string, JsonValue>;
}

/** Creates a complete diagnostic event from its normalized parts. */
export function createDiagnosticEvent(input: DiagnosticEventInput): DiagnosticEvent {
  const flags = input.flags;
  return {
    id: createReportId(),
    kind: input.kind,
    timestamp: new Date().toISOString(),
    error: input.error,
    handled: input.handled,
    ...(input.context !== undefined ? { context: input.context } : {}),
    ...(flags !== undefined && flags.length > 0 ? { flags } : {}),
  };
}
