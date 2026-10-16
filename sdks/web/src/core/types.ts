/**
 * Report payload types.
 *
 * The shape below is the SDK's implementation of the **ASSUMED** draft contract
 * in `docs/backend-contract.md`. It has NOT been verified against the real Go
 * backend; field names and nesting may change.
 */

/** Schema version carried on every report. Bumped only on breaking changes. */
export const SCHEMA_VERSION = 1;

export type SchemaVersion = typeof SCHEMA_VERSION;

export interface ReportError {
  readonly type: string;
  readonly message: string;
  readonly stack?: string;
}

export interface ReportApplication {
  readonly version?: string;
  readonly environment?: string;
}

export interface ReportContext {
  /** Page URL at capture time. Empty string when there is no `location` (SSR). */
  readonly url: string;
  /** ISO 8601 timestamp, UTC. */
  readonly timestamp: string;
  readonly application: ReportApplication;
}

/**
 * A user-supplied screenshot, embedded inline as base64.
 *
 * **ASSUMED** shape (see `docs/backend-contract.md`): inline base64 in the JSON
 * body, not multipart or a separate upload. `dataBase64` carries no data-URL
 * prefix (it is the raw base64 of the file bytes).
 */
export interface ScreenshotAttachment {
  /** Original file name, e.g. `screen.png`. */
  readonly name: string;
  /** IANA media type; one of `image/png`, `image/jpeg`, `image/webp`. */
  readonly mimeType: string;
  /** Size of the decoded image in bytes. */
  readonly sizeBytes: number;
  /** Base64-encoded file bytes (no `data:` prefix). */
  readonly dataBase64: string;
}

export interface ReportPayload {
  readonly schemaVersion: SchemaVersion;
  readonly sdkVersion: string;
  readonly reportId: string;
  readonly description: string;
  /** `null` is valid and required for manual reports. */
  readonly error: ReportError | null;
  readonly context: ReportContext;
  /** Empty in this version; shape TBD. */
  readonly breadcrumbs: readonly unknown[];
  /** Empty in this version; shape TBD. */
  readonly networkFailures: readonly unknown[];
  /** Optional screenshot attachment, or `null` when none was provided. */
  readonly screenshot: ScreenshotAttachment | null;
}
