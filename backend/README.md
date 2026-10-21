# Backend

Go service: ingestion, enrichment, deduplication, GitHub publishing. Status:
owner accounts, projects, apps, GitHub connections, the issue service, the
ingestion endpoint and the worker that turns reports into issues are in
place.

**In-depth documentation:** [docs/](docs/README.md). It covers the
architecture, data model, GitHub integration, issue service, API reference,
file-by-file reference, extension recipes and testing.

## Domain

```
owner ──< project ──< project_repository  (tracker + owner/name, one default)
  │          │
  └──< app >─┘  (app_projects, many-to-many)
        ├──< app_key              (SDK keys, stored hashed)
        └──< provider_connection  (the app's credentials: a GitHub App installation, …)
issue  ── opened in a project_repository, by an app, through one of its connections
```

- An **owner** signs up with email and password and gets a JWT, and owns
  many apps and projects.
- A **project** says where code lives: one or more repositories, each named
  by tracker and full name, and exactly one is the default. Projects hold no
  credentials.
- An **app** is a source of issues. It holds the SDK keys, links to any
  number of the owner's projects, and has its **own provider connections**.
  On GitHub, a connection is an installation of the m-Seismo GitHub App.
  Apps of one owner may share an installation; two owners can't.
- `issue.Service.OpenForApp` opens an issue in the default repository of
  every linked project, each **through the app's own connection** whose
  account covers that repository. Two apps feeding one project therefore
  use their own credentials, and a target the app has no connection for
  fails on its own. See [docs/github.md](docs/github.md#who-a-connection-belongs-to).

Trackers plug in under `internal/provider/<tracker>/`. Each one implements
`provider.Provider` (the connect flow, repository listing and `issue.Opener`)
and adds itself to the `providers` fx group. GitLab and Bitbucket go there
later.

## Running

The quickest way is the Docker dev stack, which includes Postgres, Redis,
NATS, migrations, a fake GitHub and the backend. From the repository root:

```sh
make dev-up      # http://localhost:8080
make api-test    # end-to-end API tests (backend/scripts/api)
```

See [deploy/README.md](../deploy/README.md). To run the backend directly
instead:

```sh
cp .env.example .env    # then fill in the values
make db-up              # apply migrations (needs dbmate) and dump db/schema.sql
go run .
```

`make sqlc-generate` regenerates `internal/db` after you change
`db/queries` or the schema. If `sqlc` is not installed, run
`go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate` instead.

The `20261017120000_owners_apps_providers` migration makes `projects.owner_id`
`NOT NULL`, so a local database that already has projects needs to be
recreated. `20261021130000_app_scoped_connections` moves connections to apps
and keeps existing data: each owner connection is copied to each of that
owner's apps.

## GitHub App setup

1. On GitHub, go to Settings → Developer settings → GitHub Apps → New GitHub App.
2. Under **Permissions**, set Repository → Issues to *Read and write* and
   Metadata to *Read-only*. Leave everything else at *No access*.
3. Set the **Callback URL** to
   `{SEISMO_SERVER_PUBLIC_URL}/api/v1/providers/github/callback`. GitHub
   greys out the Setup URL once step 4 is ticked and redirects to the
   Callback URL instead.
4. Tick **Request user authorization (OAuth) during installation**. The
   callback uses the resulting code to check that the installing user really
   has access to the `installation_id` GitHub sends back. Without this, a
   forged `installation_id` would be accepted.
5. Generate a private key. Note the App ID, the App slug (from its public
   URL), the Client ID, and a new Client secret.
6. Set `SEISMO_GITHUB_*` in `.env`. If `SEISMO_GITHUB_APP_ID` is unset, the
   GitHub provider is disabled and the rest of the API still runs.

## API (`/api/v1`)

Every route except register, login and the provider callback needs
`Authorization: Bearer <token>`.

| Method | Path | |
|---|---|---|
| POST | `/auth/register` | `{email, password, name}` → `{owner, token}` |
| POST | `/auth/login` | `{email, password}` → `{owner, token}` |
| GET / PATCH / DELETE | `/owners/me` | changing the email or password requires `current_password` |
| POST / GET | `/projects` | `{name, slug?}` |
| GET / PATCH / DELETE | `/projects/{id}` | |
| GET / POST | `/projects/{id}/repositories` | `{tracker: "github", repository: "owner/name", is_default}` (no credentials) |
| PUT | `/projects/{id}/repositories/{repoID}/default` | |
| DELETE | `/projects/{id}/repositories/{repoID}` | |
| POST / GET | `/apps` | `{name, platform: ios\|android\|flutter\|react_native, bundle_id}` |
| GET / PATCH / DELETE | `/apps/{id}` | |
| GET / POST | `/apps/{id}/keys` | the POST response contains the secret, shown once |
| DELETE | `/apps/{id}/keys/{keyID}` | revoke |
| GET | `/apps/{id}/projects` | |
| PUT / DELETE | `/apps/{id}/projects/{projectID}` | link / unlink |
| GET | `/apps/{id}/connect/{tracker}` | → `{url}`; send the browser there to connect **this app** |
| GET / POST | `/apps/{id}/connections` | list; or `{connection_id}` to attach another of your apps' connections |
| DELETE | `/apps/{id}/connections/{connectionID}` | only this app loses it; the App stays installed on GitHub |
| GET | `/apps/{id}/connections/{connectionID}/repositories` | |
| GET | `/apps/{id}/targets` | where the app's issues would go, and through which connection |
| POST | `/apps/{id}/test-issue` | open a test issue through the issue service |
| GET | `/providers` | the trackers configured on this deployment |
| GET | `/providers/{tracker}/callback` | public; redirects to `{frontend_url}/apps/{appID}/connections?status=…` |
| GET | `/providers/connections` | every connection of every app of yours (read-only) |

## Tests

```sh
go test ./...
# full stack against real services and GitHub:
SEISMO_E2E_APP_ID=<app uuid> go test -tags e2e -run TestOpenForAppE2E .
```

## Commands

Run these commands from the repository root:

- `make backend-fmt`: format Go source files with `gofmt`.
- `make backend-fmt-check`: verify that Go source files are formatted.
- `make backend-lint`: run `golangci-lint` with the repository's configured linters, including `gosec` and `bodyclose`.
- `make backend-build`: build the Go backend.
- `make backend-test`: run Go tests.
- `make backend-check`: run the backend formatting check and linting, build, and tests.
