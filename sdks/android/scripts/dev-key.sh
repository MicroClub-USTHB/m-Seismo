#!/usr/bin/env bash
# Prints an SDK key for the example app, from the local dev stack (make dev-up).
# Sets up owner -> fake GitHub connection -> project with a default repository -> android app -> key,
# so reports sent with the key become issues on the fake GitHub.
source "$(dirname "${BASH_SOURCE[0]}")/../../../backend/scripts/api/lib.sh"

{
  wait_for_server
  ensure_token
  ensure_github_connection || exit 1
  run=$(rand)
  create_project "Android example $run" || exit 1
  project=$CREATED_ID
  link_repo "$project" "$GITHUB_TEST_REPO" true || exit 1
  api POST /apps "{\"name\":\"Android example $run\",\"platform\":\"android\",\"bundle_id\":\"org.microclub.mseismo.example\"}"
  [[ $STATUS == 201 ]] || { fail "create app"; exit 1; }
  app=$(jget .id)
  api PUT "/apps/$app/projects/$project"
  [[ $STATUS == 204 ]] || { fail "link app to project"; exit 1; }
  api POST "/apps/$app/keys"
  [[ $STATUS == 201 ]] || { fail "create key"; exit 1; }
} >&2

jget .secret
