/**
 * BASELINE text scrubber (Phase 5.3).
 *
 * This is deliberately a **best-effort baseline**, NOT the full privacy phase.
 * It reduces the most common accidental-secret shapes in free text before a
 * report leaves the browser:
 *
 * - `Authorization: Bearer <token>` credentials;
 * - JWT-shaped strings (`eyJ….….…`);
 * - `token=…`, `secret: …`, `password=…`, `api_key=…`-style pairs;
 * - long (32+) hex strings and long random-looking base64/url-safe strings.
 *
 * It is applied to error messages/stacks, breadcrumb messages and data strings,
 * network-failure URLs and context strings. It does NOT scrub the user-written
 * `description` — that is Phase 6's job, along with structural redaction, and it
 * can over-redact legitimate text (flagged in the phase report).
 *
 * Every function here is pure and never throws.
 */
import type { ReportPayload, ReportBreadcrumb } from '../core/types';
import type { JsonValue } from '../diagnostics/types';

/** Marker substituted for scrubbed values. */
export const SCRUB_MARKER = '[REDACTED]';

const BEARER = /\bBearer\s+[A-Za-z0-9._~+/=-]+/gi;
const JWT = /\beyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g;
const KEY_VALUE =
  /\b(token|access[_-]?token|refresh[_-]?token|id[_-]?token|secret|client[_-]?secret|password|passwd|pwd|api[_-]?key|apikey|authorization|auth)\b(["']?\s*[:=]\s*["']?)([^\s"'&,;)\]}{]+)/gi;
const LONG_HEX = /\b[0-9a-fA-F]{32,}\b/g;
const LONG_B64 =
  /\b(?=[A-Za-z0-9_-]{32,}\b)(?=[A-Za-z0-9_-]*[0-9])(?=[A-Za-z0-9_-]*[A-Z_-])[A-Za-z0-9_-]+\b/g;

/** Scrubs a single string. Returns the input unchanged when nothing matches. */
export function scrubText(value: string): string {
  return value
    .replace(BEARER, `Bearer ${SCRUB_MARKER}`)
    .replace(JWT, SCRUB_MARKER)
    .replace(KEY_VALUE, (_match, key: string, sep: string) => `${key}${sep}${SCRUB_MARKER}`)
    .replace(LONG_HEX, SCRUB_MARKER)
    .replace(LONG_B64, SCRUB_MARKER);
}

/** Recursively scrubs every string inside a JSON value. */
function scrubJson(value: JsonValue): JsonValue {
  if (typeof value === 'string') return scrubText(value);
  if (Array.isArray(value)) return value.map(scrubJson);
  if (value !== null && typeof value === 'object') {
    const out: Record<string, JsonValue> = {};
    for (const key of Object.keys(value)) {
      out[key] = scrubJson(value[key]!);
    }
    return out;
  }
  return value;
}

function scrubBreadcrumb(breadcrumb: ReportBreadcrumb): ReportBreadcrumb {
  return {
    timestamp: breadcrumb.timestamp,
    category: breadcrumb.category,
    message: scrubText(breadcrumb.message),
    ...(breadcrumb.level !== undefined ? { level: breadcrumb.level } : {}),
    ...(breadcrumb.data !== undefined
      ? { data: scrubJson(breadcrumb.data) as Record<string, JsonValue> }
      : {}),
  };
}

/**
 * Returns a scrubbed copy of a report. Immutable: the input is never modified.
 */
export function scrubReport(report: ReportPayload): ReportPayload {
  const error =
    report.error === null
      ? null
      : {
          ...report.error,
          message: scrubText(report.error.message),
          ...(report.error.stack !== undefined ? { stack: scrubText(report.error.stack) } : {}),
          ...(report.error.context !== undefined
            ? { context: scrubJson(report.error.context) as Record<string, JsonValue> }
            : {}),
        };

  return {
    ...report,
    error,
    breadcrumbs: report.breadcrumbs.map(scrubBreadcrumb),
    networkFailures: report.networkFailures.map((failure) => ({
      ...failure,
      url: scrubText(failure.url),
    })),
  };
}
