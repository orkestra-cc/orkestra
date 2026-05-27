# ORKESTRA

**Orkestra is the SaaS plumbing every product rebuilds — users, auth, RBAC, multi-tenancy, billing — already done.** Seven core modules (`user`, `auth`, `authz`, `tenant`, `notification`, `navigation`, `logging`) supply the baseline on day one; a catalog of optional addons (invoicing/SDI, payments, subscriptions, RAG, AI agents, documents, identity, compliance, ...) sits on top and is toggled per internal tenant at `/admin/modules`. Orkestra manages *who* can consume those addons across a **two-tier tenancy model**.

## Tenancy Model

Orkestra operates on **two distinct tiers of tenants**. Understanding this distinction is load-bearing for every design decision — data isolation, RBAC scope, billing, and module activation all depend on which tier a request is acting in.

### Tier 1 — Internal tenants (operator side)

The companies that **run Orkestra** (one or more of "our" organizations). For each internal tenant the platform manages:

- Internal users, roles, and RBAC
- The internal company's own electronic invoicing (FatturaPA/SDI), billing, documents
- Which modules/addons are enabled for that tenant
- Operational admin (module config, audit logs, compliance evidence)

### Tier 2 — External client tenants (customer side)

**External clients register on the platform**, and each external client can itself be a multi-tenant organization (multiple sub-tenants / workspaces under one client). For each external client the platform manages:

- Client registration and onboarding
- The client's own users, roles, sub-tenants
- **Subscriptions to the services Orkestra exposes** (via the `subscriptions` + `payments` modules — Stripe-backed recurring billing)
- Usage of the subscribed services (AI agents, RAG, document generation, etc.) scoped to the client's data

The `subscriptions` and `payments` modules are **not ordinary feature addons** — they are the mechanism by which Tier-2 clients consume Tier-1-hosted services. Treat them as architecturally load-bearing.

### Implications for contributors

- Every new endpoint must declare **which tier it serves** (internal operator, external client, or both) and enforce org-scoped RBAC accordingly.
- Every new collection/table must carry a tenant scope (internal org ID *or* external client org ID) and be indexed/queried with that scope — never cross-tenant by default.
- Module enable/disable is **per internal tenant**, but service consumption is gated **per external client subscription**. Do not conflate the two.
- When in doubt about which tier owns a resource, ask before implementing.

## Tech Stack

| Layer              | Technology                                                         |
| ------------------ | ------------------------------------------------------------------ |
| **Backend**        | Go 1.25.1, Huma v2 (OpenAPI-first), 7 core + 14 optional self-contained modules |
| **Frontend**       | React 19, TypeScript 5.9, Vite 7, Redux Toolkit, TanStack Table    |
| **Mobile**         | Flutter 3.35+, Dart, Riverpod                                      |
| **Database**       | MongoDB 8.0, Redis 8.2                                             |
| **Infrastructure** | Docker Compose (dev/staging/prod), GitHub Actions CI               |
| **Auth**           | Email/password (argon2id) + OAuth 2.1 (Google, Apple, GitHub, Discord), RS256 JWT, 6-role RBAC |

## Architecture

**Plugin architecture**: 7 core modules (user, notification, tenant, authz, auth, navigation, logging) always load. All other modules are **optional** — every optional module is instantiated and initialized at boot, and operators flip individual modules on/off at `/admin/modules` (state persisted in the `module_configs` MongoDB collection). The registry resolves initialization order automatically from each module's `Dependencies()` declaration.

**Key components** (`backend/pkg/sdk/module/`):

- **Module interface** — lifecycle contract every module implements (Init, RegisterRoutes, Start, Stop, HealthCheck)
- **ModuleRegistry** — `RegisterAll()` with topological sort from `Dependencies()`; tracks failures, gates routes for disabled modules
- **ServiceRegistry** — typed key-value store for cross-module service sharing (`GetTyped[T]`, `MustGetTyped[T]`)
- **ConfigService** — DB-backed (MongoDB) + Redis-cached (30s TTL) module configuration with AES-256-GCM encrypted secrets
- **shared/iface** — consumer-facing interfaces (UserProvider, NotificationSender, PDFProvider, GraphProvider, AIModelProvider, RAGQueryProvider, JWTProvider) that prevent direct cross-module imports
- **RoleMiddleware** — interface (`module.go`) for RBAC route protection, satisfied by both `AuthMiddleware` (monolith) and `JWTValidator` (AI service)
- **Module catalog** (`cmd/server/catalog.go` + per-addon `catalog_<name>.go` files, each gated by `//go:build !no_addons || addon_<name>`) — maps module names to factory functions; addons that are compiled in are always instantiated and initialized at boot for runtime enable/disable without restart. Build tags decide what's *installable*; runtime config decides what's *on*. See [`backend/CLAUDE.md`](backend/CLAUDE.md) for the canonical profile recipes.

**Admin API**: `GET/PATCH /v1/admin/modules`, `GET /v1/admin/modules/health`, `GET/PATCH /v1/admin/modules/{name}/environments/{env}`, `PUT /v1/admin/modules/{name}/active-environment` — runtime enable/disable (hot-reload), config updates, per-environment config profiles (sandbox/production), health checks. Frontend at `/admin/modules` (list) and `/admin/modules/:name` (detail).

### Module Loading

Two layers decide what runs:

1. **Build time — which addons are *installable* in this binary.** Each addon's wiring lives in `cmd/server/catalog_<addon>.go` behind `//go:build !no_addons || addon_<name>`. Default builds compile every addon (enterprise SKU); `-tags "no_addons addon_billing addon_documents"` ships a curated subset. The `Makefile` defines named profiles (`make build-starter|minimal|billing|ai|saas|enterprise`) and CI builds the full matrix on every PR. See [`backend/CLAUDE.md`](backend/CLAUDE.md) for the canonical profile table.
2. **Runtime — which compiled-in addons are *enabled*.** All optional modules that compiled in are always **instantiated, initialized, and routed** at boot — regardless of enabled state. Routes for disabled modules are gated by `ModuleGate` middleware (returns 503). Only enabled modules have their `Start()` method called (background jobs, polling, etc.).

**Enabling/disabling at runtime:** The admin API (`PATCH /v1/admin/modules/{name}`) calls `StartModule()`/`StopModule()` on the registry. The module starts or stops immediately — no restart required. Dependency constraints are enforced: you cannot disable a module that another running module depends on (returns 409).

**Which modules start at boot** is determined by the `module_configs` collection in MongoDB (set via admin UI). On first boot of a brand-new install the document is seeded from each module's `ConfigSchema().EnvVar`; if `ORKESTRA_PROFILE` is set (typically by `docker-compose.<profile>.yml`), the seeder additionally pre-enables the SKU's addons (`billing` → billing/documents/company, `ai` → graph/aimodels/rag/agents/sales, `saas` → subscriptions/payments/compliance/identity, `enterprise` → every non-core, non-dev addon). Subsequent boots ignore `ORKESTRA_PROFILE` — admin-set values are authoritative. See `docker/CLAUDE.md` for the per-bucket split.

The registry topologically sorts modules by `Dependencies()` so initialization order is always correct.

### AI Service Sidecar (Optional Split)

The AI module chain (graph, aimodels, rag, agents) can optionally run as a **standalone sidecar service** (`cmd/ai-service/`) separate from the monolith. Controlled by the `AI_SERVICE_URL` env var on the monolith:

- **Empty (default)**: All modules run in-process in the monolith. No change from baseline.
- **Set** (e.g., `http://orkestra-ai:3100`): The monolith skips registering graph/aimodels/rag/agents modules and instead registers `RemoteAIModelProvider` + `RemoteRAGQueryProvider` (HTTP clients in `shared/remote/`) under the same `ServiceRegistry` keys. Consumer modules like `sales` use the same `GetTyped` pattern — zero code changes.

**How the split works:**

```
┌─ Monolith (port 3000) ──────────────┐   ┌─ AI Service (port 3100) ──────────┐
│ auth, user, navigation, billing,     │   │ graph, aimodels, rag, agents      │
│ documents, company, sales, dev       │   │                                    │
│                                      │   │ Same Go module (backend/),         │
│ sales → RemoteAIModelProvider ───────┼──→│ separate binary (cmd/ai-service/) │
│         (HTTP to AI service)         │   │                                    │
│                                      │   │ JWT validated with public key only │
│                                      │   │ (JWTValidator, no auth module dep) │
└──────────────────────────────────────┘   └────────────────────────────────────┘
```

**Key files:**

| File | Purpose |
|------|---------|
| `backend/cmd/ai-service/main.go` | AI service entry point — boots 4 modules with `JWTValidator` |
| `backend/internal/shared/middleware/jwt_validator.go` | Lightweight RS256 JWT validation (public key only) |
| `backend/internal/shared/remote/` | `RemoteAIModelProvider`, `RemoteRAGQueryProvider`, remote `EmbeddingProvider`/`LLMProvider` |
| `backend/internal/addons/aimodels/internal_routes.go` | Internal API: `/v1/internal/ai/embed`, `/complete`, `/embedding-info`, `/llm-info` |
| `backend/internal/addons/rag/internal_routes.go` | Internal API: `/v1/internal/rag/query` (with `documentUUIDs` scoping) |
| `backend/Dockerfile.ai-service` | Multi-stage build for AI service binary |
| `docker/docker-compose.ai-sidecar.yml` | Dev container for AI service |

**Running split mode (dev):**

```bash
cd docker
docker compose -f docker-compose.infra.yml up -d
AI_SERVICE_URL=http://orkestra-ai-dev:3100 docker compose -f docker-compose.dev.yml --env-file .env up -d
docker compose -f docker-compose.ai-sidecar.yml --env-file .env up -d
```

**Design constraints for the split:**

- Both binaries live in the **same Go module** (`backend/`) — no code duplication, shared `internal/` packages
- The AI service uses `JWTValidator` (public key only) instead of `AuthMiddleware` (which depends on the auth module). Both satisfy `module.RoleMiddleware`
- Internal API endpoints (`/v1/internal/*`) are the service-to-service contract. They serialize the `iface.AIModelProvider` and `iface.RAGQueryProvider` method calls as HTTP request/response
- Streaming (`/v1/rag/query/stream`) goes **directly** from frontend → AI service, never proxied through the monolith
- The feature flag is fully backward compatible and K8s-ready (service DNS, Ingress routing, separate `Deployment` with independent scaling)

## Module Map

### Backend Modules (`backend/internal/`)

**Core (always loaded):**

| Module           | Purpose                                                                                                           |
| ---------------- | ----------------------------------------------------------------------------------------------------------------- |
| **user**         | User CRUD, role management, document tracking                                                                     |
| **notification** | Email delivery, templates, user preferences, unsubscribe tokens — [docs](backend/internal/core/notification/CLAUDE.md) |
| **tenant**       | Orgs + memberships (two-tier tenancy)                                                                             |
| **authz**        | Permissions, roles, Cedar policy engine                                                                           |
| **auth**         | Email/password (argon2id) + OAuth 2.1, JWT, sessions, RBAC                                                        |
| **navigation**   | Dynamic menu from module NavItems + persisted reorder via `/admin/modules/navigation`                             |
| **logging**      | Runtime log-level admin (ADR-0005 Phase F): `log_levels` collection + `/admin/observability/log-levels` UI         |

Load order (topologically sorted by `Dependencies()`): `user` → `notification` → `tenant` → `authz` → `auth` → `navigation` → `logging`. Auth depends on notification (optional at runtime) so it can deliver verification and password-reset emails; `logging` has no declared dependencies.

**Optional (toggled at `/admin/modules`; all instantiated at boot):**

| Module         | Purpose                                                                                                      | Depends on       |
| -------------- | ------------------------------------------------------------------------------------------------------------ | ---------------- |
| **billing**    | Italian electronic invoicing (FatturaPA/SDI) — [docs](backend/internal/addons/billing/CLAUDE.md)                    | documents        |
| **documents**  | PDF generation via Gotenberg — [docs](backend/internal/addons/documents/CLAUDE.md)                                  | —                |
| **company**    | Italian business registry lookup (OpenAPI) — [docs](backend/internal/addons/company/CLAUDE.md)                      | —                |
| **graph**      | Memgraph knowledge graph — [docs](backend/internal/addons/graph/CLAUDE.md)                                          | —                |
| **aimodels**   | Multi-provider AI model management (Ollama, OpenAI, Anthropic) — [docs](backend/internal/addons/aimodels/CLAUDE.md) | —                |
| **rag**        | Document ingestion + retrieval-augmented generation — [docs](backend/internal/addons/rag/CLAUDE.md)                 | graph, aimodels  |
| **agents**     | Hindsight AI agents with RAG context — [docs](backend/internal/addons/agents/CLAUDE.md)                             | auth, aimodels   |
| **sales**      | AI-driven prospect analysis and scoring                                                                      | aimodels         |
| **subscriptions** | Recurring AI-services catalog, clients, subscriptions, activity log — [docs](backend/internal/addons/subscriptions/CLAUDE.md) | —                |
| **payments**   | Stripe gateway — charges, refunds, webhooks — [docs](backend/internal/addons/payments/CLAUDE.md)                    | —                |
| **compliance** | Platform audit log + (future) GDPR DSR pipelines and SOC2 evidence automation                                | —                |
| **identity**   | Per-tenant BYO OpenID Connect login + SCIM 2.0 provisioning stubs                                            | tenant, authz    |
| **marketing**  | Contact base (organizations, persons, memberships, tags, custom-field schemas) + CSV importer with email/VAT dedup — [docs](backend/internal/addons/marketing/CLAUDE.md). Phase 1 of the [Phase-1–5 roadmap](docs/plans/marketing-addon/) | —                |
| **dev**        | Dev token generation (disabled in production)                                                                | auth             |

### Other Modules

- **[`/backend/pkg/sdk/`](backend/pkg/sdk/CLAUDE.md)** — The public SDK contract module (`github.com/orkestra-cc/orkestra-sdk`) every other module depends on. See also [docs/onboarding/orkestra-sdk.md](docs/onboarding/orkestra-sdk.md) for the new-developer walkthrough.
- **[`/frontend-admin/`](frontend-admin/CLAUDE.md)** — React 19 operator console / Tier-1 admin dashboard (port 8080, host `console.localhost`)
- **[`/frontend-client/`](frontend-client/CLAUDE.md)** — React 19 Tier-2 client demo SPA — consumes the ADR-0003 client API surface (port 8081, host `client.localhost`)
- **[`/mobile/`](mobile/CLAUDE.md)** — Flutter cross-platform app
- **[`/docker/`](docker/CLAUDE.md)** — Docker Compose configs (dev/staging/prod/infra)
- **[`/docs/Authentication_flow.md`](docs/Authentication_flow.md)** — Email/password + OAuth 2.1 + RBAC details
- **[`/docs/site/`](docs/site/README.md)** — Canonical source for [docs.orkestra.cc](https://docs.orkestra.cc) hand-written pages. The Docusaurus repo ([orkestra-cc/orkestra-docs](https://github.com/orkestra-cc/orkestra-docs)) mirrors this tree on every build via `npm run sync:site` — edits live here, not there.

## Quick Start

### SKU profile (recommended for first boot)

Pull a pre-built image from GHCR and layer it on top of `docker-compose.infra.yml` (MongoDB + Redis). Five SKUs are published per push: `starter`, `billing`, `ai`, `saas`, `enterprise`. Start with `starter` for the smallest surface (core modules only), or `enterprise` for everything.

```bash
cd docker
docker network create orkestra-network                              # first time only
docker compose -f docker-compose.infra.yml up -d                    # mongodb + redis
docker compose -f docker-compose.starter.yml --env-file .env up -d  # or billing / ai / saas / enterprise

# Backend API: http://localhost:3000
# API Docs:    http://localhost:3000/docs

# Generate an administrator token for first login (run from project root):
ORKESTRA_API_URL=http://localhost:3000 ./scripts/devtoken.sh administrator
```

Every optional module compiled into the SKU is instantiated and initialized at boot regardless of enabled state. To enable additional addons that were compiled in, log in and toggle them at `/admin/modules` — the registry hot-reloads without a restart and auto-resolves dependencies. The notification module boots in `noop` mode by default — verification and password-reset emails are logged to the backend stdout rather than delivered. To send real mail, configure SMTP at `/admin/modules` after first login.

### Full development stack

Adds Gotenberg (PDF), optionally Memgraph/Hindsight, and uses the Chainguard hardened images with AIR hot reload for Go development. Requires access to the `dhi.io` registry.

```bash
# From project root — interactive TUI (pick "Full stack" from the profile menu)
./orkestra.sh

# Or manually:
cd docker
docker compose -f docker-compose.infra.yml up -d        # MongoDB, Redis, Gotenberg, Hindsight
docker compose -f docker-compose.dev-public.yml up -d   # Backend (AIR) + Frontend (Vite) on public Alpine images
# For Chainguard hardened images (requires dhi.io subscription) use docker-compose.dev.yml instead.

# Optional: run AI modules as a separate service
docker compose -f docker-compose.ai-sidecar.yml up -d  # AI Service (port 3100)
# Set AI_SERVICE_URL=http://orkestra-ai-dev:3100 on the backend to enable split mode
```

## Assistant Rules

### Do

- **Read the module's CLAUDE.md** before modifying any module — each has specific patterns and constraints
- **Use the module system** when adding new functionality: implement the `Module` interface, add a `cmd/server/catalog_<name>.go` file (build-tagged `//go:build !no_addons || addon_<name>`) that registers the factory in `optionalModules` via `init()`, declare collections/nav/config via the module methods
- **Use `shared/iface`** for cross-module dependencies — never import another module's `services/` or `repository/` package from a `module.go` wiring file
- **Validate and sanitize** all user inputs; implement RBAC on every endpoint (ask for required permissions)
- **Follow the auth patterns** in [Authentication_flow.md](docs/Authentication_flow.md) for any auth-related changes

### Do Not

- **Never start servers manually** — backend and frontend run in Docker with hot reload (AIR + Vite)
- **Never expose secrets** in logs, API responses, or Git — module secrets use AES-256-GCM encryption via ConfigService
- **Never import cross-module** service/repository packages in `module.go` — use `shared/iface` interfaces + `ServiceRegistry` typed getters instead
- **Never bypass RBAC** — all admin endpoints require `administrator` role; all protected endpoints require auth middleware

### WSL2 Development Caveat

AIR file watcher does not reliably detect changes on the Windows filesystem mounted in WSL2. If backend changes don't take effect after saving, manually rebuild inside the container:

```bash
docker exec orkestra-backend-dev go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-backend-dev
```

### CI/CD

GitHub Actions workflows (`.github/workflows/`) run on PR and push to `dev`/`main`. **CI workflows invoke `make` targets from the repo root — local and CI cannot drift.** Run `make ci-help` for the full list.

- `backend.yml` → `make ci-backend` (lint, tenantscope, policycoverage, vuln, tests, enterprise build, openapi-check) + a profile-build matrix
- `frontend-admin.yml` → `make ci-frontend-admin` (typecheck, eslint, tests, audit, build)
- `frontend-client.yml` → `make ci-frontend-client` (typecheck, eslint, build) — no tests yet
- `mobile.yml` → `make ci-mobile` (flutter analyze, test)
- `security.yml` — Trivy/CodeQL scanning, untouched by the make refactor

Local reproduction is the same one-liner CI uses:

```bash
make ci          # only the surfaces you changed (default base: origin/dev)
make ci-all      # everything — what CI does on dev/main
```

Toolchain versions live in `.mise.toml` and CI installs them via `jdx/mise-action`, so a contributor running `mise install` gets exactly the Go/Node/Flutter/golangci-lint versions CI uses. See [CONTRIBUTING.md](CONTRIBUTING.md) for the contributor flow.
