# API test scripts

End-to-end tests for every API endpoint, written in bash with curl. They run
against a running backend, normally the dev stack in `deploy/`.

```sh
make dev-up                          # start the stack
backend/scripts/api/all.sh           # run everything
backend/scripts/api/projects.sh      # or one service
backend/scripts/api/all.sh --fresh   # forget the saved owner/token/connection first
VERBOSE=1 backend/scripts/api/apps.sh  # print every request and response
```

The scripts need `curl`, plus `jq` or Python 3 for JSON parsing; Python is
used when `jq` is missing. They work in Linux and macOS shells and in Git
Bash on Windows.

## Scripts

| Script | Covers |
|---|---|
| `auth.sh` | health; register (normalisation, duplicates, validation); login (case-insensitive, same error for wrong password and unknown email); bearer checks (missing, garbage, tampered); the saved-token lifecycle |
| `owners.sh` | `GET/PATCH/DELETE /owners/me`; `current_password` rules; email/password change then login; delete then token and login fail |
| `projects.sh` | CRUD; derived and explicit slugs; conflicts; validation; malformed and unknown IDs; per-owner isolation; slugs unique per owner |
| `apps.sh` | CRUD; platform validation; SDK keys (format, shown once, revoke, revoke twice); coordinator keys (format, kept apart from SDK keys); app↔project links (idempotent, unknown, cross-owner); test-issue without targets |
| `providers.sh` | provider list; old owner-level routes gone; **per-app** connect flow (unknown state, **forged installation_id**, state replay, missing code, pending org approval, success, redirect back to the app); **connections are per app**; attach (share an installation between apps); one installation never spans two owners; repositories; link by tracker (first is default, duplicate, malformed, unconfigured tracker); default switching and rollback; the default can't be unlinked while another repository is linked; isolation; disconnect affects only that app |
| `routing.sh` | **routing rules**: create (placed last, labels cleaned up), order and replace (keeps its place), validation (enums, position, labels, assignees, repository of another project, unknown app), a repository rules use and the default can't be unlinked, isolation (including a rule for another owner's app), rules go with their app |
| `issues.sh` | **issue service**: targets preview; open in the default repository **through the app's own connection**; ref, URL, record and `connection_id`; content on the fake GitHub; default title; **an app without a connection fails, then works after attach, with its own connection**; **an app connected to two GitHub accounts** routes each repository through the matching one; fan-out to every linked project; non-defaults ignored; partial failure (201 + per-target error); total failure (502); projects without a default skipped; **GitHub rate limits** (secondary limit waited out without duplicates; exhausted quota → fast 429 + `Retry-After`, GitHub not called again, also for another app sharing the installation; per-app test-issue throttle); validation; isolation |
| `gitlab.sh` | **GitLab, per-app OAuth**: authorize URL; rejected callbacks (denied, replayed state, rejected code, missing code, wrong tracker); connect; repositories; OAuth connections cannot be attached; each app runs its own OAuth (the same user may authorise two apps); issues through each app's own token; **token refresh and rotation**; **revoked authorization** and reconnect; 500, short and long 429; app without a connection; another owner; disconnect; subgroup projects and labels |
| `bitbucket.sh` | the same suite for **Bitbucket Cloud**, plus: labels in the body, a repository without an issue tracker, name format |
| `webhooks.sh` | **GitHub App webhook**: wrong, `sha1=` and missing signatures refused (401); `ping`, other issue actions and issues m-Seismo didn't open acknowledged and ignored (204); invalid JSON (400). Needs `GITHUB_WEBHOOK_SECRET`, otherwise skipped |
| `oauth_suite.sh` | not a test script: the suite shared by `gitlab.sh` and `bitbucket.sh` |
| `all.sh` | runs all of the above in order; exit code 0 only if every script passed |

## Bearer token handling

The owner's email, password, bearer token and owner ID, plus a test app and
that app's GitHub connection ID, are saved in `scripts/api/.env.test`. That file is git-ignored; override its
location with `STATE_FILE`.

- **At the start of each script**, `ensure_token` checks the saved token with
  `GET /owners/me`. If it doesn't work, the script logs in again, or
  registers a new owner if that one no longer exists, and saves the new token.
- **On every authenticated request**, `api` re-reads the token from the state
  file. If the API answers `401 invalid or expired token`, the helper logs in
  again, writes the new token back, and retries once. `auth.sh` tests this by
  breaking the saved token on purpose.
- **Isolation tests** register a throwaway second owner with its own token
  (`new_owner`) and delete it at the end.

## GitHub

The dev stack has no fake trackers, so the GitHub checks run against a real
GitHub App (`GITHUB_MODE=real`, the default):

1. Configure the App in `deploy/.env` (see `deploy/README.md`).
2. Set `GITHUB_TEST_REPO=owner/name` to a repository the App can access.
   **This opens real issues** there, labelled `m-seismo-test`.
3. On the first run, the script prints the install URL. Install the App in
   a browser; the script polls `/apps/{id}/connections` until the
   connection appears.

Without `GITHUB_TEST_REPO`, `providers.sh` and `issues.sh` skip themselves.
Checks that need a fake tracker (forged installation IDs, replay, two
accounts, fan-out, failures, rate limits) are skipped in real mode.

`GITHUB_MODE=fake` still exists for a fake GitHub served elsewhere (set
`FAKE_GITHUB_URL`); the repository no longer ships one.

`gitlab.sh` and `bitbucket.sh` need a fake GitLab and Bitbucket, and skip
themselves when none is reachable. The GitLab and Bitbucket providers are
covered by their Go tests instead.

The test app and its GitHub connection are saved and reused across runs.
Apps that a script creates get their own connection by **attaching** the saved
one, so in real mode you install once.

## Environment

| Variable | Default |
|---|---|
| `API_URL` | `http://localhost:8080/api/v1` |
| `GITHUB_MODE` | `real` |
| `GITHUB_TEST_REPO` | empty (GitHub scripts skipped) |
| `GITHUB_WEBHOOK_SECRET` | empty (`webhooks.sh` skipped) |
| `FAKE_GITHUB_URL`, `FAKE_GITLAB_URL`, `FAKE_BITBUCKET_URL` | `http://localhost:9090`, `:9091`, `:9092` (fake mode only) |
| `STATE_FILE` | `backend/scripts/api/.env.test` |
| `VERBOSE` | `0` |

The full interface reference is in
[docs/test-scripts.md](../../docs/test-scripts.md). It covers every
`lib.sh` function, the globals, the state file and exit codes.

## Writing a new check

```bash
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token

section "my feature"
api POST /things '{"name":"x"}'          # authenticated, with auto-refresh
expect_status 201 "create a thing"
expect_json .name x "name stored"
http GET /things                          # unauthenticated (or pass a token as 4th arg)
expect_status 401 "needs a token"

summary                                   # last line: sets the exit code
```
