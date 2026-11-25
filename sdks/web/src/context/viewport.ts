/**
 * Viewport measurement.
 *
 * Reports `innerWidth` x `innerHeight` as integer pixels, but only when *both*
 * are finite, positive numbers. Anything else is omitted rather than clamped.
 */
import type { ContextEnvironment } from './env';

export interface Viewport {
  readonly width: number;
  readonly height: number;
}

/** Converts a dimension to an integer pixel count, or `undefined` if unusable. */
function toPixels(value: number | undefined): number | undefined {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return undefined;
  }
  return Math.round(value);
}

/** Detects the viewport from an injected environment, or `undefined`. */
export function detectViewport(env: ContextEnvironment): Viewport | undefined {
  const width = toPixels(env.window?.innerWidth);
  const height = toPixels(env.window?.innerHeight);
  if (width === undefined || height === undefined) return undefined;
  return { width, height };
}
