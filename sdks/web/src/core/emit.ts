/**
 * Emits an {@link SdkEvent} to the configured `onEvent` listener.
 *
 * A throwing or missing listener must never affect the SDK.
 */
import { getConfig } from './config';
import type { SdkEvent } from './events';

export function emitEvent(event: SdkEvent): void {
  const handler = getConfig()?.onEvent;
  if (typeof handler !== 'function') return;
  try {
    handler(event);
  } catch {
    // A listener must not be able to break the SDK.
  }
}
