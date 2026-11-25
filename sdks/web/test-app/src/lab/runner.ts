/**
 * Scenario runner: clean environment, trigger, verify, record.
 *
 * Guarantees:
 * - each scenario starts from a clean state: receiver reset, SDK destroyed
 *   (which clears the diagnostics buffer and dedup/limit state) and re-initialised
 *   with the scenario's config;
 * - a verifier can never take the run down: {@link safeVerify} turns a throw
 *   into a `fail` verdict;
 * - results are keyed by scenario id, so re-running replaces the previous entry.
 */
import { destroy, getConfig } from '@m-seismo/web';
import {
  clearDiagnostics,
  getDiagnosticsCount,
  getDiagnosticsSelection,
  getDiagnosticsSnapshot,
  getReportProcessorStats,
} from '@sdk/internal';
import { LAB_ENDPOINT, LAB_RECEIVER_BASE, initLabSdk } from './helpers/init';
import { ReceiverClient } from './helpers/receiver';
import { WarningCapture } from './helpers/console';
import { safeVerify } from './verdict';
import type { LabContext, LabResult, LabScenario, Verdict } from './types';

const receiver = new ReceiverClient(LAB_RECEIVER_BASE);
const warnings = new WarningCapture();

const context: LabContext = {
  endpoint: LAB_ENDPOINT,
  receiverBase: LAB_RECEIVER_BASE,
  receiver,
  snapshot: () => getDiagnosticsSnapshot(),
  selection: () => getDiagnosticsSelection(),
  count: () => getDiagnosticsCount(),
  stats: () => getReportProcessorStats(),
  config: () => getConfig(),
  init: (overrides) => initLabSdk(overrides),
  destroy: () => destroy(),
  clearDiagnostics: () => clearDiagnostics(),
  warnings,
};

/** The shared lab context (also used by the page UI). */
export function labContext(): LabContext {
  return context;
}

const results = new Map<string, LabResult>();

/** All recorded results, in `E01…E25` order. */
export function getResults(): LabResult[] {
  return Array.from(results.values());
}

/** Clears all recorded results. */
export function clearResults(): void {
  results.clear();
}

/**
 * Resets the environment for a scenario: receiver reset (best-effort — it is
 * expected to fail for the offline scenario), SDK destroy, then re-init with the
 * scenario's config.
 */
async function resetEnvironment(scenario: LabScenario): Promise<void> {
  try {
    await receiver.reset();
  } catch {
    // Offline / unreachable receiver is fine; the scenario verifies SDK state.
  }
  destroy();
  initLabSdk(scenario.config ?? {});
}

function needsManualResult(scenario: LabScenario): LabResult {
  const steps = scenario.manualSteps?.join(' ') ?? 'Follow the manual steps in the lab.';
  return {
    id: scenario.id,
    title: scenario.title,
    group: scenario.group,
    mode: scenario.mode,
    expected: scenario.expected,
    status: 'needs-manual',
    observed: `Assisted scenario. ${steps}`,
  };
}

/** Runs one scenario from a clean state and records its result. */
export async function runScenario(scenario: LabScenario): Promise<LabResult> {
  warnings.clear();
  warnings.start();
  let verdict: Verdict;
  try {
    await resetEnvironment(scenario);
    if (scenario.setup !== undefined) await scenario.setup(context);
    await scenario.run(context);
    verdict = await safeVerify(() => scenario.verify(context));
  } catch (error) {
    verdict = { status: 'fail', observed: `run threw: ${String(error)}` };
  } finally {
    warnings.stop();
  }

  const result: LabResult = {
    id: scenario.id,
    title: scenario.title,
    group: scenario.group,
    mode: scenario.mode,
    expected: scenario.expected,
    status: verdict.status,
    observed: verdict.observed,
    ...(verdict.evidence !== undefined ? { evidence: verdict.evidence } : {}),
  };
  results.set(scenario.id, result);
  return result;
}

/**
 * Runs a list of scenarios. Assisted scenarios are skipped (recorded as
 * `needs-manual`) unless `includeAssisted` is true.
 */
export async function runScenarios(
  scenarios: readonly LabScenario[],
  options: { includeAssisted?: boolean } = {},
): Promise<LabResult[]> {
  const includeAssisted = options.includeAssisted ?? false;
  const out: LabResult[] = [];
  for (const scenario of scenarios) {
    if (scenario.mode === 'assisted' && !includeAssisted) {
      const result = needsManualResult(scenario);
      results.set(result.id, result);
      out.push(result);
      continue;
    }
    out.push(await runScenario(scenario));
  }
  return out;
}
