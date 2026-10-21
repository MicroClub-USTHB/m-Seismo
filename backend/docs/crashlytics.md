# Crashlytics setup

m-Seismo links a report to the Crashlytics crash it came from by reading
crash events through the Crashlytics REST API
(`firebasecrashlytics.googleapis.com/v1alpha`), and warns at start-up when a
connected project can't be read. This page covers the setup. The code lives
in `internal/crash/crashlytics`.

> **The API is alpha.** It has no stability guarantee and can change or break
> without notice. If it stops working, reports are still processed and
> published, just without crash details, and the backend logs a warning at
> start-up.

## How it works

There are two levels of setup:

| Level | What | Where it is stored |
|---|---|---|
| **Deployment** (once per m-Seismo install) | An OAuth client that identifies m-Seismo to Google | Environment variables |
| **Project** (once per m-Seismo project) | A Google account's consent (refresh token) and the project's Firebase apps | Database, refresh token encrypted |

The API refuses service account credentials, so m-Seismo calls it as a
Google user. The project's owner connects it through the OAuth flow below;
Google returns a refresh token, which m-Seismo encrypts with
`SEISMO_SECURITY_SECRET_KEY` (AES-256-GCM) and stores in
`crashlytics_connections`.

To find a crash, `events.list` can filter by time but not by installation.
For each report the backend lists the events of the project's apps in
`[crash time - window, crash time + window]` (`SEISMO_CRASH_WINDOW`, default
`2m`) and keeps the event from the same installation that is closest in
time.

> **Open point: which installation ID.** Events carry Crashlytics' own
> installation UUID, which is not the m-Seismo `installation_id` of the
> report. Matching needs the SDK to send Crashlytics' ID (the optional
> `crashlytics.installation_id` field of the report schema), or to write the
> m-Seismo ID into Crashlytics as a custom key. Until that is decided,
> `Find` gets no match for reports that carry only the m-Seismo ID.

## 1. Deployment setup (once)

1. **Pick a Google Cloud project** for quota and the OAuth client, e.g. the
   main Firebase project. Enable the *Firebase Crashlytics API* on it:
   `gcloud services enable firebasecrashlytics.googleapis.com --project <project>`.
2. **Create an OAuth client**: *APIs & Services → Credentials → Create
   credentials → OAuth client ID → Web application*. Add the callback as an
   authorized redirect URI:
   `https://<your m-Seismo host>/api/v1/oauth/crashlytics/callback`
   (`http://localhost:8080/api/v1/oauth/crashlytics/callback` for local runs).
3. **Set the OAuth consent screen to "In production".** In "Testing" mode
   Google expires refresh tokens after 7 days and only lets listed test users
   sign in. An unverified app in production is fine for internal use; Google
   shows a warning on the consent page that you can click through.
4. **Configure the backend**:

| Variable | Value |
|---|---|
| `SEISMO_CRASH_SOURCE` | `crashlytics` (default `none`: no crash linking) |
| `SEISMO_CRASH_WINDOW` | Matching window, e.g. `2m` |
| `SEISMO_CRASH_CRASHLYTICS_CLIENT_ID` | Client ID from step 2 |
| `SEISMO_CRASH_CRASHLYTICS_CLIENT_SECRET` | Client secret from step 2 |
| `SEISMO_CRASH_CRASHLYTICS_REDIRECT_URL` | The redirect URI from step 2. Empty means `SEISMO_SERVER_PUBLIC_URL` + `/api/v1/oauth/crashlytics/callback` |
| `SEISMO_CRASH_CRASHLYTICS_QUOTA_PROJECT` | Project ID from step 1 |
| `SEISMO_SECURITY_SECRET_KEY` | `openssl rand -base64 32`; required with `crashlytics` |

Keep the secret key safe and stable: if it is lost or changed, the stored
refresh tokens can no longer be read and every project must be connected
again.

## 2. Project setup (once per project)

Use a dedicated Google account (e.g. `m-seismo-bot@...`) rather than a
personal one. It needs two roles:

| Role | On | Why |
|---|---|---|
| *Firebase Crashlytics Viewer* (or *Viewer*) | the project's Firebase project | read the crash events |
| *Service Usage Consumer* (`roles/serviceusage.serviceUsageConsumer`) | the quota project (`SEISMO_CRASH_CRASHLYTICS_QUOTA_PROJECT`) | bill API calls to it; without it every call fails with `403 USER_PROJECT_DENIED` |

An *Owner* or *Editor* of a project already has both on that project.

The endpoints take the owner's bearer token (from `POST /api/v1/auth/login`)
and only reach the owner's own projects:

```sh
AUTH="Authorization: Bearer $TOKEN"
PROJECT=<m-Seismo project id>

# 1. Get the Google consent URL
curl -X POST -H "$AUTH" https://<host>/api/v1/projects/$PROJECT/crashlytics/connect
# → {"url": "https://accounts.google.com/o/oauth2/auth?..."}
```

2. Open the URL **within 10 minutes**, sign in with the dedicated account and
   accept. Google redirects to the callback, which stores the connection and
   shows "Crashlytics is connected with ...".

```sh
# 3. Set the project's Firebase apps
#    Firebase console → Project settings → Your apps → App ID
curl -X PUT -H "$AUTH" https://<host>/api/v1/projects/$PROJECT/crashlytics/apps \
  -d '{"apps": ["projects/123456789/apps/1:123456789:android:abc123",
                "projects/123456789/apps/1:123456789:ios:def456"]}'

# 4. Check the connection (never returns the token)
curl -H "$AUTH" https://<host>/api/v1/projects/$PROJECT/crashlytics
# → {"connected": true, "google_email": "m-seismo-bot@...", "apps": [...]}
```

Connecting again replaces the account and token and keeps the apps.

## Endpoints

All under `/api/v1`.

| Method and path | Access | Purpose |
|---|---|---|
| `POST /projects/{id}/crashlytics/connect` | project owner | Returns the Google consent URL |
| `GET /oauth/crashlytics/callback` | public, protected by the one-time `state` | Google redirects here after consent |
| `GET /projects/{id}/crashlytics` | project owner | Connection status |
| `PUT /projects/{id}/crashlytics/apps` | project owner | Sets the project's Firebase apps |

Another owner's project answers `404`, the same as one that does not exist.
The `state` value is random, stored in Redis for 10 minutes and usable once,
which ties the callback to the project and protects it from CSRF.

## Start-up check

At start-up the backend reads one event from each app of each connected
project. If a read fails, or a connected project has no apps, it logs a
warning ("crash source cannot be read, reports will be published without
crash details") with the error. The service still starts. Alerting the
project owner directly will come with alerts.

## Tests

Unit tests run with `go test ./...`. The database and Redis tests are
skipped unless these are set:

```sh
SEISMO_TEST_DATABASE_URL=postgres://seismo:seismo@localhost:5432/seismo?sslmode=disable \
SEISMO_TEST_REDIS_URL=redis://localhost:6379/0 \
go test ./internal/crash/...
```
