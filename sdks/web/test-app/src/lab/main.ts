/**
 * Error-laboratory page controller.
 *
 * Renders the scenario table, wires the Run/Reset/Export controls and exposes a
 * small programmatic API (`window.__lab*`) used by the Playwright lab spec.
 * All SDK interaction goes through the runner; this file is DOM glue only.
 */
import { destroy } from '@m-seismo/web';
import { scenarios, scenarioById } from './registry';
import { labContext, runScenario, runScenarios, getResults, clearResults } from './runner';
import { initLabSdk } from './helpers/init';
import { accepts } from './verdict';
import type { LabResult } from './types';

const rowsBody = document.querySelector<HTMLTableSectionElement>('[data-testid="lab-rows"]');
const metaEl = document.querySelector<HTMLElement>('[data-testid="lab-meta"]');

interface RowRefs {
  statusCell: HTMLTableCellElement;
}

const rowRefs = new Map<string, RowRefs>();

function statusLabel(result: LabResult): string {
  if (result.status === 'needs-manual') return 'needs manual';
  return result.status;
}

function renderRows(): void {
  if (rowsBody === null) return;
  rowsBody.innerHTML = '';
  for (const scenario of scenarios) {
    const tr = document.createElement('tr');

    const idCell = document.createElement('td');
    idCell.className = 'id';
    idCell.textContent = scenario.id;

    const titleCell = document.createElement('td');
    titleCell.textContent = scenario.title;
    if (scenario.mode === 'assisted') {
      const hint = document.createElement('div');
      hint.className = 'observed';
      hint.textContent = 'assisted — needs manual confirmation';
      titleCell.appendChild(hint);
    }

    const groupCell = document.createElement('td');
    groupCell.className = 'group';
    groupCell.textContent = scenario.group;

    const expectedCell = document.createElement('td');
    expectedCell.className = 'expected';
    expectedCell.textContent = scenario.expected;

    const runCell = document.createElement('td');
    const runButton = document.createElement('button');
    runButton.type = 'button';
    runButton.textContent = 'Run';
    runButton.addEventListener('click', () => void runOne(scenario.id));
    runCell.appendChild(runButton);

    const statusCell = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = 'badge pending';
    badge.textContent = 'not run';
    statusCell.appendChild(badge);

    tr.append(idCell, titleCell, groupCell, expectedCell, runCell, statusCell);
    rowsBody.appendChild(tr);
    rowRefs.set(scenario.id, { statusCell });
  }
}

function updateRow(result: LabResult): void {
  const refs = rowRefs.get(result.id);
  if (refs === undefined) return;
  refs.statusCell.innerHTML = '';

  const badge = document.createElement('span');
  badge.className = `badge ${statusLabel(result)}`;
  badge.textContent = statusLabel(result);
  refs.statusCell.appendChild(badge);

  if (result.status !== 'needs-manual') {
    const mismatch = !accepts(result.expected, result.status);
    const observed = document.createElement('div');
    observed.className = 'observed';
    observed.textContent = result.observed;
    if (mismatch) observed.style.color = '#8f1d16';
    refs.statusCell.appendChild(observed);
    if (mismatch) {
      const flag = document.createElement('div');
      flag.className = 'expected mismatch';
      flag.textContent = 'unexpected for its category';
      refs.statusCell.appendChild(flag);
    }
  }

  if (result.evidence !== undefined) {
    const details = document.createElement('details');
    const summary = document.createElement('summary');
    summary.textContent = 'evidence';
    const pre = document.createElement('pre');
    pre.textContent = safeJson(result.evidence);
    details.append(summary, pre);
    refs.statusCell.appendChild(details);
  }
}

function safeJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2) ?? String(value);
  } catch {
    return String(value);
  }
}

function refreshAllRows(): void {
  for (const result of getResults()) updateRow(result);
}

async function updateMeta(): Promise<void> {
  if (metaEl === null) return;
  const config = labContext().config();
  let mode = 'unknown';
  try {
    mode = (await labContext().receiver.getStats()).mode;
  } catch {
    mode = 'unreachable';
  }
  metaEl.textContent = `receiver mode: ${mode} · endpoint: ${labContext().endpoint} · config: ${
    config === null ? 'disabled' : JSON.stringify({ reporting: config.reporting, diagnostics: { maxEvents: config.diagnostics.maxEvents }, shortcut: config.shortcut })
  }`;
}

async function runOne(id: string): Promise<LabResult | null> {
  const scenario = scenarioById(id);
  if (scenario === undefined) return null;
  const result = await runScenario(scenario);
  updateRow(result);
  await updateMeta();
  return result;
}

async function runAll(includeAssisted = false): Promise<LabResult[]> {
  const results = await runScenarios(scenarios, { includeAssisted });
  for (const result of results) updateRow(result);
  await updateMeta();
  return results;
}

async function resetEverything(): Promise<void> {
  clearResults();
  destroy();
  try {
    await labContext().receiver.reset();
  } catch {
    // receiver may be unreachable; that is itself a valid lab state.
  }
  initLabSdk({});
  renderRows();
  await updateMeta();
}

function exportJson(): void {
  const payload = JSON.stringify(getResults(), null, 2);
  const blob = new Blob([payload], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = 'lab-results.json';
  anchor.click();
  URL.revokeObjectURL(url);
}

document.querySelector('[data-testid="lab-run-all"]')?.addEventListener('click', () => {
  void runAll(false);
});
document.querySelector('[data-testid="lab-run-assisted"]')?.addEventListener('click', () => {
  void runAll(true);
});
document.querySelector('[data-testid="lab-reset"]')?.addEventListener('click', () => {
  void resetEverything();
});
document.querySelector('[data-testid="lab-export"]')?.addEventListener('click', exportJson);

window.__labRun = runOne;
window.__labRunAll = runAll;
window.__labReset = resetEverything;
window.__labScenarios = scenarios;

Object.defineProperty(window, '__labResults', {
  configurable: true,
  get: () => getResults(),
});

async function boot(): Promise<void> {
  renderRows();
  initLabSdk({});
  await updateMeta();
}

void boot();
