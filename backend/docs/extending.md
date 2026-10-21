# Extending the backend

Recipes for the changes you're most likely to make next.

- [Add a tracker (GitLab, Bitbucket)](#add-a-tracker-gitlab-bitbucket)
- [Add a resource with routes](#add-a-resource-with-routes)
- [Add a query or a column](#add-a-query-or-a-column)
- [Add a config setting](#add-a-config-setting)
- [Call the issue service from ingestion](#call-the-issue-service-from-ingestion)

---

## Add a tracker (GitLab, Bitbucket)

The `tracker` enum already contains `gitlab` and `bitbucket`, so no
migration is needed.

**1. Create `internal/provider/gitlab/gitlab.go`**

```go
package gitlab

var Module = fx.Module("gitlab",
    fx.Provide(provider.AsProvider(FromConfig)),
)

func FromConfig(cfg *config.Config) ([]provider.Provider, error) {
    if cfg.GitLab.ClientID == "" {
        slog.Warn("gitlab provider disabled")
        return nil, nil // registers nothing
    }
    return []provider.Provider{&Provider{ /* … */ }}, nil
}

type Provider struct{ /* client, credentials */ }

func (p *Provider) Tracker() db.Tracker { return db.TrackerGitlab }
func (p *Provider) InstallURL(state string) string { /* OAuth authorize URL with state */ }
func (p *Provider) CompleteInstall(ctx context.Context, q url.Values) (provider.Account, error) { /* exchange q["code"] */ }
func (p *Provider) ListRepositories(ctx context.Context, c issue.Connection) ([]provider.Repository, error) { /* … */ }
func (p *Provider) HasRepository(ctx context.Context, c issue.Connection, repo string) (bool, error) { /* … */ }
func (p *Provider) ValidateRepository(repo string) error { /* "group/subgroup/project" format */ }
func (p *Provider) Covers(c issue.Connection, repo string) bool { /* see step 4 */ }
func (p *Provider) Opener(ctx context.Context, c issue.Connection) (issue.Opener, error) { /* … */ }
```

**2. Add `gitlab.Module` to `Options()` in `main.go`.** That is the only
change outside the new package. `TestGraph` will fail if you forget a
dependency.

**3. Decide what `ExternalID` means, and where secrets live.**
`issue.Connection.ExternalID` is opaque to everything else.

- *GitHub* uses an App installation ID, and no secret is stored.
- *GitLab and Bitbucket* have no exact equivalent of GitHub App
  installations. They use OAuth with a user or group access token, so you
  will need to **store a token per connection**. That is, per app, because
  connections belong to apps. Two things follow:
  - Add a migration with a table such as
    `connection_secrets (provider_connection_id uuid primary key references provider_connections on delete cascade, ciphertext bytea, refreshed_at timestamptz)`
    and **encrypt** the token (AES-GCM with a key from config). Never store
    it in plain text, and never put it on `provider_connections`, which is
    listed in API responses.
  - `ExternalID` can be the GitLab user or group ID. The token is looked up
    by `c.ID`, which every connection-scoped method receives, so no
    interface change is needed.

**4. Implement `Covers` and `ValidateRepository`.** Both run without network
calls.

- `ValidateRepository` runs when a repository is linked to a project, so
  reject names the tracker can't have.
- `Covers(c, repo)` runs for every target of every report, to choose which
  of the app's connections opens the issue. Return `true` only when `c`
  could reach `repo`. For an OAuth token that reaches everything its user
  can see, `true` is acceptable. Then the first connection on that tracker
  wins, and the tracker's own error surfaces when access is missing. If the
  tracker scopes a grant to a namespace (a GitLab group token, a Bitbucket
  workspace), compare that namespace, as GitHub compares `AccountLogin`.

**5. Uphold the security rule in `CompleteInstall`.** Never trust an account
identifier from the callback query unless the tracker has confirmed it with
credentials that belong to the user, such as the OAuth code exchange. The
state is already single-use and tied to the owner and the app by
`provider.Service`. The exclusion constraint on `provider_connections` stops
two owners from holding the same `(tracker, external_id)`.

**6. Set the tracker's callback URL** to
`{public_url}/api/v1/providers/gitlab/callback`. The routes
(`GET /apps/{appID}/connect/{tracker}` and the callback) are generic over
`{tracker}`, so no handler code is needed.

**7. Test against an `httptest.Server`**, as `github_test.go` does: a forged
identifier must be rejected, and repository and issue calls must carry the
right token.

---

## Add a resource with routes

Follow the shape of `internal/app` or `internal/project`:

```
internal/thing/
  service.go   DTOs, Service (depends on db.Querier), validation, apperr errors
  handler.go   Module, Handler{svc, require}, Mount(r chi.Router), thin handlers
  service_test.go
```

Checklist:

- [ ] Write owner-scoped queries (`WHERE id = $1 AND owner_id = $2`), and use
      `:execrows` for deletes so 0 rows can become 404.
- [ ] The service returns DTOs, never `db.*` structs.
- [ ] The service returns `apperr.*` errors with client-safe messages; it
      never returns HTTP status codes.
- [ ] Handlers: `httpx.UUIDParam` → `httpx.Decode` → service →
      `httpx.JSON` / `httpx.NoContent`, or `httpx.Error`.
- [ ] Protect routes with `r.With(h.require)` or a `Group` with
      `r.Use(h.require)`.
- [ ] Register: `fx.Provide(NewService, httpx.AsRoutes(NewHandler))`, and add
      `thing.Module` to `main.go`.
- [ ] Run `go test ./...`. `TestGraph` checks the wiring.

---

## Add a query or a column

1. `make db-new name=…`, then write **up and down** migrations.
2. `make db-up`, which regenerates `db/schema.sql`.
3. Add or edit SQL in `db/queries/*.sql` with a `-- name: X :one|:many|:exec|:execrows`
   header. A `--` comment line right after the header becomes the Go doc
   comment.
4. `make sqlc-generate`, or `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate`.
5. Fakes in tests embed `db.Querier`, so new interface methods don't break
   them.

Notes:

- For a nullable `uuid` or `timestamptz` column you get `*uuid.UUID` or
  `*time.Time`; nullable text gives `*string`.
- A new enum type generates a Go type with constants. Validate it in the
  service too, so clients get a 422 with a clear message instead of a
  Postgres error.

---

## Add a config setting

1. Add the field to the struct in `internal/config/schema.go`.
2. Add a default in `defaults.go`, even if it is empty.
3. Read it in `Load` (`config.go`). Add a check to `validate()` if a bad
   value should stop startup.
4. Document it in `.env.example` as `SEISMO_<SECTION>_<KEY>`.

---

## Call the issue service from ingestion

See [issue-service.md → Calling it](issue-service.md#calling-it). In short:

1. Authenticate the SDK request with `app.Service.Authenticate(ctx, key)` →
   `appID`.
2. Build an `issue.Draft` (title, body following FR-47, labels, fingerprint).
3. Call `issue.Service.OpenForApp(ctx, appID, draft)`. It uses that app's
   own connections.
4. Handle `issue.ErrNoTargets` and each `Result.Err`.
   - `issue.ErrNoConnection` is a configuration problem: tell the owner.
   - Other errors can be retried, for example by queuing a retry on NATS
     JetStream.

Run the open step asynchronously. The PRD requires the reporter to get a
confirmation within 0.5 s, while opening an issue on GitHub can take
seconds.
