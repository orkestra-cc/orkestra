#!/bin/bash

# ============================================
# Orkestra System - Unified Interactive Deployment Script
# ============================================
#
# Purpose: Interactive deployment management for all environments
# Environments: development, staging, production
# Operations: deploy, stop, status
#
# Usage: ./deploy.sh
#
# ============================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Get script directory (now at project root)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"
DOCKER_DIR="$PROJECT_ROOT/docker"

# ============================================
# Environment Selection
# ============================================

show_env_menu() {
    clear
    echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║       Orkestra Deployment Manager                  ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
    echo ""

    # Check availability of each environment file
    if [ -f "$DOCKER_DIR/.env.development" ]; then
        DEV_STATUS="${GREEN}✓${NC}"
    else
        DEV_STATUS="${RED}✗${NC}"
    fi

    if [ -f "$DOCKER_DIR/.env.staging" ]; then
        STAGING_STATUS="${GREEN}✓${NC}"
    else
        STAGING_STATUS="${RED}✗${NC}"
    fi

    if [ -f "$DOCKER_DIR/.env.production" ]; then
        PROD_STATUS="${GREEN}✓${NC}"
    else
        PROD_STATUS="${RED}✗${NC}"
    fi

    echo -e "${CYAN}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║  Select Environment:                               ║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}║  1) Development  ${DEV_STATUS}                                 ${CYAN}║${NC}"
    echo -e "${CYAN}║  2) Staging      ${STAGING_STATUS}                                 ${CYAN}║${NC}"
    echo -e "${CYAN}║  3) Production   ${PROD_STATUS}                                 ${CYAN}║${NC}"
    echo -e "${CYAN}║  4) Exit                                           ║${NC}"
    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════╝${NC}"
    echo ""
}

select_environment() {
    show_env_menu

    while true; do
        read -p "$(echo -e ${YELLOW}Select environment [1-4]: ${NC})" ENV_CHOICE
        case $ENV_CHOICE in
            1) ENV="development"; ENV_FILE="$DOCKER_DIR/.env.development"; break ;;
            2) ENV="staging"; ENV_FILE="$DOCKER_DIR/.env.staging"; break ;;
            3) ENV="production"; ENV_FILE="$DOCKER_DIR/.env.production"; break ;;
            4) echo -e "${GREEN}Goodbye!${NC}"; exit 0 ;;
            *) echo -e "${RED}Invalid selection. Please choose 1-4.${NC}" ;;
        esac
    done

    if [ ! -f "$ENV_FILE" ]; then
        echo -e "${RED}❌ Environment file not found: $ENV_FILE${NC}"
        echo -e "${YELLOW}   Copy from .env.example: cp $DOCKER_DIR/.env.example $ENV_FILE${NC}"
        exit 1
    fi
}

# Set environment-specific colors and configurations
set_env_config() {
    case "$ENV" in
        development)
            ENV_COLOR=$GREEN
            ENV_ICON="🟢"
            BRANCH="any"
            COMPOSE_FILE="$DOCKER_DIR/docker-compose.dev.yml"
            DB_NAME="orkestra_dev"
            FRONTEND_URL="http://localhost:8080"
            BACKEND_URL="http://localhost:3000"
            ;;
        staging)
            ENV_COLOR=$YELLOW
            ENV_ICON="🟡"
            BRANCH="dev"
            COMPOSE_FILE="$DOCKER_DIR/docker-compose.staging.yml"
            DB_NAME="orkestra_staging"
            FRONTEND_URL="https://stage.orkestra.com"
            BACKEND_URL="https://stage.orkestra.com/api"
            ;;
        production)
            ENV_COLOR=$RED
            ENV_ICON="🔴"
            BRANCH="main"
            COMPOSE_FILE="$DOCKER_DIR/docker-compose.prod.yml"
            DB_NAME="orkestra"
            FRONTEND_URL="https://gestionale.orkestra.com"
            BACKEND_URL="https://api.orkestra.com"
            ;;
    esac
}

# ============================================
# Helper Functions
# ============================================

show_header() {
    clear
    echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║       Orkestra Deployment Manager                 ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
    echo -e "${ENV_COLOR}Environment: $(echo $ENV | tr '[:lower:]' '[:upper:]') $ENV_ICON${NC}"
    echo ""
}

show_operation_menu() {
    show_header
    echo -e "${CYAN}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║  Select Operation:                                 ║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}║  1) Deploy                                         ║${NC}"
    echo -e "${CYAN}║  2) Stop Services                                  ║${NC}"
    echo -e "${CYAN}║  3) Check Status                                   ║${NC}"
    echo -e "${CYAN}║  4) Exit                                           ║${NC}"
    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════╝${NC}"
    echo ""
}

ask_yes_no() {
    local prompt="$1"
    local default="${2:-y}"

    if [ "$default" = "y" ]; then
        local options="[Y/n]"
    else
        local options="[y/N]"
    fi

    while true; do
        read -p "$(echo -e ${YELLOW}$prompt $options: ${NC})" answer
        answer=${answer:-$default}
        case $answer in
            [Yy]* ) return 0;;
            [Nn]* ) return 1;;
            * ) echo "Please answer yes or no.";;
        esac
    done
}

show_summary() {
    local operation="$1"

    echo ""
    echo -e "${CYAN}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║  Summary                                           ║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}║  Environment: ${ENV_COLOR}$(printf '%-37s' "$(echo $ENV | tr '[:lower:]' '[:upper:]') $ENV_ICON")${CYAN}║${NC}"
    echo -e "${CYAN}║  Operation: $(printf '%-39s' "$operation")║${NC}"

    if [ "$operation" = "Deploy" ]; then
        echo -e "${CYAN}║  Branch: $(printf '%-42s' "$BRANCH")║${NC}"
        [ "$REBUILD_IMAGES" = "yes" ] && echo -e "${CYAN}║  Rebuild: ${GREEN}$(printf '%-41s' "Yes")${CYAN}║${NC}" || echo -e "${CYAN}║  Rebuild: ${YELLOW}$(printf '%-41s' "No")${CYAN}║${NC}"
        [ -n "$DEPLOY_SCOPE" ] && echo -e "${CYAN}║  Scope: $(printf '%-43s' "$DEPLOY_SCOPE")║${NC}"
    fi

    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════╝${NC}"
    echo ""
}

show_module_menu() {
    echo -e "${CYAN}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║  Select Deployment Scope:                          ║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}║  1) All (Full Stack)                               ║${NC}"
    echo -e "${CYAN}║  2) Backend only                                   ║${NC}"
    echo -e "${CYAN}║  3) Frontend only                                  ║${NC}"
    echo -e "${CYAN}║  4) Frontend + Backend                             ║${NC}"
    echo -e "${CYAN}║  5) Infrastructure only (MongoDB, Redis)           ║${NC}"
    echo -e "${CYAN}║                                                    ║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════╝${NC}"
    echo ""
}

ensure_network_exists() {
    local network_name="orkestra-network"
    if ! docker network inspect "$network_name" > /dev/null 2>&1; then
        echo -e "${YELLOW}  ⟳ Creating Docker network: $network_name${NC}"
        docker network create "$network_name"
        echo -e "${GREEN}  ✓ Network created${NC}"
    else
        echo -e "${GREEN}  ✓ Network $network_name exists${NC}"
    fi
}

# ============================================
# Operation: Deploy
# ============================================

operation_deploy() {
    show_header
    echo -e "${MAGENTA}═══ Deploy Configuration ═══${NC}"
    echo ""

    # Environment-specific options
    case "$ENV" in
        development)
            # Development options
            ask_yes_no "Rebuild images?" "y" && REBUILD_IMAGES="yes" || REBUILD_IMAGES="no"
            SKIP_CONFIRMATION="yes"
            ;;
        staging)
            # Staging options
            ask_yes_no "Rebuild images?" "y" && REBUILD_IMAGES="yes" || REBUILD_IMAGES="no"
            SKIP_CONFIRMATION="yes"
            ;;
        production)
            # Production options
            echo -e "${RED}⚠️  WARNING: Deploying to PRODUCTION${NC}"
            echo ""
            ask_yes_no "Skip confirmation prompts?" "n" && SKIP_CONFIRMATION="yes" || SKIP_CONFIRMATION="no"
            REBUILD_IMAGES="yes"  # Always rebuild for production
            ;;
    esac

    # Module selection
    echo ""
    show_module_menu
    while true; do
        read -p "$(echo -e ${YELLOW}Select deployment scope [1-5]: ${NC})" SCOPE_CHOICE
        case $SCOPE_CHOICE in
            1) DEPLOY_SCOPE="all"; break ;;
            2) DEPLOY_SCOPE="backend"; break ;;
            3) DEPLOY_SCOPE="frontend"; break ;;
            4) DEPLOY_SCOPE="frontend+backend"; break ;;
            5) DEPLOY_SCOPE="infra"; break ;;
            *) echo -e "${RED}Invalid selection. Please choose 1-5.${NC}" ;;
        esac
    done

    # Show summary
    show_summary "Deploy"

    # Final confirmation
    if ! ask_yes_no "Proceed with deployment?" "y"; then
        echo -e "${YELLOW}Deployment cancelled.${NC}"
        return
    fi

    # Production-specific confirmations
    if [ "$ENV" = "production" ] && [ "$SKIP_CONFIRMATION" = "no" ]; then
        if ! ask_yes_no "Have you tested this in staging?" "y"; then
            echo -e "${RED}Please test in staging first. Deployment cancelled.${NC}"
            return
        fi
    fi

    # Execute deployment
    execute_deploy
}

execute_deploy() {
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    DEPLOYMENT_ID="${ENV}_${TIMESTAMP}"

    echo ""
    echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║  Starting Deployment                               ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
    echo -e "${MAGENTA}Deployment ID: $DEPLOYMENT_ID${NC}"
    echo ""

    # Pre-deployment checks
    echo -e "${BLUE}📋 Pre-deployment Checks${NC}"

    if ! docker info > /dev/null 2>&1; then
        echo -e "${RED}  ✗ Docker daemon is not running${NC}"
        exit 1
    fi
    echo -e "${GREEN}  ✓ Docker is running${NC}"

    # Ensure Docker network exists
    ensure_network_exists

    if [ "$ENV" = "production" ]; then
        if [ "$EUID" -eq 0 ]; then
            echo -e "${RED}  ✗ Do not run as root${NC}"
            exit 1
        fi
        echo -e "${GREEN}  ✓ Not running as root${NC}"
    fi

    echo ""

    # Git operations
    if [ "$ENV" != "development" ]; then
        echo -e "${BLUE}📦 Git Operations${NC}"
        cd "$PROJECT_ROOT"

        CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

        # Check for uncommitted changes
        if ! git diff-index --quiet HEAD --; then
            echo -e "${YELLOW}  ⚠ Uncommitted changes detected${NC}"
            echo ""

            if [ "$ENV" = "production" ]; then
                echo -e "${RED}    Production requires a clean working tree.${NC}"
                echo -e "${RED}    Commit or stash changes before deploying to production.${NC}"
                exit 1
            else
                # Staging: ask user what to do
                echo -e "${CYAN}  What would you like to do?${NC}"
                echo -e "${CYAN}    1) Stash changes and continue${NC}"
                echo -e "${CYAN}    2) Stop deployment${NC}"
                echo ""
                while true; do
                    read -p "$(echo -e ${YELLOW}  Select option [1-2]: ${NC})" STASH_CHOICE
                    case $STASH_CHOICE in
                        1)
                            echo -e "${YELLOW}  ⟳ Stashing changes...${NC}"
                            git stash push -m "Auto-stash before $ENV deployment $TIMESTAMP"
                            echo -e "${GREEN}  ✓ Changes stashed (use 'git stash pop' to restore)${NC}"
                            break
                            ;;
                        2)
                            echo -e "${YELLOW}  Deployment cancelled.${NC}"
                            exit 0
                            ;;
                        *)
                            echo -e "${RED}  Invalid selection. Please choose 1 or 2.${NC}"
                            ;;
                    esac
                done
            fi
        else
            echo -e "${GREEN}  ✓ No uncommitted changes${NC}"
        fi

        # Switch branch if needed
        if [ "$CURRENT_BRANCH" != "$BRANCH" ]; then
            echo -e "${YELLOW}  ⟳ Switching to $BRANCH branch...${NC}"
            git checkout "$BRANCH"
            echo -e "${GREEN}  ✓ Switched to $BRANCH${NC}"
        fi

        # Pull latest changes
        echo -e "${YELLOW}  ⟳ Pulling latest changes...${NC}"
        git pull origin "$BRANCH"
        COMMIT_HASH=$(git rev-parse --short HEAD)
        COMMIT_MSG=$(git log -1 --pretty=%B)
        echo -e "${GREEN}  ✓ Updated to commit: $COMMIT_HASH${NC}"

        # Production checks sync with remote
        if [ "$ENV" = "production" ]; then
            LOCAL_HASH=$(git rev-parse HEAD)
            REMOTE_HASH=$(git rev-parse origin/$BRANCH)
            if [ "$LOCAL_HASH" != "$REMOTE_HASH" ]; then
                echo -e "${RED}  ✗ Local and remote branches are out of sync${NC}"
                exit 1
            fi
            echo -e "${GREEN}  ✓ Branch is up to date with origin${NC}"
        fi

        echo ""
    else
        COMMIT_HASH="dev"
        COMMIT_MSG="Development build"
    fi

    # Build images (skip for infra-only deployment)
    if [ "$REBUILD_IMAGES" = "yes" ] && [ "$DEPLOY_SCOPE" != "infra" ]; then
        echo -e "${BLUE}🔨 Building Docker Images${NC}"

        # Export version info (format: YYYY-MM-DD_HH:MM:SS_commit-hash)
        export VERSION="$(date -u +%Y-%m-%d_%H:%M:%S)_${COMMIT_HASH}"
        export BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
        export GIT_COMMIT="$COMMIT_HASH"

        echo -e "${CYAN}  Version: $VERSION${NC}"
        echo -e "${CYAN}  Build Time: $BUILD_TIME${NC}"
        echo -e "${CYAN}  Commit: $GIT_COMMIT${NC}"
        echo ""

        # Build backend - only if deploying backend, frontend+backend, or all
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "backend" ] || [ "$DEPLOY_SCOPE" = "frontend+backend" ]; then
            echo -e "${YELLOW}  ⟳ Building backend image (no cache)...${NC}"
            docker compose -f $COMPOSE_FILE --env-file $ENV_FILE build --no-cache \
                --build-arg VERSION="$VERSION" \
                --build-arg BUILD_TIME="$BUILD_TIME" \
                --build-arg GIT_COMMIT="$GIT_COMMIT" \
                backend
            echo -e "${GREEN}  ✓ Backend image built${NC}"
        fi

        # Build frontend - only if deploying frontend, frontend+backend, or all
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "frontend" ] || [ "$DEPLOY_SCOPE" = "frontend+backend" ]; then
            echo -e "${YELLOW}  ⟳ Building frontend image (no cache)...${NC}"
            docker compose -f $COMPOSE_FILE --env-file $ENV_FILE build --no-cache \
                --build-arg VERSION="$VERSION" \
                --build-arg BUILD_TIME="$BUILD_TIME" \
                --build-arg GIT_COMMIT="$GIT_COMMIT" \
                frontend
            echo -e "${GREEN}  ✓ Frontend image built${NC}"
        fi

        echo ""
    fi

    # Deploy services
    echo -e "${BLUE}🚀 Deploying Services${NC}"

    # Infrastructure is always ensured for all scopes
    echo -e "${YELLOW}  ⟳ Ensuring infrastructure services are running...${NC}"
    docker compose -f $DOCKER_DIR/docker-compose.infra.yml --env-file $ENV_FILE up -d
    sleep 5
    echo -e "${GREEN}  ✓ Infrastructure ready${NC}"

    # If infra-only, stop here
    if [ "$DEPLOY_SCOPE" = "infra" ]; then
        echo -e "${GREEN}  ✓ Infrastructure deployment complete${NC}"
    elif [ "$ENV" = "production" ]; then
        # Production: zero-downtime rolling update
        # Deploy backend if scope includes it
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "backend" ] || [ "$DEPLOY_SCOPE" = "frontend+backend" ]; then
            echo -e "${YELLOW}  ⟳ Deploying backend with rolling update...${NC}"
            docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d --no-deps backend
            echo -e "${GREEN}  ✓ Backend updated${NC}"

            echo -e "${YELLOW}  ⟳ Waiting for backend to be healthy (15s)...${NC}"
            sleep 15
        fi

        # Deploy frontend if scope includes it
        if [ "$DEPLOY_SCOPE" = "all" ] || [ "$DEPLOY_SCOPE" = "frontend" ] || [ "$DEPLOY_SCOPE" = "frontend+backend" ]; then
            echo -e "${YELLOW}  ⟳ Deploying frontend...${NC}"
            docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d --no-deps frontend
            echo -e "${GREEN}  ✓ Frontend updated${NC}"
        fi

        echo -e "${YELLOW}  ⟳ Waiting for services to stabilize (30s)...${NC}"
        sleep 30
    else
        # Development/Staging: simple down/up for selected services
        if [ "$DEPLOY_SCOPE" = "all" ]; then
            echo -e "${YELLOW}  ⟳ Stopping current services...${NC}"
            docker compose -f $COMPOSE_FILE down
            echo -e "${GREEN}  ✓ Services stopped${NC}"

            echo -e "${YELLOW}  ⟳ Starting new services...${NC}"
            docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d
            echo -e "${GREEN}  ✓ Services started${NC}"
        else
            # Stop and restart only the selected service(s)
            if [ "$DEPLOY_SCOPE" = "backend" ]; then
                echo -e "${YELLOW}  ⟳ Restarting backend service...${NC}"
                docker compose -f $COMPOSE_FILE stop backend 2>/dev/null || true
                docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d backend
                echo -e "${GREEN}  ✓ Backend restarted${NC}"
            fi

            if [ "$DEPLOY_SCOPE" = "frontend" ]; then
                echo -e "${YELLOW}  ⟳ Restarting frontend service...${NC}"
                docker compose -f $COMPOSE_FILE stop frontend 2>/dev/null || true
                docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d frontend
                echo -e "${GREEN}  ✓ Frontend restarted${NC}"
            fi

            if [ "$DEPLOY_SCOPE" = "frontend+backend" ]; then
                echo -e "${YELLOW}  ⟳ Restarting frontend and backend services...${NC}"
                docker compose -f $COMPOSE_FILE stop frontend backend 2>/dev/null || true
                docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d frontend backend
                echo -e "${GREEN}  ✓ Frontend and backend restarted${NC}"
            fi
        fi

        echo -e "${YELLOW}  ⟳ Waiting for services to initialize (30s)...${NC}"
        sleep 30
    fi

    echo ""

    # Health checks
    if [ "$ENV" != "development" ]; then
        echo -e "${BLUE}🏥 Health Checks${NC}"

        # Check if health-check.sh exists in docker/ or scripts/
        HEALTH_CHECK_SCRIPT=""
        if [ -f "$DOCKER_DIR/health-check.sh" ]; then
            HEALTH_CHECK_SCRIPT="$DOCKER_DIR/health-check.sh"
        elif [ -f "$PROJECT_ROOT/scripts/health-check.sh" ]; then
            HEALTH_CHECK_SCRIPT="$PROJECT_ROOT/scripts/health-check.sh"
        fi

        if [ "$ENV" = "production" ]; then
            # Production: retry health checks
            MAX_RETRIES=5
            RETRY_INTERVAL=10
            HEALTH_CHECK_SUCCESS=false

            for i in $(seq 1 $MAX_RETRIES); do
                echo -e "${YELLOW}  ⟳ Health check attempt $i/$MAX_RETRIES...${NC}"

                if [ -n "$HEALTH_CHECK_SCRIPT" ]; then
                    if bash "$HEALTH_CHECK_SCRIPT" $ENV > /tmp/health_check_output.txt 2>&1; then
                        echo -e "${GREEN}  ✓ All health checks passed${NC}"
                        HEALTH_CHECK_SUCCESS=true
                        break
                    else
                        echo -e "${YELLOW}  ⚠ Health checks failed, waiting ${RETRY_INTERVAL}s...${NC}"
                        sleep $RETRY_INTERVAL
                    fi
                else
                    echo -e "${RED}  ✗ Health check script not found${NC}"
                    break
                fi
            done

            if [ "$HEALTH_CHECK_SUCCESS" = false ]; then
                echo -e "${RED}  ✗ Health checks failed after $MAX_RETRIES attempts${NC}"
                echo -e "${RED}  ✗ Deployment failed - manual intervention required${NC}"
                exit 1
            fi
        else
            # Staging: single health check attempt
            if [ -n "$HEALTH_CHECK_SCRIPT" ]; then
                if bash "$HEALTH_CHECK_SCRIPT" $ENV; then
                    echo -e "${GREEN}  ✓ All health checks passed${NC}"
                else
                    echo -e "${RED}  ✗ Health checks failed${NC}"
                    echo -e "${RED}  ✗ Deployment failed - manual intervention required${NC}"
                    exit 1
                fi
            else
                echo -e "${YELLOW}  ⚠ Health check script not found, skipping...${NC}"
            fi
        fi

        echo ""
    fi

    # Smoke tests (production only)
    if [ "$ENV" = "production" ]; then
        echo -e "${BLUE}🧪 Smoke Tests${NC}"

        echo -e "${YELLOW}  ⟳ Testing authentication endpoint...${NC}"
        AUTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "${BACKEND_URL}/api/v1/auth/health" || echo "000")
        if [ "$AUTH_STATUS" == "200" ]; then
            echo -e "${GREEN}  ✓ Authentication endpoint healthy${NC}"
        else
            echo -e "${YELLOW}  ⚠ Authentication endpoint returned: $AUTH_STATUS${NC}"
        fi

        echo -e "${YELLOW}  ⟳ Testing API documentation...${NC}"
        DOCS_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "${BACKEND_URL}/docs" || echo "000")
        if [ "$DOCS_STATUS" == "200" ]; then
            echo -e "${GREEN}  ✓ API documentation accessible${NC}"
        else
            echo -e "${YELLOW}  ⚠ API documentation returned: $DOCS_STATUS${NC}"
        fi

        echo ""
    fi

    # Post-deployment
    echo -e "${BLUE}✨ Post-Deployment${NC}"

    # Tag images
    if [ "$ENV" = "production" ]; then
        docker tag orkestra-backend:production orkestra-backend:$DEPLOYMENT_ID
        docker tag orkestra-backend:production orkestra-backend:latest
        docker tag orkestra-frontend:production orkestra-frontend:$DEPLOYMENT_ID
        docker tag orkestra-frontend:production orkestra-frontend:latest
    else
        docker tag orkestra-backend:$ENV orkestra-backend:$DEPLOYMENT_ID 2>/dev/null || true
        docker tag orkestra-frontend:$ENV orkestra-frontend:$DEPLOYMENT_ID 2>/dev/null || true
    fi
    echo -e "${GREEN}  ✓ Images tagged${NC}"

    # Save deployment metadata
    METADATA_FILE="$DOCKER_DIR/deployments/${ENV}_deployments.log"
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
    echo -e "${GREEN}  ✓ Deployment metadata saved${NC}"

    # Git tag for production
    if [ "$ENV" = "production" ]; then
        cd "$PROJECT_ROOT"
        TAG_NAME="production-${TIMESTAMP}"
        git tag -a "$TAG_NAME" -m "Production deployment: $COMMIT_MSG"
        echo -e "${GREEN}  ✓ Git tag created: $TAG_NAME${NC}"
    fi

    # Success message
    echo ""
    echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║  ✓ Deployment Successful                           ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${MAGENTA}📊 Deployment Summary:${NC}"
    echo -e "  Deployment ID: ${DEPLOYMENT_ID}"
    echo -e "  Environment: $(echo $ENV | tr '[:lower:]' '[:upper:]')"
    echo -e "  Commit: ${COMMIT_HASH}"
    echo -e "  Frontend URL: ${FRONTEND_URL}"
    echo -e "  Backend URL: ${BACKEND_URL}"
    echo ""

    if [ "$ENV" = "production" ]; then
        echo -e "${MAGENTA}📝 Post-Deployment Checklist:${NC}"
        echo -e "  [ ] Verify user login flow"
        echo -e "  [ ] Test critical workflows"
        echo -e "  [ ] Monitor error rates"
        echo -e "  [ ] Check application logs"
        echo -e "  [ ] Verify background jobs"
        echo -e "  [ ] Test mobile app connectivity"
        echo ""
    fi

    echo -e "${CYAN}📊 Useful Commands:${NC}"
    echo -e "  Logs:    docker compose -f $COMPOSE_FILE logs -f"
    echo -e "  Stats:   docker stats"
    echo -e "  Health:  bash $DOCKER_DIR/health-check.sh $ENV"
    echo ""
}

# ============================================
# Operation: Stop
# ============================================

operation_stop() {
    show_header
    echo -e "${MAGENTA}═══ Stop Services ═══${NC}"
    echo ""

    if [ "$ENV" = "production" ]; then
        echo -e "${RED}⚠️  WARNING: Stopping PRODUCTION services${NC}"
        echo ""
    fi

    if ! ask_yes_no "Are you sure you want to stop all services?" "n"; then
        echo -e "${YELLOW}Operation cancelled.${NC}"
        return
    fi

    echo ""
    echo -e "${BLUE}🛑 Stopping Services${NC}"

    echo -e "${YELLOW}  ⟳ Stopping application services...${NC}"
    docker compose -f $COMPOSE_FILE down
    echo -e "${GREEN}  ✓ Application services stopped${NC}"

    if ask_yes_no "Also stop infrastructure services (MongoDB, Redis)?" "n"; then
        echo -e "${YELLOW}  ⟳ Stopping infrastructure services...${NC}"
        docker compose -f $DOCKER_DIR/docker-compose.infra.yml down
        echo -e "${GREEN}  ✓ Infrastructure services stopped${NC}"
    fi

    echo ""
    echo -e "${GREEN}✓ Services stopped successfully${NC}"
    echo ""
}

# ============================================
# Operation: Status
# ============================================

operation_status() {
    show_header
    echo -e "${MAGENTA}═══ System Status ═══${NC}"
    echo ""

    # Container status
    echo -e "${BLUE}🐳 Container Status${NC}"
    docker compose -f $DOCKER_DIR/docker-compose.infra.yml -f $COMPOSE_FILE ps
    echo ""

    # Health checks
    if [ "$ENV" != "development" ]; then
        echo -e "${BLUE}🏥 Health Checks${NC}"

        # Check if health-check.sh exists in docker/ or scripts/
        HEALTH_CHECK_SCRIPT=""
        if [ -f "$DOCKER_DIR/health-check.sh" ]; then
            HEALTH_CHECK_SCRIPT="$DOCKER_DIR/health-check.sh"
        elif [ -f "$PROJECT_ROOT/scripts/health-check.sh" ]; then
            HEALTH_CHECK_SCRIPT="$PROJECT_ROOT/scripts/health-check.sh"
        fi

        if [ -n "$HEALTH_CHECK_SCRIPT" ]; then
            bash "$HEALTH_CHECK_SCRIPT" $ENV || true
        else
            echo -e "${YELLOW}  ⚠ Health check script not found${NC}"
        fi
        echo ""
    fi

    # Resource usage
    echo -e "${BLUE}📊 Resource Usage${NC}"
    docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}" | head -n 10
    echo ""

    echo -e "${CYAN}Press Enter to continue...${NC}"
    read
}

# ============================================
# Main Menu Loop
# ============================================

main() {
    # Select environment before showing operation menu
    select_environment
    set_env_config

    while true; do
        show_operation_menu

        read -p "$(echo -e ${YELLOW}Select operation [1-4]: ${NC})" OPERATION

        case $OPERATION in
            1)
                operation_deploy
                ;;
            2)
                operation_stop
                ;;
            3)
                operation_status
                ;;
            4)
                echo ""
                echo -e "${GREEN}Goodbye!${NC}"
                exit 0
                ;;
            *)
                echo -e "${RED}Invalid selection${NC}"
                sleep 1
                ;;
        esac
    done
}

# Run main menu
main
