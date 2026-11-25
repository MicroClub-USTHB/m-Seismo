/**
 * JSON-safe, bounded value conversion (Phase 5.2).
 *
 * Shared by error normalization (to summarize non-Error values) and by
 * `captureException()` (to sanitize caller-supplied context). Every function is
 * defensive: throwing getters, circular references, DOM nodes, functions and
 * symbols are handled without ever throwing.
 *
 * TODO Phase 6: value redaction (tokens/PII inside strings) belongs here once
 * the privacy phase exists. Until then, output may contain unredacted text.
 */
import {
  CONTEXT_MAX_DEPTH,
  CONTEXT_MAX_KEYS,
  CONTEXT_MAX_STRING_LENGTH,
  MAX_MESSAGE_LENGTH,
  TRUNCATION_MARKER,
} from './types';
import type { JsonValue } from './types';

/** Sentinel returned by {@link toJsonValue} for values that must be dropped. */
const DROP = Symbol('drop');

/** Truncates a string to `max`, appending a visible marker when cut. */
export function truncateString(value: string, max: number = MAX_MESSAGE_LENGTH): string {
  if (max <= 0 || value.length <= max) return value;
  const keep = max - TRUNCATION_MARKER.length;
  if (keep <= 0) return value.slice(0, max);
  return value.slice(0, keep) + TRUNCATION_MARKER;
}

/** True when a string was actually shortened by {@link truncateString}. */
export function wasTruncated(value: string, max: number): boolean {
  return max > 0 && value.length > max;
}

/**
 * Reads a string property without throwing. A throwing getter or a non-string
 * value yields `undefined`.
 */
export function readStringProp(obj: unknown, key: string): string | undefined {
  if (obj === null || (typeof obj !== 'object' && typeof obj !== 'function')) {
    return undefined;
  }
  try {
    const value = (obj as Record<string, unknown>)[key];
    return typeof value === 'string' ? value : undefined;
  } catch {
    return undefined;
  }
}

/** Duck-typed DOM node test that never throws. */
export function isDomNode(value: unknown): boolean {
  if (value === null || typeof value !== 'object') return false;
  try {
    const node = value as { nodeType?: unknown; nodeName?: unknown };
    return typeof node.nodeType === 'number' && typeof node.nodeName === 'string';
  } catch {
    return false;
  }
}

function safePrimitive(value: unknown): string {
  try {
    return String(value);
  } catch {
    return '[unserializable]';
  }
}

function safeSymbol(value: symbol): string {
  try {
    return value.toString();
  } catch {
    return '[symbol]';
  }
}

function safeFunctionLabel(value: (...args: unknown[]) => unknown): string {
  try {
    const name = value.name;
    return typeof name === 'string' && name.length > 0 ? `[Function: ${name}]` : '[Function]';
  } catch {
    return '[Function]';
  }
}

/**
 * Converts an arbitrary value into a JSON-safe, depth- and size-bounded value.
 * Returns the internal `DROP` sentinel for values that must not appear
 * (functions, symbols, DOM nodes).
 */
export function toJsonValue(
  value: unknown,
  depth: number,
  seen: WeakSet<object>,
): JsonValue | typeof DROP {
  if (value === null) return null;

  switch (typeof value) {
    case 'string':
      return truncateString(value, CONTEXT_MAX_STRING_LENGTH);
    case 'number':
      return Number.isFinite(value) ? value : safePrimitive(value);
    case 'boolean':
      return value;
    case 'bigint':
      return safePrimitive(value);
    case 'undefined':
      return '[undefined]';
    case 'symbol':
      return DROP;
    case 'function':
      return DROP;
    default:
      break;
  }

  if (isDomNode(value)) return DROP;

  const objectValue = value as object;
  if (seen.has(objectValue)) return '[circular]';
  if (depth >= CONTEXT_MAX_DEPTH) return '[max-depth]';

  seen.add(objectValue);
  try {
    if (Array.isArray(value)) {
      const out: JsonValue[] = [];
      const length = Math.min(value.length, CONTEXT_MAX_KEYS);
      for (let i = 0; i < length; i += 1) {
        let item: unknown;
        try {
          item = value[i];
        } catch {
          out.push('[unserializable]');
          continue;
        }
        const converted = toJsonValue(item, depth + 1, seen);
        out.push(converted === DROP ? null : converted);
      }
      if (value.length > length) out.push('[truncated]');
      return out;
    }

    const out: Record<string, JsonValue> = {};
    let keys: string[];
    try {
      keys = Object.keys(objectValue);
    } catch {
      return '[unserializable]';
    }
    const limit = Math.min(keys.length, CONTEXT_MAX_KEYS);
    for (let i = 0; i < limit; i += 1) {
      const key = keys[i]!;
      let property: unknown;
      try {
        property = (objectValue as Record<string, unknown>)[key];
      } catch {
        out[key] = '[unserializable]';
        continue;
      }
      const converted = toJsonValue(property, depth + 1, seen);
      if (converted !== DROP) out[key] = converted;
    }
    return out;
  } finally {
    seen.delete(objectValue);
  }
}

/**
 * A short, human-readable summary of an arbitrary value, bounded to
 * {@link MAX_MESSAGE_LENGTH}. Used as the `message` for non-`Error` values.
 */
export function summarizeValue(value: unknown): string {
  if (value === null) return 'null';

  switch (typeof value) {
    case 'string':
      return truncateString(value, MAX_MESSAGE_LENGTH);
    case 'number':
    case 'boolean':
    case 'bigint':
      return safePrimitive(value);
    case 'undefined':
      return 'undefined';
    case 'symbol':
      return safeSymbol(value);
    case 'function':
      return safeFunctionLabel(value as (...args: unknown[]) => unknown);
    default:
      break;
  }

  // Prefer a string `message` (plain objects and error-likes often carry one).
  const message = readStringProp(value, 'message');
  if (message !== undefined && message.length > 0) {
    return truncateString(message, MAX_MESSAGE_LENGTH);
  }

  const converted = toJsonValue(value, 0, new WeakSet<object>());
  if (converted === DROP) return '[object]';
  try {
    const json = JSON.stringify(converted);
    return truncateString(json ?? safePrimitive(value), MAX_MESSAGE_LENGTH);
  } catch {
    return '[unserializable]';
  }
}

/**
 * Structurally sanitizes a `captureException()` context object: at most
 * {@link CONTEXT_MAX_KEYS} keys, depth {@link CONTEXT_MAX_DEPTH}, strings
 * truncated to {@link CONTEXT_MAX_STRING_LENGTH}, functions/symbols/DOM nodes
 * dropped, circular references and throwing getters replaced by placeholders.
 * The caller's object is never mutated.
 */
export function sanitizeContext(input: unknown): Record<string, JsonValue> {
  const out: Record<string, JsonValue> = {};
  if (input === null || typeof input !== 'object' || Array.isArray(input)) return out;

  let keys: string[];
  try {
    keys = Object.keys(input as object);
  } catch {
    return out;
  }

  const seen = new WeakSet<object>();
  seen.add(input as object);

  const limit = Math.min(keys.length, CONTEXT_MAX_KEYS);
  for (let i = 0; i < limit; i += 1) {
    const key = keys[i]!;
    let value: unknown;
    try {
      value = (input as Record<string, unknown>)[key];
    } catch {
      out[key] = '[unserializable]';
      continue;
    }
    const converted = toJsonValue(value, 1, seen);
    if (converted !== DROP) out[key] = converted;
  }
  return out;
}
