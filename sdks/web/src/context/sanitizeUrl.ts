/**
 * URL sanitizing for report context.
 *
 * A pure, never-throwing function that strips a URL down to something safe to
 * store server-side:
 * - the fragment (`#...`) is removed entirely;
 * - userinfo (`user:pass@`) is removed entirely;
 * - the values of sensitive-looking query parameters are replaced with
 *   `[REDACTED]` while the key name is kept;
 * - any query value that looks like a JWT or a long opaque secret is redacted
 *   regardless of its key name.
 *
 * This is deliberately conservative: it is a *reduction*, never a validation.
 * Everything it cannot confidently parse is dropped (`undefined`) rather than
 * passed through. See `report/phase-5-1-browser-and-app-context.md` for the exact
 * rules and the reasoning behind the key/value heuristics.
 *
 * No runtime dependency; no regex with catastrophic backtracking; never throws.
 */

/** Maximum length of the returned URL string. Longer URLs are truncated. */
export const MAX_URL_LENGTH = 2048;

/** Marker appended when a URL is truncated to {@link MAX_URL_LENGTH}. */
export const TRUNCATION_MARKER = '...[truncated]';

/**
 * Default query-parameter names whose value is always redacted. Matched
 * case-insensitively. Callers may extend this list via
 * `privacy.sensitiveQueryParams`.
 */
export const DEFAULT_SENSITIVE_QUERY_PARAMS: readonly string[] = [
  'token',
  'access_token',
  'refresh_token',
  'id_token',
  'auth',
  'authorization',
  'key',
  'api_key',
  'apikey',
  'secret',
  'password',
  'passwd',
  'pwd',
  'code',
  'session',
  'sessionid',
  'sid',
  'jwt',
  'signature',
  'sig',
  'otp',
  'email',
];

/**
 * Substrings that make a key sensitive when *contained* anywhere in the name.
 * Chosen because these tokens are almost never part of an unrelated word in a
 * query string (`authorization`, `refresh_token`, `client_secret`,
 * `user_password`, …).
 */
const SENSITIVE_SUBSTRINGS: readonly string[] = [
  'token',
  'secret',
  'password',
  'auth',
];

/**
 * Base used to parse protocol-relative / root-relative URLs without inventing a
 * real host. The host is stripped again before returning (relative inputs stay
 * relative).
 */
const RELATIVE_BASE = 'http://m-seismo.invalid/';

/** Schemes that are never safe to include in a report. */
const FORBIDDEN_PROTOCOLS = new Set(['javascript:', 'data:', 'blob:', 'vbscript:']);

export interface SanitizeUrlOptions {
  /** Extra query-parameter names to treat as sensitive (case-insensitive). */
  readonly sensitiveQueryParams?: readonly string[];
  /** Maximum returned length. Defaults to {@link MAX_URL_LENGTH}. */
  readonly maxLength?: number;
}

/** Formats a value for the sanitized query string. */
function toRedacted(): string {
  return '[REDACTED]';
}

/**
 * True when a *query value* looks like a bearer token / opaque secret,
 * independently of its key name:
 * - a three-part `header.payload.signature` JWT (base64url segments);
 * - a long (32+ chars) hex string;
 * - a long (32+ chars) base64url-ish string that mixes case or separators and
 *   contains at least one digit (to avoid redacting long ordinary words).
 */
export function looksLikeSecretValue(value: string): boolean {
  if (value.length < 20) return false;

  // JWT: three non-empty base64url segments separated by dots.
  if (/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(value)) {
    return true;
  }

  // Long hex string (hashes, hex tokens).
  if (/^[0-9a-fA-F]{32,}$/.test(value)) return true;

  // Long base64url-ish string with some evidence of being random (a digit and
  // either an uppercase letter or a separator).
  if (value.length >= 32 && /^[A-Za-z0-9_\-]+$/.test(value)) {
    const hasDigit = /[0-9]/.test(value);
    const hasUpperOrSep = /[A-Z]|[-_]/.test(value);
    if (hasDigit && hasUpperOrSep) return true;
  }

  return false;
}

/** Lower-cased sensitivity check for a single query key. */
export function isSensitiveKey(
  key: string,
  extra: readonly string[] | undefined,
): boolean {
  const lower = key.toLowerCase();
  if (lower.length === 0) return false;

  if (extra) {
    for (const name of extra) {
      if (typeof name === 'string' && name.toLowerCase() === lower) return true;
    }
  }
  for (const name of DEFAULT_SENSITIVE_QUERY_PARAMS) {
    if (name === lower) return true;
  }
  for (const fragment of SENSITIVE_SUBSTRINGS) {
    if (lower.includes(fragment)) return true;
  }
  // "key" is matched only as a whole token (`key`, `api_key`, `public-key`), so
  // ordinary words like "monkey", "keywords" or "hockey" are not redacted.
  // Underscores are word characters in JS regexes, so split into tokens instead
  // of relying on `\b`.
  if (lower.split(/[^a-z0-9]+/).includes('key')) return true;
  return false;
}

/** Rebuilds a query string from sanitized pairs, preserving key order. */
function buildQuery(
  params: URLSearchParams,
  extra: readonly string[] | undefined,
): string {
  const pairs: string[] = [];
  for (const [key, value] of params.entries()) {
    const sensitive = isSensitiveKey(key, extra) || looksLikeSecretValue(value);
    const encodedKey = encodeURIComponent(key);
    if (sensitive) {
      // The placeholder is intentionally left un-encoded so it is readable in
      // the stored report (and easy to assert on). See the phase report.
      pairs.push(`${encodedKey}=${toRedacted()}`);
    } else {
      pairs.push(`${encodedKey}=${encodeURIComponent(value)}`);
    }
  }
  return pairs.length > 0 ? `?${pairs.join('&')}` : '';
}

/** Truncates to `maxLength`, marking the cut, and never exceeds `maxLength`. */
function truncate(value: string, maxLength: number): string {
  if (maxLength <= 0) return value;
  if (value.length <= maxLength) return value;
  const keep = Math.max(0, maxLength - TRUNCATION_MARKER.length);
  return value.slice(0, keep) + TRUNCATION_MARKER;
}

/**
 * Sanitizes a URL. Returns `undefined` for inputs that cannot be parsed safely
 * (invalid strings, forbidden schemes) instead of throwing or leaking the raw
 * value.
 *
 * @param input the URL to sanitize (absolute or relative).
 * @param options optional extra sensitive keys and max length.
 */
export function sanitizeUrl(
  input: unknown,
  options: SanitizeUrlOptions = {},
): string | undefined {
  if (typeof input !== 'string' || input.length === 0) return undefined;

  const trimmed = input.trim();
  if (trimmed.length === 0) return undefined;

  // Raw whitespace never appears in a browser-reported URL (it is percent-encoded
  // there). Rejecting it stops free text like "not a url" being kept as a path.
  if (/\s/.test(trimmed)) return undefined;

  let parsed: URL;
  let relative = false;
  try {
    parsed = new URL(trimmed);
  } catch {
    // Not absolute: try as a relative URL against a dummy base.
    try {
      parsed = new URL(trimmed, RELATIVE_BASE);
      relative = true;
    } catch {
      return undefined;
    }
  }

  if (FORBIDDEN_PROTOCOLS.has(parsed.protocol.toLowerCase())) return undefined;

  const maxLength = options.maxLength ?? MAX_URL_LENGTH;
  const query = buildQuery(parsed.searchParams, options.sensitiveQueryParams);
  const base = relative ? parsed.pathname : parsed.origin + parsed.pathname;
  return truncate(base + query, maxLength);
}
