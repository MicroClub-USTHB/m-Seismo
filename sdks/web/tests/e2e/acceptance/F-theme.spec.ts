/**
 * Acceptance F — theming and style isolation.
 *
 * The SDK form lives in an open shadow root and styles itself with
 * `prefers-color-scheme`. There is no custom theme API yet (Phase 4), so the
 * host page's own theme toggle must NOT be able to restyle it.
 */
import { expect, test } from '@playwright/test';
import { openForm, resetReceiver } from './helpers';

async function dialogBackground(page: import('@playwright/test').Page): Promise<string> {
  return page.getByTestId('mseismo-dialog').evaluate((dialog) => {
    return getComputedStyle(dialog).backgroundColor;
  });
}

test.beforeEach(async ({ page, request }) => {
  await resetReceiver(request);
});

test.describe('light colour scheme', () => {
  test.use({ colorScheme: 'light' });

  test('F: the dialog uses the light palette', async ({ page }) => {
    await page.goto('/');
    await openForm(page);
    expect(await dialogBackground(page)).toBe('rgb(255, 255, 255)');
  });

  test('F: the host page theme toggle cannot restyle the dialog', async ({ page }) => {
    await page.goto('/');

    // Measure with the dialog open, then close it so the page button is clickable.
    await openForm(page);
    const before = await dialogBackground(page);
    await page.getByTestId('mseismo-cancel').click();
    await expect(page.getByTestId('mseismo-dialog')).toBeHidden();

    await page.getByTestId('theme-toggle').click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

    await openForm(page);
    expect(await dialogBackground(page)).toBe(before);
  });
});

test.describe('dark colour scheme', () => {
  test.use({ colorScheme: 'dark' });

  test('F: the dialog uses the dark palette', async ({ page }) => {
    await page.goto('/');
    await openForm(page);
    expect(await dialogBackground(page)).toBe('rgb(29, 32, 38)');
  });

  test('F: hostile host CSS cannot leak into the shadow root', async ({ page }) => {
    await page.goto('/');
    await openForm(page);

    await page.addStyleTag({
      content:
        'div { background: rgb(255, 0, 0) !important; color: rgb(0, 255, 0) !important; }',
    });

    const dialog = page.getByTestId('mseismo-dialog');
    expect(await dialogBackground(page)).toBe('rgb(29, 32, 38)');
    const titleColor = await dialog.evaluate((element) => {
      const title = element.querySelector('.title');
      return title === null ? null : getComputedStyle(title).color;
    });
    expect(titleColor).not.toBe('rgb(0, 255, 0)');
  });
});
