/**
 * Conservative platform / OS detection.
 *
 * Normalizes to one of: `Windows`, `macOS`, `Linux`, `Android`, `iOS`,
 * `ChromeOS`. Anything else (or ambiguous) is omitted.
 *
 * Order matters: Android and ChromeOS UAs contain "Linux"; iOS UAs contain
 * "like Mac OS X". We check the more specific tokens first.
 */
import type { ContextEnvironment } from './env';

export type Platform =
  | 'Windows'
  | 'macOS'
  | 'Linux'
  | 'Android'
  | 'iOS'
  | 'ChromeOS';

/** Normalizes the `userAgentData.platform` string to a known platform. */
function normalizePlatformName(value: string | undefined): Platform | undefined {
  if (typeof value !== 'string') return undefined;
  const normalized = value.trim().toLowerCase();
  switch (normalized) {
    case 'windows':
      return 'Windows';
    case 'macos':
    case 'mac os x':
    case 'macintosh':
      return 'macOS';
    case 'chrome os':
    case 'chromeos':
    case 'cros':
      return 'ChromeOS';
    case 'android':
      return 'Android';
    case 'ios':
      return 'iOS';
    case 'linux':
      return 'Linux';
    default:
      return undefined;
  }
}

function detectFromUserAgent(userAgent: string | undefined): Platform | undefined {
  if (typeof userAgent !== 'string' || userAgent.length === 0) return undefined;
  if (/Android/i.test(userAgent)) return 'Android';
  if (/iPhone|iPad|iPod/i.test(userAgent)) return 'iOS';
  if (/CrOS/i.test(userAgent)) return 'ChromeOS';
  if (/Windows/i.test(userAgent)) return 'Windows';
  if (/Macintosh|Mac OS X/i.test(userAgent)) return 'macOS';
  if (/Linux/i.test(userAgent)) return 'Linux';
  return undefined;
}

function detectFromNavigatorPlatform(platform: string | undefined): Platform | undefined {
  if (typeof platform !== 'string' || platform.length === 0) return undefined;
  if (/^(Win32|Win64|WinCE|Windows)/i.test(platform)) return 'Windows';
  if (/^(MacIntel|MacPPC|Mac68K|Macintosh)/i.test(platform)) return 'macOS';
  if (/^(iPhone|iPad|iPod)/i.test(platform)) return 'iOS';
  if (/^Android/i.test(platform)) return 'Android';
  if (/^CrOS/i.test(platform)) return 'ChromeOS';
  if (/^Linux/i.test(platform)) return 'Linux';
  return undefined;
}

/** Detects the platform from an injected environment, or `undefined`. */
export function detectPlatform(env: ContextEnvironment): Platform | undefined {
  const navigator = env.navigator;
  const fromUaData = normalizePlatformName(navigator?.userAgentData?.platform);
  if (fromUaData !== undefined) return fromUaData;

  const fromUa = detectFromUserAgent(navigator?.userAgent);
  if (fromUa !== undefined) return fromUa;

  return detectFromNavigatorPlatform(navigator?.platform);
}
