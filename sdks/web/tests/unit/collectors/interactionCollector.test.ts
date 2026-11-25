// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest';
import { installInteractionCollector } from '@sdk/collectors/interactionCollector';
import type { DiagnosticEvent, InteractionDiagnosticEvent } from '@sdk/diagnostics/types';

function recorder(): {
  events: DiagnosticEvent[];
  fn: (e: DiagnosticEvent) => void;
} {
  const events: DiagnosticEvent[] = [];
  return { events, fn: (e) => events.push(e) };
}

function interactions(events: DiagnosticEvent[]): InteractionDiagnosticEvent[] {
  return events.filter((e): e is InteractionDiagnosticEvent => e.kind === 'interaction');
}

function click(el: Element): void {
  el.dispatchEvent(new Event('click', { bubbles: true, cancelable: true }));
}

const disposers: Array<() => void> = [];
afterEach(() => {
  while (disposers.length > 0) disposers.pop()!();
  document.body.innerHTML = '';
});

describe('installInteractionCollector', () => {
  it('records a click on an allow-listed button', () => {
    const { events, fn } = recorder();
    disposers.push(installInteractionCollector(document, fn));
    const button = document.createElement('button');
    button.setAttribute('data-testid', 'save');
    button.textContent = 'Save';
    document.body.appendChild(button);

    click(button);

    const recorded = interactions(events);
    expect(recorded).toHaveLength(1);
    expect(recorded[0]!.data.action).toBe('click');
    expect(recorded[0]!.data.target.tag).toBe('button');
    expect(recorded[0]!.data.target.testId).toBe('save');
    expect(recorded[0]!.data.target.label).toBeUndefined();
  });

  it('ignores a non-allow-listed element', () => {
    const { events, fn } = recorder();
    disposers.push(installInteractionCollector(document, fn));
    const div = document.createElement('div');
    document.body.appendChild(div);
    click(div);
    expect(interactions(events)).toHaveLength(0);
  });

  it('ignores an <a> without href but records one with href', () => {
    const { events, fn } = recorder();
    disposers.push(installInteractionCollector(document, fn));
    const bare = document.createElement('a');
    const link = document.createElement('a');
    link.setAttribute('href', '/somewhere');
    document.body.append(bare, link);

    click(bare);
    expect(interactions(events)).toHaveLength(0);
    click(link);
    expect(interactions(events)).toHaveLength(1);
  });

  it('never records a password input', () => {
    const { events, fn } = recorder();
    disposers.push(installInteractionCollector(document, fn));
    const input = document.createElement('input');
    input.setAttribute('type', 'password');
    document.body.appendChild(input);
    click(input);
    expect(interactions(events)).toHaveLength(0);
  });

  it('ignores clicks inside the SDK form', () => {
    const { events, fn } = recorder();
    disposers.push(installInteractionCollector(document, fn));
    const host = document.createElement('div');
    host.setAttribute('data-mseismo-form', '');
    const button = document.createElement('button');
    host.appendChild(button);
    document.body.appendChild(host);
    click(button);
    expect(interactions(events)).toHaveLength(0);
  });

  it('records the element label only when captureElementText is enabled', () => {
    const { events, fn } = recorder();
    disposers.push(installInteractionCollector(document, fn, { captureElementText: true }));
    const button = document.createElement('button');
    button.textContent = 'Delete account';
    document.body.appendChild(button);
    click(button);
    expect(interactions(events)[0]!.data.target.label).toBe('Delete account');
  });

  it('throttles identical consecutive clicks', () => {
    const { events, fn } = recorder();
    let clock = 0;
    disposers.push(installInteractionCollector(document, fn, { now: () => clock }));
    const button = document.createElement('button');
    button.setAttribute('data-testid', 'x');
    document.body.appendChild(button);

    click(button);
    clock = 100;
    click(button);
    expect(interactions(events)).toHaveLength(1);

    clock = 1000;
    click(button);
    expect(interactions(events)).toHaveLength(2);
  });

  it('records a form submit', () => {
    const { events, fn } = recorder();
    disposers.push(installInteractionCollector(document, fn));
    const form = document.createElement('form');
    document.body.appendChild(form);
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    const recorded = interactions(events);
    expect(recorded).toHaveLength(1);
    expect(recorded[0]!.data.action).toBe('submit');
    expect(recorded[0]!.data.target.tag).toBe('form');
  });

  it('stops recording after dispose', () => {
    const { events, fn } = recorder();
    const dispose = installInteractionCollector(document, fn);
    dispose();
    const button = document.createElement('button');
    document.body.appendChild(button);
    click(button);
    expect(interactions(events)).toHaveLength(0);
  });
});
