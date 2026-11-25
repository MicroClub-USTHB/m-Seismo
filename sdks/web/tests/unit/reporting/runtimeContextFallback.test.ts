/**
 * The reporting runtime must degrade gracefully when context capture itself
 * fails: the report still goes out with a minimal, valid context.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../src/context/getContext', () => ({
  getContext: () => {
    throw new Error('context capture exploded');
  },
}));

import { configure, resetConfig } from '../../../src/core/config';
import { processInput } from '../../../src/reporting/runtime';

interface CapturedContext {
  timestamp: string;
  application: { version?: string; environment?: string };
  url?: string;
}

function captureNextReport(): { current: CapturedContext | undefined } {
  const box: { current: CapturedContext | undefined } = { current: undefined };
  configure({
    endpoint: 'http://localhost:4010/api/reports',
    appVersion: '9.9.9',
    environment: 'acceptance',
    reporting: {
      beforeSend: (report) => {
        box.current = report.context as CapturedContext;
        return null;
      },
    },
  });
  return box;
}

afterEach(() => {
  resetConfig();
});

describe('reporting runtime context fallback', () => {
  it('uses a timestamp and the configured application fields when getContext throws', async () => {
    const box = captureNextReport();

    const result = await processInput({ source: 'form', description: 'fallback please' });

    expect(result.status).toBe('suppressed');
    expect(box.current).toBeDefined();
    expect(Number.isNaN(Date.parse(box.current!.timestamp))).toBe(false);
    expect(box.current!.application).toEqual({ version: '9.9.9', environment: 'acceptance' });
    expect(box.current!.url).toBeUndefined();
  });

  it('omits application fields that were not configured', async () => {
    const box: { current: CapturedContext | undefined } = { current: undefined };
    configure({
      endpoint: 'http://localhost:4010/api/reports',
      reporting: {
        beforeSend: (report) => {
          box.current = report.context as CapturedContext;
          return null;
        },
      },
    });

    await processInput({ source: 'form', description: 'no app metadata' });

    expect(box.current!.application).toEqual({});
    expect(Number.isNaN(Date.parse(box.current!.timestamp))).toBe(false);
  });
});
