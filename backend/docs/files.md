# File-by-file reference

This page describes every file added or changed by the owners / projects /
apps / providers / issue-service work. For each file it covers what the file
is for, what it contains, and anything not obvious from the code. Files are
grouped by directory, roughly in dependency order (lowest level first).

Generated files (`internal/db/*.go`) are described here, but **never edit
them**. Change the SQL and regenerate.

- [Root](#root)
- [Config: `db/` and `sqlc.yaml`](#db-and-sqlcyaml)
- [`internal/config`](#internalconfig)
- [`internal/db` (generated)](#internaldb-generated)
- [`internal/infra`](#internalinfra)
- [`internal/apperr`](#internalapperr)
- [`internal/httpx`](#internalhttpx)
- [`internal/auth`](#internalauth)
- [`internal/owner`](#internalowner)
- [`internal/issue`](#internalissue)
- [`internal/provider`](#internalprovider)
- [`internal/provider/github`](#internalprovidergithub)
- [`internal/project`](#internalproject)
- [`internal/app`](#internalapp)
- [Dev stack, test-issue action and rate limits](#dev-stack-test-issue-action-and-rate-limits)

---

## Root

### `backend/main.go` (changed)

The entry point. `main()` runs `fx.New(Options()).Run()`, which blocks until
SIGINT or SIGTERM and then runs the shutdown hooks.

`Options()` returns every module in one `fx.Option`. It is a separate,
exported-in-package function so `main_test.go` and `e2e_test.go` build
**exactly** the production graph. The previous placeholder
`fx.Invoke(run)`, which only logged a line, was removed. The HTTP server's
lifecycle hook is now what keeps the process doing something.

Module order doesn't affect correctness, because fx resolves by type. It is
kept bottom-up for readability: config → infra → http → auth → resources →
providers → issue.

### `backend/main_test.go` (new)

`TestGraph` calls `fx.ValidateApp(Options())`. It checks that every
constructor's parameters can be satisfied, including the value groups,
**without running any constructor**, so it needs no database. It catches a
forgotten `fx.Provide` or a missing interface binding at test time instead of
at boot.

### `backend/e2e_test.go` (new, build tag `e2e`)

`TestOpenForAppE2E` starts the real graph (`fxtest.New(t, Options(),
fx.Populate(&svc))`), so Postgres, Redis, NATS and a configured GitHub App
must be reachable. It then calls `issue.Service.OpenForApp` for the app in
`SEISMO_E2E_APP_ID`. Each opened issue's ref and URL are logged, and any
per-target error fails the test. Without the env var it skips. It is excluded
from `go test ./...` by the `//go:build e2e` tag.

### `backend/.env.example` (changed)

New variables: `SEISMO_SERVER_PUBLIC_URL`, `SEISMO_SERVER_FRONTEND_URL`,
`SEISMO_AUTH_JWT_SECRET`, `SEISMO_AUTH_JWT_TTL` and the `SEISMO_GITHUB_*`
set. `JWT_SECRET` is left empty on purpose; see `config.go`.

### `backend/README.md` (changed)

A short overview, setup steps, the GitHub App checklist, and a table of
routes. These `docs/` pages are the in-depth version.

### `backend/go.mod` / `go.sum` (changed)

New direct dependencies:

| Module | Version | Why |
|---|---|---|
| `github.com/go-chi/chi/v5` | v5.5.0 | router and middleware; compatible with `net/http` |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | owner access tokens |
| `github.com/google/uuid` | v1.6.0 | the ID type in generated code and DTOs |
| `github.com/google/go-github/v93` | v93.0.0 | GitHub REST client |
| `github.com/bradleyfalzon/ghinstallation/v2` | v2.19.0 | GitHub App JWTs and installation tokens |
| `golang.org/x/crypto` | v0.58.0 (was indirect) | bcrypt |

---

## `db/` and `sqlc.yaml`

### `db/migrations/20261017120000_owners_apps_providers.sql` (new)

The single migration for this feature. The step-by-step explanation, the
down path and the warning about existing data are in
[data-model.md → The migration](data-model.md#the-migration).

### `db/schema.sql` (changed)

The schema snapshot sqlc compiles against, in pg_dump format. It now
contains both enums (`app_platform`, `tracker`), the eight application
tables, their constraints and indexes, and the three migration versions.
Normally `make db-up` regenerates it.

### `db/queries/owners.sql` (new)

Owner CRUD. `GetOwnerByEmail` uses a named parameter (`@email::text`) and
compares `lower(email) = lower(@email)`. That matches the unique expression
index, so the lookup can use it.

### `db/queries/projects.sql` (rewritten)

Before: global, unscoped project queries plus `project_keys` queries. Now:
five queries, each one owner-scoped (`WHERE id = $1 AND owner_id = $2`).
`Update` and `Delete` are new. The key queries moved to `apps.sql` as
`app_keys` queries. Deletes are `:execrows`, so the service can tell "deleted"
from "not found or not yours".

### `db/queries/project_repositories.sql` (rewritten)

Link, list, default handling and unlink, plus the two **issue-targeting
queries** `GetIssueTarget` and `ListIssueTargetsForApp`, which join through
to the connection's `tracker` and `external_id`.
`GetDefaultProjectRepository` was removed; the targeting queries replace it.

### `db/queries/provider_connections.sql` (new)

The notable query is `UpsertProviderConnection`. Its
`ON CONFLICT … DO UPDATE … WHERE owner matches` handles both re-installs by
the same owner and attempts by a *different* owner to claim the installation
(no row is returned, so the service answers 409) in one statement, with no
race window.

### `db/queries/apps.sql` (new)

App CRUD, key create/list/revoke/lookup, and app↔project
link/unlink/list. `LinkAppProject` is `ON CONFLICT DO NOTHING`, so PUT is
idempotent.

### `db/queries/issues.sql` (new)

`CreateIssue` is used now. `GetOpenIssueByFingerprint` and `SetIssueState`
are written ahead of time for dedup and status sync.

### `sqlc.yaml` (changed)

Adds `emit_pointers_for_null_types` and type overrides so generated code uses
`uuid.UUID`, `*uuid.UUID`, `time.Time` and `*time.Time` instead of the
`pgtype` wrappers. See [data-model.md → sqlc
configuration](data-model.md#sqlc-configuration), including why the
timestamp override must say `pg_catalog.timestamptz`.

---

## `internal/config`

### `schema.go` (changed)

New structs:

- `ServerConfig` gains `PublicURL` (the externally reachable base, for
  building callback URLs) and `FrontendURL` (where the provider callback
  sends the browser).
- `AuthConfig{JWTSecret string; JWTTTL time.Duration}`.
- `GitHubConfig{AppID, AppSlug, ClientID, ClientSecret, PrivateKey,
  PrivateKeyPath, APIURL, WebURL}`, each documented in the file.

### `defaults.go` (changed)

Defaults for every new key: `server.public_url=http://localhost:8080`,
`server.frontend_url=http://localhost:3000`, `auth.jwt_ttl=24h`,
`github.api_url=https://api.github.com/`, `github.web_url=https://github.com/`,
and empty strings or zero for the secrets and IDs.

### `config.go` (changed)

`Load` now fills `Auth` and `GitHub` and then calls `validate()`:

- **`auth.jwt_secret` is required unless `env == "development"`.** In
  development an empty secret falls back to the constant `devJWTSecret`, so a
  fresh checkout runs with no setup. Tokens signed with it are worthless
  anywhere else, and any other environment refuses to start without a real
  secret. `env` defaults to `production`, so the fallback only applies when
  `SEISMO_ENV=development` is set explicitly (`.env.example` and the dev
  compose stack do).
- `log_level` must be `debug`, `info`, `warn` or `error`; it sets the level
  of the default `slog` logger.
- `auth.jwt_ttl` must be positive.

---

## `internal/db` (generated)

All of these are generated by sqlc v1.30.0 from `db/queries` and
`db/schema.sql`.

| File | Contents |
|---|---|
| `db.go` | `DBTX`, `New(db DBTX) *Queries`, `(*Queries).WithTx(pgx.Tx)` |
| `models.go` | Row structs (`Owner`, `Project`, `ProviderConnection`, `ProjectRepository`, `App`, `AppKey`, `AppProject`, `Issue`), enums `Tracker` and `AppPlatform` with `Scan`, `Value` and `Null*` variants |
| `querier.go` | the `Querier` interface; services depend on it so tests can pass fakes |
| `owners.sql.go`, `projects.sql.go`, `project_repositories.sql.go`, `provider_connections.sql.go`, `apps.sql.go`, `issues.sql.go` | one method plus a `…Params` and/or `…Row` struct per query |

Join queries get their own row types: `ListProjectRepositoriesRow`,
`GetIssueTargetRow` and `ListIssueTargetsForAppRow`. The last two have
identical fields, so tests convert between them directly.

---

## `internal/infra`

### `module.go` (changed)

Besides the existing pool, Redis and NATS constructors, it now provides:

- `NewQueries(pool) *db.Queries`, the concrete sqlc type, for services that
  need `WithTx`;
- `func(q *db.Queries) db.Querier`, the same instance exposed as the
  interface, for every other service.

The package comment no longer says "Later: … the GitHub client", because
GitHub lives in `provider/github`.

---

## `internal/apperr`

### `apperr.go` (new)

A transport-neutral error vocabulary.

- `type Kind int` with `KindInternal` (the zero value, so unknown means
  internal), `KindNotFound`, `KindConflict`, `KindInvalid` and
  `KindUnauthorized`.
- `type Error struct{ Kind; Msg; Err }`. `Msg` must be **safe to show to
  clients**. `Err` optionally wraps a cause and is reachable through
  `Unwrap`, so `errors.Is` and `errors.As` see through it.
- Constructors `NotFound`, `Conflict`, `Invalid` and `Unauthorized`.
- `Classify(err) (Kind, string)` works in priority order: an `*apperr.Error`
  anywhere in the chain, then `pgx.ErrNoRows` → NotFound, then `*pgconn.PgError`
  with code `23505` → Conflict or `23503` → Invalid, otherwise Internal with
  the message `"internal error"`.

Services also use `Classify` themselves to *specialise* messages, for example
turning a unique violation into "an account with this email already exists".

The package imports pgx only for its error types. This keeps the SQL-error
mapping in one place, and `apperr` remains a leaf package, imported by others
and importing none of ours.

---

## `internal/httpx`

### `server.go` (new)

- **`Routes` interface** (`Mount(chi.Router)`), which each resource handler
  implements.
- **`AsRoutes(f)`** wraps a handler constructor with
  ``fx.Annotate(f, fx.As(new(Routes)), fx.ResultTags(`group:"routes"`))``,
  which adds it to the routes group.
- **`NewRouter`** builds the chi router: `RequestID → RealIP → Logger →
  Recoverer`, then `GET /healthz`, then `/api/v1`, on which each `Routes` is
  mounted. Routes are mounted in fx's group order, which doesn't matter
  because route patterns don't overlap.
- **`NewServer`** creates an `http.Server` with `ReadHeaderTimeout: 10s`
  (protection against Slowloris) and registers the lifecycle hooks.
  `net.Listen` runs synchronously in `OnStart`, so a bind error fails startup;
  `Serve` runs in a goroutine. `OnStop` calls `Shutdown(ctx)`, which drains
  requests within fx's stop timeout.
- **`Module`** provides the router, the server and the auth middleware, and
  `fx.Invoke`s the server so it is always built.

### `respond.go` (new)

- `JSON(w, status, v)` sets the content type, writes the status and encodes
  `v` (nil means no body).
- `NoContent(w)` writes 204.
- `Error(w, r, err)` calls `apperr.Classify`, then `statusFor`, then writes
  `{"error": msg}`. **Only 500s are logged**, with method, path, request ID
  and the full error. 4xx responses are expected client mistakes and are
  already in the access log.
- `Decode(w, r, &v)` reads at most 1 MiB with `http.MaxBytesReader`, uses
  `DisallowUnknownFields`, and then makes sure there's no second JSON value
  in the body. Any failure is `apperr.Invalid` (422). Rejecting unknown
  fields catches client typos such as `"is_defualt"` instead of silently
  ignoring them.
- `UUIDParam(r, name)` parses a chi path parameter. A malformed value is
  **NotFound** rather than Invalid, because to the client `/projects/abc`
  simply doesn't exist.

### `auth.go` (new)

- `TokenVerifier` interface (`Verify(token) (uuid.UUID, error)`). Declared
  here so `httpx` doesn't depend on `auth`.
- `AuthMiddleware` is a named `func(http.Handler) http.Handler` type, so fx
  can inject it by type.
- `NewAuthMiddleware(v)` requires the `Bearer ` prefix, verifies the token,
  and on success stores the owner ID in the context. Errors are 401 with
  either "missing bearer token" or "invalid or expired token". The reason
  verification failed is not exposed.
- `WithOwnerID(ctx, id)` / `OwnerID(ctx)` use a private context key type,
  `ownerIDKey struct{}`, which no other package can collide with. `OwnerID`
  returns `uuid.Nil` when called outside the middleware. Handlers only call it
  behind the middleware.

### `httpx_test.go` (new)

`TestErrorStatus` checks the kind-to-status table, including a wrapped
`pgx.ErrNoRows` and a raw `PgError`. `TestAuthMiddleware` drives the real
router with a fake verifier: no header, wrong scheme, bad token, good token
(the owner ID is put in the context).

---

## `internal/auth`

### `auth.go` (new)

- `Module` provides `*Issuer`, and the same instance as `httpx.TokenVerifier`.
- `HashPassword` / `CheckPassword` use bcrypt at `DefaultCost`.
- `Issuer` holds the secret, the TTL and an injectable `now` clock (for
  tests).
- `Issue(ownerID) (Token, error)` creates an HS256 JWT with
  `RegisteredClaims{iss: "m-seismo", sub: ownerID, iat, exp}`. `Token` is the
  JSON shape returned to clients: `access_token`, `token_type: "Bearer"`,
  `expires_at`.
- `Verify(token)` parses with `WithValidMethods(["HS256"])`, which blocks
  `alg=none` and algorithm-confusion attacks, plus `WithIssuer`,
  `WithExpirationRequired` and `WithTimeFunc(now)`. It then parses `sub` as a
  UUID.

Tokens aren't stored and can't be revoked individually. Changing
`auth.jwt_secret` invalidates all of them. Revocation on a password change is
a follow-up.

### `auth_test.go` (new)

Password round-trip; token round-trip; rejection of a token signed with
another secret, an expired token (using a shifted clock) and garbage input.

---

## `internal/owner`

### `service.go` (new)

- `Owner` DTO, without the password hash.
- `NewService` pre-computes a **dummy bcrypt hash** used when a login email
  doesn't exist, so both branches cost one bcrypt comparison.
- `Register`: normalise the email (trim, lower-case, `net/mail.ParseAddress`
  must give back the same bare address, which rejects `"Bob <b@x>"`), check
  the name and password, hash, insert, map a unique violation to a friendly
  409, and issue a token.
- `Login`: look up the owner case-insensitively, compare the password, and
  return the same `401 invalid email or password` for both failure cases.
- `Update`: load the owner, require `current_password` for email or password
  changes, merge the non-nil fields, and write every column (`UpdateOwner`
  sets them all).
- `Delete`: `:execrows`; 0 rows means 404.
- Validation helpers: `normaliseEmail`, `validateName` (1–200),
  `validatePassword` (8–72 bytes).

### `handler.go` (new)

`Module` provides the service and the handler. Routes:

```
POST /auth/register      public
POST /auth/login         public
/owners/me  (behind AuthMiddleware)
  GET    PATCH    DELETE
```

`sessionResponse{owner, token}` is the register and login body.

---

## `internal/issue`

### `issue.go` (changed)

Only the package doc comment changed. Tracker implementations now live in
`internal/provider/<tracker>/` instead of `internal/issue/<tracker>/`. The
vendor-isolation rule is unchanged, and the doc names `OpenerSource` as the
second interface domain code depends on. `Ref`, `Issue` and `Opener` are
unchanged.

### `service.go` (new)

`Module`, `OpenerSource`, `ErrNoTargets`, `Draft`, `Result`, `Service`,
`OpenForApp`, `OpenInRepository`, and the private `target` and `open`
helpers. The behaviour is documented in full in
[issue-service.md](issue-service.md).

### `service_test.go` (new)

Uses a `fakeQuerier` that embeds `db.Querier` (any method not overridden
panics, which shows when the service makes an unexpected query) and a
`fakeOpener` that fails chosen repositories. Cases:

- mixed success, tracker failure and an unsupported tracker → `nil` error
  with three results, and only the success recorded, with `app_id` and
  `fingerprint`;
- every target failing → error;
- no targets → `ErrNoTargets`;
- empty title → error;
- `OpenInRepository` records a nil `app_id`.

---

## `internal/provider`

### `provider.go` (new)

- `Account{ExternalID, Login, Type}` is what `CompleteInstall` returns.
- `Repository{full_name, private, url}`.
- The `Provider` interface (six methods; see
  [architecture.md → Tracker plug-in model](architecture.md#tracker-plug-in-model)).
- `AsProvider(f)` tags a constructor returning `[]Provider` with
  `group:"providers,flatten"`. A slice is used so a provider can register
  *nothing* when it isn't configured.
- `Registry` maps `db.Tracker` to `Provider`:
  - `Get` returns 404 "provider X is not available" for unknown trackers;
  - `Trackers()` returns them sorted, for `GET /providers`;
  - `Opener(ctx, tracker, externalID)` implements `issue.OpenerSource`. The
    compile-time assertion `var _ issue.OpenerSource = (*Registry)(nil)`
    keeps it that way.

### `service.go` (new)

- `Connection` DTO.
- **`StateStore`** interface (`Put` with TTL, `Take` = get and delete) and
  its Redis implementation, `redisStateStore`, which uses `SET … EX` and
  `GETDEL` (Redis ≥ 6.2). `GETDEL` makes taking a state atomic, so two
  concurrent callbacks with the same state can't both succeed.
- `Service`:
  - `StartConnect`: a 32-byte random state, stored as
    `provider_state:<state>` → `"<tracker>:<ownerID>"` for 10 minutes;
    returns `InstallURL`.
  - `FinishConnect`: take the state, check its tracker, recover the owner,
    `CompleteInstall`, upsert; `pgx.ErrNoRows` from the upsert becomes 409.
  - `ListConnections` and `Disconnect` (owner-scoped).
  - `ListRepositories` and `VerifyRepository` share
    `connection(ctx, owner, id)`, which loads the connection owner-scoped,
    rejects suspended ones, and finds the provider.

### `handler.go` (new)

`Module` provides the registry, the registry as `issue.OpenerSource`, the
Redis state store, the service and the handler. Routes are under
`/providers`:

```
GET    /{tracker}/callback               public — GitHub redirects here
GET    /                                 auth
GET    /{tracker}/connect                auth
GET    /connections                      auth
DELETE /connections/{id}                 auth
GET    /connections/{id}/repositories    auth
```

chi gives static segments priority over parameters, so `/connections/...`
never matches `{tracker}`. `callback` turns every outcome into a `302` to the
frontend with `status`, `provider` and either `connection_id` or `reason`,
and logs internal errors.

### `service_test.go` (new)

Uses an in-memory `memStates`, a `fakeProvider` and a `fakeQuerier`. Covers:
the full connect flow; that a state can't be replayed; an unknown tracker;
a missing state; a state issued for another tracker; an installation already
owned by someone else (409); and `VerifyRepository` with a reachable
repository, an unreachable one, another owner's connection and a suspended
connection.

---

## `internal/provider/github`

### `github.go` (new)

- **`Module`** contains `provider.AsProvider(FromConfig)`.
- **`FromConfig(cfg)`**:
  - returns no provider, with a warning, when `app_id == 0`;
  - otherwise loads the PEM from `private_key` or `private_key_path`,
    un-escapes literal `\n`, and calls `New`.
- **`Options`** are the explicit settings `New` takes, so tests can build a
  provider without a `config.Config`.
- **`New(o)`**:
  - requires the slug and client credentials;
  - defaults and normalises the URLs (trailing slash);
  - parses the private key into a `ghinstallation.AppsTransport` and points
    its `BaseURL` at `APIURL` (without the trailing slash, which is the form
    ghinstallation expects);
  - creates a 15-second-timeout `http.Client` for OAuth and user calls.
- **`InstallURL`**:
  `{web}apps/{slug}/installations/new?state=…`, with the slug path-escaped
  and the state query-escaped.
- **`CompleteInstall`**: the verified install (see
  [github.md](github.md#why-the-callback-needs-the-oauth-code)).
- **`exchangeCode`**: a plain form POST to `{web}login/oauth/access_token`
  with `Accept: application/json`. An error reply or an empty token from
  GitHub becomes 422. It is done by hand because `golang.org/x/oauth2` would
  be a dependency used for one call.
- **`installationClient`**: caches one `*github.Client` per installation.
  Each uses `WithTransport(ghinstallation.NewFromAppsTransport(...))`,
  `WithURLs(APIURL)` and a 30-second timeout.
- **`ListRepositories`**, **`HasRepository`**: see
  [github.md → Listing and verifying](github.md#listing-and-verifying-repositories).
  `ListRepositories` returns an empty slice, not nil, so the JSON is `[]`.
- **`Opener`**: wraps the installation client in an `opener`.
- Helpers:
  - `splitRepository` validates `owner/name` (exactly one slash, both parts
    non-empty);
  - `isNotFound` detects a `*github.ErrorResponse` with status 404;
  - `withSlash` adds the trailing slash.

### `opener.go` (new)

`opener` implements `issue.Opener`. It provides `Open` (with
milestone-by-title resolution), `Comment` and `IsOpen`, plus the
`formatRef` / `parseRef` pair for `owner/name#N`. See
[github.md → Opening issues](github.md#opening-issues).

### `github_test.go` (new)

`fakeGitHub` is an `httptest.Server` that serves both the web and API URLs.
It checks the `Authorization` header on each call, so the tests prove that
user calls use the user token and repository calls use the *installation*
token minted through `/app/installations/42/access_tokens`. A real
2048-bit RSA key is generated per test, so ghinstallation signs real JWTs.
Tests:

- `TestInstallURL`;
- `TestCompleteInstall`: the happy path, plus a forged installation ID, a bad
  code, a missing code, a missing installation and a pending org request,
  all of which must give 422;
- `TestRepositories`: listing, a reachable repository, a 404 one and a
  malformed name;
- `TestOpener`: open with milestone resolution, checking the posted JSON;
  comment; `IsOpen` on a closed issue; a malformed ref.

---

## `internal/project`

### `service.go` (new)

- `Project` and `Repository` DTOs. `Repository` includes `tracker` and
  `account_login`, taken from the join or from the verified connection.
- `Service` takes the **pool** and the concrete `*db.Queries`, because
  `LinkRepository` and `SetDefaultRepository` need transactions, and the
  `*provider.Service` for repository checks.
- `Create`: validate the name; derive the slug when missing (`slugify`:
  lower-case, runs of non-`[a-z0-9]` become `-`, trimmed, cut to 64); validate
  the slug; insert; a unique violation becomes "a project with this slug
  already exists".
- `List`, `Get`, `Update` (pointer-merge), `Delete`.
- `LinkRepository`:
  1. owner-scoped project check;
  2. trim and require the repository name;
  3. `providers.VerifyRepository`, which checks the connection's owner and
     suspension and calls the tracker;
  4. **in one transaction**: list the existing repositories → default if
     requested or if this is the first → `ClearDefault` if default →
     insert;
  5. a unique violation becomes "repository is already linked to this
     project".

  The tracker call happens *before* the transaction starts, so no database
  transaction stays open during network I/O.
- `SetDefaultRepository`: in a transaction, `ClearDefault` then
  `SetDefault`; 0 rows means 404 and the clear is rolled back.
- `UnlinkRepository`.
- `get` turns a missing row into "project not found".

### `handler.go` (new)

`Module` and the routes under `/projects` (all behind auth):

```
POST /   GET /
GET /{projectID}   PATCH /{projectID}   DELETE /{projectID}
GET /{projectID}/repositories   POST /{projectID}/repositories
PUT /{projectID}/repositories/{repositoryID}/default
DELETE /{projectID}/repositories/{repositoryID}
```

### `service_test.go` (new)

`slugify` examples (including Unicode and truncation, where every output
must pass `validateSlug`) and a list of invalid slugs.

---

## `internal/app`

### `service.go` (new)

- `App`, `Key` (with `Secret` set only on create, `omitempty`) and
  `LinkedProject` DTOs.
- CRUD with validation: name 1–200, `platform` from the enum, `bundle_id`
  1–255.
- Keys:
  - `generateKey`: `msk_` + base64url(32 bytes from `crypto/rand`), 47
    characters in all;
  - `hashKey`: SHA-256;
  - `CreateKey`: stores the hash and the first 12 characters, returns the
    secret once;
  - `RevokeKey`: only active keys can be revoked, otherwise 404;
  - `Authenticate(rawKey)`: rejects anything without the `msk_` prefix
    before touching the database, then looks up the active key by hash. Every
    failure is the same `401 invalid key`.
- Links: `LinkProject` checks that the app *and* the project belong to the
  owner, then inserts idempotently. `UnlinkProject` returns 404 when there
  was no link. `ListProjects`.

### `handler.go` (new)

`Module` and the routes under `/apps` (all behind auth):

```
POST /   GET /
GET /{appID}   PATCH /{appID}   DELETE /{appID}
GET /{appID}/keys   POST /{appID}/keys   DELETE /{appID}/keys/{keyID}
GET /{appID}/projects   PUT /{appID}/projects/{projectID}   DELETE /{appID}/projects/{projectID}
```

### `service_test.go` (new)

`TestKeyLifecycle` covers: the secret format and prefix; that the stored
value is a 32-byte hash, not the secret; that the key authenticates; that
empty, unknown and wrong-prefix keys are rejected; and that another owner
can't create keys. `TestCreateValidation` rejects an unknown platform.

---

## Dev stack, test-issue action and rate limits

These files came in a second round. That round added the Docker dev stack,
the fake GitHub, the bash API test suite, the "send test issue" action, and
handling of GitHub rate limits.

### `backend/Dockerfile` (new)

A multi-stage build. The `golang:1.27-alpine` build stage compiles both
binaries with module and build caches mounted. The shared `alpine:3.22`
runtime runs as non-root user `seismo` (uid 10001), owns `/shared` (the
volume through which the fake GitHub hands over its key), and has a
`wget`-based `HEALTHCHECK`. The two final targets are `backend` (default)
and `fakegithub`.

### `backend/.dockerignore` (new)

Keeps `bin/`, `docs/`, `scripts/`, env files, keys and test outputs out of
the build context.

### `deploy/compose.dev.yml`, `deploy/.env.example`, `deploy/README.md` (new / rewritten)

The local stack: Postgres, Redis, NATS (JetStream), a one-shot dbmate
`migrate`, `fakegithub` and `backend`. Health checks enforce the startup
order. Every `SEISMO_*` setting has a `${VAR:-default}` that deploy/.env
can override; the GitHub defaults point at the fake. See
[deploy/README.md](../../deploy/README.md).

### `Makefile` (changed), `.gitattributes` (new)

New targets: `dev-up` (`up --build -d --wait`), `dev-down`, `dev-reset`
(also removes volumes), `dev-logs`, `api-test`, `api-test-fresh`.
`.gitattributes` forces LF for `*.sh` and `Dockerfile`, because bash on
Windows fails on CRLF.

### `backend/cmd/fakegithub/main.go` (new)

A development-only stand-in for github.com and api.github.com. It
implements the endpoints the provider calls, plus `/_fake/issues` and
`/_fake/stats`. OAuth code `fake-code-N` grants installation `N`, so every
test run can use a fresh installation. It has special repositories for
failure (`broken`), secondary limits (`slow-down`) and an exhausted primary
quota (`exhausted`), and sends GitHub's `X-RateLimit-*` headers. The full
contract is in
[test-scripts.md → The fake GitHub contract](test-scripts.md#the-fake-github-contract).

### `backend/scripts/api/*.sh` (new)

The end-to-end API test suite: `lib.sh` plus one script per area and
`all.sh`. Every function and script is documented in
[test-scripts.md](test-scripts.md).

### `internal/app/testissue.go` (new) and `handler.go` / `service.go` (changed)

- `SendTestIssue` loads the app (owner-scoped) and applies the per-app
  throttle (`ratelimit.Limiter`, key `test_issue:<appID>`,
  `limits.test_issues_per_minute` per minute). It then calls
  `issue.Service.OpenForApp` with a draft labelled `m-seismo-test`, and maps
  the results to `TestIssueResult`. Rate-limited targets get
  `retry_after_seconds`.
- `TestIssueOutcome` says whether anything was opened, and, if every target
  was rate-limited, the shortest wait.
- `POST /apps/{appID}/test-issue` answers `201`, `429` with `Retry-After`,
  or `502`.
- `app.Service` now takes `*issue.Service`, `ratelimit.Limiter` and
  `*config.Config`. A nil limiter disables the throttle, which is how the
  unit tests use it.

### `internal/ratelimit/ratelimit.go` (new)

The `Limiter` interface (`Allow(ctx, key, limit, window)`) and its Redis
implementation. It is a fixed window, run as one Lua script (`INCR`, then
`PEXPIRE` on the first hit, then `PTTL`), so it is atomic and holds across
instances. When over the limit, the remaining TTL is the retry delay.
`Module` provides it.

### `internal/provider/github/ratelimit.go` (new)

GitHub rate-limit handling (explained in
[github.md → Rate limits](github.md#rate-limits)):

- `pacer`: reservation-based spacing of writes per installation.
- `Provider.call`: runs one API operation. Writes wait on the pacer first.
  If the result is a GitHub rate limit, it either waits (`Retry-After`, or
  until reset + 1 s; ≤ `MaxRateLimitWait`; within the context deadline; at
  most 3 attempts) or returns `*issue.RateLimitError`.
- `rateLimitWait`: turns go-github's `*AbuseRateLimitError` /
  `*RateLimitError` into a wait. Without `Retry-After` it uses 60 s, as
  GitHub asks.
- `quotaWatcher`: an `http.RoundTripper` that warns once per reset window
  below 10% of the quota.

### `internal/provider/github/github.go`, `opener.go` (changed)

- `Options` gains `MinWriteInterval` and `MaxRateLimitWait`.
- `installation` bundles each installation's client, behind a
  `quotaWatcher` transport, with its write `pacer`.
- `installationClient` became `installation()`.
- Every GitHub call goes through `call`: user installations, repositories,
  repository lookup, milestones, create, comment, get.
- Open milestones are cached per repository for 5 minutes
  (`milestoneEntry`).

### `internal/issue/issue.go`, `service.go` (changed)

- `RateLimitError{Tracker, Wait, Secondary, Err}`: the tracker-neutral "not
  processed, retry after Wait" error, with `RetryAfter()`.
- `OpenForApp` remembers which connections hit a rate limit and skips their
  remaining targets with the same error.

### `internal/apperr/apperr.go`, `internal/httpx/respond.go` (changed)

- `KindRateLimited`, `RateLimited(msg, wait)`, the `RetryAfterer` interface
  and `RetryAfterOf(err)`. `Classify` maps any error carrying a retry delay
  to `KindRateLimited`.
- `httpx.Error` answers `429` with `Retry-After`, and `SetRetryAfter`
  rounds up to whole seconds.

### `internal/config/*` (changed)

New keys: `github.min_write_interval` (1s), `github.max_rate_limit_wait`
(30s), and the new `limits` section with `limits.test_issues_per_minute`
(10).

### Tests (new / extended)

| File | Adds |
|---|---|
| `internal/provider/github/ratelimit_test.go` | secondary limit retried after `Retry-After` · gives up when the deadline is too short · primary exhaustion fails fast, and go-github then blocks without calling GitHub · concurrent writes paced, reads not · milestone cache · quota warning once per window |
| `internal/provider/github/github_test.go` | `slow` and `exhausted` repositories in the fake; option overrides for `newTestProvider` |
| `internal/issue/service_test.go` | rate-limited connection skipped; other connections still tried |
| `internal/httpx/httpx_test.go` | 429 + rounded `Retry-After`; upstream `RetryAfter()` errors classified |
| `internal/app/service_test.go` | test-issue throttle gives `KindRateLimited` with the limiter's wait |

