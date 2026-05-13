# Orkestra Project Management
#
# This Makefile is split into two halves:
#
#   1. Thin wrappers over ./orkestra.sh — the canonical entrypoint for
#      stack lifecycle (deploy, stop, status, logs). orkestra.sh handles
#      profile selection, env detection, health checks, and the interactive
#      TUI. For anything beyond the targets exposed here, call it directly.
#
#   2. CI parity (further down) — single source of truth for "what is a
#      passing build." Both `make ci` and GitHub Actions invoke these.

.PHONY: help up down status logs reset
.PHONY: mongo-shell redis-cli
.PHONY: backend-build backend-test sdk-test addon-documents-test addon-aimodels-test addon-company-test openapi-auth-test backend-deps backend-clean
.PHONY: frontend-admin-build frontend-admin-test frontend-admin-deps
.PHONY: frontend-admin-clean frontend-admin-preview frontend-admin-type-check
.PHONY: frontend-client-build frontend-client-typecheck frontend-client-clean

# Default target ---------------------------------------------------------

help:
	@echo "Orkestra — common make targets:"
	@echo ""
	@echo "Stack lifecycle (wrappers over ./orkestra.sh):"
	@echo "  make up                  - Launch the interactive TUI to deploy a stack"
	@echo "  make down                - Stop application services + infra (volumes kept)"
	@echo "  make status              - Show containers, /health, and resources"
	@echo "  make logs SVC=<name>     - Follow logs for a service (e.g. SVC=orkestra-backend-dev)"
	@echo "  make reset               - Wipe minimal-profile volumes and redeploy"
	@echo ""
	@echo "  For more lifecycle commands (per-profile deploy, scoped rebuilds, etc.)"
	@echo "  run ./orkestra.sh --help"
	@echo ""
	@echo "Local build / test escape hatches:"
	@echo "  make backend-build       - Build backend binary on the host go toolchain"
	@echo "  make backend-test        - Run backend unit tests (no race, no coverage)"
	@echo "  make backend-deps        - go mod download + tidy"
	@echo "  make backend-clean       - Remove backend/bin/"
	@echo "  make frontend-admin-*    - build / test / preview / type-check / deps / clean"
	@echo "  make frontend-client-*   - build / typecheck / clean"
	@echo ""
	@echo "Database shells (require infra running):"
	@echo "  make mongo-shell         - mongosh into orkestra-mongodb"
	@echo "  make redis-cli           - redis-cli into orkestra-redis"
	@echo ""
	@echo "CI parity (matches GitHub Actions):"
	@echo "  make ci                  - Run checks for changed surfaces only"
	@echo "  make ci-all              - Run every surface (what CI does on dev/main)"
	@echo "  make ci-help             - Detailed CI target list"

# Stack lifecycle --------------------------------------------------------
#
# These targets are thin wrappers around ./orkestra.sh. The script owns
# profile selection, env detection (docker/.env), health gates, and the
# interactive TUI — duplicating any of that here would invite drift.

up:
	@./orkestra.sh

down:
	@./orkestra.sh stop --with-infra

status:
	@./orkestra.sh status

logs:
	@if [ -z "$(SVC)" ]; then \
	  echo "Usage: make logs SVC=<service>  (e.g. SVC=orkestra-backend-dev)"; \
	  exit 1; \
	fi
	@./orkestra.sh logs $(SVC) -f

reset:
	@./orkestra.sh minimal reset --yes

# Database access (requires infra running) -------------------------------

mongo-shell:
	@echo "Connecting to MongoDB..."
	docker exec -it orkestra-mongodb mongosh -u $${MONGO_ROOT_USER:-admin} -p $${MONGO_ROOT_PASSWORD:-orkestra_mongo_admin_2024} --authenticationDatabase admin

redis-cli:
	@echo "Connecting to Redis..."
	docker exec -it orkestra-redis redis-cli -a $${REDIS_PASSWORD:-orkestra_redis_secure_2024}

# Backend (host toolchain escape hatches) --------------------------------
#
# The canonical dev loop runs the backend in Docker via AIR (see
# orkestra.sh / docker-compose.dev.yml). These targets exist only for
# one-shot host-side builds and tests — they don't start a server.

backend-build:
	@echo "Building backend binary..."
	@cd backend && go build -o bin/server cmd/server/main.go
	@echo "Backend built: backend/bin/server"

backend-test: sdk-test openapi-auth-test addon-documents-test addon-aimodels-test addon-company-test
	@echo "Running backend tests..."
	@cd backend && go test ./...

# sdk-test exercises the pkg/sdk/ workspace module separately — it lives
# in its own go.mod so `cd backend && go test ./...` does not include
# its packages. Run by backend-test and backend-test-ci as a dependency.
sdk-test:
	@echo "Running SDK tests..."
	@cd backend/pkg/sdk && go test ./...

# addon-documents-test mirrors sdk-test for the documents addon, which
# Phase 5a carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-documents). Source still lives
# in-tree at backend/internal/addons/documents/ behind a `replace`
# directive; the test target runs from that directory so the workspace
# resolves the dependency graph correctly.
addon-documents-test:
	@echo "Running documents addon tests..."
	@cd backend/internal/addons/documents && go test ./...

# addon-aimodels-test mirrors addon-documents-test for the aimodels
# addon, which Phase 5b carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-aimodels).
addon-aimodels-test:
	@echo "Running aimodels addon tests..."
	@cd backend/internal/addons/aimodels && go test ./...

# openapi-auth-test mirrors sdk-test for the openapiauth shared helper,
# which Phase 5c carved into its own Go module
# (github.com/orkestra-cc/orkestra-openapi-auth) so the company and
# billing addons can both import it across module boundaries.
openapi-auth-test:
	@echo "Running openapi-auth tests..."
	@cd backend/internal/shared/openapiauth && go test ./...

# addon-company-test mirrors addon-aimodels-test for the company
# addon, which Phase 5c carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-company).
addon-company-test:
	@echo "Running company addon tests..."
	@cd backend/internal/addons/company && go test ./...

backend-deps:
	@echo "Installing backend dependencies..."
	@cd backend && go mod download && go mod tidy
	@cd backend/pkg/sdk && go mod download && go mod tidy
	@cd backend/internal/shared/openapiauth && go mod download && go mod tidy
	@cd backend/internal/addons/documents && go mod download && go mod tidy
	@cd backend/internal/addons/aimodels && go mod download && go mod tidy
	@# addon-company `go mod tidy` requires orkestra-openapi-auth v0.1.0 on
	@# the Go proxy; until that mirror is published the workspace go.work
	@# resolves company's deps for build/test, but `tidy` would fail. Add
	@# this line once openapi-auth v0.1.0 is tagged on its public repo.
	@# @cd backend/internal/addons/company && go mod download && go mod tidy
	@echo "Backend dependencies installed."

backend-clean:
	@echo "Cleaning backend build artifacts..."
	@rm -rf backend/bin/
	@echo "Backend artifacts cleaned."

# Frontend admin (host npm escape hatches) -------------------------------

frontend-admin-build:
	@cd frontend-admin && npm run build

frontend-admin-preview:
	@cd frontend-admin && npm run preview

frontend-admin-test:
	@cd frontend-admin && npm test

frontend-admin-type-check:
	@cd frontend-admin && npm run typecheck

frontend-admin-deps:
	@cd frontend-admin && npm install

frontend-admin-clean:
	@rm -rf frontend-admin/dist/ frontend-admin/node_modules/.vite/

# Frontend client (host npm escape hatches) ------------------------------

frontend-client-build:
	@cd frontend-client && npm run build

frontend-client-typecheck:
	@cd frontend-client && npm run typecheck

frontend-client-clean:
	@rm -rf frontend-client/dist/ frontend-client/node_modules/.vite/

# ============================================================================
# CI parity — single source of truth for "what is a passing build."
# Both contributors (`make ci`) and GitHub Actions (.github/workflows/*.yml)
# invoke these targets, so local and CI cannot drift. See CONTRIBUTING.md.
# ============================================================================

.PHONY: install install-hooks fmt ci-help
.PHONY: ci ci-all ci-backend ci-backend-matrix ci-frontend-admin ci-frontend-client ci-mobile
.PHONY: backend-lint sdk-lint addon-documents-lint addon-aimodels-lint addon-company-lint openapi-auth-lint backend-test-ci sdk-test-ci addon-documents-test-ci addon-aimodels-test-ci addon-company-test-ci openapi-auth-test-ci backend-tenantscope backend-policycoverage backend-vulncheck backend-build-enterprise
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

ci-backend: backend-lint sdk-lint openapi-auth-lint addon-documents-lint addon-aimodels-lint addon-company-lint backend-tenantscope backend-policycoverage backend-vulncheck backend-test-ci backend-build-enterprise
	@echo "Backend CI: OK"

ci-backend-matrix: ci-backend
	@cd backend && $(MAKE) build-starter build-minimal build-billing build-ai build-saas build-enterprise

backend-lint:
	@cd backend && golangci-lint run --config=.golangci.yml

# sdk-lint runs golangci-lint against the workspace SDK module separately —
# its own go.mod means `cd backend && golangci-lint run ./...` does not
# cover its packages. Shares the backend lint config to keep settings
# unified until the SDK extracts to its own repo (Phase 4).
sdk-lint:
	@cd backend/pkg/sdk && golangci-lint run --config=../../.golangci.yml ./...

# addon-documents-lint mirrors sdk-lint for the documents addon's own
# Go module. Shares the backend lint config.
addon-documents-lint:
	@cd backend/internal/addons/documents && golangci-lint run --config=../../../.golangci.yml ./...

# addon-aimodels-lint mirrors addon-documents-lint for the aimodels
# addon's own Go module (Phase 5b). Shares the backend lint config.
addon-aimodels-lint:
	@cd backend/internal/addons/aimodels && golangci-lint run --config=../../../.golangci.yml ./...

# openapi-auth-lint mirrors sdk-lint for the openapiauth shared helper's
# own Go module (Phase 5c). Shares the backend lint config.
openapi-auth-lint:
	@cd backend/internal/shared/openapiauth && golangci-lint run --config=../../../.golangci.yml ./...

# addon-company-lint mirrors addon-aimodels-lint for the company
# addon's own Go module (Phase 5c). Shares the backend lint config.
addon-company-lint:
	@cd backend/internal/addons/company && golangci-lint run --config=../../../.golangci.yml ./...

# `-race` requires cgo. CI runners have CGO_ENABLED=1 by default, but the
# Go toolchain mise installs locally defaults to 0 — force it on so local
# and CI behave the same. Needs a working C compiler on PATH (gcc/clang).
backend-test-ci: sdk-test-ci openapi-auth-test-ci addon-documents-test-ci addon-aimodels-test-ci addon-company-test-ci
	@cd backend && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend && go tool cover -func=coverage.out | tail -1

# sdk-test-ci mirrors backend-test-ci for the workspace SDK module.
sdk-test-ci:
	@cd backend/pkg/sdk && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/pkg/sdk && go tool cover -func=coverage.out | tail -1

# addon-documents-test-ci mirrors sdk-test-ci for the documents addon.
addon-documents-test-ci:
	@cd backend/internal/addons/documents && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/documents && go tool cover -func=coverage.out | tail -1

# addon-aimodels-test-ci mirrors addon-documents-test-ci for the
# aimodels addon (Phase 5b).
addon-aimodels-test-ci:
	@cd backend/internal/addons/aimodels && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/aimodels && go tool cover -func=coverage.out | tail -1

# openapi-auth-test-ci mirrors sdk-test-ci for the openapiauth shared
# helper's own Go module (Phase 5c).
openapi-auth-test-ci:
	@cd backend/internal/shared/openapiauth && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/shared/openapiauth && go tool cover -func=coverage.out | tail -1

# addon-company-test-ci mirrors addon-aimodels-test-ci for the company
# addon (Phase 5c). The addon ships with no tests yet — the package
# list will still report 0% coverage rather than failing.
addon-company-test-ci:
	@cd backend/internal/addons/company && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/company && go tool cover -func=coverage.out | tail -1

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
