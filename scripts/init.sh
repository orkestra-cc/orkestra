#!/usr/bin/env bash
#
# scripts/init.sh — bootstrap a fresh Orkestra checkout for `docker compose up`.
#
# What this does, idempotently:
#   1. Copy docker/.env.example → docker/.env (skip if .env already exists)
#   2. Fill REPLACE_WITH_RANDOM_HEX_* placeholders with `openssl rand` output
#   3. Generate RS256 JWT keys via scripts/generate-jwt-keys.sh (skip if present)
#   4. Ensure the `orkestra-network` Docker bridge exists
#   5. Print next steps
#
# Re-running is safe — existing files are preserved unless --force is passed.
# Invoke via `make init` or `./orkestra.sh init` (both delegate here).
#
# Usage:
#   scripts/init.sh                # interactive when overwrite needed
#   scripts/init.sh --force        # overwrite .env and JWT keys without asking
#   scripts/init.sh --yes          # answer "yes" to every prompt (CI-friendly)
#   scripts/init.sh --skip-network # don't try to create the docker network

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
SKIP_NETWORK=no
while [ $# -gt 0 ]; do
  case "$1" in
    --force) FORCE=yes; ASSUME_YES=yes; shift ;;
    --yes|-y) ASSUME_YES=yes; shift ;;
    --skip-network) SKIP_NETWORK=yes; shift ;;
    -h|--help)
      sed -n '3,20p' "$0"
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
# Docker network (skippable for CI / offline)
# ---------------------------------------------------------------------------
if [ "$SKIP_NETWORK" != "yes" ] && command -v docker >/dev/null 2>&1; then
  step "Ensuring orkestra-network bridge exists"
  if docker network inspect orkestra-network >/dev/null 2>&1; then
    ok "orkestra-network already exists"
  else
    if docker network create orkestra-network >/dev/null 2>&1; then
      ok "created orkestra-network"
    else
      warn "couldn't create orkestra-network (docker daemon down? permission issue?) — create manually before `docker compose up`"
    fi
  fi
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
printf '\n'
printf "${c_bold}✨ Orkestra bootstrap complete.${c_reset}\n\n"
printf "Next steps:\n\n"
printf "  ${c_bold}1.${c_reset} Bring up infrastructure (MongoDB + Redis):\n"
printf "       cd docker && docker compose -f docker-compose.infra.yml up -d\n\n"
printf "  ${c_bold}2.${c_reset} Bring up the backend (pick a SKU profile):\n"
printf "       docker compose -f docker-compose.starter.yml --env-file .env up -d\n"
printf "       ${c_dim}# or billing | ai | saas | enterprise${c_reset}\n\n"
printf "  ${c_bold}3.${c_reset} Mint an admin dev token and log in:\n"
printf "       cd .. && ORKESTRA_API_URL=http://localhost:3000 ./scripts/devtoken.sh administrator\n\n"
printf "  ${c_bold}4.${c_reset} Configure OAuth / SMTP / Stripe / AI keys (optional):\n"
printf "       Open http://localhost:3000 and visit /admin/modules.\n"
printf "       Email/password login works without any of these.\n\n"
printf "Or skip steps 1–2 and use the TUI: ${c_bold}./orkestra.sh${c_reset}\n"
