#!/bin/bash

# ORKESTRA Environment Validator
# Validates environment files for required variables and security settings
#
# Usage:
#   ./scripts/env-validate.sh              # Validate all environments
#   ./scripts/env-validate.sh dev          # Validate development only
#   ./scripts/env-validate.sh staging      # Validate staging only
#   ./scripts/env-validate.sh prod         # Validate production only

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

print_info() { printf "${BLUE}ℹ${NC} %s\n" "$1"; }
print_success() { printf "${GREEN}✓${NC} %s\n" "$1"; }
print_error() { printf "${RED}✗${NC} %s\n" "$1"; }
print_warning() { printf "${YELLOW}⚠${NC} %s\n" "$1"; }

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

validate_file() {
    local file=$1
    local env_name=$2
    local errors=0
    local warnings=0

    print_info "Validating $env_name environment ($file)..."

    if [ ! -f "$file" ]; then
        print_error "File not found: $file"
        return 1
    fi

    # Check required variables
    for var in "${REQUIRED_VARS[@]}"; do
        if ! grep -q "^${var}=" "$file"; then
            print_error "Missing required variable: $var"
            ((errors++))
        elif grep -q "^${var}=$" "$file" || grep -q "^${var}=GENERATE" "$file" || grep -q "^${var}=your_" "$file"; then
            print_warning "Variable needs value: $var"
            ((warnings++))
        fi
    done

    # Check production-specific variables for staging/production
    if [[ "$env_name" == "staging" || "$env_name" == "production" ]]; then
        for var in "${PRODUCTION_VARS[@]}"; do
            if ! grep -q "^${var}=" "$file" || grep -q "^${var}=$" "$file"; then
                print_warning "Production variable missing or empty: $var"
                ((warnings++))
            fi
        done
    fi

    # Check for placeholder values in production
    if [[ "$env_name" == "production" ]]; then
        if grep -q "dev_\|_dev\|localhost\|changeme\|GENERATE" "$file"; then
            print_warning "Production file may contain development/placeholder values"
            ((warnings++))
        fi
    fi

    # Check cookie security for production-like environments
    if [[ "$env_name" == "staging" || "$env_name" == "production" ]]; then
        if grep -q "COOKIE_SECURE=false" "$file"; then
            print_error "COOKIE_SECURE should be true for $env_name"
            ((errors++))
        fi

        if grep -q "COOKIE_SAME_SITE=lax" "$file" && [[ "$env_name" == "production" ]]; then
            print_warning "COOKIE_SAME_SITE should be 'strict' for production"
            ((warnings++))
        fi
    fi

    # Check ENV value matches filename
    local expected_env=""
    case "$env_name" in
        development) expected_env="development" ;;
        staging) expected_env="staging" ;;
        production) expected_env="production" ;;
    esac

    if ! grep -q "^ENV=$expected_env" "$file"; then
        print_error "ENV variable should be '$expected_env' but found different value"
        ((errors++))
    fi

    echo ""
    if [ $errors -eq 0 ] && [ $warnings -eq 0 ]; then
        print_success "$env_name environment is valid (no issues)"
        return 0
    elif [ $errors -eq 0 ]; then
        print_warning "$env_name environment has $warnings warning(s)"
        return 0
    else
        print_error "$env_name environment has $errors error(s) and $warnings warning(s)"
        return 1
    fi
}

show_usage() {
    cat << EOF
${GREEN}ORKESTRA Environment Validator${NC}

${BLUE}Usage:${NC}
    $(basename "$0") [environment]

${BLUE}Arguments:${NC}
    dev, development    Validate development environment only
    staging             Validate staging environment only
    prod, production    Validate production environment only
    all                 Validate all environments (default)

${BLUE}Examples:${NC}
    $(basename "$0")            # Validate all environments
    $(basename "$0") dev        # Validate development only
    $(basename "$0") staging    # Validate staging only
    $(basename "$0") prod       # Validate production only

${BLUE}Checks performed:${NC}
    - Required variables are present and have values
    - Security settings are appropriate for the environment
    - No placeholder/development values in production
    - ENV variable matches the expected environment

EOF
}

main() {
    local env=${1:-all}
    local exit_code=0

    case $env in
        development|dev)
            validate_file "$DOCKER_DIR/.env.development" "development" || exit_code=1
            ;;
        staging)
            validate_file "$DOCKER_DIR/.env.staging" "staging" || exit_code=1
            ;;
        production|prod)
            validate_file "$DOCKER_DIR/.env.production" "production" || exit_code=1
            ;;
        all)
            echo "================================"
            echo "Validating all environment files"
            echo "================================"
            echo ""
            validate_file "$DOCKER_DIR/.env.development" "development" || exit_code=1
            echo ""
            validate_file "$DOCKER_DIR/.env.staging" "staging" || exit_code=1
            echo ""
            validate_file "$DOCKER_DIR/.env.production" "production" || exit_code=1
            ;;
        -h|--help|help)
            show_usage
            exit 0
            ;;
        *)
            print_error "Unknown environment: $env"
            show_usage
            exit 1
            ;;
    esac

    echo ""
    if [ $exit_code -eq 0 ]; then
        print_success "Validation complete - all checks passed!"
    else
        print_error "Validation complete - some checks failed"
    fi

    exit $exit_code
}

main "$@"
