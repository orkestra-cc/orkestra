#!/usr/bin/env bash
# Sources orkestra.sh (main() is guarded, so this does NOT launch the TUI) and
# unit-tests ask_value's non-interactive path. Also re-tested by Task 3.
set -uo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$DIR/../orkestra.sh"     # guarded main => no menu; sourcing is safe
trap - EXIT INT ERR              # drop orkestra's traps in this test shell
set +Eeuo pipefail               # orkestra enabled strict mode on source; relax
HAS_GUM=false                    # force the plain read path (gum may be installed)

pass=0; fail=0
check() { if [ "$2" = "$3" ]; then pass=$((pass+1)); printf '  ok   %s\n' "$1"
          else fail=$((fail+1)); printf '  FAIL %s\n       expected: [%s]\n       actual:   [%s]\n' "$1" "$2" "$3"; fi; }

check "empty input -> default"  "mydefault" "$(printf '\n'        | ask_value 'Label' 'mydefault')"
check "typed input overrides"   "custom"    "$(printf 'custom\n'  | ask_value 'Label' 'mydefault')"
check "no default, empty -> ''" ""          "$(printf '\n'        | ask_value 'Label' '')"

# --- apply_env_profile (pure, deterministic per ENV) ---
ptmp="$(mktemp)"; printf 'ENV=development\nCOOKIE_SECURE=false\nDEBUG=true\n' > "$ptmp"
apply_env_profile "$ptmp" production
check "prod cookie secure"  "true"       "$(env_get "$ptmp" COOKIE_SECURE)"
check "prod samesite strict" "strict"    "$(env_get "$ptmp" COOKIE_SAME_SITE)"
check "prod debug off"      "false"      "$(env_get "$ptmp" DEBUG)"
check "prod rate limit"     "30"         "$(env_get "$ptmp" RATE_LIMIT_REQUESTS_PER_MINUTE)"
check "prod vite env"       "production" "$(env_get "$ptmp" VITE_ENV)"
apply_env_profile "$ptmp" development
check "dev cookie insecure" "false"      "$(env_get "$ptmp" COOKIE_SECURE)"
check "dev rate limit"      "1000"       "$(env_get "$ptmp" RATE_LIMIT_REQUESTS_PER_MINUTE)"
rm -f "$ptmp"

# --- JWT key permissions: the private signing key must never become
# group/world-readable merely to accommodate a container UID mismatch. ---
keys_tmp="$(mktemp -d)"
old_docker_dir="$DOCKER_DIR"
DOCKER_DIR="$keys_tmp"
mkdir -p "$DOCKER_DIR/keys"
printf 'private\n' > "$DOCKER_DIR/keys/jwt-private.pem"
printf 'public\n' > "$DOCKER_DIR/keys/jwt-public.pem"
chmod 600 "$DOCKER_DIR/keys/jwt-private.pem" "$DOCKER_DIR/keys/jwt-public.pem"
ensure_jwt_keys_readable >/dev/null
check "private JWT key stays private" "600" "$(stat -c '%a' "$DOCKER_DIR/keys/jwt-private.pem")"
check "public JWT key becomes readable" "644" "$(stat -c '%a' "$DOCKER_DIR/keys/jwt-public.pem")"
DOCKER_DIR="$old_docker_dir"
rm -rf "$keys_tmp"

generator_tmp="$(mktemp -d)"
mkdir -p "$generator_tmp/scripts" "$generator_tmp/docker"
cp "$DIR/generate-jwt-keys.sh" "$generator_tmp/scripts/"
bash "$generator_tmp/scripts/generate-jwt-keys.sh" >/dev/null 2>&1
check "JWT generator creates private key as 600" "600" "$(stat -c '%a' "$generator_tmp/docker/keys/jwt-private.pem")"
rm -rf "$generator_tmp"

echo
printf 'orkestra-helpers: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
