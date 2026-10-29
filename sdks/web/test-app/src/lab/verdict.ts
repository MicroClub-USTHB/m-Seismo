/**
 * Verdict helpers and the expected↔observed acceptance rule.
 *
 * A scenario passes when its observed status matches its expected category:
 * - DETECTED_AUTO / MANUAL_ONLY / NOT_SUPPORTED → `pass` required (for
 *   NOT_SUPPORTED, `pass` means "confirmed NOT captured");
 * - LIMITED / KNOWN_GAP → `pass` or `info` accepted, `fail` never is.
 */
import type { LabExpected, Verdict, VerdictStatus } from './types';

export function pass(observed: string, evidence?: unknown): Verdict {
  return { status: 'pass', observed, ...(evidence !== undefined ? { evidence } : {}) };
}

export function fail(observed: string, evidence?: unknown): Verdict {
  return { status: 'fail', observed, ...(evidence !== undefined ? { evidence } : {}) };
}

export function info(observed: string, evidence?: unknown): Verdict {
  return { status: 'info', observed, ...(evidence !== undefined ? { evidence } : {}) };
}

/**
 * A gap is never a pass when evidence of a leak/problem is present: this helper
 * is the single place that decides, so a leak can never be softened to `pass`.
 */
export function gapVerdict(problemFound: boolean, observed: string, evidence?: unknown): Verdict {
  return problemFound ? info(observed, evidence) : pass(observed, evidence);
}

/** True when `status` is an acceptable observation for `expected`. */
export function accepts(expected: LabExpected, status: VerdictStatus): boolean {
  switch (expected) {
    case 'DETECTED_AUTO':
    case 'MANUAL_ONLY':
    case 'NOT_SUPPORTED':
      return status === 'pass';
    case 'LIMITED':
    case 'KNOWN_GAP':
      return status === 'pass' || status === 'info';
  }
}

/** Runs a verifier and converts any thrown error into a `fail` verdict. */
export async function safeVerify(
  verify: () => Promise<Verdict> | Verdict,
): Promise<Verdict> {
  try {
    const verdict = await verify();
    if (verdict === undefined || verdict === null) {
      return fail('verifier returned no verdict');
    }
    return verdict;
  } catch (error) {
    return fail(`verifier threw: ${String(error)}`);
  }
}
