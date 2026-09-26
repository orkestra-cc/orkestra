#!/usr/bin/env bash
# Fails if the Gotenberg hardening disappears from the compose files.
# V1 (spec 2026-09-26 §2): the backend is reachable from gotenberg on
# pdf-net, so the deny-list and the IP flags are REQUIRED, not redundant.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
env_file=$(mktemp); trap 'rm -f "$env_file"' EXIT
cat > "$env_file" <<'EOF'
APP_NAME=orkestra
ENV=development
MONGO_ROOT_PASSWORD=x
REDIS_PASSWORD=x
STORAGE_ACCESS_KEY=x
STORAGE_SECRET_KEY=x
PDF_RENDERER_PASSWORD=x
EOF
infra=$(docker compose --env-file "$env_file" -f docker-compose.infra.yml config)
need() { grep -qF -- "$1" <<<"$infra" || { echo "FAIL: infra compose lacks: $1"; fail=1; }; }
need "gotenberg/gotenberg:8@sha256:f29984bd1e226bf1b93ba90af06000afa8b315853e99d27b9aaa41b93f15c769"
need "--chromium-disable-javascript=true"
need "--chromium-deny-list=^(?!file:///tmp/).*"
need "--chromium-deny-private-ips=true"
need "--chromium-deny-public-ips=true"
need "--api-download-from-max-entries=0"
need "--webhook-disable=true"
need "--libreoffice-disable-routes=true"
need "--api-enable-basic-auth=true"
need "internal: true"
# gotenberg must publish no port and join only pdf-net
gblock=$(docker compose --env-file "$env_file" -f docker-compose.infra.yml config --format json | jq '.services.gotenberg')
[ "$(jq '.ports // [] | length' <<<"$gblock")" = "0" ] || { echo "FAIL: gotenberg publishes ports"; fail=1; }
[ "$(jq -c '.networks | keys' <<<"$gblock")" = '["pdf-net"]' ] || { echo "FAIL: gotenberg networks = $(jq -c '.networks|keys' <<<"$gblock")"; fail=1; }
for f in docker-compose.dev.yml docker-compose.staging.yml docker-compose.prod.yml; do
  standalone=$(docker compose --env-file "$env_file" -f "$f" config --format json)
  b=$(jq '.services.backend' <<<"$standalone")
  [ "$(jq -c '.networks | keys' <<<"$b")" = '["default","pdf-net"]' ] || { echo "FAIL: $f backend networks = $(jq -c '.networks|keys' <<<"$b")"; fail=1; }
  [ "$(jq -r '.networks."pdf-net".internal' <<<"$standalone")" = "true" ] \
    || { echo "FAIL: $f pdf-net is not internal: true (standalone render)"; fail=1; }

  # Combined render — this is how orkestra.sh actually deploys (infra file,
  # then the app file). Compose merges same-named networks across -f files
  # by union of keys, so an app file that forgets `internal: true` silently
  # drops it from the merged network even though the standalone render above
  # still had it, and gotenberg (only ever declared in the infra file) must
  # still end up on pdf-net alone.
  combined=$(docker compose --env-file "$env_file" -f docker-compose.infra.yml -f "$f" config --format json)
  [ "$(jq -r '.networks."pdf-net".internal' <<<"$combined")" = "true" ] \
    || { echo "FAIL: $f + infra: merged pdf-net is not internal: true"; fail=1; }
  [ "$(jq -c '.services.gotenberg.networks | keys' <<<"$combined")" = '["pdf-net"]' ] \
    || { echo "FAIL: $f + infra: gotenberg networks = $(jq -c '.services.gotenberg.networks|keys' <<<"$combined")"; fail=1; }
done
[ "$fail" = 0 ] && echo "ok: gotenberg hardening present"
exit "$fail"
