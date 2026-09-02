#!/usr/bin/env bash

# ============================================================================
# Orkestra — Unified Stack Management
# ============================================================================
#
# Single entry point for managing the Orkestra stack. Combines the former
# deploy.sh and logs.sh scripts with first-class SKU-profile support.
#
# Two modes:
#
#   Interactive TUI     ./orkestra.sh
#                       Profile menu (SKU profile / full stack) then a per-profile
#                       operations menu.
#
#   Non-interactive CLI ./orkestra.sh <command> [args...]
#                       Scriptable subcommands. See ./orkestra.sh --help.
#
# Graceful enhancements:
#   - Auto-detects gum (charm.sh) and fzf; uses them when available for a more
#     polished experience, falls back to pure bash otherwise.
#   - Honors NO_COLOR and disables colors when stdout is not a TTY.
#
# Backward compat:
#   ENV=development|staging|production ./orkestra.sh
#     Skips the profile menu and opens the full-stack TUI directly.
# ============================================================================

set -Eeuo pipefail
IFS=$'\n\t'

# Application version (TUI header + every Vite SPA + the backend
# `/health` response) is computed once below, after SCRIPT_DIR is
# defined, and exported as `ORKESTRA_VERSION` so docker-compose
# substitution picks it up.

# ---------------------------------------------------------------------------
# Capability detection
# ---------------------------------------------------------------------------

HAS_TTY=false
HAS_COLOR=false
HAS_UNICODE=true
HAS_GUM=false
HAS_FZF=false
TERM_COLS=80

detect_capabilities() {
    # TTY detection — no colors / fancy rendering when piped
    if [ -t 1 ]; then
        HAS_TTY=true
    fi

    # Honor https://no-color.org
    if [ -z "${NO_COLOR:-}" ] && [ "$HAS_TTY" = true ]; then
        # 256-color support (xterm-256color, screen-256color, etc.)
        case "${TERM:-}" in
            *-256color | *-color | xterm* | screen* | tmux* | alacritty | kitty)
                HAS_COLOR=true
                ;;
        esac
    fi

    # Terminal width (with fallback)
    if command -v tput > /dev/null 2>&1 && [ "$HAS_TTY" = true ]; then
        TERM_COLS=$(tput cols 2>/dev/null || echo 80)
    fi

    # Optional enhancers
    command -v gum > /dev/null 2>&1 && HAS_GUM=true
    command -v fzf > /dev/null 2>&1 && HAS_FZF=true

    # Assume Unicode support unless the locale clearly disables it
    case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
        *UTF-8* | *utf8* | *UTF8*) HAS_UNICODE=true ;;
        C | POSIX | "") HAS_UNICODE=false ;;
    esac
}

detect_capabilities

# ---------------------------------------------------------------------------
# Theme (semantic colors, 256-color ANSI)
# ---------------------------------------------------------------------------

if [ "$HAS_COLOR" = true ]; then
    c_reset=$'\033[0m'
    c_bold=$'\033[1m'
    c_dim=$'\033[2m'
    c_italic=$'\033[3m'
    c_underline=$'\033[4m'

    # Semantic palette (256-color)
    c_success=$'\033[38;5;114m'   # soft green
    c_warn=$'\033[38;5;215m'      # amber
    c_error=$'\033[38;5;203m'     # coral red
    c_info=$'\033[38;5;117m'      # sky blue
    c_muted=$'\033[38;5;244m'     # gray
    c_accent=$'\033[38;5;141m'    # soft purple
    c_header=$'\033[38;5;111m'    # steel blue
    c_prompt=$'\033[38;5;222m'    # warm yellow
    c_border=$'\033[38;5;240m'    # dim gray for box borders
else
    c_reset="" c_bold="" c_dim="" c_italic="" c_underline=""
    c_success="" c_warn="" c_error="" c_info="" c_muted=""
    c_accent="" c_header="" c_prompt="" c_border=""
fi

# ---------------------------------------------------------------------------
# Icons (Unicode glyphs for consistent log prefixes)
# ---------------------------------------------------------------------------

if [ "$HAS_UNICODE" = true ]; then
    ic_step="▸"
    ic_ok="✓"
    ic_err="✗"
    ic_warn="⚠"
    ic_info="ℹ"
    ic_arrow="→"
    ic_dot="●"
    ic_bullet="•"
    ic_box_tl="╭" ic_box_tr="╮" ic_box_bl="╰" ic_box_br="╯"
    ic_box_h="─" ic_box_v="│" ic_box_l="├" ic_box_r="┤"
else
    ic_step=">" ic_ok="+" ic_err="x" ic_warn="!" ic_info="i"
    ic_arrow="->" ic_dot="*" ic_bullet="-"
    ic_box_tl="+" ic_box_tr="+" ic_box_bl="+" ic_box_br="+"
    ic_box_h="-" ic_box_v="|" ic_box_l="+" ic_box_r="+"
fi

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"

# Application version → exported for docker-compose `${ORKESTRA_VERSION}`
# substitution. The container has no git, so the host computes this once
# from `git describe` and passes it through. Caller can pre-set
# ORKESTRA_VERSION to override (CI does this implicitly via GITHUB_REF_NAME
# at image-build time, but operators may want a manual override too).
if [ -z "${ORKESTRA_VERSION:-}" ]; then
    if command -v git > /dev/null 2>&1 \
        && git -C "$PROJECT_ROOT" rev-parse --git-dir > /dev/null 2>&1; then
        # --match "v[0-9]*" restricts to UPSTREAM tags — a clone's own
        # release tags ("<clone>-v*", e.g. commons-v0.1.0) must NOT be
        # picked up here, or they'd shadow the real base version.
        ORKESTRA_VERSION=$(git -C "$PROJECT_ROOT" describe --tags --match "v[0-9]*" --always --dirty 2>/dev/null | sed 's/^v//')
    fi
    ORKESTRA_VERSION=${ORKESTRA_VERSION:-dev}
fi
export ORKESTRA_VERSION
DOCKER_DIR="$PROJECT_ROOT/docker"

# Infra compose file — layered alongside the per-ENV app compose file (and,
# when active, the observability overlay) into ONE Compose project per
# stack (COMPOSE_PROJECT_NAME=${APP_NAME}-${ENV}; see resolve_stack_identity).
INFRA_COMPOSE="$DOCKER_DIR/docker-compose.infra.yml"

ENV_FILE="$DOCKER_DIR/.env"

# ADR-0005 Phase D — self-hosted observability stack (Loki, Tempo,
# Prometheus, Promtail, otel-collector, Grafana). Orthogonal to the
# SKU profile / full-stack split: you typically run it alongside
# whichever app stack is up.
OBSERVABILITY_COMPOSE="$DOCKER_DIR/docker-compose.observability.yml"

# Active stack — "fullstack" or "" (at the main menu). ADR-0006 removed the
# runtime-profile (minimal/full) path — there are no addons to pre-enable.
PROFILE=""

# Compose service names — uniform across every env file now (dev included;
# Tasks 2-7 normalized dev's service names to match staging/prod). Shared by
# fullstack_execute_deploy() and fullstack_stop().
BACKEND_SERVICE="backend"
FRONTEND_SERVICE="frontend-admin"
CLIENT_SERVICE="client-frontend"

# Env-detect utility for full-stack ENV resolution
source "$PROJECT_ROOT/scripts/env-detect.sh"

# Pure KEY=value read/write helpers (env_get / env_set) for the setup wizard.
source "$PROJECT_ROOT/scripts/env-file.sh"

# ---------------------------------------------------------------------------
# Traps — restore terminal state on exit, handle Ctrl-C gracefully
# ---------------------------------------------------------------------------

_restore_terminal() {
    # Always print the reset sequence, even if colors were disabled (no-op string)
    printf '%s' "$c_reset"
    # Re-show cursor in case a spinner hid it
    if [ "$HAS_TTY" = true ] && command -v tput > /dev/null 2>&1; then
        tput cnorm 2>/dev/null || true
    fi
}

_on_exit() {
    local code=$?
    _restore_terminal
    exit "$code"
}

_on_int() {
    _restore_terminal
    printf '\n%s%s Interrupted.%s\n' "$c_warn" "$ic_warn" "$c_reset" >&2
    exit 130
}

_on_err() {
    local code=$?
    local line=$1
    _restore_terminal
    if [ "$code" -ne 0 ] && [ "$code" -ne 130 ]; then
        printf '\n%s%s Error (exit %d) on line %d%s\n' \
            "$c_error" "$ic_err" "$code" "$line" "$c_reset" >&2
    fi
    exit "$code"
}

trap _on_exit EXIT
trap _on_int INT
trap '_on_err $LINENO' ERR

# ---------------------------------------------------------------------------
# Core printing primitives
# ---------------------------------------------------------------------------

term_width() {
    if [ "$HAS_TTY" = true ]; then
        tput cols 2>/dev/null || echo 80
    else
        echo 80
    fi
}

# Clamp terminal width for boxes (min 60, max 100 to stay readable)
box_width() {
    local w
    w=$(term_width)
    if [ "$w" -lt 60 ]; then
        echo 60
    elif [ "$w" -gt 100 ]; then
        echo 100
    else
        echo "$w"
    fi
}

p_info() { printf '%s%s%s  %s\n' "$c_info" "$ic_info" "$c_reset" "$*"; }
p_step() { printf '%s%s%s  %s\n' "$c_accent" "$ic_step" "$c_reset" "$*"; }
p_ok() { printf '%s%s%s  %s\n' "$c_success" "$ic_ok" "$c_reset" "$*"; }
p_warn() { printf '%s%s%s  %s\n' "$c_warn" "$ic_warn" "$c_reset" "$*"; }
p_err() { printf '%s%s%s  %s\n' "$c_error" "$ic_err" "$c_reset" "$*" >&2; }
p_muted() { printf '%s%s%s\n' "$c_muted" "$*" "$c_reset"; }
p_section() {
    local title=$1
    printf '\n%s%s%s %s%s\n' "$c_accent" "$c_bold" "$title" "$c_reset" ""
    printf '%s' "$c_muted"
    local i n
    n=${#title}
    for ((i = 0; i < n; i++)); do printf '%s' "$ic_box_h"; done
    printf '%s\n' "$c_reset"
}

die() {
    p_err "$*"
    exit 1
}

pause_for_return() {
    printf '\n%s%s Press Enter to continue...%s' "$c_muted" "$ic_arrow" "$c_reset"
    read -r _
}

# ---------------------------------------------------------------------------
# Box drawing (rounded borders, dynamic width)
# ---------------------------------------------------------------------------

# Repeat a string N times
_repeat() {
    local str=$1 count=$2 i
    for ((i = 0; i < count; i++)); do printf '%s' "$str"; done
}

# Strip ANSI escape sequences to count visible width
_visible_width() {
    local stripped
    # shellcheck disable=SC2001
    stripped=$(printf '%s' "$1" | sed 's/\x1b\[[0-9;]*[mGKH]//g')
    # Count characters (rough — assumes ASCII/1-wide Unicode)
    printf '%d' "${#stripped}"
}

# Pad a line to a target visible width
_pad_line() {
    local text=$1 target=$2
    local visible
    visible=$(_visible_width "$text")
    if [ "$visible" -lt "$target" ]; then
        printf '%s' "$text"
        _repeat ' ' $((target - visible))
    else
        printf '%s' "$text"
    fi
}

# draw_box <title> <line1> <line2> ...
# Renders a rounded box with the given title and body lines.
draw_box() {
    local title=$1
    shift
    local width
    width=$(box_width)
    local inner=$((width - 4)) # 2 border chars + 1 space padding each side

    # Top border with embedded title
    local title_text=" $title "
    local title_visible
    title_visible=$(_visible_width "$title_text")
    local dash_right=$((width - title_visible - 3))
    [ "$dash_right" -lt 2 ] && dash_right=2

    printf '%s%s%s' "$c_border" "$ic_box_tl" "$ic_box_h"
    printf '%s%s%s' "$c_reset$c_bold$c_header" "$title_text" "$c_reset$c_border"
    _repeat "$ic_box_h" "$dash_right"
    printf '%s%s\n' "$ic_box_tr" "$c_reset"

    # Body lines
    local line
    for line in "$@"; do
        printf '%s%s%s ' "$c_border" "$ic_box_v" "$c_reset"
        _pad_line "$line" "$inner"
        printf ' %s%s%s\n' "$c_border" "$ic_box_v" "$c_reset"
    done

    # Bottom border
    printf '%s%s' "$c_border" "$ic_box_bl"
    _repeat "$ic_box_h" $((width - 2))
    printf '%s%s\n' "$ic_box_br" "$c_reset"
}

# A short horizontal rule (muted, spans terminal width)
draw_rule() {
    local width
    width=$(term_width)
    [ "$width" -gt 100 ] && width=100
    printf '%s' "$c_muted"
    _repeat "$ic_box_h" "$width"
    printf '%s\n' "$c_reset"
}

# Status line — compact 1-liner showing current profile + env + docker state
draw_status_line() {
    local docker_state
    if docker info > /dev/null 2>&1; then
        docker_state="${c_success}${ic_dot} docker${c_reset}"
    else
        docker_state="${c_error}${ic_dot} docker down${c_reset}"
    fi

    local profile_chip
    case "$PROFILE" in
        fullstack)
            local env_upper
            env_upper=$(echo "${ENV:-?}" | tr '[:lower:]' '[:upper:]')
            local env_color=$c_muted
            case "${ENV:-}" in
                development) env_color=$c_success ;;
                staging) env_color=$c_warn ;;
                production) env_color=$c_error ;;
            esac
            profile_chip="${env_color}${ic_bullet} full stack · ${env_upper}${c_reset}"
            ;;
        *)
            profile_chip="${c_muted}${ic_bullet} no profile${c_reset}"
            ;;
    esac

    printf '%s%s Orkestra v%s%s   %s   %s\n' \
        "$c_header" "$c_bold" "$ORKESTRA_VERSION" "$c_reset" \
        "$profile_chip" "$docker_state"
}

# Full-screen header: clear, draw status line, draw title box
page_header() {
    local title=$1
    [ "$HAS_TTY" = true ] && clear 2>/dev/null || true
    draw_status_line
    draw_rule
    printf '\n%s%s %s%s\n\n' "$c_bold" "$c_header" "$title" "$c_reset"
}

# ---------------------------------------------------------------------------
# Spinner (pure bash, upgrades to gum spin when available)
# ---------------------------------------------------------------------------

_spinner_frames=(⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏)

# Run a command with a spinner. Output is suppressed; stderr is kept for
# errors. Returns the command's exit code.
#
# Usage: with_spinner "Building images..." docker compose build
with_spinner() {
    local message=$1
    shift

    # Upgrade path: gum's built-in spinner
    if [ "$HAS_GUM" = true ] && [ "$HAS_TTY" = true ]; then
        gum spin --spinner dot --title "$message" -- "$@"
        return $?
    fi

    # Non-TTY: no spinner, just run
    if [ "$HAS_TTY" != true ] || [ "$HAS_UNICODE" != true ]; then
        printf '%s %s\n' "$ic_step" "$message"
        "$@"
        return $?
    fi

    # Pure bash spinner
    local pid frame=0 delay=0.08
    tput civis 2>/dev/null || true
    (
        "$@"
    ) &
    pid=$!

    # shellcheck disable=SC2064
    trap "kill $pid 2>/dev/null; tput cnorm 2>/dev/null; exit 130" INT

    while kill -0 "$pid" 2>/dev/null; do
        local f="${_spinner_frames[$((frame % ${#_spinner_frames[@]}))]}"
        printf '\r%s%s%s %s' "$c_accent" "$f" "$c_reset" "$message"
        frame=$((frame + 1))
        sleep "$delay"
    done

    wait "$pid" 2>/dev/null
    local code=$?
    trap - INT

    # Clear spinner line
    printf '\r\033[K'
    tput cnorm 2>/dev/null || true

    if [ $code -eq 0 ]; then
        p_ok "$message"
    else
        p_err "$message (exit $code)"
    fi
    return $code
}

# ---------------------------------------------------------------------------
# Prompt helpers (upgrade to gum when present)
# ---------------------------------------------------------------------------

# ask_yes_no <prompt> [default=y|n]
ask_yes_no() {
    local prompt=$1
    local default=${2:-y}

    if [ "$HAS_GUM" = true ] && [ "$HAS_TTY" = true ]; then
        if [ "$default" = "y" ]; then
            gum confirm --default=true "$prompt"
        else
            gum confirm --default=false "$prompt"
        fi
        return $?
    fi

    local hint="[Y/n]"
    [ "$default" = "n" ] && hint="[y/N]"
    local answer
    while true; do
        printf '%s%s %s %s %s' "$c_prompt" "$ic_arrow" "$prompt" "$hint" "$c_reset"
        read -r answer
        answer=${answer:-$default}
        case "$answer" in
            [Yy]*) return 0 ;;
            [Nn]*) return 1 ;;
            *) p_warn "Please answer yes or no." ;;
        esac
    done
}

# ask_menu <prompt> <option1> <option2> ...
# Prints the selected option on stdout, or empty string on cancel.
ask_menu() {
    local prompt=$1
    shift
    local -a options=("$@")

    if [ "$HAS_GUM" = true ] && [ "$HAS_TTY" = true ]; then
        printf '%s\n' "$prompt" >&2
        gum choose "${options[@]}"
        return $?
    fi

    printf '%s%s %s%s\n' "$c_prompt" "$ic_arrow" "$prompt" "$c_reset" >&2
    local i
    for i in "${!options[@]}"; do
        printf '  %s%d)%s %s\n' "$c_accent" "$((i + 1))" "$c_reset" "${options[$i]}" >&2
    done
    local choice
    printf '%s%s [1-%d]: %s' "$c_prompt" "$ic_arrow" "${#options[@]}" "$c_reset" >&2
    read -r choice
    if ! [[ "$choice" =~ ^[0-9]+$ ]] || [ "$choice" -lt 1 ] || [ "$choice" -gt "${#options[@]}" ]; then
        return 1
    fi
    printf '%s' "${options[$((choice - 1))]}"
}

# ask_value <label> <default>
# Prints the chosen value on stdout; empty input returns <default>. The prompt
# is written to stderr so stdout carries only the value.
ask_value() {
    local label=$1 default=${2:-} answer
    if [ "$HAS_GUM" = true ] && [ "$HAS_TTY" = true ]; then
        gum input --prompt "$label » " --value "$default"
        return 0
    fi
    if [ -n "$default" ]; then
        printf '%s%s %s %s[%s]%s: ' "$c_prompt" "$ic_arrow" "$label" "$c_muted" "$default" "$c_reset" >&2
    else
        printf '%s%s %s: %s' "$c_prompt" "$ic_arrow" "$label" "$c_reset" >&2
    fi
    read -r answer
    printf '%s' "${answer:-$default}"
}

# ---------------------------------------------------------------------------
# Shared infrastructure helpers
# ---------------------------------------------------------------------------

check_docker_running() {
    if ! docker info > /dev/null 2>&1; then
        die "Docker daemon is not running"
    fi
}

# Ensure the RS256 JWT keys exist on the host with safe permissions. Never
# make the private signing key group/world-readable to work around a container
# UID mismatch; deployments must align ownership or use a proper secret mount.
#
# Non-fatal if keys are missing (init/make handles generation); we only warn.
ensure_jwt_keys_readable() {
    local keys_dir="$DOCKER_DIR/keys"
    local priv="$keys_dir/jwt-private.pem"
    local pub="$keys_dir/jwt-public.pem"

    if [ ! -f "$priv" ] || [ ! -f "$pub" ]; then
        p_warn "JWT keys not found in $keys_dir — run: make init (or scripts/generate-jwt-keys.sh)"
        return 0
    fi

    chmod 600 "$priv" 2>/dev/null || die "Cannot secure JWT private key permissions: $priv"
    chmod 644 "$pub" 2>/dev/null || die "Cannot set JWT public key permissions: $pub"
    p_ok "JWT keys present; private key permissions restricted to owner"
}

# Pre-flight: detect containers whose explicit `container_name:` matches
# what this compose file declares but whose `com.docker.compose.project`
# label points at a different project (or is empty entirely). These
# collide with raw "name already in use" errors on `up -d`, which is
# the failure operators hit when an earlier deploy used a differently
# named compose project.
#
# Reclaim strategy: stop + force-remove the orphans. Named volumes are
# never touched — they survive container removal, so data persists
# across the recreate. The subsequent `up -d` recreates each container
# under the expected project's labels and reattaches the volumes.
#
# Backend-managed containers (`orkestra.managed=true` — graph/agents
# modules' Memgraph / Hindsight) are deliberately skipped. They are
# owned by the running backend's container manager, and the right way
# to stop them is to disable the owning module at /admin/modules, NOT
# `docker rm`. The infra compose declares the same names under the
# `manual-only` profile, so `up -d` without `--profile manual-only`
# won't try to create them and won't collide.
#
# Returns 0 if there are no orphans, or all are reclaimed. Calls die()
# if the operator declines the reclaim — refusing to deploy on top of
# a known-broken state is safer than silently letting `up -d` fail.
preflight_reclaim_containers() {
    local compose_file=$1
    [ -f "$compose_file" ] || return 0

    # Effective project name: post-namespacing, orkestra.sh drives the
    # project via COMPOSE_PROJECT_NAME (the compose files no longer carry
    # a top-level `name:`). Fall back to the directory name to mirror
    # compose's own default when the var is unset.
    local expected_project="${COMPOSE_PROJECT_NAME:-}"
    if [ -z "$expected_project" ]; then
        expected_project=$(basename "$(dirname "$compose_file")")
    fi

    # Container names for this stack. `container_name:` is now interpolated
    # (${APP_NAME}-<svc>-${ENV}), so resolve it via `docker compose config`
    # rather than grepping the raw file (which would yield literal vars).
    local -a wanted=()
    while IFS= read -r line; do
        [ -n "$line" ] && wanted+=("$line")
    done < <(
        docker compose -f "$compose_file" --env-file "$ENV_FILE" config 2> /dev/null \
            | grep -E '^[[:space:]]+container_name:[[:space:]]*' \
            | sed -E 's/^[[:space:]]+container_name:[[:space:]]*//; s/^["'"'"']//; s/["'"'"']$//'
    )

    [ ${#wanted[@]} -eq 0 ] && return 0

    local -a orphans=()
    local -a orphan_owners=()
    local name owner managed
    for name in "${wanted[@]}"; do
        docker container inspect "$name" > /dev/null 2>&1 || continue
        managed=$(docker container inspect "$name" \
            --format '{{ index .Config.Labels "orkestra.managed" }}' 2>/dev/null)
        # Backend-managed (graph / agents modules) — disable the module
        # at /admin/modules to stop these, never `docker rm` here.
        [ "$managed" = "true" ] && continue
        owner=$(docker container inspect "$name" \
            --format '{{ index .Config.Labels "com.docker.compose.project" }}' 2>/dev/null)
        if [ "$owner" != "$expected_project" ]; then
            orphans+=("$name")
            orphan_owners+=("${owner:-<unlabelled>}")
        fi
    done

    [ ${#orphans[@]} -eq 0 ] && return 0

    p_warn "Found ${#orphans[@]} container(s) that will collide with project '$expected_project':"
    local i
    for i in "${!orphans[@]}"; do
        p_muted "  ${orphans[$i]}  ← currently owned by: ${orphan_owners[$i]}"
    done
    p_muted "Named volumes are not touched — data persists across the recreate."

    # CLI mode (`./orkestra.sh deploy --yes`) and non-interactive
    # contexts skip the prompt and reclaim automatically. The TUI
    # asks the operator first.
    if [ "${SKIP_CONFIRMATION:-no}" = "yes" ] || [ "$HAS_TTY" != true ]; then
        p_step "Auto-reclaiming (non-interactive mode)"
    elif ! ask_yes_no "Reclaim by stop+remove (recommended)?" "y"; then
        die "Refusing to deploy with orphan containers. Resolve manually and retry."
    fi

    if ! docker rm -f "${orphans[@]}" > /dev/null 2>&1; then
        die "Failed to remove orphan containers — inspect 'docker ps -a' and retry."
    fi
    p_ok "Reclaimed ${#orphans[@]} container(s); compose will recreate them under '$expected_project'"
}

# List services declared in a compose file.
get_services() {
    local compose_file=$1
    if [ -f "$compose_file" ]; then
        docker compose -f "$compose_file" config --services 2> /dev/null || true
    fi
}

is_service_running() {
    local compose_file=$1 service=$2 count
    # Fast path: query within the compose file's own project.
    count=$(docker compose -f "$compose_file" ps -q "$service" 2> /dev/null | wc -l)
    if [ "$count" -gt 0 ]; then
        return 0
    fi
    # Fallback: a container with this compose-service label may be running
    # under a different project (e.g. infra+profile stacks are merged under
    # one project name like "orkestra-full", so the bare infra compose
    # file's project lookup misses them). Match by label instead.
    [ -n "$(docker ps -q \
        --filter "label=com.docker.compose.service=$service" \
        --filter "status=running" 2> /dev/null)" ]
}

# Resolve the actual running container for (compose_file, service), preferring
# the compose file's project but falling back to any project hosting that
# compose-service label. Echoes the container id, or empty if nothing matches.
resolve_running_container() {
    local compose_file=$1 service=$2 cid
    cid=$(docker compose -f "$compose_file" ps -q "$service" 2> /dev/null | head -1)
    if [ -n "$cid" ]; then
        printf '%s\n' "$cid"
        return 0
    fi
    docker ps -q \
        --filter "label=com.docker.compose.service=$service" \
        --filter "status=running" 2> /dev/null | head -1
}
# ---------------------------------------------------------------------------
# Full-stack profile operations
# ---------------------------------------------------------------------------

# Map detected ENV to compose file, branch, DB, URLs, env-chip color.
# ADR-0006: dev runs the single docker-compose.dev.yml on public Alpine
# images (Chainguard via the GO_BASE build-arg on Dockerfile.dev-backend).
set_env_config() {
    case "$ENV" in
        development)
            ENV_CHIP_COLOR=$c_success
            ENV_ICON="$ic_dot"
            BRANCH="any"
            COMPOSE_FILE="$DOCKER_DIR/docker-compose.dev.yml"
            DB_NAME="orkestra_dev"
            FRONTEND_URL="http://localhost:8080"
            BACKEND_URL="http://localhost:3000"
            ;;
        staging)
            ENV_CHIP_COLOR=$c_warn
            ENV_ICON="$ic_dot"
            BRANCH="dev"
            COMPOSE_FILE="$DOCKER_DIR/docker-compose.staging.yml"
            DB_NAME="orkestra_staging"
            FRONTEND_URL="https://staging.orkestra.cc"
            BACKEND_URL="https://staging-api.orkestra.cc"
            ;;
        production)
            ENV_CHIP_COLOR=$c_error
            ENV_ICON="$ic_dot"
            BRANCH="main"
            COMPOSE_FILE="$DOCKER_DIR/docker-compose.prod.yml"
            DB_NAME="orkestra"
            FRONTEND_URL="https://gestionale.orkestra.com"
            BACKEND_URL="https://api.orkestra.com"
            ;;
    esac

    # docker/.env is the source of truth for where the stack is actually
    # reachable — the compose files already read FRONTEND_URL/BACKEND_URL from
    # it (e.g. `${FRONTEND_URL:-https://staging.orkestra.cc}`). The per-env
    # values above are only the fallback for a checkout that has not set them,
    # so a deployment that renames its hosts changes .env and nothing else.
    local env_frontend_url env_backend_url
    env_frontend_url="$(env_get "$ENV_FILE" FRONTEND_URL)"
    env_backend_url="$(env_get "$ENV_FILE" BACKEND_URL)"
    if [ -n "$env_frontend_url" ]; then FRONTEND_URL="$env_frontend_url"; fi
    if [ -n "$env_backend_url" ]; then BACKEND_URL="$env_backend_url"; fi
}

# Stack identity — one Compose project spans infra + app (+ observability
# overlay). Compose files carry no top-level `name:` anymore, so
# COMPOSE_PROJECT_NAME is what names the project. Requires ENV (and
# therefore COMPOSE_FILE, via set_env_config) to already be resolved.
resolve_stack_identity() {
    APP_NAME="$(env_get "$ENV_FILE" APP_NAME)"
    : "${APP_NAME:=orkestra}"
    STACK="${APP_NAME}-${ENV}"
    export COMPOSE_PROJECT_NAME="$STACK"

    # Clone identity surfaced in the SPA footer. The container has no git,
    # so the host computes these and passes them through docker-compose,
    # mirroring ORKESTRA_VERSION.
    #
    #   ORKESTRA_CLONE_VERSION — curated clone release version (bucket A).
    #     Derived from a clone-specific tag prefix: APP_NAME minus a leading
    #     "orkestra-" (orkestra-commons -> commons -> tags "commons-v*").
    #     Falls back to "dev" when this clone has cut no release tag yet
    #     (a bare SHA would duplicate ORKESTRA_BUILD_COMMIT below).
    #   ORKESTRA_BUILD_COMMIT — short SHA of the deployed code (bucket B).
    local clone_name clone_prefix clone_desc
    clone_name="${APP_NAME#orkestra-}"
    clone_prefix="${clone_name}-v"
    if command -v git > /dev/null 2>&1 \
        && git -C "$PROJECT_ROOT" rev-parse --git-dir > /dev/null 2>&1; then
        clone_desc=$(git -C "$PROJECT_ROOT" describe --tags \
            --match "${clone_prefix}*" --dirty 2>/dev/null || true)
        if [ -n "$clone_desc" ]; then
            # Strip the "<clone_name>-" prefix, keep the leading "v".
            ORKESTRA_CLONE_VERSION="${clone_desc#${clone_name}-}"
        else
            ORKESTRA_CLONE_VERSION="dev"
        fi
        ORKESTRA_BUILD_COMMIT=$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo "")
    else
        ORKESTRA_CLONE_VERSION="dev"
        ORKESTRA_BUILD_COMMIT=""
    fi
    export ORKESTRA_CLONE_VERSION ORKESTRA_BUILD_COMMIT
}

fullstack_init_env() {
    if [ ! -f "$ENV_FILE" ]; then
        p_warn "docker/.env not found — looks like a fresh checkout."
        if [ -t 0 ] && [ -t 1 ]; then
            env_wizard
        else
            die "docker/.env missing. Non-interactive shell — run: make init (or scripts/init.sh) first."
        fi
    fi
    if ! detect_environment > /dev/null 2>&1; then
        die "Cannot detect ENV. Set ENV=development|staging|production in docker/.env or as a shell variable."
    fi
    ENV="$DETECTED_ENV"
    set_env_config
    resolve_stack_identity
    PROFILE="fullstack"
}

# ---------------------------------------------------------------------------
# Guided docker/.env setup wizard — ENV-adaptive, section by section.
# Reuses scripts/init.sh for mechanical scaffolding (template copy, random
# secrets, JWT keys, network); this layer collects human-supplied values,
# applies the ENV security profile, and validates.
# ---------------------------------------------------------------------------

# apply_env_profile <file> <env> — deterministic values that follow from the
# chosen ENV (security posture, debug, rate limits). Interactive values (URLs,
# hosts, storage creds) are collected by the wiz_* sections separately.
apply_env_profile() {
    local file=$1 env=$2
    env_set "$file" ENV "$env"
    env_set "$file" VITE_ENV "$env"
    case "$env" in
        development)
            env_set "$file" COOKIE_SECURE false
            env_set "$file" COOKIE_SAME_SITE lax
            env_set "$file" DEBUG true
            env_set "$file" PRETTY_LOGS true
            env_set "$file" RATE_LIMIT_REQUESTS_PER_MINUTE 1000
            env_set "$file" RATE_LIMIT_BURST 100
            ;;
        staging)
            env_set "$file" COOKIE_SECURE true
            env_set "$file" COOKIE_SAME_SITE lax
            env_set "$file" DEBUG false
            env_set "$file" PRETTY_LOGS false
            env_set "$file" RATE_LIMIT_REQUESTS_PER_MINUTE 60
            env_set "$file" RATE_LIMIT_BURST 30
            ;;
        production)
            env_set "$file" COOKIE_SECURE true
            env_set "$file" COOKIE_SAME_SITE strict
            env_set "$file" DEBUG false
            env_set "$file" PRETTY_LOGS false
            env_set "$file" RATE_LIMIT_REQUESTS_PER_MINUTE 30
            env_set "$file" RATE_LIMIT_BURST 15
            ;;
    esac
}

# _wiz_default <key> <fallback> — current .env value, or fallback if unset.
_wiz_default() {
    local cur; cur=$(env_get "$ENV_FILE" "$1")
    printf '%s' "${cur:-$2}"
}

wiz_identity() {
    p_section "Identity & ports"
    env_set "$ENV_FILE" APP_NAME      "$(ask_value 'App name'      "$(_wiz_default APP_NAME orkestra)")"
    env_set "$ENV_FILE" BACKEND_PORT  "$(ask_value 'Backend port'  "$(_wiz_default BACKEND_PORT 3000)")"
    env_set "$ENV_FILE" FRONTEND_PORT "$(ask_value 'Frontend port' "$(_wiz_default FRONTEND_PORT 8080)")"
}

wiz_urls() {
    local env=$1
    p_section "URLs & hosts"
    if [ "$env" = "development" ]; then
        ask_yes_no "Customize dev URLs? (defaults use localhost)" "n" || return 0
    fi
    local backend frontend op_front cl_front console_host client_host ws
    backend=$(ask_value 'Backend URL'                    "$(_wiz_default BACKEND_URL http://localhost:3000)")
    frontend=$(ask_value 'Frontend URL (operator)'       "$(_wiz_default FRONTEND_URL http://localhost:8080)")
    op_front=$(ask_value 'Operator frontend URL (email)' "$(_wiz_default OPERATOR_FRONTEND_URL "$frontend")")
    cl_front=$(ask_value 'Client frontend URL'           "$(_wiz_default CLIENT_FRONTEND_URL http://client.localhost:8081)")
    console_host=$(ask_value 'Console host'              "$(_wiz_default CONSOLE_HOST console.localhost)")
    client_host=$(ask_value 'Client API host'           "$(_wiz_default CLIENT_API_HOST client.localhost)")

    env_set "$ENV_FILE" BACKEND_URL "$backend"
    env_set "$ENV_FILE" FRONTEND_URL "$frontend"
    env_set "$ENV_FILE" OPERATOR_FRONTEND_URL "$op_front"
    env_set "$ENV_FILE" CLIENT_FRONTEND_URL "$cl_front"
    env_set "$ENV_FILE" CONSOLE_HOST "$console_host"
    env_set "$ENV_FILE" CLIENT_API_HOST "$client_host"

    # Derived defaults (overridable): ws:// from http://, wss:// from https://.
    ws=${backend/http/ws}
    env_set "$ENV_FILE" VITE_API_URL          "$(ask_value 'Vite API URL'          "$(_wiz_default VITE_API_URL "$backend")")"
    env_set "$ENV_FILE" VITE_BACKEND_URL      "$(ask_value 'Vite backend URL'      "$(_wiz_default VITE_BACKEND_URL "$backend")")"
    env_set "$ENV_FILE" VITE_WS_URL           "$(ask_value 'Vite WS URL'           "$(_wiz_default VITE_WS_URL "${ws}/ws")")"
    env_set "$ENV_FILE" CORS_ORIGINS          "$(ask_value 'CORS origins (comma-sep)' "$(_wiz_default CORS_ORIGINS "$frontend,$cl_front")")"
    env_set "$ENV_FILE" OPERATOR_CORS_ORIGINS "$(ask_value 'Operator CORS origins'  "$(_wiz_default OPERATOR_CORS_ORIGINS "$frontend")")"
    env_set "$ENV_FILE" CLIENT_CORS_ORIGINS   "$(ask_value 'Client CORS origins'    "$(_wiz_default CLIENT_CORS_ORIGINS "$cl_front")")"
}

wiz_security() {
    local env=$1
    [ "$env" = "development" ] && return 0
    p_section "Security & cookies ($env)"
    p_info "COOKIE_SECURE / SameSite / DEBUG / rate limits were set by the $env profile."
    env_set "$ENV_FILE" OPERATOR_COOKIE_DOMAIN "$(ask_value 'Operator cookie domain' "$(_wiz_default OPERATOR_COOKIE_DOMAIN "$(env_get "$ENV_FILE" CONSOLE_HOST)")")"
    env_set "$ENV_FILE" CLIENT_COOKIE_DOMAIN   "$(ask_value 'Client cookie domain'   "$(_wiz_default CLIENT_COOKIE_DOMAIN "$(env_get "$ENV_FILE" CLIENT_API_HOST)")")"
}

wiz_storage() {
    local env=$1
    p_section "Object storage"
    if [ "$env" != "production" ]; then
        p_info "Using built-in RustFS defaults for $env (configurable later)."
        return 0
    fi
    if ask_yes_no "Use built-in RustFS for production? (No = managed S3)" "n"; then
        p_info "Keeping RustFS storage defaults."
        return 0
    fi
    env_set "$ENV_FILE" STORAGE_ENDPOINT   "$(ask_value 'S3 endpoint'   "$(_wiz_default STORAGE_ENDPOINT https://s3.amazonaws.com)")"
    env_set "$ENV_FILE" STORAGE_REGION     "$(ask_value 'S3 region'     "$(_wiz_default STORAGE_REGION us-east-1)")"
    env_set "$ENV_FILE" STORAGE_BUCKET     "$(ask_value 'S3 bucket'     "$(_wiz_default STORAGE_BUCKET orkestra-avatars)")"
    env_set "$ENV_FILE" STORAGE_ACCESS_KEY "$(ask_value 'S3 access key' "$(_wiz_default STORAGE_ACCESS_KEY '')")"
    env_set "$ENV_FILE" STORAGE_SECRET_KEY "$(ask_value 'S3 secret key' "$(_wiz_default STORAGE_SECRET_KEY '')")"
    env_set "$ENV_FILE" STORAGE_FORCE_PATH_STYLE false
    env_set "$ENV_FILE" STORAGE_ENSURE_BUCKET false
}

wiz_seeds() {
    p_section "Optional first-boot seeds"
    if ! ask_yes_no "Set up OAuth/SMTP now? (most operators skip — use /admin/modules later)" "n"; then
        p_info "Skipped — configure providers at /admin/modules after first login."
        return 0
    fi
    if ask_yes_no "Configure Google OAuth?" "y"; then
        env_set "$ENV_FILE" OAUTH_GOOGLE_CLIENT_ID     "$(ask_value 'Google client ID'     "$(env_get "$ENV_FILE" OAUTH_GOOGLE_CLIENT_ID)")"
        env_set "$ENV_FILE" OAUTH_GOOGLE_CLIENT_SECRET "$(ask_value 'Google client secret' "$(env_get "$ENV_FILE" OAUTH_GOOGLE_CLIENT_SECRET)")"
        env_set "$ENV_FILE" OAUTH_GOOGLE_REDIRECT_URL  "$(ask_value 'Google redirect URL'  "$(_wiz_default OAUTH_GOOGLE_REDIRECT_URL "$(env_get "$ENV_FILE" BACKEND_URL)/v1/auth/oauth/google/callback")")"
    fi
    if ask_yes_no "Configure SMTP email?" "y"; then
        env_set "$ENV_FILE" NOTIFICATION_EMAIL_PROVIDER smtp
        env_set "$ENV_FILE" NOTIFICATION_EMAIL_FROM "$(ask_value 'From address' "$(_wiz_default NOTIFICATION_EMAIL_FROM noreply@example.com)")"
        env_set "$ENV_FILE" SMTP_HOST     "$(ask_value 'SMTP host'     "$(env_get "$ENV_FILE" SMTP_HOST)")"
        env_set "$ENV_FILE" SMTP_PORT     "$(ask_value 'SMTP port'     "$(_wiz_default SMTP_PORT 587)")"
        env_set "$ENV_FILE" SMTP_USERNAME "$(ask_value 'SMTP username' "$(env_get "$ENV_FILE" SMTP_USERNAME)")"
        env_set "$ENV_FILE" SMTP_PASSWORD "$(ask_value 'SMTP password' "$(env_get "$ENV_FILE" SMTP_PASSWORD)")"
    fi
}

# env_wizard — orchestrator. Operates on the global $ENV_FILE.
env_wizard() {
    page_header "Guided docker/.env setup"

    # 1. Scaffold if missing (init.sh: template copy, random secrets, JWT, network).
    if [ ! -f "$ENV_FILE" ]; then
        p_step "No docker/.env — scaffolding secrets + JWT keys via scripts/init.sh"
        bash "$SCRIPT_DIR/scripts/init.sh" >/dev/null || die "init.sh failed — see output above"
        p_ok "Baseline docker/.env created"
    else
        p_info "docker/.env exists — reconfigure mode (current values pre-filled)"
        if ask_yes_no "Regenerate random secrets & JWT keys? (invalidates all tokens)" "n"; then
            bash "$SCRIPT_DIR/scripts/init.sh" --force >/dev/null || die "init.sh --force failed"
            p_ok "Secrets & JWT keys regenerated"
        fi
    fi

    # 2. ENV selection drives every downstream default.
    p_section "Target environment"
    local env
    env=$(ask_menu "Select environment" development staging production) || env=""
    [ -z "$env" ] && env="$(_wiz_default ENV development)"
    p_ok "Environment: $env"

    # 3. Deterministic ENV profile (security, debug, rate limits).
    apply_env_profile "$ENV_FILE" "$env"

    # 4. Interactive sections.
    wiz_identity
    wiz_urls "$env"
    wiz_security "$env"
    wiz_storage "$env"
    wiz_seeds

    # 5. Validate + summary.
    p_section "Validation"
    if bash "$SCRIPT_DIR/scripts/env-validate.sh"; then
        p_ok "docker/.env validated"
    else
        p_warn "Validation reported issues (above) — review docker/.env"
    fi
    draw_box "Setup complete" \
        "" \
        "  docker/.env is ready for ENV=$env." \
        "  Next: ./orkestra.sh deploy   (or the Full stack menu)" \
        "  Admin token: ORKESTRA_API_URL=http://localhost:3000 ./scripts/devtoken.sh administrator" \
        ""
}

show_deploy_summary() {
    draw_box "Summary" \
        "" \
        "  Environment  ${ENV_CHIP_COLOR}${ic_dot}${c_reset} $(echo "$ENV" | tr '[:lower:]' '[:upper:]')" \
        "  Operation    Deploy" \
        "  Branch       ${BRANCH:-any}" \
        "  Scope        ${DEPLOY_SCOPE:-all}" \
        "  Rebuild      $([ "$REBUILD_IMAGES" = "yes" ] && echo "${c_success}yes${c_reset}" || echo "${c_warn}no${c_reset}")" \
        ""
}

fullstack_deploy_interactive() {
    page_header "Full stack · Deploy"
    case "$ENV" in
        development | staging)
            ask_yes_no "Rebuild images?" "y" && REBUILD_IMAGES="yes" || REBUILD_IMAGES="no"
            SKIP_CONFIRMATION="yes"
            ;;
        production)
            draw_box "WARNING" \
                "" \
                "  ${c_error}Deploying to PRODUCTION.${c_reset}" \
                "  ${c_muted}Double-check you've tested this in staging.${c_reset}" \
                ""
            echo
            ask_yes_no "Skip confirmation prompts?" "n" && SKIP_CONFIRMATION="yes" || SKIP_CONFIRMATION="no"
            REBUILD_IMAGES="yes"
            ;;
    esac

    echo
    p_section "Select deployment scope"
    local scope
    scope=$(ask_menu "Which services?" \
        "All (full stack)" \
        "Backend only" \
        "Admin frontend only" \
        "Admin frontend + Backend" \
        "Infrastructure only")

    case "$scope" in
        "All (full stack)") DEPLOY_SCOPE="all" ;;
        "Backend only") DEPLOY_SCOPE="backend" ;;
        "Admin frontend only") DEPLOY_SCOPE="frontend-admin" ;;
        "Admin frontend + Backend") DEPLOY_SCOPE="frontend-admin+backend" ;;
        "Infrastructure only") DEPLOY_SCOPE="infra" ;;
        *)
            p_warn "Deployment cancelled."
            return
            ;;
    esac

    echo
    show_deploy_summary

    if ! ask_yes_no "Proceed with deployment?" "y"; then
        p_warn "Deployment cancelled."
        return
    fi

    if [ "$ENV" = "production" ] && [ "$SKIP_CONFIRMATION" = "no" ]; then
        if ! ask_yes_no "Have you tested this in staging?" "y"; then
            die "Please test in staging first. Deployment cancelled."
        fi
    fi

    fullstack_execute_deploy
}

# The full deploy pipeline. Preserves the behavior of the old deploy.sh
# execute_deploy() function verbatim (git ops, version tagging, rolling
# updates, health checks, smoke tests, deployment metadata log).
fullstack_execute_deploy() {
    local TIMESTAMP DEPLOYMENT_ID
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    DEPLOYMENT_ID="${ENV}_${TIMESTAMP}"

    # BACKEND_SERVICE / FRONTEND_SERVICE / CLIENT_SERVICE are uniform
    # constants set once near the top of the script (no ENV-conditional
    # switch needed — every env file uses the same service names now).

    echo
    draw_box "Starting deployment" \
        "" \
        "  Deployment ID  ${c_accent}${DEPLOYMENT_ID}${c_reset}" \
        "  Environment    $(echo "$ENV" | tr '[:lower:]' '[:upper:]')" \
        "  Scope          ${DEPLOY_SCOPE}" \
        ""
    echo

    # --- Pre-deployment checks ---
    p_section "Pre-deployment checks"
    check_docker_running
    p_ok "Docker is running"
    ensure_jwt_keys_readable
    if [ "$ENV" = "production" ] && [ "$EUID" -eq 0 ]; then
        die "Do not run as root"
    fi
    [ "$ENV" = "production" ] && p_ok "Not running as root"

    # --- Git operations (non-dev only) ---
    local COMMIT_HASH COMMIT_MSG
    if [ "$ENV" != "development" ]; then
        p_section "Git operations"
        cd "$PROJECT_ROOT"
        local CURRENT_BRANCH
        CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

        if ! git diff-index --quiet HEAD --; then
            p_warn "Uncommitted changes detected"
            if [ "$ENV" = "production" ]; then
                die "Production requires a clean working tree. Commit or stash changes."
            fi
            local action
            action=$(ask_menu "What would you like to do?" \
                "Stash changes and continue" \
                "Proceed anyway" \
                "Stop deployment")
            case "$action" in
                "Stash changes and continue")
                    git stash push -m "Auto-stash before $ENV deployment $TIMESTAMP"
                    p_ok "Changes stashed (use 'git stash pop' to restore)"
                    ;;
                "Proceed anyway")
                    p_warn "Proceeding with uncommitted changes"
                    ;;
                *)
                    p_warn "Deployment cancelled."
                    return 0
                    ;;
            esac
        else
            p_ok "No uncommitted changes"
        fi

        if [ "$CURRENT_BRANCH" != "$BRANCH" ] && [ "$BRANCH" != "any" ]; then
            p_step "Switching to $BRANCH branch"
            git checkout "$BRANCH"
            p_ok "Switched to $BRANCH"
        fi

        with_spinner "Pulling latest changes" git pull origin "$BRANCH"
        COMMIT_HASH=$(git rev-parse --short HEAD)
        COMMIT_MSG=$(git log -1 --pretty=%B)
        p_ok "Updated to commit: $COMMIT_HASH"

        if [ "$ENV" = "production" ]; then
            local LOCAL_HASH REMOTE_HASH
            LOCAL_HASH=$(git rev-parse HEAD)
            REMOTE_HASH=$(git rev-parse "origin/$BRANCH")
            if [ "$LOCAL_HASH" != "$REMOTE_HASH" ]; then
                die "Local and remote branches are out of sync"
            fi
            p_ok "Branch is up to date with origin"
        fi
    else
        COMMIT_HASH="dev"
        COMMIT_MSG="Development build"
    fi

    # --- Build images ---
    if [ "$REBUILD_IMAGES" = "yes" ] && [ "$DEPLOY_SCOPE" != "infra" ]; then
        p_section "Building Docker images"
        export VERSION BUILD_TIME GIT_COMMIT
        VERSION="$(date -u +%Y-%m-%d_%H:%M:%S)_${COMMIT_HASH}"
        BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
        GIT_COMMIT="$COMMIT_HASH"
        p_muted "  Version    $VERSION"
        p_muted "  Build time $BUILD_TIME"
        p_muted "  Commit     $GIT_COMMIT"
        echo

        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "backend" ] || [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
            with_spinner "Building backend image (no cache)" \
                docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" build --no-cache \
                --build-arg VERSION="$VERSION" \
                --build-arg BUILD_TIME="$BUILD_TIME" \
                --build-arg GIT_COMMIT="$GIT_COMMIT" \
                "$BACKEND_SERVICE"
        fi
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "frontend-admin" ] || [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
            with_spinner "Building admin frontend image (no cache)" \
                docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" build --no-cache \
                --build-arg VERSION="$VERSION" \
                --build-arg BUILD_TIME="$BUILD_TIME" \
                --build-arg GIT_COMMIT="$GIT_COMMIT" \
                "$FRONTEND_SERVICE"
        fi
    fi

    # --- Deploy services ---
    p_section "Deploying services"
    # Reclaim any infra/app containers that exist under a foreign
    # compose project — they would otherwise abort `up -d` with a raw
    # Docker name-collision error. Volumes are preserved.
    preflight_reclaim_containers "$INFRA_COMPOSE"
    preflight_reclaim_containers "$COMPOSE_FILE"

    local -a compose_files=(-f "$INFRA_COMPOSE" -f "$COMPOSE_FILE")
    # Preserve the backend's Loki/Grafana environment override whenever this
    # stack already has observability containers. The overlay is only valid as
    # part of this merged stack, never on its own.
    if observability_project_active; then
        compose_files+=(-f "$OBSERVABILITY_COMPOSE")
    fi
    with_spinner "Ensuring infrastructure services are running" \
        docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d mongodb redis rustfs
    sleep 5
    p_ok "Infrastructure ready"

    if [ "$DEPLOY_SCOPE" = "infra" ]; then
        p_ok "Infrastructure-only deployment complete"
    elif [ "$ENV" = "production" ]; then
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "backend" ] || [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
            with_spinner "Rolling-update backend" \
                docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d --no-deps "$BACKEND_SERVICE"
            with_spinner "Waiting for backend to be healthy" sleep 15
        fi
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "frontend-admin" ] || [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
            with_spinner "Deploying admin frontend" \
                docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d --no-deps "$FRONTEND_SERVICE"
        fi
        with_spinner "Waiting for services to stabilize" sleep 30
    else
        if [ "$DEPLOY_SCOPE" = "all" ]; then
            # One project, one `up -d` — do NOT `down` first (that would
            # tear down infra too, since infra is now part of this project).
            with_spinner "Starting services" \
                docker compose "${compose_files[@]}" --env-file "$ENV_FILE" up -d
        else
            if [ "$DEPLOY_SCOPE" = "backend" ]; then
                docker compose "${compose_files[@]}" --env-file "$ENV_FILE" stop "$BACKEND_SERVICE" 2> /dev/null || true
                with_spinner "Restarting backend" \
                    docker compose "${compose_files[@]}" --env-file "$ENV_FILE" up -d "$BACKEND_SERVICE"
            fi
            if [ "$DEPLOY_SCOPE" = "frontend-admin" ]; then
                docker compose "${compose_files[@]}" --env-file "$ENV_FILE" stop "$FRONTEND_SERVICE" 2> /dev/null || true
                with_spinner "Restarting admin frontend" \
                    docker compose "${compose_files[@]}" --env-file "$ENV_FILE" up -d "$FRONTEND_SERVICE"
            fi
            if [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
                docker compose "${compose_files[@]}" --env-file "$ENV_FILE" stop "$FRONTEND_SERVICE" "$BACKEND_SERVICE" 2> /dev/null || true
                with_spinner "Restarting admin frontend and backend" \
                    docker compose "${compose_files[@]}" --env-file "$ENV_FILE" up -d "$FRONTEND_SERVICE" "$BACKEND_SERVICE"
            fi
        fi
        with_spinner "Waiting for services to initialize" sleep 30
    fi

    # --- Health checks (non-dev) ---
    if [ "$ENV" != "development" ]; then
        p_section "Health checks"
        local health_script=""
        if [ -f "$DOCKER_DIR/health-check.sh" ]; then
            health_script="$DOCKER_DIR/health-check.sh"
        elif [ -f "$PROJECT_ROOT/scripts/health-check.sh" ]; then
            health_script="$PROJECT_ROOT/scripts/health-check.sh"
        fi

        if [ "$ENV" = "production" ]; then
            local max_retries=5 retry_interval=10 health_ok=false i
            for i in $(seq 1 $max_retries); do
                p_step "Health check attempt $i/$max_retries"
                if [ -n "$health_script" ]; then
                    if bash "$health_script" "$ENV" "$DEPLOY_SCOPE" > /tmp/health_check_output.txt 2>&1; then
                        p_ok "All health checks passed"
                        health_ok=true
                        break
                    else
                        p_warn "Health checks failed, waiting ${retry_interval}s..."
                        sleep $retry_interval
                    fi
                else
                    p_err "Health check script not found"
                    break
                fi
            done
            if [ "$health_ok" = false ]; then
                # Show what actually failed — the retry loop captured the output
                # to a file, and dying without printing it made a failed
                # production deploy impossible to diagnose from the terminal.
                if [ -s /tmp/health_check_output.txt ]; then
                    p_muted "$(cat /tmp/health_check_output.txt)"
                fi
                die "Health checks failed after $max_retries attempts — manual intervention required"
            fi
        else
            if [ -n "$health_script" ]; then
                if bash "$health_script" "$ENV" "$DEPLOY_SCOPE"; then
                    p_ok "All health checks passed"
                else
                    die "Health checks failed"
                fi
            else
                p_warn "Health check script not found, skipping..."
            fi
        fi
    fi

    # --- Smoke tests (production only) ---
    if [ "$ENV" = "production" ]; then
        p_section "Smoke tests"
        # /health is the backend liveness probe and reports its MongoDB/Redis
        # checks; it is on the public-routes list so it answers without a JWT.
        # (The old target, /api/v1/auth/health, never existed — routes have no
        # /api prefix — and unknown paths get a 401 from the auth middleware.)
        local HEALTH_STATUS DOCS_STATUS
        HEALTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "${BACKEND_URL}/health" || echo "000")
        [ "$HEALTH_STATUS" = "200" ] && p_ok "Backend health endpoint healthy" || p_warn "Backend health endpoint returned: $HEALTH_STATUS"
        DOCS_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "${BACKEND_URL}/docs" || echo "000")
        [ "$DOCS_STATUS" = "200" ] && p_ok "API documentation accessible" || p_warn "API documentation returned: $DOCS_STATUS"
    fi

    # --- Post-deployment ---
    p_section "Post-deployment"
    if [ "$ENV" = "production" ]; then
        docker tag orkestra-backend:production "orkestra-backend:$DEPLOYMENT_ID"
        docker tag orkestra-backend:production orkestra-backend:latest
        docker tag orkestra-frontend-admin:production "orkestra-frontend-admin:$DEPLOYMENT_ID"
        docker tag orkestra-frontend-admin:production orkestra-frontend-admin:latest
    else
        docker tag "orkestra-backend:$ENV" "orkestra-backend:$DEPLOYMENT_ID" 2> /dev/null || true
        docker tag "orkestra-frontend-admin:$ENV" "orkestra-frontend-admin:$DEPLOYMENT_ID" 2> /dev/null || true
    fi
    p_ok "Images tagged"

    local METADATA_FILE="$DOCKER_DIR/deployments/${ENV}_deployments.log"
    mkdir -p "$(dirname "$METADATA_FILE")"
    cat >> "$METADATA_FILE" << EOF
Deployment ID: $DEPLOYMENT_ID
Timestamp: $(date '+%Y-%m-%d %H:%M:%S')
Branch: $BRANCH
Commit: $COMMIT_HASH
Commit Message: $COMMIT_MSG
Status: SUCCESS
---
EOF
    p_ok "Deployment metadata saved"

    if [ "$ENV" = "production" ]; then
        cd "$PROJECT_ROOT"
        local TAG_NAME="production-${TIMESTAMP}"
        git tag -a "$TAG_NAME" -m "Production deployment: $COMMIT_MSG"
        p_ok "Git tag created: $TAG_NAME"
    fi

    echo
    draw_box "Deployment successful" \
        "" \
        "  ${c_success}${ic_ok}${c_reset} Deployment ID  ${c_accent}${DEPLOYMENT_ID}${c_reset}" \
        "  Environment    $(echo "$ENV" | tr '[:lower:]' '[:upper:]')" \
        "  Commit         ${COMMIT_HASH}" \
        "  Admin frontend ${c_info}${FRONTEND_URL}${c_reset}" \
        "  Backend URL    ${c_info}${BACKEND_URL}${c_reset}" \
        ""

    if [ "$ENV" = "production" ]; then
        p_section "Post-deployment checklist"
        printf '  %s Verify user login flow\n' "$ic_bullet"
        printf '  %s Test critical workflows\n' "$ic_bullet"
        printf '  %s Monitor error rates\n' "$ic_bullet"
        printf '  %s Check application logs\n' "$ic_bullet"
        printf '  %s Verify background jobs\n' "$ic_bullet"
        printf '  %s Test mobile app connectivity\n' "$ic_bullet"
    fi
}

fullstack_deploy_cli() {
    DEPLOY_SCOPE="${1:-all}"
    REBUILD_IMAGES="${2:-no}"
    SKIP_CONFIRMATION="${3:-yes}"
    fullstack_execute_deploy
}

fullstack_stop() {
    local with_infra=${1:-ask}
    page_header "Full stack · Stop"
    check_docker_running
    echo

    if [ "$ENV" = "production" ] && [ "$with_infra" = "ask" ]; then
        draw_box "WARNING" "" "  ${c_error}Stopping PRODUCTION services.${c_reset}" ""
        echo
        if ! ask_yes_no "Are you sure you want to stop all services?" "n"; then
            p_warn "Operation cancelled."
            return
        fi
    fi

    # App-only stop: `stop` the app services (not `down` the whole
    # project) — infra stays up unless --with-infra is requested below.
    # Derive the service list from the env's own compose file rather than
    # hardcoding BACKEND_SERVICE/FRONTEND_SERVICE/CLIENT_SERVICE — prod
    # defines no client-frontend service, and `docker compose stop` errors
    # ("no such service") on the whole list before stopping anything.
    local -a APP_SERVICES=()
    local svc
    while IFS= read -r svc; do
        [ -n "$svc" ] && APP_SERVICES+=("$svc")
    done < <(get_services "$COMPOSE_FILE")

    with_spinner "Stopping application services" \
        docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" stop \
        "${APP_SERVICES[@]}"

    local stop_infra=false
    case "$with_infra" in
        yes) stop_infra=true ;;
        no) stop_infra=false ;;
        ask) ask_yes_no "Also stop infrastructure services (MongoDB, Redis, ...)?" "n" && stop_infra=true ;;
    esac

    if [ "$stop_infra" = true ]; then
        with_spinner "Stopping the whole stack (app + infrastructure)" \
            docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" down
    fi
}

fullstack_status() {
    page_header "Full stack · Status"
    check_docker_running
    echo

    p_section "Containers"
    docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" ps
    echo

    if [ "$ENV" != "development" ]; then
        p_section "Health checks"
        local health_script=""
        if [ -f "$DOCKER_DIR/health-check.sh" ]; then
            health_script="$DOCKER_DIR/health-check.sh"
        elif [ -f "$PROJECT_ROOT/scripts/health-check.sh" ]; then
            health_script="$PROJECT_ROOT/scripts/health-check.sh"
        fi
        if [ -n "$health_script" ]; then
            bash "$health_script" "$ENV" || true
        else
            p_warn "Health check script not found"
        fi
        echo
    fi

    p_section "Resource usage"
    docker stats --no-stream \
        --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}" | head -n 10
}

# ---------------------------------------------------------------------------
# Observability stack (ADR-0005 Phase D)
# ---------------------------------------------------------------------------
#
# Orthogonal to the SKU/full-stack split. Operators run this alongside
# their app stack to get Tempo (traces), Prometheus (metrics), Loki
# (logs), Grafana (UI), Promtail (log shipper), otel-collector (OTLP
# ingest). The overlay now joins the SAME Compose project as the app
# stack (COMPOSE_PROJECT_NAME=${APP_NAME}-${ENV} — no more standalone
# "orkestra-observability" project) via multi-file `-f infra -f app -f
# observability`. Every action below is still explicitly scoped to
# observability's OWN service names, so it never starts/stops/removes
# the app or infra services (or their volumes) that happen to share
# the project.

observability_check_file() {
    if [ ! -f "$OBSERVABILITY_COMPOSE" ]; then
        die "Observability compose file not found at $OBSERVABILITY_COMPOSE"
    fi
}

# The observability command can run before any app deploy ran in this shell,
# so resolve ENV/COMPOSE_FILE + the stack identity here —
# mirrors fullstack_init_env() without the missing-.env wizard prompt.
observability_init_env() {
    if [ -z "${ENV:-}" ] || [ -z "${COMPOSE_FILE:-}" ]; then
        if ! detect_environment > /dev/null 2>&1; then
            die "Cannot detect ENV. Set ENV=development|staging|production in docker/.env or as a shell variable."
        fi
        ENV="$DETECTED_ENV"
        set_env_config
    fi
    resolve_stack_identity
}

# This allowlist is deliberately explicit. The overlay also contains a partial
# `backend` service used only to merge trusted Loki/Grafana environment values,
# so asking Compose to parse that file alone is invalid and must never become
# service discovery. Keeping service and volume ownership here makes an empty
# discovery result incapable of widening a lifecycle command to the project.
declare -ar OBS_SERVICES=(otel-collector tempo prometheus loki promtail grafana)
declare -ar OBS_VOLUMES=(tempo-data prometheus-data loki-data promtail-positions grafana-data)

# Validate the explicit ownership lists against the only valid configuration:
# infra + app + observability. Any Compose failure or missing expected resource
# aborts before a lifecycle command can run.
observability_list_services() {
    local services volumes expected
    if ! services=$(docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" \
        --env-file "$ENV_FILE" config --services); then
        die "Observability compose validation failed; no lifecycle action was taken."
    fi
    if ! volumes=$(docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" \
        --env-file "$ENV_FILE" config --volumes); then
        die "Observability volume validation failed; no lifecycle action was taken."
    fi
    for expected in "${OBS_SERVICES[@]}"; do
        if ! grep -Fxq "$expected" <<< "$services"; then
            die "Observability service '$expected' is missing from the merged compose stack."
        fi
    done
    for expected in "${OBS_VOLUMES[@]}"; do
        if ! grep -Fxq "$expected" <<< "$volumes"; then
            die "Observability volume '$expected' is missing from the merged compose stack."
        fi
    done
}

observability_remove_volumes() {
    local volume volume_name
    for volume in "${OBS_VOLUMES[@]}"; do
        volume_name="${COMPOSE_PROJECT_NAME}_${volume}"
        if docker volume inspect "$volume_name" > /dev/null 2>&1; then
            docker volume rm "$volume_name" > /dev/null || \
                die "Failed to remove observability volume '$volume_name'."
        fi
    done
}

observability_backend_running() {
    [ -n "$(docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" \
        ps -q "$BACKEND_SERVICE" 2> /dev/null)" ]
}

observability_project_active() {
    local container_ids
    container_ids=$(docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" \
        --env-file "$ENV_FILE" ps -q "${OBS_SERVICES[@]}" 2> /dev/null) || return 1
    [ -n "$container_ids" ]
}

observability_apply_backend_override() {
    observability_backend_running || return 0
    docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" --env-file "$ENV_FILE" \
        up -d --no-deps "$BACKEND_SERVICE"
}

observability_restore_backend_config() {
    observability_backend_running || return 0
    docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" \
        up -d --no-deps "$BACKEND_SERVICE"
}

observability_up() {
    page_header "Observability · Up"
    check_docker_running
    observability_check_file
    observability_init_env
    observability_list_services
    echo
    p_muted "Compose: $(basename "$OBSERVABILITY_COMPOSE")"
    echo
    with_spinner "Starting observability stack" \
        docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" --env-file "$ENV_FILE" \
        up -d "${OBS_SERVICES[@]}" || die "Observability start failed."
    with_spinner "Applying backend observability configuration" \
        observability_apply_backend_override || die "Backend observability configuration failed."
    echo
    p_ok "Observability stack is up."
    observability_info
}

observability_down() {
    page_header "Observability · Down"
    check_docker_running
    observability_check_file
    observability_init_env
    observability_list_services
    echo
    with_spinner "Stopping observability stack" \
        docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" --env-file "$ENV_FILE" \
        rm -sf "${OBS_SERVICES[@]}" || die "Observability stop failed."
    with_spinner "Restoring backend application configuration" \
        observability_restore_backend_config || die "Backend configuration restore failed."
    echo
    p_ok "Observability stack stopped (volumes preserved)."
}

observability_reset() {
    local confirm=${1:-ask}
    page_header "Observability · Reset"
    check_docker_running
    observability_check_file
    observability_init_env
    observability_list_services
    echo
    draw_box "WARNING" "" \
        "  ${c_error}This deletes Loki / Tempo / Prometheus / Grafana data volumes.${c_reset}" \
        "  ${c_muted}Dashboards / datasources are re-provisioned on next boot.${c_reset}" \
        ""
    echo
    if [ "$confirm" = "ask" ]; then
        ask_yes_no "Proceed with reset?" "n" || { p_warn "Operation cancelled."; return; }
    fi
    with_spinner "Removing observability containers" \
        docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" --env-file "$ENV_FILE" \
        rm -sfv "${OBS_SERVICES[@]}" || die "Observability container removal failed."
    with_spinner "Removing observability volumes" observability_remove_volumes || \
        die "Observability volume removal failed."
    with_spinner "Restoring backend application configuration" \
        observability_restore_backend_config || die "Backend configuration restore failed."
    p_ok "Observability state wiped."
}

observability_status() {
    page_header "Observability · Status"
    check_docker_running
    observability_check_file
    observability_init_env
    observability_list_services
    echo

    p_section "Containers"
    docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" --env-file "$ENV_FILE" \
        ps "${OBS_SERVICES[@]}"
    echo

    p_section "Resource usage"
    local container_ids
    container_ids=$(docker compose -f "$INFRA_COMPOSE" -f "$COMPOSE_FILE" -f "$OBSERVABILITY_COMPOSE" --env-file "$ENV_FILE" \
        ps -q "${OBS_SERVICES[@]}" 2>/dev/null)
    if [ -z "$container_ids" ]; then
        p_muted "No running containers — start the stack with 'observability up'."
        return
    fi
    # shellcheck disable=SC2086
    docker stats --no-stream \
        --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}" \
        $container_ids
}

observability_info() {
    local grafana_port="${GRAFANA_PORT:-3010}"
    local prometheus_port="${PROMETHEUS_PORT:-9090}"
    local tempo_port="${TEMPO_HTTP_PORT:-3200}"
    local loki_port="${LOKI_PORT:-3100}"
    local otel_http="${OTEL_OTLP_HTTP_PORT:-4318}"

    page_header "Observability · Info"
    draw_box "URLs" \
        "" \
        "  ${c_bold}Grafana${c_reset}     http://localhost:${grafana_port}   ${c_muted}(admin/admin; anon viewer enabled)${c_reset}" \
        "  ${c_bold}Prometheus${c_reset}  http://localhost:${prometheus_port}" \
        "  ${c_bold}Tempo${c_reset}       http://localhost:${tempo_port}     ${c_muted}(query via Grafana — not for direct use)${c_reset}" \
        "  ${c_bold}Loki${c_reset}        http://localhost:${loki_port}     ${c_muted}(query via Grafana — not for direct use)${c_reset}" \
        ""
    echo
    draw_box "Wire the backend at the collector" \
        "" \
        "  Add to docker/.env:" \
        "" \
        "    ${c_accent}OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:${otel_http}${c_reset}" \
        "    ${c_accent}OTEL_TRACES_ENABLED=true${c_reset}" \
        "" \
        "  Then restart the backend:" \
        "" \
        "    ${c_muted}docker compose -f docker-compose.infra.yml -f docker-compose.dev.yml restart backend${c_reset}" \
        ""
    echo
    draw_box "Dashboards" \
        "" \
        "  Open Grafana → ${c_bold}Orkestra${c_reset} folder → ${c_bold}Tenant traces + logs${c_reset}" \
        "  Type a tenant UUID; trace, log, and metric panels are tenant-scoped." \
        "  Click any trace_id in a log → jumps to Tempo. Click a span → jumps to logs." \
        ""
}

# ---------------------------------------------------------------------------
# Unified logs picker (SKU profiles + full-stack aware)
# ---------------------------------------------------------------------------

# Populates the global SERVICES array and SERVICE_FILE map.
declare -a SERVICES=()
declare -A SERVICE_FILE=()

list_all_services() {
    local profile=$1
    SERVICES=()
    SERVICE_FILE=()

    local -a files=()
    case "$profile" in
        fullstack)
            files+=("$INFRA_COMPOSE")
            files+=("$COMPOSE_FILE")
            ;;
        observability)
            for svc in "${OBS_SERVICES[@]}"; do
                SERVICES+=("$svc")
                SERVICE_FILE["$svc"]="$OBSERVABILITY_COMPOSE"
            done
            return 0
            ;;
        *)
            return 1
            ;;
    esac

    local seen=""
    local f svc
    for f in "${files[@]}"; do
        [ ! -f "$f" ] && continue
        while IFS= read -r svc; do
            [ -z "$svc" ] && continue
            case " $seen " in *" $svc "*) continue ;; esac
            SERVICES+=("$svc")
            SERVICE_FILE["$svc"]="$f"
            seen="$seen $svc"
        done < <(get_services "$f")
    done
}

PICKED_SERVICE=""
PICKED_COMPOSE=""

interactive_service_picker() {
    local profile=$1
    list_all_services "$profile"

    if [ "${#SERVICES[@]}" -eq 0 ]; then
        p_err "No services found for profile '$profile'."
        return 1
    fi

    # fzf upgrade: fuzzy-searchable picker with running-status annotations
    if [ "$HAS_FZF" = true ] && [ "$HAS_TTY" = true ]; then
        local lines="" svc file status_label
        for svc in "${SERVICES[@]}"; do
            file="${SERVICE_FILE[$svc]}"
            if is_service_running "$file" "$svc"; then
                status_label="● running"
            else
                status_label="○ stopped"
            fi
            lines+=$(printf '%-25s  [%s]  %s\n' "$svc" "$(basename "$file")" "$status_label")$'\n'
        done
        local picked
        picked=$(printf '%s' "$lines" | fzf --prompt="service > " --height=40% --reverse \
            --header="Select service for logs" --no-mouse 2>/dev/null) || return 1
        PICKED_SERVICE=$(echo "$picked" | awk '{print $1}')
        PICKED_COMPOSE="${SERVICE_FILE[$PICKED_SERVICE]}"
        return 0
    fi

    # Pure-bash numbered picker
    p_section "Available services"
    local i svc file status_chip
    for i in "${!SERVICES[@]}"; do
        svc="${SERVICES[$i]}"
        file="${SERVICE_FILE[$svc]}"
        if is_service_running "$file" "$svc"; then
            status_chip="${c_success}${ic_dot} running${c_reset}"
        else
            status_chip="${c_muted}${ic_dot} stopped${c_reset}"
        fi
        printf '  %s%2d)%s %-25s %s[%s]%s  %b\n' \
            "$c_accent" "$((i + 1))" "$c_reset" \
            "$svc" \
            "$c_muted" "$(basename "$file")" "$c_reset" \
            "$status_chip"
    done
    echo
    printf '%s%s Select a service [1-%d] or q to cancel: %s' \
        "$c_prompt" "$ic_arrow" "${#SERVICES[@]}" "$c_reset"
    local choice
    read -r choice
    if [ "$choice" = "q" ] || [ "$choice" = "Q" ]; then
        return 1
    fi
    if ! [[ "$choice" =~ ^[0-9]+$ ]] || [ "$choice" -lt 1 ] || [ "$choice" -gt "${#SERVICES[@]}" ]; then
        p_err "Invalid selection."
        return 1
    fi
    PICKED_SERVICE="${SERVICES[$((choice - 1))]}"
    PICKED_COMPOSE="${SERVICE_FILE[$PICKED_SERVICE]}"
    return 0
}

view_logs() {
    local compose_file=$1 service=$2
    local follow=${3:-true} lines=${4:-100} timestamps=${5:-false}

    if ! is_service_running "$compose_file" "$service"; then
        p_warn "Service '$service' is not currently running."
        if ! ask_yes_no "Show its buffered logs anyway?" "y"; then
            return 0
        fi
    fi

    # If the compose-file's project has no matching container, but a
    # container with this service label exists under another project (e.g.
    # the SKU-profile merge case), tail that container directly via
    # `docker logs` instead of `docker compose logs` (which would target an
    # empty project and exit silently).
    local cid_in_project cid_anywhere
    cid_in_project=$(docker compose -f "$compose_file" ps -q "$service" 2> /dev/null | head -1)
    if [ -z "$cid_in_project" ]; then
        cid_anywhere=$(resolve_running_container "$compose_file" "$service")
    fi

    local -a cmd
    if [ -z "$cid_in_project" ] && [ -n "$cid_anywhere" ]; then
        local owner_project
        owner_project=$(docker inspect --format \
            '{{ index .Config.Labels "com.docker.compose.project" }}' \
            "$cid_anywhere" 2> /dev/null)
        p_muted "  Service is running under a different compose project (${owner_project:-unknown}); tailing the container directly."
        cmd=(docker logs)
        [ "$timestamps" = "true" ] && cmd+=(--timestamps)
        if [ "$follow" = "true" ]; then
            cmd+=(--follow)
        else
            cmd+=(--tail "$lines")
        fi
        cmd+=("$cid_anywhere")
    else
        cmd=(docker compose -f "$compose_file" logs)
        [ "$timestamps" = "true" ] && cmd+=(--timestamps)
        if [ "$follow" = "true" ]; then
            cmd+=(--follow)
        else
            cmd+=(--tail="$lines")
        fi
        cmd+=("$service")
    fi

    echo
    # Build a space-joined command string; avoid ${cmd[*]} here because the
    # script sets IFS=$'\n\t', which would make the default [*] join use
    # newlines and print each arg on its own line.
    local cmd_display
    printf -v cmd_display '%s ' "${cmd[@]}"
    p_muted "${ic_arrow} ${cmd_display% }"
    p_muted "  Press Ctrl-C to stop following"
    draw_rule
    # Use a sub-shell so Ctrl-C doesn't kill orkestra.sh itself
    ( "${cmd[@]}" ) || true
}

logs_interactive() {
    local profile=$1
    page_header "Logs"
    if ! interactive_service_picker "$profile"; then
        p_warn "Cancelled."
        return 0
    fi
    view_logs "$PICKED_COMPOSE" "$PICKED_SERVICE" "true" "100" "false"
}

logs_cli() {
    local profile=$1 service=$2
    shift 2
    local follow="false" lines="100" timestamps="false"

    while [ $# -gt 0 ]; do
        case "$1" in
            -f | --follow) follow="true"; shift ;;
            -n | --lines) lines="$2"; shift 2 ;;
            -t | --timestamps) timestamps="true"; shift ;;
            *) die "Unknown logs flag: $1" ;;
        esac
    done

    list_all_services "$profile"
    if [ -z "${SERVICE_FILE[$service]:-}" ]; then
        p_err "Service '$service' not found in profile '$profile'."
        p_muted "Known services:"
        local svc
        for svc in "${SERVICES[@]}"; do
            printf '  %s %s\n' "$ic_bullet" "$svc"
        done
        exit 1
    fi
    view_logs "${SERVICE_FILE[$service]}" "$service" "$follow" "$lines" "$timestamps"
}

# ---------------------------------------------------------------------------
# TUI menus
# ---------------------------------------------------------------------------

show_main_menu() {
    [ "$HAS_TTY" = true ] && clear 2>/dev/null || true
    draw_status_line
    draw_rule
    echo
    draw_box "Orkestra Stack Manager" \
        "" \
        "  ${c_accent}1${c_reset} ${c_bold}Full stack${c_reset}              ${c_muted}(dev / staging / production)${c_reset}" \
        "     ${c_muted}ENV autodetected from docker/.env${c_reset}" \
        "" \
        "  ${c_accent}2${c_reset} ${c_bold}Observability${c_reset}           ${c_muted}(Loki, Tempo, Prometheus, Grafana)${c_reset}" \
        "     ${c_muted}Self-hosted OTEL stack — runs alongside any app stack${c_reset}" \
        "" \
        "  ${c_accent}3${c_reset} ${c_bold}Setup${c_reset}                   ${c_muted}(guided docker/.env configuration)${c_reset}" \
        "" \
        "  ${c_accent}4${c_reset} ${c_bold}Quit${c_reset}" \
        ""
}

show_fullstack_menu() {
    page_header "Full stack"
    draw_box "Select operation" \
        "" \
        "  ${c_accent}1${c_reset}  Deploy             ${c_muted}(with scope selection)${c_reset}" \
        "  ${c_accent}2${c_reset}  Stop               ${c_muted}(app + optional infra)${c_reset}" \
        "  ${c_accent}3${c_reset}  Status             ${c_muted}(ps + health + stats)${c_reset}" \
        "  ${c_accent}4${c_reset}  Logs               ${c_muted}(service picker)${c_reset}" \
        "  ${c_accent}5${c_reset}  Back to main menu" \
        ""
}

show_observability_menu() {
    page_header "Observability stack"
    draw_box "Select operation" \
        "" \
        "  ${c_accent}1${c_reset}  Up                 ${c_muted}(start the stack — Tempo, Prometheus, Loki, Grafana, …)${c_reset}" \
        "  ${c_accent}2${c_reset}  Down               ${c_muted}(stop containers, keep volumes)${c_reset}" \
        "  ${c_accent}3${c_reset}  Reset              ${c_muted}(wipe dashboards / metrics / logs — destructive)${c_reset}" \
        "  ${c_accent}4${c_reset}  Status             ${c_muted}(ps + resource usage)${c_reset}" \
        "  ${c_accent}5${c_reset}  Logs               ${c_muted}(service picker)${c_reset}" \
        "  ${c_accent}6${c_reset}  Info               ${c_muted}(URLs + backend wiring recipe)${c_reset}" \
        "  ${c_accent}7${c_reset}  Back to main menu" \
        ""
}

observability_menu_loop() {
    observability_init_env
    while true; do
        show_observability_menu
        printf '%s%s Select operation [1-7]: %s' "$c_prompt" "$ic_arrow" "$c_reset"
        local choice
        read -r choice
        case "$choice" in
            1) observability_up; pause_for_return ;;
            2) observability_down; pause_for_return ;;
            3) observability_reset "ask"; pause_for_return ;;
            4) observability_status; pause_for_return ;;
            5) logs_interactive "observability"; pause_for_return ;;
            6) observability_info; pause_for_return ;;
            7) PROFILE=""; return ;;
            *) p_warn "Invalid selection"; sleep 1 ;;
        esac
    done
}

fullstack_menu_loop() {
    fullstack_init_env
    while true; do
        show_fullstack_menu
        printf '%s%s Select operation [1-5]: %s' "$c_prompt" "$ic_arrow" "$c_reset"
        local choice
        read -r choice
        case "$choice" in
            1) fullstack_deploy_interactive; pause_for_return ;;
            2) fullstack_stop "ask"; pause_for_return ;;
            3) fullstack_status; pause_for_return ;;
            4) logs_interactive "fullstack"; pause_for_return ;;
            5) PROFILE=""; return ;;
            *) p_warn "Invalid selection"; sleep 1 ;;
        esac
    done
}

# ---------------------------------------------------------------------------
# CLI dispatcher
# ---------------------------------------------------------------------------

show_version() {
    printf '%sOrkestra Stack Manager%s v%s\n' "$c_bold" "$c_reset" "$ORKESTRA_VERSION"
    printf '%scapabilities:%s color=%s unicode=%s gum=%s fzf=%s tty=%s\n' \
        "$c_muted" "$c_reset" "$HAS_COLOR" "$HAS_UNICODE" "$HAS_GUM" "$HAS_FZF" "$HAS_TTY"
}

show_usage() {
    cat << EOF
${c_bold}${c_header}Orkestra — unified stack management${c_reset}

${c_bold}USAGE${c_reset}
  ./orkestra.sh                    ${c_muted}# interactive TUI (main menu)${c_reset}
  ./orkestra.sh <command> [args]   ${c_muted}# non-interactive CLI${c_reset}

${c_bold}FIRST-TIME SETUP${c_reset}
  ${c_accent}init${c_reset} [--quick] [--force] [--yes]   Guided docker/.env setup wizard (interactive).
                                   --quick / --force / --yes, or a non-TTY, delegate
                                   to scripts/init.sh (scaffold secrets + JWT, no prompts).

${c_bold}FULL STACK${c_reset} ${c_muted}(uses ENV from docker/.env or ENV=... prefix)${c_reset}
  ${c_accent}deploy${c_reset} [--scope SCOPE] [--rebuild] [--yes]
                                   Deploy. SCOPE: all | backend | frontend-admin |
                                                 frontend-admin+backend | infra
  ${c_accent}stop${c_reset} [--with-infra]               Stop application services (+ infra)
  ${c_accent}status${c_reset}                            Containers + health + resources
  ${c_accent}logs${c_reset} <service> [flags]            View logs for a full-stack service

${c_bold}OBSERVABILITY${c_reset} ${c_muted}(self-hosted OTEL stack — runs alongside any app stack)${c_reset}
  ${c_accent}observability up${c_reset}                  Start Tempo + Prometheus + Loki + Promtail + Grafana
  ${c_accent}observability down${c_reset}                Stop containers (volumes kept)
  ${c_accent}observability reset${c_reset} [--yes]       Wipe volumes (dashboards/metrics/logs erased)
  ${c_accent}observability status${c_reset}              Containers + resource usage
  ${c_accent}observability info${c_reset}                URLs + backend wiring recipe
  ${c_accent}observability logs${c_reset} <svc> [flags]  View logs for an observability service

${c_bold}LOG FLAGS${c_reset}
  ${c_accent}-f${c_reset}, ${c_accent}--follow${c_reset}           Follow log output (tail -f)
  ${c_accent}-n${c_reset}, ${c_accent}--lines${c_reset} N          Lines to show when not following (default 100)
  ${c_accent}-t${c_reset}, ${c_accent}--timestamps${c_reset}       Show timestamps

${c_bold}SHORTCUTS${c_reset}
  ${c_accent}-h${c_reset}, ${c_accent}--help${c_reset}             Show this message
  ${c_accent}-v${c_reset}, ${c_accent}--version${c_reset}          Show version + capabilities

${c_bold}EXAMPLES${c_reset}
  ./orkestra.sh
  ENV=development ./orkestra.sh deploy --scope backend --rebuild --yes
  ./orkestra.sh logs backend -f
  ./orkestra.sh observability up
  ./orkestra.sh observability logs grafana -f

${c_bold}ENHANCEMENTS${c_reset}
  Install ${c_accent}gum${c_reset} (charm.sh) for prettier prompts, spinners, and choose menus.
  Install ${c_accent}fzf${c_reset} for fuzzy-searchable service selection in the logs picker.
  Both are optional — orkestra.sh works with neither.
EOF
}

cli_dispatch() {
    local cmd=$1
    shift

    case "$cmd" in
        -h | --help | help)
            show_usage
            exit 0
            ;;
        -v | --version | version)
            show_version
            exit 0
            ;;

        init)
            # Guided wizard by default (interactive TTY, no flags). Any flag or a
            # non-TTY delegates to scripts/init.sh (the fast / CI scaffold path).
            if [ $# -eq 0 ] && [ -t 0 ] && [ -t 1 ]; then
                env_wizard
                exit 0
            fi
            # --quick is a wizard-only alias meaning "skip the wizard, plain
            # scaffold". Drop it before delegating so init.sh sees no unknown flag.
            local a passthru=()
            for a in "$@"; do
                case "$a" in
                    --quick) : ;;
                    *) passthru+=("$a") ;;
                esac
            done
            exec bash "$SCRIPT_DIR/scripts/init.sh" ${passthru[@]+"${passthru[@]}"}
            ;;

        deploy)
            fullstack_init_env
            local scope="all" rebuild="no" yes_flag="yes"
            while [ $# -gt 0 ]; do
                case "$1" in
                    --scope) scope="$2"; shift 2 ;;
                    --rebuild) rebuild="yes"; shift ;;
                    --yes | -y) yes_flag="yes"; shift ;;
                    *) die "Unknown flag: $1" ;;
                esac
            done
            case "$scope" in
                all | backend | frontend-admin | frontend-admin+backend | infra) ;;
                *) die "Invalid scope: $scope" ;;
            esac
            fullstack_deploy_cli "$scope" "$rebuild" "$yes_flag"
            ;;

        stop)
            fullstack_init_env
            local with_infra="no"
            while [ $# -gt 0 ]; do
                case "$1" in
                    --with-infra) with_infra="yes"; shift ;;
                    *) die "Unknown flag: $1" ;;
                esac
            done
            fullstack_stop "$with_infra"
            ;;

        status)
            fullstack_init_env
            fullstack_status
            ;;

        logs)
            fullstack_init_env
            local service=${1:-}
            [ -n "$service" ] && shift
            [ -z "$service" ] && die "Usage: ./orkestra.sh logs <service> [-f] [-n N] [-t]"
            logs_cli "fullstack" "$service" "$@"
            ;;

        observability)
            local subcmd=${1:-}
            [ -n "$subcmd" ] && shift
            case "$subcmd" in
                up) observability_up ;;
                down) observability_down ;;
                reset)
                    local confirm="ask"
                    while [ $# -gt 0 ]; do
                        case "$1" in
                            --yes | -y) confirm="yes"; shift ;;
                            *) die "Unknown flag: $1" ;;
                        esac
                    done
                    observability_reset "$confirm"
                    ;;
                status) observability_status ;;
                info) observability_info ;;
                logs)
                    local service=${1:-}
                    [ -n "$service" ] && shift
                    [ -z "$service" ] && die "Usage: ./orkestra.sh observability logs <service> [-f] [-n N] [-t]"
                    # logs_cli/list_all_services needs ENV/COMPOSE_FILE +
                    # the stack identity resolved (up/down/reset/status
                    # resolve it themselves; logs is the one entrypoint
                    # here that bypasses those functions).
                    observability_init_env
                    logs_cli "observability" "$service" "$@"
                    ;;
                "") die "Missing subcommand. Try --help." ;;
                *) die "Unknown observability subcommand: $subcmd. Valid: up | down | reset | status | info | logs" ;;
            esac
            ;;

        *) die "Unknown command: $cmd. Try --help." ;;
    esac
}

# ---------------------------------------------------------------------------
# Main entry point
# ---------------------------------------------------------------------------

main() {
    # CLI mode: any argument triggers non-interactive dispatch.
    if [ $# -gt 0 ]; then
        cli_dispatch "$@"
        exit 0
    fi

    # Non-TTY (e.g. piped): no interactive menu makes sense.
    if [ "$HAS_TTY" != true ]; then
        show_usage
        exit 0
    fi

    while true; do
        show_main_menu
        printf '%s%s Select option [1-4]: %s' "$c_prompt" "$ic_arrow" "$c_reset"
        local choice
        read -r choice
        case "$choice" in
            1) fullstack_menu_loop ;;
            2) observability_menu_loop ;;
            3) env_wizard; pause_for_return ;;
            4)
                echo
                printf '%s%s Goodbye!%s\n' "$c_success" "$ic_ok" "$c_reset"
                exit 0
                ;;
            *) p_warn "Invalid selection"; sleep 1 ;;
        esac
    done
}

# Only run the CLI when executed directly — sourcing (e.g. from tests) must not
# launch the menu/dispatch.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi
