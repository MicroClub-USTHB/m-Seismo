#!/usr/bin/env bash
# Shared helpers for the API test scripts. Source it; don't run it.
#
# Environment:
#   API_URL           backend API base           (default http://localhost:8080/api/v1)
#   GITHUB_MODE       real | fake                (default real; fake needs fake
#                     trackers, which the dev stack no longer has)
#   GITHUB_TEST_REPO  repository a real GitHub App can open issues in; the
#                     GitHub scripts skip themselves when it is empty
#   FAKE_GITHUB_URL   fake GitHub, fake mode only (default http://localhost:9090)
#   FAKE_GITLAB_URL   fake GitLab, fake mode only (default http://localhost:9091)
#   FAKE_BITBUCKET_URL fake Bitbucket, fake mode only (default http://localhost:9092)
#   STATE_FILE        where the token and IDs are saved (default scripts/api/.env.test)
#   VERBOSE=1         print every request and response
#
# State: the owner's credentials, bearer token, a test app and that app's
# GitHub connection are saved to STATE_FILE and re-read before every
# authenticated request, so each script (and each run) reuses and
# re-verifies them.

set -uo pipefail

LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
API_URL="${API_URL:-http://localhost:8080/api/v1}"
API_URL="${API_URL%/}"
BASE_URL="${API_URL%/api/v1}"
FAKE_GITHUB_URL="${FAKE_GITHUB_URL:-http://localhost:9090}"
FAKE_GITLAB_URL="${FAKE_GITLAB_URL:-http://localhost:9091}"
FAKE_BITBUCKET_URL="${FAKE_BITBUCKET_URL:-http://localhost:9092}"
GITHUB_MODE="${GITHUB_MODE:-real}"
if [[ $GITHUB_MODE == fake ]]; then
  GITHUB_TEST_REPO="${GITHUB_TEST_REPO:-octo-org/seismo-test}"
else
  GITHUB_TEST_REPO="${GITHUB_TEST_REPO:-}"
fi
STATE_FILE="${STATE_FILE:-$LIB_DIR/.env.test}"
VERBOSE="${VERBOSE:-0}"

PASS=0
FAIL=0
SKIP=0
STATUS=""
BODY=""
HEADERS=""
LAST_REQUEST=""

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

if [[ -t 1 ]]; then
  C_GREEN=$'\e[32m' C_RED=$'\e[31m' C_YELLOW=$'\e[33m' C_BOLD=$'\e[1m' C_DIM=$'\e[2m' C_RESET=$'\e[0m'
else
  C_GREEN="" C_RED="" C_YELLOW="" C_BOLD="" C_DIM="" C_RESET=""
fi

# ---------------------------------------------------------------- output --

section() { printf '\n%s== %s ==%s\n' "$C_BOLD" "$*" "$C_RESET"; }
note()    { printf '  %s%s%s\n' "$C_DIM" "$*" "$C_RESET"; }
pass()    { PASS=$((PASS + 1)); printf '  %sPASS%s %s\n' "$C_GREEN" "$C_RESET" "$1"; }
skip()    { SKIP=$((SKIP + 1)); printf '  %sSKIP%s %s\n' "$C_YELLOW" "$C_RESET" "$1"; }
fatal()   { printf '%sFATAL%s %s\n' "$C_RED" "$C_RESET" "$*" >&2; exit 2; }

fail() {
  FAIL=$((FAIL + 1))
  printf '  %sFAIL%s %s\n' "$C_RED" "$C_RESET" "$1"
  [[ -n ${2-} ]] && printf '       %s\n' "$2"
  printf '       request: %s -> %s\n' "$LAST_REQUEST" "$STATUS"
  printf '       body:    %.400s\n' "$BODY"
}

# summary prints the counts and returns non-zero when anything failed.
# Call it last so the script's exit code reflects the result.
summary() {
  printf '\n%s%s: %d passed, %d failed, %d skipped%s\n' \
    "$C_BOLD" "$(basename "$0")" "$PASS" "$FAIL" "$SKIP" "$C_RESET"
  [[ $FAIL -eq 0 ]]
}

rand() { printf '%s%05d' "$(date +%s)" "$RANDOM"; }

# ------------------------------------------------------------------ JSON --

JSON_TOOL=""
PY=""
if command -v jq >/dev/null 2>&1; then
  JSON_TOOL=jq
else
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import json' >/dev/null 2>&1; then
      PY=$candidate
      JSON_TOOL=python
      break
    fi
  done
fi
[[ -n $JSON_TOOL ]] || fatal "need jq or python3 to parse JSON"

# Python fallback understanding the small jq subset used here:
# paths like .a.b, .[0], .a[1].b, and the ops "get", "len" and "count".
PY_JSON='
import json, re, sys
op, path = sys.argv[1], sys.argv[2]
raw = sys.stdin.read()
try:
    v = json.loads(raw) if raw.strip() else None
except ValueError:
    print("null"); sys.exit(0)
for name, idx in re.findall(r"\.([A-Za-z_][A-Za-z0-9_]*)|\[(\d+)\]", path):
    try:
        v = v[name] if name else v[int(idx)]
    except (KeyError, IndexError, TypeError):
        v = None
        break
def out(x):
    if x is None: return "null"
    if isinstance(x, bool): return "true" if x else "false"
    if isinstance(x, str): return x
    return json.dumps(x)
if op == "get":
    print(out(v))
elif op == "len":
    print(len(v) if isinstance(v, (list, dict, str)) else 0)
elif op == "count":
    field, want = sys.argv[3], sys.argv[4]
    print(sum(1 for e in (v or []) if isinstance(e, dict) and out(e.get(field)) == want))
'

# jget PATH [JSON]: value at PATH (strings unquoted, missing -> null).
jget() {
  local path=$1 input=${2-$BODY}
  if [[ $JSON_TOOL == jq ]]; then
    printf '%s' "$input" | jq -r "$path" 2>/dev/null || echo null
  else
    printf '%s' "$input" | "$PY" -c "$PY_JSON" get "$path"
  fi
}

# jlen PATH [JSON]: length of the array/object at PATH.
jlen() {
  local path=$1 input=${2-$BODY}
  if [[ $JSON_TOOL == jq ]]; then
    printf '%s' "$input" | jq "$path | length" 2>/dev/null || echo 0
  else
    printf '%s' "$input" | "$PY" -c "$PY_JSON" len "$path"
  fi
}

# jcount PATH FIELD VALUE [JSON]: number of objects in the array at PATH
# whose FIELD equals VALUE.
jcount() {
  local path=$1 field=$2 want=$3 input=${4-$BODY}
  if [[ $JSON_TOOL == jq ]]; then
    local each="$path[]"
    [[ $path == . ]] && each=".[]"
    printf '%s' "$input" |
      jq --arg f "$field" --arg v "$want" \
        "[$each | select((.[\$f] | if type == \"string\" then . else tojson end) == \$v)] | length" 2>/dev/null || echo 0
  else
    printf '%s' "$input" | "$PY" -c "$PY_JSON" count "$path" "$field" "$want"
  fi
}

# ------------------------------------------------------------------ HTTP --

# http METHOD PATH [BODY] [TOKEN]
# PATH is relative to API_URL unless it starts with http. Sets STATUS,
# BODY and HEADERS. Never follows redirects.
http() {
  local method=$1 path=$2 body=${3-} token=${4-}
  local url=$path
  [[ $path == http* ]] || url="$API_URL$path"

  local args=(-sS -X "$method" -o "$TMP_DIR/body" -D "$TMP_DIR/headers" -w '%{http_code}'
    -H 'Accept: application/json' --max-time 30)
  [[ -n $body ]] && args+=(-H 'Content-Type: application/json' --data-binary "$body")
  [[ -n $token ]] && args+=(-H "Authorization: Bearer $token")

  LAST_REQUEST="$method ${url#"$BASE_URL"}"
  STATUS=$(curl "${args[@]}" "$url" 2>"$TMP_DIR/err") || STATUS=000
  BODY=$(cat "$TMP_DIR/body" 2>/dev/null || true)
  HEADERS=$(tr -d '\r' <"$TMP_DIR/headers" 2>/dev/null || true)
  [[ $STATUS == 000 ]] && BODY="curl: $(cat "$TMP_DIR/err")"

  if [[ $VERBOSE == 1 ]]; then
    note "$LAST_REQUEST -> $STATUS"
    [[ -n $body ]] && note "  > $body"
    note "  < ${BODY:0:300}"
  fi
}

# header NAME: value of a response header from the last request.
header() {
  printf '%s\n' "$HEADERS" | awk -v n="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" \
    'BEGIN{FS=": "} tolower($1)==n {sub(/^[^:]*: /, ""); v=$0} END{print v}'
}

# query_param URL NAME: one query parameter of URL (not URL-decoded).
query_param() {
  printf '%s' "$1" | sed -n "s/.*[?&]$2=\([^&]*\).*/\1/p"
}

# api METHOD PATH [BODY]: authenticated request with the saved token.
# The token is re-read from STATE_FILE on every call. If the API rejects it
# as invalid or expired, the owner logs in again, the new token is saved,
# and the request is retried once.
api() {
  load_state
  http "$1" "$2" "${3-}" "${TOKEN-}"
  if [[ $STATUS == 401 ]]; then
    local reason
    reason=$(jget .error)
    if [[ $reason == "invalid or expired token" || $reason == "missing bearer token" ]]; then
      note "token rejected ($reason); logging in again"
      local saved_status=$STATUS saved_body=$BODY
      if refresh_token; then
        http "$1" "$2" "${3-}" "$TOKEN"
      else
        STATUS=$saved_status BODY=$saved_body
      fi
    fi
  fi
}

# ------------------------------------------------------------ assertions --

expect_status() {
  if [[ $STATUS == "$1" ]]; then pass "$2"; else fail "$2" "expected HTTP $1, got $STATUS"; fi
}

expect_eq() {
  if [[ $1 == "$2" ]]; then pass "$3"; else fail "$3" "expected '$2', got '$1'"; fi
}

expect_ne() {
  if [[ $1 != "$2" ]]; then pass "$3"; else fail "$3" "did not expect '$2'"; fi
}

# expect_match VALUE REGEX DESC
expect_match() {
  if [[ $1 =~ $2 ]]; then pass "$3"; else fail "$3" "'$1' does not match /$2/"; fi
}

# expect_json PATH VALUE DESC: field of the last response body.
expect_json() { expect_eq "$(jget "$1")" "$2" "$3"; }

# ----------------------------------------------------------------- state --

load_state() {
  # shellcheck disable=SC1090
  [[ -f $STATE_FILE ]] && source "$STATE_FILE"
  return 0
}

# save_var NAME VALUE: persist NAME=VALUE in STATE_FILE (and the shell).
save_var() {
  local name=$1 value=$2
  touch "$STATE_FILE"
  grep -v "^$name=" "$STATE_FILE" >"$TMP_DIR/state" || true
  printf "%s='%s'\n" "$name" "$value" >>"$TMP_DIR/state"
  cat "$TMP_DIR/state" >"$STATE_FILE"
  printf -v "$name" '%s' "$value"
}

clear_var() {
  [[ -f $STATE_FILE ]] || return 0
  grep -v "^$1=" "$STATE_FILE" >"$TMP_DIR/state" || true
  cat "$TMP_DIR/state" >"$STATE_FILE"
  unset "$1"
}

# ---------------------------------------------------------------- setup --

wait_for_server() {
  local i
  for i in $(seq 1 60); do
    http GET "$BASE_URL/healthz"
    [[ $STATUS == 200 ]] && return 0
    sleep 1
  done
  fatal "backend not reachable at $BASE_URL/healthz (start it: docker compose -f deploy/compose.dev.yml up -d)"
}

# refresh_token logs the saved owner in again, or registers a new owner if
# there is none (or it no longer exists), then saves the new token.
refresh_token() {
  load_state
  STATUS=""
  if [[ -n ${OWNER_EMAIL-} && -n ${OWNER_PASSWORD-} ]]; then
    http POST /auth/login "{\"email\":\"$OWNER_EMAIL\",\"password\":\"$OWNER_PASSWORD\"}"
  fi
  if [[ $STATUS != 200 ]]; then
    local email="owner-$(rand)@example.com" password="pw-$(rand)-secret"
    http POST /auth/register "{\"email\":\"$email\",\"password\":\"$password\",\"name\":\"API Test Owner\"}"
    [[ $STATUS == 201 ]] || return 1
    save_var OWNER_EMAIL "$email"
    save_var OWNER_PASSWORD "$password"
    clear_var APP_ID
    clear_var CONNECTION_ID
  fi
  save_var TOKEN "$(jget .token.access_token)"
  save_var OWNER_ID "$(jget .owner.id)"
  return 0
}

# ensure_token makes sure STATE_FILE holds a working bearer token, verifying
# a saved one with GET /owners/me before reusing it.
ensure_token() {
  load_state
  if [[ -n ${TOKEN-} ]]; then
    http GET /owners/me "" "$TOKEN"
    if [[ $STATUS == 200 ]]; then
      pass "saved bearer token from $(basename "$STATE_FILE") is valid"
      return 0
    fi
    note "saved token rejected (HTTP $STATUS); getting a new one"
  fi
  refresh_token || fatal "could not log in or register an owner"
  http GET /owners/me "" "$TOKEN"
  expect_status 200 "new bearer token saved to $(basename "$STATE_FILE") works"
}

# new_owner registers a throwaway owner (not saved to STATE_FILE) and sets
# NEW_EMAIL, NEW_PASSWORD, NEW_TOKEN and NEW_OWNER_ID.
new_owner() {
  NEW_EMAIL="other-$(rand)@example.com"
  NEW_PASSWORD="pw-$(rand)-other"
  http POST /auth/register "{\"email\":\"$NEW_EMAIL\",\"password\":\"$NEW_PASSWORD\",\"name\":\"Other Owner\"}"
  [[ $STATUS == 201 ]] || fatal "could not register a second owner: $STATUS $BODY"
  NEW_TOKEN=$(jget .token.access_token)
  NEW_OWNER_ID=$(jget .owner.id)
}

# create_project NAME: creates a project as the saved owner; sets CREATED_ID.
create_project() {
  CREATED_ID=""
  api POST /projects "{\"name\":\"$1\"}"
  [[ $STATUS == 201 ]] || { fail "create project '$1'"; return 1; }
  CREATED_ID=$(jget .id)
}

# create_app NAME: creates an app as the saved owner; sets CREATED_ID.
create_app() {
  CREATED_ID=""
  api POST /apps "{\"name\":\"$1\",\"platform\":\"ios\",\"bundle_id\":\"com.example.a$(rand)\"}"
  [[ $STATUS == 201 ]] || { fail "create app '$1'"; return 1; }
  CREATED_ID=$(jget .id)
}

# link_repo PROJECT_ID REPOSITORY [IS_DEFAULT] [TRACKER]: links a
# repository (tracker github unless given) to the project; sets CREATED_ID.
# Projects hold no credentials: apps reach the repository through their own
# connections.
link_repo() {
  CREATED_ID=""
  api POST "/projects/$1/repositories"     "{\"tracker\":\"${4:-github}\",\"repository\":\"$2\",\"is_default\":${3:-false}}"
  [[ $STATUS == 201 ]] || { fail "link $2 to project $1"; return 1; }
  CREATED_ID=$(jget .id)
}

# ensure_app makes sure STATE_FILE holds APP_ID, an app of the saved owner
# that the saved GitHub connection belongs to. A saved app that no longer
# exists is replaced, and its connection forgotten.
ensure_app() {
  load_state
  if [[ -n ${APP_ID-} ]]; then
    api GET "/apps/$APP_ID"
    [[ $STATUS == 200 ]] && return 0
    note "saved app $APP_ID is gone; creating another"
  fi
  create_app "API test app" || fatal "could not create the test app"
  save_var APP_ID "$CREATED_ID"
  clear_var CONNECTION_ID
}

# ---------------------------------------------------------------- GitHub --

# fake_install_id prints a fresh installation ID on octo-org for the fake
# GitHub (IDs from 100000000 up are on other-org).
fake_install_id() { echo $((RANDOM * 1000 + RANDOM % 1000 + 1)); }

# github_connect APP_ID [INSTALLATION_ID] runs the connect flow for the app
# and sets CONNECTION_ID (it does not save it). With the fake GitHub it
# plays the browser: it calls the callback the way GitHub's redirect would,
# with INSTALLATION_ID or a fresh one. With a real GitHub App it prints the
# install URL and waits for a new connection to appear on the app.
github_connect() {
  local app=$1 install_id=${2:-$(fake_install_id)}
  CONNECTION_ID=""
  api GET "/apps/$app/connect/github"
  [[ $STATUS == 200 ]] || { fail "GET /apps/$app/connect/github" "is the GitHub provider configured?"; return 1; }
  local url state
  url=$(jget .url)
  state=$(query_param "$url" state)

  if [[ $GITHUB_MODE == fake ]]; then
    http GET "/providers/github/callback?installation_id=$install_id&setup_action=install&code=fake-code-$install_id&state=$state"
    local location
    location=$(header Location)
    if [[ $STATUS != 302 || $(query_param "$location" status) != connected ]]; then
      fail "connect GitHub installation $install_id to app $app" "callback redirected to: $location"
      return 1
    fi
    CONNECTION_ID=$(query_param "$location" connection_id)
  else
    printf '
  %sOpen this URL, install the GitHub App on the account that owns %s:%s
  %s

'       "$C_BOLD" "$GITHUB_TEST_REPO" "$C_RESET" "$url"
    api GET "/apps/$app/connections"
    local before
    before=$(jlen .)
    local i
    for i in $(seq 1 100); do
      sleep 3
      api GET "/apps/$app/connections"
      if [[ $(jlen .) -gt $before ]]; then
        CONNECTION_ID=$(jget ".[$(($(jlen .) - 1))].id")
        break
      fi
    done
    [[ -n $CONNECTION_ID ]] || { fail "waited 5 minutes for the GitHub App install"; return 1; }
  fi
}

# ensure_github_connection makes sure the saved app (ensure_app) has a
# GitHub connection: it reuses the saved CONNECTION_ID while the app still
# has it, otherwise connects and saves the new one.
ensure_github_connection() {
  ensure_app
  load_state
  if [[ -n ${CONNECTION_ID-} ]]; then
    api GET "/apps/$APP_ID/connections"
    if [[ $STATUS == 200 && $(jcount . id "$CONNECTION_ID") == 1 ]]; then
      note "reusing GitHub connection $CONNECTION_ID of app $APP_ID"
      return 0
    fi
  fi
  github_connect "$APP_ID" || return 1
  save_var CONNECTION_ID "$CONNECTION_ID"
}

# attach_connection APP_ID CONNECTION_ID gives the app a connection that
# another of the owner's apps already has (no new OAuth); sets CREATED_ID to
# the app's new connection.
attach_connection() {
  CREATED_ID=""
  api POST "/apps/$1/connections" "{\"connection_id\":\"$2\"}"
  [[ $STATUS == 201 ]] || { fail "attach connection $2 to app $1"; return 1; }
  CREATED_ID=$(jget .id)
}

# ------------------------------------------------- GitLab and Bitbucket --

# fake_tracker_url TRACKER: prints the base URL of the fake for TRACKER.
fake_tracker_url() {
  case $1 in
    github) echo "$FAKE_GITHUB_URL" ;;
    gitlab) echo "$FAKE_GITLAB_URL" ;;
    bitbucket) echo "$FAKE_BITBUCKET_URL" ;;
    *) fatal "no fake for tracker $1" ;;
  esac
}

# start_connect APP_ID TRACKER: GET /apps/{APP_ID}/connect/{TRACKER}; sets
# INSTALL_URL and STATE (the one-time state inside it).
start_connect() {
  INSTALL_URL="" STATE=""
  api GET "/apps/$1/connect/$2"
  [[ $STATUS == 200 ]] || return 1
  INSTALL_URL=$(jget .url)
  STATE=$(query_param "$INSTALL_URL" state)
}

# tracker_callback TRACKER QUERY: calls /providers/{TRACKER}/callback?QUERY
# the way the tracker's redirect would; sets LOCATION (the redirect).
tracker_callback() {
  http GET "/providers/$1/callback?$2"
  LOCATION=$(header Location)
}

# tracker_connect APP_ID TRACKER [CODE]: runs a whole OAuth connect flow for
# the app against the fake tracker (CODE defaults to a fresh
# fake-code-N) and sets CONNECTION_ID (not saved). Fails the check when the
# callback does not end in status=connected.
tracker_connect() {
  local app=$1 tracker=$2 code=${3:-fake-code-$(fake_install_id)}
  CONNECTION_ID=""
  start_connect "$app" "$tracker" || { fail "GET /apps/$app/connect/$tracker" "is the $tracker provider configured?"; return 1; }
  tracker_callback "$tracker" "code=$code&state=$STATE"
  if [[ $STATUS != 302 || $(query_param "$LOCATION" status) != connected ]]; then
    fail "connect $tracker ($code) to app $app" "callback redirected to: $LOCATION"
    return 1
  fi
  CONNECTION_ID=$(query_param "$LOCATION" connection_id)
}

# fake_tracker_issues TRACKER: GET {fake}/_fake/issues into BODY.
fake_tracker_issues() { http GET "$(fake_tracker_url "$1")/_fake/issues"; }

# fake_tracker_stat TRACKER NAME: prints one counter of the fake's
# /_fake/stats (exchanges, refreshes, rate_limited, issues).
fake_tracker_stat() {
  http GET "$(fake_tracker_url "$1")/_fake/stats"
  jget ".$2"
}

# fake_tracker_up TRACKER: returns 0 when the fake for TRACKER answers.
fake_tracker_up() {
  http GET "$(fake_tracker_url "$1")/healthz"
  [[ $STATUS == 200 ]]
}

# fake_issue_count TITLE: prints how many issues with that title the fake
# GitHub has. Usually called as $(...), so it does not change BODY/STATUS
# in the caller; call http GET "$FAKE_GITHUB_URL/_fake/issues" for those.
fake_issue_count() {
  http GET "$FAKE_GITHUB_URL/_fake/issues"
  jcount . title "$1"
}

# fake_stat NAME: prints one counter from the fake GitHub's /_fake/stats
# (secondary_limited, primary_limited, issues).
fake_stat() {
  http GET "$FAKE_GITHUB_URL/_fake/stats"
  jget ".$1"
}

# require_github_repo: returns 0 when GITHUB_TEST_REPO is set; otherwise
# records a skip. Scripts that need a real GitHub App start with
# require_github_repo || { summary; exit 0; }
require_github_repo() {
  [[ -n $GITHUB_TEST_REPO ]] || { skip "GitHub checks (set GITHUB_TEST_REPO to a repository the GitHub App can access)"; return 1; }
}

# require_fake DESC: returns 0 in fake mode; otherwise records a skip.
require_fake() {
  [[ $GITHUB_MODE == fake ]] || { skip "$1 (needs GITHUB_MODE=fake)"; return 1; }
}
