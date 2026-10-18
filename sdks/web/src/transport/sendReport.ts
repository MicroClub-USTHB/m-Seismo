/**
 * Minimal HTTP transport.
 *
 * Sends a {@link SanitizedReport} to the configured endpoint with `fetch`,
 * honouring the **ASSUMED** contract in `docs/backend-contract.md`
 * (`POST`, `application/json`, optional `X-MSeismo-Key`, `201 {id}`).
 *
 * Guarantees:
 * - NEVER throws — always resolves to a {@link SendResult}.
 * - Distinguishes HTTP error responses (e.g. 500) from network failures.
 * - Distinguishes timeouts (via `AbortController`) from other failures.
 * - Does not log the payload.
 *
 * Out of scope: retries and queueing.
 * TODO Phase 8: add retries, backoff, and an offline queue.
 */
import type { SdkConfig } from '../core/config';
import type { SanitizedReport } from '../privacy/sanitize';
import { getOriginalFetch } from '../network/originalFetch';
import type { SendResult } from './types';

/** Extracts a string `id` from an unknown parsed JSON body, if present. */
function extractId(data: unknown): string | undefined {
  if (typeof data !== 'object' || data === null) return undefined;
  const id = (data as { id?: unknown }).id;
  return typeof id === 'string' && id.length > 0 ? id : undefined;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  return String(error);
}

/**
 * POST a sanitized report. Never rejects.
 *
 * @param report output of {@link sanitize} — the transport accepts nothing else.
 * @param config validated SDK config (endpoint, timeout, optional key).
 */
export async function sendReport(
  report: SanitizedReport,
  config: SdkConfig,
): Promise<SendResult> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  // ASSUMED auth: send the public project key only when configured.
  if (config.projectKey !== undefined) {
    headers['X-MSeismo-Key'] = config.projectKey;
  }

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), config.timeoutMs);

  // Use the captured original fetch so the SDK's own upload is never observed
  // by its own fetch instrumentation (Phase 5.3).
  const fetchImpl = getOriginalFetch();
  if (typeof fetchImpl !== 'function') {
    clearTimeout(timer);
    return { ok: false, kind: 'network', message: 'fetch is not available in this environment' };
  }

  let response: Response;
  try {
    response = await fetchImpl.call(globalThis, config.endpoint, {
      method: 'POST',
      headers,
      body: JSON.stringify(report),
      signal: controller.signal,
      mode: 'cors',
    });
  } catch (error) {
    if (controller.signal.aborted) {
      return {
        ok: false,
        kind: 'timeout',
        message: `Request timed out after ${config.timeoutMs}ms`,
      };
    }
    return { ok: false, kind: 'network', message: errorMessage(error) };
  } finally {
    clearTimeout(timer);
  }

  if (!response.ok) {
    return {
      ok: false,
      kind: 'http',
      status: response.status,
      message: `HTTP ${response.status} from ${config.endpoint}`,
    };
  }

  let data: unknown;
  try {
    data = await response.json();
  } catch {
    return {
      ok: false,
      kind: 'invalid-response',
      status: response.status,
      message: 'Success response was not valid JSON',
    };
  }

  const id = extractId(data);
  return id === undefined
    ? { ok: true, status: response.status }
    : { ok: true, status: response.status, id };
}
