/**
 * Keyboard-shortcut parsing and matching.
 *
 * Shortcuts are written as `+`-separated combos, e.g. `'Ctrl+Enter'`,
 * `'Mod+Shift+K'`. `Mod` maps to Cmd/Meta on macOS and Ctrl elsewhere. Matching
 * is exact: every modifier must match the pressed state, so `'Ctrl+Enter'` does
 * not fire when Shift is also held.
 *
 * SSR-safe: nothing here touches `window`/`document` at module load.
 */

export interface ShortcutSpec {
  /** Normalised, lower-cased key name, e.g. `'enter'`, `'k'`, `'space'`. */
  readonly key: string;
  readonly alt: boolean;
  readonly ctrl: boolean;
  readonly meta: boolean;
  readonly shift: boolean;
}

const MODIFIER_TOKENS = new Set([
  'ctrl',
  'control',
  'alt',
  'option',
  'shift',
  'meta',
  'cmd',
  'command',
  'mod',
]);

function isModifierToken(token: string): boolean {
  return MODIFIER_TOKENS.has(token);
}

/**
 * Parses a shortcut string into a {@link ShortcutSpec}, or `null` when it is not
 * a valid combo (empty, multiple non-modifier keys, modifier-only, etc.).
 *
 * @param input the combo, e.g. `'Ctrl+Enter'`.
 * @param isMac whether `Mod` should resolve to Meta (`true`) or Ctrl (`false`).
 */
export function parseShortcut(input: string, isMac: boolean): ShortcutSpec | null {
  if (typeof input !== 'string') return null;
  const tokens = input
    .split('+')
    .map((token) => token.trim())
    .filter((token) => token.length > 0);
  if (tokens.length === 0) return null;

  let key: string | null = null;
  let alt = false;
  let ctrl = false;
  let meta = false;
  let shift = false;

  for (const rawToken of tokens) {
    const token = rawToken.toLowerCase();
    if (isModifierToken(token)) {
      if (token === 'ctrl' || token === 'control') ctrl = true;
      else if (token === 'alt' || token === 'option') alt = true;
      else if (token === 'shift') shift = true;
      else if (token === 'meta' || token === 'cmd' || token === 'command') meta = true;
      else if (token === 'mod') {
        if (isMac) meta = true;
        else ctrl = true;
      }
      continue;
    }
    // A second non-modifier token means the combo is ambiguous -> invalid.
    if (key !== null) return null;
    key = token;
  }

  if (key === null) return null;
  return { key, alt, ctrl, meta, shift };
}

function normaliseEventKey(event: KeyboardEvent): string {
  if (event.key === ' ') return 'space';
  return event.key.toLowerCase();
}

/** True when the keyboard event matches the spec exactly (modifiers included). */
export function matchesShortcut(event: KeyboardEvent, spec: ShortcutSpec): boolean {
  if (normaliseEventKey(event) !== spec.key) return false;
  return (
    event.altKey === spec.alt &&
    event.ctrlKey === spec.ctrl &&
    event.metaKey === spec.meta &&
    event.shiftKey === spec.shift
  );
}

/** Best-effort macOS detection. Returns `false` outside a browser. */
export function detectMac(): boolean {
  if (typeof navigator === 'undefined') return false;
  const platform = typeof navigator.platform === 'string' ? navigator.platform : '';
  const userAgent = typeof navigator.userAgent === 'string' ? navigator.userAgent : '';
  return /mac|iphone|ipad|ipod/i.test(platform) || /mac/i.test(userAgent);
}
