/**
 * Minimal report builder.
 *
 * Produces a {@link ReportPayload} matching the **ASSUMED** draft contract in
 * `docs/backend-contract.md`. This is SSR-safe: it never touches `window`,
 * `document`, or `location` unless they exist.
 */
import { SDK_VERSION } from './version';
import { SCHEMA_VERSION } from './types';
import type { ReportApplication, ReportError, ReportPayload } from './types';
import { getConfig } from './config';

export interface BuildReportInput {
  /** Human description. Must be non-empty by contract. */
  readonly description: string;
  /** Captured error, or `null` for a manual report. */
  readonly error?: ReportError | null;
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

/** Returns `location.href` when a browser location exists, else an empty string. */
function safeLocationHref(): string {
  if (
    typeof location !== 'undefined' &&
    location !== null &&
    typeof location.href === 'string'
  ) {
    return location.href;
  }
  return '';
}

/** Builds `context.application` from the stored config (omitting absent fields). */
function applicationFromConfig(): ReportApplication {
  const config = getConfig();
  const application: { version?: string; environment?: string } = {};
  if (config?.appVersion !== undefined) application.version = config.appVersion;
  if (config?.environment !== undefined) {
    application.environment = config.environment;
  }
  return application;
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
    context: {
      url: options.url ?? safeLocationHref(),
      timestamp: now.toISOString(),
      application: options.application ?? applicationFromConfig(),
    },
    breadcrumbs: [],
    networkFailures: [],
    screenshot: null,
  };
}
