/**
 * `fetch` instrumentation edge cases:
 * - the `other_error` category,
 * - "host wrapped fetch after us" (dispose must not clobber the host's wrapper),
 * - "host wrapped fetch before us" (restore to the host wrapper),
 * - and the guarantee that request bodies/headers/cookies are never recorded.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { instrumentFetch, isInstrumentedFetch } from '@sdk/network/instrumentFetch';
import { resetOriginalFetch } from '@sdk/network/originalFetch';
import type { DiagnosticEvent, NetworkDiagnosticEvent } from '@sdk/diagnostics/types';

const ENDPOINT = 'https://receiver.test/api/reports';
const realFetch = globalThis.fetch;

let events: DiagnosticEvent[];

beforeEach(() => {
  resetOriginalFetch();
  events = [];
});

afterEach(() => {
  globalThis.fetch = realFetch;
  resetOriginalFetch();
  vi.restoreAllMocks();
});

function networkEvents(): NetworkDiagnosticEvent[] {
  return events.filter((e): e is NetworkDiagnosticEvent => e.kind === 'network');
}

describe('instrumentFetch edge cases', () => {
  it('records an other_error for a rejection that is neither TypeError nor AbortError', async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new Error('boom');
    }) as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, (e) => events.push(e));
    await expect(fetch('https://api.test/thing')).rejects.toThrow('boom');
    expect(networkEvents()[0]!.data.category).toBe('other_error');
    dispose();
  });

  it('does not clobber a fetch that the host replaced after us, on dispose', () => {
    const inner = vi.fn(async () => new Response('ok'));
    globalThis.fetch = inner as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, (e) => events.push(e));
    const foreign = vi.fn(async () => new Response('host'));
    globalThis.fetch = foreign as unknown as typeof fetch;

    dispose();
    expect(globalThis.fetch).toBe(foreign);
    expect(isInstrumentedFetch(globalThis.fetch)).toBe(false);
  });

  it('wraps a host wrapper that was installed before us and restores it on dispose', async () => {
    const hostWrapper = vi.fn(async () => new Response('bad', { status: 500 }));
    globalThis.fetch = hostWrapper as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, (e) => events.push(e));
    expect(isInstrumentedFetch(globalThis.fetch)).toBe(true);

    await fetch('https://api.test/thing');
    expect(hostWrapper).toHaveBeenCalledTimes(1);
    expect(networkEvents()).toHaveLength(1);

    dispose();
    expect(globalThis.fetch).toBe(hostWrapper);
  });

  it('never records request headers, body or cookies', async () => {
    globalThis.fetch = vi.fn(async () => new Response('nope', { status: 500 })) as unknown as typeof fetch;

    const dispose = instrumentFetch({ endpoint: ENDPOINT }, (e) => events.push(e));
    await fetch('https://api.test/thing?ok=1', {
      method: 'POST',
      headers: { Authorization: 'Bearer SECRET-HEADER-VALUE', Cookie: 'session=SECRET-COOKIE' },
      body: JSON.stringify({ password: 'SECRET-BODY-VALUE' }),
    });

    const serialized = JSON.stringify(events);
    expect(serialized).not.toContain('SECRET-HEADER-VALUE');
    expect(serialized).not.toContain('SECRET-COOKIE');
    expect(serialized).not.toContain('SECRET-BODY-VALUE');
    expect(Object.keys(networkEvents()[0]!.data).sort()).toEqual([
      'category',
      'durationMs',
      'method',
      'status',
      'url',
    ]);
    dispose();
  });
});
