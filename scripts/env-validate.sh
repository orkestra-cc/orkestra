#!/bin/bash

# ORKESTRA Environment Validator
# Validates the .env file for required variables and security settings
#
# Usage:
#   ./scripts/env-validate.sh              # Validate .env file
#   ./scripts/env-validate.sh --help       # Show help

set -e

# Colors
RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[1;33m'
BLUE=$'\033[0;34m'
NC=$'\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
DOCKER_DIR="$PROJECT_ROOT/docker"
ENV_FILE="$DOCKER_DIR/.env"

# secret_is_placeholder / secret_is_weak live in env-file.sh so the setup
# wizard, this validator and the tests share one definition.
# shellcheck source=/dev/null
source "$SCRIPT_DIR/env-file.sh"

print_info() { printf "${BLUE}i${NC} %s\n" "$1"; }
print_success() { printf "${GREEN}v${NC} %s\n" "$1"; }
print_error() { printf "${RED}x${NC} %s\n" "$1"; }
print_warning() { printf "${YELLOW}!${NC} %s\n" "$1"; }

# Required variables for all environments
REQUIRED_VARS=(
    "ENV"
    "BACKEND_PORT"
    "FRONTEND_PORT"
    "MONGO_ROOT_USERNAME"
    "MONGO_ROOT_PASSWORD"
    "MONGO_DATABASE"
    "REDIS_PASSWORD"
    "COOKIE_SECRET"
    "JWT_PRIVATE_KEY_PATH"
    "JWT_PUBLIC_KEY_PATH"
    "OAUTH_TOKEN_ENCRYPTION_KEY"
)

# Production-only required variables
PRODUCTION_VARS=(
    "OAUTH_GOOGLE_CLIENT_ID"
    "OAUTH_GOOGLE_CLIENT_SECRET"
)

# Secrets that must be REAL values. A placeholder (see secret_is_placeholder in
# env-file.sh) or a value shorter than 16 characters is an error in staging and
# production and a warning in development. The bundled RustFS derives its root
# pair from STORAGE_ACCESS_KEY / STORAGE_SECRET_KEY and its S3 API is
# browser-facing behind the proxy, so a placeholder there is a public root
# password — that is the case this list exists for. Object storage may be
# disabled outright (both STORAGE_* keys empty); RUSTFS_ROOT_PASSWORD, QDRANT_API_KEY
# and RETRIEVAL_INFERENCE_API_KEY (opt-in retrieval overlay) are only
# checked when set.
SECRET_VARS=(
    "COOKIE_SECRET"
    "OAUTH_TOKEN_ENCRYPTION_KEY"
    "ORKESTRA_KMS_MASTER_KEY"
    "LLM_SECRET_ENCRYPTION_KEY"
    "MONGO_ROOT_PASSWORD"
    "REDIS_PASSWORD"
    "STORAGE_SECRET_KEY"
    "RUSTFS_ROOT_PASSWORD"
    "PDF_RENDERER_PASSWORD"
    "QDRANT_API_KEY"
    "RETRIEVAL_INFERENCE_API_KEY"
)

# --- Same-site host pairings (spec §8 follow-up #16) ---------------------
#
# A tier's SPA and the API it calls with `credentials: 'include'` must be
# same-site, or the browser drops the `SameSite=Lax` refresh cookie and the
# stack fails as an auth bug (login works, every rotation 401s) rather than
# as the config error it is.
#
# The rule is **same site**, not same host: `staging.orkestra.cc` and
# `staging-api.orkestra.cc` are one site, and comparing hosts would refuse
# every real deployment. The one place where site == host is `*.localhost`,
# which has no Public Suffix List entry — see site_of below.
#
# Checked in every ENV, not under a `development` gate: the same constraint
# is stated for staging and production in `.env.example` itself.

# env_value KEY — first active assignment of KEY in $ENV_FILE, quotes and
# whitespace stripped. The script never sources .env (values are untrusted
# shell), so this mirrors the ENV read above.
env_value() {
    grep -E "^$1=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d'=' -f2- | tr -d '"' | tr -d "'" | tr -d '[:space:]'
}

# env_raw_value KEY — first ACTIVE assignment of KEY in $ENV_FILE, surrounding
# whitespace and ONE pair of surrounding single or double quotes removed; inner
# quotes are kept (JSON values). Unlike env_get (env-file.sh) it never falls
# back to a commented stub, and unlike env_value it does not strip inner quotes.
env_raw_value() {
    local v
    v=$(grep -E "^$1=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d'=' -f2-)
    v="${v#"${v%%[![:space:]]*}"}"
    v="${v%"${v##*[![:space:]]}"}"
    case "$v" in
        \"*\") [ "${#v}" -ge 2 ] && v="${v:1:${#v}-2}" ;;
        \'*\') [ "${#v}" -ge 2 ] && v="${v:1:${#v}-2}" ;;
    esac
    printf '%s' "$v"
}

# inference_endpoints_error VALUE — prints "" when VALUE is a valid
# RETRIEVAL_INFERENCE_ENDPOINTS list, otherwise one short reason. Mirrors the
# element rules of backend/internal/core/llm/installation (parseElement), which
# DROPS an invalid entry with only a WARN — so a deploy gate must refuse it.
# Duplicate profile IDs need the Go canonical-JSON hash and are not re-derived.
# Needs jq; the caller checks `command -v jq` first.
inference_endpoints_error() {
    local reason
    reason=$(printf '%s' "$1" | jq -r '
        def isstr: type == "string";
        def optstr($k): (.[$k] == null) or (.[$k] | isstr);
        def optbool($k): (.[$k] == null) or (.[$k] | type == "boolean");
        def bad($i; $f): "entry \($i): missing or invalid \($f)";
        def entry_error($i):
          if type != "object" then "entry \($i): not an object"
          else
            . as $e
            | ["name","url","kind","boundary","model","revision","pooling","normalize","truncate","queryPrefix","documentPrefix","apiKeyEnv"] as $known
            | (($e | keys_unsorted) - $known | first // null) as $unk
            | ($e.kind // "") as $kind
            | first(
                (if $unk != null then "entry \($i): unknown field \($unk)" else empty end),
                (["name","url","kind","boundary","model","revision","pooling","queryPrefix","documentPrefix","apiKeyEnv"][]
                  | select(. as $k | $e | optstr($k) | not) | bad($i; .)),
                (["normalize","truncate"][] | select(. as $k | $e | optbool($k) | not) | bad($i; .)),
                (if ($e.name // "" | test("\\A[a-z][a-z0-9_-]{0,31}\\z")) | not then bad($i; "name") else empty end),
                (if ($e.url // "" | test("\\Ahttps?://(\\[[0-9A-Fa-f:.]+\\]|[^\\s/?#@:\\[\\]%]+)(:[0-9]*)?(/([^?#%]|%[0-9A-Fa-f]{2})*)?\\z"; "i")) | not then bad($i; "url") else empty end),
                (if ($kind | IN("embed","rerank","chat")) | not then bad($i; "kind") else empty end),
                (if (($e.boundary // "") | IN("local","hosted")) | not then bad($i; "boundary") else empty end),
                (if ($e.model // "" | test("\\A[A-Za-z0-9/][A-Za-z0-9._/:-]{0,199}\\z")) | not then bad($i; "model") else empty end),
                (if ($e.revision // "" | test("\\A[A-Za-z0-9][A-Za-z0-9._/-]{0,127}\\z")) | not then bad($i; "revision") else empty end),
                (if $kind == "embed" then
                    (if (($e.pooling // "") | IN("cls","mean","last_token")) | not then bad($i; "pooling") else empty end)
                 elif ($e.pooling // "") != "" then bad($i; "pooling") else empty end),
                (if $kind != "embed" and ($e.normalize // false) then bad($i; "normalize") else empty end),
                (if $kind == "chat" and ($e.truncate // false) then bad($i; "truncate") else empty end),
                (["queryPrefix","documentPrefix"][] as $k
                  | if $kind != "embed" then (if ($e[$k] // "") != "" then bad($i; $k) else empty end)
                    elif (($e[$k] // "") | utf8bytelength) > 256 then bad($i; $k) else empty end),
                (if $e.apiKeyEnv != null and (($e.apiKeyEnv) | test("\\ARETRIEVAL_INFERENCE_[A-Z0-9_]{1,64}\\z") | not) then bad($i; "apiKeyEnv") else empty end)
              ) // empty
          end;
        if type != "array" then "not a JSON array"
        elif length == 0 then "empty list"
        else
          first(
            (to_entries[] | .key as $i | .value | entry_error($i)),
            ([.[].name] | group_by(.)[] | select(length > 1) | "duplicate name \(.[0])")
          ) // ""
        end' 2>/dev/null) || reason="not a JSON array"
    printf '%s' "$reason"
}

# inference_endpoints_query VALUE FILTER — jq over a list already validated.
inference_endpoints_query() {
    printf '%s' "$1" | jq -r "$2" 2>/dev/null
}

# host_only VALUE — hostname of a URL or a bare host[:port], with scheme,
# userinfo, path and **port** removed. The port stripping is load-bearing:
# .env.example and orkestra.sh's wizard write these hosts bare while the
# compose defaults write them ported, and the host mux accepts both.
host_only() {
    printf '%s' "$1" | sed -E 's#^[A-Za-z][A-Za-z0-9+.-]*://##; s#^[^/@]*@##; s#/.*$##; s#:[0-9]+$##'
}

# site_of HOST — the browser's notion of the "site" HOST belongs to,
# lowercased. Two hosts are same-site when this returns the same value.
#
#   * `localhost` and `*.localhost` -> the WHOLE host. `localhost` is not in
#     the Public Suffix List, so a browser falls back to the full hostname:
#     `client.localhost` and `api.localhost` are different sites, which is
#     the exact trap this guard exists for.
#   * an IP literal, or a single-label host -> itself.
#   * anything else -> its last two labels. That is a deliberate eTLD+1
#     APPROXIMATION: a shell script has no Public Suffix List, so a
#     multi-label suffix like `co.uk` under-reports (`a.co.uk` and `b.co.uk`
#     look like one site). It can therefore miss a genuine cross-site
#     pairing; it never invents one, which is the safe direction for a check
#     that hard-stops a deploy.
site_of() {
    local host
    host=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
    case "$host" in
        localhost | *.localhost) printf '%s' "$host"; return 0 ;;
        *:* | \[*) printf '%s' "$host"; return 0 ;;   # IPv6 literal
    esac
    case "$host" in
        *[!0-9.]*) ;;                                 # not an IPv4 literal
        *) printf '%s' "$host"; return 0 ;;
    esac
    case "$host" in
        *.*) ;;
        *) printf '%s' "$host"; return 0 ;;            # single label
    esac
    printf '%s' "$host" | awk -F. '{ printf "%s.%s", $(NF-1), $NF }'
}

# check_same_site LABEL KEY... — 0 when every *set* key resolves to the same
# SITE (an unset or empty key is not checked at all), 1 on a mismatch.
# Prints the offending keys with the hostnames they resolved to, and the site
# too wherever the two differ; the caller adds the remediation, which differs
# per tier.
check_same_site() {
    local label=$1
    shift
    local key value host site first_site="" mismatch=0 shown=""
    for key in "$@"; do
        value=$(env_value "$key")
        if [ -n "$value" ]; then
            host=$(host_only "$value")
            if [ -n "$host" ]; then
                site=$(site_of "$host")
                if [ "$site" = "$host" ]; then
                    shown="${shown}${shown:+, }${key}=${host}"
                else
                    shown="${shown}${shown:+, }${key}=${host} (site ${site})"
                fi
                if [ -z "$first_site" ]; then
                    first_site=$site
                elif [ "$site" != "$first_site" ]; then
                    mismatch=1
                fi
            fi
        fi
    done

    if [ -z "$first_site" ]; then
        print_info "$label: no host keys set — skipped"
        return 0
    fi
    if [ "$mismatch" -eq 0 ]; then
        print_success "$label is same-site ($first_site)"
        return 0
    fi
    print_error "$label is cross-site: $shown"
    return 1
}

validate_env_file() {
    local errors=0
    local warnings=0

    # Check file exists
    if [ ! -f "$ENV_FILE" ]; then
        print_error "Environment file not found: $ENV_FILE"
        print_info "Create one by copying from .env.example:"
        print_info "  cp $DOCKER_DIR/.env.example $ENV_FILE"
        return 1
    fi

    # Extract ENV value from file
    local env_name
    env_name=$(grep -E "^ENV=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d'=' -f2 | tr -d '"' | tr -d "'" | tr -d '[:space:]')

    if [ -z "$env_name" ]; then
        print_error "ENV variable not found in $ENV_FILE"
        print_info "Add ENV=development|staging|production to the file"
        return 1
    fi

    # Validate ENV value
    case "$env_name" in
        development|staging|production)
            print_info "Validating .env file (ENV=$env_name)..."
            ;;
        *)
            print_error "Invalid ENV value: '$env_name'"
            print_info "Valid values: development, staging, production"
            return 1
            ;;
    esac

    echo ""

    # Check required variables
    for var in "${REQUIRED_VARS[@]}"; do
        if ! grep -q "^${var}=" "$ENV_FILE"; then
            print_error "Missing required variable: $var"
            errors=$((errors + 1))
        elif grep -q "^${var}=$" "$ENV_FILE" || grep -q "^${var}=GENERATE" "$ENV_FILE" || grep -q "^${var}=your_" "$ENV_FILE"; then
            print_warning "Variable needs value: $var"
            warnings=$((warnings + 1))
        else
            print_success "$var is set"
        fi
    done

    echo ""

    # Secret hygiene — see SECRET_VARS above.
    print_info "Checking secret values..."
    local strict=0 value reason hint
    [[ "$env_name" == "staging" || "$env_name" == "production" ]] && strict=1
    for var in "${SECRET_VARS[@]}"; do
        value=$(env_value "$var")
        case "$var" in
            STORAGE_SECRET_KEY)
                if [ -z "$value" ] && [ -z "$(env_value STORAGE_ACCESS_KEY)" ]; then
                    print_info "STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY are empty — object storage disabled"
                    # The bundled rustfs container still starts with the infra
                    # stack, and docker-compose.infra.yml derives its root from
                    # the STORAGE_* pair — with both empty it refuses to render
                    # unless RUSTFS_ROOT_* provide a root of their own. Every
                    # environment: the stack would not come up at all.
                    if [ -z "$(env_value RUSTFS_ROOT_USER)" ] || [ -z "$(env_value RUSTFS_ROOT_PASSWORD)" ]; then
                        print_error "object storage is disabled but the bundled rustfs container still starts — set RUSTFS_ROOT_USER and RUSTFS_ROOT_PASSWORD (openssl rand -hex 16) or docker-compose.infra.yml will not render"
                        errors=$((errors + 1))
                    fi
                    continue
                fi
                ;;
            RUSTFS_ROOT_PASSWORD | QDRANT_API_KEY | RETRIEVAL_INFERENCE_API_KEY)
                [ -z "$value" ] && continue
                ;;
        esac
        # The hint depends on the key, not on the failure: the llm vault
        # takes exactly 64 hex chars (32 bytes), so every LLM branch —
        # missing, placeholder or malformed — must recommend -hex 32; a
        # -hex 16 value would only fail the 64-hex check on the next run.
        hint="openssl rand -hex 16"
        case "$var" in
            LLM_SECRET_ENCRYPTION_KEY | QDRANT_API_KEY | RETRIEVAL_INFERENCE_API_KEY) hint="openssl rand -hex 32" ;;
        esac
        if secret_is_placeholder "$value"; then
            reason="is empty or a placeholder"
        elif [ "$var" = "LLM_SECRET_ENCRYPTION_KEY" ] && ! [[ "$value" =~ ^[0-9a-fA-F]{64}$ ]]; then
            # The backend (llm vault) accepts exactly 64 hex chars (32 bytes);
            # anything else is logged as a WARN and silently disables secret
            # writes, so this is the only deploy-time gate.
            reason="must be 64 hex characters"
        elif secret_is_weak "$value"; then
            reason="is shorter than 16 characters"
        else
            print_success "$var is a real secret"
            continue
        fi
        if [ "$strict" -eq 1 ]; then
            print_error "$var $reason — generate one with: $hint (make init does this for a fresh checkout)"
            errors=$((errors + 1))
        else
            print_warning "$var $reason — tolerated in development, refused in staging/production"
            warnings=$((warnings + 1))
        fi
    done

    echo ""

    # Retrieval overlay (ADR-0023): Qdrant + installation inference endpoints.
    # Both keys are optional (retrieval is opt-in) but checked when set, and the
    # backend only WARNs on a bad endpoint list, so the deploy gate is here.
    print_info "Checking retrieval settings..."
    local q_url q_key i_key i_eps q_prefix ep_reason ep_var ep_idx
    q_url=$(env_value QDRANT_URL)
    q_key=$(env_value QDRANT_API_KEY)
    i_key=$(env_value RETRIEVAL_INFERENCE_API_KEY)
    i_eps=$(env_raw_value RETRIEVAL_INFERENCE_ENDPOINTS)
    q_prefix=$(env_value QDRANT_COLLECTION_PREFIX)
    retrieval_problem() {
        if [ "$strict" -eq 1 ]; then
            print_error "$1"
            errors=$((errors + 1))
        else
            print_warning "$1 — tolerated in development, refused in staging/production"
            warnings=$((warnings + 1))
        fi
    }
    if [ -z "$q_key" ] && [ -z "$i_key" ] && [ -z "$q_url" ] && [ -z "$i_eps" ]; then
        print_info "Retrieval not configured (QDRANT_API_KEY and RETRIEVAL_INFERENCE_API_KEY empty) — the retrieval overlay will refuse to start"
    fi
    if { [ -z "$q_key" ] && [ -n "$i_key" ]; } || { [ -n "$q_key" ] && [ -z "$i_key" ]; }; then
        print_warning "the retrieval overlay needs both QDRANT_API_KEY and RETRIEVAL_INFERENCE_API_KEY"
        warnings=$((warnings + 1))
    fi
    if [ -n "$q_url" ] && [ -z "$q_key" ]; then
        retrieval_problem "QDRANT_URL is set but QDRANT_API_KEY is empty — Qdrant must require an API key (ADR-0023 D3)"
    fi
    if [ -n "$q_prefix" ] && ! [[ "$q_prefix" =~ ^[a-z][a-z0-9-]{0,31}$ ]]; then
        print_warning "QDRANT_COLLECTION_PREFIX '$q_prefix' does not match ^[a-z][a-z0-9-]{0,31}\$ — the backend falls back to 'orkestra'"
        warnings=$((warnings + 1))
    fi
    if [ -n "$i_eps" ]; then
        if ! command -v jq > /dev/null 2>&1; then
            print_warning "jq not found — RETRIEVAL_INFERENCE_ENDPOINTS not validated"
            warnings=$((warnings + 1))
        else
            ep_reason=$(inference_endpoints_error "$i_eps")
            if [ -n "$ep_reason" ]; then
                retrieval_problem "RETRIEVAL_INFERENCE_ENDPOINTS is not a valid endpoint list: $ep_reason"
            else
                if [ -n "$(inference_endpoints_query "$i_eps" '.[] | select(.boundary == "hosted" and (.url | ascii_downcase | startswith("http://"))) | .name')" ]; then
                    retrieval_problem "RETRIEVAL_INFERENCE_ENDPOINTS has a hosted endpoint on http:// — the backend drops it in a production-like environment"
                fi
                # apiKeyEnv names a variable that must be non-empty AND reach the backend.
                while IFS=$'\t' read -r ep_idx ep_var; do
                    [ -z "$ep_var" ] && continue
                    if [ -z "$(env_value "$ep_var")" ]; then
                        retrieval_problem "RETRIEVAL_INFERENCE_ENDPOINTS entry $ep_idx: $ep_var is empty — set it in .env and pass it to the backend container (add $ep_var: \${$ep_var:-} to the app compose file)"
                    fi
                done < <(inference_endpoints_query "$i_eps" 'to_entries[] | select(.value.apiKeyEnv != null) | "\(.key)\t\(.value.apiKeyEnv)"')
                if [ -z "$i_key" ] && [ -n "$(inference_endpoints_query "$i_eps" '.[] | select(.apiKeyEnv == null) | .name')" ]; then
                    retrieval_problem "RETRIEVAL_INFERENCE_API_KEY is empty and an endpoint names no apiKeyEnv"
                fi
            fi
        fi
    fi

    echo ""

    # Same-site pairings — see site_of / check_same_site above (spec §8 #16).
    print_info "Checking same-site host pairings..."
    # An RFC 2606 `.invalid` client API host is how a deployment says it has
    # no client tier at all (staging does exactly this). There is no pairing
    # to check then, and the unresolvable host is the point.
    local client_api_host
    client_api_host=$(site_of "$(host_only "$(env_value CLIENT_API_HOST)")")
    case "$client_api_host" in
        *.invalid | invalid)
            print_info "Client tier disabled (CLIENT_API_HOST is a .invalid host) — pairing not checked"
            ;;
        *)
            if ! check_same_site "Client tier" CLIENT_API_HOST CLIENT_API_URL CLIENT_FRONTEND_URL; then
                print_info "The client SPA and the client API must be same-site — the same registrable"
                print_info "domain, not the same host: every client call carries credentials:'include'"
                print_info "and the refresh cookie is SameSite=Lax, so a cross-SITE pairing loses it (in"
                print_info "development the unmatched Host also falls through to the operator mux, which"
                print_info "serves no /v1/auth/client/* route, and answers 404). *.localhost is the"
                print_info "exception: it has no Public Suffix List entry, so client.localhost and"
                print_info "api.localhost are different sites and the dev hostnames must be identical."
                print_info "Migrate the three keys together — the dev values are:"
                print_info "  CLIENT_API_HOST=client.localhost"
                print_info "  CLIENT_API_URL=http://client.localhost:3000"
                print_info "  CLIENT_FRONTEND_URL=http://client.localhost:8081"
                print_info "See docker/AGENTS.md -> \"Client tier: the SPA and the client API must be same-site\","
                print_info "under \"Upgrading an existing dev checkout\"."
                errors=$((errors + 1))
            fi
            ;;
    esac
    if ! check_same_site "Operator tier" VITE_API_URL FRONTEND_URL; then
        print_info "The operator console is bound by the same rule and has no OPERATOR_API_HOST, so"
        print_info "the pairing is the whole contract: the console's own origin (FRONTEND_URL) and"
        print_info "the API its SPA calls (VITE_API_URL) must be one site — the same registrable"
        print_info "domain in staging/production, the same hostname on *.localhost. The shipped"
        print_info "development values are:"
        print_info "  FRONTEND_URL=http://localhost:8080"
        print_info "  VITE_API_URL=http://localhost:3000"
        print_info "See docker/AGENTS.md -> \"Client tier: the SPA and the client API must be same-site\","
        print_info "whose closing paragraph covers the operator tier."
        errors=$((errors + 1))
    fi

    echo ""

    # --- RustFS S3 API reachability ---------------------------------------
    # STORAGE_PUBLIC_ENDPOINT is the host the BROWSER PUTs presigned uploads
    # to, so the RustFS S3 port must be reachable by the reverse proxy in
    # front of it. docker-compose.infra.yml publishes that port on
    # ${HOST_BIND_ADDRESS:-127.0.0.1}: unset or loopback and the S3 API
    # answers on the docker host only. Nothing else in the stack shows it —
    # the app ports in docker-compose.dev.yml default to 0.0.0.0 — so the
    # deployment looks healthy while every presigned upload dies at the proxy
    # with the proxy's own 503 page (never an S3 error). Worse, the value is
    # read when the container is CREATED, so an .env that predates the
    # variable keeps working until rustfs is next recreated, for any reason.
    local storage_public bind_addr
    storage_public=$(env_value STORAGE_PUBLIC_ENDPOINT)
    bind_addr=$(env_value HOST_BIND_ADDRESS)
    if [ -n "$storage_public" ]; then
        print_info "Checking object-storage reachability..."
        case "$bind_addr" in
            "" | 127.0.0.1 | localhost | ::1 | "[::1]")
                print_warning "HOST_BIND_ADDRESS is ${bind_addr:-unset} while STORAGE_PUBLIC_ENDPOINT is set — presigned uploads will fail"
                print_info "The browser PUTs presigned uploads to $storage_public, which a reverse proxy"
                print_info "forwards to the RustFS S3 port. docker-compose.infra.yml publishes that port"
                print_info "on \${HOST_BIND_ADDRESS:-127.0.0.1}, so a loopback value accepts the proxy's"
                print_info "connection only when the proxy runs on this host. Set HOST_BIND_ADDRESS to the"
                print_info "address the proxy reaches (0.0.0.0, or a specific private/VPN IP) — .env.example"
                print_info "ships 0.0.0.0. Symptom when wrong: HTTP 503 from the proxy on upload, with the"
                print_info "rest of the stack healthy."
                warnings=$((warnings + 1))
                ;;
            *)
                print_success "RustFS S3 API is published on $bind_addr for the reverse proxy"
                ;;
        esac
        echo ""
    fi

    # Check production-specific variables for staging/production
    if [[ "$env_name" == "staging" || "$env_name" == "production" ]]; then
        print_info "Checking production-specific variables..."
        for var in "${PRODUCTION_VARS[@]}"; do
            if ! grep -q "^${var}=" "$ENV_FILE" || grep -q "^${var}=$" "$ENV_FILE"; then
                print_warning "Production variable missing or empty: $var"
                warnings=$((warnings + 1))
            else
                print_success "$var is set"
            fi
        done

        # STORAGE_PUBLIC_ENDPOINT is the host browsers — and, for public
        # cover images, anonymous visitors — fetch presigned URLs from. Plain
        # HTTP there is a cleartext signed URL and mixed content on every
        # HTTPS site that embeds the image. Unset is fine (single-endpoint or
        # managed S3); set means https.
        pub_ep=$(env_value STORAGE_PUBLIC_ENDPOINT)
        if [ -n "$pub_ep" ] && [[ "$pub_ep" != https://* ]]; then
            print_error "STORAGE_PUBLIC_ENDPOINT must be an https:// URL in $env_name (got: $pub_ep)"
            errors=$((errors + 1))
        fi
        echo ""
    fi

    # Check for placeholder values in production
    if [[ "$env_name" == "production" ]]; then
        print_info "Checking for development/placeholder values..."
        if grep -qE "dev_|_dev|localhost|changeme|GENERATE" "$ENV_FILE"; then
            print_warning "Production file may contain development/placeholder values"
            warnings=$((warnings + 1))
        else
            print_success "No obvious placeholder values found"
        fi
        echo ""
    fi

    # Check cookie security for production-like environments
    if [[ "$env_name" == "staging" || "$env_name" == "production" ]]; then
        print_info "Checking security settings..."

        if grep -q "COOKIE_SECURE=false" "$ENV_FILE"; then
            print_error "COOKIE_SECURE should be true for $env_name"
            errors=$((errors + 1))
        else
            print_success "COOKIE_SECURE is properly configured"
        fi

        if grep -q "COOKIE_SAME_SITE=lax" "$ENV_FILE" && [[ "$env_name" == "production" ]]; then
            print_warning "COOKIE_SAME_SITE should be 'strict' for production"
            warnings=$((warnings + 1))
        else
            print_success "COOKIE_SAME_SITE is properly configured"
        fi

        # A port Docker publishes on 0.0.0.0 bypasses the host firewall's INPUT
        # chain, so in production the bind addresses are the exposure control.
        if [[ "$env_name" == "production" ]]; then
            for var in HOST_BIND_ADDRESS INFRA_BIND_ADDRESS; do
                if grep -q "^${var}=0.0.0.0" "$ENV_FILE"; then
                    print_warning "$var=0.0.0.0 publishes on every interface — use 127.0.0.1 or the private IP your reverse proxy reaches"
                    warnings=$((warnings + 1))
                else
                    print_success "$var is not wide open"
                fi
            done
        fi
        echo ""
    fi

    # Summary
    echo "================================"
    if [ $errors -eq 0 ] && [ $warnings -eq 0 ]; then
        print_success "Validation passed - no issues found"
        return 0
    elif [ $errors -eq 0 ]; then
        print_warning "Validation passed with $warnings warning(s)"
        return 0
    else
        print_error "Validation failed: $errors error(s), $warnings warning(s)"
        return 1
    fi
}

show_usage() {
    cat << EOF
${GREEN}ORKESTRA Environment Validator${NC}

${BLUE}Usage:${NC}
    $(basename "$0")            # Validate .env file
    $(basename "$0") --help     # Show this help

${BLUE}Description:${NC}
    Validates the docker/.env file for required variables and security settings.
    The ENV variable inside the file determines which validation rules apply:

    - development: Basic checks only
    - staging: Security checks + production variables warning
    - production: Strict security checks + no placeholder values

${BLUE}Environment File:${NC}
    $ENV_FILE

${BLUE}Checks performed:${NC}
    - Required variables are present and have values
    - ENV value is valid (development|staging|production)
    - Each tier's SPA and API are same-site — same registrable domain, or the
      same hostname under *.localhost (scheme and port ignored):
      CLIENT_API_HOST / CLIENT_API_URL / CLIENT_FRONTEND_URL, and
      VITE_API_URL / FRONTEND_URL. A .invalid CLIENT_API_HOST means the
      client tier is disabled and is not checked.
    - Security settings appropriate for the environment
    - Every secret (COOKIE_SECRET, OAUTH_TOKEN_ENCRYPTION_KEY,
      ORKESTRA_KMS_MASTER_KEY, LLM_SECRET_ENCRYPTION_KEY, MONGO_ROOT_PASSWORD,
      REDIS_PASSWORD, STORAGE_SECRET_KEY, a set RUSTFS_ROOT_PASSWORD) is a
      real value: not a shipped placeholder, at least 16 characters.
      LLM_SECRET_ENCRYPTION_KEY must also be exactly 64 hex characters
      (the llm module ignores anything else). Error in staging and
      production, warning in development. Object storage disabled (both
      STORAGE_* keys empty) needs RUSTFS_ROOT_* for the bundled container.
      QDRANT_API_KEY and RETRIEVAL_INFERENCE_API_KEY are checked when set.
    - Retrieval (opt-in overlay): QDRANT_URL needs QDRANT_API_KEY; both keys
      together; a non-empty QDRANT_COLLECTION_PREFIX matches
      ^[a-z][a-z0-9-]{0,31}\$ (warning); RETRIEVAL_INFERENCE_ENDPOINTS is a JSON
      list the backend accepts (needs jq; checks the llm installation element
      rules, a hosted endpoint on http://, and that an apiKeyEnv variable is
      set — it must also be passed to the backend container). Duplicate
      profile IDs are not checked here; the backend WARNs and drops them.
    - No placeholder/development values in production

${BLUE}Switching Environments:${NC}
    Edit the ENV= line in $ENV_FILE:
        ENV=development   # For local development
        ENV=staging       # For staging deployment
        ENV=production    # For production deployment

EOF
}

main() {
    case "${1:-}" in
        -h|--help|help)
            show_usage
            exit 0
            ;;
        "")
            validate_env_file
            ;;
        *)
            print_error "Unknown option: $1"
            show_usage
            exit 1
            ;;
    esac
}

main "$@"
