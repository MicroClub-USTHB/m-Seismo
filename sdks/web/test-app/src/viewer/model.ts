/**
 * Report-to-view-model mapping for the test-app "Output" viewer.
 *
 * Pure functions only: they take the EXISTING stored report object (the receiver
 * returns exactly what the SDK sent) and derive a presentation model. Nothing
 * here mutates the payload, adds fields, or re-implements privacy — the viewer
 * is read-only presentation.
 */
import type { ReportPayload } from '@sdk/core/types';

export type ReportTypeKey =
  | 'manual'
  | 'manual_with_error'
  | 'automatic_error'
  | 'captured_exception'
  | 'captured_message'
  | 'unknown';

export interface TypePresentation {
  readonly key: ReportTypeKey;
  readonly label: string;
  readonly tone: 'info' | 'warn' | 'fail' | 'neutral';
}

const TYPE_LABELS: Record<string, TypePresentation> = {
  manual: { key: 'manual', label: 'Manual report', tone: 'info' },
  manual_with_error: { key: 'manual_with_error', label: 'Manual report with error', tone: 'warn' },
  automatic_error: { key: 'automatic_error', label: 'Automatic error', tone: 'fail' },
  captured_exception: { key: 'captured_exception', label: 'Captured exception', tone: 'warn' },
  captured_message: { key: 'captured_message', label: 'Captured message', tone: 'neutral' },
};

/** Maps a raw `type` string to a label + tone, defaulting to `unknown`. */
export function presentType(type: unknown): TypePresentation {
  if (typeof type === 'string' && type in TYPE_LABELS) {
    return TYPE_LABELS[type]!;
  }
  return { key: 'unknown', label: typeof type === 'string' ? type : 'unknown', tone: 'neutral' };
}

export interface KeyValue {
  readonly key: string;
  readonly value: string;
  readonly absent: boolean;
}

/** One entry per context field; absent fields are marked, never hidden. */
export function contextEntries(report: ReportPayload): KeyValue[] {
  const context = report.context;
  const browser = context.browser;
  const browserValue =
    browser === undefined
      ? undefined
      : [browser.name, browser.version].filter((part) => typeof part === 'string' && part.length > 0).join(' ');

  const viewport = context.viewport;
  const viewportValue =
    viewport === undefined ? undefined : `${viewport.width} × ${viewport.height}`;

  const entries: Array<[string, string | undefined]> = [
    ['URL', context.url],
    ['Timestamp', context.timestamp],
    ['Browser', browserValue],
    ['Platform', context.platform],
    ['Viewport', viewportValue],
    ['App version', context.application.version],
    ['Environment', context.application.environment],
  ];

  return entries.map(([key, value]) => ({
    key,
    value: typeof value === 'string' && value.length > 0 ? value : 'not available',
    absent: !(typeof value === 'string' && value.length > 0),
  }));
}

export interface HeaderModel {
  readonly type: TypePresentation;
  readonly trigger: string;
  readonly reportId: string;
  readonly schemaVersion: string;
  readonly sdkVersion: string;
  readonly timestamp: string;
}

/** Header card values, all derived from the stored report. */
export function headerModel(report: ReportPayload): HeaderModel {
  return {
    type: presentType(report.type),
    trigger: report.trigger ?? 'unknown',
    reportId: report.reportId,
    schemaVersion: String(report.schemaVersion),
    sdkVersion: report.sdkVersion,
    timestamp: report.context.timestamp,
  };
}

export interface ErrorModel {
  readonly present: boolean;
  readonly type: string;
  readonly message: string;
  readonly stack: string | null;
  readonly flags: string[];
  readonly fingerprint: string | null;
  readonly occurrences: number | null;
}

export function errorModel(report: ReportPayload): ErrorModel {
  const error = report.error;
  if (error === null || error === undefined) {
    return {
      present: false,
      type: '',
      message: '',
      stack: null,
      flags: [],
      fingerprint: null,
      occurrences: null,
    };
  }
  return {
    present: true,
    type: error.type,
    message: error.message,
    stack: error.stack ?? null,
    flags: Array.isArray(error.flags) ? [...error.flags] : [],
    fingerprint: error.fingerprint ?? null,
    occurrences: typeof error.occurrences === 'number' ? error.occurrences : null,
  };
}

export interface BreadcrumbModel {
  readonly timestamp: string;
  readonly category: string;
  readonly message: string;
  readonly icon: string;
  readonly level: string | null;
  readonly data: Record<string, unknown> | null;
}

const CATEGORY_ICONS: Record<string, string> = {
  error: '!',
  rejection: '!',
  interaction: '▸',
  navigation: '⇢',
  network: '⇄',
  console: '›',
  sdk: '◆',
  ui: '◇',
};

export function presentBreadcrumbs(report: ReportPayload): BreadcrumbModel[] {
  return report.breadcrumbs.map((crumb) => ({
    timestamp: crumb.timestamp,
    category: crumb.category,
    message: crumb.message,
    icon: CATEGORY_ICONS[crumb.category] ?? '•',
    level: typeof crumb.level === 'string' ? crumb.level : null,
    data: (crumb.data as Record<string, unknown> | undefined) ?? null,
  }));
}

/**
 * Index of the breadcrumb closest before the error timestamp, or -1. Both the
 * breadcrumb and error timestamps are ISO strings from the SDK.
 */
export function highlightIndex(report: ReportPayload): number {
  const error = report.error;
  if (error === null || error === undefined || error.timestamp === undefined) return -1;
  const errorTime = Date.parse(error.timestamp);
  if (Number.isNaN(errorTime)) return -1;
  let best = -1;
  for (let index = 0; index < report.breadcrumbs.length; index += 1) {
    const time = Date.parse(report.breadcrumbs[index]!.timestamp);
    if (!Number.isNaN(time) && time <= errorTime) best = index;
  }
  return best;
}

export interface NetworkModel {
  readonly method: string;
  readonly url: string;
  readonly status: string;
  readonly durationMs: number | null;
  readonly category: string;
}

export function presentNetworkFailures(report: ReportPayload): NetworkModel[] {
  return report.networkFailures.map((failure) => ({
    method: failure.method ?? 'GET',
    url: failure.url,
    status: typeof failure.status === 'number' ? String(failure.status) : '—',
    durationMs: typeof failure.durationMs === 'number' ? failure.durationMs : null,
    category: failure.category ?? 'unknown',
  }));
}

export interface ScreenshotModel {
  readonly present: boolean;
  readonly name: string;
  readonly mimeType: string;
  readonly sizeBytes: number;
  readonly dataUrl: string | null;
  readonly dataBase64Length: number;
}

/**
 * Screenshot metadata. `dataBase64` may have been replaced by the receiver's
 * `redactScreenshot()` (a `dataBase64Length` number), so both shapes are read.
 */
export function screenshotModel(report: ReportPayload): ScreenshotModel {
  const screenshot = report.screenshot;
  if (screenshot === null || screenshot === undefined) {
    return {
      present: false,
      name: '',
      mimeType: '',
      sizeBytes: 0,
      dataUrl: null,
      dataBase64Length: 0,
    };
  }
  const record = screenshot as unknown as Record<string, unknown>;
  const dataBase64 = typeof record['dataBase64'] === 'string' ? record['dataBase64'] : null;
  const length =
    typeof record['dataBase64Length'] === 'number'
      ? record['dataBase64Length']
      : typeof dataBase64 === 'string'
        ? dataBase64.length
        : 0;
  return {
    present: true,
    name: screenshot.name,
    mimeType: screenshot.mimeType,
    sizeBytes: screenshot.sizeBytes,
    dataUrl: dataBase64 === null ? null : `data:${screenshot.mimeType};base64,${dataBase64}`,
    dataBase64Length: length,
  };
}

export interface ReportViewModel {
  readonly header: HeaderModel;
  readonly hasDescription: boolean;
  readonly description: string;
  readonly error: ErrorModel;
  readonly context: KeyValue[];
  readonly breadcrumbs: BreadcrumbModel[];
  readonly highlight: number;
  readonly networkFailures: NetworkModel[];
  readonly screenshot: ScreenshotModel;
}

/** The full presentation model for one stored report. */
export function toViewModel(report: ReportPayload): ReportViewModel {
  const description = report.description ?? '';
  return {
    header: headerModel(report),
    hasDescription: description.length > 0,
    description,
    error: errorModel(report),
    context: contextEntries(report),
    breadcrumbs: presentBreadcrumbs(report),
    highlight: highlightIndex(report),
    networkFailures: presentNetworkFailures(report),
    screenshot: screenshotModel(report),
  };
}
