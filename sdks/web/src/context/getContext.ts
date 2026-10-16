/**
 * Report context capture.
 *
 * Today this gathers the page URL, a timestamp, and the configured application
 * metadata. It is deliberately a single seam: Phase 5 (device/user context,
 * breadcrumbs) plugs in here without touching the builder or the form.
 *
 * SSR-safe: `location` is only read when it exists.
 */
import { getConfig } from '../core/config';
import type { ReportApplication, ReportContext } from '../core/types';

export interface ContextOverrides {
  /** Override the page URL (defaults to `location.href` when available). */
  readonly url?: string;
  /** Override `context.application` (defaults to the configured app metadata). */
  readonly application?: ReportApplication;
  /** Override the timestamp (defaults to `new Date()`). */
  readonly now?: Date;
}

/** Returns `location.href` when a browser location exists, else an empty string. */
export function safeLocationHref(): string {
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
export function applicationFromConfig(): ReportApplication {
  const config = getConfig();
  const application: { version?: string; environment?: string } = {};
  if (config?.appVersion !== undefined) application.version = config.appVersion;
  if (config?.environment !== undefined) {
    application.environment = config.environment;
  }
  return application;
}

/** Builds a {@link ReportContext}, honouring overrides. */
export function getContext(overrides: ContextOverrides = {}): ReportContext {
  const now = overrides.now ?? new Date();
  return {
    url: overrides.url ?? safeLocationHref(),
    timestamp: now.toISOString(),
    application: overrides.application ?? applicationFromConfig(),
  };
}
