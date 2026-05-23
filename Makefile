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
.PHONY: backend-build backend-test sdk-test addon-documents-test addon-aimodels-test addon-company-test addon-graph-test addon-sales-test addon-subscriptions-test addon-payments-test addon-billing-test addon-dev-test addon-compliance-test addon-identity-test addon-rag-test openapi-auth-test backend-deps backend-clean
.PHONY: frontend-admin-build frontend-admin-test frontend-admin-deps
.PHONY: frontend-admin-clean frontend-admin-preview frontend-admin-type-check
.PHONY: frontend-client-build frontend-client-typecheck frontend-client-clean

# Default target ---------------------------------------------------------

help:
	@echo "Orkestra — common make targets:"
	@echo ""
	@echo "First-time setup:"
	@echo "  make init                - Scaffold docker/.env with random secrets + RS256 JWT keys"
	@echo "  make init-force          - Re-init even if .env / keys exist (invalidates tokens)"
	@echo "  make init-yes            - Non-interactive init for CI / scripted environments"
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

backend-test: sdk-test openapi-auth-test addon-documents-test addon-aimodels-test addon-company-test addon-graph-test addon-sales-test addon-subscriptions-test addon-payments-test addon-billing-test addon-dev-test addon-compliance-test addon-identity-test addon-rag-test
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

# addon-graph-test mirrors addon-company-test for the graph addon,
# which Phase 5d carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-graph).
addon-graph-test:
	@echo "Running graph addon tests..."
	@cd backend/internal/addons/graph && go test ./...

# addon-sales-test mirrors addon-graph-test for the sales addon,
# which Phase 5e carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-sales).
addon-sales-test:
	@echo "Running sales addon tests..."
	@cd backend/internal/addons/sales && go test ./...

# addon-subscriptions-test mirrors addon-sales-test for the
# subscriptions addon, which Phase 5f carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-subscriptions).
addon-subscriptions-test:
	@echo "Running subscriptions addon tests..."
	@cd backend/internal/addons/subscriptions && go test ./...

# addon-payments-test mirrors addon-subscriptions-test for the
# payments addon, which Phase 5g carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-payments).
addon-payments-test:
	@echo "Running payments addon tests..."
	@cd backend/internal/addons/payments && go test ./...

# addon-billing-test mirrors addon-payments-test for the billing
# addon, which Phase 5h carved into its own Go module
# (github.com/orkestra-cc/orkestra-addon-billing). Largest of the
# carve-outs at ~15K LOC; depends on orkestra-openapi-auth (extracted
# in Phase 5c) for the OpenAPI.it OAuth minter.
addon-billing-test:
	@echo "Running billing addon tests..."
	@cd backend/internal/addons/billing && go test ./...

# addon-dev-test mirrors addon-billing-test for the dev addon
# (Phase 5i — first of the "hard tier" extractions). Tiny addon (~600
# LOC) but the test exercises the audience-routing dispatch table.
addon-dev-test:
	@echo "Running dev addon tests..."
	@cd backend/internal/addons/dev && go test ./...

# addon-compliance-test mirrors addon-dev-test for the compliance
# addon (Phase 5j). Covers the DSR pipeline (PIIProducer fan-out +
# erase gate), the SOC2 evidence aggregator, and the audit sink.
addon-compliance-test:
	@echo "Running compliance addon tests..."
	@cd backend/internal/addons/compliance && go test ./...

# addon-identity-test mirrors addon-compliance-test for the identity
# addon (Phase 5k — second hard-tier extraction). Covers the OIDC
# orchestration (via mock_oidc_harness) and SCIM 2.0 types.
addon-identity-test:
	@echo "Running identity addon tests..."
	@cd backend/internal/addons/identity && go test ./...

# addon-rag-test mirrors addon-identity-test for the rag addon
# (Phase 5l — final addon extraction in the SDK split series).
# Covers the markdown parser test suite and the chunker / model-
# service unit tests.
addon-rag-test:
	@echo "Running rag addon tests..."
	@cd backend/internal/addons/rag && go test ./...

backend-deps:
	@echo "Installing backend dependencies..."
	@cd backend && go mod download && go mod tidy
	@cd backend/pkg/sdk && go mod download && go mod tidy
	@cd backend/internal/shared/openapiauth && go mod download && go mod tidy
	@cd backend/internal/addons/documents && go mod download && go mod tidy
	@cd backend/internal/addons/aimodels && go mod download && go mod tidy
	@cd backend/internal/addons/company && go mod download && go mod tidy
	@cd backend/internal/addons/graph && go mod download && go mod tidy
	@cd backend/internal/addons/sales && go mod download && go mod tidy
	@cd backend/internal/addons/subscriptions && go mod download && go mod tidy
	@cd backend/internal/addons/payments && go mod download && go mod tidy
	@cd backend/internal/addons/billing && go mod download && go mod tidy
	@cd backend/internal/addons/dev && go mod download && go mod tidy
	@cd backend/internal/addons/compliance && go mod download && go mod tidy
	@# addon-identity + addon-rag `go mod tidy` require orkestra-sdk v0.4.0
	@# on the Go proxy. Workspace go.work resolves the deps for
	@# build/test, but `tidy` would fail until the SDK tag is indexed by
	@# the proxy — uncomment once orkestra-cc/orkestra-sdk@v0.4.0 is on
	@# the proxy and addon-{identity,rag} have generated go.sum.
	@# @cd backend/internal/addons/identity && go mod download && go mod tidy
	@# @cd backend/internal/addons/rag && go mod download && go mod tidy
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
.PHONY: backend-lint sdk-lint addon-documents-lint addon-aimodels-lint addon-company-lint addon-graph-lint addon-sales-lint addon-subscriptions-lint addon-payments-lint addon-billing-lint addon-dev-lint addon-compliance-lint addon-identity-lint addon-rag-lint openapi-auth-lint backend-test-ci sdk-test-ci addon-documents-test-ci addon-aimodels-test-ci addon-company-test-ci addon-graph-test-ci addon-sales-test-ci addon-subscriptions-test-ci addon-payments-test-ci addon-billing-test-ci addon-dev-test-ci addon-compliance-test-ci addon-identity-test-ci addon-rag-test-ci openapi-auth-test-ci backend-tenantscope backend-policycoverage backend-vulncheck backend-build-enterprise backend-openapi-check
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

# Bootstrap docker/.env (random secrets) + JWT keys for a fresh checkout.
# Idempotent — preserves existing files unless --force is passed.
# Implementation lives in scripts/init.sh so orkestra.sh can call the
# same logic via `./orkestra.sh init`.
init:
	@bash scripts/init.sh

init-force:
	@bash scripts/init.sh --force

# CI-friendly init — answers "yes" to every prompt (no overwrite of existing
# files unless paired with --force, just suppresses the interactive prompt
# when stdin isn't a TTY). Equivalent to `bash scripts/init.sh --yes`.
init-yes:
	@bash scripts/init.sh --yes

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

ci-backend: backend-lint sdk-lint openapi-auth-lint addon-documents-lint addon-aimodels-lint addon-company-lint addon-graph-lint addon-sales-lint addon-subscriptions-lint addon-payments-lint addon-billing-lint addon-dev-lint addon-compliance-lint addon-identity-lint addon-rag-lint backend-tenantscope backend-policycoverage backend-vulncheck backend-test-ci backend-build-enterprise backend-openapi-check
	@echo "Backend CI: OK"

# backend-openapi-check fails if the committed openapi/enterprise.json drifted
# from the routes in the current source — same gate as policycoverage but for
# the API surface that docs.orkestra.cc renders. Requires Mongo+Redis; CI gets
# them as services on the backend job.
.PHONY: backend-openapi-check
backend-openapi-check:
	@cd backend && $(MAKE) openapi-check

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

# addon-graph-lint mirrors addon-company-lint for the graph addon's
# own Go module (Phase 5d). Shares the backend lint config.
addon-graph-lint:
	@cd backend/internal/addons/graph && golangci-lint run --config=../../../.golangci.yml ./...

# addon-sales-lint mirrors addon-graph-lint for the sales addon's
# own Go module (Phase 5e). Shares the backend lint config.
addon-sales-lint:
	@cd backend/internal/addons/sales && golangci-lint run --config=../../../.golangci.yml ./...

# addon-subscriptions-lint mirrors addon-sales-lint for the
# subscriptions addon's own Go module (Phase 5f). Shares the lint
# config.
addon-subscriptions-lint:
	@cd backend/internal/addons/subscriptions && golangci-lint run --config=../../../.golangci.yml ./...

# addon-payments-lint mirrors addon-subscriptions-lint for the
# payments addon's own Go module (Phase 5g). Shares the lint config.
addon-payments-lint:
	@cd backend/internal/addons/payments && golangci-lint run --config=../../../.golangci.yml ./...

# addon-billing-lint mirrors addon-payments-lint for the billing
# addon's own Go module (Phase 5h). Shares the lint config.
addon-billing-lint:
	@cd backend/internal/addons/billing && golangci-lint run --config=../../../.golangci.yml ./...

# addon-dev-lint mirrors addon-billing-lint for the dev addon's own
# Go module (Phase 5i). Shares the lint config.
addon-dev-lint:
	@cd backend/internal/addons/dev && golangci-lint run --config=../../../.golangci.yml ./...

# addon-compliance-lint mirrors addon-dev-lint for the compliance
# addon's own Go module (Phase 5j). Shares the lint config.
addon-compliance-lint:
	@cd backend/internal/addons/compliance && golangci-lint run --config=../../../.golangci.yml ./...

# addon-identity-lint mirrors addon-compliance-lint for the identity
# addon's own Go module (Phase 5k). Shares the lint config.
addon-identity-lint:
	@cd backend/internal/addons/identity && golangci-lint run --config=../../../.golangci.yml ./...

# addon-rag-lint mirrors addon-identity-lint for the rag addon's
# own Go module (Phase 5l). Shares the lint config.
addon-rag-lint:
	@cd backend/internal/addons/rag && golangci-lint run --config=../../../.golangci.yml ./...

# `-race` requires cgo. CI runners have CGO_ENABLED=1 by default, but the
# Go toolchain mise installs locally defaults to 0 — force it on so local
# and CI behave the same. Needs a working C compiler on PATH (gcc/clang).
backend-test-ci: sdk-test-ci openapi-auth-test-ci addon-documents-test-ci addon-aimodels-test-ci addon-company-test-ci addon-graph-test-ci addon-sales-test-ci addon-subscriptions-test-ci addon-payments-test-ci addon-billing-test-ci addon-dev-test-ci addon-compliance-test-ci addon-identity-test-ci addon-rag-test-ci
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

# addon-graph-test-ci mirrors addon-company-test-ci for the graph
# addon (Phase 5d). The addon ships with no tests yet — see above.
addon-graph-test-ci:
	@cd backend/internal/addons/graph && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/graph && go tool cover -func=coverage.out | tail -1

# addon-sales-test-ci mirrors addon-graph-test-ci for the sales addon
# (Phase 5e). The addon ships with several `models` and `services`
# package tests that exercise the prompt loader, scorer, scraper
# helpers, and report generator.
addon-sales-test-ci:
	@cd backend/internal/addons/sales && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/sales && go tool cover -func=coverage.out | tail -1

# addon-subscriptions-test-ci mirrors addon-sales-test-ci for the
# subscriptions addon (Phase 5f). Covers the renewal pipeline, the
# entitlement syncer, and the self-service MeList tenant-fan-out.
addon-subscriptions-test-ci:
	@cd backend/internal/addons/subscriptions && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/subscriptions && go tool cover -func=coverage.out | tail -1

# addon-payments-test-ci mirrors addon-subscriptions-test-ci for the
# payments addon (Phase 5g). Covers the client-handler fan-out tests
# for /v1/me/transactions and /v1/me/payment-methods.
addon-payments-test-ci:
	@cd backend/internal/addons/payments && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/payments && go tool cover -func=coverage.out | tail -1

# addon-billing-test-ci mirrors addon-payments-test-ci for the
# billing addon (Phase 5h). Largest carved addon by LOC; covers the
# FatturaPA XML builder, party/invoice/notification model tests, and
# the OpenAPI config parser.
addon-billing-test-ci:
	@cd backend/internal/addons/billing && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/billing && go tool cover -func=coverage.out | tail -1

# addon-dev-test-ci mirrors addon-billing-test-ci for the dev addon
# (Phase 5i). Covers the audience-routing dispatch table for the
# generate-token endpoint.
addon-dev-test-ci:
	@cd backend/internal/addons/dev && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/dev && go tool cover -func=coverage.out | tail -1

# addon-compliance-test-ci mirrors addon-dev-test-ci for the
# compliance addon (Phase 5j). Covers the DSR + audit sink + SOC2
# evidence services.
addon-compliance-test-ci:
	@cd backend/internal/addons/compliance && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/compliance && go tool cover -func=coverage.out | tail -1

# addon-identity-test-ci mirrors addon-compliance-test-ci for the
# identity addon (Phase 5k). Covers the OIDC mock harness flow + SCIM
# types.
addon-identity-test-ci:
	@cd backend/internal/addons/identity && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/identity && go tool cover -func=coverage.out | tail -1

# addon-rag-test-ci mirrors addon-identity-test-ci for the rag addon
# (Phase 5l — final addon extraction). Covers the goldmark markdown
# parser, the structural chunker, and the model-service unit tests.
addon-rag-test-ci:
	@cd backend/internal/addons/rag && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend/internal/addons/rag && go tool cover -func=coverage.out | tail -1

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
