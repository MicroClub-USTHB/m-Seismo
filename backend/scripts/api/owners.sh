#!/usr/bin/env bash
# Owners: GET / PATCH / DELETE /owners/me. Email, password and delete are
# tested on a throwaway owner so the saved owner keeps working.
#
#   backend/scripts/api/owners.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server
ensure_token

section "read and update the saved owner"
api GET /owners/me
expect_status 200 "GET /owners/me"
expect_json .email "$OWNER_EMAIL" "returns the saved owner"
expect_json .password_hash null "no password hash in the response"

new_name="API Test Owner $(rand)"
api PATCH /owners/me "{\"name\":\"$new_name\"}"
expect_status 200 "PATCH name without current_password"
expect_json .name "$new_name" "name updated"
api GET /owners/me
expect_json .name "$new_name" "name change is persisted"

api PATCH /owners/me '{"name":"   "}'
expect_status 422 "blank name is rejected"
api PATCH /owners/me '{"nickname":"x"}'
expect_status 422 "unknown field is rejected"
api PATCH /owners/me '{}'
expect_status 200 "empty PATCH changes nothing"
expect_json .name "$new_name" "name unchanged by empty PATCH"

section "email and password changes (throwaway owner)"
new_owner
changed_email="changed-$(rand)@example.com"

http PATCH /owners/me "{\"email\":\"$changed_email\"}" "$NEW_TOKEN"
expect_status 401 "email change without current_password"
http PATCH /owners/me "{\"email\":\"$changed_email\",\"current_password\":\"wrong-one\"}" "$NEW_TOKEN"
expect_status 401 "email change with a wrong current_password"
http PATCH /owners/me "{\"email\":\"$OWNER_EMAIL\",\"current_password\":\"$NEW_PASSWORD\"}" "$NEW_TOKEN"
expect_status 409 "changing to another owner's email is a conflict"
http PATCH /owners/me "{\"email\":\"$changed_email\",\"current_password\":\"$NEW_PASSWORD\"}" "$NEW_TOKEN"
expect_status 200 "email change with current_password"
expect_json .email "$changed_email" "email updated"

http POST /auth/login "{\"email\":\"$NEW_EMAIL\",\"password\":\"$NEW_PASSWORD\"}"
expect_status 401 "old email no longer logs in"
http POST /auth/login "{\"email\":\"$changed_email\",\"password\":\"$NEW_PASSWORD\"}"
expect_status 200 "new email logs in"

http PATCH /owners/me '{"password":"short","current_password":"'"$NEW_PASSWORD"'"}' "$NEW_TOKEN"
expect_status 422 "too-short new password is rejected"
http PATCH /owners/me "{\"password\":\"brand-new-password\",\"current_password\":\"$NEW_PASSWORD\"}" "$NEW_TOKEN"
expect_status 200 "password change with current_password"
http POST /auth/login "{\"email\":\"$changed_email\",\"password\":\"$NEW_PASSWORD\"}"
expect_status 401 "old password no longer logs in"
http POST /auth/login "{\"email\":\"$changed_email\",\"password\":\"brand-new-password\"}"
expect_status 200 "new password logs in"
NEW_TOKEN=$(jget .token.access_token)

section "delete (throwaway owner)"
http DELETE /owners/me "" "$NEW_TOKEN"
expect_status 204 "DELETE /owners/me"
http GET /owners/me "" "$NEW_TOKEN"
expect_status 404 "deleted owner's token finds nothing"
http POST /auth/login "{\"email\":\"$changed_email\",\"password\":\"brand-new-password\"}"
expect_status 401 "deleted owner cannot log in"

section "saved owner unaffected"
api GET /owners/me
expect_status 200 "saved token still works"
expect_json .email "$OWNER_EMAIL" "still the saved owner"

summary
