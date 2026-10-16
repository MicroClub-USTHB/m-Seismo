# 10. Roadmap and gaps

This is the honest list: what is done, what is not, what is weak in what is
done, and what still needs a decision.

## Phase status

| Phase | Content | Status |
| --- | --- | --- |
| 0 | Config, logging, pgx pool, goose migrations, router and middleware, RFC 9457 errors, health checks, graceful shutdown, dev compose, test harness | **Done** |
| 1 | Users CRUD + audit | **Done** |
| 2 | Projects + apps CRUD | **Done** |
| 3 | `secrets` (AES-GCM), `vcs` adapters (GitHub App, GitLab, Bitbucket), connections API, GitHub install flow, remote repository listing | Next. Design in [6](06-git-connections.md) |
| 4 | Repository links, default repository, app override | Planned. Design in [6](06-git-connections.md) |
| 5 | Issues service: `Submit`, idempotency, dedup, limiter, attachments, outbox worker, publishing | Planned. Design in [7](07-issues-service.md) |
| 6 | GitHub webhooks (issue closed → resolved, FR-51; installation removed), retention jobs, metrics, Postgres limiter, OAuth for GitLab/Bitbucket | Later |

## Missing: functionality

- **Git connections, repository links, issues service**: phases 3–5.
- **Two-way status** (PRD FR-51): needs the webhook receiver (phase 6).
- **Retention and deletion** (FR-30, FR-31): no job yet deletes old issues,
  submissions, attachments or orphan blobs.
- **Distinct installations and affected versions per issue** (FR-56):
  designed (`issue_occurrences`) but not in the phase 5 schema yet.
- **Regression detection** (FR-57): v1.x. A closed issue's bug opens a new
  issue today.
- **Routing rules by severity or category** (FR-58): only "app override,
  else project default" is planned. Labels, assignees and templates per rule
  are not designed yet.
- **Issue templates per project** (FR-50): one built-in template is planned.
- **Manual retry of failed publish jobs** (FR-62): jobs are kept but there is
  no API to retry them. It comes with the console.
- **List filtering and search** on the admin API.
- **OpenAPI description** of the admin API. Proposed: write it in `spec/`
  once phase 4 settles the surface, and test responses against it.

## Security

| Gap | Risk | Plan |
| --- | --- | --- |
| **Single shared admin token**; `user_id` in the path is trusted | Anyone with the token can act as any user | Replace with real authentication (OIDC/SSO or per-user API keys). Middleware then sets the actor and the allowed `user_id`; services already scope by `user_id`, so only the middleware changes. Until then, keep `/v1` on a private network |
| `RealIP` trusts forwarding headers | Faked client IPs in logs | Deploy only behind a proxy that overwrites them, or drop the middleware |
| No rate limit on the admin API | Brute force of the token is impractical (≥ 32 characters), but there is no throttling | Add a per-IP limiter when the API is exposed beyond an internal network |
| GitHub install callback (planned) | Installation-id swap (see [6](06-git-connections.md#github-github-app)) | Turn on OAuth-during-install and verify the user can access the installation |
| User-supplied `base_url` (planned) | SSRF | https only; block private and link-local addresses by default |
| Presigned attachment links (planned) | Anyone with the link can read the file until it expires | Short enough TTL; private bucket; links only inside issues (which are access-controlled) |
| No security headers | Low: JSON API, no browser UI | Add `X-Content-Type-Options: nosniff` and `Cache-Control: no-store` on `/v1` |

## Known limitations of the implemented code

- **Empty PATCH still writes.** `PATCH {}` runs an `UPDATE` that changes no
  values but bumps `updated_at` (the trigger fires) and writes an audit row.
  Fix: skip the statement (return the current row) when no field is set.
- **Concurrent edits are last-write-wins.** No `ETag` / `If-Match`.
- **Slugs drop non-ASCII letters.** `"Été 2026"` → `t-2026`. Fix: transliterate
  first (e.g. `golang.org/x/text` + a fold table), or ask clients to send
  `slug`.
- **User deletion keeps audit rows but unlinks them** (`user_id` set to NULL).
  The rows are still found by `entity_id`, but "everything this deleted user
  did" can no longer be queried. Decide whether audit rows should copy the
  owner id into a non-FK column.
- **Hard deletes only.** Deleting a user immediately cascades to all projects
  and apps, with no undo and no confirmation step. Consider a soft-delete or
  a "type the name to confirm" flow in the console.
- **Email is optional and unverified.** Uniqueness ignores case. There is no
  verification flow, so the email is only informative.
- **Pagination order is fixed** (oldest first). No sorting options.
- **`project.settings` is stored but unused** until the issues service
  exists.
- **`504` on timeout has no problem body.** chi's `Timeout` middleware
  writes a bare status.

## Deviations from the plan

| Plan said | Code does | Why | Revisit when |
| --- | --- | --- | --- |
| `sqlc` for typed queries | Hand-written SQL with pgx | No codegen tool in the environment; few queries, all integration-tested; partial-update `CASE` pattern | Query count grows a lot, or a schema change breaks queries that tests did not catch |
| Layered `handler/service/repository/model` folders (wail-docs) | Package per feature + `httpapi` | Keeps each feature in one place; less exported surface | — |
| `uuid-ossp` + `uuid_generate_v4()` | `gen_random_uuid()` | Built in since PG 13; no extension | — |
| `error_logs` table | Structured logs | The DB is the wrong place for errors that may be caused by the DB | — |
| RFC 7807 | RFC 9457 | 9457 replaces 7807 | — |
| Audit middleware | Audit in services, same transaction | Middleware cannot know the entity or the diff, and could log rolled-back changes | — |
| `apps` owned by users + `app_projects` junction | `apps` belong to one project; repositories link to projects | Clear routing rule for the issues service | — |

## Open decisions (owner: product)

1. **Bitbucket and GitLab in v1?** The PRD lists GitHub only for v1. This
   branch builds all three behind one interface. Confirm, and update the PRD
   (PRD section 22).
2. **GitLab and Bitbucket authentication:** access tokens now, or OAuth
   from the start? The schema supports both. OAuth needs a registered app per
   instance and a callback route.
3. **Attachment links in long-lived issues:** presigned links expire (max 7
   days with S3 SigV4). Options: (a) accept that old attachments go dark;
   (b) a small redirect endpoint that re-signs on demand, which needs a
   route, ruled out for now; (c) upload images to the provider where
   possible (GitLab and Bitbucket support uploads; GitHub's REST API does
   not).
4. **Who are "users"?** Today they are configuration owners managed by an
   admin. With real authentication: one person, or a team/organisation?
   This decides whether projects need an `organisation_id`.
5. **Retention defaults** (FR-31): how long to keep issues metadata,
   submissions and attachments.

## Engineering backlog (small, ready to pick up)

- [ ] CI workflow: fmt, vet, test on Linux with Docker; fail on skipped tests;
  `-race`.
- [ ] Skip no-op PATCHes (no `updated_at` bump, no audit row).
- [ ] `nosniff` / `no-store` headers on `/v1`.
- [ ] Dockerfile (distroless, `CGO_ENABLED=0`) and a Helm chart in `deploy/`.
- [ ] Prometheus metrics: request count/latency per route, pool stats, and
  later outbox depth and failure count.
- [ ] Problem body for timeouts (custom timeout middleware).
- [ ] OpenAPI spec in `spec/`.
