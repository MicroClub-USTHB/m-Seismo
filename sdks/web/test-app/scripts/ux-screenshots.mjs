/**
 * UX screenshot capture (dev-only helper).
 *
 * Captures the before/after screenshot sets described in the UI/UX phase:
 * test app home, error laboratory, every form state, the launcher, and (with
 * --include-viewer) the report viewer cards and the receiver viewer.
 *
 * The dev server and mock receiver must already be running. Run with:
 *   node test-app/scripts/ux-screenshots.mjs --out report/screenshots/before
 *   node test-app/scripts/ux-screenshots.mjs --out report/screenshots/after --include-viewer
 *
 * Chromium only: the phase only asks for Chromium screenshots. All output is
 * written under the (git-ignored) report/ folder.
 */
import { chromium } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import path from 'node:path';

const args = process.argv.slice(2);
const outIndex = args.indexOf('--out');
const outDir = path.resolve(
  process.cwd(),
  outIndex >= 0 ? args[outIndex + 1] : 'report/screenshots/after',
);
const includeViewer = args.includes('--include-viewer');

const BASE = process.env.BASE_URL ?? 'http://localhost:5173';
const RECEIVER = process.env.RECEIVER_URL ?? 'http://localhost:4010';

mkdirSync(outDir, { recursive: true });

const VIEWPORTS = {
  desktop: { width: 1280, height: 800 },
  mobile: { width: 375, height: 667 },
};
const THEMES = ['light', 'dark'];

const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
  'base64',
);

async function setMode(mode, delayMs) {
  const body = delayMs === undefined ? { mode } : { mode, delayMs };
  await fetch(`${RECEIVER}/__mode`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

async function resetReceiver() {
  await fetch(`${RECEIVER}/__reset`, { method: 'POST' });
}

async function shot(page, name, fullPage = false) {
  const file = path.join(outDir, `${name}.png`);
  await page.screenshot({ path: file, fullPage });
  process.stdout.write(`shot ${path.relative(process.cwd(), file)}\n`);
}

async function openForm(page) {
  await closeForm(page);
  await page.getByTestId('open-form').click();
  await page.getByTestId('mseismo-dialog').waitFor({ state: 'visible' });
}

async function closeForm(page) {
  const dialog = page.getByTestId('mseismo-dialog');
  if (!(await dialog.isVisible().catch(() => false))) return;
  await page.keyboard.press('Escape');
  await dialog.waitFor({ state: 'hidden', timeout: 1000 }).catch(() => {});
  if (!(await dialog.isVisible().catch(() => false))) return;
  const cancel = page.getByTestId('mseismo-cancel');
  if (await cancel.isEnabled().catch(() => false)) {
    await cancel.click().catch(() => {});
    await dialog.waitFor({ state: 'hidden', timeout: 1000 }).catch(() => {});
  }
  if (await dialog.isVisible().catch(() => false)) await page.goto(BASE);
}

async function captureFormStates(page, label) {
  await page.goto(BASE);
  await setMode('ok');
  await openForm(page);
  await shot(page, `form-idle-${label}`);
  await closeForm(page);

  await openForm(page);
  await page.getByTestId('mseismo-send').click();
  await page.getByTestId('mseismo-message').waitFor({ state: 'visible' });
  await shot(page, `form-validation-${label}`);
  await closeForm(page);

  await openForm(page);
  await page
    .getByTestId('mseismo-screenshot')
    .setInputFiles({ name: 'example.png', mimeType: 'image/png', buffer: PNG });
  await page.getByTestId('mseismo-file-info').waitFor({ state: 'visible' });
  await shot(page, `form-screenshot-${label}`);
  await closeForm(page);

  await setMode('slow', 6000);
  await openForm(page);
  await page.getByTestId('mseismo-description').fill('The export button did nothing.');
  await page.getByTestId('mseismo-send').click();
  await page.waitForTimeout(250);
  await shot(page, `form-sending-${label}`);
  // The in-flight slow request keeps the form busy (Escape is ignored), so
  // reset the page instead of forcing the host out of the DOM.
  await page.goto(BASE);

  await setMode('fail500');
  await openForm(page);
  await page.getByTestId('mseismo-description').fill('The export button did nothing.');
  await page.getByTestId('mseismo-send').click();
  await page.getByTestId('mseismo-message').filter({ hasText: 'HTTP 500' }).waitFor();
  await shot(page, `form-failure-${label}`);
  await closeForm(page);

  await setMode('ok');
  await openForm(page);
  await page.getByTestId('mseismo-description').fill('The export button did nothing.');
  await page.getByTestId('mseismo-send').click();
  await page
    .getByTestId('mseismo-message')
    .filter({ hasText: /Thanks|sent/i })
    .waitFor();
  await shot(page, `form-success-${label}`);
  await page.getByTestId('mseismo-dialog').waitFor({ state: 'hidden' }).catch(() => {});
}

async function captureViewer(browser) {
  const context = await browser.newContext({
    viewport: VIEWPORTS.desktop,
    colorScheme: 'light',
  });
  const page = await context.newPage();
  await page.goto(BASE);

  await resetReceiver();
  await fetch(`${RECEIVER}/__mode`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode: 'ok' }),
  });

  // One manual report with a screenshot.
  await openForm(page);
  await page.getByTestId('mseismo-description').fill('Viewer demo: export button did nothing.');
  await page
    .getByTestId('mseismo-screenshot')
    .setInputFiles({ name: 'viewer.png', mimeType: 'image/png', buffer: PNG });
  await page.getByTestId('mseismo-send').click();
  await page.getByTestId('mseismo-dialog').waitFor({ state: 'hidden' });

  // One automatic error report.
  await page.getByTestId('rp-reinit-auto-errors').click();
  await page.getByTestId('rp-uncaught').click();
  await page.waitForTimeout(500);

  if (includeViewer) {
    await page.getByTestId('viewer-refresh').click().catch(() => {});
    await page.getByTestId('viewer-load-latest').click().catch(() => {});
    await page.waitForTimeout(400);
    await page.locator('#output').scrollIntoViewIfNeeded().catch(() => {});
    await shot(page, 'viewer-cards-desktop-light', false);
    await page.getByTestId('viewer-tab-raw').click().catch(() => {});
    await shot(page, 'viewer-raw-desktop-light', false);
    await page.getByTestId('viewer-tab-summary').click().catch(() => {});
    await page.getByTestId('viewer-tab-redaction').click().catch(() => {});
    await shot(page, 'viewer-redaction-desktop-light', false);
  }

  await context.close();

  const mobile = await browser.newContext({
    viewport: VIEWPORTS.mobile,
    colorScheme: 'light',
  });
  const mobilePage = await mobile.newPage();
  await mobilePage.goto(BASE);
  if (includeViewer) {
    await mobilePage.getByTestId('viewer-load-latest').click().catch(() => {});
    await mobilePage.waitForTimeout(300);
    await mobilePage.locator('#output').scrollIntoViewIfNeeded().catch(() => {});
    await shot(mobilePage, 'viewer-cards-mobile-light', false);
  }
  await mobile.close();

  if (includeViewer) {
    await shotReceiverViewer(browser, 'desktop-light', VIEWPORTS.desktop, 'light');
    await shotReceiverViewer(browser, 'desktop-dark', VIEWPORTS.desktop, 'dark');
    await shotReceiverViewer(browser, 'mobile-light', VIEWPORTS.mobile, 'light');
  }
}

async function shotReceiverViewer(browser, label, viewport, colorScheme) {
  const context = await browser.newContext({ viewport, colorScheme });
  const page = await context.newPage();
  await page.goto(`${RECEIVER}/__viewer`);
  await page.waitForTimeout(400);
  await shot(page, `receiver-viewer-${label}`, false);
  await context.close();
}

async function main() {
  const browser = await chromium.launch();
  try {
    for (const [viewportName, viewport] of Object.entries(VIEWPORTS)) {
      for (const theme of THEMES) {
        const context = await browser.newContext({ viewport, colorScheme: theme });
        const page = await context.newPage();
        await page.goto(BASE);
        await shot(page, `home-${viewportName}-${theme}`, true);
        await shot(page, `launcher-${viewportName}-${theme}`, false);
        await page.goto(`${BASE}/lab.html`);
        await page.locator('[data-testid="lab-rows"] tr').first().waitFor();
        await shot(page, `lab-${viewportName}-${theme}`, true);
        await captureFormStates(page, `${viewportName}-${theme}`);
        await context.close();
      }
    }
    if (includeViewer) await captureViewer(browser);
  } finally {
    await browser.close();
    await resetReceiver().catch(() => {});
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
