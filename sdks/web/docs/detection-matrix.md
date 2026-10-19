# Detection matrix (verified)

What the m-Seismo Web SDK actually captures, verified end-to-end in a real
browser against the local mock receiver by the **error laboratory**
(`test-app/lab.html`, scenarios E01–E25). This file records **only verified
facts**; anything unbuilt is called out explicitly.

- Verification run: Chromium and Firefox (WebKit could not run in this
  environment — missing OS libraries). Results: 24 pass, 1 expected gap.
- Categories: **AUTO** = captured and sent automatically, **MANUAL** = only in a
  user-submitted report, **NO** = not captured, **LIMITED** = captured but with
  caveats, **GAP** = known deficiency (must be closed before production).

| ID | Trigger | Category | Notes |
| --- | --- | --- | --- |
| E01 | Uncaught exception (`window.onerror`) | AUTO | Buffered; `automatic_error` when `autoReport=errors/all`. |
| E02 | Unhandled promise rejection | AUTO | Normalized like an error; same autoReport policy. |
| E03 | Null reference (`TypeError`) | AUTO | Type, message and source location preserved. |
| E04 | Undefined function (`ReferenceError`) | AUTO | Captured when thrown on the main task. |
| E05 | Exception caught by app code | MANUAL | Invisible unless the app calls `captureException()`. |
| E06 | Manual report (no error) | AUTO (form) | `type=manual`, `trigger=form`, `error=null`. |
| E07 | `captureException(err, ctx)` / `captureMessage(msg)` | AUTO | `captured_exception` / `captured_message`. |
| E08 | `console.error(...)` | NO | Deliberately not captured (not an uncaught error). |
| E09 | `fetch` with HTTP ≥ 400 | AUTO | Recorded as `http_error`, attached as `networkFailures`. |
| E10 | Offline / closed port / CORS block | LIMITED | All three look identical (`network_error`); cause is not claimed. |
| E11 | Slow failing request | AUTO | Failing request recorded with `durationMs`; slow success not recorded. |
| E12 | Cross-origin script without CORS | LIMITED | Masked as "Script error."; `cross-origin-script-error` + weak fingerprint; no stack. |
| E13 | Minified production error | LIMITED | Stack points at the minified file/column; **no** source-map resolution (server-side job). |
| E14 | Same error 500× | AUTO | Deduped to one report per fingerprint per window; `occurrences` counted. |
| E15 | Secrets in description/context/URL/headers/storage | GAP | Baseline scrubber redacts some shapes only; the description is never scrubbed. See below. |
| E16 | Oversized payload | AUTO | Oversize description/screenshot rejected inline; context/breadcrumbs bounded; receiver 413 → clear failure. |
| E17 | Backend down (drop/500/wrong port) | AUTO | Distinct failure message, text preserved, **no** automatic retry (Phase 8). |
| E18 | `init()` more than once | AUTO | Previous instance torn down first: one launcher, one form host, one listener each. |
| E19 | Invalid configuration | AUTO | One warning per problem, never throws, safe default; two silent fallbacks. |
| E20 | 500 distinct rapid errors | AUTO | Buffer bounded; sends capped per minute/session; page stays responsive. |
| E21 | Logical bug (button does nothing) | MANUAL | No error/event; only a manual report; the click is a breadcrumb. |
| E22 | Form submission fails then recovers | AUTO | 500/413/drop/timeout each give a distinct message; `reportId` stable across retry. |
| E23 | Hostile page CSS / dark mode | LIMITED | Shadow DOM isolates the dialog; **no** custom theme API (Phase 4). |
| E24 | Keyboard shortcut | LIMITED | Opens on Ctrl+Enter outside fields; typing-safe; customisable/disable-able. |
| E25 | Screenshot attach/reject/clear | AUTO | Attached with name/mime/size; wrong type/oversize rejected; no remove control or notice. |

## Not captured (by design)

- `console.error`/`console.warn` output (E08).
- Errors the application catches and does not rethrow (E05) — unless it calls
  `captureException()`.
- Logical bugs with no thrown error (E21) — only reachable via a manual report.

## Limited / caveat behaviours

- Offline vs CORS vs DNS is indistinguishable in the browser (E10).
- Cross-origin script errors are masked; the SDK flags them and uses a weak
  fingerprint (E12).
- Minified stacks are reported as-is; mapping is a backend concern (E13).
- Success with no network failure (HTTP < 400, fast) is not recorded (E11).

## Known gaps before production

1. **Privacy (E15 / Phase 6).** The baseline scrubber misses:
   - the free-text **description** is never scrubbed;
   - secrets under benign-looking keys (e.g. `api_key` in `captureException`
     context) pass through because redaction is value-pattern based;
   - request **bodies** are not inspected.
   Request headers, cookies and `localStorage` are not read by the SDK at all
   (nothing to leak), and sensitive URL query params are redacted.
2. **Transport reliability (Phase 8).** No retry/backoff/queue on failure; a
   failed submission is lost unless the user retries manually (E17/E22).
3. **Theming (Phase 4).** No custom theme API; the form is light + built-in
   `prefers-color-scheme`. A host cannot brand it (E23).

## Backend implications

- Group stored reports by `error.fingerprint`; the SDK sets
  `weak-fingerprint` when the fingerprint is weak (E12).
- `error.occurrences` is the count since the last sent report (starts at 1);
  it is not a monotonic total.
- Treat `reportId` as an idempotency key: retries reuse it (E22).
- Source-map resolution for minified stacks is the backend's responsibility
  (E13).
- Accept `schemaVersion` 1 and 2 (`docs/report.schema.json` is the source of
  truth for v2).
