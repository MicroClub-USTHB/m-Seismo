/**
 * Error normalization against the "nasty reasons" list: null prototypes,
 * symbols, functions, circular references, throwing getters and hostile proxies.
 * Every case must produce a normalized error without throwing.
 */
import { describe, expect, it } from 'vitest';
import { normalizeError } from '@sdk/errors/normalize';

describe('normalizeError: hostile values never throw', () => {
  it('handles a null-prototype object', () => {
    const value = Object.create(null) as Record<string, unknown>;
    value['name'] = 'CustomError';
    value['message'] = 'from a namespace';
    const { error, errorLike } = normalizeError(value);
    expect(errorLike).toBe(true);
    expect(error.type).toBe('CustomError');
    expect(error.message).toBe('from a namespace');
  });

  it('handles a Symbol value', () => {
    const { error, errorLike } = normalizeError(Symbol('s'));
    expect(errorLike).toBe(false);
    expect(error.type).toBe('symbol');
    expect(typeof error.message).toBe('string');
  });

  it('handles a function value', () => {
    const { error } = normalizeError(() => 42);
    expect(error.type).toBe('function');
  });

  it('handles a throwing message getter', () => {
    const hostile = {
      name: 'Error',
      get message(): string {
        throw new Error('nope');
      },
      stack: 'at x',
    };
    const { error } = normalizeError(hostile);
    expect(error.type).toBe('Error');
    expect(error.message).toBe('');
  });

  it('handles a circular error-like object', () => {
    const value: Record<string, unknown> = { name: 'Error', message: 'loop' };
    value['self'] = value;
    const { error } = normalizeError(value);
    expect(error.type).toBe('Error');
    expect(error.message).toBe('loop');
  });

  it('handles a hostile Proxy that throws on every get', () => {
    const proxy = new Proxy(
      {},
      {
        get() {
          throw new Error('blocked');
        },
        getPrototypeOf() {
          throw new Error('blocked');
        },
      },
    );
    expect(() => normalizeError(proxy)).not.toThrow();
  });

  it('handles a cause whose getter throws', () => {
    const value = {
      name: 'Error',
      message: 'outer',
      get cause(): unknown {
        throw new Error('blocked');
      },
    };
    const { error } = normalizeError(value);
    expect(error.type).toBe('Error');
    expect(error.cause).toBeUndefined();
  });

  it('handles an array value', () => {
    const { error } = normalizeError([1, 2, 3]);
    expect(error.type).toBe('array');
  });
});
