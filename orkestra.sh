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

ORKESTRA_VERSION="1.0.0"

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
DOCKER_DIR="$PROJECT_ROOT/docker"

ENV_FILE="$DOCKER_DIR/.env"

# ADR-0005 Phase D — self-hosted observability stack (Loki, Tempo,
# Prometheus, Promtail, otel-collector, Grafana). Orthogonal to the
# SKU profile / full-stack split: you typically run it alongside
# whichever app stack is up.
OBSERVABILITY_COMPOSE="$DOCKER_DIR/docker-compose.observability.yml"

# Profile state — "fullstack", a SKU profile name, or "" (at profile menu)
PROFILE=""

# SKU profiles published to GHCR. Order matters: it drives the TUI picker.
SKU_PROFILES=(starter billing ai saas enterprise)

# Per-profile compose file, env file, backend URL, refresh mode, and infra
# layering. SKU profiles pull a pre-built image from GHCR and expect
# docker-compose.infra.yml to be running first.
declare -A PROFILE_COMPOSE
declare -A PROFILE_ENV
declare -A PROFILE_BACKEND_URL
declare -A PROFILE_FRONTEND_URL
declare -A PROFILE_REFRESH_MODE      # "pull"
declare -A PROFILE_LAYER_INFRA       # "yes" or "no"
declare -A PROFILE_DESCRIPTION

init_profile_configs() {
    local p
    for p in "${SKU_PROFILES[@]}"; do
        PROFILE_COMPOSE[$p]="$DOCKER_DIR/docker-compose.$p.yml"
        PROFILE_ENV[$p]="$ENV_FILE"
        PROFILE_BACKEND_URL[$p]="http://localhost:3000"
        PROFILE_FRONTEND_URL[$p]=""
        PROFILE_REFRESH_MODE[$p]="pull"
        PROFILE_LAYER_INFRA[$p]="yes"
    done
    PROFILE_DESCRIPTION[starter]="Core only (no addons) — pulled from GHCR"
    PROFILE_DESCRIPTION[billing]="billing + documents + company addons"
    PROFILE_DESCRIPTION[ai]="graph + aimodels + rag + agents + sales addons"
    PROFILE_DESCRIPTION[saas]="subscriptions + payments + compliance + identity addons"
    PROFILE_DESCRIPTION[enterprise]="Every addon (alias for :latest)"
}

init_profile_configs

is_sku_profile() {
    local name=$1 p
    for p in "${SKU_PROFILES[@]}"; do
        [ "$p" = "$name" ] && return 0
    done
    return 1
}

# Render the SKU profile list as a comma-separated string for error messages.
# Necessary because the script uses IFS=$'\n\t', so "${SKU_PROFILES[*]}" would
# join with newlines and break single-line error output.
sku_profile_list() {
    local IFS=', '
    printf '%s' "${SKU_PROFILES[*]}"
}

# Env-detect utility for full-stack ENV resolution
source "$PROJECT_ROOT/scripts/env-detect.sh"

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

# ---------------------------------------------------------------------------
# Shared infrastructure helpers
# ---------------------------------------------------------------------------

check_docker_running() {
    if ! docker info > /dev/null 2>&1; then
        die "Docker daemon is not running"
    fi
}

ensure_network_exists() {
    local network_name="orkestra-network"
    if ! docker network inspect "$network_name" > /dev/null 2>&1; then
        p_step "Creating Docker network: $network_name"
        docker network create "$network_name" > /dev/null
        p_ok "Network created"
    else
        p_ok "Network $network_name exists"
    fi
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
    # under a different project (e.g. infra+SKU stacks are merged under one
    # project name like "orkestra-enterprise", so the bare infra compose
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
# Generic profile operations
# ---------------------------------------------------------------------------
#
# A "profile" here is any entry in PROFILE_COMPOSE — the five SKU profiles
# (starter/billing/ai/saas/enterprise). Each pulls a pre-built image from
# GHCR; everything else is the same operation surface
# (deploy/stop/reset/status/info/logs).

# Echo the docker-compose -f / --env-file argument list for the named profile,
# one token per line. Capture with: mapfile -t args < <(_profile_compose_args foo)
_profile_compose_args() {
    local name=$1
    if [ "${PROFILE_LAYER_INFRA[$name]:-no}" = "yes" ]; then
        printf -- '-f\n%s\n' "$DOCKER_DIR/docker-compose.infra.yml"
    fi
    printf -- '-f\n%s\n' "${PROFILE_COMPOSE[$name]}"
    local env_file="${PROFILE_ENV[$name]:-}"
    if [ -n "$env_file" ] && [ -f "$env_file" ]; then
        printf -- '--env-file\n%s\n' "$env_file"
    fi
}

profile_check_prereqs() {
    local name=$1
    check_docker_running

    local compose_file="${PROFILE_COMPOSE[$name]:-}"
    [ -z "$compose_file" ] && die "Unknown profile: $name"
    [ ! -f "$compose_file" ] && die "$compose_file not found"

    if [ "${PROFILE_LAYER_INFRA[$name]}" = "yes" ]; then
        local infra_file="$DOCKER_DIR/docker-compose.infra.yml"
        [ ! -f "$infra_file" ] && die "$infra_file not found"
    fi

    local env_file="${PROFILE_ENV[$name]:-}"
    if [ -n "$env_file" ] && [ ! -f "$env_file" ]; then
        p_warn "$env_file not found — required secrets must be exported in the shell"
    fi

    ensure_network_exists
}

# Render the post-deploy access box. SKU profiles are backend-only (no frontend).
_profile_show_access_box() {
    local name=$1
    local backend_url="${PROFILE_BACKEND_URL[$name]:-http://localhost:3000}"
    local frontend_url="${PROFILE_FRONTEND_URL[$name]:-}"

    if [ -n "$frontend_url" ]; then
        draw_box "${name^} stack is up" \
            "" \
            "  Admin frontend ${c_info}${frontend_url}${c_reset}" \
            "  Backend API    ${c_info}${backend_url}${c_reset}" \
            "  API docs       ${c_info}${backend_url}/docs${c_reset}" \
            "  Health         ${c_info}${backend_url}/health${c_reset}" \
            "" \
            "  ${c_muted}Generate an admin token for first login:${c_reset}" \
            "  ${c_accent}ORKESTRA_API_URL=${backend_url} ./scripts/devtoken.sh administrator${c_reset}" \
            ""
    else
        draw_box "${name^} stack is up" \
            "" \
            "  Backend API    ${c_info}${backend_url}${c_reset}" \
            "  API docs       ${c_info}${backend_url}/docs${c_reset}" \
            "  Health         ${c_info}${backend_url}/health${c_reset}" \
            "" \
            "  ${c_muted}Image pulled from GHCR. Toggle addons at /admin/modules.${c_reset}" \
            "  ${c_muted}Generate an admin token for first login:${c_reset}" \
            "  ${c_accent}ORKESTRA_API_URL=${backend_url} ./scripts/devtoken.sh administrator${c_reset}" \
            ""
    fi
}

# profile_deploy <name> [refresh=yes|no]
# refresh=yes: pull the SKU image from GHCR before bringing up. refresh=no: cached.
profile_deploy() {
    local name=$1 refresh=${2:-no}
    PROFILE="$name"
    local title
    title=$(echo "${name:0:1}" | tr '[:lower:]' '[:upper:]')${name:1}
    page_header "${title} · Deploy / Update"
    profile_check_prereqs "$name"
    echo

    local -a base_args
    mapfile -t base_args < <(_profile_compose_args "$name")

    # Pull mode: refresh the image before bringing the stack up.
    if [ "$refresh" = "yes" ] && [ "${PROFILE_REFRESH_MODE[$name]}" = "pull" ]; then
        if ! with_spinner "Pulling latest image from GHCR" \
            docker compose "${base_args[@]}" pull; then
            die "Image pull failed"
        fi
    fi

    local -a up_cmd=(docker compose "${base_args[@]}" up -d)
    if [ "$refresh" = "yes" ] && [ "${PROFILE_REFRESH_MODE[$name]}" = "build" ]; then
        up_cmd+=(--build)
    fi

    local msg="Starting containers"
    if [ "$refresh" = "yes" ] && [ "${PROFILE_REFRESH_MODE[$name]}" = "build" ]; then
        msg="Building and starting containers (first run can take several minutes)"
    fi

    if ! with_spinner "$msg" "${up_cmd[@]}"; then
        die "Deployment failed"
    fi

    echo
    _profile_show_access_box "$name"
}

profile_stop() {
    local name=$1
    PROFILE="$name"
    local title
    title=$(echo "${name:0:1}" | tr '[:lower:]' '[:upper:]')${name:1}
    page_header "${title} · Stop"
    check_docker_running
    echo

    local -a base_args
    mapfile -t base_args < <(_profile_compose_args "$name")

    if ! with_spinner "Stopping containers (volumes kept)" \
        docker compose "${base_args[@]}" down; then
        die "Stop failed"
    fi

    echo
    p_muted "  Data volumes were NOT removed. Use 'reset' to wipe them."
}

profile_reset() {
    local name=$1 confirm=${2:-ask}
    PROFILE="$name"
    local title
    title=$(echo "${name:0:1}" | tr '[:lower:]' '[:upper:]')${name:1}
    page_header "${title} · Reset (wipe volumes)"
    echo
    draw_box "Destructive operation" \
        "" \
        "  ${c_error}${ic_warn} This will DELETE the ${name} stack's mongo and redis volumes.${c_reset}" \
        "  ${c_error}   All users, sessions, and module configs will be lost.${c_reset}" \
        ""

    if [ "$confirm" != "yes" ]; then
        echo
        if ! ask_yes_no "Are you sure you want to wipe the ${name} data?" "n"; then
            p_warn "Reset cancelled."
            return 0
        fi
    fi

    profile_check_prereqs "$name"
    echo

    local -a base_args
    mapfile -t base_args < <(_profile_compose_args "$name")

    if ! with_spinner "Removing containers and volumes" \
        docker compose "${base_args[@]}" down -v; then
        die "Reset failed during teardown"
    fi

    local -a up_cmd=(docker compose "${base_args[@]}" up -d)
    [ "${PROFILE_REFRESH_MODE[$name]}" = "build" ] && up_cmd+=(--build)

    if ! with_spinner "Restarting" "${up_cmd[@]}"; then
        die "Reset failed during restart"
    fi

    echo
    p_ok "${title} stack reset complete"
    p_muted "  Collections + indexes are recreated on boot by the module registry."
}

profile_status() {
    local name=$1
    PROFILE="$name"
    local title
    title=$(echo "${name:0:1}" | tr '[:lower:]' '[:upper:]')${name:1}
    page_header "${title} · Status"
    check_docker_running
    echo

    local -a base_args
    mapfile -t base_args < <(_profile_compose_args "$name")

    p_section "Containers"
    docker compose "${base_args[@]}" ps
    echo

    p_section "Backend /health"
    local backend_url="${PROFILE_BACKEND_URL[$name]:-http://localhost:3000}"
    if command -v curl > /dev/null 2>&1; then
        local health_json
        if health_json=$(curl -s --max-time 3 "${backend_url}/health" 2>&1); then
            if command -v jq > /dev/null 2>&1; then
                echo "$health_json" | jq -r '"  status: \(.status)\n  checks:\n    mongodb: \(.checks.mongodb)\n    redis:   \(.checks.redis)"' 2>/dev/null || printf '  %s\n' "$health_json"
            else
                printf '  %s\n' "$health_json"
            fi
            if echo "$health_json" | grep -q '"status":"healthy"'; then
                p_ok "Backend reports healthy"
            else
                p_warn "Backend reachable but not healthy"
            fi
        else
            p_err "Backend unreachable at ${backend_url}"
        fi
    else
        p_warn "curl not installed — skipping HTTP health check"
    fi
    echo

    p_section "Resource usage"
    local names
    names=$(docker ps --filter "name=$name" --format '{{.Names}}' 2>/dev/null || true)
    if [ -n "$names" ]; then
        # shellcheck disable=SC2086
        docker stats --no-stream \
            --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}" $names
    else
        p_warn "No ${name} containers running"
    fi
}

# Generic info screen for SKU profiles.
profile_info() {
    local name=$1
    PROFILE="$name"
    local title
    title=$(echo "${name:0:1}" | tr '[:lower:]' '[:upper:]')${name:1}
    page_header "${title} · Profile Info"

    draw_box "${title} profile" \
        "" \
        "  ${PROFILE_DESCRIPTION[$name]:-}" \
        "" \
        "  Image  ${c_info}ghcr.io/orkestra-cc/orkestra/backend:${name}${c_reset}" \
        "  Layer  ${c_muted}docker-compose.infra.yml + docker-compose.${name}.yml${c_reset}" \
        ""
    echo

    local backend_url="${PROFILE_BACKEND_URL[$name]:-http://localhost:3000}"
    p_section "Access points"
    printf '  Backend API    %s%s%s\n' "$c_info" "$backend_url" "$c_reset"
    printf '  API docs       %s%s/docs%s\n' "$c_info" "$backend_url" "$c_reset"
    printf '  OpenAPI JSON   %s%s/openapi.json%s\n' "$c_info" "$backend_url" "$c_reset"
    printf '  Health         %s%s/health%s\n' "$c_info" "$backend_url" "$c_reset"
    echo

    p_section "Refresh image from GHCR"
    printf '  %s./orkestra.sh profile %s deploy --pull%s\n' "$c_accent" "$name" "$c_reset"
    echo

    p_section "Generate a dev token for first login"
    printf '  %sORKESTRA_API_URL=%s ./scripts/devtoken.sh administrator%s\n' \
        "$c_accent" "$backend_url" "$c_reset"
    echo

    p_section "Toggle addons"
    p_muted "  Visit /admin/modules in the operator console to enable/disable addons."
    p_muted "  Build tags decide what is installable; runtime config decides what is on."
}

# ---------------------------------------------------------------------------
# Full-stack profile operations
# ---------------------------------------------------------------------------

# Map detected ENV to compose file, branch, DB, URLs, env-chip color.
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
            FRONTEND_URL="https://stage.orkestra.com"
            BACKEND_URL="https://stage.orkestra.com/api"
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
}

fullstack_init_env() {
    if [ ! -f "$ENV_FILE" ]; then
        p_warn "docker/.env not found — looks like a fresh checkout."
        if [ -t 0 ] && [ -t 1 ]; then
            if ask_yes_no "Run scripts/init.sh now to scaffold .env + JWT keys?" "y"; then
                bash "$SCRIPT_DIR/scripts/init.sh" || die "init failed — fix the error above and retry"
            else
                die "Cannot proceed without docker/.env. Run: make init (or scripts/init.sh)"
            fi
        else
            die "docker/.env missing. Non-interactive shell — run: make init (or scripts/init.sh) first."
        fi
    fi
    if ! detect_environment > /dev/null 2>&1; then
        die "Cannot detect ENV. Set ENV=development|staging|production in docker/.env or as a shell variable."
    fi
    ENV="$DETECTED_ENV"
    set_env_config
    PROFILE="fullstack"
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
        "Frontend only" \
        "Frontend + Backend" \
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
    local TIMESTAMP DEPLOYMENT_ID BACKEND_SERVICE FRONTEND_SERVICE
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    DEPLOYMENT_ID="${ENV}_${TIMESTAMP}"

    if [ "$ENV" = "development" ]; then
        BACKEND_SERVICE="orkestra-backend"
        FRONTEND_SERVICE="orkestra-frontend-admin"
    else
        BACKEND_SERVICE="backend"
        FRONTEND_SERVICE="frontend-admin"
    fi

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
    ensure_network_exists
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
                docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" build --no-cache \
                --build-arg VERSION="$VERSION" \
                --build-arg BUILD_TIME="$BUILD_TIME" \
                --build-arg GIT_COMMIT="$GIT_COMMIT" \
                "$BACKEND_SERVICE"
        fi
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "frontend-admin" ] || [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
            with_spinner "Building admin frontend image (no cache)" \
                docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" build --no-cache \
                --build-arg VERSION="$VERSION" \
                --build-arg BUILD_TIME="$BUILD_TIME" \
                --build-arg GIT_COMMIT="$GIT_COMMIT" \
                "$FRONTEND_SERVICE"
        fi
    fi

    # --- Deploy services ---
    p_section "Deploying services"
    with_spinner "Ensuring infrastructure services are running" \
        docker compose -f "$DOCKER_DIR/docker-compose.infra.yml" --env-file "$ENV_FILE" up -d
    sleep 5
    p_ok "Infrastructure ready"

    if [ "$DEPLOY_SCOPE" = "infra" ]; then
        p_ok "Infrastructure-only deployment complete"
    elif [ "$ENV" = "production" ]; then
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "backend" ] || [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
            with_spinner "Rolling-update backend" \
                docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d --no-deps "$BACKEND_SERVICE"
            with_spinner "Waiting for backend to be healthy" sleep 15
        fi
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "frontend-admin" ] || [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
            with_spinner "Deploying admin frontend" \
                docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d --no-deps "$FRONTEND_SERVICE"
        fi
        with_spinner "Waiting for services to stabilize" sleep 30
    else
        if [ "$DEPLOY_SCOPE" = "all" ]; then
            with_spinner "Stopping current services" \
                docker compose -f "$COMPOSE_FILE" down
            with_spinner "Starting new services" \
                docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d
        else
            if [ "$DEPLOY_SCOPE" = "backend" ]; then
                docker compose -f "$COMPOSE_FILE" stop "$BACKEND_SERVICE" 2> /dev/null || true
                with_spinner "Restarting backend" \
                    docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d "$BACKEND_SERVICE"
            fi
            if [ "$DEPLOY_SCOPE" = "frontend-admin" ]; then
                docker compose -f "$COMPOSE_FILE" stop "$FRONTEND_SERVICE" 2> /dev/null || true
                with_spinner "Restarting admin frontend" \
                    docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d "$FRONTEND_SERVICE"
            fi
            if [ "$DEPLOY_SCOPE" = "frontend-admin+backend" ]; then
                docker compose -f "$COMPOSE_FILE" stop "$FRONTEND_SERVICE" "$BACKEND_SERVICE" 2> /dev/null || true
                with_spinner "Restarting admin frontend and backend" \
                    docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d "$FRONTEND_SERVICE" "$BACKEND_SERVICE"
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
                    if bash "$health_script" "$ENV" > /tmp/health_check_output.txt 2>&1; then
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
                die "Health checks failed after $max_retries attempts — manual intervention required"
            fi
        else
            if [ -n "$health_script" ]; then
                if bash "$health_script" "$ENV"; then
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
        local AUTH_STATUS DOCS_STATUS
        AUTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "${BACKEND_URL}/api/v1/auth/health" || echo "000")
        [ "$AUTH_STATUS" = "200" ] && p_ok "Authentication endpoint healthy" || p_warn "Authentication endpoint returned: $AUTH_STATUS"
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

    with_spinner "Stopping application services" \
        docker compose -f "$COMPOSE_FILE" down

    local stop_infra=false
    case "$with_infra" in
        yes) stop_infra=true ;;
        no) stop_infra=false ;;
        ask) ask_yes_no "Also stop infrastructure services (MongoDB, Redis, ...)?" "n" && stop_infra=true ;;
    esac

    if [ "$stop_infra" = true ]; then
        with_spinner "Stopping infrastructure services" \
            docker compose -f "$DOCKER_DIR/docker-compose.infra.yml" down
    fi
}

fullstack_status() {
    page_header "Full stack · Status"
    check_docker_running
    echo

    p_section "Containers"
    docker compose -f "$DOCKER_DIR/docker-compose.infra.yml" -f "$COMPOSE_FILE" ps
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
# ingest). The compose file declares its own `name:` so its containers
# live in their own project — they're not mixed into the app's
# `docker compose ps` output and `docker compose down` on the app
# stack never accidentally takes them down.

observability_check_file() {
    if [ ! -f "$OBSERVABILITY_COMPOSE" ]; then
        die "Observability compose file not found at $OBSERVABILITY_COMPOSE"
    fi
}

observability_up() {
    page_header "Observability · Up"
    check_docker_running
    observability_check_file
    echo
    p_muted "Compose: $(basename "$OBSERVABILITY_COMPOSE")"
    echo
    with_spinner "Starting observability stack" \
        docker compose -f "$OBSERVABILITY_COMPOSE" up -d
    echo
    p_ok "Observability stack is up."
    observability_info
}

observability_down() {
    page_header "Observability · Down"
    check_docker_running
    observability_check_file
    echo
    with_spinner "Stopping observability stack" \
        docker compose -f "$OBSERVABILITY_COMPOSE" down
    echo
    p_ok "Observability stack stopped (volumes preserved)."
}

observability_reset() {
    local confirm=${1:-ask}
    page_header "Observability · Reset"
    check_docker_running
    observability_check_file
    echo
    draw_box "WARNING" "" \
        "  ${c_error}This deletes Loki / Tempo / Prometheus / Grafana data volumes.${c_reset}" \
        "  ${c_muted}Dashboards / datasources are re-provisioned on next boot.${c_reset}" \
        ""
    echo
    if [ "$confirm" = "ask" ]; then
        ask_yes_no "Proceed with reset?" "n" || { p_warn "Operation cancelled."; return; }
    fi
    with_spinner "Removing observability stack and volumes" \
        docker compose -f "$OBSERVABILITY_COMPOSE" down -v
    p_ok "Observability state wiped."
}

observability_status() {
    page_header "Observability · Status"
    check_docker_running
    observability_check_file
    echo

    p_section "Containers"
    docker compose -f "$OBSERVABILITY_COMPOSE" ps
    echo

    p_section "Resource usage"
    local container_ids
    container_ids=$(docker compose -f "$OBSERVABILITY_COMPOSE" ps -q 2>/dev/null)
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
        "    ${c_accent}OTEL_EXPORTER_OTLP_ENDPOINT=http://orkestra-otel:${otel_http}${c_reset}" \
        "    ${c_accent}OTEL_TRACES_ENABLED=true${c_reset}" \
        "" \
        "  Then restart the backend:" \
        "" \
        "    ${c_muted}docker compose -f docker-compose.dev.yml restart backend${c_reset}" \
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
            files+=("$DOCKER_DIR/docker-compose.infra.yml")
            files+=("$COMPOSE_FILE")
            ;;
        observability)
            files+=("$OBSERVABILITY_COMPOSE")
            ;;
        *)
            if is_sku_profile "$profile"; then
                files+=("$DOCKER_DIR/docker-compose.infra.yml")
                files+=("${PROFILE_COMPOSE[$profile]}")
            else
                return 1
            fi
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

show_profile_menu() {
    [ "$HAS_TTY" = true ] && clear 2>/dev/null || true
    draw_status_line
    draw_rule
    echo
    draw_box "Orkestra Stack Manager" \
        "" \
        "  ${c_accent}1${c_reset} ${c_bold}Profile${c_reset}                 ${c_muted}(pull published image)${c_reset}" \
        "     ${c_muted}starter / billing / ai / saas / enterprise${c_reset}" \
        "" \
        "  ${c_accent}2${c_reset} ${c_bold}Full stack${c_reset}              ${c_muted}(dev / staging / production)${c_reset}" \
        "     ${c_muted}ENV autodetected from docker/.env${c_reset}" \
        "" \
        "  ${c_accent}3${c_reset} ${c_bold}Observability${c_reset}           ${c_muted}(Loki, Tempo, Prometheus, Grafana)${c_reset}" \
        "     ${c_muted}ADR-0005 Phase D — runs alongside any app stack${c_reset}" \
        "" \
        "  ${c_accent}4${c_reset} ${c_bold}Quit${c_reset}" \
        ""
}

show_sku_profile_picker() {
    page_header "Profile picker"
    draw_box "Select SKU" \
        "" \
        "  ${c_accent}1${c_reset} starter      ${c_muted}core only (no addons)${c_reset}" \
        "  ${c_accent}2${c_reset} billing      ${c_muted}billing + documents + company${c_reset}" \
        "  ${c_accent}3${c_reset} ai           ${c_muted}graph + aimodels + rag + agents + sales${c_reset}" \
        "  ${c_accent}4${c_reset} saas         ${c_muted}subscriptions + payments + compliance + identity${c_reset}" \
        "  ${c_accent}5${c_reset} enterprise   ${c_muted}every addon${c_reset}" \
        "  ${c_accent}6${c_reset} Back" \
        ""
}

show_profile_ops_menu() {
    local name=$1
    local title
    title=$(echo "${name:0:1}" | tr '[:lower:]' '[:upper:]')${name:1}
    page_header "${title} profile"
    draw_box "Select operation" \
        "" \
        "  ${c_accent}1${c_reset}  Deploy / Update    ${c_muted}(up -d [--pull])${c_reset}" \
        "  ${c_accent}2${c_reset}  Stop               ${c_muted}(keeps volumes)${c_reset}" \
        "  ${c_accent}3${c_reset}  Reset              ${c_muted}(wipes volumes — destructive)${c_reset}" \
        "  ${c_accent}4${c_reset}  Status             ${c_muted}(ps + /health + stats)${c_reset}" \
        "  ${c_accent}5${c_reset}  Logs               ${c_muted}(service picker)${c_reset}" \
        "  ${c_accent}6${c_reset}  Info               ${c_muted}(URLs + dev-token recipe)${c_reset}" \
        "  ${c_accent}7${c_reset}  Back to profile picker" \
        ""
}

profile_picker_loop() {
    while true; do
        show_sku_profile_picker
        printf '%s%s Select profile [1-6]: %s' "$c_prompt" "$ic_arrow" "$c_reset"
        local choice
        read -r choice
        case "$choice" in
            1) profile_ops_menu_loop "starter" ;;
            2) profile_ops_menu_loop "billing" ;;
            3) profile_ops_menu_loop "ai" ;;
            4) profile_ops_menu_loop "saas" ;;
            5) profile_ops_menu_loop "enterprise" ;;
            6) PROFILE=""; return ;;
            *) p_warn "Invalid selection"; sleep 1 ;;
        esac
    done
}

profile_ops_menu_loop() {
    local name=$1
    PROFILE="$name"
    while true; do
        show_profile_ops_menu "$name"
        printf '%s%s Select operation [1-7]: %s' "$c_prompt" "$ic_arrow" "$c_reset"
        local choice
        read -r choice
        case "$choice" in
            1)
                local refresh="no"
                ask_yes_no "Pull latest image from GHCR?" "n" && refresh="yes"
                profile_deploy "$name" "$refresh"
                pause_for_return
                ;;
            2) profile_stop "$name"; pause_for_return ;;
            3) profile_reset "$name" "ask"; pause_for_return ;;
            4) profile_status "$name"; pause_for_return ;;
            5) logs_interactive "$name"; pause_for_return ;;
            6) profile_info "$name"; pause_for_return ;;
            7) PROFILE=""; return ;;
            *) p_warn "Invalid selection"; sleep 1 ;;
        esac
    done
}

show_fullstack_menu() {
    page_header "Full stack"
    draw_box "Select operation" \
        "" \
        "  ${c_accent}1${c_reset}  Deploy             ${c_muted}(with scope selection)${c_reset}" \
        "  ${c_accent}2${c_reset}  Stop               ${c_muted}(app + optional infra)${c_reset}" \
        "  ${c_accent}3${c_reset}  Status             ${c_muted}(ps + health + stats)${c_reset}" \
        "  ${c_accent}4${c_reset}  Logs               ${c_muted}(service picker)${c_reset}" \
        "  ${c_accent}5${c_reset}  Back to profile menu" \
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
        "  ${c_accent}7${c_reset}  Back to profile menu" \
        ""
}

observability_menu_loop() {
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
  ./orkestra.sh                    ${c_muted}# interactive TUI (profile menu)${c_reset}
  ./orkestra.sh <command> [args]   ${c_muted}# non-interactive CLI${c_reset}

${c_bold}FIRST-TIME SETUP${c_reset}
  ${c_accent}init${c_reset} [--force] [--yes]            Scaffold docker/.env (random secrets) +
                                   RS256 JWT keys. Idempotent — preserves
                                   existing files unless --force.

${c_bold}SKU PROFILE${c_reset} ${c_muted}(pulls published image from GHCR)${c_reset}
  ${c_accent}profile <name> deploy${c_reset} [--pull]    Start SKU stack (--pull refreshes the image)
  ${c_accent}profile <name> stop${c_reset}               Stop containers (volumes kept)
  ${c_accent}profile <name> reset${c_reset} [--yes]      Wipe volumes and redeploy
  ${c_accent}profile <name> status${c_reset}             Containers + /health + resources
  ${c_accent}profile <name> info${c_reset}               URLs, image, dev-token recipe
  ${c_accent}profile <name> logs${c_reset} <svc> [flags] View logs for a SKU-stack service

  Available <name>: starter | billing | ai | saas | enterprise
  Requires docker-compose.infra.yml to be running first.

${c_bold}FULL STACK${c_reset} ${c_muted}(uses ENV from docker/.env or ENV=... prefix)${c_reset}
  ${c_accent}deploy${c_reset} [--scope SCOPE] [--rebuild] [--yes]
                                   Deploy. SCOPE: all | backend | frontend-admin |
                                                 frontend-admin+backend | infra
  ${c_accent}stop${c_reset} [--with-infra]               Stop application services (+ infra)
  ${c_accent}status${c_reset}                            Containers + health + resources
  ${c_accent}logs${c_reset} <service> [flags]            View logs for a full-stack service

${c_bold}OBSERVABILITY${c_reset} ${c_muted}(ADR-0005 Phase D — runs alongside any app stack)${c_reset}
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
  ${c_muted}ENV=development ./orkestra.sh${c_reset}     Skip profile menu, open full-stack TUI
  ${c_accent}-h${c_reset}, ${c_accent}--help${c_reset}             Show this message
  ${c_accent}-v${c_reset}, ${c_accent}--version${c_reset}          Show version + capabilities

${c_bold}EXAMPLES${c_reset}
  ./orkestra.sh
  ./orkestra.sh profile billing deploy --pull
  ./orkestra.sh profile ai status
  ./orkestra.sh profile enterprise logs backend -f
  ENV=development ./orkestra.sh deploy --scope backend --rebuild --yes
  ./orkestra.sh logs orkestra-backend-dev -f
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
            # Delegate to scripts/init.sh. Forwards every flag so
            # `./orkestra.sh init --force` works the same as `make init-force`.
            exec bash "$SCRIPT_DIR/scripts/init.sh" "$@"
            ;;

        profile)
            local name=${1:-}
            [ -n "$name" ] && shift
            [ -z "$name" ] && die "Usage: ./orkestra.sh profile <name> <subcmd>. Try --help."
            if ! is_sku_profile "$name"; then
                die "Unknown profile: $name. Valid: $(sku_profile_list)"
            fi
            PROFILE="$name"
            local subcmd=${1:-}
            [ -n "$subcmd" ] && shift
            case "$subcmd" in
                deploy)
                    local refresh="no"
                    while [ $# -gt 0 ]; do
                        case "$1" in
                            --pull) refresh="yes"; shift ;;
                            *) die "Unknown flag: $1" ;;
                        esac
                    done
                    profile_deploy "$name" "$refresh"
                    ;;
                stop) profile_stop "$name" ;;
                reset)
                    local confirm="ask"
                    while [ $# -gt 0 ]; do
                        case "$1" in
                            --yes | -y) confirm="yes"; shift ;;
                            *) die "Unknown flag: $1" ;;
                        esac
                    done
                    profile_reset "$name" "$confirm"
                    ;;
                status) profile_status "$name" ;;
                info) profile_info "$name" ;;
                logs)
                    local service=${1:-}
                    [ -n "$service" ] && shift
                    [ -z "$service" ] && die "Usage: ./orkestra.sh profile $name logs <service> [-f] [-n N] [-t]"
                    logs_cli "$name" "$service" "$@"
                    ;;
                "") die "Missing subcommand. Try --help." ;;
                *) die "Unknown subcommand: $subcmd" ;;
            esac
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

    # Always show the top-level menu. We used to auto-route into the
    # full-stack loop when $ENV was set (from shell or docker/.env), but that
    # silently hid the SKU-profile menu — so users who had deployed e.g.
    # the `enterprise` profile would be shown the staging service list with
    # everything marked "stopped" (since each compose file declares its own
    # `name:`, the staging-project lookup misses orkestra-enterprise's
    # containers). Force the menu so the user can pick the profile that
    # matches their running stack.
    while true; do
        show_profile_menu
        printf '%s%s Select profile [1-4]: %s' "$c_prompt" "$ic_arrow" "$c_reset"
        local choice
        read -r choice
        case "$choice" in
            1) profile_picker_loop ;;
            2) fullstack_menu_loop ;;
            3) observability_menu_loop ;;
            4)
                echo
                printf '%s%s Goodbye!%s\n' "$c_success" "$ic_ok" "$c_reset"
                exit 0
                ;;
            *) p_warn "Invalid selection"; sleep 1 ;;
        esac
    done
}

main "$@"
