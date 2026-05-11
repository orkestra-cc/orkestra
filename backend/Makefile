.PHONY: help run build test clean dev deps profiles
.PHONY: build-starter build-minimal build-billing build-ai build-saas build-enterprise

# Build-tag sets per profile. Each profile is closed under module
# dependencies — see catalog.go for the runtime topo sort that enforces
# this at boot.
TAGS_STARTER    := no_addons
TAGS_MINIMAL    := no_addons addon_dev
TAGS_BILLING    := no_addons addon_billing addon_documents addon_company addon_dev
TAGS_AI         := no_addons addon_graph addon_aimodels addon_rag addon_agents addon_sales addon_dev
TAGS_SAAS       := no_addons addon_subscriptions addon_payments addon_compliance addon_identity addon_dev
# enterprise = no tags — every addon's catalog file matches !no_addons.

help:
	@echo "Available commands:"
	@echo "  make run               - Run the server"
	@echo "  make build             - Build the server binary (enterprise — all addons)"
	@echo "  make test              - Run tests"
	@echo "  make clean             - Clean build artifacts"
	@echo "  make dev               - Run server in development mode with hot reload"
	@echo "  make deps              - Download dependencies"
	@echo ""
	@echo "Profile builds (smaller binaries — pick one):"
	@echo "  make build-starter     - Core modules only"
	@echo "  make build-minimal     - Core + dev (what docker-compose.minimal.yml expects)"
	@echo "  make build-billing     - Italian invoicing SKU (billing+documents+company)"
	@echo "  make build-ai          - AI suite (graph+aimodels+rag+agents+sales)"
	@echo "  make build-saas        - Tier-2 SaaS SKU (subscriptions+payments+compliance+identity)"
	@echo "  make build-enterprise  - All addons (alias for 'make build')"
	@echo "  make profiles          - Print resolved tag sets for each profile"

run:
	go run ./cmd/server

# Note: `./cmd/server` (package form) — `cmd/server/main.go` excludes
# the sibling files (catalog.go, middleware.go, health.go, hostmux.go,
# catalog_<addon>.go) and the build fails.
build:
	go build -o bin/server ./cmd/server

build-starter:
	go build -tags "$(TAGS_STARTER)" -o bin/server-starter ./cmd/server
	@echo "Built bin/server-starter (tags: $(TAGS_STARTER))"

build-minimal:
	go build -tags "$(TAGS_MINIMAL)" -o bin/server-minimal ./cmd/server
	@echo "Built bin/server-minimal (tags: $(TAGS_MINIMAL))"

build-billing:
	go build -tags "$(TAGS_BILLING)" -o bin/server-billing ./cmd/server
	@echo "Built bin/server-billing (tags: $(TAGS_BILLING))"

build-ai:
	go build -tags "$(TAGS_AI)" -o bin/server-ai ./cmd/server
	@echo "Built bin/server-ai (tags: $(TAGS_AI))"

build-saas:
	go build -tags "$(TAGS_SAAS)" -o bin/server-saas ./cmd/server
	@echo "Built bin/server-saas (tags: $(TAGS_SAAS))"

build-enterprise: build

profiles:
	@echo "starter:    $(TAGS_STARTER)"
	@echo "minimal:    $(TAGS_MINIMAL)"
	@echo "billing:    $(TAGS_BILLING)"
	@echo "ai:         $(TAGS_AI)"
	@echo "saas:       $(TAGS_SAAS)"
	@echo "enterprise: (no tags — all addons)"

# Tests always run with the default (no tag) build so addon test files
# in internal/addons/* always compile, regardless of profile.
test:
	go test ./...

clean:
	rm -rf bin/

dev:
	@which air > /dev/null 2>&1 || (echo "Installing air..." && go install github.com/air-verse/air@latest)
	@PATH="$${HOME}/go/bin:$${PATH}" air

deps:
	go mod download
	go mod tidy

docker-build:
	docker build -t orkestra-backend:latest .

docker-run:
	docker run -p 3000:3000 --env-file .env orkestra-backend:latest
