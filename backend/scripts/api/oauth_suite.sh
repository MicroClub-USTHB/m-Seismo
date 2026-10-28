#!/usr/bin/env bash
# Shared checks for trackers reached with per-app OAuth tokens (GitLab,
# Bitbucket). Sourced by gitlab.sh and bitbucket.sh after lib.sh; not a
# test script on its own.
#
# The caller sets:
#   TRACKER        gitlab | bitbucket
#   AUTH_PATH      authorize path on the tracker's web host (regex-safe)
#   LOGIN_PREFIX   account login of fake user N, minus N (gl-user-)
#   REPO_OK        a repository that accepts issues
#   REPO_BROKEN    issue creation answers 500
#   REPO_LIMITED   first create per title answers 429, Retry-After 1
#   REPO_EXHAUSTED issue creation answers 429, Retry-After 3600
#   REPO_COUNT     how many repositories every fake user sees
#   AUTHOR_FIELD   field of the fake's issues naming who opened it
# and defines:
#   fake_author N        prints AUTHOR_FIELD's value for fake user N
#   tracker_specifics    optional; runs extra checks with $app_b, $conn_b,
#                        $project and $run before the cleanup

# run_oauth_tracker_suite runs every shared check. Its variables are global
# so tracker_specifics can use them.
run_oauth_tracker_suite() {
  run=$(rand)

  section "$TRACKER provider"
  api GET /providers
  expect_match "$(jget .providers)" "$TRACKER" "$TRACKER is configured"
  if ! fake_tracker_up "$TRACKER"; then
    skip "$TRACKER checks (the fake $TRACKER is not reachable at $(fake_tracker_url "$TRACKER"))"
    return 0
  fi

  create_app "$TRACKER A $run"
  app_a=$CREATED_ID
  create_app "$TRACKER B $run"
  app_b=$CREATED_ID

  section "connect flow: the authorize URL"
  start_connect "$app_a" "$TRACKER"
  expect_status 200 "GET /apps/{id}/connect/$TRACKER"
  expect_match "$INSTALL_URL" "/$AUTH_PATH\\?" "points at the $TRACKER authorize page"
  expect_match "$INSTALL_URL" 'client_id=fake-client' "carries the OAuth client ID"
  expect_match "$INSTALL_URL" 'response_type=code' "asks for an authorization code"
  expect_match "$INSTALL_URL" "redirect_uri=[^&]*%2Fproviders%2F$TRACKER%2Fcallback" "returns to the $TRACKER callback"
  expect_match "$STATE" '^[A-Za-z0-9_-]{40,}$' "carries a random one-time state"

  section "connect flow: rejected callbacks"
  tracker_callback "$TRACKER" "error=access_denied&error_description=The+user+denied+access&state=$STATE"
  expect_status 302 "the callback always redirects"
  expect_eq "$(query_param "$LOCATION" status)" error "denied → status=error"
  expect_match "$LOCATION" 'not\+granted' "reason: authorization was not granted"
  expect_match "$LOCATION" "/apps/$app_a/connections\\?" "back to app A's page"
  tracker_callback "$TRACKER" "code=fake-code-1&state=$STATE"
  expect_match "$LOCATION" 'unknown\+or\+expired\+state' "a state works once"
  start_connect "$app_a" "$TRACKER"
  tracker_callback "$TRACKER" "code=stolen-code&state=$STATE"
  expect_match "$LOCATION" 'rejected\+the\+authorization\+code' "a code $TRACKER rejects is refused"
  start_connect "$app_a" "$TRACKER"
  tracker_callback "$TRACKER" "state=$STATE"
  expect_match "$LOCATION" 'missing\+authorization\+code' "a callback without a code is refused"
  start_connect "$app_a" "github"
  tracker_callback "$TRACKER" "code=fake-code-1&state=$STATE"
  expect_match "$LOCATION" 'does\+not\+match\+this\+provider' "a state issued for another tracker is refused"
  api GET "/apps/$app_a/connections"
  expect_eq "$(jlen .)" 0 "no rejected callback created a connection"

  section "connect flow: success"
  user_a=$(fake_install_id)
  before=$(fake_tracker_stat "$TRACKER" exchanges)
  tracker_connect "$app_a" "$TRACKER" "fake-code-$user_a" && pass "app A authorised $TRACKER as user $user_a"
  conn_a=$CONNECTION_ID
  expect_match "$LOCATION" "/apps/$app_a/connections\\?" "the browser goes back to app A"
  expect_eq "$(fake_tracker_stat "$TRACKER" exchanges)" "$((before + 1))" "the code was exchanged once"
  api GET "/apps/$app_a/connections"
  expect_eq "$(jcount . id "$conn_a")" 1 "app A lists the connection"
  expect_json '.[0].tracker' "$TRACKER" "tracker $TRACKER"
  expect_json '.[0].account_login' "$LOGIN_PREFIX$user_a" "the account is the authorising user"
  expect_json '.[0].app_id' "$app_a" "it belongs to app A"
  expect_eq "$(printf '%s' "$BODY" | grep -c 'access_token\|refresh_token\|fake-code')" 0 "no token or code appears in the API"

  section "repositories of the connection"
  api GET "/apps/$app_a/connections/$conn_a/repositories"
  expect_status 200 "list repositories"
  expect_eq "$(jlen .)" "$REPO_COUNT" "every repository of the user, across pages"
  expect_eq "$(jcount . full_name "$REPO_OK")" 1 "$REPO_OK is listed"

  section "the OAuth authorization belongs to the app"
  api POST "/apps/$app_b/connections" "{\"connection_id\":\"$conn_a\"}"
  expect_status 422 "app A's $TRACKER connection cannot be attached to app B"
  expect_match "$(jget .error)" 'cannot be shared' "the error explains why"
  user_b=$(fake_install_id)
  tracker_connect "$app_b" "$TRACKER" "fake-code-$user_b" && pass "app B runs its own OAuth flow (user $user_b)"
  conn_b=$CONNECTION_ID
  tracker_connect "$app_b" "$TRACKER" "fake-code-$user_a" && pass "user $user_a may also authorise app B"
  expect_ne "$CONNECTION_ID" "$conn_a" "app B gets its own connection (and token) for that user"
  api GET "/apps/$app_b/connections"
  expect_eq "$(jlen .)" 2 "app B has two $TRACKER connections"
  api GET "/apps/$app_a/connections"
  expect_eq "$(jlen .)" 1 "app A still has exactly its own"

  section "issues open through each app's own token"
  create_project "$TRACKER project $run"
  project=$CREATED_ID
  link_repo "$project" "$REPO_OK" true "$TRACKER"
  expect_json .tracker "$TRACKER" "repository linked as a $TRACKER repository"
  api PUT "/apps/$app_a/projects/$project"
  api PUT "/apps/$app_b/projects/$project"
  api GET "/apps/$app_a/targets"
  expect_json '.[0].connection_id' "$conn_a" "app A's target uses app A's connection"
  api GET "/apps/$app_b/targets"
  expect_json '.[0].connection_id' "$conn_b" "app B's target uses app B's oldest $TRACKER connection"

  title_a="m-Seismo $TRACKER A $run"
  api POST "/apps/$app_a/test-issue" "{\"title\":\"$title_a\",\"body\":\"Opened by scripts/api run $run\"}"
  expect_status 201 "app A opened a $TRACKER issue"
  expect_json '.results[0].tracker' "$TRACKER" "on $TRACKER"
  expect_json '.results[0].connection_id' "$conn_a" "through app A's connection"
  expect_match "$(jget '.results[0].ref')" "^${REPO_OK}#[0-9]+$" "ref is repository#number"
  expect_match "$(jget '.results[0].url')" '^https?://.+/issues/[0-9]+$' "url points at the issue"
  expect_match "$(jget '.results[0].issue_id')" '^[0-9a-f-]{36}$' "recorded in m-Seismo"

  title_b="m-Seismo $TRACKER B $run"
  api POST "/apps/$app_b/test-issue" "{\"title\":\"$title_b\"}"
  expect_status 201 "app B opened a $TRACKER issue"
  expect_json '.results[0].connection_id' "$conn_b" "through app B's connection"

  fake_tracker_issues "$TRACKER"
  expect_eq "$(jcount . title "$title_a")" 1 "the fake $TRACKER has app A's issue"
  expect_eq "$(jcount . title "$title_b")" 1 "and app B's"
  expect_eq "$(jcount . "$AUTHOR_FIELD" "$(fake_author "$user_b")")" 1 "app B's issue was opened as app B's user, not app A's"

  section "token refresh"
  create_app "$TRACKER C $run"
  app_c=$CREATED_ID
  user_c=$(fake_install_id)
  tracker_connect "$app_c" "$TRACKER" "fake-code-$user_c-short" && pass "connected with a token that expires at once"
  conn_c=$CONNECTION_ID
  api PUT "/apps/$app_c/projects/$project"
  before=$(fake_tracker_stat "$TRACKER" refreshes)
  api POST "/apps/$app_c/test-issue" '{"title":"m-Seismo refresh 1"}'
  expect_status 201 "the expiring token was refreshed before use"
  expect_eq "$(fake_tracker_stat "$TRACKER" refreshes)" "$((before + 1))" "refreshed exactly once"
  api POST "/apps/$app_c/test-issue" '{"title":"m-Seismo refresh 2"}'
  expect_status 201 "the next issue opens too"
  expect_eq "$(fake_tracker_stat "$TRACKER" refreshes)" "$((before + 1))" "with the saved new token, no second refresh"

  section "revoked authorization"
  http POST "$(fake_tracker_url "$TRACKER")/_fake/revoke?user=$user_c"
  api POST "/apps/$app_c/test-issue" '{"title":"m-Seismo after revoke"}'
  expect_status 502 "a revoked authorization fails the issue"
  expect_match "$(jget '.results[0].error')" 'connect the app again' "and says to connect the app again"
  tracker_connect "$app_c" "$TRACKER" "fake-code-$user_c" && pass "reconnecting the app"
  expect_eq "$CONNECTION_ID" "$conn_c" "updates the same connection with a new token"
  api POST "/apps/$app_c/test-issue" '{"title":"m-Seismo after reconnect"}'
  expect_status 201 "issues open again"

  section "tracker failures and rate limits"
  create_app "$TRACKER D $run"
  app_d=$CREATED_ID
  tracker_connect "$app_d" "$TRACKER"
  create_project "$TRACKER broken $run"
  project_broken=$CREATED_ID
  link_repo "$project_broken" "$REPO_BROKEN" true "$TRACKER"
  api PUT "/apps/$app_d/projects/$project_broken"
  api POST "/apps/$app_d/test-issue" '{"title":"never opened"}'
  expect_status 502 "a $TRACKER server error gives 502"
  expect_match "$(jget '.results[0].error')" '500' "with the tracker's error"
  api DELETE "/apps/$app_d/projects/$project_broken"

  create_project "$TRACKER limited $run"
  project_limited=$CREATED_ID
  link_repo "$project_limited" "$REPO_LIMITED" true "$TRACKER"
  api PUT "/apps/$app_d/projects/$project_limited"
  before=$(fake_tracker_stat "$TRACKER" rate_limited)
  limited_title="m-Seismo $TRACKER limited $run"
  api POST "/apps/$app_d/test-issue" "{\"title\":\"$limited_title\"}"
  expect_status 201 "a short 429 is waited out"
  expect_eq "$(fake_tracker_stat "$TRACKER" rate_limited)" "$((before + 1))" "$TRACKER answered 429 once"
  fake_tracker_issues "$TRACKER"
  expect_eq "$(jcount . title "$limited_title")" 1 "and exactly one issue was opened"
  api DELETE "/apps/$app_d/projects/$project_limited"

  create_project "$TRACKER exhausted $run"
  project_exhausted=$CREATED_ID
  link_repo "$project_exhausted" "$REPO_EXHAUSTED" true "$TRACKER"
  api PUT "/apps/$app_d/projects/$project_exhausted"
  started=$SECONDS
  api POST "/apps/$app_d/test-issue" '{"title":"never opened (quota)"}'
  expect_status 429 "a long 429 fails fast with 429"
  expect_eq "$( (($(header Retry-After) > 3000)) && echo yes)" yes "Retry-After carries $TRACKER's wait"
  expect_match "$(jget '.results[0].error')" "$TRACKER .*rate limit" "the error names the $TRACKER rate limit"
  expect_eq "$( ((SECONDS - started < 10)) && echo yes)" yes "without waiting an hour"

  section "an app without a $TRACKER connection"
  create_app "$TRACKER E $run"
  app_e=$CREATED_ID
  api PUT "/apps/$app_e/projects/$project"
  api POST "/apps/$app_e/test-issue"
  expect_status 502 "cannot open $TRACKER issues"
  expect_match "$(jget '.results[0].error')" "no $TRACKER connection on this app covers $REPO_OK" "the error names the missing connection"

  section "another owner"
  new_owner
  http POST /apps "{\"name\":\"Theirs\",\"platform\":\"ios\",\"bundle_id\":\"com.example.theirs$run\"}" "$NEW_TOKEN"
  their_app=$(jget .id)
  http GET "/apps/$their_app/connect/$TRACKER" "" "$NEW_TOKEN"
  tracker_callback "$TRACKER" "code=fake-code-$user_a&state=$(query_param "$(jget .url)" state)"
  expect_match "$LOCATION" 'already\+connected\+by\+another\+owner' "a $TRACKER user already connected by us cannot be connected by another owner"
  http GET "/apps/$app_a/connections/$conn_a/repositories" "" "$NEW_TOKEN"
  expect_status 404 "another owner cannot use our connection"
  http DELETE /owners/me "" "$NEW_TOKEN"

  if declare -F tracker_specifics >/dev/null; then
    tracker_specifics
  fi

  section "disconnect"
  api DELETE "/apps/$app_a/connections/$conn_a"
  expect_status 204 "disconnect app A"
  api POST "/apps/$app_a/test-issue" '{"title":"never opened (disconnected)"}'
  expect_status 502 "app A can no longer open $TRACKER issues"
  api GET "/apps/$app_b/targets"
  expect_json '.[0].connection_id' "$conn_b" "app B's connection is untouched"

  section "cleanup"
  for a in "$app_a" "$app_b" "$app_c" "$app_d" "$app_e"; do api DELETE "/apps/$a"; done
  for pr in "$project" "$project_broken" "$project_limited" "$project_exhausted"; do api DELETE "/projects/$pr"; done
  api GET /providers/connections
  expect_eq "$(jcount . app_id "$app_b")" 0 "deleting the apps deleted their connections"
}
