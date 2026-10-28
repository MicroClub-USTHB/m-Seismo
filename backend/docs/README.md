# Backend documentation

In-depth documentation for the m-Seismo backend: owners, projects, apps,
each app's tracker connections (GitHub, GitLab, Bitbucket), and the issue
service. For setup and a quick
route list, start with [../README.md](../README.md).

See [crash-providers.md](crash-providers.md) for per-app Sentry/Crashlytics selection, web reporting, and Sentry event resolution.

## Reading order

| # | Page | Read it to… |
|---|---|---|
| 1 | [architecture.md](architecture.md) | understand the layers, package graph, fx wiring, request flow, error and auth model |
| 2 | [usage-guide.md](usage-guide.md) | **use** the backend: the HTTP workflow end to end, auth and retries for clients, and the Go service interfaces with examples |
| 3 | [data-model.md](data-model.md) | learn the tables, constraints, cascades, the migration and the sqlc setup |
| 4 | [github.md](github.md) | see **who a connection belongs to** (the app), how GitHub access works, why it is secure, how **rate limits** are handled, and how to create the GitHub App |
| 5 | [gitlab-bitbucket.md](gitlab-bitbucket.md) | see how GitLab and Bitbucket are reached with **per-app OAuth tokens**: the connect flow, encrypted token storage, refresh and rotation, issue field mapping, and how to set up a real GitLab application or Bitbucket consumer |
| 6 | [issue-service.md](issue-service.md) | see how an app's issue is targeted, matched to one of the app's connections, opened and recorded |
| 7 | [worker.md](worker.md) | see how an accepted report becomes an issue: the queue, retries and idempotency |
| 8 | [routing.md](routing.md) | see how routing rules pick each issue's repository, labels, assignees and milestone |
| 9 | [coordinator.md](coordinator.md) | see how the log coordinator attaches and how the worker gets server logs from any instance |
| 10 | [../bruno](../bruno) | look up or run an endpoint: a Bruno collection with the body, responses and errors of each route |
| 11 | [files.md](files.md) | find out what a specific file does |
| 12 | [extending.md](extending.md) | add a tracker, a resource, a query or a setting |
| 13 | [testing.md](testing.md) | run and write tests (unit, API scripts, e2e) |
| 14 | [test-scripts.md](test-scripts.md) | the bash API test suite: every `lib.sh` function, the state file, token handling and the fakes' contracts |

The local stack (Docker Compose: Postgres, Redis, NATS and the backend) is
described in [deploy/README.md](../../deploy/README.md); it needs a real
GitHub App and/or GitLab application or Bitbucket consumer (see
[github.md](github.md) and [gitlab-bitbucket.md](gitlab-bitbucket.md)).

## Glossary

| Term | Meaning |
|---|---|
| **Owner** | An account that manages projects and apps. Logs in with email and password. |
| **Project** | Where code lives: a set of tracker repositories that receive issues. Has exactly one *default repository*. Holds no credentials. |
| **App** | A mobile app that reports bugs, the *source* of issues. Holds SDK keys and its own provider connections, and links to projects (many-to-many). |
| **App key** | The SDK credential (`msk_…`). Only its SHA-256 hash is stored. |
| **Tracker** | An issue tracker product: `github`, `gitlab` or `bitbucket`. |
| **Provider** | The code integration for one tracker (`provider.Provider`). |
| **Provider connection** | An **app's** grant on a tracker account: its credentials. On GitHub, one App installation. Apps of one owner may share an installation (one row each); two owners never can. |
| **Attach** | Giving an app a connection the owner already has on another app, without a new install. |
| **External ID** | The tracker's identifier for a connection (GitHub: the installation ID). Opaque outside the provider. |
| **Project repository** | A repository linked to a project, named by tracker and full name. |
| **Issue target** | A project's default repository paired with the reporting app's connection that covers it (`issue.Target`). |
| **Covers** | `Provider.Covers(connection, repository)`: whether a connection can reach a repository at all, decided without a network call. On GitHub, the repository owner is the installation's account. |
| **Opener** | `issue.Opener`, which opens, comments on and checks issues on one connection. |
| **Draft** | `issue.Draft`, an issue before its repository is chosen. |
| **Ref** | `issue.Ref`, the tracker's reference to an opened issue (`owner/name#N` + URL). |
| **OAuth tracker** | A tracker reached with a per-connection OAuth token (GitLab, Bitbucket), as opposed to GitHub's App installations. |
| **Connection secret** | An OAuth tracker connection's token, stored AES-256-GCM-encrypted in `connection_secrets`, bound to that connection. |
| **State** | A random, single-use, 10-minute token that ties a tracker callback to the owner who started it. |

## Feature map against the PRD

| PRD item | Status in this backend |
|---|---|
| FR-01 / FR-04: project keys, create, rotate, revoke | done, as **app keys** (`/apps/{id}/keys`) |
| FR-46: issue opened automatically | done: the worker opens the issue of every accepted report |
| FR-49: labels, assignees, milestone | set by routing rules, carried by `Draft`; GitHub resolves milestones by title |
| FR-51: two-way status | schema and `Opener.IsOpen` ready; no sync job yet |
| FR-55: comment on a duplicate | schema, fingerprint and `Opener.Comment` ready; logic not built |
| FR-58 / FR-59: routing, default repository | done: routing rules by app, severity and category, else the default repository; no issue templates yet |
| Per-app tracker credentials | done: connections belong to apps; `GET /apps/{id}/targets` previews the routing |
| FR-62: failed-delivery retries | the worker retries with backoff and waits out rate limits; no admin view yet |
| FR-70: other trackers | GitHub, GitLab (gitlab.com or self-managed) and Bitbucket Cloud implemented |
| §3.4: least-privilege GitHub access | GitHub App with Issues R/W + Metadata R |

## Known gaps and follow-ups

These are deliberate omissions or limitations in the current code:

- **No GitHub webhooks.** `suspended_at` is never set, and an uninstalled App
  is only detected when a call fails.
- **JWTs can't be revoked.** Changing a password doesn't log out other
  sessions, and there are no refresh tokens.
- **No rate limiting** on `/auth/login`, `/auth/register` or the provider
  callback. `*ratelimit.Limiter` is ready for this; today it throttles
  `POST /apps/{id}/test-issue` and `POST /reports`.
- **No view of failed deliveries.** A report the worker gave up on keeps
  `processed_at` empty and is only in the logs (see [worker.md](worker.md)).
- **The access log contains full URLs**, including the one-time OAuth
  `code` and `state` on the callback. Both are single-use and short-lived,
  but consider redacting query strings on that route.
- **Linking the first repository at the same moment from two requests** can
  have both try to become the default. The partial unique index stops it, but
  the loser gets a misleading "already linked" 409.
- **OAuth refreshes are serialised per process only.** Two backend
  instances could refresh the same GitLab token at once; one would get
  `invalid_grant` and the app would have to reconnect. Add a Postgres
  advisory lock before scaling out (see
  [gitlab-bitbucket.md → Limitations](gitlab-bitbucket.md#limitations)).
- **No key rotation** for `secrets.encryption_key`, and revoked OAuth
  connections are not suspended automatically.
- **`Covers` is a local check.** On GitHub it compares the repository owner
  with the installation's account. A repository on that account that wasn't
  selected for the installation looks covered in `GET /apps/{id}/targets`,
  and fails only when the issue is opened.
- **Apps sharing an installation share its GitHub rate limits**, because
  GitHub counts per installation, not per m-Seismo app.
- **The connection migration's down path is lossy**: copies collapse to one
  per installation, and project repositories that no connection's account
  owns are deleted.
- **No Go-level DB tests.** Owner isolation and SQL behaviour are covered
  by the API scripts against the dev stack, not by `go test`
  (see [testing.md](testing.md)).
- **`backend/Makefile` uses `sed -i ''`** (BSD/macOS syntax) in `db-up` and
  `db-down`, which fails with GNU sed on Linux and Git Bash. This predates the
  work documented here.
