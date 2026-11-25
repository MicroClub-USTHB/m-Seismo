/**
 * Error detection scenarios: E01–E05, E07, E08, E12, E13.
 */
import { captureException, captureMessage } from '@m-seismo/web';
import type { ErrorDiagnosticEvent } from '@sdk/diagnostics/types';
import { FAKE_SECRETS } from '../fixtures/secrets';
import { json, waitForEvent, waitForStoredCount, storedReports } from '../helpers/reports';
import { waitFor } from '../helpers/waiters';
import { fail, pass } from '../verdict';
import type { LabScenario, Verdict } from '../types';

function isErrorEvent(event: { kind: string }): event is ErrorDiagnosticEvent {
  return event.kind === 'error' || event.kind === 'unhandledrejection' || event.kind === 'captured';
}

/** Schedules an uncaught synchronous throw on the macrotask queue. */
function throwLater(fn: () => unknown): void {
  setTimeout(() => {
    fn();
  }, 0);
}

function scriptLoader(src: string): Promise<void> {
  return new Promise((resolve) => {
    const script = document.createElement('script');
    script.src = src;
    script.addEventListener('load', () => resolve());
    script.addEventListener('error', () => resolve());
    document.head.append(script);
  });
}

// --- E01 -------------------------------------------------------------------
const e01 = { hostHeard: false, offCount: -1, autoCount: -1, bufferHasError: false };

const e01Scenario: LabScenario = {
  id: 'E01',
  title: 'Uncaught exception from a timeout',
  group: 'errors',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes:
    'The SDK adds its own window "error" listener and never calls preventDefault, so the host listener still fires. ' +
    'With autoReport "off" the error is buffered only; with "errors" exactly one automatic_error is sent.',
  config: { reporting: { autoReport: 'off' } },
  async run(ctx) {
    let hostHeard = false;
    const onError = (): void => {
      hostHeard = true;
    };
    window.addEventListener('error', onError);

    throwLater(() => {
      throw new Error('E01 uncaught exception');
    });
    e01.bufferHasError = (await waitForEvent(ctx, (event) => isErrorEvent(event) && event.error.message.includes('E01'))) !== undefined;
    e01.hostHeard = hostHeard;
    window.removeEventListener('error', onError);

    await waitFor(() => false, 300);
    e01.offCount = (await ctx.receiver.getReports()).count;

    ctx.init({ reporting: { autoReport: 'errors' } });
    throwLater(() => {
      throw new Error('E01 uncaught exception (auto)');
    });
    await waitForStoredCount(ctx, 1);
    e01.autoCount = (await ctx.receiver.getReports()).count;
  },
  async verify(): Promise<Verdict> {
    const ok = e01.hostHeard && e01.bufferHasError && e01.offCount === 0 && e01.autoCount === 1;
    const evidence = { ...e01 };
    return ok
      ? pass('host listener fired; buffered with autoReport off; one automatic_error when on', evidence)
      : fail('unexpected E01 outcome', evidence);
  },
};

// --- E02 -------------------------------------------------------------------
const e02 = { bufferHasRejection: false, offCount: -1, autoCount: -1, normalized: false };

const e02Scenario: LabScenario = {
  id: 'E02',
  title: 'Unhandled promise rejection',
  group: 'errors',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'The rejection reason is normalized (Error or non-Error) and follows the same autoReport policy as E01.',
  config: { reporting: { autoReport: 'off' } },
  async run(ctx) {
    void Promise.reject(new Error('E02 unhandled rejection'));
    const event = await waitForEvent(
      ctx,
      (item) => item.kind === 'unhandledrejection' && item.error.message.includes('E02'),
    );
    e02.bufferHasRejection = event !== undefined;
    e02.normalized = event !== undefined && isErrorEvent(event) && event.error.type.length > 0;
    await waitFor(() => false, 300);
    e02.offCount = (await ctx.receiver.getReports()).count;

    ctx.init({ reporting: { autoReport: 'errors' } });
    void Promise.reject(new Error('E02 unhandled rejection (auto)'));
    await waitForStoredCount(ctx, 1);
    e02.autoCount = (await ctx.receiver.getReports()).count;
  },
  async verify(): Promise<Verdict> {
    const ok = e02.bufferHasRejection && e02.normalized && e02.offCount === 0 && e02.autoCount === 1;
    return ok
      ? pass('unhandledrejection normalized and buffered; one automatic_error when autoReport=errors', { ...e02 })
      : fail('unexpected E02 outcome', { ...e02 });
  },
};

// --- E03 -------------------------------------------------------------------
const e03: { event?: ErrorDiagnosticEvent } = {};

const e03Scenario: LabScenario = {
  id: 'E03',
  title: 'Null reference (TypeError)',
  group: 'errors',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'Records the per-browser error type/message, whether a stack is present, and source line/column when the browser provides it.',
  config: { reporting: { autoReport: 'errors' } },
  async run(ctx) {
    throwLater(() => {
      const value = null as unknown as { nested: { depth: number } };
      void value.nested.depth;
    });
    const event = await waitForEvent(ctx, (item) => isErrorEvent(item) && item.error.type === 'TypeError');
    if (event !== undefined && isErrorEvent(event)) e03.event = event;
    await waitForStoredCount(ctx, 1);
  },
  async verify(): Promise<Verdict> {
    const event = e03.event;
    if (event === undefined) return fail('no TypeError event captured');
    const evidence = {
      type: event.error.type,
      message: event.error.message,
      hasStack: event.error.stack !== undefined,
      source: event.error.source ?? null,
      stackHead: event.error.stack?.split('\n')[1]?.trim() ?? null,
    };
    return pass('TypeError captured with type/message', evidence);
  },
};

// --- E04 -------------------------------------------------------------------
const e04: { event?: ErrorDiagnosticEvent } = {};

const e04Scenario: LabScenario = {
  id: 'E04',
  title: 'Undefined function call',
  group: 'errors',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'Browsers disagree on the type (ReferenceError vs TypeError) and message; the lab records the real value per browser.',
  config: { reporting: { autoReport: 'errors' } },
  async run(ctx) {
    throwLater(() => {
      (0, eval)('mSeismoLabUndefinedFunction()');
    });
    const event = await waitForEvent(ctx, (item) => isErrorEvent(item) && /undefined|not defined|not a function/i.test(item.error.message));
    if (event !== undefined && isErrorEvent(event)) e04.event = event;
    await waitForStoredCount(ctx, 1);
  },
  async verify(): Promise<Verdict> {
    const event = e04.event;
    if (event === undefined) return fail('no undefined-call error captured');
    return pass('undefined function call captured', {
      type: event.error.type,
      message: event.error.message,
      hasStack: event.error.stack !== undefined,
    });
  },
};

// --- E05 -------------------------------------------------------------------
let e05: { afterSwallow: number; captured: boolean } = { afterSwallow: -1, captured: false };

const e05Scenario: LabScenario = {
  id: 'E05',
  title: 'Caught exception (two steps)',
  group: 'errors',
  mode: 'inpage',
  expected: 'MANUAL_ONLY',
  notes:
    'Step 1: a try/catch that swallows an error WITHOUT an SDK call proves the SDK cannot see it. ' +
    'Step 2: the same catch with captureException() is observed.',
  config: { reporting: { autoReport: 'off', sendCapturedExceptions: false } },
  async run(ctx) {
    ctx.clearDiagnostics();
    try {
      throw new Error('E05 swallowed');
    } catch {
      // deliberately swallowed
    }
    await waitFor(() => ctx.count() > 0, 300);
    e05.afterSwallow = ctx.count();

    try {
      throw new Error('E05 caught and reported');
    } catch (error) {
      captureException(error, { step: 'E05' });
    }
    e05.captured = (await waitForEvent(ctx, (item) => item.kind === 'captured')) !== undefined;
  },
  async verify(): Promise<Verdict> {
    const ok = e05.afterSwallow === 0 && e05.captured;
    return ok
      ? pass('swallowed error invisible (0 events); captureException observed', { ...e05 })
      : fail('unexpected E05 outcome', { ...e05 });
  },
};

// --- E07 -------------------------------------------------------------------
let e07: { captured?: ErrorDiagnosticEvent; messageCaptured: boolean } = { messageCaptured: false };

const e07Scenario: LabScenario = {
  id: 'E07',
  title: 'captureException(error, ctx) and captureMessage()',
  group: 'errors',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes: 'The captured context is structurally sanitized (the fake token is redacted); the report is typed captured_exception / captured_message.',
  config: { reporting: { autoReport: 'off', sendCapturedExceptions: true } },
  async run(ctx) {
    captureException(new Error('E07 captured exception'), {
      feature: 'checkout',
      token: FAKE_SECRETS.longToken,
    });
    captureMessage('E07 captured message', { area: 'lab' });
    const event = await waitForEvent(ctx, (item) => isErrorEvent(item) && item.error.message.includes('E07 captured exception'));
    if (event !== undefined && isErrorEvent(event)) e07.captured = event;
    e07.messageCaptured = (await waitForEvent(ctx, (item) => isErrorEvent(item) && item.error.message.includes('E07 captured message'))) !== undefined;
    await waitForStoredCount(ctx, 2);
  },
  async verify(ctx): Promise<Verdict> {
    const reports = await storedReports(ctx, true);
    const captured = reports.find((report) => report.type === 'captured_exception');
    const message = reports.find((report) => report.type === 'captured_message');
    const errors: string[] = [];
    if (e07.captured === undefined) errors.push('no buffered captured event');
    if (captured === undefined) errors.push('no captured_exception stored');
    if (message === undefined || message.error?.type !== 'Message') errors.push('no captured_message stored');
    if (captured !== undefined) {
      if (captured.error?.handled !== true) errors.push('handled !== true');
      if (!/^[0-9a-f]{16}$/.test(captured.error?.fingerprint ?? '')) errors.push('fingerprint not 16 hex');
      if (typeof captured.error?.occurrences !== 'number') errors.push('occurrences missing');
      if (captured.error?.context?.['token'] !== '[REDACTED]') errors.push('token not redacted');
      if (json(captured.error).includes(FAKE_SECRETS.longToken)) errors.push('secret leaked');
    }
    return errors.length === 0
      ? pass('captured_exception + captured_message stored with fingerprint/secret redaction', { types: reports.map((r) => r.type) })
      : fail(errors.join('; '), { reports });
  },
};

// --- E08 -------------------------------------------------------------------
let e08: { before: number; after: number; consoleTextSeen: boolean } = { before: -1, after: -1, consoleTextSeen: false };

const e08Scenario: LabScenario = {
  id: 'E08',
  title: 'console.error() is not captured',
  group: 'errors',
  mode: 'inpage',
  expected: 'NOT_SUPPORTED',
  notes:
    'Intentional non-feature: the SDK never wraps console. A developer who wants these must call captureException() themselves. ' +
    'Pass means the buffer is unchanged (confirmed NOT captured).',
  config: { reporting: { autoReport: 'errors' } },
  async run(ctx) {
    ctx.clearDiagnostics();
    await waitFor(() => false, 100);
    e08.before = ctx.count();
    console.error('E08 this console.error must not become a diagnostic');
    e08.consoleTextSeen = ctx.warnings.messages.some((message) => message.text.includes('E08'));
    await waitFor(() => false, 300);
    e08.after = ctx.count();
  },
  async verify(): Promise<Verdict> {
    const unchanged = e08.before === 0 && e08.after === 0;
    return unchanged
      ? pass('console.error produced no diagnostic event (confirmed not captured)', { ...e08 })
      : fail(`console.error unexpectedly recorded ${e08.after} events`, { ...e08 });
  },
};

// --- E12 -------------------------------------------------------------------
let e12: { message?: string; flags?: readonly string[] | undefined; hasStack: boolean; weak?: boolean };

const e12Scenario: LabScenario = {
  id: 'E12',
  title: 'Cross-origin script without CORS',
  group: 'errors',
  mode: 'inpage',
  expected: 'LIMITED',
  notes:
    'Chrome/Safari mask the details as "Script error." and the collector flags it (no stack, weak fingerprint). ' +
    'Firefox can expose a real stack, so the per-browser result is recorded rather than assumed.',
  config: { reporting: { autoReport: 'errors' } },
  async run(ctx) {
    void scriptLoader(`${ctx.receiverBase}/cross-origin-script.js?e12=${Date.now()}`);
    const event = await waitForEvent(ctx, (item) => isErrorEvent(item) && /script error|m-seismo cross-origin/i.test(item.error.message));
    if (event !== undefined && isErrorEvent(event)) {
      e12 = {
        message: event.error.message,
        flags: event.flags,
        hasStack: event.error.stack !== undefined,
        weak: event.flags?.includes('weak-fingerprint') ?? false,
      };
    }
    await waitForStoredCount(ctx, 1);
  },
  async verify(ctx): Promise<Verdict> {
    if (e12.message === undefined) return fail('cross-origin script error not captured');
    const reports = await storedReports(ctx);
    const report = reports[0];
    const storedFlags = report?.error?.flags ?? [];
    const evidence = {
      ...e12,
      storedFlags,
      fingerprint: report?.error?.fingerprint ?? null,
      masked: (e12.flags?.includes('cross-origin-script-error') ?? false) && e12.hasStack !== true,
    };
    const flagged = (e12.flags?.includes('cross-origin-script-error') ?? false) || storedFlags.includes('cross-origin-script-error');
    if (flagged) {
      // Masked errors must not carry a stack and must be flagged weak; browsers
      // that expose a real stack (Firefox) are recorded, not failed.
      if (e12.hasStack !== true && !storedFlags.includes('weak-fingerprint')) {
        return fail('masked cross-origin error should have a weak fingerprint', evidence);
      }
      if (e12.hasStack === true) return pass('cross-origin error captured with a real stack (browser did not mask)', evidence);
      return pass('masked cross-origin error captured with a weak fingerprint', evidence);
    }
    return pass('cross-origin script error captured (no masking flag recorded)', evidence);
  },
};

// --- E13 -------------------------------------------------------------------
const e13Scenario: LabScenario = {
  id: 'E13',
  title: 'Minified production error (no source-map resolution)',
  group: 'errors',
  mode: 'inpage',
  expected: 'LIMITED',
  notes:
    'The lab loads a tiny single-line minified bundle (generated by test-app/scripts/build-minified.mjs), with and without a source map. ' +
    'The SDK performs NO source-map resolution: the stack points at the minified file/column. The backend/dev must upload and resolve maps server-side.',
  config: { reporting: { autoReport: 'errors' } },
  async run(ctx) {
    await scriptLoader(`/minified/plain.js?e13=${Date.now()}`);
    await waitForEvent(ctx, (item) => isErrorEvent(item) && item.error.message.includes('minified plain'));
    await scriptLoader(`/minified/mapped.js?e13=${Date.now()}`);
    await waitForEvent(ctx, (item) => isErrorEvent(item) && item.error.message.includes('minified mapped'));
    await waitForStoredCount(ctx, 2);
  },
  async verify(ctx): Promise<Verdict> {
    const events = ctx.snapshot().filter((event) => isErrorEvent(event) && /minified/.test(event.error.message));
    const stacks = events.map((event) => (isErrorEvent(event) ? event.error.stack : undefined));
    if (stacks.length < 2) return fail('expected both minified errors to be captured', { stacks });
    const referencesMinified = stacks.every((stack) => stack === undefined || stack.includes('minified/'));
    const resolvedSource = stacks.some((stack) => stack !== undefined && stack.includes('.src.'));
    const evidence = { stacks: stacks.map((stack) => stack?.split('\n').slice(0, 2).join(' | ') ?? null), referencesMinified, resolvedSource };
    if (!referencesMinified) return fail('minified stacks do not reference the minified file', evidence);
    if (resolvedSource) return fail('SDK appears to have resolved a source map (it must not)', evidence);
    return pass('minified errors reported with minified frames; no source-map resolution performed', evidence);
  },
};

export const errorScenarios: readonly LabScenario[] = [
  e01Scenario,
  e02Scenario,
  e03Scenario,
  e04Scenario,
  e05Scenario,
  e07Scenario,
  e08Scenario,
  e12Scenario,
  e13Scenario,
];
