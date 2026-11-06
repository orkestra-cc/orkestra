#!/usr/bin/env bash
# `./orkestra.sh tunnel` (Cloudflare Tunnel <-> direct switch) against a FAKE
# Cloudflare API (docker/tests/fake-cloudflare-api.py on 127.0.0.1, with the
# all-or-nothing batch endpoint) and a fake `docker` (a shell function:
# container state, `cloudflared tunnel ready`, metrics, route probes, logs,
# compose). Never calls the real API, never starts cloudflared, never touches a
# real container: every name below is a scratch one.
#
# Pins: every DNS change of a name is one batch, so a failed call never leaves
# a name without its record (review B1: a failed restore used to lose it, and
# a re-run then called it "already direct"); the snapshot entry of a name is
# never overwritten and `switch direct` reconciles every name to it; a name
# with no record and no wildcard, or on another tunnel, is an error; the DNS
# moves towards the tunnel only once cloudflared is ready, has no remote
# config and every route answers from its network, and towards the direct
# path only once the origin answers; a name failing its smoke test is put
# back automatically; cloudflared is removed only once it is drained; a
# wildcard record is never written; the API token never reaches curl's argv.
# Plus the compose contract of the cloudflared service.
set -uo pipefail
# shellcheck disable=SC2046
unset $(git rev-parse --local-env-vars 2> /dev/null)
repo=$(cd "$(dirname "$0")/../.." && pwd)
command -v python3 > /dev/null || { echo "FAIL: python3 is required"; exit 1; }
command -v jq > /dev/null || { echo "FAIL: jq is required"; exit 1; }

pass=0; fail=0
check() {
    if [ "$2" = "$3" ]; then pass=$((pass + 1)); printf '  ok   %s\n' "$1"
    else fail=$((fail + 1)); printf '  FAIL %s\n       expected: [%s]\n       actual:   [%s]\n' "$1" "$2" "$3"; fi
}

tmp=$(mktemp -d)
api_pid=""
cleanup() { [ -z "$api_pid" ] || kill "$api_pid" 2> /dev/null; rm -rf "$tmp"; }
trap cleanup EXIT

# --- scratch checkout: docker/ with routes for both environments ----------
dd="$tmp/docker"
mkdir -p "$dd/cloudflared"
touch "$dd/docker-compose.infra.yml" "$dd/docker-compose.prod.yml" "$dd/docker-compose.staging.yml"
PROD_TID=11111111-2222-4333-8444-555555555555
STG_TID=99999999-8888-4777-8666-555555555555
routes() { # routes ENV TUNNEL_ID
    cat > "$dd/cloudflared/$1.json" << EOF
{
  "_note": "test",
  "_tunnel_id": "$2",
  "_smoke": {"console.example.com": "/health", "api.example.com": "/health", "form.public.example.com": "/health"},
  "ingress": [
    {"hostname": "console.example.com", "service": "http://frontend-admin:80"},
    {"hostname": "api.example.com", "service": "http://backend:3000"},
    {"hostname": "form.public.example.com", "service": "http://frontend-public:80"},
    {"service": "http_status:404"}
  ]
}
EOF
    chmod 644 "$dd/cloudflared/$1.json"
}
routes production "$PROD_TID"
routes staging "$STG_TID"

# token ACCOUNT TUNNEL_ID — base64url JSON without padding, as the dashboard
# prints it.
token() { printf '{"a":"%s","t":"%s","s":"c2VjcmV0"}' "$1" "$2" | base64 -w0 | tr '/+' '_-' | tr -d '='; }
API_TOKEN="cf-api-token-$RANDOM$RANDOM"
write_env() { # write_env ENV TUNNEL_TOKEN [EXTRA LINES...] (TRUSTED_PROXY_CIDRS covering the fake network unless given)
    local env=$1 tok=$2; shift 2
    {
        printf 'APP_NAME=acme\nENV=%s\nCLOUDFLARE_TUNNEL=false\n' "$env"
        printf 'CLOUDFLARE_TUNNEL_TOKEN=%s\nCLOUDFLARE_API_TOKEN=%s\n' "$tok" "$API_TOKEN"
        printf '%s\n' "$@"
    } > "$dd/.env"
    grep -q '^TRUSTED_PROXY_' "$dd/.env" || echo TRUSTED_PROXY_CIDRS=192.168.88.2/32,172.30.0.0/24 >> "$dd/.env"
}

# --- fake Cloudflare API --------------------------------------------------
# Direct records: console A + AAAA (proxied) + a TXT that must survive, api
# CNAME, and form.public.example.com with NO record of its own — it answers
# through the wildcard *.public.example.com, which must never be written.
state="$tmp/cf.json"
seed_dns() {
    jq -n --arg t "$API_TOKEN" '{token: $t, zones: {"example.com": "zone-1"}, fail_batch: [], lax_name: false,
        records: [
          {id: "r1", type: "A", name: "console.example.com", content: "203.0.113.10", proxied: true, ttl: 1, comment: "office line"},
          {id: "r1b", type: "AAAA", name: "console.example.com", content: "2001:db8::10", proxied: true, ttl: 1},
          {id: "r2", type: "CNAME", name: "api.example.com", content: "origin.example.net", proxied: true, ttl: 1},
          {id: "r3", type: "TXT", name: "console.example.com", content: "keep-me", proxied: false, ttl: 300},
          {id: "rw", type: "A", name: "*.public.example.com", content: "203.0.113.99", proxied: true, ttl: 1}
        ]}' > "$state"
    : > "$state.log"; : > "$state.batches"
}
seed_dns
python3 -I "$repo/docker/tests/fake-cloudflare-api.py" "$state" &
api_pid=$!
for _ in $(seq 1 50); do [ -s "$state.port" ] && break; /bin/sleep 0.1; done
[ -s "$state.port" ] || { echo "FAIL: fake Cloudflare API did not start"; exit 1; }
# The fake serves /zones... at its root (no /client/v4 prefix).
export CLOUDFLARE_API_URL="http://127.0.0.1:$(cat "$state.port")"
api_set() { jq "$@" "$state" > "$tmp/s" && cp "$tmp/s" "$state"; }

dns() { jq -c --arg n "$1" '[.records[] | select(.name == $n and .type != "TXT") | {type, content, proxied}] | sort_by(.type)' "$state"; }
writes() { grep -cE '^(POST|DELETE) ' "$state.log" || true; }
single_writes() { grep -cE '^(DELETE |POST /zones/zone-1/dns_records )' "$state.log" || true; }
batches() { grep -c . "$state.batches" || true; }
wildcard_intact() { jq -r '[.records[] | select(.name | startswith("*."))] | if length == 1 and .[0].id == "rw" and .[0].content == "203.0.113.99" then "intact" else tostring end' "$state"; }

# --- fake docker + probes -------------------------------------------------
# $tmp/container: absent | running | stopped | foreign. $tmp/ready: edge
# connections. $tmp/origin/<host>: the route probe's status from cloudflared's
# network; $tmp/http/<host>: the public smoke status; $tmp/direct/<host>: the
# origin's status for the direct pre-check. $tmp/requests-seq: successive
# cloudflared_tunnel_total_requests readings (the last one repeats).
export T_TMP="$tmp"
mkdir -p "$tmp/http" "$tmp/origin" "$tmp/direct"

# --- the code under test --------------------------------------------------
# shellcheck source=/dev/null
source "$repo/orkestra.sh"
trap - INT ERR
trap cleanup EXIT
set +Eeuo pipefail

# run_tunnel ENV CMD... — one command in a subshell (die exits), with scratch
# paths and the fakes. Prints the exit status; output in $tmp/out.
run_tunnel() {
    local env=$1; shift
    (
        # The CLI runs under orkestra.sh's strict mode: so do the cases.
        set -euo pipefail
        ENV=$env PROJECT_ROOT="$tmp" DOCKER_DIR="$dd" ENV_FILE="$dd/.env"
        TUNNEL_CF_API="$CLOUDFLARE_API_URL"
        TUNNEL_READY_ATTEMPTS=2 TUNNEL_SMOKE_ATTEMPTS=1 TUNNEL_SMOKE_ROUNDS=2
        TUNNEL_DRAIN_FLAT_SAMPLES=2 TUNNEL_DRAIN_MAX_SAMPLES=5
        COMPOSE_EXTRA_FILES=()
        docker() {
            local IFS=' ' h
            printf '%s\n' "docker $*" >> "$T_TMP/docker.log"
            case "$*" in
                "ps -q") cat "$T_TMP/others-ids" 2> /dev/null || true ;;
                "inspect --type container -f {{.Name}}"*)
                    cat "$T_TMP/others/${@: -1}" 2> /dev/null || return 1 ;;
                "inspect --type container -f "*)
                    case "$(cat "$T_TMP/container")" in
                        running) printf 'true|acme-%s\n' "$ENV" ;;
                        stopped) printf 'false|acme-%s\n' "$ENV" ;;
                        foreign) printf 'true|someone-else\n' ;;
                        *) return 1 ;;
                    esac ;;
                "image inspect "*) ! grep -qxF "${3:-}" "$T_TMP/no-image" 2> /dev/null ;;
                "pull "*) [ ! -f "$T_TMP/pull-fails" ] ;;
                "run --rm --network none "*"ingress validate")
                    if [ -f "$T_TMP/validate-fails" ]; then echo "Validating rules: error parsing ingress"; return 1; fi
                    echo OK ;;
                "run --rm --network container:"*" ready")
                    [ "$(cat "$T_TMP/container")" = running ] && [ "$(cat "$T_TMP/ready")" -gt 0 ] ;;
                "run --rm --network container:"*"/metrics")
                    local n
                    n=$(head -1 "$T_TMP/requests-seq" 2> /dev/null || echo 7)
                    [ "$(wc -l < "$T_TMP/requests-seq" 2> /dev/null || echo 0)" -le 1 ] || sed -i 1d "$T_TMP/requests-seq"
                    printf '# HELP x\ncloudflared_tunnel_total_requests %s\n' "$n" ;;
                "run --rm --network container:"*"-H Host: "*)
                    h="$*"; h=${h#*Host: }; h=${h%% *}
                    if [ -f "$T_TMP/origin/$h" ]; then cat "$T_TMP/origin/$h"; else printf 200; fi ;;
                "logs "*) cat "$T_TMP/cf-logs" 2> /dev/null || true ;;
                "network inspect "*"_default -f "*) printf '%s ' "$(cat "$T_TMP/subnet" 2> /dev/null || echo 172.30.0.0/24)" ;;
                "network inspect "*) : ;;
                *" config --format json") printf '{"networks":{"default":{"name":"acme_default"}}}' ;;
                *" up -d --no-deps cloudflared") echo running > "$T_TMP/container" ;;
                *" rm -sf cloudflared") echo absent > "$T_TMP/container" ;;
            esac
        }
        curl() { local IFS=' '; printf '%s\n' "$*" >> "$T_TMP/curl.log"; command curl "$@"; }
        tunnel_http_status() {
            printf '%s\n' "$1" >> "$T_TMP/smoke.log"
            local host=${1#https://}; host=${host%%/*}
            if [ -f "$T_TMP/http/$host" ]; then cat "$T_TMP/http/$host"; else printf 200; fi
        }
        tunnel_direct_probe() {
            printf '%s %s %s\n' "$1" "$2" "$3" >> "$T_TMP/direct.log"
            if [ -f "$T_TMP/direct/$1" ]; then cat "$T_TMP/direct/$1"; else printf 200; fi
        }
        sleep() { :; }
        "$@"
    ) < /dev/null > "$tmp/out" 2>&1
    printf '%s' "$?"
}
said() { grep -qF -- "$1" "$tmp/out" && printf yes || printf no; }
logged() { grep -qE -- "$1" "$tmp/docker.log" 2> /dev/null && printf yes || printf no; }
reset_fakes() {
    : > "$tmp/docker.log"; : > "$tmp/curl.log"; : > "$tmp/smoke.log"; : > "$tmp/direct.log"
    echo absent > "$tmp/container"; echo 4 > "$tmp/ready"
    rm -f "$tmp/http/"* "$tmp/origin/"* "$tmp/direct/"* "$tmp/requests-seq" "$tmp/cf-logs" \
        "$tmp/validate-fails" "$tmp/no-image" "$tmp/pull-fails" "$tmp/subnet"
    : > "$state.log"; : > "$state.batches"
}
flag() { env_get_active "$dd/.env" CLOUDFLARE_TUNNEL; }

switch_tunnel() { tunnel_init_env && tunnel_switch_tunnel yes; }
switch_direct() { tunnel_init_env && tunnel_switch_direct yes; }
status() { tunnel_init_env && tunnel_status; }
reload() { tunnel_init_env && tunnel_reload; }

PROD_TOKEN=$(token acct "$PROD_TID")
snap="$dd/cloudflared/state/production-direct-dns.json"
direct_console='[{"type":"A","content":"203.0.113.10","proxied":true},{"type":"AAAA","content":"2001:db8::10","proxied":true}]'
direct_api='[{"type":"CNAME","content":"origin.example.net","proxied":true}]'
on_tunnel="[{\"type\":\"CNAME\",\"content\":\"$PROD_TID.cfargotunnel.com\",\"proxied\":true}]"
all_direct() { [ "$(dns console.example.com)|$(dns api.example.com)|$(dns form.public.example.com)" = "$direct_console|$direct_api|[]" ] && echo yes || echo no; }

echo "refusals (nothing changes)"
reset_fakes; write_env development "$PROD_TOKEN"
check "development is refused" "1|yes" "$(run_tunnel development switch_tunnel)|$(said 'staging and production only')"
write_env staging "$PROD_TOKEN"
check "shell ENV that disagrees with docker/.env is refused" "1|yes" \
    "$(run_tunnel production switch_tunnel)|$(said 'docker/.env says ENV=staging')"
write_env production "$PROD_TID"
check "a tunnel id pasted as the token is refused" "1|yes" \
    "$(run_tunnel production switch_tunnel)|$(said 'looks like the tunnel ID')"
write_env production "$(token acct "$STG_TID")"
check "the staging tunnel's token is refused in production" "1|yes" \
    "$(run_tunnel production switch_tunnel)|$(said 'is the tunnel of staging')"
write_env production "not-a-token"
check "garbage token is refused" "1|yes" "$(run_tunnel production switch_tunnel)|$(said 'not a valid tunnel token')"
routes production "TODO-tunnel-id"; write_env production "$PROD_TOKEN"
check "a TODO _tunnel_id is refused, naming the id to pin" "1|yes" \
    "$(run_tunnel production switch_tunnel)|$(said "write $PROD_TID there")"
routes production "$PROD_TID"
mv "$dd/cloudflared/production.json" "$tmp/routes.bak"
check "a missing routes file is refused with the way out" "1|yes" \
    "$(run_tunnel production switch_tunnel)|$(said 'The base ships no routes')"
mv "$tmp/routes.bak" "$dd/cloudflared/production.json"
jq '.ingress |= map(select(.hostname))' "$dd/cloudflared/production.json" > "$tmp/r" && cp "$tmp/r" "$dd/cloudflared/production.json"
check "routes without the catch-all rule are refused" "1|yes" "$(run_tunnel production switch_tunnel)|$(said 'catch-all')"
routes production "$PROD_TID"
jq '.ingress = [{"hostname": "*.example.com", "service": "http://backend:3000"}] + .ingress' "$dd/cloudflared/production.json" > "$tmp/r" \
    && cp "$tmp/r" "$dd/cloudflared/production.json"
check "a wildcard hostname is refused" "1|yes" "$(run_tunnel production switch_tunnel)|$(said 'wildcard hostname')"
routes production "$PROD_TID"
chmod 600 "$dd/cloudflared/production.json"
check "routes cloudflared cannot read are refused" "1|yes" "$(run_tunnel production switch_tunnel)|$(said 'not world-readable')"
chmod 644 "$dd/cloudflared/production.json"
check "production switch without --yes and no TTY is refused" "1|yes" \
    "$(run_tunnel production eval 'tunnel_init_env && tunnel_switch_tunnel no')|$(said 'interactive confirmation or --yes')"
check "refusals made no DNS write and started nothing" "0|no" "$(writes)|$(logged ' up -d')"

echo "names in an unexpected DNS state (nothing changes)"
reset_fakes
api_set '(.records[] | select(.id == "r2")) |= (.content = "00000000-0000-4000-8000-000000000000.cfargotunnel.com")'
check "a name on ANOTHER tunnel is refused before anything starts" "1|yes|no|0" \
    "$(run_tunnel production switch_tunnel)|$(said 'points at another tunnel')|$(logged ' up -d')|$(writes)"
check "status shows it as OTHER, not direct" "yes" "$(run_tunnel production status > /dev/null; said 'OTHER    api.example.com')"
seed_dns; reset_fakes
api_set '.records |= map(select(.id != "rw"))'
check "a name with no record and no wildcard is refused (N7)" "1|yes|0" \
    "$(run_tunnel production switch_tunnel)|$(said 'form.public.example.com has no DNS record and no wildcard')|$(writes)"
seed_dns; reset_fakes

echo "cloudflared must be proved before any DNS write"
touch "$tmp/validate-fails"
check "ingress validate failure stops the switch before cloudflared starts" "1|yes|no|0" \
    "$(run_tunnel production switch_tunnel)|$(said 'fails ')|$(logged ' up -d')|$(writes)"
reset_fakes; echo 0 > "$tmp/ready"
check "cloudflared never ready: switch fails, DNS untouched" "1|yes|0|false" \
    "$(run_tunnel production switch_tunnel)|$(said 'No DNS record was changed')|$(writes)|$(flag)"
reset_fakes; echo "2026-11-05T10:00:00Z INF Updated to new configuration config=..." > "$tmp/cf-logs"
check "a remote (dashboard) configuration is refused" "1|yes|0" \
    "$(run_tunnel production switch_tunnel)|$(said 'REMOTE configuration')|$(writes)"
reset_fakes; echo 502 > "$tmp/origin/api.example.com"
check "a route that does not answer from cloudflared's network is refused" "1|yes|0|false" \
    "$(run_tunnel production switch_tunnel)|$(said 'Routes that do not answer from cloudflared: api.example.com')|$(writes)|$(flag)"
check "the route probe sends the public Host header to the service" "yes" \
    "$(logged 'curlimages/curl:8\.11\.1 -sk .* -H Host: api\.example\.com http://backend:3000/health$')"
reset_fakes; echo "$TUNNEL_PROBE_IMAGE" > "$tmp/no-image"; touch "$tmp/pull-fails"
check "a probe image that cannot be pulled is reported as a pull failure" "1|yes|0" \
    "$(run_tunnel production switch_tunnel)|$(said "Cannot pull $TUNNEL_PROBE_IMAGE")|$(writes)"

echo "switch tunnel"
reset_fakes
check "switch tunnel succeeds" "0" "$(run_tunnel production switch_tunnel)"
check "cloudflared started by exact service, tunnel profile, no deps" "yes" "$(logged '^docker compose .*--profile tunnel up -d --no-deps cloudflared$')"
check "readiness = 'cloudflared tunnel ready' with the pinned image in its namespace (no pull)" "yes" \
    "$(logged "^docker run --rm --network container:acme-cloudflared-production .*$TUNNEL_IMAGE tunnel --metrics 127\.0\.0\.1:20241 ready$")"
check "ingress validated in a throwaway container with no network" "yes" \
    "$(logged "^docker run --rm --network none .*$TUNNEL_IMAGE tunnel --config /etc/cloudflared/production.json ingress validate$")"
check "console (A + AAAA) now a proxied CNAME to the tunnel" "$on_tunnel" "$(dns console.example.com)"
check "api now a proxied CNAME to the tunnel" "$on_tunnel" "$(dns api.example.com)"
check "form (wildcard only) now has its own tunnel CNAME" "$on_tunnel" "$(dns form.public.example.com)"
check "one batch per moved name, no single-record write" "3|0" "$(batches)|$(single_writes)"
check "console's batch deletes A and AAAA and posts the CNAME together" '["r1","r1b"]|1' \
    "$(sed -n 1p "$state.batches" | jq -c '[.deletes[].id] | sort')|$(sed -n 1p "$state.batches" | jq '.posts | length')"
check "the TXT record of console survived" "keep-me" "$(jq -r '.records[] | select(.type == "TXT") | .content' "$state")"
check "snapshot holds console's A + AAAA with their comment" "$direct_console|office line" \
    "$(jq -c '.["console.example.com"].records | map({type, content, proxied}) | sort_by(.type)' "$snap")|$(jq -r '.["console.example.com"].records[] | select(.type == "A") | .comment' "$snap")"
check "snapshot holds api's direct record" "$direct_api" "$(jq -c '.["api.example.com"].records | map({type, content, proxied})' "$snap")"
check "snapshot records form as 'no record, via the wildcard'" '{"records":[],"via_wildcard":true}' "$(jq -c '.["form.public.example.com"]' "$snap")"
check "the wildcard was not touched" "intact" "$(wildcard_intact)"
check "CLOUDFLARE_TUNNEL=true for the next deploys" "true" "$(flag)"
check "smoke URLs carry a cache-busting query" "yes" "$(grep -qE '^https://api\.example\.com/health\?_orkestra_smoke=[0-9]+$' "$tmp/smoke.log" && echo yes || echo no)"
check "every API call carried the token" "0" "$(grep -c 'AUTH-BAD' "$state.log")"
check "the API token never reached curl's argv" "no" "$(grep -qF "$API_TOKEN" "$tmp/curl.log" && echo yes || echo no)"
check "no temp file left behind (atomic writes)" "" "$(find "$dd" -name '.*.json.*' -o -name '.env.tmp.*' | head -1)"

echo "switch tunnel again (idempotent)"
cp "$snap" "$tmp/snap.before"; reset_fakes; echo running > "$tmp/container"
check "second switch tunnel succeeds without a DNS write" "0|yes|0" "$(run_tunnel production switch_tunnel)|$(said 'already on the tunnel')|$(writes)"
check "second switch tunnel keeps the snapshot" "same" "$(cmp -s "$snap" "$tmp/snap.before" && echo same || echo changed)"

echo "status"
reset_fakes; echo running > "$tmp/container"
check "status lists every hostname on the tunnel and cloudflared ready" "0|3|yes" \
    "$(run_tunnel production status)|$(grep -c 'tunnel   .*example.com' "$tmp/out")|$(said 'ready (connected to Cloudflare)')"
api_set '.lax_name = true'
check "an API answer that also lists the wildcard is filtered by name" "0|3" \
    "$(run_tunnel production status)|$(grep -c 'tunnel   .*example.com' "$tmp/out")"
api_set '.lax_name = false'

echo "reload"
reset_fakes; echo running > "$tmp/container"
check "reload validates, probes, restarts and waits for ready" "0|yes|yes|yes" \
    "$(run_tunnel production reload)|$(logged 'ingress validate$')|$(logged '--profile tunnel restart cloudflared$')|$(said 'Tunnel ready')"
check "reload validates before it restarts" "yes" \
    "$(awk '/ingress validate$/ {v = NR} /restart cloudflared$/ {r = NR} END {print (v && r && v < r) ? "yes" : "no"}' "$tmp/docker.log")"
reset_fakes; echo running > "$tmp/container"; touch "$tmp/validate-fails"
check "reload refuses routes that fail validation, cloudflared untouched" "1|no" \
    "$(run_tunnel production reload)|$(logged 'restart cloudflared')"
reset_fakes; echo running > "$tmp/container"; echo 0 > "$tmp/ready"
check "reload never ready: the message does not claim 'No DNS record was changed'" "1|no|yes" \
    "$(run_tunnel production reload)|$(said 'No DNS record was changed')|$(said 'The DNS was not touched')"
reset_fakes
check "reload refuses when cloudflared is not running" "1" "$(run_tunnel production reload)"

echo "smoke failure through the tunnel: never roll back a name that was already there"
# Every name is on the tunnel (moved by the earlier runs): a failing smoke
# test must not send any of them back — during an outage the way back may be
# exactly what is down.
reset_fakes; echo running > "$tmp/container"; echo 404 > "$tmp/http/api.example.com"
check "a 404 fails the switch" "1|yes" "$(run_tunnel production switch_tunnel)|$(said 'api.example.com -> HTTP 404')"
check "…but no name was moved by this run: DNS untouched" "0|$on_tunnel|yes" \
    "$(writes)|$(dns api.example.com)|$(said 'api.example.com was already on the tunnel before this run: left as is')"

echo "smoke failure through the tunnel: a name this run moved goes back by itself"
reset_fakes; echo running > "$tmp/container"
run_tunnel production switch_direct > /dev/null
check "(setup) every name direct again" "yes" "$(all_direct)"
reset_fakes; echo 404 > "$tmp/http/api.example.com"
check "a 404 (catch-all: no route) fails the switch" "1|yes" "$(run_tunnel production switch_tunnel)|$(said 'api.example.com -> HTTP 404')"
check "api put back on its exact direct record, the others stay on the tunnel" "$direct_api|$on_tunnel" \
    "$(dns api.example.com)|$(dns console.example.com)"
check "CLOUDFLARE_TUNNEL stays true (names still on the tunnel)" "true" "$(flag)"
reset_fakes; echo running > "$tmp/container"
check "switch tunnel again moves api back" "0|$on_tunnel" "$(run_tunnel production switch_tunnel)|$(dns api.example.com)"
reset_fakes; echo running > "$tmp/container"; echo 301 > "$tmp/http/console.example.com"
check "a 3xx passes the smoke test" "0" "$(run_tunnel production switch_tunnel)"

echo "switch direct: the direct path is proved before any change"
reset_fakes; echo running > "$tmp/container"; echo 522 > "$tmp/direct/console.example.com"
check "origin not answering for a name: refused, nothing changed" "1|yes|0" \
    "$(run_tunnel production switch_direct)|$(said 'The direct path does not answer for console.example.com')|$(writes)"
check "the pre-check aims at the snapshot's A record, then the wildcard's" "yes|yes" \
    "$(grep -qxF 'console.example.com 203.0.113.10 /health' "$tmp/direct.log" && echo yes || echo no)|$(rm -f "$tmp/direct/"*; run_tunnel production switch_direct > /dev/null; grep -qxF 'form.public.example.com 203.0.113.99 /health' "$tmp/direct.log" && echo yes || echo no)"
run_tunnel production switch_tunnel > /dev/null
reset_fakes; echo running > "$tmp/container"
write_env production "$PROD_TOKEN" CLOUDFLARE_DIRECT_CHECK_IP=192.168.88.2; env_set "$dd/.env" CLOUDFLARE_TUNNEL true
run_tunnel production switch_direct > /dev/null
check "CLOUDFLARE_DIRECT_CHECK_IP overrides the target (no hairpin needed)" "3" "$(grep -c ' 192\.168\.88\.2 /health$' "$tmp/direct.log")"
write_env production "$PROD_TOKEN"; env_set "$dd/.env" CLOUDFLARE_TUNNEL true

echo "switch direct"
reset_fakes; run_tunnel production switch_tunnel > /dev/null
reset_fakes; echo running > "$tmp/container"; printf '5\n6\n7\n7\n7\n' > "$tmp/requests-seq"
check "switch direct succeeds once cloudflared is drained" "0|yes" "$(run_tunnel production switch_direct)|$(said 'cloudflared idle (7 requests in total)')"
check "every name back to exactly its direct records" "yes" "$(all_direct)"
check "console's A + AAAA restored in one batch, comment kept" "1|office line" \
    "$(jq -s '[.[] | select(any(.posts[]?; .name == "console.example.com"))] | length' "$state.batches")|$(jq -r '.records[] | select(.name == "console.example.com" and .type == "A") | .comment' "$state")"
check "form back to the wildcard: no record of its own, wildcard untouched" "[]|intact" "$(dns form.public.example.com)|$(wildcard_intact)"
check "cloudflared removed by exact service" "yes|absent" "$(logged '^docker compose .*--profile tunnel rm -sf cloudflared$')|$(cat "$tmp/container")"
check "snapshot removed, CLOUDFLARE_TUNNEL=false" "no|false" "$([ -f "$snap" ] && echo yes || echo no)|$(flag)"
reset_fakes
check "second switch direct is a no-op" "0|0" "$(run_tunnel production switch_direct)|$(writes)"

echo "switch direct while cloudflared still receives requests"
reset_fakes; run_tunnel production switch_tunnel > /dev/null
reset_fakes; echo running > "$tmp/container"; seq 1 20 > "$tmp/requests-seq"
check "DNS goes direct, but cloudflared is kept" "0|yes|running|true" \
    "$(run_tunnel production switch_direct)|$(all_direct)|$(cat "$tmp/container")|$(flag)"
reset_fakes; echo running > "$tmp/container"
check "a later run finds it drained and removes it" "0|absent|false" "$(run_tunnel production switch_direct)|$(cat "$tmp/container")|$(flag)"

echo "switch direct: smoke failure puts the failing name back on the tunnel"
reset_fakes; run_tunnel production switch_tunnel > /dev/null
reset_fakes; echo running > "$tmp/container"; echo 522 > "$tmp/http/api.example.com"
check "a 522 on the direct path fails the switch" "1|yes" "$(run_tunnel production switch_direct)|$(said 'api.example.com -> HTTP 522')"
check "api is back on the tunnel, console stays direct" "$on_tunnel|$direct_console" "$(dns api.example.com)|$(dns console.example.com)"
check "cloudflared kept, snapshot kept, CLOUDFLARE_TUNNEL still true" "running|yes|true" \
    "$(cat "$tmp/container")|$([ -f "$snap" ] && echo yes || echo no)|$(flag)"
reset_fakes; echo running > "$tmp/container"
check "a clean re-run finishes the way back" "0|yes|false" "$(run_tunnel production switch_direct)|$(all_direct)|$(flag)"

echo "switch direct: a name already direct before the run is never sent to the tunnel"
reset_fakes; run_tunnel production switch_tunnel > /dev/null
reset_fakes; echo running > "$tmp/container"; echo 522 > "$tmp/http/api.example.com"
run_tunnel production switch_direct > /dev/null     # api goes back to the tunnel, the others are direct
reset_fakes; echo running > "$tmp/container"; echo 522 > "$tmp/http/console.example.com"
check "console (direct before this run) fails the smoke test" "1|yes" \
    "$(run_tunnel production switch_direct)|$(said 'console.example.com was already direct before this run: left as is')"
check "console stays direct, api (moved by this run) is direct too" "$direct_console|$direct_api" \
    "$(dns console.example.com)|$(dns api.example.com)"
reset_fakes; echo running > "$tmp/container"
check "a clean re-run finishes" "0|yes|false" "$(run_tunnel production switch_direct)|$(all_direct)|$(flag)"

echo "B1: a failed API call never loses a name's records"
reset_fakes; run_tunnel production switch_tunnel > /dev/null
reset_fakes; echo running > "$tmp/container"; api_set '.fail_batch = ["api.example.com"]'
check "switch direct: the restore batch of api fails" "1|yes" "$(run_tunnel production switch_direct)|$(said 'Putting api.example.com back failed; it is unchanged')"
check "api still has its tunnel CNAME (never left without a record)" "$on_tunnel" "$(dns api.example.com)"
reset_fakes; echo running > "$tmp/container"
check "status says api is on the tunnel, not 'already direct'" "0|yes" "$(run_tunnel production status)|$(said 'tunnel   api.example.com')"
api_set '.fail_batch = []'; reset_fakes; echo running > "$tmp/container"
check "the re-run restores api exactly" "0|yes" "$(run_tunnel production switch_direct)|$(all_direct)"

reset_fakes; api_set '.fail_batch = ["api.example.com"]'
check "switch tunnel: the batch of api fails" "1|yes" "$(run_tunnel production switch_tunnel)|$(said 'Moving api.example.com failed; it is unchanged')"
check "api keeps its direct record; the name before it moved" "$direct_api|$on_tunnel" "$(dns api.example.com)|$(dns console.example.com)"
check "CLOUDFLARE_TUNNEL was set before the first DNS write" "true" "$(flag)"
cp "$snap" "$tmp/snap.before"
api_set '.fail_batch = []'; reset_fakes; echo running > "$tmp/container"
check "the re-run moves the rest" "0|$on_tunnel" "$(run_tunnel production switch_tunnel)|$(dns api.example.com)"
check "…without overwriting console's snapshot entry" "same" \
    "$(diff <(jq -S '.["console.example.com"]' "$tmp/snap.before") <(jq -S '.["console.example.com"]' "$snap") > /dev/null && echo same || echo changed)"

echo "reconcile: a name the old code left with no record comes back"
api_set '.records |= map(select(.name != "api.example.com"))'
reset_fakes; echo running > "$tmp/container"
check "switch tunnel refuses a name with no record and no wildcard" "1" "$(run_tunnel production switch_tunnel)"
reset_fakes; echo running > "$tmp/container"
check "switch direct reconciles it from its snapshot entry" "0|no|$direct_api" \
    "$(run_tunnel production switch_direct)|$(said 'api.example.com already direct')|$(dns api.example.com)"

echo "a snapshot entry is never overwritten"
reset_fakes; run_tunnel production switch_tunnel > /dev/null
reset_fakes; echo running > "$tmp/container"; echo 404 > "$tmp/http/api.example.com"
run_tunnel production switch_tunnel > /dev/null   # api fails and goes back to direct
api_set '(.records[] | select(.name == "api.example.com")) |= (.content = "changed-by-hand.example.net")'
reset_fakes; echo running > "$tmp/container"
check "switch tunnel refuses direct records that differ from the entry" "1|yes|0" \
    "$(run_tunnel production switch_tunnel)|$(said 'differ from its snapshot entry')|$(writes)"
check "switch direct refuses them too" "1|yes|0" \
    "$(run_tunnel production switch_direct)|$(said 'changed by hand')|$(writes)"
api_set '(.records[] | select(.name == "api.example.com")) |= (.content = "origin.example.net")'
reset_fakes; echo running > "$tmp/container"
check "with the record fixed, switch direct finishes" "0|yes" "$(run_tunnel production switch_direct)|$(all_direct)"

echo "switch direct without a snapshot"
reset_fakes; run_tunnel production switch_tunnel > /dev/null
rm -f "$snap"; reset_fakes; echo running > "$tmp/container"
check "no snapshot, no wildcard, no CLOUDFLARE_DIRECT_ORIGIN: refused, nothing written" "1|0|yes" \
    "$(run_tunnel production switch_direct)|$(writes)|$(said "don't know where the direct records point")"
write_env production "$PROD_TOKEN" CLOUDFLARE_DIRECT_ORIGIN=198.51.100.7
env_set "$dd/.env" CLOUDFLARE_TUNNEL true
reset_fakes; echo running > "$tmp/container"
check "CLOUDFLARE_DIRECT_ORIGIN becomes proxied A records" "0|[{\"type\":\"A\",\"content\":\"198.51.100.7\",\"proxied\":true}]" \
    "$(run_tunnel production switch_direct)|$(dns console.example.com)"
check "…but a name a wildcard answers for gets no A: back to the wildcard" "[]|intact" \
    "$(dns form.public.example.com)|$(wildcard_intact)"
seed_dns; write_env production "$PROD_TOKEN"

echo "gaterei 2026-11-05: names CNAMEd to the tunnel by hand, no snapshot, a hand-started replica"
# All three names already point at the tunnel (created by hand during the
# outage), there is no snapshot, and another connector of the SAME tunnel
# runs on the host. Cloudflare spreads requests over both connectors, so only
# HTTP answers can be checked; a failing smoke test must change no DNS.
seed_dns
api_set --arg t "$PROD_TID.cfargotunnel.com" '.records |= (map(select(.type == "TXT" or (.name | startswith("*.")))) + [
    {id: "h1", type: "CNAME", name: "console.example.com", content: $t, proxied: true, ttl: 1},
    {id: "h2", type: "CNAME", name: "api.example.com", content: $t, proxied: true, ttl: 1},
    {id: "h3", type: "CNAME", name: "form.public.example.com", content: $t, proxied: true, ttl: 1}])'
rm -f "$snap"
mkdir -p "$tmp/others"
echo emergency1 > "$tmp/others-ids"; echo unrelated1 >> "$tmp/others-ids"
printf '/gaterei-emergency-cloudflared\tcloudflare/cloudflared:2026.10.7 PATH=/usr/bin tunnel --no-autoupdate run --token %s' "$PROD_TOKEN" > "$tmp/others/emergency1"
printf '/cloudflared-gaterei-staging\tcloudflare/cloudflared:2026.10.7 TUNNEL_TOKEN=%s tunnel run' "$(token acct "$STG_TID")" > "$tmp/others/unrelated1"
reset_fakes; echo 404 > "$tmp/http/api.example.com"
check "smoke failure with pre-existing tunnel names: exit 1, no DNS write, no snapshot" "1|0|no" \
    "$(run_tunnel production switch_tunnel)|$(writes)|$([ -f "$snap" ] && echo yes || echo no)"
check "every name still on the tunnel" "$on_tunnel|$on_tunnel|$on_tunnel" \
    "$(dns console.example.com)|$(dns api.example.com)|$(dns form.public.example.com)"
check "the hand-started replica of this tunnel is named, the other tunnel's is not" "yes|no" \
    "$(said 'run on this host: gaterei-emergency-cloudflared —')|$(said 'cloudflared-gaterei-staging')"
check "no token is ever printed" "no" "$(grep -qF "$PROD_TOKEN" "$tmp/out" && echo yes || echo no)"
check "the replica is never touched" "no" "$(logged '(rm|stop|kill|restart) .*(emergency1|gaterei-emergency)')"
reset_fakes
check "with the smoke green, the switch is idempotent: success, no DNS write" "0|0|yes" \
    "$(run_tunnel production switch_tunnel)|$(writes)|$(said 'already on the tunnel')"
check "status lists the other replica" "yes" "$(run_tunnel production status > /dev/null; said 'gaterei-emergency-cloudflared')"
rm -f "$tmp/others-ids"; rm -rf "$tmp/others"
seed_dns

echo "concurrent runs"
reset_fakes
mkdir -p "$dd/cloudflared/state"
if command -v flock > /dev/null; then
    exec 8> "$dd/cloudflared/state/.lock"; flock -n 8
    check "a second tunnel command on the stack is refused" "1|yes|0" \
        "$(run_tunnel production switch_tunnel)|$(said 'Another')|$(writes)"
    exec 8>&-
fi

echo "trusted proxies hint"
reset_fakes
write_env production "$PROD_TOKEN" TRUSTED_PROXY_COUNT=2
run_tunnel production status > /dev/null
check "TRUSTED_PROXY_COUNT=2 without CIDRs is flagged" "yes" "$(said 'the backend sees ONE hop')"
write_env production "$PROD_TOKEN" TRUSTED_PROXY_COUNT=1
run_tunnel production status > /dev/null
check "TRUSTED_PROXY_COUNT=1: CIDRs recommended for both modes" "yes" "$(said 'fits the tunnel only')"
write_env production "$PROD_TOKEN" TRUSTED_PROXY_CIDRS=192.168.88.2/32
run_tunnel production status > /dev/null
check "CIDRs that miss the stack network are flagged" "yes" "$(said 'does not cover this stack')"
write_env production "$PROD_TOKEN" TRUSTED_PROXY_CIDRS=192.168.88.2/32,172.16.0.0/12
run_tunnel production status > /dev/null
check "CIDRs covering the stack network: no hint" "no|no" "$(said 'does not cover')|$(said 'ONE hop')"
write_env production "$PROD_TOKEN"
check "IPv4 containment helper" "yes|no|yes|no" \
    "$(tunnel_cidr_contains 172.20.0.0/24 172.20.0.5 && echo yes || echo no)|$(tunnel_cidr_contains 172.20.0.0/24 172.21.0.5 && echo yes || echo no)|$(tunnel_cidr_contains 10.1.2.3 10.1.2.3 && echo yes || echo no)|$(tunnel_cidr_contains 2001:db8::/32 172.20.0.5 && echo yes || echo no)"

echo "the shipped examples are valid routes"
for env in production staging; do
    cp "$repo/docker/cloudflared/$env.json.example" "$dd/cloudflared/$env.json"
    chmod 644 "$dd/cloudflared/$env.json"
    check "$env.json.example passes the routes checks" "0" \
        "$( (ENV=$env DOCKER_DIR="$dd"; tunnel_check_routes) > /dev/null 2>&1; echo $?)"
    check "$env.json.example ends with the 404 catch-all" "http_status:404" \
        "$(jq -r '.ingress[-1].service' "$dd/cloudflared/$env.json")"
done

echo "compose contract of the cloudflared service"
empty_env="$tmp/empty.env"; : > "$empty_env"
render() {
    (cd "$repo/docker" && APP_NAME=acme ENV=production MONGO_ROOT_PASSWORD=x REDIS_PASSWORD=x \
        STORAGE_ACCESS_KEY=x STORAGE_SECRET_KEY=x PDF_RENDERER_PASSWORD=x CLOUDFLARE_TUNNEL_TOKEN=tok \
        docker compose --env-file "$empty_env" -f docker-compose.infra.yml -f docker-compose.prod.yml "$@" config --format json 2> /dev/null)
}
off=$(render)
on=$(render --profile tunnel)
check "without the profile there is no cloudflared" "false" "$(jq '.services | has("cloudflared")' <<< "$off")"
cf() { jq -c ".services.cloudflared$1" <<< "$on"; }
check "image pinned, and the same pin as TUNNEL_IMAGE in orkestra.sh" "\"$TUNNEL_IMAGE\"" "$(cf .image)"
check "exact container name" '"acme-cloudflared-production"' "$(cf .container_name)"
check "read-only rootfs" "true" "$(cf .read_only)"
check "no-new-privileges" '["no-new-privileges:true"]' "$(cf .security_opt)"
check "all capabilities dropped" '["ALL"]' "$(cf .cap_drop)"
check "no published port" "null" "$(cf .ports)"
check "routes of THIS env, metrics on loopback only" \
    '["tunnel","--no-autoupdate","--config","/etc/cloudflared/production.json","--metrics","127.0.0.1:20241","run"]' "$(cf .command)"
check "token from the environment" '"tok"' "$(cf .environment.TUNNEL_TOKEN)"
check "routes directory mounted read-only" "$repo/docker/cloudflared|/etc/cloudflared|true" \
    "$(jq -r '.services.cloudflared.volumes[0] | "\(.source)|\(.target)|\(.read_only)"' <<< "$on")"
check "on the stack network with the services it routes to" '["default"]' "$(jq -c '.services.cloudflared.networks | keys' <<< "$on")"

# Staging runs its own full docker-compose.staging.yml (AIR hot reload), not
# the production file: the same service must be there, equal but for the
# ENV-derived name and routes file.
echo "compose contract of the cloudflared service on staging"
render_staging() {
    (cd "$repo/docker" && APP_NAME=acme ENV=staging MONGO_ROOT_PASSWORD=x REDIS_PASSWORD=x \
        STORAGE_ACCESS_KEY=x STORAGE_SECRET_KEY=x PDF_RENDERER_PASSWORD=x CLOUDFLARE_TUNNEL_TOKEN=tok \
        docker compose --env-file "$empty_env" -f docker-compose.infra.yml -f docker-compose.staging.yml "$@" config --format json 2> /dev/null)
}
stg_off=$(render_staging)
stg_on=$(render_staging --profile tunnel)
check "staging: without the profile there is no cloudflared" "false" "$(jq '.services | has("cloudflared")' <<< "$stg_off")"
check "staging: exact container name and its own routes file" \
    '"acme-cloudflared-staging"|["tunnel","--no-autoupdate","--config","/etc/cloudflared/staging.json","--metrics","127.0.0.1:20241","run"]' \
    "$(jq -c '.services.cloudflared.container_name' <<< "$stg_on")|$(jq -c '.services.cloudflared.command' <<< "$stg_on")"
check "staging: otherwise the same service as production" "same" \
    "$(diff <(jq -S '.services.cloudflared | del(.container_name, .command, .labels)' <<< "$on") \
        <(jq -S '.services.cloudflared | del(.container_name, .command, .labels)' <<< "$stg_on") > /dev/null && echo same || echo different)"

echo
echo "cloudflare-tunnel: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
