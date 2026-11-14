import { describe, expect, it } from 'vitest';
import { normalizeError } from '@sdk/errors/normalize';
import {
  FLAG_STACK_MISSING,
  FLAG_TRUNCATED,
  MAX_STACK_LENGTH,
} from '@sdk/diagnostics/types';

describe('normalizeError — Error-like values', () => {
  it('captures type, message and stack', () => {
    const { error, flags, errorLike } = normalizeError(new TypeError('bad thing'));
    expect(errorLike).toBe(true);
    expect(error.type).toBe('TypeError');
    expect(error.message).toBe('bad thing');
    expect(typeof error.stack).toBe('string');
    expect(flags).not.toContain(FLAG_STACK_MISSING);
  });

  it('sets stack-missing when there is no stack', () => {
    const err = new Error('x');
    delete (err as { stack?: string }).stack;
    const { flags } = normalizeError(err);
    expect(flags).toContain(FLAG_STACK_MISSING);
  });

  it('truncates a long stack and flags it', () => {
    const err = new Error('x');
    err.stack = 's'.repeat(MAX_STACK_LENGTH + 100);
    const { error, flags } = normalizeError(err);
    expect(error.stack!.length).toBeLessThanOrEqual(MAX_STACK_LENGTH);
    expect(flags).toContain(FLAG_TRUNCATED);
  });

  it('follows cause exactly one level', () => {
    const root = new Error('root');
    const mid = Object.assign(new Error('mid'), { cause: root });
    const top = Object.assign(new Error('top'), { cause: mid });
    const { error } = normalizeError(top);
    expect(error.cause).toEqual({ type: 'Error', message: 'mid' });
  });

  it('normalizes a non-Error cause via summary', () => {
    const err = Object.assign(new Error('top'), { cause: 'a string' });
    const { error } = normalizeError(err);
    expect(error.cause).toEqual({ type: 'string', message: 'a string' });
  });

  it('recognizes a cross-realm-like error object', () => {
    const fake = { name: 'CustomError', message: 'from another realm', stack: 'at x' };
    const { error, errorLike } = normalizeError(fake);
    expect(errorLike).toBe(true);
    expect(error.type).toBe('CustomError');
    expect(error.message).toBe('from another realm');
  });

  it('does not throw on a throwing message getter', () => {
    const obj = Object.defineProperty({ name: 'E' }, 'message', {
      get() {
        throw new Error('nope');
      },
    });
    expect(() => normalizeError(obj)).not.toThrow();
  });
});

describe('normalizeError — non-Error values', () => {
  it.each([
    ['string', 'boom', 'string'],
    ['number', 42, 'number'],
    ['boolean', false, 'boolean'],
    ['null', null, 'null'],
    ['undefined', undefined, 'undefined'],
    ['array', [1, 2], 'array'],
    ['object', { a: 1 }, 'object'],
  ])('normalizes %s without throwing', (_label, value, expectedType) => {
    const { error, flags, errorLike } = normalizeError(value);
    expect(errorLike).toBe(false);
    expect(error.type).toBe(expectedType);
    expect(typeof error.message).toBe('string');
    expect(flags).toContain(FLAG_STACK_MISSING);
  });

  it('summarizes a plain object message', () => {
    expect(normalizeError({ message: 'plain boom' }).error.message).toBe('plain boom');
  });
});
