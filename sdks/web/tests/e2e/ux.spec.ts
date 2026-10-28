/**
 * UX / presentation e2e.
 *
 * These tests pin the behaviour that the UI-polish phase added, without
 * asserting any pixel values (no committed visual baselines). They cover:
 *   - narrow-viewport layout (no horizontal scroll at 375px),
 *   - the event-log tools (filter, pause, clear),
 *   - the quick-start checklist + golden path,
 *   - the receiver panel (mode switcher, reset, reachability),
 *   - the Output report viewer (load, tabs, redaction check),
 *   - the viewer's XSS safety against hostile stored content,
 *   - the SDK form's character counter.
 *
 * Every test resets the mock receiver first so state does not leak between
 * tests (the suite runs serialized for the same reason).
 */
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const RECEIVER = 'http://localhost:4010';
const ENDPOINT = `${RECEIVER}/api/reports`;

async function resetReceiver(page: Page): Promise<void> {
  await page.request.post(`${RECEIVER}/__reset`);
}

test.beforeEach(async ({ page }) => {
  await resetReceiver(page);
});

test.describe('layout', () => {
  test('the test app has no horizontal scroll at 375px', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 800 });
    await page.goto('/');
    const overflow = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    }));
    expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth);
  });

  test('the error laboratory has no horizontal scroll at 375px', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 800 });
    await page.goto('/lab.html');
    const overflow = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    }));
    expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth);
  });
});

test.describe('event log', () => {
  test('records SDK events and can be cleared and filtered', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('open-form').click();
    await expect(page.getByTestId('event-log')).toContainText('openReportForm() called');
    await expect(page.getByTestId('event-log')).toContainText('sdk event: form:open');
    // Close the modal so its full-screen backdrop no longer covers the log tools.
    await page.getByTestId('mseismo-cancel').click();

    await page.getByTestId('event-log-level-filter').selectOption('error');
    // An info message must disappear once we filter to errors only.
    await expect(page.getByTestId('event-log')).not.toContainText('openReportForm() called');

    await page.getByTestId('event-log-level-filter').selectOption('all');
    await page.getByTestId('event-log-clear').click();
    await expect(page.getByTestId('event-log')).toContainText('event log cleared');
  });

  test('search narrows the log', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('throw-sync').click();
    await page.getByTestId('silent-save').click();
    await page.getByTestId('event-log-search').fill('nothing happened');
    await expect(page.getByTestId('event-log')).toContainText('nothing happened');
    await expect(page.getByTestId('event-log')).not.toContainText('uncaught exception thrown');
  });

  test('the log stays bounded at 500 entries', async ({ page }) => {
    await page.goto('/');
    // Drive the clicks in-page so 510 iterations do not cost 510 round-trips.
    await page.evaluate(() => {
      const button = document.querySelector<HTMLButtonElement>('[data-testid="silent-save"]');
      for (let i = 0; i < 510; i += 1) button?.click();
    });
    const count = await page.getByTestId('event-log').locator('li').count();
    expect(count).toBeLessThanOrEqual(500);
    await expect(page.getByTestId('event-log-count')).toHaveText('500');
  });
});

test.describe('quick start', () => {
  test('the golden path ticks the checklist and opens the form', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('golden-path').click();
    await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
    await expect(page.locator('[data-testid="quickstart-steps"] li[data-step="receiver"]')).toHaveAttribute(
      'data-done',
      'true',
    );
    await expect(page.locator('[data-testid="quickstart-steps"] li[data-step="bug"]')).toHaveAttribute(
      'data-done',
      'true',
    );
  });
});

test.describe('receiver panel', () => {
  test('shows the current mode and can switch it', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByTestId('status-mode')).toContainText('mode: ok');

    await page.getByTestId('mode-fail500').click();
    await expect(page.getByTestId('status-mode')).toContainText('mode: fail500');
    await expect(page.getByTestId('mode-fail500')).toHaveAttribute('aria-pressed', 'true');

    await page.getByTestId('mode-ok').click();
    await expect(page.getByTestId('status-mode')).toContainText('mode: ok');
  });

  test('reset clears the stored count', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('send-report').click();
    await expect(page.getByTestId('receiver-count')).toContainText('1 stored', { timeout: 10_000 });
    await page.getByTestId('receiver-reset').click();
    await expect(page.getByTestId('receiver-count')).toContainText('0 stored');
  });
});

test.describe('output viewer', () => {
  test('loads a stored report and shows all panels', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('send-report').click();
    await expect(page.getByTestId('receiver-count')).toContainText('1 stored', { timeout: 10_000 });

    await page.getByTestId('viewer-load-latest').click();
    await expect(page.getByTestId('viewer-card')).toHaveCount(1);
    await expect(page.getByTestId('viewer-tab-summary')).toBeVisible();
    await expect(page.getByTestId('viewer-tab-redaction')).toBeVisible();

    await page.getByTestId('viewer-tab-redaction').click();
    await expect(page.getByTestId('viewer-card')).toContainText('Redaction check');
  });
});

test.describe('viewer XSS safety', () => {
  test('hostile stored content renders as inert text', async ({ page }) => {
    const hostile = {
      schemaVersion: 2,
      sdkVersion: '0.0.0',
      reportId: 'xss-report-id',
      type: 'manual',
      trigger: 'form',
      description: '<img src=x onerror="window.__xss=1"><script>window.__xss=1</script>',
      error: null,
      context: { timestamp: new Date().toISOString(), application: {} },
      breadcrumbs: [
        {
          timestamp: new Date().toISOString(),
          category: 'interaction',
          message: '<b>bold</b><script>window.__xss=1</script>',
        },
      ],
      networkFailures: [],
      screenshot: null,
    };
    const response = await page.request.post(ENDPOINT, { data: hostile });
    expect(response.ok()).toBe(true);

    await page.goto('/');
    await page.getByTestId('viewer-load-latest').click();
    await expect(page.getByTestId('viewer-card')).toHaveCount(1);

    // Open every tab; hostile content must never become live DOM.
    for (const tab of ['breadcrumbs', 'raw', 'redaction', 'network']) {
      await page.getByTestId(`viewer-tab-${tab}`).click();
    }
    const result = await page.evaluate(() => ({
      xss: (window as unknown as { __xss?: number }).__xss ?? null,
      scripts: document.querySelectorAll('#viewer-root script').length,
      imgs: document.querySelectorAll('#viewer-root img').length,
    }));
    expect(result.xss).toBeNull();
    expect(result.scripts).toBe(0);
    expect(result.imgs).toBe(0);
  });
});

test.describe('SDK form polish', () => {
  test('shows a live character counter', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('open-form').click();
    await expect(page.getByTestId('mseismo-counter')).toHaveText('0 / 5000');
    await page.getByTestId('mseismo-description').fill('hello');
    await expect(page.getByTestId('mseismo-counter')).toHaveText('5 / 5000');
  });

  test('the description field is at least 16px to avoid mobile zoom', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('open-form').click();
    const fontSize = await page
      .getByTestId('mseismo-description')
      .evaluate((el) => getComputedStyle(el).fontSize);
    expect(parseFloat(fontSize)).toBeGreaterThanOrEqual(16);
  });
});
