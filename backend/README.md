# backend

Go service: ingestion, enrichment, deduplication, GitHub publishing. Status:
owner accounts, projects, apps, GitHub connections and the issue service are
in place. There is no ingestion endpoint yet.

**In-depth documentation:** [docs/](docs/README.md). It covers the
architecture, data model, GitHub integration, issue service, API reference,
file-by-file reference, extension recipes and testing.

## Domain

```
owner ──< project ──< project_repository >── provider_connection >── owner
  │          │                                  (GitHub App installation)
  └──< app >─┘  (app_projects, many-to-many)
        └──< app_key  (SDK keys, stored hashed)
issue  ── opened in a project_repository, optionally from an app
```

- An **owner** signs up with email and password and gets a JWT.
- A **provider connection** is the owner's link to a tracker. On GitHub this
  is one installation of the m-Seismo GitHub App.
- A **project** sends issues to one or more repositories. Each repository is
  reached through one of the owner's connections, and exactly one repository
  is the project's default.
- An **app** is a source of issues. It holds the SDK keys and links to any
  number of the owner's projects. `issue.Service.OpenForApp` opens an issue in
  the default repository of every linked project.

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
recreated.

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
| GET / POST | `/projects/{id}/repositories` | `{connection_id, repository: "owner/name", is_default}` |
| PUT | `/projects/{id}/repositories/{repoID}/default` | |
| DELETE | `/projects/{id}/repositories/{repoID}` | |
| POST / GET | `/apps` | `{name, platform: ios\|android\|flutter\|react_native, bundle_id}` |
| GET / PATCH / DELETE | `/apps/{id}` | |
| GET / POST | `/apps/{id}/keys` | the POST response contains the secret, shown once |
| DELETE | `/apps/{id}/keys/{keyID}` | revoke |
| GET | `/apps/{id}/projects` | |
| PUT / DELETE | `/apps/{id}/projects/{projectID}` | link / unlink |
| GET | `/providers` | the trackers configured on this deployment |
| GET | `/providers/{tracker}/connect` | → `{url}`; send the browser there |
| GET | `/providers/{tracker}/callback` | public; redirects to `{frontend_url}/settings/providers?status=…` |
| GET | `/providers/connections` | |
| DELETE | `/providers/connections/{id}` | m-Seismo forgets the connection; the App stays installed on GitHub |
| GET | `/providers/connections/{id}/repositories` | |

## Tests

```sh
go test ./...
# full stack against real services and GitHub:
SEISMO_E2E_APP_ID=<app uuid> go test -tags e2e -run TestOpenForAppE2E .
```
