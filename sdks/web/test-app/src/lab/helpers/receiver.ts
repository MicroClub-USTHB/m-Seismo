/**
 * Mock-receiver client (the stand-in for the Go backend).
 *
 * Thin, typed wrapper over the `/__reset`, `/__mode`, `/__reports` and
 * `/__stats` routes. Every method rejects on a non-OK response so callers can
 * decide whether that matters (offline scenarios tolerate it).
 */

/** A stored report as returned by `/__reports` (screenshot data redacted). */
export interface StoredScreenshotMeta {
  readonly name: string;
  readonly mimeType: string;
  readonly sizeBytes: number;
  readonly dataBase64Length?: number;
}

export interface StoredError {
  readonly type?: string;
  readonly message?: string;
  readonly stack?: string;
  readonly flags?: readonly string[];
  readonly handled?: boolean;
  readonly context?: Record<string, unknown>;
  readonly fingerprint?: string;
  readonly occurrences?: number;
}

export interface StoredReport {
  readonly schemaVersion?: number;
  readonly sdkVersion?: string;
  readonly reportId?: string;
  readonly type?: string;
  readonly trigger?: string;
  readonly description?: string | null;
  readonly error?: StoredError | null;
  readonly context?: Record<string, unknown>;
  readonly breadcrumbs?: readonly unknown[];
  readonly networkFailures?: readonly unknown[];
  readonly screenshot?: StoredScreenshotMeta | null;
  readonly [key: string]: unknown;
}

export interface ReportsResponse {
  readonly count: number;
  readonly reports: StoredReport[];
}

export interface ReceiverStats {
  readonly mode: string;
  readonly preflightCount: number;
  readonly requestCount: number;
  readonly storedCount: number;
  readonly byType: Record<string, number>;
  readonly byFingerprint: Record<string, number>;
}

export type ReceiverMode =
  | 'ok'
  | 'fail500'
  | 'fail413'
  | 'slow'
  | 'offline'
  | 'rateLimited';

export class ReceiverClient {
  constructor(readonly base: string) {}

  private async request(path: string, init?: RequestInit): Promise<Response> {
    const response = await fetch(`${this.base}${path}`, init);
    return response;
  }

  /** Clears stored reports + counters and resets mode to `ok`. */
  async reset(): Promise<void> {
    const response = await this.request('/__reset', { method: 'POST' });
    if (!response.ok) throw new Error(`receiver reset failed: HTTP ${response.status}`);
  }

  async health(): Promise<boolean> {
    try {
      const response = await this.request('/__health');
      return response.ok;
    } catch {
      return false;
    }
  }

  async setMode(mode: ReceiverMode, opts: { delayMs?: number } = {}): Promise<void> {
    const body: Record<string, unknown> = { mode };
    if (opts.delayMs !== undefined) body['delayMs'] = opts.delayMs;
    const response = await this.request('/__mode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!response.ok) throw new Error(`receiver setMode(${mode}) failed: HTTP ${response.status}`);
  }

  async getReports(options: { full?: boolean; type?: string } = {}): Promise<ReportsResponse> {
    const params = new URLSearchParams();
    if (options.full) params.set('full', '1');
    if (options.type !== undefined) params.set('type', options.type);
    const suffix = params.size > 0 ? `?${params.toString()}` : '';
    const response = await this.request(`/__reports${suffix}`);
    if (!response.ok) throw new Error(`receiver getReports failed: HTTP ${response.status}`);
    return (await response.json()) as ReportsResponse;
  }

  async getStats(): Promise<ReceiverStats> {
    const response = await this.request('/__stats');
    if (!response.ok) throw new Error(`receiver getStats failed: HTTP ${response.status}`);
    return (await response.json()) as ReceiverStats;
  }
}
