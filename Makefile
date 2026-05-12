# Orkestra Project Management
.PHONY: help infra-up infra-down infra-restart infra-logs infra-status clean volumes-clean
.PHONY: backend-run backend-dev backend-build backend-test backend-clean backend-deps
.PHONY: frontend-admin-run frontend-admin-dev frontend-admin-build frontend-admin-test frontend-admin-clean frontend-admin-deps frontend-admin-preview frontend-admin-type-check
.PHONY: run dev build test start stop restart full-dev

# Default target
help:
	@echo "Orkestra Project Management Commands:"
	@echo ""
	@echo "Quick Start:"
	@echo "  make dev              - Start everything in development mode (infra + backend)"
	@echo "  make full-dev         - Start everything including frontend (infra + backend + frontend)"
	@echo "  make start            - Start all services"
	@echo "  make stop             - Stop all services"
	@echo "  make restart          - Restart all services"
	@echo ""
	@echo "Backend:"
	@echo "  make backend-run      - Run backend server"
	@echo "  make backend-dev      - Run backend with hot reload"
	@echo "  make backend-build    - Build backend binary"
	@echo "  make backend-test     - Run backend tests"
	@echo "  make backend-deps     - Install backend dependencies"
	@echo "  make backend-clean    - Clean backend build artifacts"
	@echo ""
	@echo "Frontend:"
	@echo "  make frontend-admin-run     - Run frontend dev server"
	@echo "  make frontend-admin-dev     - Run frontend with hot reload (alias for frontend-admin-run)"
	@echo "  make frontend-admin-build   - Build frontend for production"
	@echo "  make frontend-admin-preview - Preview production build"
	@echo "  make frontend-admin-test    - Run frontend tests"
	@echo "  make frontend-admin-type-check - Run TypeScript type checking"
	@echo "  make frontend-admin-deps    - Install frontend dependencies"
	@echo "  make frontend-admin-clean   - Clean frontend build artifacts"
	@echo ""
	@echo "Infrastructure:"
	@echo "  make infra-up         - Start all infrastructure services"
	@echo "  make infra-down       - Stop all infrastructure services"
	@echo "  make infra-restart    - Restart all infrastructure services"
	@echo "  make infra-logs       - Show infrastructure logs"
	@echo "  make infra-status     - Show infrastructure status"
	@echo "  make infra-dev        - Start development infrastructure (includes mailpit)"
	@echo "  make infra-monitoring - Start with monitoring (includes SigNoz)"
	@echo ""
	@echo "Database:"
	@echo "  make mongo-shell      - Connect to MongoDB shell"
	@echo "  make redis-cli        - Connect to Redis CLI"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean            - Stop all services and clean build artifacts"
	@echo "  make volumes-clean    - Remove all data volumes (WARNING: Deletes all data!)"

# Infrastructure management
infra-up:
	@echo "Starting Orkestra infrastructure..."
	docker compose -f docker/docker-compose.yml up -d
	@echo "Infrastructure is starting. Run 'make infra-status' to check status."

infra-down:
	@echo "Stopping Orkestra infrastructure..."
	docker compose -f docker/docker-compose.yml down
	@echo "Infrastructure stopped."

infra-restart:
	@echo "Restarting Orkestra infrastructure..."
	docker compose -f docker/docker-compose.yml restart
	@echo "Infrastructure restarted."

infra-logs:
	docker compose -f docker/docker-compose.yml logs -f

infra-status:
	@echo "Orkestra Infrastructure Status:"
	@echo "================================"
	@docker compose -f docker/docker-compose.yml ps

# Development profile (includes mailpit)
infra-dev:
	@echo "Starting Orkestra infrastructure with development tools..."
	docker-compose -f docker/docker-compose.yml --profile development up -d
	@echo "Development infrastructure is starting."
	@echo "Mailpit UI: http://localhost:8025"

# Monitoring profile (includes SigNoz)
infra-monitoring:
	@echo "Starting Orkestra infrastructure with monitoring..."
	docker compose -f docker/docker-compose.yml --profile monitoring up -d
	@echo "Infrastructure with monitoring is starting."

# Database access
mongo-shell:
	@echo "Connecting to MongoDB..."
	docker exec -it orkestra-mongodb mongosh -u $${MONGO_ROOT_USER:-admin} -p $${MONGO_ROOT_PASSWORD:-orkestra_mongo_admin_2024} --authenticationDatabase admin

redis-cli:
	@echo "Connecting to Redis..."
	docker exec -it orkestra-redis redis-cli -a $${REDIS_PASSWORD:-orkestra_redis_secure_2024}

# RabbitMQ Management
rabbitmq-ui:
	@echo "Opening RabbitMQ Management UI..."
	@echo "URL: http://localhost:15672"
	@echo "Username: $${RABBITMQ_USER:-orkestra}"
	@echo "Password: $${RABBITMQ_PASSWORD:-orkestra_rmq_secure_2024}"

# MinIO Console
minio-ui:
	@echo "Opening MinIO Console..."
	@echo "URL: http://localhost:9001"
	@echo "Username: $${MINIO_ROOT_USER:-orkestra_admin}"
	@echo "Password: $${MINIO_ROOT_PASSWORD:-orkestra_minio_secure_2024}"

# Backend Management
backend-run:
	@echo "Starting backend server..."
	@cd backend && go run cmd/server/main.go

backend-dev:
	@echo "Starting backend in development mode with hot reload..."
	@cd backend && bash ../scripts/install-air.sh

backend-build:
	@echo "Building backend binary..."
	@cd backend && go build -o bin/server cmd/server/main.go
	@echo "Backend built: backend/bin/server"

backend-test:
	@echo "Running backend tests..."
	@cd backend && go test ./...

backend-deps:
	@echo "Installing backend dependencies..."
	@cd backend && go mod download && go mod tidy
	@echo "Backend dependencies installed."

backend-clean:
	@echo "Cleaning backend build artifacts..."
	@rm -rf backend/bin/
	@echo "Backend artifacts cleaned."

# Frontend Management
frontend-admin-run:
	@echo "Starting frontend development server..."
	@cd frontend-admin && npm run dev

frontend-admin-dev: frontend-admin-run

frontend-admin-build:
	@echo "Building frontend for production..."
	@cd frontend-admin && npm run build
	@echo "Frontend built in frontend-admin/dist/"

frontend-admin-preview:
	@echo "Starting frontend preview server..."
	@cd frontend-admin && npm run preview

frontend-admin-test:
	@echo "Running frontend tests..."
	@cd frontend-admin && npm test

frontend-admin-type-check:
	@echo "Running TypeScript type checking..."
	@cd frontend-admin && npm run type-check

frontend-admin-deps:
	@echo "Installing frontend dependencies..."
	@cd frontend-admin && npm install
	@echo "Frontend dependencies installed."

frontend-admin-clean:
	@echo "Cleaning frontend build artifacts..."
	@rm -rf frontend-admin/dist/
	@rm -rf frontend-admin/node_modules/.vite/
	@echo "Frontend artifacts cleaned."

# Combined Operations
start: infra-up
	@echo "Waiting for infrastructure to be ready..."
	@sleep 5
	@make backend-run

stop:
	@echo "Stopping all services..."
	@pkill -f "go run cmd/server/main.go" || true
	@pkill -f "npm run dev" || true
	@pkill -f "vite" || true
	@make infra-down

restart: stop start

dev:
	@echo "Starting full development environment..."
	@make infra-dev
	@echo "Waiting for infrastructure to be ready..."
	@sleep 5
	@echo ""
	@echo "================================"
	@echo "Infrastructure is ready!"
	@echo "================================"
	@echo "MongoDB:    localhost:27017"
	@echo "Redis:      localhost:6379"
	@echo "RabbitMQ:   localhost:5672 (Management: http://localhost:15672)"
	@echo "MinIO:      localhost:9000 (Console: http://localhost:9001)"
	@echo "Mailpit:    localhost:1025 (UI: http://localhost:8025)"
	@echo ""
	@echo "Starting backend server..."
	@echo "================================"
	@make backend-dev

dev-simple:
	@echo "Starting backend without hot reload (no air required)..."
	@make infra-dev
	@echo "Waiting for infrastructure to be ready..."
	@sleep 5
	@echo "Infrastructure ready. Starting backend..."
	@make backend-run

full-dev:
	@echo "Starting full development environment (infrastructure + backend + frontend)..."
	@make infra-dev
	@echo "Waiting for infrastructure to be ready..."
	@sleep 5
	@echo ""
	@echo "================================"
	@echo "Infrastructure is ready!"
	@echo "================================"
	@echo "MongoDB:    localhost:27017"
	@echo "Redis:      localhost:6379"
	@echo "RabbitMQ:   localhost:5672 (Management: http://localhost:15672)"
	@echo "MinIO:      localhost:9000 (Console: http://localhost:9001)"
	@echo "Mailpit:    localhost:1025 (UI: http://localhost:8025)"
	@echo ""
	@echo "Starting backend and frontend servers..."
	@echo "Backend API will be available at: http://localhost:3000"
	@echo "Frontend will be available at: http://localhost:8080"
	@echo "================================"
	@echo ""
	@echo "Starting backend in the background..."
	@cd backend && bash ../scripts/install-air.sh &
	@echo "Waiting for backend to start..."
	@sleep 3
	@echo "Starting frontend..."
	@make frontend-admin-dev

build: backend-deps frontend-admin-deps backend-build frontend-admin-build
	@echo "Build complete! Backend and frontend built successfully."

test: backend-test frontend-admin-test
	@echo "All tests complete!"

# Cleanup
clean:
	@echo "Stopping all services and cleaning up..."
	@pkill -f "air" || true
	@pkill -f "go run cmd/server/main.go" || true
	@pkill -f "npm run dev" || true
	@pkill -f "vite" || true
	@docker-compose -f docker/docker-compose.yml down
	@make backend-clean
	@make frontend-admin-clean
	@echo "Cleanup complete."

volumes-clean:
	@echo "WARNING: This will delete all data volumes!"
	@read -p "Are you sure? [y/N]: " confirm && [ "$${confirm}" = "y" ] || exit 1
	docker-compose -f docker/docker-compose.yml down -v
	@echo "All volumes removed."

# Health checks
health-check:
	@echo "Checking infrastructure health..."
	@docker exec orkestra-mongodb echo 'db.runCommand("ping").ok' | mongosh localhost:27017/test --quiet && echo "✓ MongoDB is healthy" || echo "✗ MongoDB is not responding"
	@docker exec orkestra-redis redis-cli ping > /dev/null 2>&1 && echo "✓ Redis is healthy" || echo "✗ Redis is not responding"
	@docker exec orkestra-rabbitmq rabbitmq-diagnostics -q ping > /dev/null 2>&1 && echo "✓ RabbitMQ is healthy" || echo "✗ RabbitMQ is not responding"

# Quick start for development
quick-start: dev

# System health check
system-check: health-check
	@echo ""
	@echo "Checking backend..."
	@curl -s http://localhost:3000/health > /dev/null 2>&1 && echo "✓ Backend is healthy" || echo "✗ Backend is not responding"
	@echo ""
	@echo "Checking frontend..."
	@curl -s http://localhost:8080 > /dev/null 2>&1 && echo "✓ Frontend is responding" || echo "✗ Frontend is not responding"
	@echo ""
	@echo "System Status:"
	@echo "=============="
	@echo "Backend API:  http://localhost:3000"
	@echo "API Docs:     http://localhost:3000/docs"
	@echo "Health:       http://localhost:3000/health"
	@echo "Frontend:     http://localhost:8080"
	@echo ""
	@echo "OAuth Endpoints:"
	@echo "  Google:     http://localhost:3000/auth/oauth/google"
	@echo "  Apple:      http://localhost:3000/auth/oauth/apple"

# Backend logs
backend-logs:
	@echo "Showing backend logs (Ctrl+C to exit)..."
	@tail -f backend/logs/*.log 2>/dev/null || echo "No log files found. Backend may not be running."

# Full system logs
logs:
	@echo "Showing all system logs..."
	@make infra-logs &
	@make backend-logs

# Docker utilities
docker-build-backend:
	@echo "Building backend Docker image..."
	@cd backend && docker build -t orkestra-backend:latest .
	@echo "Backend image built: orkestra-backend:latest"

docker-run-backend:
	@echo "Running backend in Docker..."
	@docker run -p 3000:3000 --env-file backend/.env --network orkestra-network orkestra-backend:latest

# ============================================================================
# CI parity — single source of truth for "what is a passing build."
# Both contributors (`make ci`) and GitHub Actions (.github/workflows/*.yml)
# invoke these targets, so local and CI cannot drift. See CONTRIBUTING.md.
# ============================================================================

.PHONY: install install-hooks fmt ci-help
.PHONY: ci ci-all ci-backend ci-backend-matrix ci-frontend-admin ci-frontend-client ci-mobile
.PHONY: backend-lint backend-test-ci backend-tenantscope backend-policycoverage backend-vulncheck backend-build-enterprise
.PHONY: admin-typecheck admin-lint admin-test admin-audit admin-build
.PHONY: client-typecheck client-lint client-build
.PHONY: mobile-analyze mobile-test

# Detect changed surfaces vs $(BASE_REF). Override with `BASE_REF=origin/main make ci`.
BASE_REF ?= origin/dev
SINCE := $(shell git merge-base HEAD $(BASE_REF) 2>/dev/null || echo HEAD~1)
CI_CHANGED := $(shell { git diff --name-only $(SINCE)...HEAD 2>/dev/null; git diff --name-only 2>/dev/null; git diff --name-only --cached 2>/dev/null; } | sort -u)
BACKEND_CHANGED := $(if $(filter backend/%,$(CI_CHANGED)),1,)
ADMIN_CHANGED   := $(if $(filter frontend-admin/%,$(CI_CHANGED)),1,)
CLIENT_CHANGED  := $(if $(filter frontend-client/%,$(CI_CHANGED)),1,)
MOBILE_CHANGED  := $(if $(filter mobile/%,$(CI_CHANGED)),1,)

# ---- Setup ----

install:
	@echo "Provisioning all toolchains and dependencies..."
	@command -v mise >/dev/null 2>&1 && mise install || echo "mise not installed — see CONTRIBUTING.md"
	@command -v pre-commit >/dev/null 2>&1 && pre-commit install --install-hooks || echo "pre-commit not installed — see CONTRIBUTING.md"
	@cd backend && go mod download
	@cd frontend-admin && npm ci
	@cd frontend-client && npm ci
	@cd mobile && flutter pub get
	@echo "Install complete."

install-hooks:
	@pre-commit install --install-hooks

# ---- Top-level CI dispatch ----

ci:
	@echo "Detecting changed surfaces vs $(BASE_REF)..."
	@if [ -z "$(BACKEND_CHANGED)$(ADMIN_CHANGED)$(CLIENT_CHANGED)$(MOBILE_CHANGED)" ]; then \
	  echo "  (no surface changes — nothing to check)"; \
	else \
	  [ -n "$(BACKEND_CHANGED)" ] && echo "  - backend"          || true; \
	  [ -n "$(ADMIN_CHANGED)"   ] && echo "  - frontend-admin"   || true; \
	  [ -n "$(CLIENT_CHANGED)"  ] && echo "  - frontend-client"  || true; \
	  [ -n "$(MOBILE_CHANGED)"  ] && echo "  - mobile"           || true; \
	fi
	@[ -n "$(BACKEND_CHANGED)" ] && $(MAKE) ci-backend         || true
	@[ -n "$(ADMIN_CHANGED)"   ] && $(MAKE) ci-frontend-admin  || true
	@[ -n "$(CLIENT_CHANGED)"  ] && $(MAKE) ci-frontend-client || true
	@[ -n "$(MOBILE_CHANGED)"  ] && $(MAKE) ci-mobile          || true

ci-all: ci-backend ci-frontend-admin ci-frontend-client ci-mobile
	@echo "All surface checks passed."

# ---- Backend ----

ci-backend: backend-lint backend-tenantscope backend-policycoverage backend-vulncheck backend-test-ci backend-build-enterprise
	@echo "Backend CI: OK"

ci-backend-matrix: ci-backend
	@cd backend && $(MAKE) build-starter build-minimal build-billing build-ai build-saas build-enterprise

backend-lint:
	@cd backend && golangci-lint run --config=.golangci.yml

backend-test-ci:
	@cd backend && go test -race -coverprofile=coverage.out ./...
	@cd backend && go tool cover -func=coverage.out | tail -1

backend-tenantscope:
	@cd backend && go test ./tools/tenantscope/...
	@cd backend && go run ./tools/tenantscope/cmd/tenantscope \
	  -baseline=tools/tenantscope/baseline.txt ./internal/...

backend-policycoverage:
	@cd backend && go test ./tools/policycoverage/...
	@cd backend && go run ./tools/policycoverage/cmd/policycoverage \
	  -baseline=tools/policycoverage/baseline.txt \
	  -cedar=internal/core/authz/cedar/policies ./internal/...

# Reads OSV IDs (one per line, '#'-comments) from backend/.vulncheck-allowlist.txt.
# Fails only if a reachable vulnerability is NOT on the allowlist.
backend-vulncheck:
	@cd backend && { \
	  set +e; \
	  govulncheck -format=json ./... > /tmp/govuln.json; \
	  set -e; \
	  govulncheck ./... || true; \
	  reachable_ids=$$(jq -r 'select(.finding != null and (.finding.trace | length) > 0) | .finding.osv' /tmp/govuln.json | sort -u); \
	  echo "Reachable vulnerability IDs: $${reachable_ids:-<none>}"; \
	  allowlist=$$(grep -vE '^\s*(#|$$)' .vulncheck-allowlist.txt | tr '\n' ' '); \
	  unaccepted=""; \
	  for id in $$reachable_ids; do \
	    case " $$allowlist " in \
	      *" $$id "*) ;; \
	      *) unaccepted="$$unaccepted $$id" ;; \
	    esac; \
	  done; \
	  if [ -n "$$unaccepted" ]; then \
	    echo "::error::Unaccepted vulnerabilities reachable from our code:$$unaccepted"; \
	    exit 1; \
	  fi; \
	  echo "All reachable vulnerabilities are on the allowlist."; \
	}

backend-build-enterprise:
	@cd backend && $(MAKE) build-enterprise

# ---- Frontend Admin ----

ci-frontend-admin: admin-typecheck admin-lint admin-test admin-audit admin-build
	@echo "Frontend-admin CI: OK"

admin-typecheck:
	@cd frontend-admin && npm run typecheck

admin-lint:
	@cd frontend-admin && npx eslint src/ --ext .js,.jsx,.ts,.tsx --max-warnings 0

admin-test:
	@cd frontend-admin && npm run test:coverage

admin-audit:
	@cd frontend-admin && npm audit --audit-level=high

admin-build:
	@cd frontend-admin && npm run build

# ---- Frontend Client ----

ci-frontend-client: client-typecheck client-lint client-build
	@echo "Frontend-client CI: OK"

client-typecheck:
	@cd frontend-client && npm run typecheck

client-lint:
	@cd frontend-client && npm run lint -- --max-warnings 0

client-build:
	@cd frontend-client && npm run build

# ---- Mobile ----

ci-mobile: mobile-analyze mobile-test
	@echo "Mobile CI: OK"

mobile-analyze:
	@cd mobile && flutter analyze

mobile-test:
	@cd mobile && flutter test

# ---- Formatters (write mode) ----

fmt:
	@echo "Running all formatters..."
	@cd backend && gofmt -w .
	@cd frontend-admin && npx prettier --write 'src/**/*.{ts,tsx,js,jsx,json,css,scss}' 2>/dev/null || true
	@cd frontend-client && npx prettier --write 'src/**/*.{ts,tsx,js,jsx,json,css,scss}' 2>/dev/null || true
	@cd mobile && dart format .
	@echo "Formatters done."

# ---- Help ----

ci-help:
	@echo "CI parity targets (see CONTRIBUTING.md for the full guide):"
	@echo ""
	@echo "  make install               - Provision toolchains + dependencies (mise, npm, go, flutter)"
	@echo "  make install-hooks         - (Re-)install pre-commit git hooks"
	@echo "  make fmt                   - Run all formatters in write mode"
	@echo ""
	@echo "  make ci                    - Run CI checks for changed surfaces only (pre-push)"
	@echo "  make ci-all                - Run every surface (what CI does on dev/main)"
	@echo ""
	@echo "  make ci-backend            - Backend CI (lint + tests + analyzers + vuln + enterprise build)"
	@echo "  make ci-backend-matrix     - Backend CI + all 6 profile builds"
	@echo "  make ci-frontend-admin     - Admin SPA CI (typecheck + lint + tests + build + audit)"
	@echo "  make ci-frontend-client    - Client SPA CI (typecheck + lint + build)"
	@echo "  make ci-mobile             - Flutter CI (analyze + test)"
	@echo ""
	@echo "Scope detection uses BASE_REF (default: origin/dev)."
	@echo "  BASE_REF=origin/main make ci"
