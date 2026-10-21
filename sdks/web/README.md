# Web reporting adapter

A small, dependency-free ES module for reporting from browser apps that already
use Sentry. It uses the shared ingestion contract and does not initialize Sentry.
Import `createReporter` from `./index.js` (or use a local package dependency).

```js
import * as Sentry from "@sentry/browser"; // or your existing framework SDK
import { createReporter } from "./m-seismo/index.js";

// Reuse your app's anonymous installation ID. Persist a random UUID once;
// do not derive it from a user's email/account or a hardware fingerprint.
const installationID = getOrCreateAnonymousInstallationID();
const reporter = createReporter({
  endpoint: "/api/v1/reports", // same origin, or a same-origin reverse proxy
  appKey: "msk_YOUR_APP_KEY", // ingestion-only credential; never a Sentry API token
  installationID,
  environment: "production",
  release: { version: "1.0.0", build: "123" },
});

// Example for a caught frontend error. Keep your existing Sentry.init config.
const eventID = Sentry.captureException(error);
const report = reporter.createReport({
  sentryEventID: eventID,
  trigger: "non_fatal",
  consent: "given", // after consent, or not_required under your internal policy
});
await reporter.sendReport(report);
```

To attach references to uncaught errors, use the bridge at the end of the host
app's existing Sentry `beforeSend` hook, after its filtering/scrubbing:

```js
import { createSentryBridge } from "./m-seismo/index.js";

const bridge = createSentryBridge({
  reporter,
  consent: () => hasReportingConsent() ? "given" : undefined,
  onReport: report => enqueueForDelivery(report), // your existing offline queue
  onError: error => logLocally(error),
});

// In the existing Sentry.init options:
// beforeSend: async (event, hint) => bridge(await scrubAndFilter(event, hint))
```

The bridge returns the original event (including `null` after filtering), preserves
its occurrence timestamp, and isolates m-Seismo queue/delivery failures from Sentry.
Do not make Sentry delivery depend on m-Seismo being reachable.
The SDK capture result is a reference, not proof of delivery: sampling, filtering
and processing delays may produce `event_not_found` during resolution.

For a manual bug report, omit `sentryEventID`, set `title`/`description`, and send an
optional masked JPEG/PNG Blob using `sendReport(report, { screenshot })`.

The adapter deliberately does not provide a report UI or a persistent offline
queue. Keep the original `report` object (including its UUID and timestamp) in
your app's queue and call `sendReport` with it again on retry. Errors expose
`status`, `retryable`, and `retryAfter`; retry 429/503 with exponential backoff and
jitter, honoring Retry-After. Network failures also reject and may be retried.
Drop permanent 4xx errors. Do not create a new report for a retry.

The backend currently has no cross-origin CORS configuration. Use a same-origin
reverse proxy for browser ingestion. App keys grant submission only; owner JWTs
and Sentry API tokens belong in your management/backend flow.

Run tests with `npm test` (Node 20+, no npm dependencies).

The bridge classifies exceptions with `mechanism.handled === false` (including
unhandled promise rejections) as `crash`. Handled exceptions and events without
that signal remain `non_fatal`. The original Sentry event is preserved.
