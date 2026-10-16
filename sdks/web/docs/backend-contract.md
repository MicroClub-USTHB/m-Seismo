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
  "schemaVersion": 1,
  "sdkVersion": "0.0.0",
  "reportId": "6f1c2b9e-3a4d-4c1f-9b2e-2f0a7d5c8e11",
  "description": "Non-empty human description of the problem",
  "error": null, // or { "type": "...", "message": "...", "stack": "..." }
  "context": {
    "url": "https://app.example.com/page",
    "timestamp": "2026-10-16T20:00:00.000Z",
    "application": {
      "version": "1.4.2",     // optional
      "environment": "prod"    // optional
    }
  },
  "breadcrumbs": [],
  "networkFailures": [],
  "screenshot": null
}
```

Field notes (ASSUMED):

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `schemaVersion` | literal `1` | yes | Bumped only on breaking payload changes. |
| `sdkVersion` | string | yes | Version of the Web SDK that produced the report. |
| `reportId` | string (UUID v4) | yes | Unique per report; see §7 idempotency. |
| `description` | string | yes | Must be non-empty. |
| `error` | `null` or object | yes | `null` for manual reports. Object: `type` (string), `message` (string), optional `stack` (string). |
| `context.url` | string | yes | Page URL at capture time (may be empty in non-browser contexts). |
| `context.timestamp` | string (ISO 8601) | yes | Capture time, UTC. |
| `context.application` | object | yes | May be `{}`. `version`/`environment` optional. |
| `breadcrumbs` | array | yes | Empty in this version; shape TBD in a later phase. |
| `networkFailures` | array | yes | Empty in this version; shape TBD in a later phase. |
| `screenshot` | literal `null` | yes | **Out of scope** for this version of the contract. |

**Screenshot is explicitly OUT of this contract.** The field is present and always
`null` so the schema is stable, but no image data is sent yet.

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
- The mock receiver enforces a default limit of 256 KiB (`MAX_BYTES` env var).

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
7. **Attachments:** When screenshots/attachments are added later, will they be
   inline base64 in the JSON, multipart, or a separate upload + reference flow?
8. **Rate limiting:** What are the limits and is `Retry-After` returned?

---

_This is a living draft. Update it as soon as the real backend contract is known._
