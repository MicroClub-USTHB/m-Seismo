# 9. Testing

## Strategy

| Level | What | Where | Database |
| --- | --- | --- | --- |
| Unit | Pure functions: cursors, slugs, (planned) fingerprints, encryption, templates | Next to the code, `*_test.go` | none |
| Integration (API) | Full HTTP stack → services → real PostgreSQL | [`internal/httpapi/api_test.go`](../internal/httpapi/api_test.go) | real, one per test |
| Adapter *(planned)* | Provider clients against `httptest` servers replaying recorded responses | `internal/vcs/*/…_test.go` | none |
| End-to-end *(planned)* | Real GitHub sandbox repository, opt-in via environment variable | `internal/issues/e2e_test.go` | real |

**Why integration tests hit a real database instead of mocks:** most of the
logic that matters lives in SQL and constraints. That includes ownership
joins, unique indexes, cascades, partial updates, keyset pagination and,
later, deduplication upserts and `SKIP LOCKED` claims. A mock would test
nothing of that. A real PostgreSQL per test costs about 100–200 ms after the
first start.

**Why tests go through HTTP:** a single request exercises routing, decoding,
validation, the service, SQL, error mapping and the response format. Tests
assert what a client actually sees (status, body, problem type), so
internals can be refactored without rewriting tests.

## The database harness

[`internal/testutil`](../internal/testutil/testutil.go) provides
`testutil.NewDB(t) *pgxpool.Pool`:

1. **Find a server**, once per test binary:
   - if `TEST_DATABASE_URL` is set, use it (the role needs `CREATEDB`);
   - otherwise start `postgres:17-alpine` with testcontainers-go. It is
     reaped automatically when the test process exits.
2. **Per test**: `CREATE DATABASE mseismo_test_<random>`, connect, and run
   **all migrations**. Every run therefore also tests the migrations from an
   empty database.
3. **Cleanup** (`t.Cleanup`): close the pool, then
   `DROP DATABASE … WITH (FORCE)`.
4. **No server available** (no Docker, no URL): the test is **skipped** with
   a message saying how to enable it. It does not fail.

Each test has its own database, so tests can run in parallel and never see
each other's rows. There is no truncation or ordering between tests.

## Running

```sh
make backend-test                 # everything

cd backend
go test ./...                     # same
go test -count=1 -v ./internal/httpapi           # integration tests, no cache, verbose
go test -run TestUsersCRUD ./internal/httpapi    # one test
TEST_DATABASE_URL='postgres://me:pw@localhost:5432/postgres?sslmode=disable' go test ./...
```

- The first run with Docker takes ~50 s (image pull and container start).
  Later runs take a few seconds.
- **Check for skips.** `ok` with skipped tests still looks green. Run with
  `-v` and look for `--- SKIP` when it matters, e.g. in CI.
- On Windows, testcontainers needs Docker Desktop running. If it reports
  "rootless Docker is not supported on Windows", Docker isn't reachable:
  start Docker Desktop and retry.

## What is covered today

| Test | Checks |
| --- | --- |
| `TestAuthRequired` | No token, wrong token and a missing `Bearer` prefix → 401 problem; `/healthz` open |
| `TestUsersCRUD` | Create with trimming; audit row written; case-insensitive email conflict → 409; multi-field validation → 422 listing all fields; unknown field → 400; get, 404 for a missing user and for a malformed id; PATCH absent vs null; `updated_at` trigger; null name → 422; delete then 404; delete audited |
| `TestUsersPagination` | Pages of 2 over 5 users, no gaps or duplicates; bad cursor and bad limit → 400 |
| `TestProjectsAndAppsCRUD` | Slug derivation and conflict; invalid slug; unknown user → 404; **another user cannot get, patch, delete or add apps to the project**; settings reset with null; app defaults; bundle id + environment uniqueness; invalid platform; another user's app → 404; partial PATCH; list; delete; **cascade from user to projects to apps** |
| `TestCursorRoundTrip`, `TestTrim` | Cursor encoding and decoding, invalid cursors, page trimming |
| `TestSlugify` | ASCII folding, separators, empty input |

## Writing a new test

- Use the `client` helper in `api_test.go`:
  - `c.mustStatus(want, method, path, body, &out)`: `body` can be a Go value
    (marshalled to JSON) or a raw string (to send exact JSON, e.g. `null`);
  - `c.auditCount(action)` checks audit rows.
- **Decode into a fresh variable** for each response. `encoding/json` leaves
  pointer fields that are absent from the new JSON unchanged, so reusing a
  struct can hide bugs. This exact issue happened once while writing these
  tests.
- For every new resource, test at least: happy path, validation (422 with
  all fields), conflict (409), **another user's id → 404**, PATCH absent vs
  null, delete and cascade, and the audit row.
- Add `t.Parallel()` freely: databases are isolated.

## Not yet in place

- **No CI workflow** in the repository yet. Proposed: a GitHub Actions job on
  `ubuntu-latest` (Docker is available) running `make backend-fmt
  backend-vet backend-test`, and failing if any test is skipped
  (`go test -json` + a check for `"Action":"skip"`).
- No race detector run yet (`go test -race` needs CGO on Windows; run it in
  CI on Linux).
- No coverage reporting.
- No load test for the NFR-07 target (100 reports/s). It belongs with the
  issues service.
