/**
 * Screenshot file handling for the bug-report form.
 *
 * The user picks an image file; we validate its type and size and encode it as
 * base64 for the **ASSUMED** inline-attachment contract. There is NO automatic
 * screen capture — a screenshot is only ever attached when the user chooses one.
 */
import type { ScreenshotAttachment } from '../core/types';

/** Maximum accepted screenshot size, in bytes (1 MiB). */
export const MAX_SCREENSHOT_BYTES = 1_048_576;

/** Media types accepted for screenshots. */
export const ALLOWED_SCREENSHOT_MIME: readonly string[] = [
  'image/png',
  'image/jpeg',
  'image/webp',
];

export type ScreenshotValidation =
  | { readonly ok: true; readonly attachment: ScreenshotAttachment }
  | { readonly ok: false; readonly message: string };

/** Human-readable byte size, e.g. `512 KB`. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** Base64-encodes an `ArrayBuffer` without blowing the argument limit. */
export function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}

/**
 * Validates and encodes a picked file. Never throws; returns a typed result.
 */
export async function fileToAttachment(file: File): Promise<ScreenshotValidation> {
  if (!ALLOWED_SCREENSHOT_MIME.includes(file.type)) {
    return {
      ok: false,
      message: 'Screenshot must be a PNG, JPEG, or WebP image.',
    };
  }
  if (file.size > MAX_SCREENSHOT_BYTES) {
    return {
      ok: false,
      message: `Screenshot must be ${formatBytes(MAX_SCREENSHOT_BYTES)} or smaller.`,
    };
  }
  let dataBase64: string;
  try {
    dataBase64 = arrayBufferToBase64(await file.arrayBuffer());
  } catch {
    return { ok: false, message: 'The screenshot could not be read.' };
  }
  return {
    ok: true,
    attachment: {
      name: file.name,
      mimeType: file.type,
      sizeBytes: file.size,
      dataBase64,
    },
  };
}
