/**
 * SDK initialisation helper for the lab.
 *
 * The endpoint mirrors the existing test app (Vite env override, local mock
 * receiver by default). Every scenario is initialised with the same baseline
 * options plus its own overrides, so scenarios differ only by what they declare.
 */
import { init } from '@m-seismo/web';
import type { InitOptions, SdkEvent } from '@m-seismo/web';
import type { LabSdkConfig } from '../types';

/** The ingest endpoint used by the lab (and the mock receiver origin). */
export const LAB_ENDPOINT =
  (import.meta.env.VITE_MSEISMO_ENDPOINT as string | undefined) ??
  'http://localhost:4010/api/reports';

/** Origin of the mock receiver derived from {@link LAB_ENDPOINT}. */
export const LAB_RECEIVER_BASE = new URL(LAB_ENDPOINT).origin;

let eventSink: ((event: SdkEvent) => void) | null = null;

/** Test-only: observe SDK lifecycle events (used by a couple of verifiers). */
export function setLabEventSink(sink: ((event: SdkEvent) => void) | null): void {
  eventSink = sink;
}

/** Always registered so a sink set later still receives events. */
function forwardEvent(event: SdkEvent): void {
  eventSink?.(event);
}

/** Builds the effective init options for a scenario. */
export function buildInitOptions(overrides: LabSdkConfig = {}): InitOptions {
  const options: InitOptions = {
    endpoint: overrides.endpoint ?? LAB_ENDPOINT,
    appVersion: overrides.appVersion ?? '0.0.0',
    environment: overrides.environment ?? 'error-lab',
    launcher: overrides.launcher ?? true,
    screenshot: overrides.screenshot ?? true,
    shortcut: overrides.shortcut ?? 'Ctrl+Enter',
    onEvent: forwardEvent,
    ...(overrides.timeoutMs !== undefined ? { timeoutMs: overrides.timeoutMs } : {}),
    ...(overrides.privacy !== undefined ? { privacy: overrides.privacy } : {}),
    ...(overrides.diagnostics !== undefined ? { diagnostics: overrides.diagnostics } : {}),
    ...(overrides.reporting !== undefined ? { reporting: overrides.reporting } : {}),
  };
  return options;
}

/** Re-initialises the SDK with the lab baseline plus `overrides`. */
export function initLabSdk(overrides: LabSdkConfig = {}): boolean {
  return init(buildInitOptions(overrides));
}
