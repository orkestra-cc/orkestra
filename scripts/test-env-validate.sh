#!/usr/bin/env bash
# Plain-bash tests for scripts/env-validate.sh's secret-hygiene rules (no
# framework). The validator derives its ENV_FILE from its own location, so a
# copy in a scratch PROJECT_ROOT is run against a scratch docker/.env — the
# developer's real one is never read (same trick as test-orkestra-helpers.sh).
set -uo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$DIR")"
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

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/scripts" "$tmp/docker"
cp "$DIR/env-validate.sh" "$DIR/env-file.sh" "$tmp/scripts/"
env_file="$tmp/docker/.env"
out="$tmp/out"

# run KEY=VALUE... — shipped .env.example plus the overrides, validated; echoes
# the exit status and leaves the output in $out for assertions.
run() {
    local kv
    cp "$PROJECT_ROOT/docker/.env.example" "$env_file"
    for kv in "$@"; do env_set "$env_file" "${kv%%=*}" "${kv#*=}"; done
    bash "$tmp/scripts/env-validate.sh" > "$out" 2>&1
    printf '%s' "$?"
}
saw() { grep -q "$1" "$out" && printf yes || printf no; }

hex32=0123456789abcdef0123456789abcdef
# What `make init` plus the wizard leave behind for a production stack.
prod=(
    ENV=production
    COOKIE_SECURE=true
    COOKIE_SAME_SITE=strict
    OAUTH_GOOGLE_CLIENT_ID=client-id-for-tests
    OAUTH_GOOGLE_CLIENT_SECRET=client-secret-for-tests
    "COOKIE_SECRET=${hex32}${hex32}"
    "OAUTH_TOKEN_ENCRYPTION_KEY=${hex32}${hex32}"
    "ORKESTRA_KMS_MASTER_KEY=${hex32}${hex32}"
    "MONGO_ROOT_PASSWORD=${hex32}"
    "REDIS_PASSWORD=${hex32}"
    "STORAGE_SECRET_KEY=${hex32}"
)

# --- development: the shipped placeholders are tolerated, but named ---
check "development: shipped placeholders only warn"        "0"   "$(run)"
check "development: the warning names STORAGE_SECRET_KEY"  "yes" "$(saw 'STORAGE_SECRET_KEY is empty or a placeholder')"

# --- production: generated secrets pass, anything weaker is an error ---
check "production: generated secrets pass"                 "0"   "$(run "${prod[@]}")"
check "production: the shipped RustFS literal is refused"  "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=changeme-rustfs)"
check "production: the refusal names the key"              "yes" "$(saw 'STORAGE_SECRET_KEY is empty or a placeholder')"
check "production: the refusal says how to fix it"         "yes" "$(saw 'openssl rand -hex 16')"
check "production: an unfilled placeholder is refused"     "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=REPLACE_WITH_RANDOM_HEX_32_STORAGE_SECRET)"
check "production: the image default is refused"           "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=rustfsadmin)"
check "production: a short datastore password is refused"  "1"   "$(run "${prod[@]}" REDIS_PASSWORD=short1234)"
check "production: the refusal explains the length rule"   "yes" "$(saw 'REDIS_PASSWORD is shorter than 16 characters')"
check "production: a set RustFS root password is checked"  "1"   "$(run "${prod[@]}" RUSTFS_ROOT_PASSWORD=changeme-rustfs)"
check "production: an unset RustFS root is not an error"   "0"   "$(run "${prod[@]}")"

# --- object storage may be disabled outright: both keys empty ---
check "production: storage disabled passes"                "0"   "$(run "${prod[@]}" STORAGE_ACCESS_KEY= STORAGE_SECRET_KEY=)"
check "production: storage disabled is reported"           "yes" "$(saw 'object storage disabled')"
check "production: a key id with no secret is an error"    "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=)"

# --- staging is as strict as production ---
check "staging: the shipped RustFS literal is refused"     "1"   "$(run "${prod[@]}" ENV=staging COOKIE_SAME_SITE=lax STORAGE_SECRET_KEY=changeme-rustfs)"

echo
printf 'env-validate: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
