/**
 * Conservative browser name/version detection.
 *
 * Strategy (in order):
 * 1. `navigator.userAgentData.brands` (synchronous, low-entropy) when present;
 * 2. an ordered User-Agent token list.
 *
 * The ordered list matters: Edge, Opera and Samsung Internet all contain
 * "Chrome" in their UA, and Chrome-based and iOS browsers contain "Safari".
 * Rules are checked most-specific first. If nothing confidently matches, the
 * field is omitted — we never guess from a partial match.
 *
 * Only the major version is reported; we do not claim more precision than the
 * UA reliably provides.
 */
import type { ContextEnvironment, UserAgentDataLike } from './env';

export interface BrowserInfo {
  readonly name?: string;
  readonly version?: string;
}

/** Brands we recognise, in priority order (most specific first). */
const BRAND_RULES: ReadonlyArray<{ readonly matches: RegExp; readonly name: string }> = [
  { matches: /^microsoft edge$/i, name: 'Edge' },
  { matches: /^opera$/i, name: 'Opera' },
  { matches: /^opera gx$/i, name: 'Opera' },
  { matches: /^samsung internet$/i, name: 'Samsung Internet' },
  { matches: /^google chrome$/i, name: 'Chrome' },
  { matches: /^firefox$/i, name: 'Firefox' },
];

/** True for Chrome's GREASE placeholder brands ("Not A Brand", "Not.A/Brand", …). */
function isGreaseBrand(brand: string): boolean {
  const normalized = brand.toLowerCase().replace(/[^a-z]/g, '');
  return normalized.startsWith('notabrand');
}

function major(version: string | undefined): string | undefined {
  if (typeof version !== 'string' || version.length === 0) return undefined;
  const first = version.split('.')[0];
  return first && first.length > 0 ? first : undefined;
}

function detectFromBrands(data: UserAgentDataLike | undefined): BrowserInfo | undefined {
  const brands = data?.brands;
  if (!Array.isArray(brands)) return undefined;
  const usable = brands.filter(
    (entry) => entry && typeof entry.brand === 'string' && !isGreaseBrand(entry.brand),
  );
  for (const rule of BRAND_RULES) {
    const match = usable.find((entry) => rule.matches.test(entry.brand.trim()));
    if (match) {
      const info: { name: string; version?: string } = { name: rule.name };
      const version = major(match.version);
      if (version !== undefined) info.version = version;
      return info;
    }
  }
  return undefined;
}

/** Ordered UA rules — most specific first. */
const UA_RULES: ReadonlyArray<{ readonly pattern: RegExp; readonly name: string }> = [
  { pattern: /Edg(?:iOS|A)?\/([\d.]+)/, name: 'Edge' },
  { pattern: /OPR\/([\d.]+)/, name: 'Opera' },
  { pattern: /Opera\/([\d.]+)/, name: 'Opera' },
  { pattern: /SamsungBrowser\/([\d.]+)/, name: 'Samsung Internet' },
  { pattern: /FxiOS\/([\d.]+)/, name: 'Firefox' },
  { pattern: /Firefox\/([\d.]+)/, name: 'Firefox' },
  { pattern: /CriOS\/([\d.]+)/, name: 'Chrome' },
  { pattern: /Chrome\/([\d.]+)/, name: 'Chrome' },
];

function detectFromUserAgent(userAgent: string | undefined): BrowserInfo | undefined {
  if (typeof userAgent !== 'string' || userAgent.length === 0) return undefined;
  for (const rule of UA_RULES) {
    const match = rule.pattern.exec(userAgent);
    if (match) {
      const info: { name: string; version?: string } = { name: rule.name };
      const version = major(match[1]);
      if (version !== undefined) info.version = version;
      return info;
    }
  }
  // Safari identifies itself via the Version/ token and the Safari/ token.
  if (/Safari\//.test(userAgent)) {
    const versionMatch = /Version\/([\d.]+)/.exec(userAgent);
    const info: { name: string; version?: string } = { name: 'Safari' };
    const version = major(versionMatch?.[1]);
    if (version !== undefined) info.version = version;
    return info;
  }
  return undefined;
}

/**
 * Detects the browser from an injected environment. Returns `undefined` when it
 * cannot be confidently determined (or when the UA claims no known browser).
 */
export function detectBrowser(env: ContextEnvironment): BrowserInfo | undefined {
  const fromBrands = detectFromBrands(env.navigator?.userAgentData);
  if (fromBrands) return fromBrands;
  return detectFromUserAgent(env.navigator?.userAgent);
}
