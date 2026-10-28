#!/usr/bin/env bash
# Bitbucket Cloud: per-app OAuth connect flow (authorize URL, denied /
# replayed / rejected / missing codes, success), the token stored per app
# (cannot be attached to another app; the same user may authorise several
# apps), repositories, issues opened through each app's own token, token
# refresh, revoked authorizations, Bitbucket errors and rate limits,
# isolation and disconnect. Then Bitbucket specifics: repositories without
# an issue tracker, labels in the body, name format. Needs the fake
# Bitbucket of the dev stack.
#
#   backend/scripts/api/bitbucket.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
source "$LIB_DIR/oauth_suite.sh"
wait_for_server
ensure_token

TRACKER=bitbucket
AUTH_PATH=site/oauth2/authorize
LOGIN_PREFIX=bb-user-
REPO_OK=seismo-ws/seismo-test
REPO_BROKEN=seismo-ws/broken
REPO_LIMITED=seismo-ws/limited
REPO_EXHAUSTED=seismo-ws/exhausted
REPO_COUNT=6
AUTHOR_FIELD=reporter
fake_author() { printf '{%08d-0000-4000-8000-000000000000}' "$1"; }

tracker_specifics() {
  section "Bitbucket: issue tracker, labels and names"
  fake_tracker_issues bitbucket
  expect_eq "$(jcount . content "Opened by scripts/api run $run

Labels: m-seismo-test")" 1 "labels are listed at the end of the body (Bitbucket has none)"
  expect_eq "$(jcount . kind bug)" "$(jlen .)" "issues are of kind bug"

  link_repo "$project" seismo-ws/no-issues true bitbucket
  api POST "/apps/$app_b/test-issue" '{"title":"never opened (no tracker)"}'
  expect_status 502 "a repository without an issue tracker fails"
  expect_match "$(jget '.results[0].error')" 'Repository has no issue tracker' "with Bitbucket's reason"
  api GET "/projects/$project/repositories"
  for i in 0 1; do
    [[ $(jget ".[$i].repository") == "$REPO_OK" ]] && api PUT "/projects/$project/repositories/$(jget ".[$i].id")/default"
  done

  for bad in no-slash "ws/sub/repo" "ws/-repo"; do
    api POST "/projects/$project/repositories" "{\"tracker\":\"bitbucket\",\"repository\":\"$bad\"}"
    expect_status 422 "malformed Bitbucket name '$bad' is rejected"
  done
}

run_oauth_tracker_suite
summary
