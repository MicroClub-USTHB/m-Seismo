/**
 * Registry for the SDK's captured *original* `fetch` (Phase 5.3).
 *
 * The SDK wraps `globalThis.fetch` to observe failed requests (see
 * {@link ../network/instrumentFetch}). The SDK's own report upload must NOT be
 * observed (otherwise every send would record itself), so the transport uses
 * this captured original instead of the (possibly wrapped) global.
 *
 * The original is remembered the first time the wrapper is installed; before
 * that, {@link getOriginalFetch} falls back to the current global `fetch`. All
 * functions are SSR-safe and never throw.
 */

type FetchFn = typeof globalThis.fetch;

let remembered: FetchFn | undefined;

/** Remembers `fn` as the original fetch the first time it is called. */
export function rememberOriginalFetch(fn: FetchFn | undefined): void {
  if (remembered === undefined && typeof fn === 'function') {
    remembered = fn;
  }
}

/** The captured original fetch, or the ambient global `fetch` if none captured. */
export function getOriginalFetch(): FetchFn | undefined {
  if (remembered !== undefined) return remembered;
  if (typeof globalThis.fetch === 'function') return globalThis.fetch;
  return undefined;
}

/** Test-only: forget the remembered original. */
export function resetOriginalFetch(): void {
  remembered = undefined;
}
