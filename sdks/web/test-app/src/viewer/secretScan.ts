/**
 * Secret scanner for the "Redaction check" panel.
 *
 * Given the full stored report and a list of candidate secrets, reports which
 * ones still appear in the serialized JSON, and at which path. This is purely a
 * *check* on the existing payload — it never edits the report and never claims
 * more than the baseline scrubber does. The panel is labelled "Baseline privacy
 * only; Phase 6 pending".
 */

export interface SecretHit {
  readonly secret: string;
  /** JSON path of the first value that contains the secret, or null. */
  readonly path: string | null;
  /** Number of distinct paths where the secret was found. */
  readonly count: number;
}

export interface ScanResult {
  readonly found: SecretHit[];
  readonly clean: string[];
  /** `[REDACTED]` occurrences in the serialized report. */
  readonly redactionMarkers: number;
}

const REDACTION_MARKER = '[REDACTED]';

/**
 * Scans `value` for each secret. Values are matched against their serialized
 * form so a secret inside an array or object is found too. Empty secrets are
 * ignored (an empty string matches everything).
 */
export function scanForSecrets(value: unknown, secrets: readonly string[]): ScanResult {
  const wanted = Array.from(new Set(secrets.filter((secret) => secret.length > 0)));
  const pathsBySecret = new Map<string, string[]>();

  const walk = (current: unknown, path: string): void => {
    if (typeof current === 'string') {
      for (const secret of wanted) {
        if (current.includes(secret)) {
          const list = pathsBySecret.get(secret) ?? [];
          list.push(path);
          pathsBySecret.set(secret, list);
        }
      }
      return;
    }
    if (Array.isArray(current)) {
      current.forEach((item, index) => walk(item, `${path}[${index}]`));
      return;
    }
    if (current !== null && typeof current === 'object') {
      for (const [key, child] of Object.entries(current)) {
        walk(child, path.length === 0 ? key : `${path}.${key}`);
      }
    }
  };
  walk(value, '');

  const found: SecretHit[] = [];
  const clean: string[] = [];
  for (const secret of wanted) {
    const paths = pathsBySecret.get(secret);
    if (paths === undefined || paths.length === 0) {
      clean.push(secret);
    } else {
      found.push({ secret, path: paths[0]!, count: paths.length });
    }
  }

  const serialized = JSON.stringify(value) ?? '';
  const redactionMarkers = serialized.split(REDACTION_MARKER).length - 1;

  return { found, clean, redactionMarkers };
}
