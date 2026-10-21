# Live Sentry verification

Passed on 2026-10-21 against an actual self-hosted Sentry 26.10.0 installation
using its errors-only profile. This was a manual live-service test, separate from
the repeatable suite that uses a TLS Sentry stub.

## Environment

- Chromium 147.0.7727.101, launched through Playwright in a fresh browser context.
- Official Sentry browser SDK 10.22.0 and the repository's `sdks/web/index.js`.
- The production Go dependency graph with disposable PostgreSQL 17, Redis 7 and
  NATS 2.10 services, fresh migrations and a temporary owner/app.
- Local HTTPS Sentry origin with a dedicated test certificate. Go and Node trusted
  that certificate explicitly; browser certificate handling was scoped to the
  test context. Production TLS verification remained enabled.

## Verified flow

1. Create a Sentry organization, JavaScript project, public ingestion DSN and a
   private, expiring `project:read` token.
2. Register an m-Seismo owner, create a web app and SDK key, and connect the real
   Sentry project through the owner API. Check that credentials are not returned.
3. Initialize Sentry with `createSentryBridge` as `beforeSend`, then throw an
   **unhandled browser exception** from a timer. Assert Chromium observes it.
4. Send the bridge's report through `createReporter.sendReport` and the actual
   multipart ingestion endpoint. Assert acceptance and flush the Sentry SDK.
5. Poll the owner's report crash endpoint until Sentry finishes processing. Assert
   `linked`, an exact event ID match and a nonempty exception stack.
6. Retrieve the event independently from Sentry's authenticated event API. Assert
   its ID and unique exception message match the browser error.

Result:

```json
{
  "tested_at": "2026-10-21T15:33:03.244Z",
  "report_id": "ad6a1c07-b6d7-4bd4-990f-adf6f1c464e1",
  "event_id": "c59e24eab4714330a07f045b46477d27",
  "issue_id": "1",
  "stack_frames": 1,
  "status": "passed"
}
```

The browser, m-Seismo process and disposable test services were removed after the
assertions. The local Sentry installation and captured event were retained.
Credentials and installation-specific scripts are kept outside the repository.

This verifies browser capture and report correlation against a real Sentry
server. Native mobile SDK execution, Crashlytics resolution and automatic GitHub
issue enrichment were not exercised by this test; their existing limitations
are described in [crash-providers.md](crash-providers.md).

## Follow-up after integrating the report worker

Passed again against the same installed Sentry 26.10.0 server after merging the
current `dev` worker and addressing review feedback. Chromium's unhandled error
was classified as `crash`. The real Go application accepted the multipart report,
resolved the actual Sentry event and stack, and its JetStream consumer and issue
service posted exactly one issue through the GitHub HTTP publisher to fake GitHub.
The captured issue body contained `### Crash`, the same event ID, a Sentry link and
the `crash` trigger. The Sentry auth token was absent from the issue body.

Sentry capture and lookup used the real installed server; the GitHub boundary
used the local simulator and created no external GitHub issue. When an event is
still processing, the worker retains its exact ID and a validated Sentry search
link. Full stack details are included when Sentry already has them; published
issues are not automatically backfilled after a later Sentry lookup.

```json
{
  "tested_at": "2026-10-21T22:28:56.429Z",
  "sentry_release": "26.10.0",
  "browser": "147.0.7727.101",
  "sdk": "@sentry/browser 10.22.0",
  "report_id": "3151cd2b-ba0e-4935-bade-b1cbe60ff58d",
  "event_id": "f38196c4957e4f1f824742b34bf4c22d",
  "issue_id": "1",
  "event_url": "https://127.0.0.1:19443/organizations/m-seismo-live/issues/1/events/f38196c4957e4f1f824742b34bf4c22d/",
  "stack_frames": 1,
  "trigger": "crash",
  "status": "passed",
  "github_issue_body_verified": true,
  "github_boundary": "fake GitHub; real report worker and HTTP publisher"
}
```

The temporary backend, GitHub simulator, browser, database, Redis and NATS were
removed after this run. The installed Sentry service remains available locally.
