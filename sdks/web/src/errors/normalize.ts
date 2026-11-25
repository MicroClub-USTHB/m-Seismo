/**
 * Shared error normalization (Phase 5.2).
 *
 * One function, {@link normalizeError}, is used by all three capture paths
 * (uncaught errors, unhandled rejections, `captureException()`) so every
 * diagnostic event carries the same error shape.
 *
 * Guarantees:
 * - NEVER throws. Throwing getters, proxies, circular references and exotic
 *   values are all handled.
 * - Message and stack are bounded ({@link MAX_MESSAGE_LENGTH},
 *   {@link MAX_STACK_LENGTH}); truncation sets the `truncated` flag.
 * - `Error.cause` is followed exactly one level.
 *
 * TODO Phase 6: value redaction. Messages and stacks are recorded verbatim
 * apart from truncation, so they may contain unredacted text.
 */
import { summarizeValue, truncateString, wasTruncated } from '../diagnostics/jsonSafe';
import {
  FLAG_STACK_MISSING,
  FLAG_TRUNCATED,
  MAX_MESSAGE_LENGTH,
  MAX_STACK_LENGTH,
} from '../diagnostics/types';
import type { DiagnosticCause, DiagnosticError } from '../diagnostics/types';

export interface NormalizedError {
  /** The normalized error shape for the event. */
  readonly error: DiagnosticError;
  /** Flags derived from normalization (`stack-missing`, `truncated`). */
  readonly flags: string[];
  /** True when the input was `Error`-like rather than a plain value. */
  readonly errorLike: boolean;
}

function addFlag(flags: string[], flag: string): void {
  if (!flags.includes(flag)) flags.push(flag);
}

/**
 * Returns the error type name when `value` is `Error`-like.
 *
 * Duck-typing (not only `instanceof`) so cross-realm errors, `DOMException`, and
 * error-like objects are recognized. A plain object is NOT error-like unless it
 * carries a string `stack` or both a string `name` and `message`.
 */
function errorTypeFrom(value: unknown): string | undefined {
  if (value === null || (typeof value !== 'object' && typeof value !== 'function')) {
    return undefined;
  }

  try {
    if (value instanceof Error) {
      const name = readString(value, 'name');
      return name !== undefined && name.length > 0 ? name : 'Error';
    }
  } catch {
    // `instanceof` on a hostile proxy can throw; fall through to duck-typing.
  }

  const name = readString(value, 'name');
  const message = readString(value, 'message');
  const stack = readString(value, 'stack');

  if (stack !== undefined) {
    return name !== undefined && name.length > 0 ? name : 'Error';
  }
  if (name !== undefined && message !== undefined) {
    return name.length > 0 ? name : 'Error';
  }
  return undefined;
}

function readString(value: unknown, key: string): string | undefined {
  try {
    const property = (value as Record<string, unknown>)[key];
    return typeof property === 'string' ? property : undefined;
  } catch {
    return undefined;
  }
}

/** The type label for a non-Error value (`'string'`, `'object'`, `'null'`, …). */
function nonErrorType(value: unknown): string {
  if (value === null) return 'null';
  if (Array.isArray(value)) return 'array';
  return typeof value;
}

/** Normalizes one level of `Error.cause`, or `undefined` when absent. */
function normalizeCause(value: unknown): DiagnosticCause | undefined {
  let cause: unknown;
  try {
    cause = (value as { cause?: unknown }).cause;
  } catch {
    return undefined;
  }
  if (cause === undefined || cause === null) return undefined;

  const errorLike = errorTypeFrom(cause);
  const type = errorLike ?? nonErrorType(cause);
  const raw = errorLike !== undefined ? (readString(cause, 'message') ?? '') : summarizeValue(cause);
  return { type, message: truncateString(raw, MAX_MESSAGE_LENGTH) };
}

/**
 * Normalizes any value into a {@link DiagnosticError} plus flags. Never throws.
 */
export function normalizeError(value: unknown): NormalizedError {
  const flags: string[] = [];
  const type = errorTypeFrom(value);

  if (type !== undefined) {
    const rawMessage = readString(value, 'message') ?? '';
    const rawStack = readString(value, 'stack');

    let message = rawMessage;
    if (wasTruncated(rawMessage, MAX_MESSAGE_LENGTH)) {
      message = truncateString(rawMessage, MAX_MESSAGE_LENGTH);
      addFlag(flags, FLAG_TRUNCATED);
    }

    let stack = rawStack;
    if (rawStack !== undefined && wasTruncated(rawStack, MAX_STACK_LENGTH)) {
      stack = truncateString(rawStack, MAX_STACK_LENGTH);
      addFlag(flags, FLAG_TRUNCATED);
    }
    if (stack === undefined) addFlag(flags, FLAG_STACK_MISSING);

    const cause = normalizeCause(value);

    const error: DiagnosticError = {
      type,
      message,
      ...(stack !== undefined ? { stack } : {}),
      ...(cause !== undefined ? { cause } : {}),
    };
    return { error, flags, errorLike: true };
  }

  const raw = summarizeValue(value);
  let message = raw;
  if (wasTruncated(raw, MAX_MESSAGE_LENGTH)) {
    message = truncateString(raw, MAX_MESSAGE_LENGTH);
    addFlag(flags, FLAG_TRUNCATED);
  }
  addFlag(flags, FLAG_STACK_MISSING);
  return { error: { type: nonErrorType(value), message }, flags, errorLike: false };
}
