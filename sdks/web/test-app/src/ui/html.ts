/**
 * Tiny helpers shared by the test-app UI: HTML escaping, JSON syntax
 * highlighting, and a "hostile content is inert" guarantee.
 *
 * Everything returns plain strings. The rule for the whole test app UI is:
 * `textContent` for data, `innerHTML` only for markup this module built from
 * escaped tokens. `escapeHtml()` is the single gate for interpolated values.
 */

const ESCAPE_MAP: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

/** Escapes `& < > " '` so a value can be placed in HTML text or an attribute. */
export function escapeHtml(value: unknown): string {
  return String(value).replace(/[&<>"']/g, (char) => ESCAPE_MAP[char] ?? char);
}

/**
 * Highlights a JSON string as HTML. The input is tokenized with a single regex
 * pass covering strings (with escapes), numbers, booleans/null, and keys, then
 * every token is escaped before it is wrapped in a span. Because escaping
 * happens inside the tokenizer, hostile content (HTML tags, script strings)
 * always lands as text.
 */
export function highlightJson(json: string): string {
  const pattern =
    /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;

  let result = '';
  let lastIndex = 0;
  for (const match of json.matchAll(pattern)) {
    const index = match.index ?? 0;
    result += escapeHtml(json.slice(lastIndex, index));
    const [token, stringToken, colon, literal, number] = match;
    if (stringToken !== undefined) {
      const cls = colon !== undefined ? 'j-key' : 'j-str';
      result += `<span class="${cls}">${escapeHtml(stringToken)}</span>`;
      if (colon !== undefined) result += escapeHtml(colon);
    } else if (literal !== undefined) {
      result += `<span class="j-lit">${escapeHtml(literal)}</span>`;
    } else if (number !== undefined) {
      result += `<span class="j-num">${escapeHtml(number)}</span>`;
    } else {
      result += escapeHtml(token);
    }
    lastIndex = index + token.length;
  }
  result += escapeHtml(json.slice(lastIndex));
  return result;
}

/** Formats a byte count as `KB`/`MB` for display. */
export function formatKilobytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

export interface DiffRow {
  readonly path: string;
  readonly left: string;
  readonly right: string;
}

/**
 * A shallow, path-based diff of two JSON values. Values are compared as sorted
 * JSON strings; only differing paths are returned. Designed for comparing two
 * stored reports (dedup / retry checks), not for deep structural diffs.
 */
export function diffJson(left: unknown, right: unknown): DiffRow[] {
  const rows: DiffRow[] = [];
  const walk = (a: unknown, b: unknown, path: string): void => {
    const bothObjects =
      typeof a === 'object' && a !== null && typeof b === 'object' && b !== null;
    if (bothObjects && !Array.isArray(a) && !Array.isArray(b)) {
      const keys = new Set([
        ...Object.keys(a as Record<string, unknown>),
        ...Object.keys(b as Record<string, unknown>),
      ]);
      for (const key of Array.from(keys).sort()) {
        walk(
          (a as Record<string, unknown>)[key],
          (b as Record<string, unknown>)[key],
          path.length === 0 ? key : `${path}.${key}`,
        );
      }
      return;
    }
    const sa = stableString(a);
    const sb = stableString(b);
    if (sa !== sb) rows.push({ path: path.length === 0 ? '(root)' : path, left: sa, right: sb });
  };
  walk(left, right, '');
  return rows;
}

function stableString(value: unknown): string {
  if (value === undefined) return 'undefined';
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}
