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
  /** Screenshot is OUT of scope for this contract version. */
  readonly screenshot: null;
}
