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

echo
printf 'orkestra-helpers: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
