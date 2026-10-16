// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createFormController } from '@sdk/form/formController';
import { configure, resetConfig } from '@sdk/core/config';
import type { SdkEvent } from '@sdk/core/events';
import type { SanitizedReport } from '@sdk/privacy/sanitize';
import type { SendResult } from '@sdk/transport/types';

let events: SdkEvent[];
let controllers: ReturnType<typeof createFormController>[];

function createController(
  send: (report: SanitizedReport) => Promise<SendResult>,
): ReturnType<typeof createFormController> {
  const controller = createFormController({ send });
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

function sendSpy(
  responses: SendResult[],
  sent: SanitizedReport[],
): (report: SanitizedReport) => Promise<SendResult> {
  let call = 0;
  return async (report) => {
    sent.push(report);
    const result = responses[Math.min(call, responses.length - 1)]!;
    call += 1;
    return result;
  };
}

const OK: SendResult = { ok: true, status: 201, id: 'srv-1' };
const FAIL: SendResult = {
  ok: false,
  kind: 'http',
  status: 500,
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
    const controller = createController(sendSpy([OK], []));
    expect(document.querySelector('[data-mseismo-form]')).toBeNull();

    controller.open();

    const host = document.querySelector('[data-mseismo-form]');
    expect(host?.shadowRoot).not.toBeNull();
    expect(q('[data-testid="mseismo-dialog"]').getAttribute('role')).toBe('dialog');
    expect(events).toContainEqual({ type: 'form:open' });
  });

  it('rejects an empty description without sending', async () => {
    vi.useFakeTimers();
    const sent: SanitizedReport[] = [];
    const send = vi.fn(sendSpy([OK], sent));
    const controller = createController(send);
    controller.open();

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(send).not.toHaveBeenCalled();
    expect(events.some((e) => e.type === 'validation:error')).toBe(true);
    expect(q('[data-testid="mseismo-message"]').textContent).toContain('describe');
    expect(controller.isOpen()).toBe(true);
  });

  it('sends a valid report, emits success, then closes', async () => {
    vi.useFakeTimers();
    const sent: SanitizedReport[] = [];
    const controller = createController(sendSpy([OK], sent));
    controller.open();
    typeDescription('  the save button did nothing  ');

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(sent).toHaveLength(1);
    expect(sent[0]!.description).toBe('the save button did nothing');
    expect(sent[0]!.screenshot).toBeNull();
    expect(sent[0]!.error).toBeNull();
    expect(events.some((e) => e.type === 'submit:start')).toBe(true);
    expect(events.some((e) => e.type === 'submit:success')).toBe(true);
    expect(controller.isOpen()).toBe(true);

    await vi.advanceTimersByTimeAsync(1000);
    expect(controller.isOpen()).toBe(false);
    expect(events).toContainEqual({ type: 'form:close', reason: 'sent' });
  });

  it('keeps the form open on failure and reuses the reportId on retry', async () => {
    vi.useFakeTimers();
    const sent: SanitizedReport[] = [];
    const controller = createController(sendSpy([FAIL, OK], sent));
    controller.open();
    typeDescription('something broke');

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(controller.isOpen()).toBe(true);
    expect(events.some((e) => e.type === 'submit:failure')).toBe(true);
    expect(q('[data-testid="mseismo-send"]').textContent).toBe('Try again');

    submit();
    await vi.advanceTimersByTimeAsync(0);

    expect(sent).toHaveLength(2);
    expect(sent[1]!.reportId).toBe(sent[0]!.reportId);
    expect(events.some((e) => e.type === 'submit:success')).toBe(true);
  });

  it('discards typed text when closed without sending', () => {
    const controller = createController(sendSpy([OK], []));
    controller.open();
    typeDescription('to be discarded');

    controller.close('cancel');
    expect(events).toContainEqual({ type: 'form:close', reason: 'cancel' });

    controller.open();
    expect(q<HTMLTextAreaElement>('[data-testid="mseismo-description"]').value).toBe('');
  });

  it('attaches a screenshot picked by the user', async () => {
    vi.useFakeTimers();
    const sent: SanitizedReport[] = [];
    const controller = createController(sendSpy([OK], sent));
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

    expect(sent).toHaveLength(1);
    const screenshot = sent[0]!.screenshot;
    expect(screenshot).not.toBeNull();
    expect(screenshot?.mimeType).toBe('image/png');
    expect(screenshot?.name).toBe('shot.png');
    expect(screenshot?.sizeBytes).toBe(4);
    expect(typeof screenshot?.dataBase64).toBe('string');
    expect(screenshot?.dataBase64.length).toBeGreaterThan(0);
  });
});
