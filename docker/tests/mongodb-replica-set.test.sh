#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo_root"

fail() {
  echo "mongodb replica-set config: $*" >&2
  exit 1
}

compose_env=(
  APP_NAME=orkestra-test
  ENV=development
  MONGO_ROOT_USERNAME=admin
  MONGO_ROOT_PASSWORD=test-mongo-password
  MONGO_DATABASE=orkestra
  MONGO_HOST=mongodb
  REDIS_HOST=redis
  REDIS_PASSWORD=test-redis-password
  OTEL_EXPORTER_OTLP_ENDPOINT=
  SENTRY_DSN_FRONTEND=
  OAUTH_TOKEN_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  ORKESTRA_KMS_MASTER_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  COOKIE_SECRET=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
)

infra="$(env "${compose_env[@]}" docker compose -f docker/docker-compose.infra.yml config)"
grep -q '/opt/orkestra/replica-entrypoint.sh' <<<"$infra" || fail "compose does not install the replica entrypoint"
grep -q -- '--replSet rs0' docker/mongo-init/replica-entrypoint.sh || fail "mongod does not enable rs0"
grep -q -- '--keyFile' docker/mongo-init/replica-entrypoint.sh || fail "authenticated mongod has no internal key file"
grep -q 'rs.initiate' <<<"$infra" || fail "health check does not initialize rs0 idempotently"
grep -q 'isWritablePrimary' <<<"$infra" || fail "health check does not wait for a writable primary"

for app_file in dev staging prod; do
  rendered="$(env "${compose_env[@]}" docker compose \
    -f docker/docker-compose.infra.yml \
    -f "docker/docker-compose.${app_file}.yml" config)"
  mongo_uri="$(awk '/MONGO_URI:/ { print; exit }' <<<"$rendered")"
  grep -q 'replicaSet=rs0' <<<"$mongo_uri" || fail "$app_file backend URI does not select rs0"
  grep -q 'directConnection=true' <<<"$mongo_uri" || fail "$app_file backend URI is not host-safe direct connection"
done

grep -q -- '--replSet rs0' .github/workflows/backend.yml || fail "backend CI does not start a replica set"
grep -q 'isWritablePrimary' .github/workflows/backend.yml || fail "backend CI does not wait for primary readiness"

echo "mongodb replica-set config: OK"
