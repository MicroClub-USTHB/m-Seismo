import { init, openReportForm, SDK_VERSION } from '@m-seismo/web';
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

// --- SDK init (stub) -------------------------------------------------------
init({});
record(`SDK imported, SDK_VERSION=${SDK_VERSION}`);

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
