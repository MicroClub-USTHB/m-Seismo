import { afterEach, describe, expect, it, vi } from 'vitest';
import { waitFor, waitForValue } from '@app/lab/helpers/waiters';
import { ReceiverClient } from '@app/lab/helpers/receiver';

describe('lab polling helpers', () => {
  it('returns true as soon as the predicate holds', async () => {
    let count = 0;
    const ok = await waitFor(() => {
      count += 1;
      return count >= 2;
    }, 1_000, 1);
    expect(ok).toBe(true);
  });

  it('returns false (does not throw) when the predicate never holds', async () => {
    const ok = await waitFor(() => false, 30, 5);
    expect(ok).toBe(false);
  });

  it('treats a throwing predicate as false', async () => {
    const ok = await waitFor(() => {
      throw new Error('nope');
    }, 30, 5);
    expect(ok).toBe(false);
  });

  it('waitForValue returns the first defined value', async () => {
    let value: number | undefined;
    setTimeout(() => {
      value = 7;
    }, 5);
    await expect(waitForValue(() => value, 500, 2)).resolves.toBe(7);
    await expect(waitForValue(() => undefined as number | undefined, 20, 5)).resolves.toBeUndefined();
  });
});

describe('lab receiver client', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('builds the reset, mode and reports requests', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = [];
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
      calls.push({ url, init });
      return Promise.resolve(
        new Response(JSON.stringify({ ok: true, count: 0, reports: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    });

    const client = new ReceiverClient('http://localhost:4010');
    await client.reset();
    await client.setMode('fail413', { delayMs: 10 });
    await client.getReports({ full: true, type: 'manual' });
    await client.getStats();

    expect(calls[0]?.url).toBe('http://localhost:4010/__reset');
    expect(calls[0]?.init?.method).toBe('POST');
    expect(calls[1]?.url).toBe('http://localhost:4010/__mode');
    expect(calls[1]?.init?.body).toBe(JSON.stringify({ mode: 'fail413', delayMs: 10 }));
    expect(calls[2]?.url).toContain('/__reports?');
    expect(calls[2]?.url).toContain('full=1');
    expect(calls[2]?.url).toContain('type=manual');
    expect(calls[3]?.url).toBe('http://localhost:4010/__stats');
  });

  it('rejects when the receiver answers non-OK', async () => {
    vi.stubGlobal('fetch', () => Promise.resolve(new Response('nope', { status: 500 })));
    const client = new ReceiverClient('http://localhost:4010');
    await expect(client.getReports()).rejects.toThrow(/500/);
  });
});
