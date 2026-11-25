/**
 * Reporting scenarios: E06, E14–E17, E20–E22.
 */
import { captureException, captureMessage, sendReport, sanitize, buildReport, getContext } from '@m-seismo/web';
import { fileToAttachment } from '@sdk/form/screenshot';
import { validateDescription } from '@sdk/form/validation';
import { FAKE_SECRETS, SCRUBBED_SECRETS, UNSCRUBBED_SECRETS } from '../fixtures/secrets';
import {
  clickSend,
  fillAndSubmit,
  formDescription,
  formMessage,
  formSendLabel,
  openFillSubmit,
  openFormAndWait,
  oversizedPngFile,
  pickFile,
  pngFile,
  cancelForm,
  waitForFormClosed,
} from '../helpers/form';
import { json, storedReports, waitForEvent, waitForStoredCount } from '../helpers/reports';
import { setLabEventSink } from '../helpers/init';
import { waitFor } from '../helpers/waiters';
import { fail, info, pass } from '../verdict';
import type { LabScenario, Verdict } from '../types';

// --- E06 -------------------------------------------------------------------
const e06 = { message: '', stored: 0 };

const e06Scenario: LabScenario = {
  id: 'E06',
  title: 'Manual report via the form (no exception)',
  group: 'reporting',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'Drives the real shadow-DOM form. The stored report is type "manual", trigger "form", error null, with the description present.',
  config: { reporting: { autoReport: 'off' } },
  async run(ctx) {
    await openFillSubmit('E06 manual report, nothing broke');
    await waitForStoredCount(ctx, 1);
    e06.message = formMessage();
    e06.stored = (await ctx.receiver.getReports()).count;
  },
  async verify(ctx): Promise<Verdict> {
    const report = (await storedReports(ctx))[0];
    const ok =
      report?.type === 'manual' &&
      report?.trigger === 'form' &&
      report?.error === null &&
      typeof report?.description === 'string' &&
      report.description.includes('E06');
    return ok
      ? pass('manual form report stored as type=manual, trigger=form, error=null', { description: report?.description })
      : fail('unexpected E06 report shape', { report });
  },
};

// --- E14 -------------------------------------------------------------------
function e14PathA(): void {
  captureException(new Error('E14 same message'));
}
function e14PathB(): void {
  captureException(new Error('E14 same message'));
}

const e14 = { first: 0, second: 0, fingerprints: [] as Array<string | undefined>, suppressedDuplicate: 0 };

const e14Scenario: LabScenario = {
  id: 'E14',
  title: 'Same error 500× from one path; a second path differs',
  group: 'reporting',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'Repeats inside the dedup window collapse to one report per fingerprint; the second code path has a different stack and thus a different fingerprint.',
  config: { reporting: { autoReport: 'off', sendCapturedExceptions: true } },
  async run(ctx) {
    for (let i = 0; i < 500; i += 1) e14PathA();
    await waitForStoredCount(ctx, 1);
    e14PathB();
    await waitForStoredCount(ctx, 2);
    const reports = await storedReports(ctx);
    e14.first = reports[0]?.error?.occurrences ?? -1;
    e14.second = reports[1]?.error?.occurrences ?? -1;
    e14.fingerprints = reports.map((report) => report.error?.fingerprint);
    e14.suppressedDuplicate = ctx.stats().suppressed.duplicate;
    void ctx.receiver.getStats();
  },
  async verify(ctx): Promise<Verdict> {
    const stats = await ctx.receiver.getStats();
    const ok =
      e14.fingerprints.length === 2 &&
      e14.fingerprints[0] !== undefined &&
      e14.fingerprints[1] !== undefined &&
      e14.fingerprints[0] !== e14.fingerprints[1] &&
      e14.first === 1 &&
      Object.keys(stats.byFingerprint).length === 2;
    return ok
      ? pass('deduped 500 repeats; one fingerprint per code path; occurrences counted', { ...e14, byFingerprint: Object.keys(stats.byFingerprint) })
      : fail('unexpected E14 outcome', { ...e14, stats });
  },
};

// --- E15 -------------------------------------------------------------------
const e15 = { leaked: [] as string[], redacted: [] as string[], vectors: {} as Record<string, boolean> };

const e15Scenario: LabScenario = {
  id: 'E15',
  title: 'Sensitive information handling',
  group: 'reporting',
  mode: 'inpage',
  expected: 'KNOWN_GAP',
  notes:
    'Plants fake secrets in description, error message, captureException context, URL query, Authorization header, request body, cookie and localStorage. ' +
    'The BASELINE scrubber only targets some shapes and never scrubs the user description — Phase 6 must handle the rest. A leak is never reported as pass.',
  config: { reporting: { autoReport: 'off', sendCapturedExceptions: true } },
  async run(ctx) {
    history.replaceState(null, '', `${location.pathname}?token=${FAKE_SECRETS.longToken}&page=1`);
    document.cookie = `mseismo_lab=${FAKE_SECRETS.longToken}`;
    try {
      localStorage.setItem('mseismo_lab', FAKE_SECRETS.jwt);
    } catch {
      // storage may be unavailable; not important
    }

    await fetch(`${ctx.receiverBase}/api/echo`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: FAKE_SECRETS.bearer },
      body: JSON.stringify({ password: FAKE_SECRETS.prosePassword, token: FAKE_SECRETS.longToken }),
    }).catch(() => {});

    captureException(
      new Error(`E15 token=${FAKE_SECRETS.longToken} hex=${FAKE_SECRETS.longHex} jwt=${FAKE_SECRETS.jwt}`),
      { authorization: FAKE_SECRETS.bearer, api_key: 'abcd1234efgh5678ijkl9012mnop3456' },
    );
    await openFillSubmit(`E15 ${FAKE_SECRETS.prosePassword} password=${FAKE_SECRETS.keyValue} token=${FAKE_SECRETS.longToken}`);
    await waitForStoredCount(ctx, 2);
  },
  async verify(ctx): Promise<Verdict> {
    const reports = await storedReports(ctx, true);
    const blob = json({ reports, buffer: ctx.snapshot() });
    const leaked: string[] = [];
    for (const secret of SCRUBBED_SECRETS) {
      if (blob.includes(secret)) leaked.push(secret);
    }
    const unscrubbedLeaks = UNSCRUBBED_SECRETS.filter((secret) => blob.includes(secret));
    const descriptionLeaks = reports
      .filter((report) => typeof report.description === 'string')
      .some((report) => SCRUBBED_SECRETS.some((secret) => (report.description ?? '').includes(secret)));

    const vectors = {
      descriptionLeaks,
      headerInReport: blob.includes('Authorization') && blob.includes('sk-live'),
      bodyInReport: blob.includes(FAKE_SECRETS.prosePassword),
      cookieInReport: blob.includes('mseismo_lab='),
      storageInReport: blob.includes('localStorage'),
      urlTokenRedacted: !json(reports.map((report) => report.context)).includes(FAKE_SECRETS.longToken),
    };
    e15.leaked = [...leaked, ...unscrubbedLeaks];
    e15.vectors = vectors;

    const problemFound = leaked.length > 0 || unscrubbedLeaks.length > 0;
    const observed = problemFound
      ? `baseline scrubber leaves secrets exposed: ${[...new Set([...leaked, ...unscrubbedLeaks])].length} value(s); description is never scrubbed (Phase 6)`
      : 'no planted secret appeared in the stored reports';
    return info(observed, { leaked: [...new Set([...leaked, ...unscrubbedLeaks])], vectors });
  },
};

// --- E16 -------------------------------------------------------------------
const e16: Record<string, unknown> = {};

const e16Scenario: LabScenario = {
  id: 'E16',
  title: 'Large payload limits',
  group: 'reporting',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes:
    'Oversize description is rejected by validation; oversize/wrong-type screenshots are rejected inline; a huge context and 1000+ breadcrumbs are bounded; ' +
    'the receiver limit maps to a clear failure state. Per-field limits come from earlier phases; Phase 6 will finalise policy.',
  config: { reporting: { autoReport: 'off', sendCapturedExceptions: false } },
  async run(ctx) {
    e16['descriptionRejected'] = !validateDescription('x'.repeat(5001)).ok;

    // Huge captureException context: bounded by the context sanitiser.
    captureException(new Error('E16 huge context'), {
      blob: 'y'.repeat(2000),
      extra: Object.fromEntries(Array.from({ length: 40 }, (_value, index) => [`k${index}`, 'z'.repeat(600)])),
    });
    const event = await waitForEvent(
      ctx,
      (item) => item.kind === 'captured' && item.error.message.includes('E16 huge context'),
    );
    const capturedContext = event?.kind === 'captured' ? event.context : undefined;
    e16['contextKeys'] = capturedContext ? Object.keys(capturedContext).length : 0;
    e16['contextTruncatedFlag'] = event?.flags?.includes('truncated') ?? false;

    // 1000+ breadcrumbs: only the bounded tail is attached.
    for (let i = 0; i < 1000; i += 1) captureMessage(`E16 breadcrumb ${i}`);
    e16['buffered'] = ctx.count();
    ctx.init({ reporting: { autoReport: 'off', sendCapturedExceptions: true } });
    captureException(new Error('E16 report with breadcrumbs'));
    await waitForStoredCount(ctx, 1);
    const report = (await storedReports(ctx))[0];
    e16['reportBreadcrumbs'] = report?.breadcrumbs?.length ?? 0;

    // Screenshot limits via the real file input.
    await openFormAndWait();
    const wrong = new File(['not an image'], 'note.txt', { type: 'text/plain' });
    await pickFile(wrong);
    e16['wrongTypeMessage'] = formMessage();
    const big = oversizedPngFile();
    await pickFile(big);
    e16['oversizeMessage'] = formMessage();
    cancelForm();

    // Receiver 413 maps to a clear failure state.
    const direct = await fileToAttachment(pngFile('direct.png'));
    e16['directAttachmentOk'] = direct.ok;

    await ctx.receiver.setMode('fail413');
    await openFillSubmit('E16 receiver too large');
    await waitFor(() => formMessage().includes('413'), 3_000);
    e16['http413Message'] = formMessage();
    e16['http413TextPreserved'] = formDescription().includes('E16');
    await ctx.receiver.setMode('ok');
  },
  async verify(): Promise<Verdict> {
    const checks: Record<string, boolean> = {
      descriptionRejected: e16['descriptionRejected'] === true,
      contextBounded: typeof e16['contextKeys'] === 'number' && e16['contextKeys'] <= 20,
      breadcrumbsBounded: typeof e16['reportBreadcrumbs'] === 'number' && e16['reportBreadcrumbs'] <= 50 && e16['reportBreadcrumbs'] > 0,
      wrongTypeRejected: String(e16['wrongTypeMessage']).includes('PNG, JPEG, or WebP'),
      oversizeRejected: String(e16['oversizeMessage']).includes('smaller'),
      directAttachmentOk: e16['directAttachmentOk'] === true,
      http413Message: String(e16['http413Message']).includes('413'),
      http413TextPreserved: e16['http413TextPreserved'] === true,
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    return failed.length === 0
      ? pass('description/screenshot/context/breadcrumb limits hold and receiver 413 maps to a clear failure state', { ...e16, checks })
      : fail(`large-payload checks failed: ${failed.join(', ')}`, { ...e16, checks });
  },
};

// --- E17 -------------------------------------------------------------------
const e17: Record<string, unknown> = {};

async function attemptSend(ctx: Parameters<LabScenario['run']>[0], label: string): Promise<{ message: string; preserved: boolean }> {
  await openFormAndWait();
  const text = `E17 ${label} preserved text`;
  await fillAndSubmit(text);
  await waitFor(() => formMessage().length > 0 || formSendLabel() === 'Try again', 6_000);
  const message = formMessage();
  const preserved = formDescription() === text;
  await waitFor(() => false, 400);
  cancelForm();
  return { message, preserved };
}

const e17Scenario: LabScenario = {
  id: 'E17',
  title: 'Backend unavailable (drop, fail500, wrong port)',
  group: 'reporting',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'The form shows a clear failure, the typed text is preserved, the host stays usable, and there is NO automatic retry (Phase 8 is not built).',
  config: { reporting: { autoReport: 'off' } },
  async run(ctx) {
    await ctx.receiver.setMode('fail500');
    const fail500 = await attemptSend(ctx, 'fail500');
    e17['fail500Message'] = fail500.message;
    e17['fail500Preserved'] = fail500.preserved;
    const requestCountAfter = (await ctx.receiver.getStats()).requestCount;
    await ctx.receiver.setMode('ok');

    // No automatic retry: wait, then confirm the request count did not grow.
    await waitFor(() => false, 1_000);
    e17['requestStable'] = (await ctx.receiver.getStats()).requestCount === requestCountAfter;

    await ctx.receiver.setMode('offline');
    const drop = await attemptSend(ctx, 'drop');
    e17['dropMessage'] = drop.message;
    e17['dropPreserved'] = drop.preserved;
    await ctx.receiver.setMode('ok');

    const baseConfig = ctx.config();
    if (baseConfig !== null) {
      const report = buildReport({
        type: 'manual',
        trigger: 'form',
        description: 'E17 wrong port preserved text',
        context: getContext().context,
      });
      const result = await sendReport(sanitize(report), {
        ...baseConfig,
        endpoint: 'http://127.0.0.1:1/api/reports',
        timeoutMs: 1_500,
      });
      e17['wrongPortKind'] = result.ok ? 'ok' : result.kind;
    }
  },
  async verify(): Promise<Verdict> {
    const checks: Record<string, boolean> = {
      fail500Message: String(e17['fail500Message']).includes('HTTP 500'),
      fail500Preserved: e17['fail500Preserved'] === true,
      dropMessage: String(e17['dropMessage']).toLowerCase().includes('reach the server'),
      dropPreserved: e17['dropPreserved'] === true,
      noAutoRetry: e17['requestStable'] === true,
      wrongPortNetwork: e17['wrongPortKind'] === 'network',
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    return failed.length === 0
      ? pass('clear failure states, text preserved, no automatic retry', { ...e17, checks })
      : fail(`backend-unavailable checks failed: ${failed.join(', ')}`, { ...e17, checks });
  },
};

// --- E20 -------------------------------------------------------------------
const e20: Record<string, unknown> = {};

const e20Scenario: LabScenario = {
  id: 'E20',
  title: '500 distinct rapid errors',
  group: 'reporting',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'The buffer is bounded at maxEvents; automatic reports are capped per minute/session; suppressed counts are visible; the page stays responsive.',
  config: {
    reporting: { autoReport: 'errors', sendCapturedExceptions: true, maxAutoReportsPerMinute: 3, maxAutoReportsPerSession: 4 },
    diagnostics: { maxEvents: 20 },
  },
  async run(ctx) {
    const started = performance.now();
    for (let i = 0; i < 500; i += 1) captureException(new Error(`E20 distinct error ${i}`));
    e20['producedInMs'] = Math.round(performance.now() - started);

    await waitFor(() => ctx.stats().suppressed.duplicate + ctx.stats().suppressed.rate_limited > 0, 5_000);
    await waitFor(() => false, 500);

    const latencyStart = performance.now();
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
    e20['responsiveLagMs'] = Math.round(performance.now() - latencyStart);

    const stats = ctx.stats();
    e20['snapshotLength'] = ctx.count();
    e20['sessionSends'] = stats.sessionSends;
    e20['suppressed'] = stats.suppressed;
    e20['fingerprints'] = stats.fingerprints;
    e20['storedCount'] = (await ctx.receiver.getReports()).count;
  },
  async verify(): Promise<Verdict> {
    const stats = e20['suppressed'] as { duplicate: number; rate_limited: number } | undefined;
    const checks: Record<string, boolean> = {
      bufferBounded: typeof e20['snapshotLength'] === 'number' && e20['snapshotLength'] <= 20,
      sessionCapped: typeof e20['sessionSends'] === 'number' && e20['sessionSends'] <= 4,
      suppressionVisible: (stats?.duplicate ?? 0) + (stats?.rate_limited ?? 0) > 0,
      responsive: typeof e20['responsiveLagMs'] === 'number' && e20['responsiveLagMs'] < 2_000,
      fingerprintsBounded: typeof e20['fingerprints'] === 'number' && e20['fingerprints'] <= 100,
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    return failed.length === 0
      ? pass('buffer and send rate bounded, suppression visible, page responsive', { ...e20, checks })
      : fail(`rapid-error checks failed: ${failed.join(', ')}`, { ...e20, checks });
  },
};

// --- E21 -------------------------------------------------------------------
const e21 = { errorEvents: 0, clickBreadcrumb: false };

const e21Scenario: LabScenario = {
  id: 'E21',
  title: 'Logical bug (Save does nothing)',
  group: 'reporting',
  mode: 'inpage',
  expected: 'MANUAL_ONLY',
  notes: 'A logical bug produces no error and no buffer event; only a manual report captures it, and the click that triggered it is a breadcrumb.',
  config: { reporting: { autoReport: 'errors' } },
  async run(ctx) {
    ctx.clearDiagnostics();
    // The lab's "silent save" button: does nothing, records no error.
    document.querySelector<HTMLButtonElement>('[data-testid="lab-silent-save"]')?.click();
    await waitFor(() => false, 300);
    e21.errorEvents = ctx
      .snapshot()
      .filter((event) => event.kind === 'error' || event.kind === 'unhandledrejection')
      .length;

    await openFillSubmit('E21 Save did nothing — manual report');
    await waitForStoredCount(ctx, 1);
    const report = (await storedReports(ctx))[0];
    e21.clickBreadcrumb = (report?.breadcrumbs ?? []).some(
      (breadcrumb) => (breadcrumb as { category?: string }).category === 'interaction',
    );
  },
  async verify(): Promise<Verdict> {
    const ok = e21.errorEvents === 0 && e21.clickBreadcrumb;
    return ok
      ? pass('no error/buffer event; manual report stored; interaction breadcrumb present', { ...e21 })
      : fail('unexpected E21 outcome', { ...e21 });
  },
};

// --- E22 -------------------------------------------------------------------
const e22: Record<string, unknown> = {};

const e22Scenario: LabScenario = {
  id: 'E22',
  title: 'Form submission fails (500/413/drop/timeout) then recovers',
  group: 'reporting',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes:
    'Each failure kind shows a distinct clear message and preserves the typed text + attachment; Retry is user-triggered; the reportId is stable across retries; one report is stored after recovery. ' +
    'A 400 is unreachable from the form because client validation blocks it (recorded in evidence).',
  config: { reporting: { autoReport: 'off' }, timeoutMs: 2_000 },
  async run(ctx) {
    const starts: string[] = [];
    const successes: string[] = [];
    setLabEventSink((event) => {
      if (event.type === 'submit:start') starts.push(event.reportId);
      if (event.type === 'submit:success') successes.push(event.reportId);
    });
    ctx.init({ timeoutMs: 2_000 });

    // HTTP 500 then a user-triggered retry.
    await ctx.receiver.setMode('fail500');
    await openFillSubmit('E22 text preserved across retry');
    await waitFor(() => formMessage().includes('HTTP 500'), 4_000);
    e22['http500Message'] = formMessage();
    e22['textPreserved'] = formDescription() === 'E22 text preserved across retry';
    e22['retryLabel'] = formSendLabel();
    await ctx.receiver.setMode('ok');
    clickSend();
    await waitForStoredCount(ctx, 1);
    e22['storedAfterRecovery'] = (await ctx.receiver.getReports()).count;
    e22['reportIdStable'] = starts.length > 0 && successes.length > 0 && starts[0] === successes[0];
    // A successful send disables the buttons and auto-closes; wait for that.
    await waitForFormClosed();

    // HTTP 413.
    await ctx.receiver.setMode('fail413');
    await openFillSubmit('E22 too large');
    await waitFor(() => formMessage().includes('413'), 4_000);
    e22['http413Message'] = formMessage();
    cancelForm();
    await ctx.receiver.setMode('ok');

    // Drop (network).
    await ctx.receiver.setMode('offline');
    await openFillSubmit('E22 drop');
    await waitFor(() => formMessage().toLowerCase().includes('reach the server'), 4_000);
    e22['dropMessage'] = formMessage();
    cancelForm();
    await ctx.receiver.setMode('ok');

    // Timeout.
    ctx.init({ timeoutMs: 500 });
    await ctx.receiver.setMode('slow', { delayMs: 3_000 });
    await openFillSubmit('E22 timeout');
    await waitFor(() => formMessage().toLowerCase().includes('timed out'), 6_000);
    e22['timeoutMessage'] = formMessage();
    cancelForm();
    await ctx.receiver.setMode('ok');
    setLabEventSink(null);
  },
  async verify(): Promise<Verdict> {
    const checks: Record<string, boolean> = {
      http500: String(e22['http500Message']).includes('HTTP 500'),
      http413: String(e22['http413Message']).includes('HTTP 413'),
      drop: String(e22['dropMessage']).toLowerCase().includes('reach the server'),
      timeout: String(e22['timeoutMessage']).toLowerCase().includes('timed out'),
      textPreserved: e22['textPreserved'] === true,
      retryIsUserTriggered: String(e22['retryLabel']).toLowerCase().includes('try again'),
      reportIdStable: e22['reportIdStable'] === true,
      storedAfterRecovery: e22['storedAfterRecovery'] === 1,
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    return failed.length === 0
      ? pass('distinct failure messages, preserved text, stable reportId, one report after recovery', { ...e22, checks })
      : fail(`form-failure checks failed: ${failed.join(', ')}`, { ...e22, checks });
  },
};

export const reportingScenarios: readonly LabScenario[] = [
  e06Scenario,
  e14Scenario,
  e15Scenario,
  e16Scenario,
  e17Scenario,
  e20Scenario,
  e21Scenario,
  e22Scenario,
];
