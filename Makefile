# Orkestra Project Management
#
# This Makefile is split into two halves:
#
#   1. Thin wrappers over ./orkestra.sh — the canonical entrypoint for
#      stack lifecycle (deploy, stop, status, logs). orkestra.sh handles
#      env detection, health checks, and the interactive
#      TUI. For anything beyond the targets exposed here, call it directly.
#
#   2. CI parity (further down) — single source of truth for "what is a
#      passing build." Both `make ci` and GitHub Actions invoke these.

.PHONY: help up down status logs
.PHONY: mongo-shell redis-cli
.PHONY: backend-build backend-test backend-deps backend-clean
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
	@echo ""
	@echo "  For more lifecycle commands (scoped rebuilds, observability, etc.)"
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
# env detection (docker/.env), health gates, and the
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

# Database access (requires infra running) -------------------------------

mongo-shell:
	@echo "Connecting to MongoDB..."
	docker exec -it orkestra-mongodb mongosh -u $${MONGO_ROOT_USER:-admin} -p $${MONGO_ROOT_PASSWORD:-orkestra_mongo_admin_2024} --authenticationDatabase admin

redis-cli:
	@echo "Connecting to Redis..."
	docker exec -it orkestra-redis redis-cli -a $${REDIS_PASSWORD:-orkestra_redis_secure_2024}

# Plain-bash tests for the env helpers and the validator's secret-hygiene
# rules (scripts/test-env-file.sh, scripts/test-env-validate.sh). Part of
# `make ci-backend`: the validator is what gates a staging/production deploy.
backend-script-tests:
	@bash -n scripts/env-file.sh scripts/env-validate.sh scripts/test-env-file.sh scripts/test-env-validate.sh
	@./scripts/test-env-file.sh
	@./scripts/test-env-validate.sh

# Backend (host toolchain escape hatches) --------------------------------
#
# The canonical dev loop runs the backend in Docker via AIR (see
# orkestra.sh / docker-compose.dev.yml). These targets exist only for
# one-shot host-side builds and tests — they don't start a server.

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
.PHONY: ci ci-all ci-mcp ci-backend ci-frontend-admin ci-frontend-client ci-mobile
.PHONY: mcp-check mcp-test
.PHONY: backend-lint backend-test-ci backend-tenantscope backend-errquality backend-policycoverage backend-piiscan backend-vulncheck backend-build-ci backend-openapi-check backend-coverage-gate backend-mongo-config backend-credential-fallbacks backend-script-tests
.PHONY: admin-lockcheck admin-typecheck admin-lint admin-test admin-audit admin-build
.PHONY: client-lockcheck client-typecheck client-lint client-test client-build
.PHONY: mobile-lockcheck
.PHONY: mobile-analyze mobile-test

# Detect changed surfaces vs $(BASE_REF). Override with `BASE_REF=origin/main make ci`.
BASE_REF ?= origin/dev
SINCE := $(shell git merge-base HEAD $(BASE_REF) 2>/dev/null || echo HEAD~1)
CI_CHANGED := $(shell { git diff --name-only $(SINCE)...HEAD 2>/dev/null; git diff --name-only 2>/dev/null; git diff --name-only --cached 2>/dev/null; } | sort -u)
BACKEND_CHANGED := $(if $(filter backend/% scripts/env-file.sh scripts/env-validate.sh scripts/test-env-file.sh scripts/test-env-validate.sh docker/docker-compose.% docker/.env.example docker/tests/%,$(CI_CHANGED)),1,)
ADMIN_CHANGED   := $(if $(filter frontend-admin/%,$(CI_CHANGED)),1,)
CLIENT_CHANGED  := $(if $(filter frontend-client/%,$(CI_CHANGED)),1,)
MOBILE_CHANGED  := $(if $(filter mobile/%,$(CI_CHANGED)),1,)
MCP_CHANGED     := $(if $(filter .mcp.json .codex/config.toml .github/workflows/mcp-config.yml scripts/check-mcp-sync.py scripts/test-mcp-sync.py,$(CI_CHANGED)),1,)

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

# Minimum total backend statement coverage. CI used to enforce this in the
# workflow, after `make ci-backend` had already returned OK — so the gate did
# not exist locally at all. It lives here now; the workflow may still override
# the value via the environment.
COVERAGE_THRESHOLD ?= 15

# A lockfile that disagrees with its manifest is invisible locally: every check
# below runs against whatever node_modules already exists, while CI installs
# with `npm ci` first and fails outright. A lockfile can therefore sit unable
# to satisfy its own manifest for weeks — nothing local notices, and on a fork
# whose Actions runners are unavailable, nothing notices at all until the next
# production image build.
#
# `npm install --package-lock-only` resolves the tree on paper and rewrites the
# lockfile when it disagrees. No node_modules is touched, so this is safe in a
# working checkout, and the original file is restored either way. npm's own
# exit code is not a usable signal here — it reports failure even on a healthy
# tree — so the rewritten file is what we read.
define npm-lockcheck
	cd $(1) && cp package-lock.json /tmp/orkestra-lockcheck-$(1).bak && \
	{ npm install --package-lock-only --no-audit --no-fund >/dev/null 2>&1 || true; }; \
	if cmp -s /tmp/orkestra-lockcheck-$(1).bak package-lock.json; then \
	  rm -f /tmp/orkestra-lockcheck-$(1).bak; \
	  echo "$(1): package-lock.json is in sync with package.json"; \
	else \
	  cp /tmp/orkestra-lockcheck-$(1).bak package-lock.json; \
	  rm -f /tmp/orkestra-lockcheck-$(1).bak; \
	  echo "FAIL: $(1)/package-lock.json is out of sync with package.json."; \
	  echo "      'npm ci' — what CI and the production image build both run —"; \
	  echo "      would refuse this tree. Your lockfile was left untouched."; \
	  echo "      Fix: cd $(1) && npm install, then commit the lockfile."; \
	  exit 1; \
	fi
endef

# ---- Top-level CI dispatch ----

ci:
	@echo "Detecting changed surfaces vs $(BASE_REF)..."
	@if [ -z "$(BACKEND_CHANGED)$(ADMIN_CHANGED)$(CLIENT_CHANGED)$(MOBILE_CHANGED)$(MCP_CHANGED)" ]; then \
	  echo "  (no surface changes — nothing to check)"; \
	else \
	  [ -n "$(BACKEND_CHANGED)" ] && echo "  - backend"          || true; \
	  [ -n "$(ADMIN_CHANGED)"   ] && echo "  - frontend-admin"   || true; \
	  [ -n "$(CLIENT_CHANGED)"  ] && echo "  - frontend-client"  || true; \
	  [ -n "$(MOBILE_CHANGED)"  ] && echo "  - mobile"           || true; \
	  [ -n "$(MCP_CHANGED)"     ] && echo "  - MCP configuration" || true; \
	fi
	@if [ -n "$(BACKEND_CHANGED)" ]; then $(MAKE) ci-backend;         fi
	@if [ -n "$(ADMIN_CHANGED)"   ]; then $(MAKE) ci-frontend-admin;  fi
	@if [ -n "$(CLIENT_CHANGED)"  ]; then $(MAKE) ci-frontend-client; fi
	@if [ -n "$(MOBILE_CHANGED)"  ]; then $(MAKE) ci-mobile;          fi
	@if [ -n "$(MCP_CHANGED)"     ]; then $(MAKE) ci-mcp;             fi

ci-all: ci-mcp ci-backend ci-frontend-admin ci-frontend-client ci-mobile
	@echo "All surface checks passed."

# ---- Shared MCP configuration ----

ci-mcp: mcp-test mcp-check
	@echo "MCP configuration CI: OK"

mcp-check:
	@python3 scripts/check-mcp-sync.py

mcp-test:
	@python3 scripts/test-mcp-sync.py

# ---- Backend ----

ci-backend: backend-script-tests backend-mongo-config backend-credential-fallbacks backend-lint backend-tenantscope backend-errquality backend-policycoverage backend-piiscan backend-vulncheck backend-test-ci backend-coverage-gate backend-build-ci backend-openapi-check
	@echo "Backend CI: OK"

# Static gate: the compose stacks and CI must all provide a transaction-capable
# MongoDB. This caught a real regression — a merge once kept the capabilities
# the replica entrypoint needs while dropping the entrypoint itself, leaving a
# standalone mongod that failed only at setup-finalization time.
backend-mongo-config:
	@docker/tests/mongodb-replica-set.test.sh

# Static gate: a credential never has a default. The bundled RustFS once fell
# back to a literal root password printed in this repository — on a browser-
# facing S3 API — and nothing said so. Also pins Redis to its volume (--dir).
backend-credential-fallbacks:
	@docker/tests/credential-fallbacks.test.sh

# backend-openapi-check fails if the committed openapi/enterprise.json drifted
# from the routes in the current source — same gate as policycoverage but for
# the API surface that docs.orkestra.cc renders. Requires Mongo+Redis; CI gets
# them as services on the backend job.
.PHONY: backend-openapi-check
backend-openapi-check:
	@cd backend && $(MAKE) openapi-check

backend-lint:
	@cd backend && golangci-lint run --config=.golangci.yml

# `-race` requires cgo. CI runners have CGO_ENABLED=1 by default, but the
# Go toolchain mise installs locally defaults to 0 — force it on so local
# and CI behave the same. Needs a working C compiler on PATH (gcc/clang).
backend-test-ci:
	@cd backend && CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	@cd backend && go tool cover -func=coverage.out | tail -1

backend-tenantscope:
	@cd backend && go test ./tools/tenantscope/...
	@cd backend && go run ./tools/tenantscope/cmd/tenantscope \
	  -baseline=tools/tenantscope/baseline.txt ./internal/...

# backend-errquality fails on any client-facing error response that leaks
# raw error text, says nothing, or reports a server fault as a client
# error. Pre-existing violations are frozen in tools/errquality/baseline.txt;
# new ones fail the build. See docs/superpowers/specs/2026-08-21-backend-error-quality-design.md.
backend-errquality:
	@cd backend && go test ./tools/errquality/...
	@cd backend && go run ./tools/errquality/cmd/errquality \
	  -baseline=tools/errquality/baseline.txt ./internal/...

backend-policycoverage:
	@cd backend && go test ./tools/policycoverage/...
	@cd backend && go run ./tools/policycoverage/cmd/policycoverage \
	  -baseline=tools/policycoverage/baseline.txt \
	  -cedar=internal/core/authz/cedar/policies ./internal/...

# backend-piiscan flags modules that persist data-subject PII (a userUUID-style
# bson field) but register no iface.PIIProducer, so personal data can't silently
# escape the GDPR DSR export/erase sweep (ADR-0009). Baseline carries the
# retained-by-design exceptions (compliance audit/erasure/legal-hold rows).
backend-piiscan:
	@cd backend && go test ./tools/piiscan/...
	@cd backend && go run ./tools/piiscan/cmd/piiscan \
	  -baseline=tools/piiscan/baseline.txt ./internal/...

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

# Depends on backend-test-ci rather than trusting prerequisite order, so the
# coverage profile it reads is always the one this run produced.
backend-coverage-gate: backend-test-ci
	@cd backend && PCT=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%",""); print $$NF}'); \
	echo "Coverage: $${PCT}%"; \
	awk -v p="$${PCT}" -v t="$(COVERAGE_THRESHOLD)" 'BEGIN { exit !(p+0 >= t+0) }' \
	  || { echo "FAIL: coverage $${PCT}% is below the $(COVERAGE_THRESHOLD)% threshold"; exit 1; }

backend-build-ci:
	@cd backend && $(MAKE) build

# ---- Frontend Admin ----

ci-frontend-admin: admin-lockcheck admin-typecheck admin-lint admin-test admin-audit admin-build
	@echo "Frontend-admin CI: OK"

admin-lockcheck:
	@$(call npm-lockcheck,frontend-admin)

admin-typecheck:
	@cd frontend-admin && npm run typecheck

admin-lint:
	@cd frontend-admin && npx eslint src/ --ext .js,.jsx,.ts,.tsx --max-warnings 0

admin-test:
	@cd frontend-admin && npm run test:coverage

admin-audit:
	# Audit runtime deps only: the admin SPA ships a static bundle, so dev
	# tooling (vitest et al.) never reaches users. A dev-only advisory (e.g. the
	# Vitest-UI-server RCE, only exposed under `vitest --ui` — we run `vitest run`)
	# must not gate a release. See docs/adr/0006 + project_ci_release_blockers.
	@cd frontend-admin && npm audit --omit=dev --audit-level=high

admin-build:
	@cd frontend-admin && npm run build

# ---- Frontend Client ----

ci-frontend-client: client-lockcheck client-typecheck client-lint client-test client-build
	@echo "Frontend-client CI: OK"

client-lockcheck:
	@$(call npm-lockcheck,frontend-client)

client-typecheck:
	@cd frontend-client && npm run typecheck

client-lint:
	@cd frontend-client && npm run lint -- --max-warnings 0

client-test:
	@cd frontend-client && npm test

client-build:
	@cd frontend-client && npm run build

# ---- Mobile ----

ci-mobile: mobile-lockcheck mobile-analyze mobile-test
	@echo "Mobile CI: OK"

# Flutter ships the check natively: --enforce-lockfile fails pub get when
# pubspec.lock cannot satisfy pubspec.yaml, and never rewrites the lockfile.
mobile-lockcheck:
	@cd mobile && flutter pub get --enforce-lockfile

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
	@echo "  make ci-backend            - Backend CI (lint + tests + analyzers + vuln + coverage + build)"
	@echo "  make ci-frontend-admin     - Admin SPA CI (lockfile + typecheck + lint + tests + build + audit)"
	@echo "  make ci-frontend-client    - Client SPA CI (lockfile + typecheck + lint + test + build)"
	@echo "  make ci-mobile             - Flutter CI (lockfile + analyze + test)"
	@echo "  make ci-mcp                - Shared Claude Code/Codex MCP config check"
	@echo "  make mcp-check             - Verify project MCP definitions are in sync"
	@echo ""
	@echo "  make admin-lockcheck       - Is frontend-admin/package-lock.json in sync? (no install)"
	@echo "  make client-lockcheck      - Same for frontend-client"
	@echo "  make mobile-lockcheck      - Same for mobile/pubspec.lock"
	@echo ""
	@echo "Scope detection uses BASE_REF (default: origin/dev)."
	@echo "  BASE_REF=origin/main make ci"
	@echo "  COVERAGE_THRESHOLD=25 make ci-backend   (default: $(COVERAGE_THRESHOLD))"
