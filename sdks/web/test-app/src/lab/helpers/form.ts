/**
 * Drives the SDK's shadow-DOM bug-report form from the lab.
 *
 * The shadow root is `mode: 'open'`, so the lab can reach it directly. These
 * helpers are only used by scenarios/tests; they never touch SDK internals.
 */
import { openReportForm } from '@m-seismo/web';
import { waitFor } from './waiters';

const HOST_SELECTOR = '[data-mseismo-form]';

function host(): HTMLElement | null {
  return document.querySelector<HTMLElement>(HOST_SELECTOR);
}

function root(): ShadowRoot | null {
  return host()?.shadowRoot ?? null;
}

function el<T extends Element>(testId: string): T | null {
  return root()?.querySelector<T>(`[data-testid="${testId}"]`) ?? null;
}

/** Opens the form and waits for the shadow root to exist. */
export async function openFormAndWait(): Promise<boolean> {
  openReportForm();
  return waitFor(() => root() !== null, 3_000);
}

/** Fills the description and submits the form. */
export async function fillAndSubmit(description: string): Promise<void> {
  const textarea = el<HTMLTextAreaElement>('mseismo-description');
  if (textarea === null) throw new Error('form textarea not found');
  textarea.value = description;
  textarea.dispatchEvent(new Event('input', { bubbles: true }));
  const form = root()?.querySelector('form');
  if (form === null || form === undefined) throw new Error('form element not found');
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
}

/** Opens the form, fills the description and submits. */
export async function openFillSubmit(description: string): Promise<void> {
  const opened = await openFormAndWait();
  if (!opened) throw new Error('form did not open');
  await fillAndSubmit(description);
}

/** The current inline form message text (empty when none). */
export function formMessage(): string {
  return el<HTMLElement>('mseismo-message')?.textContent ?? '';
}

/** The current description textarea value (for "text preserved" checks). */
export function formDescription(): string {
  return el<HTMLTextAreaElement>('mseismo-description')?.value ?? '';
}

/** The send button's current label ("Send report" / "Sending…" / "Try again" / "Sent"). */
export function formSendLabel(): string {
  return el<HTMLButtonElement>('mseismo-send')?.textContent ?? '';
}

/** Clicks the send/retry button. */
export function clickSend(): void {
  el<HTMLButtonElement>('mseismo-send')?.click();
}

/** The file info line (name + size) shown after a successful pick. */
export function formFileInfo(): string {
  return el<HTMLElement>('mseismo-file-info')?.textContent ?? '';
}

/** Picks a file in the form's file input and waits for the change handler. */
export async function pickFile(file: File): Promise<boolean> {
  const input = el<HTMLInputElement>('mseismo-screenshot');
  if (input === null) return false;
  const transfer = new DataTransfer();
  transfer.items.add(file);
  input.files = transfer.files;
  input.dispatchEvent(new Event('change', { bubbles: true }));
  await waitFor(() => true, 0);
  // Give the async file reader a moment to settle by polling the info/message.
  await waitFor(() => formFileInfo().length > 0 || formMessage().length > 0, 2_000);
  return true;
}

/** Closes (cancels) the open form. */
export function cancelForm(): void {
  el<HTMLButtonElement>('mseismo-cancel')?.click();
}

/** True once no form host is attached (e.g. a successful send auto-closes it). */
export async function waitForFormClosed(timeoutMs = 3_000): Promise<boolean> {
  return waitFor(() => host() === null, timeoutMs);
}

/** Builds a small valid PNG file of the given name. */
export function pngFile(name = 'shot.png'): File {
  const base64 =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==';
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return new File([bytes], name, { type: 'image/png' });
}

/** Builds an oversized (> 1 MiB) PNG-typed file without real image content. */
export function oversizedPngFile(name = 'big.png'): File {
  const bytes = new Uint8Array(1_100_000);
  bytes[0] = 0x89;
  return new File([bytes], name, { type: 'image/png' });
}
