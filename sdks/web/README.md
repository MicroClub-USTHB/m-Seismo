# m-Seismo Web SDK

A browser SDK for collecting bug reports: built-in form, optional screenshots, automatic capture of uncaught errors and unhandled rejections, breadcrumbs (navigation, selected clicks), failed `fetch` diagnostics, browser/app context, and sending a unified JSON report to a backend.

## What it does

- **Built-in bug report form**: opens via keyboard shortcut (`Ctrl+Enter` by default), floating launcher, or `openReportForm()`.
- **Optional screenshot**: attach an image from the form.
- **Automatic error capture**: uncaught errors and unhandled promise rejections (configurable).
- **Breadcrumbs**: navigation and allow-listed interaction clicks.
- **Failed `fetch` diagnostics**: records failed/opaque `fetch` calls; successful calls not recorded. XHR not captured.
- **Browser/app context**: URL (sanitized), browser/platform/viewport when available, app version/environment.
- **Unified JSON report**: processed, sanitized, sent via transport to the configured endpoint.

## Quick start

```js
import { init, openReportForm, captureException, captureMessage } from '@m-seismo/web';

const ok = init({
  endpoint: 'http://localhost:4010/api/reports',
  appVersion: '1.0.0',
  environment: 'dev',
  shortcut: 'Ctrl+Enter',
  launcher: true,
  screenshot: true,
});

openReportForm();
captureException(err);
captureMessage('note');
```

`endpoint` is required (absolute http(s)). Invalid config logs a warning and leaves the SDK disabled. `destroy()` tears down DOM side effects.

## Run it locally

**Prerequisites:** Node.js v20.20.0.

From `sdks/web/`:
```bash
npm ci
```

In separate terminals:
```bash
node tools/mock-receiver/server.mjs  # http://localhost:4010
npm run dev                          # http://localhost:5173
```

**4-step walkthrough:** (1) Check receiver up. (2) Trigger a scenario. (3) Open form (launcher or Ctrl+Enter) and send. (4) Read the report in the test app "Output" or at `http://localhost:4010/__viewer`.

## Test it

From `sdks/web/`:

| Task | Command |
| --- | --- |
| Typecheck | `npm run typecheck` |
| Unit tests | `npm test` |
| Build | `npm run build` |
| E2E (one project) | `npx playwright test --project=chromium` |
| E2E (chromium+firefox) | `npm run test:e2e:local` |
| E2E (all) | `npm run test:e2e` |

Open Playwright report with `npx playwright show-report`.  
**CI:** `.github/workflows/web-sdk-ci.yml` runs typecheck, test, build on Node 20 for `sdks/web/**`.

## Main components

| Folder | Responsibility |
| --- | --- |
| `src/core` | Config/validation, lifecycle/init/destroy, types/version/events |
| `src/form`, `src/theme` | Shadow-DOM form, launcher, theme |
| `src/errors` | Error normalization |
| `src/context` | Context collection and URL/query sanitization |
| `src/network` | Fetch instrumentation |
| `src/privacy` | Sanitization boundary |
| `src/transport` | HTTP send |
| `src/diagnostics`, `src/collectors` | Diagnostics buffer and collectors |
| `src/reporting` | Processor, fingerprinting, dedup/limits, runtime |
| `tools/mock-receiver` | Local mock ingest (dev/e2e) |
| `test-app` | Test app/lab |
| `tests` | Unit and e2e tests |

**Flow:** `form / collectors -> context + diagnostics -> report processor -> sanitize -> transport -> backend`

## Report format

```json
{
  "schemaVersion": 2,
  "sdkVersion": "0.0.0",
  "reportId": "6f1c2b9e-3a4d-4c1f-9b2e-2f0a7d5c8e11",
  "type": "manual",
  "trigger": "form",
  "description": "Button does nothing",
  "error": null,
  "context": {"url": "http://localhost:5173/", "timestamp": "2026-10-27T00:00:00.000Z", "application": {}},
  "breadcrumbs": [],
  "networkFailures": [],
  "screenshot": null
}
```

See `docs/report.schema.json` and `docs/backend-contract.md` (assumed/draft).

## Configuration

| Option | Type | Default | Notes |
| --- | --- | --- | --- |
| `endpoint` | `string` (http(s)) | — | Required. |
| `appVersion`/`environment` | `string` | — | Copied to context.application. |
| `projectKey` | `string` | — | Sent as `X-MSeismo-Key` only if set. |
| `timeoutMs` | `number` | `10000` | Request timeout. |
| `shortcut` | `string \| false` | `'Ctrl+Enter'` | Invalid disables with warning. |
| `launcher` | `boolean \| {label?,position?}` | `false` | Floating button. |
| `screenshot` | `boolean` | `true` | Offer screenshot in form. |
| `privacy.sensitiveQueryParams` | `string[]` | — | Extra params to redact. |
| `diagnostics` | `object` | see code | Errors/rej/nav/interactions/network true by default; element text false; include true; maxEvents 50; maxAgeMs 600000. |
| `reporting` | `object` | see code | `autoReport` 'off' by default; `sendCapturedExceptions` true; limits as configured. |
| `onEvent` | `function` | — | Event listener. |

## Limitations / roadmap

- **Privacy:** baseline scrubber only (Phase 6 pending).
- **Reliability:** no retries or offline queue (Phase 8 pending).
- **Backend:** Go backend does not exist; contract is a draft verified only against the mock receiver.
- **Capture gaps:** no XHR, no `console.error`; CSS/layout problems not auto-detected.
- **Other:** no source-map resolution; weak fingerprints possible for cross-origin script errors.

## Project status

- **Package:** `@m-seismo/web` v0.0.0 (private, **not published yet**)
- **Location:** `sdks/web/`
