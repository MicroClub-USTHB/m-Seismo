/**
 * Ambient types for the error-laboratory page API.
 *
 * Declared once here so both the page controller and the Playwright spec share
 * the same `window.__lab*` shape (two separate `declare global` blocks with
 * structurally similar but distinct local types would clash).
 */
import type { LabResult, LabScenario } from './types';

declare global {
  interface Window {
    __labRun?: (id: string) => Promise<LabResult | null>;
    __labRunAll?: (includeAssisted?: boolean) => Promise<LabResult[]>;
    __labReset?: () => Promise<void>;
    __labResults?: LabResult[];
    __labScenarios?: readonly LabScenario[];
  }
}

export {};
