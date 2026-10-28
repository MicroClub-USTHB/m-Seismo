/**
 * Navigation collector (Phase 5.3).
 *
 * Records route changes as `navigation` breadcrumbs from three sources:
 * - wrapping `history.pushState` / `history.replaceState` (one wrapper each,
 *   symbol-marked, restoring the originals on dispose);
 * - `popstate` and `hashchange` listeners on the host window;
 * - one initial `load` breadcrumb when the collector is installed.
 *
 * URLs are reduced with the Phase 5.1 `sanitizeUrl` (fragment, userinfo and
 * sensitive query values removed) before being recorded, so a token in the URL
 * never reaches the buffer.
 *
 * The wrappers forward `this` and all arguments unchanged, return the same
 * value, and rethrow the same error, so host behaviour is identical with and
 * without the SDK. The collector never throws into the host.
 */
import { sanitizeUrl } from '../context/sanitizeUrl';
import { createNavigationDiagnosticEvent } from '../diagnostics/event';
import type { DiagnosticEvent, NavigationTrigger } from '../diagnostics/types';

const WRAPPED = Symbol('m-seismo.instrumented-history');

interface HistoryLike {
  pushState: (data: unknown, unused: string, url?: string | URL | null) => void;
  replaceState: (data: unknown, unused: string, url?: string | URL | null) => void;
}

export interface NavigationCollectorOptions {
  /** Extra sensitive query params to redact from recorded URLs. */
  readonly sensitiveQueryParams?: readonly string[];
}

function currentHref(host: Window): string | undefined {
  try {
    const href = host.location?.href;
    return typeof href === 'string' && href.length > 0 ? href : undefined;
  } catch {
    return undefined;
  }
}

function isWrapped(fn: unknown): boolean {
  return (
    typeof fn === 'function' &&
    (fn as unknown as Record<PropertyKey, unknown>)[WRAPPED] === true
  );
}

/**
 * Installs the collector on `host` (the window). Returns a disposer that
 * restores the history methods and removes the listeners.
 */
export function installNavigationCollector(
  host: Window,
  record: (event: DiagnosticEvent) => void,
  options: NavigationCollectorOptions = {},
): () => void {
  const urlOptions =
    options.sensitiveQueryParams !== undefined
      ? { sensitiveQueryParams: options.sensitiveQueryParams }
      : {};

  let lastUrl = currentHref(host);

  const sanitize = (value: string | undefined): string | undefined => {
    if (value === undefined) return undefined;
    try {
      return sanitizeUrl(value, urlOptions);
    } catch {
      return undefined;
    }
  };

  const recordNav = (trigger: NavigationTrigger, from: string | undefined): void => {
    try {
      const toRaw = currentHref(host);
      const to = sanitize(toRaw);
      if (to === undefined) return;
      const fromSanitized = sanitize(from);
      const data =
        fromSanitized !== undefined && fromSanitized !== to
          ? { trigger, to, from: fromSanitized }
          : { trigger, to };
      record(createNavigationDiagnosticEvent(data));
      if (toRaw !== undefined) lastUrl = toRaw;
    } catch {
      // Observing navigation must never affect it.
    }
  };

  // Initial load breadcrumb.
  recordNav('load', undefined);

  const disposers: Array<() => void> = [];

  let history: HistoryLike | undefined;
  try {
    history = host.history as unknown as HistoryLike | undefined;
  } catch {
    history = undefined;
  }

  if (history !== undefined) {
    const originalPush = history.pushState;
    const originalReplace = history.replaceState;

    if (!isWrapped(originalPush)) {
      const wrappedPush = function (
        this: History,
        data: unknown,
        unused: string,
        url?: string | URL | null,
      ): void {
        const from = lastUrl;
        originalPush.call(this, data, unused, url);
        recordNav('pushState', from);
      };
      (wrappedPush as unknown as Record<PropertyKey, unknown>)[WRAPPED] = true;
      history.pushState = wrappedPush as unknown as HistoryLike['pushState'];
      disposers.push(() => {
        if (history!.pushState === (wrappedPush as unknown as HistoryLike['pushState'])) {
          history!.pushState = originalPush;
        }
      });
    }

    if (!isWrapped(originalReplace)) {
      const wrappedReplace = function (
        this: History,
        data: unknown,
        unused: string,
        url?: string | URL | null,
      ): void {
        const from = lastUrl;
        originalReplace.call(this, data, unused, url);
        recordNav('replaceState', from);
      };
      (wrappedReplace as unknown as Record<PropertyKey, unknown>)[WRAPPED] = true;
      history.replaceState = wrappedReplace as unknown as HistoryLike['replaceState'];
      disposers.push(() => {
        if (history!.replaceState === (wrappedReplace as unknown as HistoryLike['replaceState'])) {
          history!.replaceState = originalReplace;
        }
      });
    }
  }

  const onPopState = (): void => recordNav('popstate', lastUrl);
  const onHashChange = (): void => recordNav('hashchange', lastUrl);

  try {
    host.addEventListener('popstate', onPopState);
    host.addEventListener('hashchange', onHashChange);
  } catch {
    // ignore
  }
  disposers.push(() => {
    try {
      host.removeEventListener('popstate', onPopState);
      host.removeEventListener('hashchange', onHashChange);
    } catch {
      // ignore
    }
  });

  return () => {
    for (const dispose of disposers.reverse()) {
      try {
        dispose();
      } catch {
        // ignore
      }
    }
  };
}
