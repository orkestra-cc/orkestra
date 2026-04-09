# ORKESTRA

Professional business and service management system — electronic invoicing, AI sales intelligence, RAG pipeline, and document generation.

## Tech Stack

| Layer              | Technology                                                         |
| ------------------ | ------------------------------------------------------------------ |
| **Backend**        | Go 1.25.1, Huma v2 (OpenAPI-first), 12 self-contained modules      |
| **Frontend**       | React 19, TypeScript 5.9, Vite 7, Redux Toolkit, TanStack Table    |
| **Mobile**         | Flutter 3.35+, Dart, Riverpod                                      |
| **Database**       | MongoDB 8.0, Redis 8.2                                             |
| **Infrastructure** | Docker Compose (dev/staging/prod), GitHub Actions CI               |
| **Auth**           | OAuth 2.1 (Google, Apple, GitHub, Discord), RS256 JWT, 6-role RBAC |

## Architecture

**Plugin architecture**: 3 core modules (auth, user, navigation) always load. All other modules are **optional** — users choose which to enable via the `MODULES` env var or per-module env vars. The registry resolves initialization order automatically from each module's `Dependencies()` declaration.

**Key components** (`backend/internal/shared/module/`):

- **Module interface** — lifecycle contract every module implements (Init, RegisterRoutes, Start, Stop, HealthCheck)
- **ModuleRegistry** — `RegisterAll()` with topological sort from `Dependencies()`; tracks failures, gates routes for disabled modules
- **ServiceRegistry** — typed key-value store for cross-module service sharing (`GetTyped[T]`, `MustGetTyped[T]`)
- **ConfigService** — DB-backed (MongoDB) + Redis-cached (30s TTL) module configuration with AES-256-GCM encrypted secrets
- **shared/iface** — consumer-facing interfaces (UserProvider, PDFProvider, GraphProvider, AIModelProvider, RAGQueryProvider, JWTProvider) that prevent direct cross-module imports
- **RoleMiddleware** — interface (`module.go`) for RBAC route protection, satisfied by both `AuthMiddleware` (monolith) and `JWTValidator` (AI service)
- **Module catalog** (`cmd/server/catalog.go`) — maps module names to factory functions; `selectOptionalModules()` resolves which to load and auto-includes transitive dependencies

**Admin API**: `GET/PATCH /v1/admin/modules`, `GET /v1/admin/modules/health` — runtime enable/disable, config updates, health checks. Frontend at `/admin/modules`.

### Module Loading

Two modes for selecting which optional modules to load:

**1. Explicit (recommended for production):**
```bash
MODULES=billing,documents,company,sales
```
Only the listed modules (plus their dependencies) are loaded. Dependencies are auto-included — e.g., `MODULES=billing` auto-includes `documents` because billing depends on it.

**2. Auto (default, backward compatible):**
```bash
# No MODULES set — each module's Enabled() method is checked:
GRAPH_ENABLED=true
RAG_ENABLED=true
AIMODELS_ENABLED=true
# etc.
```

In both modes, the registry topologically sorts modules by `Dependencies()` so users don't need to worry about initialization order.

### AI Service Sidecar (Optional Split)

The AI module chain (graph, aimodels, rag, agents) can optionally run as a **standalone sidecar service** (`cmd/ai-service/`) separate from the monolith. Controlled by the `AI_SERVICE_URL` env var on the monolith:

- **Empty (default)**: All 12 modules run in-process in the monolith. No change from baseline.
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
| `docker/docker-compose.ai.yml` | Dev container for AI service |

**Running split mode (dev):**

```bash
cd docker
docker compose -f docker-compose.infra.yml up -d
AI_SERVICE_URL=http://orkestra-ai-dev:3100 docker compose -f docker-compose.dev.yml --env-file .env up -d
docker compose -f docker-compose.ai.yml --env-file .env up -d
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

| Module         | Purpose                                                              |
| -------------- | -------------------------------------------------------------------- |
| **auth**       | OAuth 2.1, JWT, sessions, RBAC                                       |
| **user**       | User CRUD, role management, document tracking                        |
| **navigation** | Dynamic menu from module NavItems                                    |

**Optional (loaded via `MODULES` env var or per-module env vars):**

| Module         | Purpose                                                                                                      | Depends on       |
| -------------- | ------------------------------------------------------------------------------------------------------------ | ---------------- |
| **billing**    | Italian electronic invoicing (FatturaPA/SDI) — [docs](backend/internal/addons/billing/CLAUDE.md)                    | documents        |
| **documents**  | PDF generation via Gotenberg — [docs](backend/internal/addons/documents/CLAUDE.md)                                  | —                |
| **company**    | Italian business registry lookup (OpenAPI) — [docs](backend/internal/addons/company/CLAUDE.md)                      | —                |
| **graph**      | Memgraph knowledge graph — [docs](backend/internal/addons/graph/CLAUDE.md)                                          | —                |
| **aimodels**   | Multi-provider AI model management (Ollama, OpenAI, Anthropic) — [docs](backend/internal/addons/aimodels/CLAUDE.md) | —                |
| **rag**        | Document ingestion + retrieval-augmented generation — [docs](backend/internal/addons/rag/CLAUDE.md)                 | graph, aimodels  |
| **agents**     | Hindsight AI agents with RAG context — [docs](backend/internal/addons/agents/CLAUDE.md)                             | auth             |
| **sales**      | AI-driven prospect analysis and scoring                                                                      | —                |
| **dev**        | Dev token generation (disabled in production)                                                                | auth             |

### Other Modules

- **[`/frontend/`](frontend/CLAUDE.md)** — React 19 admin dashboard (port 8080)
- **[`/mobile/`](mobile/CLAUDE.md)** — Flutter cross-platform app
- **[`/docker/`](docker/CLAUDE.md)** — Docker Compose configs (dev/staging/prod/infra)
- **[`/docs/Authentication_flow.md`](docs/Authentication_flow.md)** — OAuth 2.1 + RBAC details

## Quick Start

```bash
# From project root — interactive deployment
./deploy.sh

# Or manually:
cd docker
docker compose -f docker-compose.infra.yml up -d   # MongoDB, Redis, Gotenberg, Hindsight
docker compose -f docker-compose.dev.yml up -d      # Backend (AIR) + Frontend (Vite)

# Optional: run AI modules as a separate service
docker compose -f docker-compose.ai.yml up -d       # AI Service (port 3100)
# Set AI_SERVICE_URL=http://orkestra-ai-dev:3100 on the backend to enable split mode

# Frontend: http://localhost:8080
# Backend API: http://localhost:3000
# AI Service API: http://localhost:3100 (when running split)
# API Docs: http://localhost:3000/docs
```

## Assistant Rules

### Do

- **Read the module's CLAUDE.md** before modifying any module — each has specific patterns and constraints
- **Use the module system** when adding new functionality: implement the `Module` interface, add to the `optionalModules` catalog in `cmd/server/catalog.go`, declare collections/nav/config via the module methods
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

GitHub Actions workflows (`.github/workflows/`) run on PR and push to `dev`/`main`:

- `backend.yml` — lint (golangci-lint), test (80% coverage gate), build, Docker push to GHCR
- `frontend.yml` — TypeScript check, ESLint, build, Docker push
- `mobile.yml` — Flutter analyze, test, APK build
