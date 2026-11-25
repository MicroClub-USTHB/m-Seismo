/**
 * Deduplication + rate limiting state for automatic/captured reports (Phase 7).
 *
 * Manual (form) reports NEVER pass through here — a user's explicit report is
 * always sent. This state only bounds the *unsolicited* reports that the SDK
 * itself decides to send.
 *
 * Rules:
 * - Per fingerprint: the first occurrence in a window is sent; further
 *   occurrences are counted, not sent. When the window closes (checked lazily on
 *   the next occurrence — no timers are kept alive) the next occurrence is sent
 *   with `occurrences` = the count since the last send.
 * - Global: at most `maxPerMinute` sends in a tumbling one-minute window, and at
 *   most `maxPerSession` sends for the lifetime of the page.
 * - Suppressed occurrences are counted (for stats) but never sent.
 *
 * The fingerprint table is bounded and in-memory only: no storage, no cookies.
 * It survives repeated `init()` and is cleared by `destroy()`.
 */

/** Action returned by {@link ReportLimiter.decide}. */
export type LimitAction = 'send' | 'duplicate' | 'rate_limited';

export interface LimiterDecision {
  readonly action: LimitAction;
  /** Occurrences of this fingerprint since the last sent report (inclusive). */
  readonly occurrences: number;
}

export interface LimiterOptions {
  /** Per-fingerprint dedup window, in milliseconds. */
  readonly dedupWindowMs: number;
  /** Max automatic/captured sends per tumbling minute. */
  readonly maxPerMinute: number;
  /** Max automatic/captured sends for the lifetime of the page. */
  readonly maxPerSession: number;
  /** Max distinct fingerprints tracked (oldest evicted). Defaults to 100. */
  readonly maxFingerprints?: number;
}

/** Hard bound on the fingerprint table so state can never grow unbounded. */
export const MAX_TRACKED_FINGERPRINTS = 100;

const MINUTE_MS = 60_000;

interface Entry {
  lastSentAt: number;
  /** Occurrences observed since the last send, inclusive of that send. */
  count: number;
}

export interface LimiterStats {
  readonly fingerprints: number;
  readonly sessionSends: number;
  readonly minuteSends: number;
  readonly suppressed: { readonly duplicate: number; readonly rate_limited: number };
}

export class ReportLimiter {
  private options: Required<LimiterOptions>;
  private readonly entries = new Map<string, Entry>();
  private minuteStart = 0;
  private minuteCount = 0;
  private sessionCount = 0;
  private duplicateCount = 0;
  private rateLimitedCount = 0;

  constructor(options: LimiterOptions) {
    this.options = {
      maxFingerprints: options.maxFingerprints ?? MAX_TRACKED_FINGERPRINTS,
      dedupWindowMs: options.dedupWindowMs,
      maxPerMinute: options.maxPerMinute,
      maxPerSession: options.maxPerSession,
    };
  }

  /**
   * Updates the limits (used by a repeated `init()`). Existing state is kept:
   * the window/caps change, history does not.
   */
  configure(options: LimiterOptions): void {
    this.options = {
      maxFingerprints: options.maxFingerprints ?? MAX_TRACKED_FINGERPRINTS,
      dedupWindowMs: options.dedupWindowMs,
      maxPerMinute: options.maxPerMinute,
      maxPerSession: options.maxPerSession,
    };
  }

  private touch(fingerprint: string, entry: Entry): void {
    // Re-insert to move this fingerprint to the most-recent end of the Map.
    this.entries.delete(fingerprint);
    this.entries.set(fingerprint, entry);
    while (this.entries.size > this.options.maxFingerprints) {
      const oldest = this.entries.keys().next().value;
      if (oldest === undefined) break;
      this.entries.delete(oldest);
    }
  }

  private rollMinute(now: number): void {
    if (this.minuteStart === 0 || now - this.minuteStart >= MINUTE_MS) {
      this.minuteStart = now;
      this.minuteCount = 0;
    }
  }

  /**
   * Decides whether a new automatic/captured occurrence should be sent. Updates
   * internal counters accordingly.
   */
  decide(fingerprint: string, now: number): LimiterDecision {
    const existing = this.entries.get(fingerprint);

    if (existing !== undefined && now - existing.lastSentAt < this.options.dedupWindowMs) {
      existing.count += 1;
      this.touch(fingerprint, existing);
      this.duplicateCount += 1;
      return { action: 'duplicate', occurrences: existing.count };
    }

    this.rollMinute(now);
    if (this.minuteCount >= this.options.maxPerMinute || this.sessionCount >= this.options.maxPerSession) {
      this.rateLimitedCount += 1;
      if (existing !== undefined) {
        existing.count += 1;
        this.touch(fingerprint, existing);
        return { action: 'rate_limited', occurrences: existing.count };
      }
      return { action: 'rate_limited', occurrences: 1 };
    }

    const occurrences = existing !== undefined ? existing.count + 1 : 1;
    this.touch(fingerprint, { lastSentAt: now, count: 1 });
    this.minuteCount += 1;
    this.sessionCount += 1;
    return { action: 'send', occurrences };
  }

  /**
   * Occurrence count for a fingerprint without mutating state. Used by manual
   * reports, which are never deduplicated: they simply report the current count
   * (defaulting to `1` when the fingerprint was never seen automatically).
   */
  occurrencesFor(fingerprint: string): number {
    return this.entries.get(fingerprint)?.count ?? 1;
  }

  /** Clears all dedup/limit state. Called by `destroy()`. */
  clear(): void {
    this.entries.clear();
    this.minuteStart = 0;
    this.minuteCount = 0;
    this.sessionCount = 0;
    this.duplicateCount = 0;
    this.rateLimitedCount = 0;
  }

  stats(): LimiterStats {
    return {
      fingerprints: this.entries.size,
      sessionSends: this.sessionCount,
      minuteSends: this.minuteCount,
      suppressed: { duplicate: this.duplicateCount, rate_limited: this.rateLimitedCount },
    };
  }
}
