#!/bin/bash

# Development Token Generator
# Generates JWT tokens for testing backend endpoints
# Only works in development and staging environments

set -e

# Configuration
API_URL="${ORKESTRA_API_URL:-http://localhost:3000}"
VALID_ROLES=("super_admin" "administrator" "developer" "manager" "operator" "guest")
# ADR-0003 PR-D D-10: dev tokens are stamped with an explicit JWT `aud`
# so the host muxes' RequireAudience gates accept them. Default operator
# matches the canonical service binding; pass --audience client to mint
# tokens that satisfy api.* surfaces.
VALID_AUDIENCES=("operator" "client")
DEFAULT_AUDIENCE="operator"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print usage
usage() {
    echo "Usage: $0 [ROLE] [OPTIONS]"
    echo ""
    echo "Generate JWT tokens for testing backend endpoints."
    echo "Only works in development and staging environments."
    echo ""
    echo "Arguments:"
    echo "  ROLE              Role for the token (default: interactive selection)"
    echo ""
    echo "Options:"
    echo "  -q, --quiet       Output only the token (for piping)"
    echo "  -c, --curl        Output a ready-to-use curl command"
    echo "  -e, --expiry      Token expiry duration (e.g., '15m', '1h', '24h')"
    echo "  -a, --audience    JWT audience: operator (default) or client (ADR-0003 PR-D)"
    echo "  -u, --url         API URL (default: $API_URL)"
    echo "  -h, --help        Show this help message"
    echo ""
    echo "Available roles:"
    for role in "${VALID_ROLES[@]}"; do
        echo "  - $role"
    done
    echo ""
    echo "Examples:"
    echo "  $0                                      # Interactive role selection"
    echo "  $0 administrator                        # Generate admin operator token"
    echo "  $0 manager --quiet                      # Token only (for pbcopy/clipboard)"
    echo "  $0 operator --curl                      # Output curl command"
    echo "  $0 admin --expiry 1h                    # Token with 1 hour expiry"
    echo "  $0 administrator --audience client      # Mint a client-aud token for api.*"
    echo "  $0 admin -a client -q                   # Quiet client-aud token"
    echo ""
    echo "Shorthand roles:"
    echo "  su    -> super_admin"
    echo "  admin -> administrator"
    echo "  dev   -> developer"
    echo "  mgr   -> manager"
    echo "  op    -> operator"
}

# Expand shorthand role names
expand_role() {
    local role="$1"
    case "$role" in
        su)    echo "super_admin" ;;
        admin) echo "administrator" ;;
        dev)   echo "developer" ;;
        mgr)   echo "manager" ;;
        op)    echo "operator" ;;
        *)     echo "$role" ;;
    esac
}

# Check if role is valid
is_valid_role() {
    local role="$1"
    for valid in "${VALID_ROLES[@]}"; do
        if [ "$role" = "$valid" ]; then
            return 0
        fi
    done
    return 1
}

# Check if audience is valid (ADR-0003 PR-D D-10).
is_valid_audience() {
    local audience="$1"
    for valid in "${VALID_AUDIENCES[@]}"; do
        if [ "$audience" = "$valid" ]; then
            return 0
        fi
    done
    return 1
}

# Interactive role selection
select_role() {
    echo -e "${BLUE}Select a role for the token:${NC}"
    echo ""
    select role in "${VALID_ROLES[@]}"; do
        if [ -n "$role" ]; then
            echo "$role"
            return 0
        fi
        echo -e "${RED}Invalid selection. Please try again.${NC}"
    done
}

# Parse arguments
ROLE=""
QUIET=false
CURL_OUTPUT=false
EXPIRY=""
AUDIENCE="$DEFAULT_AUDIENCE"

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            usage
            exit 0
            ;;
        -q|--quiet)
            QUIET=true
            shift
            ;;
        -c|--curl)
            CURL_OUTPUT=true
            shift
            ;;
        -e|--expiry)
            EXPIRY="$2"
            shift 2
            ;;
        -a|--audience)
            AUDIENCE="$2"
            shift 2
            ;;
        -u|--url)
            API_URL="$2"
            shift 2
            ;;
        -*)
            echo -e "${RED}Unknown option: $1${NC}"
            usage
            exit 1
            ;;
        *)
            if [ -z "$ROLE" ]; then
                ROLE=$(expand_role "$1")
            fi
            shift
            ;;
    esac
done

# Validate audience
if ! is_valid_audience "$AUDIENCE"; then
    echo -e "${RED}Error: Invalid audience '$AUDIENCE'${NC}" >&2
    echo "Valid audiences: ${VALID_AUDIENCES[*]}" >&2
    exit 1
fi

# Interactive role selection if no role provided
if [ -z "$ROLE" ]; then
    if [ "$QUIET" = true ]; then
        echo -e "${RED}Error: Role is required in quiet mode${NC}" >&2
        exit 1
    fi
    ROLE=$(select_role)
fi

# Validate role
if ! is_valid_role "$ROLE"; then
    echo -e "${RED}Error: Invalid role '$ROLE'${NC}" >&2
    echo "Valid roles: ${VALID_ROLES[*]}" >&2
    exit 1
fi

# Build request body
REQUEST_BODY="{\"role\": \"$ROLE\""
REQUEST_BODY="$REQUEST_BODY, \"audience\": \"$AUDIENCE\""
if [ -n "$EXPIRY" ]; then
    REQUEST_BODY="$REQUEST_BODY, \"expiry\": \"$EXPIRY\""
fi
REQUEST_BODY="$REQUEST_BODY}"

# Check if API is reachable
if ! $QUIET; then
    echo -e "${BLUE}Checking API availability...${NC}"
fi

if ! curl -s --fail "$API_URL/health" > /dev/null 2>&1; then
    echo -e "${RED}Error: Cannot reach API at $API_URL${NC}" >&2
    echo "Make sure the backend is running (./orkestra.sh)" >&2
    exit 1
fi

# Generate token
if ! $QUIET; then
    echo -e "${BLUE}Generating token for role: ${YELLOW}$ROLE${NC}"
fi

RESPONSE=$(curl -s -X POST "$API_URL/dev/token" \
    -H "Content-Type: application/json" \
    -d "$REQUEST_BODY")

# Check for errors
if echo "$RESPONSE" | grep -q '"error"'; then
    ERROR=$(echo "$RESPONSE" | grep -o '"detail":"[^"]*"' | sed 's/"detail":"//;s/"$//')
    if [ -z "$ERROR" ]; then
        ERROR=$(echo "$RESPONSE" | grep -o '"message":"[^"]*"' | sed 's/"message":"//;s/"$//')
    fi
    echo -e "${RED}Error: ${ERROR:-Unknown error}${NC}" >&2
    echo "$RESPONSE" >&2
    exit 1
fi

# Extract token from response
TOKEN=$(echo "$RESPONSE" | grep -o '"accessToken":"[^"]*"' | sed 's/"accessToken":"//;s/"$//')

if [ -z "$TOKEN" ]; then
    echo -e "${RED}Error: Failed to extract token from response${NC}" >&2
    echo "$RESPONSE" >&2
    exit 1
fi

# Output based on mode
if [ "$QUIET" = true ]; then
    echo "$TOKEN"
elif [ "$CURL_OUTPUT" = true ]; then
    echo -e "${GREEN}Curl command:${NC}"
    echo ""
    echo "curl -H 'Authorization: Bearer $TOKEN' $API_URL/v1/users"
else
    # Extract additional info
    EXPIRES_AT=$(echo "$RESPONSE" | grep -o '"expiresAt":"[^"]*"' | sed 's/"expiresAt":"//;s/"$//')
    EXPIRES_IN=$(echo "$RESPONSE" | grep -o '"expiresIn":[0-9]*' | sed 's/"expiresIn"://')
    EMAIL=$(echo "$RESPONSE" | grep -o '"email":"[^"]*"' | sed 's/"email":"//;s/"$//')
    AUD=$(echo "$RESPONSE" | grep -o '"audience":"[^"]*"' | sed 's/"audience":"//;s/"$//')

    echo ""
    echo -e "${GREEN}Token generated successfully!${NC}"
    echo ""
    echo -e "${YELLOW}Role:${NC}       $ROLE"
    echo -e "${YELLOW}Audience:${NC}   ${AUD:-$AUDIENCE}"
    echo -e "${YELLOW}Email:${NC}      $EMAIL"
    echo -e "${YELLOW}Expires:${NC}    $EXPIRES_AT (${EXPIRES_IN}s)"
    echo ""
    echo -e "${YELLOW}Token:${NC}"
    echo "$TOKEN"
    echo ""
    echo -e "${BLUE}Example usage:${NC}"
    echo "  curl -H 'Authorization: Bearer \$TOKEN' $API_URL/v1/users"
    echo ""
    echo -e "${BLUE}Copy token to clipboard:${NC}"
    echo "  $0 $ROLE --quiet | pbcopy    # macOS"
    echo "  $0 $ROLE --quiet | xclip     # Linux"
fi
