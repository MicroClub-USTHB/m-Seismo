#!/usr/bin/env bash
# Auth: registration, login, bearer token checks, and the saved-token
# lifecycle (save to the state file, reuse, automatic refresh).
#
#   backend/scripts/api/auth.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
wait_for_server

section "health"
http GET "$BASE_URL/healthz"
expect_status 200 "GET /healthz"
expect_json .status ok "health status is ok"

section "register"
id=$(rand)
email="Auth-$id@Example.com"
password="pw-$id-secret"
http POST /auth/register "{\"email\":\"$email\",\"password\":\"$password\",\"name\":\"Auth Test\"}"
expect_status 201 "register a new owner"
expect_json .owner.email "auth-$id@example.com" "email is trimmed and lower-cased"
expect_json .owner.name "Auth Test" "name is stored"
expect_json .token.token_type Bearer "token type is Bearer"
expect_match "$(jget .token.access_token)" '^ey[^.]+\.[^.]+\.[^.]+$' "access token is a JWT"
expect_json .owner.password_hash null "password hash is never returned"
reg_token=$(jget .token.access_token)

http POST /auth/register "{\"email\":\"AUTH-$id@example.com\",\"password\":\"$password\",\"name\":\"Dup\"}"
expect_status 409 "same email in another case is a conflict"

http POST /auth/register '{"email":"not-an-email","password":"long-enough-pw","name":"X"}'
expect_status 422 "invalid email is rejected"
http POST /auth/register "{\"email\":\"short-$id@example.com\",\"password\":\"short\",\"name\":\"X\"}"
expect_status 422 "password under 8 bytes is rejected"
http POST /auth/register "{\"email\":\"noname-$id@example.com\",\"password\":\"long-enough-pw\",\"name\":\"  \"}"
expect_status 422 "blank name is rejected"
http POST /auth/register "{\"email\":\"x-$id@example.com\",\"password\":\"long-enough-pw\",\"name\":\"X\",\"role\":\"admin\"}"
expect_status 422 "unknown JSON field is rejected"
http POST /auth/register '{"email":'
expect_status 422 "malformed JSON is rejected"

section "login"
http POST /auth/login "{\"email\":\"auth-$id@example.com\",\"password\":\"$password\"}"
expect_status 200 "login with the right password"
expect_json .owner.email "auth-$id@example.com" "login returns the owner"
http POST /auth/login "{\"email\":\"AUTH-$id@EXAMPLE.COM\",\"password\":\"$password\"}"
expect_status 200 "login email is case-insensitive"
http POST /auth/login "{\"email\":\"auth-$id@example.com\",\"password\":\"wrong-password\"}"
expect_status 401 "wrong password is rejected"
wrong_pw_error=$(jget .error)
http POST /auth/login "{\"email\":\"nobody-$id@example.com\",\"password\":\"$password\"}"
expect_status 401 "unknown email is rejected"
expect_eq "$(jget .error)" "$wrong_pw_error" "unknown email and wrong password give the same error"

section "bearer token checks"
http GET /owners/me
expect_status 401 "no token -> 401"
expect_json .error "missing bearer token" "error says the token is missing"
http GET /owners/me "" "not-a-jwt"
expect_status 401 "garbage token -> 401"
# Flip a character in the middle of the signature (the last base64
# character only carries padding bits, so changing it may be a no-op).
sig=${reg_token##*.}
c=${sig:5:1}
[[ $c == A ]] && r=B || r=A
http GET /owners/me "" "${reg_token%.*}.${sig:0:5}$r${sig:6}"
expect_status 401 "tampered signature -> 401"
http GET /owners/me "" "$reg_token"
expect_status 200 "registration token works"
expect_json .email "auth-$id@example.com" "token belongs to the registered owner"

section "saved token lifecycle"
ensure_token
load_state
note "state file: $STATE_FILE"
api GET /owners/me
expect_status 200 "request with the saved token"
expect_json .email "$OWNER_EMAIL" "saved token belongs to the saved owner"

saved_good=$TOKEN
save_var TOKEN "deliberately-broken-token"
api GET /owners/me
expect_status 200 "a rejected saved token is refreshed automatically"
load_state
expect_ne "$TOKEN" "deliberately-broken-token" "refreshed token was written back to the state file"
http GET /owners/me "" "$TOKEN"
expect_status 200 "refreshed token works on its own"
http GET /owners/me "" "$saved_good"
expect_status 200 "the previous token is still valid until it expires"

summary
