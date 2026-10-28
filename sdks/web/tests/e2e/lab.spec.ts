/**
 * Error-laboratory e2e: runs the 25 scenarios in a real browser and asserts each
 * observed verdict matches its expected category, then writes a per-browser
 * results file for `npm run lab:matrix`.
 *
 * The page exposes a small programmatic API (`window.__lab*`) so the spec does
 * not have to click 25 rows. A few scenarios get stronger, browser-specific
 * driving (real keys, real file chooser, media emulation, true offline).
 */
import { expect, test } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import type { Page } from '@playwright/test';

interface LabResult {
  id: string;
  title: string;
  group: string;
  mode: string;
  expected: 'DETECTED_AUTO' | 'MANUAL_ONLY' | 'NOT_SUPPORTED' | 'LIMITED' | 'KNOWN_GAP';
  status: 'pass' | 'fail' | 'info' | 'needs-manual';
  observed: string;
  evidence?: unknown;
}

const STATUS_ACCEPTED: Record<LabResult['expected'], LabResult['status'][]> = {
  DETECTED_AUTO: ['pass'],
  MANUAL_ONLY: ['pass'],
  NOT_SUPPORTED: ['pass'],
  LIMITED: ['pass', 'info'],
  KNOWN_GAP: ['pass', 'info'],
};

function assertAccepted(results: LabResult[]): void {
  expect(results.length).toBe(25);
  for (const result of results) {
    expect(result.status, `${result.id} observed: ${result.observed}`).not.toBe('needs-manual');
    expect(
      STATUS_ACCEPTED[result.expected],
      `${result.id} expected ${result.expected} but observed ${result.status}: ${result.observed}`,
    ).toContain(result.status);
  }
}

async function openLab(page: Page): Promise<void> {
  await page.goto('/lab.html');
  await expect(page.locator('[data-testid="lab-rows"] tr')).toHaveCount(25);
}

function writeResults(project: string, results: LabResult[]): void {
  const dir = path.resolve(process.cwd(), 'report/lab-results');
  mkdirSync(dir, { recursive: true });
  writeFileSync(path.join(dir, `${project}.json`), `${JSON.stringify(results, null, 2)}\n`, 'utf8');
}

test.describe('error laboratory', () => {
  test('every scenario matches its expected category', async ({ page }, testInfo) => {
    await openLab(page);
    const results = (await page.evaluate(() => window.__labRunAll!(true))) as LabResult[];
    assertAccepted(results);
    writeResults(testInfo.project.name, results);
  });

  test('E10 also holds with the browser truly offline', async ({ page, context }, testInfo) => {
    test.skip(testInfo.project.name === 'webkit', 'webkit is not run here');
    await openLab(page);
    await context.setOffline(true);
    const result = (await page.evaluate(() => window.__labRun!('E10'))) as LabResult;
    await context.setOffline(false);
    expect(STATUS_ACCEPTED[result.expected]).toContain(result.status);
  });

  test('E23 renders in dark emulation', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'webkit', 'webkit is not run here');
    await openLab(page);
    await page.emulateMedia({ colorScheme: 'dark' });
    const result = (await page.evaluate(() => window.__labRun!('E23'))) as LabResult;
    await page.emulateMedia({ colorScheme: null });
    expect(STATUS_ACCEPTED[result.expected]).toContain(result.status);
    const evidence = result.evidence as { mediaDark?: boolean } | undefined;
    expect(evidence?.mediaDark).toBe(true);
  });

  test('E24 opens with real Ctrl+Enter and ignores it while typing', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'webkit', 'webkit is not run here');
    await openLab(page);
    await page.locator('[data-testid="lab-typing"]').focus();
    await page.keyboard.press('Control+Enter');
    await expect(page.locator('[data-mseismo-form]')).toHaveCount(0);

    await page.locator('body').click({ position: { x: 5, y: 5 } });
    await page.keyboard.press('Control+Enter');
    await expect(page.locator('[data-mseismo-form]')).toHaveCount(1);
    await page.keyboard.press('Escape');
    await expect(page.locator('[data-mseismo-form]')).toHaveCount(0);
  });

  test('E25 attaches a screenshot chosen through the real file input', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'webkit', 'webkit is not run here');
    await openLab(page);
    const png = Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
      'base64',
    );
    await page.locator('body').click({ position: { x: 5, y: 5 } });
    await page.keyboard.press('Control+Enter');
    await expect(page.locator('[data-mseismo-form]')).toHaveCount(1);
    await page.locator('[data-testid="mseismo-screenshot"]').setInputFiles({
      name: 'real.png',
      mimeType: 'image/png',
      buffer: png,
    });
    await expect(page.locator('[data-testid="mseismo-file-info"]')).toContainText('real.png');
    await page.locator('[data-testid="mseismo-description"]').fill('E25 real file input');
    await page.locator('[data-testid="mseismo-send"]').click();
    await page.keyboard.press('Escape');
    const reports = await page.evaluate(async () => {
      const response = await fetch('http://localhost:4010/__reports');
      return (await response.json()) as { count: number; reports: Array<{ screenshot?: { name?: string } }> };
    });
    expect(reports.reports.some((report) => report.screenshot?.name === 'real.png')).toBe(true);
  });
});
