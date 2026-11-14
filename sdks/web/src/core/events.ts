/**
 * Lifecycle events emitted to the optional `init({ onEvent })` listener.
 *
 * These let a host application observe (and test) what the SDK is doing without
 * reading SDK internals. The `kind` on a failure mirrors the transport
 * {@link SendFailureKind}, so event names stay stable.
 */
import type { SendFailureKind } from '../transport/types';

export type SdkEvent =
  | { readonly type: 'form:open' }
  | { readonly type: 'form:close'; readonly reason: 'cancel' | 'sent' }
  | { readonly type: 'submit:start'; readonly reportId: string }
  | {
      readonly type: 'submit:success';
      readonly reportId: string;
      readonly status: number;
      readonly id?: string;
    }
  | {
      readonly type: 'submit:failure';
      readonly reportId: string;
      readonly kind: SendFailureKind;
      readonly status?: number;
      readonly message: string;
    }
  | { readonly type: 'validation:error'; readonly message: string };

export type SdkEventListener = (event: SdkEvent) => void;
