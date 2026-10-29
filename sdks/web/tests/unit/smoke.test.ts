import { describe, expect, it } from 'vitest';
import * as sdk from '@m-seismo/web';
import { SDK_VERSION, captureException, captureMessage, init, openReportForm } from '@m-seismo/web';

describe('public API surface', () => {
  it('exposes the Phase 1 stubs and SDK_VERSION', () => {
    expect(typeof init).toBe('function');
    expect(typeof openReportForm).toBe('function');
    expect(typeof captureException).toBe('function');
    expect(typeof captureMessage).toBe('function');
    expect(typeof SDK_VERSION).toBe('string');
    expect(SDK_VERSION.length).toBeGreaterThan(0);
  });

  it('stub functions are callable no-ops in a Node environment', () => {
    expect(() => init({ endpoint: 'https://example.test/ingest' })).not.toThrow();
    expect(() => openReportForm()).not.toThrow();
    expect(() => captureException(new Error('boom'), { tag: 'test' })).not.toThrow();
    expect(() => captureMessage('hello', { tag: 'test' })).not.toThrow();
  });

  it('imports cleanly with no DOM globals present (SSR safety)', () => {
    expect(typeof globalThis.window).toBe('undefined');
    expect(typeof globalThis.document).toBe('undefined');
    expect(Object.keys(sdk)).toContain('SDK_VERSION');
  });
});
