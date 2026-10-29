#!/usr/bin/env bash
# Routing rules: CRUD, order, validation, the repository guards and owner
# isolation. No tracker is called: rules only pick among a project's
# repositories.
#
#   backend/scripts/api/routing.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token

run=$(rand)

section "setup: project with two repositories, an app"
create_project "Routing $run"
project_id=$CREATED_ID
link_repo "$project_id" "octo-org/triage-$run" true
default_repo=$CREATED_ID
link_repo "$project_id" "octo-org/mobile-$run" false
mobile_repo=$CREATED_ID
create_app "Routing App $run"
app_id=$CREATED_ID
create_project "Routing Other $run"
other_project=$CREATED_ID
link_repo "$other_project" "octo-org/elsewhere-$run" true
foreign_repo=$CREATED_ID

rules="/projects/$project_id/routing-rules"

section "create"
api GET "$rules"
expect_status 200 "list rules"
expect_eq "$(jlen .)" 0 "no rules yet"

api POST "$rules" "{\"category\":\"crash\",\"repository_id\":\"$mobile_repo\",\"labels\":[\" crash \",\"crash\",\"ios\"],\"assignees\":[\"octocat\"],\"milestone\":\"v2.4\"}"
expect_status 201 "rule by category"
crash_rule=$(jget .id)
expect_json .position 0 "first rule goes first"
expect_json .category crash "condition stored"
expect_json .severity null "unset condition matches any report"
expect_json .repository_id "$mobile_repo" "sends to the chosen repository"
expect_json '.labels | join(",")' "crash,ios" "labels trimmed, duplicates dropped"
expect_json '.assignees[0]' octocat "assignees stored"
expect_json .milestone v2.4 "milestone stored"

api POST "$rules" "{\"app_id\":\"$app_id\",\"severity\":\"critical\",\"repository_id\":\"$default_repo\",\"labels\":[\"urgent\"]}"
expect_status 201 "rule by app and severity"
critical_rule=$(jget .id)
expect_json .position 1 "a new rule goes after the last one"
expect_json .app_id "$app_id" "app condition stored"
expect_json .milestone null "no milestone"

section "order"
api PUT "$rules/$critical_rule" "{\"position\":0,\"app_id\":\"$app_id\",\"severity\":\"critical\",\"repository_id\":\"$default_repo\",\"labels\":[\"urgent\"]}"
expect_status 200 "move the critical rule first"
api PUT "$rules/$crash_rule" "{\"position\":5,\"category\":\"crash\",\"repository_id\":\"$mobile_repo\",\"labels\":[\"crash\"]}"
expect_status 200 "replace the crash rule"
expect_json '.assignees | length' 0 "replace drops what the new body leaves out"
api GET "$rules"
expect_json '.[0].id' "$critical_rule" "rules listed in the order they are tried"
expect_json '.[1].id' "$crash_rule" "…the crash rule second"
api PUT "$rules/$crash_rule" "{\"category\":\"crash\",\"repository_id\":\"$mobile_repo\"}"
expect_status 200 "replace without a position"
expect_json .position 5 "keeps its place"

section "validation"
api POST "$rules" '{"category":"crash"}'
expect_status 422 "repository_id is required"
api POST "$rules" "{\"severity\":\"loud\",\"repository_id\":\"$mobile_repo\"}"
expect_status 422 "unknown severity"
api POST "$rules" "{\"category\":\"perf\",\"repository_id\":\"$mobile_repo\"}"
expect_status 422 "unknown category"
api POST "$rules" "{\"position\":-1,\"repository_id\":\"$mobile_repo\"}"
expect_status 422 "negative position"
api POST "$rules" "{\"repository_id\":\"$mobile_repo\",\"assignees\":[\"@octocat\"]}"
expect_status 422 "assignees are usernames, without @"
labels=$(printf '"l%d",' $(seq 1 21))
api POST "$rules" "{\"repository_id\":\"$mobile_repo\",\"labels\":[${labels%,}]}"
expect_status 422 "too many labels"
api POST "$rules" "{\"repository_id\":\"$mobile_repo\",\"labels\":[\"$(printf 'x%.0s' $(seq 1 51))\"]}"
expect_status 422 "label too long"
api POST "$rules" "{\"repository_id\":\"$foreign_repo\"}"
expect_status 404 "a repository of another project"
api POST "$rules" '{"repository_id":"7c6a3f2e-0b1d-4e5f-9a8b-1c2d3e4f5a6b"}'
expect_status 404 "an unknown repository"
api POST "$rules" "{\"app_id\":\"00000000-0000-0000-0000-000000000000\",\"repository_id\":\"$mobile_repo\"}"
expect_status 404 "an unknown app"
api POST "$rules" "{\"repository_id\":\"$mobile_repo\",\"template\":\"x\"}"
expect_status 422 "unknown field is rejected"
api PUT "$rules/00000000-0000-0000-0000-000000000000" "{\"repository_id\":\"$mobile_repo\"}"
expect_status 404 "replace an unknown rule"
api GET "/projects/00000000-0000-0000-0000-000000000000/routing-rules"
expect_status 404 "rules of an unknown project"

section "repository guards"
api DELETE "/projects/$project_id/repositories/$mobile_repo"
expect_status 409 "a repository rules send to can't be unlinked"
api DELETE "/projects/$project_id/repositories/$default_repo"
expect_status 409 "the default can't be unlinked while another repository is linked"
api GET "$rules"
expect_eq "$(jlen .)" 2 "rules untouched"

section "owner isolation"
new_owner
http GET "$rules" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot list them"
http POST "$rules" "{\"repository_id\":\"$mobile_repo\"}" "$NEW_TOKEN"
expect_status 404 "another owner cannot add one"
http PUT "$rules/$crash_rule" "{\"repository_id\":\"$mobile_repo\"}" "$NEW_TOKEN"
expect_status 404 "another owner cannot replace one"
http DELETE "$rules/$crash_rule" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot delete one"
http POST /projects "{\"name\":\"Theirs $run\"}" "$NEW_TOKEN"
their_project=$(jget .id)
http POST "/projects/$their_project/repositories" "{\"tracker\":\"github\",\"repository\":\"them/repo-$run\"}" "$NEW_TOKEN"
their_repo=$(jget .id)
http POST "/projects/$their_project/routing-rules" "{\"app_id\":\"$app_id\",\"repository_id\":\"$their_repo\"}" "$NEW_TOKEN"
expect_status 404 "another owner cannot write a rule for our app"
http DELETE /owners/me "" "$NEW_TOKEN"

section "delete"
api DELETE "$rules/$crash_rule"
expect_status 204 "delete a rule"
api DELETE "$rules/$crash_rule"
expect_status 404 "deleting twice -> 404"
api DELETE "/projects/$project_id/repositories/$mobile_repo"
expect_status 204 "the repository can be unlinked once no rule uses it"
api DELETE "/apps/$app_id"
expect_status 204 "delete the app"
api GET "$rules"
expect_eq "$(jlen .)" 0 "the app's rules go with it"

api DELETE "/projects/$project_id"
expect_status 204 "delete the project with its rules and repositories"
api DELETE "/projects/$other_project"

summary
