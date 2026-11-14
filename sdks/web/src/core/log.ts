/**
 * Shared, predictable logging. The SDK logs at most a single warning per
 * problem and never throws. Keeping it in one place means every module uses the
 * same `[m-seismo]` prefix.
 */

/** Logs one `[m-seismo]`-prefixed warning. Never throws. */
export function warn(message: string): void {
  // eslint-disable-next-line no-console -- one predictable warning per problem.
  console.warn(`[m-seismo] ${message}`);
}
