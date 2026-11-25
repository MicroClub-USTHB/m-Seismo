/**
 * Secret scanner used by the viewer's "Redaction check" panel. It only reports
 * what is in the payload; it never edits it.
 */
import { describe, expect, it } from 'vitest';
import { scanForSecrets } from '@app/viewer/secretScan';

describe('scanForSecrets', () => {
  it('finds a secret embedded in a string and reports its path', () => {
    const result = scanForSecrets({ description: 'token super-secret-1 here' }, ['super-secret-1']);
    expect(result.found).toHaveLength(1);
    expect(result.found[0]!.path).toBe('description');
    expect(result.clean).toEqual([]);
  });

  it('reports a secret as clean when it is absent', () => {
    const result = scanForSecrets({ description: 'nothing' }, ['super-secret-1']);
    expect(result.found).toEqual([]);
    expect(result.clean).toEqual(['super-secret-1']);
  });

  it('descends arrays and nested objects, counting distinct paths', () => {
    const result = scanForSecrets(
      { a: ['x-secret', { b: 'x-secret' }] },
      ['x-secret'],
    );
    expect(result.found[0]!.count).toBe(2);
    expect(result.found[0]!.path).toBe('a[0]');
  });

  it('ignores empty candidate secrets', () => {
    const result = scanForSecrets({ x: 'anything' }, ['', 'yes']);
    expect(result.clean).toEqual(['yes']);
    expect(result.found).toEqual([]);
  });

  it('counts [REDACTED] markers in the serialized report', () => {
    const result = scanForSecrets({ a: '[REDACTED]', b: 'x [REDACTED] y' }, []);
    expect(result.redactionMarkers).toBe(2);
  });

  it('deduplicates repeated candidate secrets', () => {
    const result = scanForSecrets({ a: 'dup-secret' }, ['dup-secret', 'dup-secret']);
    expect(result.found).toHaveLength(1);
  });
});
