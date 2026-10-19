# deploy

## Local development stack

`compose.dev.yml` runs everything the backend needs, plus the backend itself:

| Service | Image | Host port | Purpose |
|---|---|---|---|
| `postgres` | `postgres:17-alpine` | `5432` (`POSTGRES_PORT`) | database `seismo`, user and password `seismo` |
| `redis` | `redis:7-alpine` | — | provider connect-flow state |
| `nats` | `nats:2-alpine` (JetStream on) | — | queue, connected but not used yet |
| `migrate` | `ghcr.io/amacneil/dbmate:2` | — | applies `backend/db/migrations`, then exits |
| `fakegithub` | built from `backend/Dockerfile` (target `fakegithub`) | `9090` (`FAKEGITHUB_PORT`) | stand-in for github.com and api.github.com |
| `backend` | built from `backend/Dockerfile` (target `backend`) | `8080` (`BACKEND_PORT`) | the API |

```sh
make dev-up          # = docker compose -f deploy/compose.dev.yml up --build -d --wait
curl localhost:8080/healthz
make api-test        # run every API test script against it
make dev-logs        # follow backend and fakegithub logs
make dev-down        # stop (keeps data)
make dev-reset       # stop and delete the database and NATS volumes
```

Startup order is enforced with health checks: Postgres → `migrate` (must
exit 0) → backend. The backend also waits for Redis, NATS and the fake
GitHub to be healthy.

### Fake GitHub

By default the backend's GitHub provider points at `fakegithub`
(`backend/cmd/fakegithub`). This lets the full connect flow and issue
opening work without a GitHub App, credentials or a browser:

- On start it writes a fresh RSA key to the shared `github-app-key` volume,
  and the backend reads it as the App's private key.
- OAuth code `fake-code-N` grants access to installation `N`, for any
  positive N.
- Repositories: `octo-org/seismo-test`, `octo-org/another`, plus three
  that misbehave on purpose:
  - `octo-org/broken`: issue creation always returns 500.
  - `octo-org/slow-down`: GitHub's secondary rate limit, once per title.
  - `octo-org/exhausted`: the hourly quota is used up.
- Every response carries GitHub's `X-RateLimit-*` headers.
- `GET http://localhost:9090/_fake/issues` lists every issue opened;
  `DELETE` clears the list. `GET /_fake/stats` counts rate-limited
  responses. Everything is kept in memory only.

The fake is for development and tests only. It doesn't verify JWT
signatures.

### Using a real GitHub App

Copy `deploy/.env.example` to `deploy/.env` and fill in the
`SEISMO_GITHUB_*` values: app ID, slug, client ID and secret, the private
key as one line with `\n`, and the real `API_URL` / `WEB_URL`. Then run
`make dev-up` again. Compose reads `deploy/.env` automatically.

The App's Setup URL must reach your machine:
`http://localhost:8080/api/v1/providers/github/callback` works when you
install from a browser on the same machine. See
[backend/docs/github.md](../backend/docs/github.md#creating-the-github-app).

### Image

`backend/Dockerfile` is multi-stage:

1. `golang:1.27-alpine` builds both binaries, with module and build caches
   mounted.
2. The runtime stage is `alpine:3.22`, running as non-root user `seismo`
   (uid 10001), with a `wget`-based `HEALTHCHECK`.
3. There are two final targets, `backend` (default) and `fakegithub`.

Build only the backend image:

```sh
docker build -t m-seismo-backend --target backend backend/
```
