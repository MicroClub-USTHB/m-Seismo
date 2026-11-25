/**
 * Network detection scenarios: E09–E11.
 */
import { captureException } from '@m-seismo/web';
import type { NetworkDiagnosticEvent } from '@sdk/diagnostics/types';
import { waitForEvent, waitForStoredCount, storedReports } from '../helpers/reports';
import { waitFor } from '../helpers/waiters';
import { fail, pass } from '../verdict';
import type { LabScenario, Verdict } from '../types';

function isNetworkEvent(event: { kind: string }): event is NetworkDiagnosticEvent {
  return event.kind === 'network';
}

// --- E09 -------------------------------------------------------------------
const e09 = { appStatus: -1, httpErrorEvent: false, reportFailures: -1 };

const e09Scenario: LabScenario = {
  id: 'E09',
  title: 'fetch to a 500 endpoint',
  group: 'network',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'The app still receives the 500 Response; the SDK records an http_error network event that is attached to a report as networkFailures.',
  config: { reporting: { autoReport: 'off', sendCapturedExceptions: true } },
  async run(ctx) {
    const response = await fetch(`${ctx.receiverBase}/api/status/500`);
    e09.appStatus = response.status;
    e09.httpErrorEvent =
      (await waitForEvent(
        ctx,
        (event) =>
          isNetworkEvent(event) &&
          event.data.category === 'http_error' &&
          event.data.status === 500,
      )) !== undefined;

    captureException(new Error('E09 after http 500'));
    await waitForStoredCount(ctx, 1);
  },
  async verify(ctx): Promise<Verdict> {
    const reports = await storedReports(ctx);
    const failures = reports[0]?.networkFailures as ReadonlyArray<{ status?: number; category?: string }> | undefined;
    e09.reportFailures = failures?.length ?? -1;
    const hasFailure = failures?.some((failure) => failure.status === 500 && failure.category === 'http_error') ?? false;
    const ok = e09.appStatus === 500 && e09.httpErrorEvent && hasFailure;
    return ok
      ? pass('app received HTTP 500; http_error recorded and attached as networkFailures', { ...e09 })
      : fail('unexpected E09 outcome', { ...e09, failures });
  },
};

// --- E10 -------------------------------------------------------------------
const e10 = { appRejected: false, closedPortEvent: false, corsEvent: false };

const e10Scenario: LabScenario = {
  id: 'E10',
  title: 'Offline / closed port / CORS-blocked fetch',
  group: 'network',
  mode: 'assisted',
  expected: 'LIMITED',
  notes:
    'All three causes surface as the SAME generic network_error; the SDK does not claim a cause. Offline vs CORS vs DNS is indistinguishable in the browser. ' +
    'Run with DevTools "Offline" for the true-offline path; the automated path uses a closed port and a CORS-blocked URL.',
  manualSteps: [
    'Open DevTools → Network → set throttling to "Offline".',
    'Click Run on E10, observe an http_error-free network_error is buffered.',
    'Reload and set throttling back to "No throttling".',
  ],
  config: { reporting: { autoReport: 'off' } },
  async run(ctx) {
    try {
      await fetch('http://127.0.0.1:1/closed-port', { mode: 'no-cors' });
    } catch {
      e10.appRejected = true;
    }
    e10.closedPortEvent =
      (await waitForEvent(
        ctx,
        (event) => isNetworkEvent(event) && event.data.category === 'network_error' && event.data.url.includes('127.0.0.1'),
      )) !== undefined;

    try {
      await fetch(`${ctx.receiverBase}/api/no-cors`);
    } catch {
      // expected CORS rejection
    }
    e10.corsEvent =
      (await waitForEvent(
        ctx,
        (event) => isNetworkEvent(event) && event.data.category === 'network_error' && event.data.url.includes('no-cors'),
      )) !== undefined;
  },
  async verify(): Promise<Verdict> {
    const ok = e10.appRejected && e10.closedPortEvent && e10.corsEvent;
    return ok
      ? pass('closed port and CORS both produce generic network_error; app rejection observed; cause not claimed', { ...e10 })
      : fail('unexpected E10 outcome', { ...e10 });
  },
};

// --- E11 -------------------------------------------------------------------
const e11 = { failingStatus: -1, failingDuration: -1, failureRecorded: false, slowSuccessRecorded: -1 };

const e11Scenario: LabScenario = {
  id: 'E11',
  title: 'Slow request: failing recorded, successful not',
  group: 'network',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes:
    'A slow FAILING request is recorded with durationMs (~the delay, tolerance 250 ms–3 s). ' +
    'A slow SUCCESSFUL request is NOT recorded (Phase 5.3 only records failures / opaque responses).',
  config: { reporting: { autoReport: 'off' } },
  async run(ctx) {
    ctx.clearDiagnostics();

    const started = performance.now();
    const failing = await fetch(`${ctx.receiverBase}/api/slow?ms=700&status=503`);
    e11.failingStatus = failing.status;
    e11.failingDuration = performance.now() - started;
    e11.failureRecorded =
      (await waitForEvent(
        ctx,
        (event) => isNetworkEvent(event) && event.data.category === 'http_error' && event.data.url.includes('slow'),
      )) !== undefined;

    const success = await fetch(`${ctx.receiverBase}/api/slow?ms=500`);
    e11.failingDuration = Math.round(e11.failingDuration);
    void success;
    await waitFor(() => false, 900);
    e11.slowSuccessRecorded = ctx
      .snapshot()
      .filter((event) => isNetworkEvent(event) && event.data.url.includes('/api/slow') && !event.data.url.includes('status='))
      .length;
  },
  async verify(): Promise<Verdict> {
    const durationOk = e11.failingDuration >= 250 && e11.failingDuration <= 3000;
    const ok = e11.failingStatus === 503 && durationOk && e11.failureRecorded && e11.slowSuccessRecorded === 0;
    return ok
      ? pass('slow failing request recorded with matching duration; slow success not recorded', { ...e11 })
      : fail('unexpected E11 outcome', { ...e11 });
  },
};

export const networkScenarios: readonly LabScenario[] = [e09Scenario, e10Scenario, e11Scenario];
