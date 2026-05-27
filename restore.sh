#!/usr/bin/env bash
#
# restore.sh — Orkestra restore tool
#
# Restores a tarball produced by ./backup.sh into the local Orkestra
# stack. By default, replaces existing data — be careful.
#
# Components understood (only restored if present in the archive):
#   - mongodb        (mongorestore --drop --gzip --archive)
#   - redis          (stop redis, replace dump.rdb, start redis)
#   - rustfs / S3    (aws s3 sync FROM bundle TO bucket)
#   - memgraph       (replace volume contents)
#   - secrets        (write docker/.env and docker/keys/* — prompts for overwrite)
#
# Run without arguments for the TUI (lists available backups), or pass a
# tarball path for CLI use.
#
# Usage:
#   ./restore.sh                                    # TUI — pick a backup from ./backups/
#   ./restore.sh ./backups/orkestra-backup-*.tar.gz # restore from file
#   ./restore.sh <file> --components mongodb,redis  # subset
#   ./restore.sh <file> --dry-run                   # show what WOULD happen, change nothing
#   ./restore.sh <file> --yes                       # skip confirmation (DANGEROUS)
#   ./restore.sh --help

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
INPUT_PATH=""
SHOW_HELP=no
DRY_RUN=no

print_help() {
  awk '/^# restore\.sh/{f=1} f && !/^#/{exit} f' "$0" | sed 's/^# \{0,1\}//'
  echo "Available components: ${ALL_COMPONENTS[*]}"
}

POSITIONAL=()
while [ $# -gt 0 ]; do
  case "$1" in
    -y|--yes)            ASSUME_YES=yes; shift ;;
    -n|--dry-run)        DRY_RUN=yes; shift ;;
    -c|--components)     COMPONENTS_CSV="$2"; shift 2 ;;
    --components=*)      COMPONENTS_CSV="${1#*=}"; shift ;;
    -h|--help)           SHOW_HELP=yes; shift ;;
    --)                  shift; while [ $# -gt 0 ]; do POSITIONAL+=("$1"); shift; done ;;
    -*)                  err "unknown flag: $1"; print_help; exit 2 ;;
    *)                   POSITIONAL+=("$1"); shift ;;
  esac
done

if [ "$SHOW_HELP" = "yes" ]; then print_help; exit 0; fi
[ ${#POSITIONAL[@]} -ge 1 ] && INPUT_PATH="${POSITIONAL[0]}"

# ---------------------------------------------------------------------------
# Environment + tools
# ---------------------------------------------------------------------------
if ! command -v docker >/dev/null 2>&1; then
  err "docker is not installed or not on PATH"
  exit 1
fi

if [ -f "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  set -a; . "$ENV_FILE"; set +a
fi

container_running() {
  docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$1"
}

# ---------------------------------------------------------------------------
# Resolve backup file (TUI when missing)
# ---------------------------------------------------------------------------
if [ -z "$INPUT_PATH" ]; then
  step "Orkestra restore"
  if [ ! -d "$BACKUPS_DIR" ]; then
    err "no input file given and $BACKUPS_DIR does not exist"
    print_help; exit 2
  fi
  CANDIDATES=()
  while IFS= read -r f; do CANDIDATES+=("$f"); done < <(
    find "$BACKUPS_DIR" -maxdepth 1 -type f -name 'orkestra-backup-*.tar.gz' -printf '%T@ %p\n' 2>/dev/null \
      | sort -rn | awk '{print $2}'
  )
  if [ ${#CANDIDATES[@]} -eq 0 ]; then
    err "no backups found in $BACKUPS_DIR — run ./backup.sh first or pass a path"
    exit 1
  fi
  if [ -t 0 ] && [ -t 1 ]; then
    if command -v gum >/dev/null 2>&1; then
      INPUT_PATH="$(printf '%s\n' "${CANDIDATES[@]}" | gum choose --header='Pick a backup to restore')" || true
    else
      echo "  Available backups (newest first):"
      i=1
      for f in "${CANDIDATES[@]}"; do
        printf "    [%d] %s  %s\n" "$i" "$(basename "$f")" "$(du -h "$f" | cut -f1)"
        i=$((i+1))
      done
      echo
      printf "  > Pick number [1]: "
      read -r pick
      pick="${pick:-1}"
      idx=$((pick-1))
      if [ "$idx" -ge 0 ] && [ "$idx" -lt "${#CANDIDATES[@]}" ]; then
        INPUT_PATH="${CANDIDATES[$idx]}"
      else
        err "invalid selection"; exit 2
      fi
    fi
  else
    err "non-interactive shell — pass a backup file path"
    print_help; exit 2
  fi
fi

if [ -z "$INPUT_PATH" ] || [ ! -f "$INPUT_PATH" ]; then
  err "backup file not found: ${INPUT_PATH:-<empty>}"
  exit 1
fi

INPUT_PATH="$(cd "$(dirname "$INPUT_PATH")" && pwd)/$(basename "$INPUT_PATH")"
step "Restore source: $INPUT_PATH ($(du -h "$INPUT_PATH" | cut -f1))"

# ---------------------------------------------------------------------------
# Extract to staging and read manifest
# ---------------------------------------------------------------------------
STAGE="$(mktemp -d -t orkestra-restore.XXXXXX)"
trap 'rm -rf "$STAGE"' EXIT

tar xzf "$INPUT_PATH" -C "$STAGE"

if [ ! -f "$STAGE/manifest.json" ]; then
  err "manifest.json missing from archive — is this an orkestra-backup tarball?"
  exit 1
fi

MANIFEST_COMPONENTS=()
while IFS= read -r c; do
  [ -n "$c" ] && MANIFEST_COMPONENTS+=("$c")
done < <(
  # Extract the JSON array literal after "components": then pull names from inside it.
  grep -oE '"components"[[:space:]]*:[[:space:]]*\[[^]]*\]' "$STAGE/manifest.json" \
    | sed -E 's/^[^[]*\[//; s/\][^]]*$//' \
    | grep -oE '"[a-z]+"' | tr -d '"'
)

if [ ${#MANIFEST_COMPONENTS[@]} -eq 0 ]; then
  err "manifest declares no components"
  exit 1
fi

MANIFEST_ENV="$(grep -oE '"environment"[[:space:]]*:[[:space:]]*"[^"]+"' "$STAGE/manifest.json" | sed -E 's/.*"([^"]+)"$/\1/')"
MANIFEST_DATE="$(grep -oE '"createdAt"[[:space:]]*:[[:space:]]*"[^"]+"' "$STAGE/manifest.json" | sed -E 's/.*"([^"]+)"$/\1/')"

muted "  archive environment: ${MANIFEST_ENV:-unknown}"
muted "  archive timestamp:   ${MANIFEST_DATE:-unknown}"
muted "  archive components:  ${MANIFEST_COMPONENTS[*]}"

CURRENT_ENV="${ENV:-development}"
if [ -n "${MANIFEST_ENV:-}" ] && [ "$MANIFEST_ENV" != "$CURRENT_ENV" ]; then
  warn "archive was taken on '$MANIFEST_ENV' but current env is '$CURRENT_ENV'"
fi

# ---------------------------------------------------------------------------
# Component selection
# ---------------------------------------------------------------------------
SELECTED=()
if [ -n "$COMPONENTS_CSV" ]; then
  IFS=',' read -r -a SELECTED <<< "$COMPONENTS_CSV"
elif [ -t 0 ] && [ -t 1 ]; then
  echo
  if command -v gum >/dev/null 2>&1; then
    mapfile -t SELECTED < <(printf '%s\n' "${MANIFEST_COMPONENTS[@]}" | gum choose --no-limit --header='Select components to restore') || true
  else
    echo "  Select components to restore (space-separated numbers, 'a' for all):"
    i=1
    for c in "${MANIFEST_COMPONENTS[@]}"; do printf "    [%d] %s\n" "$i" "$c"; i=$((i+1)); done
    echo
    printf "  > "
    read -r answer
    if [ "$answer" = "a" ] || [ "$answer" = "all" ] || [ -z "$answer" ]; then
      SELECTED=("${MANIFEST_COMPONENTS[@]}")
    else
      for n in $answer; do
        idx=$((n-1))
        if [ "$idx" -ge 0 ] && [ "$idx" -lt "${#MANIFEST_COMPONENTS[@]}" ]; then
          SELECTED+=("${MANIFEST_COMPONENTS[$idx]}")
        fi
      done
    fi
  fi
else
  SELECTED=("${MANIFEST_COMPONENTS[@]}")
fi

if [ ${#SELECTED[@]} -eq 0 ]; then
  err "no components selected; aborting"
  exit 1
fi

# Validate selection: must be in BOTH ALL_COMPONENTS and the manifest
for c in "${SELECTED[@]}"; do
  case " ${ALL_COMPONENTS[*]} " in
    *" $c "*) ;;
    *) err "unknown component: $c"; exit 2 ;;
  esac
  case " ${MANIFEST_COMPONENTS[*]} " in
    *" $c "*) ;;
    *) err "component '$c' is not present in the archive"; exit 2 ;;
  esac
done

# ---------------------------------------------------------------------------
# Confirmation
# ---------------------------------------------------------------------------
echo
if [ "$DRY_RUN" = "yes" ]; then
  info "DRY RUN — no changes will be made"
  muted "would restore: ${SELECTED[*]}"
  echo
else
  warn "Restore will REPLACE existing data for: ${SELECTED[*]}"
  warn "Stop the backend before restoring to avoid concurrent writes:"
  muted "    ./orkestra.sh stop   (or pause your dev stack)"
  echo

  if [ "$ASSUME_YES" != "yes" ]; then
    if [ -t 0 ]; then
      printf "  Type 'restore' to confirm: "
      read -r confirm
      if [ "$confirm" != "restore" ]; then
        info "aborted"; exit 0
      fi
    else
      err "refusing to restore without --yes in a non-interactive shell"
      exit 2
    fi
  fi
fi

# ---------------------------------------------------------------------------
# Component implementations
# ---------------------------------------------------------------------------

restore_mongodb() {
  if ! container_running "$MONGO_CONTAINER"; then
    err "mongodb: container $MONGO_CONTAINER is not running — start infra first"
    return 1
  fi
  local archive="$STAGE/mongodb/mongo.archive.gz"
  if [ ! -f "$archive" ]; then
    err "mongodb: archive missing at $archive"; return 1
  fi
  local user="${MONGO_ROOT_USERNAME:-admin}"
  local pass="${MONGO_ROOT_PASSWORD:-}"
  local target_db="${MONGO_DATABASE:-orkestra}"
  if [ -z "$pass" ]; then err "MONGO_ROOT_PASSWORD not set in env"; return 1; fi

  # Read the source DB name from the bundle (older archives without
  # database.txt fall back to "orkestra" — matches the default).
  local source_db="orkestra"
  if [ -f "$STAGE/mongodb/database.txt" ]; then
    source_db="$(tr -d '[:space:]' < "$STAGE/mongodb/database.txt")"
    [ -z "$source_db" ] && source_db="orkestra"
  fi

  local -a ns_args=()
  if [ "$source_db" != "$target_db" ]; then
    warn "mongodb: archive was from '$source_db', restoring into '$target_db'"
    ns_args=(--nsFrom="${source_db}.*" --nsTo="${target_db}.*")
  fi

  if [ "$DRY_RUN" = "yes" ]; then
    step "mongodb: [dry-run] source db='$source_db' → target db='$target_db'"
    muted "archive size: $(du -h "$archive" | cut -f1)"
    muted "would run: mongorestore --archive --gzip --drop ${ns_args[*]}"
    # mongorestore --dryRun reads the archive and reports which collections
    # would be restored, without writing anything to the DB.
    docker exec -i "$MONGO_CONTAINER" \
      mongorestore --username "$user" --password "$pass" \
                   --authenticationDatabase admin \
                   --archive --gzip --drop --dryRun \
                   "${ns_args[@]}" \
      < "$archive" 2>&1 | sed 's/^/      /'
    return 0
  fi

  step "mongodb: restoring with mongorestore --drop (db=$target_db)"
  docker exec -i "$MONGO_CONTAINER" \
    mongorestore --username "$user" --password "$pass" \
                 --authenticationDatabase admin \
                 --archive --gzip --drop \
                 "${ns_args[@]}" \
    < "$archive"
  ok "mongodb: restored"
}

restore_redis() {
  if ! container_running "$REDIS_CONTAINER"; then
    err "redis: container $REDIS_CONTAINER is not running — start infra first"
    return 1
  fi
  local src="$STAGE/redis"
  if [ ! -d "$src" ]; then
    err "redis: directory missing at $src"; return 1
  fi

  # Discover where the live server keeps files (matches backup-time logic).
  local pass="${REDIS_PASSWORD:-}"
  local -a redis_exec=(docker exec -e "REDISCLI_AUTH=$pass" "$REDIS_CONTAINER" redis-cli --no-auth-warning)
  local r_dir r_rdb r_aofdir r_aoffile
  r_dir="$(   "${redis_exec[@]}" CONFIG GET dir            | tail -n +2)"
  r_rdb="$(   "${redis_exec[@]}" CONFIG GET dbfilename     | tail -n +2)"
  r_aofdir="$("${redis_exec[@]}" CONFIG GET appenddirname  | tail -n +2 || true)"
  r_aoffile="$("${redis_exec[@]}" CONFIG GET appendfilename | tail -n +2 || true)"
  r_dir="${r_dir%/}"

  if [ "$DRY_RUN" = "yes" ]; then
    step "redis: [dry-run] would write persistence files to '${r_dir:-/}' in $REDIS_CONTAINER"
    muted "container would be: stop → docker cp → start"
    if [ -f "$src/dump.rdb" ] && [ -n "$r_rdb" ]; then
      muted "  cp $src/dump.rdb → $REDIS_CONTAINER:${r_dir}/$r_rdb ($(du -h "$src/dump.rdb" | cut -f1))"
    fi
    if [ -d "$src/appendonlydir" ] && [ -n "$r_aofdir" ]; then
      muted "  cp $src/appendonlydir/ → $REDIS_CONTAINER:${r_dir}/$r_aofdir/ ($(du -sh "$src/appendonlydir" | cut -f1))"
    elif [ -f "$src/appendonly.aof" ] && [ -n "$r_aoffile" ]; then
      muted "  cp $src/appendonly.aof → $REDIS_CONTAINER:${r_dir}/$r_aoffile ($(du -h "$src/appendonly.aof" | cut -f1))"
    fi
    return 0
  fi

  step "redis: replacing persistence files at $r_dir (will restart container)"
  docker stop "$REDIS_CONTAINER" >/dev/null

  # Wipe any existing persistence so AOF and RDB don't conflict on restart.
  # We restart the container in stopped state, so we use a one-shot helper
  # to mutate the bind-mounted dir (or root if dir=/) inside the image.
  if [ "$r_dir" = "" ] || [ "$r_dir" = "/" ]; then
    docker start "$REDIS_CONTAINER" >/dev/null
    "${redis_exec[@]}" FLUSHALL ASYNC >/dev/null 2>&1 || true
    docker stop "$REDIS_CONTAINER" >/dev/null
  fi

  # Copy files back. docker cp into a stopped container is supported.
  if [ -f "$src/dump.rdb" ] && [ -n "$r_rdb" ]; then
    docker cp "$src/dump.rdb" "$REDIS_CONTAINER:$r_dir/$r_rdb"
  fi
  if [ -d "$src/appendonlydir" ] && [ -n "$r_aofdir" ]; then
    # Remove the existing dir contents first via a temp tar trick.
    docker cp "$src/appendonlydir/." "$REDIS_CONTAINER:$r_dir/$r_aofdir/" 2>/dev/null || \
      docker cp "$src/appendonlydir" "$REDIS_CONTAINER:$r_dir/$r_aofdir"
  elif [ -f "$src/appendonly.aof" ] && [ -n "$r_aoffile" ]; then
    docker cp "$src/appendonly.aof" "$REDIS_CONTAINER:$r_dir/$r_aoffile"
  fi

  docker start "$REDIS_CONTAINER" >/dev/null
  ok "redis: restored and restarted"
}

restore_rustfs() {
  if ! container_running "$RUSTFS_CONTAINER"; then
    err "rustfs: container $RUSTFS_CONTAINER is not running — start infra first"
    return 1
  fi
  local src="$STAGE/rustfs"
  if [ ! -d "$src" ]; then
    err "rustfs: bucket data missing at $src"; return 1
  fi
  local endpoint="http://${RUSTFS_CONTAINER}:9000"
  local access="${STORAGE_ACCESS_KEY:-${RUSTFS_ROOT_USER:-orkestra}}"
  local secret="${STORAGE_SECRET_KEY:-${RUSTFS_ROOT_PASSWORD:-changeme-rustfs}}"
  local region="${STORAGE_REGION:-us-east-1}"

  if [ "$DRY_RUN" = "yes" ]; then
    step "rustfs: [dry-run] would sync buckets to $endpoint"
    for d in "$src"/*/; do
      [ -d "$d" ] || continue
      local b
      b="$(basename "$d")"
      local count size
      count="$(find "$d" -type f | wc -l)"
      size="$(du -sh "$d" | cut -f1)"
      muted "  bucket '$b': $count file(s), $size — would create if missing + sync"
      # aws s3 sync --dryrun shows the per-object operations
      docker run --rm --network "$NETWORK" \
        -e AWS_ACCESS_KEY_ID="$access" \
        -e AWS_SECRET_ACCESS_KEY="$secret" \
        -e AWS_DEFAULT_REGION="$region" \
        -v "$d":/backup:ro \
        amazon/aws-cli:latest \
        --endpoint-url "$endpoint" s3 sync /backup "s3://$b" --dryrun 2>&1 \
        | sed 's/^/      /' | head -20
    done
    return 0
  fi

  step "rustfs: syncing buckets back from archive"
  for d in "$src"/*/; do
    [ -d "$d" ] || continue
    local b
    b="$(basename "$d")"
    # Best-effort bucket create (ignore "BucketAlreadyOwnedByYou" / "already exists")
    docker run --rm --network "$NETWORK" \
      -e AWS_ACCESS_KEY_ID="$access" \
      -e AWS_SECRET_ACCESS_KEY="$secret" \
      -e AWS_DEFAULT_REGION="$region" \
      amazon/aws-cli:latest \
      --endpoint-url "$endpoint" s3 mb "s3://$b" 2>/dev/null || true

    docker run --rm --network "$NETWORK" \
      -e AWS_ACCESS_KEY_ID="$access" \
      -e AWS_SECRET_ACCESS_KEY="$secret" \
      -e AWS_DEFAULT_REGION="$region" \
      -v "$d":/backup:ro \
      amazon/aws-cli:latest \
      --endpoint-url "$endpoint" s3 sync /backup "s3://$b" --only-show-errors
    ok "rustfs: synced bucket '$b'"
  done
}

restore_memgraph() {
  local archive="$STAGE/memgraph/memgraph-volume.tar.gz"
  if [ ! -f "$archive" ]; then
    err "memgraph: volume tarball missing at $archive"; return 1
  fi
  if [ "$DRY_RUN" = "yes" ]; then
    step "memgraph: [dry-run] would replace contents of volume $MEMGRAPH_VOLUME"
    muted "archive size: $(du -h "$archive" | cut -f1)"
    if container_running "$MEMGRAPH_CONTAINER"; then
      muted "container $MEMGRAPH_CONTAINER would be stopped, volume wiped + restored, then restarted"
    else
      muted "container $MEMGRAPH_CONTAINER is not running — volume would be wiped + restored in place"
    fi
    return 0
  fi
  step "memgraph: restoring volume $MEMGRAPH_VOLUME"
  local was_running=no
  if container_running "$MEMGRAPH_CONTAINER"; then
    docker stop "$MEMGRAPH_CONTAINER" >/dev/null
    was_running=yes
  fi
  # Wipe + extract — using a throwaway alpine container so we don't depend
  # on host tar against a docker-managed volume.
  docker run --rm \
    -v "$MEMGRAPH_VOLUME":/data \
    -v "$STAGE/memgraph":/backup:ro \
    alpine:latest \
    sh -c 'rm -rf /data/* /data/.[!.]* /data/..?* 2>/dev/null; tar xzf /backup/memgraph-volume.tar.gz -C /data'
  if [ "$was_running" = "yes" ]; then
    docker start "$MEMGRAPH_CONTAINER" >/dev/null
  fi
  ok "memgraph: volume restored"
}

restore_secrets() {
  local src="$STAGE/secrets"
  if [ ! -d "$src" ]; then
    err "secrets: directory missing at $src"; return 1
  fi
  if [ "$DRY_RUN" = "yes" ]; then
    step "secrets: [dry-run] would write the following files"
    if [ -f "$src/.env" ]; then
      if [ -f "$ENV_FILE" ]; then
        muted "  $ENV_FILE (exists — would prompt to overwrite)"
      else
        muted "  $ENV_FILE (new)"
      fi
    fi
    if [ -d "$src/keys" ]; then
      if [ -d "$KEYS_DIR" ]; then
        muted "  $KEYS_DIR/ (exists — would prompt to overwrite)"
      else
        muted "  $KEYS_DIR/ (new)"
      fi
      local f
      for f in "$src/keys"/*; do
        [ -e "$f" ] && muted "    + $(basename "$f")"
      done
    fi
    return 0
  fi
  step "secrets: writing docker/.env and docker/keys/*"

  if [ -f "$src/.env" ]; then
    if [ -f "$ENV_FILE" ] && [ "$ASSUME_YES" != "yes" ] && [ -t 0 ]; then
      printf "    %s.env exists. Overwrite? [y/N] %s" "$c_warn" "$c_reset"
      read -r ans
      case "$ans" in y|Y|yes|YES) ;; *) warn "skipped .env"; src_skip_env=yes ;; esac
    fi
    if [ "${src_skip_env:-no}" != "yes" ]; then
      mkdir -p "$DOCKER_DIR"
      cp -a "$src/.env" "$ENV_FILE"
      chmod 600 "$ENV_FILE"
      ok "secrets: wrote $ENV_FILE"
    fi
  fi

  if [ -d "$src/keys" ]; then
    if [ -d "$KEYS_DIR" ] && [ "$ASSUME_YES" != "yes" ] && [ -t 0 ]; then
      printf "    %skeys/ directory exists. Overwrite? [y/N] %s" "$c_warn" "$c_reset"
      read -r ans
      case "$ans" in y|Y|yes|YES) ;; *) warn "skipped keys/"; return 0 ;; esac
    fi
    mkdir -p "$KEYS_DIR"
    cp -a "$src/keys/." "$KEYS_DIR/"
    chmod -R go-rwx "$KEYS_DIR" 2>/dev/null || true
    ok "secrets: wrote $KEYS_DIR"
  fi
}

# ---------------------------------------------------------------------------
# Run
# ---------------------------------------------------------------------------
FAILED=()
for c in "${SELECTED[@]}"; do
  case "$c" in
    mongodb)  restore_mongodb  || FAILED+=("$c") ;;
    redis)    restore_redis    || FAILED+=("$c") ;;
    rustfs)   restore_rustfs   || FAILED+=("$c") ;;
    memgraph) restore_memgraph || FAILED+=("$c") ;;
    secrets)  restore_secrets  || FAILED+=("$c") ;;
  esac
done

echo
if [ ${#FAILED[@]} -gt 0 ]; then
  if [ "$DRY_RUN" = "yes" ]; then
    err "dry-run finished with failures: ${FAILED[*]}"
  else
    err "restore finished with failures: ${FAILED[*]}"
  fi
  exit 1
fi

if [ "$DRY_RUN" = "yes" ]; then
  ok "dry-run complete — no changes were made"
  info "To execute for real, re-run without --dry-run:"
  muted "  ./restore.sh $INPUT_PATH${COMPONENTS_CSV:+ --components $COMPONENTS_CSV}"
else
  ok "restore complete"
  info "Next steps:"
  muted "  • If secrets were restored, recreate containers so the new .env is loaded:"
  muted "        ./orkestra.sh deploy"
  muted "  • Smoke-test the backend: curl http://localhost:3000/health"
fi
