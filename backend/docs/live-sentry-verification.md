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
