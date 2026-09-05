# Module: Docker Infrastructure

_Path: `/docker`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

<!-- Navigation -->

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)

<!-- /Navigation -->

> ## ⚠️ ADR-0006 — core-only base
> This file predates the [core-only collapse](../docs/adr/0006-collapse-to-core-only-base.md) and still contains sections describing **removed** features. The current Docker topology is: **`docker-compose.infra.yml`** (MongoDB + Redis + RustFS) + **one app file per environment** (`docker-compose.{dev,staging,prod}.yml`) + an opt-in **`docker-compose.observability.yml`** overlay. Removed: the `minimal`/`full`/`dev-public`/`ai-sidecar` compose files, `ORKESTRA_PROFILE`, `DEV_COMPOSE_VARIANT`, the `/var/run/docker.sock` mount + `CONTAINER_CONTROL_ENABLED`, and the Gotenberg/Memgraph/Hindsight addon-infra services. Where a section below still describes those, treat it as **a fork's responsibility**, not the base.

## Multi-Stack Model

Every checkout × environment combination is **one Compose project**: `STACK=${APP_NAME}-${ENV}` (e.g. `orkestra-development`). Everything the stack owns is namespaced under that identity, so any number of stacks coexist on one host with zero overlap:

- **Containers**: `${APP_NAME}-<svc>-${ENV}` — e.g. `orkestra-backend-development` for the `backend` service of a stack running the `docker/.env.example` defaults (`APP_NAME=orkestra`, `ENV=development`).
- **Volumes**: `${STACK}_<vol>` — Compose auto-prefixes them (no pinned `name:` anymore).
- **Network**: `${STACK}_default` — Compose's own per-project default network. There is no shared `orkestra-network` bridge to create; nothing joins another stack's network.
- **Ports**: explicit values in `docker/.env`, seeded non-colliding by `scripts/init.sh` on first run (no arithmetic or shared defaults baked into compose).
- **Observability**: a per-stack overlay (`docker-compose.observability.yml`) layered **into** the same `${STACK}` project when opted in (`./orkestra.sh observability up`) — not a separate `orkestra-observability` project.
- **Service names** (`backend`, `frontend-admin`, `client-frontend`, `mongodb`, `redis`, `rustfs`, plus the observability services) are uniform across dev/staging/prod — only the container/volume/network layer is stack-namespaced. `docker compose ... <cmd> <service>` always takes the bare service name; `docker exec`/`docker inspect`/`docker logs` on a raw container need the full `${APP_NAME}-<svc>-${ENV}` name.

### ⚠️ A running container does not belong to the checkout you are standing in

The names above are **worked examples using the shipped defaults, not a description of
your machine.** Because every checkout × environment is its own project, one host
commonly runs several stacks from *different clones* at once — an upstream checkout on
`staging` and a fork's checkout on `development`, for instance. Nothing in a container's
name tells you which directory owns it; `APP_NAME` is per-checkout and arbitrary.

Acting on the wrong stack is a real failure mode: a `git pull` in one clone has **zero**
effect on another clone's containers, so "I updated the code and redeployed" can restart
a stack built from a completely different repository.

Always resolve ownership from the container's own Compose labels before acting on it:

```bash
docker inspect <container> \
  --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}} | {{index .Config.Labels "com.docker.compose.project"}}'
```

For *this* checkout's identity — which is only ever what its own env file says — read
`grep -E '^(APP_NAME|ENV)=' docker/.env`.

The rules above are the authoritative description; the model arrived in [#143](https://github.com/orkestra-cc/orkestra/pull/143) (`dde83ef3`), whose commit message and diff carry the migration detail.

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
# ALWAYS run this FIRST before any docker operations (run from the repo root)
grep -E "^(APP_NAME|ENV)=" docker/.env
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
./orkestra.sh                      # Main menu: full stack / observability

# CLI mode (scriptable, same operations)
ENV=development ./orkestra.sh deploy --scope backend --rebuild --yes
./orkestra.sh status
./orkestra.sh logs backend -f
./orkestra.sh observability up
./orkestra.sh --help               # Full command surface

# Validate docker/.env — required keys, security settings, same-site pairings.
# Takes no arguments (--help aside): the ENV= inside the file selects the rules.
./scripts/env-validate.sh
```

### What belongs in `.env` / compose vs `/admin/modules`

After the architecture-modernization refactor, **module-level configuration lives in MongoDB** (`module_configs`), is edited at `/admin/modules`, and secrets are AES-256-GCM-encrypted with `OAUTH_TOKEN_ENCRYPTION_KEY`. The `EnvVar` field on each module's `ConfigSchema()` is consulted **only on first-boot seeding** (`pkg/sdk/module/config_service.go::buildInitialConfig`); after that the document is authoritative and editing the env var has no effect.

Keep this split when touching `.env*` or `docker-compose.*.yml`:

| Bucket | Owner | Goes in compose / `.env` |
|--------|-------|--------------------------|
| Boot identity (`APP_NAME`, `ENV`, `PORT`, host/port mappings) | process | ✅ yes |
| Database connections (`MONGO_URI`, `REDIS_URL`, credentials) | process | ✅ yes |
| JWT keys, cookies, CORS, rate limits, observability | process | ✅ yes |
| `LOKI_QUERY_URL` / `GRAFANA_URL` | process — trusted Tier-1 log-preview upstream + optional browser deep-link base; never derived from request data | ✅ yes |
| Encryption keys (`OAUTH_TOKEN_ENCRYPTION_KEY`, `ORKESTRA_KMS_MASTER_KEY`, optional `MFA_SECRET_ENCRYPTION_KEY`) | process — bootstraps ConfigService | ✅ yes |
| Process-scoped auth tunables (`AUTH_REQUIRE_EMAIL_VERIFICATION`, `AUTH_RISK_STEP_UP_THRESHOLD`, `WEBAUTHN_RP_ID`, `AUTH_GEOIP_DB_PATH`, `TENANT_KIND_ENFORCEMENT`, `CEDAR_ENFORCE_ACTIONS`) | process | ✅ yes |
| `ORKESTRA_VERSION` | process — application version surfaced in the SPA footer (frontend-admin + frontend-client) and embedded in the dev `/health` JSON. `orkestra.sh` auto-exports this from `git describe --tags --always --dirty`, and docker-compose substitutes it into both frontend `environment:` blocks (dev/staging dev-server) and the `args:` block in `docker-compose.prod.yml` (production image build). CI overrides it with `--build-arg ORKESTRA_VERSION=${{ github.ref_name }}` on tag pushes. The container has no git binary and no `.git`, so this host-side env var is the only path that delivers a real version — without it the SPA falls back to `"dev"`. | ✅ yes |
| `STORAGE_ENDPOINT` / `STORAGE_PUBLIC_ENDPOINT` / `STORAGE_REGION` / `STORAGE_BUCKET_PREFIX` / `STORAGE_ACCESS_KEY` / `STORAGE_SECRET_KEY` / `STORAGE_FORCE_PATH_STYLE` / `STORAGE_ENSURE_BUCKET` | process — S3-compatible object storage consumed by `internal/shared/blob` for user-uploaded blobs. One connection vends **per-domain buckets** `<STORAGE_BUCKET_PREFIX>-<domain>` (e.g. `orkestra-avatars`, `orkestra-crm-photos`) through `blob.Provider` / `iface.ObjectStoreProvider` (ADR-0011). `STORAGE_ENDPOINT` is the endpoint the backend uses for its own ops; `STORAGE_PUBLIC_ENDPOINT` (optional) is the browser-reachable host baked into presigned PUT/GET URLs — set it when RustFS sits behind a proxy the SPA must reach while the backend keeps using the internal endpoint (see RustFS reachability gotcha). `STORAGE_BUCKET` is **deprecated** (superseded by the prefix; a custom value is ignored with a boot WARN). Process-scoped because rotating credentials at runtime would invalidate every in-flight presigned URL. **The presign endpoint must be browser-reachable** for upload PUTs to succeed (RustFS gotcha in Infrastructure Services). **Production**: point `STORAGE_ENDPOINT` at a managed S3, pre-provision the per-domain buckets, set `STORAGE_ENSURE_BUCKET=false` (IAM rarely grants CreateBucket) + `STORAGE_FORCE_PATH_STYLE=false`, and apply per-bucket lifecycle (expire orphaned uncommitted uploads) + backup policies. **Credentials**: `STORAGE_SECRET_KEY` is generated by `make init` and, on the bundled RustFS, *is* the store's root secret — `docker-compose.infra.yml` derives `RUSTFS_ACCESS_KEY` / `RUSTFS_SECRET_KEY` from the `STORAGE_*` pair unless `RUSTFS_ROOT_USER` / `RUSTFS_ROOT_PASSWORD` override it. No compose file gives a credential a literal fallback (`docker/tests/credential-fallbacks.test.sh` is the gate), `scripts/env-validate.sh` refuses a placeholder or a secret under 16 characters in staging/production, and `config.Validate()` refuses to boot on one. Rotation recipe under "RustFS credentials" in Infrastructure Services. | ✅ yes |
| OAuth provider credentials (`OAUTH_GOOGLE/APPLE/GITHUB/DISCORD_*`) | ConfigService (auth module) | ❌ admin UI |
| AI provider keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OLLAMA_BASE_URL`) | ConfigService (auth/notification, shared) | ❌ admin UI |
| SMTP / notification settings (`SMTP_*`, `NOTIFICATION_EMAIL_*`) | ConfigService (notification module) | ❌ admin UI |
| Per-module enable flags (`*_ENABLED`) | DB (`module_configs.enabled`) flipped at runtime | ❌ admin UI |

> A fork's addon modules add their own ConfigService-managed env (Stripe, OpenAPI/SDI, Gotenberg, graph/agents URLs, …) — those left with the addons (ADR-0006) and are configured at `/admin/modules` in a fork that re-adds them.

For first-boot bootstrap of a fresh deployment without using the admin UI, export the seed env vars in the shell before `docker compose up` — they're listed as commented stubs at the bottom of `docker/.env`. Once the document exists, those env vars become inert.

Vars **deleted as dead code** during the cleanup (do not re-add): `MODULES`, `BACKEND_HOST`, `FRONTEND_HOST`, `SIGNOZ_ENABLED`, `MAX_FILE_SIZE`, `ALLOWED_FILE_TYPES`, `HINDSIGHT_LLM_*` (the agents module reuses the AI Models default LLM).

### Environment Behavior Matrix

| Feature | Development | Staging | Production |
|---------|-------------|---------|------------|
| `ENV` value | `development` | `staging` | `production` |
| Log Format | Text (pretty) | JSON | JSON |
| Error Details | Exposed | Hidden | Hidden |
| Cookie Secure | `false` | `true` | `true` |
| Cookie SameSite | `lax` | `lax` | `lax` |
| Rate Limits | 1000/min | 60/min | 30/min |
| Debug Mode | Enabled | Disabled | Disabled |
| HTTPS Required | No | Yes | Yes |
| Observability | Disabled | Enabled | Enabled |

> **`COOKIE_SAME_SITE` is a dead key.** `config.go:339` reads it into
> `CookieConfig.SameSite` and nothing ever reads it back — every mint path writes
> `http.SameSiteLaxMode` as a Go literal (`utils/http.go:62,77,127,150`,
> `middleware/device.go:68`, `setup/routes.go:442`,
> `oauth_state_binding.go:56,71`, `password_handler.go:421`). So the SameSite row
> above is `lax` in all three columns, and there is **no configuration path to
> `SameSite=None`** without a code change. Do not reach for this key to fix a
> cross-site cookie problem — fix the host layout instead (below).

---

## Clean Docker Compose Architecture

> **ADR-0006:** the runtime-profile (`minimal`/`full`) and `dev-public` variant compose files, the AI sidecar, and the addon infra (Gotenberg / Memgraph / Hindsight) were removed when Orkestra collapsed to a core-only base. The topology is now **one infra base + one app file per environment + an opt-in observability overlay**. Sections below that still mention SKU profiles, `ORKESTRA_PROFILE`, `DEV_COMPOSE_VARIANT`, or backend-managed containers describe a fork's responsibility, not the base.

### Core Philosophy

Separate infrastructure from applications. One infra compose + one app compose per environment:

1. **`docker-compose.infra.yml`** — infrastructure only: MongoDB, Redis, RustFS (S3-compatible blob store). No addon infra.
2. **`docker-compose.dev.yml`** — development stack (hot reload). The backend builds `Dockerfile.dev-backend` (public `golang:alpine`, AIR pre-baked; `GO_BASE` build-arg for a Chainguard base); frontends use `node:24-alpine`.
3. **`docker-compose.staging.yml`** — application services in staging mode (staging-like env + AIR/Vite hot reload).
4. **`docker-compose.prod.yml`** — application services in production mode.
5. **`docker-compose.observability.yml`** — opt-in OTEL overlay (Tempo, Prometheus, Loki, Promtail, Grafana). Runs alongside any app stack.

### File Organization

```
/                              # Project root
├── README.md
├── orkestra.sh                # Unified TUI + CLI for the whole stack (replaces deploy.sh and logs.sh)
└── docker/
    ├── docker-compose.infra.yml   # Infrastructure: MongoDB, Redis, RustFS
    ├── docker-compose.dev.yml     # Development: backend (AIR) + both frontends (Vite), public Alpine
    ├── Dockerfile.dev-backend     # Dev backend image (golang:alpine + AIR; GO_BASE build-arg)
    ├── docker-compose.staging.yml # Staging: AIR/Vite hot reload + staging-like env
    ├── docker-compose.prod.yml    # Production: optimized backend + frontends
    ├── docker-compose.observability.yml # Opt-in OTEL overlay
    ├── .env.example               # Template for environment files
    ├── .env                       # Active env (gitignored) - contains ENV=development|staging|production
    ├── keys/                      # JWT and OAuth keys (gitignored)
    └── mongo-init/                # MongoDB initialization scripts
```

### Environment Combinations

- **Development**: `docker-compose.infra.yml` + `docker-compose.dev.yml` + `.env` (with `ENV=development`)
- **Staging**: `docker-compose.infra.yml` + `docker-compose.staging.yml` + `.env` (with `ENV=staging`)
- **Production**: `docker-compose.infra.yml` + `docker-compose.prod.yml` + `.env` (with `ENV=production`)
- **+ Observability** (any env): add `-f docker-compose.observability.yml` or `./orkestra.sh observability up`

**IMPORTANT**: All Docker files must remain in `/docker` directory for proper build contexts.

## Quick Start

### Prerequisites

```bash
# First-time setup — scaffolds docker/.env with random secrets, generates RS256
# JWT keys under docker/keys/, and seeds a non-colliding block of host ports.
# Idempotent: re-runs preserve existing files unless --force.
make init                # from the repo root
# equivalently:
./orkestra.sh init       # same script, via the TUI entry point
bash scripts/init.sh     # direct invocation
```

For a multi-environment setup (separate `.env.development` / `.env.staging` / `.env.production`), copy the generated `docker/.env` to per-env files after `make init` and tweak each. See [Three-Stage Environment Workflow](#three-stage-environment-workflow) above.

### Using orkestra.sh (Recommended)

`orkestra.sh` is the single entry point for every stack operation — interactive TUI and scriptable CLI. The TUI's main menu is **Full stack** (dev / staging / prod, auto-detected from `docker/.env`) or **Observability**.

```bash
# Interactive TUI
./orkestra.sh

# CLI mode (ENV from docker/.env, or ENV=... prefix)
ENV=development ./orkestra.sh deploy [--scope all|backend|frontend-admin|frontend-admin+backend|infra] [--rebuild] [--yes]
./orkestra.sh stop [--with-infra]
./orkestra.sh status
./orkestra.sh logs <service> [-f] [-n N] [-t]
./orkestra.sh observability {up|down|reset|status|info|logs}
```

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

### Infrastructure Services (`docker-compose.infra.yml`)

**One instance per stack** — layered into the same `${STACK}` Compose project as the app services, not shared across stacks (see [Multi-Stack Model](#multi-stack-model)). Host ports below are the compose defaults; `docker/.env` overrides them per stack so several stacks coexist. They bind to `INFRA_BIND_ADDRESS` (default `127.0.0.1`) — except the RustFS S3 API port, which follows `HOST_BIND_ADDRESS` because presigned browser uploads reach it through the reverse proxy when `STORAGE_PUBLIC_ENDPOINT` is set. Nothing else off-host needs them — the backend uses service names on the stack network and backup/restore/audit go through `docker exec` — and a port published on `0.0.0.0` bypasses the host firewall. Every infra service runs with `cap_drop: [ALL]` + `no-new-privileges`. Mongo **and redis** re-add `CHOWN/DAC_OVERRIDE/SETGID/SETUID`: mongo for its entrypoints' keyfile write, chown and gosu; redis because its entrypoint fixes `/data` ownership and drops to the `redis` user only when it holds setuid+setgid — without them it stays root, and a **fresh** volume (seeded 999-owned from the image) then refuses `appendonlydir` outright. In both cases the capabilities buy a privilege *drop*: the server process ends up non-root holding none of them.

| Service       | Host port (default) | Purpose             | Health Check     |
| ------------- | ----------- | ------------------- | ---------------- |
| **mongodb**   | 27017       | Primary database (single-node `rs0`) | writable-primary check |
| **redis**     | 6379        | Cache & sessions    | redis-cli incr ping |
| **rustfs**    | 9100 / 9101 | S3-compatible object storage for user-uploaded blobs (avatars today) | wget /health |

> **ADR-0006:** the Gotenberg / Memgraph / Hindsight addon-infra services, the backend-managed-container wiring (`Module.InfraContainers()` → `shared/container.Manager`), the `/var/run/docker.sock` mount, and `CONTAINER_CONTROL_ENABLED` were all removed with the addons. A fork that adds a module declaring `InfraContainers()` re-adds the socket mount + `CONTAINER_CONTROL_ENABLED=true` and provisions its own infra service. The `shared/container.Manager` seam is kept in the codebase for that purpose.

**MongoDB transaction readiness:** The bundled Mongo service runs as a
single-node replica set named `rs0`; the app relies on MongoDB transactions
(tenant provisioning during setup finalization is the first thing to hit one),
which standalone `mongod` does not support. `mongo-init/replica-entrypoint.sh`
derives the internal replica key file from the generated root password on first
start and retains it in the stack's `mongodb-config` volume, so there is no
extra secret to manage. The health check idempotently calls `rs.initiate()` for
an unconfigured volume and reports healthy only after the member is writable
primary. Backend connection strings must keep both `replicaSet=rs0` and
`directConnection=true`: the latter lets a host-side tool connect through the
published port while containers connect by the `mongodb` service name, without
depending on the advertised member name. Changing an existing stack requires
**recreating** the Mongo container so it starts with `--replSet` (a plain
`restart` is not enough); the data volume is retained and initialized in place.
`docker/tests/mongodb-replica-set.test.sh` is a static gate wired into
`make ci-backend` — it exists because a merge once kept the capabilities the
entrypoint needs while dropping the entrypoint itself, leaving a standalone
mongod whose only symptom was a failure at setup-finalization time.

**RustFS status note**: RustFS 1.x is in beta (`rustfs/rustfs:latest` resolves to `1.0.0-beta.4` at time of writing). Single-node S3-API mode is "available" and fine for avatars; distributed mode is "under testing". Production deploys running at scale should swap `STORAGE_ENDPOINT` to a managed S3 (AWS / Backblaze B2 / etc.) and drop `STORAGE_FORCE_PATH_STYLE`. The backend's `internal/shared/blob` package speaks the S3 API uniformly via AWS SDK v2, so swapping is an env-var change.

**RustFS credentials**: the store's root pair is `STORAGE_ACCESS_KEY` / `STORAGE_SECRET_KEY` — `docker-compose.infra.yml` maps them onto RustFS's documented `RUSTFS_ACCESS_KEY` / `RUSTFS_SECRET_KEY` (the `RUSTFS_ROOT_USER` / `RUSTFS_ROOT_PASSWORD` spellings it used to set are undocumented aliases), and `RUSTFS_ROOT_*` in `docker/.env` override the derivation for a deployment whose backend must not be root. No compose file carries a literal fallback for any credential any more: the base once shipped `changeme-rustfs` as both the example `STORAGE_SECRET_KEY` and the RustFS fallback, which made every un-edited checkout a browser-facing S3 API whose root password was printed in this repository. `make init` generates the secret, `scripts/env-validate.sh` refuses a placeholder (or anything under 16 characters) before a staging/production deploy, `config.Validate()` refuses to boot on one, and `docker/tests/credential-fallbacks.test.sh` keeps the fallbacks out. A deployment with object storage disabled (both `STORAGE_*` keys empty) still starts the bundled rustfs container with the infra stack, so it needs a `RUSTFS_ROOT_*` pair of its own — the infra file refuses to render without any credential, and the validator says so first. RustFS reads the pair from the environment on every start and does not persist it in the volume, so **rotation** is: set the new values, recreate `rustfs` then `backend`, then `redis-cli --scan --pattern 'blob:url:*' | xargs redis-cli DEL` — presigned GET URLs cached under the old signature otherwise 403 for up to an hour. The key id rides in every presigned URL and is not secret. **Least privilege** on the bundled store: RustFS serves the MinIO-compatible admin API, so a dedicated backend user with a policy scoped to `arn:aws:s3:::<STORAGE_BUCKET_PREFIX>-*` (`s3:CreateBucket`, `s3:ListBucket`, `s3:GetBucketLocation`, `s3:GetBucketCORS`, `s3:PutBucketCORS` on the buckets; `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject` on their objects) can be created with `mc admin user add` / `mc admin policy attach` against the root pair; `STORAGE_*` then names that user and `RUSTFS_ROOT_*` keeps the root.

**Redis eviction policy is load-bearing for authz** — do not add a `maxmemory` with an `allkeys-*` policy, and check the setting on any managed Redis you point this stack at. The effective-permission cache retires verdicts by bumping two counters (`authz:gen`, `authz:gen:<userUUID>`) that carry **no TTL**; an evicted counter reads back as `0` and *resurrects* the pre-revocation entries still inside their 60s TTL, so a revoked permission silently comes back. `noeviction` and the `volatile-*` policies are both safe (`volatile-*` only ever selects keys that have a TTL, which these do not); every `allkeys-*` policy is not. The bundled service sets no `maxmemory`, so Redis's own defaults apply — verified on the running stack as `maxmemory 0` / `maxmemory-policy noeviction`. See the cache invariant in [`backend/internal/core/authz/CLAUDE.md`](../backend/internal/core/authz/CLAUDE.md).

**Redis persistence**: the compose `command:` replaces the image's entrypoint, which is what used to pass `--dir`; without it `redis-server` writes its RDB and AOF into its working directory, `/`, outside the `redis-data` volume, and every recreate started from an empty dataset. The command therefore carries `--dir /data` explicitly and `docker/tests/credential-fallbacks.test.sh` asserts it stays there. `backup.sh` / `restore.sh` read `CONFIG GET dir` from the live server either way, so a bundle taken before the fix restores into the right place.

**RustFS endpoint reachability gotcha**: the backend uses `STORAGE_ENDPOINT` for its own ops (HEAD/Delete/Get). The **browser** uploads to a *presigned* URL, whose host is `STORAGE_PUBLIC_ENDPOINT` when set, else `STORAGE_ENDPOINT`. The internal default `http://rustfs:9000` (the compose **service** name — stable across stacks; not the stack-namespaced container name) works for the backend but leaves browser uploads broken unless rustfs is also reachable from the host at that URL. Two fixes:

- **Single-endpoint (local dev)**: make `STORAGE_ENDPOINT` itself browser-reachable — add `127.0.0.1 rustfs` to `/etc/hosts` and publish `RUSTFS_API_PORT=9000` (default 9100). Works because a plain browser can reach `rustfs:9000` directly with no proxy in between.
- **Dual-endpoint (behind a TLS proxy, e.g. Cloudflare/HAProxy)**: keep `STORAGE_ENDPOINT=http://rustfs:9000` (internal, for backend ops) and set `STORAGE_PUBLIC_ENDPOINT` to the browser-reachable public host (e.g. `https://storage.example.com`). Only the presigned PUT/GET URLs get the public host; the backend never hairpins through the proxy. **This split is required behind Cloudflare** — presigned URLs sign only `host` and survive proxying, but the backend's SDK-signed HEAD/Delete sign more headers and **403 through the proxy**, so they must hit the origin directly. The proxy must preserve the `Host` header (the presigned signature validates against it) and answer the browser's CORS preflight (`OPTIONS` → 204 + `Access-Control-Allow-Origin: <SPA origin>`, methods `GET,PUT,HEAD,OPTIONS`). Wired via `blob.S3Config.PublicEndpoint` (ADR-0011). Production: terminate rustfs behind a publicly-resolvable hostname or use managed S3 (where endpoint == public endpoint and the split is moot).

### Application Services

#### Development (`docker-compose.dev.yml`)

**Hot-reload dev stack on public Alpine images (Dockerfile.dev-backend; Chainguard via GO_BASE build-arg).**

| Service                  | Host port | Purpose                              | Features                                                       |
| ------------------------ | --------- | ------------------------------------ | -------------------------------------------------------------- |
| **backend**              | 3007      | Go API server                        | Hot reload (AIR), debug logs                                   |
| **frontend-admin**             | 8087      | Operator console (Tier-1)            | Vite dev server, HMR; host `localhost`; consumes the operator API on `localhost:3000` (same site — see below) |
| **client-frontend**      | 8081      | Tier-2 client demo SPA               | Vite dev server, HMR; host `client.localhost`; consumes the client API on `client.localhost:3000` (same site — see below) |

#### Staging (`docker-compose.staging.yml`)

**Hot-reload stack with staging-like behavior (cookie strict, JWT, CORS, rate limits).** Used as the primary development environment on long-lived VMs that mirror production-style URLs/cookies but still need fast iteration.

| Service                      | Host port | Purpose                              | Features                                                        |
| ---------------------------- | --------- | ------------------------------------ | --------------------------------------------------------------- |
| **backend**                  | 3000      | Go API server                        | Hot reload (AIR), staging-like env, RS256 JWT                   |
| **frontend-admin**           | 8080      | Operator console (Tier-1)            | Vite dev server, HMR; host `staging.orkestra.cc`                |
| **client-frontend**          | 8081      | Tier-2 client demo SPA               | Vite dev server, HMR; host `app.orkestra.cc`; consumes `staging-api.*` |

Service names are uniform across dev/staging/prod (`backend`/`frontend-admin`/`client-frontend`); only the `container_name:` is stack-namespaced (`${APP_NAME}-<svc>-${ENV}`), e.g. `orkestra-backend-development` on the shipped defaults.

The backend mounts `../backend:/app` and runs AIR from the bind mount — no image rebuild on code change. AIR and the Go module/build cache live under `backend/.go-bin/` and `backend/.go-mod-cache/` (gitignored), pre-installed by the host. To bootstrap on a fresh machine:

```bash
cd backend
GOMODCACHE=$PWD/.go-mod-cache go mod download
GOBIN=$PWD/.go-bin GOMODCACHE=$PWD/.go-mod-cache go install github.com/air-verse/air@v1.67.1
```

**AIR is pinned, never `@latest`.** A release that raises its `go` directive above the project's toolchain (`go.mod`, `.mise.toml`, CI) refuses to build under `GOTOOLCHAIN=local` — air v1.67.2 did exactly that on Go 1.25, and `@latest` would have broken every dev-image build the day it was tagged. The pin lives in `Dockerfile.dev-backend` (`ARG AIR_VERSION`), `backend/Dockerfile`, `backend/Makefile` and `scripts/install-air.sh`; bump all four together with the Go version (currently Go 1.26.8 / air v1.67.4).

`userns_mode: "host"` is required when the Docker daemon runs with `userns-remap` (otherwise `group_add` for the docker GID is rewritten and the mounted socket stays unreadable). DNS is inherited from the daemon (`/etc/docker/daemon.json` → `dns: [...]`); the staging compose deliberately does **not** set per-service `dns:`, because public resolvers (8.8.8.8, 1.1.1.1) may be blocked on UDP/53 in restricted networks.

#### Production (`docker-compose.prod.yml`)

**Optimized production services**

| Service      | Host port (default) | Purpose       | Features                       |
| ------------ | --------- | ------------- | ------------------------------ |
| **backend**  | `${BACKEND_PORT:-3000}`  | Go API server | Optimized build, health checks, `read_only` rootfs |
| **frontend-admin** | `${FRONTEND_PORT:-8080}` | React web app | Nginx static serving           |

Both bind to `${HOST_BIND_ADDRESS:-127.0.0.1}` — closed by default; `docker/.env` opens them to the address the reverse proxy reaches. See [Container hardening](#container-hardening) for the capability/rootfs contract.

## Network Architecture

### Internal Communication

- **Network**: `${STACK}_default` — Compose's own per-project default network (no `name:`/`external:` pin; see [Multi-Stack Model](#multi-stack-model)). Each stack gets its own network; nothing is shared across stacks.
- **Service Discovery**: Docker internal DNS resolution by **service name** (`backend`, `mongodb`, `otel-collector`, …) — stable across stacks since only the container/network layer is namespaced.
- **Security**: Isolated per-stack container network, no external access to internal services
- **Subnet**: Docker-assigned per project (no fixed subnet pinned in compose)

### Host split (ADR-0003)

The backend serves three audiences from one Go binary, dispatched by `Host` header at the application layer (no reverse-proxy needed for the split itself):

| Audience | Default host (dev) | Default host (prod) | Purpose |
|---|---|---|---|
| `operator` | `console.localhost:3000` | `console.orkestra.com` | Tier-1 operator dashboard — module admin, SDI/FatturaPA self-invoicing, dev tooling |
| `client` | `client.localhost:3000` | `api.orkestra.com` | Tier-2 client tenants — subscriptions, payments, future AI runtime (PR-E) |
| `service` | *(internal docker network only)* | *(internal docker network only)* | AI sidecar `/v1/internal/*` — never published by ingress |

The host mux ([cmd/server/hostmux.go](../backend/cmd/server/hostmux.go)) indexes each configured host and, when it carries a port, its bare form too ([`hostmux.go:61-67`](../backend/cmd/server/hostmux.go)); on the request side it matches `r.Host` exactly, then bare ([`:75-84`](../backend/cmd/server/hostmux.go)). It then dispatches to the matching audience's chi.Mux — so `CLIENT_API_HOST=client.localhost` and `client.localhost:3000` behave the same. Each mux mounts its own `RequireAudience` middleware ([shared/middleware/audience.go](../backend/internal/shared/middleware/audience.go)) so a token issued for the wrong audience is rejected before any handler runs (defense in depth above per-route RBAC).

**Dev fallthrough**: when `ENV=development` an unmatched Host falls through to the operator mux, so `curl http://localhost:3000` keeps working without `/etc/hosts` gymnastics. In staging/prod an unmatched Host returns 421 Misdirected Request — the canonical signal that an HTTP/1.1 request reached a server that doesn't serve it. This closes the door on host-header smuggling against the Tier-1 console.

#### Client tier: the SPA and the client API must be same-site

`CLIENT_API_HOST` is not a free choice. Every client-tier cookie the backend mints
is `SameSite=Lax` with an **empty `Domain`** (host-only) — the refresh cookie
(`password_handler.go:411-424`, `utils/http.go:53-64`), the OAuth state cookie
(`oauth_state_binding.go:56,71`) and the device cookie
(`middleware/device.go:61-69`). A `SameSite=Lax` cookie is neither stored from nor
sent on a **cross-site** subresource request, and the client SPA reaches its API
exclusively through `fetch(..., {credentials: "include"})`.

`localhost` is not in the Public Suffix List, so Chromium's `SchemefulSite` falls
back to `scheme://host`: `client.localhost` and `api.localhost` are **different
sites**. The historical dev layout (SPA on `client.localhost:8081`, client API on
`api.localhost:3000`) therefore could never carry them — measured on Chrome 151,
client login succeeds and the refresh cookie is simply absent from the jar, so
`POST /v1/auth/client/refresh-cookie` answers `401 No refresh token provided`.
Moving only the API hostname to `client.localhost` makes the cookie appear.

Hence the dev defaults: SPA `http://client.localhost:8081`, client API
`http://client.localhost:3000`. **A port is not part of a site**, so the two share
a site while staying cross-*origin* — the CORS preflight and
`Access-Control-Allow-Credentials` are still on the path, which is what
`CLIENT_CORS_ORIGINS` is for. Neither cookie knob is an alternative:
`CLIENT_COOKIE_DOMAIN` cannot help (SameSite is computed from the request's site,
not from the cookie's `Domain`) and `COOKIE_SAME_SITE` is unread (above).

Staging and prod keep the three-host ADR-0003 split, because there the hosts *do*
share a registrable domain — `app.orkestra.cc` and `staging-api.orkestra.cc` are
both `orkestra.cc`, i.e. same-site. The rule is "same site", not "same host"; only
`*.localhost` forces the hostnames to coincide.

**Upgrading an existing dev checkout.** A `docker/.env` written before this change
carries the previous triple — `CLIENT_API_HOST=api.localhost`,
`CLIENT_API_URL=http://api.localhost:3000`,
`CLIENT_FRONTEND_URL=http://localhost:8081` — and an existing `.env` value always
wins over the compose default, so only the SPA moves: it is built with
`VITE_CLIENT_API_BASE`, which is **not** in `.env.example` and therefore takes the
new compose default `http://client.localhost:3000`, while the client mux is still
listening on `api.localhost`. In dev that is not a connection error — the
unmatched Host falls through to the operator mux (above), which mounts no
`/v1/auth/client/*` routes, so every client-tier call answers **404**. Migrate the
three keys by hand:

```bash
CLIENT_API_HOST=client.localhost
CLIENT_API_URL=http://client.localhost:3000
CLIENT_FRONTEND_URL=http://client.localhost:8081
```

**`scripts/env-validate.sh` refuses the un-migrated pairing.** With scheme and
port stripped and the hostname lowercased, `CLIENT_API_HOST`, `CLIENT_API_URL`
and `CLIENT_FRONTEND_URL` must resolve to the same **site** — each key compared
only when it is set. The operator twin is checked the same way: `VITE_API_URL`
against `FRONTEND_URL`.

"Site" is what a browser computes, because that is what decides whether the
cookie survives. Under `*.localhost`, which has no Public Suffix List entry, the
site is the **whole hostname** — `client.localhost` and `api.localhost` are
different sites, and the dev hostnames must therefore be identical. Everywhere
else it is the **last two labels**, so `staging.orkestra.cc` and
`staging-api.orkestra.cc` are one site and a real deployment passes untouched.
Those two labels are a deliberate eTLD+1 approximation: a shell script has no
PSL, so a `co.uk`-style suffix under-reports and the check can miss a genuine
cross-site pairing — it never invents one, which is the safe direction for
something that hard-stops a deploy. Ports cannot take part, because
`.env.example` and the wizard write these hosts bare while the compose defaults
write them ported. A `CLIENT_API_HOST` under the RFC 2606 `.invalid` TLD is how a
deployment says it has **no** client tier (staging uses
`client-disabled.invalid`); the client pairing is then skipped with a note.

Both groups are checked in **every** `ENV`, not only development, and a mismatch
is an **error**, not a warning, whose remediation is the three-key block above.

It runs from two places, with deliberately different severities:
`./orkestra.sh init` prints whatever the validator reported and carries on (the
wizard user is still mid-file), while `./orkestra.sh deploy` runs it in
**"Pre-deployment checks"** — ahead of the image build and every `docker compose`
command — and **aborts**. `--yes` skips confirmation prompts, never this. The
rule is pinned by `scripts/test-orkestra-helpers.sh`, which runs the validator
against a scratch `.env` and drives the deploy preflight with a stubbed `docker`
to prove it aborts having issued no compose command (spec §8 follow-up #16).

**The operator console is bound by the same condition**, and only its shipped
default satisfies it. It has no `OPERATOR_API_HOST`: the console SPA calls
whatever `VITE_API_URL` says (`docker-compose.dev.yml:205`, default
`http://localhost:3000`), with `credentials: 'include'` on every request
(`frontend-admin/src/store/api/baseApi.ts:295-297`). Opened at
`http://localhost:8080` — the shipped entry point, and the only hostname besides
`127.0.0.1` in the console dev server's allow-list
(`frontend-admin/vite.config.js:366-381`; `VITE_ADMIN_ALLOWED_HOSTS` is empty by
default) — origin and API are both `localhost`, hence same site. Open that same
console at `http://console.localhost:8080` while `VITE_API_URL` still points at
`http://localhost:3000` and the operator tier reproduces this bug exactly:
`POST /v1/auth/operator/refresh-cookie` becomes a cross-site credentialed
request and drops the `SameSite=Lax` cookie. **Both on `localhost`, or both on
`console.localhost` (which also needs `VITE_API_URL=http://console.localhost:3000`
and `console.localhost` in `VITE_ADMIN_ALLOWED_HOSTS`) — never one of each.**

**The shipped convention is `localhost` end to end** (spec §8 follow-up #13): the
console's origin (`FRONTEND_URL=http://localhost:8080`), the API its SPA calls
(`VITE_API_URL=http://localhost:3000`), the code fallbacks in
`frontend-admin/src/config/environment.ts` and `public/config.example.js`, and the
compiled OAuth callback defaults (`http://localhost:3000/v1/auth/oauth/…`) all name
one host, and `scripts/env-validate.sh` refuses a **mixed** pairing (either
convention passes; one of each does not). It costs no env key, no allow-list entry
and no host registration — `localhost:3000` reaches the operator mux through the
dev fallthrough above. `CONSOLE_HOST` keeps its `console.localhost:3000` default
and is what it has always been in practice, a staging/production knob: nothing
stops a contributor putting the console on `console.localhost` end to end, but the
docs, the defaults and the OAuth recipe all prescribe `localhost`.

**Per-audience env vars** (compose passes these through to the backend):

| Variable | Purpose | Default |
|---|---|---|
| `CONSOLE_HOST` | Hostname the operator mux answers on | `console.localhost:3000` (dev) / unset (prod, operator-set) |
| `CLIENT_API_HOST` | Hostname the client mux answers on. **Must be same-site with the client SPA's origin** — see "Client tier: the SPA and the client API must be same-site" above. A host under the RFC 2606 `.invalid` TLD (e.g. `client-disabled.invalid`) disables the client tier: no Host can ever match it, and `env-validate` skips the pairing check. | `client.localhost:3000` (dev) / unset (prod) |
| `OPERATOR_CORS_ORIGINS` | CORS allowlist for operator mux | falls back to `CORS_ORIGINS` |
| `CLIENT_CORS_ORIGINS` | CORS allowlist for client mux | falls back to `CORS_ORIGINS` |
| `OPERATOR_RATE_LIMIT_REQUESTS_PER_MINUTE` / `_BURST` | Per-audience throttling | falls back to `RATE_LIMIT_*` |
| `CLIENT_RATE_LIMIT_REQUESTS_PER_MINUTE` / `_BURST` | Per-audience throttling | falls back to `RATE_LIMIT_*` |
| `OPERATOR_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for operator-tier tokens (ADR-0003 PR-D D-9). | **empty / host-only** (dev — works on localhost, *.localhost, AND a LAN IP) / empty (prod, operator-set for cross-subdomain) |
| `CLIENT_COOKIE_DOMAIN` | Refresh-cookie `Domain=` for client-tier tokens. | **empty / host-only** (dev) / empty (prod, operator-set for cross-subdomain) |
| `OPERATOR_FRONTEND_URL` | Operator-tier SPA origin (`console.*`) used to build verify-email / reset-password links in transactional email. | falls back to `FRONTEND_URL` |
| `CLIENT_FRONTEND_URL` | Client-tier SPA origin (`app.*`) used to build verify-email / reset-password links for signups landing on the client API host. | falls back to `FRONTEND_URL` |
| `TRUSTED_PROXY_CIDRS` | Networks your reverse proxies live in. **Preferred** form — survives a topology change without a recount. | empty |
| `TRUSTED_PROXY_COUNT` | How many proxy hops sit in front of the backend. Used only when `TRUSTED_PROXY_CIDRS` is empty. | `0` (dev) / `2` (staging: Cloudflare → HAProxy) / `0` (prod — **set this before going live**) |
| `API_DOCS_ENABLED` | Register `/docs` + `/openapi.json`. The spec is a complete route inventory and the docs page runs a third-party bundle on the API origin (the one holding the refresh cookie) — gate both paths at the edge if you turn this on in prod. | `true` (dev) / `false` (staging, prod) |

**Trusted proxies are a security control, not a logging nicety.** `X-Forwarded-For` is an ordinary request header, so with no policy configured the backend ignores it entirely and attributes every request to its direct peer. Behind a proxy that means all callers collapse onto the proxy's address — they share one login rate-limit bucket, the operator IP allowlist matches nothing real, and geo-blocking sees the proxy's country. Set one of the two vars in any environment that terminates TLS somewhere other than the Go process. A production-like boot with neither set logs a startup warning; a malformed value is fatal at boot. Details in [backend/CLAUDE.md](../backend/CLAUDE.md#client-ip-resolution-trusted-proxies).

In production-like environments **set both `OPERATOR_COOKIE_DOMAIN` and `CLIENT_COOKIE_DOMAIN` explicitly** — leaving one empty mints that tier's cookie without a `Domain` attribute (scoped to the minting host), so each tier's session is confined to its own subdomain.

**JWT audience** (post PR-D): operator login mints `aud=operator`, client login mints `aud=client`; both issuance paths now exist. Each mux's `RequireAudience` gate rejects cross-audience tokens with `401 audience_mismatch`. The dev token endpoint accepts an `audience` field (`operator`|`client`) to mint a matching token for either surface — see `scripts/devtoken.sh --audience client`.

**Smoke test**:
```bash
# Operator surface (default — works via dev fallthrough or *.localhost):
curl -i http://console.localhost:3000/health
curl -i http://localhost:3000/health   # dev fallthrough → operator
# Client surface (post-PR-D registers per-tier auth + onboarding/subscriptions/payments):
curl -i http://client.localhost:3000/health
# 421 in non-dev when Host doesn't match (run with ENV=staging):
curl -i -H 'Host: example.com' http://localhost:3000/health

# Mint per-audience dev tokens (see scripts/devtoken.sh):
./scripts/devtoken.sh administrator                  # default — aud=operator
./scripts/devtoken.sh administrator --audience client  # aud=client (api.* surface)
```

### Port Mapping Strategy

Every published host port is an `.env` variable with a compose default. `scripts/init.sh` seeds a non-colliding block per stack on first run, so multiple stacks coexist without arithmetic baked into compose. Container-internal ports stay standard.

```
Infra (docker-compose.infra.yml) — one instance per stack, bound to
${INFRA_BIND_ADDRESS:-127.0.0.1} (loopback unless docker/.env says otherwise):
${MONGO_PORT:-27017}        → mongodb:27017
${REDIS_PORT:-6379}         → redis:6379
${RUSTFS_API_PORT:-9100}    → rustfs:9000    # S3 API — on HOST_BIND_ADDRESS (browser-facing via proxy)
${RUSTFS_CONSOLE_PORT:-9101}→ rustfs:9001    # admin console

App (docker-compose.dev.yml / docker-compose.staging.yml):
${BACKEND_PORT:-3000}         → backend:3000
${FRONTEND_PORT:-8080}        → frontend-admin:5173    # operator console (Vite)
${CLIENT_FRONTEND_PORT:-8081} → client-frontend:5173   # Tier-2 client SPA (Vite)

Production (docker-compose.prod.yml) — bound to ${HOST_BIND_ADDRESS:-127.0.0.1}:
${BACKEND_PORT:-3000}  → backend:3000
${FRONTEND_PORT:-8080} → frontend-admin:80   # Nginx static
```

`HOST_BIND_ADDRESS` defaults to `0.0.0.0` in dev (the compose default) and to `127.0.0.1` in prod: a production stack publishes nothing unless `docker/.env` says where. `scripts/env-validate.sh` warns when a production `.env` leaves either bind address at `0.0.0.0`.

The observability overlay publishes its own block (Grafana, Prometheus, Loki, Tempo, OTel) — see the [observability section](#self-hosted-otel-stack-docker-composeobservabilityyml).

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

### Container hardening

What `docker-compose.prod.yml` and `docker-compose.infra.yml` actually enforce (as opposed to aspire to):

| Service | `cap_drop` | `cap_add` | `read_only` | Why |
| --- | --- | --- | --- | --- |
| backend | ALL | — | yes (+ `tmpfs: /tmp`) | Listens on 3000, no socket, writes only to mounted volumes. Runs as `user: "1000:1000"` (like staging): the image's own `nonroot` uid 65532 cannot read the 0600 JWT private key `orkestra.sh` enforces on the deploy user's `docker/keys/` |
| frontend-admin | ALL | `CHOWN SETGID SETUID NET_BIND_SERVICE` | no | nginx master is root on :80 and drops workers to `nginx`; `10-write-config.sh` rewrites `config.js` inside the docroot at every start, so the rootfs must stay writable |
| mongodb | ALL | `CHOWN DAC_OVERRIDE SETGID SETUID` | no | `replica-entrypoint.sh` writes the keyfile into the `mongodb`-owned config volume as root, then `docker-entrypoint.sh` chowns and `gosu`s |
| redis, rustfs | ALL | — | no | Run on unprivileged ports, own their data dirs |

Every service also sets `security_opt: [no-new-privileges:true]`. The backend's mutable paths are named volumes (`backend-uploads`, `backend-logs`) rather than `./backend/*` binds, which used to appear as root-owned untracked files under `docker/`. `backend/Dockerfile` pre-creates both mount points `1777` so an empty named volume inherits a directory a non-root uid can write — Docker never chowns volumes for you.

**Adding a service**: start from `cap_drop: [ALL]` + `no-new-privileges`, bring it up, and add only the capability the failing syscall names — commenting *why* next to each `cap_add`. Prefer `read_only: true` + `tmpfs` for scratch paths; if the image's entrypoint writes into its own filesystem (as nginx does here), say so in the compose comment instead of silently leaving it writable.

**Follow-up**: `read_only` for frontend-admin needs `config.js` relocated out of the docroot (a `tmpfs` dir + an `alias` in the `location = /config.js` block of `frontend-admin/nginx.conf` and the Dockerfile's baked config) — tracked, not done.

**Still aspirational**: resource limits, Trivy scanning, a non-root `USER` in the backend's production stage (the DHI base picks the uid today).

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
MONGO_ROOT_PASSWORD=<hex>     # generated by make init — compose refuses an empty value
MONGO_DATABASE=orkestra

# Redis
REDIS_PASSWORD=<hex>          # generated by make init — compose refuses an empty value

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

Phase 5.2 of the tenancy plan ships a self-hosted observability profile that layers on top of the dev/staging/prod stacks. After ADR-0005 Phase D, six containers, all pinned public images:

| Service           | Image                                         | Host port | Purpose                                                                            |
| ----------------- | --------------------------------------------- | --------- | ---------------------------------------------------------------------------------- |
| **otel-collector**| `otel/opentelemetry-collector-contrib:0.96.0` | 4318 / 4317 | OTLP receiver; fans traces to Tempo, exposes collected metrics for Prometheus     |
| **tempo**         | `grafana/tempo:2.4.1`                         | 3200        | Trace backend (local storage, 72 h retention)                                      |
| **prometheus**    | `prom/prometheus:v2.51.2`                     | 9090        | Metric scraper (15 d retention)                                                    |
| **loki**          | `grafana/loki:3.0.0`                          | 3100        | Log backend (filesystem store; 14 d info / 30 d warn+ via per-stream retention)   |
| **promtail**      | `grafana/promtail:3.0.0`                      | —           | Log shipper; tails docker stdout, JSON-parses, ships to Loki                       |
| **grafana**       | `grafana/grafana-oss:10.4.2`                  | 3010        | UI; Tempo + Prometheus + Loki datasources auto-provisioned with cross-jumping; six dashboards under `Orkestra/` pre-loaded |

Boot it alongside the dev stack. Easiest path via `orkestra.sh`:

```bash
./orkestra.sh observability up      # CLI
./orkestra.sh                       # TUI → option 3 "Observability"
```

Or directly via docker compose — one project spanning all three files, named to match what `orkestra.sh` would use (`${APP_NAME}-${ENV}` from `docker/.env`, e.g. `orkestra-development`):

```bash
cd docker
export COMPOSE_PROJECT_NAME="${APP_NAME}-${ENV}"   # match docker/.env's APP_NAME + ENV
docker compose -f docker-compose.infra.yml -f docker-compose.dev.yml --env-file .env up -d
docker compose -f docker-compose.infra.yml -f docker-compose.dev.yml -f docker-compose.observability.yml --env-file .env up -d
```

The orkestra.sh launcher also exposes `observability {down,reset,status,info,logs}`. Observability is a **per-stack overlay** layered into the same `${STACK}` project (see [Multi-Stack Model](#multi-stack-model)) — `docker-compose.observability.yml` contains a partial `backend` override and is deliberately invalid as a standalone Compose project. The launcher validates the merged infra + app + overlay graph before acting and fails closed if that graph cannot be resolved. Lifecycle ownership is explicit: `down`/`reset` target only `otel-collector`, `tempo`, `prometheus`, `loki`, `promtail`, and `grafana`; reset removes only their five named data volumes. App and infra containers/volumes are never widened into an observability reset.

The base app compose files pass `LOKI_QUERY_URL` and `GRAFANA_URL` through with empty defaults, so the logging workspace degrades cleanly without the overlay. `observability up` applies the overlay's `LOKI_QUERY_URL=http://loki:3100` and default `GRAFANA_URL=http://localhost:3010` to an already-running backend through the valid merged graph; `down` and `reset` restore the app-only backend configuration. Later full-stack deploys retain the override while observability containers are active. Set `GRAFANA_URL` explicitly when the browser reaches Grafana through another hostname, scheme, or host port; it is a browser link, whereas the Loki URL remains internal to the stack network.

Point the backend at the collector by setting in `docker/.env`:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_TRACES_ENABLED=true
```

Restart the backend (`docker compose -f docker-compose.dev.yml restart backend`) and open:

- Grafana — http://localhost:3010 (admin / admin; anonymous Viewer access is enabled so shareable links work without login)
- Prometheus — http://localhost:9090
- Tempo — http://localhost:3200 (not meant for direct use; query via Grafana's Tempo datasource)
- Loki — http://localhost:3100 (not meant for direct use; query via Grafana's Loki datasource or the Explore tab)

Six pre-provisioned dashboards ship under the `Orkestra/` folder in Grafana (`docker/grafana/provisioning/dashboards/*.json`):

| Dashboard | Purpose | Primary signal sources |
| --- | --- | --- |
| **Service Overview** | Operator landing page — request rate, error %, p95 latency, log volume by level, top routes, recent error tail | Prometheus (Phase B histogram) + Loki |
| **HTTP RED** | Per-audience RED method with `audience`/`route` template variables; latency percentiles, status-class breakdown, top slow/heavy routes | Prometheus histogram |
| **Logs Explorer** | Log volume by level + by module, application log feed (filter by `module`+`level`), HTTP request feed (filter by `audience`+`level`). Split into two feeds because `module` and `audience` labels are mutually exclusive per stream | Loki |
| **Observability Pipeline Health** | Self-check for the trifecta: collector receiver/exporter rates, drops, failures, scrape target up/down, scrape duration | Prometheus (`otelcol_*` self-telemetry + `up`) |
| **Module Health Matrix** | Per-module log volume + ERROR count, rate by module over time, ADR-0001 Cedar/capability/entitlement panels (populate when those code paths fire) | Loki + Prometheus |
| **Tenant traces + logs** | Multi-tenant correlation surface. Takes a `tenant.id`, an optional tier filter, and an optional module name. The traces panel shows every span where the `TenantBaggage` middleware stamped the matching `tenant.id` attribute; the logs panels show every Loki entry where the structured JSON line carries the matching `tenant_id` or `module` field | Tempo + Loki |

All six cross-link by `trace_id` — clicking a span jumps to filtered Loki logs, and clicking `trace_id` in any log line jumps back to Tempo. Backing evidence: `backend/internal/shared/middleware/tenant_baggage.go`, `backend/internal/shared/middleware/request_logger.go` (ADR-0005 Phase A), and `backend/internal/shared/utils/per_module_level_handler.go` (ADR-0005 Phase C).

Phase 5.3 landed `/metrics` on the backend (`GET http://backend:3000/metrics`), scraped automatically by Prometheus. The handler is mounted on both the operator mux (for in-product browsing under the audience host) AND on the `lanOpsHandler` so a scrape against the `backend` service (`backend:3000` by service-name DNS within the stack's network) works without spoofing a Host header — Prometheus has no per-scrape Host override. The hostMux `opsPaths` allowlist (`/health`, `/ready`, `/metrics`) is the single source of truth for paths that escape the audience gate; the client mux still does not mount `/metrics`, so a browser hitting `api.orkestra.com/metrics` continues to 404 through the reverse proxy. Four metric families ship today — Cedar shadow divergence, capability denial, entitlement projection lag, and (ADR-0005 Phase B) `orkestra_http_request_duration_seconds` — with the label schema frozen in [ADR-0002](../docs/adr/0002-metrics-label-schema.md). Disable the endpoint by setting `METRICS_ENABLED=false`.

Prometheus scrapes two endpoints on the OTel collector — `:8889` (the Prometheus exporter for OTLP-received customer metrics, populated only when a sender sets `OTEL_METRICS_ENABLED=true`) and `:8888` (the collector's own self-telemetry — `otelcol_receiver_*`, `otelcol_exporter_*`, `otelcol_processor_dropped_*`). The latter is what the Pipeline Health dashboard reads to surface drops, failures, and signal-throughput regressions.

The HTTP latency histogram is labelled `{audience, method, route, status_class}` (Chi route template, never raw path) and carries `trace_id` as a Prometheus exemplar. With Prometheus's `--enable-feature=exemplar-storage` and Grafana's "Prometheus → Tempo" datasource link, clicking a slow bucket jumps straight to the matching trace — no external correlation table.

[ADR-0005](../docs/adr/0005-observability-logging-tracing-metrics.md) (Phase C) adds per-module log levels. Set `LOG_LEVEL_<MODULE>=debug` for any module (e.g. `LOG_LEVEL_RAG=debug`, `LOG_LEVEL_BILLING=warn`) to override the global `LOG_LEVEL` for just that module — the registry auto-stamps `module=<name>` on every line emitted from a module's `deps.Logger`, and the slog handler gates by it. Unset overrides fall back to `LOG_LEVEL`.

[ADR-0005](../docs/adr/0005-observability-logging-tracing-metrics.md) (Phase A) replaced Chi's unstructured request logger with a structured one that emits one JSON line per request with `trace_id`, `span_id`, `tenant_id`, `tenant_kind`, `user_id`, `user_role`, `audience`, `request_id`, `method`, `path`, `status`, `duration_ms`, `bytes`, `remote`, `ua` (and `slow=true` when over threshold). Two process-scoped tunables, both safe to leave at the default:

- `LOG_HTTP_SKIP_PATHS` — comma list of exact paths to suppress (`/health,/ready,/metrics,/openapi.json` by default). When set, REPLACES the default list — include defaults explicitly to extend.
- `LOG_HTTP_SLOW_THRESHOLD_MS` — integer milliseconds; default `1000`. Slower requests get `slow=true` stamped so `{slow=true}` is a one-liner in Loki.

The request-log payload is **allowlist-only** by ADR contract — bodies, the `Authorization` header, and raw query strings never reach the log surface.

### Legacy: External monitoring integrations

### External monitoring alternatives

For production environments, consider managed services instead of (or in addition to) the self-hosted stack above:

- **DataDog**: Comprehensive APM and infrastructure monitoring
- **New Relic**: Application performance monitoring
- **Prometheus + Grafana**: Self-hosted metrics and alerting
- **Cloud Provider Solutions**: AWS CloudWatch, GCP Operations, Azure Monitor

### Basic Health Checks

Container names below use the shipped defaults (`APP_NAME=orkestra`, `ENV=development`) as a worked example — substitute your own `${APP_NAME}-<svc>-${ENV}`, read from `docker/.env`.

```bash
# Check MongoDB health
docker exec orkestra-mongodb-development mongosh --eval "db.adminCommand('ping')"

# Check Redis health
docker exec orkestra-redis-development redis-cli ping

# Check Gotenberg health (PDF service)
curl http://localhost:3030/health

# Check application health endpoints
curl http://localhost:3000/health  # Backend health
curl http://localhost:8080         # Frontend availability
```

## Backup & Restore

**Prefer `./backup.sh` / `./restore.sh` at the repo root** (documented in [scripts/CLAUDE.md](../scripts/CLAUDE.md) and [docs/site/operating/backup-and-restore.mdx](../docs/site/operating/backup-and-restore.mdx)) — they resolve the current stack's container/network names from `docker/.env` automatically. The manual snippets below are illustrative only; container names again use the `orkestra`/`development` worked example — substitute your own `${APP_NAME}-<svc>-${ENV}`.

### Manual Backup Commands

**MongoDB Backup:**

```bash
#!/bin/bash
# Create timestamped backup
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="./backups/mongo_${TIMESTAMP}"

# Run mongodump
docker exec orkestra-mongodb-development mongodump \
  --uri="mongodb://admin:changeme@localhost:27017" \
  --out=/tmp/backup \
  --gzip

# Copy to host
docker cp orkestra-mongodb-development:/tmp/backup ${BACKUP_DIR}

# Upload to S3 (optional)
# aws s3 cp ${BACKUP_DIR} s3://orkestra-backups/mongo/${TIMESTAMP}/ --recursive
```

**Redis Backup:**

```bash
#!/bin/bash
# Force Redis to save current dataset
docker exec orkestra-redis-development redis-cli --pass changeme BGSAVE
sleep 5

# Copy RDB file
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
docker cp orkestra-redis-development:/data/dump.rdb ./backups/redis_${TIMESTAMP}.rdb
```

### Restore Procedures

**MongoDB Restore:**

```bash
# Stop application services
docker compose -f docker-compose.dev.yml down    # Or prod.yml
docker compose -f docker-compose.prod.yml down

# Restore from backup
docker exec orkestra-mongodb-development mongorestore \
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
docker cp ./backups/redis_backup.rdb orkestra-redis-development:/data/dump.rdb

# Start Redis
docker compose start redis
```

## Common Operations

### Service Management

```bash
# Navigate to docker directory first
cd docker

# Start infrastructure only (its own instance per stack — see Multi-Stack Model)
docker compose -f docker-compose.infra.yml up -d

# Start development environment
docker compose -f docker-compose.dev.yml up -d

# Start production environment
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d

# View logs for specific services
docker compose -f docker-compose.dev.yml logs -f backend frontend-admin

# Restart a specific service
docker compose -f docker-compose.dev.yml restart backend

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

# Execute commands in containers (container names are stack-namespaced —
# ${APP_NAME}-<svc>-${ENV}; worked example below is orkestra/development)
docker exec -it orkestra-backend-development sh
docker exec -it orkestra-mongodb-development mongosh

# Check network connectivity (${STACK}_default, e.g.:)
docker network inspect orkestra-development_default

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

# View complete container information (stack-namespaced name — worked
# example uses the shipped defaults, orkestra/development)
docker inspect orkestra-backend-development

# Access container shell for debugging (emergency only)
docker exec -it orkestra-backend-development sh
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
- [SDK package](../backend/pkg/sdk/CLAUDE.md) - the Module contract a fork builds addons against
- [Deployment Scripts](../scripts/CLAUDE.md) - Automation and deployment orchestration
