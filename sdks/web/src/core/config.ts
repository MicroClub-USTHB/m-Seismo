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
import type { ReportPayload } from './types';
import { clampMaxAgeMs } from '../diagnostics/buffer';
import {
  DEFAULT_MAX_AGE_MS,
  DEFAULT_MAX_EVENTS,
  MAX_EVENTS_CEILING,
} from '../diagnostics/types';

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

/** Automatic diagnostics controls. Phase 5.2 introduces these; 5.3 extends them. */
export interface DiagnosticsOptions {
  /**
   * Maximum number of buffered diagnostic events. Must be a positive integer;
   * larger values are clamped to {@link MAX_EVENTS_CEILING}. Defaults to
   * {@link DEFAULT_MAX_EVENTS}. Invalid values warn once and fall back.
   */
  readonly maxEvents?: number;
  /**
   * Maximum age of a buffered event, in milliseconds. Clamped to the
   * [10s, 1h] range. Defaults to {@link DEFAULT_MAX_AGE_MS} (10 minutes).
   */
  readonly maxAgeMs?: number;
  /** Capture uncaught runtime errors. Defaults to `true`. */
  readonly captureUncaughtErrors?: boolean;
  /** Capture unhandled promise rejections. Defaults to `true`. */
  readonly captureUnhandledRejections?: boolean;
  /** Capture navigation breadcrumbs. Defaults to `true`. */
  readonly captureNavigation?: boolean;
  /** Capture allow-listed interaction breadcrumbs. Defaults to `true`. */
  readonly captureInteractions?: boolean;
  /** Capture failed `fetch` calls. Defaults to `true`. */
  readonly captureNetwork?: boolean;
  /** Include visible element text in interaction breadcrumbs. Defaults to `false`. */
  readonly captureElementText?: boolean;
  /** Attach recent diagnostics to submitted reports. Defaults to `true`. */
  readonly includeInReports?: boolean;
  /**
   * URLs (exact strings or RegExps matched against the resolved absolute URL)
   * whose `fetch` calls are never recorded. Invalid entries are ignored with a
   * single warning.
   */
  readonly ignoreUrls?: readonly (string | RegExp)[];
}

/** Normalised diagnostics config stored on {@link SdkConfig}. */
export interface DiagnosticsConfig {
  readonly maxEvents: number;
  readonly maxAgeMs: number;
  readonly captureUncaughtErrors: boolean;
  readonly captureUnhandledRejections: boolean;
  readonly captureNavigation: boolean;
  readonly captureInteractions: boolean;
  readonly captureNetwork: boolean;
  readonly captureElementText: boolean;
  readonly includeInReports: boolean;
  /** Omitted when empty. */
  readonly ignoreUrls?: readonly (string | RegExp)[];
}

/** Automatic-reporting policy for unsolicited (non-form) reports. */
export type AutoReportMode = 'off' | 'errors' | 'all';

/** Last-chance hook to inspect/edit or drop a report before it is sent. */
export type BeforeSendHook = (report: ReportPayload) => ReportPayload | null;

/** Default per-fingerprint dedup window (60 s). */
export const DEFAULT_DEDUP_WINDOW_MS = 60_000;
/** Lower clamp for `reporting.dedupWindowMs` (5 s). */
export const MIN_DEDUP_WINDOW_MS = 5_000;
/** Upper clamp for `reporting.dedupWindowMs` (1 h). */
export const MAX_DEDUP_WINDOW_MS = 60 * 60 * 1000;
/** Default automatic/captured sends per minute. */
export const DEFAULT_MAX_AUTO_PER_MINUTE = 5;
/** Ceiling for `reporting.maxAutoReportsPerMinute`. */
export const MAX_AUTO_PER_MINUTE_CEILING = 30;
/** Default automatic/captured sends per page lifetime. */
export const DEFAULT_MAX_AUTO_PER_SESSION = 50;
/** Ceiling for `reporting.maxAutoReportsPerSession`. */
export const MAX_AUTO_PER_SESSION_CEILING = 200;

/**
 * Automatic-reporting controls (Phase 7).
 *
 * `autoReport` defaults to `'off'`: unsolicited reporting is a privacy- and
 * quota-sensitive behaviour that a host must opt into explicitly. `'all'` is
 * accepted and behaves exactly like `'errors'` (only error-like events exist to
 * report automatically today).
 */
export interface ReportingOptions {
  /** `'off'` (buffer only, default), `'errors'`, or `'all'` (alias of `'errors'`). */
  readonly autoReport?: AutoReportMode;
  /** Send a report when `captureException()` is called. Defaults to `true`. */
  readonly sendCapturedExceptions?: boolean;
  /** Per-fingerprint dedup window in ms. Clamped to [5 s, 1 h]. Default 60 s. */
  readonly dedupWindowMs?: number;
  /** Max automatic/captured sends per minute. Default 5; clamped to ≤ 30. */
  readonly maxAutoReportsPerMinute?: number;
  /** Max automatic/captured sends per session. Default 50; clamped to ≤ 200. */
  readonly maxAutoReportsPerSession?: number;
  /**
   * Called after sanitization with a frozen copy of the report. Return the
   * report to send it, or `null` to drop it. A throwing hook causes the
   * unmodified report to be sent. The hook's return value is NOT re-sanitized.
   */
  readonly beforeSend?: BeforeSendHook;
}

/** Normalised reporting config stored on {@link SdkConfig}. */
export interface ReportingConfig {
  readonly autoReport: AutoReportMode;
  readonly sendCapturedExceptions: boolean;
  readonly dedupWindowMs: number;
  readonly maxAutoReportsPerMinute: number;
  readonly maxAutoReportsPerSession: number;
  /** Omitted when no hook was configured. */
  readonly beforeSend?: BeforeSendHook;
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
  /** Optional automatic-diagnostics controls (Phase 5.2). */
  readonly diagnostics?: DiagnosticsOptions;
  /** Optional automatic-reporting controls (Phase 7). */
  readonly reporting?: ReportingOptions;
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
  /** Normalised automatic-diagnostics config. Always present. */
  readonly diagnostics: DiagnosticsConfig;
  /** Normalised automatic-reporting config. Always present. */
  readonly reporting: ReportingConfig;
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
 * Normalises `diagnostics.ignoreUrls`. String and RegExp entries are kept;
 * anything else is ignored with at most one warning.
 */
function normaliseIgnoreUrls(
  value: unknown,
): readonly (string | RegExp)[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value)) return undefined;
  const valid: (string | RegExp)[] = [];
  for (const entry of value) {
    if (typeof entry === 'string' || entry instanceof RegExp) valid.push(entry);
  }
  return valid.length > 0 ? valid : undefined;
}

/**
 * Normalises `diagnostics` options. Every field falls back to a safe default;
 * at most one warning is logged for the whole block.
 */
function normaliseDiagnostics(value: unknown): DiagnosticsConfig {
  const defaults: DiagnosticsConfig = {
    maxEvents: DEFAULT_MAX_EVENTS,
    maxAgeMs: DEFAULT_MAX_AGE_MS,
    captureUncaughtErrors: true,
    captureUnhandledRejections: true,
    captureNavigation: true,
    captureInteractions: true,
    captureNetwork: true,
    captureElementText: false,
    includeInReports: true,
  };
  if (value === undefined) return defaults;
  if (value === null || typeof value !== 'object') {
    warn('init() option "diagnostics" must be an object; using defaults.');
    return defaults;
  }

  const options = value as DiagnosticsOptions;
  const config: {
    maxEvents: number;
    maxAgeMs: number;
    captureUncaughtErrors: boolean;
    captureUnhandledRejections: boolean;
    captureNavigation: boolean;
    captureInteractions: boolean;
    captureNetwork: boolean;
    captureElementText: boolean;
    includeInReports: boolean;
    ignoreUrls?: readonly (string | RegExp)[];
  } = { ...defaults };
  let warned = false;

  if (options.maxEvents !== undefined) {
    if (typeof options.maxEvents === 'number' && Number.isInteger(options.maxEvents) && options.maxEvents > 0) {
      config.maxEvents = Math.min(options.maxEvents, MAX_EVENTS_CEILING);
    } else {
      warn('init() option "diagnostics.maxEvents" must be a positive integer; using default.');
      warned = true;
    }
  }
  if (options.maxAgeMs !== undefined) {
    if (typeof options.maxAgeMs === 'number' && Number.isFinite(options.maxAgeMs) && options.maxAgeMs > 0) {
      config.maxAgeMs = clampMaxAgeMs(options.maxAgeMs);
    } else {
      warn('init() option "diagnostics.maxAgeMs" must be a positive number; using default.');
      warned = true;
    }
  }

  const booleans: readonly (keyof DiagnosticsOptions & keyof typeof config)[] = [
    'captureUncaughtErrors',
    'captureUnhandledRejections',
    'captureNavigation',
    'captureInteractions',
    'captureNetwork',
    'captureElementText',
    'includeInReports',
  ];
  for (const key of booleans) {
    const optionValue = options[key];
    if (optionValue === undefined) continue;
    if (typeof optionValue === 'boolean') {
      (config as Record<string, unknown>)[key] = optionValue;
    } else if (!warned) {
      warn(`init() option "diagnostics.${String(key)}" must be a boolean; using default.`);
      warned = true;
    }
  }

  if (options.ignoreUrls !== undefined) {
    if (!Array.isArray(options.ignoreUrls)) {
      warn('init() option "diagnostics.ignoreUrls" must be an array; ignored.');
      warned = true;
    } else {
      const ignoreUrls = normaliseIgnoreUrls(options.ignoreUrls);
      if (ignoreUrls !== undefined) config.ignoreUrls = ignoreUrls;
    }
  }

  return config;
}

const AUTO_REPORT_MODES: readonly AutoReportMode[] = ['off', 'errors', 'all'];

function isAutoReportMode(value: unknown): value is AutoReportMode {
  return typeof value === 'string' && (AUTO_REPORT_MODES as readonly string[]).includes(value);
}

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

/**
 * Normalises `reporting` options. Every field falls back to a safe default and
 * at most one warning is logged for the whole block; validation never throws.
 */
function normaliseReporting(value: unknown): ReportingConfig {
  const defaults: ReportingConfig = {
    autoReport: 'off',
    sendCapturedExceptions: true,
    dedupWindowMs: DEFAULT_DEDUP_WINDOW_MS,
    maxAutoReportsPerMinute: DEFAULT_MAX_AUTO_PER_MINUTE,
    maxAutoReportsPerSession: DEFAULT_MAX_AUTO_PER_SESSION,
  };
  if (value === undefined) return defaults;
  if (value === null || typeof value !== 'object') {
    warn('init() option "reporting" must be an object; using defaults.');
    return defaults;
  }

  const options = value as ReportingOptions;
  const config: {
    autoReport: AutoReportMode;
    sendCapturedExceptions: boolean;
    dedupWindowMs: number;
    maxAutoReportsPerMinute: number;
    maxAutoReportsPerSession: number;
    beforeSend?: BeforeSendHook;
  } = { ...defaults };
  let warned = false;

  if (options.autoReport !== undefined) {
    if (isAutoReportMode(options.autoReport)) {
      config.autoReport = options.autoReport;
    } else {
      warn('init() option "reporting.autoReport" must be "off", "errors" or "all"; using "off".');
      warned = true;
    }
  }

  if (options.sendCapturedExceptions !== undefined) {
    if (typeof options.sendCapturedExceptions === 'boolean') {
      config.sendCapturedExceptions = options.sendCapturedExceptions;
    } else if (!warned) {
      warn('init() option "reporting.sendCapturedExceptions" must be a boolean; using default.');
      warned = true;
    }
  }

  if (options.dedupWindowMs !== undefined) {
    if (typeof options.dedupWindowMs === 'number' && Number.isFinite(options.dedupWindowMs) && options.dedupWindowMs > 0) {
      config.dedupWindowMs = clampNumber(options.dedupWindowMs, MIN_DEDUP_WINDOW_MS, MAX_DEDUP_WINDOW_MS);
    } else if (!warned) {
      warn('init() option "reporting.dedupWindowMs" must be a positive number; using default.');
      warned = true;
    }
  }

  if (options.maxAutoReportsPerMinute !== undefined) {
    if (typeof options.maxAutoReportsPerMinute === 'number' && Number.isInteger(options.maxAutoReportsPerMinute) && options.maxAutoReportsPerMinute > 0) {
      config.maxAutoReportsPerMinute = Math.min(options.maxAutoReportsPerMinute, MAX_AUTO_PER_MINUTE_CEILING);
    } else if (!warned) {
      warn('init() option "reporting.maxAutoReportsPerMinute" must be a positive integer; using default.');
      warned = true;
    }
  }

  if (options.maxAutoReportsPerSession !== undefined) {
    if (typeof options.maxAutoReportsPerSession === 'number' && Number.isInteger(options.maxAutoReportsPerSession) && options.maxAutoReportsPerSession > 0) {
      config.maxAutoReportsPerSession = Math.min(options.maxAutoReportsPerSession, MAX_AUTO_PER_SESSION_CEILING);
    } else if (!warned) {
      warn('init() option "reporting.maxAutoReportsPerSession" must be a positive integer; using default.');
      warned = true;
    }
  }

  if (options.beforeSend !== undefined) {
    if (typeof options.beforeSend === 'function') {
      config.beforeSend = options.beforeSend;
    } else if (!warned) {
      warn('init() option "reporting.beforeSend" must be a function; ignored.');
      warned = true;
    }
  }

  return config;
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
    diagnostics: DiagnosticsConfig;
    reporting: ReportingConfig;
    onEvent?: SdkEventListener;
  } = {
    endpoint: options.endpoint,
    timeoutMs: normaliseTimeout(options.timeoutMs),
    shortcut: normaliseShortcut(options.shortcut, detectMac()),
    launcher: normaliseLauncher(options.launcher),
    screenshot: normaliseScreenshot(options.screenshot),
    diagnostics: normaliseDiagnostics(options.diagnostics),
    reporting: normaliseReporting(options.reporting),
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
