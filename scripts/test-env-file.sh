#!/usr/bin/env bash
# Plain-bash unit tests for scripts/env-file.sh (no framework).
set -uo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$DIR/env-file.sh"

pass=0; fail=0
check() {  # check <desc> <expected> <actual>
    if [ "$2" = "$3" ]; then
        pass=$((pass + 1)); printf '  ok   %s\n' "$1"
    else
        fail=$((fail + 1)); printf '  FAIL %s\n       expected: [%s]\n       actual:   [%s]\n' "$1" "$2" "$3"
    fi
}

tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
cat > "$tmp" <<'EOF'
ENV=development
COOKIE_SECURE=false
# OAUTH_GOOGLE_CLIENT_ID=
STORAGE_ENDPOINT=http://orkestra-rustfs:9000
EOF

# --- env_get ---
check "get active key"        "development"                    "$(env_get "$tmp" ENV)"
check "get URL value"         "http://orkestra-rustfs:9000"    "$(env_get "$tmp" STORAGE_ENDPOINT)"
check "get commented stub"    ""                               "$(env_get "$tmp" OAUTH_GOOGLE_CLIENT_ID)"
check "get absent key"        ""                               "$(env_get "$tmp" NOPE)"

# --- env_set: replace active, no duplicate ---
env_set "$tmp" COOKIE_SECURE true
check "replace active"        "true"                           "$(env_get "$tmp" COOKIE_SECURE)"
check "no duplicate line"     "1"                              "$(grep -c '^COOKIE_SECURE=' "$tmp" || true)"

# --- env_set: uncomment a stub ---
env_set "$tmp" OAUTH_GOOGLE_CLIENT_ID "abc.apps.googleusercontent.com"
check "uncomment stub value"  "abc.apps.googleusercontent.com" "$(env_get "$tmp" OAUTH_GOOGLE_CLIENT_ID)"
check "stub no longer #"       "0"                             "$(grep -c '^# OAUTH_GOOGLE_CLIENT_ID=' "$tmp" || true)"

# --- env_set: append + URL round-trip ---
env_set "$tmp" NEW_KEY hello
check "append new key"        "hello"                          "$(env_get "$tmp" NEW_KEY)"
env_set "$tmp" BACKEND_URL "https://api.example.com/v1"
check "URL value round-trip"  "https://api.example.com/v1"     "$(env_get "$tmp" BACKEND_URL)"

# --- env_set: mode 600 ---
check "file mode is 600"      "600"                            "$(stat -c '%a' "$tmp")"

# --- env_get: tab-indented commented stub (regression: grep [[:space:]]) ---
printf '#\tTAB_STUB=tabval\n' >> "$tmp"
check "get tab-indented stub"   "tabval"          "$(env_get "$tmp" TAB_STUB)"
printf '#tSPUR=zzz\n' >> "$tmp"
check "no spurious t-match"      ""               "$(env_get "$tmp" SPUR)"
# --- env_get: pair-aware quote stripping ---
printf 'LONEQ="unterminated\n' >> "$tmp"
check "lone leading quote kept"  '"unterminated'  "$(env_get "$tmp" LONEQ)"
printf "QUOTED='/'\n" >> "$tmp"
check "paired quotes stripped"   "/"              "$(env_get "$tmp" QUOTED)"

echo
printf 'env-file: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
