import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { configure, getConfig, resetConfig } from '@sdk/core/config';
import type { InitOptions, LauncherPosition, SdkConfig } from '@sdk/core/config';
import {
  DEFAULT_MAX_AGE_MS,
  DEFAULT_MAX_EVENTS,
} from '@sdk/diagnostics/types';

const ENDPOINT = 'https://receiver.test/api/reports';

let warnSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  resetConfig();
  warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  resetConfig();
  warnSpy.mockRestore();
});

function init(options: Partial<InitOptions> = {}): SdkConfig {
  const ok = configure({ endpoint: ENDPOINT, ...options });
  expect(ok).toBe(true);
  const config = getConfig();
  if (config === null) throw new Error('config was not stored');
  return config;
}

describe('configuration defaults (table-driven)', () => {
  it.each<
    readonly [string, (config: SdkConfig) => unknown, unknown]
  >([
    ['timeoutMs', (c) => c.timeoutMs, 10_000],
    ['shortcut default is Ctrl+Enter', (c) => c.shortcut, {
      key: 'enter',
      alt: false,
      ctrl: true,
      meta: false,
      shift: false,
    }],
    ['launcher disabled by default', (c) => c.launcher, { enabled: false }],
    ['screenshot enabled by default', (c) => c.screenshot, true],
    ['sensitiveQueryParams omitted by default', (c) => c.sensitiveQueryParams, undefined],
    ['onEvent omitted by default', (c) => c.onEvent, undefined],
    ['diagnostics.maxEvents', (c) => c.diagnostics.maxEvents, DEFAULT_MAX_EVENTS],
    ['diagnostics.maxAgeMs', (c) => c.diagnostics.maxAgeMs, DEFAULT_MAX_AGE_MS],
    ['diagnostics.captureUncaughtErrors', (c) => c.diagnostics.captureUncaughtErrors, true],
    ['diagnostics.captureUnhandledRejections', (c) => c.diagnostics.captureUnhandledRejections, true],
    ['diagnostics.captureNavigation', (c) => c.diagnostics.captureNavigation, true],
    ['diagnostics.captureInteractions', (c) => c.diagnostics.captureInteractions, true],
    ['diagnostics.captureNetwork', (c) => c.diagnostics.captureNetwork, true],
    ['diagnostics.captureElementText', (c) => c.diagnostics.captureElementText, false],
    ['diagnostics.includeInReports', (c) => c.diagnostics.includeInReports, true],
    ['diagnostics.ignoreUrls omitted by default', (c) => c.diagnostics.ignoreUrls, undefined],
    ['reporting.autoReport', (c) => c.reporting.autoReport, 'off'],
    ['reporting.sendCapturedExceptions', (c) => c.reporting.sendCapturedExceptions, true],
    ['reporting.dedupWindowMs', (c) => c.reporting.dedupWindowMs, 60_000],
    ['reporting.maxAutoReportsPerMinute', (c) => c.reporting.maxAutoReportsPerMinute, 5],
    ['reporting.maxAutoReportsPerSession', (c) => c.reporting.maxAutoReportsPerSession, 50],
    ['reporting.beforeSend omitted by default', (c) => c.reporting.beforeSend, undefined],
    ['appVersion omitted by default', (c) => c.appVersion, undefined],
    ['environment omitted by default', (c) => c.environment, undefined],
    ['projectKey omitted by default', (c) => c.projectKey, undefined],
  ])('default %s is %o', (_label, read, expected) => {
    expect(read(init())).toEqual(expected);
  });
});

describe('configuration isolation', () => {
  it('does not pick up mutations of the input object after init()', () => {
    const diagnostics: {
      maxEvents: number;
      ignoreUrls: string[];
      captureNetwork: boolean;
    } = { maxEvents: 25, ignoreUrls: ['https://a.test'], captureNetwork: true };
    const privacy = { sensitiveQueryParams: ['session'] };
    const launcher: { label: string; position: LauncherPosition } = {
      label: 'Bug',
      position: 'top-left',
    };

    const config = init({ diagnostics, launcher, privacy });

    // Mutate the very objects handed in.
    diagnostics.maxEvents = 1;
    diagnostics.ignoreUrls.push('https://b.test');
    diagnostics.captureNetwork = false;
    privacy.sensitiveQueryParams.push('extra');
    launcher.label = 'mutated';
    launcher.position = 'bottom-left';

    const after = getConfig();
    expect(after).toBe(config);
    expect(after?.diagnostics.maxEvents).toBe(25);
    expect(after?.diagnostics.captureNetwork).toBe(true);
    expect(after?.diagnostics.ignoreUrls).toEqual(['https://a.test']);
    expect(after?.sensitiveQueryParams).toEqual(['session']);
    expect(after?.launcher).toEqual({ enabled: true, label: 'Bug', position: 'top-left' });
  });

  it('ignores unknown options without warning or side effects', () => {
    const options = {
      endpoint: ENDPOINT,
      somethingUnknown: true,
      diagnostics: { maxEvents: 10, anotherUnknown: 'x' },
    } as unknown as InitOptions;

    const ok = configure(options);
    expect(ok).toBe(true);
    expect(warnSpy).not.toHaveBeenCalled();
    expect(getConfig()?.diagnostics.maxEvents).toBe(10);
    expect(getConfig()).not.toHaveProperty('somethingUnknown');
  });

  it('a valid re-init fully replaces the previous configuration', () => {
    init({ appVersion: '1.0', projectKey: 'pk_old' });
    init({ appVersion: '2.0' });
    const config = getConfig();
    expect(config?.appVersion).toBe('2.0');
    expect(config?.projectKey).toBeUndefined();
  });
});
