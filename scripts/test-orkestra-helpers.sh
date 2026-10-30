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

# --- Retrieval overlay (ADR-0023): fail closed, seed before serving, scope the
# five owned services, and keep the other overlay's backend override. ---
# has <file> <ERE> -> yes|no. Negative checks pair it with a positive one
# ("<ran>/<forbidden>") so a run that logged nothing cannot pass them.
has() { grep -Eq -- "$2" "$1" && printf yes || printf no; }
# A shell-exported setting would change what the lifecycle does; the tests set
# them per call instead.
unset RETRIEVAL_SKIP_SEED RETRIEVAL_SEED_TIMEOUT QDRANT_API_KEY RETRIEVAL_INFERENCE_API_KEY
# Scratch docker/.env: the developer's real one is never read.
ret_env_file="$(mktemp)"
ret_log="$(mktemp)"
ret_out="$(mktemp)"
ret_env_log="$(mktemp)"
export RET_ENV_FILE="$ret_env_file" RET_LOG="$ret_log" RET_ENV_LOG="$ret_env_log"
printf 'ENV=development\nQDRANT_API_KEY=qdrant-key-value-1234\nRETRIEVAL_INFERENCE_API_KEY=inference-key-value-5678\n' > "$ret_env_file"

# ret_run <cmd...> — runs cmd in a subshell against a stubbed Docker that logs
# every call to $RET_LOG. RET_FAIL (an ERE) makes the matching call return 1.
# Output goes to $ret_out; returns the subshell's status.
ret_run() {
    : > "$RET_LOG"
    : > "$RET_ENV_LOG"
    (
        check_docker_running() { :; }
        retrieval_check_file() { :; }
        retrieval_init_env() {
            ENV=development
            COMPOSE_PROJECT_NAME=orkestra-test-development
            INFRA_COMPOSE="$PROJECT_ROOT/docker/docker-compose.infra.yml"
            COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.dev.yml"
            RETRIEVAL_COMPOSE="$PROJECT_ROOT/docker/docker-compose.retrieval.yml"
            ENV_FILE="$RET_ENV_FILE"
        }
        observability_init_env() { retrieval_init_env; }
        page_header() { :; }
        p_ok() { :; }
        with_spinner() {
            shift
            "$@"
        }
        docker() {
            local IFS=' '
            printf '%s\n' "$*" >> "$RET_LOG"
            # What Compose would interpolate: the exported value only.
            printenv RETRIEVAL_SEED_TIMEOUT >> "$RET_ENV_LOG" || echo unset >> "$RET_ENV_LOG"
            if [ -n "${RET_FAIL:-}" ] && printf '%s\n' "$*" | grep -Eq -- "$RET_FAIL"; then
                return 1
            fi
            case " $* " in
                *" config --services "*)
                    printf '%s\n' backend mongodb redis rustfs frontend-admin client-frontend \
                        otel-collector tempo prometheus loki promtail grafana \
                        qdrant tei-embed tei-rerank tei-embed-seed tei-rerank-seed
                    ;;
                *" config --volumes "*)
                    printf '%s\n' mongodb-data redis-data rustfs-data \
                        tempo-data prometheus-data loki-data promtail-positions grafana-data \
                        qdrant-data tei-weights
                    ;;
                *" ps -q backend "*) printf 'backend-id\n' ;;
                *" ps -q qdrant tei-embed tei-rerank "*) printf '%s\n' qdrant-id tei-embed-id tei-rerank-id ;;
            esac
            return 0
        }
        retrieval_init_env
        "$@"
    ) > "$ret_out" 2>&1
}

ret_discovery_log="$(mktemp)"
export RET_DISCOVERY_LOG="$ret_discovery_log"
(
    docker() {
        local IFS=' '
        printf '%s\n' "$*" >> "$RET_DISCOVERY_LOG"
        return 17
    }
    INFRA_COMPOSE="$PROJECT_ROOT/docker/docker-compose.infra.yml"
    COMPOSE_FILE="$PROJECT_ROOT/docker/docker-compose.dev.yml"
    RETRIEVAL_COMPOSE="$PROJECT_ROOT/docker/docker-compose.retrieval.yml"
    ENV_FILE="$RET_ENV_FILE"
    retrieval_list_services
) >/dev/null 2>&1
ret_discovery_status=$?
check "retrieval discovery fails closed on invalid compose" "1" "$ret_discovery_status"
check "retrieval discovery validates the merged stack" "3" "$(sed -n '1p' "$ret_discovery_log" | grep -o -- ' -f ' | wc -l | tr -d ' ')"
check "retrieval discovery lists the seed profile" "yes" "$(has "$ret_discovery_log" '--profile retrieval-seed config --services$')"
rm -f "$ret_discovery_log"

# Missing keys: the failure names the empty one and prints no key value.
printf 'ENV=development\nQDRANT_API_KEY=qdrant-key-value-1234\nRETRIEVAL_INFERENCE_API_KEY=\n' > "$ret_env_file"
RET_FAIL='config --services' ret_run retrieval_up; rc=$?
check "retrieval up with a missing key fails before any lifecycle call" "1/no" \
    "$rc/$(has "$ret_log" ' (up|rm) ')"
check "the missing-key failure names the empty key only" "yes/no" \
    "$(has "$ret_out" 'RETRIEVAL_INFERENCE_API_KEY must be set')/$(has "$ret_out" 'QDRANT_API_KEY')"
check "the missing-key failure prints no key value" "no" "$(has "$ret_out" 'qdrant-key-value-1234')"
printf 'ENV=development\nQDRANT_API_KEY=qdrant-key-value-1234\nRETRIEVAL_INFERENCE_API_KEY=inference-key-value-5678\n' > "$ret_env_file"

ret_run retrieval_up; rc=$?
ret_seed_line="$(grep -nE 'compose .* --profile retrieval-seed up -d --wait .* tei-embed-seed tei-rerank-seed$' "$ret_log" | head -1 | cut -d: -f1)"
ret_serve_line="$(grep -nE 'compose .* up -d --wait .* qdrant tei-embed tei-rerank$' "$ret_log" | head -1 | cut -d: -f1)"
check "retrieval up succeeds" "0" "$rc"
check "retrieval up seeds before serving" "yes" \
    "$([ -n "$ret_seed_line" ] && [ -n "$ret_serve_line" ] && [ "$ret_seed_line" -lt "$ret_serve_line" ] && printf yes || printf no)"
check "retrieval up waits with the default seed timeout, exported to Compose" "yes/1800" \
    "$(has "$ret_log" 'up -d --wait --wait-timeout 1800 tei-embed-seed tei-rerank-seed$')/$(sort -u "$ret_env_log" | tr '\n' ' ' | sed 's/ $//')"
check "retrieval up removes the seed containers" "yes" "$(has "$ret_log" 'compose .* --profile retrieval-seed rm -sf tei-embed-seed tei-rerank-seed$')"
check "retrieval up applies its backend override" "yes" "$(has "$ret_log" 'compose .* -f .*docker-compose.retrieval.yml .* up -d --no-deps backend$')"
check "retrieval up and info print no key value" "no/no" \
    "$(has "$ret_out" 'qdrant-key-value-1234')/$(has "$ret_out" 'inference-key-value-5678')"

RETRIEVAL_SKIP_SEED=true ret_run retrieval_up
check "RETRIEVAL_SKIP_SEED skips the seed step" "yes/no" \
    "$(has "$ret_log" 'up -d --wait .* qdrant tei-embed tei-rerank$')/$(has "$ret_log" ' up .*tei-embed-seed')"
printf 'RETRIEVAL_SKIP_SEED=true\n' >> "$ret_env_file"
ret_run retrieval_up
check "RETRIEVAL_SKIP_SEED in docker/.env skips the seed step" "yes/no" \
    "$(has "$ret_log" 'up -d --wait .* qdrant tei-embed tei-rerank$')/$(has "$ret_log" ' up .*tei-embed-seed')"
RETRIEVAL_SKIP_SEED=false ret_run retrieval_up
check "the shell RETRIEVAL_SKIP_SEED wins over docker/.env" "yes" "$(has "$ret_log" ' up .*tei-embed-seed')"
printf 'ENV=development\nQDRANT_API_KEY=qdrant-key-value-1234\nRETRIEVAL_INFERENCE_API_KEY=inference-key-value-5678\n# RETRIEVAL_SKIP_SEED=true\nRETRIEVAL_SEED_TIMEOUT=60\n' > "$ret_env_file"
ret_run retrieval_up
check "a commented RETRIEVAL_SKIP_SEED is ignored" "yes" "$(has "$ret_log" ' up .*tei-embed-seed')"
check "RETRIEVAL_SEED_TIMEOUT from docker/.env bounds the seed wait" "yes" "$(has "$ret_log" 'up -d --wait --wait-timeout 60 tei-embed-seed tei-rerank-seed$')"
# The overlay renders the same variable into the seeds' healthcheck
# start_period, so every Compose call must see the value resolved here.
check "every Compose call sees the .env seed timeout exported" "60" "$(sort -u "$ret_env_log" | tr '\n' ' ' | sed 's/ $//')"
RETRIEVAL_SEED_TIMEOUT=120 ret_run retrieval_up
check "the shell seed timeout wins and is what Compose sees" "yes/120" \
    "$(has "$ret_log" 'up -d --wait --wait-timeout 120 tei-embed-seed tei-rerank-seed$')/$(sort -u "$ret_env_log" | tr '\n' ' ' | sed 's/ $//')"
RETRIEVAL_SEED_TIMEOUT=soon ret_run retrieval_up; rc=$?
check "an invalid RETRIEVAL_SEED_TIMEOUT is refused before any Compose call" "1/0/yes" \
    "$rc/$(wc -l < "$ret_log" | tr -d ' ')/$(has "$ret_out" 'RETRIEVAL_SEED_TIMEOUT must be a whole number')"
# A deploy probes the overlay through stack_compose_files: an invalid value
# must stop it rather than fail the render and silently unwire the backend.
RETRIEVAL_SEED_TIMEOUT=soon ret_run stack_compose_files; rc=$?
check "an invalid seed timeout stops the overlay probe a deploy uses" "1/no" "$rc/$(has "$ret_log" 'docker-compose.retrieval.yml')"
printf 'ENV=development\nQDRANT_API_KEY=qdrant-key-value-1234\nRETRIEVAL_INFERENCE_API_KEY=inference-key-value-5678\n' > "$ret_env_file"

RET_FAIL='up -d --wait .* tei-embed-seed' ret_run retrieval_up; rc=$?
check "a failed seed exits non-zero" "1" "$rc"
check "a failed seed still removes the seeds" "yes" "$(has "$ret_log" '--profile retrieval-seed rm -sf tei-embed-seed tei-rerank-seed$')"
check "a failed seed starts no serving container" "yes/no" \
    "$(has "$ret_log" 'tei-embed-seed tei-rerank-seed$')/$(has "$ret_log" 'up -d --wait .* qdrant tei-embed tei-rerank$')"
check "a failed seed applies no backend override" "no" "$(has "$ret_log" 'up -d --no-deps backend$')"

ret_run retrieval_down; rc=$?
check "retrieval down succeeds" "0" "$rc"
check "retrieval down removes only five explicit services" "yes" \
    "$(has "$ret_log" 'compose .* --profile retrieval-seed rm -sf qdrant tei-embed tei-rerank tei-embed-seed tei-rerank-seed$')"
check "retrieval down never uses project-wide down or removes volumes" "yes/no/no" \
    "$(has "$ret_log" ' rm -sf ')/$(has "$ret_log" 'compose .* down( |$)')/$(has "$ret_log" '( rm -sfv |^volume rm )')"
check "retrieval down restores the backend without the overlay" "yes" \
    "$(has "$ret_log" 'compose -f .*docker-compose.infra.yml -f .*docker-compose.dev.yml --env-file .* up -d --no-deps backend$')"
(
    observability_project_active() { return 0; }
    ret_run retrieval_down
)
check "retrieval down keeps the observability override" "yes/no" \
    "$(has "$ret_log" 'docker-compose.observability.yml .*up -d --no-deps backend$')/$(has "$ret_log" 'docker-compose.retrieval.yml .*up -d --no-deps backend$')"
(
    retrieval_project_active() { return 0; }
    ret_run observability_down
)
check "observability down keeps the retrieval override" "yes/no" \
    "$(has "$ret_log" 'docker-compose.retrieval.yml .*up -d --no-deps backend$')/$(has "$ret_log" 'docker-compose.observability.yml .*up -d --no-deps backend$')"
(
    retrieval_project_active() { return 0; }
    ret_run observability_up
)
check "observability up keeps the retrieval override" "yes" \
    "$(has "$ret_log" 'docker-compose.observability.yml -f .*docker-compose.retrieval.yml .*up -d --no-deps backend$')"

ret_run retrieval_status
check "retrieval status reports memory usage of the three servers" "yes/yes" \
    "$(has "$ret_log" 'ps qdrant tei-embed tei-rerank$')/$(has "$ret_log" '^stats --no-stream .*MemUsage')"
check "retrieval status prints no key value" "no" "$(has "$ret_out" '(qdrant|inference)-key-value')"

# stack_compose_files: an overlay joins the -f list when its containers exist
# or --with names it, unless --without names it.
ret_stack_files() {
    (
        ENV_FILE=/x/.env
        INFRA_COMPOSE=/x/docker-compose.infra.yml
        COMPOSE_FILE=/x/docker-compose.dev.yml
        OBSERVABILITY_COMPOSE=/x/docker-compose.observability.yml
        RETRIEVAL_COMPOSE=/x/docker-compose.retrieval.yml
        # Not obs/ret: stack_compose_files' own locals would shadow them.
        local stub_obs=$1 stub_ret=$2
        shift 2
        observability_project_active() { [ "$stub_obs" = yes ]; }
        retrieval_project_active() { [ "$stub_ret" = yes ]; }
        stack_compose_files "$@"
        local IFS=' '
        printf '%s:%s' "${#STACK_COMPOSE_FILES[@]}" "${STACK_COMPOSE_FILES[*]}"
    ) 2>/dev/null
}
check "stack files: no overlay active" "4" "$(ret_stack_files no no | cut -d: -f1)"
check "stack files: both overlays active" \
    "8:-f /x/docker-compose.infra.yml -f /x/docker-compose.dev.yml -f /x/docker-compose.observability.yml -f /x/docker-compose.retrieval.yml" \
    "$(ret_stack_files yes yes)"
check "stack files: --without retrieval" "6" "$(ret_stack_files yes yes --without retrieval | cut -d: -f1)"
check "stack files: --with retrieval when inactive" \
    "6:-f /x/docker-compose.infra.yml -f /x/docker-compose.dev.yml -f /x/docker-compose.retrieval.yml" \
    "$(ret_stack_files no no --with retrieval)"
check "stack files: an unknown overlay is refused" "no" \
    "$(ret_stack_files no no --with grafana >/dev/null && printf yes || printf no)"

# The production rolling update must keep both overlays' backend override. The
# companion counts stop the "no line uses another list" checks passing on a
# function that no longer has the line at all.
ret_deploy_fn="$(declare -f fullstack_execute_deploy)"
check "production rolling update exists once" "1" \
    "$(printf '%s\n' "$ret_deploy_fn" | grep -c 'up -d --no-deps "$BACKEND_SERVICE"')"
check "production rolling update uses the stack file list" "0" \
    "$(printf '%s\n' "$ret_deploy_fn" | grep 'up -d --no-deps "$BACKEND_SERVICE"' | grep -vc 'compose_files\[@\]')"
check "production admin frontend update exists once" "1" \
    "$(printf '%s\n' "$ret_deploy_fn" | grep -c 'up -d --no-deps "$FRONTEND_SERVICE"')"
check "production admin frontend update uses the stack file list" "0" \
    "$(printf '%s\n' "$ret_deploy_fn" | grep 'up -d --no-deps "$FRONTEND_SERVICE"' | grep -vc 'compose_files\[@\]')"
check "deploy builds its file list with stack_compose_files" "yes/yes" \
    "$(printf '%s\n' "$ret_deploy_fn" | grep -q '^ *stack_compose_files;\{0,1\}$' && printf yes || printf no)/$(printf '%s\n' "$ret_deploy_fn" | grep -q 'compose_files=("${STACK_COMPOSE_FILES\[@\]}")' && printf yes || printf no)"

# gum spin runs an external process and the TTY spinner a subshell: a shell
# function passed to with_spinner fails under gum and loses globals such as
# STACK_COMPOSE_FILES. Every with_spinner call must run a real command.
check "overlay lifecycle functions exist" "yes" \
    "$(declare -F retrieval_up retrieval_down retrieval_seed observability_up observability_down observability_reset >/dev/null && printf yes || printf no)"
check "overlay lifecycle passes no shell function to with_spinner" "0" \
    "$(declare -f retrieval_up retrieval_down retrieval_seed observability_up observability_down observability_reset | grep -c 'with_spinner .*\(retrieval\|observability\)_')"
ret_spinner_fns=""
while IFS= read -r ret_word; do
    if [ "$(type -t "$ret_word" 2>/dev/null)" = function ]; then
        ret_spinner_fns="$ret_spinner_fns $ret_word"
    fi
done < <(declare -f | sed -n 's/^.*with_spinner "[^"]*" \([^ ;|&]*\).*$/\1/p' | sort -u)
check "no with_spinner call in orkestra.sh runs a shell function" "" "$ret_spinner_fns"

ret_catalog_count="$(
    (
        get_services() { return 17; }
        list_all_services retrieval
        printf '%s' "${#SERVICES[@]}"
    ) 2>/dev/null
)"
check "retrieval log picker uses the explicit service catalog" "5" "$ret_catalog_count"
ret_run logs_cli retrieval qdrant -n 20 -t; rc=$?
check "retrieval logs read the merged stack, scoped to the project" "0/yes/no" \
    "$rc/$(has "$ret_log" '^compose -f .*docker-compose.infra.yml -f .*docker-compose.dev.yml -f .*docker-compose.retrieval.yml --env-file .* logs --timestamps --tail=20 qdrant$')/$(has "$ret_log" '^ps .*--filter')"
ret_run logs_cli retrieval tei-embed-seed -f
check "retrieval logs of a seed carry its profile" "yes" \
    "$(has "$ret_log" '-f .*docker-compose.retrieval.yml --env-file .* --profile retrieval-seed logs --follow tei-embed-seed$')"
ret_run logs_cli retrieval backend; rc=$?
check "retrieval logs refuse a service outside the catalog" "1/no" "$rc/$(has "$ret_log" ' logs ')"

# retrieval bench runs the Go benchmark in the backend container (dev/staging)
# against the overlay. The stubs are redefined inside the wrapper subshell
# because ret_run's own retrieval_init_env would otherwise win.
ret_bench() {
    retrieval_project_active() { return 0; }
    retrieval_bench "$@"
}
ret_bench_prod() {
    retrieval_init_env() { ENV=production; ENV_FILE="$RET_ENV_FILE"; }
    retrieval_project_active() { return 0; }
    retrieval_bench "$@"
}
ret_bench_inactive() {
    retrieval_project_active() { return 1; }
    retrieval_bench "$@"
}
ret_run ret_bench_prod; rc=$?
check "retrieval bench refuses production" "yes" \
    "$([ "$rc" -ne 0 ] && [ "$(has "$ret_log" ' exec ')" = no ] && printf yes || printf no)"
ret_run ret_bench_inactive; rc=$?
check "retrieval bench needs the overlay" "yes" \
    "$([ "$rc" -ne 0 ] && [ "$(has "$ret_log" ' exec ')" = no ] && grep -q 'retrieval up' "$ret_out" && printf yes || printf no)"
ret_run ret_bench -k 5; rc=$?
check "retrieval bench runs in the backend container" "0/yes" \
    "$rc/$(has "$ret_log" 'compose .* -f .*docker-compose.retrieval.yml .* exec -T backend go run ./cmd/retrieval-bench -fixture internal/core/retrieval/bench/testdata/smoke-it-en.jsonl -k 5$')"

rm -f "$ret_env_file" "$ret_log" "$ret_out" "$ret_env_log"
unset -f ret_run ret_stack_files has ret_bench ret_bench_prod ret_bench_inactive


# --- env-validate.sh: same-site host pairings (spec §8 follow-up #16) ---
# The validator derives its ENV_FILE from its own location, so a copy in a
# scratch PROJECT_ROOT is tested against a scratch docker/.env — the
# developer's real one is never read. Same trick as the JWT generator above.
ev_tmp="$(mktemp -d)"
mkdir -p "$ev_tmp/scripts" "$ev_tmp/docker"
cp "$DIR/env-validate.sh" "$DIR/env-file.sh" "$ev_tmp/scripts/"
ev_env="$ev_tmp/docker/.env"
ev_out="$(mktemp)"
# ev_run KEY=VALUE... — shipped .env.example plus the overrides, validated;
# echoes the exit status and leaves the output in $ev_out for assertions.
ev_run() {
    local kv
    cp "$PROJECT_ROOT/docker/.env.example" "$ev_env"
    for kv in "$@"; do env_set "$ev_env" "${kv%%=*}" "${kv#*=}"; done
    bash "$ev_tmp/scripts/env-validate.sh" > "$ev_out" 2>&1
    printf '%s' "$?"
}
ev_saw() { grep -q "$1" "$ev_out" && printf yes || printf no; }

check "env-validate: the shipped .env.example is same-site" "0" "$(ev_run)"

ev_status="$(ev_run CLIENT_API_HOST=api.localhost \
    CLIENT_API_URL=http://api.localhost:3000 \
    CLIENT_FRONTEND_URL=http://localhost:8081)"
check "env-validate: the pre-#10 client triple is an error" "1" "$ev_status"
# Hostnames only: the compare strips the scheme and the :3000 / :8081 ports.
check "env-validate: the client refusal names all three keys" "yes" \
    "$(ev_saw 'Client tier is cross-site: CLIENT_API_HOST=api.localhost, CLIENT_API_URL=api.localhost, CLIENT_FRONTEND_URL=localhost')"
check "env-validate: the client refusal carries the migration keys" "yes" \
    "$(ev_saw 'CLIENT_FRONTEND_URL=http://client.localhost:8081')"
check "env-validate: the client refusal points at docker/AGENTS.md" "yes" \
    "$(ev_saw 'docker/AGENTS.md')"

check "env-validate: the migrated client triple passes" "0" \
    "$(ev_run CLIENT_API_HOST=client.localhost \
        CLIENT_API_URL=http://client.localhost:3000 \
        CLIENT_FRONTEND_URL=http://client.localhost:8081)"

ev_status="$(ev_run FRONTEND_URL=http://console.localhost:8080 VITE_API_URL=http://localhost:3000)"
check "env-validate: a cross-site operator pairing is an error" "1" "$ev_status"
check "env-validate: the operator refusal names both keys" "yes" \
    "$(ev_saw 'Operator tier is cross-site: VITE_API_URL=localhost, FRONTEND_URL=console.localhost')"

# An unset key is not part of the comparison — only the keys that are set.
check "env-validate: an empty CLIENT_API_URL is not compared" "0" "$(ev_run CLIENT_API_URL=)"

# The rule is same SITE, not same host: a real deployment spreads a tier over
# sibling subdomains of one registrable domain and must pass. ENV stays
# development in these rows so the staging/production security checks (a
# different part of the script) cannot decide the exit code.
check "env-validate: a staging-shaped env is same-site" "0" \
    "$(ev_run CLIENT_API_HOST=staging-api.orkestra.cc \
        CLIENT_API_URL=https://staging-api.orkestra.cc \
        CLIENT_FRONTEND_URL=https://app.orkestra.cc \
        FRONTEND_URL=https://staging.orkestra.cc \
        VITE_API_URL=https://staging-api.orkestra.cc)"
check "env-validate: a production-shaped env is same-site" "0" \
    "$(ev_run CLIENT_API_HOST=api.orkestra.cc \
        CLIENT_API_URL=https://api.orkestra.cc \
        CLIENT_FRONTEND_URL=https://app.orkestra.cc \
        FRONTEND_URL=https://orkestra.cc \
        VITE_API_URL=https://api.orkestra.cc)"
# Same rule, one registrable domain apart.
check "env-validate: sibling subdomains of one domain pass" "0" \
    "$(ev_run CLIENT_API_HOST=a.example.com \
        CLIENT_API_URL=https://a.example.com \
        CLIENT_FRONTEND_URL=https://b.example.com)"
check "env-validate: two registrable domains are refused" "1" \
    "$(ev_run CLIENT_API_HOST=example.com \
        CLIENT_API_URL=https://example.com \
        CLIENT_FRONTEND_URL=https://example.org)"

# A .invalid client API host (RFC 2606) is how a deployment says it has no
# client tier — there is no pairing to check.
check "env-validate: a .invalid client host skips the pairing" "0" \
    "$(ev_run CLIENT_API_HOST=client-disabled.invalid \
        CLIENT_FRONTEND_URL=https://app.orkestra.cc)"
check "env-validate: the .invalid skip says so" "yes" \
    "$(ev_saw 'Client tier disabled (CLIENT_API_HOST is a .invalid host) — pairing not checked')"

# Hostnames are case-insensitive; the comparison lowercases before comparing.
check "env-validate: host case does not make a mismatch" "0" \
    "$(ev_run CLIENT_API_HOST=Client.LocalHost \
        CLIENT_API_URL=http://client.localhost:3000 \
        CLIENT_FRONTEND_URL=http://CLIENT.localhost:8081)"

# The deploy preflight is the other half of the guard: a cross-site .env must
# abort the deploy before any compose command runs, `--yes` included (there is
# no prompt in front of it to skip).
touch "$ev_tmp/docker/docker-compose.infra.yml" "$ev_tmp/docker/docker-compose.dev.yml"
ev_deploy_log="$(mktemp)"
ev_deploy_out="$(mktemp)"
export EV_DEPLOY_LOG="$ev_deploy_log"
ev_run CLIENT_API_HOST=api.localhost > /dev/null
ev_deploy_status="$(
    (
        SCRIPT_DIR="$ev_tmp"
        ENV=development
        DEPLOY_SCOPE=all
        REBUILD_IMAGES=yes
        BRANCH=any
        SKIP_CONFIRMATION=yes
        INFRA_COMPOSE="$ev_tmp/docker/docker-compose.infra.yml"
        COMPOSE_FILE="$ev_tmp/docker/docker-compose.dev.yml"
        ENV_FILE="$ev_env"
        check_docker_running() { :; }
        ensure_jwt_keys_readable() { :; }
        docker() {
            local IFS=' '
            printf '%s\n' "docker $*" >> "$EV_DEPLOY_LOG"
            return 0
        }
        fullstack_execute_deploy
    ) > "$ev_deploy_out" 2>&1
    printf '%s' "$?"
)"
check "orkestra.sh deploy aborts on a cross-site docker/.env" "1" "$ev_deploy_status"
check "orkestra.sh deploy runs no compose command when it aborts" "0" \
    "$(wc -l < "$ev_deploy_log" | tr -d ' ')"
# ...and that it aborted for THIS reason, not an unrelated earlier failure.
check "orkestra.sh deploy names the validation as the reason" "yes" \
    "$(grep -q 'failed validation' "$ev_deploy_out" && grep -q 'Nothing was started' "$ev_deploy_out" && printf yes || printf no)"
rm -f "$ev_deploy_log" "$ev_deploy_out"

# --- The Gotenberg PDF sidecar is a first-class infra service —
# `orkestra.sh deploy --scope infra` must start it alongside mongodb/redis/
# rustfs, not silently skip it (it used to: the `up -d` list hardcoded the
# original three and every clone ran with no gotenberg container at all). ---
ev_run > /dev/null # restore a valid same-site .env; the previous case left a cross-site one
gotenberg_deploy_log="$(mktemp)"
gotenberg_deploy_out="$(mktemp)"
export GOTENBERG_DEPLOY_LOG="$gotenberg_deploy_log"
gotenberg_deploy_status="$(
    (
        SCRIPT_DIR="$ev_tmp"
        DOCKER_DIR="$ev_tmp/docker"
        ENV=development
        DEPLOY_SCOPE=infra
        REBUILD_IMAGES=no
        BRANCH=any
        SKIP_CONFIRMATION=yes
        INFRA_COMPOSE="$ev_tmp/docker/docker-compose.infra.yml"
        COMPOSE_FILE="$ev_tmp/docker/docker-compose.dev.yml"
        ENV_FILE="$ev_env"
        check_docker_running() { :; }
        ensure_jwt_keys_readable() { :; }
        docker() {
            local IFS=' '
            printf '%s\n' "docker $*" >> "$GOTENBERG_DEPLOY_LOG"
            return 0
        }
        fullstack_execute_deploy
    ) > "$gotenberg_deploy_out" 2>&1
    printf '%s' "$?"
)"
check "orkestra.sh infra deploy succeeds" "0" "$gotenberg_deploy_status"
check "orkestra.sh infra deploy starts gotenberg with mongodb/redis/rustfs" "yes" \
    "$(grep -Eq 'compose .* up -d mongodb redis rustfs gotenberg$' "$gotenberg_deploy_log" && printf yes || printf no)"
rm -f "$gotenberg_deploy_log" "$gotenberg_deploy_out"

# --- scripts/health-check.sh checks gotenberg exactly like the other infra
# containers: Docker container state + its compose healthcheck, no HTTP
# probe (it has no published port to probe from the host). Run the real
# script as its own process with a stub `docker` on PATH so it never
# touches a live daemon; the app-tier checks short-circuit on "container
# not found" before any HTTP probe, so this stays instant. ---
hc_tmp="$(mktemp -d)"
mkdir -p "$hc_tmp/scripts" "$hc_tmp/docker" "$hc_tmp/bin"
cp "$DIR/health-check.sh" "$DIR/env-file.sh" "$hc_tmp/scripts/"
printf 'APP_NAME=hc-test\nENV=development\n' > "$hc_tmp/docker/.env"
cat > "$hc_tmp/bin/docker" <<'DOCKEREOF'
#!/usr/bin/env bash
# Fake docker: answers `inspect -f` for mongodb/redis/rustfs/gotenberg only;
# everything else "not found" (empty stdout, exit 1), same as a real daemon
# asked about a container that was never started.
args="$*"
case "$args" in
    *"State.Health"*)
        case "$args" in
            *gotenberg*) echo "${GOTENBERG_HEALTH:-healthy}" ;;
            *mongodb*|*redis*|*rustfs*) echo healthy ;;
            *) exit 1 ;;
        esac ;;
    *"State.Status"*)
        case "$args" in
            *gotenberg*|*mongodb*|*redis*|*rustfs*) echo running ;;
            *) exit 1 ;;
        esac ;;
    *) exit 1 ;;
esac
DOCKEREOF
chmod +x "$hc_tmp/bin/docker"

hc_out="$(mktemp)"
hc_status="$(PATH="$hc_tmp/bin:$PATH" bash "$hc_tmp/scripts/health-check.sh" development infra > "$hc_out" 2>&1; printf '%s' "$?")"
check "health-check.sh infra scope passes when gotenberg is healthy" "0" "$hc_status"
check "health-check.sh reports gotenberg healthy" "yes" \
    "$(grep -q 'gotenberg: running (healthy)' "$hc_out" && printf yes || printf no)"

hc_status_unhealthy="$(GOTENBERG_HEALTH=unhealthy PATH="$hc_tmp/bin:$PATH" bash "$hc_tmp/scripts/health-check.sh" development infra > "$hc_out" 2>&1; printf '%s' "$?")"
check "health-check.sh infra scope fails when gotenberg is unhealthy" "1" "$hc_status_unhealthy"
check "health-check.sh names gotenberg as the failing service" "yes" \
    "$(grep -q 'gotenberg: container reports unhealthy' "$hc_out" && printf yes || printf no)"
rm -rf "$hc_tmp"
rm -f "$hc_out"

# wiz_urls on a RE-RUN over a pre-#10 .env: the stored CLIENT_API_URL still
# says api.localhost, so it must NOT come back as the default once the user
# moves CLIENT_API_HOST to client.localhost and presses Enter through.
ev_run CLIENT_API_HOST=api.localhost CLIENT_API_URL=http://api.localhost:3000 > /dev/null
(
    ENV_FILE="$ev_env"
    HAS_GUM=false
    # y = customize; 5 defaults; the new client host typed; 7 defaults.
    printf 'y\n\n\n\n\n\nclient.localhost\n\n\n\n\n\n\n\n' | wiz_urls development
) > /dev/null 2>&1
check "wiz_urls rebuilds a stale CLIENT_API_URL from the new host" \
    "http://client.localhost" "$(env_get "$ev_env" CLIENT_API_URL)"
check "wiz_urls kept the host the user typed" \
    "client.localhost" "$(env_get "$ev_env" CLIENT_API_HOST)"
rm -rf "$ev_tmp"
rm -f "$ev_out"

# --- Clone-version pin: exported once by the caller, honored on every
# resolution, never confused with the resolver's own previous export. ---
pin_tmp="$(mktemp)"
printf 'APP_NAME=orkestra-test\nENV=staging\n' > "$pin_tmp"
old_env_file="$ENV_FILE"
ENV_FILE="$pin_tmp"
ENV=staging
ORKESTRA_CLONE_VERSION_PIN="v9.9.9"
resolve_stack_identity
check "clone-version pin wins over git describe" "v9.9.9" "$ORKESTRA_CLONE_VERSION"
ORKESTRA_CLONE_VERSION_PIN=""
ORKESTRA_CLONE_VERSION="stale-from-previous-resolution"
resolve_stack_identity
check "re-resolution without a pin recomputes the clone version" "dev" "$ORKESTRA_CLONE_VERSION"
check "build commit is derived from HEAD" "$(git rev-parse --short HEAD)" "$ORKESTRA_BUILD_COMMIT"
unset ORKESTRA_CLONE_VERSION ORKESTRA_BUILD_COMMIT
ENV_FILE="$old_env_file"
rm -f "$pin_tmp"


# prepare_dev_frontend_dirs creates each dev frontend's bind sources and its
# node_modules mount point before compose would create them as root, skips an
# app the checkout does not have, and refuses a dependency or npm cache
# directory the container could not write.
old_project_root="$PROJECT_ROOT"
PROJECT_ROOT=$(mktemp -d)
mkdir -p "$PROJECT_ROOT/frontend-admin"
prepare_dev_frontend_dirs; rc=$?
check "dev frontend dirs: succeeds" "0" "$rc"
check "dev frontend dirs: admin dependencies and mount point" "yes" \
    "$([ -d "$PROJECT_ROOT/frontend-admin/.docker/node_modules" ] && [ -d "$PROJECT_ROOT/frontend-admin/node_modules" ] && echo yes)"
check "dev frontend dirs: caches" "yes" \
    "$([ -d "$PROJECT_ROOT/frontend-admin/.npm-cache" ] && [ -d "$PROJECT_ROOT/frontend-admin/.vite-cache" ] && echo yes)"
check "dev frontend dirs: a missing app is skipped" "no" "$([ -e "$PROJECT_ROOT/frontend-client" ] && echo yes || echo no)"
mkdir -p "$PROJECT_ROOT/frontend-client"
prepare_dev_frontend_dirs; rc=$?
check "dev frontend dirs: client dependencies and mount point" "yes" \
    "$([ "$rc" = 0 ] && [ -d "$PROJECT_ROOT/frontend-client/.docker/node_modules" ] && [ -d "$PROJECT_ROOT/frontend-client/node_modules" ] && echo yes)"
check "dev frontend dirs: only the apps it is given" "yes" \
    "$(rm -r "$PROJECT_ROOT/frontend-client/.npm-cache" && prepare_dev_frontend_dirs frontend-admin && [ ! -e "$PROJECT_ROOT/frontend-client/.npm-cache" ] && echo yes)"
prepare_dev_frontend_dirs
# The refusals rely on chmod making a directory unwritable, which root ignores.
if [ "$(id -u)" = 0 ]; then
    echo "  skip dev frontend dirs: unwritable-directory checks (running as root)"
else
chmod 555 "$PROJECT_ROOT/frontend-client/.docker/node_modules"
msg=$(prepare_dev_frontend_dirs 2>&1); rc=$?
check "dev frontend dirs: unwritable dependencies refused" "1" "$rc"
check "dev frontend dirs: the refusal names the directory" "yes" \
    "$(grep -Fq "$PROJECT_ROOT/frontend-client/.docker/node_modules" <<<"$msg" && echo yes)"
chmod 755 "$PROJECT_ROOT/frontend-client/.docker/node_modules"
chmod 555 "$PROJECT_ROOT/frontend-client/.npm-cache"
prepare_dev_frontend_dirs 2>/dev/null; rc=$?
check "dev frontend dirs: unwritable npm cache refused" "1" "$rc"
chmod 755 "$PROJECT_ROOT/frontend-client/.npm-cache"
# frontend-admin's npm cache is /app/.npm-cache too: an unwritable one is refused.
chmod 555 "$PROJECT_ROOT/frontend-admin/.npm-cache"
prepare_dev_frontend_dirs 2>/dev/null; rc=$?
check "dev frontend dirs: admin's unwritable npm cache refused" "1" "$rc"
chmod 755 "$PROJECT_ROOT/frontend-admin/.npm-cache"
# A backend-only deploy prepares no frontend: an unwritable one does not stop it.
chmod 555 "$PROJECT_ROOT/frontend-client/.npm-cache"
prepare_dev_frontend_dirs frontend-admin; rc=$?
check "dev frontend dirs: an app not given is not checked" "0" "$rc"
chmod 755 "$PROJECT_ROOT/frontend-client/.npm-cache"
fi
rm -r "$PROJECT_ROOT"
PROJECT_ROOT="$old_project_root"

echo
printf 'orkestra-helpers: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
