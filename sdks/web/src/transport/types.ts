/**
 * Transport result types. The transport NEVER throws; it always resolves to a
 * {@link SendResult}.
 */

export type SendFailureKind =
  | 'network'
  | 'timeout'
  | 'http'
  | 'invalid-response';

export interface SendSuccess {
  readonly ok: true;
  readonly status: number;
  /** Server id, when the success response carried a usable `id`. */
  readonly id?: string;
}

export interface SendFailure {
  readonly ok: false;
  readonly kind: SendFailureKind;
  /** Present for `http` and `invalid-response` failures. */
  readonly status?: number;
  readonly message: string;
}

export type SendResult = SendSuccess | SendFailure;
