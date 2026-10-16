/**
 * SDK configuration and `init()` validation.
 *
 * `endpoint` is the only required option. Invalid configuration makes the SDK
 * fail predictably: exactly one warning is logged, nothing is thrown, and the
 * SDK stays disabled until a valid `init()` call.
 *
 * The endpoint value is user-supplied. `/api/reports` is only an **ASSUMED**
 * convention (see `docs/backend-contract.md`); no backend host is hard-coded.
 */

/** Default request timeout, in milliseconds. */
export const DEFAULT_TIMEOUT_MS = 10_000;

export interface InitOptions {
  /** Full URL of the ingest endpoint, e.g. `https://example.com/api/reports`. */
  readonly endpoint: string;
  /** Application version, copied into `context.application.version`. */
  readonly appVersion?: string;
  /** Environment name, copied into `context.application.environment`. */
  readonly environment?: string;
  /**
   * Optional public project key. Sent as `X-MSeismo-Key` **only if set**.
   * ASSUMED — see the auth open decision in `docs/backend-contract.md`.
   */
  readonly projectKey?: string;
  /** Request timeout in milliseconds. Defaults to {@link DEFAULT_TIMEOUT_MS}. */
  readonly timeoutMs?: number;
}

export interface SdkConfig {
  readonly endpoint: string;
  readonly appVersion?: string;
  readonly environment?: string;
  readonly projectKey?: string;
  readonly timeoutMs: number;
}

let currentConfig: SdkConfig | null = null;

function warn(message: string): void {
  // eslint-disable-next-line no-console -- one predictable warning per bad init.
  console.warn(`[m-seismo] ${message}`);
}

/** Returns true for an absolute http(s) URL string. */
function isHttpUrl(value: unknown): value is string {
  if (typeof value !== 'string' || value.length === 0) return false;
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

function normaliseTimeout(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return value;
  }
  return DEFAULT_TIMEOUT_MS;
}

/**
 * Validate and store configuration.
 *
 * Calling this again with a valid config fully replaces the previous one (no
 * duplicate state). An invalid call disables the SDK and logs one warning.
 *
 * @returns `true` when the config was accepted and the SDK is enabled.
 */
export function configure(options: InitOptions): boolean {
  if (!isHttpUrl(options?.endpoint)) {
    currentConfig = null;
    warn(
      'init() ignored: "endpoint" must be an absolute http(s) URL. SDK disabled.',
    );
    return false;
  }

  const next: {
    endpoint: string;
    appVersion?: string;
    environment?: string;
    projectKey?: string;
    timeoutMs: number;
  } = {
    endpoint: options.endpoint,
    timeoutMs: normaliseTimeout(options.timeoutMs),
  };
  if (options.appVersion !== undefined) next.appVersion = options.appVersion;
  if (options.environment !== undefined) next.environment = options.environment;
  if (options.projectKey !== undefined) next.projectKey = options.projectKey;

  currentConfig = next;
  return true;
}

/** Current config, or `null` when the SDK is disabled. */
export function getConfig(): SdkConfig | null {
  return currentConfig;
}

/** Whether a valid config is stored. */
export function isEnabled(): boolean {
  return currentConfig !== null;
}

/** Test-only: clear stored config and return to the disabled state. */
export function resetConfig(): void {
  currentConfig = null;
}
