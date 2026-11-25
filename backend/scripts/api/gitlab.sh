#!/usr/bin/env bash
# GitLab: per-app OAuth connect flow (authorize URL, denied / replayed /
# rejected / missing codes, success), the token stored per app (cannot be
# attached to another app; the same user may authorise several apps),
# repositories, issues opened through each app's own token, token refresh
# and rotation, revoked authorizations, GitLab errors and rate limits,
# isolation and disconnect. Then GitLab specifics: nested groups and
# labels. Needs the fake GitLab of the dev stack.
#
#   backend/scripts/api/gitlab.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
source "$LIB_DIR/oauth_suite.sh"
wait_for_server
ensure_token

TRACKER=gitlab
AUTH_PATH=oauth/authorize
LOGIN_PREFIX=gl-user-
REPO_OK=seismo-group/seismo-test
REPO_BROKEN=seismo-group/broken
REPO_LIMITED=seismo-group/limited
REPO_EXHAUSTED=seismo-group/exhausted
REPO_COUNT=6
AUTHOR_FIELD=author_id
fake_author() { echo "$1"; }

tracker_specifics() {
  section "GitLab: nested groups and labels"
  link_repo "$project" seismo-group/sub/nested true gitlab
  expect_json .repository seismo-group/sub/nested "a project in a subgroup can be linked"
  nested_title="m-Seismo nested $run"
  api POST "/apps/$app_b/test-issue" "{\"title\":\"$nested_title\"}"
  expect_status 201 "an issue opens in a subgroup project"
  expect_match "$(jget '.results[0].ref')" '^seismo-group/sub/nested#[0-9]+$' "ref keeps the full project path"
  fake_tracker_issues gitlab
  expect_eq "$(jcount . title "$nested_title")" 1 "GitLab received it (the path is sent URL-encoded)"
  expect_eq "$(jcount . labels '["m-seismo-test"]')" "$(jlen .)" "issues carry the m-seismo-test label"

  for bad in no-slash "seismo-group/-x" "group//project"; do
    api POST "/projects/$project/repositories" "{\"tracker\":\"gitlab\",\"repository\":\"$bad\"}"
    expect_status 422 "malformed GitLab path '$bad' is rejected"
  done
}

run_oauth_tracker_suite
summary
