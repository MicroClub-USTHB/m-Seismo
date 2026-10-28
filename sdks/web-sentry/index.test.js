import { test } from "node:test";
import assert from "node:assert/strict";
import { createReporter, createSentryBridge } from "./index.js";

const options = {
  endpoint: "https://bugs.example.com/api/v1/reports",
  appKey: "msk_example",
  installationID: "84f5f612-4560-4f08-a2d2-e47d6e465ffc",
  release: { version: "1.0.0", build: "123" },
};

test("Sentry references use the backend's multipart contract with a stable retry ID", async () => {
  const requests = [];
  const reporter = createReporter({ ...options, fetch: async (url, request) => {
    requests.push({ url, ...request });
    return new Response(JSON.stringify({ status: "accepted" }), { status: 202 });
  } });
  const report = reporter.createReport({
    sentryEventID: "A".repeat(32), trigger: "non_fatal", consent: "not_required",
  });
  await reporter.sendReport(report);
  await reporter.sendReport(report);
  assert.equal(requests[0].headers.Authorization, `Bearer ${options.appKey}`);
  assert.equal(requests[0].headers["Content-Type"], undefined); // fetch adds the boundary
  assert.equal(requests[0].credentials, "omit");
  assert.equal(requests[0].redirect, "error");
  const first = JSON.parse(await requests[0].body.get("report").text());
  const retry = JSON.parse(await requests[1].body.get("report").text());
  assert.equal(first.platform, "web");
  assert.equal(first.sentry.event_id, "a".repeat(32));
  assert.equal(first.id, retry.id);
  assert.equal(first.session_id, retry.session_id);
  assert.equal(first.bodies_captured, false);
  assert.equal(first.device.os_name, "browser");
});

test("reports work without Sentry and require consent", () => {
  const reporter = createReporter(options);
  assert.throws(() => reporter.createReport(), /consent/);
  assert.throws(() => reporter.createReport({ consent: "given", sentryEventID: "bad" }), /32 hexadecimal/);
  assert.equal(reporter.createReport({ consent: "given" }).sentry, undefined);
});

test("permanent errors and retryable responses are distinguishable", async () => {
  for (const status of [400, 401, 413, 429, 503]) {
    const reporter = createReporter({ ...options, fetch: async () => new Response("{}", {
      status, headers: { "Retry-After": "15" },
    }) });
    await assert.rejects(reporter.sendReport(reporter.createReport({ consent: "given" })), error => {
      assert.equal(error.status, status);
      assert.equal(error.retryable, status === 429 || status === 503);
      assert.equal(error.retryAfter, "15");
      return true;
    });
  }
});

test("Sentry bridge preserves the original event and original occurrence time", () => {
  const reporter = createReporter(options);
  const captured = [];
  const bridge = createSentryBridge({ reporter, consent: "given", onReport: report => captured.push(report) });
  const event = { event_id: "b".repeat(32), timestamp: 1700000000.123, exception: { values: [{ type: "TypeError" }] } };
  assert.equal(bridge(event), event);
  assert.equal(captured[0].sentry.event_id, event.event_id);
  assert.equal(captured[0].occurred_at, "2023-11-14T22:13:20.123Z");
  assert.equal(captured[0].trigger, "non_fatal");
  assert.equal(bridge(null), null); // preserve the host app's filtering decision
  assert.equal(captured.length, 1);
});

test("Sentry bridge honors changing consent and isolates delivery failures", async () => {
  const reporter = createReporter(options);
  let allowed = false;
  let attempted = 0;
  const failures = [];
  const bridge = createSentryBridge({ reporter, consent: () => allowed ? "given" : undefined,
    onReport: () => { attempted++; return Promise.reject(new Error("offline")); },
    onError: error => failures.push(error.message),
  });
  const event = { event_id: "c".repeat(32), exception: { values: [{ type: "Error" }] } };
  assert.equal(bridge(event), event);
  assert.equal(attempted, 0);
  allowed = true;
  assert.equal(bridge(event), event);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(attempted, 1);
  assert.deepEqual(failures, ["offline"]);
  const broken = createSentryBridge({ reporter, consent: "given",
    onReport: () => { throw new Error("queue failed"); },
    onError: () => { throw new Error("logger failed"); },
  });
  assert.equal(broken(event), event);
});

test("Sentry bridge distinguishes unhandled exceptions and rejected promises from handled errors", () => {
  const captured = [];
  const bridge = createSentryBridge({reporter:createReporter(options),consent:"given",onReport:r=>captured.push(r)});
  for (const [mechanism,expected] of [
    [{type:"onerror",handled:false},"crash"],
    [{type:"onunhandledrejection",handled:false},"crash"],
    [{type:"generic",handled:true},"non_fatal"],
    [undefined,"non_fatal"],
  ]) {
    const event={event_id:"d".repeat(32),exception:{values:[{type:"Error",mechanism}]}};
    assert.equal(bridge(event),event);
    assert.equal(captured.at(-1).trigger,expected);
    assert.equal(captured.at(-1).auto_submitted,true);
  }
});
