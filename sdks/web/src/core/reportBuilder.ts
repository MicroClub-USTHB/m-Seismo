/**
 * Minimal report builder.
 *
 * Produces a {@link ReportPayload} matching the **ASSUMED** draft contract in
 * `docs/backend-contract.md`. This is SSR-safe: it never touches `window`,
 * `document`, or `location` unless they exist (context capture lives in
 * `src/context/getContext.ts`).
 */
import { SDK_VERSION } from './version';
import { SCHEMA_VERSION } from './types';
import type {
  ReportApplication,
  ReportError,
  ReportPayload,
  ScreenshotAttachment,
} from './types';
import { getContext } from '../context/getContext';

export interface BuildReportInput {
  /** Human description. Must be non-empty by contract. */
  readonly description: string;
  /** Captured error, or `null` for a manual report. */
  readonly error?: ReportError | null;
  /** Optional screenshot attachment; omitted/`null` when none. */
  readonly screenshot?: ScreenshotAttachment | null;
}

export interface BuildReportOptions {
  /** Override the page URL (defaults to `location.href` when available). */
  readonly url?: string;
  /** Override `context.application` (defaults to the configured app metadata). */
  readonly application?: ReportApplication;
  /** Override the timestamp (defaults to `new Date()`). */
  readonly now?: Date;
  /** Override the generated id (useful in tests / for idempotency). */
  readonly reportId?: string;
}

/**
 * Creates a UUID v4 string. Uses `crypto.randomUUID` when available, with a
 * `crypto.getRandomValues` / `Math.random` fallback for older or insecure
 * contexts. Never throws.
 */
export function createReportId(): string {
  const cryptoObj = globalThis.crypto;
  if (cryptoObj && typeof cryptoObj.randomUUID === 'function') {
    return cryptoObj.randomUUID();
  }

  const bytes = new Uint8Array(16);
  if (cryptoObj && typeof cryptoObj.getRandomValues === 'function') {
    cryptoObj.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i += 1) {
      bytes[i] = Math.floor(Math.random() * 256);
    }
  }
  // Set version (4) and variant bits.
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;

  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0'));
  const part = (start: number, end: number): string =>
    hex.slice(start, end).join('');
  return `${part(0, 4)}-${part(4, 6)}-${part(6, 8)}-${part(8, 10)}-${part(10, 16)}`;
}

/**
 * Builds a report payload. `error` defaults to `null`, which is valid for manual
 * reports.
 */
export function buildReport(
  input: BuildReportInput,
  options: BuildReportOptions = {},
): ReportPayload {
  const now = options.now ?? new Date();
  return {
    schemaVersion: SCHEMA_VERSION,
    sdkVersion: SDK_VERSION,
    reportId: options.reportId ?? createReportId(),
    description: input.description,
    error: input.error ?? null,
    context: getContext({
      ...(options.url !== undefined ? { url: options.url } : {}),
      ...(options.application !== undefined
        ? { application: options.application }
        : {}),
      now,
    }),
    breadcrumbs: [],
    networkFailures: [],
    screenshot: input.screenshot ?? null,
  };
}
