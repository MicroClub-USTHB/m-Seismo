#!/usr/bin/env bash
# Providers and app connections: the per-app GitHub connect flow (state
# handling, forged-installation protection), each app's own connections,
# sharing an installation between apps of one owner (attach), never between
# owners, repository listing, and linking repositories to projects
# (tracker + name, defaults, isolation).
#
#   GITHUB_TEST_REPO=me/repo backend/scripts/api/providers.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token
require_github_repo || { summary; exit 0; }

run=$(rand)
create_app "Connections A $run"
app_a=$CREATED_ID
create_app "Connections B $run"
app_b=$CREATED_ID

section "provider list"
api GET /providers
expect_status 200 "GET /providers"
expect_match "$(jget .providers)" 'github' "github is configured"
expect_match "$(jget .providers)" 'gitlab' "gitlab is configured"
expect_match "$(jget .providers)" 'bitbucket' "bitbucket is configured"
api GET "/apps/$app_a/connect/jira"
expect_status 404 "unknown tracker -> 404"
http GET "/apps/$app_a/connect/github"
expect_status 401 "connect requires a bearer token"
api GET /apps/00000000-0000-0000-0000-000000000000/connect/github
expect_status 404 "connect for an unknown app -> 404"

section "old owner-level routes are gone"
api GET /providers/github/connect
expect_status 404 "GET /providers/github/connect -> 404"
api GET /providers/connections/00000000-0000-0000-0000-000000000000/repositories
expect_status 404 "GET /providers/connections/{id}/repositories -> 404"
api DELETE /providers/connections/00000000-0000-0000-0000-000000000000
expect_status 404 "DELETE /providers/connections/{id} -> 404"

section "connect flow: rejected callbacks"
http GET "/providers/github/callback?installation_id=1&code=fake-code-1&state=made-up"
expect_status 302 "callback always redirects"
location=$(header Location)
expect_eq "$(query_param "$location" status)" error "unknown state -> status=error"
expect_match "$location" 'reason=unknown\+or\+expired\+state' "reason explains the state problem"
expect_match "$location" '/apps\?' "unknown state -> back to the app list"

if require_fake "forged installation and replay checks"; then
  api GET "/apps/$app_a/connect/github"
  expect_status 200 "start connect for app A"
  url=$(jget .url)
  expect_match "$url" '/apps/[^/]+/installations/new\?state=' "install URL points at the GitHub App"
  state=$(query_param "$url" state)

  # The user's OAuth code proves access to installation 1001 only.
  http GET "/providers/github/callback?installation_id=999999&setup_action=install&code=fake-code-1001&state=$state"
  location=$(header Location)
  expect_eq "$(query_param "$location" status)" error "forged installation_id is rejected"
  expect_match "$location" 'cannot\+access\+this\+installation' "reason: user cannot access it"
  expect_match "$location" "/apps/$app_a/connections\\?" "error goes back to app A's connections page"

  http GET "/providers/github/callback?installation_id=1001&setup_action=install&code=fake-code-1001&state=$state"
  location=$(header Location)
  expect_match "$location" 'unknown\+or\+expired\+state' "a state works only once"

  api GET "/apps/$app_a/connect/github"
  state=$(query_param "$(jget .url)" state)
  http GET "/providers/github/callback?installation_id=1001&setup_action=install&state=$state"
  location=$(header Location)
  expect_match "$location" 'missing\+authorization\+code' "callback without an OAuth code is rejected"

  api GET "/apps/$app_a/connect/github"
  state=$(query_param "$(jget .url)" state)
  http GET "/providers/github/callback?setup_action=request&state=$state"
  location=$(header Location)
  expect_match "$location" 'organization\+owner\+must\+approve' "pending org approval is explained"

  api GET /apps/$app_a/connections
  expect_eq "$(jlen .)" 0 "no rejected callback created a connection"
fi

section "connect flow: success"
if [[ $GITHUB_MODE == fake ]]; then
  install_a=$(fake_install_id)
  github_connect "$app_a" "$install_a" && pass "connected app A (connection $CONNECTION_ID)"
  location=$(header Location)
  expect_match "$location" "/apps/$app_a/connections\\?" "browser goes back to app A's connections page"
else
  github_connect "$app_a" && pass "connected app A (connection $CONNECTION_ID)"
fi
conn_a=$CONNECTION_ID
api GET "/apps/$app_a/connections"
expect_status 200 "list app A's connections"
expect_eq "$(jlen .)" 1 "app A has one connection"
expect_json .[0].id "$conn_a" "it is the new connection"
expect_json .[0].app_id "$app_a" "it belongs to app A"
expect_json .[0].tracker github "tracker github"

section "connections are per app"
api GET "/apps/$app_b/connections"
expect_status 200 "list app B's connections"
expect_eq "$(jlen .)" 0 "app B does not see app A's connection"
api GET "/apps/$app_b/connections/$conn_a/repositories"
expect_status 404 "app B cannot use app A's connection"
api DELETE "/apps/$app_b/connections/$conn_a"
expect_status 404 "app B cannot delete app A's connection"
api GET /providers/connections
expect_status 200 "owner-wide connection list"
expect_eq "$(jcount . id "$conn_a")" 1 "owner-wide list includes app A's connection"

section "repositories of a connection"
api GET "/apps/$app_a/connections/$conn_a/repositories"
expect_status 200 "list repositories"
expect_eq "$(jcount . full_name "$GITHUB_TEST_REPO")" 1 "$GITHUB_TEST_REPO is reachable"
api GET "/apps/$app_a/connections/00000000-0000-0000-0000-000000000000/repositories"
expect_status 404 "unknown connection -> 404"
if require_fake "an installation only lists its own account's repositories"; then
  expect_eq "$(jcount . full_name other-org/tools)" 0 "other-org/tools is not listed for an octo-org installation"
fi

section "sharing an installation between apps"
attach_connection "$app_b" "$conn_a" && pass "attach app A's connection to app B"
conn_b=$CREATED_ID
expect_json .app_id "$app_b" "attached connection belongs to app B"
expect_ne "$conn_b" "$conn_a" "app B gets its own connection row"
api GET "/apps/$app_b/connections"
expect_eq "$(jcount . id "$conn_b")" 1 "app B lists it"
api POST "/apps/$app_b/connections" "{\"connection_id\":\"$conn_a\"}"
expect_status 201 "attaching again is idempotent"
expect_json .id "$conn_b" "same row as before"
api POST "/apps/$app_b/connections" '{"connection_id":"00000000-0000-0000-0000-000000000000"}'
expect_status 404 "attach an unknown connection -> 404"
api POST "/apps/$app_b/connections" '{"connection":"x"}'
expect_status 422 "unknown field -> 422"
if require_fake "connecting the same installation through the flow"; then
  github_connect "$app_b" "$install_a" && pass "the connect flow on app B for the same installation succeeds"
  expect_eq "$CONNECTION_ID" "$conn_b" "and reuses app B's row"
fi

section "linking repositories to a project (no credentials)"
create_project "Repos $run"
project_id=$CREATED_ID

api POST "/projects/$project_id/repositories" \
  "{\"tracker\":\"github\",\"repository\":\"$GITHUB_TEST_REPO\",\"is_default\":false}"
expect_status 201 "link $GITHUB_TEST_REPO"
repo1=$(jget .id)
expect_json .is_default true "first repository becomes the default anyway"
expect_json .tracker github "tracker stored"
expect_json .connection_id null "no connection on a project repository"

api POST "/projects/$project_id/repositories" \
  "{\"tracker\":\"github\",\"repository\":\"$GITHUB_TEST_REPO\"}"
expect_status 409 "linking the same repository twice -> 409"
api POST "/projects/$project_id/repositories" \
  "{\"tracker\":\"github\",\"repository\":\"no-slash\"}"
expect_status 422 "malformed repository name -> 422"
api POST "/projects/$project_id/repositories" \
  "{\"repository\":\"$GITHUB_TEST_REPO\"}"
expect_status 422 "missing tracker -> 422"
api POST "/projects/$project_id/repositories" \
  "{\"tracker\":\"jira\",\"repository\":\"$GITHUB_TEST_REPO\"}"
expect_status 404 "unknown tracker -> 404"
api POST "/projects/$project_id/repositories" \
  "{\"connection_id\":\"$conn_a\",\"repository\":\"$GITHUB_TEST_REPO\"}"
expect_status 422 "the old connection_id field is rejected"

if require_fake "default switching between two repositories"; then
  link_repo "$project_id" octo-org/another false
  repo2=$CREATED_ID
  expect_json .is_default false "second repository is not the default"

  api PUT "/projects/$project_id/repositories/$repo2/default"
  expect_status 204 "make the second repository the default"
  api GET "/projects/$project_id/repositories"
  expect_eq "$(jlen .)" 2 "two repositories listed"
  expect_eq "$(jcount . is_default true)" 1 "exactly one default"
  expect_eq "$(jcount . id "$repo2")" 1 "second repository listed"
  for i in 0 1; do
    [[ $(jget ".[$i].id") == "$repo2" ]] && expect_json ".[$i].is_default" true "default moved to the second repository"
  done
  api PUT "/projects/$project_id/repositories/00000000-0000-0000-0000-000000000000/default"
  expect_status 404 "default to an unknown repository -> 404"
  api GET "/projects/$project_id/repositories"
  expect_eq "$(jcount . is_default true)" 1 "failed default switch rolled back"

  api DELETE "/projects/$project_id/repositories/$repo2"
  expect_status 409 "the default can't be unlinked while another repository is linked"
  api PUT "/projects/$project_id/repositories/$repo1/default"
  expect_status 204 "make the first repository the default again"
  api DELETE "/projects/$project_id/repositories/$repo2"
  expect_status 204 "unlink the second repository"
fi

section "owner isolation"
new_owner
http GET "/apps/$app_a/connections" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot list our app's connections"
http GET "/apps/$app_a/connections/$conn_a/repositories" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot use our connection"
http DELETE "/apps/$app_a/connections/$conn_a" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot delete our connection"
http GET "/apps/$app_a/connect/github" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot start a connect flow for our app"
http GET /providers/connections "" "$NEW_TOKEN"
expect_eq "$(jlen .)" 0 "another owner has no connections"
http POST /apps "{\"name\":\"Theirs\",\"platform\":\"ios\",\"bundle_id\":\"com.example.theirs$run\"}" "$NEW_TOKEN"
their_app=$(jget .id)
http POST "/apps/$their_app/connections" "{\"connection_id\":\"$conn_a\"}" "$NEW_TOKEN"
expect_status 404 "another owner cannot attach our connection"
if require_fake "one installation never spans two owners"; then
  http GET "/apps/$their_app/connect/github" "" "$NEW_TOKEN"
  state=$(query_param "$(jget .url)" state)
  http GET "/providers/github/callback?installation_id=$install_a&setup_action=install&code=fake-code-$install_a&state=$state"
  location=$(header Location)
  expect_eq "$(query_param "$location" status)" error "another owner connecting our installation is rejected"
  expect_match "$location" 'already\+connected\+by\+another\+owner' "reason: held by another owner"
  expect_match "$location" "/apps/$their_app/connections\\?" "back to their app's page"
fi
http DELETE /owners/me "" "$NEW_TOKEN"

section "disconnect"
api DELETE "/apps/$app_b/connections/$conn_b"
expect_status 204 "disconnect app B"
api GET "/apps/$app_b/connections"
expect_eq "$(jlen .)" 0 "app B's connection is gone"
api GET "/apps/$app_a/connections"
expect_eq "$(jcount . id "$conn_a")" 1 "app A keeps its connection to the same installation"
api GET "/projects/$project_id/repositories"
expect_eq "$(jcount . id "$repo1")" 1 "project repositories are untouched"
api DELETE "/apps/$app_b/connections/$conn_b"
expect_status 404 "disconnecting twice -> 404"

section "deleting an app removes its connections"
api DELETE "/apps/$app_a"
expect_status 204 "delete app A"
api GET /providers/connections
expect_eq "$(jcount . id "$conn_a")" 0 "app A's connection is gone with it"

section "unlink"
api DELETE "/projects/$project_id/repositories/$repo1"
expect_status 204 "unlink repository"
api DELETE "/projects/$project_id/repositories/$repo1"
expect_status 404 "unlinking twice -> 404"
api DELETE "/projects/$project_id"
api DELETE "/apps/$app_b"

summary
