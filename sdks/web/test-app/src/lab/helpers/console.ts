/**
 * Captures `console.warn`/`console.error` output while a scenario runs.
 *
 * The SDK signals every configuration problem with one `console.warn` (see
 * `src/core/log.ts`) and never throws. E19 asserts those warnings are emitted
 * exactly once per problem.
 */

export interface CapturedConsoleMessage {
  readonly level: 'warn' | 'error';
  readonly text: string;
}

export class WarningCapture {
  private originalWarn: typeof console.warn | null = null;
  private originalError: typeof console.error | null = null;
  private captured: CapturedConsoleMessage[] = [];

  /** Begins capturing; safe to call twice (idempotent). */
  start(): void {
    if (this.originalWarn !== null) return;
    this.originalWarn = console.warn.bind(console);
    this.originalError = console.error.bind(console);
    console.warn = ((...args: unknown[]): void => {
      this.captured.push({ level: 'warn', text: args.map(String).join(' ') });
    }) as typeof console.warn;
    console.error = ((...args: unknown[]): void => {
      this.captured.push({ level: 'error', text: args.map(String).join(' ') });
    }) as typeof console.error;
  }

  /** Stops capturing and restores the originals. */
  stop(): void {
    if (this.originalWarn !== null) {
      console.warn = this.originalWarn;
      this.originalWarn = null;
    }
    if (this.originalError !== null) {
      console.error = this.originalError;
      this.originalError = null;
    }
  }

  /** Clears captured messages without stopping. */
  clear(): void {
    this.captured = [];
  }

  get messages(): readonly CapturedConsoleMessage[] {
    return this.captured;
  }

  /** Captured messages whose text contains `needle` (case-sensitive). */
  matching(needle: string): CapturedConsoleMessage[] {
    return this.captured.filter((message) => message.text.includes(needle));
  }

  count(): number {
    return this.captured.length;
  }
}
