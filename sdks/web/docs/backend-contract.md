# m-Seismo ingest contract — DRAFT (assumed, not verified against the backend)

> **STATUS: DRAFT — ASSUMED.**
> Nothing in this document has been verified against the real Go backend. Every
> endpoint, field, status code, and header here is a **proposal** so the Web SDK
> has something concrete to target. When the backend exists, compare it with this
> file and reconcile. Field names and paths may all change.

The Web SDK's implementation of this contract lives in `src/core/` (payload),
`src/transport/` (HTTP), and is exercised against the local mock receiver in
`tools/mock-receiver/` (which encodes these same assumptions).

---

## 1. Endpoint (ASSUMED)

- **Method:** `POST`
- **Path:** `/api/reports`
- **Content-Type:** `application/json`
- **Body:** a single JSON report object (see §3).
- The full URL is **configurable** in the SDK (`init({ endpoint })`). The value
  `/api/reports` is only a convention; there is no hard-coded backend host.

## 2. Authentication (ASSUMED — open decision)

- **No authentication** for the first version.
- An **optional public project key** may be sent as a header:
  `X-MSeismo-Key: <project key>`.
- The SDK sends this header **only if** `projectKey` is configured.
- Whether the backend requires this header, and whether it is a public (client-safe)
  key or a signed token, is an **open decision** (see §8).

## 3. Request payload (ASSUMED)

```jsonc
{
  "schemaVersion": 2,
  "sdkVersion": "0.0.0",
  "reportId": "6f1c2b9e-3a4d-4c1f-9b2e-2f0a7d5c8e11",
  "type": "manual",        // see §3.3
  "trigger": "form",       // see §3.3
  "description": "Non-empty human description of the problem", // null for automatic/captured
  "error": null, // or { "type", "message", "stack", "fingerprint", "occurrences", ... }
  "context": {
    "url": "https://app.example.com/page?utm_source=docs&token=[REDACTED]",
    "timestamp": "2026-10-16T20:00:00.000Z",
    "browser": {
      "name": "Chrome",        // optional
      "version": "120"         // optional
    },
    "platform": "Windows",     // optional
    "viewport": {
      "width": 1280,           // optional
      "height": 720
    },
    "application": {
      "version": "1.4.2",     // optional
      "environment": "prod"    // optional
    }
  },
  "breadcrumbs": [],
  "networkFailures": [],
  // null, or an inline base64 attachment (see §3.1):
  "screenshot": null
}
```

Field notes (ASSUMED):

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `schemaVersion` | literal `2` | yes | Bumped only on breaking payload changes. `1` reports are legacy. |
| `sdkVersion` | string | yes | Version of the Web SDK that produced the report. |
| `reportId` | string (UUID v4) | yes | Unique per report; see §7 idempotency. |
| `type` | enum | yes | Phase 7: `manual`, `manual_with_error`, `automatic_error`, `captured_exception`, `captured_message`. See §3.3. |
| `trigger` | enum | yes | Phase 7: `form`, `automatic`, `api`. How the report was produced. |
| `description` | string \| `null` | yes | Non-empty string for `manual`/`manual_with_error`; `null` for automatic/captured reports. |
| `error` | `null` or object | yes | `null` for a `manual` report. Object: `type` (string), `message` (string), optional `stack` (string), `handled` (bool), `timestamp`, `source`, `flags`, `context`, and Phase 7 `fingerprint` (16-char lowercase hex) + `occurrences` (integer ≥ 1). |
| `context.url` | string | no | Sanitized page URL at capture time. Omitted (not `null`) when unavailable or unparseable. See §3.2. |
| `context.timestamp` | string (ISO 8601) | yes | Capture time, UTC. |
| `context.browser` | object | no | `{ name, version }`; both optional. Omitted when it cannot be detected. See §3.2. |
| `context.platform` | string | no | e.g. `Windows`, `macOS`, `Linux`, `Android`, `iOS`, `ChromeOS`. Omitted when unknown. |
| `context.viewport` | object | no | `{ width, height }`, positive integers. Omitted when unavailable. |
| `context.application` | object | yes | May be `{}`. `version`/`environment` optional. |
| `breadcrumbs` | array | yes | Phase 5.3: array of breadcrumbs (oldest→newest retained). Each entry: `{timestamp, category, message, level?, data?}`. Categories: `error`, `unhandledrejection`, `captured`, `navigation`, `interaction`, `sdk`. Messages may include `[REDACTED]` placeholders. `data` is a shallow JSON object. Cap ~50 most recent; age-filtered. |
| `networkFailures` | array | yes | Phase 5.3: array of failed/opaque network calls (oldest→newest retained). Shape: `{timestamp, method, url, status?, durationMs, category}`. `category` in {`http_error`,`network_error`,`aborted`,`other_error`,`opaque_response`}. `url` is sanitized (sensitive query values redacted, `[REDACTED]`, truncated to 1024). Cap ~20 most recent. Opaque responses go to breadcrumbs, not here. |
| `screenshot` | `null` or object | yes | Optional inline attachment; see §3.1. Always `null` unless the reporter attached a file. |

### 3.1 Screenshot attachment (ASSUMED — Phase 3)

When the reporter attaches a file, `screenshot` is an object (otherwise `null`):

```jsonc
{
  "screenshot": {
    "name": "screen.png",
    "mimeType": "image/png",
    "sizeBytes": 20480,
    "dataBase64": "iVBORw0KGgo..." // raw base64, NO data: prefix
  }
}
```

Constraints enforced by the SDK before sending:

- `mimeType` ∈ { `image/png`, `image/jpeg`, `image/webp` }.
- `sizeBytes` ≤ 1 048 576 (1 MiB).
- `dataBase64` is the raw base64 of the file bytes (no `data:` URL prefix).

The SDK does **not** capture the screen automatically; a screenshot is only ever
attached when the user picks a file. Inline base64 is an **assumption** — the
real backend may instead want multipart or a separate upload + reference (see
§8, question 7).

### 3.2 Automatic context (ASSUMED — Phase 5.1)

Phase 5.1 adds **optional** `browser`, `platform`, and `viewport` fields to
`context`, and makes `context.url` optional. This is an **additive,
backward-compatible** change: `schemaVersion` stays `1`, and a backend that
ignores the new fields continues to work.

Collection and privacy rules (implemented in `src/context/`):

- **`url`** is sanitized before sending: the fragment (`#...`) and userinfo
  (`user:pass@`) are stripped; the values of sensitive-looking query parameters
  are replaced with the literal `[REDACTED]` (key kept); values that look like a
  JWT or long opaque secret are redacted even under an ordinary key; the result
  is truncated to 2048 chars (`...[truncated]`). Unparseable URLs are omitted.
  The default sensitive keys are listed in
  `DEFAULT_SENSITIVE_QUERY_PARAMS`; apps may add more via
  `init({ privacy: { sensitiveQueryParams: [...] } })`.
- **`browser`** is `{ name, version }` with `name` one of `Chrome`, `Edge`,
  `Firefox`, `Safari`, `Opera`, `Samsung Internet`, and `version` the major
  version only. Detection prefers `navigator.userAgentData.brands` (ignoring
  GREASE entries) and falls back to the user-agent string.
- **`platform`** is a coarse OS label (see the table row above).
- **`viewport`** is `{ width, height }` from `window.innerWidth/innerHeight`
  (positive integers only).
- The raw **`userAgent` is never sent** (not top-level, not inside `context`).
  Only the parsed `browser`/`platform` labels are transmitted.
- The whole `context` object is collected fresh at submit time, is read-only
  (nothing is written to `navigator`, `location`, `history`, storage, or
  cookies), and never throws — a failing collector simply omits its field.

### 3.3 Unified report types, fingerprints & dedup (ASSUMED — Phase 7)

Phase 7 makes **every** report flow through one processor
(`src/reporting/processor.ts`) with one shape. The breaking additions are
`type`, `trigger`, a nullable `description`, and `error.fingerprint` /
`error.occurrences`, so `schemaVersion` becomes `2` (see
`docs/report.schema.json`, which is the machine-readable single source of
truth the mock receiver validates against).

`type` and `trigger`:

| `type` | `trigger` | Meaning | `description` | `error` |
| ------ | --------- | ------- | ------------- | ------- |
| `manual` | `form` | User submitted the form; no recent error attached. | non-empty | `null` |
| `manual_with_error` | `form` | User submitted the form with a recent error attached. | non-empty | present |
| `automatic_error` | `automatic` | SDK auto-reported an uncaught error / unhandled rejection. | `null` | present |
| `captured_exception` | `api` | Host called `captureException()`. | `null` | present |
| `captured_message` | `api` | Host called `captureMessage()`. | `null` | present |

Optional `reporting` controls (`init({ reporting })`) — all client-side, never
sent:

- `autoReport`: `'off'` (default) or `'errors'`. `'all'` is accepted as an alias
  of `'errors'` (the SDK has no non-error automatic reports).
- `sendCapturedExceptions`: send `captured_exception` / `captured_message`
  reports (default `true`).
- `dedupWindowMs` (default 60000, clamped 5000–3600000),
  `maxAutoReportsPerMinute` (default 5, ceiling 30),
  `maxAutoReportsPerSession` (default 50, ceiling 200).
- `beforeSend(report)`: last-chance hook after sanitization; return the report or
  `null` to drop it.

**Fingerprint** (`error.fingerprint`): a stable, non-cryptographic 16-char
lowercase-hex FNV-1a hash of `type` + normalized `message` (whitespace
collapsed, volatile tokens like UUIDs/hex ids/4+-digit numbers/timestamps
replaced, secrets scrubbed, truncated to 200), + the **top application stack
frame** (`fn@path`, line/column/query stripped). A missing/unusable stack, or a
cross-origin script error, yields a **weak** fingerprint and adds the flag
`weak-fingerprint` to `error.flags`. Cross-origin weak fingerprints fold the
page path in so the same bare `"Script error."` on two pages does not merge.

**Dedup / rate limiting** (client-side only): automatic and captured reports are
keyed by fingerprint. The first occurrence in the window is sent; repeats are
counted but not resent. `error.occurrences` is the number of times the
fingerprint occurred **since the last report sent for it** (≥ 1) — it stays `1`
on the first report and grows only when a later report is actually sent.
Manual reports are **never** deduplicated; they carry the current occurrence
count read-only. The limit table is in-memory (bounded to 100 fingerprints), no
storage/cookies, and is cleared by `destroy()`.

The SDK also drops optional parts if the serialized report would exceed ~5 MiB
(screenshot first, then breadcrumbs/network failures).

## 4. Responses (ASSUMED)

| Status | Meaning | Body (ASSUMED) |
| ------ | ------- | -------------- |
| `201` | Created / accepted | `{ "id": "<server id>" }` |
| `400` | Validation error | `{ "message": "why it failed" }` |
| `413` | Payload too large | `{ "message": "..." }` |
| `429` | Rate limited | `{ "message": "..." }` (optionally `Retry-After`) |
| `5xx` | Server error | `{ "message": "..." }` |

The SDK treats any non-2xx as a failure with kind `http` plus the status. It reads
the JSON `id` only on success. A 2xx response whose body is not valid JSON is
reported as kind `invalid-response`. The SDK never throws.

## 5. CORS expectations (ASSUMED — required on the backend)

Because the browser POSTs cross-origin, the backend must:

- Allow the app's origin in `Access-Control-Allow-Origin` (or echo it).
- Allow the methods `POST` and `OPTIONS`.
- Allow the request header `Content-Type` and, if used, `X-MSeismo-Key`.
- Handle the `OPTIONS` preflight (a `POST` with `Content-Type: application/json`
  always triggers a preflight; the custom key header triggers one too).
- Set `Vary: Origin` when echoing the origin.

The local mock receiver implements exactly this, with the allowed origin
configurable via the `ALLOWED_ORIGIN` env var (default `http://localhost:5173`).

## 6. Idempotency (ASSUMED)

The backend should treat `reportId` as a unique key: re-sending the same
`reportId` must return the same success result and must **not** create a second
record. (The mock receiver models this by returning the stored id without storing
again.)

## 7. Size limits (ASSUMED)

- The backend may reject oversized bodies with `413`.
- The mock receiver enforces a default limit of 4 MiB (`MAX_BYTES` env var),
  raised from 256 KiB in Phase 3 to allow inline screenshots.
- The SDK caps a single screenshot at 1 MiB before sending; the 4 MiB body limit
  leaves room for the rest of the JSON and a little headroom.

## 8. Questions for the backend

1. **Endpoint:** Is it `POST /api/reports`? What is the base URL and versioning
   scheme (`/v1/...`)?
2. **Method / content type:** Confirm `POST` + `application/json` (no form-encoded
   or multipart variant)?
3. **Payload:** Do the field names and nesting in §3 match the real schema? Is
   `schemaVersion` numeric or a string (`"1.0"`)?
4. **Auth:** Is any auth required? If a project key is used, is it a public key in
   `X-MSeismo-Key`, a signed token, or a query param? What is the header name?
5. **Validation / storage:** What are the exact validation rules (max `description`
   length, required `context` subfields, UUID format enforcement)? What does the
   server store and return?
6. **Responses:** Are the status codes and bodies in §4 correct? What is the
   success body shape — `{ "id": "..." }` or something else? Any location header?
7. **Attachments (Phase 3):** The SDK currently sends screenshots as inline
   base64 in the JSON (§3.1). Is that acceptable, or does the backend require
   multipart/form-data or a separate upload-then-reference flow? If inline, is
   there a stricter decoded-size limit, and should `dataBase64` include the
   `data:<mime>;base64,` prefix?
8. **Rate limiting:** What are the limits and is `Retry-After` returned?
9. **Automatic context (Phase 5.1):** Does the backend accept the optional
   `context.browser`, `context.platform`, and `context.viewport` fields, and an
   omitted `context.url`? Does anything downstream require `url` to always be
   present? Is the `[REDACTED]` placeholder in the query string acceptable as a
   stored value, or should redacted params be dropped entirely? Are the
   `browser.name` / `platform` vocabularies above acceptable, or is a fixed
   enum expected?
10. **Breadcrumbs & network diagnostics (Phase 5.3):** Do the `breadcrumbs` and `networkFailures` shapes above match the backend’s expected schema? Are the category vocabularies, limits (≤50 breadcrumbs, ≤20 network failures), and the “[REDACTED]” placeholder acceptable? Does the backend expect `opaque_response` to appear here (no) or only as breadcrumbs? Can `data` objects in breadcrumbs include only JSON-safe primitives/objects/arrays?
11. **Report processor (Phase 7):** Are the `type`/`trigger` vocabularies above (and the nullable `description`) acceptable, or does the backend want different names? Does the backend group/roll up by `error.fingerprint`? Is it acceptable that `error.occurrences` reflects only the count at send time (it is not updated for deduped, unsent repeats)? Should the backend key idempotency on `reportId`, or dedupe on `fingerprint` + time window? Does the backend accept `schemaVersion` `2`, or must it also keep accepting `1`? Finally: can a `captured_message` (which has no stack, so a weak `type`+`message` fingerprint) be grouped the same way as errors, or does the backend want a separate key?

---

_This is a living draft. Update it as soon as the real backend contract is known._
