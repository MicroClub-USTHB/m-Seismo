import { describe, expect, it } from 'vitest';
import { boundEventSize, byteSize } from '@sdk/diagnostics/bound';
import { EVENT_MAX_BYTES, FLAG_TRUNCATED } from '@sdk/diagnostics/types';
import type {
  ErrorDiagnosticEvent,
  NetworkDiagnosticEvent,
} from '@sdk/diagnostics/types';

function errorEvent(patch: Partial<ErrorDiagnosticEvent['error']> = {}): ErrorDiagnosticEvent {
  return {
    id: 'e-1',
    kind: 'error',
    timestamp: new Date(0).toISOString(),
    error: { type: 'Error', message: 'boom', ...patch },
    handled: false,
  };
}

describe('boundEventSize', () => {
  it('returns small events unchanged', () => {
    const event = errorEvent();
    expect(boundEventSize(event)).toBe(event);
  });

  it('trims a huge stack and flags the event truncated', () => {
    const event = errorEvent({ stack: 'x'.repeat(20_000) });
    const bounded = boundEventSize(event) as ErrorDiagnosticEvent;
    expect(byteSize(bounded)).toBeLessThanOrEqual(EVENT_MAX_BYTES);
    expect(bounded.flags).toContain(FLAG_TRUNCATED);
    expect(bounded.error.type).toBe('Error');
    expect(bounded.error.message).toBe('boom');
  });

  it('keeps the headline facts when trimming a huge message and stack', () => {
    const event = errorEvent({ message: 'm'.repeat(10_000), stack: 's'.repeat(10_000) });
    const bounded = boundEventSize(event) as ErrorDiagnosticEvent;
    expect(byteSize(bounded)).toBeLessThanOrEqual(EVENT_MAX_BYTES);
    expect(bounded.error.message.length).toBeGreaterThan(0);
  });

  it('trims an oversized network URL', () => {
    const event: NetworkDiagnosticEvent = {
      id: 'n-1',
      kind: 'network',
      timestamp: new Date(0).toISOString(),
      data: {
        method: 'GET',
        url: `https://x.test/${'a'.repeat(5000)}`,
        durationMs: 5,
        category: 'http_error',
        status: 500,
      },
    };
    const bounded = boundEventSize(event) as NetworkDiagnosticEvent;
    expect(byteSize(bounded)).toBeLessThanOrEqual(EVENT_MAX_BYTES);
    expect(bounded.flags).toContain(FLAG_TRUNCATED);
    expect(bounded.data.status).toBe(500);
    expect(bounded.data.method).toBe('GET');
  });
});
