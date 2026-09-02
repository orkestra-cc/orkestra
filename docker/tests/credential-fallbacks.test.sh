#!/usr/bin/env bash
# Static gate: a credential never has a default.
#
# The bundled RustFS once shipped with `${RUSTFS_ROOT_PASSWORD:-changeme-rustfs}`
# in docker-compose.infra.yml and the same literal as STORAGE_SECRET_KEY in
# docker/.env.example — so a checkout whose .env was never edited ran a
# browser-facing S3 API whose root password was printed in a public repository,
# and nothing (init, the validator, the backend) ever said so. This gate keeps
# the three properties that closed it:
#
#   1. no compose file gives a credential a LITERAL fallback (`${X:-value}`);
#      an empty fallback (`${X:-}`, storage disabled) or a required variable
#      (`${X:?...}`) is fine, and so is a fallback to another variable;
#   2. docker/.env.example ships every secret as a REPLACE_WITH_RANDOM_HEX_*
#      placeholder that scripts/init.sh fills, never as a value;
#   3. the infra file derives the RustFS root from STORAGE_* and refuses to
#      render without any storage credential at all, and Redis persists into
#      its volume.
#
# Wired into `make ci-backend` next to mongodb-replica-set.test.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo_root"

fail() {
  echo "credential fallbacks: $*" >&2
  exit 1
}

creds='MONGO_ROOT_PASSWORD|REDIS_PASSWORD|STORAGE_SECRET_KEY|RUSTFS_ROOT_PASSWORD|RUSTFS_SECRET_KEY|COOKIE_SECRET|OAUTH_TOKEN_ENCRYPTION_KEY|ORKESTRA_KMS_MASTER_KEY'

# 1. Literal fallbacks. `[^$}]` after `:-` excludes both the empty fallback and
#    a nested variable reference (`${A:-${B:?...}}`).
for f in docker/docker-compose.infra.yml docker/docker-compose.dev.yml \
         docker/docker-compose.staging.yml docker/docker-compose.prod.yml; do
  if hits="$(grep -nE "\\\$\\{(${creds}):-[^\$}][^}]*\\}" "$f")"; then
    fail "$f gives a credential a literal fallback:"$'\n'"$hits"
  fi
done

# 2. .env.example ships placeholders, never values, for the generated secrets.
for var in COOKIE_SECRET OAUTH_TOKEN_ENCRYPTION_KEY ORKESTRA_KMS_MASTER_KEY \
           MONGO_ROOT_PASSWORD REDIS_PASSWORD STORAGE_SECRET_KEY; do
  val="$(grep -E "^${var}=" docker/.env.example | head -1 | cut -d= -f2-)"
  case "$val" in
    REPLACE_WITH_RANDOM_HEX_*) ;;
    *) fail "docker/.env.example ships a literal for $var (want a REPLACE_WITH_RANDOM_HEX_* placeholder)" ;;
  esac
  grep -q "s|${val}|" scripts/init.sh || fail "scripts/init.sh does not fill the $var placeholder ($val)"
done
grep -qE '^# ?RUSTFS_ROOT_PASSWORD=$' docker/.env.example \
  || fail "docker/.env.example must keep RUSTFS_ROOT_PASSWORD as an EMPTY commented stub"

# 3. Rendering contract of the infra file.
compose_env=(
  APP_NAME=orkestra-test
  ENV=development
  MONGO_ROOT_PASSWORD=test-mongo-password
  REDIS_PASSWORD=test-redis-password
  STORAGE_ACCESS_KEY=test-access
  STORAGE_SECRET_KEY=test-secret-key-0123
)
rendered="$(env "${compose_env[@]}" docker compose -f docker/docker-compose.infra.yml config)"
grep -q 'RUSTFS_ACCESS_KEY: test-access' <<<"$rendered" \
  || fail "rustfs root key id is not derived from STORAGE_ACCESS_KEY"
grep -q 'RUSTFS_SECRET_KEY: test-secret-key-0123' <<<"$rendered" \
  || fail "rustfs root secret is not derived from STORAGE_SECRET_KEY"
# `config` renders the command as a YAML list, one token per line — join it.
redis_cmd="$(env "${compose_env[@]}" docker compose -f docker/docker-compose.infra.yml config --format json \
  | jq -r '.services.redis.command | join(" ")')"
grep -q -- '--dir /data' <<<"$redis_cmd" \
  || fail "redis does not persist into its volume (--dir /data missing from: $redis_cmd)"

rendered="$(env "${compose_env[@]}" RUSTFS_ROOT_USER=rustfs-root RUSTFS_ROOT_PASSWORD=dedicated-root-secret \
  docker compose -f docker/docker-compose.infra.yml config)"
grep -q 'RUSTFS_SECRET_KEY: dedicated-root-secret' <<<"$rendered" \
  || fail "an explicit RUSTFS_ROOT_PASSWORD does not override the STORAGE_* pair"

if env APP_NAME=orkestra-test ENV=development MONGO_ROOT_PASSWORD=x REDIS_PASSWORD=y \
     docker compose -f docker/docker-compose.infra.yml config >/dev/null 2>&1; then
  fail "the infra file renders with no storage credential at all — it must refuse"
fi

echo "credential fallbacks: OK"
