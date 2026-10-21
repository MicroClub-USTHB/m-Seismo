#!/usr/bin/env bash
# Apps: CRUD, SDK keys, app <-> project links and owner isolation.
#
#   backend/scripts/api/apps.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token

run=$(rand)

section "create"
api POST /apps '{"name":"Bad","platform":"windows","bundle_id":"x"}'
expect_status 422 "unknown platform is rejected"
api POST /apps '{"name":"Bad","platform":"ios","bundle_id":""}'
expect_status 422 "empty bundle_id is rejected"
api POST /apps '{"name":"","platform":"ios","bundle_id":"x"}'
expect_status 422 "empty name is rejected"

api POST /apps "{\"name\":\"Shop iOS $run\",\"platform\":\"ios\",\"bundle_id\":\"com.example.shop$run\"}"
expect_status 201 "create app"
app_id=$(jget .id)
expect_json .platform ios "platform stored"
expect_json .bundle_id "com.example.shop$run" "bundle_id stored"

section "read and update"
api GET /apps
expect_status 200 "list apps"
expect_eq "$(jcount . id "$app_id")" 1 "list contains the app"
api GET "/apps/$app_id"
expect_status 200 "get app"
api PATCH "/apps/$app_id" "{\"name\":\"Shop Android $run\",\"platform\":\"android\"}"
expect_status 200 "update name and platform"
expect_json .platform android "platform updated"
expect_json .bundle_id "com.example.shop$run" "bundle_id unchanged"
api PATCH "/apps/$app_id" '{"platform":"symbian"}'
expect_status 422 "invalid platform on update is rejected"

section "SDK keys"
api GET "/apps/$app_id/keys"
expect_status 200 "list keys"
expect_eq "$(jlen .)" 0 "no keys yet"

api POST "/apps/$app_id/keys"
expect_status 201 "create key"
key1_id=$(jget .id)
key1_secret=$(jget .secret)
expect_match "$key1_secret" '^msk_[A-Za-z0-9_-]{43}$' "secret has the msk_ format"
expect_eq "${key1_secret:0:12}" "$(jget .prefix)" "prefix is the start of the secret"

api POST "/apps/$app_id/keys"
expect_status 201 "create a second key"
key2_id=$(jget .id)

api GET "/apps/$app_id/keys"
expect_eq "$(jlen .)" 2 "two keys listed"
expect_json '.[0].secret' null "secrets are never listed"

api DELETE "/apps/$app_id/keys/$key1_id"
expect_status 204 "revoke the first key"
api DELETE "/apps/$app_id/keys/$key1_id"
expect_status 404 "revoking twice -> 404"
api GET "/apps/$app_id/keys"
key1_revoked="" key2_revoked=""
for i in 0 1; do
  [[ $(jget ".[$i].id") == "$key1_id" ]] && key1_revoked=$(jget ".[$i].revoked_at")
  [[ $(jget ".[$i].id") == "$key2_id" ]] && key2_revoked=$(jget ".[$i].revoked_at")
done
expect_match "$key1_revoked" '^[0-9]{4}-' "first key has a revoked_at timestamp"
expect_eq "$key2_revoked" null "second key is still active"

section "project links"
create_project "Linked $run"
project_id=$CREATED_ID

api GET "/apps/$app_id/projects"
expect_eq "$(jlen .)" 0 "no linked projects yet"
api PUT "/apps/$app_id/projects/$project_id"
expect_status 204 "link app to project"
api PUT "/apps/$app_id/projects/$project_id"
expect_status 204 "linking again is idempotent"
api GET "/apps/$app_id/projects"
expect_eq "$(jlen .)" 1 "one linked project"
expect_json '.[0].id' "$project_id" "it is the right project"
api PUT "/apps/$app_id/projects/00000000-0000-0000-0000-000000000000"
expect_status 404 "linking an unknown project -> 404"

api POST "/apps/$app_id/test-issue"
expect_status 422 "test issue with no default repository -> 422"

section "owner isolation"
new_owner
http GET "/apps/$app_id" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot read the app"
http POST "/apps/$app_id/keys" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot create keys"
http DELETE "/apps/$app_id/keys/$key2_id" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot revoke keys"
http POST /apps "{\"name\":\"Theirs\",\"platform\":\"flutter\",\"bundle_id\":\"com.other\"}" "$NEW_TOKEN"
their_app=$(jget .id)
http PUT "/apps/$their_app/projects/$project_id" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot link our project to their app"
api PUT "/apps/$app_id/projects/$their_app"
expect_status 404 "an app id is not a project id"
http DELETE /owners/me "" "$NEW_TOKEN"

section "unlink and delete"
api DELETE "/apps/$app_id/projects/$project_id"
expect_status 204 "unlink"
api DELETE "/apps/$app_id/projects/$project_id"
expect_status 404 "unlinking twice -> 404"
api DELETE "/apps/$app_id"
expect_status 204 "delete app"
api GET "/apps/$app_id"
expect_status 404 "deleted app is gone"
api GET "/apps/$app_id/keys"
expect_status 404 "its keys are gone too"
api DELETE "/projects/$project_id"

summary
