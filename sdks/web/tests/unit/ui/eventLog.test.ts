/**
 * Event-log model: level classification, source tagging, bounded append,
 * filtering and counts. Pure functions, no DOM.
 */
import { describe, expect, it } from 'vitest';
import {
  appendEntry,
  classifyLevel,
  classifySource,
  countsByLevel,
  DEFAULT_FILTER,
  filterEntries,
  MAX_LOG_ENTRIES,
  sourcesOf,
  type LogEntry,
} from '@app/ui/eventLog';

function entry(over: Partial<LogEntry> = {}): LogEntry {
  return {
    id: 0,
    time: '12:00:00.000',
    level: 'info',
    source: 'app',
    message: 'hello',
    ...over,
  };
}

describe('classifyLevel', () => {
  it('prefers an explicit level', () => {
    expect(classifyLevel('all good', 'error')).toBe('error');
  });

  it('classifies error-like messages', () => {
    expect(classifyLevel('request failed')).toBe('error');
    expect(classifyLevel('uncaught exception')).toBe('error');
    expect(classifyLevel('report rejected')).toBe('error');
  });

  it('classifies warning-like messages', () => {
    expect(classifyLevel('request slow')).toBe('warn');
    expect(classifyLevel('capture disabled')).toBe('warn');
  });

  it('defaults to info', () => {
    expect(classifyLevel('opened the form')).toBe('info');
  });
});

describe('classifySource', () => {
  it('extracts a leading source label before a colon', () => {
    expect(classifySource('report lab: opened form')).toBe('report lab');
    expect(classifySource('diagnostics: buffer cleared')).toBe('diagnostics');
  });

  it('tags sdk events by their leading label', () => {
    expect(classifySource('sdk event: form:open')).toBe('sdk event');
  });

  it('falls back to app', () => {
    expect(classifySource('theme set to dark')).toBe('app');
  });
});

describe('appendEntry', () => {
  it('appends without trimming below the cap', () => {
    const result = appendEntry([entry({ id: 1 })], entry({ id: 2 }), 5);
    expect(result.map((e) => e.id)).toEqual([1, 2]);
  });

  it('drops the oldest entry once the cap is exceeded', () => {
    const full = Array.from({ length: 3 }, (_, i) => entry({ id: i }));
    const result = appendEntry(full, entry({ id: 3 }), 3);
    expect(result.map((e) => e.id)).toEqual([1, 2, 3]);
  });

  it('never mutates the input array', () => {
    const input = [entry({ id: 1 })];
    appendEntry(input, entry({ id: 2 }), 10);
    expect(input).toHaveLength(1);
  });

  it('uses the 500-entry cap by default', () => {
    const big = Array.from({ length: MAX_LOG_ENTRIES }, (_, i) => entry({ id: i }));
    const result = appendEntry(big, entry({ id: MAX_LOG_ENTRIES }));
    expect(result).toHaveLength(MAX_LOG_ENTRIES);
    expect(result[0]!.id).toBe(1);
  });
});

describe('filterEntries', () => {
  const entries = [
    entry({ id: 1, level: 'info', source: 'app', message: 'opened the form' }),
    entry({ id: 2, level: 'error', source: 'sdk', message: 'submit failed' }),
    entry({ id: 3, level: 'warn', source: 'sdk', message: 'request slow' }),
  ];

  it('returns everything for the default filter', () => {
    expect(filterEntries(entries, DEFAULT_FILTER)).toHaveLength(3);
  });

  it('filters by level', () => {
    expect(filterEntries(entries, { ...DEFAULT_FILTER, level: 'error' }).map((e) => e.id)).toEqual([2]);
  });

  it('filters by source', () => {
    expect(filterEntries(entries, { ...DEFAULT_FILTER, source: 'sdk' }).map((e) => e.id)).toEqual([2, 3]);
  });

  it('searches case-insensitively', () => {
    expect(filterEntries(entries, { ...DEFAULT_FILTER, search: 'FAILED' }).map((e) => e.id)).toEqual([2]);
  });

  it('combines level, source and search', () => {
    expect(
      filterEntries(entries, { level: 'warn', source: 'sdk', search: 'slow' }).map((e) => e.id),
    ).toEqual([3]);
  });
});

describe('sourcesOf and countsByLevel', () => {
  it('lists distinct sources sorted', () => {
    expect(sourcesOf([entry({ source: 'sdk' }), entry({ source: 'app' }), entry({ source: 'sdk' })])).toEqual([
      'app',
      'sdk',
    ]);
  });

  it('counts entries per level', () => {
    expect(
      countsByLevel([entry({ level: 'info' }), entry({ level: 'error' }), entry({ level: 'error' })]),
    ).toEqual({ info: 1, warn: 0, error: 2 });
  });
});
