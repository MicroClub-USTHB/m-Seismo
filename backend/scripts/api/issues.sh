#!/usr/bin/env bash
# Issue service: opens issues through POST /apps/{id}/test-issue, which
# calls issue.Service.OpenForApp exactly as a real report would, then checks
# the failure modes (and, in fake mode, the issues on the tracker side). Every
# issue is opened through the reporting app's own connection, so this also
# covers how targets are resolved per app (GET /apps/{id}/targets).
#
#   GITHUB_TEST_REPO=me/repo backend/scripts/api/issues.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token
require_github_repo || { summary; exit 0; }
ensure_github_connection || { summary; exit 1; }
# The saved app's connection is attached to the apps created below, so a
# real GitHub App is installed once and reused.
shared_connection=$CONNECTION_ID

run=$(rand)

section "setup: app + connection -> project -> default repository"
create_project "Issues A $run"
project_a=$CREATED_ID
link_repo "$project_a" "$GITHUB_TEST_REPO" true
create_app "Issues App $run"
app_id=$CREATED_ID
attach_connection "$app_id" "$shared_connection" && pass "give the app a GitHub connection"
app_conn=$CREATED_ID
api PUT "/apps/$app_id/projects/$project_a"
expect_status 204 "link app to project A ($GITHUB_TEST_REPO)"

section "targets preview"
api GET "/apps/$app_id/targets"
expect_status 200 "GET /apps/{id}/targets"
expect_eq "$(jlen .)" 1 "one target"
expect_json '.[0].repository' "$GITHUB_TEST_REPO" "the project's default repository"
expect_json '.[0].connection_id' "$app_conn" "reached through the app's own connection"
expect_json '.[0].error' null "no error"

section "open an issue in one repository"
title="m-Seismo API test $run"
api POST "/apps/$app_id/test-issue" "{\"title\":\"$title\",\"body\":\"Opened by scripts/api/issues.sh run $run\"}"
expect_status 201 "test issue opened"
expect_eq "$(jlen .results)" 1 "one target"
expect_json '.results[0].repository' "$GITHUB_TEST_REPO" "opened in the default repository"
expect_json '.results[0].tracker' github "on github"
expect_json '.results[0].connection_id' "$app_conn" "through the app's own connection"
expect_json '.results[0].error' null "no error"
expect_match "$(jget '.results[0].ref')" "^${GITHUB_TEST_REPO}#[0-9]+$" "ref is owner/name#number"
expect_match "$(jget '.results[0].url')" '^https?://.+/issues/[0-9]+$' "url points at the issue"
expect_match "$(jget '.results[0].issue_id')" '^[0-9a-f-]{36}$' "issue recorded in m-Seismo"
note "issue: $(jget '.results[0].url')"

if require_fake "issue content checks on the tracker"; then
  expect_eq "$(fake_issue_count "$title")" 1 "fake GitHub received exactly one issue with that title"
  # Only test issues are opened on the fake, so every one must carry the label.
  http GET "$FAKE_GITHUB_URL/_fake/issues"
  expect_eq "$(jcount . labels '["m-seismo-test"]')" "$(jlen .)" "issues carry the m-seismo-test label"
  expect_eq "$(jcount . body "Opened by scripts/api/issues.sh run $run")" 1 "body reached the tracker"
fi

section "default title and body"
api POST "/apps/$app_id/test-issue"
expect_status 201 "test issue without a body"
default_title="m-Seismo test issue from Issues App $run"
if require_fake "default title on the tracker"; then
  expect_eq "$(fake_issue_count "$default_title")" 1 "default title used"
fi

section "each app opens through its own connection"
create_app "Second App $run"
second_app=$CREATED_ID
api PUT "/apps/$second_app/projects/$project_a"
expect_status 204 "link a second app to the same project"
api GET "/apps/$second_app/targets"
expect_json '.[0].connection_id' null "without a connection the target has none"
expect_match "$(jget '.[0].error')" 'no github connection on this app covers' "and says why"
noconn_title="m-Seismo no connection $run"
api POST "/apps/$second_app/test-issue" "{\"title\":\"$noconn_title\"}"
expect_status 502 "the project's connection-less app cannot open issues (502)"
expect_match "$(jget '.results[0].error')" 'no github connection on this app covers' "per-target error names the missing connection"
expect_json '.results[0].connection_id' null "no connection used"
if require_fake "nothing reached the tracker"; then
  expect_eq "$(fake_issue_count "$noconn_title")" 0 "no issue was opened"
fi
attach_connection "$second_app" "$shared_connection" && pass "give the second app its own connection"
second_conn=$CREATED_ID
expect_ne "$second_conn" "$app_conn" "a separate connection row from the first app's"
api POST "/apps/$second_app/test-issue" '{"title":"m-Seismo second app"}'
expect_status 201 "the second app now opens issues"
expect_json '.results[0].connection_id' "$second_conn" "through its own connection, not the first app's"
api DELETE "/apps/$second_app/connections/$second_conn"
api GET "/apps/$app_id/targets"
expect_json '.[0].connection_id' "$app_conn" "disconnecting one app leaves the other app's targets intact"
api DELETE "/apps/$second_app"

if require_fake "connections on several GitHub accounts"; then
  section "an app with connections on two GitHub accounts"
  create_project "Other Org $run"
  project_other=$CREATED_ID
  link_repo "$project_other" other-org/tools true
  api PUT "/apps/$app_id/projects/$project_other"
  api GET "/apps/$app_id/targets"
  expect_eq "$(jlen .)" 2 "two targets"
  expect_eq "$(jcount . repository other-org/tools)" 1 "other-org/tools is a target"
  for i in 0 1; do
    if [[ $(jget ".[$i].repository") == other-org/tools ]]; then
      expect_json ".[$i].connection_id" null "an octo-org installation does not cover other-org"
    fi
  done
  org_title="m-Seismo two orgs before $run"
  api POST "/apps/$app_id/test-issue" "{\"title\":\"$org_title\"}"
  expect_status 201 "201: the octo-org target still opens"
  expect_eq "$(jcount .results error null)" 1 "one of two targets opened"
  expect_eq "$(fake_issue_count "$org_title")" 1 "only octo-org got the issue"

  github_connect "$app_id" "$((100000000 + $(fake_install_id)))" && pass "connect the app to an other-org installation"
  other_conn=$CONNECTION_ID
  api GET "/apps/$app_id/connections"
  expect_eq "$(jlen .)" 2 "the app has two GitHub connections"
  expect_eq "$(jcount . account_login other-org)" 1 "one of them on other-org"
  org_title="m-Seismo two orgs after $run"
  api POST "/apps/$app_id/test-issue" "{\"title\":\"$org_title\"}"
  expect_status 201 "issue opened in both accounts"
  expect_eq "$(jcount .results error null)" 2 "both targets opened"
  expect_eq "$(jcount .results connection_id "$other_conn")" 1 "other-org/tools went through the other-org connection"
  expect_eq "$(jcount .results connection_id "$app_conn")" 1 "the octo-org repository through the octo-org one"
  expect_eq "$(fake_issue_count "$org_title")" 2 "fake GitHub received both"

  api DELETE "/apps/$app_id/connections/$other_conn"
  api DELETE "/apps/$app_id/projects/$project_other"
  api DELETE "/projects/$project_other"
fi

if require_fake "fan-out and failure checks"; then
  section "fan-out to every linked project"
  create_project "Issues B $run"
  project_b=$CREATED_ID
  link_repo "$project_b" octo-org/another true
  api PUT "/apps/$app_id/projects/$project_b"
  fan_title="m-Seismo fan-out $run"
  api POST "/apps/$app_id/test-issue" "{\"title\":\"$fan_title\"}"
  expect_status 201 "fan-out issue opened"
  expect_eq "$(jlen .results)" 2 "two targets"
  expect_eq "$(jcount .results repository "$GITHUB_TEST_REPO")" 1 "one in project A's repository"
  expect_eq "$(jcount .results repository octo-org/another)" 1 "one in project B's repository"
  expect_eq "$(jcount .results error null)" 2 "both succeeded"
  expect_eq "$(fake_issue_count "$fan_title")" 2 "fake GitHub received both"

  section "non-default repositories are not targeted"
  link_repo "$project_a" octo-org/another false
  extra_repo=$CREATED_ID
  only_title="m-Seismo defaults only $run"
  api POST "/apps/$app_id/test-issue" "{\"title\":\"$only_title\"}"
  expect_eq "$(jlen .results)" 2 "still one target per project"
  expect_eq "$(fake_issue_count "$only_title")" 2 "two issues, not three"
  api DELETE "/projects/$project_a/repositories/$extra_repo"

  section "partial failure: one tracker target fails"
  create_project "Issues Broken $run"
  project_broken=$CREATED_ID
  link_repo "$project_broken" octo-org/broken true
  api PUT "/apps/$app_id/projects/$project_broken"
  partial_title="m-Seismo partial $run"
  api POST "/apps/$app_id/test-issue" "{\"title\":\"$partial_title\"}"
  expect_status 201 "still 201 when at least one target succeeds"
  expect_eq "$(jlen .results)" 3 "three targets"
  expect_eq "$(jcount .results error null)" 2 "two succeeded"
  expect_eq "$(jcount .results repository octo-org/broken)" 1 "broken repository reported"
  for i in 0 1 2; do
    if [[ $(jget ".results[$i].repository") == octo-org/broken ]]; then
      expect_match "$(jget ".results[$i].error")" '500|Server Error' "its error explains the tracker failure"
      expect_json ".results[$i].issue_id" null "nothing recorded for the failed target"
    fi
  done
  expect_eq "$(fake_issue_count "$partial_title")" 2 "the healthy repositories got the issue"

  section "total failure: every target fails"
  create_app "Broken App $run"
  broken_app=$CREATED_ID
  attach_connection "$broken_app" "$shared_connection"
  api PUT "/apps/$broken_app/projects/$project_broken"
  api POST "/apps/$broken_app/test-issue" '{"title":"never opened"}'
  expect_status 502 "502 when no issue could be opened"
  expect_json .error "no issue could be opened" "error summary"
  expect_eq "$(jlen .results)" 1 "per-target result still returned"
  expect_ne "$(jget '.results[0].error')" null "with the target's error"

  section "skipped targets"
  api GET "/projects/$project_broken/repositories"
  broken_repo=$(jget '.[0].id')
  api DELETE "/projects/$project_broken/repositories/$broken_repo"
  expect_status 204 "remove the broken project's only repository"
  api POST "/apps/$broken_app/test-issue"
  expect_status 422 "a project without a default repository is not a target"
  api GET "/apps/$broken_app/targets"
  expect_eq "$(jlen .)" 0 "and it is not in the targets preview"
  api DELETE "/apps/$broken_app"
  api DELETE "/projects/$project_broken"
  api DELETE "/projects/$project_b"
fi

if require_fake "GitHub rate-limit checks"; then
  section "GitHub secondary rate limit is waited out"
  create_project "Slow $run"
  project_slow=$CREATED_ID
  link_repo "$project_slow" octo-org/slow-down true
  create_app "Slow App $run"
  slow_app=$CREATED_ID
  attach_connection "$slow_app" "$shared_connection"
  api PUT "/apps/$slow_app/projects/$project_slow"
  before=$(fake_stat secondary_limited)
  slow_title="m-Seismo slow-down $run"
  started=$SECONDS
  api POST "/apps/$slow_app/test-issue" "{\"title\":\"$slow_title\"}"
  expect_status 201 "issue opened despite a secondary rate limit"
  expect_eq "$(fake_stat secondary_limited)" "$((before + 1))" "GitHub answered with a secondary limit once"
  expect_eq "$(fake_issue_count "$slow_title")" 1 "retry opened exactly one issue (no duplicate)"
  expect_match "$((SECONDS - started))" '^[1-9][0-9]*$' "waited at least Retry-After (1s) before retrying"

  section "GitHub primary quota exhausted fails fast"
  # A fresh installation, so the exhausted quota does not block the shared one.
  create_app "Exhausted App $run"
  exhausted_app=$CREATED_ID
  github_connect "$exhausted_app"
  exhausted_connection=$CONNECTION_ID
  create_project "Exhausted $run"
  project_exhausted=$CREATED_ID
  link_repo "$project_exhausted" octo-org/exhausted true
  api PUT "/apps/$exhausted_app/projects/$project_exhausted"
  before=$(fake_stat primary_limited)
  started=$SECONDS
  api POST "/apps/$exhausted_app/test-issue" '{"title":"never opened (quota)"}'
  expect_status 429 "429 when the installation's hourly quota is gone"
  expect_match "$(header Retry-After)" '^[0-9]+$' "Retry-After header present"
  expect_eq "$( (($(header Retry-After) > 3000)) && echo yes)" yes "Retry-After points at the quota reset (~1h)"
  expect_match "$(jget '.results[0].retry_after_seconds')" '^[0-9]+$' "per-target retry_after_seconds"
  expect_match "$(jget '.results[0].error')" 'primary rate limit' "error names the primary limit"
  expect_eq "$( ((SECONDS - started < 10)) && echo yes)" yes "did not wait an hour for the reset"
  expect_eq "$(fake_stat primary_limited)" "$((before + 1))" "GitHub was called once"

  api POST "/apps/$exhausted_app/test-issue" '{"title":"never opened (quota) again"}'
  expect_status 429 "still 429 while the quota is exhausted"
  expect_eq "$(fake_stat primary_limited)" "$((before + 1))" "no further GitHub calls until the reset"

  # GitHub's quota is per installation, so another app sharing it is limited too.
  create_app "Exhausted Sibling $run"
  sibling_app=$CREATED_ID
  attach_connection "$sibling_app" "$exhausted_connection"
  api PUT "/apps/$sibling_app/projects/$project_exhausted"
  api POST "/apps/$sibling_app/test-issue" '{"title":"never opened (shared quota)"}'
  expect_status 429 "an app sharing the exhausted installation gets 429 too"
  expect_eq "$(fake_stat primary_limited)" "$((before + 1))" "without calling GitHub"

  section "test-issue is throttled per app"
  create_app "Throttle App $run"
  throttle_app=$CREATED_ID
  got429=""
  for i in $(seq 1 30); do
    api POST "/apps/$throttle_app/test-issue"
    if [[ $STATUS == 429 ]]; then got429=$i; break; fi
  done
  expect_match "$got429" '^[0-9]+$' "429 after limits.test_issues_per_minute requests"
  expect_json .error "too many test issues for this app; retry later" "error explains the throttle"
  expect_match "$(header Retry-After)" '^[0-9]+$' "throttle sets Retry-After"
  api POST "/apps/$app_id/test-issue" '{"title":"other apps are not throttled"}'
  expect_status 201 "the throttle is per app"

  # Deleting the apps removes their connections.
  for a in "$slow_app" "$exhausted_app" "$sibling_app" "$throttle_app"; do api DELETE "/apps/$a"; done
  for pr in "$project_slow" "$project_exhausted"; do api DELETE "/projects/$pr"; done
fi

section "validation and isolation"
api POST "/apps/$app_id/test-issue" '{"title":"x","severity":"high"}'
expect_status 422 "unknown field is rejected"
create_app "Lonely App $run"
lonely=$CREATED_ID
api POST "/apps/$lonely/test-issue"
expect_status 422 "app with no linked project -> 422"
expect_json .error "app has no linked project with a default repository" "error explains why"
api GET "/apps/$lonely/targets"
expect_status 200 "targets of an app with no linked project"
expect_eq "$(jlen .)" 0 "is an empty list"
api DELETE "/apps/$lonely"

new_owner
http POST "/apps/$app_id/test-issue" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot open issues for our app"
http GET "/apps/$app_id/targets" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot see our app's targets"
http POST "/apps/$app_id/test-issue" ""
expect_status 401 "no bearer token -> 401"
http DELETE /owners/me "" "$NEW_TOKEN"

section "cleanup"
api DELETE "/apps/$app_id"
expect_status 204 "delete app"
api DELETE "/projects/$project_a"
expect_status 204 "delete project"

summary
