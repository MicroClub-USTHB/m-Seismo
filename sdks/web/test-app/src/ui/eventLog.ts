/**
 * Event-log model: level/source classification, filtering, and a bounded ring
 * of entries. Pure so it can be unit-tested; the DOM panel is a thin renderer.
 */

export type LogLevel = 'info' | 'warn' | 'error';

export interface LogEntry {
  readonly id: number;
  readonly time: string;
  readonly level: LogLevel;
  readonly source: string;
  readonly message: string;
}

export interface LogFilter {
  readonly level: 'all' | LogLevel;
  readonly source: string;
  readonly search: string;
}

export const DEFAULT_FILTER: LogFilter = { level: 'all', source: 'all', search: '' };

/** The maximum number of entries kept; older entries fall off the front. */
export const MAX_LOG_ENTRIES = 500;

const ERROR_HINTS = /\b(error|fail|failure|failed|rejected|uncaught|exception)\b/i;
const WARN_HINTS = /\b(warn|slow|timeout|ignored|disabled|deprecat)\b/i;

/** Classifies a message into a level. Explicit `level` always wins. */
export function classifyLevel(message: string, level?: LogLevel): LogLevel {
  if (level !== undefined) return level;
  if (ERROR_HINTS.test(message)) return 'error';
  if (WARN_HINTS.test(message)) return 'warn';
  return 'info';
}

/** Derives a short source tag from a message like `report lab: …`. */
export function classifySource(message: string): string {
  const match = /^([a-z][a-z0-9 _-]{0,24}):/i.exec(message.trim());
  if (match !== null) return match[1]!.trim().toLowerCase();
  if (message.startsWith('sdk event')) return 'sdk';
  return 'app';
}

/** Appends an entry, dropping the oldest so the log stays bounded. */
export function appendEntry(
  entries: readonly LogEntry[],
  entry: LogEntry,
  max = MAX_LOG_ENTRIES,
): LogEntry[] {
  const next = [...entries, entry];
  if (next.length <= max) return next;
  return next.slice(next.length - max);
}

/** Applies the filter. Search is a case-insensitive substring match. */
export function filterEntries(
  entries: readonly LogEntry[],
  filter: LogFilter,
): LogEntry[] {
  const search = filter.search.trim().toLowerCase();
  return entries.filter((entry) => {
    if (filter.level !== 'all' && entry.level !== filter.level) return false;
    if (filter.source !== 'all' && entry.source !== filter.source) return false;
    if (search.length > 0 && !entry.message.toLowerCase().includes(search)) return false;
    return true;
  });
}

/** Distinct sources present in the log, sorted, for the filter dropdown. */
export function sourcesOf(entries: readonly LogEntry[]): string[] {
  return Array.from(new Set(entries.map((entry) => entry.source))).sort();
}

/** Counts per level, for the level filter chips and the count badge. */
export function countsByLevel(entries: readonly LogEntry[]): Record<LogLevel, number> {
  const counts: Record<LogLevel, number> = { info: 0, warn: 0, error: 0 };
  for (const entry of entries) counts[entry.level] += 1;
  return counts;
}
