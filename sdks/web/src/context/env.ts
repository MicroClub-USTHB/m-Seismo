/**
 * Injected, read-only view of the browser environment.
 *
 * Context collectors never read `window`/`navigator`/`location` directly. They
 * receive a small structural interface so they are trivially testable without a
 * real browser, and so nothing can accidentally mutate host state.
 *
 * The SDK only ever *reads* these objects. It never writes to `navigator`,
 * `location`, `history`, storage or cookies.
 */

/** The synchronous, low-entropy subset of `navigator.userAgentData` we use. */
export interface UserAgentDataLike {
  readonly brands?: ReadonlyArray<{ readonly brand: string; readonly version: string }>;
  readonly platform?: string;
  // NOTE: `getHighEntropyValues` is intentionally NOT used.
}

/** Read-only subset of `Navigator` used for context. */
export interface NavigatorLike {
  readonly userAgent?: string;
  readonly platform?: string;
  readonly userAgentData?: UserAgentDataLike;
}

/** Read-only subset of `Location`. */
export interface LocationLike {
  readonly href?: string;
}

/** Read-only subset of `Window` used for viewport measurement. */
export interface WindowLike {
  readonly innerWidth?: number;
  readonly innerHeight?: number;
}

/** Everything a collector may read from. All fields optional. */
export interface ContextEnvironment {
  readonly navigator?: NavigatorLike | null;
  readonly location?: LocationLike | null;
  readonly window?: WindowLike | null;
  readonly now?: Date;
}

/**
 * Reads the ambient browser globals, if any. Returns `{}` in a non-browser
 * (SSR/Node) environment. Read-only: no property is written.
 */
export function readGlobalEnvironment(): ContextEnvironment {
  const env: {
    navigator?: NavigatorLike;
    location?: LocationLike;
    window?: WindowLike;
  } = {};

  const g = globalThis as {
    navigator?: NavigatorLike;
    location?: LocationLike;
    window?: WindowLike;
  };

  if (typeof g.navigator !== 'undefined' && g.navigator !== null) {
    env.navigator = g.navigator;
  }
  if (typeof g.location !== 'undefined' && g.location !== null) {
    env.location = g.location;
  }
  if (typeof g.window !== 'undefined' && g.window !== null) {
    env.window = g.window;
  }
  return env;
}
