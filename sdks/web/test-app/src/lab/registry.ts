/**
 * The single source of truth for the error laboratory: all 25 scenarios.
 *
 * The registry is imported by the lab page, by the Playwright lab spec and by
 * the unit tests (integrity checks), so it must stay free of DOM side effects at
 * import time.
 */
import { errorScenarios } from './scenarios/errors';
import { networkScenarios } from './scenarios/network';
import { reportingScenarios } from './scenarios/reporting';
import { lifecycleScenarios } from './scenarios/lifecycle';
import { uiScenarios } from './scenarios/ui';
import type { LabScenario } from './types';

/** All scenarios, in ascending id order (E01 … E25). */
export const scenarios: readonly LabScenario[] = [
  ...errorScenarios,
  ...networkScenarios,
  ...reportingScenarios,
  ...lifecycleScenarios,
  ...uiScenarios,
];

/** Look up a scenario by id. */
export function scenarioById(id: string): LabScenario | undefined {
  return scenarios.find((scenario) => scenario.id === id);
}

/**
 * The full set of ids the plan requires. Kept here so the integrity test can
 * assert nothing is missing or duplicated.
 */
export const REQUIRED_SCENARIO_IDS: readonly string[] = [
  'E01', 'E02', 'E03', 'E04', 'E05', 'E06', 'E07', 'E08', 'E09', 'E10',
  'E11', 'E12', 'E13', 'E14', 'E15', 'E16', 'E17', 'E18', 'E19', 'E20',
  'E21', 'E22', 'E23', 'E24', 'E25',
];
