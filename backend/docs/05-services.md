# 5. Internal services

This document walks through every package that exists today: what it is
for, how it works, and the details that are easy to get wrong. Planned
packages are covered in [6](06-git-connections.md) and
[7](07-issues-service.md).

---

## `cmd/server`

[`cmd/server/main.go`](../cmd/server/main.go) is the composition root: the
only place where concrete dependencies are built and connected.

Start-up sequence (`run`):

1. `config.Load()`: read and validate the environment. On any error, exit
   with status 1 **before** anything else starts.
2. `setupLogger`: install the default `slog` logger, JSON or text, at the
   configured level.
3. `signal.NotifyContext(SIGINT, SIGTERM)`: a context that is cancelled on
   shutdown signals.
4. `db.Open`: create the pgx pool and ping the database. The service will not
   start without a database.
5. Subcommand: `server migrate` applies migrations and exits.
6. With `MIGRATE_ON_START=true`, apply migrations.
7. Build the services, the router and the `http.Server`. The server has
   `ReadHeaderTimeout` (10 s, against slowloris) and `IdleTimeout` (2 min).
8. Serve until the server fails or a signal arrives.
9. **Graceful shutdown**: `srv.Shutdown` with `SHUTDOWN_TIMEOUT`. New
   connections are refused and in-flight requests are allowed to finish. Then
   the pool is closed (deferred).

`run` returns errors instead of calling `os.Exit` itself, so deferred cleanup
always runs. Only `main` exits.

---

## `internal/config`

[`config.go`](../internal/config/config.go) maps environment variables to a
`Config` struct with [`caarlos0/env`](https://github.com/caarlos0/env)
(`env:"NAME,required"`, `envDefault:"…"`).

`Validate` checks rules that struct tags cannot express:

- the admin token is at least 32 characters;
- the log level is valid (`debug`, `info`, `warn`, `error`);
- the log format is `json` or `text`;
- the request timeout is positive.

It reports **all** problems at once with `errors.Join`, so the operator fixes
everything in one go.

Design rule: configuration is validated **at start-up, never at first use**.
A bad value must stop the process immediately, not cause a 500 on some later
request. Every variable is listed in [8. Operations](08-operations.md).

---

## `internal/db`

[`db.go`](../internal/db/db.go) contains:

| Function | Purpose |
| --- | --- |
| `Open(ctx, url)` | Creates a `pgxpool.Pool` and pings it. Pool size and timeouts can be tuned through the URL (`pool_max_conns=…`, see pgx docs) |
| `Migrate(ctx, pool)` | Runs the embedded goose migrations. Opens a `database/sql` view of the pool (`stdlib.OpenDBFromPool`) because goose needs `*sql.DB` |
| `Querier` | Interface satisfied by `*pgxpool.Pool`, `*pgxpool.Conn` and `pgx.Tx`. Helpers such as `audit.Record` and `projects.Exists` accept it, so they work inside or outside a transaction |
| `InTx(ctx, pool, fn)` | Runs `fn` in a transaction: commit if `fn` returns nil, rollback on error **or panic** (via `pgx.BeginFunc`) |
| `PgError(err)` | Extracts the SQLSTATE code and the constraint name from a PostgreSQL error |
| `CodeUniqueViolation`, … | SQLSTATE constants the services match on |

### How services turn database errors into domain errors

Each domain package has a `mapErr` function:

```go
func mapErr(err error) error {
    if errors.Is(err, pgx.ErrNoRows) {
        return apperr.NotFound(entityType)          // → 404
    }
    if code, constraint, ok := db.PgError(err); ok &&
        code == db.CodeUniqueViolation && constraint == "users_email_key" {
        return apperr.Conflict("email is already in use") // → 409
    }
    return fmt.Errorf("users: %w", err)            // → 500, logged
}
```

It matches on the **constraint name** as well as the code, so an unrelated
unique violation is not reported with the wrong message. When you add a
constraint the API should report as a 409, give it an explicit name in the
migration and add a case to `mapErr`.

---

## `internal/audit`

[`audit.go`](../internal/audit/audit.go) writes one row to `audit_logs` per
state change.

```go
audit.Record(ctx, tx, audit.Entry{
    UserID:     &userID,          // owning user (nil if none / deleted)
    Action:     "project.update", // <entity>.<verb>
    EntityType: "project",
    EntityID:   p.ID,
    Payload:    changes,          // JSON-marshalled by pgx
})
```

- **Actor.** Who acted is carried in the request context: `audit.WithActor`
  sets it, `ActorFrom` reads it. The auth middleware sets `admin-token`.
  Calls with no actor (future background jobs) record `system`. When real
  authentication arrives, the middleware will set the authenticated
  principal, and no service code changes.
- **Same transaction.** Services always pass the transaction (`tx`), never
  the pool, so the audit row commits or rolls back together with the change.
  If the audit insert fails, the whole change fails. That is deliberate: an
  unaudited change is worse than a failed request.
- **Payload rules.** On create: the full new row. On update: only the fields
  that were present in the request, built with `opt.Field.PutIfSet`. On
  delete: nothing. **Never put secrets in a payload.** The planned
  connections service must audit "credentials rotated", never the token.

Action names in use: `user.create|update|delete`,
`project.create|update|delete`, `app.create|update|delete`.

---

## `internal/apperr`

[`apperr.go`](../internal/apperr/apperr.go) defines the small set of errors
services may return, so the HTTP layer never needs to know about SQL:

| Error | Built with | HTTP |
| --- | --- | --- |
| `ErrNotFound` | `apperr.NotFound("project")` → `"project not found"` | 404 |
| `ErrConflict` | `apperr.Conflict("reason")` | 409 |
| `*ValidationError` | `apperr.Validator` | 422 with `invalid-params` |
| anything else | — | 500 (logged, message hidden) |

`Validator` collects field errors instead of stopping at the first one:

```go
var v apperr.Validator
v.Check(n >= 1 && n <= 255, "name", "must be between 1 and 255 characters")
v.Check(p.Valid(), "platform", "must be one of ios, android, flutter, react_native")
if err := v.Err(); err != nil { return App{}, err } // nil when no errors
```

The messages are shown to API clients as they are, so write them for a human
reading an error response.

---

## `internal/opt`

[`opt.go`](../internal/opt/opt.go): `opt.Field[T]` is what makes PATCH work.
Plain Go JSON decoding cannot tell `{}` from `{"email": null}`; both leave a
`*string` nil. `Field[T]` implements `UnmarshalJSON`, which is **only called
when the key is present**:

| JSON | `Set` | `Null` | `Value` |
| --- | --- | --- | --- |
| key absent | false | false | zero |
| `"k": null` | true | true | zero |
| `"k": "x"` | true | false | `"x"` |

Helpers:

- `Ptr()`: nil when null, else `&Value`. Use it to write a nullable column.
- `PutIfSet(m, key)`: adds the field to an audit "changes" map only if it was
  sent.
- `Of(v)`: builds a set field in tests.

Services turn a `Field` into SQL with this pattern:

```sql
UPDATE users SET
    name  = CASE WHEN $2 THEN $3 ELSE name  END,   -- $2 = Name.Set
    email = CASE WHEN $4 THEN $5 ELSE email END    -- $5 = nil when Null
WHERE user_id = $1
RETURNING …
```

One statement, no dynamic SQL, and only the fields that were sent change.

---

## `internal/paging`

[`paging.go`](../internal/paging/paging.go) implements keyset pagination.

- **Cursor** = `(created_at, id)` of the last row returned, encoded as
  base64url of `RFC3339Nano|uuid`. The encoding is opaque to clients and may
  change.
- **Query pattern** (works on the first page too, where both arguments are
  NULL):

  ```sql
  WHERE $1::timestamptz IS NULL OR (created_at, user_id) > ($1, $2)
  ORDER BY created_at, user_id
  LIMIT $3            -- limit + 1
  ```

  The row comparison `(a, b) > (x, y)` uses the composite index
  `(created_at, user_id)`. The `id` breaks ties between rows created in the
  same microsecond.
- **Fetch one extra row.** Services ask for `limit + 1` rows. `paging.Trim`
  drops the extra one and, if it existed, returns a `NextCursor` built from
  the last kept row. No `COUNT(*)` is needed.
- `Page.Normalize` clamps the limit (default 50, max 200). The HTTP layer
  already rejects out-of-range values, so this is a second line of defence
  for in-process callers.

---

## `internal/users`

[`users.go`](../internal/users/users.go): `Service` with `Create`, `Get`,
`List`, `Update`, `Delete`.

How it works:

- **Normalisation**: name and email are trimmed. An empty email becomes NULL
  (no email).
- **Email validation** uses `net/mail.ParseAddress` and also requires the
  parsed address to equal the input. This rejects display-name forms like
  `Wail <w@x.com>`. Uniqueness ignores case (index on `lower(email)`).
  Conflicts become `409 email is already in use`.
- **Delete** removes the user. The database cascades to projects and apps
  (and later connections, repositories, issues). The audit row is written
  with `UserID: nil`, because the `audit_logs.user_id` FK would point to a
  row deleted in the same transaction.

---

## `internal/projects`

[`projects.go`](../internal/projects/projects.go): `Service` with `Create`,
`Get`, `List`, `Update`, `Delete`, plus two package functions.

- **Ownership**: every query filters on `user_id` *and* `project_id`.
  Creating a project for an unknown user fails on the FK, which `mapErr` turns
  into `404 user not found`.
- **Slugs**: `Slugify(name)` lower-cases the name and replaces each run of
  characters outside `[a-z0-9]` with a single `-`, then trims dashes and caps
  the length at 100. Accented letters are dropped (`"Été"` → `"t"`); callers
  with non-ASCII names should pass `slug` explicitly. A slug collision is a
  409. There is no automatic `-2` suffix, so the caller chooses.
- **Settings**: a typed `Settings` struct stored as `jsonb`, which pgx
  marshals and unmarshals. Nil fields are left out of the JSON, so `{}` means
  "all defaults". `PATCH {"settings": null}` stores `{}`.
- **`projects.Exists(ctx, q, userID, projectID)`** is the ownership check
  other packages use before touching a project's children. It takes a
  `Querier`, so it runs inside the caller's transaction.

---

## `internal/apps`

[`apps.go`](../internal/apps/apps.go): `Service` with `Create`, `Get`,
`List`, `Update`, `Delete`.

- **Ownership through the project.** Apps have no `user_id`. Every
  statement joins `projects`:
  - reads: `FROM apps a JOIN projects p USING (project_id) WHERE p.user_id = $1 …`
  - updates: `UPDATE apps AS a … FROM projects p WHERE p.project_id = a.project_id AND p.user_id = $1 …`
  - deletes: `DELETE FROM apps a USING projects p WHERE …`

  A single statement does both the ownership check and the change, so nothing
  can happen between a check and a write.
- **Create** runs `projects.Exists` in the same transaction first, so an
  unknown or foreign project is a clean `404 project not found` instead of an
  FK error.
- **List** checks the project first for the same reason: an empty list must
  mean "no apps", not "not your project".
- **Platform** is a typed string (`apps.Platform`) with a `Valid()` method.
  The database CHECK constraint matches it.

---

## `internal/httpapi`

The transport layer. One file per concern:

| File | Contents |
| --- | --- |
| [`router.go`](../internal/httpapi/router.go) | `Deps` (everything the router needs) and `NewRouter`. Global middleware, health routes, `/v1` group |
| [`middleware.go`](../internal/httpapi/middleware.go) | `requireAdminToken`, `requestLogger`, `recoverer` |
| [`problem.go`](../internal/httpapi/problem.go) | `Problem` type, `writeProblem`, `writeError` (domain error → status) |
| [`json.go`](../internal/httpapi/json.go) | `decodeJSON`, `writeJSON`, `pathUUID`, `pageParams`, `writeList` |
| `users.go`, `projects.go`, `apps.go` | Handlers: parse → call service → write |

Middleware order matters:

```
RequestID → RealIP → requestLogger → recoverer → [ /v1: Timeout → requireAdminToken ] → handler
```

- `requestLogger` sits outside `recoverer`, so a panicking request is still
  logged, with status 500.
- `recoverer` re-panics `http.ErrAbortHandler`, which is the standard library
  signal for "abort this response" and must not become a 500.
- `Timeout` sets a deadline on the request context. pgx respects it, so a
  slow query is cancelled in the database too. If the handler returns after
  the deadline, chi writes `504`.
- `RealIP` trusts `X-Forwarded-For` / `X-Real-IP`. **Only deploy behind a
  proxy that sets these headers**; otherwise clients can fake their IP in the
  logs. See [8. Operations](08-operations.md).

Handlers are thin and repetitive on purpose. Every handler follows the same
four steps (ids → body → service → response), so new resources are copied
from an existing one.

---

## `internal/testutil`

See [9. Testing](09-testing.md).
