#!/usr/bin/env bash
#
# scripts/init.sh — bootstrap a fresh Orkestra checkout for `docker compose up`.
#
# What this does, idempotently:
#   1. Copy docker/.env.example → docker/.env (skip if .env already exists)
#   2. Fill REPLACE_WITH_RANDOM_HEX_* placeholders with `openssl rand` output
#   3. Seed a non-colliding host-port block (BACKEND_PORT, FRONTEND_PORT,
#      CLIENT_FRONTEND_PORT, MONGO_PORT, REDIS_PORT, RUSTFS_API_PORT,
#      RUSTFS_CONSOLE_PORT) based on ENV, so a second stack on the same host
#      doesn't collide with an already-running one
#   4. Generate RS256 JWT keys via scripts/generate-jwt-keys.sh (skip if present)
#   5. Print next steps
#
# Re-running is safe — existing files are preserved unless --force is passed.
# Invoke via `make init` or `./orkestra.sh init` (both delegate here).
#
# Usage:
#   scripts/init.sh                # interactive when overwrite needed
#   scripts/init.sh --force        # overwrite .env and JWT keys without asking
#   scripts/init.sh --yes          # answer "yes" to every prompt (CI-friendly)

set -eu

# ---------------------------------------------------------------------------
# Locate paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DOCKER_DIR="$REPO_ROOT/docker"
ENV_FILE="$DOCKER_DIR/.env"
ENV_TEMPLATE="$DOCKER_DIR/.env.example"
KEYS_DIR="$DOCKER_DIR/keys"
JWT_KEY="$KEYS_DIR/jwt-private.pem"
JWT_PUB="$KEYS_DIR/jwt-public.pem"

# ---------------------------------------------------------------------------
# Flags
# ---------------------------------------------------------------------------
FORCE=no
ASSUME_YES=no
while [ $# -gt 0 ]; do
  case "$1" in
    --force) FORCE=yes; ASSUME_YES=yes; shift ;;
    --yes|-y) ASSUME_YES=yes; shift ;;
    -h|--help)
      sed -n '3,22p' "$0"
      exit 0
      ;;
    *) echo "Unknown flag: $1" >&2; exit 2 ;;
  esac
done

# ---------------------------------------------------------------------------
# Output helpers (POSIX, no bashisms)
# ---------------------------------------------------------------------------
if [ -t 1 ]; then
  c_dim='\033[2m'; c_ok='\033[32m'; c_warn='\033[33m'; c_err='\033[31m'; c_bold='\033[1m'; c_reset='\033[0m'
else
  c_dim=''; c_ok=''; c_warn=''; c_err=''; c_bold=''; c_reset=''
fi

step()  { printf "${c_bold}==>${c_reset} %s\n" "$*"; }
ok()    { printf "    ${c_ok}✓${c_reset} %s\n" "$*"; }
warn()  { printf "    ${c_warn}!${c_reset} %s\n" "$*"; }
err()   { printf "${c_err}error:${c_reset} %s\n" "$*" >&2; }
muted() { printf "    ${c_dim}%s${c_reset}\n" "$*"; }

prompt_yes_no() {
  # $1 = question, $2 = default (y|n). Returns 0 on yes.
  local q="$1" def="${2:-n}" ans
  if [ "$ASSUME_YES" = "yes" ]; then
    return 0
  fi
  if [ "$def" = "y" ]; then
    printf "    %s [Y/n] " "$q"
  else
    printf "    %s [y/N] " "$q"
  fi
  read -r ans
  case "$ans" in
    "") [ "$def" = "y" ] && return 0 || return 1 ;;
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

# ---------------------------------------------------------------------------
# Tool prerequisites
# ---------------------------------------------------------------------------
require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { err "missing required command: $1 — install it and re-run"; exit 1; }
}

# ---------------------------------------------------------------------------
# Port helpers
# ---------------------------------------------------------------------------
# Echo $1 if the TCP port is free on the host, else the next free port above
# it. Lets two Orkestra stacks coexist on one host without colliding.
#
# $2 (optional) is a space-separated list of ports to also treat as taken —
# ports this same init run already claimed for an earlier var. Those aren't
# actually bound yet (nothing is listening until `docker compose up`), so
# `ss` alone can't see them; without this, two adjacent-but-both-occupied
# base ports (e.g. FRONTEND_PORT=8080 / CLIENT_FRONTEND_PORT=8081, both
# already in use) can each independently "advance" to the SAME free port.
next_free_port() {
  p="$1"
  claimed=" ${2:-} "
  have_ss=yes
  command -v ss >/dev/null 2>&1 || have_ss=no
  while :; do
    busy=no
    if [ "$have_ss" = "yes" ] && ss -ltnH "( sport = :$p )" 2>/dev/null | grep -q ":$p"; then
      busy=yes
    fi
    case "$claimed" in
      *" $p "*) busy=yes ;;
    esac
    [ "$busy" = "yes" ] || break
    p=$((p + 1))
  done
  printf '%s' "$p"
}

step "Checking prerequisites"
require_cmd openssl
ok "openssl present"
if command -v docker >/dev/null 2>&1; then
  ok "docker present"
else
  warn "docker not found on PATH — init will still scaffold env+keys, but you'll need docker to actually run the stack"
fi

# ---------------------------------------------------------------------------
# Sanity: template must exist
# ---------------------------------------------------------------------------
[ -f "$ENV_TEMPLATE" ] || { err "$ENV_TEMPLATE not found — is this an Orkestra checkout?"; exit 1; }

# ---------------------------------------------------------------------------
# Copy .env.example → .env (with overwrite guard)
# ---------------------------------------------------------------------------
step "Provisioning docker/.env"

if [ -f "$ENV_FILE" ]; then
  if [ "$FORCE" = "yes" ]; then
    warn "overwriting existing docker/.env (--force)"
  else
    warn "docker/.env already exists — leaving it alone"
    muted "re-run with --force to regenerate from .env.example (will overwrite your secrets)"
    SKIP_ENV=yes
  fi
fi

if [ "${SKIP_ENV:-no}" != "yes" ]; then
  cp "$ENV_TEMPLATE" "$ENV_FILE"
  ok "copied .env.example → .env"

  # Generate random secrets and substitute placeholders. POSIX-safe path:
  # write to a temp file then mv (avoids GNU vs BSD sed -i differences).
  step "Filling random secrets"
  cookie_secret=$(openssl rand -hex 32)
  oauth_key=$(openssl rand -hex 32)
  kms_key=$(openssl rand -hex 32)
  mongo_pw=$(openssl rand -hex 16)
  redis_pw=$(openssl rand -hex 16)

  tmp_env="${ENV_FILE}.tmp.$$"
  # sed delimiter `|` so the hex secrets don't collide with `/`.
  sed \
    -e "s|REPLACE_WITH_RANDOM_HEX_64_COOKIE_SECRET|${cookie_secret}|" \
    -e "s|REPLACE_WITH_RANDOM_HEX_64_OAUTH_ENCRYPTION|${oauth_key}|" \
    -e "s|REPLACE_WITH_RANDOM_HEX_64_KMS_MASTER|${kms_key}|" \
    -e "s|REPLACE_WITH_RANDOM_HEX_32_MONGO_PASSWORD|${mongo_pw}|" \
    -e "s|REPLACE_WITH_RANDOM_HEX_32_REDIS_PASSWORD|${redis_pw}|" \
    "$ENV_FILE" > "$tmp_env"

  # Sanity: every placeholder got replaced (otherwise the backend boot will
  # fail in mysterious ways much later).
  if grep -q "REPLACE_WITH_RANDOM_HEX" "$tmp_env"; then
    rm -f "$tmp_env"
    err "some REPLACE_WITH_RANDOM_HEX_* placeholders remained — see docker/.env.example"
    grep -n "REPLACE_WITH_RANDOM_HEX" "$ENV_TEMPLATE" >&2 || true
    exit 1
  fi

  mv "$tmp_env" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  ok "filled COOKIE_SECRET / OAUTH_TOKEN_ENCRYPTION_KEY / ORKESTRA_KMS_MASTER_KEY / MONGO_ROOT_PASSWORD / REDIS_PASSWORD"
  muted "chmod 600 applied — .env now contains live secrets"

  # Seed a non-colliding port block so a second Orkestra stack on this host
  # doesn't fight the first one over BACKEND_PORT/MONGO_PORT/etc. Per-ENV
  # base ports, bumped to the next free host port via next_free_port().
  # Observability ports (GRAFANA_PORT, PROMETHEUS_PORT, LOKI_PORT, OTEL_*,
  # TEMPO_*) are left at their .env.example defaults — that overlay is
  # opt-in, not part of the port block a fresh stack needs on first boot.
  step "Seeding non-colliding ports"
  env_val=$(grep -E '^ENV=' "$ENV_FILE" | head -1 | cut -d= -f2- | tr -d '[:space:]')
  : "${env_val:=development}"

  case "$env_val" in
    staging)
      base_backend=3100; base_frontend=8180; base_client=8181
      base_mongo=27117;  base_redis=6479;   base_rustfs_api=9200; base_rustfs_console=9201
      ;;
    production)
      base_backend=3200; base_frontend=8280; base_client=8281
      base_mongo=27217;  base_redis=6579;   base_rustfs_api=9300; base_rustfs_console=9301
      ;;
    development|*)
      base_backend=3000; base_frontend=8080; base_client=8081
      base_mongo=27017;  base_redis=6379;   base_rustfs_api=9100; base_rustfs_console=9101
      ;;
  esac

  port_specs="BACKEND_PORT:$base_backend FRONTEND_PORT:$base_frontend CLIENT_FRONTEND_PORT:$base_client MONGO_PORT:$base_mongo REDIS_PORT:$base_redis RUSTFS_API_PORT:$base_rustfs_api RUSTFS_CONSOLE_PORT:$base_rustfs_console"

  ports_tmp="${ENV_FILE}.ports.tmp.$$"
  cp "$ENV_FILE" "$ports_tmp"
  ports_claimed=""
  for spec in $port_specs; do
    port_var="${spec%%:*}"
    port_base="${spec#*:}"
    port_free=$(next_free_port "$port_base" "$ports_claimed")
    ports_claimed="$ports_claimed $port_free"
    sed "s|^${port_var}=.*|${port_var}=${port_free}|" "$ports_tmp" > "${ports_tmp}.next"
    mv "${ports_tmp}.next" "$ports_tmp"
    muted "${port_var}=${port_free}"
  done
  mv "$ports_tmp" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  ok "seeded non-colliding ports for ENV=${env_val}"
fi

# ---------------------------------------------------------------------------
# JWT keys
# ---------------------------------------------------------------------------
step "Provisioning RS256 JWT keys"

if [ -f "$JWT_KEY" ] && [ -f "$JWT_PUB" ]; then
  if [ "$FORCE" = "yes" ]; then
    warn "regenerating JWT keys (--force) — all existing tokens will be invalidated"
    rm -f "$JWT_KEY" "$JWT_PUB"
  else
    ok "JWT keys already present at $KEYS_DIR/jwt-{private,public}.pem"
    muted "re-run with --force to regenerate (invalidates every issued token)"
    SKIP_JWT=yes
  fi
fi

if [ "${SKIP_JWT:-no}" != "yes" ]; then
  # Delegate to the existing generator so there's a single source of truth.
  if [ -x "$SCRIPT_DIR/generate-jwt-keys.sh" ]; then
    "$SCRIPT_DIR/generate-jwt-keys.sh" >/dev/null
  else
    bash "$SCRIPT_DIR/generate-jwt-keys.sh" >/dev/null
  fi
  ok "generated jwt-private.pem (chmod 600) + jwt-public.pem"
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
printf '\n'
printf "${c_bold}✨ Orkestra bootstrap complete.${c_reset}\n\n"
printf "Next steps:\n\n"
printf "  ${c_bold}1.${c_reset} Bring up infrastructure (MongoDB + Redis):\n"
printf "       cd docker && docker compose -f docker-compose.infra.yml up -d\n\n"
printf "  ${c_bold}2.${c_reset} Bring up the backend + frontends (dev stack, hot reload):\n"
printf "       docker compose -f docker-compose.dev.yml --env-file .env up -d\n"
printf "       ${c_dim}# or just run ./orkestra.sh — it auto-detects ENV from docker/.env${c_reset}\n\n"
printf "  ${c_bold}3.${c_reset} Mint an admin dev token and log in:\n"
printf "       cd .. && ORKESTRA_API_URL=http://localhost:3000 ./scripts/devtoken.sh administrator\n\n"
printf "  ${c_bold}4.${c_reset} Configure OAuth / SMTP / Stripe / AI keys (optional):\n"
printf "       Open http://localhost:3000 and visit /admin/modules.\n"
printf "       Email/password login works without any of these.\n\n"
printf "Or skip steps 1–2 and use the TUI: ${c_bold}./orkestra.sh${c_reset}\n"
