# Backend — Go Modular Server

Single Go binary. 6 core modules (always loaded) + 13 optional addons. Slim `cmd/server/main.go` (~240 lines) that wires infrastructure and delegates everything else to the module registry. Port 3000 inside the container.

## Stack

Go 1.25.1 | Huma v2 (OpenAPI-first) | MongoDB 8.0 | Redis 8.2 | Chi router | AIR hot-reload (Docker)

## Module System

Every module implements the `Module` interface from the Orkestra SDK
(`pkg/sdk/module/module.go` — see [`pkg/sdk/CLAUDE.md`](pkg/sdk/CLAUDE.md)
for the SDK boundary rules and [`../docs/onboarding/orkestra-sdk.md`](../docs/onboarding/orkestra-sdk.md)
for the new-developer walkthrough):

```
Name, DisplayName, Description, Category
ConfigSchema, Collections, NavItems, Dependencies
ProvidedServices, RequiredServices, OptionalServices
Enabled, Init, RegisterRoutes, Start, Stop, HealthCheck
```

**Registration** (`cmd/server/catalog.go` + `catalog_<addon>.go`): core modules (user → notification → tenant → authz → auth → navigation) are always loaded — they live in `catalog.go`. Each optional addon lives in its own `cmd/server/catalog_<addon>.go` file, gated by `//go:build !no_addons || addon_<name>`, and registers itself into `optionalModules` via `init()`. The default build (no tags) compiles every addon — same behavior as before. Pass `-tags "no_addons"` for a core-only "starter" binary, or `-tags "no_addons addon_billing addon_documents"` for a curated subset. All optional modules that are compiled in are always instantiated, initialized, and routed at boot — only enabled ones have `Start()` called. The admin API can enable/disable modules at runtime via `StartModule()`/`StopModule()` without restart. The registry topologically sorts by `Dependencies()` so producers init before consumers, auto-creates MongoDB collections with their declared indexes, seeds configs, collects nav items, and gates routes for disabled modules via `ModuleGate` middleware.

**Profile builds** (Makefile + Dockerfile `BUILD_TAGS` arg): the canonical addon SKUs are defined in `backend/Makefile`. Build-tag sets are closed under module dependencies (see the `Dependencies()` declarations) — picking a profile that omits a transitive dependency fails loudly at boot via the registry's topo sort, not silently at request time.

| Profile      | `make` target          | Tag set                                                                                  |
| ------------ | ---------------------- | ---------------------------------------------------------------------------------------- |
| starter      | `make build-starter`   | `no_addons`                                                                              |
| minimal      | `make build-minimal`   | `no_addons addon_dev`                                                                    |
| billing      | `make build-billing`   | `no_addons addon_billing addon_documents addon_company addon_dev`                        |
| ai           | `make build-ai`        | `no_addons addon_graph addon_aimodels addon_rag addon_agents addon_sales addon_dev`      |
| saas         | `make build-saas`      | `no_addons addon_subscriptions addon_payments addon_compliance addon_identity addon_dev` |
| enterprise   | `make build`           | (no tags — every addon)                                                                  |

Container builds: `Dockerfile` accepts `--build-arg BUILD_TAGS="..."` (default empty = enterprise). CI builds every profile on each PR via the matrix in `.github/workflows/backend.yml` — that's how a missing tag in `catalog_<addon>.go` gets caught before merge. On push to `dev`/`main`, the same matrix publishes one image per profile to GHCR: `ghcr.io/<repo>/backend:<profile>` (rolling) and `:<profile>-<sha>` (pinned). `:latest` stays as an alias for `:enterprise` for backward compatibility.

**Cross-module communication**: modules discover each other through the `ServiceRegistry` (typed key-value store). Consumer modules import interfaces from `pkg/sdk/iface/` — never import another module's `services/` or `repository/` package.

**Runtime config**: `ModuleConfigService` stores module state in MongoDB (`module_configs` collection), cached in Redis (30s TTL). Secrets encrypted with AES-256-GCM. Each module supports named config environments (production/sandbox) stored as nested maps in the same document. Admin API at `GET/PATCH /v1/admin/modules`, with per-environment endpoints at `/v1/admin/modules/{name}/environments/{env}` and `PUT /v1/admin/modules/{name}/active-environment`.

**Module infrastructure containers**: modules that need an external service (e.g. `agents` → `orkestra-hindsight`) declare it via `InfraContainers() []InfraContainerSpec` on the `Module` interface. The registry routes these specs through `shared/container.Manager`, which uses the Docker Go SDK over the host docker socket to create/start containers before `Start()` and stop them after `Stop()`. A module's container is therefore bound to its enabled state: toggling the module on/off via the admin UI starts/stops the container within the same request. The manager falls back to a no-op implementation when `CONTAINER_CONTROL_ENABLED=false` or the socket is unreachable — module toggling still works, operators are expected to manage infra externally. Security: dev/staging/prod compose files mount `/var/run/docker.sock` into the backend container; front this with `tecnativa/docker-socket-proxy` (restricted to container endpoints) when running on shared/production hosts. Note: backend-managed containers are **not** part of any compose project — they won't appear in `docker compose ps`. Use `docker ps --filter label=orkestra.managed=true` to discover them; see [docker/CLAUDE.md](../docker/CLAUDE.md#backend-managed-containers-not-visible-to-compose) for the ownership split and volume-sharing details.

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
│   ├── core/                       # Always loaded (init order: user → notification → tenant → authz → auth → navigation)
│   │   ├── user/                   # User CRUD, roles, documents
│   │   ├── notification/           # Email delivery, templates, preferences, unsubscribe
│   │   ├── tenant/                 # Orgs + memberships (two-tier tenancy)
│   │   ├── authz/                  # Permissions, roles, Cedar policy engine
│   │   ├── auth/                   # Email/password + OAuth 2.1, JWT, sessions, RBAC
│   │   └── navigation/             # Dynamic menu from module NavItems
│   ├── addons/                     # Optional — toggled at /admin/modules
│   │   ├── billing/                # FatturaPA/SDI invoicing
│   │   ├── documents/              # PDF generation via Gotenberg
│   │   ├── company/                # Business registry lookup
│   │   ├── graph/                  # Memgraph knowledge graph
│   │   ├── aimodels/               # AI model management
│   │   ├── rag/                    # RAG pipeline
│   │   ├── agents/                 # Hindsight AI agents
│   │   ├── sales/                  # AI prospect analysis
│   │   ├── subscriptions/          # Recurring services catalog, clients, subscriptions
│   │   ├── payments/               # Stripe gateway, refunds, webhooks
│   │   ├── compliance/             # Platform audit log + (future) DSR / SOC2 evidence
│   │   ├── identity/               # Per-tenant BYO OIDC + SCIM 2.0 stubs
│   │   └── dev/                    # Dev token generator
│   ├── shared/                     # Infrastructure — used by core and addons
│   │   ├── module/                 # Module interface, registry, config service
│   │   ├── iface/                  # Cross-module interfaces
│   │   ├── config/                 # App configuration
│   │   ├── database/               # MongoDB, Redis, Graph connections
│   │   ├── middleware/             # Auth, JWT validator, rate limiting
│   │   ├── remote/                 # Remote service clients (HTTP)
│   │   ├── setup/                  # First-install wizard endpoints (/v1/setup/*)
│   │   ├── systeminit/             # Atomic first-admin sentinel (system_init collection)
│   │   ├── tenantrepo/             # orgId scope helpers (every addon repo must use these)
│   │   ├── errors/                 # Error management
│   │   └── utils/                  # Utilities
│   └── testkit/                    # Test helpers for auth identity + context
├── tools/
│   └── tenantscope/                # Static analyzer: enforces tenantrepo use in addons (CI gate)
├── Dockerfile                      # Multi-stage: dev (AIR) / production — Chainguard hardened base
├── Dockerfile.ai-service           # AI service build (multi-stage, dhi.io hardened base)
└── go.mod
```

Each module follows: `module.go` → `handlers/` → `services/` → `repository/` → `models/`

## Adding a New Module

1. Create `internal/addons/yourmodule/module.go` implementing the `Module` interface
2. Create `cmd/server/catalog_yourmodule.go` with build tag `//go:build !no_addons || addon_yourmodule`, a single `init()` that registers `optionalModules["yourmodule"] = func() module.Module { return yourmodule.NewModule() }` (the registry auto-sorts by `Dependencies()`)
3. Declare `Collections()` for auto-created MongoDB collections + indexes
4. Declare `NavItems()` for sidebar entries (group, icon, path, minRole)
5. Declare `ConfigSchema()` for admin-configurable fields
6. Declare `Dependencies()` if your module needs other modules to init first
7. Use `shared/iface` interfaces for cross-module deps — add new interfaces there if needed
8. Use `deps.Services.Register(key, impl)` to expose services to other modules

Users enable the module via the admin UI at `/admin/modules` (takes effect immediately, no restart needed). For first boot of a fresh install, the module's `ConfigSchema().EnvVar` fields seed the initial `module_configs` document from the host environment — see [docker/CLAUDE.md](../docker/CLAUDE.md) for the env-var-vs-admin-UI split. On first boot only, setting `ORKESTRA_PROFILE` (resolved by `shared/module/config_service.go::computeProfileOverride`) pre-enables the SKU's addons in the seeded document; the dev addon is excluded so it keeps its `!IsProduction()` gate.

## API Endpoints

- **`/docs`** — Interactive API documentation (Scalar)
- **`/openapi.json`** — Auto-generated OpenAPI 3.1 spec
- **`/v1/admin/modules`** — Module management (administrator only)
- **`/v1/admin/modules/{name}/environments/{env}`** — Per-environment config CRUD
- **`/v1/admin/modules/{name}/active-environment`** — Switch active environment
- **`/v1/admin/modules/health`** — Per-module health checks

OpenAPI specs are auto-generated by Huma v2 — add endpoints with `huma.Register()` and they appear in `/docs` after restart.

### Canonical spec for docs.orkestra.cc

`backend/openapi/enterprise.json` is the **canonical OpenAPI document** consumed by [docs.orkestra.cc](https://docs.orkestra.cc) (rendered under `/api` via `docusaurus-plugin-openapi-docs`). It is **committed**, **regenerated by `make openapi-dump`**, and **gated by `make openapi-check`** in `ci-backend`.

When you change routes, regenerate before committing:

```bash
# from backend/
(cd ../docker && docker compose -f docker-compose.infra.yml up -d)   # if not already running
make openapi-dump                                                     # writes openapi/enterprise.json
git add openapi/enterprise.json
```

The dump runs the full enterprise build (`cmd/server` with default tags) with `OPENAPI_DUMP=1` set, which serializes `huma.API.OpenAPI()` to disk and exits before binding any listener. Module Init runs against an isolated Mongo namespace (`orkestra_openapi_dump`) and Redis DB index `15`, so dev/staging data is never touched. Both `operatorAPI` and `clientAPI` share a single in-memory OpenAPI document (the audience split lives at the mux/host level today), so one file covers both surfaces.

## Dev Tokens (Dev/Staging Only)

```bash
./scripts/devtoken.sh developer                       # Generate operator-aud token
./scripts/devtoken.sh admin --quiet                   # Token only (for piping)
./scripts/devtoken.sh operator --curl                 # Ready-to-use curl command
./scripts/devtoken.sh administrator --audience client # ADR-0003 PR-D — mint aud=client for api.*
```

Roles (highest to lowest): `super_admin` > `administrator` > `developer` > `manager` > `operator` > `guest`.

Audiences (ADR-0003 PR-D D-10): `operator` (default, hits `console.*`) or `client` (hits `api.*`). Both surfaces' `RequireAudience` gates reject cross-audience tokens with `401 audience_mismatch`.

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

**SKU profile stack (pre-built image from GHCR, no source build, no hot reload):**
```bash
cd docker
docker compose -f docker-compose.infra.yml up -d                       # MongoDB + Redis
docker compose -f docker-compose.starter.yml --env-file .env up -d     # or billing / ai / saas / enterprise
docker compose -f docker-compose.starter.yml logs -f backend
```

The five SKU compose files (`starter`/`billing`/`ai`/`saas`/`enterprise`) pull `ghcr.io/orkestra-cc/orkestra/backend:<sku>` and layer on `docker-compose.infra.yml`. They're the recommended path when you don't have `dhi.io` registry access or just want a smoke-test-ready backend; the `starter` SKU is the leanest (core modules only, no addons), with `enterprise` covering every addon.

**WSL2 caveat**: AIR doesn't detect file changes on Windows mounts. Rebuild manually:
```bash
docker exec orkestra-backend-dev go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-backend-dev
```

**Log level**: controlled by `LOG_LEVEL` env var — `debug` (dev), `info` (staging), `warn` (prod).

**Structured request logger** (ADR-0005 Phase A): every HTTP request produces one JSON line via `shared/middleware.RequestLogger` (mounted outermost on each audience mux after `RequestID` + `RealIP`). The payload is **allowlist-only** — never log bodies, headers, or raw query strings; module code uses `slog.InfoContext(ctx, "msg", slog.String(...))` so `trace_id` / `span_id` correlate to the same request automatically via `shared/utils.TraceContextHandler`. Tunables: `LOG_HTTP_SKIP_PATHS` (default `/health,/ready,/metrics,/openapi.json`), `LOG_HTTP_SLOW_THRESHOLD_MS` (default `1000`).

## Rules

- **Read the module's own CLAUDE.md** before modifying it — notification, billing, documents, graph, rag, agents, aimodels, company each have one
- **Use the module system** — don't add routes or init logic directly to main.go
- **Use `shared/iface`** for cross-module deps — never import another module's services package from module.go
- **Validate all inputs**, implement RBAC on every endpoint, never expose secrets in responses
- **MongoDB indexes** — declare them in `Collections()`, don't create them manually
- **Vulnerability allowlist** — `backend/.vulncheck-allowlist.txt` lists upstream-unfixed reachable CVEs accepted by `make backend-vulncheck` (and the Backend CI workflow). Each entry must be re-evaluated when the relevant dependency is bumped.
