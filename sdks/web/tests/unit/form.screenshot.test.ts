/**
 * Screenshot attachment handling: validation, size limit, encoding and the
 * human-readable byte formatter. Pure logic, so it runs in the Node
 * environment (Node's `File`/`btoa` stand in for the browser's).
 */
import { describe, expect, it } from 'vitest';
import {
  ALLOWED_SCREENSHOT_MIME,
  MAX_SCREENSHOT_BYTES,
  arrayBufferToBase64,
  fileToAttachment,
  formatBytes,
} from '@sdk/form/screenshot';

function file(bytes: Uint8Array, name: string, type: string): File {
  return new File([bytes as unknown as BlobPart], name, { type });
}

describe('formatBytes', () => {
  it('formats bytes', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
  });

  it('formats kilobytes', () => {
    expect(formatBytes(4 * 1024)).toBe('4 KB');
    expect(formatBytes(1_500)).toBe('1 KB');
  });

  it('formats megabytes', () => {
    expect(formatBytes(MAX_SCREENSHOT_BYTES)).toBe('1.0 MB');
    expect(formatBytes(3 * 1024 * 1024)).toBe('3.0 MB');
  });
});

describe('arrayBufferToBase64', () => {
  it('round-trips arbitrary bytes', () => {
    const bytes = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10]);
    const encoded = arrayBufferToBase64(bytes.buffer as ArrayBuffer);
    expect(atob(encoded)).toBe(String.fromCharCode(...bytes));
  });

  it('handles a payload larger than one chunk', () => {
    const bytes = new Uint8Array(50_000).fill(7);
    const encoded = arrayBufferToBase64(bytes.buffer as ArrayBuffer);
    const decoded = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
    expect(decoded).toHaveLength(50_000);
    expect(decoded.every((b) => b === 7)).toBe(true);
  });
});

describe('fileToAttachment', () => {
  it('accepts an allowed image and encodes it', async () => {
    const result = await fileToAttachment(file(new Uint8Array([1, 2, 3]), 'shot.png', 'image/png'));
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.attachment).toMatchObject({
      name: 'shot.png',
      mimeType: 'image/png',
      sizeBytes: 3,
    });
    expect(atob(result.attachment.dataBase64)).toBe('\x01\x02\x03');
  });

  it('rejects a disallowed media type', async () => {
    const result = await fileToAttachment(file(new Uint8Array([1]), 'notes.txt', 'text/plain'));
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.message).toContain('PNG, JPEG, or WebP');
  });

  it('rejects a file larger than the size limit', async () => {
    const oversized = new Uint8Array(MAX_SCREENSHOT_BYTES + 1);
    const result = await fileToAttachment(file(oversized, 'big.png', 'image/png'));
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.message).toContain('1.0 MB or smaller');
  });

  it('reports a read failure instead of throwing', async () => {
    const broken = file(new Uint8Array([1]), 'shot.png', 'image/png');
    Object.defineProperty(broken, 'arrayBuffer', {
      value: () => Promise.reject(new Error('disk gone')),
      configurable: true,
    });
    const result = await fileToAttachment(broken);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.message).toBe('The screenshot could not be read.');
  });

  it('only lists the three supported media types', () => {
    expect([...ALLOWED_SCREENSHOT_MIME]).toEqual(['image/png', 'image/jpeg', 'image/webp']);
  });
});
