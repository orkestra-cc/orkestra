.PHONY: help run build test clean dev deps
.PHONY: openapi-dump openapi-check

help:
	@echo "Available commands:"
	@echo "  make run               - Run the server"
	@echo "  make build             - Build the server binary (every addon compiled in)"
	@echo "  make test              - Run tests"
	@echo "  make clean             - Clean build artifacts"
	@echo "  make dev               - Run server in development mode with hot reload"
	@echo "  make deps              - Download dependencies"
	@echo ""
	@echo "Runtime addon enablement on a fresh install is controlled by"
	@echo "ORKESTRA_PROFILE (minimal | full); on subsequent boots the"
	@echo "module_configs document edited at /admin/modules is authoritative."
	@echo ""
	@echo "OpenAPI spec (for docs.orkestra.cc /api):"
	@echo "  make openapi-dump      - Regenerate openapi/enterprise.json"
	@echo "  make openapi-check     - Fail if committed spec is out of date with code (CI)"

run:
	go run ./cmd/server

# Note: `./cmd/server` (package form) — `cmd/server/main.go` excludes
# the sibling files (catalog.go, middleware.go, health.go, hostmux.go,
# catalog_<addon>.go) and the build fails.
build:
	go build -o bin/server ./cmd/server

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

# Regenerate openapi/enterprise.json from the current source.
# The dump runs the server with OPENAPI_DUMP=1 set so cmd/server
# serializes huma.API.OpenAPI() to disk and exits before binding a listener.
# Requires MongoDB + Redis from docker/docker-compose.infra.yml to be running.
openapi-dump:
	@./scripts/openapi-dump.sh

# CI gate — fails if a route change wasn't accompanied by a regenerated spec.
openapi-check: openapi-dump
	@if ! git diff --quiet -- openapi/; then \
		echo ""; \
		echo "✗ openapi/*.json is out of date with the current routes."; \
		echo "  Run 'make openapi-dump' and commit the resulting changes."; \
		echo ""; \
		git --no-pager diff --stat -- openapi/; \
		exit 1; \
	fi
	@echo "✓ openapi/*.json matches the current source."
