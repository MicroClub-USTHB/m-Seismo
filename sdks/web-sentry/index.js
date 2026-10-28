const eventIDPattern = /^[a-f\d]{32}$/i;

/**
 * Build and send reports using the shared ingestion contract. This adapter
 * uses the host application's Sentry SDK; it never starts another SDK or
 * needs a Sentry auth token. Keep the returned report when retrying a send.
 */
export function createReporter({
  endpoint,
  appKey,
  installationID,
  sessionID = globalThis.crypto.randomUUID(),
  environment = "production",
  release,
  fetch: send = globalThis.fetch,
}) {
  if (!endpoint || !appKey || !installationID || !release?.version || !release?.build) {
    throw new TypeError("endpoint, appKey, installationID, and release version/build are required");
  }

  function createReport({ sentryEventID, title = "", description = "", trigger = "manual", consent, occurredAt = new Date().toISOString() } = {}) {
    if (consent !== "given" && consent !== "not_required") {
      throw new TypeError("Explicit consent (given or not_required) is required");
    }
    if (sentryEventID != null && !eventIDPattern.test(sentryEventID)) {
      throw new TypeError("Sentry event ID must contain 32 hexadecimal characters");
    }
    const report = {
      schema_version: 1,
      id: globalThis.crypto.randomUUID(),
      installation_id: installationID,
      session_id: sessionID,
      occurred_at: occurredAt,
      platform: "web",
      sdk: { name: "m-seismo-web", version: "0.1.0" },
      environment,
      release: { version: release.version, build: release.build },
      trigger,
      auto_submitted: trigger === "crash" || trigger === "non_fatal",
      consent,
      user_input: { title, description },
      device: { os_name: "browser", os_version: "unknown", model: "browser" },
      bodies_captured: false,
    };
    if (sentryEventID != null) report.sentry = { event_id: sentryEventID.toLowerCase() };
    return report;
  }

  async function sendReport(report, { screenshot, signal } = {}) {
    const body = new FormData();
    body.append("report", new Blob([JSON.stringify(report)], { type: "application/json" }), "report.json");
    if (screenshot) body.append("screenshot", screenshot, "screenshot");
    const response = await send(endpoint, {
      method: "POST",
      headers: { Authorization: `Bearer ${appKey}` },
      body,
      signal,
      // Do not send application cookies to the ingestion endpoint.
      credentials: "omit",
      redirect: "error",
    });
    if (response.status !== 202) {
      const error = new Error(`m-Seismo rejected the report (${response.status})`);
      error.status = response.status;
      error.retryable = response.status === 429 || response.status === 503;
      error.retryAfter = response.headers.get("Retry-After");
      throw error;
    }
    return response.json();
  }

  return { createReport, sendReport };
}

/**
 * Use at the end of the host app's existing Sentry beforeSend hook, after
 * scrubbing/filtering. onReport should enqueue the report in the host app's
 * delivery queue. Failures must never drop the original Sentry event.
 */
export function createSentryBridge({ reporter, consent, onReport, onError = () => {} }) {
  if (typeof onReport !== "function") throw new TypeError("onReport must enqueue the report");
  const failed = error => { try { onError(error); } catch { /* preserve Sentry delivery */ } };
  return function beforeSend(event) {
    if (!event?.exception?.values?.length || !eventIDPattern.test(event.event_id ?? "")) return event;
    try {
      const permission = typeof consent === "function" ? consent() : consent;
      if (permission !== "given" && permission !== "not_required") return event;
      const report = reporter.createReport({
        sentryEventID: event.event_id,
        trigger: event.exception.values.some(value => value.mechanism?.handled === false)
          ? "crash" : "non_fatal",
        consent: permission,
        occurredAt: typeof event.timestamp === "number"
          ? new Date(event.timestamp * 1000).toISOString()
          : undefined,
      });
      void Promise.resolve(onReport(report)).catch(failed);
    } catch (error) {
      failed(error);
    }
    return event;
  };
}
