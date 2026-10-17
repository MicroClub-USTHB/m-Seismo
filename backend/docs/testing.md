# Testing

## Running

```sh
cd backend
go test ./...          # unit tests and graph validation, no services needed
go vet ./...
go test -tags e2e -run TestOpenForAppE2E .   # full stack; see below
```

## What is covered

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

- **SQL and owner isolation.** Unit tests fake the database, so the
  `WHERE … AND owner_id = $2` clauses, the cascades, the partial unique index
  and the upsert guard aren't tested. The next step is an integration suite
  against a real Postgres (testcontainers-go or a CI service container) that
  applies the migrations and runs the services.
- **Handlers of the resource packages** (`owner`, `project`, `app`,
  `provider`) beyond the shared middleware. They are thin, but request
  decoding and status codes per route are only checked by hand.
- **Redis state store.** Only the in-memory fake is tested.

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
