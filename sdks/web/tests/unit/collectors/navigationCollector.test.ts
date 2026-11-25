// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest';
import { installNavigationCollector } from '@sdk/collectors/navigationCollector';
import type { DiagnosticEvent, NavigationDiagnosticEvent } from '@sdk/diagnostics/types';

function recorder(): {
  events: DiagnosticEvent[];
  fn: (e: DiagnosticEvent) => void;
} {
  const events: DiagnosticEvent[] = [];
  return { events, fn: (e) => events.push(e) };
}

function navs(events: DiagnosticEvent[]): NavigationDiagnosticEvent[] {
  return events.filter((e): e is NavigationDiagnosticEvent => e.kind === 'navigation');
}

const disposers: Array<() => void> = [];
afterEach(() => {
  while (disposers.length > 0) disposers.pop()!();
  window.history.replaceState(null, '', '/');
});

describe('installNavigationCollector', () => {
  it('records one initial load breadcrumb', () => {
    const { events, fn } = recorder();
    disposers.push(installNavigationCollector(window, fn));
    const recorded = navs(events);
    expect(recorded).toHaveLength(1);
    expect(recorded[0]!.data.trigger).toBe('load');
    expect(recorded[0]!.data.from).toBeUndefined();
    expect(typeof recorded[0]!.data.to).toBe('string');
  });

  it('records pushState with from/to and the sanitized URL', () => {
    const { events, fn } = recorder();
    disposers.push(installNavigationCollector(window, fn));
    window.history.pushState(null, '', '/next?token=fake-secret-123&ok=1');
    const recorded = navs(events);
    expect(recorded).toHaveLength(2);
    const nav = recorded[1]!;
    expect(nav.data.trigger).toBe('pushState');
    expect(nav.data.to).toContain('/next');
    expect(nav.data.to).toContain('token=[REDACTED]');
    expect(nav.data.to).not.toContain('fake-secret-123');
    expect(nav.data.from).toBeDefined();
  });

  it('records replaceState', () => {
    const { events, fn } = recorder();
    disposers.push(installNavigationCollector(window, fn));
    window.history.replaceState(null, '', '/replaced');
    expect(navs(events).map((n) => n.data.trigger)).toEqual(['load', 'replaceState']);
  });

  it('records popstate and hashchange', () => {
    const { events, fn } = recorder();
    disposers.push(installNavigationCollector(window, fn));
    window.dispatchEvent(new Event('popstate'));
    window.dispatchEvent(new Event('hashchange'));
    expect(navs(events).map((n) => n.data.trigger)).toEqual([
      'load',
      'popstate',
      'hashchange',
    ]);
  });

  it('restores history methods on dispose', () => {
    const { events, fn } = recorder();
    const dispose = installNavigationCollector(window, fn);
    const before = navs(events).length;
    dispose();
    window.history.pushState(null, '', '/after-dispose');
    expect(navs(events).length).toBe(before);
  });
});
