// @vitest-environment happy-dom
/**
 * Viewer renderer: tab wiring, redaction panel, and — most importantly — the
 * XSS safety contract. Hostile report content (script tags, img onerror) must
 * be rendered as inert text, never as live DOM.
 */
import { describe, expect, it, vi } from 'vitest';
import type { ReportPayload } from '@m-seismo/web';
import { renderReportCard, renderViewer } from '@app/viewer/render';

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

describe('renderReportCard', () => {
  it('renders the tab list with all six panels', () => {
    const card = renderReportCard(report(), { secrets: [] });
    const tabs = Array.from(card.querySelectorAll<HTMLElement>('[role="tab"]')).map(
      (t) => t.dataset['panel'],
    );
    expect(tabs).toEqual(['summary', 'breadcrumbs', 'network', 'screenshot', 'raw', 'redaction']);
  });

  it('activates the summary tab by default', () => {
    const card = renderReportCard(report(), { secrets: [] });
    const selected = card.querySelector('[role="tab"][aria-selected="true"]');
    expect(selected?.getAttribute('data-panel')).toBe('summary');
  });

  it('renders hostile description as inert text, never live nodes', () => {
    const card = renderReportCard(
      report({ description: '<script>window.__xss=1</script><img src=x onerror="window.__xss=1">' }),
      { secrets: [] },
    );
    expect(card.querySelector('script')).toBeNull();
    expect(card.querySelector('img')).toBeNull();
    expect(card.textContent).toContain('<script>');
  });

  it('renders hostile error message/stack as inert text', () => {
    const card = renderReportCard(
      report({ error: { type: 'E', message: '<b>x</b>', stack: '<script>y</script>' } }),
      { secrets: [] },
    );
    expect(card.querySelector('b')).toBeNull();
    expect(card.querySelector('script')).toBeNull();
  });
});

describe('raw JSON tab', () => {
  it('escapes hostile content when highlighting', () => {
    const card = renderReportCard(report({ description: '</span><script>x</script>' }), {
      secrets: [],
    });
    const rawBtn = card.querySelector<HTMLElement>('[data-panel="raw"]')!;
    rawBtn.click();
    expect(card.querySelector('script')).toBeNull();
  });

  it('invokes onCopy with the raw JSON text', () => {
    const onCopy = vi.fn();
    const card = renderReportCard(report(), { secrets: [], onCopy });
    card.querySelector<HTMLElement>('[data-panel="raw"]')!.click();
    const copyBtn = card.querySelector<HTMLButtonElement>('[data-testid="viewer-copy-json"]')!;
    copyBtn.click();
    expect(onCopy).toHaveBeenCalledOnce();
    expect(JSON.parse(onCopy.mock.calls[0]![0] as string).reportId).toBe('r-1');
  });
});

describe('redaction panel', () => {
  it('shows a leak when a secret remains, and stays safe', () => {
    const card = renderReportCard(report({ description: 'token abc-secret-value here' }), {
      secrets: ['abc-secret-value'],
    });
    card.querySelector<HTMLElement>('[data-panel="redaction"]')!.click();
    expect(card.textContent).toContain('LEAK');
    expect(card.querySelector('script')).toBeNull();
  });

  it('reports no leaks when candidates are absent', () => {
    const card = renderReportCard(report({ description: 'clean' }), { secrets: ['missing-secret'] });
    card.querySelector<HTMLElement>('[data-panel="redaction"]')!.click();
    expect(card.textContent).not.toContain('LEAK');
  });
});

describe('renderViewer', () => {
  it('shows an empty state when there are no reports', () => {
    const host = document.createElement('div');
    renderViewer(host, { reports: [], secrets: [] });
    expect(host.textContent).toContain('No reports loaded');
  });

  it('renders one card per report', () => {
    const host = document.createElement('div');
    renderViewer(host, {
      reports: [report({ reportId: 'a' }), report({ reportId: 'b' })],
      secrets: [],
    });
    expect(host.querySelectorAll('[data-testid="viewer-card"]')).toHaveLength(2);
  });

  it('renders a compare panel for two reports when compare is enabled', () => {
    const host = document.createElement('div');
    renderViewer(host, {
      reports: [report({ description: 'one' }), report({ description: 'two' })],
      secrets: [],
      compare: true,
    });
    const compare = host.querySelector('[data-testid="viewer-compare"]');
    expect(compare).not.toBeNull();
    expect(compare!.textContent).toContain('description');
  });

  it('replaces previous content on re-render', () => {
    const host = document.createElement('div');
    renderViewer(host, { reports: [report()], secrets: [] });
    renderViewer(host, { reports: [], secrets: [] });
    expect(host.querySelectorAll('[data-testid="viewer-card"]')).toHaveLength(0);
  });
});
