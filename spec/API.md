# Ingestion API

Status: **draft, not reviewed.** Items marked `TBD` are still open. Items marked `pending PRD` depend on a product decision.
Source of truth for behaviour: [`PRD.md`](../PRD.md). Requirement IDs (`FR-nn`) in brackets.

## 1. Scope

Covers how SDKs send reports to the backend and how the backend answers.

Out of scope here:

- the agent and backend protocol (separate doc, TBD);
- GitHub webhooks (section 9, TBD);
- admin API (v1.x, FR-61).

## 2. Authentication

- Every request carries a project key. The key can only send reports, never read them [FR-01, NFR-04].
- Sent as `Authorization: Bearer <project key>`.
- The project admin sets each key in the app's production config before deploy. Each key is unique to its project.
- Unknown, revoked and malformed keys all get the same `401` with the same body. The response reveals nothing about which keys exist [FR-04].
- **Key rotation is pending a PRD decision.** This spec assumes keys never change, but FR-04 requires admins to rotate them. The product owner has to decide before the rotation design goes in (see section 10).

## 3. Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/v1/reports` (proposed) | Submit one report and its attachments, in one `multipart/form-data` request |

Attachments travel in the same request as the report, so one request is one retry unit and one idempotency key.

## 4. Report payload

Each field group maps to PRD requirements. Field names and types are TBD.

| Group | Content | Req |
| --- | --- | --- |
| Identity | client-generated report ID (UUIDv7, idempotency key); anonymous installation ID | FR-24, FR-34 |
| Project config | environment, app version, build, project key reference | FR-02 |
| Mode | `internal` / `QA` / `production` (fixed enum) | FR-03 |
| Trigger | shake, screenshot, long-press, manual, crash, non-fatal (fixed enum) | FR-07 to FR-09 |
| User input | title, description, category, severity (all optional; category and severity from fixed lists) | FR-10, FR-12 |
| Auto-submit | whether the report was sent without the sheet | FR-12 |
| Consent | consent state at send time | FR-14 |
| Device and app | OS, device model, locale, battery, memory, network, orientation | FR-15 |
| Flags and experiments | active flags and variants | FR-16 |
| Logs | last N lines or last N seconds | FR-17 |
| User actions | screens, taps, network calls, lifecycle events | FR-18 |
| Crash | trace and app state (crash reports only) | FR-20 |
| Crashlytics link | installation and user IDs, custom keys | FR-21 |
| Session reference | value also sent on every app request to our servers | FR-22 |
| Network requests | address, method, result, duration (default off) | FR-23, FR-28 |
| Screenshot | original and annotated image (attachments) | FR-11, FR-19 |

Rules:

- **Timestamps.** RFC 3339, UTC, with milliseconds (e.g. `2026-10-15T19:13:00.123Z`). The device sends its own timestamp. The server also records receive time and uses it for ordering, since offline reports arrive late.
- **Unknown fields.** They are ignored and counted, not rejected. Version skew is expected, since app builds stay installed long after a backend change.
- **Enums.** Trigger, mode and severity are fixed. Category is a fixed list in v1. Unknown enum values are rejected (`400`), even though unknown fields are ignored.
- **Absent vs `null`.** TBD.

## 5. Responses

### Success

- Status `202 Accepted`. The issue is not created yet, but has been registered in the system.
- Body: `{"id": "<report uuid>", "status": "accepted"}`. No issue URL, since the issue is created later.
- A duplicate (same report ID) returns the same `202` and body, so the SDK never needs a second code path [FR-34].
- The reporter sees confirmation within 0.5 s [FR-32].

### Errors

Every error body uses the same shape:

```json
{"code": "RATE_LIMITED", "message": "Too many reports from this installation."}
```

- `code` is stable and machine-readable. SDKs branch on it.
- `message` is for humans and may change.
- Codes below are proposed names, not final.

| Case | Status | Code (proposed) | SDK retries? |
| --- | --- | --- | --- |
| Missing, unknown, revoked or malformed key | `401` | `UNAUTHORIZED` | no |
| Malformed payload, missing required field, wrong type, unknown enum value | `400` | `INVALID_PAYLOAD` | no |
| Body or attachment too large | `413` | `PAYLOAD_TOO_LARGE` | no |
| Rate limited, per key or per installation [FR-35] | `429` | `RATE_LIMITED` | yes, after `Retry-After` |
| Server unavailable or failing | `503` | `UNAVAILABLE` | yes, after `Retry-After` if sent, otherwise with backoff [FR-33] |
| Network error, no response | n/a | n/a | yes, with backoff [FR-33] |

The backend never returns `500` on purpose. Any server-side failure returns `503`, so the retry rule above covers it. Confirm this when reviewing.

Retry rules for the SDK:

- retry `429`, `503` and network errors with exponential backoff and jitter;
- use `Retry-After` when present;
- never retry any other `4xx`. The SDK logs the drop locally, so one bad report can't block the offline queue.

## 6. Limits

Starting values, to tune after the pilot.

| Limit | Value |
| --- | --- |
| Request body, total (JSON plus attachments) | 15 MB |
| Per attachment | 8 MB |
| Log lines per report | 500 |
| User actions per report | 200 |
| Reports per installation | 10 per minute, burst of 5 |
| Requests per project key | 600 per minute |

Over-limit requests get `413` (size) or `429` (rate), as above.

## 7. Privacy rules the API enforces

- The backend rejects any field that identifies a person (name, email, account) [FR-24]. Exact field list TBD.
- Request and response bodies, headers and form values are accepted only when the report says the app opted in [FR-28].
- The backend stores attachments privately. Presigned links to them expire [FR-48].
- Deletion by report ID or installation ID must reach attachments too [FR-30].

## 8. Delivery guarantees

- The SDK delivers each report at least once [FR-34].
- Same report ID in, same result out: a retry never creates a second report or issue [FR-34].
- The API never loses a report it has accepted, even if GitHub is down or the backend restarts [NFR-06].
- Server log collection and issue creation run after the acknowledgement, asynchronously [FR-32, NFR-01].

## 9. GitHub webhook (TBD)

- Receives issue-closed events to mark reports resolved [FR-51].
- The signature verification method and endpoint path are TBD.

## 10. Open items

Product decision needed:

1. **Key rotation (FR-04).** This spec assumes keys never change. Options for @wail:
   - keep FR-04 as written, and design rotation (for example, a two-key overlap window) in a later revision of this spec;
   - amend FR-04 to create and revoke only. Revoking a leaked key then strands every install that uses it until the next build ships.

Still open in this spec:

2. Exact JSON field names and types (section 4).
3. Attachment part names and content types in the multipart request.
4. Whether `null` and absent fields are treated the same.
5. Exact list of fields that count as personal data (section 7).
6. Final error code names (section 5).
7. Section 9, GitHub webhook.
8. The agent and backend protocol, in its own doc.
