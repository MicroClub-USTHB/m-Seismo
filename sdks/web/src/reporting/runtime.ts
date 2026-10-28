/**
 * Report runtime (Phase 7): binds the pure processor to real globals and owns
 * the single {@link ReportLimiter} and the automatic-path re-entrancy guard.
 *
 * The processor itself stays pure; everything ambient (`Date.now`, ids,
 * `getContext()`, the diagnostic buffer, the transport, the config) is injected
 * here. This is also the only place that decides *when* an automatic report is
 * attempted.
 */
import { getConfig } from '../core/config';
import type { SdkConfig } from '../core/config';
import { createReportId } from '../core/reportBuilder';
import { warn } from '../core/log';
import type { ReportContext } from '../core/types';
import { getContext } from '../context/getContext';
import { selectDiagnostics } from '../diagnostics/select';
import { recordDiagnostic, getDiagnosticsBuffer } from '../diagnostics/store';
import type { DiagnosticEvent, ErrorDiagnosticEvent } from '../diagnostics/types';
import { sendReport } from '../transport/sendReport';
import { processReport } from './processor';
import type { ProcessReportInput, ProcessResult } from './processor';
import { ReportLimiter } from './limiter';
import type { LimiterStats } from './limiter';

let limiter = new ReportLimiter({
  dedupWindowMs: 60_000,
  maxPerMinute: 5,
  maxPerSession: 50,
});
let processing = false;

/** Applies the reporting limits from a validated config (repeated init safe). */
export function configureReporting(config: SdkConfig): void {
  limiter.configure({
    dedupWindowMs: config.reporting.dedupWindowMs,
    maxPerMinute: config.reporting.maxAutoReportsPerMinute,
    maxPerSession: config.reporting.maxAutoReportsPerSession,
  });
}

/** Clears all dedup/limit state (called by `destroy()`). */
export function resetReportingState(): void {
  limiter.clear();
  processing = false;
}

function collectReportContext(reportId: string, config: SdkConfig): ReportContext {
  try {
    return getContext({
      reportId,
      ...(config.appVersion !== undefined ? { appVersion: config.appVersion } : {}),
      ...(config.environment !== undefined ? { environment: config.environment } : {}),
      ...(config.sensitiveQueryParams !== undefined
        ? { sensitiveQueryParams: config.sensitiveQueryParams }
        : {}),
    }).context;
  } catch {
    const application: { version?: string; environment?: string } = {};
    if (config.appVersion !== undefined) application.version = config.appVersion;
    if (config.environment !== undefined) application.environment = config.environment;
    return { timestamp: new Date().toISOString(), application };
  }
}

/** Runs the unified processor with the real, injected dependencies. */
export function processInput(input: ProcessReportInput): Promise<ProcessResult> {
  return processReport(input, {
    now: Date.now,
    createId: createReportId,
    getConfig,
    collectContext: collectReportContext,
    selectDiagnostics: (now, config) =>
      selectDiagnostics(getDiagnosticsBuffer()?.snapshot() ?? [], now, {
        includeInReports: config.diagnostics.includeInReports,
        maxAgeMs: config.diagnostics.maxAgeMs,
      }),
    send: sendReport,
    limiter,
    warn,
  });
}

function isAutoReportable(event: DiagnosticEvent): event is ErrorDiagnosticEvent {
  return event.kind === 'error' || event.kind === 'unhandledrejection';
}

/**
 * Auto-report an error/rejection when enabled. Guarded so a report produced
 * while another is being built/sent can never start a second one.
 */
function maybeAutoReport(event: DiagnosticEvent): void {
  if (processing) return;
  const config = getConfig();
  if (config === null || config.reporting.autoReport === 'off') return;
  if (!isAutoReportable(event)) return;
  processing = true;
  void processInput({ source: 'automatic', event }).finally(() => {
    processing = false;
  });
}

/**
 * Intake for the automatic collectors: records the event first, then (when
 * `autoReport` is enabled) sends an `automatic_error` report. Recording happens
 * even when reporting is off or throttled.
 */
export function handleAutomaticDiagnostic(event: DiagnosticEvent): void {
  recordDiagnostic(event);
  maybeAutoReport(event);
}

/** Test-only reporting stats. */
export function getReportingStats(): LimiterStats & { readonly processing: boolean } {
  return { ...limiter.stats(), processing };
}
