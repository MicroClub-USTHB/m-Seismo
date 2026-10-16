import {
  buildReport,
  getConfig,
  init,
  openReportForm,
  sanitize,
  SDK_VERSION,
  sendReport,
} from '@m-seismo/web';
import './styles.css';

/**
 * Phase 1 test harness. Every button triggers a scenario and records what
 * happened locally. No SDK collector, form, or network instrumentation is wired
 * up yet — that is deliberate.
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
// Default to the system preference; the toggle flips between light and dark.
function applyInitialTheme(): void {
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  document.documentElement.dataset['theme'] = prefersDark ? 'dark' : 'light';
  record(`initial theme = ${prefersDark ? 'dark' : 'light'} (system preference)`);
}

applyInitialTheme();

// --- SDK init --------------------------------------------------------------
// Endpoint is configurable via Vite env; defaults to the local mock receiver so
// the browser makes a REAL cross-origin request during development.
// `import.meta.env` values are typed `any` by Vite; cast to the expected type.
const ENDPOINT =
  (import.meta.env.VITE_MSEISMO_ENDPOINT as string | undefined) ??
  'http://localhost:4010/api/reports';

const enabled = init({
  endpoint: ENDPOINT,
  appVersion: '0.0.0',
  environment: 'test-app',
});
const endpointLabel = document.querySelector('[data-testid="receiver-endpoint"]');
if (endpointLabel) endpointLabel.textContent = ENDPOINT;
record(
  `SDK imported, SDK_VERSION=${SDK_VERSION}, init ${enabled ? 'enabled' : 'disabled'}`,
);

// --- Scenario 1: open the bug-reporting form (stub) -----------------------
document
  .querySelector('[data-testid="open-form"]')
  ?.addEventListener('click', () => {
    openReportForm();
    record('openReportForm() stub called');
  });

// --- Scenario 2: Ctrl + Enter shortcut ------------------------------------
window.addEventListener('keydown', (event) => {
  if (event.ctrlKey && event.key === 'Enter') {
    if (indicator) indicator.textContent = `Ctrl+Enter pressed at ${new Date().toISOString()}`;
    record('Ctrl+Enter pressed');
  }
});

// --- Scenario 3: uncaught synchronous exception ---------------------------
document
  .querySelector('[data-testid="throw-sync"]')
  ?.addEventListener('click', () => {
    record('uncaught exception thrown (see console)');
    // Thrown from a timeout so it is truly uncaught and not swallowed by the
    // click handler / Playwright's event dispatch.
    setTimeout(() => {
      throw new Error('m-Seismo test app: uncaught synchronous exception');
    }, 0);
  });

// --- Scenario 4: unhandled promise rejection ------------------------------
document
  .querySelector('[data-testid="unhandled-rejection"]')
  ?.addEventListener('click', () => {
    record('unhandled promise rejection triggered (see console)');
    void Promise.reject(
      new Error('m-Seismo test app: unhandled promise rejection'),
    );
  });

// --- Scenarios 5 & 6: HTTP 500 / 200 --------------------------------------
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

// --- Scenario 7: silent logical bug ---------------------------------------
document
  .querySelector('[data-testid="silent-save"]')
  ?.addEventListener('click', () => {
    record('Save clicked — nothing happened (simulated logical bug)');
  });

// --- Theme toggle ----------------------------------------------------------
document
  .querySelector('[data-testid="theme-toggle"]')
  ?.addEventListener('click', () => {
    const root = document.documentElement;
    const next = root.dataset['theme'] === 'dark' ? 'light' : 'dark';
    root.dataset['theme'] = next;
    record(`theme toggled to ${next}`);
  });

// --- Send a test report via the SDK transport ------------------------------
document
  .querySelector('[data-testid="send-report"]')
  ?.addEventListener('click', async () => {
    const config = getConfig();
    if (!config) {
      record('send test report: SDK is not configured (init failed)');
      return;
    }
    // Build a minimal manual report (error: null) then send it through the
    // privacy boundary + transport, exactly like the real flow will.
    const report = buildReport({
      description: 'Manual test report from the m-Seismo test app',
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
