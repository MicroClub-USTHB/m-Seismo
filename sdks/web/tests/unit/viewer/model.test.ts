/**
 * Report-to-view-model mapping. Every value is derived from a stored report and
 * nothing is invented or hidden: absent fields are marked "not available".
 */
import { describe, expect, it } from 'vitest';
import type { ReportPayload } from '@m-seismo/web';
import {
  contextEntries,
  errorModel,
  headerModel,
  highlightIndex,
  presentBreadcrumbs,
  presentNetworkFailures,
  presentType,
  screenshotModel,
  toViewModel,
} from '@app/viewer/model';

function report(over: Partial<ReportPayload> = {}): ReportPayload {
  return {
    schemaVersion: 2,
    sdkVersion: '0.0.0',
    reportId: 'r-1',
    type: 'manual',
    trigger: 'form',
    description: 'a bug',
    error: null,
    context: { timestamp: '2026-01-01T00:00:00.000Z', application: {} },
    breadcrumbs: [],
    networkFailures: [],
    screenshot: null,
    ...over,
  } as ReportPayload;
}

describe('presentType', () => {
  it('maps known types to a label and tone', () => {
    expect(presentType('manual')).toEqual({ key: 'manual', label: 'Manual report', tone: 'info' });
    expect(presentType('automatic_error').tone).toBe('fail');
  });

  it('falls back to unknown for unrecognized types', () => {
    expect(presentType('mystery')).toEqual({ key: 'unknown', label: 'mystery', tone: 'neutral' });
    expect(presentType(undefined).key).toBe('unknown');
  });
});

describe('contextEntries', () => {
  it('marks absent fields as "not available"', () => {
    const entries = contextEntries(report());
    const byKey = Object.fromEntries(entries.map((e) => [e.key, e]));
    expect(byKey['Platform']!.absent).toBe(true);
    expect(byKey['Platform']!.value).toBe('not available');
    expect(byKey['URL']!.absent).toBe(true);
  });

  it('joins browser name and version and formats the viewport', () => {
    const entries = contextEntries(
      report({
        context: {
          timestamp: '2026-01-01T00:00:00.000Z',
          application: {},
          browser: { name: 'Chrome', version: '120' },
          viewport: { width: 800, height: 600 },
        },
      }),
    );
    const byKey = Object.fromEntries(entries.map((e) => [e.key, e]));
    expect(byKey['Browser']!.value).toBe('Chrome 120');
    expect(byKey['Viewport']!.value).toBe('800 × 600');
  });
});

describe('errorModel', () => {
  it('reports absence when there is no error', () => {
    expect(errorModel(report()).present).toBe(false);
  });

  it('copies error fields defensively', () => {
    const model = errorModel(
      report({
        error: {
          type: 'TypeError',
          message: 'boom',
          stack: 'at x',
          flags: ['weak-fingerprint'],
          fingerprint: 'abc',
          occurrences: 3,
        },
      }),
    );
    expect(model.present).toBe(true);
    expect(model.flags).toEqual(['weak-fingerprint']);
    expect(model.occurrences).toBe(3);
    expect(model.fingerprint).toBe('abc');
  });
});

describe('highlightIndex', () => {
  it('returns the last breadcrumb at or before the error time', () => {
    const model = highlightIndex(
      report({
        error: { type: 'E', message: 'm', timestamp: '2026-01-01T00:00:02.000Z' },
        breadcrumbs: [
          { timestamp: '2026-01-01T00:00:01.000Z', category: 'interaction', message: 'a' },
          { timestamp: '2026-01-01T00:00:03.000Z', category: 'interaction', message: 'b' },
        ],
      }),
    );
    expect(model).toBe(0);
  });

  it('returns -1 when there is no error timestamp', () => {
    expect(highlightIndex(report())).toBe(-1);
  });
});

describe('presentBreadcrumbs', () => {
  it('assigns an icon per category', () => {
    const crumbs = presentBreadcrumbs(
      report({
        breadcrumbs: [
          { timestamp: 't', category: 'navigation', message: 'n' },
          { timestamp: 't', category: 'captured', message: 'm' },
        ],
      }),
    );
    expect(crumbs[0]!.icon).toBe('⇢');
    expect(crumbs[1]!.icon).toBe('•');
  });
});

describe('presentNetworkFailures', () => {
  it('formats status and duration, using — when status is absent', () => {
    const list = presentNetworkFailures(
      report({
        networkFailures: [
          { timestamp: 't', method: 'GET', url: 'u', status: 500, durationMs: 12, category: 'http_error' },
          { timestamp: 't', method: 'POST', url: 'u2', durationMs: 5, category: 'network_error' },
        ],
      }),
    );
    expect(list[0]!.status).toBe('500');
    expect(list[1]!.status).toBe('—');
  });
});

describe('screenshotModel', () => {
  it('reports absence', () => {
    expect(screenshotModel(report()).present).toBe(false);
  });

  it('builds a data URL from inline base64', () => {
    const model = screenshotModel(
      report({
        screenshot: { name: 'a.png', mimeType: 'image/png', sizeBytes: 3, dataBase64: 'AAAA' },
      }),
    );
    expect(model.dataUrl).toBe('data:image/png;base64,AAAA');
  });

  it('treats a redacted screenshot (no base64) as stripped', () => {
    const model = screenshotModel(
      report({
        screenshot: { name: 'a.png', mimeType: 'image/png', sizeBytes: 3, dataBase64Length: 999 } as never,
      }),
    );
    expect(model.dataUrl).toBeNull();
    expect(model.dataBase64Length).toBe(999);
  });
});

describe('toViewModel', () => {
  it('assembles the full model and flags an empty description', () => {
    const model = toViewModel(report({ description: null }));
    expect(model.hasDescription).toBe(false);
    expect(model.header.reportId).toBe('r-1');
    expect(model.context.length).toBeGreaterThan(0);
    expect(headerModel(report()).schemaVersion).toBe('2');
  });
});
