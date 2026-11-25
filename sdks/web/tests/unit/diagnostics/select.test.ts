import { describe, expect, it } from 'vitest';
import { selectDiagnostics } from '@sdk/diagnostics/select';
import { MAX_REPORT_BREADCRUMBS } from '@sdk/diagnostics/types';
import type {
  DiagnosticEvent,
  ErrorDiagnosticEvent,
  InteractionDiagnosticEvent,
  NavigationDiagnosticEvent,
  NetworkDiagnosticEvent,
  SdkDiagnosticEvent,
} from '@sdk/diagnostics/types';

const NOW = Date.parse('2026-01-02T03:04:05.000Z');
const OPTS = { includeInReports: true, maxAgeMs: 600_000 };

function at(offsetMs = 0): string {
  return new Date(NOW + offsetMs).toISOString();
}

function errorEvent(offsetMs = 0, message = 'boom'): ErrorDiagnosticEvent {
  return {
    id: `e-${offsetMs}`,
    kind: 'error',
    timestamp: at(offsetMs),
    error: { type: 'TypeError', message },
    handled: false,
  };
}

function navEvent(offsetMs = 0): NavigationDiagnosticEvent {
  return {
    id: `n-${offsetMs}`,
    kind: 'navigation',
    timestamp: at(offsetMs),
    data: { trigger: 'pushState', from: 'https://app.test/a', to: 'https://app.test/b' },
  };
}

function interactionEvent(offsetMs = 0): InteractionDiagnosticEvent {
  return {
    id: `i-${offsetMs}`,
    kind: 'interaction',
    timestamp: at(offsetMs),
    data: { action: 'click', target: { tag: 'button', testId: 'save' } },
  };
}

function sdkEvent(offsetMs = 0): SdkDiagnosticEvent {
  return {
    id: `s-${offsetMs}`,
    kind: 'sdk',
    timestamp: at(offsetMs),
    data: { name: 'report-form-opened' },
  };
}

function networkEvent(
  offsetMs = 0,
  category: NetworkDiagnosticEvent['data']['category'] = 'http_error',
): NetworkDiagnosticEvent {
  return {
    id: `net-${offsetMs}-${category}`,
    kind: 'network',
    timestamp: at(offsetMs),
    data: {
      method: 'GET',
      url: `https://api.test/${category}`,
      durationMs: 12,
      category,
      ...(category === 'http_error' ? { status: 500 } : {}),
    },
  };
}

describe('selectDiagnostics', () => {
  it('returns empty when includeInReports is false', () => {
    const selection = selectDiagnostics([errorEvent(), navEvent()], NOW, {
      includeInReports: false,
      maxAgeMs: 600_000,
    });
    expect(selection).toEqual({ breadcrumbs: [], networkFailures: [], error: null });
  });

  it('excludes events older than maxAgeMs', () => {
    const selection = selectDiagnostics([errorEvent(-700_000)], NOW, OPTS);
    expect(selection.error).toBeNull();
    expect(selection.breadcrumbs).toEqual([]);
  });

  it('picks the latest error-like event as the report error', () => {
    const selection = selectDiagnostics(
      [errorEvent(0, 'first'), errorEvent(1000, 'second')],
      NOW,
      OPTS,
    );
    expect(selection.error?.message).toBe('second');
    expect(selection.error?.type).toBe('TypeError');
    expect(selection.error?.handled).toBe(false);
    expect(selection.error?.timestamp).toBe(at(1000));
  });

  it('maps all non-network kinds to breadcrumbs oldest-first', () => {
    const selection = selectDiagnostics(
      [errorEvent(0), navEvent(1000), interactionEvent(2000), sdkEvent(3000)],
      NOW,
      OPTS,
    );
    expect(selection.breadcrumbs.map((b) => b.category)).toEqual([
      'error',
      'navigation',
      'interaction',
      'sdk',
    ]);
    expect(selection.breadcrumbs[0]!.level).toBe('error');
    expect(selection.breadcrumbs[1]!.level).toBe('info');
  });

  it('puts network failures in networkFailures, oldest-first', () => {
    const selection = selectDiagnostics(
      [networkEvent(0), networkEvent(1000)],
      NOW,
      OPTS,
    );
    expect(selection.networkFailures).toHaveLength(2);
    expect(selection.networkFailures[0]!.url).toContain('http_error');
    expect(selection.networkFailures[0]!.status).toBe(500);
  });

  it('treats an opaque_response as an info breadcrumb, not a failure', () => {
    const selection = selectDiagnostics([networkEvent(0, 'opaque_response')], NOW, OPTS);
    expect(selection.networkFailures).toEqual([]);
    expect(selection.breadcrumbs).toHaveLength(1);
    expect(selection.breadcrumbs[0]!.level).toBe('info');
  });

  it('caps breadcrumbs to the most recent and re-orders oldest-first', () => {
    const events: DiagnosticEvent[] = [];
    for (let i = 0; i < MAX_REPORT_BREADCRUMBS + 10; i += 1) {
      events.push(navEvent(i));
    }
    const selection = selectDiagnostics(events, NOW, OPTS);
    expect(selection.breadcrumbs).toHaveLength(MAX_REPORT_BREADCRUMBS);
    // Oldest of the retained 50 is the 10th event (offset i=10).
    expect(selection.breadcrumbs[0]!.timestamp).toBe(at(10));
    expect(selection.breadcrumbs[MAX_REPORT_BREADCRUMBS - 1]!.timestamp).toBe(
      at(MAX_REPORT_BREADCRUMBS + 9),
    );
  });
});
