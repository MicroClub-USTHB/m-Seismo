# API reference

Base URL: `{server.public_url}/api/v1`. Every request and response body is
JSON (`Content-Type: application/json`).

- [Conventions](#conventions)
- [Walkthrough with curl](#walkthrough-with-curl)
- [Auth and owners](#auth-and-owners)
- [Projects](#projects)
- [Project repositories](#project-repositories)
- [Apps](#apps)
- [App keys](#app-keys) (SDK and coordinator)
- [App ↔ project links](#app--project-links)
- [Test issue](#test-issue)
- [Providers](#providers)
- [Health](#health)

---

## Conventions

**Authentication.** Every route except `POST /auth/register`,
`POST /auth/login`, `GET /providers/{tracker}/callback` and `GET /healthz`
needs:

```
Authorization: Bearer <access_token>
```

A missing, malformed, expired or wrongly signed token returns `401`.

**Errors.** Errors are always a JSON body with a single `error` string:

```json
{ "error": "a project with this slug already exists" }
```

| Status | Meaning |
|---|---|
| 401 | not authenticated, or wrong credentials |
| 404 | not found. This also covers *belongs to another owner* and *malformed UUID in the path*, which look the same on purpose. |
| 409 | conflict with existing data (duplicate) |
| 422 | invalid input: validation, malformed JSON, unknown JSON fields, trailing data, body > 1 MiB |
| 429 | rate limited, either by m-Seismo's own throttle or by the tracker (GitHub). Always has a **`Retry-After`** header in seconds. Wait that long, then retry. |
| 500 | `{"error":"internal error"}`; the details are only in the server log |
| 502 | only from `POST /apps/{id}/test-issue`: every target failed for a reason other than a rate limit |

**Requests.** Unknown fields are rejected (`422`). In `PATCH` bodies, a field
you leave out is left unchanged.

**Responses.** IDs are UUID strings, timestamps are RFC 3339, and an empty
list is `[]`, never `null`. Lists of owned resources are ordered newest first
unless noted.

**Status codes.** `201` create · `200` read or update · `204` delete, link,
unlink or set-default (no body) · `302` provider callback.

---

## Walkthrough with curl

```sh
API=http://localhost:8080/api/v1

# 1. sign up (or log in) and keep the token
TOKEN=$(curl -s $API/auth/register -d '{"email":"lead@example.com","password":"s3cure-pass","name":"Team Lead"}' \
  | jq -r .token.access_token)
AUTH="Authorization: Bearer $TOKEN"

# 2. create a project and an app, and link them
PROJECT=$(curl -s -H "$AUTH" $API/projects -d '{"name":"Shop Mobile"}' | jq -r .id)
APP=$(curl -s -H "$AUTH" $API/apps -d '{"name":"Shop iOS","platform":"ios","bundle_id":"com.example.shop"}' | jq -r .id)
curl -s -X PUT -H "$AUTH" $API/apps/$APP/projects/$PROJECT

# 3. connect GitHub: open the returned URL in a browser and install the App
curl -s -H "$AUTH" $API/providers/github/connect | jq -r .url

# 4. after the redirect, find the connection and its repositories
CONN=$(curl -s -H "$AUTH" $API/providers/connections | jq -r '.[0].id')
curl -s -H "$AUTH" $API/providers/connections/$CONN/repositories

# 5. send the project's issues to one of them (the first repository becomes the default)
curl -s -H "$AUTH" $API/projects/$PROJECT/repositories \
  -d "{\"connection_id\":\"$CONN\",\"repository\":\"octo-org/shop-ios\"}"

# 6. create an SDK key for the app (the secret is shown only once)
curl -s -X POST -H "$AUTH" $API/apps/$APP/keys
```

---

## Auth and owners

### `POST /auth/register` (public)

```json
{ "email": "lead@example.com", "password": "s3cure-pass", "name": "Team Lead" }
```

→ `201`

```json
{
  "owner": {
    "id": "4c0b…", "email": "lead@example.com", "name": "Team Lead",
    "created_at": "2026-10-17T12:00:00Z", "updated_at": "2026-10-17T12:00:00Z"
  },
  "token": { "access_token": "eyJ…", "token_type": "Bearer", "expires_at": "2026-10-18T12:00:00Z" }
}
```

| Rule | Error |
|---|---|
| email must be a bare address; it is trimmed and lower-cased | 422 `email is not a valid address` |
| name 1–200 characters after trimming | 422 |
| password 8–72 bytes | 422 `password must be between 8 and 72 bytes` |
| email already registered, case-insensitive | 409 `an account with this email already exists` |

### `POST /auth/login` (public)

```json
{ "email": "lead@example.com", "password": "s3cure-pass" }
```

→ `200`, with the same body as register. A wrong email *or* wrong password
gives `401 invalid email or password`; the response doesn't say which.

The token lasts for `auth.jwt_ttl` (default 24 h). There is no refresh
endpoint; log in again.

### `GET /owners/me`

→ `200` with the owner object.

### `PATCH /owners/me`

```json
{ "name": "New Name", "email": "new@example.com", "password": "new-pass-123", "current_password": "s3cure-pass" }
```

Every field is optional. Changing `email` or `password` requires a correct
`current_password`; otherwise the result is `401 current_password is required
and must be correct`. Changing only `name` doesn't need it. → `200` with the
owner. A duplicate email gives `409`.

### `DELETE /owners/me`

→ `204`. **This permanently deletes** the owner's projects, apps, keys,
connections and issue records. Nothing on GitHub is deleted.

---

## Projects

Project object:

```json
{ "id": "…", "name": "Shop Mobile", "slug": "shop-mobile", "created_at": "…", "updated_at": "…" }
```

| Method & path | Body | Success |
|---|---|---|
| `POST /projects` | `{"name": "Shop Mobile", "slug": "shop-mobile"}` (slug optional) | `201` project |
| `GET /projects` | — | `200` `[project]`, newest first |
| `GET /projects/{projectID}` | — | `200` project |
| `PATCH /projects/{projectID}` | `{"name"?, "slug"?}` | `200` project |
| `DELETE /projects/{projectID}` | — | `204` (removes its repository links, app links and issue records) |

Slug rules: lowercase letters and digits in groups separated by single
dashes, at most 64 characters, unique **per owner**. When `slug` is left out
on create, it is derived from the name (`"Shop Mobile!"` → `shop-mobile`).
A duplicate gives `409 a project with this slug already exists`.

---

## Project repositories

Repository object:

```json
{
  "id": "…", "project_id": "…", "connection_id": "…",
  "tracker": "github", "account_login": "octo-org",
  "repository": "octo-org/shop-ios", "is_default": true, "created_at": "…"
}
```

### `GET /projects/{projectID}/repositories`

→ `200` `[repository]`, oldest first.

### `POST /projects/{projectID}/repositories`

```json
{ "connection_id": "…", "repository": "octo-org/shop-ios", "is_default": false }
```

→ `201` repository.

- The connection must be one of yours and not suspended. The tracker is asked
  whether the connection can reach the repository.
- `is_default: true` moves the default to this repository.
- **The first repository linked to a project always becomes the default**,
  whatever `is_default` says.

| Failure | Error |
|---|---|
| project or connection not yours / missing | 404 |
| `repository` empty / not `owner/name` | 422 |
| connection suspended | 422 `connection is suspended` |
| repository not visible to the installation | 422 `repository … is not reachable through this connection` |
| already linked through that connection | 409 `repository is already linked to this project` |

### `PUT /projects/{projectID}/repositories/{repositoryID}/default`

No body. → `204`. In one transaction, it clears the old default and sets
this one. `404 repository not found` if the repository isn't linked to this
project.

### `DELETE /projects/{projectID}/repositories/{repositoryID}`

→ `204`. If this was the default, the project has **no default** until you
set another one, and issues from linked apps skip this project.

---

## Apps

App object:

```json
{ "id": "…", "name": "Shop iOS", "platform": "ios", "bundle_id": "com.example.shop", "created_at": "…", "updated_at": "…" }
```

| Method & path | Body | Success |
|---|---|---|
| `POST /apps` | `{"name", "platform", "bundle_id"}` (all required) | `201` app |
| `GET /apps` | — | `200` `[app]`, newest first |
| `GET /apps/{appID}` | — | `200` app |
| `PATCH /apps/{appID}` | `{"name"?, "platform"?, "bundle_id"?}` | `200` app |
| `DELETE /apps/{appID}` | — | `204` (keys and links removed; recorded issues keep a null `app_id`) |

`platform` must be one of `ios`, `android`, `flutter`, `react_native`
(otherwise 422). `bundle_id` is 1–255 characters.

---

## App keys

Key object:

```json
{ "id": "…", "prefix": "msk_AbCdEfGh", "revoked_at": null, "created_at": "…" }
```

| Method & path | Success |
|---|---|
| `GET /apps/{appID}/keys` | `200` `[key]`, newest first, revoked ones included |
| `POST /apps/{appID}/keys` (no body) | `201` key **plus `"secret": "msk_…"`** |
| `DELETE /apps/{appID}/keys/{keyID}` | `204`; revokes it immediately. `404 active key not found` if already revoked. |

The `secret` appears **only** in the `POST` response. m-Seismo stores only
its SHA-256 hash and can't show it again. An app can have several active
keys, which lets you rotate: create the new one, ship it, then revoke the old
one.

### Coordinator keys

The app's log coordinator attaches to the backend's gRPC
`Coordinator.Attach` with a coordinator key (see
[coordinator.md](coordinator.md)). Same key object and rules as SDK keys,
with an `mco_` prefix:

| Method & path | Success |
|---|---|
| `GET /apps/{appID}/coordinator-keys` | `200` `[key]`, newest first, revoked ones included |
| `POST /apps/{appID}/coordinator-keys` (no body) | `201` key **plus `"secret": "mco_…"`** |
| `DELETE /apps/{appID}/coordinator-keys/{keyID}` | `204`; revokes it immediately. `404 active key not found` if already revoked. |

The two kinds are kept apart: an SDK key is never accepted by `Attach`, a
coordinator key never by `POST /reports`, and each is listed and revoked
only under its own path.

---

## App ↔ project links

| Method & path | Success |
|---|---|
| `GET /apps/{appID}/projects` | `200` `[{"id","name","slug"}]`, oldest link first |
| `PUT /apps/{appID}/projects/{projectID}` | `204`; idempotent. `404 project not found` if the project isn't yours. |
| `DELETE /apps/{appID}/projects/{projectID}` | `204`; `404 app is not linked to this project` |

An app's issues go to the **default repository of every linked project**
(see [issue-service.md](issue-service.md)).

## Test issue

### `POST /apps/{appID}/test-issue`

Opens a test issue through `issue.Service.OpenForApp`, exactly as a real
report would, so an owner can check that an app is wired up end to end. The
body is optional:

```json
{ "title": "Custom title", "body": "Custom body" }
```

Defaults: title `m-Seismo test issue from <app name>`, a body with the
timestamp, and the label `m-seismo-test`.

→ `201` when at least one issue was opened. When nothing was opened, the
response is `429` with `Retry-After` if every target was rate-limited by its
tracker, and `502` otherwise. All three include one result per target
repository:

```json
{
  "results": [
    {
      "project_id": "…", "project_repository_id": "…",
      "repository": "octo-org/shop-ios", "tracker": "github",
      "issue_id": "…", "ref": "octo-org/shop-ios#12",
      "url": "https://github.com/octo-org/shop-ios/issues/12"
    },
    {
      "project_id": "…", "project_repository_id": "…",
      "repository": "octo-org/broken", "tracker": "github",
      "error": "issue: open on github octo-org/broken: …"
    }
  ]
}
```

A rate-limited target also has `"retry_after_seconds": 3600`. On `502` the
body has `"error": "no issue could be opened"`, and on a tracker `429` it has
`"error": "rate limited by the issue tracker; retry later"`. A per-target
`error` contains the tracker's error message, which is useful for diagnosing
App permissions. The caller is the owner, so this is safe to show.

| Failure | Error |
|---|---|
| app not yours / missing | 404 |
| no linked project with a default repository | 422 `app has no linked project with a default repository` |
| unknown field in the body | 422 |
| more than `limits.test_issues_per_minute` calls for this app in the current minute (default 10, counted before the target check) | 429 `too many test issues for this app; retry later` + `Retry-After` |

While GitHub is being waited out (secondary limits under 30 s), the call
just takes longer. See [github.md → Rate limits](github.md#rate-limits).

---

## Providers

Connection object:

```json
{
  "id": "…", "tracker": "github", "account_login": "octo-org", "account_type": "Organization",
  "suspended_at": null, "created_at": "…", "updated_at": "…"
}
```

### `GET /providers`

→ `200` `{"providers": ["github"]}`: the trackers configured on this
deployment. The list is empty when no GitHub App is configured.

### `GET /providers/{tracker}/connect`

→ `200` `{"url": "https://github.com/apps/m-seismo/installations/new?state=…"}`.
Send the browser to `url`. The state inside it is valid for 10 minutes and
can be used once. Returns `404 provider gitlab is not available` for a
tracker that isn't configured.

### `GET /providers/{tracker}/callback` (public, called by GitHub)

This isn't for API clients. GitHub redirects the browser here and the API
answers with `302` to:

```
{server.frontend_url}/settings/providers?provider=github&status=connected&connection_id=<uuid>
{server.frontend_url}/settings/providers?provider=github&status=error&reason=<message>
```

The possible `reason` values are listed in
[github.md → Failure modes](github.md#failure-modes).

### `GET /providers/connections`

→ `200` `[connection]`, oldest first.

### `DELETE /providers/connections/{id}`

→ `204`. m-Seismo forgets the connection, **and every project repository
linked through it** (with their issue records). The GitHub App stays
installed; remove it on GitHub if you want to.

### `GET /providers/connections/{id}/repositories`

→ `200`

```json
[ { "full_name": "octo-org/shop-ios", "private": true, "url": "https://github.com/octo-org/shop-ios" } ]
```

These are the repositories the installation was granted. `422 connection is
suspended` for suspended connections.

---

## Health

### `GET /healthz` (public, outside `/api/v1`)

→ `200` `{"status":"ok"}`. This shows the process is serving HTTP. It does
not check Postgres, Redis or NATS, although the server won't start unless
all three were reachable at boot.
