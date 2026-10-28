/**
 * Report context capture — the single entry point used by the submit flow.
 *
 * `getContext()` coordinates every automatic-context collector. It takes
 * everything it needs as explicit input (config values, the report id, and an
 * optional injected environment such as `navigator`/`location`/`window`/`now`)
 * so it can be exercised without a real browser and never reads globals in the
 * deep helpers. When no environment is injected it reads the ambient browser
 * globals *once* here (via {@link readGlobalEnvironment}); in a non-browser
 * environment that yields `{}` and only the browser-independent fields remain.
 *
 * Guarantees:
 * - NEVER throws. Each field is collected behind its own try/catch; a failing
 *   collector omits that one field and leaves the rest intact.
 * - Read-only. Nothing is written to `navigator`, `location`, `history`,
 *   storage, cookies or any other host state.
 * - Absent fields are omitted, never filled with placeholder strings.
 */
import type { ReportApplication, ReportContext } from '../core/types';
import { detectBrowser } from './browser';
import type { BrowserInfo } from './browser';
import { readGlobalEnvironment } from './env';
import type { ContextEnvironment } from './env';
import { detectPlatform } from './platform';
import type { Platform } from './platform';
import { sanitizeUrl } from './sanitizeUrl';
import { detectViewport } from './viewport';
import type { Viewport } from './viewport';

export interface ContextInput {
  /**
   * The report id already created for this form session. Echoed back for the
   * submit flow. It is NOT embedded in {@link ReportContext} (the contract keeps
   * `reportId` top-level); `getContext` never generates one.
   */
  readonly reportId?: string;
  /** Application version, copied from SDK config. */
  readonly appVersion?: string;
  /** Environment name, copied from SDK config. */
  readonly environment?: string;
  /** Extra sensitive query parameters to redact from the URL. */
  readonly sensitiveQueryParams?: readonly string[];
  /** Override the page URL (defaults to `location.href` when available). */
  readonly url?: string;
  /** Override the capture time (defaults to `env.now` or `new Date()`). */
  readonly now?: Date;
  /** Injected, read-only environment. Defaults to the ambient browser globals. */
  readonly env?: ContextEnvironment;
}

export interface CollectedContext {
  /** Echo of {@link ContextInput.reportId} (never generated here). */
  readonly reportId: string | undefined;
  /** The collected context. */
  readonly context: ReportContext;
}

/** Runs a collector, turning any throw into `undefined` for that field only. */
function safeField<T>(collect: () => T | undefined): T | undefined {
  try {
    return collect();
  } catch {
    return undefined;
  }
}

/** ISO timestamp for the submission. Never throws (falls back to "now"). */
function resolveTimestamp(provided: Date | undefined): string {
  try {
    if (provided instanceof Date && !Number.isNaN(provided.getTime())) {
      return provided.toISOString();
    }
    return new Date().toISOString();
  } catch {
    return new Date().toISOString();
  }
}

/** Builds `context.application` from the provided config values. */
function resolveApplication(
  appVersion: string | undefined,
  environment: string | undefined,
): ReportApplication {
  const application: { version?: string; environment?: string } = {};
  if (typeof appVersion === 'string' && appVersion.length > 0) {
    application.version = appVersion;
  }
  if (typeof environment === 'string' && environment.length > 0) {
    application.environment = environment;
  }
  return application;
}

/**
 * Collects the report context. See the module doc for guarantees.
 */
export function getContext(input: ContextInput = {}): CollectedContext {
  const env: ContextEnvironment = input.env ?? safeField(readGlobalEnvironment) ?? {};

  const timestamp = resolveTimestamp(input.now ?? env.now);

  const rawUrl = input.url ?? env.location?.href;
  const url = safeField(() =>
    input.sensitiveQueryParams !== undefined
      ? sanitizeUrl(rawUrl, { sensitiveQueryParams: input.sensitiveQueryParams })
      : sanitizeUrl(rawUrl),
  );

  const browser: BrowserInfo | undefined = safeField(() => detectBrowser(env));
  const platform: Platform | undefined = safeField(() => detectPlatform(env));
  const viewport: Viewport | undefined = safeField(() => detectViewport(env));

  const context: {
    url?: string;
    timestamp: string;
    browser?: BrowserInfo;
    platform?: Platform;
    viewport?: Viewport;
    application: ReportApplication;
  } = {
    timestamp,
    application: resolveApplication(input.appVersion, input.environment),
  };

  if (url !== undefined) context.url = url;
  if (browser !== undefined) context.browser = browser;
  if (platform !== undefined) context.platform = platform;
  if (viewport !== undefined) context.viewport = viewport;

  return { reportId: input.reportId, context };
}
