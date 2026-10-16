# 4. HTTP API

The admin API manages users, projects and apps, and later git connections and
repository links. It is a JSON REST API under `/v1`.

There is **no** issues or report endpoint. Issues are created through the
in-process issues service (see [7. Issues service](07-issues-service.md)).

## Endpoints

### Implemented

| Method | Path | Success | Description |
| --- | --- | --- | --- |
| `GET` | `/healthz` | 204 | Process is alive. No token needed |
| `GET` | `/readyz` | 204 / 503 | Database reachable within 2 s. No token needed |
| `POST` | `/v1/users` | 201 | Create a user |
| `GET` | `/v1/users` | 200 | List users (paginated) |
| `GET` | `/v1/users/{user_id}` | 200 | Get a user |
| `PATCH` | `/v1/users/{user_id}` | 200 | Update a user |
| `DELETE` | `/v1/users/{user_id}` | 204 | Delete a user and **everything it owns** |
| `POST` | `/v1/users/{user_id}/projects` | 201 | Create a project |
| `GET` | `/v1/users/{user_id}/projects` | 200 | List the user's projects |
| `GET` | `/v1/users/{user_id}/projects/{project_id}` | 200 | Get a project |
| `PATCH` | `/v1/users/{user_id}/projects/{project_id}` | 200 | Update a project |
| `DELETE` | `/v1/users/{user_id}/projects/{project_id}` | 204 | Delete a project and its apps |
| `POST` | `/v1/users/{user_id}/projects/{project_id}/apps` | 201 | Create an app |
| `GET` | `/v1/users/{user_id}/projects/{project_id}/apps` | 200 | List the project's apps |
| `GET` | `/v1/users/{user_id}/projects/{project_id}/apps/{app_id}` | 200 | Get an app |
| `PATCH` | `/v1/users/{user_id}/projects/{project_id}/apps/{app_id}` | 200 | Update an app |
| `DELETE` | `/v1/users/{user_id}/projects/{project_id}/apps/{app_id}` | 204 | Delete an app |

`POST` responses carry a `Location` header with the new resource's path.

### Planned

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/users/{uid}/connections` | Create a GitLab or Bitbucket connection from a token (checked against the provider) |
| `GET` | `/v1/users/{uid}/connections[/{cid}]` | List or get connections. Credentials are never returned |
| `PATCH` | `/v1/users/{uid}/connections/{cid}` | Rotate credentials or rename |
| `DELETE` | `/v1/users/{uid}/connections/{cid}` | Remove a connection (refused while repositories use it) |
| `POST` | `/v1/users/{uid}/connections/github/install` | Start the GitHub App installation; returns the install URL |
| `GET` | `/v1/integrations/github/callback` | GitHub redirects here after installation; creates the connection |
| `GET` | `/v1/users/{uid}/connections/{cid}/remote-repositories` | Repositories this connection can reach |
| `POST`, `GET` | `/v1/users/{uid}/projects/{pid}/repositories` | Link or list repositories |
| `PATCH`, `DELETE` | `/v1/users/{uid}/projects/{pid}/repositories/{rid}` | Set default, or unlink |

`PATCH …/apps/{aid}` will also accept `repository_id` (the per-app
override). See [6. Git connections](06-git-connections.md).

## Authentication

Real authentication is deferred for v1. Until it exists, every `/v1` request
must send the shared admin token:

```
Authorization: Bearer <ADMIN_API_TOKEN>
```

- The token comes from the `ADMIN_API_TOKEN` setting, which must be at least
  32 characters.
- The check compares SHA-256 hashes in constant time, so response timing
  reveals nothing about the token.
- A missing or wrong token gets `401` with
  `WWW-Authenticate: Bearer realm="m-seismo"`.
- Requests that pass are audited with actor `admin-token`.

**What this means:** anyone with the token can act as **any** user. The
`user_id` in the path selects whose data you act on; it is not an identity.
Do not expose the API to people who should not administer every user. Replace
this before any multi-tenant use; see
[10. Roadmap and gaps](10-roadmap-and-gaps.md#security).

## Request bodies

- `Content-Type` must be `application/json` (parameters such as
  `; charset=utf-8` are allowed). Any other type gets `415`. A missing header
  is accepted.
- The body must be **one** JSON object, at most **1 MiB** (`413` otherwise).
- **Unknown fields are rejected** with `400`. A typo such as `"emial"` fails
  loudly instead of being silently ignored.
- Strings are trimmed of surrounding whitespace before validation.

## PATCH semantics

`PATCH` bodies are partial. For each field:

| In the body | Effect |
| --- | --- |
| key absent | field unchanged |
| `"key": value` | field set to `value` (validated) |
| `"key": null` | field cleared, **only for nullable fields** (`email`, project `settings`); otherwise `422` |

Example: `{"email": null}` removes a user's email and leaves the name as it
is. `{"settings": null}` resets every project override to the defaults.

This uses `opt.Field[T]`, which records whether the key was present, null, or
a value (see [5. Internal services](05-services.md#opt)).

An empty body `{}` is valid. It changes no data, but it still bumps
`updated_at` and writes an audit row (a known limitation; see
[10](10-roadmap-and-gaps.md)).

## Pagination

Lists use **keyset (cursor) pagination**, ordered by `(created_at, id)`:

```
GET /v1/users?limit=50
→ { "items": [ … ], "next_cursor": "MjAyNi0xMC0xNlQx…" }

GET /v1/users?limit=50&cursor=MjAyNi0xMC0xNlQx…
→ { "items": [ … ] }                    ← no next_cursor: last page
```

- `limit`: 1–200, default 50. Out of range gets `400`.
- `cursor`: opaque; send back exactly what you got. Invalid gets `400`.
- `next_cursor` is left out on the last page.
- Rows added while you page through appear at the end and are never skipped
  or repeated. With `OFFSET`, an insert would shift every later page.

## Errors

Every error is an [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem
document, `Content-Type: application/problem+json`:

```json
{
  "type": "https://m-seismo.dev/problems/validation",
  "title": "Your request is not valid.",
  "status": 422,
  "instance": "/v1/users",
  "request_id": "host/abc123-000042",
  "invalid-params": [
    { "name": "name", "reason": "must be between 1 and 255 characters" },
    { "name": "email", "reason": "must be a valid email address" }
  ]
}
```

| Status | `type` suffix | When |
| --- | --- | --- |
| 400 | `bad-request` | Malformed JSON, unknown field, several JSON values, bad `limit` or `cursor` |
| 401 | `about:blank` | Missing or wrong admin token |
| 404 | `not-found` | Resource missing, owned by another user, or id not a UUID |
| 405 | `about:blank` | Wrong method on a known path |
| 409 | `conflict` | Unique rule broken: email in use, slug taken, bundle id + environment taken |
| 413 | `about:blank` | Body over 1 MiB |
| 415 | `unsupported-media-type` | `Content-Type` not JSON |
| 422 | `validation` | Field values invalid; `invalid-params` lists **all** of them |
| 500 | `about:blank` | Unexpected error. Details are logged with the `request_id`, never returned |
| 503 | `about:blank` | `/readyz` only: database unreachable |
| 504 | — | Request exceeded `REQUEST_TIMEOUT` |

The `type` URIs are identifiers, not links that resolve. Clients should branch
on `status` and `type`.

Other users' resources return **404, not 403**, so the API never confirms that
an id exists.

## Resources

### User

```json
{
  "user_id": "8f0c…",
  "name": "Wail",
  "email": "wail@example.com",
  "created_at": "2026-10-16T14:03:11.52Z",
  "updated_at": "2026-10-16T14:03:11.52Z"
}
```

| Field | Create | Update | Rules |
| --- | --- | --- | --- |
| `name` | required | optional, not null | 1–255 characters |
| `email` | optional | optional, nullable | A bare RFC 5322 address (`name@example.com`, no display name such as `"Wail <…>"`), ≤ 320; unique ignoring case; `""` is treated as no email |

### Project

```json
{
  "project_id": "1d2e…",
  "user_id": "8f0c…",
  "name": "Club App",
  "slug": "club-app",
  "settings": { "rate_limit_per_minute": 60 },
  "created_at": "…",
  "updated_at": "…"
}
```

| Field | Create | Update | Rules |
| --- | --- | --- | --- |
| `name` | required | optional, not null | 1–255 characters |
| `slug` | optional (from `name`) | optional, not null | `a-z`, `0-9`, single dashes; ≤ 100; unique per user |
| `settings` | optional | optional, nullable (null = reset) | Object with the keys in [3. Data model](03-data-model.md#projects-migration-00002); values > 0 |

Derived slugs keep only ASCII letters and digits: `"Club App"` →
`club-app`, `"Été 2026"` → `t-2026`. Pass `slug` explicitly for names that
are not ASCII.

### App

```json
{
  "app_id": "77aa…",
  "project_id": "1d2e…",
  "name": "Club iOS",
  "platform": "ios",
  "bundle_id": "dev.microclub.app",
  "environment": "production",
  "created_at": "…",
  "updated_at": "…"
}
```

| Field | Create | Update | Rules |
| --- | --- | --- | --- |
| `name` | required | optional, not null | 1–255 characters |
| `platform` | required | optional, not null | `ios`, `android`, `flutter`, `react_native` |
| `bundle_id` | required | optional, not null | 1–255 characters |
| `environment` | optional (`production`) | optional, not null | `^[a-z0-9][a-z0-9_-]{0,49}$` |

`(bundle_id, environment)` is unique within a project.

## Walkthrough

```sh
H='-H "Authorization: Bearer $ADMIN_API_TOKEN" -H Content-Type:application/json'

# 1. user
curl -s -X POST localhost:8080/v1/users $H -d '{"name":"Wail"}'
# → {"user_id":"U", …}

# 2. project
curl -s -X POST localhost:8080/v1/users/U/projects $H -d '{"name":"Club App"}'
# → {"project_id":"P","slug":"club-app", …}

# 3. apps
curl -s -X POST localhost:8080/v1/users/U/projects/P/apps $H \
  -d '{"name":"Club iOS","platform":"ios","bundle_id":"dev.microclub.app"}'
curl -s -X POST localhost:8080/v1/users/U/projects/P/apps $H \
  -d '{"name":"Club iOS","platform":"ios","bundle_id":"dev.microclub.app","environment":"staging"}'

# 4. change a setting, then reset all settings
curl -s -X PATCH localhost:8080/v1/users/U/projects/P $H -d '{"settings":{"rate_limit_per_minute":120}}'
curl -s -X PATCH localhost:8080/v1/users/U/projects/P $H -d '{"settings":null}'
```

(`$H` is shown as a shorthand; in a real shell, pass the two `-H` flags directly.)

## Not yet available

- No machine-readable OpenAPI description yet; this document is the
  reference.
- No filtering or search on lists.
- No `ETag` or `If-Match`: concurrent `PATCH`es are last-write-wins.
- No rate limiting on the admin API itself.
