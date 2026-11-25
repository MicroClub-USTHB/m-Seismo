import { describe, expect, it } from 'vitest';
import { scenarios, scenarioById, REQUIRED_SCENARIO_IDS } from '@app/lab/registry';

const GROUPS = ['errors', 'network', 'reporting', 'lifecycle', 'ui'] as const;
const EXPECTED = ['DETECTED_AUTO', 'MANUAL_ONLY', 'NOT_SUPPORTED', 'LIMITED', 'KNOWN_GAP'] as const;

describe('lab registry', () => {
  it('contains exactly the 25 required scenarios', () => {
    expect(scenarios.map((scenario) => scenario.id).sort()).toEqual([...REQUIRED_SCENARIO_IDS].sort());
    expect(scenarios).toHaveLength(25);
  });

  it('has unique ids', () => {
    const ids = scenarios.map((scenario) => scenario.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('uses only known groups and expected categories', () => {
    for (const scenario of scenarios) {
      expect(GROUPS).toContain(scenario.group);
      expect(EXPECTED).toContain(scenario.expected);
      expect(scenario.title.length).toBeGreaterThan(0);
      expect(scenario.notes.length).toBeGreaterThan(20);
    }
  });

  it('gives every scenario a run and verify function', () => {
    for (const scenario of scenarios) {
      expect(typeof scenario.run).toBe('function');
      expect(typeof scenario.verify).toBe('function');
    }
  });

  it('requires manual steps for every assisted scenario', () => {
    for (const scenario of scenarios.filter((item) => item.mode === 'assisted')) {
      expect(scenario.manualSteps?.length ?? 0).toBeGreaterThan(0);
    }
  });

  it('looks scenarios up by id', () => {
    expect(scenarioById('E01')?.id).toBe('E01');
    expect(scenarioById('E99')).toBeUndefined();
  });
});
