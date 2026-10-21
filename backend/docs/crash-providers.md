# Per-app crash providers

Crash data sources are separate from issue trackers. One owner can select Sentry
for a web app, Crashlytics for an Android app, and no crash provider for another
app. This does not change which GitHub repositories receive issues.

## Current behavior

- Sentry connections are verified against the selected project before saving.
- Reports can carry an exact Sentry event ID on iOS, Android, Flutter, React Native
  or web. No Firebase installation is required for Sentry apps.
- An owner can resolve that reference into a Sentry event URL, title and exception
  stack trace. Resolution is lazy and does not delay report ingestion. An event
  can be resolved again after Sentry finishes processing it.
- Crashlytics selection stores the app's Firebase project/app identifiers. The
  Firebase export/correlation adapter was absent before this change and is still
  pending; the crash endpoint explicitly returns `adapter_pending` for it.
- No crash-provider connection is required to submit a normal bug report.
- The existing iOS SDK still uses its older transport format. Mobile integrations
  must send the shared multipart contract directly until that SDK is migrated.
- The report-to-GitHub worker is not implemented in this repository. This change
  adds report crash resolution; it does not claim automatic issue enrichment.

## Operator setup

Apply the database migrations, then configure the backend:

```sh
# Generate once and store as SEISMO_CRASH_PROVIDERS_ENCRYPTION_KEY.
openssl rand -base64 32
```

The key encrypts Sentry auth tokens with AES-256-GCM, bound to the app ID. Keep it
stable across deployments and keep a backup separate from the database. Changing
it requires reconnecting existing Sentry apps. An empty key disables Sentry setup
without disabling the server or apps using no provider/Crashlytics.

`SEISMO_CRASH_PROVIDERS_SENTRY_ORIGINS` is a space-separated list of allowed HTTPS
origins. Defaults: `https://sentry.io https://us.sentry.io https://de.sentry.io`.
An operator can add a self-hosted Sentry origin. Paths, userinfo, query strings,
fragments and HTTP origins are rejected. The host must use a trusted TLS certificate.
API calls never follow redirects. Owners cannot introduce arbitrary destinations.
Use the origin appropriate to the Sentry account's region.

## Owner API

All routes below require an **owner JWT**, never an SDK app key. Ownership is
checked before reading settings, resolving reports, or contacting Sentry.

| Method | Path | Behavior |
| --- | --- | --- |
| PUT | `/api/v1/apps/{appID}/crash-provider` | Set or replace this app's provider |
| GET | `/api/v1/apps/{appID}/crash-provider` | Read configuration without credentials; 404 if unset |
| DELETE | `/api/v1/apps/{appID}/crash-provider` | Disconnect; repeated deletion is a no-op |
| GET | `/api/v1/apps/{appID}/reports/{reportID}/crash` | Resolve the report's crash reference |

`reportID` is the SDK report UUID returned by ingestion, not the backend's internal
row UUID. Reports are scoped to the app as well as the authenticated owner.

Connect Sentry with an auth token that can read the chosen project (`project:read`):

```json
{
  "provider": "sentry",
  "sentry": {
    "base_url": "https://sentry.io",
    "organization": "microclub",
    "project": "website",
    "auth_token": "YOUR_PRIVATE_SENTRY_AUTH_TOKEN"
  }
}
```

A Sentry DSN is an ingestion credential, not an API read token. Keep the auth token
on the backend: do not ship it in mobile apps, browser bundles, or report payloads.
Only the owner management flow sends it to m-Seismo.

Responses include `provider`, the public `sentry` settings, `has_credentials`, and
`updated_at`; the token is never returned. To rotate the token, PUT a replacement.
To retain it, omit `auth_token`; a new token is required when changing provider or
origin. Every PUT verifies access to the selected project. Failed verification
leaves the old connection intact.

Choose Crashlytics independently on another app:

```json
{
  "provider": "crashlytics",
  "crashlytics": { "project_id": "firebase-project", "app_id": "1:123:android:abc" }
}
```

The PUT selects exactly one provider; DELETE restores no-provider operation.

## Report contract and mobile integration

Use the app's existing Sentry SDK to capture the error. Attach the resulting
32-character hexadecimal event ID to the m-Seismo report:

```json
"sentry": { "event_id": "9999aaaaca8b46d797c23c6077c6ff01" }
```

The organization and project come from the owner-configured app connection, never
from an untrusted report. A report cannot select another app's Sentry project.
No timestamp-based matching is performed. A normal manual report may reference a
Sentry event too. For `crash`/`non_fatal`, either inline `crash` details or a valid
`sentry` reference is required. Existing Crashlytics report fields remain accepted.

Send the complete report as the `report` part of `multipart/form-data` to
`POST /api/v1/reports` with `Authorization: Bearer msk_...`. See
[the shared contract](../../spec/ingestion.md) and
[the web example](../../sdks/web/README.md). Keep the same report UUID on retries.
On native platforms, persist the event ID and original session/timestamp so a
crash reported on the next launch retains its original context. Do not replace an
app's existing Sentry initialization or install a second crash handler.

## Resolution response

```json
{
  "status": "linked",
  "provider": "sentry",
  "event": {
    "provider": "sentry",
    "event_id": "9999aaaaca8b46d797c23c6077c6ff01",
    "issue_id": "42",
    "url": "https://sentry.io/organizations/microclub/issues/42/events/9999aaaaca8b46d797c23c6077c6ff01/",
    "title": "TypeError",
    "stack_trace": ["render (app.js:12)"]
  }
}
```

Other successful statuses are `not_configured`, `no_reference`, `event_not_found`
and `adapter_pending`. `event_not_found` can mean processing delay, deletion,
retention expiry, or a reference to the wrong project; it is never guessed into
a match. Resolution uses the app's **current** connection; disconnecting or
switching projects can make older references unavailable.

Invalid credentials return 422, upstream rate limits return 429 with `Retry-After`,
and provider transport/server failures return 503. All are separate from report
acceptance: Sentry failures cannot reject an already accepted m-Seismo report.

The projection excludes Sentry user identities, arbitrary contexts, request
headers/bodies and breadcrumbs. Title and stack-frame text remain crash diagnostic
data and follow the host app's Sentry scrubbing configuration. Responses are
bounded and no raw upstream response/error body is exposed or persisted.

## Sentry API references

- [Retrieve a project](https://docs.sentry.io/api/projects/retrieve-a-project/)
- [Retrieve a project event](https://docs.sentry.io/api/events/retrieve-an-event-for-a-project/)

## Validation

`go test ./...` covers app isolation, owner authorization, token rotation,
encryption integrity, HTTP routing, credential redaction, upstream failures and
schema references. `cd sdks/web && npm test` covers multipart delivery, the
Sentry beforeSend bridge, consent and retries. To exercise real persistence after
applying migrations to an isolated test database:

```sh
cd backend
SEISMO_TEST_DATABASE_URL=postgres://... go test ./internal/crashprovider -run TestPostgresReportCrashIntegration -v
```

The integration test creates temporary owner/app/report rows and removes them on
completion. It uses a local TLS Sentry stub and never contacts a real account.
