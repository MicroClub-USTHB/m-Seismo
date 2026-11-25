/**
 * Polling helpers. Never `sleep` to synchronise: poll a predicate until it is
 * true or a timeout elapses, then report the failure to the caller.
 */

/** Default poll budget for a single assertion, in milliseconds. */
export const DEFAULT_WAIT_MS = 5_000;
/** Default poll interval, in milliseconds. */
export const DEFAULT_INTERVAL_MS = 50;

/**
 * Polls `predicate` until it returns true or `timeoutMs` elapses. Returns
 * `true`/`false` instead of throwing so verifiers can turn a timeout into a
 * `fail` verdict with their own message.
 */
export async function waitFor(
  predicate: () => boolean | Promise<boolean>,
  timeoutMs: number = DEFAULT_WAIT_MS,
  intervalMs: number = DEFAULT_INTERVAL_MS,
): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    let ok = false;
    try {
      ok = await predicate();
    } catch {
      ok = false;
    }
    if (ok) return true;
    if (Date.now() >= deadline) return false;
    await new Promise<void>((resolve) => setTimeout(resolve, intervalMs));
  }
}

/**
 * Polls `read` until it returns a defined value, or the timeout elapses. Unlike
 * {@link waitFor} this returns the value (useful for "wait until ≥ 1 report").
 */
export async function waitForValue<T>(
  read: () => T | undefined | Promise<T | undefined>,
  timeoutMs: number = DEFAULT_WAIT_MS,
  intervalMs: number = DEFAULT_INTERVAL_MS,
): Promise<T | undefined> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    try {
      const value = await read();
      if (value !== undefined) return value;
    } catch {
      // keep polling
    }
    if (Date.now() >= deadline) return undefined;
    await new Promise<void>((resolve) => setTimeout(resolve, intervalMs));
  }
}

/** Resolves after one animation frame (used to measure responsiveness). */
export function nextFrame(): Promise<void> {
  return new Promise((resolve) => {
    if (typeof requestAnimationFrame === 'function') {
      requestAnimationFrame(() => resolve());
    } else {
      setTimeout(resolve, 16);
    }
  });
}
