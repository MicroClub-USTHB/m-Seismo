/**
 * SDK configuration and `init()` validation.
 *
 * `endpoint` is the only required option. Invalid configuration makes the SDK
 * fail predictably: a warning is logged, nothing is thrown, and the SDK stays
 * disabled until a valid `init()` call.
 *
 * The endpoint value is user-supplied. `/api/reports` is only an **ASSUMED**
 * convention (see `docs/backend-contract.md`); no backend host is hard-coded.
 */
import { warn } from './log';
import { detectMac, parseShortcut } from './shortcut';
import type { ShortcutSpec } from './shortcut';
import type { SdkEvent, SdkEventListener } from './events';

/** Default request timeout, in milliseconds. */
export const DEFAULT_TIMEOUT_MS = 10_000;

/** Default keyboard shortcut that opens the form. */
export const DEFAULT_SHORTCUT = 'Ctrl+Enter';

/** Default label for the launcher button. */
export const DEFAULT_LAUNCHER_LABEL = 'Report a bug';

export type LauncherPosition =
  | 'bottom-right'
  | 'bottom-left'
  | 'top-right'
  | 'top-left';

export interface LauncherOptions {
  /** Button label. Defaults to {@link DEFAULT_LAUNCHER_LABEL}. */
  readonly label?: string;
  /** Corner to anchor the button in. Defaults to `'bottom-right'`. */
  readonly position?: LauncherPosition;
}

/** Normalised launcher configuration stored on {@link SdkConfig}. */
export type LauncherConfig =
  | { readonly enabled: false }
  | {
      readonly enabled: true;
      readonly label: string;
      readonly position: LauncherPosition;
    };

/** Privacy controls. Phase 5.1 introduces `sensitiveQueryParams`. */
export interface PrivacyOptions {
  /**
   * Extra query-parameter names whose values are redacted from the report URL,
   * in addition to the built-in sensitive list. Case-insensitive. Invalid
   * entries are ignored with a single warning.
   */
  readonly sensitiveQueryParams?: readonly string[];
}

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
  /**
   * Keyboard combo that opens the form, e.g. `'Ctrl+Enter'` or `'Mod+Shift+K'`.
   * Defaults to {@link DEFAULT_SHORTCUT}. Pass `false` to disable it. An invalid
   * value logs a warning and disables the shortcut.
   */
  readonly shortcut?: string | false;
  /**
   * Floating launcher button. `true` shows it with defaults; an object customises
   * its label/position. Defaults to `false` (no launcher).
   */
  readonly launcher?: boolean | LauncherOptions;
  /** Whether the form offers a screenshot attachment. Defaults to `true`. */
  readonly screenshot?: boolean;
  /** Optional additional privacy controls (Phase 5.1). */
  readonly privacy?: PrivacyOptions;
  /** Optional listener for SDK lifecycle events. */
  readonly onEvent?: SdkEventListener;
}

export interface SdkConfig {
  readonly endpoint: string;
  readonly appVersion?: string;
  readonly environment?: string;
  readonly projectKey?: string;
  readonly timeoutMs: number;
  readonly shortcut: ShortcutSpec | false;
  readonly launcher: LauncherConfig;
  readonly screenshot: boolean;
  /** Extra sensitive query params for URL sanitizing; omitted when empty. */
  readonly sensitiveQueryParams?: readonly string[];
  readonly onEvent?: SdkEventListener;
}

let currentConfig: SdkConfig | null = null;

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

function normaliseShortcut(value: unknown, isMac: boolean): ShortcutSpec | false {
  if (value === false) return false;
  const raw = value === undefined ? DEFAULT_SHORTCUT : value;
  if (typeof raw !== 'string') {
    warn(
      'init() option "shortcut" must be a key-combo string or false; shortcut disabled.',
    );
    return false;
  }
  const spec = parseShortcut(raw, isMac);
  if (spec === null) {
    warn(
      `init() option "shortcut" is not a valid key combo ("${raw}"); shortcut disabled.`,
    );
    return false;
  }
  return spec;
}

const LAUNCHER_POSITIONS: readonly LauncherPosition[] = [
  'bottom-right',
  'bottom-left',
  'top-right',
  'top-left',
];

function isLauncherPosition(value: unknown): value is LauncherPosition {
  return (
    typeof value === 'string' &&
    (LAUNCHER_POSITIONS as readonly string[]).includes(value)
  );
}

function normaliseLauncher(value: unknown): LauncherConfig {
  if (value === true) {
    return { enabled: true, label: DEFAULT_LAUNCHER_LABEL, position: 'bottom-right' };
  }
  if (value !== null && typeof value === 'object') {
    const options = value as LauncherOptions;
    const label =
      typeof options.label === 'string' && options.label.length > 0
        ? options.label
        : DEFAULT_LAUNCHER_LABEL;
    const position = isLauncherPosition(options.position)
      ? options.position
      : 'bottom-right';
    return { enabled: true, label, position };
  }
  return { enabled: false };
}

function normaliseScreenshot(value: unknown): boolean {
  return value !== false;
}

/**
 * Normalises `privacy.sensitiveQueryParams`. Returns `undefined` when there is
 * nothing valid to store. A non-array value or non-string entries are ignored
 * with at most one warning.
 */
function normaliseSensitiveQueryParams(
  value: unknown,
): readonly string[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value)) {
    warn(
      'init() option "privacy.sensitiveQueryParams" must be an array of strings; ignored.',
    );
    return undefined;
  }
  const valid: string[] = [];
  let warned = false;
  for (const entry of value) {
    if (typeof entry === 'string' && entry.trim().length > 0) {
      valid.push(entry.trim());
    } else if (!warned) {
      warn(
        'init() option "privacy.sensitiveQueryParams" contains non-string entries; those were ignored.',
      );
      warned = true;
    }
  }
  return valid.length > 0 ? valid : undefined;
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
    shortcut: ShortcutSpec | false;
    launcher: LauncherConfig;
    screenshot: boolean;
    sensitiveQueryParams?: readonly string[];
    onEvent?: SdkEventListener;
  } = {
    endpoint: options.endpoint,
    timeoutMs: normaliseTimeout(options.timeoutMs),
    shortcut: normaliseShortcut(options.shortcut, detectMac()),
    launcher: normaliseLauncher(options.launcher),
    screenshot: normaliseScreenshot(options.screenshot),
  };
  if (options.appVersion !== undefined) next.appVersion = options.appVersion;
  if (options.environment !== undefined) next.environment = options.environment;
  if (options.projectKey !== undefined) next.projectKey = options.projectKey;
  if (typeof options.onEvent === 'function') next.onEvent = options.onEvent;

  const privacy = options.privacy;
  const sensitive = normaliseSensitiveQueryParams(
    privacy !== null && typeof privacy === 'object'
      ? (privacy as PrivacyOptions).sensitiveQueryParams
      : undefined,
  );
  if (sensitive !== undefined) next.sensitiveQueryParams = sensitive;

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

// Re-exported for convenience so callers can type event handlers.
export type { SdkEvent, SdkEventListener };
