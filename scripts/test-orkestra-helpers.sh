#!/usr/bin/env bash
# Sources orkestra.sh (main() is guarded, so this does NOT launch the TUI) and
# unit-tests ask_value's non-interactive path. Also re-tested by Task 3.
set -uo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$DIR/../orkestra.sh"     # guarded main => no menu; sourcing is safe
trap - EXIT INT ERR              # drop orkestra's traps in this test shell
set +Eeuo pipefail               # orkestra enabled strict mode on source; relax
HAS_GUM=false                    # force the plain read path (gum may be installed)

pass=0; fail=0
check() { if [ "$2" = "$3" ]; then pass=$((pass+1)); printf '  ok   %s\n' "$1"
          else fail=$((fail+1)); printf '  FAIL %s\n       expected: [%s]\n       actual:   [%s]\n' "$1" "$2" "$3"; fi; }

check "empty input -> default"  "mydefault" "$(printf '\n'        | ask_value 'Label' 'mydefault')"
check "typed input overrides"   "custom"    "$(printf 'custom\n'  | ask_value 'Label' 'mydefault')"
check "no default, empty -> ''" ""          "$(printf '\n'        | ask_value 'Label' '')"

# --- apply_env_profile (pure, deterministic per ENV) ---
ptmp="$(mktemp)"; printf 'ENV=development\nCOOKIE_SECURE=false\nDEBUG=true\n' > "$ptmp"
apply_env_profile "$ptmp" production
check "prod cookie secure"  "true"       "$(env_get "$ptmp" COOKIE_SECURE)"
check "prod samesite strict" "strict"    "$(env_get "$ptmp" COOKIE_SAME_SITE)"
check "prod debug off"      "false"      "$(env_get "$ptmp" DEBUG)"
check "prod rate limit"     "30"         "$(env_get "$ptmp" RATE_LIMIT_REQUESTS_PER_MINUTE)"
check "prod vite env"       "production" "$(env_get "$ptmp" VITE_ENV)"
apply_env_profile "$ptmp" development
check "dev cookie insecure" "false"      "$(env_get "$ptmp" COOKIE_SECURE)"
check "dev rate limit"      "1000"       "$(env_get "$ptmp" RATE_LIMIT_REQUESTS_PER_MINUTE)"
rm -f "$ptmp"

# --- JWT key permissions: the private signing key must never become
# group/world-readable merely to accommodate a container UID mismatch. ---
keys_tmp="$(mktemp -d)"
old_docker_dir="$DOCKER_DIR"
DOCKER_DIR="$keys_tmp"
mkdir -p "$DOCKER_DIR/keys"
printf 'private\n' > "$DOCKER_DIR/keys/jwt-private.pem"
printf 'public\n' > "$DOCKER_DIR/keys/jwt-public.pem"
chmod 600 "$DOCKER_DIR/keys/jwt-private.pem" "$DOCKER_DIR/keys/jwt-public.pem"
ensure_jwt_keys_readable >/dev/null
check "private JWT key stays private" "600" "$(stat -c '%a' "$DOCKER_DIR/keys/jwt-private.pem")"
check "public JWT key becomes readable" "644" "$(stat -c '%a' "$DOCKER_DIR/keys/jwt-public.pem")"
DOCKER_DIR="$old_docker_dir"
rm -rf "$keys_tmp"

generator_tmp="$(mktemp -d)"
mkdir -p "$generator_tmp/scripts" "$generator_tmp/docker"
cp "$DIR/generate-jwt-keys.sh" "$generator_tmp/scripts/"
bash "$generator_tmp/scripts/generate-jwt-keys.sh" >/dev/null 2>&1
check "JWT generator creates private key as 600" "600" "$(stat -c '%a' "$generator_tmp/docker/keys/jwt-private.pem")"
rm -rf "$generator_tmp"

# --- Observability lifecycle must fail closed and stay overlay-scoped. ---
obs_discovery_log="$(mktemp)"
export OBS_DISCOVERY_LOG="$obs_discovery_log"
(
    docker() {
        local IFS=' '
        printf '%s\n' "$*" >> "$OBS_DISCOVERY_LOG"
        return 17
    }
    INFRA_COMPOSE="$PROJECT_ROOT/docker/docker-compose.infra.yml"
    COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.dev.yml"
    OBSERVABILITY_COMPOSE="$PROJECT_ROOT/docker/docker-compose.observability.yml"
    ENV_FILE="$PROJECT_ROOT/docker/.env"
    observability_list_services
) >/dev/null 2>&1
obs_discovery_status=$?
check "observability discovery fails closed on invalid compose" "yes" "$([ "$obs_discovery_status" -ne 0 ] && printf yes || printf no)"
check "observability discovery validates the merged stack" "3" "$(sed -n '1p' "$obs_discovery_log" | grep -o -- ' -f ' | wc -l | tr -d ' ')"
rm -f "$obs_discovery_log"

obs_reset_log="$(mktemp)"
export OBS_RESET_LOG="$obs_reset_log"
(
    check_docker_running() { :; }
    observability_check_file() { :; }
    observability_init_env() {
        ENV=development
        COMPOSE_PROJECT_NAME=orkestra-test-development
        INFRA_COMPOSE="$PROJECT_ROOT/docker/docker-compose.infra.yml"
        COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.dev.yml"
        OBSERVABILITY_COMPOSE="$PROJECT_ROOT/docker/docker-compose.observability.yml"
        ENV_FILE="$PROJECT_ROOT/docker/.env"
    }
    page_header() { :; }
    draw_box() { :; }
    p_ok() { :; }
    with_spinner() {
        shift
        "$@"
    }
    docker() {
        local IFS=' '
        printf '%s\n' "$*" >> "$OBS_RESET_LOG"
        case " $* " in
            *" config --services "*)
                printf '%s\n' backend mongodb redis rustfs frontend-admin client-frontend \
                    otel-collector tempo prometheus loki promtail grafana
                ;;
            *" config --volumes "*)
                printf '%s\n' mongodb-data redis-data rustfs-data \
                    tempo-data prometheus-data loki-data promtail-positions grafana-data
                ;;
        esac
        return 0
    }
    observability_reset skip
) >/dev/null 2>&1
check "observability reset never falls through to down -v" "no" "$(grep -Eq 'compose .* down -v( |$)' "$obs_reset_log" && printf yes || printf no)"
check "observability reset removes only six explicit services" \
    "yes" \
    "$(grep -Eq 'compose .* rm -sfv otel-collector tempo prometheus loki promtail grafana$' "$obs_reset_log" && printf yes || printf no)"
check "observability reset removes five explicit named volumes" \
    "5" \
    "$(grep -Ec '^volume rm orkestra-test-development_(tempo-data|prometheus-data|loki-data|promtail-positions|grafana-data)$' "$obs_reset_log" || true)"
check "observability reset never targets application or infra volumes" \
    "no" \
    "$(grep -Eq '^volume rm .*_(mongodb-data|redis-data|rustfs-data)$' "$obs_reset_log" && printf yes || printf no)"
rm -f "$obs_reset_log"

obs_up_log="$(mktemp)"
export OBS_UP_LOG="$obs_up_log"
(
    check_docker_running() { :; }
    observability_check_file() { :; }
    observability_init_env() {
        ENV=development
        COMPOSE_PROJECT_NAME=orkestra-test-development
        INFRA_COMPOSE="$PROJECT_ROOT/docker/docker-compose.infra.yml"
        COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.dev.yml"
        OBSERVABILITY_COMPOSE="$PROJECT_ROOT/docker/docker-compose.observability.yml"
        ENV_FILE="$PROJECT_ROOT/docker/.env"
    }
    page_header() { :; }
    p_muted() { :; }
    p_ok() { :; }
    observability_info() { :; }
    with_spinner() {
        shift
        "$@"
    }
    docker() {
        local IFS=' '
        printf '%s\n' "$*" >> "$OBS_UP_LOG"
        case " $* " in
            *" config --services "*)
                printf '%s\n' backend mongodb redis rustfs frontend-admin client-frontend \
                    otel-collector tempo prometheus loki promtail grafana
                ;;
            *" config --volumes "*)
                printf '%s\n' mongodb-data redis-data rustfs-data \
                    tempo-data prometheus-data loki-data promtail-positions grafana-data
                ;;
            *" ps -q backend "*) printf 'backend-id\n' ;;
        esac
        return 0
    }
    observability_up
) >/dev/null 2>&1
check "observability up applies its backend override through the merged stack" \
    "yes" \
    "$(grep -Eq 'compose .* -f .*docker-compose.observability.yml .* up -d --no-deps backend$' "$obs_up_log" && printf yes || printf no)"
rm -f "$obs_up_log"

obs_down_log="$(mktemp)"
export OBS_DOWN_LOG="$obs_down_log"
(
    check_docker_running() { :; }
    observability_check_file() { :; }
    observability_init_env() {
        ENV=development
        COMPOSE_PROJECT_NAME=orkestra-test-development
        INFRA_COMPOSE="$PROJECT_ROOT/docker/docker-compose.infra.yml"
        COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.dev.yml"
        OBSERVABILITY_COMPOSE="$PROJECT_ROOT/docker/docker-compose.observability.yml"
        ENV_FILE="$PROJECT_ROOT/docker/.env"
    }
    page_header() { :; }
    p_ok() { :; }
    with_spinner() {
        shift
        "$@"
    }
    docker() {
        local IFS=' '
        printf '%s\n' "$*" >> "$OBS_DOWN_LOG"
        case " $* " in
            *" config --services "*)
                printf '%s\n' backend mongodb redis rustfs frontend-admin client-frontend \
                    otel-collector tempo prometheus loki promtail grafana
                ;;
            *" config --volumes "*)
                printf '%s\n' mongodb-data redis-data rustfs-data \
                    tempo-data prometheus-data loki-data promtail-positions grafana-data
                ;;
            *" ps -q backend "*) printf 'backend-id\n' ;;
        esac
        return 0
    }
    observability_down
) >/dev/null 2>&1
check "observability down dry-run removes only six explicit services" \
    "yes" \
    "$(grep -Eq 'compose .* rm -sf otel-collector tempo prometheus loki promtail grafana$' "$obs_down_log" && printf yes || printf no)"
check "observability down dry-run never uses project-wide down" \
    "no" \
    "$(grep -Eq 'compose .* down( |$)' "$obs_down_log" && printf yes || printf no)"
check "observability down restores the backend without the overlay" \
    "yes" \
    "$(grep -Eq 'compose -f .*docker-compose.infra.yml -f .*docker-compose.dev.yml --env-file .* up -d --no-deps backend$' "$obs_down_log" && printf yes || printf no)"
rm -f "$obs_down_log"

obs_catalog_count="$(
    (
        get_services() { return 17; }
        list_all_services observability
        printf '%s' "${#SERVICES[@]}"
    ) 2>/dev/null
)"
check "observability log picker uses the explicit service catalog" "6" "$obs_catalog_count"


echo
printf 'orkestra-helpers: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
