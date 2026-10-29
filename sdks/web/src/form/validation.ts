/**
 * Bug-report form validation.
 *
 * Kept free of DOM types so it is trivially unit-testable in a Node environment.
 */

/** Maximum number of characters accepted in the description (after trimming). */
export const MAX_DESCRIPTION_LENGTH = 5000;

export type ValidationResult =
  | { readonly ok: true; readonly value: string }
  | { readonly ok: false; readonly message: string };

/**
 * Validates the description: must be non-empty after trimming, and no longer
 * than {@link MAX_DESCRIPTION_LENGTH} characters.
 *
 * @returns the trimmed value on success, or a user-facing message on failure.
 */
export function validateDescription(raw: string): ValidationResult {
  const value = typeof raw === 'string' ? raw.trim() : '';
  if (value.length === 0) {
    return { ok: false, message: 'Please describe the problem before sending.' };
  }
  if (value.length > MAX_DESCRIPTION_LENGTH) {
    return {
      ok: false,
      message: `Please keep the description under ${MAX_DESCRIPTION_LENGTH} characters.`,
    };
  }
  return { ok: true, value };
}
