// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createFormController } from '@sdk/form/formController';
import { configure, resetConfig } from '@sdk/core/config';
import type { SdkEvent } from '@sdk/core/events';
import type { ProcessReportInput, ProcessResult } from '@sdk/reporting/processor';

let events: SdkEvent[];
let controllers: ReturnType<typeof createFormController>[];

function createController(
  process: (input: ProcessReportInput) => Promise<ProcessResult>,
): ReturnType<typeof createFormController> {
  const controller = createFormController({ process });
  controllers.push(controller);
  return controller;
}

function q<T extends Element>(selector: string): T {
  const host = document.querySelector('[data-mseismo-form]');
  const root = host?.shadowRoot ?? null;
  const el = root?.querySelector<T>(selector) ?? null;
  if (el === null) throw new Error(`missing ${selector}`);
  return el;
}

function submit(): void {
  q<HTMLFormElement>('.form').dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true }),
  );
}

function typeDescription(value: string): void {
  q<HTMLTextAreaElement>('[data-testid="mseismo-description"]').value = value;
}

function processSpy(
  results: ProcessResult[],
  seen: ProcessReportInput[],
): (input: ProcessReportInput) => Promise<ProcessResult> {
  let call = 0;
  return async (input) => {
    seen.push(input);
    const result = results[Math.min(call, results.length - 1)]!;
    call += 1;
    return result;
  };
}

function sent(reportId = 'r', httpStatus = 201, id?: string): ProcessResult {
  return {
    status: 'sent',
    reportId,
    type: 'manual',
    trigger: 'form',
    httpStatus,
    ...(id !== undefined ? { id } : {}),
  };
}

const FAIL: ProcessResult = {
  status: 'failed',
  reportId: 'r',
  type: 'manual',
  trigger: 'form',
  kind: 'http',
  httpStatus: 500,
  message: 'HTTP 500',
};

beforeEach(() => {
  events = [];
  controllers = [];
  document.body.innerHTML = '';
  configure({
    endpoint: 'https://receiver.test/api/reports',
    shortcut: false,
    onEvent: (event) => events.push(event),
  });
});

afterEach(() => {
  for (const controller of controllers) controller.destroy();
  resetConfig();
  document.body.innerHTML = '';
  vi.useRealTimers();
});

describe('createFormController', () => {
  it('open() attaches the dialog in a shadow root and emits form:open', () => {
    const controller = createController(processSpy([sent('r')], []));
    expect(document.querySelector('[data-mseismo-form]')).toBeNull();

    controller.open();

    const host = document.querySelector('[data-mseismo-form]');
    expect(host?.shadowRoot).not.toBeNull();
    expect(q('[data-testid="mseismo-dialog"]').getAttribute('role')).toBe('dialog');
    expect(events).toContainEqual({ type: 'form:open' });
  });

  it('rejects an empty description without processing', async () => {
    vi.useFakeTimers();
    const seen: ProcessReportInput[] = [];
    const process = vi.fn(processSpy([sent('r')], seen));
    const controller = createController(process);
    controller.open();

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(process).not.toHaveBeenCalled();
    expect(events.some((e) => e.type === 'validation:error')).toBe(true);
    expect(q('[data-testid="mseismo-message"]').textContent).toContain('describe');
    expect(controller.isOpen()).toBe(true);
  });

  it('sends a valid report through the processor, emits success, then closes', async () => {
    vi.useFakeTimers();
    const seen: ProcessReportInput[] = [];
    const controller = createController(
      processSpy([{ ...sent('r', 201, 'srv-1') }], seen),
    );
    controller.open();
    typeDescription('  the save button did nothing  ');

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(seen).toHaveLength(1);
    expect(seen[0]!.source).toBe('form');
    expect(seen[0]!.description).toBe('the save button did nothing');
    expect(seen[0]!.screenshot).toBeNull();
    expect(events.some((e) => e.type === 'submit:start')).toBe(true);
    expect(events.some((e) => e.type === 'submit:success')).toBe(true);
    expect(controller.isOpen()).toBe(true);

    await vi.advanceTimersByTimeAsync(1000);
    expect(controller.isOpen()).toBe(false);
    expect(events).toContainEqual({ type: 'form:close', reason: 'sent' });
  });

  it('keeps the form open on failure and reuses the reportId on retry', async () => {
    vi.useFakeTimers();
    const seen: ProcessReportInput[] = [];
    const first: ProcessResult = { ...FAIL, reportId: 'ignored' };
    const controller = createController(
      processSpy([first, sent('r')], seen),
    );
    controller.open();
    typeDescription('something broke');

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(controller.isOpen()).toBe(true);
    expect(events.some((e) => e.type === 'submit:failure')).toBe(true);
    expect(q('[data-testid="mseismo-send"]').textContent).toBe('Try again');

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(seen).toHaveLength(2);
    expect(seen[1]!.reportId).toBe(seen[0]!.reportId);
    expect(events.some((e) => e.type === 'submit:success')).toBe(true);
  });

  it('shows an error and does not emit success when suppressed', async () => {
    vi.useFakeTimers();
    const seen: ProcessReportInput[] = [];
    const suppressed: ProcessResult = {
      status: 'suppressed',
      reportId: 'r',
      type: 'manual',
      trigger: 'form',
      reason: 'disabled',
    };
    const controller = createController(processSpy([suppressed], seen));
    controller.open();
    typeDescription('anything');

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(events.some((e) => e.type === 'submit:success')).toBe(false);
    expect(events.some((e) => e.type === 'submit:failure')).toBe(false);
    expect(q('[data-testid="mseismo-message"]').textContent).toContain('not configured');
  });

  it('discards typed text when closed without sending', () => {
    const controller = createController(processSpy([sent('r')], []));
    controller.open();
    typeDescription('to be discarded');

    controller.close('cancel');
    expect(events).toContainEqual({ type: 'form:close', reason: 'cancel' });

    controller.open();
    expect(q<HTMLTextAreaElement>('[data-testid="mseismo-description"]').value).toBe('');
  });

  it('passes a screenshot picked by the user to the processor', async () => {
    vi.useFakeTimers();
    const seen: ProcessReportInput[] = [];
    const controller = createController(processSpy([sent('r')], seen));
    controller.open();

    const file = new File([new Uint8Array([137, 80, 78, 71])], 'shot.png', {
      type: 'image/png',
    });
    const input = q<HTMLInputElement>('[data-testid="mseismo-screenshot"]');
    Object.defineProperty(input, 'files', { value: [file], configurable: true });
    input.dispatchEvent(new Event('change'));
    await vi.advanceTimersByTimeAsync(0);

    typeDescription('with screenshot');
    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(seen).toHaveLength(1);
    const screenshot = seen[0]!.screenshot;
    expect(screenshot).not.toBeNull();
    expect(screenshot?.mimeType).toBe('image/png');
    expect(screenshot?.name).toBe('shot.png');
    expect(screenshot?.sizeBytes).toBe(4);
    expect(typeof screenshot?.dataBase64).toBe('string');
    expect(screenshot?.dataBase64.length).toBeGreaterThan(0);
  });
});
