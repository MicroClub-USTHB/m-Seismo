// @vitest-environment happy-dom
/**
 * SDK lifecycle: `init()` must be idempotent (a second init tears the first
 * down, so no listener/wrapper is registered twice) and `destroy()` must
 * remove every side effect and restore the host page's globals.
 *
 * Each assertion is behavioural: instead of counting `addEventListener` calls
 * we perform exactly one action and require exactly one effect.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { destroy, init, openReportForm } from '@m-seismo/web';
import { clearDiagnostics, getDiagnosticsSnapshot } from '@sdk/internal';
import { isEnabled } from '@sdk/core/config';
import { resetOriginalFetch } from '@sdk/network/originalFetch';
import type {
  DiagnosticEvent,
  InteractionDiagnosticEvent,
  NavigationDiagnosticEvent,
  NetworkDiagnosticEvent,
} from '@sdk/diagnostics/types';

const ENDPOINT = 'https://receiver.test/api/reports';

let events: { type: string; [key: string]: unknown }[];
let pageFetch: typeof globalThis.fetch;
let fetchCalls: string[];

function navs(): NavigationDiagnosticEvent[] {
  return getDiagnosticsSnapshot().filter(
    (e: DiagnosticEvent): e is NavigationDiagnosticEvent => e.kind === 'navigation',
  );
}

function networks(): NetworkDiagnosticEvent[] {
  return getDiagnosticsSnapshot().filter(
    (e: DiagnosticEvent): e is NetworkDiagnosticEvent => e.kind === 'network',
  );
}

function interactions(): InteractionDiagnosticEvent[] {
  return getDiagnosticsSnapshot().filter(
    (e: DiagnosticEvent): e is InteractionDiagnosticEvent => e.kind === 'interaction',
  );
}

function forms(): NodeListOf<Element> {
  return document.querySelectorAll('[data-mseismo-form]');
}

function launchers(): NodeListOf<HTMLButtonElement> {
  return document.querySelectorAll('[data-mseismo-launcher]');
}

function pressShortcut(): void {
  document.dispatchEvent(
    new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true, cancelable: true }),
  );
}

beforeEach(() => {
  events = [];
  fetchCalls = [];
  document.body.innerHTML = '';
  window.history.replaceState(null, '', '/');
  pageFetch = globalThis.fetch;
  globalThis.fetch = ((input: RequestInfo | URL): Promise<Response> => {
    fetchCalls.push(String(input));
    return Promise.reject(new TypeError('network down'));
  }) as typeof globalThis.fetch;
});

afterEach(() => {
  destroy();
  globalThis.fetch = pageFetch;
  resetOriginalFetch();
  window.history.replaceState(null, '', '/');
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

function options(extra: Record<string, unknown> = {}): Parameters<typeof init>[0] {
  return {
    endpoint: ENDPOINT,
    shortcut: false,
    onEvent: (event: { type: string }) => events.push(event),
    ...extra,
  } as Parameters<typeof init>[0];
}

describe('init() idempotence', () => {
  it('mounts exactly one launcher, no matter how often init() runs', () => {
    expect(init(options({ launcher: true }))).toBe(true);
    expect(init(options({ launcher: true }))).toBe(true);
    expect(init(options({ launcher: true }))).toBe(true);

    expect(launchers()).toHaveLength(1);
    launchers()[0]!.click();
    expect(forms()).toHaveLength(1);
    expect(events.filter((e) => e.type === 'form:open')).toHaveLength(1);
  });

  it('registers the shortcut listener only once', () => {
    init(options({ shortcut: 'Ctrl+Enter' }));
    init(options({ shortcut: 'Ctrl+Enter' }));

    pressShortcut();

    expect(forms()).toHaveLength(1);
    expect(events.filter((e) => e.type === 'form:open')).toHaveLength(1);
  });

  it('wraps fetch only once, so one failing request records one event', async () => {
    init(options());
    init(options());

    clearDiagnostics();
    await expect(fetch('https://api.example.test/orders')).rejects.toThrow('network down');

    expect(fetchCalls).toHaveLength(1);
    expect(networks()).toHaveLength(1);
    expect(networks()[0]!.data.url).toBe('https://api.example.test/orders');
  });

  it('wraps history.pushState only once', () => {
    init(options());
    init(options());

    clearDiagnostics();
    history.pushState(null, '', '/checkout?step=2');

    const pushes = navs().filter((e) => e.data.trigger === 'pushState');
    expect(pushes).toHaveLength(1);
    expect(pushes[0]!.data.to).toContain('/checkout');
  });

  it('registers the interaction collector only once', () => {
    const button = document.createElement('button');
    button.id = 'pay';
    document.body.appendChild(button);

    init(options());
    init(options());

    clearDiagnostics();
    button.click();
    button.click();

    const recorded = interactions();
    expect(recorded).toHaveLength(1);
    expect(recorded[0]!.data.action).toBe('click');
    expect(recorded[0]!.data.target.id).toBe('pay');
  });

  it('never records interactions with its own UI', () => {
    init(options({ launcher: true }));

    clearDiagnostics();
    document.querySelector<HTMLButtonElement>('[data-mseismo-launcher]')!.click();

    expect(interactions()).toHaveLength(0);
  });

  it('keeps the recorded history of a repeated init', () => {
    init(options());
    clearDiagnostics();
    history.pushState(null, '', '/step-a');
    const before = getDiagnosticsSnapshot().length;
    expect(before).toBeGreaterThan(0);

    init(options());

    expect(getDiagnosticsSnapshot().length).toBeGreaterThanOrEqual(before);
  });
});

describe('destroy()', () => {
  it('removes the launcher and any open form', () => {
    init(options({ launcher: true, shortcut: 'Ctrl+Enter' }));
    launchers()[0]!.click();
    expect(forms()).toHaveLength(1);

    destroy();

    expect(launchers()).toHaveLength(0);
    expect(forms()).toHaveLength(0);
  });

  it('restores the fetch and history.pushState that were installed before init()', () => {
    const fetchBefore = globalThis.fetch;
    const pushBefore = history.pushState;
    init(options());

    expect(globalThis.fetch).not.toBe(fetchBefore);
    expect(history.pushState).not.toBe(pushBefore);

    destroy();

    expect(globalThis.fetch).toBe(fetchBefore);
    expect(history.pushState).toBe(pushBefore);
  });

  it('disables the SDK and clears the diagnostic history', () => {
    init(options());
    history.pushState(null, '', '/somewhere');
    expect(getDiagnosticsSnapshot().length).toBeGreaterThan(0);

    destroy();

    expect(isEnabled()).toBe(false);
    expect(getDiagnosticsSnapshot()).toEqual([]);
  });

  it('makes openReportForm() a warning-only no-op', () => {
    init(options());
    destroy();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    openReportForm();

    expect(warn).toHaveBeenCalledTimes(1);
    expect(forms()).toHaveLength(0);
  });

  it('ignores the shortcut once destroyed', () => {
    init(options({ shortcut: 'Ctrl+Enter' }));
    destroy();

    pressShortcut();

    expect(forms()).toHaveLength(0);
  });

  it('can be followed by a fresh init()', () => {
    init(options({ launcher: true }));
    destroy();
    expect(init(options({ launcher: true }))).toBe(true);

    expect(isEnabled()).toBe(true);
    expect(launchers()).toHaveLength(1);
  });

  it('is safe to call without a prior init', () => {
    expect(() => destroy()).not.toThrow();
  });
});

describe('init() validation', () => {
  it('refuses an invalid endpoint and leaves the SDK disabled', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    expect(init({ endpoint: 'not-a-url', launcher: true })).toBe(false);

    expect(isEnabled()).toBe(false);
    expect(launchers()).toHaveLength(0);
    expect(warn).toHaveBeenCalled();
  });

  it('leaves the page globals untouched when config is rejected', () => {
    const fetchBefore = globalThis.fetch;
    const pushBefore = history.pushState;

    expect(init({ endpoint: 'nope' })).toBe(false);

    expect(globalThis.fetch).toBe(fetchBefore);
    expect(history.pushState).toBe(pushBefore);
    destroy();
  });
});

describe('launcher', () => {
  it('applies the configured label, aria-label and position', () => {
    init(
      options({
        launcher: { label: 'Tell us what broke', position: 'bottom-left' },
      }),
    );

    const button = document.querySelector<HTMLButtonElement>('[data-mseismo-launcher]');
    expect(button).not.toBeNull();
    expect(button!.textContent).toBe('Tell us what broke');
    expect(button!.getAttribute('aria-label')).toBe('Tell us what broke');
    expect(button!.type).toBe('button');
    expect(button!.style.getPropertyValue('position')).toBe('fixed');
    expect(button!.style.getPropertyValue('left')).toBe('20px');
    expect(button!.style.getPropertyValue('bottom')).toBe('20px');
    expect(button!.style.getPropertyValue('right')).toBe('');
  });

  it('places the launcher in every supported corner', () => {
    const corners: [string, Record<string, string>][] = [
      ['top-right', { top: '20px', right: '20px' }],
      ['top-left', { top: '20px', left: '20px' }],
      ['bottom-right', { bottom: '20px', right: '20px' }],
      ['bottom-left', { bottom: '20px', left: '20px' }],
    ];
    for (const [position, expected] of corners) {
      destroy();
      init(
        options({
          launcher: { position },
        } as Record<string, unknown>),
      );
      const button = document.querySelector<HTMLButtonElement>('[data-mseismo-launcher]');
      expect(button, `launcher missing for ${position}`).not.toBeNull();
      for (const [property, value] of Object.entries(expected)) {
        expect(button!.style.getPropertyValue(property)).toBe(value);
      }
    }
  });
});
