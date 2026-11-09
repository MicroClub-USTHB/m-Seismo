# deploy

Two compose files:

- `compose.dev.yml` runs everything the backend needs, plus the backend
  itself, for local development.
- `compose.server.yml` is an overlay for a server reachable from the
  internet. It goes on top of `compose.dev.yml`; see
  [Running on a server](#running-on-a-server).

## Local development stack

| Service | Image | Host port | Purpose |
|---|---|---|---|
| `postgres` | `postgres:17-alpine` | `5432` (`POSTGRES_PORT`) | database `seismo`, user and password `seismo` |
| `redis` | `redis:7-alpine` | — | provider connect-flow state |
| `nats` | `nats:2-alpine` (JetStream on) | — | report queue |
| `migrate` | `ghcr.io/amacneil/dbmate:2` | — | applies `backend/db/migrations`, then exits |
| `backend` | built from `backend/Dockerfile` | `8080` (`BACKEND_PORT`), `9443` (`COORDINATOR_PORT`) | the API, and the log coordinator (`Coordinator.Attach`, plain gRPC in development) |

```sh
make dev-up          # = docker compose -f deploy/compose.dev.yml up --build -d --wait
curl localhost:8080/healthz
make api-test        # run the API test scripts against it
make dev-logs        # follow the backend logs
make dev-down        # stop (keeps data)
make dev-reset       # stop and delete the database and NATS volumes
```

Startup order is enforced with health checks: Postgres → `migrate` (must
exit 0) → backend. The backend also waits for Redis and NATS to be healthy.

There are no fake trackers. Without any `SEISMO_GITHUB_*` value the GitHub
provider is disabled and the rest of the API still runs. To open issues,
connect a real GitHub App, GitLab application or Bitbucket consumer, as
below.

### Configuration

Copy `deploy/.env.example` to `deploy/.env` and set what you need. Compose
reads `deploy/.env` automatically, and `compose.dev.yml` passes every
`SEISMO_*` setting of the backend through to it. An unset or empty value
means the backend's own default. Every setting is described in
`backend/.env.example`.

### Using a real GitHub App

Set the `SEISMO_GITHUB_*` values in `deploy/.env`: app ID, slug, client ID
and secret, and the private key as one line with `\n`. Then run
`make dev-up` again.

The App's Callback URL must reach the backend:
`http://localhost:8080/api/v1/providers/github/callback` works when you
install from a browser on the same machine. See
[backend/docs/github.md](../backend/docs/github.md#creating-the-github-app).

### Using a real GitLab application or Bitbucket consumer

There is no deployment-wide client ID or secret for GitLab and Bitbucket:
each app registers its own OAuth application through
`PUT /apps/{appID}/providers/{tracker}/credentials`. `deploy/.env` only sets
the instance URLs (`SEISMO_GITLAB_*_URL`, `SEISMO_BITBUCKET_*_URL`, which
default to gitlab.com and bitbucket.org). The callback URLs to register
are `http://localhost:8080/api/v1/providers/gitlab/callback` and
`…/providers/bitbucket/callback`. See
[backend/docs/gitlab-bitbucket.md](../backend/docs/gitlab-bitbucket.md#setting-up-a-real-gitlab-application).

## Running on a server

`compose.dev.yml` alone is not safe on a machine with a public IP: it
publishes Postgres with a known password, and Docker's published ports
bypass host firewalls such as `ufw`. `compose.server.yml` changes that:

- Postgres is not published at all. The backend reaches it on the compose
  network.
- The API (`8080`) and the coordinator (`9443`) listen on `127.0.0.1` only.
  Reach them through an SSH tunnel
  (`ssh -L 8080:localhost:8080 <server>`) or a reverse proxy with TLS on the
  same machine.
- The backend runs with `SEISMO_ENV=production`, so it refuses to start
  without a JWT secret and an encryption key.
- The Postgres password comes from `deploy/.env` instead of the repository.
- Every long-running service restarts with the Docker daemon.

Required in `deploy/.env` (compose stops with an error when one is
missing):

| Variable | How to generate |
|---|---|
| `POSTGRES_PASSWORD` | `openssl rand -hex 24` (letters and digits only, since it goes into a URL) |
| `SEISMO_AUTH_JWT_SECRET` | `openssl rand -base64 48` |
| `SEISMO_SECRETS_ENCRYPTION_KEY` | `openssl rand -base64 32` |
| `SEISMO_SERVER_PUBLIC_URL` | the URL clients use to reach the API, e.g. `https://seismo.example.org` |

`POSTGRES_PASSWORD` is only applied when the database volume is created.
To change it later, change it inside Postgres too, or start over with
`make server-reset`, which deletes the data.

The coordinator runs in plaintext behind `127.0.0.1` by default. To expose
it to agents on other machines, mount a certificate into the container and
set `SEISMO_COORDINATOR_TLS_CERT_FILE` and `SEISMO_COORDINATOR_TLS_KEY_FILE`
(see [backend/docs/coordinator.md](../backend/docs/coordinator.md)).

```sh
cp deploy/.env.example deploy/.env    # then fill in the values above
make server-up       # = docker compose -f deploy/compose.dev.yml -f deploy/compose.server.yml up --build -d --wait
curl localhost:8080/healthz
make server-logs     # follow the backend logs
make server-down     # stop (keeps data)
make server-reset    # stop and delete the database and NATS volumes
```

## Image

`backend/Dockerfile` is multi-stage:

1. `golang:1.27-alpine` builds the backend, with module and build caches
   mounted.
2. The runtime stage is `alpine:3.22`, running as non-root user `seismo`
   (uid 10001).
3. The final `backend` target adds the binary, exposes `8080` and has a
   `wget`-based `HEALTHCHECK` on `/healthz`.

Build only the backend image:

```sh
docker build -t m-seismo-backend backend/
```
