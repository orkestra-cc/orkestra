#!/usr/bin/env bash
#
# backup.sh — Orkestra backup tool
#
# Bundles the project's stateful surfaces into a single tarball:
#   - MongoDB        (logical dump via mongodump)
#   - Redis          (RDB snapshot via BGSAVE + docker cp)
#   - RustFS / S3    (object bucket sync via aws-cli over the stack's own network)
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
#   ./backup.sh --require mongodb,redis          # fail (exit 3) if any listed
#                                                 # component is unavailable or
#                                                 # fails to capture, instead of
#                                                 # silently downgrading to a
#                                                 # partial backup
#   ./backup.sh --help
#
# `all` implies `--require mongodb,redis,rustfs,secrets` unless you pass your
# own --require explicitly — a bare `all` must mean every component or fail,
# never "every component that happened to be up".

set -Eeuo pipefail

# The tarball this script produces carries docker/.env (DB passwords, the KMS
# master key) and docker/keys/* (the RS256 JWT private key). A restrictive
# umask here is the single point that guarantees every file and directory
# this script creates — the backups/ dir, the tarball itself, the staging
# dir — comes out non-group/world-readable regardless of the ambient shell's
# umask (a host running 0002 would otherwise leave a fresh tarball
# world-readable, -rw-rw-r--, until something later chmod's it explicitly).
umask 077

# ---------------------------------------------------------------------------
# Paths and constants
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
DOCKER_DIR="$REPO_ROOT/docker"
ENV_FILE="${ORKESTRA_ENV_FILE:-$DOCKER_DIR/.env}"
KEYS_DIR="$DOCKER_DIR/keys"
BACKUPS_DIR="$REPO_ROOT/backups"

# Container/network names are stack-namespaced (${APP_NAME}-<svc>-${ENV} and
# ${APP_NAME}-${ENV}_default) — resolved below, once docker/.env is sourced.
MONGO_CONTAINER=""
REDIS_CONTAINER=""
RUSTFS_CONTAINER=""
NETWORK=""

ALL_COMPONENTS=(mongodb redis rustfs secrets)

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
REQUIRE_CSV=""

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
    --require)           REQUIRE_CSV="$2"; shift 2 ;;
    --require=*)         REQUIRE_CSV="${1#*=}"; shift ;;
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

# Remember any caller-supplied overrides, source the env file, then restore
# them — a multi-stack host must be able to target one stack per invocation.
_cli_app_name="${APP_NAME:-}"
_cli_env="${ENV:-}"
# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a
[ -n "$_cli_app_name" ] && APP_NAME="$_cli_app_name"
[ -n "$_cli_env" ] && ENV="$_cli_env"
ENV_NAME="${ENV:-development}"

# Every stack is one Compose project named ${APP_NAME}-${ENV}; containers are
# ${APP_NAME}-<svc>-${ENV} and the project's own default network is
# ${APP_NAME}-${ENV}_default (the old shared `orkestra-network` bridge was
# removed — see docs/superpowers/specs/2026-07-05-multi-stack-isolation-design.md).
: "${APP_NAME:=orkestra}"
STACK="${APP_NAME}-${ENV_NAME}"
MONGO_CONTAINER="${APP_NAME}-mongodb-${ENV_NAME}"
REDIS_CONTAINER="${APP_NAME}-redis-${ENV_NAME}"
RUSTFS_CONTAINER="${APP_NAME}-rustfs-${ENV_NAME}"
NETWORK="${STACK}_default"

container_running() {
  docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$1"
}

available_components() {
  local out=()
  container_running "$MONGO_CONTAINER"   && out+=(mongodb)
  container_running "$REDIS_CONTAINER"   && out+=(redis)
  container_running "$RUSTFS_CONTAINER"  && out+=(rustfs)
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

# `all` means every component, not "every component that happens to be up".
# Without this, a stack rename or a stopped container silently downgrades the
# run to secrets-only and still exits 0 — see backups/ 2026-07-12.
if [ -z "$REQUIRE_CSV" ] && [ "$MODE_ALL" = "yes" ]; then
  REQUIRE_CSV="mongodb,redis,rustfs,secrets"
fi

REQUIRED=()
if [ -n "$REQUIRE_CSV" ]; then
  IFS=',' read -r -a REQUIRED <<< "$REQUIRE_CSV"
  missing=()
  for want in "${REQUIRED[@]}"; do
    found=no
    for have in "${SELECTED[@]}"; do [ "$have" = "$want" ] && found=yes && break; done
    [ "$found" = "no" ] && missing+=("$want")
  done
  if [ ${#missing[@]} -gt 0 ]; then
    err "required component(s) unavailable: ${missing[*]}"
    err "expected containers: ${MONGO_CONTAINER}, ${REDIS_CONTAINER}, ${RUSTFS_CONTAINER}"
    err "refusing to write a partial backup — start the stack, or pass an explicit --components subset"
    exit 3
  fi
fi

# ---------------------------------------------------------------------------
# Prepare output
# ---------------------------------------------------------------------------
TIMESTAMP="$(date -u +%Y%m%d-%H%M%S)"
if [ -z "$OUTPUT_PATH" ]; then
  mkdir -p "$BACKUPS_DIR"
  # mkdir -p is a no-op on a directory that already exists, so it won't
  # tighten permissions left over from before `umask 077` was introduced
  # (this host's backups/ was drwxrwxr-x under the ambient 0002 umask) —
  # chmod it explicitly every run rather than relying on it having been
  # created correctly once.
  chmod 700 "$BACKUPS_DIR"
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
  # Checked explicitly (not left as the function's implicit last-command
  # status): backup_mongodb is invoked as `backup_mongodb && ... || true`,
  # and under `set -e` a failing command that isn't the last one in a
  # function called from an &&/|| list is silently swallowed — mongodump
  # would fail (e.g. stale MONGO_ROOT_PASSWORD) but the function would still
  # fall through to `ok` and report success with a 0-byte archive.
  if ! docker exec -i "$MONGO_CONTAINER" \
    mongodump --username "$user" --password "$pass" \
              --authenticationDatabase admin \
              --db "$db" \
              --archive --gzip \
    > "$out/mongo.archive.gz"; then
    err "mongodb: mongodump failed (bad credentials, unreachable db, disk full?) — see output above"
    return 1
  fi
  if [ ! -s "$out/mongo.archive.gz" ]; then
    err "mongodb: mongodump produced an empty archive"
    return 1
  fi
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
  # Checked explicitly against the reply text, not the process exit status:
  # confirmed empirically that `redis-cli` exits 0 even on a WRONGPASS/NOAUTH
  # reply (the connection and the CLI invocation "succeeded"; only the
  # in-band Redis reply signals the failure). A successful SAVE's only
  # reply is the literal string "OK". Checking this matters because a
  # *stale* dump.rdb from a previous successful save could still be sitting
  # on disk and pass the file-exists check below, masking today's failure
  # with yesterday's data.
  local save_out
  save_out="$("${redis_exec[@]}" SAVE 2>&1)"
  if [ "$save_out" != "OK" ]; then
    err "redis: SAVE did not return OK: $save_out"
    return 1
  fi

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
  # Same precedence as docker-compose.infra.yml: an explicit RustFS root wins,
  # else the backend's pair IS the root. No literal fallback — a missing pair
  # is a config error, never a reason to try the literal the base once shipped.
  local access="${RUSTFS_ROOT_USER:-${STORAGE_ACCESS_KEY:-}}"
  local secret="${RUSTFS_ROOT_PASSWORD:-${STORAGE_SECRET_KEY:-}}"
  if [ -z "$access" ] || [ -z "$secret" ]; then
    err "rustfs: no storage credentials in docker/.env (STORAGE_ACCESS_KEY/STORAGE_SECRET_KEY, or RUSTFS_ROOT_USER/RUSTFS_ROOT_PASSWORD)"
    return 1
  fi
  local bucket="${STORAGE_BUCKET:-orkestra-avatars}"

  # List buckets so we capture everything, not just the default one. The
  # list-buckets exit status is checked explicitly now (previously folded
  # into `|| echo "$bucket"`) so a genuine list-buckets *failure* (endpoint
  # unreachable, bad credentials — a real problem, worth surfacing if the
  # fallback sync below also fails) can be told apart from a list-buckets
  # *success* that legitimately found none (see below).
  local buckets list_rc=0
  buckets="$(docker run --rm --network "$NETWORK" \
    -e AWS_ACCESS_KEY_ID="$access" \
    -e AWS_SECRET_ACCESS_KEY="$secret" \
    -e AWS_DEFAULT_REGION="${STORAGE_REGION:-us-east-1}" \
    amazon/aws-cli:latest \
    --endpoint-url "$endpoint" s3api list-buckets --query 'Buckets[].Name' --output text 2>/dev/null)" || list_rc=$?

  if [ "$list_rc" -ne 0 ]; then
    # list-buckets itself failed. Unchanged from before this fix: fall back
    # to the single configured default bucket and let the sync loop below
    # decide — deliberately NOT adding an existence pre-check here, because
    # a failed HEAD (bucket missing) and a failed HEAD (bad credentials /
    # no permission) return the same non-zero exit status and can't be told
    # apart without parsing AWS's error text. Since we couldn't even list
    # buckets, we have no independent signal that credentials are good, so
    # the safer default is to attempt the sync and let a real failure
    # surface via the existing sync_failed check below (this exact path —
    # bad STORAGE_SECRET_KEY — is what tests/backup/test_rustfs_capture_failure.sh
    # exercises).
    warn "rustfs: list-buckets failed (endpoint unreachable or bad credentials?) — will attempt the configured default bucket only: $bucket"
    buckets="$bucket"
  elif [ -z "$buckets" ]; then
    # list-buckets *succeeded* and genuinely found none. Per-domain buckets
    # are provisioned lazily on first use, not guaranteed to exist at boot
    # (backend/internal/shared/config/config.go:346 STORAGE_ENSURE_BUCKET;
    # docker/.env.example:298-299) — a fresh or lightly-used install can
    # legitimately have zero buckets. That is a successful, empty capture,
    # not a failure: forcing a sync against a bucket nothing ever created
    # would always fail with NoSuchBucket and take an otherwise-good
    # mongodb/redis/secrets backup down with it (backup.sh's post-execution
    # --require check trusts INCLUDED, and would drop the whole run to a
    # hard failure over a bucket that was simply never provisioned).
    ok "rustfs: no buckets exist yet on this install — nothing to capture"
    return 0
  fi

  # A bucket that's merely absent never reaches this loop when list-buckets
  # itself succeeded — `buckets` came straight from a live listing, so every
  # `$b` here is a bucket rustfs itself says exists right now, and a sync
  # failure always means real data in a real bucket wasn't captured. In the
  # list-buckets-failed fallback case (single default bucket, unconfirmed)
  # a NoSuchBucket-style sync failure is indistinguishable from a real
  # credentials/connectivity failure — both correctly fail the component;
  # see the comment above.
  local sync_failed=no synced_any=no
  for b in $buckets; do
    mkdir -p "$out/$b"
    # Checked explicitly: confirmed empirically (bad credentials against the
    # live endpoint) that aws-cli's own exit status DOES propagate through
    # `docker run` on a sync failure — unlike redis-cli's in-band error
    # reporting, this one behaves as expected, but it still can't be left
    # unchecked as the loop's last statement under set -e's &&/|| exemption.
    # Run the sync as the invoking user: the image defaults to root, which leaves
    # root-owned files in $STAGE that a non-root caller cannot delete on EXIT
    # (and that end up root-owned inside the tarball). aws-cli needs a writable
    # HOME for its cache, so point it at the container's /tmp.
    if docker run --rm --network "$NETWORK" \
      --user "$(id -u):$(id -g)" -e HOME=/tmp \
      -e AWS_ACCESS_KEY_ID="$access" \
      -e AWS_SECRET_ACCESS_KEY="$secret" \
      -e AWS_DEFAULT_REGION="${STORAGE_REGION:-us-east-1}" \
      -v "$out/$b":/backup \
      amazon/aws-cli:latest \
      --endpoint-url "$endpoint" s3 sync "s3://$b" /backup --only-show-errors; then
      ok "rustfs: synced bucket '$b' ($(du -sh "$out/$b" | cut -f1))"
      synced_any=yes
    else
      err "rustfs: failed to sync bucket '$b' — see aws-cli output above"
      sync_failed=yes
    fi
  done

  if [ "$sync_failed" = "yes" ]; then
    err "rustfs: one or more buckets failed to sync — refusing to report this component as captured"
    return 1
  fi

  if [ "$synced_any" = "no" ]; then
    ok "rustfs: no existing buckets found to sync — nothing to capture"
  fi
}

backup_secrets() {
  step "secrets: copying docker/.env and docker/keys/*"
  local out="$STAGE/data/secrets"
  mkdir -p "$out"

  # docker/.env is mandatory (the script already refused to start without it
  # — see the check near the top — but that was minutes/hours before this
  # runs in a cron context, so re-check rather than assume). A copy/chmod
  # failure here (disk full, permissions) must not fall through unchecked.
  if [ ! -f "$ENV_FILE" ]; then
    err "secrets: $ENV_FILE is missing"
    return 1
  fi
  if ! cp -a "$ENV_FILE" "$out/.env"; then
    err "secrets: failed to copy $ENV_FILE"
    return 1
  fi
  if ! chmod 600 "$out/.env"; then
    err "secrets: failed to chmod $out/.env"
    return 1
  fi

  # docker/keys/* is optional — a fresh install may not have generated keys
  # yet, and that absence is not a failure. But once the directory exists,
  # a copy failure is exactly as serious as the mandatory .env case above.
  if [ -d "$KEYS_DIR" ]; then
    if ! cp -a "$KEYS_DIR" "$out/keys"; then
      err "secrets: failed to copy $KEYS_DIR"
      return 1
    fi
    chmod -R go-rwx "$out/keys" 2>/dev/null || true
  else
    muted "secrets: $KEYS_DIR not present, skipping (optional)"
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
    secrets)  backup_secrets  && INCLUDED+=("$c") || true ;;
  esac
done

if [ ${#INCLUDED[@]} -eq 0 ]; then
  err "no components were backed up successfully"
  exit 1
fi

# The pre-execution --require check above only proves a container was
# reachable at selection time; it can't see a capture that starts and then
# fails (bad credentials, disk full, mongodump erroring out, ...) — that
# failure is swallowed into a plain skip by the `backup_X && INCLUDED+=(...)
# || true` idiom below. Re-check REQUIRED against what actually landed in
# INCLUDED so a required component's capture failure is a hard error too,
# and is distinguishable in cron logs from "the stack was down".
if [ ${#REQUIRED[@]} -gt 0 ]; then
  captured_missing=()
  for want in "${REQUIRED[@]}"; do
    found=no
    for have in "${INCLUDED[@]}"; do [ "$have" = "$want" ] && found=yes && break; done
    [ "$found" = "no" ] && captured_missing+=("$want")
  done
  if [ ${#captured_missing[@]} -gt 0 ]; then
    err "required component(s) failed to capture: ${captured_missing[*]}"
    err "the container(s) were present at selection time, but the backup step itself did not succeed — see the errors/warnings above"
    err "refusing to report success on a partial backup"
    exit 3
  fi
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
# Belt and braces on top of `umask 077` above: guarantee the mode even if
# OUTPUT_PATH landed somewhere with an inherited ACL/default-mode that
# ignores umask (e.g. a directory with a POSIX default ACL).
chmod 600 "$OUTPUT_PATH"
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
