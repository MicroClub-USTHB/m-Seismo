import { describe, expect, it } from 'vitest';
import {
  isDomNode,
  readStringProp,
  sanitizeContext,
  summarizeValue,
  toJsonValue,
  truncateString,
  wasTruncated,
} from '@sdk/diagnostics/jsonSafe';
import {
  CONTEXT_MAX_DEPTH,
  CONTEXT_MAX_KEYS,
  CONTEXT_MAX_STRING_LENGTH,
  MAX_MESSAGE_LENGTH,
  TRUNCATION_MARKER,
} from '@sdk/diagnostics/types';

describe('truncateString', () => {
  it('returns short strings unchanged', () => {
    expect(truncateString('hello', 10)).toBe('hello');
    expect(wasTruncated('hello', 10)).toBe(false);
  });

  it('truncates and appends the marker', () => {
    const out = truncateString('abcdefghijklmnopqrstuvwxyz', 15);
    expect(out).toBe(`abc${TRUNCATION_MARKER}`);
    expect(out.length).toBe(15);
    expect(wasTruncated('abcdefghijklmnopqrstuvwxyz', 15)).toBe(true);
  });

  it('handles a max shorter than the marker', () => {
    expect(truncateString('abcdefghij', 5)).toBe('abcde');
    expect(wasTruncated('abcdefghij', 5)).toBe(true);
  });

  it('uses MAX_MESSAGE_LENGTH by default', () => {
    const long = 'x'.repeat(MAX_MESSAGE_LENGTH + 10);
    expect(truncateString(long)).toBe(truncateString(long, MAX_MESSAGE_LENGTH));
  });
});

describe('readStringProp', () => {
  it('reads a string property', () => {
    expect(readStringProp({ a: 'v' }, 'a')).toBe('v');
  });

  it('returns undefined for non-strings and primitives', () => {
    expect(readStringProp({ a: 1 }, 'a')).toBeUndefined();
    expect(readStringProp(null, 'a')).toBeUndefined();
    expect(readStringProp('str', 'length')).toBeUndefined();
  });

  it('does not throw on a throwing getter', () => {
    const obj = Object.defineProperty({}, 'a', {
      get() {
        throw new Error('nope');
      },
    });
    expect(readStringProp(obj, 'a')).toBeUndefined();
  });
});

describe('isDomNode', () => {
  it('detects node-like objects', () => {
    expect(isDomNode({ nodeType: 1, nodeName: 'DIV' })).toBe(true);
    expect(isDomNode({})).toBe(false);
    expect(isDomNode(null)).toBe(false);
    expect(isDomNode('x')).toBe(false);
  });
});

describe('summarizeValue', () => {
  it('summarizes primitives', () => {
    expect(summarizeValue('hi')).toBe('hi');
    expect(summarizeValue(42)).toBe('42');
    expect(summarizeValue(true)).toBe('true');
    expect(summarizeValue(null)).toBe('null');
    expect(summarizeValue(undefined)).toBe('undefined');
    expect(summarizeValue(Symbol('s'))).toBe('Symbol(s)');
  });

  it('labels functions', () => {
    function named() {}
    expect(summarizeValue(named)).toBe('[Function: named]');
  });

  it('prefers a string message on objects', () => {
    expect(summarizeValue({ message: 'boom', other: 1 })).toBe('boom');
  });

  it('serializes plain objects', () => {
    expect(summarizeValue({ a: 1, b: [2, 3] })).toBe('{"a":1,"b":[2,3]}');
  });

  it('handles circular references', () => {
    const obj: Record<string, unknown> = { a: 1 };
    obj.self = obj;
    expect(() => summarizeValue(obj)).not.toThrow();
    expect(summarizeValue(obj)).toContain('[circular]');
  });
});

describe('toJsonValue', () => {
  it('drops functions and symbols', () => {
    const fn = () => undefined;
    const out = toJsonValue({ fn, sym: Symbol('x') }, 0, new WeakSet());
    expect(out).toEqual({});
  });

  it('bounds depth', () => {
    let deep: Record<string, unknown> = { v: 'leaf' };
    for (let i = 0; i < CONTEXT_MAX_DEPTH + 3; i += 1) deep = { next: deep };
    const out = toJsonValue(deep, 0, new WeakSet());
    expect(JSON.stringify(out)).toContain('[max-depth]');
  });
});

describe('sanitizeContext', () => {
  it('returns {} for non-objects', () => {
    expect(sanitizeContext(null)).toEqual({});
    expect(sanitizeContext('x')).toEqual({});
    expect(sanitizeContext([1, 2])).toEqual({});
  });

  it('bounds the number of keys', () => {
    const input: Record<string, number> = {};
    for (let i = 0; i < CONTEXT_MAX_KEYS + 10; i += 1) input[`k${i}`] = i;
    expect(Object.keys(sanitizeContext(input)).length).toBe(CONTEXT_MAX_KEYS);
  });

  it('truncates long string values', () => {
    const out = sanitizeContext({ s: 'x'.repeat(CONTEXT_MAX_STRING_LENGTH + 50) });
    expect(String(out.s).endsWith(TRUNCATION_MARKER)).toBe(true);
  });

  it('does not mutate the caller and handles throwing getters', () => {
    const input: Record<string, unknown> = { a: 1 };
    Object.defineProperty(input, 'bad', {
      enumerable: true,
      get() {
        throw new Error('nope');
      },
    });
    const out = sanitizeContext(input);
    expect(out.a).toBe(1);
    expect(out.bad).toBe('[unserializable]');
    expect(input.a).toBe(1);
  });
});
