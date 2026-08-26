# ORKESTRA

**Orkestra is the SaaS plumbing every product rebuilds — users, auth, RBAC, multi-tenancy, navigation, logging — already done.** Eight core modules (`user`, `auth`, `authz`, `tenant`, `notification`, `navigation`, `logging`, `compliance`) supply the baseline on day one. Per [ADR-0006](docs/adr/0006-collapse-to-core-only-base.md) Orkestra is a **core-only base**: it ships *no* addons. A fork that needs invoicing, payments, subscriptions, AI, marketing, etc. builds those verticals on top, against the in-tree SDK contract, using the same `Module` extension seam the core itself is built on.

## Tenancy Model

Orkestra operates on **two distinct tiers of tenants**. Understanding this distinction is load-bearing for every design decision — data isolation, RBAC scope, and module activation all depend on which tier a request is acting in.

### Tier 1 — Internal tenants (operator side)

The companies that **run Orkestra** (one or more of "our" organizations). For each internal tenant the platform manages:

- Internal users, roles, and RBAC
- Which optional modules a fork has added are enabled for that tenant
- Operational admin (module config, audit logs)

### Tier 2 — External client tenants (customer side)

**External clients register on the platform**, and each external client can itself be a multi-tenant organization (multiple sub-tenants / workspaces under one client). For each external client the platform manages:

- Client registration and onboarding
- The client's own users, roles, sub-tenants

> **ADR-0006 (D1):** the two-tier **data model** survives intact in the `tenant` module (`TenantKind = internal | external`, orgs + memberships). The **mechanism** by which Tier-2 clients *consume* Tier-1 services (catalog → subscribe → Stripe → entitlement) left with the `subscriptions`/`payments` addons. A fork that sells services to external clients rebuilds that layer.

### Implications for contributors

- Every new endpoint must declare **which tier it serves** (internal operator, external client, or both) and enforce org-scoped RBAC accordingly.
- Every new collection/table must carry a tenant scope (internal org ID *or* external client org ID) and be indexed/queried with that scope — never cross-tenant by default.
- When in doubt about which tier owns a resource, ask before implementing.

## Tech Stack

| Layer              | Technology                                                         |
| ------------------ | ------------------------------------------------------------------ |
| **Backend**        | Go 1.25.13, Huma v2 (OpenAPI-first), 8 core modules, single Go module |
| **Frontend**       | React 19, TypeScript 5.9, Vite 8 (admin) / Vite 7 (client), Redux Toolkit, TanStack Table |
| **Mobile**         | Flutter 3.44+, Dart, Riverpod                                      |
| **Database**       | MongoDB 8.0, Redis 8.2                                             |
| **Infrastructure** | Docker Compose (dev/staging/prod), GitHub Actions CI               |
| **Auth**           | Email/password (argon2id) + OAuth 2.1 (Google, Apple, GitHub, Discord), RS256 JWT, 6-role RBAC |

## Architecture

**Plugin architecture, core-only.** The 8 core modules are themselves implementations of the `Module` contract. The module system that hosts them is **kept by design**: a fork adds its own optional modules through the same clean `Module` + `catalog_<name>.go` + `iface` path the core uses. The `optionalModules` catalog ships **empty** — there is nothing to toggle out of the box, but the `/admin/modules` surface remains for forks that add their own.

**Key components** (`backend/pkg/sdk/module/`):

- **Module interface** — lifecycle contract every module implements (Init, RegisterRoutes, Start, Stop, HealthCheck)
- **ModuleRegistry** — `RegisterAll()` with topological sort from `Dependencies()`; tracks failures, gates routes for disabled modules
- **ServiceRegistry** — typed key-value store for cross-module service sharing (`GetTyped[T]`, `MustGetTyped[T]`)
- **ConfigService** — DB-backed (MongoDB) + Redis-cached (30s TTL) module configuration with AES-256-GCM encrypted secrets
- **pkg/sdk/iface** — consumer-facing interfaces (UserProvider, NotificationSender, TenantProvider, AuthzProvider, JWTProvider, AuditSink, KMSProvider, …) that prevent direct cross-module imports. The `AuditSink`/`KMSProvider` setter seams survive on the core services (nil by default) so a fork's audit/compliance module can wire them the way the removed `compliance` addon did.
- **RoleMiddleware** — interface (`module.go`) for RBAC route protection, satisfied by `AuthMiddleware`
- **Module catalog** (`cmd/server/catalog.go` + per-module `catalog_<name>.go` files) — maps module names to factory functions. A fork drops a `catalog_<name>.go` with one `init()` to register its module. See [`backend/CLAUDE.md`](backend/CLAUDE.md) for the registry mechanics.

`pkg/sdk` is an **in-tree package** of the single backend Go module — there is no separate `go.mod`, no `go.work`, no `replace`, and nothing published to the Go proxy (ADR-0006 D2 reverted the multi-repo SDK split).

**Admin API**: `GET/PATCH /v1/admin/modules`, `GET /v1/admin/modules/health`, `GET/PATCH /v1/admin/modules/{name}/environments/{env}`, `PUT /v1/admin/modules/{name}/active-environment` — runtime enable/disable (hot-reload), config updates, per-environment config profiles (sandbox/production), health checks. Frontend at `/admin/modules` (list) and `/admin/modules/:name` (detail).

### Module Loading

A fork's optional modules are **instantiated, initialized, and routed** at boot regardless of enabled state — routes for disabled modules are gated by `ModuleGate` middleware (returns 503), and only enabled modules have their `Start()` method called (background jobs, polling, etc.).

**Enabling/disabling at runtime:** The admin API (`PATCH /v1/admin/modules/{name}`) calls `StartModule()`/`StopModule()` on the registry. The module starts or stops immediately — no restart required. Dependency constraints are enforced: you cannot disable a module that another running module depends on (returns 409).

**Which modules start at boot** is determined by the `module_configs` collection in MongoDB (set via admin UI). On first boot of a brand-new install the document is seeded from each module's `ConfigSchema().EnvVar` and the module's own `EnabledByDefault`. The registry topologically sorts modules by `Dependencies()` so initialization order is always correct.

## Module Map

### Backend Modules (`backend/internal/`)

**Core (always loaded):**

Every core module has **two** docs: the in-repo `CLAUDE.md` is the AI-facing *contract* (invariants, wiring, rules — read this before editing); the `docs/site` page is the human-facing *reference* published to [docs.orkestra.cc](https://docs.orkestra.cc). Keep both current when you change a module.

| Module           | Purpose                                                                                   | Contract | Page |
| ---------------- | ------------------------------------------------------------------------------------------ | -------- | ---- |
| **user**         | Per-tier user collections, profiles, the global system role, OAuth links, avatar pipeline | [CLAUDE.md](backend/internal/core/user/CLAUDE.md) | [user](docs/site/modules/core/user.mdx) |
| **notification** | Email delivery, templates, preferences, unsubscribe tokens — boots in `noop`              | [CLAUDE.md](backend/internal/core/notification/CLAUDE.md) | [notification](docs/site/modules/core/notification.mdx) |
| **tenant**       | Orgs + memberships (two-tier tenancy), divisions, provisioning policy, entitlements        | [CLAUDE.md](backend/internal/core/tenant/CLAUDE.md) | [tenant](docs/site/modules/core/tenant.mdx) |
| **authz**        | Permission catalog, 6 platform + 5 tenant roles, bindings, Cedar policy engine             | [CLAUDE.md](backend/internal/core/authz/CLAUDE.md) | [authz](docs/site/modules/core/authz.mdx) |
| **auth**         | Email/password (argon2id) + OAuth 2.1, MFA + passkeys, JWT, sessions, service accounts     | [CLAUDE.md](backend/internal/core/auth/CLAUDE.md) | [auth](docs/site/modules/core/auth.mdx) |
| **navigation**   | Dynamic menu from module NavItems + persisted reorder via `/admin/modules/navigation`      | [CLAUDE.md](backend/internal/core/navigation/CLAUDE.md) | [navigation](docs/site/modules/core/navigation.mdx) |
| **logging**      | Tier-1 runtime logging workspace: permanent levels, expiring diagnostics, bounded preview  | [CLAUDE.md](backend/internal/core/logging/CLAUDE.md) | [logging](docs/site/modules/core/logging.mdx) |
| **compliance**   | Audit trail + GDPR DSR, per-tenant KMS crypto-shred, legal hold, retention, SOC2 (ADR-0009) | [CLAUDE.md](backend/internal/core/compliance/CLAUDE.md) | [compliance](docs/site/modules/core/compliance.mdx) |

Load order (topologically sorted by `Dependencies()`): `user` → `notification` → `tenant` → `authz` → `auth` → `navigation` → `logging` → `compliance`. Auth depends on notification (optional at runtime) so it can deliver verification and password-reset emails; `logging` has no declared dependencies; `compliance` (ADR-0009, always-on) depends on `user`/`auth`/`tenant` so it resolves the PII-producer registry + audit sink after they init.

**Optional (added by a fork; the base ships none):** `internal/addons/` does not exist in the base. A fork that adds a vertical creates `internal/addons/<name>/` implementing the `Module` interface and a `cmd/server/catalog_<name>.go` to register it — see [`backend/CLAUDE.md`](backend/CLAUDE.md) and the docs-site [addon-authoring guide](docs/site/sdk/build-your-first-addon.mdx). The archived `orkestra-cc/orkestra-addon-<name>` repos preserve snapshots of most verticals removed by ADR-0006 (billing/SDI, documents, company, graph, aimodels, rag, sales, subscriptions, payments, compliance, identity, dev) for forks to crib from. Two verticals — `agents` and `marketing` — were never split out into standalone repos, so their last in-tree state lives in this repo's own history, in the commits before the ADR-0006 removal.

### Other Modules

- **[`/backend/pkg/sdk/`](backend/pkg/sdk/CLAUDE.md)** — The SDK contract package every module depends on (in-tree, part of the single backend Go module). See also [docs/onboarding/orkestra-sdk.md](docs/onboarding/orkestra-sdk.md) for the new-developer walkthrough.
- **[`/frontend-admin/`](frontend-admin/CLAUDE.md)** — React 19 operator console / Tier-1 admin dashboard (port 8080, host `console.localhost`)
- **[`/frontend-client/`](frontend-client/CLAUDE.md)** — React 19 Tier-2 client demo SPA — a thin login + account + billing-identity skeleton (the subscribe/transactions/payment flows left with the addons)
- **[`/mobile/`](mobile/CLAUDE.md)** — Flutter cross-platform app
- **[`/docker/`](docker/CLAUDE.md)** — Docker Compose configs (dev/staging/prod/infra)
- **[`/docs/site/architecture/authentication-flow.mdx`](docs/site/architecture/authentication-flow.mdx)** — Email/password + OAuth 2.1 + MFA + service-account (ADR-0014) + RBAC details. **This is the canonical copy.** `docs/Authentication_flow.md` is a pre-migration duplicate that has since drifted — it predates the `service` audience entirely.
- **[`/docs/site/`](docs/site/README.md)** — Canonical source for [docs.orkestra.cc](https://docs.orkestra.cc) hand-written pages. The Docusaurus repo ([orkestra-cc/orkestra-docs](https://github.com/orkestra-cc/orkestra-docs)) mirrors this tree on every build via `npm run sync:site` — edits live here, not there. Covers all eight core modules, the SDK contract ([`Module`](docs/site/sdk/module-interface.mdx), [ServiceRegistry](docs/site/sdk/service-registry.mdx), [ConfigService](docs/site/sdk/config-service.mdx), [iface](docs/site/sdk/shared-iface.mdx), [object storage](docs/site/sdk/object-storage.mdx)), operating guides, and the public ADRs.
  **Nothing in this repo's CI builds the site** — render locally before merging a `docs/site/**` change; the recipe is in [`docs/site/README.md`](docs/site/README.md). Only a push to `main` publishes: the sync pulls `orkestra@main`, never `dev`.

## Quick Start

```bash
# From project root — interactive TUI (pick "Full stack")
./orkestra.sh

# Or manually — infra (MongoDB + Redis + RustFS) then the dev app stack
# (public Alpine images, AIR + Vite hot reload):
cd docker
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml --env-file .env up -d

# Backend API: http://localhost:3000
# API Docs:    http://localhost:3000/docs

# Generate an administrator token for first login (run from project root):
ORKESTRA_API_URL=http://localhost:3000 ./scripts/devtoken.sh administrator
```

> **`localhost` assumes `HOST_BIND_ADDRESS=0.0.0.0`** — its default, and what
> `docker/.env.example` ships. The compose files publish the browser-facing
> ports on that address, so a host that pins it to a single interface (e.g. a
> VM serving the stack over a Tailscale IP) is reachable *only* there — and the
> failure is easy to misread: `curl` gets a connection refusal, and although
> `devtoken.sh` exits non-zero, the `T=$(…)` idiom discards that, leaving an
> empty token and a puzzling 401. Substitute that address for `localhost`, or
> derive it with `sed -n 's/^HOST_BIND_ADDRESS=//p' docker/.env`.

One infra base + one app file per environment (`docker-compose.{dev,staging,prod}.yml`), plus an opt-in `docker-compose.observability.yml` overlay (ADR-0005). The dev backend builds `docker/Dockerfile.dev-backend` (golang:alpine, AIR pre-baked; override the base via the `GO_BASE` build-arg for a Chainguard image). The notification module boots in `noop` mode by default — verification and password-reset emails are logged to the backend stdout rather than delivered. To send real mail, configure SMTP at `/admin/modules` after first login.

## Assistant Rules

### Do

- **Read the module's CLAUDE.md** before modifying any module — each has specific patterns and constraints
- **Use the module system** when adding new functionality: implement the `Module` interface, add a `cmd/server/catalog_<name>.go` file that registers the factory in `optionalModules` via `init()`, declare collections/nav/config via the module methods
- **Use `pkg/sdk/iface`** for cross-module dependencies — never import another module's `services/` or `repository/` package from a `module.go` wiring file
- **Validate and sanitize** all user inputs; implement RBAC on every endpoint (ask for required permissions)
- **Follow the auth patterns** in [authentication-flow.mdx](docs/site/architecture/authentication-flow.mdx) for any auth-related changes — not the drifted `docs/Authentication_flow.md`
- **Invoke the `orkestra-frontend-admin` skill before any `frontend-admin/` UI work** — including full-stack features whose UI lands in the operator console. Invoke it while planning the feature, not when you reach the JSX; it enforces the reference-first workflow (`src/reference/*.tsx` + production-page precedent)
- **Use `integrated-browser-mcp` for browser automation** (`browser_navigate`, `browser_eval`, `browser_snapshot`, `browser_screenshot`, etc.); it controls the browser embedded in the matching VS Code workspace

### Do Not

- **Never start servers manually** — backend and frontend run in Docker with hot reload (AIR + Vite)
- **Never expose secrets** in logs, API responses, or Git — module secrets use AES-256-GCM encryption via ConfigService
- **Never import cross-module** service/repository packages in `module.go` — use `pkg/sdk/iface` interfaces + `ServiceRegistry` typed getters instead
- **Never bypass RBAC** — all admin endpoints require `administrator` role; all protected endpoints require auth middleware
- **Never re-introduce a satellite `go.mod` / `replace` / `go.work`** (ADR-0006 forbidden pattern). The base is a single Go module; a fork's addons live in-tree.

### WSL2 Development Caveat

AIR file watcher does not reliably detect changes on the Windows filesystem mounted in WSL2. If backend changes don't take effect after saving, manually rebuild inside the container (container names are now stack-namespaced — `${APP_NAME}-<svc>-${ENV}`; the example below uses the shipped defaults, **not necessarily your stack** — read yours from `docker/.env`, see [docker/CLAUDE.md](docker/CLAUDE.md#multi-stack-model)):

```bash
docker exec orkestra-backend-development go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-backend-development
```

### CI/CD

GitHub Actions workflows (`.github/workflows/`) run on PR and push to `dev`/`main` (except where noted below). Non-gating jobs — Docker image publish, coverage-badge refresh, the weekly security cron — additionally require the repo-level Actions variable **`CI_FULL=true`** (set on the public upstream and commons); a product fork defaults to minimal CI and opts in per repo, no file edits. **CI workflows invoke `make` targets from the repo root — local and CI cannot drift.** Run `make ci-help` for the full list.

- `backend.yml` → `make ci-backend` (lint, tenantscope, policycoverage, piiscan, vuln, tests, build, openapi-check) + a single Docker image build on push
- `frontend-admin.yml` → `make ci-frontend-admin` (typecheck, eslint, tests, audit, build)
- `frontend-client.yml` → `make ci-frontend-client` (typecheck, eslint, build) — no tests yet
- `mobile.yml` → `make ci-mobile` (flutter analyze, test)
- `security.yml` — govulncheck + npm audit; runs on PR (jobs gated per changed area) + weekly cron, **no push trigger** — a push would re-scan the dependency set the PR just scanned
- `ghcr-cleanup.yml` — weekly deletion of untagged GHCR image versions (private-repo package storage is billed); manifest-aware, safe with buildx provenance
- `docs-dispatch.yml` — on push to `main` touching `docs/site/**`, `docs/adr/**`, or `backend/openapi/enterprise.json`, sends the `repository_dispatch` that rebuilds docs.orkestra.cc (fires in the upstream repo only; **fails the run** if the `DOCS_DISPATCH_TOKEN` secret is missing — a publish path that can't publish must not report success)

Local reproduction is the same one-liner CI uses:

```bash
make ci          # only the surfaces you changed (default base: origin/dev)
make ci-all      # everything — what CI does on dev/main
```

Toolchain versions live in `.mise.toml` and CI installs them via `jdx/mise-action`, so a contributor running `mise install` gets exactly the Go/Node/Flutter/golangci-lint versions CI uses. See [CONTRIBUTING.md](CONTRIBUTING.md) for the contributor flow.
