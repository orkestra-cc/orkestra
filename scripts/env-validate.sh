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
# disabled outright (both STORAGE_* keys empty); RUSTFS_ROOT_PASSWORD is only
# checked when set.
SECRET_VARS=(
    "COOKIE_SECRET"
    "OAUTH_TOKEN_ENCRYPTION_KEY"
    "ORKESTRA_KMS_MASTER_KEY"
    "MONGO_ROOT_PASSWORD"
    "REDIS_PASSWORD"
    "STORAGE_SECRET_KEY"
    "RUSTFS_ROOT_PASSWORD"
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
    local strict=0 value reason
    [[ "$env_name" == "staging" || "$env_name" == "production" ]] && strict=1
    for var in "${SECRET_VARS[@]}"; do
        value=$(env_value "$var")
        case "$var" in
            STORAGE_SECRET_KEY)
                if [ -z "$value" ] && [ -z "$(env_value STORAGE_ACCESS_KEY)" ]; then
                    print_info "STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY are empty — object storage disabled"
                    continue
                fi
                ;;
            RUSTFS_ROOT_PASSWORD)
                [ -z "$value" ] && continue
                ;;
        esac
        if secret_is_placeholder "$value"; then
            reason="is empty or a placeholder"
        elif secret_is_weak "$value"; then
            reason="is shorter than 16 characters"
        else
            print_success "$var is a real secret"
            continue
        fi
        if [ "$strict" -eq 1 ]; then
            print_error "$var $reason — generate one with: openssl rand -hex 16 (make init does this for a fresh checkout)"
            errors=$((errors + 1))
        else
            print_warning "$var $reason — tolerated in development, refused in staging/production"
            warnings=$((warnings + 1))
        fi
    done

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
                print_info "See docker/CLAUDE.md -> \"Client tier: the SPA and the client API must be same-site\","
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
        print_info "See docker/CLAUDE.md -> \"Client tier: the SPA and the client API must be same-site\","
        print_info "whose closing paragraph covers the operator tier."
        errors=$((errors + 1))
    fi

    echo ""

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
      ORKESTRA_KMS_MASTER_KEY, MONGO_ROOT_PASSWORD, REDIS_PASSWORD,
      STORAGE_SECRET_KEY, a set RUSTFS_ROOT_PASSWORD) is a real value: not a
      shipped placeholder, at least 16 characters. Error in staging and
      production, warning in development.
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
