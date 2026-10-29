/**
 * UI scenarios: E23–E25.
 */
import {
  cancelForm,
  fillAndSubmit,
  formFileInfo,
  formMessage,
  openFillSubmit,
  openFormAndWait,
  oversizedPngFile,
  pickFile,
  pngFile,
  waitForFormClosed,
} from '../helpers/form';
import { storedReports, waitForStoredCount } from '../helpers/reports';
import { nextFrame, waitFor } from '../helpers/waiters';
import { fail, pass } from '../verdict';
import type { LabScenario, Verdict } from '../types';

function dialogEl(): HTMLElement | null {
  return document.querySelector('[data-mseismo-form]')?.shadowRoot?.querySelector<HTMLElement>('[data-testid="mseismo-dialog"]') ?? null;
}

function styleSnapshot(): { background: string; color: string } | null {
  const dialog = dialogEl();
  if (dialog === null) return null;
  const style = getComputedStyle(dialog);
  return { background: style.backgroundColor, color: style.color };
}

// --- E23 -------------------------------------------------------------------
const e23: Record<string, unknown> = {};

const e23Scenario: LabScenario = {
  id: 'E23',
  title: 'Light/dark and hostile page CSS',
  group: 'ui',
  mode: 'assisted',
  expected: 'LIMITED',
  notes:
    'The shadow root isolates the dialog from the host page; html.dark and the page CSS variable do not affect it. ' +
    'There is NO custom theme API (Phase 4 was skipped) — a host cannot theme the form. Dark mode is only the built-in prefers-color-scheme. ' +
    'Run under DevTools rendering emulation for real dark. The lab page deliberately uses a nasty colour rule to prove isolation.',
  manualSteps: [
    'Open DevTools → Rendering → "Emulate CSS media feature prefers-color-scheme" → dark.',
    'Run E23 and confirm the dialog still renders readably (light theme is expected: no custom dark theme).',
    'Reset emulation to "No emulation".',
  ],
  config: {},
  async run(ctx) {
    await openFormAndWait();
    const before = styleSnapshot();
    const mediaDark = window.matchMedia('(prefers-color-scheme: dark)').matches;

    const hostile = document.createElement('style');
    hostile.textContent =
      '* { background: #ff00ff !important; color: #00ff00 !important; font-size: 40px !important; }';
    document.head.appendChild(hostile);
    await nextFrame();
    const after = styleSnapshot();
    hostile.remove();

    e23['before'] = before;
    e23['after'] = after;
    e23['mediaDark'] = mediaDark;
    e23['hostileUnchanged'] = before !== null && after !== null && before.background === after.background && before.color === after.color;
    e23['customThemeApi'] = false;
    cancelForm();
  },
  async verify(): Promise<Verdict> {
    const before = e23['before'] as { background: string; color: string } | null;
    const readable =
      before !== null && before.background !== 'rgba(0, 0, 0, 0)' && before.background !== 'transparent' && before.color !== before.background;
    const checks: Record<string, boolean> = {
      dialogVisible: dialogEl() !== null || before !== null,
      readable,
      hostileIsolated: e23['hostileUnchanged'] === true,
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    const evidence = {
      background: before?.background,
      color: before?.color,
      mediaDark: e23['mediaDark'],
      customThemeApi: false,
      note: 'no custom theme API (Phase 4 not built); light theme is the only option',
    };
    return failed.length === 0
      ? pass('dialog readable and isolated from hostile host CSS; no custom theme API (documented gap)', evidence)
      : fail(`UI isolation checks failed: ${failed.join(', ')}`, evidence);
  },
};

// --- E24 -------------------------------------------------------------------
function press(target: EventTarget, modifiers: { ctrl?: boolean; alt?: boolean; shift?: boolean; key: string }): boolean {
  const event = new KeyboardEvent('keydown', {
    key: modifiers.key,
    code: modifiers.key,
    ctrlKey: modifiers.ctrl ?? false,
    altKey: modifiers.alt ?? false,
    shiftKey: modifiers.shift ?? false,
    bubbles: true,
    cancelable: true,
    composed: true,
  });
  return target.dispatchEvent(event);
}

const e24: Record<string, boolean> = {};

const e24Scenario: LabScenario = {
  id: 'E24',
  title: 'Keyboard: Ctrl+Enter, custom, disabled, typing-safe',
  group: 'ui',
  mode: 'assisted',
  expected: 'LIMITED',
  notes:
    'A real Ctrl+Enter opens the form; inside a textarea it is left to the field; a custom shortcut works; false disables it; the SDK calls preventDefault only when it acts. ' +
    'The lab drives synthetic KeyboardEvents (not real keys); run manually for the true key path.',
  manualSteps: [
    'Click on the page background and press Ctrl+Enter — the form opens.',
    'Cancel, click into the "Type here" textarea and press Ctrl+Enter — the form must NOT open.',
  ],
  config: {},
  async run(ctx) {
    cancelForm();
    e24['defaultOpens'] = await openViaShortcut(() => press(document.body, { ctrl: true, key: 'Enter' }));
    cancelForm();
    await nextFrame();

    const textarea = document.querySelector<HTMLTextAreaElement>('[data-testid="lab-typing"]');
    if (textarea !== null) textarea.focus();
    press(textarea ?? document.body, { ctrl: true, key: 'Enter' });
    await nextFrame();
    e24['typingNotIntercepted'] = document.querySelector('[data-mseismo-form]') === null;

    ctx.init({ shortcut: 'Alt+Shift+K' });
    cancelForm();
    e24['customOpens'] = await openViaShortcut(() => press(document.body, { alt: true, shift: true, key: 'K' }));
    cancelForm();
    press(document.body, { ctrl: true, key: 'Enter' });
    await nextFrame();
    e24['oldShortcutIgnored'] = document.querySelector('[data-mseismo-form]') === null;

    ctx.init({ shortcut: false });
    cancelForm();
    press(document.body, { ctrl: true, key: 'Enter' });
    await nextFrame();
    e24['disabledNoOpen'] = document.querySelector('[data-mseismo-form]') === null;

    ctx.init({});
    cancelForm();
    let preventedWhenActing = false;
    let preventedWhileTyping = true;
    const observer = (event: KeyboardEvent): void => {
      if (document.querySelector('[data-mseismo-form]') !== null) preventedWhenActing = event.defaultPrevented;
      else if (document.activeElement === textarea) preventedWhileTyping = event.defaultPrevented;
    };
    window.addEventListener('keydown', observer);
    press(document.body, { ctrl: true, key: 'Enter' });
    await nextFrame();
    cancelForm();
    if (textarea !== null) textarea.focus();
    press(textarea ?? document.body, { ctrl: true, key: 'Enter' });
    await nextFrame();
    window.removeEventListener('keydown', observer);
    e24['preventsDefaultWhenActing'] = preventedWhenActing;
    e24['doesNotPreventWhileTyping'] = !preventedWhileTyping;
    cancelForm();
  },
  async verify(): Promise<Verdict> {
    const checks: Record<string, boolean> = {
      defaultOpens: e24['defaultOpens'] === true,
      typingNotIntercepted: e24['typingNotIntercepted'] === true,
      customOpens: e24['customOpens'] === true,
      oldShortcutIgnored: e24['oldShortcutIgnored'] === true,
      disabledNoOpen: e24['disabledNoOpen'] === true,
      preventsDefaultWhenActing: e24['preventsDefaultWhenActing'] === true,
      doesNotPreventWhileTyping: e24['doesNotPreventWhileTyping'] === true,
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    return failed.length === 0
      ? pass('shortcut opens only where intended, respects typing targets and can be customised/disabled', { ...e24 })
      : fail(`keyboard checks failed: ${failed.join(', ')}`, { ...e24 });
  },
};

async function openViaShortcut(trigger: () => void): Promise<boolean> {
  trigger();
  return waitFor(() => document.querySelector('[data-mseismo-form]') !== null, 1_000);
}

// --- E25 -------------------------------------------------------------------
const e25: Record<string, unknown> = {};

const e25Scenario: LabScenario = {
  id: 'E25',
  title: 'Screenshot: attach, reject, clear',
  group: 'ui',
  mode: 'assisted',
  expected: 'DETECTED_AUTO',
  notes:
    'A valid screenshot is attached and stored with name/mime/size; a wrong-type or oversize file is rejected inline; no file means no screenshot; reopening clears the previous pick. ' +
    'The data is inlined base64 (the documented ASSUMED contract). There is no explicit Remove control and no sensitivity notice (recorded as findings).',
  manualSteps: [
    'Run E25 and confirm a small image attaches and is sent with the report.',
    'Use the real file picker to repeat the wrong-type and oversize rejections if desired.',
  ],
  config: { reporting: { autoReport: 'off' } },
  async run(ctx) {
    await openFormAndWait();
    await pickFile(pngFile('lab.png'));
    e25['fileInfo'] = formFileInfo();
    await fillAndSubmit('E25 with screenshot');
    await waitForStoredCount(ctx, 1);
    const withShot = (await storedReports(ctx))[0];
    e25['screenshot'] = withShot?.screenshot ?? null;
    await waitForFormClosed();

    await openFormAndWait();
    await pickFile(new File(['not an image'], 'note.txt', { type: 'text/plain' }));
    e25['wrongTypeMessage'] = formMessage();
    cancelForm();

    await openFormAndWait();
    await pickFile(oversizedPngFile());
    e25['oversizeMessage'] = formMessage();
    cancelForm();

    await openFillSubmit('E25 without screenshot');
    await waitForStoredCount(ctx, 2);
    const noShot = (await storedReports(ctx))[1];
    e25['noScreenshot'] = noShot?.screenshot ?? null;
    await waitForFormClosed();

    await openFormAndWait();
    await pickFile(pngFile('again.png'));
    const infoBeforeCancel = formFileInfo();
    cancelForm();
    await openFormAndWait();
    e25['clearedOnReopen'] = formFileInfo() === '';
    e25['infoBeforeCancel'] = infoBeforeCancel;
    e25['hasRemoveControl'] = false;
    e25['sensitivityNotice'] = false;
    cancelForm();
  },
  async verify(): Promise<Verdict> {
    const shot = e25['screenshot'] as { name?: string; mimeType?: string; sizeBytes?: number } | null;
    const checks: Record<string, boolean> = {
      attached: shot?.name === 'lab.png' && shot?.mimeType === 'image/png' && (shot?.sizeBytes ?? 0) > 0,
      wrongTypeRejected: String(e25['wrongTypeMessage']).includes('PNG, JPEG, or WebP'),
      oversizeRejected: String(e25['oversizeMessage']).includes('smaller'),
      noneMeansNull: e25['noScreenshot'] === null,
      clearedOnReopen: e25['clearedOnReopen'] === true,
    };
    const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
    const evidence = { ...e25, findings: { hasRemoveControl: false, sensitivityNotice: false, inlineBase64: true } };
    return failed.length === 0
      ? pass('screenshot attaches/rejects/clears correctly; no remove control or sensitivity notice (findings)', evidence)
      : fail(`screenshot checks failed: ${failed.join(', ')}`, evidence);
  },
};

export const uiScenarios: readonly LabScenario[] = [e23Scenario, e24Scenario, e25Scenario];
