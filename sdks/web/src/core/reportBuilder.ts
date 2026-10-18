/**
 * Report builder — assembly only.
 *
 * The builder takes an already-collected {@link ReportContext} and the user's
 * input and assembles a {@link ReportPayload} matching the **ASSUMED** draft
 * contract in `docs/backend-contract.md`. It performs NO context collection: the
 * submit flow calls `getContext()` first and passes the result in. This keeps
 * collection, assembly and transport separate.
 *
 * SSR-safe: it never touches `window`, `document`, or `location`.
 */
import { SDK_VERSION } from './version';
import { SCHEMA_VERSION } from './types';
import type {
  ReportBreadcrumb,
  ReportContext,
  ReportError,
  ReportNetworkFailure,
  ReportPayload,
  ScreenshotAttachment,
} from './types';

export interface BuildReportInput {
  /** Human description. Must be non-empty by contract. */
  readonly description: string;
  /** Context collected by `getContext()` at submit time. */
  readonly context: ReportContext;
  /** Captured error, or `null` for a manual report. */
  readonly error?: ReportError | null;
  /** Recent breadcrumbs (Phase 5.3); defaults to empty. */
  readonly breadcrumbs?: readonly ReportBreadcrumb[];
  /** Recent network failures (Phase 5.3); defaults to empty. */
  readonly networkFailures?: readonly ReportNetworkFailure[];
  /** Optional screenshot attachment; omitted/`null` when none. */
  readonly screenshot?: ScreenshotAttachment | null;
}

export interface BuildReportOptions {
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
 * Assembles a report payload from user input and pre-collected context.
 * `error` defaults to `null`, which is valid for manual reports.
 */
export function buildReport(
  input: BuildReportInput,
  options: BuildReportOptions = {},
): ReportPayload {
  return {
    schemaVersion: SCHEMA_VERSION,
    sdkVersion: SDK_VERSION,
    reportId: options.reportId ?? createReportId(),
    description: input.description,
    error: input.error ?? null,
    context: input.context,
    breadcrumbs: input.breadcrumbs ?? [],
    networkFailures: input.networkFailures ?? [],
    screenshot: input.screenshot ?? null,
  };
}
