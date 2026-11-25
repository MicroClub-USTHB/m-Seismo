/**
 * Builds {@link DiagnosticEvent}s with a consistent id/timestamp.
 *
 * Uses the same id generator as reports so ids are uniform across the SDK. The
 * error-like creator ({@link createDiagnosticEvent}) is used by the Phase 5.2
 * collectors; the four Phase 5.3 creators build behavioural breadcrumbs.
 */
import { createReportId } from '../core/reportBuilder';
import type {
  DiagnosticError,
  ErrorDiagnosticEvent,
  ErrorDiagnosticKind,
  InteractionData,
  InteractionDiagnosticEvent,
  JsonValue,
  NavigationData,
  NavigationDiagnosticEvent,
  NetworkData,
  NetworkDiagnosticEvent,
  SdkDiagnosticEvent,
} from './types';

export interface DiagnosticEventInput {
  readonly kind: ErrorDiagnosticKind;
  readonly error: DiagnosticError;
  readonly handled: boolean;
  readonly flags?: readonly string[];
  readonly context?: Record<string, JsonValue>;
}

/** Common `id`/`timestamp` pair for every event. */
function base(): { id: string; timestamp: string } {
  return { id: createReportId(), timestamp: new Date().toISOString() };
}

/** Creates a complete error-like diagnostic event from its normalized parts. */
export function createDiagnosticEvent(
  input: DiagnosticEventInput,
): ErrorDiagnosticEvent {
  const flags = input.flags;
  return {
    ...base(),
    kind: input.kind,
    error: input.error,
    handled: input.handled,
    ...(input.context !== undefined ? { context: input.context } : {}),
    ...(flags !== undefined && flags.length > 0 ? { flags } : {}),
  };
}

/** Creates a navigation breadcrumb event. */
export function createNavigationDiagnosticEvent(
  data: NavigationData,
): NavigationDiagnosticEvent {
  return { ...base(), kind: 'navigation', data };
}

/** Creates an interaction breadcrumb event. */
export function createInteractionDiagnosticEvent(
  data: InteractionData,
): InteractionDiagnosticEvent {
  return { ...base(), kind: 'interaction', data };
}

/** Creates a network breadcrumb event. */
export function createNetworkDiagnosticEvent(
  data: NetworkData,
): NetworkDiagnosticEvent {
  return { ...base(), kind: 'network', data };
}

/** Creates an SDK lifecycle breadcrumb event. */
export function createSdkDiagnosticEvent(name: string): SdkDiagnosticEvent {
  return { ...base(), kind: 'sdk', data: { name } };
}
