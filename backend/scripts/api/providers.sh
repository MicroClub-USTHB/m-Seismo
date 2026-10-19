#!/usr/bin/env bash
# Providers: the GitHub connect flow (state handling, forged-installation
# protection), connections, repository listing, and linking repositories
# to projects (defaults, reachability, isolation).
#
#   backend/scripts/api/providers.sh                       # fake GitHub
#   GITHUB_MODE=real GITHUB_TEST_REPO=me/repo backend/scripts/api/providers.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token

run=$(rand)

section "provider list"
api GET /providers
expect_status 200 "GET /providers"
expect_match "$(jget .providers)" 'github' "github is configured"
api GET /providers/gitlab/connect
expect_status 404 "unconfigured tracker -> 404"
http GET /providers/github/connect
expect_status 401 "connect requires a bearer token"

section "connect flow: rejected callbacks"
http GET "/providers/github/callback?installation_id=1&code=fake-code-1&state=made-up"
expect_status 302 "callback always redirects"
location=$(header Location)
expect_eq "$(query_param "$location" status)" error "unknown state -> status=error"
expect_match "$location" 'reason=unknown\+or\+expired\+state' "reason explains the state problem"

if require_fake "forged installation and replay checks"; then
  api GET /providers/github/connect
  expect_status 200 "start connect"
  url=$(jget .url)
  expect_match "$url" '/apps/[^/]+/installations/new\?state=' "install URL points at the GitHub App"
  state=$(query_param "$url" state)

  # The user's OAuth code proves access to installation 1001 only.
  http GET "/providers/github/callback?installation_id=999999&setup_action=install&code=fake-code-1001&state=$state"
  location=$(header Location)
  expect_eq "$(query_param "$location" status)" error "forged installation_id is rejected"
  expect_match "$location" 'cannot\+access\+this\+installation' "reason: user cannot access it"

  http GET "/providers/github/callback?installation_id=1001&setup_action=install&code=fake-code-1001&state=$state"
  location=$(header Location)
  expect_match "$location" 'unknown\+or\+expired\+state' "a state works only once"

  api GET /providers/github/connect
  state=$(query_param "$(jget .url)" state)
  http GET "/providers/github/callback?installation_id=1001&setup_action=install&state=$state"
  location=$(header Location)
  expect_match "$location" 'missing\+authorization\+code' "callback without an OAuth code is rejected"

  api GET /providers/github/connect
  state=$(query_param "$(jget .url)" state)
  http GET "/providers/github/callback?setup_action=request&state=$state"
  location=$(header Location)
  expect_match "$location" 'organization\+owner\+must\+approve' "pending org approval is explained"
fi

section "connect flow: success"
github_connect && pass "connected GitHub (connection $CONNECTION_ID)"
api GET /providers/connections
expect_status 200 "list connections"
expect_eq "$(jcount . id "$CONNECTION_ID")" 1 "new connection is listed"
expect_eq "$(jcount . tracker github)" "$(jlen .)" "every connection is a github one"

section "repositories of the connection"
api GET "/providers/connections/$CONNECTION_ID/repositories"
expect_status 200 "list repositories"
expect_eq "$(jcount . full_name "$GITHUB_TEST_REPO")" 1 "$GITHUB_TEST_REPO is reachable"
api GET /providers/connections/00000000-0000-0000-0000-000000000000/repositories
expect_status 404 "unknown connection -> 404"

section "link repositories to a project"
create_project "Repos $run"
project_id=$CREATED_ID

api POST "/projects/$project_id/repositories" \
  "{\"connection_id\":\"$CONNECTION_ID\",\"repository\":\"$GITHUB_TEST_REPO\",\"is_default\":false}"
expect_status 201 "link $GITHUB_TEST_REPO"
repo1=$(jget .id)
expect_json .is_default true "first repository becomes the default anyway"
expect_json .tracker github "tracker comes from the connection"

api POST "/projects/$project_id/repositories" \
  "{\"connection_id\":\"$CONNECTION_ID\",\"repository\":\"$GITHUB_TEST_REPO\"}"
expect_status 409 "linking the same repository twice -> 409"
api POST "/projects/$project_id/repositories" \
  "{\"connection_id\":\"$CONNECTION_ID\",\"repository\":\"octo-org/does-not-exist-$run\"}"
expect_status 422 "unreachable repository -> 422"
api POST "/projects/$project_id/repositories" \
  "{\"connection_id\":\"$CONNECTION_ID\",\"repository\":\"no-slash\"}"
expect_status 422 "malformed repository name -> 422"
api POST "/projects/$project_id/repositories" \
  "{\"connection_id\":\"00000000-0000-0000-0000-000000000000\",\"repository\":\"$GITHUB_TEST_REPO\"}"
expect_status 404 "unknown connection -> 404"

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
  expect_status 204 "unlink the second repository"
fi

section "owner isolation"
new_owner
http GET "/providers/connections/$CONNECTION_ID/repositories" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot use our connection"
http DELETE "/providers/connections/$CONNECTION_ID" "" "$NEW_TOKEN"
expect_status 404 "another owner cannot delete our connection"
http GET /providers/connections "" "$NEW_TOKEN"
expect_eq "$(jlen .)" 0 "another owner has no connections"
http POST /projects '{"name":"Theirs"}' "$NEW_TOKEN"
their_project=$(jget .id)
http POST "/projects/$their_project/repositories" \
  "{\"connection_id\":\"$CONNECTION_ID\",\"repository\":\"$GITHUB_TEST_REPO\"}" "$NEW_TOKEN"
expect_status 404 "another owner cannot link through our connection"
http DELETE /owners/me "" "$NEW_TOKEN"

section "unlink"
api DELETE "/projects/$project_id/repositories/$repo1"
expect_status 204 "unlink repository"
api DELETE "/projects/$project_id/repositories/$repo1"
expect_status 404 "unlinking twice -> 404"
api DELETE "/projects/$project_id"

if require_fake "disconnect"; then
  section "disconnect"
  saved_connection=$CONNECTION_ID
  github_connect
  throwaway=$CONNECTION_ID
  save_var CONNECTION_ID "$saved_connection"
  api DELETE "/providers/connections/$throwaway"
  expect_status 204 "disconnect"
  api GET /providers/connections
  expect_eq "$(jcount . id "$throwaway")" 0 "disconnected connection is gone"
  api DELETE "/providers/connections/$throwaway"
  expect_status 404 "disconnecting twice -> 404"
fi

summary
