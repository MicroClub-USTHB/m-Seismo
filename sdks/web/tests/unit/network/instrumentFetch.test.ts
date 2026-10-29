import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { instrumentFetch, isInstrumentedFetch } from '@sdk/network/instrumentFetch';
import { resetOriginalFetch } from '@sdk/network/originalFetch';
import type { DiagnosticEvent, NetworkDiagnosticEvent } from '@sdk/diagnostics/types';

const ENDPOINT = 'https://receiver.test/api/reports';
const realFetch = globalThis.fetch;

let events: DiagnosticEvent[];
let record: (event: DiagnosticEvent) => void;

beforeEach(() => {
  resetOriginalFetch();
  events = [];
  record = (event) => events.push(event);
});

afterEach(() => {
  globalThis.fetch = realFetch;
  resetOriginalFetch();
  vi.restoreAllMocks();
});

function networkEvents(): NetworkDiagnosticEvent[] {
  return events.filter((e): e is NetworkDiagnosticEvent => e.kind === 'network');
}

describe('instrumentFetch', () => {
  it('records an http_error for a 4xx/5xx response and passes it through', async () => {
    const inner = vi.fn(async () => new Response('nope', { status: 500 }));
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    const response = await fetch('https://api.test/thing', { method: 'post' });

    expect(response.status).toBe(500);
    expect(inner).toHaveBeenCalledTimes(1);
    const nets = networkEvents();
    expect(nets).toHaveLength(1);
    expect(nets[0]!.data.category).toBe('http_error');
    expect(nets[0]!.data.method).toBe('POST');
    expect(nets[0]!.data.status).toBe(500);
    dispose();
  });

  it('records a network_error for a TypeError rejection and rethrows', async () => {
    const inner = vi.fn(async () => {
      throw new TypeError('Failed to fetch');
    });
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    await expect(fetch('https://api.test/thing')).rejects.toThrow('Failed to fetch');

    expect(networkEvents()[0]!.data.category).toBe('network_error');
    dispose();
  });

  it('records an aborted event for an AbortError rejection', async () => {
    const inner = vi.fn(async () => {
      throw new DOMException('aborted', 'AbortError');
    });
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    await expect(fetch('https://api.test/thing')).rejects.toThrow();

    const net = networkEvents()[0]!;
    expect(net.data.category).toBe('aborted');
    expect(net.data.aborted).toBe(true);
    dispose();
  });

  it('records an opaque_response but considers it a non-failure', async () => {
    const inner = vi.fn(async () => ({ status: 0 }) as unknown as Response);
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    await fetch('https://api.test/opaque');

    expect(networkEvents()[0]!.data.category).toBe('opaque_response');
    expect(networkEvents()[0]!.data.status).toBeUndefined();
    dispose();
  });

  it('never records a 2xx response', async () => {
    const inner = vi.fn(async () => new Response('ok', { status: 200 }));
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    await fetch('https://api.test/ok');

    expect(networkEvents()).toHaveLength(0);
    dispose();
  });

  it('skips the SDK endpoint so a report upload never records itself', async () => {
    const inner = vi.fn(async () => new Response('bad', { status: 500 }));
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    await fetch(ENDPOINT, { method: 'POST' });

    expect(inner).toHaveBeenCalledTimes(1);
    expect(networkEvents()).toHaveLength(0);
    dispose();
  });

  it('honours ignoreUrls (exact strings and RegExp)', async () => {
    const inner = vi.fn(async () => new Response('bad', { status: 500 }));
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch(
      {
        endpoint: ENDPOINT,
        ignoreUrls: ['https://api.test/ignored', /\/noisy\//],
      },
      record,
    );
    await fetch('https://api.test/ignored');
    await fetch('https://api.test/noisy/thing');
    expect(networkEvents()).toHaveLength(0);

    await fetch('https://api.test/recorded');
    expect(networkEvents()).toHaveLength(1);
    dispose();
  });

  it('marks the wrapper and restores the original on dispose', () => {
    const inner = vi.fn(async () => new Response('ok'));
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    expect(isInstrumentedFetch(globalThis.fetch)).toBe(true);
    dispose();
    expect(globalThis.fetch).toBe(inner);
    expect(isInstrumentedFetch(globalThis.fetch)).toBe(false);
  });

  it('redacts sensitive query values in the recorded URL', async () => {
    const inner = vi.fn(async () => new Response('bad', { status: 403 }));
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, record);
    await fetch('https://api.test/thing?access_token=fake-secret-123&ok=1');

    const url = networkEvents()[0]!.data.url;
    expect(url).toContain('access_token=[REDACTED]');
    expect(url).not.toContain('fake-secret-123');
    dispose();
  });
});
