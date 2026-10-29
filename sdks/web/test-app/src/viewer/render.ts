/**
 * DOM renderer for the "Output" report viewer. Takes the pure view model from
 * `./model.ts` plus the raw stored report, and produces a card with tabs:
 * Summary, Breadcrumbs, Network, Raw JSON, and Redaction check.
 *
 * Safety: every dynamic value is written with `textContent` or via `escapeHtml`
 * in `highlightJson`. Hostile report content (a description containing a script
 * tag, a message with HTML) can only ever appear as text.
 */
import type { ReportPayload } from '@sdk/core/types';
import { escapeHtml, highlightJson, diffJson, type DiffRow } from '../ui/html';
import { scanForSecrets } from './secretScan';
import { toViewModel, type ReportViewModel } from './model';

export interface ViewerOptions {
  /** Candidate secrets to check for in the redaction panel. */
  readonly secrets: readonly string[];
  /** Called when the user clicks "Copy raw JSON". */
  readonly onCopy?: (text: string) => void;
}

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className !== undefined) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function chip(label: string, tone: string): HTMLElement {
  const span = el('span', 'badge', label);
  span.dataset['tone'] = tone;
  return span;
}

function kvList(entries: ReadonlyArray<{ key: string; value: string; absent: boolean }>): HTMLElement {
  const dl = el('dl', 'kv-grid');
  for (const entry of entries) {
    dl.append(el('dt', undefined, entry.key));
    const dd = el('dd', entry.absent ? 'absent' : undefined, entry.value);
    dl.append(dd);
  }
  return dl;
}

function summaryPanel(view: ReportViewModel): HTMLElement {
  const wrap = el('div');
  const header = el('div', 'card-actions');
  header.append(chip(view.header.type.label, view.header.type.tone));
  header.append(chip(`trigger: ${view.header.trigger}`, 'neutral'));
  header.append(chip(`id: ${view.header.reportId}`, 'neutral'));
  wrap.append(header);

  const meta = el('dl', 'kv-grid');
  const metaEntries: Array<[string, string]> = [
    ['Schema version', view.header.schemaVersion],
    ['SDK version', view.header.sdkVersion],
    ['Timestamp', view.header.timestamp],
  ];
  for (const [key, value] of metaEntries) {
    meta.append(el('dt', undefined, key));
    meta.append(el('dd', undefined, value));
  }
  wrap.append(meta);

  const descHeading = el('h4', undefined, 'Description');
  wrap.append(descHeading);
  const desc = el('p', undefined, view.hasDescription ? view.description : '(no description)');
  wrap.append(desc);

  if (view.error.present) {
    const errHeading = el('h4', undefined, 'Attached error');
    wrap.append(errHeading);
    const errMeta = el('dl', 'kv-grid');
    const rows: Array<[string, string]> = [
      ['Type', view.error.type],
      ['Message', view.error.message],
      ['Fingerprint', view.error.fingerprint ?? 'not available'],
      ['Occurrences', view.error.occurrences === null ? 'not available' : String(view.error.occurrences)],
      ['Flags', view.error.flags.length > 0 ? view.error.flags.join(', ') : 'none'],
    ];
    for (const [key, value] of rows) {
      errMeta.append(el('dt', undefined, key));
      errMeta.append(el('dd', undefined, value));
    }
    wrap.append(errMeta);
    if (view.error.stack !== null) {
      const stackHeading = el('h4', undefined, 'Stack');
      wrap.append(stackHeading);
      wrap.append(el('pre', 'code stack-block', view.error.stack));
    }
  } else {
    wrap.append(el('p', 'subtle', 'No error attached to this report.'));
  }

  const ctxHeading = el('h4', undefined, 'Context');
  wrap.append(ctxHeading);
  wrap.append(kvList(view.context));
  return wrap;
}

function breadcrumbsPanel(view: ReportViewModel): HTMLElement {
  if (view.breadcrumbs.length === 0) {
    return el('p', 'subtle', 'No breadcrumbs (diagnostics may have been disabled).');
  }
  const ol = el('ol', 'timeline');
  view.breadcrumbs.forEach((crumb, index) => {
    const li = el('li');
    li.dataset['icon'] = crumb.icon;
    if (index === view.highlight) li.dataset['highlight'] = 'true';
    const head = el('div');
    const cat = el('span', 'badge', crumb.category);
    cat.dataset['tone'] = crumb.level === 'error' ? 'fail' : 'info';
    head.append(cat, document.createTextNode(` ${crumb.timestamp}`));
    if (index === view.highlight) head.append(document.createTextNode('  (nearest before error)'));
    li.append(head);
    li.append(el('div', undefined, crumb.message));
    if (crumb.data !== null) {
      li.append(el('pre', 'code', JSON.stringify(crumb.data, null, 2)));
    }
    ol.append(li);
  });
  return ol;
}

function networkPanel(view: ReportViewModel): HTMLElement {
  if (view.networkFailures.length === 0) {
    return el('p', 'subtle', 'No network failures recorded.');
  }
  const table = el('table', 'table');
  const thead = el('thead');
  const headRow = el('tr');
  for (const heading of ['Method', 'URL', 'Status', 'Duration', 'Category']) {
    headRow.append(el('th', undefined, heading));
  }
  thead.append(headRow);
  table.append(thead);
  const tbody = el('tbody');
  for (const failure of view.networkFailures) {
    const row = el('tr');
    row.append(el('td', undefined, failure.method));
    const urlCell = el('td', 'mono', failure.url);
    row.append(urlCell);
    row.append(el('td', undefined, failure.status));
    row.append(el('td', undefined, failure.durationMs === null ? '—' : `${failure.durationMs} ms`));
    row.append(el('td', undefined, failure.category));
    tbody.append(row);
  }
  table.append(tbody);
  return table;
}

function rawPanel(report: ReportPayload, onCopy?: (text: string) => void): HTMLElement {
  const wrap = el('div');
  const json = JSON.stringify(report, null, 2);
  const pre = el('pre', 'code');
  pre.innerHTML = highlightJson(json);
  wrap.append(pre);
  const actions = el('div', 'card-actions');
  const copy = el('button', 'btn small copy-btn', 'Copy raw JSON');
  copy.type = 'button';
  copy.dataset['testid'] = 'viewer-copy-json';
  copy.addEventListener('click', () => onCopy?.(json));
  actions.append(copy);
  wrap.append(actions);
  return wrap;
}

function screenshotRow(view: ReportViewModel): HTMLElement {
  const s = view.screenshot;
  if (!s.present) return el('p', 'subtle', 'No screenshot attached.');
  const wrap = el('div');
  const meta = el('dl', 'kv-grid');
  const rows: Array<[string, string]> = [
    ['Name', s.name],
    ['MIME type', s.mimeType],
    ['Size', `${s.sizeBytes} bytes`],
    ['Redacted', s.dataUrl === null ? 'yes (payload stripped)' : 'no (inline base64 present)'],
  ];
  for (const [key, value] of rows) {
    meta.append(el('dt', undefined, key));
    meta.append(el('dd', undefined, value));
  }
  wrap.append(meta);
  if (s.dataUrl !== null) {
    const img = el('img');
    img.src = s.dataUrl;
    img.alt = 'Attached screenshot';
    img.style.maxWidth = '100%';
    wrap.append(img);
  }
  return wrap;
}

function redactionPanel(report: ReportPayload, secrets: readonly string[]): HTMLElement {
  const wrap = el('div');
  const scan = scanForSecrets(report, secrets);
  const note = el(
    'p',
    'subtle',
    'Baseline privacy only (Phase 6 pending). This checks the stored payload for known fake secrets; it does not scrub anything.',
  );
  wrap.append(note);

  const summary = el('p');
  summary.append(chip(`${scan.found.length} leaked`, scan.found.length === 0 ? 'pass' : 'fail'));
  summary.append(document.createTextNode(' '));
  summary.append(chip(`${scan.clean.length} redacted`, 'info'));
  summary.append(document.createTextNode(' '));
  summary.append(chip(`${scan.redactionMarkers} redaction markers`, 'neutral'));
  wrap.append(summary);

  if (secrets.length === 0) {
    wrap.append(el('p', 'subtle', 'No candidate secrets configured.'));
    return wrap;
  }

  const list = el('ul', 'timeline');
  for (const hit of scan.found) {
    const li = el('li');
    li.dataset['icon'] = '✗';
    li.append(el('span', 'redaction-miss', 'LEAK'));
    li.append(document.createTextNode(` ${hit.secret}`));
    li.append(el('div', 'subtle', `found ${hit.count}× (first at ${hit.path ?? '(root)'})`));
    list.append(li);
  }
  const redacted = el('p', 'subtle');
  redacted.textContent = `Redacted / absent: ${scan.clean.length} candidate secret(s) not found in the payload.`;
  wrap.append(redacted, list);
  return wrap;
}

export interface ViewerRenderResult {
  readonly element: HTMLElement;
  /** Panel IDs in tab order. */
  readonly tabs: readonly string[];
}

/** Renders one report card. */
export function renderReportCard(
  report: ReportPayload,
  options: ViewerOptions,
): HTMLElement {
  const view = toViewModel(report);
  const card = el('article', 'card viewer-card');
  card.dataset['testid'] = 'viewer-card';

  const topline = el('div', 'card-actions');
  topline.append(chip(view.header.type.label, view.header.type.tone));
  topline.append(el('strong', undefined, view.header.reportId));
  topline.append(el('span', 'subtle', view.header.timestamp));
  card.append(topline);

  const tabsBar = el('div', 'output-tabs');
  tabsBar.setAttribute('role', 'tablist');
  const panels: Array<{ id: string; label: string; node: HTMLElement }> = [
    { id: 'summary', label: 'Summary', node: summaryPanel(view) },
    { id: 'breadcrumbs', label: `Breadcrumbs (${view.breadcrumbs.length})`, node: breadcrumbsPanel(view) },
    { id: 'network', label: `Network (${view.networkFailures.length})`, node: networkPanel(view) },
    { id: 'screenshot', label: 'Screenshot', node: screenshotRow(view) },
    { id: 'raw', label: 'Raw JSON', node: rawPanel(report, options.onCopy) },
    { id: 'redaction', label: 'Redaction check', node: redactionPanel(report, options.secrets) },
  ];

  const panelHost = el('div');
  const buttons: HTMLButtonElement[] = [];
  const activate = (id: string): void => {
    for (const button of buttons) {
      const selected = button.dataset['panel'] === id;
      button.setAttribute('aria-selected', selected ? 'true' : 'false');
      button.tabIndex = selected ? 0 : -1;
    }
    panelHost.replaceChildren(panels.find((panel) => panel.id === id)!.node);
  };
  for (const panel of panels) {
    const button = el('button', undefined, panel.label);
    button.type = 'button';
    button.setAttribute('role', 'tab');
    button.dataset['panel'] = panel.id;
    button.dataset['testid'] = `viewer-tab-${panel.id}`;
    button.addEventListener('click', () => activate(panel.id));
    buttons.push(button);
    tabsBar.append(button);
  }

  card.append(tabsBar, panelHost);
  activate('summary');
  return card;
}

export interface ViewerState {
  readonly reports: readonly ReportPayload[];
  readonly secrets: readonly string[];
  readonly onCopy?: (text: string) => void;
  readonly compare?: boolean;
}

/** Renders the whole viewer (list of cards, optional compare grid). */
export function renderViewer(container: HTMLElement, state: ViewerState): void {
  container.replaceChildren();
  if (state.reports.length === 0) {
    container.append(el('p', 'subtle', 'No reports loaded. Click “Load latest from receiver”.'));
    return;
  }

  const options: ViewerOptions = {
    secrets: state.secrets,
    ...(state.onCopy !== undefined ? { onCopy: state.onCopy } : {}),
  };

  if (state.compare === true && state.reports.length >= 2) {
    const left = state.reports[state.reports.length - 2]!;
    const right = state.reports[state.reports.length - 1]!;
    const compare = el('div', 'card');
    compare.dataset['testid'] = 'viewer-compare';
    compare.append(el('h4', 'card-title', 'Compare last two reports (shallow diff)'));
    const rows = diffJson(left, right);
    if (rows.length === 0) {
      compare.append(el('p', 'subtle', 'The two reports are identical (ignoring field order).'));
    } else {
      compare.append(renderDiffTable(rows));
    }
    container.append(compare);
  }

  for (const report of state.reports) {
    container.append(renderReportCard(report, options));
  }
}

function renderDiffTable(rows: readonly DiffRow[]): HTMLElement {
  const table = el('table', 'table');
  const thead = el('thead');
  const headRow = el('tr');
  for (const heading of ['Path', 'Earlier', 'Latest']) headRow.append(el('th', undefined, heading));
  thead.append(headRow);
  table.append(thead);
  const tbody = el('tbody');
  for (const row of rows) {
    const tr = el('tr');
    tr.append(el('td', 'mono', row.path));
    tr.append(el('td', 'mono', row.left));
    tr.append(el('td', 'mono', row.right));
    tbody.append(tr);
  }
  table.append(tbody);
  return table;
}

/** Small helper so callers can reuse the escaping without importing two files. */
export { escapeHtml, diffJson, highlightJson };
