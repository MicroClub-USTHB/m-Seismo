import {
  buildReport,
  captureException,
  captureMessage,
  destroy,
  getConfig,
  getContext,
  init,
  openReportForm,
  sanitize,
  SDK_VERSION,
  sendReport,
} from '@m-seismo/web';
import type { ContextInput, InitOptions, ReportPayload, SdkEvent } from '@m-seismo/web';
import {
  clearDiagnostics,
  getDiagnosticsCount,
  getDiagnosticsSelection,
  getDiagnosticsSnapshot,
  getReportProcessorStats,
} from '@sdk/internal';
import { FAKE_SECRETS } from './lab/fixtures/secrets';
import { appendEntry, classifyLevel, classifySource, countsByLevel, DEFAULT_FILTER, filterEntries, sourcesOf, type LogEntry, type LogFilter } from './ui/eventLog';
import { renderViewer } from './viewer/render';
import './styles/tokens.css';
import './styles/layout.css';
import './styles/components.css';

/**
 * Test harness for the m-Seismo Web SDK. Wires every SDK trigger to a button and
 * renders a readable, bounded event log + report viewer. Presentation only: it
 * never changes what the SDK captures or sends.
 */

const $ = <T extends HTMLElement>(testid: string): T | null =>
  document.querySelector<T>(`[data-testid="${testid}"]`);

// --- Event log -------------------------------------------------------------
const logView = $('event-log');
const logCount = $('event-log-count');
let logEntries: LogEntry[] = [];
let logSeq = 0;
let logFilter: LogFilter = DEFAULT_FILTER;
let logPaused = false;

function timeNow(): string {
  return new Date().toISOString().slice(11, 23);
}

function renderLog(): void {
  if (!logView) return;
  logView.replaceChildren();
  const visible = filterEntries(logEntries, logFilter);
  const frag = document.createDocumentFragment();
  for (const entry of visible) {
    const li = document.createElement('li');
    li.dataset['level'] = entry.level;
    const time = document.createElement('span');
    time.className = 'el-time';
    time.textContent = entry.time;
    const icon = document.createElement('span');
    icon.className = 'el-icon';
    icon.textContent = entry.level === 'error' ? '✗' : entry.level === 'warn' ? '!' : '·';
    const source = document.createElement('span');
    source.className = 'el-source';
    source.textContent = entry.source;
    const message = document.createElement('span');
    message.className = 'el-msg';
    message.textContent = entry.message;
    li.append(time, icon, source, message);
    frag.append(li);
  }
  logView.append(frag);
  if (logCount) logCount.textContent = String(logEntries.length);
  if (!logPaused) logView.scrollTop = logView.scrollHeight;
}

function refreshSourceOptions(): void {
  const select = $<HTMLSelectElement>('event-log-source-filter');
  if (!select) return;
  const current = select.value;
  const options = ['all', ...sourcesOf(logEntries)];
  select.replaceChildren(
    ...options.map((source) => {
      const option = document.createElement('option');
      option.value = source;
      option.textContent = source;
      return option;
    }),
  );
  select.value = options.includes(current) ? current : 'all';
}

/** Records one message into the bounded event log. */
function record(message: string, level?: 'info' | 'warn' | 'error'): void {
  logSeq += 1;
  logEntries = appendEntry(logEntries, {
    id: logSeq,
    time: timeNow(),
    level: classifyLevel(message, level),
    source: classifySource(message),
    message,
  });
  refreshSourceOptions();
  renderLog();
}

// --- Toasts ----------------------------------------------------------------
let toastTimer: number | undefined;
function toast(message: string, tone: 'ok' | 'error' = 'ok'): void {
  const region = $('toast-region');
  if (!region) return;
  const node = document.createElement('div');
  node.className = 'toast';
  node.dataset['tone'] = tone;
  node.textContent = message;
  region.append(node);
  window.setTimeout(() => node.remove(), 3200);
  if (toastTimer !== undefined) window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => {
    if (region.childElementCount > 4) region.firstElementChild?.remove();
  }, 100);
}

// --- Theme -----------------------------------------------------------------
function currentTheme(): 'light' | 'dark' {
  return document.documentElement.dataset['theme'] === 'dark' ? 'dark' : 'light';
}

function updateThemeChip(): void {
  const chip = $('status-theme')?.querySelector('[data-role="text"]');
  const theme = currentTheme();
  if (chip) chip.textContent = `theme: ${theme}`;
}

function setTheme(theme: 'light' | 'dark'): void {
  document.documentElement.dataset['theme'] = theme;
  updateThemeChip();
  record(`theme set to ${theme}`);
}

function applyInitialTheme(): void {
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  document.documentElement.dataset['theme'] = prefersDark ? 'dark' : 'light';
  updateThemeChip();
  record(`initial theme = ${prefersDark ? 'dark' : 'light'} (system preference)`);
}

// --- SDK init --------------------------------------------------------------
const ENDPOINT =
  (import.meta.env.VITE_MSEISMO_ENDPOINT as string | undefined) ??
  'http://localhost:4010/api/reports';
const RECEIVER_BASE = new URL(ENDPOINT).origin;

function setStatusChip(testid: string, text: string, state: 'ok' | 'bad' | 'warn'): void {
  const chip = $(testid);
  if (!chip) return;
  chip.dataset['state'] = state;
  const label = chip.querySelector('[data-role="text"]');
  if (label) label.textContent = text;
}

function describeEvent(event: SdkEvent): string {
  switch (event.type) {
    case 'form:open':
      return 'sdk event: form:open';
    case 'form:close':
      return `sdk event: form:close (${event.reason})`;
    case 'submit:start':
      return `sdk event: submit:start id=${event.reportId}`;
    case 'submit:success':
      return `sdk event: submit:success id=${event.reportId} status=${event.status}`;
    case 'submit:failure':
      return `sdk event: submit:failure id=${event.reportId} kind=${event.kind}`;
    case 'validation:error':
      return 'sdk event: validation:error';
  }
}

const onEvent = (event: SdkEvent): void => {
  record(describeEvent(event));
  if (event.type === 'form:open') markStep('open');
  if (event.type === 'submit:start') markStep('send');
  if (event.type === 'submit:success') {
    markStep('read');
    toast('Report sent', 'ok');
    void refreshReceiver();
    void loadLatestReport();
  }
  if (event.type === 'submit:failure') toast('Report failed', 'error');
};

function initSdk(
  label: string,
  extra: Partial<Pick<InitOptions, 'shortcut' | 'diagnostics' | 'reporting'>> = {},
): void {
  const enabled = init({
    endpoint: ENDPOINT,
    appVersion: '0.0.0',
    environment: 'test-app',
    launcher: true,
    screenshot: true,
    onEvent,
    ...extra,
  });
  record(`${label}: init ${enabled ? 'enabled' : 'disabled'}`);
  setStatusChip('status-sdk', enabled ? 'initialized' : 'disabled', enabled ? 'ok' : 'warn');
}

function initSdkWithoutMeta(label: string): void {
  const enabled = init({
    endpoint: ENDPOINT,
    launcher: true,
    screenshot: true,
    onEvent,
  });
  record(`${label}: init ${enabled ? 'enabled' : 'disabled'} (no app metadata)`);
  setStatusChip('status-sdk', enabled ? 'initialized' : 'disabled', enabled ? 'ok' : 'warn');
}

function contextInput(reportId?: string): ContextInput {
  const config = getConfig();
  return {
    ...(reportId !== undefined ? { reportId } : {}),
    ...(config?.appVersion !== undefined ? { appVersion: config.appVersion } : {}),
    ...(config?.environment !== undefined ? { environment: config.environment } : {}),
    ...(config?.sensitiveQueryParams !== undefined
      ? { sensitiveQueryParams: config.sensitiveQueryParams }
      : {}),
  };
}

// --- Quick start checklist -------------------------------------------------
const doneSteps = new Set<string>();
function markStep(step: string): void {
  if (doneSteps.has(step)) return;
  doneSteps.add(step);
  const li = document.querySelector(`[data-testid="quickstart-steps"] li[data-step="${step}"]`);
  if (li) li.setAttribute('data-done', 'true');
}
function resetChecklist(): void {
  doneSteps.clear();
  document
    .querySelectorAll('[data-testid="quickstart-steps"] li')
    .forEach((li) => li.setAttribute('data-done', 'false'));
}

// --- Receiver panel --------------------------------------------------------
let receiverDown = false;

async function receiverHealth(): Promise<boolean> {
  try {
    const response = await fetch(`${RECEIVER_BASE}/__health`);
    return response.ok;
  } catch {
    return false;
  }
}

async function refreshReceiver(): Promise<void> {
  try {
    const response = await fetch(`${RECEIVER_BASE}/__stats`);
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const stats = (await response.json()) as { storedCount?: number; mode?: string };
    receiverDown = false;
    setStatusChip('status-receiver', 'receiver ok', 'ok');
    setStatusChip('receiver-state', 'reachable', 'ok');
    const count = $('receiver-count');
    if (count) count.textContent = `${stats.storedCount ?? 0} stored`;
    if (typeof stats.mode === 'string') {
      setStatusChip('status-mode', `mode: ${stats.mode}`, stats.mode === 'ok' ? 'ok' : 'warn');
      syncModeButtons(stats.mode);
    }
    const note = $('receiver-note');
    if (note) note.textContent = '';
  } catch {
    receiverDown = true;
    setStatusChip('status-receiver', 'receiver down', 'bad');
    setStatusChip('receiver-state', 'unreachable', 'bad');
    const note = $('receiver-note');
    if (note) note.textContent = `Receiver not reachable at ${RECEIVER_BASE}. Start it with "node tools/mock-receiver/server.mjs".`;
  }
}

function syncModeButtons(mode: string): void {
  document
    .querySelectorAll<HTMLButtonElement>('[data-testid="receiver-modes"] button')
    .forEach((button) => {
      const active = button.dataset['mode'] === mode;
      button.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
}

// --- Output viewer ---------------------------------------------------------
const viewerSecrets: string[] = [
  FAKE_SECRETS.longToken,
  FAKE_SECRETS.jwt,
  FAKE_SECRETS.longHex,
  FAKE_SECRETS.keyValue,
  FAKE_SECRETS.bearer,
  'fake-secret-123',
  'fake-secret-nav',
  FAKE_SECRETS.prosePassword,
];
let viewerReports: ReportPayload[] = [];
let viewerCompare = false;

function renderOutput(): void {
  const root = $('viewer-root');
  if (!root) return;
  const count = $('viewer-count');
  if (count) count.textContent = `${viewerReports.length} report${viewerReports.length === 1 ? '' : 's'}`;
  renderViewer(root, {
    reports: viewerReports,
    secrets: viewerSecrets,
    compare: viewerCompare,
    onCopy: (text) => {
      void copyText(text);
      toast('Raw JSON copied', 'ok');
    },
  });
}

async function loadLatestReport(): Promise<void> {
  try {
    const response = await fetch(`${RECEIVER_BASE}/__reports?full=1`);
    const body = (await response.json()) as { count: number; reports: ReportPayload[] };
    viewerReports = Array.isArray(body.reports) ? body.reports : [];
    renderOutput();
  } catch (error) {
    record(`viewer: load failed: ${String(error)}`);
    toast('Could not load reports', 'error');
  }
}

async function copyText(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    record('clipboard: write blocked by the browser', 'warn');
  }
}

// ---------------------------------------------------------------------------
applyInitialTheme();
initSdk('initial');
const endpointLabel = $('receiver-endpoint');
if (endpointLabel) endpointLabel.textContent = ENDPOINT;
const versionLabel = $('sdk-version');
if (versionLabel) versionLabel.textContent = `v${SDK_VERSION}`;
record(`SDK imported, SDK_VERSION=${SDK_VERSION}`);
void refreshReceiver();
window.setInterval(() => void refreshReceiver(), 5000);

// --- Open the form ---------------------------------------------------------
$('open-form')?.addEventListener('click', () => {
  openReportForm();
  record('openReportForm() called');
});

// --- Phase 1 error/request scenarios ---------------------------------------
$('throw-sync')?.addEventListener('click', () => {
  record('uncaught exception thrown (see console)');
  setTimeout(() => {
    throw new Error('m-Seismo test app: uncaught synchronous exception');
  }, 0);
});

$('unhandled-rejection')?.addEventListener('click', () => {
  record('unhandled promise rejection triggered (see console)');
  void Promise.reject(new Error('m-Seismo test app: unhandled promise rejection'));
});

async function requestEndpoint(url: string, testid: string): Promise<void> {
  $(testid)?.addEventListener('click', async () => {
    try {
      const response = await fetch(url);
      record(`HTTP ${response.status} received from ${url}`);
    } catch (error) {
      record(`request to ${url} failed: ${String(error)}`);
    }
  });
}

void requestEndpoint('/api/fail', 'fetch-500');
void requestEndpoint('/api/ok', 'fetch-200');

$('silent-save')?.addEventListener('click', () => {
  record('Save clicked — nothing happened (simulated logical bug)');
});

// --- Config / lifecycle controls -------------------------------------------
$('init-twice')?.addEventListener('click', () => initSdk('init() again'));
$('init-no-shortcut')?.addEventListener('click', () =>
  initSdk('init() without shortcut', { shortcut: false }),
);
$('destroy')?.addEventListener('click', () => {
  destroy();
  record('destroy() called');
  setStatusChip('status-sdk', 'destroyed', 'bad');
});

// --- Ctrl + Enter indicator ------------------------------------------------
window.addEventListener('keydown', (event) => {
  if (event.ctrlKey && event.key === 'Enter') {
    const indicator = $('ctrl-enter-indicator');
    if (indicator) indicator.textContent = `Ctrl+Enter pressed at ${new Date().toISOString()}`;
    record('Ctrl+Enter pressed');
  }
});

// --- Receiver mode controls ------------------------------------------------
function wireModeButton(testid: string, mode: string): void {
  $(testid)?.addEventListener('click', () => {
    void fetch(`${RECEIVER_BASE}/__mode`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode }),
    })
      .then((response) => {
        record(`receiver mode -> ${mode} (HTTP ${response.status})`);
        syncModeButtons(mode);
        void refreshReceiver();
      })
      .catch((error) => record(`receiver mode -> ${mode} failed: ${String(error)}`));
  });
}

wireModeButton('mode-ok', 'ok');
wireModeButton('mode-fail500', 'fail500');
wireModeButton('mode-slow', 'slow');
wireModeButton('mode-drop', 'offline');

$('receiver-reset')?.addEventListener('click', () => {
  void fetch(`${RECEIVER_BASE}/__reset`, { method: 'POST' })
    .then((response) => {
      record(`receiver reset (HTTP ${response.status})`);
      toast('Receiver cleared', 'ok');
      viewerReports = [];
      renderOutput();
      void refreshReceiver();
    })
    .catch((error) => record(`receiver reset failed: ${String(error)}`));
});

// --- Theme toggle ----------------------------------------------------------
function toggleTheme(): void {
  setTheme(currentTheme() === 'dark' ? 'light' : 'dark');
}
$('theme-toggle')?.addEventListener('click', toggleTheme);
$('theme-toggle-2')?.addEventListener('click', toggleTheme);

// --- Phase 5.1 context lab -------------------------------------------------
$('context-lab-navigate')?.addEventListener('click', () => {
  const url = `${location.pathname}?token=fake-secret-123&utm_source=docs&email=a@b.c#section`;
  history.replaceState(null, '', url);
  record(`context lab: URL set to ${location.href}`);
});

$('context-lab-reset-url')?.addEventListener('click', () => {
  history.replaceState(null, '', location.pathname);
  record(`context lab: URL reset to ${location.href}`);
});

$('context-lab-reinit')?.addEventListener('click', () =>
  initSdk('context lab: re-init with app metadata'),
);
$('context-lab-reinit-no-meta')?.addEventListener('click', () =>
  initSdkWithoutMeta('context lab: re-init without app metadata'),
);

$('show-context')?.addEventListener('click', async () => {
  try {
    const response = await fetch(`${RECEIVER_BASE}/__reports`);
    const body = (await response.json()) as {
      count: number;
      reports: Array<{ context?: unknown }>;
    };
    if (body.count === 0) {
      record('show last context: no reports stored yet');
      return;
    }
    const latest = body.reports[body.reports.length - 1];
    record(`show last context: ${JSON.stringify(latest?.context)}`);
  } catch (error) {
    record(`show last context failed: ${String(error)}`);
  }
});

// --- Send a test report via the SDK transport ------------------------------
$('send-report')?.addEventListener('click', async () => {
  const config = getConfig();
  if (!config) {
    record('send test report: SDK is not configured (init failed or destroyed)');
    return;
  }
  const report = buildReport({
    type: 'manual',
    trigger: 'form',
    description: 'Manual test report from the m-Seismo test app',
    context: getContext(contextInput()).context,
  });
  const result = await sendReport(sanitize(report), config);
  if (result.ok) {
    const id = result.id ? ` id=${result.id}` : '';
    record(`send test report: ok status=${result.status}${id}`);
    void refreshReceiver();
  } else {
    const status = result.status !== undefined ? ` status=${result.status}` : '';
    record(`send test report: failed kind=${result.kind}${status} (${result.message})`);
  }
});

// --- Phase 5.2 diagnostics lab ---------------------------------------------
const diagnosticsView = $('diagnostics-buffer');

function renderDiagnostics(): void {
  if (!diagnosticsView) return;
  diagnosticsView.textContent = JSON.stringify(getDiagnosticsSnapshot(), null, 2);
}

function onClick(testid: string, handler: () => void): void {
  $(testid)?.addEventListener('click', handler);
}

onClick('diag-uncaught-typeerror', () => {
  record('diagnostics: throwing uncaught TypeError');
  setTimeout(() => {
    throw new TypeError('m-Seismo test app: diagnostics TypeError');
  }, 0);
});

onClick('diag-uncaught-undefined-fn', () => {
  record('diagnostics: calling undefined function');
  setTimeout(() => {
    const fn = undefined as unknown as () => void;
    fn();
  }, 0);
});

onClick('diag-throw-string', () => {
  record('diagnostics: throwing a string');
  setTimeout(() => {
    throw 'm-Seismo test app: thrown string';
  }, 0);
});

onClick('diag-throw-object', () => {
  record('diagnostics: throwing an object');
  setTimeout(() => {
    throw { message: 'thrown object', code: 42 };
  }, 0);
});

onClick('diag-reject-error', () => {
  record('diagnostics: rejecting with Error');
  void Promise.reject(new Error('m-Seismo test app: diagnostics rejection'));
});

onClick('diag-reject-string', () => {
  record('diagnostics: rejecting with string');
  void Promise.reject('m-Seismo test app: rejected string');
});

onClick('diag-reject-object', () => {
  record('diagnostics: rejecting with object');
  void Promise.reject({ message: 'rejected object', code: 7 });
});

onClick('diag-reject-undefined', () => {
  record('diagnostics: rejecting with undefined');
  void Promise.reject(undefined);
});

onClick('diag-capture-exception', () => {
  captureException(new Error('m-Seismo test app: captured exception'), {
    feature: 'checkout',
    userId: 'user-42',
    nested: { step: 3, retries: 1 },
  });
  record('diagnostics: captureException(Error, context) called');
});

onClick('diag-capture-exception-no-context', () => {
  captureException(new Error('captured without context'));
  record('diagnostics: captureException(Error) called');
});

onClick('diag-capture-non-error', () => {
  captureException('m-Seismo test app: captured string');
  record('diagnostics: captureException(string) called');
});

onClick('diag-broken-image', () => {
  const img = document.createElement('img');
  img.src = `/definitely-missing-${Date.now()}.png`;
  document.body.append(img);
  record('diagnostics: broken image requested (resource error)');
});

onClick('diag-broken-script', () => {
  const script = document.createElement('script');
  script.src = `/definitely-missing-${Date.now()}.js`;
  document.body.append(script);
  record('diagnostics: broken script requested (resource error)');
});

onClick('diag-cross-origin', () => {
  const script = document.createElement('script');
  script.src = `${RECEIVER_BASE}/cross-origin-script.js`;
  document.body.append(script);
  record('diagnostics: cross-origin script requested');
});

onClick('diag-flood', () => {
  record('diagnostics: flooding 100 uncaught errors');
  for (let i = 0; i < 100; i += 1) {
    setTimeout(() => {
      throw new Error(`flood ${i}`);
    }, 0);
  }
});

onClick('diag-show-buffer', () => {
  renderDiagnostics();
  record(`diagnostics: buffer count = ${getDiagnosticsCount()}`);
});

onClick('diag-clear-buffer', () => {
  clearDiagnostics();
  renderDiagnostics();
  record('diagnostics: buffer cleared');
});

onClick('diag-reinit-small', () => {
  initSdk('diagnostics: re-init maxEvents=5', { diagnostics: { maxEvents: 5 } });
});

onClick('diag-reinit-disabled', () => {
  initSdk('diagnostics: re-init capture disabled', {
    diagnostics: { captureUncaughtErrors: false, captureUnhandledRejections: false },
  });
});

// --- Phase 5.3 breadcrumbs & network lab -----------------------------------
const selectionView = $('selection-buffer');

function renderSelection(): void {
  if (!selectionView) return;
  selectionView.textContent = JSON.stringify(getDiagnosticsSelection(), null, 2);
}

onClick('bc-nav-push', () => {
  const url = `${location.pathname}?access_token=fake-secret-nav&page=2`;
  history.pushState(null, '', url);
  record(`breadcrumbs: pushState to ${location.href}`);
});

onClick('bc-nav-back', () => {
  history.back();
  record('breadcrumbs: history.back() called');
});

onClick('bc-click', () => {
  record('breadcrumbs: clicked the lab button');
});

onClick('bc-network-success', () => {
  void fetch(`${RECEIVER_BASE}/api/echo`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ hello: 'world' }),
  })
    .then((response) => record(`breadcrumbs: /api/echo -> HTTP ${response.status} (not recorded)`))
    .catch((error) => record(`breadcrumbs: /api/echo failed: ${String(error)}`));
});

onClick('bc-network-http-error', () => {
  void fetch(`${RECEIVER_BASE}/api/status/500`)
    .then((response) => record(`breadcrumbs: /api/status/500 -> HTTP ${response.status}`))
    .catch((error) => record(`breadcrumbs: /api/status/500 failed: ${String(error)}`));
});

onClick('bc-network-network-error', () => {
  void fetch(`${RECEIVER_BASE}/api/drop`)
    .then((response) => record(`breadcrumbs: /api/drop -> HTTP ${response.status}`))
    .catch((error) => record(`breadcrumbs: /api/drop failed (expected): ${String(error)}`));
});

onClick('bc-network-opaque', () => {
  void fetch(`${RECEIVER_BASE}/api/no-cors`, { mode: 'no-cors' })
    .then((response) => record(`breadcrumbs: /api/no-cors -> opaque status=${response.status}`))
    .catch((error) => record(`breadcrumbs: /api/no-cors failed: ${String(error)}`));
});

onClick('bc-network-slow', () => {
  const started = Date.now();
  void fetch(`${RECEIVER_BASE}/api/slow?ms=1500`)
    .then((response) =>
      record(`breadcrumbs: /api/slow -> HTTP ${response.status} in ${Date.now() - started}ms (not recorded)`),
    )
    .catch((error) => record(`breadcrumbs: /api/slow failed: ${String(error)}`));
});

onClick('bc-show-selection', () => {
  renderSelection();
  const selection = getDiagnosticsSelection();
  record(
    `breadcrumbs: selection = ${selection.breadcrumbs.length} breadcrumbs, ` +
      `${selection.networkFailures.length} network failures, error=${selection.error ? 'yes' : 'no'}`,
  );
});

// --- Phase 7 report processor lab ------------------------------------------
const statsView = $('processor-stats');
const lastView = $('processor-last');

function reportSameError(): void {
  captureException(new Error('m-Seismo test app: repeated error'));
}

function reportSameErrorViaSecondPath(): void {
  captureException(new Error('m-Seismo test app: repeated error'));
}

onClick('rp-open-form', () => {
  openReportForm();
  record('report lab: opened manual form');
});

onClick('rp-seed-error-then-form', () => {
  captureException(new Error('m-Seismo test app: recent error before manual report'));
  openReportForm();
  record('report lab: seeded a recent error, opened form');
});

onClick('rp-uncaught', () => {
  record('report lab: throwing uncaught error');
  setTimeout(() => {
    throw new Error('m-Seismo test app: automatic uncaught error');
  }, 0);
});

onClick('rp-capture-exception', () => {
  captureException(new Error('m-Seismo test app: captured exception (lab)'), {
    feature: 'checkout',
    token: 'super-secret-token-value-1234567890',
  });
  record('report lab: captureException() with context');
});

onClick('rp-capture-message', () => {
  captureMessage('m-Seismo test app: captured message (lab)', { area: 'lab' });
  record('report lab: captureMessage()');
});

for (const count of [1, 10, 100, 500]) {
  onClick(`rp-repeat-${count}`, () => {
    for (let i = 0; i < count; i += 1) reportSameError();
    record(`report lab: repeated same error ×${count}`);
  });
}

onClick('rp-two-paths', () => {
  reportSameError();
  reportSameErrorViaSecondPath();
  record('report lab: same message from two code paths');
});

onClick('rp-different-messages', () => {
  for (let i = 0; i < 5; i += 1) captureMessage(`m-Seismo test app: message ${i}`);
  record('report lab: 5 different messages');
});

onClick('rp-weak-no-stack', () => {
  captureException({ name: 'Error', message: 'm-Seismo test app: error without a stack' });
  record('report lab: captureException() with no stack');
});

onClick('rp-cross-origin', () => {
  const script = document.createElement('script');
  script.src = `${RECEIVER_BASE}/cross-origin-script.js?lab=${Date.now()}`;
  document.body.append(script);
  record('report lab: cross-origin script (weak fingerprint)');
});

onClick('rp-reinit-auto-errors', () => {
  initSdk('report lab: autoReport=errors', { reporting: { autoReport: 'errors' } });
});

onClick('rp-reinit-auto-off', () => {
  initSdk('report lab: autoReport=off', { reporting: { autoReport: 'off' } });
});

onClick('rp-reinit-small-limits', () => {
  initSdk('report lab: tiny window + caps', {
    reporting: {
      autoReport: 'errors',
      dedupWindowMs: 5_000,
      maxAutoReportsPerMinute: 2,
      maxAutoReportsPerSession: 3,
    },
  });
});

onClick('rp-reinit-no-capture', () => {
  initSdk('report lab: sendCapturedExceptions=false', {
    reporting: { autoReport: 'errors', sendCapturedExceptions: false },
  });
});

onClick('rp-reinit-beforesend-drop', () => {
  initSdk('report lab: beforeSend drops automatic', {
    reporting: {
      autoReport: 'errors',
      beforeSend: (report) => (report.trigger === 'automatic' ? null : report),
    },
  });
});

onClick('rp-reinit-beforesend-throw', () => {
  initSdk('report lab: beforeSend throws', {
    reporting: {
      autoReport: 'errors',
      beforeSend: () => {
        throw new Error('beforeSend boom');
      },
    },
  });
});

onClick('rp-show-stats', async () => {
  const local = getReportProcessorStats();
  if (statsView) statsView.textContent = JSON.stringify(local, null, 2);
  try {
    const response = await fetch(`${RECEIVER_BASE}/__stats`);
    const remote = (await response.json()) as unknown;
    record(`report lab: local stats = ${JSON.stringify(local)}`);
    record(`report lab: receiver stats = ${JSON.stringify(remote)}`);
  } catch (error) {
    record(`report lab: receiver stats failed: ${String(error)}`);
  }
});

onClick('rp-show-last', async () => {
  try {
    const response = await fetch(`${RECEIVER_BASE}/__reports?full=1`);
    const body = (await response.json()) as {
      count: number;
      reports: Array<{ type?: string; trigger?: string; error?: unknown }>;
    };
    if (body.count === 0) {
      record('report lab: no reports stored yet');
      return;
    }
    const latest = body.reports[body.reports.length - 1];
    if (lastView) lastView.textContent = JSON.stringify(latest, null, 2);
    record(
      `report lab: last report type=${latest?.type} trigger=${latest?.trigger} ` +
        `error=${latest?.error ? 'yes' : 'null'}`,
    );
  } catch (error) {
    record(`report lab: show last failed: ${String(error)}`);
  }
});

onClick('rp-stats-link', () => {
  window.open(`${RECEIVER_BASE}/__stats`, '_blank');
  record('report lab: opened /__stats in a new tab');
});

// --- Quick start / golden path ---------------------------------------------
$('quickstart-reset')?.addEventListener('click', () => {
  resetChecklist();
  record('quickstart: checklist reset');
});

$('golden-path')?.addEventListener('click', async () => {
  record('golden path: starting');
  const healthy = await receiverHealth();
  if (healthy) markStep('receiver');
  const config = getConfig();
  if (!config) {
    toast('SDK not initialized', 'error');
    return;
  }
  captureException(new Error('m-Seismo test app: golden path demo error'), {
    feature: 'golden-path',
  });
  markStep('bug');
  record('golden path: seeded an error, opening the form');
  openReportForm();
  toast('Form opened — describe the bug and send', 'ok');
});

// --- Event log controls ----------------------------------------------------
$('event-log-toggle')?.addEventListener('click', () => {
  const panel = $('event-log-panel');
  const toggle = $('event-log-toggle');
  if (!panel || !toggle) return;
  const collapsed = panel.dataset['collapsed'] === 'true';
  panel.dataset['collapsed'] = collapsed ? 'false' : 'true';
  toggle.setAttribute('aria-expanded', collapsed ? 'true' : 'false');
  toggle.textContent = collapsed ? 'Collapse' : 'Expand';
});

$<HTMLSelectElement>('event-log-level-filter')?.addEventListener('change', (event) => {
  logFilter = { ...logFilter, level: (event.target as HTMLSelectElement).value as LogFilter['level'] };
  renderLog();
});

$<HTMLSelectElement>('event-log-source-filter')?.addEventListener('change', (event) => {
  logFilter = { ...logFilter, source: (event.target as HTMLSelectElement).value };
  renderLog();
});

$<HTMLInputElement>('event-log-search')?.addEventListener('input', (event) => {
  logFilter = { ...logFilter, search: (event.target as HTMLInputElement).value };
  renderLog();
});

$('event-log-pause')?.addEventListener('click', () => {
  logPaused = !logPaused;
  const button = $('event-log-pause');
  if (button) {
    button.setAttribute('aria-pressed', logPaused ? 'true' : 'false');
    button.textContent = logPaused ? 'Resume auto-scroll' : 'Pause auto-scroll';
  }
});

$('event-log-copy')?.addEventListener('click', () => {
  const text = logEntries.map((entry) => `[${entry.time}] ${entry.level} ${entry.source} ${entry.message}`).join('\n');
  void copyText(text).then(() => toast('Log copied', 'ok'));
});

$('event-log-clear')?.addEventListener('click', () => {
  logEntries = [];
  renderLog();
  record('event log cleared');
});

// --- Output viewer controls ------------------------------------------------
$('viewer-load-latest')?.addEventListener('click', () => {
  viewerCompare = false;
  void loadLatestReport();
  record('viewer: loading latest reports');
});

$('viewer-refresh')?.addEventListener('click', () => {
  void loadLatestReport();
  record('viewer: refreshed');
});

// --- Initial render --------------------------------------------------------
renderLog();
refreshSourceOptions();
renderOutput();
