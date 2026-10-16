/**
 * Bug-report form controller.
 *
 * Owns a single dialog rendered inside an **open shadow root**, so the host
 * page's CSS cannot leak in (and the SDK's markup cannot leak out). The form
 * collects a description and an optional screenshot, builds a report, passes it
 * through the privacy boundary, and sends it with the existing transport.
 *
 * Design notes:
 * - The dialog is a `<div role="dialog" aria-modal="true">` (not native
 *   `<dialog>`) with a manual focus trap. Chosen for consistent behaviour across
 *   browsers and for testability without a full browser.
 * - The `reportId` is generated on open and reused for every retry within that
 *   session, so a resend is idempotent. It is regenerated on the next open.
 * - Closing without sending discards the typed text.
 * - The transport is injectable so the flow can be unit-tested without network.
 */
import { getConfig } from '../core/config';
import type { SdkConfig } from '../core/config';
import { emitEvent } from '../core/emit';
import { buildReport, createReportId } from '../core/reportBuilder';
import { matchesShortcut } from '../core/shortcut';
import type { ScreenshotAttachment } from '../core/types';
import { sanitize } from '../privacy/sanitize';
import type { SanitizedReport } from '../privacy/sanitize';
import { sendReport } from '../transport/sendReport';
import type { SendFailure, SendResult } from '../transport/types';
import { fileToAttachment, formatBytes } from './screenshot';
import { validateDescription } from './validation';

export interface FormControllerDeps {
  /** Transport override (defaults to the real {@link sendReport}). */
  readonly send?: (report: SanitizedReport, config: SdkConfig) => Promise<SendResult>;
}

export interface FormController {
  open(): void;
  close(reason?: 'cancel' | 'sent'): void;
  isOpen(): boolean;
  destroy(): void;
}

const FOCUSABLE_SELECTOR =
  'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

const STYLES = `
  :host { all: initial; }
  * { box-sizing: border-box; }
  .backdrop {
    position: fixed; inset: 0; z-index: 2147483646;
    background: rgba(15, 17, 21, 0.55);
  }
  .dialog {
    position: fixed; z-index: 2147483647;
    top: 50%; left: 50%; transform: translate(-50%, -50%);
    width: min(480px, calc(100vw - 32px));
    max-height: calc(100vh - 32px); overflow: auto;
    padding: 20px; border-radius: 12px;
    background: #ffffff; color: #14161a;
    font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
    font-size: 14px; line-height: 1.45;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.35);
  }
  .title { margin: 0 0 14px; font-size: 17px; font-weight: 700; color: #14161a; }
  .field { display: block; margin-bottom: 6px; font-size: 13px; font-weight: 600; color: #333a44; }
  textarea {
    display: block; width: 100%; min-height: 108px; resize: vertical;
    padding: 10px; margin-bottom: 14px;
    border: 1px solid #c7ccd4; border-radius: 8px;
    background: #ffffff; color: #14161a;
    font-family: inherit; font-size: 14px; line-height: 1.45;
  }
  textarea:focus, input[type="file"]:focus {
    outline: 2px solid #2f6fed; outline-offset: 1px;
  }
  input[type="file"] {
    display: block; width: 100%; margin-bottom: 6px;
    font-family: inherit; font-size: 13px; color: #333a44;
  }
  .file-info { margin: 0 0 12px; font-size: 12px; color: #5b6470; }
  .message { margin: 0 0 12px; min-height: 1.2em; font-size: 13px; }
  .message.error { color: #c0392b; }
  .message.success { color: #1a7f37; }
  .actions { display: flex; justify-content: flex-end; gap: 8px; }
  .actions button {
    padding: 9px 15px; border-radius: 8px; cursor: pointer;
    font-family: inherit; font-size: 14px; font-weight: 600;
  }
  .cancel { border: 1px solid #c7ccd4; background: #ffffff; color: #333a44; }
  .send { border: 1px solid #2f6fed; background: #2f6fed; color: #ffffff; }
  .send:disabled, .cancel:disabled { opacity: 0.6; cursor: default; }
  @media (prefers-color-scheme: dark) {
    .dialog { background: #1d2026; color: #eef1f5; }
    .title { color: #eef1f5; }
    .field { color: #c7ccd4; }
    textarea { background: #14161a; color: #eef1f5; border-color: #333a44; }
    input[type="file"] { color: #c7ccd4; }
    .file-info { color: #9aa4b2; }
    .cancel { border-color: #333a44; background: #1d2026; color: #eef1f5; }
  }
`;

function template(screenshotEnabled: boolean): string {
  const screenshotField = screenshotEnabled
    ? `
      <label class="field" for="mseismo-file">Screenshot (optional)</label>
      <input
        id="mseismo-file"
        data-testid="mseismo-screenshot"
        type="file"
        accept="image/png,image/jpeg,image/webp"
      />
      <p class="file-info" data-testid="mseismo-file-info"></p>
    `
    : '';

  return `
    <style>${STYLES}</style>
    <div class="backdrop" data-testid="mseismo-backdrop"></div>
    <div
      class="dialog"
      data-testid="mseismo-dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="mseismo-title"
    >
      <h2 class="title" id="mseismo-title">Report a problem</h2>
      <form class="form" novalidate>
        <label class="field" for="mseismo-desc">What went wrong?</label>
        <textarea
          id="mseismo-desc"
          data-testid="mseismo-description"
          maxlength="5000"
          rows="5"
          placeholder="Describe what you did and what happened…"
        ></textarea>
        ${screenshotField}
        <p class="message" data-testid="mseismo-message" role="status" aria-live="polite"></p>
        <div class="actions">
          <button type="button" class="cancel" data-testid="mseismo-cancel">Cancel</button>
          <button type="submit" class="send" data-testid="mseismo-send">Send report</button>
        </div>
      </form>
    </div>
  `;
}

function failureMessage(result: SendFailure): string {
  switch (result.kind) {
    case 'network':
      return 'Could not reach the server. Check your connection and try again.';
    case 'timeout':
      return 'The request timed out. Please try again.';
    case 'http':
      return `The server rejected the report (HTTP ${result.status ?? 'error'}).`;
    case 'invalid-response':
      return 'The server response was not understood. Please try again.';
  }
}

export function createFormController(
  deps: FormControllerDeps = {},
): FormController {
  const send = deps.send ?? sendReport;
  const config = getConfig();
  const screenshotEnabled = config?.screenshot ?? true;

  const host = document.createElement('div');
  host.setAttribute('data-mseismo-form', '');
  const shadow = host.attachShadow({ mode: 'open' });
  shadow.innerHTML = template(screenshotEnabled);

  const query = <T extends Element>(selector: string): T => {
    const el = shadow.querySelector<T>(selector);
    if (el === null) {
      throw new Error(`m-seismo form: missing element ${selector}`);
    }
    return el;
  };

  const dialog = query<HTMLElement>('.dialog');
  const formEl = query<HTMLFormElement>('.form');
  const textarea = query<HTMLTextAreaElement>('[data-testid="mseismo-description"]');
  const messageEl = query<HTMLElement>('[data-testid="mseismo-message"]');
  const sendBtn = query<HTMLButtonElement>('[data-testid="mseismo-send"]');
  const cancelBtn = query<HTMLButtonElement>('[data-testid="mseismo-cancel"]');
  const fileInput = screenshotEnabled
    ? query<HTMLInputElement>('[data-testid="mseismo-screenshot"]')
    : null;
  const fileInfo = screenshotEnabled
    ? query<HTMLElement>('[data-testid="mseismo-file-info"]')
    : null;

  let open = false;
  let busy = false;
  let reportId: string | null = null;
  let screenshot: ScreenshotAttachment | null = null;
  let screenshotError: string | null = null;
  let closeTimer: ReturnType<typeof setTimeout> | null = null;

  function setMessage(kind: 'error' | 'success' | null, text: string): void {
    messageEl.className = kind === null ? 'message' : `message ${kind}`;
    messageEl.textContent = text;
  }

  function setBusy(next: boolean): void {
    busy = next;
    sendBtn.disabled = next;
    cancelBtn.disabled = next;
    sendBtn.textContent = next ? 'Sending…' : 'Send report';
  }

  function reset(): void {
    textarea.value = '';
    if (fileInput) fileInput.value = '';
    screenshot = null;
    screenshotError = null;
    reportId = null;
    if (fileInfo) fileInfo.textContent = '';
    setMessage(null, '');
    setBusy(false);
    if (closeTimer !== null) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }
  }

  function focusable(): HTMLElement[] {
    return Array.from(shadow.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
      (el) => !el.hasAttribute('disabled') && el.tabIndex !== -1,
    );
  }

  function trapFocus(event: KeyboardEvent): void {
    const elements = focusable();
    if (elements.length === 0) return;
    const first = elements[0]!;
    const last = elements[elements.length - 1]!;
    const active = shadow.activeElement;
    if (event.shiftKey) {
      if (active === first || active === null) {
        event.preventDefault();
        last.focus();
      }
    } else if (active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  async function onSubmit(event: Event): Promise<void> {
    event.preventDefault();
    await submit();
  }

  async function submit(): Promise<void> {
    if (busy) return;

    const validation = validateDescription(textarea.value);
    if (!validation.ok) {
      setMessage('error', validation.message);
      emitEvent({ type: 'validation:error', message: validation.message });
      textarea.focus();
      return;
    }
    if (screenshotError !== null) {
      setMessage('error', screenshotError);
      return;
    }

    const current = getConfig();
    if (current === null) {
      setMessage('error', 'The SDK is not configured.');
      return;
    }

    if (reportId === null) reportId = createReportId();
    const id = reportId;
    setMessage(null, '');
    setBusy(true);
    emitEvent({ type: 'submit:start', reportId: id });

    const payload = buildReport(
      { description: validation.value, screenshot },
      { reportId: id },
    );

    const result = await send(sanitize(payload), current);
    if (!result.ok) {
      setBusy(false);
      setMessage('error', failureMessage(result));
      sendBtn.textContent = 'Try again';
      emitEvent({
        type: 'submit:failure',
        reportId: id,
        kind: result.kind,
        ...(result.status !== undefined ? { status: result.status } : {}),
        message: result.message,
      });
      return;
    }

    emitEvent({
      type: 'submit:success',
      reportId: id,
      status: result.status,
      ...(result.id !== undefined ? { id: result.id } : {}),
    });
    sendBtn.disabled = true;
    cancelBtn.disabled = true;
    sendBtn.textContent = 'Sent';
    setMessage('success', 'Thanks! Your report was sent.');
    closeTimer = setTimeout(() => close('sent'), 900);
  }

  async function onFileChange(): Promise<void> {
    screenshotError = null;
    screenshot = null;
    if (fileInfo) fileInfo.textContent = '';
    const file = fileInput?.files?.[0] ?? null;
    if (file === null) return;

    const result = await fileToAttachment(file);
    if (!result.ok) {
      screenshotError = result.message;
      setMessage('error', result.message);
      return;
    }
    screenshot = result.attachment;
    if (fileInfo) {
      fileInfo.textContent = `${file.name} · ${formatBytes(file.size)}`;
    }
    setMessage(null, '');
  }

  function onDialogKeydown(event: KeyboardEvent): void {
    if (!open) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      close('cancel');
      return;
    }
    if (event.key === 'Tab') {
      trapFocus(event);
      return;
    }
    const spec = getConfig()?.shortcut ?? false;
    if (spec !== false && matchesShortcut(event, spec)) {
      event.preventDefault();
      void submit();
    }
  }

  formEl.addEventListener('submit', (event) => void onSubmit(event));
  cancelBtn.addEventListener('click', () => close('cancel'));
  dialog.addEventListener('keydown', onDialogKeydown);
  fileInput?.addEventListener('change', () => void onFileChange());

  function openForm(): void {
    if (open) return;
    open = true;
    reset();
    document.body.appendChild(host);
    emitEvent({ type: 'form:open' });
    const elements = focusable();
    (elements.find((el) => el === textarea) ?? elements[0] ?? dialog).focus();
  }

  function close(reason: 'cancel' | 'sent' = 'cancel'): void {
    if (!open) return;
    open = false;
    if (closeTimer !== null) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }
    host.remove();
    emitEvent({ type: 'form:close', reason });
  }

  function destroy(): void {
    if (closeTimer !== null) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }
    open = false;
    host.remove();
  }

  return {
    open: openForm,
    close,
    isOpen: () => open,
    destroy,
  };
}
