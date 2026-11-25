/**
 * Lifecycle scenarios: E18–E19.
 */
import { init } from '@m-seismo/web';
import { cancelForm, openFormAndWait } from '../helpers/form';
import { setLabEventSink, LAB_ENDPOINT } from '../helpers/init';
import { waitFor } from '../helpers/waiters';
import { fail, pass } from '../verdict';
import type { DiagnosticEvent } from '@sdk/diagnostics/types';
import type { LabScenario, Verdict } from '../types';

// --- E18 -------------------------------------------------------------------
const e18: Record<string, number> = {};

const e18Scenario: LabScenario = {
  id: 'E18',
  title: 'init() twice and three times',
  group: 'lifecycle',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes:
    'A repeated init() tears the previous instance down first, so there is one launcher, one form host and one listener of each kind, and one event per trigger. ' +
    'A duplicated listener would double every buffered event (dedup would hide a doubled *report*, so the lab counts buffer events).',
  config: { reporting: { autoReport: 'errors' } },
  async run(ctx) {
    let formOpens = 0;
    setLabEventSink((event) => {
      if (event.type === 'form:open') formOpens += 1;
    });

    ctx.init();
    ctx.init();
    ctx.init();

    e18['launchers'] = document.querySelectorAll('[data-mseismo-launcher]').length;

    formOpens = 0;
    await openFormAndWait();
    e18['formHosts'] = document.querySelectorAll('[data-mseismo-form]').length;
    cancelForm();
    e18['formOpens'] = formOpens;

    ctx.clearDiagnostics();
    setTimeout(() => {
      throw new Error('E18 single error');
    }, 0);
    void Promise.reject(new Error('E18 single rejection'));
    await fetch(`${ctx.receiverBase}/api/status/500`);

    await waitFor(() => ctx.snapshot().length >= 3, 5_000);
    const snapshot = ctx.snapshot();
    const countKind = (kind: DiagnosticEvent['kind']): number => snapshot.filter((event) => event.kind === kind).length;
    e18['errorEvents'] = countKind('error');
    e18['rejectionEvents'] = countKind('unhandledrejection');
    e18['networkEvents'] = countKind('network');
    e18['stored'] = (await ctx.receiver.getReports()).count;
    setLabEventSink(null);
  },
  async verify(): Promise<Verdict> {
    const checks: Record<string, boolean> = {
      oneLauncher: e18['launchers'] === 1,
      oneFormHost: e18['formHosts'] === 1,
      oneFormOpen: e18['formOpens'] === 1,
      oneErrorListener: e18['errorEvents'] === 1,
      oneRejectionListener: e18['rejectionEvents'] === 1,
      oneFetchWrap: e18['networkEvents'] === 1,
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    return failed.length === 0
      ? pass('single listener/DOM instance after three inits; one event per trigger', { ...e18, checks })
      : fail(`double-init checks failed: ${failed.join(', ')}`, { ...e18, checks });
  },
};

// --- E19 -------------------------------------------------------------------
const e19: Record<string, unknown> = {};

const e19Scenario: LabScenario = {
  id: 'E19',
  title: 'Invalid configuration',
  group: 'lifecycle',
  mode: 'inpage',
  expected: 'DETECTED_AUTO',
  notes:
    'Each invalid value logs one warning and never throws; the SDK disables or falls back to a safe default and the host is unaffected. ' +
    'Invalid launcher position and unknown options (e.g. "theme") fall back SILENTLY — recorded as findings.',
  config: {},
  async run(ctx) {
    const results: Record<string, boolean> = {};
    results['missingEndpoint'] = init({ endpoint: undefined as unknown as string });
    results['nonHttpEndpoint'] = init({ endpoint: 'ftp://example.com/api' });
    // A modifier-only combo is invalid (a single unknown key like "NotAKey" is
    // in fact a valid combo, so it must not be used here).
    results['badShortcut'] = init({ endpoint: LAB_ENDPOINT, shortcut: 'Ctrl+Shift' });
    results['badLauncherPosition'] = init({
      endpoint: LAB_ENDPOINT,
      launcher: { position: 'middle' as unknown as 'bottom-right' },
    });
    results['badDiagnostics'] = init({ endpoint: LAB_ENDPOINT, diagnostics: { maxEvents: -1 } });
    results['badReporting'] = init({ endpoint: LAB_ENDPOINT, reporting: { autoReport: 'sometimes' as unknown as 'off' } });
    // Unknown/removed option: the SDK has no theme support (Phase 4 skipped).
    results['unknownThemeOption'] = init({ endpoint: LAB_ENDPOINT, theme: 'neon' } as unknown as { endpoint: string });
    results['recover'] = ctx.init();

    e19['results'] = results;
    e19['warnings'] = ctx.warnings.messages.map((message) => message.text);
    e19['sdkEnabledAfterRecover'] = ctx.config() !== null;
  },
  async verify(): Promise<Verdict> {
    const results = e19['results'] as Record<string, boolean>;
    const warnings = e19['warnings'] as string[];
    const has = (needle: string): boolean => warnings.some((warning) => warning.includes(needle));
    const checks: Record<string, boolean> = {
      missingEndpointDisabled: results['missingEndpoint'] === false,
      nonHttpDisabled: results['nonHttpEndpoint'] === false,
      badShortcutStillEnabled: results['badShortcut'] === true,
      recoverEnabled: results['recover'] === true && e19['sdkEnabledAfterRecover'] === true,
      warnedEndpoint: has('endpoint'),
      warnedShortcut: has('shortcut'),
      warnedDiagnostics: has('diagnostics.maxEvents'),
      warnedReporting: has('reporting.autoReport'),
    };
    const notes = {
      launcherPositionSilent: results['badLauncherPosition'] === true && !has('launcher'),
      themeOptionSilent: !has('theme'),
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    const evidence = { warnings, results, notes };
    return failed.length === 0
      ? pass('each invalid config warns once, never throws, and recovers; silent fallbacks noted', evidence)
      : fail(`invalid-config checks failed: ${failed.join(', ')}`, evidence);
  },
};

export const lifecycleScenarios: readonly LabScenario[] = [e18Scenario, e19Scenario];
