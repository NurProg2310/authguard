#!/usr/bin/env bash
# End-to-end check of the single-session policy against a running stack.
# Usage: ./scripts/smoke-test.sh [base_url]
set -euo pipefail

BASE="${1:-http://localhost:8080}"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
A="$TMP/device-a.jar"; B="$TMP/device-b.jar"
EMAIL="smoke-$(date +%s)-$RANDOM@example.com"
PASS="Sm0ke-test-password"
H=(-H "Content-Type: application/json" -H "X-AuthGuard-CSRF: 1")
REG=$(printf '{"name":"Smoke Test","email":"%s","password":"%s"}' "$EMAIL" "$PASS")
CREDS=$(printf '{"email":"%s","password":"%s"}' "$EMAIL" "$PASS")
BAD=$(printf '{"email":"%s","password":"%s"}' "$EMAIL" "nope-1234")
pass=0; fail=0

check() { # name expected actual
  if [[ "$2" == "$3" ]]; then echo "  ✓ $1"; pass=$((pass+1)); else echo "  ✗ $1 (expected $2, got $3)"; fail=$((fail+1)); fi
}
req() { # jar method path [json] [token] -> writes body to $TMP/body, prints status
  local jar=$1 method=$2 path=$3 data=${4:-} tok=${5:-}
  local args=(-s -o "$TMP/body" -w '%{http_code}' -X "$method" -b "$jar" -c "$jar" "${H[@]}")
  [[ -n $data ]] && args+=(-d "$data")
  [[ -n $tok ]] && args+=(-H "Authorization: Bearer $tok")
  curl "${args[@]}" "$BASE/api$path"
}
json() { python3 -c "import json,sys; d=json.load(open('$TMP/body')); print(eval('d'+sys.argv[1]))" "$1"; }

echo "→ $BASE  ($EMAIL)"
check "health"                       200 "$(req "$A" GET /health)"

echo "Device A"
check "register"                     201 "$(req "$A" POST /auth/register "$REG")"
TOK_A="$(json "['access_token']")"
check "protected route with token"   200 "$(req "$A" GET /auth/me "" "$TOK_A")"
check "missing CSRF header rejected" 403 "$(curl -s -o /dev/null -w '%{http_code}' -X POST -b "$A" "$BASE/api/auth/refresh")"
check "refresh rotates token"        200 "$(req "$A" POST /auth/refresh)"
TOK_A="$(json "['access_token']")"

echo "Device B"
check "second login blocked (409)"   409 "$(req "$B" POST /auth/login "$CREDS")"
check "conflict code"                SESSION_ACTIVE "$(json "['error']['code']")"
TAKEOVER="$(json "['error']['takeover_token']")"
check "device A still valid"         200 "$(req "$A" GET /sessions/current "" "$TOK_A")"
check "takeover succeeds"            200 "$(req "$B" POST /auth/takeover "$(printf '{"takeover_token":"%s"}' "$TAKEOVER")")"
TOK_B="$(json "['access_token']")"
check "takeover token is single-use" 401 "$(req "$TMP/c.jar" POST /auth/takeover "$(printf '{"takeover_token":"%s"}' "$TAKEOVER")")"

echo "Device A after takeover"
check "old access token rejected"    401 "$(req "$A" GET /sessions/current "" "$TOK_A")"
check "revoke reason"                superseded "$(json "['error']['reason']")"
check "old refresh token rejected"   401 "$(req "$A" POST /auth/refresh)"

echo "Device B"
check "new session works"            200 "$(req "$B" GET /sessions "" "$TOK_B")"
check "history shows 2 sessions"     2 "$(json "['sessions'].__len__()")"
check "logout"                       204 "$(req "$B" POST /auth/logout "" "$TOK_B")"
check "token dead after logout"      401 "$(req "$B" GET /auth/me "" "$TOK_B")"
check "login allowed after logout"   200 "$(req "$A" POST /auth/login "$CREDS")"
TOK_A="$(json "['access_token']")"
check "wrong password"               401 "$(req "$B" POST /auth/login "$BAD")"
check "logout A"                     204 "$(req "$A" POST /auth/logout "" "$TOK_A")"

# Per-email login budget is 10 / 15 min; 4 used so far.
echo "Concurrent logins (6 parallel, no active session)"
for i in $(seq 1 6); do
  curl -s -o /dev/null -w '%{http_code}\n' -X POST "${H[@]}" -d "$CREDS" "$BASE/api/auth/login" &
done > "$TMP/race"; wait
check "exactly one parallel login wins" 1 "$(grep -c '^200$' "$TMP/race" || true)"
check "the rest get 409"                5 "$(grep -c '^409$' "$TMP/race" || true)"

echo; echo "passed: $pass  failed: $fail"
[[ $fail -eq 0 ]]
