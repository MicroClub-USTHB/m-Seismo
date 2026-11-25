/**
 * Fake secrets used by the lab AND the tests (single source of truth).
 *
 * Each value is shaped to trigger (or deliberately NOT trigger) one rule of the
 * BASIC baseline scrubber in `src/privacy/scrub.ts`. None of these are real.
 */
export const FAKE_SECRETS = {
  /** Long url-safe token; matches the baseline LONG_B64 rule. */
  longToken: 'super-secret-token-value-1234567890',
  /** A JWT-shaped string; matches the baseline JWT rule. */
  jwt: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U',
  /** A 40-char hex string; matches the baseline LONG_HEX rule. */
  longHex: 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef',
  /** `key=value` pair; matches the baseline KEY_VALUE rule. */
  keyValue: 'api_key=abcd1234efgh5678ijkl9012mnop3456',
  /** `Authorization: Bearer …`; matches the baseline BEARER rule. */
  bearer: 'Bearer sk-live-abcdefghijklmnopqrstuvwxyz012345',
  /** Extra query parameter value used for URL sanitisation. */
  queryValue: 'fake-url-secret-1234567890abcd',
  /** Free-text password with NO separator; the baseline does NOT target this. */
  prosePassword: 'my password is correct-horse-battery-staple',
} as const;

/** Every string that the baseline scrubber is expected to redact. */
export const SCRUBBED_SECRETS: readonly string[] = [
  FAKE_SECRETS.longToken,
  FAKE_SECRETS.jwt,
  FAKE_SECRETS.longHex,
  'abcd1234efgh5678ijkl9012mnop3456',
  'sk-live-abcdefghijklmnopqrstuvwxyz012345',
];

/** A secret the baseline is documented NOT to catch (Phase 6 must handle it). */
export const UNSCRUBBED_SECRETS: readonly string[] = [FAKE_SECRETS.prosePassword];
