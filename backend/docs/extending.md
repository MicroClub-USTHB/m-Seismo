# Extending the backend

Recipes for the changes you're most likely to make next.

- [Add a tracker](#add-a-tracker)
- [Add a resource with routes](#add-a-resource-with-routes)
- [Add a query or a column](#add-a-query-or-a-column)
- [Add a config setting](#add-a-config-setting)
- [Call the issue service from ingestion](#call-the-issue-service-from-ingestion)

---

## Add a tracker

GitHub, GitLab and Bitbucket Cloud are implemented. A new tracker (Gitea,
Jira, Bitbucket Data Center, …) follows the same shape. Use the closest
existing provider as the template:

| The tracker grants access with… | Copy | Why |
|---|---|---|
| an installable app with its own key (no user token) | `internal/provider/github` | nothing to store; tokens are minted per installation |
| OAuth 2.0 user tokens | `internal/provider/gitlab` (or `bitbucket`) | `internal/provider/oauth` already does the code exchange, encrypted storage, refresh, rotation, 401 retry and 429 handling |

**1. Add the enum value.** `tracker` is a Postgres enum (`github`, `gitlab`,
`bitbucket`). A new tracker needs a migration
(`alter type tracker add value 'gitea'`), then `make sqlc-generate`.

**2. Create `internal/provider/<tracker>/`.** For an OAuth tracker the whole
provider is about 250 lines (see `gitlab.go`):

```go
var Module = fx.Module("gitea", fx.Provide(provider.AsProvider(FromConfig)))

func FromConfig(cfg *config.Config, store provider.SecretStore) ([]provider.Provider, error) {
    if cfg.Gitea.ClientID == "" {
        slog.Warn("gitea provider disabled: gitea.client_id is not set")
        return nil, nil // registers nothing; the server runs without it
    }
    return []provider.Provider{New(...)}, nil
}

func New(o Options, store provider.SecretStore) (*Provider, error) {
    endpoint := oauth.Endpoint{Tracker: "gitea", AuthURL: …, TokenURL: …, ClientID: …,
        ClientSecret: …, RedirectURL: …, Scopes: …, BasicAuth: false}
    return &Provider{
        endpoint: endpoint,
        tokens:   oauth.NewTokens(store, endpoint),          // per-connection tokens
        api:      &oauth.Client{Tracker: "gitea", BaseURL: …}, // REST calls
    }, nil
}

func (p *Provider) InstallURL(state string) string { return p.endpoint.AuthCodeURL(state) }
func (p *Provider) CompleteInstall(ctx context.Context, q url.Values) (provider.Account, error) {
    // q.Get("error") → apperr.Invalid; exchange q.Get("code"); read the user with
    // oauth.StaticToken(tok.AccessToken); return Account{…, Secret: tok.Encode()}
}
func (p *Provider) ListRepositories(ctx context.Context, c issue.Connection) ([]provider.Repository, error) {
    // p.api.Do(ctx, p.tokens.For(c.ID), "GET", "…", nil, &out) — paginate
}
// HasRepository, ValidateRepository, Covers, Opener (+ an opener.go with Open/Comment/IsOpen)
```

**3. Wire it.**

- Add a config section: copy `gitlab` in `config/schema.go`,
  `defaults.go` and `config.go` (`oauthTracker(v, "gitea")`). Extend the
  `secrets.encryption_key` check in `validate()` if the tracker stores
  tokens.
- Add `gitea.Module` to `Options()` in `main.go`. `TestGraph` fails if a
  dependency is missing.

**4. Decide what `ExternalID` and `Secret` are.**
`issue.Connection.ExternalID` is opaque outside the provider.

- GitHub uses the installation ID and has no secret.
- GitLab uses the user ID and Bitbucket the user UUID, and both store an
  `oauth.Token` as `Account.Secret`.

`provider.Service` encrypts and stores the secret per connection, so per
app. The provider reads it back by `c.ID` through `oauth.Tokens`; never put
a token on `provider_connections`, which is returned by the API.

**5. Implement `Covers` and `ValidateRepository`.** Both run without network
calls.

- `ValidateRepository` runs when a repository is linked to a project, so
  reject names the tracker can't have.
- `Covers(c, repo)` runs for every target of every report, to choose which
  of the app's connections opens the issue. Return `true` only when `c`
  could reach `repo`.
  - For an OAuth token that reaches everything its user can see, `true` is
    acceptable, as GitLab and Bitbucket do. The first connection on that
    tracker then wins, and the tracker's own error surfaces when access is
    missing.
  - If the tracker scopes a grant to a namespace, compare that namespace, as
    GitHub compares `AccountLogin`.

**6. Uphold the security rule in `CompleteInstall`.** Never trust an account
identifier from the callback query unless the tracker has confirmed it with
credentials that belong to the user, such as the OAuth code exchange. The
state is already single-use and tied to the owner and the app by
`provider.Service`. The exclusion constraint on `provider_connections` stops
two owners from holding the same `(tracker, external_id)`. Return
`apperr.Invalid` for anything the user can fix (denied, bad code).

**7. Register the callback URL** with the tracker:
`{public_url}/api/v1/providers/<tracker>/callback`. The routes
(`GET /apps/{appID}/connect/{tracker}` and the callback) are generic over
`{tracker}`, so no handler code is needed.

**8. Write a fake and test against it.**

- Serve it from the provider's tests with `httptest`, as `gitlab_test.go`
  does. The dev stack has no fake trackers.
- Cover: the code exchange (good, bad, denied); the user lookup;
  pagination; issue creation, comments and state; token refresh with an
  already-expired token; a revoked grant; 429 short and long; a 5xx.
  `gitlab_test.go` is the model.
- `scripts/api/oauth_suite.sh` (see `gitlab.sh`) runs only against a fake
  tracker, and skips itself when none is reachable.

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
