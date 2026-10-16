/**
 * SDK orchestration: wires config to DOM side effects (shortcut, launcher, form)
 * and owns their lifecycle.
 *
 * Everything DOM-shaped is guarded by `typeof document`/`typeof window` checks,
 * so importing and even initialising the SDK in a non-browser environment is
 * safe and simply does nothing DOM-related.
 */
import { configure, getConfig, isEnabled, resetConfig } from './config';
import type { InitOptions } from './config';
import { Lifecycle } from './lifecycle';
import { warn } from './log';
import { matchesShortcut } from './shortcut';
import { createFormController } from '../form/formController';
import type { FormController } from '../form/formController';
import { createLauncher } from '../form/launcher';

let lifecycle: Lifecycle | null = null;
let form: FormController | null = null;
let launcherEl: HTMLElement | null = null;

function hasDom(): boolean {
  return typeof document !== 'undefined' && typeof window !== 'undefined';
}

/** True for elements where a keystroke is ordinary typing, not a shortcut. */
function isTypingTarget(target: EventTarget | null): boolean {
  if (target === null || !(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  return target.isContentEditable;
}

function teardownDom(): void {
  lifecycle?.dispose();
  lifecycle = null;
  form = null;
  launcherEl = null;
}

function setupDom(): void {
  if (!hasDom()) return;
  const config = getConfig();
  if (config === null) return;

  const next = new Lifecycle();
  lifecycle = next;

  const controller = createFormController();
  form = controller;
  next.add(() => {
    controller.destroy();
    if (form === controller) form = null;
  });

  if (config.shortcut !== false) {
    const spec = config.shortcut;
    const onKeydown = (event: KeyboardEvent): void => {
      if (event.defaultPrevented || event.repeat) return;
      if (controller.isOpen()) return;
      if (isTypingTarget(event.target)) return;
      if (!matchesShortcut(event, spec)) return;
      event.preventDefault();
      controller.open();
    };
    document.addEventListener('keydown', onKeydown);
    next.add(() => document.removeEventListener('keydown', onKeydown));
  }

  if (config.launcher.enabled) {
    const button = createLauncher(config.launcher, () => controller.open());
    launcherEl = button;
    document.body.appendChild(button);
    next.add(() => {
      button.remove();
      if (launcherEl === button) launcherEl = null;
    });
  }
}

/**
 * Validate config and (re)build all DOM side effects. Re-running tears down the
 * previous instance first, so `init()` is idempotent.
 */
export function initSdk(options: InitOptions): boolean {
  const ok = configure(options);
  teardownDom();
  if (ok) setupDom();
  return ok;
}

/** Open the bug-report form. No-op before a successful {@link initSdk}. */
export function openForm(): void {
  if (!isEnabled()) {
    warn('openReportForm() called before a successful init(); ignoring.');
    return;
  }
  form?.open();
}

/** Remove all DOM side effects and disable the SDK (config is cleared). */
export function destroySdk(): void {
  teardownDom();
  resetConfig();
}
