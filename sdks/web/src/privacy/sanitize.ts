/**
 * Privacy / sanitisation boundary.
 *
 * `sanitize()` is the ONLY route from a raw {@link ReportPayload} to the
 * transport. The transport accepts `SanitizedReport` and nothing else, so the
 * architecture is locked in even before full redaction exists.
 *
 * Phase 5.3 adds a **BASELINE** text scrubber ({@link scrubReport}) that redacts
 * the most common accidental-secret shapes from error messages/stacks,
 * breadcrumb messages/data and network URLs. This is NOT the full privacy phase:
 * the user description is left untouched and structural redaction is deferred.
 *
 * TODO Phase 6: replace the baseline with real privacy filtering (scrub the
 * description, structured field allow-lists, PII handling, etc.).
 */
import { scrubReport } from './scrub';
import type { ReportPayload } from '../core/types';

declare const sanitizedBrand: unique symbol;

/**
 * A report that has passed through {@link sanitize}. Branded so it cannot be
 * produced except by `sanitize()`.
 */
export type SanitizedReport = ReportPayload & {
  readonly [sanitizedBrand]: true;
};

/**
 * Applies the baseline scrubber and brands the result as the single value the
 * transport will accept. Later phases can add redaction without touching the
 * transport.
 */
export function sanitize(report: ReportPayload): SanitizedReport {
  return scrubReport(report) as SanitizedReport;
}
