#!/usr/bin/env bash
# Fails if the retrieval overlay (Qdrant + TEI, ADR-0023) loses its hardening:
# every image pinned by digest, no published port, the engine and the inference
# servers reachable only on the internal retrieval-net, the weight seeds the
# only services with egress, authentication on every sidecar, and the endpoints
# JSON the backend receives matching the model each TEI actually serves.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
bad() { echo "FAIL: $*"; fail=1; }

env_file=$(mktemp); trap 'rm -f "$env_file" "${nokey_env:-}"' EXIT
cat > "$env_file" <<'EOT'
APP_NAME=orkestra
ENV=development
MONGO_ROOT_PASSWORD=x
REDIS_PASSWORD=x
STORAGE_ACCESS_KEY=x
STORAGE_SECRET_KEY=x
PDF_RENDERER_PASSWORD=x
QDRANT_API_KEY=x
RETRIEVAL_INFERENCE_API_KEY=y
EOT

# The process environment overrides --env-file in Compose, so every render
# runs without the retrieval variables a developer shell may have exported.
dc() { env -u QDRANT_URL -u QDRANT_API_KEY -u QDRANT_COLLECTION_PREFIX \
  -u RETRIEVAL_INFERENCE_ENDPOINTS -u RETRIEVAL_INFERENCE_API_KEY \
  -u QDRANT_MEM_LIMIT -u TEI_EMBED_MEM_LIMIT -u TEI_RERANK_MEM_LIMIT \
  -u RETRIEVAL_SEED_TIMEOUT \
  docker compose "$@"; }

serving="tei-embed tei-rerank"
seeds="tei-embed-seed tei-rerank-seed"
all="qdrant $serving $seeds"

for app in docker-compose.dev.yml docker-compose.staging.yml docker-compose.prod.yml; do
  m=$(dc --env-file "$env_file" -f docker-compose.infra.yml -f "$app" -f docker-compose.retrieval.yml \
    --profile retrieval-seed config --format json 2>/dev/null) || { bad "$app + retrieval overlay does not render"; continue; }
  s() { jq -r --arg n "$1" "(.services[\$n] | $2) // empty" <<<"$m"; }
  # flag <service> <flag>: the argument following <flag> in the service command.
  flag() { jq -r --arg n "$1" --arg f "$2" '.services[$n].command as $c | [range(0; $c|length) | select($c[.]==$f) | $c[.+1]] | first // empty' <<<"$m"; }

  # Images: pinned by digest; the seeds run the image they warm the cache for.
  [[ "$(s qdrant .image)" =~ ^qdrant/qdrant:v1\.(2[0-9]|[3-9][0-9])\.[0-9]+-unprivileged@sha256:[0-9a-f]{64}$ ]] \
    || bad "$app qdrant image '$(s qdrant .image)' is not a pinned >=1.20 unprivileged digest"
  for svc in $serving $seeds; do
    [[ "$(s "$svc" .image)" =~ ^ghcr\.io/huggingface/text-embeddings-inference:cpu-[0-9.]+@sha256:[0-9a-f]{64}$ ]] \
      || bad "$app $svc image '$(s "$svc" .image)' is not a pinned TEI cpu digest"
  done
  for pair in tei-embed:tei-embed-seed tei-rerank:tei-rerank-seed; do
    [ "$(s "${pair%%:*}" .image)" = "$(s "${pair##*:}" .image)" ] || bad "$app ${pair##*:} image differs from ${pair%%:*}"
    [ "$(s "${pair%%:*}" '.command|tostring')" = "$(s "${pair##*:}" '.command|tostring')" ] \
      || bad "$app ${pair##*:} command differs from ${pair%%:*}"
  done

  for svc in $all; do
    [ "$(s "$svc" '(.ports // []) | length')" = "0" ] || bad "$app $svc publishes ports"
    [ "$(s "$svc" '.security_opt | index("no-new-privileges:true") != null')" = "true" ] || bad "$app $svc lacks no-new-privileges"
    [ "$(s "$svc" '.cap_drop|tostring')" = '["ALL"]' ] || bad "$app $svc cap_drop != [ALL]"
    [ "$(s "$svc" '(.healthcheck.test // []) | length > 0')" = "true" ] || bad "$app $svc has no healthcheck"
    [ "$(s "$svc" '.labels["orkestra.stack"]')" = "orkestra-development" ] || bad "$app $svc lacks the orkestra.stack label"
    [ -n "$(s "$svc" .mem_limit)" ] || bad "$app $svc has no mem_limit"
    [ "$(s "$svc" .container_name)" != "" ] || bad "$app $svc has no container_name"
  done

  # Health windows: the seeds' start_period is RETRIEVAL_SEED_TIMEOUT (default
  # 1800 s, the default `up --wait` bound of orkestra.sh); serving stays 300 s.
  for svc in $seeds; do
    [ "$(s "$svc" .healthcheck.start_period)" = "30m0s" ] || bad "$app $svc start_period = '$(s "$svc" .healthcheck.start_period)' (want 30m0s)"
  done
  for svc in $serving; do
    [ "$(s "$svc" .healthcheck.start_period)" = "5m0s" ] || bad "$app $svc start_period = '$(s "$svc" .healthcheck.start_period)' (want 5m0s)"
  done

  # Networks: engine and inference on retrieval-net only; seeds on egress only.
  for svc in qdrant $serving; do
    [ "$(s "$svc" '.networks|keys|tostring')" = '["retrieval-net"]' ] || bad "$app $svc networks = $(s "$svc" '.networks|keys|tostring')"
  done
  for svc in $seeds; do
    [ "$(s "$svc" '.networks|keys|tostring')" = '["retrieval-egress"]' ] || bad "$app $svc networks = $(s "$svc" '.networks|keys|tostring')"
    [ "$(s "$svc" '.profiles|tostring')" = '["retrieval-seed"]' ] || bad "$app $svc is not in the retrieval-seed profile"
    [ "$(s "$svc" .restart)" = "no" ] || bad "$app $svc restart != no"
  done
  [ "$(s backend '.networks|keys|tostring')" = '["default","pdf-net","retrieval-net"]' ] \
    || bad "$app backend networks = $(s backend '.networks|keys|tostring')"
  [ "$(jq -r '.networks."retrieval-net".internal' <<<"$m")" = "true" ] || bad "$app retrieval-net is not internal: true"
  [ "$(jq -r '.networks."retrieval-egress".internal // false' <<<"$m")" != "true" ] || bad "$app retrieval-egress must not be internal"
  joined=$(jq -r '[.services|to_entries[]|select(.value.networks // {}|has("retrieval-egress"))|.key]|sort|join(" ")' <<<"$m")
  [ "$joined" = "tei-embed-seed tei-rerank-seed" ] || bad "$app services on retrieval-egress: '$joined'"

  # Authentication on every sidecar, and the same secret on the backend.
  [ "$(s qdrant '.environment.QDRANT__SERVICE__API_KEY')" = "x" ] || bad "$app qdrant has no API key"
  [ "$(s qdrant '.environment.QDRANT__SERVICE__ENABLE_STATIC_CONTENT')" = "false" ] || bad "$app qdrant serves the web UI"
  [ "$(s qdrant '.environment.QDRANT__TELEMETRY_DISABLED')" = "true" ] || bad "$app qdrant telemetry not disabled"
  for svc in $serving $seeds; do
    [ "$(s "$svc" '.environment.API_KEY')" = "y" ] || bad "$app $svc has no API_KEY"
  done
  [ "$(s backend '.environment.RETRIEVAL_INFERENCE_API_KEY')" = "y" ] || bad "$app backend lacks RETRIEVAL_INFERENCE_API_KEY"
  [ "$(s backend '.environment.QDRANT_URL')" = "http://qdrant:6333" ] || bad "$app backend QDRANT_URL = '$(s backend .environment.QDRANT_URL)'"
  [ "$(s backend '.environment.QDRANT_API_KEY')" = "x" ] || bad "$app backend QDRANT_API_KEY mismatch"

  # Weights: one volume at /data; serving read-only, seeds read-write.
  for svc in $serving $seeds; do
    src=$(s "$svc" '.volumes[]|select(.target=="/data")|.source')
    [ "$src" = "tei-weights" ] || bad "$app $svc /data source = '$src'"
    ro=$(s "$svc" '.volumes[]|select(.target=="/data")|(.read_only // false)|tostring')
    case $svc in
      *-seed) [ "$ro" = "false" ] || bad "$app $svc must mount tei-weights read-write" ;;
      *) [ "$ro" = "true" ] || bad "$app $svc must mount tei-weights read-only" ;;
    esac
  done
  [ "$(s qdrant '.volumes[]|select(.target=="/qdrant/storage")|.source')" = "qdrant-data" ] || bad "$app qdrant storage volume"

  # Offline intent: declared on the serving side, absent on the seeds.
  for svc in $serving; do
    [ "$(s "$svc" '.environment.HF_HUB_OFFLINE')" = "1" ] || bad "$app $svc lacks HF_HUB_OFFLINE=1"
  done
  for svc in $seeds; do
    [ -z "$(s "$svc" '.environment.HF_HUB_OFFLINE')" ] || bad "$app $svc sets HF_HUB_OFFLINE"
  done

  # --payload-limit large enough for 32 inputs of 64 KiB plus JSON escaping.
  for svc in $serving $seeds; do
    pl=$(flag "$svc" --payload-limit)
    [[ "$pl" =~ ^[0-9]+$ ]] && [ "$pl" -ge 13000000 ] || bad "$app $svc --payload-limit = '$pl' (want >= 13000000)"
  done

  # Endpoints JSON must describe what each TEI serves.
  ep=$(s backend '.environment.RETRIEVAL_INFERENCE_ENDPOINTS')
  jq -e 'type=="array" and length==2' <<<"$ep" >/dev/null 2>&1 || { bad "$app endpoints JSON is not a 2-entry array"; continue; }
  for name in embed rerank; do
    svc="tei-$name"
    e=$(jq -c --arg n "$name" '.[]|select(.name==$n)' <<<"$ep")
    [ -n "$e" ] || { bad "$app endpoints JSON has no '$name' entry"; continue; }
    port=$(flag "$svc" --port)
    [ "$(jq -r .url <<<"$e")" = "http://$svc:$port" ] || bad "$app $name url '$(jq -r .url <<<"$e")' != http://$svc:$port"
    for pair in model:--model-id revision:--revision; do
      want=$(flag "$svc" "${pair##*:}")
      [ "$(jq -r ".${pair%%:*}" <<<"$e")" = "$want" ] && [ -n "$want" ] || bad "$app $name ${pair%%:*} '$(jq -r ".${pair%%:*}" <<<"$e")' != command '$want'"
    done
    [ "$(jq -r .boundary <<<"$e")" = "local" ] || bad "$app $name boundary != local"
  done
done

# Standalone base files: retrieval degrades to "not configured" and an external
# service can be passed straight through, with no overlay.
ext_env=$(mktemp)
{ cat "$env_file"; echo "QDRANT_URL=http://ext-qdrant:6333"; echo "RETRIEVAL_INFERENCE_ENDPOINTS=[]"; } > "$ext_env"
for f in docker-compose.dev.yml docker-compose.staging.yml docker-compose.prod.yml; do
  b=$(dc --env-file "$env_file" -f "$f" config --format json 2>/dev/null | jq '.services.backend.environment')
  for k in QDRANT_URL QDRANT_API_KEY QDRANT_COLLECTION_PREFIX RETRIEVAL_INFERENCE_ENDPOINTS RETRIEVAL_INFERENCE_API_KEY; do
    jq -e --arg k "$k" 'has($k)' <<<"$b" >/dev/null || bad "$f backend does not pass $k through"
  done
  [ "$(jq -r '.QDRANT_URL' <<<"$b")" = "" ] || bad "$f QDRANT_URL renders non-empty when unset"
  [ "$(jq -r '.RETRIEVAL_INFERENCE_ENDPOINTS' <<<"$b")" = "" ] || bad "$f RETRIEVAL_INFERENCE_ENDPOINTS renders non-empty when unset"
  b=$(dc --env-file "$ext_env" -f "$f" config --format json 2>/dev/null | jq '.services.backend.environment')
  [ "$(jq -r '.QDRANT_URL' <<<"$b")" = "http://ext-qdrant:6333" ] || bad "$f does not pass QDRANT_URL from .env"
  [ "$(jq -r '.RETRIEVAL_INFERENCE_ENDPOINTS' <<<"$b")" = "[]" ] || bad "$f does not pass RETRIEVAL_INFERENCE_ENDPOINTS from .env"
done
rm -f "$ext_env"

# RETRIEVAL_SEED_TIMEOUT widens the seeds' health window together with the
# orkestra.sh wait, from docker/.env or from the shell (which wins, as in
# orkestra.sh); the serving containers keep theirs.
seed_windows() {
  jq -r '[.services["tei-embed-seed","tei-rerank-seed","tei-embed","tei-rerank"].healthcheck.start_period]|join(" ")'
}
timeout_env=$(mktemp)
{ cat "$env_file"; echo "RETRIEVAL_SEED_TIMEOUT=3600"; } > "$timeout_env"
got=$(dc --env-file "$timeout_env" -f docker-compose.infra.yml -f docker-compose.dev.yml -f docker-compose.retrieval.yml \
  --profile retrieval-seed config --format json 2>/dev/null | seed_windows)
[ "$got" = "1h0m0s 1h0m0s 5m0s 5m0s" ] || bad "RETRIEVAL_SEED_TIMEOUT=3600 in .env renders start_period '$got'"
got=$(env -u QDRANT_URL -u QDRANT_API_KEY -u QDRANT_COLLECTION_PREFIX -u RETRIEVAL_INFERENCE_ENDPOINTS \
  -u RETRIEVAL_INFERENCE_API_KEY RETRIEVAL_SEED_TIMEOUT=5400 \
  docker compose --env-file "$timeout_env" -f docker-compose.infra.yml -f docker-compose.dev.yml -f docker-compose.retrieval.yml \
  --profile retrieval-seed config --format json 2>/dev/null | seed_windows)
[ "$got" = "1h30m0s 1h30m0s 5m0s 5m0s" ] || bad "shell RETRIEVAL_SEED_TIMEOUT=5400 renders start_period '$got'"
rm -f "$timeout_env"

# Fail-closed: the overlay refuses to render without either key.
for key in QDRANT_API_KEY RETRIEVAL_INFERENCE_API_KEY; do
  nokey_env=$(mktemp)
  grep -v "^${key}=" "$env_file" > "$nokey_env"
  if dc --env-file "$nokey_env" -f docker-compose.infra.yml -f docker-compose.dev.yml -f docker-compose.retrieval.yml config >/dev/null 2>&1; then
    bad "overlay renders without $key (must fail closed)"
  fi
  rm -f "$nokey_env"
done

[ "$fail" = 0 ] && echo "ok: retrieval overlay hardening present"
exit "$fail"
