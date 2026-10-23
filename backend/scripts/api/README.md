# API test scripts

End-to-end tests for every API endpoint, written in bash with curl. They run
against a running backend, normally the dev stack in `deploy/`.

```sh
make dev-up                          # start the stack (fake GitHub included)
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
| `providers.sh` | provider list; old owner-level routes gone; **per-app** connect flow (unknown state, **forged installation_id**, state replay, missing code, pending org approval, success, redirect back to the app); **connections are per app**; attach (share an installation between apps); one installation never spans two owners; repositories; link by tracker (first is default, duplicate, malformed, unconfigured tracker); default switching and rollback; isolation; disconnect affects only that app |
| `issues.sh` | **issue service**: targets preview; open in the default repository **through the app's own connection**; ref, URL, record and `connection_id`; content on the fake GitHub; default title; **an app without a connection fails, then works after attach, with its own connection**; **an app connected to two GitHub accounts** routes each repository through the matching one; fan-out to every linked project; non-defaults ignored; partial failure (201 + per-target error); total failure (502); projects without a default skipped; **GitHub rate limits** (secondary limit waited out without duplicates; exhausted quota → fast 429 + `Retry-After`, GitHub not called again, also for another app sharing the installation; per-app test-issue throttle); validation; isolation |
| `gitlab.sh` | **GitLab, per-app OAuth**: authorize URL; rejected callbacks (denied, replayed state, rejected code, missing code, wrong tracker); connect; repositories; OAuth connections cannot be attached; each app runs its own OAuth (the same user may authorise two apps); issues through each app's own token; **token refresh and rotation**; **revoked authorization** and reconnect; 500, short and long 429; app without a connection; another owner; disconnect; subgroup projects and labels |
| `bitbucket.sh` | the same suite for **Bitbucket Cloud**, plus: labels in the body, a repository without an issue tracker, name format |
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

## GitHub modes

| `GITHUB_MODE` | How the connection is made | Notes |
|---|---|---|
| `fake` (default) | The script plays the browser. It calls `/apps/{id}/connect/github`, then `/providers/github/callback` with a random installation `N` and code `fake-code-N`, as GitHub's redirect would. | Fully automatic. Also runs the forged-ID, replay, two-account, fan-out and failure tests. |
| `real` | The script prints the install URL, you install the App in a browser, and the script polls `/apps/{id}/connections` until the connection appears. | Set `GITHUB_TEST_REPO=owner/name` to a repository the App can access. **This opens real issues**, labelled `m-seismo-test`. Fake-only tests are skipped. |

`gitlab.sh` and `bitbucket.sh` always use the fake GitLab and Bitbucket
(they skip themselves when those are not reachable) and create fresh apps
and connections on every run.

The test app and its GitHub connection are saved and reused across runs.
Apps that a script creates get their own connection by **attaching** the saved
one, so in real mode you install once.

## Environment

| Variable | Default |
|---|---|
| `API_URL` | `http://localhost:8080/api/v1` |
| `FAKE_GITHUB_URL` | `http://localhost:9090` |
| `FAKE_GITLAB_URL` | `http://localhost:9091` |
| `FAKE_BITBUCKET_URL` | `http://localhost:9092` |
| `GITHUB_MODE` | `fake` |
| `GITHUB_TEST_REPO` | `octo-org/seismo-test` |
| `STATE_FILE` | `backend/scripts/api/.env.test` |
| `VERBOSE` | `0` |

The full interface reference is in
[docs/test-scripts.md](../../docs/test-scripts.md). It covers every
`lib.sh` function, the globals, the state file, exit codes and the fake
GitHub contract.

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
