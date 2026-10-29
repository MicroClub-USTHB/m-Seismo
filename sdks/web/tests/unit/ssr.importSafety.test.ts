/**
 * SSR import safety, per module.
 *
 * The SDK is published as a single bundle, but every `src/**` module must be
 * importable in a plain server environment (no `window`, no `document`, no
 * `navigator`). This test runs in the default Node environment and asserts that
 * importing each module neither throws nor requires DOM globals at load time.
 *
 * The smoke test already covers the public entry; this covers *every* module so
 * a future module that touches `window` at import time is caught immediately.
 */
import { describe, expect, it } from 'vitest';

const modules = import.meta.glob('/src/**/*.ts');

function normalize(path: string): string {
  return path.replace(/^\/src\//, 'src/');
}

describe('SSR import safety (every src module)', () => {
  it('discovers more than a token number of modules', () => {
    expect(Object.keys(modules).length).toBeGreaterThan(25);
  });

  it('imports every src module in a Node environment without throwing', async () => {
    const paths = Object.keys(modules).sort();
    const failures: string[] = [];
    for (const path of paths) {
      try {
        await modules[path]!();
      } catch (error) {
        failures.push(`${normalize(path)}: ${String(error)}`);
      }
    }
    expect(failures).toEqual([]);
  });

  it('does not create DOM globals merely by importing the SDK modules', async () => {
    // `window`/`document` are absent in this environment; if any module created
    // them on import the guard below would fail.
    await import('@sdk/index');
    await import('@sdk/internal');
    expect(typeof (globalThis as { document?: unknown }).document).toBe('undefined');
    expect(typeof (globalThis as { window?: unknown }).window).toBe('undefined');
  });
});
