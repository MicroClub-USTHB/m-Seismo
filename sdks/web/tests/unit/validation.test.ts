import { describe, expect, it } from 'vitest';
import { MAX_DESCRIPTION_LENGTH, validateDescription } from '@sdk/form/validation';

describe('validateDescription', () => {
  it('rejects an empty string', () => {
    const result = validateDescription('');
    expect(result.ok).toBe(false);
  });

  it('rejects whitespace-only input', () => {
    const result = validateDescription('   \n\t  ');
    expect(result.ok).toBe(false);
  });

  it('trims and accepts a normal description', () => {
    const result = validateDescription('  the save button did nothing  ');
    expect(result).toEqual({ ok: true, value: 'the save button did nothing' });
  });

  it('accepts a description at exactly the maximum length', () => {
    const value = 'x'.repeat(MAX_DESCRIPTION_LENGTH);
    const result = validateDescription(value);
    expect(result.ok).toBe(true);
  });

  it('rejects a description longer than the maximum length', () => {
    const value = 'x'.repeat(MAX_DESCRIPTION_LENGTH + 1);
    const result = validateDescription(value);
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.message).toContain(String(MAX_DESCRIPTION_LENGTH));
  });

  it('ignores surrounding whitespace when measuring length', () => {
    const value = `  ${'x'.repeat(MAX_DESCRIPTION_LENGTH)}  `;
    expect(validateDescription(value).ok).toBe(true);
  });
});
