/**
 * `fetch` instrumentation (Phase 5.3).
 *
 * Wraps `globalThis.fetch` so the SDK can record *failed* network calls as
 * breadcrumbs, without ever seeing request/response bodies, headers, cookies or
 * credentials.
 *
 * Design decisions (the six questions from the phase brief):
 * 1. **What is observed?** Only `method`, the sanitized resolved URL, the status
 *    code (if a response arrived), the elapsed time to headers-arrived, and a
 *    failure category. Nothing else is read.
 * 2. **When is an event recorded?** Only on failure: a `TypeError` rejection
 *    (`network_error`), an `AbortError` rejection (`aborted`), any other
 *    rejection (`other_error`), or a response with status ≥ 400 (`http_error`).
 *    A `type: 'opaque'`/status-0 response is recorded as `opaque_response` but is
 *    NOT a failure (it becomes an `info` breadcrumb, not a network failure).
 * 3. **Does the host see any change?** No. The wrapper returns the same promise
 *    result and never touches `init`, the `AbortSignal`, streaming, or the body.
 *    Synchronous throws from `fetch` are rethrown unchanged.
 * 4. **The SDK's own request.** The configured endpoint is skipped, so a report
 *    upload never records itself (and the transport uses the captured original
 *    fetch anyway — see `network/originalFetch`).
 * 5. **Restoration.** On dispose the wrapper is restored only if
 *    `globalThis.fetch` is still ours; if another library replaced it, we leave
 *    it alone (pass-through) and record that fact in the phase report.
 * 6. **SSR-safety.** When there is no global `fetch`, instrumentation is a
 *    no-op disposer.
 */
import { sanitizeUrl } from '../context/sanitizeUrl';
import { createNetworkDiagnosticEvent } from '../diagnostics/event';
import { BREADCRUMB_URL_MAX_LENGTH } from '../diagnostics/types';
import type { DiagnosticEvent, NetworkCategory } from '../diagnostics/types';
import { getOriginalFetch, rememberOriginalFetch } from './originalFetch';

/** Marker placed on the wrapper so double-wrapping is detectable. */
const WRAPPED = Symbol('m-seismo.instrumented-fetch');

type FetchFn = typeof globalThis.fetch;
type FetchInput = Parameters<FetchFn>[0];
type FetchInit = Parameters<FetchFn>[1];

export interface InstrumentFetchOptions {
  /** Absolute SDK endpoint; requests to it are never recorded. */
  readonly endpoint: string;
  /** Extra sensitive query params to redact from recorded URLs. */
  readonly sensitiveQueryParams?: readonly string[];
  /** Exact-string or RegExp URL patterns to ignore. */
  readonly ignoreUrls?: readonly (string | RegExp)[];
  /** Injectable monotonic clock (ms). Defaults to `performance.now`/`Date.now`. */
  readonly now?: () => number;
}

/** True when `fn` is (or was) our fetch wrapper. */
export function isInstrumentedFetch(fn: unknown): boolean {
  return (
    typeof fn === 'function' &&
    (fn as unknown as Record<PropertyKey, unknown>)[WRAPPED] === true
  );
}

function defaultNow(): number {
  try {
    if (typeof performance !== 'undefined' && typeof performance.now === 'function') {
      return performance.now();
    }
  } catch {
    // fall through
  }
  return Date.now();
}

const RELATIVE_BASE = 'http://m-seismo.invalid/';

function baseHref(): string {
  try {
    if (typeof location !== 'undefined' && typeof location.href === 'string' && location.href.length > 0) {
      return location.href;
    }
  } catch {
    // fall through
  }
  return RELATIVE_BASE;
}

function absolute(raw: string): string {
  try {
    return new URL(raw, baseHref()).href;
  } catch {
    try {
      return new URL(raw, RELATIVE_BASE).href;
    } catch {
      return raw;
    }
  }
}

function inputUrl(input: FetchInput): string | undefined {
  if (typeof input === 'string') return input;
  try {
    if (typeof URL !== 'undefined' && input instanceof URL) return input.href;
  } catch {
    // ignore
  }
  const maybe = input as { url?: unknown };
  return typeof maybe.url === 'string' ? maybe.url : undefined;
}

function inputMethod(input: FetchInput, init: FetchInit): string {
  const initMethod = init?.method;
  if (typeof initMethod === 'string' && initMethod.length > 0) {
    return initMethod.toUpperCase();
  }
  const requestMethod = (input as { method?: unknown }).method;
  if (typeof requestMethod === 'string' && requestMethod.length > 0) {
    return requestMethod.toUpperCase();
  }
  return 'GET';
}

function matchesIgnore(url: string, patterns: readonly (string | RegExp)[] | undefined): boolean {
  if (patterns === undefined) return false;
  for (const pattern of patterns) {
    try {
      if (typeof pattern === 'string') {
        if (absolute(pattern) === url) return true;
      } else if (pattern instanceof RegExp) {
        pattern.lastIndex = 0;
        if (pattern.test(url)) return true;
      }
    } catch {
      // A hostile/odd pattern must never break fetch.
    }
  }
  return false;
}

function isAbortError(error: unknown): boolean {
  try {
    if (typeof DOMException !== 'undefined' && error instanceof DOMException && error.name === 'AbortError') {
      return true;
    }
  } catch {
    // ignore
  }
  const name = (error as { name?: unknown } | null)?.name;
  return name === 'AbortError';
}

function isTypeError(error: unknown): boolean {
  try {
    if (error instanceof TypeError) return true;
  } catch {
    // ignore
  }
  return (error as { name?: unknown } | null)?.name === 'TypeError';
}

/** Installs the wrapper. Returns a disposer. */
export function instrumentFetch(
  options: InstrumentFetchOptions,
  record: (event: DiagnosticEvent) => void,
): () => void {
  if (typeof globalThis.fetch !== 'function') return () => {};

  const original = getOriginalFetch() ?? globalThis.fetch;
  rememberOriginalFetch(original);
  const now = options.now ?? defaultNow;
  const endpointAbsolute = absolute(options.endpoint);

  const shouldSkip = (url: string): boolean => {
    if (url === endpointAbsolute) return true;
    return matchesIgnore(url, options.ignoreUrls);
  };

  const emit = (
    category: NetworkCategory,
    method: string,
    resolved: string,
    durationMs: number,
    status: number | undefined,
    aborted: boolean,
  ): void => {
    try {
      const url =
        sanitizeUrl(resolved, {
          ...(options.sensitiveQueryParams !== undefined
            ? { sensitiveQueryParams: options.sensitiveQueryParams }
            : {}),
          maxLength: BREADCRUMB_URL_MAX_LENGTH,
        }) ?? resolved.slice(0, BREADCRUMB_URL_MAX_LENGTH);
      record(
        createNetworkDiagnosticEvent({
          method,
          url,
          durationMs: Math.round(durationMs),
          category,
          ...(status !== undefined ? { status } : {}),
          ...(aborted ? { aborted: true } : {}),
        }),
      );
    } catch {
      // Observing a request must never affect it.
    }
  };

  const observeResponse = (response: Response, method: string, resolved: string, start: number): void => {
    let status: unknown;
    try {
      status = response.status;
    } catch {
      return;
    }
    if (typeof status !== 'number') return;
    const duration = now() - start;
    if (status === 0) {
      emit('opaque_response', method, resolved, duration, undefined, false);
      return;
    }
    if (status >= 400) {
      emit('http_error', method, resolved, duration, status, false);
    }
  };

  const observeError = (error: unknown, method: string, resolved: string, start: number): void => {
    const duration = now() - start;
    if (isAbortError(error)) {
      emit('aborted', method, resolved, duration, undefined, true);
    } else if (isTypeError(error)) {
      emit('network_error', method, resolved, duration, undefined, false);
    } else {
      emit('other_error', method, resolved, duration, undefined, false);
    }
  };

  const wrapped = function (
    this: unknown,
    input: FetchInput,
    init?: FetchInit,
  ): Promise<Response> {
    let raw: string | undefined;
    let skip = true;
    try {
      raw = inputUrl(input);
      if (raw !== undefined) skip = shouldSkip(absolute(raw));
    } catch {
      skip = true;
    }

    const promise = original.call(globalThis, input, init);
    if (skip || raw === undefined) return promise;

    const method = inputMethod(input, init);
    const resolved = absolute(raw);
    const start = now();
    try {
      return promise.then(
        (response: Response) => {
          observeResponse(response, method, resolved, start);
          return response;
        },
        (error: unknown) => {
          observeError(error, method, resolved, start);
          throw error;
        },
      );
    } catch {
      return promise;
    }
  };

  (wrapped as unknown as Record<PropertyKey, unknown>)[WRAPPED] = true;
  try {
    globalThis.fetch = wrapped as FetchFn;
  } catch {
    return () => {};
  }

  return () => {
    if (globalThis.fetch === (wrapped as FetchFn)) {
      try {
        globalThis.fetch = original;
      } catch {
        // ignore
      }
    }
  };
}
