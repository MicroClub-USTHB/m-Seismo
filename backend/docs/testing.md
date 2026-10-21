# Testing

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

- **Go-level DB tests.** The SQL behaviour (owner scoping, cascades, the
  one-default index, the upsert guard) is only covered by the API scripts.
  Those need the compose stack and don't run in `go test ./...`. A
  testcontainers-based Go suite would also bring it into CI.
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
