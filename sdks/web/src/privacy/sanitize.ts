/**
 * Privacy / sanitisation boundary.
 *
 * `sanitize()` is the ONLY route from a raw {@link ReportPayload} to the
 * transport. The transport accepts `SanitizedReport` and nothing else, so the
 * architecture is locked in even before real redaction exists.
 *
 * TODO Phase 6: implement real privacy filtering (redact URLs/headers/PII,
 * scrub the description, drop sensitive breadcrumb fields, etc.).
 */
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
 * Pass-through in this phase. Marked as the single entry point so later phases
 * can add redaction without touching the transport.
 */
export function sanitize(report: ReportPayload): SanitizedReport {
  // TODO Phase 6: real privacy filtering / redaction.
  return report as SanitizedReport;
}
