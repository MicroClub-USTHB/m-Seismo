# 8. Operations

## Configuration

All configuration comes from environment variables. Values are validated at
start-up, and the process exits with every problem listed if any value is
invalid. See [`backend/.env.example`](../.env.example).

### Implemented

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `DATABASE_URL` | yes | — | PostgreSQL URL, e.g. `postgres://user:pass@host:5432/db?sslmode=require`. Pool options such as `pool_max_conns=20` can be added as query parameters |
| `ADMIN_API_TOKEN` | yes | — | Shared bearer token for `/v1`, at least 32 characters. Generate one with `openssl rand -base64 48` |
| `HTTP_ADDR` | no | `:8080` | Listen address |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | no | `json` | `json` for production, `text` for local reading |
| `REQUEST_TIMEOUT` | no | `30s` | Deadline for each `/v1` request (Go duration) |
| `SHUTDOWN_TIMEOUT` | no | `15s` | How long in-flight requests get to finish on SIGTERM |
| `MIGRATE_ON_START` | no | `false` | Apply pending migrations before serving |

### Planned

Phase 3 adds `SECRETS_KEY`, `SECRETS_KEY_ID`, `SECRETS_OLD_KEYS`,
`GITHUB_APP_ID`, `GITHUB_APP_SLUG`, `GITHUB_APP_PRIVATE_KEY_PATH`,
`GITHUB_WEBHOOK_SECRET`, `ALLOW_PRIVATE_GIT_HOSTS` and `PUBLIC_BASE_URL` (to
build the GitHub callback URL). Phase 5 adds the issues settings listed in
[7. Issues service](07-issues-service.md#configuration).

## Running locally

Requirements: Go 1.27+, Docker (for the dev database and the tests), and
`make` (or run the commands in the `Makefile` by hand).

```sh
make dev-up                          # PostgreSQL 17 on localhost:5433, data in a named volume
cp backend/.env.example backend/.env
set -a; . backend/.env; set +a
make backend-run                     # go run ./cmd/server
```

Check it:

```sh
curl -i localhost:8080/healthz       # 204
curl -i localhost:8080/readyz        # 204 when the DB is reachable
curl -s localhost:8080/v1/users -H "Authorization: Bearer $ADMIN_API_TOKEN"
```

`make dev-down` stops the database. The data stays in the `pgdata` volume
until you run `docker compose -f deploy/compose.yaml down -v`.

On Windows, `make` and `set -a` need Git Bash or WSL. In PowerShell, set the
variables with `$env:NAME = "value"` and run `go run ./cmd/server` from
`backend/`.

## Make targets

| Target | Does |
| --- | --- |
| `backend-build` | Builds binaries into `backend/bin/` |
| `backend-run` | Runs the server from source |
| `backend-migrate` | Applies migrations and exits |
| `backend-test` | All tests (integration tests need Docker or `TEST_DATABASE_URL`) |
| `backend-vet` | `go vet` |
| `backend-fmt` | Fails if any file is not `gofmt`-formatted |
| `dev-up` / `dev-down` | Start / stop the local PostgreSQL |

## Migrations in production

Two options:

1. **Separate step (recommended):** run `server migrate` as a one-off job or
   init container before rolling out the new version, and keep
   `MIGRATE_ON_START=false`. Several replicas starting at once then never race
   on migrations, and a failing migration blocks the rollout instead of
   crash-looping the pods.
2. **On start:** `MIGRATE_ON_START=true`. Fine for a single instance.

Write migrations to be **backward compatible** with the previous version of
the code (expand, then contract): add a column, deploy code that uses it,
and only drop the old one in a later release. During a rolling deploy, old
and new code run against the same schema.

## Logging

- One JSON line per event on stdout (`log/slog`). Collect stdout with the
  platform (Docker, Kubernetes, journald).
- Every request logs `method`, `path`, `status`, `bytes`, `duration_ms` and
  `request_id`.
- Unexpected errors (500) log the full error chain with the `request_id`.
  The client gets only the `request_id`, so a user can report it and an
  operator can find the matching log line.
- Never logged: request bodies, the admin token, and (planned) provider
  tokens.
- Paths contain user ids (UUIDs). They are internal identifiers, not personal
  data, but keep that in mind when sharing logs.

## Health checks

| Endpoint | Meaning | Use as |
| --- | --- | --- |
| `GET /healthz` | Process is up and serving HTTP | Liveness probe |
| `GET /readyz` | Database answered a ping within 2 s | Readiness probe |

Neither needs the admin token, and neither reveals anything beyond
up/down.

Do **not** use `/readyz` as a liveness probe. A database outage would then
restart every pod, which does not fix the database and makes recovery
slower.

## Shutdown

On SIGINT or SIGTERM, the server stops accepting connections, gives
in-flight requests up to `SHUTDOWN_TIMEOUT` to finish, then closes the
database pool and exits 0. Set your orchestrator's termination grace period
above `SHUTDOWN_TIMEOUT`; in Kubernetes, `terminationGracePeriodSeconds`
should be at least 20 with the defaults. The planned outbox worker will stop
claiming new jobs on shutdown and finish the current one.

## Deployment notes

- **TLS:** the server speaks plain HTTP. Terminate TLS at a reverse proxy or
  ingress.
- **Proxy headers:** the `RealIP` middleware trusts `X-Forwarded-For` and
  `X-Real-IP`. Run the service **only behind a proxy that overwrites these
  headers**, or remove the middleware; otherwise clients can put any IP in
  the logs.
- **Network exposure:** with the shared admin token as the only protection,
  keep `/v1` on an internal network or behind an authenticating proxy (VPN,
  SSO proxy) until real authentication exists.
- **Database:** use `sslmode=require` (or `verify-full`) outside local
  development. The application role needs DML on its tables. Run migrations
  with a role that can also run DDL; that role may be the same in small
  deployments.
- **Container image and Helm chart:** not written yet (`deploy/README.md`
  plans them). The binary is static (`CGO_ENABLED=0 go build ./cmd/server`)
  and needs only CA certificates, so a distroless base image is enough.

## Backups

Everything durable is in PostgreSQL, plus (planned) the attachment bucket.
Back up both. Use point-in-time recovery for the database if the platform
offers it. Restoring the database without the matching bucket gives broken
attachment links, not broken issues.
