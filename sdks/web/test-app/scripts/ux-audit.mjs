/**
 * UX audit probe (dev-only helper).
 *
 * Measures objective, reproducible UI facts — horizontal scroll, text contrast,
 * touch-target sizes, focus rings, font sizes, and the visible control count —
 * for the test app, the lab page and the SDK form. The report quotes these
 * numbers as the "before" and "after" audit, so every claim in the report can be
 * re-measured with one command.
 *
 * Servers must already be running:
 *   node test-app/scripts/ux-audit.mjs --out report/ux-audit-before.json
 */
import { chromium } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

const args = process.argv.slice(2);
const outIndex = args.indexOf('--out');
const outFile = path.resolve(
  process.cwd(),
  outIndex >= 0 ? args[outIndex + 1] : 'report/ux-audit-before.json',
);

const BASE = process.env.BASE_URL ?? 'http://localhost:5173';
const RECEIVER = process.env.RECEIVER_URL ?? 'http://localhost:4010';

const VIEWPORTS = {
  desktop: { width: 1280, height: 800 },
  mobile: { width: 375, height: 667 },
};

async function setMode(mode) {
  await fetch(`${RECEIVER}/__mode`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode }),
  });
}

async function resetReceiver() {
  await fetch(`${RECEIVER}/__reset`, { method: 'POST' });
}

/** Runs measurement code inside the page and returns a plain object. */
const MEASURE = () => {
  const parse = (value) => {
    const m = /rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(value);
    return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
  };
  const lum = ([r, g, b]) => {
    const f = (c) => {
      const s = c / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
  };
  const contrast = (fg, bg) => {
    const a = parse(fg), b = parse(bg);
    if (!a || !b) return null;
    const l1 = lum(a), l2 = lum(b);
    const hi = Math.max(l1, l2), lo = Math.min(l1, l2);
    return Math.round(((hi + 0.05) / (lo + 0.05)) * 100) / 100;
  };
  const bodyStyle = getComputedStyle(document.body);
  const root = document.documentElement;
  const buttons = Array.from(document.querySelectorAll('button'));
  const first = buttons[0] ?? null;
  const focusable = Array.from(
    document.querySelectorAll('a[href], button, input, textarea, select, [tabindex]:not([tabindex="-1"])'),
  ).filter((el) => !el.hasAttribute('disabled'));
  const colors = new Set();
  for (const el of Array.from(document.querySelectorAll('p, li, h1, h2, td, th, label, button, span')).slice(0, 400)) {
    colors.add(getComputedStyle(el).color);
  }
  return {
    horizontalScroll: {
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    },
    theme: root.dataset.theme ?? null,
    bodyColor: bodyStyle.color,
    bodyBackground: bodyStyle.backgroundColor,
    bodyColorContrast: contrast(bodyStyle.color, bodyStyle.backgroundColor),
    distinctTextColors: Array.from(colors).slice(0, 12),
    redTextRulePresent: Array.from(document.styleSheets).some((sheet) => {
      try {
        return Array.from(sheet.cssRules ?? []).some((rule) => rule.selectorText === '.page, .page *');
      } catch { return false; }
    }),
    buttonCount: buttons.length,
    tabbableCount: focusable.length,
    smallTargets: buttons
      .map((el) => {
        const r = el.getBoundingClientRect();
        return { label: (el.textContent ?? '').trim().slice(0, 40), w: Math.round(r.width), h: Math.round(r.height) };
      })
      .filter((b) => b.h > 0 && (b.h < 44 || b.w < 44))
      .slice(0, 10),
    firstButtonFocusRing: first ? (() => {
      first.focus();
      const s = getComputedStyle(first);
      return { outlineStyle: s.outlineStyle, outlineWidth: s.outlineWidth, outlineColor: s.outlineColor };
    })() : null,
  };
};

async function measurePage(page) {
  return page.evaluate(MEASURE);
}

async function measureForm(page, label = '') {
  const value = await page.evaluate(() => {
    // The test app reuses the data-mseismo-form attribute on one section, so
    // pick the host that actually owns a shadow root with the dialog in it.
    const host = Array.from(document.querySelectorAll('[data-mseismo-form]')).find(
      (el) => el.shadowRoot !== null && el.shadowRoot.querySelector('[data-testid="mseismo-dialog"]') !== null,
    );
    const shadow = host === undefined ? null : host.shadowRoot;
    const dialog = shadow === null ? null : shadow.querySelector('[data-testid="mseismo-dialog"]');
    if (shadow === null || dialog === null) return null;
    const ta = shadow.querySelector('[data-testid="mseismo-description"]');
    const send = shadow.querySelector('[data-testid="mseismo-send"]');
    const cancel = shadow.querySelector('[data-testid="mseismo-cancel"]');
    const title = shadow.querySelector('.title');
    const r = dialog.getBoundingClientRect();
    return {
      dialog: { width: Math.round(r.width), height: Math.round(r.height) },
      textarea: ta ? { fontSize: getComputedStyle(ta).fontSize, minHeight: getComputedStyle(ta).minHeight } : null,
      send: send ? {
        fontSize: getComputedStyle(send).fontSize,
        height: Math.round(send.getBoundingClientRect().height),
        background: getComputedStyle(send).backgroundColor,
        color: getComputedStyle(send).color,
        borderRadius: getComputedStyle(send).borderRadius,
      } : null,
      cancelHeight: cancel ? Math.round(cancel.getBoundingClientRect().height) : null,
      title: title ? { fontSize: getComputedStyle(title).fontSize } : null,
      hasCharacterCounter: shadow.querySelector('[data-testid="mseismo-counter"]') !== null,
      hasPrivacyDisclosure: shadow.querySelector('[data-testid="mseismo-privacy"]') !== null,
      hasSuccessPanel: shadow.querySelector('[data-testid="mseismo-success"]') !== null,
      hasSpinner: shadow.querySelector('[data-testid="mseismo-spinner"]') !== null,
      shadowTransitions: Array.from(shadow.querySelectorAll('*')).filter(
        (el) => getComputedStyle(el).transitionDuration !== '0s',
      ).length,
      inputFontSizePx: ta ? Number.parseFloat(getComputedStyle(ta).fontSize) : null,
    };
  });
  process.stdout.write(`measureForm ${label}: ${value === undefined ? 'undefined' : value === null ? 'null' : 'object'}\n`);
  return value;
}

async function main() {
  const browser = await chromium.launch();
  const results = {};
  try {
    for (const [viewportName, viewport] of Object.entries(VIEWPORTS)) {
      for (const theme of ['light', 'dark']) {
        const context = await browser.newContext({ viewport, colorScheme: theme });
        const page = await context.newPage();
        const key = `${viewportName}/${theme}`;

        await page.goto(BASE);
        results[`${key}/home`] = await measurePage(page);

        await page.goto(`${BASE}/lab.html`);
        await page.locator('[data-testid="lab-rows"] tr').first().waitFor();
        results[`${key}/lab`] = await measurePage(page);

        // Form states: idle, then failure (worst-contrast case).
        await page.goto(BASE);
        await page.getByTestId('open-form').click();
        await page.getByTestId('mseismo-dialog').waitFor();
        results[`${key}/form-idle`] = await measureForm(page, `${key}/form-idle`);

        await setMode('fail500');
        await page.getByTestId('mseismo-description').fill('audit probe description');
        await page.getByTestId('mseismo-send').click();
        await page.getByTestId('mseismo-message').filter({ hasText: 'HTTP 500' }).waitFor();
        results[`${key}/form-failure`] = await measureForm(page, `${key}/form-failure`);
        results[`${key}/form-failure`].message = await page
          .getByTestId('mseismo-message')
          .evaluate((el) => ({
            text: el.textContent,
            color: getComputedStyle(el).color,
            fontSize: getComputedStyle(el).fontSize,
          }));
        await page.keyboard.press('Escape').catch(() => {});
        await setMode('ok');
        await context.close();
      }
    }
  } finally {
    await browser.close();
    await resetReceiver().catch(() => {});
  }

  mkdirSync(path.dirname(outFile), { recursive: true });
  writeFileSync(outFile, `${JSON.stringify(results, null, 2)}\n`, 'utf8');
  process.stdout.write(`${path.relative(process.cwd(), outFile)}\n`);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
