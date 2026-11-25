/**
 * Shared helpers for the A–H acceptance suite.
 *
 * The acceptance suite drives the *real* test app (`/`) against the mock
 * receiver over real cross-origin HTTP. Every helper here is deliberately thin:
 * it uses only the app's `data-testid` hooks and the receiver's `__` endpoints,
 * never SDK internals.
 */
import { expect } from '@playwright/test';
import type { APIRequestContext, Page } from '@playwright/test';

export const RECEIVER = 'http://localhost:4010';
export const ENDPOINT = `${RECEIVER}/api/reports`;

export type ReceiverMode =
  | 'ok'
  | 'fail500'
  | 'fail413'
  | 'slow'
  | 'offline'
  | 'rateLimited';

export interface StoredReport {
  readonly reportId: string;
  readonly schemaVersion: number;
  readonly type: string;
  readonly trigger: string;
  readonly description: string | null;
  readonly error: {
    readonly type: string;
    readonly message: string;
    readonly stack?: string;
    readonly fingerprint: string;
    readonly occurrences: number;
    readonly handled?: boolean;
  } | null;
  readonly breadcrumbs: ReadonlyArray<{
    readonly category: string;
    readonly message: string;
    readonly level?: string;
    readonly data?: Record<string, unknown>;
  }>;
  readonly networkFailures: ReadonlyArray<{
    readonly method: string;
    readonly url: string;
    readonly status?: number;
  }>;
  readonly screenshot: {
    readonly name: string;
    readonly mimeType: string;
    readonly sizeBytes: number;
    readonly dataBase64Length?: number;
  } | null;
  readonly context: {
    readonly url?: string;
    readonly timestamp: string;
    readonly application: Record<string, unknown>;
    readonly browser?: { readonly name?: string; readonly version?: string };
    readonly platform?: string;
    readonly viewport?: { readonly width: number; readonly height: number };
  };
}

/** Clears stored reports + counters and returns the receiver to `ok` mode. */
export async function resetReceiver(request: APIRequestContext): Promise<void> {
  const response = await request.post(`${RECEIVER}/__reset`);
  expect(response.ok(), 'receiver reset failed').toBe(true);
}

/** Changes the receiver's behaviour for the next requests. */
export async function setMode(
  request: APIRequestContext,
  mode: ReceiverMode,
  delayMs?: number,
): Promise<void> {
  const response = await request.post(`${RECEIVER}/__mode`, {
    data: delayMs === undefined ? { mode } : { mode, delayMs },
  });
  expect(response.ok(), `setting receiver mode to ${mode} failed`).toBe(true);
}

/** Reads every stored report (screenshot base64 replaced by its length). */
export async function getReports(request: APIRequestContext): Promise<StoredReport[]> {
  const response = await request.get(`${RECEIVER}/__reports`);
  expect(response.ok(), 'reading stored reports failed').toBe(true);
  const body = (await response.json()) as { reports: StoredReport[] };
  return body.reports;
}

export async function openForm(page: Page): Promise<void> {
  await page.getByTestId('open-form').click();
  await expect(page.getByTestId('mseismo-dialog')).toBeVisible();
}

export async function fillDescription(page: Page, text: string): Promise<void> {
  await page.getByTestId('mseismo-description').fill(text);
}

/** Fills the description and clicks Send (or Try again). */
export async function submitDescription(page: Page, text: string): Promise<void> {
  await fillDescription(page, text);
  await page.getByTestId('mseismo-send').click();
}

/** Waits for the SDK's success event in the app's event log. */
export async function waitForSuccess(page: Page): Promise<void> {
  await expect(page.getByTestId('event-log')).toContainText('sdk event: submit:success');
}

/** Waits for the SDK's failure event and returns the matching log line. */
export async function waitForFailure(page: Page): Promise<string> {
  const log = page.getByTestId('event-log');
  await expect(log).toContainText('sdk event: submit:failure');
  const text = await log.innerText();
  const line = text
    .split('\n')
    .filter((entry) => entry.includes('sdk event: submit:failure'))
    .pop();
  expect(line, 'failure event missing from the log').toBeDefined();
  return line!;
}

/** Waits for the dialog to disappear after a successful send. */
export async function waitForFormClosed(page: Page): Promise<void> {
  await expect(page.getByTestId('mseismo-dialog')).toBeHidden();
}

/** All `reportId`s mentioned by `submit:start` events, in order. */
export async function submitStartIds(page: Page): Promise<string[]> {
  const text = await page.getByTestId('event-log').innerText();
  return [...text.matchAll(/submit:start id=([0-9a-f-]+)/gi)].map((match) => match[1]!);
}

/** A tiny valid 1×1 PNG. */
export const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==';

export function pngBuffer(): Buffer {
  return Buffer.from(PNG_BASE64, 'base64');
}
