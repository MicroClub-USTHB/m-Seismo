/**
 * Failure handling at the runtime boundary: whatever the transport or a user
 * hook does, `processInput()` must always resolve with a `ProcessResult` and
 * never throw into the host. Also covers the automatic-path re-entrancy guard
 * and the absence of retries/queuing (a documented Phase 8 gap).
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { SendResult } from '@sdk/transport/types';
import type { ErrorDiagnosticEvent } from '@sdk/diagnostics/types';
import type { ProcessResult } from '@sdk/reporting/processor';
import type { InitOptions } from '@sdk/core/config';

const transport = vi.hoisted(() => ({
  calls: 0,
  impl: async (): Promise<SendResult> => ({ ok: true, status: 201 }),
}));

vi.mock('@sdk/transport/sendReport', () => ({
  sendReport: (): Promise<SendResult> => {
    transport.calls += 1;
    return transport.impl();
  },
}));

const { init, destroy, captureException } = await import('@m-seismo/web');
const { processInput, handleAutomaticDiagnostic } =
  await import('@sdk/reporting/runtime');

const ENDPOINT = 'https://receiver.test/api/reports';

function event(message: string): ErrorDiagnosticEvent {
  return {
    kind: 'error',
    id: `e-${message}`,
    timestamp: '2026-10-19T12:00:00.000Z',
    handled: false,
    error: { type: 'Error', message, stack: `Error: ${message}` },
  };
}

function boot(overrides: Partial<InitOptions> = {}): void {
  expect(
    init({
      endpoint: ENDPOINT,
      shortcut: false,
      ...overrides,
    }),
  ).toBe(true);
}

const flush = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  transport.calls = 0;
  transport.impl = async () => ({ ok: true, status: 201 });
  vi.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  destroy();
  vi.restoreAllMocks();
});

describe('a failing transport never throws into the host', () => {
  it('maps a rejected send to a network failure', async () => {
    boot();
    transport.impl = async () => {
      throw new TypeError('Failed to fetch');
    };

    const result = await processInput({ source: 'form', description: 'x' });

    expect(result).toMatchObject({ status: 'failed', kind: 'network' });
    expect(transport.calls).toBe(1);
  });

  it('maps a non-Error rejection to a failure without leaking the value', async () => {
    boot();
    transport.impl = () => Promise.reject('plain string rejection');

    await expect(
      processInput({ source: 'form', description: 'x' }),
    ).resolves.toMatchObject({ status: 'failed' });
  });

  it('maps a synchronous throw in send to a failure', async () => {
    boot();
    transport.impl = (() => {
      throw new Error('exploded synchronously');
    }) as unknown as () => Promise<SendResult>;

    const result = await processInput({ source: 'form', description: 'x' });
    expect(result).toMatchObject({ status: 'failed', message: 'exploded synchronously' });
  });

  it('does not retry a single failed send', async () => {
    boot();
    transport.impl = async () => ({ ok: false, kind: 'http', status: 503, message: 'HTTP 503' });

    const result = await processInput({ source: 'form', description: 'x' });

    expect(result).toMatchObject({ status: 'failed', kind: 'http', httpStatus: 503 });
    expect(transport.calls).toBe(1);
  });

  it('keeps working after a failed send', async () => {
    boot();
    transport.impl = async () => {
      throw new Error('offline');
    };
    await expect(processInput({ source: 'form', description: 'first' })).resolves.toMatchObject({
      status: 'failed',
    });

    transport.impl = async () => ({ ok: true, status: 201 });
    await expect(processInput({ source: 'form', description: 'second' })).resolves.toMatchObject({
      status: 'sent',
    });
  });
});

describe('a throwing beforeSend hook', () => {
  it('sends the unmodified report, warns once, and resolves', async () => {
    boot({
      reporting: {
        beforeSend: () => {
          throw new Error('hook bug');
        },
      },
    });

    const result = await processInput({ source: 'form', description: 'keep me' });

    expect(result.status).toBe('sent');
    expect(transport.calls).toBe(1);
    expect(console.warn).toHaveBeenCalledTimes(1);
  });
});

describe('automatic-path re-entrancy', () => {
  it('never starts a second automatic report while one is in flight', async () => {
    boot({ reporting: { autoReport: 'errors' } });
    transport.impl = () => new Promise<SendResult>(() => {});

    handleAutomaticDiagnostic(event('first failure'));
    await flush();
    handleAutomaticDiagnostic(event('second failure'));
    await flush();

    expect(transport.calls).toBe(1);
  });

  it('sends one report per distinct automatic error once idle again', async () => {
    boot({ reporting: { autoReport: 'errors' } });

    handleAutomaticDiagnostic(event('boom one'));
    await flush();
    handleAutomaticDiagnostic(event('boom two'));
    await flush();

    expect(transport.calls).toBe(2);
  });

  it('deduplicates an identical automatic error', async () => {
    boot({ reporting: { autoReport: 'errors' } });

    handleAutomaticDiagnostic(event('same'));
    await flush();
    handleAutomaticDiagnostic(event('same'));
    await flush();

    expect(transport.calls).toBe(1);
  });

  it('records but never sends when autoReport is off', async () => {
    boot();
    transport.impl = async () => ({ ok: true, status: 201 });

    handleAutomaticDiagnostic(event('quiet'));
    await flush();

    expect(transport.calls).toBe(0);
  });
});

describe('captureException() under a broken transport', () => {
  it('never throws into the caller', () => {
    boot({ reporting: { autoReport: 'off' } });
    transport.impl = async () => {
      throw new Error('offline');
    };

    expect(() => captureException(new Error('host caught this'))).not.toThrow();
  });

  it('resolves the resulting report as failed without rejecting', async () => {
    boot();
    transport.impl = async () => {
      throw new Error('offline');
    };

    const result: ProcessResult = await processInput({
      source: 'api',
      error: new Error('host caught this'),
    });
    expect(result.status).toBe('failed');
  });
});
