/**
 * Error-laboratory scenario model (plan section 4, E01–E25).
 *
 * A scenario is pure data plus three async hooks. The runner guarantees a clean
 * environment before each scenario and always maps a verifier result to a
 * {@link LabResult}; a verifier must never throw.
 */
import type { DiagnosticEvent } from '@sdk/diagnostics/types';
import type { DiagnosticsSelection } from '@sdk/diagnostics/select';
import type { LimiterStats } from '@sdk/reporting/limiter';
import type {
  DiagnosticsOptions,
  InitOptions,
  LauncherOptions,
  PrivacyOptions,
  ReportingOptions,
  SdkConfig,
} from '@m-seismo/web';
import type { ReceiverClient } from './helpers/receiver';
import type { WarningCapture } from './helpers/console';

/** The five scenario groups in the lab. */
export type LabGroup = 'errors' | 'network' | 'reporting' | 'lifecycle' | 'ui';

/** What the plan expects the SDK to be able to do for this scenario. */
export type LabExpected =
  | 'DETECTED_AUTO'
  | 'MANUAL_ONLY'
  | 'NOT_SUPPORTED'
  | 'LIMITED'
  | 'KNOWN_GAP';

/** How a scenario can be driven. */
export type LabMode = 'inpage' | 'assisted';

/** Outcome of a single verification. */
export type VerdictStatus = 'pass' | 'fail' | 'info';

export interface Verdict {
  readonly status: VerdictStatus;
  /** Human-readable summary of what was actually observed. */
  readonly observed: string;
  /** Optional structured proof (buffer excerpts, stored reports, …). */
  readonly evidence?: unknown;
}

/** Config overrides applied when the runner re-initialises the SDK. */
export interface LabSdkConfig {
  readonly endpoint?: string;
  readonly appVersion?: string;
  readonly environment?: string;
  readonly timeoutMs?: number;
  readonly shortcut?: string | false;
  readonly launcher?: boolean | LauncherOptions;
  readonly screenshot?: boolean;
  readonly privacy?: PrivacyOptions;
  readonly diagnostics?: DiagnosticsOptions;
  readonly reporting?: ReportingOptions;
}

/** Runtime services handed to every scenario hook. */
export interface LabContext {
  /** The SDK ingest endpoint the lab is configured with. */
  readonly endpoint: string;
  /** Origin of the mock receiver, e.g. `http://localhost:4010`. */
  readonly receiverBase: string;
  /** Client for the mock receiver inspection/mode routes. */
  readonly receiver: ReceiverClient;
  /** Buffered diagnostics snapshot (test-only internal accessor). */
  snapshot(): readonly DiagnosticEvent[];
  /** What would currently be attached to a report. */
  selection(): DiagnosticsSelection;
  /** Number of buffered diagnostics. */
  count(): number;
  /** Processor dedup/limit stats. */
  stats(): LimiterStats & { readonly processing: boolean };
  /** Current SDK config, or null when disabled. */
  config(): SdkConfig | null;
  /** Re-initialise the SDK with the lab defaults plus the given overrides. */
  init(overrides?: LabSdkConfig): boolean;
  /** Tear the SDK down (clears the buffer and dedup/limit state). */
  destroy(): void;
  /** Clear only the diagnostics buffer. */
  clearDiagnostics(): void;
  /** Console warning capture, controlled by the runner. */
  readonly warnings: WarningCapture;
}

export interface LabScenario {
  /** `E01` … `E25`. */
  readonly id: string;
  readonly title: string;
  readonly group: LabGroup;
  readonly mode: LabMode;
  readonly expected: LabExpected;
  /** What the browser hides, caveats, and how to read the result. */
  readonly notes: string;
  /** Applied by the runner before `setup`. */
  readonly config?: LabSdkConfig;
  /** Extra per-run preparation (e.g. receiver mode, seeded state). */
  setup?(ctx: LabContext): Promise<void>;
  /** The deliberate trigger. */
  run(ctx: LabContext): Promise<void>;
  /** Reads SDK + receiver state. MUST NOT throw; return `fail` instead. */
  verify(ctx: LabContext): Promise<Verdict>;
  /** Steps a human follows for assisted scenarios. */
  readonly manualSteps?: readonly string[];
}

/** A stored lab result, also exported on `window.__labResults`. */
export interface LabResult {
  readonly id: string;
  readonly title: string;
  readonly group: LabGroup;
  readonly mode: LabMode;
  readonly expected: LabExpected;
  readonly status: VerdictStatus | 'needs-manual';
  readonly observed: string;
  readonly evidence?: unknown;
}
