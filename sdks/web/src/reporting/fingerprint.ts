/**
 * Error fingerprinting (Phase 7).
 *
 * A fingerprint is a small, deterministic, **non-cryptographic** hash that
 * identifies a *class* of error so repeats can be de-duplicated. It is built
 * from three things:
 *
 * 1. the error `type` (e.g. `TypeError`);
 * 2. the **normalized** message (whitespace collapsed, volatile tokens such as
 *    UUIDs / long numbers / hex ids / timestamps replaced, truncated, scrubbed);
 * 3. the **top application stack frame** — function name + file path only.
 *
 * Design decisions (see the phase report for the full rationale):
 * - Line and column are **excluded** from the frame on purpose: they change
 *   when unrelated code shifts a few lines up, which would fragment one bug
 *   into many fingerprints. The trade-off is that two distinct errors thrown
 *   from the same function with the same message collapse into one class.
 * - Query string and fragment are stripped from the frame path so cache-busting
 *   or per-navigation params do not change identity.
 * - A missing/undecipherable stack yields a weaker fingerprint (type + message
 *   only) and sets {@link FLAG_WEAK_FINGERPRINT}.
 * - A cross-origin script error (`"Script error."`) always yields a weak
 *   fingerprint and folds the **page URL path** into the identity, so the same
 *   bare message from two different pages is NOT merged.
 * - The message is scrubbed with the BASELINE scrubber before hashing, so a
 *   secret in a message can never influence (or appear in) a fingerprint.
 *
 * Every function here is pure and never throws.
 */
import { scrubText } from '../privacy/scrub';

/** Marks a fingerprint that was derived without a usable stack frame. */
export const FLAG_WEAK_FINGERPRINT = 'weak-fingerprint';

/** Max length of a message before it is hashed into a fingerprint. */
export const FINGERPRINT_MESSAGE_MAX_LENGTH = 200;

const FNV_OFFSET = 0xcbf29ce484222325n;
const FNV_PRIME = 0x100000001b3n;
const U64_MASK = 0xffffffffffffffffn;

/** FNV-1a (64-bit), rendered as 16 lowercase hex chars. Deterministic. */
function hash64(input: string): string {
  let hash = FNV_OFFSET;
  for (let i = 0; i < input.length; i += 1) {
    hash ^= BigInt(input.charCodeAt(i));
    hash = (hash * FNV_PRIME) & U64_MASK;
  }
  return hash.toString(16).padStart(16, '0');
}

const ISO_TIMESTAMP =
  /\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?/g;
const UUID =
  /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi;
const HEX_ID = /\b[0-9a-f]{8,}\b/gi;
const LONG_NUMBER = /\b\d{4,}\b/g;

function replaceVolatile(message: string): string {
  return message
    .replace(ISO_TIMESTAMP, '<timestamp>')
    .replace(UUID, '<uuid>')
    .replace(HEX_ID, '<hex>')
    .replace(LONG_NUMBER, '<number>');
}

/**
 * Normalizes a message for fingerprinting: collapse whitespace, replace
 * volatile tokens, scrub secrets, then truncate. Returns the text that is
 * hashed (and which is safe to expose as a debugging aid).
 */
export function normalizeMessage(message: string): string {
  let normalized = replaceVolatile(message.replace(/\s+/g, ' ').trim());
  normalized = scrubText(normalized);
  if (normalized.length > FINGERPRINT_MESSAGE_MAX_LENGTH) {
    normalized = normalized.slice(0, FINGERPRINT_MESSAGE_MAX_LENGTH);
  }
  return normalized;
}

interface ParsedFrame {
  readonly frame: string;
}

/** Removes a trailing `:line:col` / `:line`, then any query string / fragment. */
function cleanLocation(location: string): string {
  return location
    .replace(/:\d+:\d+$/, '')
    .replace(/:\d+$/, '')
    .replace(/[?#].*$/, '')
    .trim();
}

function parseFrameLine(line: string): ParsedFrame | null {
  let fn = '';
  let location = '';

  if (line.startsWith('at ')) {
    const rest = line.slice(3).trim();
    const withParens = /^(.+?)\s+\((.+)\)$/.exec(rest);
    if (withParens !== null) {
      fn = withParens[1]!.trim();
      location = withParens[2]!.trim();
    } else {
      location = rest;
    }
  } else if (line.includes('@')) {
    const at = line.indexOf('@');
    fn = line.slice(0, at).trim();
    location = line.slice(at + 1).trim();
  } else {
    return null;
  }

  fn = fn.replace(/^async\s+/, '').replace(/^new\s+/, '').trim();
  if (fn === '<anonymous>' || fn === 'anonymous') fn = '';

  location = cleanLocation(location);
  if (location.length === 0 || location === 'native' || location === '<anonymous>') {
    return null;
  }

  return { frame: fn.length > 0 ? `${fn}@${location}` : `@${location}` };
}

/**
 * Extracts the top usable frame from a stack string as `fn@path`, or `null`
 * when no frame can be recovered.
 */
export function topFrame(stack: string): ParsedFrame | null {
  for (const rawLine of stack.split('\n')) {
    const line = rawLine.trim();
    if (line.length === 0) continue;
    const parsed = parseFrameLine(line);
    if (parsed !== null) return parsed;
  }
  return null;
}

export interface FingerprintInput {
  readonly type: string;
  readonly message: string;
  /** Raw stack, when available. */
  readonly stack?: string;
  /** True for a cross-origin script error (always weak). */
  readonly crossOrigin: boolean;
  /** Page URL path, folded in only for cross-origin errors. */
  readonly pagePath: string;
}

export interface FingerprintResult {
  readonly fingerprint: string;
  /** True when the fingerprint was built without a reliable stack frame. */
  readonly weak: boolean;
  /** The normalized, scrubbed message that was hashed. */
  readonly normalizedMessage: string;
  /** The frame contribution (`fn@path`, `@path`, `@path:<page>`, or `''`). */
  readonly frame: string;
}

/**
 * Computes a fingerprint. Identical (type, normalized message, top frame)
 * triples produce identical fingerprints; changing any one changes the output.
 */
export function computeFingerprint(input: FingerprintInput): FingerprintResult {
  const normalizedMessage = normalizeMessage(input.message);

  let frame = '';
  let weak = input.crossOrigin;

  if (input.crossOrigin) {
    // Fold the page path in so the same "Script error." on two pages differs.
    frame = input.pagePath.length > 0 ? `page:${input.pagePath}` : '';
  } else if (input.stack !== undefined && input.stack.length > 0) {
    const parsed = topFrame(input.stack);
    if (parsed !== null) {
      frame = parsed.frame;
    } else {
      weak = true;
    }
  } else {
    weak = true;
  }

  const material = `${input.type}\u0000${normalizedMessage}\u0000${frame}`;
  return { fingerprint: hash64(material), weak, normalizedMessage, frame };
}
