/**
 * Interaction collector (Phase 5.3).
 *
 * Records allow-listed `click` and `submit` events as `interaction`
 * breadcrumbs. It is deliberately low-cardinality and privacy-conscious:
 *
 * - only a fixed allow-list of elements is recorded (buttons, links with an
 *   `href`, button-ish inputs, `[role="button"]`, `summary`, and forms on
 *   submit);
 * - the target is described by `tag`/`id`/`role`/`data-testid`/`type`/`name`
 *   only — never `href`, values, or (unless `captureElementText` is enabled)
 *   visible text;
 * - password inputs are never recorded;
 * - visible text is never read from inputs/textareas/selects/contenteditable
 *   elements, and is capped to {@link INTERACTION_LABEL_MAX_LENGTH} characters;
 * - identical consecutive interactions are collapsed within
 *   {@link INTERACTION_THROTTLE_MS};
 * - clicks/submits inside the SDK's own form or launcher are ignored.
 *
 * Listeners are passive lifecycle listeners; the collector never throws and
 * never calls `preventDefault`/`stopPropagation`.
 */
import { createInteractionDiagnosticEvent } from '../diagnostics/event';
import {
  INTERACTION_LABEL_MAX_LENGTH,
  INTERACTION_THROTTLE_MS,
} from '../diagnostics/types';
import type { DiagnosticEvent, InteractionAction, InteractionTarget } from '../diagnostics/types';

const CLICK_ALLOWLIST =
  'button, a[href], input[type="button"], input[type="submit"], input[type="reset"], input[type="checkbox"], input[type="radio"], [role="button"], summary';

const SDK_SELECTOR = '[data-mseismo-form], [data-mseismo-launcher]';

export interface InteractionCollectorOptions {
  /** Whether to include a short visible-text/aria label. Defaults to `false`. */
  readonly captureElementText?: boolean;
  /** Injectable wall clock (ms). Defaults to `Date.now`. */
  readonly now?: () => number;
  /** Throttle window for identical consecutive interactions. */
  readonly throttleMs?: number;
}

function isElement(value: unknown): value is Element {
  if (value === null || typeof value !== 'object') return false;
  try {
    const node = value as { nodeType?: unknown; closest?: unknown };
    return typeof node.nodeType === 'number' && typeof node.closest === 'function';
  } catch {
    return false;
  }
}

function attr(el: Element, name: string): string | undefined {
  try {
    const value = el.getAttribute(name);
    return value !== null && value.length > 0 ? value : undefined;
  } catch {
    return undefined;
  }
}

function shouldReadText(el: Element): boolean {
  const tag = el.tagName.toLowerCase();
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return false;
  try {
    if ((el as HTMLElement).isContentEditable) return false;
  } catch {
    // ignore
  }
  return true;
}

function elementLabel(el: Element): string | undefined {
  const aria = attr(el, 'aria-label');
  if (aria !== undefined) return aria.slice(0, INTERACTION_LABEL_MAX_LENGTH);
  if (!shouldReadText(el)) return undefined;
  let text: string | null;
  try {
    text = el.textContent;
  } catch {
    return undefined;
  }
  if (text === null) return undefined;
  const trimmed = text.replace(/\s+/g, ' ').trim();
  return trimmed.length > 0 ? trimmed.slice(0, INTERACTION_LABEL_MAX_LENGTH) : undefined;
}

function describeTarget(
  el: Element,
  action: InteractionAction,
  captureElementText: boolean,
): InteractionTarget {
  const target: {
    tag: string;
    id?: string;
    role?: string;
    testId?: string;
    type?: string;
    name?: string;
    label?: string;
  } = { tag: el.tagName.toLowerCase() };

  const id = attr(el, 'id');
  if (id !== undefined) target.id = id;
  const role = attr(el, 'role');
  if (role !== undefined) target.role = role;
  const testId = attr(el, 'data-testid');
  if (testId !== undefined) target.testId = testId;
  const type = attr(el, 'type');
  if (type !== undefined) target.type = type;
  const name = attr(el, 'name');
  if (name !== undefined) target.name = name;

  if (captureElementText && action === 'click') {
    const label = elementLabel(el);
    if (label !== undefined) target.label = label;
  }
  return target;
}

function isPasswordInput(el: Element): boolean {
  return el.tagName.toLowerCase() === 'input' && (attr(el, 'type') ?? '').toLowerCase() === 'password';
}

function signature(action: InteractionAction, target: InteractionTarget): string {
  return [action, target.tag, target.id ?? '', target.testId ?? '', target.name ?? ''].join('|');
}

/**
 * Installs the collector on `host` (the document). Returns a disposer that
 * removes the listeners.
 */
export function installInteractionCollector(
  host: Document,
  record: (event: DiagnosticEvent) => void,
  options: InteractionCollectorOptions = {},
): () => void {
  const captureElementText = options.captureElementText ?? false;
  const now = options.now ?? Date.now;
  const throttleMs = options.throttleMs ?? INTERACTION_THROTTLE_MS;

  let lastSignature: string | null = null;
  let lastTime = 0;

  const handle = (event: Event, action: InteractionAction): void => {
    try {
      const rawTarget = event.target;
      if (!isElement(rawTarget)) return;

      const el =
        action === 'click'
          ? rawTarget.closest(CLICK_ALLOWLIST)
          : rawTarget.closest('form');
      if (el === null || el === undefined) return;

      // Never record interactions with the SDK's own UI.
      if (el.closest(SDK_SELECTOR) !== null) return;
      if (action === 'click' && isPasswordInput(el)) return;

      const target = describeTarget(el, action, captureElementText);
      const sig = signature(action, target);
      const time = now();
      if (sig === lastSignature && time - lastTime < throttleMs) return;
      lastSignature = sig;
      lastTime = time;

      record(createInteractionDiagnosticEvent({ action, target }));
    } catch {
      // Observing an interaction must never affect it.
    }
  };

  const onClick = (event: Event): void => handle(event, 'click');
  const onSubmit = (event: Event): void => handle(event, 'submit');

  try {
    host.addEventListener('click', onClick);
    host.addEventListener('submit', onSubmit);
  } catch {
    return () => {};
  }

  return () => {
    try {
      host.removeEventListener('click', onClick);
      host.removeEventListener('submit', onSubmit);
    } catch {
      // ignore
    }
  };
}
