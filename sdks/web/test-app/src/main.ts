import {
  buildReport,
  captureException,
  destroy,
  getConfig,
  getContext,
  init,
  openReportForm,
  sanitize,
  SDK_VERSION,
  sendReport,
} from '@m-seismo/web';
import type { ContextInput, InitOptions, SdkEvent } from '@m-seismo/web';
import { clearDiagnostics, getDiagnosticsCount, getDiagnosticsSelection, getDiagnosticsSnapshot } from '@sdk/internal';
import './styles.css';

/**
 * Phase 3 test harness. The SDK is initialised with a launcher + shortcut;
 * buttons drive config/lifecycle/receiver scenarios and every SDK lifecycle
 * event is written to the event log via `onEvent`.
 */

const log = document.querySelector<HTMLOListElement>('[data-testid="event-log"]');
const indicator = document.querySelector<HTMLElement>(
  '[data-testid="ctrl-enter-indicator"]',
);

function record(message: string): void {
  if (!log) return;
  const li = document.createElement('li');
  const time = new Date().toISOString().slice(11, 23);
  li.textContent = `[${time}] ${message}`;
  log.append(li);
  log.scrollTop = log.scrollHeight;
}

// --- Theme -----------------------------------------------------------------
function applyInitialTheme(): void {
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  document.documentElement.dataset['theme'] = prefersDark ? 'dark' : 'light';
  record(`initial theme = ${prefersDark ? 'dark' : 'light'} (system preference)`);
}

applyInitialTheme();

// --- SDK init --------------------------------------------------------------
// Endpoint is configurable via Vite env; defaults to the local mock receiver so
// the browser makes a REAL cross-origin request during development.
const ENDPOINT =
  (import.meta.env.VITE_MSEISMO_ENDPOINT as string | undefined) ??
  'http://localhost:4010/api/reports';
const RECEIVER_BASE = new URL(ENDPOINT).origin;

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
      return `sdk event: validation:error`;
  }
}

const onEvent = (event: SdkEvent): void => record(describeEvent(event));

function initSdk(
  label: string,
  extra: Partial<Pick<InitOptions, 'shortcut' | 'diagnostics'>> = {},
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
}

/** Re-init with no appVersion/environment, to test context without app metadata. */
function initSdkWithoutMeta(label: string): void {
  const enabled = init({
    endpoint: ENDPOINT,
    launcher: true,
    screenshot: true,
    onEvent,
  });
  record(`${label}: init ${enabled ? 'enabled' : 'disabled'} (no app metadata)`);
}

/** Builds a fresh context input from the current SDK config (Phase 5.1). */
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

initSdk('initial');
const endpointLabel = document.querySelector('[data-testid="receiver-endpoint"]');
if (endpointLabel) endpointLabel.textContent = ENDPOINT;
record(`SDK imported, SDK_VERSION=${SDK_VERSION}`);

// --- Open the form ---------------------------------------------------------
document
  .querySelector('[data-testid="open-form"]')
  ?.addEventListener('click', () => {
    openReportForm();
    record('openReportForm() called');
  });

// --- Phase 1 error/request scenarios (kept for regression coverage) --------
document
  .querySelector('[data-testid="throw-sync"]')
  ?.addEventListener('click', () => {
    record('uncaught exception thrown (see console)');
    setTimeout(() => {
      throw new Error('m-Seismo test app: uncaught synchronous exception');
    }, 0);
  });

document
  .querySelector('[data-testid="unhandled-rejection"]')
  ?.addEventListener('click', () => {
    record('unhandled promise rejection triggered (see console)');
    void Promise.reject(new Error('m-Seismo test app: unhandled promise rejection'));
  });

async function requestEndpoint(url: string, testid: string): Promise<void> {
  document.querySelector(`[data-testid="${testid}"]`)?.addEventListener(
    'click',
    async () => {
      try {
        const response = await fetch(url);
        record(`HTTP ${response.status} received from ${url}`);
      } catch (error) {
        record(`request to ${url} failed: ${String(error)}`);
      }
    },
  );
}

void requestEndpoint('/api/fail', 'fetch-500');
void requestEndpoint('/api/ok', 'fetch-200');

document
  .querySelector('[data-testid="silent-save"]')
  ?.addEventListener('click', () => {
    record('Save clicked — nothing happened (simulated logical bug)');
  });

// --- Config / lifecycle controls ------------------------------------------
document
  .querySelector('[data-testid="init-twice"]')
  ?.addEventListener('click', () => initSdk('init() again'));

document
  .querySelector('[data-testid="init-no-shortcut"]')
  ?.addEventListener('click', () => initSdk('init() without shortcut', { shortcut: false }));

document
  .querySelector('[data-testid="destroy"]')
  ?.addEventListener('click', () => {
    destroy();
    record('destroy() called');
  });

// --- Ctrl + Enter indicator (test-app's own handler) ----------------------
window.addEventListener('keydown', (event) => {
  if (event.ctrlKey && event.key === 'Enter') {
    if (indicator) indicator.textContent = `Ctrl+Enter pressed at ${new Date().toISOString()}`;
    record('Ctrl+Enter pressed');
  }
});

// --- Receiver mode controls ------------------------------------------------
function wireModeButton(testid: string, mode: string): void {
  document.querySelector(`[data-testid="${testid}"]`)?.addEventListener('click', () => {
    void fetch(`${RECEIVER_BASE}/__mode`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode }),
    })
      .then((response) => record(`receiver mode -> ${mode} (HTTP ${response.status})`))
      .catch((error) => record(`receiver mode -> ${mode} failed: ${String(error)}`));
  });
}

wireModeButton('mode-ok', 'ok');
wireModeButton('mode-fail500', 'fail500');
wireModeButton('mode-slow', 'slow');
wireModeButton('mode-drop', 'offline');

// --- Theme toggle ----------------------------------------------------------
document
  .querySelector('[data-testid="theme-toggle"]')
  ?.addEventListener('click', () => {
    const root = document.documentElement;
    const next = root.dataset['theme'] === 'dark' ? 'light' : 'dark';
    root.dataset['theme'] = next;
    record(`theme toggled to ${next}`);
  });

// --- Phase 5.1 context lab -------------------------------------------------
document
  .querySelector('[data-testid="context-lab-navigate"]')
  ?.addEventListener('click', () => {
    const url = `${location.pathname}?token=fake-secret-123&utm_source=docs&email=a@b.c#section`;
    history.replaceState(null, '', url);
    record(`context lab: URL set to ${location.href}`);
  });

document
  .querySelector('[data-testid="context-lab-reset-url"]')
  ?.addEventListener('click', () => {
    history.replaceState(null, '', location.pathname);
    record(`context lab: URL reset to ${location.href}`);
  });

document
  .querySelector('[data-testid="context-lab-reinit"]')
  ?.addEventListener('click', () => initSdk('context lab: re-init with app metadata'));

document
  .querySelector('[data-testid="context-lab-reinit-no-meta"]')
  ?.addEventListener('click', () =>
    initSdkWithoutMeta('context lab: re-init without app metadata'),
  );

document
  .querySelector('[data-testid="show-context"]')
  ?.addEventListener('click', async () => {
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
document
  .querySelector('[data-testid="send-report"]')
  ?.addEventListener('click', async () => {
    const config = getConfig();
    if (!config) {
      record('send test report: SDK is not configured (init failed or destroyed)');
      return;
    }
    const report = buildReport({
      description: 'Manual test report from the m-Seismo test app',
      context: getContext(contextInput()).context,
    });
    const result = await sendReport(sanitize(report), config);
    if (result.ok) {
      const id = result.id ? ` id=${result.id}` : '';
      record(`send test report: ok status=${result.status}${id}`);
    } else {
      const status = result.status !== undefined ? ` status=${result.status}` : '';
      record(`send test report: failed kind=${result.kind}${status} (${result.message})`);
    }
  });

// --- Phase 5.2 diagnostics lab ---------------------------------------------
const diagnosticsView = document.querySelector<HTMLElement>(
  '[data-testid="diagnostics-buffer"]',
);

function renderDiagnostics(): void {
  if (!diagnosticsView) return;
  diagnosticsView.textContent = JSON.stringify(getDiagnosticsSnapshot(), null, 2);
}

function onClick(testid: string, handler: () => void): void {
  document.querySelector(`[data-testid="${testid}"]`)?.addEventListener('click', handler);
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
const selectionView = document.querySelector<HTMLElement>(
  '[data-testid="selection-buffer"]',
);

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
