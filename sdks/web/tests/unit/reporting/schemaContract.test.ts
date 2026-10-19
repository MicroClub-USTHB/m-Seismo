import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import Ajv from 'ajv';
import { beforeAll, describe, expect, it } from 'vitest';
import { processReport } from '@sdk/reporting/processor';
import type { ProcessReportInput, ReportProcessorDeps } from '@sdk/reporting/processor';
import { ReportLimiter } from '@sdk/reporting/limiter';
import type { BeforeSendHook, ReportingConfig, SdkConfig } from '@sdk/core/config';
import type { DiagnosticsSelection } from '@sdk/diagnostics/select';
import type { ErrorDiagnosticEvent } from '@sdk/diagnostics/types';
import type { ReportContext } from '@sdk/core/types';
import type { SanitizedReport } from '@sdk/privacy/sanitize';
import type { SendResult } from '@sdk/transport/types';

const ISO = '2026-10-19T12:00:00.000Z';
const NOW = Date.parse(ISO);

const SCHEMA_PATH = fileURLToPath(new URL('../../../docs/report.schema.json', import.meta.url));
let validate: (report: unknown) => boolean;

beforeAll(() => {
  const schema = JSON.parse(readFileSync(SCHEMA_PATH, 'utf8'));
  validate = new Ajv({ allErrors: true, strict: false }).compile(schema);
});

function makeConfig(opts: { beforeSend?: BeforeSendHook } = {}): SdkConfig {
  const reporting: ReportingConfig = {
    autoReport: 'off',
    sendCapturedExceptions: true,
    dedupWindowMs: 60_000,
    maxAutoReportsPerMinute: 5,
    maxAutoReportsPerSession: 50,
    ...(opts.beforeSend !== undefined ? { beforeSend: opts.beforeSend } : {}),
  };
  return {
    endpoint: 'https://receiver.test/api/reports',
    timeoutMs: 10_000,
    shortcut: false,
    launcher: { enabled: false },
    screenshot: true,
    diagnostics: {
      maxEvents: 100,
      maxAgeMs: 600_000,
      captureUncaughtErrors: true,
      captureUnhandledRejections: true,
      captureNavigation: true,
      captureInteractions: true,
      captureNetwork: true,
      captureElementText: false,
      includeInReports: true,
    },
    reporting,
  };
}

const CONTEXT: ReportContext = { url: 'https://app.test/checkout', timestamp: ISO, application: {} };

const EMPTY: DiagnosticsSelection = { breadcrumbs: [], networkFailures: [], error: null };

function makeEvent(): ErrorDiagnosticEvent {
  return {
    kind: 'error',
    id: 'e-1',
    timestamp: ISO,
    handled: false,
    error: { type: 'TypeError', message: 'boom', stack: 'TypeError: boom\n    at save (/app/a.js:1:1)' },
  };
}

function makeDeps(selection: DiagnosticsSelection = EMPTY) {
  const sent: SanitizedReport[] = [];
  const deps: ReportProcessorDeps = {
    now: () => NOW,
    createId: () => 'r-1',
    getConfig: () => makeConfig(),
    collectContext: () => CONTEXT,
    selectDiagnostics: () => selection,
    send: async (report): Promise<SendResult> => {
      sent.push(report);
      return { ok: true, status: 201, id: 'srv-1' };
    },
    limiter: new ReportLimiter({ dedupWindowMs: 60_000, maxPerMinute: 5, maxPerSession: 50 }),
    warn: () => undefined,
  };
  return { deps, sent };
}

const ERROR_SELECTION: DiagnosticsSelection = {
  breadcrumbs: [],
  networkFailures: [],
  error: { type: 'Error', message: 'recent', stack: 'Error: recent\n    at f (/app/f.js:1:1)', handled: false, timestamp: ISO },
};

async function produce(input: ProcessReportInput, selection?: DiagnosticsSelection): Promise<unknown> {
  const { deps, sent } = makeDeps(selection);
  await processReport(input, deps);
  expect(sent).toHaveLength(1);
  return sent[0];
}

describe('docs/report.schema.json contract', () => {
  it('accepts a manual report (error:null, non-null description)', async () => {
    const report = await produce({ source: 'form', description: 'broken' });
    expect(validate(report)).toBe(true);
  });

  it('accepts a manual_with_error report', async () => {
    const report = await produce({ source: 'form', description: 'broken' }, ERROR_SELECTION);
    expect(validate(report)).toBe(true);
  });

  it('accepts an automatic_error report (null description)', async () => {
    const report = await produce({ source: 'automatic', event: makeEvent() });
    expect(validate(report)).toBe(true);
  });

  it('accepts a captured_exception report', async () => {
    const report = await produce({ source: 'api', event: { ...makeEvent(), handled: true } });
    expect(validate(report)).toBe(true);
  });

  it('accepts a captured_message report', async () => {
    const report = await produce({ source: 'api', message: 'checkout stalled' });
    expect(validate(report)).toBe(true);
  });

  it('rejects a manual report with a null description', () => {
    expect(
      validate({
        schemaVersion: 2,
        sdkVersion: '0.0.0',
        reportId: 'r-1',
        type: 'manual',
        trigger: 'form',
        description: null,
        error: null,
        context: CONTEXT,
        breadcrumbs: [],
        networkFailures: [],
        screenshot: null,
      }),
    ).toBe(false);
  });

  it('rejects a malformed fingerprint', () => {
    expect(
      validate({
        schemaVersion: 2,
        sdkVersion: '0.0.0',
        reportId: 'r-1',
        type: 'automatic_error',
        trigger: 'automatic',
        description: null,
        error: { type: 'Error', message: 'x', fingerprint: 'NOTHEX' },
        context: CONTEXT,
        breadcrumbs: [],
        networkFailures: [],
        screenshot: null,
      }),
    ).toBe(false);
  });

  it('rejects an unknown type', () => {
    expect(
      validate({
        schemaVersion: 2,
        sdkVersion: '0.0.0',
        reportId: 'r-1',
        type: 'mystery',
        trigger: 'api',
        description: null,
        error: null,
        context: CONTEXT,
        breadcrumbs: [],
        networkFailures: [],
        screenshot: null,
      }),
    ).toBe(false);
  });

  it('rejects a wrong schemaVersion', () => {
    expect(
      validate({
        schemaVersion: 1,
        sdkVersion: '0.0.0',
        reportId: 'r-1',
        type: 'manual',
        trigger: 'form',
        description: 'x',
        error: null,
        context: CONTEXT,
        breadcrumbs: [],
        networkFailures: [],
        screenshot: null,
      }),
    ).toBe(false);
  });
});
