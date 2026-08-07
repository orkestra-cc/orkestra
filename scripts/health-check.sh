#!/usr/bin/env bash
#
# scripts/health-check.sh — post-deploy verification for an Orkestra stack.
#
#   bash scripts/health-check.sh <env> [scope]
#
#   <env>    development | staging | production
#   [scope]  all (default) | backend | frontend-admin | frontend-admin+backend | infra
#
# Exit 0 when every in-scope service is healthy, 1 otherwise. Services outside
# the deploy scope are still probed but only ever produce a warning, so a
# backend-only deploy is not failed by an unrelated, pre-existing frontend
# problem. Scope is optional: `orkestra.sh status` calls without one and gets
# the full-stack view.
#
# Container names are resolved as ${APP_NAME}-<svc>-${ENV} from docker/.env —
# never assumed. One machine routinely runs several stacks from different
# checkouts (APP_NAME is per-checkout and arbitrary), so a guessed name inspects
# some other clone's containers and reports health for code nobody deployed.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$PROJECT_ROOT/docker/.env"

# shellcheck source=scripts/env-file.sh
. "$SCRIPT_DIR/env-file.sh"

# --- presentation (mirrors orkestra.sh, degrades on dumb terminals) ---------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    c_reset=$'\033[0m'; c_success=$'\033[38;5;114m'
    c_warn=$'\033[38;5;215m'; c_error=$'\033[38;5;203m'; c_muted=$'\033[38;5;244m'
else
    c_reset=""; c_success=""; c_warn=""; c_error=""; c_muted=""
fi
case "${LANG:-}${LC_ALL:-}" in
    *UTF-8*|*utf8*) ic_ok="✓"; ic_warn="⚠"; ic_err="✗" ;;
    *)              ic_ok="+"; ic_warn="!"; ic_err="x" ;;
esac

p_ok()    { printf '%s%s%s  %s\n' "$c_success" "$ic_ok"   "$c_reset" "$*"; }
p_warn()  { printf '%s%s%s  %s\n' "$c_warn"    "$ic_warn" "$c_reset" "$*"; }
p_err()   { printf '%s%s%s  %s\n' "$c_error"   "$ic_err"  "$c_reset" "$*" >&2; }
p_muted() { printf '%s%s%s\n'     "$c_muted"   "$*"       "$c_reset"; }

# --- arguments --------------------------------------------------------------
ENV_NAME="${1:-}"
SCOPE="${2:-all}"

if [ -z "$ENV_NAME" ]; then
    p_err "usage: health-check.sh <env> [scope]"
    exit 2
fi
case "$ENV_NAME" in
    development|staging|production) ;;
    *) p_err "unknown environment: $ENV_NAME"; exit 2 ;;
esac

if ! command -v docker >/dev/null 2>&1; then
    p_err "docker not found on PATH"
    exit 2
fi

# --- stack identity ---------------------------------------------------------
APP_NAME="$(env_get "$ENV_FILE" APP_NAME)"; : "${APP_NAME:=orkestra}"
BACKEND_PORT="$(env_get "$ENV_FILE" BACKEND_PORT)"; : "${BACKEND_PORT:=3000}"
FRONTEND_PORT="$(env_get "$ENV_FILE" FRONTEND_PORT)"; : "${FRONTEND_PORT:=8080}"
CLIENT_PORT="$(env_get "$ENV_FILE" CLIENT_FRONTEND_PORT)"; : "${CLIENT_PORT:=8081}"

STACK="${APP_NAME}-${ENV_NAME}"
container_for() { printf '%s-%s-%s' "$APP_NAME" "$1" "$ENV_NAME"; }

FAILURES=0

# in_scope SERVICE — did this deploy touch it? Decides fail vs warn.
in_scope() {
    case "$SCOPE" in
        all) return 0 ;;
        backend) [ "$1" = "backend" ] ;;
        frontend-admin) [ "$1" = "frontend-admin" ] ;;
        frontend-admin+backend) [ "$1" = "backend" ] || [ "$1" = "frontend-admin" ] ;;
        infra) case "$1" in mongodb|redis|rustfs) return 0 ;; *) return 1 ;; esac ;;
        *) return 0 ;;
    esac
}

# record SERVICE OK MESSAGE — in-scope failures are fatal, the rest are noise
# worth seeing (this is how a service broken by an earlier deploy surfaces).
record() {
    local svc=$1 ok=$2 msg=$3
    if [ "$ok" = "yes" ]; then
        p_ok "$msg"
    elif in_scope "$svc"; then
        p_err "$msg"
        FAILURES=$((FAILURES + 1))
    else
        p_warn "$msg (outside deploy scope)"
    fi
}

container_state()  { docker inspect -f '{{.State.Status}}' "$1" 2>/dev/null; }
container_health() {
    docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$1" 2>/dev/null
}

# http_ok URL — retries for ~21s. Vite reports "ready in ~12s" on a cold start
# and a refused connection fails instantly, so a short budget just races the
# frontend's boot. Deploys sleep 30 beforehand and hit this on the first try;
# the budget only costs time when a service is genuinely down. Sets HTTP_CODE.
HTTP_PROBE_ATTEMPTS=8
HTTP_PROBE_INTERVAL=3
HTTP_CODE=""
http_ok() {
    local url=$1 i
    for i in $(seq 1 "$HTTP_PROBE_ATTEMPTS"); do
        # curl already prints 000 on a connection failure; do not append another.
        HTTP_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$url" 2>/dev/null)" || true
        [ -n "$HTTP_CODE" ] || HTTP_CODE="000"
        [ "$HTTP_CODE" = "200" ] && return 0
        [ "$i" -lt "$HTTP_PROBE_ATTEMPTS" ] && sleep "$HTTP_PROBE_INTERVAL"
    done
    return 1
}

# check_app SERVICE PORT [optional] — container state, then an HTTP probe.
# frontend-admin declares no Docker healthcheck, so HTTP is its only real
# signal; checking container state alone would pass with the app broken.
check_app() {
    local svc=$1 port=$2 optional=${3:-no} name state health
    name="$(container_for "$svc")"
    state="$(container_state "$name")"

    if [ -z "$state" ]; then
        if [ "$optional" = "optional" ]; then
            p_muted "    $svc — not defined in this stack, skipped"
        else
            record "$svc" no "$svc: container $name not found"
        fi
        return
    fi
    if [ "$state" != "running" ]; then
        record "$svc" no "$svc: container is $state"
        return
    fi

    health="$(container_health "$name")"
    if [ "$health" = "unhealthy" ]; then
        record "$svc" no "$svc: container reports unhealthy"
        return
    fi

    if http_ok "http://127.0.0.1:${port}/health"; then
        record "$svc" yes "$svc: running, /health 200"
    else
        record "$svc" no "$svc: /health on 127.0.0.1:${port} returned ${HTTP_CODE}"
    fi
}

# check_infra SERVICE — Docker health only; these expose no HTTP health route.
check_infra() {
    local svc=$1 name state health
    name="$(container_for "$svc")"
    state="$(container_state "$name")"

    if [ -z "$state" ]; then
        p_muted "    $svc — not defined in this stack, skipped"
        return
    fi
    if [ "$state" != "running" ]; then
        record "$svc" no "$svc: container is $state"
        return
    fi

    health="$(container_health "$name")"
    case "$health" in
        healthy) record "$svc" yes "$svc: running (healthy)" ;;
        none)    record "$svc" yes "$svc: running (no healthcheck declared)" ;;
        *)       record "$svc" no  "$svc: container reports $health" ;;
    esac
}

# --- run --------------------------------------------------------------------
p_muted "    stack ${STACK} · scope ${SCOPE}"

check_app backend "$BACKEND_PORT"
check_app frontend-admin "$FRONTEND_PORT"
check_app client-frontend "$CLIENT_PORT" optional

check_infra mongodb
check_infra redis
check_infra rustfs

if [ "$FAILURES" -gt 0 ]; then
    p_err "$FAILURES in-scope service(s) unhealthy"
    exit 1
fi
exit 0
