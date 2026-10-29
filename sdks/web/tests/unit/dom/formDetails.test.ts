// @vitest-environment happy-dom
/**
 * Form behaviour details not covered by the submit-flow suite: focus
 * restoration, the in-flight submit guard, every user-facing failure message,
 * screenshot rejection, dialog idempotence, Escape/focus-trap/shortcut keys,
 * and the auto-close timer being cleared on destroy.
 */
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

function shadow(): ShadowRoot {
  const host = document.querySelector('[data-mseismo-form]');
  const root = host?.shadowRoot ?? null;
  if (root === null) throw new Error('form host is not mounted');
  return root;
}

function q<T extends Element>(selector: string): T {
  const el = shadow().querySelector<T>(selector);
  if (el === null) throw new Error(`missing ${selector}`);
  return el;
}

function submitEvent(): void {
  q<HTMLFormElement>('.form').dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true }),
  );
}

function typeDescription(value: string): void {
  q<HTMLTextAreaElement>('[data-testid="mseismo-description"]').value = value;
}

function message(): string {
  return q<HTMLElement>('[data-testid="mseismo-message"]').textContent ?? '';
}

function sent(): ProcessResult {
  return { status: 'sent', reportId: 'r', type: 'manual', trigger: 'form', httpStatus: 201 };
}

function failed(kind: 'network' | 'timeout' | 'http' | 'invalid-response'): ProcessResult {
  return {
    status: 'failed',
    reportId: 'r',
    type: 'manual',
    trigger: 'form',
    kind,
    ...(kind === 'http' ? { httpStatus: 413 } : {}),
    message: `kind=${kind}`,
  };
}

function deferred(): { promise: Promise<ProcessResult>; resolve: (r: ProcessResult) => void } {
  let resolve!: (r: ProcessResult) => void;
  const promise = new Promise<ProcessResult>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

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
  vi.restoreAllMocks();
});

describe('focus restoration', () => {
  it('puts focus back on the textarea after a validation error', () => {
    const controller = createController(async () => sent());
    controller.open();
    const textarea = q<HTMLTextAreaElement>('[data-testid="mseismo-description"]');
    const focus = vi.spyOn(textarea, 'focus');
    q<HTMLElement>('[data-testid="mseismo-send"]').focus();

    submitEvent();

    expect(focus).toHaveBeenCalledTimes(1);
    expect(shadow().activeElement).toBe(textarea);
    expect(controller.isOpen()).toBe(true);
  });

  it('focuses the textarea when the dialog opens', () => {
    const controller = createController(async () => sent());
    controller.open();
    expect(shadow().activeElement).toBe(
      q<HTMLTextAreaElement>('[data-testid="mseismo-description"]'),
    );
  });
});

describe('in-flight submit guard', () => {
  it('ignores a second submit while the first is still running', async () => {
    vi.useFakeTimers();
    const gate = deferred();
    const process = vi.fn(() => gate.promise);
    const controller = createController(process);
    controller.open();
    typeDescription('double click');

    submitEvent();
    submitEvent();
    await vi.advanceTimersByTimeAsync(0);

    expect(process).toHaveBeenCalledTimes(1);
    const send = q<HTMLButtonElement>('[data-testid="mseismo-send"]');
    expect(send.disabled).toBe(true);
    expect(send.textContent).toBe('Sending…');
    expect(q<HTMLButtonElement>('[data-testid="mseismo-cancel"]').disabled).toBe(true);

    gate.resolve(sent());
    await vi.advanceTimersByTimeAsync(0);
    expect(process).toHaveBeenCalledTimes(1);
  });
});

describe('user-facing messages', () => {
  const cases: [Parameters<typeof failed>[0], string][] = [
    ['network', 'Could not reach the server'],
    ['timeout', 'The request timed out'],
    ['http', 'HTTP 413'],
    ['invalid-response', 'not understood'],
  ];

  it.each(cases)('shows the %s failure message', async (kind, expected) => {
    vi.useFakeTimers();
    const controller = createController(async () => failed(kind));
    controller.open();
    typeDescription('something failed');

    submitEvent();
    await vi.advanceTimersByTimeAsync(0);

    expect(message()).toContain(expected);
    expect(events.some((e) => e.type === 'submit:failure')).toBe(true);
    expect(controller.isOpen()).toBe(true);
  });

  it('explains a missing description when the processor suppresses it', async () => {
    vi.useFakeTimers();
    const controller = createController(async () => ({
      status: 'suppressed',
      reportId: 'r',
      type: 'manual',
      trigger: 'form',
      reason: 'invalid_input',
    }));
    controller.open();
    typeDescription('typed but suppressed');

    submitEvent();
    await vi.advanceTimersByTimeAsync(0);

    expect(message()).toContain('the description is missing');
    expect(events.some((e) => e.type === 'submit:success')).toBe(false);
  });

  it('shows a generic message for a rate-limited report', async () => {
    vi.useFakeTimers();
    const controller = createController(async () => ({
      status: 'suppressed',
      reportId: 'r',
      type: 'manual',
      trigger: 'form',
      reason: 'rate_limited',
    }));
    controller.open();
    typeDescription('too many');

    submitEvent();
    await vi.advanceTimersByTimeAsync(0);

    expect(message()).toBe('The report could not be sent. Please try again.');
  });
});

describe('screenshot rejection', () => {
  it('blocks submission and shows why the screenshot was refused', async () => {
    vi.useFakeTimers();
    const process = vi.fn(async () => sent());
    const controller = createController(process);
    controller.open();

    const file = new File([new Uint8Array([1])], 'notes.txt', { type: 'text/plain' });
    const input = q<HTMLInputElement>('[data-testid="mseismo-screenshot"]');
    Object.defineProperty(input, 'files', { value: [file], configurable: true });
    input.dispatchEvent(new Event('change'));
    await vi.advanceTimersByTimeAsync(0);

    expect(message()).toContain('PNG, JPEG, or WebP');

    typeDescription('with a bad screenshot');
    submitEvent();
    await vi.advanceTimersByTimeAsync(0);

    expect(process).not.toHaveBeenCalled();
    expect(controller.isOpen()).toBe(true);
  });

  it('replaces a previous screenshot error with the newly selected file', async () => {
    vi.useFakeTimers();
    const controller = createController(async () => sent());
    controller.open();

    const input = q<HTMLInputElement>('[data-testid="mseismo-screenshot"]');
    const bad = new File([new Uint8Array([1])], 'notes.txt', { type: 'text/plain' });
    Object.defineProperty(input, 'files', { value: [bad], configurable: true, writable: true });
    input.dispatchEvent(new Event('change'));
    await vi.advanceTimersByTimeAsync(0);
    expect(message()).toContain('PNG, JPEG, or WebP');

    const good = new File([new Uint8Array([1, 2])], 'ok.png', { type: 'image/png' });
    Object.defineProperty(input, 'files', { value: [good], configurable: true, writable: true });
    input.dispatchEvent(new Event('change'));
    await vi.advanceTimersByTimeAsync(0);

    expect(message()).toBe('');
    expect(q<HTMLElement>('[data-testid="mseismo-file-info"]').textContent).toBe('ok.png · 2 B');
  });
});

describe('dialog keys and idempotence', () => {
  it('open() is a no-op while the dialog is already open', () => {
    const controller = createController(async () => sent());
    controller.open();
    controller.open();

    expect(document.querySelectorAll('[data-mseismo-form]')).toHaveLength(1);
    expect(events.filter((e) => e.type === 'form:open')).toHaveLength(1);
  });

  it('Escape closes the dialog as a cancellation', () => {
    const controller = createController(async () => sent());
    controller.open();

    q<HTMLElement>('[data-testid="mseismo-dialog"]').dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );

    expect(controller.isOpen()).toBe(false);
    expect(events).toContainEqual({ type: 'form:close', reason: 'cancel' });
  });

  it('traps Tab inside the dialog', () => {
    const controller = createController(async () => sent());
    controller.open();
    const dialog = q<HTMLElement>('[data-testid="mseismo-dialog"]');
    const send = q<HTMLButtonElement>('[data-testid="mseismo-send"]');

    send.focus();
    const forward = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
    dialog.dispatchEvent(forward);
    expect(forward.defaultPrevented).toBe(true);
    expect(shadow().activeElement).toBe(
      q<HTMLTextAreaElement>('[data-testid="mseismo-description"]'),
    );

    const backward = new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: true,
      bubbles: true,
      cancelable: true,
    });
    dialog.dispatchEvent(backward);
    expect(backward.defaultPrevented).toBe(true);
    expect(shadow().activeElement).toBe(send);

    controller.close();
  });

  it('submits through the configured shortcut from inside the dialog', async () => {
    vi.useFakeTimers();
    configure({
      endpoint: 'https://receiver.test/api/reports',
      shortcut: 'Ctrl+Enter',
      onEvent: (event) => events.push(event),
    });
    const process = vi.fn(async () => sent());
    const controller = createController(process);
    controller.open();
    typeDescription('via keyboard');

    q<HTMLElement>('[data-testid="mseismo-dialog"]').dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    await vi.advanceTimersByTimeAsync(0);

    expect(process).toHaveBeenCalledTimes(1);
    expect(events.some((e) => e.type === 'submit:start')).toBe(true);
  });
});

describe('destroy()', () => {
  it('cancels the pending auto-close timer without emitting form:close', async () => {
    vi.useFakeTimers();
    const controller = createController(async () => sent());
    controller.open();
    typeDescription('about to be destroyed');

    submitEvent();
    await vi.advanceTimersByTimeAsync(0);
    expect(controller.isOpen()).toBe(true);

    controller.destroy();
    await vi.advanceTimersByTimeAsync(2000);

    expect(controller.isOpen()).toBe(false);
    expect(events.some((e) => e.type === 'form:close')).toBe(false);
    expect(document.querySelector('[data-mseismo-form]')).toBeNull();
  });

  it('is safe to call twice', () => {
    const controller = createController(async () => sent());
    controller.open();
    controller.destroy();
    controller.destroy();
    expect(controller.isOpen()).toBe(false);
  });
});
