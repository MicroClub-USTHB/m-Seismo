import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildReport, sanitize, sendReport } from '@m-seismo/web';
import type { SdkConfig } from '@m-seismo/web';

function makeConfig(
  overrides: { endpoint?: string; projectKey?: string; timeoutMs?: number } = {},
): SdkConfig {
  const config: {
    endpoint: string;
    projectKey?: string;
    timeoutMs: number;
    shortcut: false;
    launcher: { enabled: false };
    screenshot: boolean;
  } = {
    endpoint: overrides.endpoint ?? 'https://receiver.test/api/reports',
    timeoutMs: overrides.timeoutMs ?? 10_000,
    shortcut: false,
    launcher: { enabled: false },
    screenshot: true,
  };
  if (overrides.projectKey !== undefined) config.projectKey = overrides.projectKey;
  return config;
}

function makeReport() {
  return sanitize(
    buildReport(
      {
        description: 'transport test',
        context: {
          url: 'https://app.test/',
          timestamp: new Date('2026-01-02T03:04:05.000Z').toISOString(),
          application: {},
        },
      },
      { reportId: 'r-1' },
    ),
  );
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('sendReport result mapping', () => {
  it('201 with {id} -> ok with status and id, POST + JSON', async () => {
    const fetchMock = vi.fn((_url: string, _init: RequestInit) =>
      Promise.resolve(jsonResponse(201, { id: 'srv-1' })),
    );
    vi.stubGlobal('fetch', fetchMock);

    const result = await sendReport(makeReport(), makeConfig());

    expect(result).toEqual({ ok: true, status: 201, id: 'srv-1' });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe('https://receiver.test/api/reports');
    expect(init.method).toBe('POST');
    const headers = init.headers as Record<string, string>;
    expect(headers['Content-Type']).toBe('application/json');
    expect(headers['X-MSeismo-Key']).toBeUndefined();
    expect(JSON.parse(init.body as string).reportId).toBe('r-1');
  });

  it('sends X-MSeismo-Key only when a projectKey is configured', async () => {
    const fetchMock = vi.fn((_url: string, _init: RequestInit) =>
      Promise.resolve(jsonResponse(201, { id: 'x' })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await sendReport(makeReport(), makeConfig({ projectKey: 'pk_test' }));
    const [, init] = fetchMock.mock.calls[0]!;
    expect((init.headers as Record<string, string>)['X-MSeismo-Key']).toBe('pk_test');
  });

  it('201 without an id -> ok without id', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(201, {}))),
    );
    const result = await sendReport(makeReport(), makeConfig());
    expect(result).toEqual({ ok: true, status: 201 });
  });

  it.each([
    [400, 'validation'],
    [429, 'rate limited'],
    [500, 'server error'],
  ])('HTTP %i -> kind http with status', async (status) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(status, { message: 'nope' }))),
    );
    const result = await sendReport(makeReport(), makeConfig());
    expect(result).toMatchObject({ ok: false, kind: 'http', status });
  });

  it('network failure (fetch rejects) -> kind network', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    );
    const result = await sendReport(makeReport(), makeConfig());
    expect(result).toMatchObject({ ok: false, kind: 'network' });
    if (!result.ok) expect(result.message).toContain('Failed to fetch');
  });

  it('timeout (abort) -> kind timeout', async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn(
      (_url: string, init: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init.signal?.addEventListener('abort', () => reject(new Error('aborted')));
        }),
    );
    vi.stubGlobal('fetch', fetchMock);

    const promise = sendReport(makeReport(), makeConfig({ timeoutMs: 1000 }));
    await vi.advanceTimersByTimeAsync(1000);
    const result = await promise;

    expect(result).toMatchObject({ ok: false, kind: 'timeout' });
    if (!result.ok) expect(result.message).toContain('1000ms');
  });

  it('non-JSON success body -> kind invalid-response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response('not json', {
            status: 201,
            headers: { 'content-type': 'text/plain' },
          }),
        ),
      ),
    );
    const result = await sendReport(makeReport(), makeConfig());
    expect(result).toMatchObject({
      ok: false,
      kind: 'invalid-response',
      status: 201,
    });
  });
});
