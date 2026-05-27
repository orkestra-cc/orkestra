#!/usr/bin/env bash
#
# backup.sh — Orkestra backup tool
#
# Bundles the project's stateful surfaces into a single tarball:
#   - MongoDB        (logical dump via mongodump)
#   - Redis          (RDB snapshot via BGSAVE + docker cp)
#   - RustFS / S3    (object bucket sync via aws-cli over the orkestra network)
#   - Memgraph       (raw volume snapshot — only if the container is up)
#   - Secrets        (docker/.env and docker/keys/*)
#
# Run without arguments for an interactive TUI, or pass flags for CLI use.
#
# Usage:
#   ./backup.sh                                  # TUI
#   ./backup.sh all                              # back up everything available
#   ./backup.sh --components mongodb,redis       # subset
#   ./backup.sh --output /tmp/snap.tar.gz        # custom output path
#   ./backup.sh --yes all                        # no prompts (CI / cron)
#   ./backup.sh --help

set -Eeuo pipefail

# ---------------------------------------------------------------------------
# Paths and constants
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
DOCKER_DIR="$REPO_ROOT/docker"
ENV_FILE="$DOCKER_DIR/.env"
KEYS_DIR="$DOCKER_DIR/keys"
BACKUPS_DIR="$REPO_ROOT/backups"

MONGO_CONTAINER="orkestra-mongodb"
REDIS_CONTAINER="orkestra-redis"
RUSTFS_CONTAINER="orkestra-rustfs"
MEMGRAPH_CONTAINER="orkestra-memgraph"
MEMGRAPH_VOLUME="orkestra-memgraph-data"
NETWORK="orkestra-network"

ALL_COMPONENTS=(mongodb redis rustfs memgraph secrets)

# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  c_bold=$'\033[1m'; c_dim=$'\033[2m'; c_ok=$'\033[32m'
  c_warn=$'\033[33m'; c_err=$'\033[31m'; c_info=$'\033[36m'; c_reset=$'\033[0m'
else
  c_bold=''; c_dim=''; c_ok=''; c_warn=''; c_err=''; c_info=''; c_reset=''
fi

step()  { printf "%s==>%s %s\n" "$c_bold" "$c_reset" "$*"; }
ok()    { printf "    %s✓%s %s\n" "$c_ok" "$c_reset" "$*"; }
warn()  { printf "    %s!%s %s\n" "$c_warn" "$c_reset" "$*"; }
err()   { printf "%serror:%s %s\n" "$c_err" "$c_reset" "$*" >&2; }
info()  { printf "    %s%s%s\n" "$c_info" "$*" "$c_reset"; }
muted() { printf "    %s%s%s\n" "$c_dim" "$*" "$c_reset"; }

# ---------------------------------------------------------------------------
# CLI parsing
# ---------------------------------------------------------------------------
ASSUME_YES=no
COMPONENTS_CSV=""
OUTPUT_PATH=""
MODE_ALL=no
SHOW_HELP=no

print_help() {
  awk '/^# backup\.sh/{f=1} f && !/^#/{exit} f' "$0" | sed 's/^# \{0,1\}//'
  echo "Available components: ${ALL_COMPONENTS[*]}"
}

POSITIONAL=()
while [ $# -gt 0 ]; do
  case "$1" in
    -y|--yes)            ASSUME_YES=yes; shift ;;
    -c|--components)     COMPONENTS_CSV="$2"; shift 2 ;;
    --components=*)      COMPONENTS_CSV="${1#*=}"; shift ;;
    -o|--output)         OUTPUT_PATH="$2"; shift 2 ;;
    --output=*)          OUTPUT_PATH="${1#*=}"; shift ;;
    all)                 MODE_ALL=yes; shift ;;
    -h|--help)           SHOW_HELP=yes; shift ;;
    --)                  shift; while [ $# -gt 0 ]; do POSITIONAL+=("$1"); shift; done ;;
    -*)                  err "unknown flag: $1"; print_help; exit 2 ;;
    *)                   POSITIONAL+=("$1"); shift ;;
  esac
done

if [ "$SHOW_HELP" = "yes" ]; then print_help; exit 0; fi

# ---------------------------------------------------------------------------
# Environment + container checks
# ---------------------------------------------------------------------------
if ! command -v docker >/dev/null 2>&1; then
  err "docker is not installed or not on PATH"
  exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
  err "$ENV_FILE not found — run ./scripts/init.sh first"
  exit 1
fi

# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a
ENV_NAME="${ENV:-development}"

container_running() {
  docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$1"
}

available_components() {
  local out=()
  container_running "$MONGO_CONTAINER"   && out+=(mongodb)
  container_running "$REDIS_CONTAINER"   && out+=(redis)
  container_running "$RUSTFS_CONTAINER"  && out+=(rustfs)
  container_running "$MEMGRAPH_CONTAINER" && out+=(memgraph)
  [ -f "$ENV_FILE" ] || [ -d "$KEYS_DIR" ] && out+=(secrets)
  printf '%s\n' "${out[@]}"
}

# ---------------------------------------------------------------------------
# TUI / component selection
# ---------------------------------------------------------------------------
AVAILABLE=()
while IFS= read -r line; do AVAILABLE+=("$line"); done < <(available_components)

if [ ${#AVAILABLE[@]} -eq 0 ]; then
  err "no backup-able components detected (no infra containers running, no secrets present)"
  err "start the stack first: ./orkestra.sh"
  exit 1
fi

SELECTED=()

if [ -n "$COMPONENTS_CSV" ]; then
  IFS=',' read -r -a SELECTED <<< "$COMPONENTS_CSV"
elif [ "$MODE_ALL" = "yes" ]; then
  SELECTED=("${AVAILABLE[@]}")
elif [ -t 0 ] && [ -t 1 ]; then
  # Interactive TUI
  step "Orkestra backup ($ENV_NAME)"
  muted "available components: ${AVAILABLE[*]}"
  echo
  if command -v gum >/dev/null 2>&1; then
    mapfile -t SELECTED < <(printf '%s\n' "${AVAILABLE[@]}" | gum choose --no-limit --header="Select components to back up (space to toggle, enter to confirm)") || true
  else
    echo "  Select components to include (space-separated numbers, 'a' for all):"
    local_i=1
    for c in "${AVAILABLE[@]}"; do printf "    [%d] %s\n" "$local_i" "$c"; local_i=$((local_i+1)); done
    echo
    printf "  > "
    read -r answer
    if [ "$answer" = "a" ] || [ "$answer" = "all" ] || [ -z "$answer" ]; then
      SELECTED=("${AVAILABLE[@]}")
    else
      for n in $answer; do
        idx=$((n-1))
        if [ "$idx" -ge 0 ] && [ "$idx" -lt "${#AVAILABLE[@]}" ]; then
          SELECTED+=("${AVAILABLE[$idx]}")
        fi
      done
    fi
  fi
else
  err "non-interactive shell — pass 'all' or --components <list>"
  print_help
  exit 2
fi

if [ ${#SELECTED[@]} -eq 0 ]; then
  err "no components selected; aborting"
  exit 1
fi

# Validate selection against ALL_COMPONENTS
for c in "${SELECTED[@]}"; do
  case " ${ALL_COMPONENTS[*]} " in
    *" $c "*) ;;
    *) err "unknown component: $c (valid: ${ALL_COMPONENTS[*]})"; exit 2 ;;
  esac
done

# Warn for any selected component that's not currently available
for c in "${SELECTED[@]}"; do
  case " ${AVAILABLE[*]} " in
    *" $c "*) ;;
    *) warn "component '$c' is not currently available (container not running?) — skipping" ;;
  esac
done

# ---------------------------------------------------------------------------
# Prepare output
# ---------------------------------------------------------------------------
TIMESTAMP="$(date -u +%Y%m%d-%H%M%S)"
if [ -z "$OUTPUT_PATH" ]; then
  mkdir -p "$BACKUPS_DIR"
  OUTPUT_PATH="$BACKUPS_DIR/orkestra-backup-${ENV_NAME}-${TIMESTAMP}.tar.gz"
fi

# Resolve to absolute path so we can stage in a temp dir and still write it correctly
OUTPUT_DIR="$(cd "$(dirname "$OUTPUT_PATH")" 2>/dev/null && pwd)" || {
  err "output directory does not exist: $(dirname "$OUTPUT_PATH")"; exit 1; }
OUTPUT_PATH="$OUTPUT_DIR/$(basename "$OUTPUT_PATH")"

step "Backing up ${SELECTED[*]} to $OUTPUT_PATH"

if [ "$ASSUME_YES" != "yes" ] && [ -t 0 ]; then
  printf "  Proceed? [Y/n] "
  read -r confirm
  case "$confirm" in n|N|no|NO) info "aborted"; exit 0 ;; esac
fi

STAGE="$(mktemp -d -t orkestra-backup.XXXXXX)"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$STAGE/data"
MANIFEST="$STAGE/data/manifest.json"

# ---------------------------------------------------------------------------
# Component implementations
# ---------------------------------------------------------------------------

backup_mongodb() {
  if ! container_running "$MONGO_CONTAINER"; then
    warn "mongodb: container not running, skipping"; return 1
  fi
  local out="$STAGE/data/mongodb"
  mkdir -p "$out"
  local user="${MONGO_ROOT_USERNAME:-admin}"
  local pass="${MONGO_ROOT_PASSWORD:-}"
  local db="${MONGO_DATABASE:-orkestra}"
  if [ -z "$pass" ]; then err "MONGO_ROOT_PASSWORD not set in $ENV_FILE"; return 1; fi
  step "mongodb: dumping database '$db' with mongodump"
  # Scope to the application DB only — skips mongo's admin/local/config
  # system DBs and the throwaway orkestra_openapi_dump sandbox that
  # `make openapi-dump` creates to serialize the OpenAPI schema.
  docker exec -i "$MONGO_CONTAINER" \
    mongodump --username "$user" --password "$pass" \
              --authenticationDatabase admin \
              --db "$db" \
              --archive --gzip \
    > "$out/mongo.archive.gz"
  # Stash the DB name alongside the archive so restore knows where to put it.
  printf '%s\n' "$db" > "$out/database.txt"
  ok "mongodb: $(du -h "$out/mongo.archive.gz" | cut -f1) → mongodb/mongo.archive.gz (db=$db)"
}

backup_redis() {
  if ! container_running "$REDIS_CONTAINER"; then
    warn "redis: container not running, skipping"; return 1
  fi
  step "redis: forcing SAVE and capturing persistence files"
  local out="$STAGE/data/redis"
  mkdir -p "$out"
  local pass="${REDIS_PASSWORD:-}"

  # Use REDISCLI_AUTH so the password never appears in `ps` or in shell
  # quoting hell. Empty string means no auth.
  local -a redis_exec=(docker exec -e "REDISCLI_AUTH=$pass" "$REDIS_CONTAINER" redis-cli --no-auth-warning)

  # Synchronous SAVE so any RDB-enabled deployments have a current snapshot.
  "${redis_exec[@]}" SAVE >/dev/null

  # Ask the live server where it actually writes persistence (the running
  # config can differ from defaults — e.g. redis-stack-server in our infra
  # uses `dir=/` instead of `/data`).
  local r_dir r_rdb r_aofdir r_aoffile
  r_dir="$(   "${redis_exec[@]}" CONFIG GET dir          | tail -n +2)"
  r_rdb="$(   "${redis_exec[@]}" CONFIG GET dbfilename   | tail -n +2)"
  r_aofdir="$("${redis_exec[@]}" CONFIG GET appenddirname | tail -n +2 || true)"
  r_aoffile="$("${redis_exec[@]}" CONFIG GET appendfilename | tail -n +2 || true)"
  r_dir="${r_dir%/}"

  muted "redis dir=$r_dir dbfilename=$r_rdb appenddirname=${r_aofdir:-<none>}"

  # RDB
  if [ -n "$r_rdb" ] && docker exec "$REDIS_CONTAINER" test -f "$r_dir/$r_rdb" 2>/dev/null; then
    docker cp "$REDIS_CONTAINER:$r_dir/$r_rdb" "$out/dump.rdb"
  fi

  # AOF (Redis 7+: directory). Older: single appendonly.aof file.
  if [ -n "$r_aofdir" ] && docker exec "$REDIS_CONTAINER" test -d "$r_dir/$r_aofdir" 2>/dev/null; then
    # Only copy if non-empty (AOF may be configured but unused).
    if [ -n "$(docker exec "$REDIS_CONTAINER" ls -A "$r_dir/$r_aofdir" 2>/dev/null)" ]; then
      docker cp "$REDIS_CONTAINER:$r_dir/$r_aofdir" "$out/appendonlydir"
    fi
  elif [ -n "$r_aoffile" ] && docker exec "$REDIS_CONTAINER" test -f "$r_dir/$r_aoffile" 2>/dev/null; then
    docker cp "$REDIS_CONTAINER:$r_dir/$r_aoffile" "$out/appendonly.aof"
  fi

  # Record the live dir so restore can put files back in the right place.
  printf 'dir=%s\ndbfilename=%s\nappenddirname=%s\nappendfilename=%s\n' \
    "$r_dir" "$r_rdb" "$r_aofdir" "$r_aoffile" > "$out/redis-layout.txt"

  if [ ! -s "$out/dump.rdb" ] && [ ! -e "$out/appendonlydir" ] && [ ! -e "$out/appendonly.aof" ]; then
    err "redis: no persistence files captured (dir=$r_dir)"; return 1
  fi
  ok "redis: $(du -sh "$out" | cut -f1) → redis/"
}

backup_rustfs() {
  if ! container_running "$RUSTFS_CONTAINER"; then
    warn "rustfs: container not running, skipping"; return 1
  fi
  step "rustfs: syncing S3 buckets"
  local out="$STAGE/data/rustfs"
  mkdir -p "$out"
  local endpoint="http://${RUSTFS_CONTAINER}:9000"
  local access="${STORAGE_ACCESS_KEY:-${RUSTFS_ROOT_USER:-orkestra}}"
  local secret="${STORAGE_SECRET_KEY:-${RUSTFS_ROOT_PASSWORD:-changeme-rustfs}}"
  local bucket="${STORAGE_BUCKET:-orkestra-avatars}"

  # List buckets so we capture everything, not just the default one.
  local buckets
  buckets="$(docker run --rm --network "$NETWORK" \
    -e AWS_ACCESS_KEY_ID="$access" \
    -e AWS_SECRET_ACCESS_KEY="$secret" \
    -e AWS_DEFAULT_REGION="${STORAGE_REGION:-us-east-1}" \
    amazon/aws-cli:latest \
    --endpoint-url "$endpoint" s3api list-buckets --query 'Buckets[].Name' --output text 2>/dev/null || echo "$bucket")"

  if [ -z "$buckets" ]; then
    warn "rustfs: no buckets returned from list-buckets, falling back to STORAGE_BUCKET=$bucket"
    buckets="$bucket"
  fi

  for b in $buckets; do
    mkdir -p "$out/$b"
    docker run --rm --network "$NETWORK" \
      -e AWS_ACCESS_KEY_ID="$access" \
      -e AWS_SECRET_ACCESS_KEY="$secret" \
      -e AWS_DEFAULT_REGION="${STORAGE_REGION:-us-east-1}" \
      -v "$out/$b":/backup \
      amazon/aws-cli:latest \
      --endpoint-url "$endpoint" s3 sync "s3://$b" /backup --only-show-errors
    ok "rustfs: synced bucket '$b' ($(du -sh "$out/$b" | cut -f1))"
  done
}

backup_memgraph() {
  if ! container_running "$MEMGRAPH_CONTAINER"; then
    warn "memgraph: container not running (lifecycle-managed by graph module), skipping"; return 1
  fi
  step "memgraph: snapshotting volume $MEMGRAPH_VOLUME"
  local out="$STAGE/data/memgraph"
  mkdir -p "$out"
  docker run --rm \
    -v "$MEMGRAPH_VOLUME":/data:ro \
    -v "$out":/backup \
    alpine:latest \
    tar czf /backup/memgraph-volume.tar.gz -C /data . 2>/dev/null
  ok "memgraph: $(du -h "$out/memgraph-volume.tar.gz" | cut -f1) → memgraph/memgraph-volume.tar.gz"
}

backup_secrets() {
  step "secrets: copying docker/.env and docker/keys/*"
  local out="$STAGE/data/secrets"
  mkdir -p "$out"
  if [ -f "$ENV_FILE" ]; then
    cp -a "$ENV_FILE" "$out/.env"
    chmod 600 "$out/.env"
  fi
  if [ -d "$KEYS_DIR" ]; then
    cp -a "$KEYS_DIR" "$out/keys"
    chmod -R go-rwx "$out/keys" 2>/dev/null || true
  fi
  ok "secrets: $(du -sh "$out" | cut -f1) → secrets/"
  warn "secrets bundle contains JWT keys and DB passwords — store the resulting tarball securely"
}

# ---------------------------------------------------------------------------
# Run selected components, collecting which actually ran
# ---------------------------------------------------------------------------
INCLUDED=()
for c in "${SELECTED[@]}"; do
  case "$c" in
    mongodb)  backup_mongodb  && INCLUDED+=("$c") || true ;;
    redis)    backup_redis    && INCLUDED+=("$c") || true ;;
    rustfs)   backup_rustfs   && INCLUDED+=("$c") || true ;;
    memgraph) backup_memgraph && INCLUDED+=("$c") || true ;;
    secrets)  backup_secrets  && INCLUDED+=("$c") || true ;;
  esac
done

if [ ${#INCLUDED[@]} -eq 0 ]; then
  err "no components were backed up successfully"
  exit 1
fi

# ---------------------------------------------------------------------------
# Manifest + tarball
# ---------------------------------------------------------------------------
{
  printf '{\n'
  printf '  "schema": "orkestra-backup/v1",\n'
  printf '  "createdAt": "%s",\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '  "environment": "%s",\n' "$ENV_NAME"
  printf '  "host": "%s",\n' "$(hostname)"
  printf '  "components": ['
  local_first=yes
  for c in "${INCLUDED[@]}"; do
    [ "$local_first" = "yes" ] && local_first=no || printf ','
    printf '"%s"' "$c"
  done
  printf ']\n'
  printf '}\n'
} > "$MANIFEST"

step "Creating tarball"
tar czf "$OUTPUT_PATH" -C "$STAGE/data" .
SIZE="$(du -h "$OUTPUT_PATH" | cut -f1)"
ok "wrote $OUTPUT_PATH ($SIZE)"
muted "components included: ${INCLUDED[*]}"

# ---------------------------------------------------------------------------
# Footer
# ---------------------------------------------------------------------------
echo
info "Next steps:"
muted "  • Verify:  tar tzf $OUTPUT_PATH | head"
muted "  • Restore: ./restore.sh $OUTPUT_PATH"
