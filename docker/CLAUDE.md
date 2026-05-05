# Module: Docker Infrastructure

_Path: `/docker`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

<!-- Navigation -->

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)

<!-- /Navigation -->

## Module Purpose

The docker module provides **containerized infrastructure and deployment configurations** for the Orkestra system across development and production environments.

- **Primary Role**: Container orchestration and environment management
- **System Integration**: Provides database, caching, and application container services
- **Architecture**: Clean separation between infrastructure and application services

## Dependencies

### Imports

- **[`/backend/`](../backend/CLAUDE.md)** - Go application containerization
- **[`/frontend-admin/`](../frontend-admin/CLAUDE.md)** - React application containerization

### Importers

- **[`/scripts/`](../scripts/CLAUDE.md)** - Automation scripts for container orchestration
- **Developers**: Local development environment setup
- **Production**: Deployment and scaling configurations

## AI Assistant Critical Rules

### 🚨 MANDATORY: Check ENV Variable FIRST

**BEFORE running ANY Docker command, you MUST check the current environment:**

```bash
# ALWAYS run this FIRST before any docker operations
grep "^ENV=" /home/tore/orkestra/docker/.env
```

**This determines which compose file to use:**
- `ENV=development` → Use `docker-compose.dev.yml`
- `ENV=staging` → Use `docker-compose.staging.yml`
- `ENV=production` → Use `docker-compose.prod.yml`

**⛔ NEVER assume the environment. ALWAYS check first.**

---

### 🚨 ALWAYS use `--env-file` when running Docker Compose commands:

```bash
# ✅ CORRECT - Always specify the env file
docker compose -f docker-compose.staging.yml --env-file .env up -d
docker compose -f docker-compose.staging.yml --env-file .env restart frontend-admin
docker compose -f docker-compose.staging.yml --env-file .env logs frontend-admin

# ❌ WRONG - Using wrong compose file without checking ENV first
docker compose -f docker-compose.dev.yml up -d
```

**Compose file selection based on ENV variable:**
- `ENV=development` → `docker-compose.dev.yml` with `--env-file .env`
- `ENV=staging` → `docker-compose.staging.yml` with `--env-file .env`
- `ENV=production` → `docker-compose.prod.yml` with `--env-file .env`

---

## Three-Stage Environment Workflow

ORKESTRA uses a three-stage DevOps workflow: **Development**, **Staging**, and **Production**.

### Environment Files

| File | Environment | Purpose |
|------|-------------|---------|
| `.env.development` | Development | Local dev with hot reload, relaxed security |
| `.env.staging` | Staging | Production-like behavior, staging credentials |
| `.env.production` | Production | Full security, production credentials |
| `.env.example` | Template | Copy to create new environment files |

### Quick Commands

```bash
# Interactive TUI — single entry point for every stack operation
./orkestra.sh                      # Profile menu: minimal or full stack

# CLI mode (scriptable, same operations)
./orkestra.sh minimal deploy --build
./orkestra.sh minimal logs backend -f
./orkestra.sh minimal reset --yes
ENV=development ./orkestra.sh deploy --scope backend --rebuild --yes
./orkestra.sh logs orkestra-backend-dev -f
./orkestra.sh --help               # Full command surface

# Validate environment files
./scripts/env-validate.sh all      # Validate all
./scripts/env-validate.sh staging  # Validate specific
```

### What belongs in `.env` / compose vs `/admin/modules`

After the architecture-modernization refactor, **module-level configuration lives in MongoDB** (`module_configs`), is edited at `/admin/modules`, and secrets are AES-256-GCM-encrypted with `OAUTH_TOKEN_ENCRYPTION_KEY`. The `EnvVar` field on each module's `ConfigSchema()` is consulted **only on first-boot seeding** (`shared/module/config_service.go::buildInitialConfig`); after that the document is authoritative and editing the env var has no effect.

Keep this split when touching `.env*` or `docker-compose.*.yml`:

| Bucket | Owner | Goes in compose / `.env` |
|--------|-------|--------------------------|
| Boot identity (`APP_NAME`, `ENV`, `PORT`, host/port mappings) | process | ✅ yes |
| Database connections (`MONGO_URI`, `REDIS_URL`, credentials) | process | ✅ yes |
| JWT keys, cookies, CORS, rate limits, observability | process | ✅ yes |
| Encryption keys (`OAUTH_TOKEN_ENCRYPTION_KEY`, `ORKESTRA_KMS_MASTER_KEY`, optional `MFA_SECRET_ENCRYPTION_KEY`) | process — bootstraps ConfigService | ✅ yes |
| Process-scoped auth tunables (`AUTH_REQUIRE_EMAIL_VERIFICATION`, `AUTH_RISK_STEP_UP_THRESHOLD`, `WEBAUTHN_RP_ID`, `AUTH_GEOIP_DB_PATH`, `TENANT_KIND_ENFORCEMENT`, `CEDAR_ENFORCE_ACTIONS`) | process | ✅ yes |
| `CONTAINER_CONTROL_ENABLED`, `DOCKER_GID`, `AI_SERVICE_URL`, `AI_SERVICE_PORT` | process | ✅ yes |
| `SALES_*`, `RAG_CHUNK_*` | process — runtime knobs not yet migrated to ConfigSchema | ✅ yes (transitional) |
| OAuth provider credentials (`OAUTH_GOOGLE/APPLE/GITHUB/DISCORD_*`) | ConfigService (auth module) | ❌ admin UI |
| OpenAPI billing / company credentials (`OPENAPI_BILLING_*`, `OPENAPI_COMPANY_*`, `OPENAPI_OAUTH_BASE_URL`, `OPENAPI_SANDBOX_MODE`, `BILLING_WEBHOOK_*`) — `accountEmail` + `apiKey` for the shared OAuth minter, or legacy static `bearerToken` | ConfigService (billing, company modules) | ❌ admin UI |
| AI provider keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OLLAMA_BASE_URL`) | ConfigService (aimodels module) | ❌ admin UI |
| Memgraph / Hindsight URLs and credentials (`GRAPH_*`, `HINDSIGHT_URL`, `HINDSIGHT_NAMESPACE`, `HINDSIGHT_IMAGE`, `GRAPH_IMAGE`) | ConfigService (graph, agents modules) | ❌ admin UI |
| Gotenberg URL (`GOTENBERG_*`) | ConfigService (documents module) | ❌ admin UI |
| Stripe keys (`STRIPE_API_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_API_VERSION`) | ConfigService (payments module) | ❌ admin UI |
| SMTP / notification settings (`SMTP_*`, `NOTIFICATION_EMAIL_*`) | ConfigService (notification module) | ❌ admin UI |
| Per-module enable flags (`*_ENABLED`) | DB (`module_configs.enabled`) flipped at runtime | ❌ admin UI |

For first-boot bootstrap of a fresh deployment without using the admin UI, export the seed env vars in the shell before `docker compose up` — they're listed as commented stubs at the bottom of `docker/.env`. Once the document exists, those env vars become inert.

Vars **deleted as dead code** during the cleanup (do not re-add): `MODULES`, `BACKEND_HOST`, `FRONTEND_HOST`, `SIGNOZ_ENABLED`, `MAX_FILE_SIZE`, `ALLOWED_FILE_TYPES`, `HINDSIGHT_LLM_*` (the agents module reuses the AI Models default LLM).

### Environment Behavior Matrix

| Feature | Development | Staging | Production |
|---------|-------------|---------|------------|
| `ENV` value | `development` | `staging` | `production` |
| Log Format | Text (pretty) | JSON | JSON |
| Error Details | Exposed | Hidden | Hidden |
| Cookie Secure | `false` | `true` | `true` |
| Cookie SameSite | `lax` | `strict` | `strict` |
| Rate Limits | 1000/min | 60/min | 30/min |
| Debug Mode | Enabled | Disabled | Disabled |
| HTTPS Required | No | Yes | Yes |
| Observability | Disabled | Enabled | Enabled |

---

## Clean Docker Compose Architecture

### Core Philosophy

Separate infrastructure from applications across five compose files:

1. **`docker-compose.minimal.yml`** — Self-contained 4-container stack (mongo, redis, backend, frontend-admin) using only public images. Recommended for first boot on a modest VM or when you don't have `dhi.io` registry access.
2. **`docker-compose.infra.yml`** - Infrastructure services only (MongoDB, Redis, Gotenberg, Hindsight)
3. **`docker-compose.dev.yml`** - Application services in development mode with hot reload
4. **`docker-compose.staging.yml`** - Application services in staging mode (staging-like env + AIR/Vite hot reload)
5. **`docker-compose.prod.yml`** - Application services in production mode with optimizations

The minimal profile is **self-contained** — it does NOT layer on top of `docker-compose.infra.yml`. It brings its own mongo/redis containers (on non-standard ports to avoid colliding with the dev stack's infra containers) and only starts the core module chain.

### File Organization

```
/                              # Project root
├── README.md                  # Leads with the minimal bootstrap path
├── orkestra.sh                # Unified TUI + CLI for the whole stack (replaces deploy.sh and logs.sh)
└── docker/
    ├── docker-compose.minimal.yml # Minimal: self-contained 4-container stack
    ├── docker-compose.infra.yml   # Infrastructure: MongoDB, Redis, Gotenberg, Hindsight
    ├── docker-compose.dev.yml     # Development: Backend, Frontend with hot reload
    ├── docker-compose.staging.yml # Staging: AIR/Vite hot reload + staging-like env
    ├── docker-compose.prod.yml    # Production: Optimized Backend, Frontend
    ├── docker-compose.ai.yml      # Optional AI sidecar (graph, rag, agents, aimodels)
    ├── .env.example               # Template for environment files
    ├── .env.minimal               # Tracked dev-only env for the minimal profile
    ├── .env                       # Active env (gitignored) - contains ENV=development|staging|production
    ├── keys/                      # JWT and OAuth keys (gitignored)
    └── mongo-init/                # MongoDB initialization scripts
```

### Environment Combinations

- **Minimal**: `docker-compose.minimal.yml` + `.env.minimal` — one-command boot with core modules only, uses public images
- **Development**: `docker-compose.infra.yml` + `docker-compose.dev.yml` + `.env` (with `ENV=development`)
- **Staging**: `docker-compose.infra.yml` + `docker-compose.staging.yml` + `.env` (with `ENV=staging`)
- **Production**: `docker-compose.infra.yml` + `docker-compose.prod.yml` + `.env` (with `ENV=production`)

**IMPORTANT**: All Docker files must remain in `/docker` directory for proper build contexts.

## Quick Start

### Prerequisites

```bash
# Create external network (required for all environments)
docker network create orkestra-network

# Copy environment template and configure (first time only)
cd docker
cp .env.example .env.development
cp .env.example .env.staging
cp .env.example .env.production
# Edit each file with appropriate values
```

### Using orkestra.sh (Recommended)

`orkestra.sh` is the single entry point for every stack operation. It works as both an interactive TUI and a scriptable CLI, and knows about all five profiles (minimal, infra+dev, infra+staging, infra+prod, ai sidecar).

```bash
# Interactive TUI — profile menu appears, then a per-profile op menu
./orkestra.sh

# The TUI flow:
# 1. Pick profile: "Minimal" or "Full stack"
# 2. Minimal: Deploy / Stop / Reset (wipe volumes) / Status / Logs / Info / Back
# 3. Full stack: Deploy (with scope selection) / Stop / Status / Logs / Back
# 4. ENV is autodetected from docker/.env for the full-stack path
```

**CLI mode** — same operations, non-interactive, suitable for scripting and CI:

```bash
# Minimal profile
./orkestra.sh minimal deploy [--build]
./orkestra.sh minimal stop
./orkestra.sh minimal reset [--yes]
./orkestra.sh minimal status
./orkestra.sh minimal info
./orkestra.sh minimal logs <service> [-f] [-n N] [-t]

# Full stack (uses ENV from docker/.env, or ENV=... prefix)
./orkestra.sh deploy [--scope all|backend|frontend-admin|frontend-admin+backend|infra] [--rebuild] [--yes]
./orkestra.sh stop [--with-infra]
./orkestra.sh status
./orkestra.sh logs <service> [-f] [-n N] [-t]
```

**Backward-compat shortcut**: `ENV=development ./orkestra.sh` skips the profile menu and opens the full-stack TUI directly (same convention the old deploy.sh used). Any existing scripted usage along those lines keeps working.

### Manual Docker Compose (Alternative)

```bash
# Navigate to docker directory
cd docker

# Development
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml --env-file .env.development up -d

# Production
docker compose -f docker-compose.infra.yml --env-file .env.production up -d
docker compose -f docker-compose.prod.yml --env-file .env.production up -d
```

## Service Architecture

### Minimal Profile (`docker-compose.minimal.yml`)

**Self-contained 4-container stack using only public images — recommended for first boot**

| Service      | Host port | Purpose       | Features                                                              |
| ------------ | --------- | ------------- | --------------------------------------------------------------------- |
| **mongodb**  | 27050     | Primary DB    | `mongo:8.0`, authenticated healthcheck                                |
| **redis**    | 6350      | Cache         | `redis:8.2-alpine`                                                    |
| **backend**  | 3050      | Go API server | Built from `backend/Dockerfile.minimal` (public `golang:1.25-alpine`) |
| **frontend-admin** | 8050      | React web app | Built from `frontend-admin/Dockerfile` (`nginx:alpine` static serving)      |

The minimal profile:

- Uses **only publicly available images** (no `dhi.io` Chainguard registry required)
- Runs on **non-standard host ports** (3050/8050/27050/6350) so it coexists with the dev/staging/prod stacks without colliding
- Boots **only the core modules** (auth, user, navigation) plus the `dev` token generator — no Gotenberg, Memgraph, Hindsight, Ollama, or AI sidecar
- Has its own `.env.minimal` file (tracked in git, all values are dev placeholders)
- Is a **self-contained compose file** — do NOT layer `docker-compose.infra.yml` on top of it

```bash
cd docker
docker network create orkestra-network   # once, if it does not exist
docker compose -f docker-compose.minimal.yml --env-file .env.minimal up -d
```

All optional modules (billing, rag, agents, etc.) are still instantiated when the minimal profile boots — they sit idle until you toggle them on at `/admin/modules`. The registry auto-resolves transitive dependencies on enable.

### Infrastructure Services (`docker-compose.infra.yml`)

**Shared across dev/staging/prod environments — start once, use everywhere**

| Service       | Port        | Purpose             | Health Check     |
| ------------- | ----------- | ------------------- | ---------------- |
| **mongodb**   | 27027       | Primary database    | mongosh ping     |
| **redis**     | 6387        | Cache & sessions    | redis-cli ping   |
| **gotenberg** | 3030        | PDF generation      | curl /health     |
| **memgraph**  | 7687 / 7444 | Graph database (managed by backend — see note below) | TCP dial on 7687 |
| **hindsight** | 8888        | AI agents backend (managed by backend — see note below) | curl /health     |

**Memgraph and Hindsight are no longer auto-started.** The `graph` and `agents` backend modules each own their respective container's lifecycle: enabling the module at `/admin/modules` starts `orkestra-memgraph` / `orkestra-hindsight`; disabling stops it. Both services are still declared in `docker-compose.infra.yml` for documentation and volume ownership, but live behind the `manual-only` compose profile. To run them by hand anyway (e.g. to inspect a container without the backend):

```bash
docker compose -f docker-compose.infra.yml --profile manual-only up -d memgraph hindsight
```

### Backend-managed Containers (Not Visible to Compose)

Containers declared by a backend module via `Module.InfraContainers()` — `orkestra-memgraph` from the `graph` module and `orkestra-hindsight` from the `agents` module — are created and destroyed by the backend's `shared/container.Manager` directly against the Docker daemon. They are **not** part of any compose project:

- They do NOT appear in `docker compose ls`, `docker compose ps`, or `docker compose -f docker-compose.infra.yml ps`.
- `docker compose down` on any compose file does **not** stop them. Disable the owning module at `/admin/modules` to stop the container.
- To discover them from the shell:
  ```bash
  docker ps --filter label=orkestra.managed=true
  ```
  Every backend-managed container carries `orkestra.managed=true` and `orkestra.module=<name>`.

**Shared volumes**: `orkestra-memgraph-data` and `orkestra-hindsight-data` in `docker-compose.infra.yml` are declared with explicit `name:` directives so they are **not** project-prefixed. Both the backend (when it creates the container at module enable time) and the compose `manual-only` profile reference the exact same Docker volume, so data persists across whichever path started the container first.

**Health probes**: the container manager supports two readiness modes — HTTP GET (`InfraHealthCheck.HTTPPath`, used by hindsight against `/health`) and raw TCP dial (`InfraHealthCheck.TCPPort`, used by memgraph against Bolt port 7687). Pick TCP for services whose native protocol isn't HTTP.

**Migration from older setups**: users who ran hindsight from compose before this change will have an orphaned `orkestra-infra_orkestra-hindsight-data` volume. To salvage that data into the shared volume:

```bash
# 1. Disable agents at /admin/modules (unmounts orkestra-hindsight-data)
# 2. Copy old → new
docker run --rm \
  -v orkestra-infra_orkestra-hindsight-data:/from \
  -v orkestra-hindsight-data:/to \
  alpine sh -c 'cd /from && cp -a . /to/'
# 3. Drop the stale volume
docker volume rm orkestra-infra_orkestra-hindsight-data
# 4. Re-enable agents
```

To discard the old data instead, just run step 3 once no container is mounting it.

### Container Lifecycle Control (Docker Socket Mount)

The dev/staging/prod compose files mount `/var/run/docker.sock` into the `orkestra-backend` container so the shared container manager can start/stop module infrastructure (currently hindsight and memgraph; other modules may opt in later by declaring `InfraContainers()`). Toggle behavior with `CONTAINER_CONTROL_ENABLED`:

| Value | Effect |
|-------|--------|
| `true` (default in dev/staging/prod) | Backend uses the Docker SDK to manage declared containers |
| `false` (default in minimal) | Container control is a no-op; operators manage infra externally (Kubernetes, systemd, etc.) |

**Security**: mounting docker.sock gives the backend container effective root on the host. Acceptable on dev workstations; for production or shared hosts, front the socket with `tecnativa/docker-socket-proxy` restricted to `/containers/...` endpoints and point `DOCKER_HOST` at the proxy instead of the raw socket.

### Application Services

#### Development (`docker-compose.dev.yml`)

**Lightweight development with hot reload. Uses `dhi.io` Chainguard hardened base images.**

| Service                  | Host port | Purpose                              | Features                                                       |
| ------------------------ | --------- | ------------------------------------ | -------------------------------------------------------------- |
| **backend**              | 3007      | Go API server                        | Hot reload (AIR), debug logs                                   |
| **frontend-admin**             | 8087      | Operator console (Tier-1)            | Vite dev server, HMR; host `console.localhost`                 |
| **client-frontend**      | 8081      | Tier-2 client demo SPA               | Vite dev server, HMR; host `client.localhost`; consumes `api.*`|

#### Staging (`docker-compose.staging.yml`)

**Hot-reload stack with staging-like behavior (cookie strict, JWT, CORS, rate limits).** Used as the primary development environment on long-lived VMs that mirror production-style URLs/cookies but still need fast iteration.

| Service                      | Host port | Purpose                              | Features                                                        |
| ---------------------------- | --------- | ------------------------------------ | --------------------------------------------------------------- |
| **orkestra-backend**         | 3000      | Go API server                        | Hot reload (AIR), staging-like env, RS256 JWT                   |
| **orkestra-frontend-admin**        | 8080      | Operator console (Tier-1)            | Vite dev server, HMR; host `staging.orkestra.cc`                |
| **orkestra-client-frontend** | 8081      | Tier-2 client demo SPA               | Vite dev server, HMR; host `app.orkestra.cc`; consumes `staging-api.*` |

The backend mounts `../backend:/app` and runs AIR from the bind mount — no image rebuild on code change. AIR and the Go module/build cache live under `backend/.go-bin/` and `backend/.go-mod-cache/` (gitignored), pre-installed by the host. To bootstrap on a fresh machine:

```bash
cd backend
GOMODCACHE=$PWD/.go-mod-cache go mod download
GOBIN=$PWD/.go-bin GOMODCACHE=$PWD/.go-mod-cache go install github.com/air-verse/air@latest
```

`userns_mode: "host"` is required when the Docker daemon runs with `userns-remap` (otherwise `group_add` for the docker GID is rewritten and the mounted socket stays unreadable). DNS is inherited from the daemon (`/etc/docker/daemon.json` → `dns: [...]`); the staging compose deliberately does **not** set per-service `dns:`, because public resolvers (8.8.8.8, 1.1.1.1) may be blocked on UDP/53 in restricted networks.

#### Production (`docker-compose.prod.yml`)

**Optimized production services**

| Service      | Host port | Purpose       | Features                       |
| ------------ | --------- | ------------- | ------------------------------ |
| **backend**  | 3000      | Go API server | Optimized build, health checks |
| **frontend-admin** | 8080      | React web app | Nginx static serving           |

## Network Architecture

### Internal Communication

- **Network**: `orkestra-network` (external bridge network)
- **Service Discovery**: Docker internal DNS resolution
- **Security**: Isolated container network, no external access to internal services
- **Subnet**: 172.20.0.0/16 (configured in docker-compose.yml)

### Host split (ADR-0003)

The backend serves three audiences from one Go binary, dispatched by `Host` header at the application layer (no reverse-proxy needed for the split itself):

| Audience | Default host (dev) | Default host (prod) | Purpose |
|---|---|---|---|
| `operator` | `console.localhost:3000` | `console.orkestra.com` | Tier-1 operator dashboard — module admin, SDI/FatturaPA self-invoicing, dev tooling |
| `client` | `api.localhost:3000` | `api.orkestra.com` | Tier-2 client tenants — subscriptions, payments, future AI runtime (PR-E) |
| `service` | *(internal docker network only)* | *(internal docker network only)* | AI sidecar `/v1/internal/*` — never published by ingress |

The host mux ([cmd/server/hostmux.go](../backend/cmd/server/hostmux.go)) strips the port from `r.Host` and dispatches to the matching audience's chi.Mux. Each mux mounts its own `RequireAudience` middleware ([shared/middleware/audience.go](../backend/internal/shared/middleware/audience.go)) so a token issued for the wrong audience is rejected before any handler runs (defense in depth above per-route RBAC).

**Dev fallthrough**: when `ENV=development` an unmatched Host falls through to the operator mux, so `curl http://localhost:3000` keeps working without `/etc/hosts` gymnastics. In staging/prod an unmatched Host returns 421 Misdirected Request — the canonical signal that an HTTP/1.1 request reached a server that doesn't serve it. This closes the door on host-header smuggling against the Tier-1 console.

**Per-audience env vars** (compose passes these through to the backend):

| Variable | Purpose | Default |
|---|---|---|
| `CONSOLE_HOST` | Hostname the operator mux answers on | `console.localhost:3000` (dev) / unset (prod, operator-set) |
| `CLIENT_API_HOST` | Hostname the client mux answers on | `api.localhost:3000` (dev) / unset (prod) |
| `OPERATOR_CORS_ORIGINS` | CORS allowlist for operator mux | falls back to `CORS_ORIGINS` |
| `CLIENT_CORS_ORIGINS` | CORS allowlist for client mux | falls back to `CORS_ORIGINS` |
| `OPERATOR_RATE_LIMIT_REQUESTS_PER_MINUTE` / `_BURST` | Per-audience throttling | falls back to `RATE_LIMIT_*` |
| `CLIENT_RATE_LIMIT_REQUESTS_PER_MINUTE` / `_BURST` | Per-audience throttling | falls back to `RATE_LIMIT_*` |
| `OPERATOR_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for operator-tier tokens (ADR-0003 PR-D D-9). | `console.localhost` (dev) / empty (prod, operator-set) |
| `CLIENT_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for client-tier tokens. | `api.localhost` (dev) / empty (prod, operator-set) |

In production-like environments **set both `OPERATOR_COOKIE_DOMAIN` and `CLIENT_COOKIE_DOMAIN` explicitly** — leaving them empty falls back to the legacy `COOKIE_DOMAIN` (which spans both audiences) and defeats the host split.

**JWT audience** (post PR-D): operator login mints `aud=operator`, client login mints `aud=client`; both issuance paths now exist. Each mux's `RequireAudience` gate rejects cross-audience tokens with `401 audience_mismatch`. The dev token endpoint accepts an `audience` field (`operator`|`client`) to mint a matching token for either surface — see `scripts/devtoken.sh --audience client`.

**Smoke test**:
```bash
# Operator surface (default — works via dev fallthrough or *.localhost):
curl -i http://console.localhost:3000/health
curl -i http://localhost:3000/health   # dev fallthrough → operator
# Client surface (post-PR-D registers per-tier auth + onboarding/subscriptions/payments):
curl -i http://api.localhost:3000/health
# 421 in non-dev when Host doesn't match (run with ENV=staging):
curl -i -H 'Host: example.com' http://localhost:3000/health

# Mint per-audience dev tokens (see scripts/devtoken.sh):
./scripts/devtoken.sh administrator                  # default — aud=operator
./scripts/devtoken.sh administrator --audience client  # aud=client (api.* surface)
```

### Port Mapping Strategy

Host ports vary per profile so multiple stacks can coexist on the same machine. Container-internal ports stay standard.

```
Minimal profile (docker-compose.minimal.yml):
3050  → backend:3000      # API server
8050  → frontend-admin:80       # Web application
27050 → mongodb:27017     # Dedicated mongo for minimal
6350  → redis:6379        # Dedicated redis for minimal

Dev stack (docker-compose.infra.yml + docker-compose.dev.yml):
3007  → backend:3000             # API server
8087  → frontend-admin:5173            # Operator console (host: console.localhost)
8081  → client-frontend:5173     # Tier-2 client demo SPA (host: client.localhost)
27027 → mongodb:27017            # Shared infra mongo
6387  → redis:6379               # Shared infra redis
3030  → gotenberg:3000           # PDF generation
8888  → hindsight:8888           # AI agents backend

Production (docker-compose.prod.yml):
3000  → backend:3000      # API server
8080  → frontend-admin:80       # Nginx static
```

### Security & Secrets Management

**Environment File Security:**

```bash
# Ensure .env files are gitignored
echo ".env*" >> .gitignore
echo "!.env.example" >> .gitignore

# Encrypt production secrets (optional)
openssl enc -aes-256-cbc -salt -in .env.prod -out .env.prod.enc
openssl enc -aes-256-cbc -d -in .env.prod.enc -out .env.prod

# Use external secret management in production
# - HashiCorp Vault
# - AWS Secrets Manager
# - Kubernetes Secrets
```

**Container Security:**

- Non-root users in production containers
- Read-only root filesystems where possible
- Resource limits to prevent DoS
- Regular security scanning with Trivy
- Minimal Alpine base images

## Environment Configuration

### Current Setup

The existing `.env` file contains development defaults. For production, create `.env.prod` with secure values.

### Development Environment Variables (`.env`)

```bash
# Infrastructure Ports
MONGO_PORT=27017              # Host port for MongoDB
REDIS_PORT=6379               # Host port for Redis

# Application Ports
BACKEND_PORT=3000             # Host port for backend API
FRONTEND_PORT=8080            # Host port for frontend

# MongoDB
MONGO_ROOT_USERNAME=admin
MONGO_ROOT_PASSWORD=changeme  # Change in production
MONGO_DATABASE=orkestra

# Redis
REDIS_PASSWORD=changeme       # Change in production

# JWT (Development defaults)
JWT_SECRET=dev-jwt-secret-key-change-in-production
JWT_REFRESH_SECRET=dev-jwt-refresh-secret-change-in-production
JWT_ACCESS_EXPIRY=15m
JWT_REFRESH_EXPIRY=7d

# URLs (Development)
FRONTEND_URL=http://localhost:8080
BACKEND_URL=http://localhost:3000
VITE_API_URL=http://localhost:3000
VITE_WS_URL=ws://localhost:3000

# Security (Development - relaxed)
CORS_ORIGINS=http://localhost:8080,http://localhost:5173
RATE_LIMIT_MAX=1000
RATE_LIMIT_WINDOW=1m

# OAuth (Development - optional)
GOOGLE_CLIENT_ID_DEV=your_dev_google_client_id
GOOGLE_CLIENT_SECRET_DEV=your_dev_google_secret
APPLE_CLIENT_ID_DEV=your_dev_apple_client_id
APPLE_TEAM_ID_DEV=your_dev_apple_team_id
APPLE_KEY_ID_DEV=your_dev_apple_key_id
APPLE_PRIVATE_KEY_DEV=your_dev_apple_private_key
```

### Production Environment Template (`.env.prod`)

```bash
# Database Credentials (CHANGE THESE)
MONGO_USERNAME=orkestra_prod_user
MONGO_PASSWORD=SECURE_MONGO_PASSWORD_HERE
MONGO_DATABASE=orkestra
REDIS_PASSWORD=SECURE_REDIS_PASSWORD_HERE

# JWT Configuration
JWT_SECRET=BASE64_ENCODED_SECRET_KEY_HERE
JWT_REFRESH_SECRET=BASE64_ENCODED_REFRESH_SECRET_HERE
JWT_ACCESS_EXPIRY=15m
JWT_REFRESH_EXPIRY=7d

# OAuth Credentials
GOOGLE_CLIENT_ID=your_production_google_client_id
GOOGLE_CLIENT_SECRET=your_production_google_client_secret
APPLE_CLIENT_ID=your_production_apple_client_id
APPLE_TEAM_ID=your_apple_team_id
APPLE_KEY_ID=your_apple_key_id
APPLE_PRIVATE_KEY=your_apple_private_key

# Production URLs
FRONTEND_URL=https://orkestra.cc
BACKEND_URL=https://api.orkestra.cc
WS_URL=wss://io.orkestra.cc
CORS_ORIGINS=https://orkestra.cc

# Security Settings
RATE_LIMIT_MAX=100
RATE_LIMIT_WINDOW=15m

# Email Configuration
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your_email@gmail.com
SMTP_PASSWORD=your_app_password
SMTP_FROM=noreply@orkestra.cc

# AWS S3 Storage
S3_BUCKET=orkestra-prod-uploads
S3_REGION=us-east-1
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key
```

## Monitoring & Observability

### Self-hosted OTEL stack (docker-compose.observability.yml)

Phase 5.2 of the tenancy plan ships a self-hosted observability profile that layers on top of the dev/staging/prod stacks. Four containers, all pinned public images:

| Service           | Image                                         | Host port | Purpose                                                                            |
| ----------------- | --------------------------------------------- | --------- | ---------------------------------------------------------------------------------- |
| **otel-collector**| `otel/opentelemetry-collector-contrib:0.96.0` | 4318 / 4317 | OTLP receiver; fans traces to Tempo, exposes collected metrics for Prometheus     |
| **tempo**         | `grafana/tempo:2.4.1`                         | 3200        | Trace backend (local storage, 72 h retention)                                      |
| **prometheus**    | `prom/prometheus:v2.51.2`                     | 9090        | Metric scraper (15 d retention)                                                    |
| **grafana**       | `grafana/grafana-oss:10.4.2`                  | 3010        | UI; Tempo + Prometheus datasources auto-provisioned; "Tenant traces" dashboard pre-loaded |

Boot it alongside the dev stack:

```bash
cd docker
docker network create orkestra-network   # first time only
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml  up -d
docker compose -f docker-compose.observability.yml up -d
```

Point the backend at the collector by setting in `docker/.env`:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://orkestra-otel:4318
OTEL_TRACES_ENABLED=true
```

Restart the backend (`docker compose -f docker-compose.dev.yml restart backend`) and open:

- Grafana — http://localhost:3010 (admin / admin; anonymous Viewer access is enabled so shareable links work without login)
- Prometheus — http://localhost:9090
- Tempo — http://localhost:3200 (not meant for direct use; query via Grafana's Tempo datasource)

The pre-provisioned "Tenant traces" dashboard (`Orkestra` folder in Grafana) takes a `tenant.id` and optional tier filter and shows every span where the `TenantBaggage` middleware stamped the matching attribute. Backing evidence: `backend/internal/shared/middleware/tenant_baggage.go` + `backend/internal/shared/middleware/baggage_coverage_test.go`.

Phase 5.3 landed `/metrics` on the backend (`GET http://backend:3000/metrics`), scraped automatically by Prometheus. Three metric families ship today — Cedar shadow divergence, capability denial, entitlement projection lag — with the label schema frozen in [ADR-0002](../docs/adr/0002-metrics-label-schema.md). Disable the endpoint by setting `METRICS_ENABLED=false`.

### Legacy: External monitoring integrations

### External monitoring alternatives

For production environments, consider managed services instead of (or in addition to) the self-hosted stack above:

- **DataDog**: Comprehensive APM and infrastructure monitoring
- **New Relic**: Application performance monitoring
- **Prometheus + Grafana**: Self-hosted metrics and alerting
- **Cloud Provider Solutions**: AWS CloudWatch, GCP Operations, Azure Monitor

### Basic Health Checks

```bash
# Check MongoDB health
docker exec orkestra-mongodb mongosh --eval "db.adminCommand('ping')"

# Check Redis health
docker exec orkestra-redis redis-cli ping

# Check Gotenberg health (PDF service)
curl http://localhost:3030/health

# Check application health endpoints
curl http://localhost:3000/health  # Backend health
curl http://localhost:8080         # Frontend availability
```

## Backup & Restore

### Manual Backup Commands

**MongoDB Backup:**

```bash
#!/bin/bash
# Create timestamped backup
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="./backups/mongo_${TIMESTAMP}"

# Run mongodump
docker exec orkestra-mongodb mongodump \
  --uri="mongodb://admin:changeme@localhost:27017" \
  --out=/tmp/backup \
  --gzip

# Copy to host
docker cp orkestra-mongodb:/tmp/backup ${BACKUP_DIR}

# Upload to S3 (optional)
# aws s3 cp ${BACKUP_DIR} s3://orkestra-backups/mongo/${TIMESTAMP}/ --recursive
```

**Redis Backup:**

```bash
#!/bin/bash
# Force Redis to save current dataset
docker exec orkestra-redis redis-cli --pass changeme BGSAVE
sleep 5

# Copy RDB file
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
docker cp orkestra-redis:/data/dump.rdb ./backups/redis_${TIMESTAMP}.rdb
```

### Restore Procedures

**MongoDB Restore:**

```bash
# Stop application services
docker compose -f docker-compose.dev.yml down    # Or prod.yml
docker compose -f docker-compose.prod.yml down

# Restore from backup
docker exec orkestra-mongodb mongorestore \
  --uri="mongodb://admin:changeme@localhost:27017" \
  --gzip \
  /path/to/backup/directory

# Restart services
docker compose -f docker-compose.dev.yml up -d   # Or prod.yml
docker compose -f docker-compose.prod.yml up -d
```

**Redis Restore:**

```bash
# Stop Redis
docker compose stop redis

# Copy backup file
docker cp ./backups/redis_backup.rdb orkestra-redis:/data/dump.rdb

# Start Redis
docker compose start redis
```

## Common Operations

### Service Management

```bash
# Navigate to docker directory first
cd docker

# Start infrastructure only (shared by all environments)
docker compose -f docker-compose.infra.yml up -d

# Start development environment
docker compose -f docker-compose.dev.yml up -d

# Start production environment
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d

# View logs for specific services
docker compose -f docker-compose.dev.yml logs -f orkestra-backend orkestra-frontend-admin

# Restart a specific service
docker compose -f docker-compose.dev.yml restart orkestra-backend

# Scale production backend
docker compose -f docker-compose.prod.yml up -d --scale backend=3

# Stop application services (keep infrastructure running)
docker compose -f docker-compose.dev.yml down
docker compose -f docker-compose.prod.yml down

# Stop infrastructure (when completely done)
docker compose -f docker-compose.infra.yml down
```

### Troubleshooting

```bash
# Check service health
docker compose ps

# View detailed logs
docker compose logs --tail=100 backend

# Execute commands in containers
docker exec -it orkestra-backend sh
docker exec -it orkestra-mongodb mongosh

# Check network connectivity
docker network inspect orkestra-network

# Monitor resource usage
docker stats
```

### Development Workflow

```bash
# Start infrastructure (once per development session)
docker compose -f docker-compose.infra.yml up -d

# Hot reload development (recommended)
docker compose -f docker-compose.dev.yml up -d

# Rebuild after Dockerfile changes
docker compose -f docker-compose.dev.yml up -d --build backend

# Clean rebuild (remove cache)
docker compose -f docker-compose.dev.yml build --no-cache backend

# Update dependencies and rebuild
docker compose -f docker-compose.dev.yml pull
```

## Log Management - CRITICAL FOR DEVELOPMENT

### 🚫 NEVER manually start servers

The backend and frontend run automatically in Docker with hot reload. **ALWAYS** use Docker Compose commands to check logs and status.

### ✅ Essential Logging Commands

#### Backend Server Logs

```bash
# View backend logs (MOST IMPORTANT - use this to check server status)
docker compose logs backend

# Follow backend logs in real-time (for debugging)
docker compose logs -f backend

# View recent backend logs with timestamps
docker compose logs -t --tail=50 backend

# Search backend logs for errors
docker compose logs backend | grep -i error
```

#### Frontend Logs

```bash
# View admin frontend logs (Tier-1 operator console)
docker compose logs frontend-admin

# Follow admin frontend logs in real-time
docker compose logs -f frontend-admin

# Frontend build errors
docker compose logs frontend-admin | grep -i error
```

#### Infrastructure Logs

```bash
# Database logs
docker compose -f docker-compose.infra.yml logs mongodb

# Cache logs
docker compose -f docker-compose.infra.yml logs redis

# All infrastructure logs
docker compose -f docker-compose.infra.yml logs
```

#### Service Status

```bash
# Check all service health
docker compose ps

# Check infrastructure status
docker compose -f docker-compose.infra.yml ps

# View resource usage
docker stats
```

### 🔄 Hot Reload Status

```bash
# Backend: AIR hot reload logs
docker compose logs backend | grep -i "watching\|building\|reload"

# Frontend: Vite HMR logs
docker compose logs frontend-admin | grep -i "hmr\|updated\|ready"
```

### 🚨 Troubleshooting

```bash
# Restart specific service (rarely needed)
docker compose restart backend

# Rebuild service after Dockerfile changes
docker compose up -d --build backend

# View complete container information
docker inspect orkestra-backend-dev

# Access container shell for debugging (emergency only)
docker exec -it orkestra-backend-dev sh
docker compose -f docker-compose.dev.yml up -d --build

# Quick restart of application services (keep infrastructure running)
docker compose -f docker-compose.dev.yml restart
```

## Security Best Practices

### Container Security

1. **Alpine base images** - Minimal attack surface
2. **Non-root users** - Run containers as unprivileged users
3. **Image scanning** - Use Trivy/Snyk for vulnerability scanning
4. **Resource limits** - CPU/memory limits to prevent DoS
5. **Read-only filesystems** - Mount as read-only where possible
6. **Secrets management** - Never hardcode secrets in images
7. **Network isolation** - Use custom bridge networks
8. **Regular updates** - Keep base images and dependencies current

### Production Security Checklist

- [ ] Change all default passwords in `.env.prod`
- [ ] Use strong, unique passwords (>20 characters)
- [ ] Enable Docker Content Trust for image signing
- [ ] Configure firewall rules (only expose necessary ports)
- [ ] Set up SSL certificates for Nginx
- [ ] Enable audit logging for containers
- [ ] Implement backup encryption
- [ ] Configure log rotation and retention
- [ ] Set up intrusion detection
- [ ] Regular security scanning

---

## Module-Specific Guidelines

- **Environment Separation**: Maintain clear separation between infrastructure and application services
- **Security**: Use secure defaults and environment-specific configurations
- **Scalability**: Design for horizontal scaling in production environments
- **Monitoring**: Implement comprehensive health checks and logging
- **Backup**: Ensure data persistence and backup strategies for databases
- **Documentation**: Maintain clear instructions for development and production setup

---

### Related Guides

- [Project Overview](../CLAUDE.md) - System architecture and design principles
- [Backend Containerization](../backend/CLAUDE.md) - Go API server configuration
- [Frontend Containerization](../frontend-admin/CLAUDE.md) - React application setup
- [Documents Module](../backend/internal/addons/documents/CLAUDE.md) - PDF generation with Gotenberg
- [Deployment Scripts](../scripts/CLAUDE.md) - Automation and deployment orchestration
