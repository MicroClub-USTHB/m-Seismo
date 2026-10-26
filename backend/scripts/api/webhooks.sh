#!/usr/bin/env bash
# GitHub App webhook: signature checks and which events are taken. Needs
# the backend's webhook secret:
#
#   GITHUB_WEBHOOK_SECRET=… backend/scripts/api/webhooks.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server

hook="$API_URL/webhooks/github"

# deliver EVENT BODY [SIGNATURE]: posts like GitHub does, signed with
# GITHUB_WEBHOOK_SECRET unless SIGNATURE is given.
deliver() {
  local sig=${3-}
  if [[ -z $sig ]]; then
    sig="sha256=$(printf '%s' "$2" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')"
  fi
  STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$hook" \
    -H 'Content-Type: application/json' -H "X-GitHub-Event: $1" \
    -H "X-GitHub-Delivery: $(rand)" -H "X-Hub-Signature-256: $sig" \
    --data-binary "$2")
}

if [[ -z ${GITHUB_WEBHOOK_SECRET-} ]]; then
  skip "webhooks (set GITHUB_WEBHOOK_SECRET to the backend's SEISMO_GITHUB_WEBHOOK_SECRET)"
  summary
  exit 0
fi

closed='{"action":"closed","issue":{"number":1},"repository":{"full_name":"octo-org/not-ours"}}'

section "signature"
deliver issues "$closed" "sha256=$(printf '%064d' 0)"
expect_status 401 "wrong signature -> 401"
deliver issues "$closed" "sha1=abc"
expect_status 401 "sha1 form -> 401"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$hook" -H 'X-GitHub-Event: issues' --data-binary "$closed")
expect_status 401 "unsigned -> 401"

section "events"
deliver ping '{"zen":"Design for failure.","hook_id":1}'
expect_status 204 "ping is acknowledged"
deliver issues '{"action":"labeled","issue":{"number":1},"repository":{"full_name":"octo-org/not-ours"}}'
expect_status 204 "other issue actions are acknowledged and ignored"
deliver issues "$closed"
expect_status 204 "an issue m-Seismo didn't open is acknowledged and ignored"
deliver issues '{'
expect_status 400 "invalid JSON -> 400"

summary
