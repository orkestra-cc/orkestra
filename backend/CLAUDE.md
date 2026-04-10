# Backend — Go Modular Server

Single Go binary. 4 core modules (always loaded) + 9 optional addons. Slim `cmd/server/main.go` (~240 lines) that wires infrastructure and delegates everything else to the module registry. Port 3000 inside the container.

## Stack

Go 1.25.1 | Huma v2 (OpenAPI-first) | MongoDB 8.0 | Redis 8.2 | Chi router | AIR hot-reload (Docker)

## Module System

Every module implements the `Module` interface (`internal/shared/module/module.go`):

```
Name, DisplayName, Description, Category
ConfigSchema, Collections, NavItems, Dependencies
ProvidedServices, RequiredServices, OptionalServices
Enabled, Init, RegisterRoutes, Start, Stop, HealthCheck
```

**Registration** (`cmd/server/catalog.go`): core modules (user → notification → auth → navigation) are always loaded. Optional modules come from `MODULES=billing,sales,...` or from per-module `Enabled()` env vars, and dependencies are auto-included (e.g. enabling `rag` auto-pulls `graph` and `aimodels`). The registry topologically sorts by `Dependencies()` so producers init before consumers, auto-creates MongoDB collections with their declared indexes, seeds configs, collects nav items, and gates routes for disabled modules.

**Cross-module communication**: modules discover each other through the `ServiceRegistry` (typed key-value store). Consumer modules import interfaces from `internal/shared/iface/` — never import another module's `services/` or `repository/` package.

**Runtime config**: `ModuleConfigService` stores module state in MongoDB (`module_configs` collection), cached in Redis (30s TTL). Secrets encrypted with AES-256-GCM. Admin API at `GET/PATCH /v1/admin/modules`.

**Startup reliability**: `NewMongoConnection` and `NewRedisConnection` (in `internal/shared/database/`) retry with exponential backoff (up to 20 attempts, 500ms → 5s) to wait out first-boot auth races — container servers start accepting TCP before SCRAM user / `--requirepass` provisioning completes. The Mongo readiness probe uses `ListDatabaseNames`, not `Ping`, because `Ping` bypasses the auth path and can pass prematurely. `ensureCollection` in the registry also retries transient Mongo errors (pool cleared, `AuthenticationFailed` code 18) because the driver's background monitoring connections re-authenticate for several seconds after the main client succeeds. If you see `Transient mongo error, retrying` at debug level during startup, that's this mechanism working as intended.

## Project Structure

```
backend/
├── cmd/
│   ├── server/                     # Monolith binary
│   │   ├── main.go                 # Boot, register modules, start
│   │   └── catalog.go              # Module catalog (core + optional)
│   └── ai-service/                 # AI sidecar binary (optional)
│       └── main.go
├── internal/
│   ├── core/                       # Always loaded (init order: user → notification → auth → navigation)
│   │   ├── user/                   # User CRUD, roles, documents
│   │   ├── notification/           # Email delivery, templates, preferences, unsubscribe
│   │   ├── auth/                   # Email/password + OAuth 2.1, JWT, sessions, RBAC
│   │   └── navigation/             # Dynamic menu from module NavItems
│   ├── addons/                     # Optional — loaded via MODULES env var
│   │   ├── billing/                # FatturaPA/SDI invoicing
│   │   ├── documents/              # PDF generation via Gotenberg
│   │   ├── company/                # Business registry lookup
│   │   ├── graph/                  # Memgraph knowledge graph
│   │   ├── aimodels/               # AI model management
│   │   ├── rag/                    # RAG pipeline
│   │   ├── agents/                 # Hindsight AI agents
│   │   ├── sales/                  # AI prospect analysis
│   │   └── dev/                    # Dev token generator
│   └── shared/                     # Infrastructure — used by core and addons
│       ├── module/                 # Module interface, registry, config service
│       ├── iface/                  # Cross-module interfaces
│       ├── config/                 # App configuration
│       ├── database/               # MongoDB, Redis, Graph connections
│       ├── middleware/             # Auth, JWT validator, rate limiting
│       ├── remote/                 # Remote service clients (HTTP)
│       ├── errors/                 # Error management
│       └── utils/                  # Utilities
├── Dockerfile                      # Multi-stage: dev (AIR) / production — Chainguard hardened base
├── Dockerfile.minimal              # Public-image build (golang:1.25-alpine → alpine:3.20) used by the minimal compose profile
├── Dockerfile.ai-service           # AI service build
└── go.mod
```

Each module follows: `module.go` → `handlers/` → `services/` → `repository/` → `models/`

## Adding a New Module

1. Create `internal/addons/yourmodule/module.go` implementing the `Module` interface
2. Add to `optionalModules` in `cmd/server/catalog.go` (the registry auto-sorts by `Dependencies()`)
3. Declare `Collections()` for auto-created MongoDB collections + indexes
4. Declare `NavItems()` for sidebar entries (group, icon, path, minRole)
5. Declare `ConfigSchema()` for admin-configurable fields
6. Declare `Dependencies()` if your module needs other modules to init first
7. Use `shared/iface` interfaces for cross-module deps — add new interfaces there if needed
8. Use `deps.Services.Register(key, impl)` to expose services to other modules

Users enable the module by adding its name to the `MODULES` env var or by setting its `Enabled()` env var.

## API Endpoints

- **`/docs`** — Interactive API documentation (Scalar)
- **`/openapi.json`** — Auto-generated OpenAPI 3.1 spec
- **`/v1/admin/modules`** — Module management (administrator only)
- **`/v1/admin/modules/health`** — Per-module health checks

OpenAPI specs are auto-generated by Huma v2 — add endpoints with `huma.Register()` and they appear in `/docs` after restart.

## Dev Tokens (Dev/Staging Only)

```bash
./scripts/devtoken.sh developer          # Generate token for role
./scripts/devtoken.sh admin --quiet      # Token only (for piping)
./scripts/devtoken.sh operator --curl    # Ready-to-use curl command
```

Roles (highest to lowest): `developer` > `ceo` > `administrator` > `manager` > `operator` > `guest`

Disabled in production. Creates synthetic users (no DB writes).

## Development

All services run in Docker. Never start the server manually. Two workflows depending on what you need:

**Full dev stack (Chainguard hardened images, AIR hot reload):**
```bash
cd docker
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml up -d
docker compose logs -f orkestra-backend-dev
```

**Minimal stack (public images only, core modules only, no hot reload):**
```bash
cd docker
docker compose -f docker-compose.minimal.yml --env-file .env.minimal up -d
docker compose -f docker-compose.minimal.yml logs -f backend
```

The minimal stack builds from `backend/Dockerfile.minimal` which uses `golang:1.25-alpine` → `alpine:3.20`. It's the recommended path when you don't have `dhi.io` registry access or just want a smoke-test-ready backend with `MODULES=dev` (user + notification + auth + navigation + dev token generator). Runs on host ports 3050/8050/27050/6350 to avoid colliding with the dev stack.

**WSL2 caveat**: AIR doesn't detect file changes on Windows mounts. Rebuild manually:
```bash
docker exec orkestra-backend-dev go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-backend-dev
```

**Log level**: controlled by `LOG_LEVEL` env var — `debug` (dev), `info` (staging), `warn` (prod).

## Rules

- **Read the module's own CLAUDE.md** before modifying it — notification, billing, documents, graph, rag, agents, aimodels, company each have one
- **Use the module system** — don't add routes or init logic directly to main.go
- **Use `shared/iface`** for cross-module deps — never import another module's services package from module.go
- **Validate all inputs**, implement RBAC on every endpoint, never expose secrets in responses
- **MongoDB indexes** — declare them in `Collections()`, don't create them manually
