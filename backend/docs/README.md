# Backend documentation

In-depth documentation for the m-Seismo backend: owners, projects, apps,
tracker connections (GitHub), and the issue service. For setup and a quick
route list, start with [../README.md](../README.md).

See [crash-providers.md](crash-providers.md) for per-app Sentry/Crashlytics selection, web reporting, and Sentry event resolution.

## Reading order

| # | Page | Read it to… |
|---|---|---|
| 1 | [architecture.md](architecture.md) | understand the layers, package graph, fx wiring, request flow, error and auth model |
| 2 | [usage-guide.md](usage-guide.md) | **use** the backend: the HTTP workflow end to end, auth and retries for clients, and the Go service interfaces with examples |
| 3 | [data-model.md](data-model.md) | learn the tables, constraints, cascades, the migration and the sqlc setup |
| 4 | [github.md](github.md) | see how GitHub access works, why it is secure, how **rate limits** are handled, and how to create the GitHub App |
| 5 | [issue-service.md](issue-service.md) | see how an app's issue is targeted, opened and recorded |
| 6 | [api.md](api.md) | look up an endpoint, its body, responses and errors |
| 7 | [files.md](files.md) | find out what a specific file does |
| 8 | [extending.md](extending.md) | add a tracker, a resource, a query or a setting |
| 9 | [testing.md](testing.md) | run and write tests (unit, API scripts, e2e) |
| 10 | [test-scripts.md](test-scripts.md) | the bash API test suite: every `lib.sh` function, the state file, token handling and the fake GitHub contract |

The local stack (Docker Compose, fake GitHub) is described in
[deploy/README.md](../../deploy/README.md).

## Glossary

| Term | Meaning |
|---|---|
| **Owner** | An account that manages projects, apps and connections. Logs in with email and password. |
| **Project** | A set of tracker repositories that receive issues. Has exactly one *default repository*. |
| **App** | A mobile app that reports bugs, the *source* of issues. Holds SDK keys and links to projects (many-to-many). |
| **App key** | The SDK credential (`msk_…`). Only its SHA-256 hash is stored. |
| **Tracker** | An issue tracker product: `github`, `gitlab` or `bitbucket`. |
| **Provider** | The code integration for one tracker (`provider.Provider`). |
| **Provider connection** | An owner's grant on a tracker account. On GitHub, one App installation. |
| **External ID** | The tracker's identifier for a connection (GitHub: the installation ID). Opaque outside the provider. |
| **Project repository** | A repository linked to a project through one connection. |
| **Issue target** | A (repository, tracker, external ID) triple that `issue.Service` opens an issue in. |
| **Opener** | `issue.Opener`, which opens, comments on and checks issues on one connection. |
| **Draft** | `issue.Draft`, an issue before its repository is chosen. |
| **Ref** | `issue.Ref`, the tracker's reference to an opened issue (`owner/name#N` + URL). |
| **State** | A random, single-use, 10-minute token that ties a tracker callback to the owner who started it. |

## Feature map against the PRD

| PRD item | Status in this backend |
|---|---|
| FR-01 / FR-04: project keys, create, rotate, revoke | done, as **app keys** (`/apps/{id}/keys`) |
| FR-46: issue opened automatically | `issue.Service` done; ingestion stores/queues reports, but the consumer is pending |
| FR-49: labels, assignees, milestone | carried by `Draft`; GitHub resolves milestones by title |
| FR-51: two-way status | schema and `Opener.IsOpen` ready; no sync job yet |
| FR-55: comment on a duplicate | schema, fingerprint and `Opener.Comment` ready; logic not built |
| FR-58 / FR-59: routing, default repository | default repository done; rules will use `OpenInRepository` |
| FR-62: failed-delivery retries | per-target errors returned; no retry queue yet |
| FR-70: other trackers | plug-in model ready; only GitHub implemented |
| §3.4: least-privilege GitHub access | GitHub App with Issues R/W + Metadata R |

## Known gaps and follow-ups

These are deliberate omissions or limitations in the current code:

- **No report-to-issue worker.** Ingestion stores and queues reports, but no consumer calls `issue.Service` for accepted reports yet.
- **No GitHub webhooks.** `suspended_at` is never set, and an uninstalled App
  is only detected when a call fails.
- **JWTs can't be revoked.** Changing a password doesn't log out other
  sessions, and there are no refresh tokens.
- **No rate limiting** on `/auth/login`, `/auth/register` or the provider
  callback. `*ratelimit.Limiter` is ready for this; today it throttles
  `POST /apps/{id}/test-issue` and `POST /reports`.
- **No retry queue yet** for issues that failed or were rate-limited for too
  long. `*issue.RateLimitError.Wait` and the per-target results are the hooks
  for it (FR-62).
- **The access log contains full URLs**, including the one-time OAuth
  `code` and `state` on the callback. Both are single-use and short-lived,
  but consider redacting query strings on that route.
- **Linking the first repository at the same moment from two requests** can
  have both try to become the default. The partial unique index stops it, but
  the loser gets a misleading "already linked" 409.
- **Disconnecting a provider** removes its project repositories, so projects
  may be left without a default and no warning is given.
- **No Go-level DB tests.** Owner isolation and SQL behaviour are covered
  by the API scripts against the dev stack, not by `go test`
  (see [testing.md](testing.md)).
- **`backend/Makefile` uses `sed -i ''`** (BSD/macOS syntax) in `db-up` and
  `db-down`, which fails with GNU sed on Linux and Git Bash. This predates the
  work documented here.
