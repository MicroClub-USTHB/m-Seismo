# Ingestion API

How an SDK sends a report to the backend, and how the backend answers.

- **Report format:** [`report.schema.json`](report.schema.json) (JSON Schema 2020-12), version 1.
- **Examples:** [`examples/`](examples/). Files named `invalid-*` must be rejected.

Status: v1 draft. Changes need review from the SDK and backend owners.

## 1. Endpoint

```
POST /api/v1/reports
Authorization: Bearer msk_…
Content-Type: multipart/form-data; boundary=…
```

One request carries one report and its screenshot, so one request is one
retry unit with one idempotency key.

| Part | Content type | Required | Content |
|---|---|---|---|
| `report` | `application/json` | yes | The report, valid against `report.schema.json` |
| `screenshot` | `image/jpeg` or `image/png` | no | The screenshot as sent by the reporter: annotated, with masked areas and sensitive views already hidden. The unmasked original never leaves the device. |

Other parts are ignored.

## 2. Authentication

- The SDK sends its **app key** (`msk_…`) as `Authorization: Bearer <key>`.
  A key can only send reports, never read anything.
- Missing, malformed, unknown and revoked keys all get the same `401` with
  the same body, so the answer reveals nothing about which keys exist.
- Rotation: an app can have several active keys. Ship a build with
  the new key, then revoke the old one; a revoked key is refused at once.

## 3. What the backend does, in order

1. **Key** — resolve the app key; reject with `401` if it is not active.
2. **Rate limit for the app** — all keys of the app share one limit
   (section 6); reject with `429`. This runs before the body is read, so
   abuse costs little.
3. **Size** — reject bodies over the limits with `413`.
4. **Validate** — parse the `report` part and validate it against the
   schema; reject with `400`.
5. **Rate limit for the installation** — it needs `installation_id`, so it
   runs once the report is read; reject with `429`.
6. **Idempotency** — if a report with the same `id` was already accepted
   for this app, answer as the first time and stop.
7. **Store** — save the report and the screenshot, privately.
8. **Queue** — publish a "report accepted" message for the pipeline
   (server context, crash link, grouping, routing, GitHub issue).
9. **Acknowledge** — answer `202`, within 0.5 s of receiving the request.
   Nothing after this point makes the SDK wait.

Steps 7 and 8 are atomic from the SDK's point of view: if either fails, the
backend answers `503` and the SDK retries; a retry never creates a second
report.

A retried report counts against the rate limits like any other request.

## 4. Field rules

- **Names** are `snake_case`.
- **Timestamps** are RFC 3339 in UTC with milliseconds:
  `2026-10-18T14:03:12.123Z`. The device clock can be wrong or late; the
  backend also records when it received the report and orders by that.
- **Unknown fields are ignored and never stored.** App builds stay installed
  long after a backend change, so an older or newer SDK must not be
  rejected for an extra field. Not storing them also means a field the
  backend doesn't know can't leak personal data into an issue.
- **Enums are strict.** An unknown value for `platform`, `mode`, `trigger`,
  `consent`, `category`, `severity` or any other enum is rejected (`400`).
- **Absent and `null`** mean the same thing: no value.
- **Crash reports.** When `trigger` is `crash` or `non_fatal`, `crash` is
  required. `session_id`, `logs` and `breadcrumbs` must come from the
  session that crashed, not from the launch that sends the report.
- **Bodies.** When `bodies_captured` is false or absent, any
  `request_body` or `response_body` is dropped.

## 5. Responses

### Accepted

```
202 Accepted
{"id": "<report id>", "status": "accepted"}
```

The issue is created later; no issue URL is returned. A duplicate (same
`id`, same app) gets the same `202` and body, so the SDK needs no special
case.

### Errors

Every error has the same body:

```json
{"code": "invalid_report", "message": "occurred_at: does not match the timestamp format"}
```

`code` is stable and the SDK may branch on it. `message` is for humans and
may change.

| Case | Status | `code` | SDK retries? |
|---|---|---|---|
| Missing, malformed, unknown or revoked key | `401` | `unauthorized` | No |
| Not `multipart/form-data`, or no `report` part | `400` | `invalid_request` | No |
| `report` is not valid JSON or fails the schema | `400` | `invalid_report` | No |
| `schema_version` not supported | `400` | `unsupported_version` | No |
| Screenshot is not JPEG or PNG | `415` | `unsupported_media_type` | No |
| Body or screenshot over the size limit | `413` | `too_large` | No |
| Over the rate limit (section 6) | `429` | `rate_limited` | Yes, after `Retry-After` |
| Backend unable to store or queue | `503` | `unavailable` | Yes, after `Retry-After` if present, else with backoff |
| No response (network error, timeout) | — | — | Yes, with backoff |

The backend never answers `500` on purpose: any server-side failure is
`503`, so the retry rules cover it.

**SDK retry rules:** retry `429`, `503` and network errors with
exponential backoff and jitter, honouring `Retry-After`. Never retry another
`4xx`: drop the report and log it locally, so one bad report can't block
the offline queue.

## 6. Limits

Starting values, to tune after the pilot.

| Limit | Value |
|---|---|
| Whole request | 15 MB |
| `report` part | 1 MB |
| `screenshot` part | 8 MB |
| Reports per installation | 10 per minute, bursts of 5 |
| Reports per app (all its keys together) | 600 per minute |

Array and string lengths are capped in the schema (500 log lines, 200
breadcrumbs, …).

## 7. Versioning

- The path carries the API version (`/api/v1/`), the report carries
  `schema_version`.
- Adding an optional field does not change the version: older backends
  ignore it.
- Removing a field, making one required, or changing a type or an enum's
  meaning needs a new `schema_version`. The backend keeps accepting the
  previous version for as long as builds using it are installed.

## 8. Open points

1. **Crashlytics link.** Which ID lets the backend find the
   matching Crashlytics crash: Crashlytics' own installation ID in
   `crashlytics.installation_id`, or the m-Seismo `installation_id` written
   into Crashlytics as a custom key. The field is in the schema but stays
   optional until this is decided.
2. **GitHub webhook** for closed issues: separate document.
3. **Agent protocol** (server-side context): separate
   document.
