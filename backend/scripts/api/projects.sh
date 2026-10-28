#!/usr/bin/env bash
# Projects: CRUD, slug rules and owner isolation.
#
#   backend/scripts/api/projects.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token

run=$(rand)

section "create"
api POST /projects "{\"name\":\"API Test Project $run!\"}"
expect_status 201 "create with a derived slug"
project_id=$(jget .id)
expect_json .slug "api-test-project-$run" "slug derived from the name"
expect_json .name "API Test Project $run!" "name stored as given"

api POST /projects "{\"name\":\"Explicit $run\",\"slug\":\"explicit-$run\"}"
expect_status 201 "create with an explicit slug"
other_id=$(jget .id)
expect_json .slug "explicit-$run" "explicit slug kept"

api POST /projects "{\"name\":\"Dup\",\"slug\":\"explicit-$run\"}"
expect_status 409 "duplicate slug is a conflict"
api POST /projects '{"name":"Bad","slug":"Bad Slug"}'
expect_status 422 "invalid slug is rejected"
api POST /projects '{"name":""}'
expect_status 422 "empty name is rejected"
api POST /projects '{"title":"x"}'
expect_status 422 "unknown field is rejected"

section "read"
api GET /projects
expect_status 200 "list projects"
expect_eq "$(jcount . id "$project_id")" 1 "list contains the new project"
api GET "/projects/$project_id"
expect_status 200 "get by id"
expect_json .id "$project_id" "returns the right project"
api GET /projects/00000000-0000-0000-0000-000000000000
expect_status 404 "unknown id -> 404"
api GET /projects/not-a-uuid
expect_status 404 "malformed id -> 404"

section "update"
api PATCH "/projects/$project_id" "{\"name\":\"Renamed $run\"}"
expect_status 200 "rename"
expect_json .name "Renamed $run" "name updated"
expect_json .slug "api-test-project-$run" "slug unchanged by rename"
api PATCH "/projects/$project_id" "{\"slug\":\"explicit-$run\"}"
expect_status 409 "slug taken by another project"
api PATCH "/projects/$project_id" "{\"slug\":\"renamed-$run\"}"
expect_status 200 "change slug"
expect_json .slug "renamed-$run" "slug updated"
api PATCH "/projects/$project_id" '{"slug":"-bad-"}'
expect_status 422 "invalid new slug is rejected"

section "repositories (empty)"
api GET "/projects/$project_id/repositories"
expect_status 200 "list repositories"
expect_eq "$(jlen .)" 0 "no repositories yet"

section "owner isolation"
new_owner
http GET "/projects/$project_id" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot read it"
http PATCH "/projects/$project_id" '{"name":"hijack"}' "$NEW_TOKEN"
expect_status 404 "another owner cannot update it"
http DELETE "/projects/$project_id" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot delete it"
http GET "/projects/$project_id/repositories" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot list its repositories"
http GET /projects "" "$NEW_TOKEN"
expect_eq "$(jlen .)" 0 "another owner's list is empty"
http POST /projects "{\"name\":\"Theirs\",\"slug\":\"explicit-$run\"}" "$NEW_TOKEN"
expect_status 201 "slugs are unique per owner, not globally"
http DELETE /owners/me "" "$NEW_TOKEN"

section "delete"
api DELETE "/projects/$project_id"
expect_status 204 "delete"
api GET "/projects/$project_id"
expect_status 404 "deleted project is gone"
api DELETE "/projects/$project_id"
expect_status 404 "deleting twice -> 404"
api DELETE "/projects/$other_id"
expect_status 204 "clean up the second project"

summary
