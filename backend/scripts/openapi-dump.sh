#!/usr/bin/env bash
# Dump the enterprise OpenAPI spec to backend/openapi/{enterprise-operator,enterprise-client}.json.
#
# The dump runs the real server binary (default build tags = enterprise) with
# OPENAPI_DUMP=1 set, which causes main.go to serialize both huma.API.OpenAPI()
# documents and exit before binding any listener. Module Init runs against
# isolated Mongo/Redis namespaces so dev data is never touched.
#
# Requirements:
#   - MongoDB + Redis reachable on localhost (start with docker/docker-compose.infra.yml)
#   - docker/.env present, with MONGO_ROOT_PASSWORD and REDIS_PASSWORD set
#   - Go on PATH
#
# Usage:  scripts/openapi-dump.sh
#         (or `make openapi-dump` from backend/)

set -euo pipefail

cd "$(dirname "$0")/.."   # repo backend/
BACKEND_ROOT="$PWD"

OUT_DIR="$BACKEND_ROOT/openapi"
OUT_FILE="$OUT_DIR/enterprise.json"
mkdir -p "$OUT_DIR"

# Connection strings: honor caller-set MONGO_URI / REDIS_URL (CI path — GitHub
# services run Mongo/Redis without auth) and otherwise compose them from
# docker/.env (local dev path — infra runs with credentials).
DUMP_DB="orkestra_openapi_dump"
REDIS_DB_INDEX=15

if [[ -z "${MONGO_URI:-}" || -z "${REDIS_URL:-}" ]]; then
  DOCKER_ENV="$BACKEND_ROOT/../docker/.env"
  if [[ ! -f "$DOCKER_ENV" ]]; then
    echo "✗ MONGO_URI / REDIS_URL not set and $DOCKER_ENV not found." >&2
    echo "  Either run from a dev checkout (with docker/.env present) or pre-set MONGO_URI + REDIS_URL." >&2
    exit 1
  fi
  set -a
  # shellcheck disable=SC1090
  . "$DOCKER_ENV"
  set +a
  : "${MONGO_ROOT_USERNAME:=admin}"
  : "${MONGO_PORT:=27017}"
  : "${REDIS_PORT:=6379}"
  if [[ -z "${MONGO_URI:-}" ]]; then
    : "${MONGO_ROOT_PASSWORD:?MONGO_ROOT_PASSWORD not in docker/.env}"
    MONGO_URI="mongodb://${MONGO_ROOT_USERNAME}:${MONGO_ROOT_PASSWORD}@localhost:${MONGO_PORT}/${DUMP_DB}?authSource=admin&replicaSet=rs0&directConnection=true"
  fi
  if [[ -z "${REDIS_URL:-}" ]]; then
    REDIS_AUTH=""
    [[ -n "${REDIS_PASSWORD:-}" ]] && REDIS_AUTH=":${REDIS_PASSWORD}@"
    REDIS_URL="redis://${REDIS_AUTH}localhost:${REDIS_PORT}/${REDIS_DB_INDEX}"
  fi
fi

# Sanity: Mongo must be reachable (CI services + local docker compose both
# expose on localhost). Best-effort — skip if nc isn't available.
if command -v nc >/dev/null 2>&1; then
  MONGO_HOST="$(echo "$MONGO_URI" | sed -E 's|.*@([^/:]+).*|\1|; s|.*://([^/:]+).*|\1|')"
  MONGO_PORT_GUESS="$(echo "$MONGO_URI" | sed -nE 's|.*:([0-9]+).*|\1|p' | head -1)"
  if ! nc -z "${MONGO_HOST:-localhost}" "${MONGO_PORT_GUESS:-27017}" 2>/dev/null; then
    echo "✗ MongoDB not reachable at ${MONGO_HOST:-localhost}:${MONGO_PORT_GUESS:-27017}." >&2
    echo "  Local dev: (cd docker && docker compose -f docker-compose.infra.yml up -d)" >&2
    exit 1
  fi
fi

# JWT keys: the auth module's Init opens the configured RSA key pair to parse
# it into memory. The dump exits before signing or verifying anything, so we
# generate a throwaway pair just to satisfy the file-open + RSA-parse check.
TMP_KEYS="$(mktemp -d)"
trap 'rm -rf "$TMP_KEYS"' EXIT
openssl genrsa -out "$TMP_KEYS/private.pem" 2048 >/dev/null 2>&1
openssl rsa -in "$TMP_KEYS/private.pem" -pubout -out "$TMP_KEYS/public.pem" >/dev/null 2>&1

echo "→ Running enterprise binary with OPENAPI_DUMP=1 (mongo-db=${DUMP_DB})"

OPENAPI_DUMP=1 \
  OPENAPI_DUMP_PATH="$OUT_FILE" \
  MONGO_URI="$MONGO_URI" \
  MONGO_DATABASE="$DUMP_DB" \
  REDIS_URL="$REDIS_URL" \
  JWT_PRIVATE_KEY_PATH="$TMP_KEYS/private.pem" \
  JWT_PUBLIC_KEY_PATH="$TMP_KEYS/public.pem" \
  CONSOLE_HOST="console.localhost" \
  CLIENT_API_HOST="api.localhost" \
  ENV="development" \
  LOG_LEVEL="warn" \
  CONTAINER_CONTROL_ENABLED="false" \
  go run ./cmd/server

if [[ ! -s "$OUT_FILE" ]]; then
  echo "✗ Spec was not written (or is empty): $OUT_FILE" >&2
  exit 1
fi

# The path count is cosmetic; jq is not guaranteed on dev hosts, so fall
# back to python3 and finally to "?" rather than printing a broken message.
path_count="$(jq '.paths | keys | length' "$OUT_FILE" 2>/dev/null \
  || python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["paths"]))' "$OUT_FILE" 2>/dev/null \
  || echo '?')"
echo "✓ Wrote $OUT_FILE ($(wc -c < "$OUT_FILE" | tr -d ' ') bytes, ${path_count} paths)"
