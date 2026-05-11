# Module: Scripts - Automation & Development Tools
*Path: `/scripts`*
*Parent: [../CLAUDE.md](../CLAUDE.md)*

<!-- Navigation -->
[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)
<!-- /Navigation -->

## Module Purpose

The scripts module contains **automation scripts, development tools, and utilities** for managing the Orkestra monorepo lifecycle and development workflows.

- **Primary Role**: Development environment automation and CI/CD pipeline support
- **System Integration**: Orchestrates build, test, and deployment processes across all modules
- **Architecture**: Bash scripts with Makefile orchestration for cross-platform compatibility

## Dependencies

### Imports
- **[`/docker/`](../docker/CLAUDE.md)** - Infrastructure and container orchestration
- **All Modules**: Build and deployment targets for backend, frontend, and mobile

### Importers
- **Developers**: Local development environment setup and automation
- **CI/CD Pipelines**: Automated build, test, and deployment processes

## Overview

This module contains automation scripts, development tools, and utilities for managing the Orkestra monorepo. All scripts should be idempotent and include proper error handling.

## 🚫 CRITICAL: Development Workflow

**All stack management lives at project root in `orkestra.sh`.** No management scripts belong in this directory — only utilities called by the main script.

### ✅ Current Development Workflow

```bash
# Single entry point — interactive TUI
./orkestra.sh                         # Profile menu: minimal or full stack

# Same operations via CLI (scriptable)
./orkestra.sh minimal deploy --build
./orkestra.sh minimal logs backend -f
./orkestra.sh minimal reset --yes
ENV=development ./orkestra.sh deploy --scope backend --rebuild --yes
./orkestra.sh logs orkestra-backend-dev -f
./orkestra.sh --help                  # Full command surface
```

`orkestra.sh` handles every docker compose operation for both the minimal profile (`docker-compose.minimal.yml`) and the full-stack dev/staging/prod profiles (`docker-compose.infra.yml` + `docker-compose.{dev,staging,prod}.yml`). See [docker/CLAUDE.md](../docker/CLAUDE.md) for compose-file details.

### 🚫 Removed / Consolidated Scripts

The following scripts used to exist and have been folded into `./orkestra.sh`:

- **deploy.sh** (project root): deploy/stop/status for dev/staging/prod → `./orkestra.sh deploy|stop|status`
- **logs.sh** (project root): interactive log viewer → `./orkestra.sh logs`
- **docker-manage.sh** (scripts/): removed earlier, now `./orkestra.sh`
- **env-switch.sh** (scripts/): removed earlier, now edit `docker/.env` and rerun `./orkestra.sh`
- **start-infra.sh** (scripts/): removed earlier, handled automatically by `./orkestra.sh deploy`

### ✅ Utility Scripts (still in this directory)

These are called by `orkestra.sh` or used directly during development:

- **env-detect.sh**: Sourced by `orkestra.sh` to detect ENV from `docker/.env`
- **env-validate.sh**: Validates environment files (`./scripts/env-validate.sh all`)
- **generate-jwt-keys.sh**: Generates the RS256 JWT key pair
- **install-air.sh**: Installs AIR hot-reload tool
- **devtoken.sh**: Generates dev JWT tokens for testing (`ORKESTRA_API_URL=... ./scripts/devtoken.sh administrator`). Pass `--audience client` (or `-a client`) to mint an `aud=client` token that satisfies the `api.*` surface; default is `operator` (ADR-0003 PR-D D-10).
- **health-check.sh** *(if present)*: Called by `orkestra.sh deploy` post-deployment

## Script Categories

- **Development**: Local development environment setup and management
- **Build**: Build and compilation scripts
- **Testing**: Test execution and coverage reporting
- **Deployment**: Deployment automation
- **Database**: Migration and seeding scripts
- **Monitoring**: Health checks and performance monitoring
- **Utilities**: General purpose helper scripts

## Development Scripts

### setup.sh - Initial Project Setup
```bash
#!/bin/bash
set -e

echo "Setting up orkestra development environment..."

# Check prerequisites
command -v docker >/dev/null 2>&1 || { echo "Docker is required but not installed. Aborting." >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "Go is required but not installed. Aborting." >&2; exit 1; }
command -v node >/dev/null 2>&1 || { echo "Node.js is required but not installed. Aborting." >&2; exit 1; }

# Create necessary directories
mkdir -p logs
mkdir -p data/{mongo,redis,timescale}

# Copy environment template
if [ ! -f .env ]; then
    cp .env.template .env
    echo "Created .env file. Please update with your configuration."
fi

# Install backend dependencies
echo "Installing backend dependencies..."
for service in backend/*/; do
    if [ -f "$service/go.mod" ]; then
        echo "Installing dependencies for $service"
        (cd "$service" && go mod download)
    fi
done

# Install frontend dependencies
echo "Installing frontend dependencies..."
(cd frontend-admin && npm install)

# Install mobile dependencies
echo "Installing mobile dependencies..."
(cd mobile && flutter pub get)

echo "Setup complete! Run './scripts/dev.sh' to start development environment."
```

### dev.sh - Start Development Environment
```bash
#!/bin/bash
set -e

# Load environment variables
source .env

# Function to cleanup on exit
cleanup() {
    echo "Stopping services..."
    docker-compose down
}
trap cleanup EXIT

# Start infrastructure services
echo "Starting infrastructure services..."
docker-compose up -d mongo redis timescale

# Wait for services to be ready
echo "Waiting for services..."
./scripts/wait-for-services.sh

# Run database migrations
echo "Running database migrations..."
./scripts/migrate.sh

# Start backend services
echo "Starting backend services..."
for service in backend/*/; do
    service_name=$(basename "$service")
    echo "Starting $service_name..."
    (cd "$service" && go run cmd/server/main.go) &
done

# Start frontend
echo "Starting frontend..."
(cd frontend-admin && npm run dev) &

echo "Development environment is ready!"
echo "Frontend: http://localhost:3000"
echo "API: http://localhost:8001"

# Wait for user interrupt
wait
```

### test.sh - Run All Tests
```bash
#!/bin/bash
set -e

FAILED_TESTS=0

# Backend tests
echo "Running backend tests..."
for service in backend/*/; do
    if [ -f "$service/go.mod" ]; then
        echo "Testing $(basename $service)..."
        (cd "$service" && go test -v -cover ./...) || FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
done

# Frontend tests
echo "Running frontend tests..."
(cd frontend-admin && npm test) || FAILED_TESTS=$((FAILED_TESTS + 1))

# Mobile tests
echo "Running mobile tests..."
(cd mobile && flutter test) || FAILED_TESTS=$((FAILED_TESTS + 1))

if [ $FAILED_TESTS -gt 0 ]; then
    echo "❌ $FAILED_TESTS test suite(s) failed"
    exit 1
else
    echo "✅ All tests passed"
fi
```

## Build Scripts

### build.sh - Build All Services
```bash
#!/bin/bash
set -e

VERSION=${1:-latest}

echo "Building orkestra services (version: $VERSION)..."

# Build backend services
for service in backend/*/; do
    service_name=$(basename "$service")
    echo "Building $service_name..."
    docker build -t orkestra/$service_name:$VERSION -f $service/Dockerfile $service
done

# Build frontend
echo "Building frontend..."
docker build -t orkestra/frontend-admin:$VERSION -f frontend-admin/Dockerfile frontend-admin

echo "Build complete!"
```

### build-mobile.sh - Build Mobile Apps
```bash
#!/bin/bash
set -e

# Build Android APK
echo "Building Android APK..."
(cd mobile && flutter build apk --release)

# Build iOS IPA
if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "Building iOS IPA..."
    (cd mobile && flutter build ipa --release)
fi

echo "Mobile builds complete!"
echo "Android APK: mobile/build/app/outputs/flutter-apk/app-release.apk"
if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "iOS IPA: mobile/build/ios/ipa/*.ipa"
fi
```

## Database Scripts

### migrate.sh - Run Database Migrations
```bash
#!/bin/bash
set -e

MONGO_URI=${MONGO_URI:-"mongodb://localhost:27017/orkestra"}

echo "Running database migrations..."

# MongoDB migrations
for migration in migrations/mongo/*.js; do
    if [ -f "$migration" ]; then
        echo "Applying migration: $(basename $migration)"
        docker exec orkestra-mongo mongosh $MONGO_URI < $migration
    fi
done

# TimescaleDB migrations
for migration in migrations/timescale/*.sql; do
    if [ -f "$migration" ]; then
        echo "Applying migration: $(basename $migration)"
        docker exec orkestra-timescale psql -U postgres -d orkestra_metrics -f $migration
    fi
done

echo "Migrations complete!"
```

### seed.sh - Seed Database with Test Data
```bash
#!/bin/bash
set -e

MONGO_URI=${MONGO_URI:-"mongodb://localhost:27017/orkestra"}

echo "Seeding database with test data..."

# Seed users
docker exec orkestra-mongo mongosh $MONGO_URI --eval '
db.users.insertMany([
    {
        email: "admin@orkestra.com",
        name: "Admin User",
        roles: ["admin"],
        created_at: new Date()
    },
    {
        email: "operator@orkestra.com",
        name: "Test Operator",
        roles: ["operator"],
        created_at: new Date()
    }
]);
'

# Seed vehicles
docker exec orkestra-mongo mongosh $MONGO_URI --eval '
db.vehicles.insertMany([
    {
        registration_number: "ABC123",
        make: "Ford",
        model: "Transit",
        year: 2023,
        status: "active",
        created_at: new Date()
    },
    {
        registration_number: "XYZ789",
        make: "Mercedes",
        model: "Sprinter",
        year: 2022,
        status: "active",
        created_at: new Date()
    }
]);
'

echo "Database seeded successfully!"
```

## Deployment Scripts

### deploy.sh - Deploy to Environment
```bash
#!/bin/bash
set -e

ENVIRONMENT=${1:-staging}
VERSION=${2:-latest}

if [ "$ENVIRONMENT" != "staging" ] && [ "$ENVIRONMENT" != "production" ]; then
    echo "Invalid environment. Use 'staging' or 'production'"
    exit 1
fi

echo "Deploying version $VERSION to $ENVIRONMENT..."

# Load environment-specific config
source ./config/$ENVIRONMENT.env

# Export version for docker-compose
export VERSION=$VERSION

# Push Docker images
echo "Pushing Docker images..."
for service in auth user operator tracking task; do
    docker push orkestra/$service:$VERSION
done
docker push orkestra/frontend-admin:$VERSION

# Deploy using docker-compose
echo "Deploying with Docker Compose to $ENVIRONMENT..."

# Select compose files based on environment
if [ "$ENVIRONMENT" == "production" ]; then
    COMPOSE_FILES="-f docker-compose.yml -f docker-compose.prod.yml"
    ENV_FILE="--env-file .env.prod"
else
    COMPOSE_FILES="-f docker-compose.yml -f docker-compose.dev.yml"
    ENV_FILE="--env-file .env.dev"
fi

# Pull latest images
docker-compose $COMPOSE_FILES $ENV_FILE pull

# Stop old containers and start new ones
docker-compose $COMPOSE_FILES $ENV_FILE down
docker-compose $COMPOSE_FILES $ENV_FILE up -d

# For production, scale services for high availability
if [ "$ENVIRONMENT" == "production" ]; then
    echo "Scaling services for production..."
    docker-compose $COMPOSE_FILES $ENV_FILE up -d --scale auth-service=3 --scale user-service=2 --scale tracking-service=2
fi

# Health check
echo "Waiting for services to become healthy..."
sleep 10

# Check container status
docker-compose $COMPOSE_FILES $ENV_FILE ps

echo "Deployment complete!"
```

### rollback.sh - Rollback Deployment
```bash
#!/bin/bash
set -e

ENVIRONMENT=${1:-staging}
SERVICE=${2:-all}
PREVIOUS_VERSION=${3:-previous}

echo "Rolling back $SERVICE in $ENVIRONMENT to version $PREVIOUS_VERSION..."

# Load environment-specific config
source ./config/$ENVIRONMENT.env

# Export version for docker-compose
export VERSION=$PREVIOUS_VERSION

# Select compose files based on environment
if [ "$ENVIRONMENT" == "production" ]; then
    COMPOSE_FILES="-f docker-compose.yml -f docker-compose.prod.yml"
    ENV_FILE="--env-file .env.prod"
else
    COMPOSE_FILES="-f docker-compose.yml -f docker-compose.dev.yml"
    ENV_FILE="--env-file .env.dev"
fi

if [ "$SERVICE" == "all" ]; then
    echo "Rolling back all services..."

    # Stop current containers
    docker-compose $COMPOSE_FILES $ENV_FILE down

    # Start with previous version
    docker-compose $COMPOSE_FILES $ENV_FILE up -d

    # Scale for production if needed
    if [ "$ENVIRONMENT" == "production" ]; then
        docker-compose $COMPOSE_FILES $ENV_FILE up -d --scale auth-service=3 --scale user-service=2 --scale tracking-service=2
    fi
else
    echo "Rolling back $SERVICE service..."

    # Stop and remove specific service
    docker-compose $COMPOSE_FILES $ENV_FILE stop $SERVICE
    docker-compose $COMPOSE_FILES $ENV_FILE rm -f $SERVICE

    # Start service with previous version
    docker-compose $COMPOSE_FILES $ENV_FILE up -d $SERVICE

    # Scale if production
    if [ "$ENVIRONMENT" == "production" ]; then
        case $SERVICE in
            auth-service)
                docker-compose $COMPOSE_FILES $ENV_FILE up -d --scale auth-service=3
                ;;
            user-service|tracking-service)
                docker-compose $COMPOSE_FILES $ENV_FILE up -d --scale $SERVICE=2
                ;;
        esac
    fi
fi

# Check container status
docker-compose $COMPOSE_FILES $ENV_FILE ps

echo "Rollback complete!"
```

## Monitoring Scripts

### health-check.sh - Check Service Health
```bash
#!/bin/bash

SERVICES=(
    "http://localhost:8001/health:Auth Service"
    "http://localhost:8002/health:User Service"
    "http://localhost:8003/health:Operator Service"
    "http://localhost:8004/health:Tracking Service"
    "http://localhost:8005/health:Task Service"
    "http://localhost:8006/health:Reporting Service"
)

echo "Checking service health..."

for service in "${SERVICES[@]}"; do
    IFS=':' read -r url name <<< "$service"

    if curl -f -s "$url" > /dev/null; then
        echo "✅ $name is healthy"
    else
        echo "❌ $name is unhealthy"
    fi
done
```

### performance-test.sh - Run Performance Tests with SigNoz
```bash
#!/bin/bash
set -e

echo "Running performance tests with SigNoz monitoring..."

# Install k6 if not present (with xk6-client-tracing for SigNoz)
if ! command -v k6 &> /dev/null; then
    echo "Installing k6 with OpenTelemetry support..."
    go install go.k6.io/xk6/cmd/xk6@latest
    xk6 build --with github.com/grafana/xk6-client-tracing
fi

# Export SigNoz endpoint
export K6_TRACING_ENABLED=true
export K6_OTEL_GRPC_EXPORTER_ENDPOINT=localhost:4317
export K6_OTEL_GRPC_EXPORTER_INSECURE=true

# Run load tests with tracing
for test in tests/load/*.js; do
    if [ -f "$test" ]; then
        echo "Running $(basename $test) with SigNoz tracing..."
        ./k6 run --vus 10 --duration 30s \
            --tag testid=$(date +%s) \
            --tag test_name=$(basename $test .js) \
            $test
    fi
done

echo "Performance tests complete! View results in SigNoz: http://localhost:3301"
```

### signoz-check.sh - Check SigNoz Health
```bash
#!/bin/bash

echo "Checking SigNoz services health..."

# Check SigNoz services
SIGNOZ_SERVICES=(
    "http://localhost:3301:SigNoz Frontend"
    "http://localhost:8080/api/v1/health:Query Service"
    "http://localhost:4318:OTEL Collector HTTP"
    "http://localhost:8888/metrics:OTEL Collector Metrics"
    "http://localhost:8123/ping:ClickHouse"
)

for service in "${SIGNOZ_SERVICES[@]}"; do
    IFS=':' read -r url port name <<< "$service"
    full_url="${url}:${port}"

    if curl -f -s "$full_url" > /dev/null 2>&1; then
        echo "✅ $name is healthy"
    else
        echo "❌ $name is unhealthy"
    fi
done

# Check if services are sending telemetry
echo ""
echo "Checking telemetry flow..."

# Query recent traces
TRACES=$(curl -s "http://localhost:8080/api/v1/traces" | jq '.data | length')
if [ "$TRACES" -gt 0 ]; then
    echo "✅ Traces are being collected ($TRACES recent traces)"
else
    echo "⚠️  No traces found"
fi

# Query recent metrics
METRICS=$(curl -s "http://localhost:8080/api/v1/query_range?query=up" | jq '.data.result | length')
if [ "$METRICS" -gt 0 ]; then
    echo "✅ Metrics are being collected ($METRICS series)"
else
    echo "⚠️  No metrics found"
fi
```

## Utility Scripts

### clean.sh - Clean Build Artifacts
```bash
#!/bin/bash
set -e

echo "Cleaning build artifacts..."

# Clean Go builds
find . -type f -name "*.exe" -delete
find . -type f -name "main" -delete
find . -type d -name "vendor" -exec rm -rf {} + 2>/dev/null || true

# Clean Node modules
find . -type d -name "node_modules" -exec rm -rf {} + 2>/dev/null || true
find . -type d -name "dist" -exec rm -rf {} + 2>/dev/null || true
find . -type d -name "build" -exec rm -rf {} + 2>/dev/null || true

# Clean Flutter
(cd mobile && flutter clean)

# Clean Docker
docker system prune -f

echo "Cleanup complete!"
```

### generate-docs.sh - Generate Documentation
```bash
#!/bin/bash
set -e

echo "Generating documentation..."

# Generate Go docs
for service in backend/*/; do
    if [ -f "$service/go.mod" ]; then
        echo "Generating docs for $(basename $service)..."
        (cd "$service" && go doc -all > ../../docs/api/$(basename $service).md)
    fi
done

# Generate API documentation
echo "Generating OpenAPI documentation..."
for service in backend/*/; do
    service_name=$(basename "$service")
    port=$((8000 + $(echo $service_name | wc -c)))
    curl -s http://localhost:$port/openapi.json > docs/api/$service_name-openapi.json
done

# Generate frontend docs
(cd frontend-admin && npm run build:docs)

echo "Documentation generated in docs/"
```

### backup.sh - Backup Databases
```bash
#!/bin/bash
set -e

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="backups/$TIMESTAMP"

echo "Creating backup at $BACKUP_DIR..."
mkdir -p $BACKUP_DIR

# Backup MongoDB
docker exec orkestra-mongo mongodump \
    --archive=$BACKUP_DIR/mongo.archive \
    --gzip

# Backup Redis
docker exec orkestra-redis redis-cli --rdb $BACKUP_DIR/redis.rdb

# Backup TimescaleDB
docker exec orkestra-timescale pg_dump \
    -U postgres -d orkestra_metrics \
    | gzip > $BACKUP_DIR/timescale.sql.gz

# Upload to S3 (if configured)
if [ ! -z "$S3_BACKUP_BUCKET" ]; then
    echo "Uploading to S3..."
    aws s3 cp $BACKUP_DIR s3://$S3_BACKUP_BUCKET/backups/$TIMESTAMP/ --recursive
fi

echo "Backup complete!"
```

### wait-for-services.sh - Wait for Services to be Ready
```bash
#!/bin/bash

echo "Waiting for services to be ready..."

# Wait for MongoDB
until docker exec orkestra-mongo mongosh --eval "db.adminCommand('ping')" &>/dev/null; do
    echo "Waiting for MongoDB..."
    sleep 2
done
echo "MongoDB is ready!"

# Wait for Redis
until docker exec orkestra-redis redis-cli ping &>/dev/null; do
    echo "Waiting for Redis..."
    sleep 2
done
echo "Redis is ready!"

# Wait for TimescaleDB
until docker exec orkestra-timescale pg_isready -U postgres &>/dev/null; do
    echo "Waiting for TimescaleDB..."
    sleep 2
done
echo "TimescaleDB is ready!"

echo "All services are ready!"
```

## Makefile

```makefile
.PHONY: help setup dev test build deploy clean

help:
	@echo "Available commands:"
	@echo "  make setup    - Set up development environment"
	@echo "  make dev      - Start development environment"
	@echo "  make test     - Run all tests"
	@echo "  make build    - Build all services"
	@echo "  make deploy   - Deploy to staging"
	@echo "  make clean    - Clean build artifacts"

setup:
	./scripts/setup.sh

dev:
	./scripts/dev.sh

test:
	./scripts/test.sh

build:
	./scripts/build.sh

deploy:
	./scripts/deploy.sh staging

clean:
	./scripts/clean.sh

# Advanced targets
test-unit:
	@echo "Running unit tests..."
	@./scripts/test.sh unit

test-integration:
	@echo "Running integration tests..."
	@./scripts/test.sh integration

test-e2e:
	@echo "Running end-to-end tests..."
	@./scripts/test.sh e2e

build-backend:
	@echo "Building backend services..."
	@./scripts/build.sh backend

build-frontend:
	@echo "Building frontend..."
	@./scripts/build.sh frontend

deploy-staging:
	./scripts/deploy.sh staging

deploy-production:
	./scripts/deploy.sh production

rollback:
	./scripts/rollback.sh

backup:
	./scripts/backup.sh

restore:
	./scripts/restore.sh

monitor:
	./scripts/health-check.sh

logs:
	docker-compose logs -f

db-migrate:
	./scripts/migrate.sh

db-seed:
	./scripts/seed.sh

docs:
	./scripts/generate-docs.sh
```

## CI/CD Scripts

### ci-test.sh - CI Test Runner
```bash
#!/bin/bash
set -e

# This script is used by CI/CD pipelines

echo "Starting CI test suite..."

# Set up test environment
export CI=true
export NODE_ENV=test
export GO_ENV=test

# Start test databases
docker-compose -f docker-compose.test.yml up -d

# Wait for services
./scripts/wait-for-services.sh

# Run migrations
./scripts/migrate.sh

# Run tests with coverage
echo "Running backend tests..."
for service in backend/*/; do
    if [ -f "$service/go.mod" ]; then
        (cd "$service" && go test -v -race -coverprofile=coverage.out ./...)
        (cd "$service" && go tool cover -html=coverage.out -o coverage.html)
    fi
done

echo "Running frontend tests..."
(cd frontend-admin && npm run test:ci)

echo "Running mobile tests..."
(cd mobile && flutter test --coverage)

# Generate reports
echo "Generating test reports..."
mkdir -p reports
find . -name "coverage.out" -exec cp {} reports/ \;
find . -name "coverage.html" -exec cp {} reports/ \;

# Cleanup
docker-compose -f docker-compose.test.yml down

echo "CI tests complete!"
```

## Best Practices

1. **Idempotency**: All scripts should be safe to run multiple times
2. **Error Handling**: Use `set -e` and proper error checking
3. **Logging**: Provide clear output about what's happening
4. **Documentation**: Include help text and usage examples
5. **Environment Variables**: Use defaults but allow overrides
6. **Cleanup**: Always clean up resources on exit
7. **Validation**: Check prerequisites before running
8. **Atomic Operations**: Use transactions where possible
9. **Rollback Support**: Provide rollback mechanisms
10. **Testing**: Test scripts in isolated environments first

## Module-Specific Guidelines

- **Idempotency**: All scripts must be safe to run multiple times without side effects
- **Error Handling**: Use comprehensive error checking and graceful failure modes
- **Documentation**: Provide clear usage instructions and examples for all scripts
- **Cross-Platform**: Ensure scripts work across development environments
- **Security**: Never hardcode secrets; use environment variables and secure storage
- **Logging**: Implement detailed logging for debugging and monitoring
- **Modularity**: Create reusable functions and avoid code duplication

---

### Related Guides
- [Project Overview](../CLAUDE.md) - System architecture and development workflow
- [Docker Infrastructure](../docker/CLAUDE.md) - Container orchestration and environment setup
- [Backend Deployment](../backend/CLAUDE.md) - Go application build and deployment
- [Frontend Build](../frontend-admin/CLAUDE.md) - React application build process
- [Mobile Build](../mobile/CLAUDE.md) - Flutter application build and release