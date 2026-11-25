import { describe, expect, it } from 'vitest';
import { matchesShortcut, parseShortcut } from '@sdk/core/shortcut';

function key(
  keyName: string,
  mods: { alt?: boolean; ctrl?: boolean; meta?: boolean; shift?: boolean } = {},
): KeyboardEvent {
  return {
    key: keyName,
    altKey: mods.alt ?? false,
    ctrlKey: mods.ctrl ?? false,
    metaKey: mods.meta ?? false,
    shiftKey: mods.shift ?? false,
  } as KeyboardEvent;
}

describe('parseShortcut', () => {
  it('parses Ctrl+Enter', () => {
    expect(parseShortcut('Ctrl+Enter', false)).toEqual({
      key: 'enter',
      alt: false,
      ctrl: true,
      meta: false,
      shift: false,
    });
  });

  it('is case-insensitive and tolerates spaces around tokens', () => {
    expect(parseShortcut(' ctrl + enter ', false)).toEqual({
      key: 'enter',
      alt: false,
      ctrl: true,
      meta: false,
      shift: false,
    });
  });

  it('resolves Mod to Ctrl on non-mac and Meta on mac', () => {
    expect(parseShortcut('Mod+K', false)?.ctrl).toBe(true);
    expect(parseShortcut('Mod+K', false)?.meta).toBe(false);
    expect(parseShortcut('Mod+K', true)?.meta).toBe(true);
    expect(parseShortcut('Mod+K', true)?.ctrl).toBe(false);
  });

  it('accepts Cmd/Meta/Command synonyms', () => {
    for (const token of ['Cmd', 'Meta', 'Command']) {
      expect(parseShortcut(`${token}+K`, false)?.meta).toBe(true);
    }
  });

  it('rejects empty, modifier-only and multi-key combos', () => {
    expect(parseShortcut('', false)).toBeNull();
    expect(parseShortcut('   ', false)).toBeNull();
    expect(parseShortcut('Ctrl+Shift', false)).toBeNull();
    expect(parseShortcut('Ctrl+K+J', false)).toBeNull();
  });

  it('returns a lowercase key name', () => {
    expect(parseShortcut('Ctrl+Space', false)?.key).toBe('space');
  });
});

describe('matchesShortcut', () => {
  const ctrlEnter = parseShortcut('Ctrl+Enter', false)!;

  it('matches the exact combo', () => {
    expect(matchesShortcut(key('Enter', { ctrl: true }), ctrlEnter)).toBe(true);
  });

  it('does not match when a modifier is missing', () => {
    expect(matchesShortcut(key('Enter'), ctrlEnter)).toBe(false);
  });

  it('does not match when an extra modifier is held (exact match)', () => {
    expect(matchesShortcut(key('Enter', { ctrl: true, shift: true }), ctrlEnter)).toBe(
      false,
    );
  });

  it('does not match a different key', () => {
    expect(matchesShortcut(key('K', { ctrl: true }), ctrlEnter)).toBe(false);
  });

  it('normalises the space key', () => {
    const spec = parseShortcut('Alt+Space', false)!;
    expect(matchesShortcut(key(' ', { alt: true }), spec)).toBe(true);
    expect(matchesShortcut(key('Space', { alt: true }), spec)).toBe(true);
  });
});
