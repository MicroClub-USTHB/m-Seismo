# Testing

The [live Sentry verification](live-sentry-verification.md) records a successful
real browser crash test against self-hosted Sentry, in addition to the automated
isolated suite below.

## Full isolated unit and integration suite

From the repository root:

```sh
make test-integration
# Equivalent:
python3 backend/scripts/test-integration.py
```

Requires Docker, Python 3, Bash/curl, Go matching `backend/go.mod` with a C compiler
for race detection, and Node 20+.
The runner creates disposable PostgreSQL, Redis and NATS containers with ports
bound only to localhost, applies all migrations, and runs:

- Every Go package with `-race`, `-count=1` and the `integration` build tag.
- The PostgreSQL report store, Redis rate limiter and JetStream integration tests
  that otherwise skip when their `SEISMO_TEST_*` variables are missing.
- `TestCrashProviderHTTPIntegration`: the production dependency graph and real
  owner/SDK HTTP routes, including a JavaScript adapter upload, encrypted tokens,
  Sentry failures/recovery, screenshots, queue publication and access isolation.
- `TestSentryReportQueueToIssueIntegration`: a persisted Sentry-only report moves
  through the real JetStream consumer and processor into a crash issue draft with
  the exact event ID, resolved stack and Sentry URL. The tracker write boundary
  and Sentry API are simulated.
- Go vet and the six dependency-free web adapter tests.
- Every existing API script against a compiled backend and fake GitHub.

Only external Sentry and GitHub are simulated. No vendor credentials are needed.
Containers, backend processes, temporary keys and test state are removed even
when a test fails. The runner does not use existing application databases or
the API scripts' normal saved credentials.

Optional `--go`, `--postgres-image`, `--redis-image` and `--nats-image` arguments
select installed tooling or cached service images. Defaults match the development
stack: PostgreSQL 17, Redis 7 and NATS 2 with JetStream.

To verify upgrading an existing pre-Sentry database using the same migration
tracking as deployment, also supply dbmate:

```sh
python3 backend/scripts/test-integration.py --dbmate /path/to/dbmate
```

This applies the existing migrations first (including report deliveries), then
upgrades to the Sentry migration, rolls it back, reapplies it and checks a repeat
upgrade is a no-op. It verifies both tables coexist and rejects duplicate migration
versions. All operations use the runner's disposable database.

The iOS suite requires a macOS/Xcode host and is separate from this runner. The
Rust agent has its own checks (`make agent-check`); its optional Docker integration
tests are run with `make agent-integration`.

To use existing **isolated test services**, apply migrations and supply
`SEISMO_TEST_DATABASE_URL`, `SEISMO_TEST_REDIS_URL` and `SEISMO_TEST_NATS_URL`:

```sh
cd backend
go test -p 1 -count=1 -race -tags integration ./...
```

Run packages serially (`-p 1`): the integration tests share a JetStream work-queue
stream, and concurrent consumers would interfere. The HTTP test's optional web
subtest needs Node 20+; the full runner verifies this prerequisite up front.

## Running

```sh
cd backend
go test ./...          # unit tests and graph validation, no services needed
go vet ./...
go test -tags e2e -run TestOpenForAppE2E .   # full stack; see below
```

## API tests against the running stack

`backend/scripts/api/` holds bash and curl scripts that exercise every
endpoint over HTTP: CRUD, validation, owner isolation, bearer-token handling,
the GitHub connect flow and the issue service. They run against the dev
stack, whose fake GitHub makes the GitHub parts fully automatic:

```sh
make dev-up     # deploy/compose.dev.yml
make api-test   # backend/scripts/api/all.sh
```

These scripts cover the SQL behaviour that the unit tests fake out: owner
scoping, cascades, the one-default index, and the upsert guard. See
[scripts/api/README.md](../scripts/api/README.md) for the list of checks,
token handling and real-GitHub mode.

## What is covered by unit tests

| Package | Test | Approach |
|---|---|---|
| `backend` (main) | `TestGraph` | `fx.ValidateApp(Options())`: every dependency is provided, and no constructor runs |
| `auth` | password and token tests | real bcrypt and JWT; injected clock for expiry |
| `httpx` | `TestErrorStatus`, `TestAuthMiddleware` | `httptest` against the real router and middleware |
| `issue` | `OpenForApp` / `OpenInRepository` cases | fake `Querier` and `OpenerSource` |
| `provider` | connect flow, state replay, cross-owner conflict, `VerifyRepository` | in-memory `StateStore`, fake `Provider` and `Querier` |
| `provider/github` | install URL, verified install, repositories, opener | `httptest.Server` imitating github.com and api.github.com; real RSA key; checks auth headers |
| `app` | key lifecycle, platform validation | fake `Querier` |
| `project` | slug derivation and validation | pure functions |

## Patterns

**Partial fakes of `db.Querier`.** Test fakes embed the interface and
override only what the code under test should call:

```go
type fakeQuerier struct {
    db.Querier                       // nil: any other call panics
    targets []db.ListIssueTargetsForAppRow
}
func (f *fakeQuerier) ListIssueTargetsForApp(...) (...) { return f.targets, nil }
```

A panic from the nil embedded interface means the code made a query the
test didn't expect, which is usually worth knowing.

**Fake GitHub.** `github_test.go` points `APIURL` and `WebURL` at one
`httptest.Server` and routes by method and path. Because ghinstallation
really mints installation tokens through
`POST /app/installations/42/access_tokens`, the tests exercise the whole
token path, including JWT signing with a generated RSA key.

## Not covered yet

- **Broader Go-level DB coverage.** Report persistence and crash-provider
  connections now have real PostgreSQL tests. Other domain SQL behavior, such
  as repository-default constraints, is exercised by the API scripts.
- **A real GitHub App** is only exercised manually (`GITHUB_MODE=real`) or
  by the `e2e` Go test.

## The e2e test

`e2e_test.go` (build tag `e2e`) runs the production graph and opens a
**real GitHub issue**.

Prerequisites:

1. Postgres, Redis and NATS running, and `make db-up` applied.
2. `.env` with a working GitHub App (see [github.md](github.md#creating-the-github-app)).
3. Through the API: an owner, an app linked to a project, a GitHub connection,
   and a default repository on that project (the curl walkthrough in
   [api.md](api.md#walkthrough-with-curl) does all of this).
4. `SEISMO_E2E_APP_ID=<app uuid> go test -tags e2e -run TestOpenForAppE2E -v .`

It logs every opened issue, labelled `m-seismo-e2e`. Close them afterwards.
