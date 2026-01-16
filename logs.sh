#!/bin/bash

# Orkestra Docker Service Logs Viewer
# View logs for specific Docker services across all compose files

set -euo pipefail

# Color codes for better output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Script directory (at project root)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"
DOCKER_DIR="$PROJECT_ROOT/docker"

echo -e "${BLUE}Orkestra Service Logs Viewer${NC}"
echo "=================================="

# Function to display usage
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -h, --help          Show this help message"
    echo "  -f, --follow        Follow log output (tail -f)"
    echo "  -n, --lines NUM     Number of lines to show (default: 100)"
    echo "  -t, --timestamps    Show timestamps"
    echo ""
    echo "Examples:"
    echo "  $0                  # Interactive service selection"
    echo "  $0 -f               # Follow logs with interactive selection"
    echo "  $0 -n 50            # Show last 50 lines"
    echo "  $0 -f -t            # Follow logs with timestamps"
}

# Default options
FOLLOW_LOGS=true
LINE_COUNT=100
SHOW_TIMESTAMPS=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_usage
            exit 0
            ;;
        -f|--follow)
            FOLLOW_LOGS=true
            shift
            ;;
        -n|--lines)
            LINE_COUNT="$2"
            shift 2
            ;;
        -t|--timestamps)
            SHOW_TIMESTAMPS=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            show_usage
            exit 1
            ;;
    esac
done

# Function to get available services from compose files
get_services() {
    local compose_file="$1"
    if [[ -f "$compose_file" ]]; then
        docker compose -f "$compose_file" config --services 2>/dev/null || true
    fi
}

# Function to check if service is running
is_service_running() {
    local compose_file="$1"
    local service="$2"
    local status
    status=$(docker compose -f "$compose_file" ps -q "$service" 2>/dev/null | wc -l)
    [[ "$status" -gt 0 ]]
}

# Get services from all compose files
echo -e "${CYAN}Scanning for available services...${NC}"
echo ""

# Infrastructure services
INFRA_SERVICES=()
if [[ -f "$DOCKER_DIR/docker-compose.infra.yml" ]]; then
    mapfile -t INFRA_SERVICES < <(get_services "$DOCKER_DIR/docker-compose.infra.yml")
fi

# Development services
DEV_SERVICES=()
if [[ -f "$DOCKER_DIR/docker-compose.dev.yml" ]]; then
    mapfile -t DEV_SERVICES < <(get_services "$DOCKER_DIR/docker-compose.dev.yml")
fi

# Production services
PROD_SERVICES=()
if [[ -f "$DOCKER_DIR/docker-compose.prod.yml" ]]; then
    mapfile -t PROD_SERVICES < <(get_services "$DOCKER_DIR/docker-compose.prod.yml")
fi

# Build complete service list with their source files
declare -A SERVICE_COMPOSE_FILE
declare -a ALL_SERVICES

# Add infrastructure services
for service in "${INFRA_SERVICES[@]}"; do
    if [[ -n "$service" ]]; then
        ALL_SERVICES+=("$service")
        SERVICE_COMPOSE_FILE["$service"]="docker-compose.infra.yml"
    fi
done

# Add development services
for service in "${DEV_SERVICES[@]}"; do
    if [[ -n "$service" ]]; then
        ALL_SERVICES+=("$service")
        SERVICE_COMPOSE_FILE["$service"]="docker-compose.dev.yml"
    fi
done

# Add production services
for service in "${PROD_SERVICES[@]}"; do
    if [[ -n "$service" ]]; then
        ALL_SERVICES+=("$service")
        SERVICE_COMPOSE_FILE["$service"]="docker-compose.prod.yml"
    fi
done

# Remove duplicates while preserving order
declare -A SEEN
declare -a UNIQUE_SERVICES
for service in "${ALL_SERVICES[@]}"; do
    if [[ -z "${SEEN[$service]:-}" ]]; then
        UNIQUE_SERVICES+=("$service")
        SEEN["$service"]=1
    fi
done

# Check if any services were found
if [[ ${#UNIQUE_SERVICES[@]} -eq 0 ]]; then
    echo -e "${RED}No services found in compose files.${NC}"
    echo "Make sure you're in the docker directory and compose files exist."
    exit 1
fi

# Display available services with their status
echo -e "${GREEN}Available services:${NC}"
echo ""

for i in "${!UNIQUE_SERVICES[@]}"; do
    service="${UNIQUE_SERVICES[$i]}"
    compose_file="${SERVICE_COMPOSE_FILE[$service]}"

    # Check if service is running
    if is_service_running "$DOCKER_DIR/$compose_file" "$service"; then
        status="${GREEN}●${NC} Running"
    else
        status="${RED}●${NC} Stopped"
    fi

    printf "%2d) ${YELLOW}%-15s${NC} [%s] - %s\n" $((i+1)) "$service" "$compose_file" "$status"
done

echo ""
echo -e "${CYAN}Select a service to view logs (1-${#UNIQUE_SERVICES[@]}) or 'q' to quit:${NC}"
read -r choice

# Validate input
if [[ "$choice" == "q" || "$choice" == "Q" ]]; then
    echo "Goodbye!"
    exit 0
fi

if ! [[ "$choice" =~ ^[0-9]+$ ]] || [[ "$choice" -lt 1 ]] || [[ "$choice" -gt ${#UNIQUE_SERVICES[@]} ]]; then
    echo -e "${RED}Invalid selection. Please choose a number between 1 and ${#UNIQUE_SERVICES[@]}.${NC}"
    exit 1
fi

# Get selected service
SELECTED_SERVICE="${UNIQUE_SERVICES[$((choice-1))]}"
COMPOSE_FILE="${SERVICE_COMPOSE_FILE[$SELECTED_SERVICE]}"

echo ""
echo -e "${GREEN}Selected service:${NC} ${YELLOW}$SELECTED_SERVICE${NC}"
echo -e "${GREEN}Compose file:${NC} $COMPOSE_FILE"

# Check if service is running
if ! is_service_running "$DOCKER_DIR/$COMPOSE_FILE" "$SELECTED_SERVICE"; then
    echo -e "${YELLOW}Warning: Service '$SELECTED_SERVICE' is not currently running.${NC}"
    echo -e "${CYAN}Would you like to continue anyway? (y/N):${NC}"
    read -r continue_choice
    if [[ "$continue_choice" != "y" && "$continue_choice" != "Y" ]]; then
        echo "Exiting."
        exit 0
    fi
fi

# Build docker compose command
COMPOSE_CMD="docker compose -f $DOCKER_DIR/$COMPOSE_FILE logs"

# Add options
if [[ "$SHOW_TIMESTAMPS" == true ]]; then
    COMPOSE_CMD="$COMPOSE_CMD --timestamps"
fi

if [[ "$FOLLOW_LOGS" == true ]]; then
    COMPOSE_CMD="$COMPOSE_CMD --follow"
else
    COMPOSE_CMD="$COMPOSE_CMD --tail=$LINE_COUNT"
fi

# Add service name
COMPOSE_CMD="$COMPOSE_CMD $SELECTED_SERVICE"

echo ""
echo -e "${CYAN}Command:${NC} $COMPOSE_CMD"
echo -e "${CYAN}Press Ctrl+C to stop following logs${NC}"
echo "=================================="

# Execute command
exec $COMPOSE_CMD