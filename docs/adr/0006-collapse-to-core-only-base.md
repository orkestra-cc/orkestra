---
title: ADR-0006 — Collapse Orkestra to a core-only base; addons become per-fork responsibility
status: proposed
public: true
---

# ADR-0006 — Collapse Orkestra to a core-only base; addons become per-fork responsibility

| Field | Value |
|---|---|
| **Status** | 🟡 Proposed |
| **Date** | 2026-06-01 (proposed) |
| **Authors** | @salvatore.balestrino |
| **Supersedes** | Reverses the multi-repo extraction track (SDK split "Phase 5", addon repo extraction). Partially supersedes [ADR-0003](0003-three-audience-host-split.md) (the `client` audience is emptied of subscriptions/payments/onboarding). |
| **Related** | [ADR-0001](0001-unified-tenant-model.md) (two-tier *data* model survives, two-tier *consumption* mechanism removed), [ADR-0003](0003-three-audience-host-split.md), [ADR-0004](0004-external-services-integration.md) |

## Context

Orkestra grew a four-layer modularity stack:

1. **The module system** — the `Module` interface, `ModuleRegistry` (topological init from `Dependencies()`), `ServiceRegistry`, `ConfigService`, and `shared/iface` consumer interfaces. The 7 core modules are themselves implementations of this contract.
2. **The addon payload** — 14 optional modules (`billing`, `documents`, `company`, `graph`, `aimodels`, `rag`, `agents`, `sales`, `subscriptions`, `payments`, `compliance`, `identity`, `marketing`, `dev`) compiled into every binary, toggled at `/admin/modules`.
3. **The multi-repo extraction** — `pkg/sdk` published as a standalone Go module (`github.com/orkestra-cc/orkestra-sdk` v0.4.0), each addon extracted to its own public repo (`github.com/orkestra-cc/orkestra-addon-*`), wired back together in the monorepo via 15 `replace` directives, a `go.work` with 17 modules, and 13 per-addon `go.mod` files.
4. **The AI sidecar** — `cmd/ai-service/` plus `shared/remote/*` HTTP clients, an optional second binary running the `graph`/`aimodels`/`rag`/`agents` chain out-of-process.

A codebase audit (2026-06-01) quantified the footprint:

| Dimension | Core (stays) | Addon (goes) |
|---|---|---|
| Backend Go | 59,056 LOC | **75,453 LOC (56%)** |
| frontend-admin | 19,199 LOC pages + 2,329 LOC component library | **32,362 LOC (63%)** |
| OpenAPI paths | 156 | **174 (53%)** |
| Docs tree | 9 files | **29 files (76%)** |
| Go modules | 1 (target) | 16 (`backend` + `sdk` + `openapiauth` + 13 addons) |

The audit also established the structural fact that makes this decision cheap to execute: **core → addon import coupling is zero.** No core module imports anything under `internal/addons/`. The dependency runs one way only — addons reach core through `iface` + `ServiceRegistry`. The single exception is `compliance`, which is *injected into* core via post-init setters (`AuditSink` on auth/tenant/subscriptions, `KMSProvider` on tenant).

The problem is not the code — it is the **coordination cost of layers 2–4**. Concretely:

1. **Multi-repo drift.** The 15 `replace` directives mask version skew during in-tree development; an external consumer importing `orkestra-addon-aimodels` gets whatever the Go proxy last published, which may differ from in-tree source. The "verify external examples" discipline exists solely to fight this.
2. **Tooling blind spots.** The `policycoverage` analyzer cannot walk extracted addon `go.mod` paths, forcing manual baseline entries on every new cross-module permission.
3. **Release friction.** A public-mirror CI was specified to keep the 14 repos in sync and has been deferred for months because it is genuinely hard.
4. **Cognitive load per change.** A contributor touching a cross-module contract reasons about `go.work`, `replace`, per-addon `go.mod` versions, and which repo is authoritative — for a project with effectively one author.

The v0.2.0 release already collapsed the build-time SKU matrix (5 build tags → single binary), signalling that build-time modularity was over-engineered. This ADR extends that judgment to the addon payload and the multi-repo machinery.

We remain pre-GA with permission to make breaking changes.

## Decision

**Strip Orkestra down to a core-only base. The 7 core modules, the module system that hosts them, and a rich frontend component library are the product. Every addon is deleted from the monorepo; a fork that needs invoicing, AI, marketing, or subscriptions builds it against the in-tree SDK contract.**

Three load-bearing sub-decisions were settled explicitly (2026-06-01):

### D1 — The base is mono-tier; the two-tier *consumption* mechanism leaves with the addons

`subscriptions` and `payments` are **not** promoted to core. They are archived with the other addons. The two-tier **data model** from [ADR-0001](0001-unified-tenant-model.md) survives intact in the `tenant` module (`TenantKind = internal | external`, orgs + memberships), but the **mechanism** by which Tier-2 clients consume Tier-1 services (catalog → subscribe → Stripe → entitlement) is removed. Orkestra's identity shifts from *"SaaS skeleton with built-in client billing"* to *"SaaS foundation a fork extends"*.

### D2 — The SDK collapses to a local in-monorepo package

`pkg/sdk` stays — it is the contract a fork builds addons against — but loses its separate `go.mod` and is no longer published. `orkestra-sdk` (the public repo) is archived. All imports move from `github.com/orkestra-cc/orkestra-sdk/*` to in-tree paths under the single backend module. No `replace`, no `go.work`, no version skew.

**The module system itself is kept — by necessity and by design.** The 7 core modules *are* `Module` implementations; removing the interface would mean rewriting core boot for zero benefit. Keeping it also means a fork adds its addons through the same clean `Module` + `catalog_<name>.go` + `iface` path the core uses. The `optionalModules` catalog map ships empty.

### D3 — The 14 extracted addon repos (and `orkestra-sdk`) are archived

They are not maintained as an official catalog. Their git history is preserved (`gh repo archive`), but a fork starts from the core base and writes its own verticals — optionally cribbing from the archived snapshots. We accept that the Phase-5 extraction work is, in net, reverted.

### D4 — Compose collapses to one app file per environment

`ORKESTRA_PROFILE` existed only to seed first-boot addon enablement; with an empty catalog it seeds nothing. The runtime topology becomes:

- **`docker-compose.infra.yml`** — shared base (mongodb / redis / rustfs), addon infra stripped (no Gotenberg / Memgraph / Hindsight).
- **`docker-compose.{dev,staging,prod}.yml`** — exactly **one app file per environment**, layered on the infra base.
- **`docker-compose.observability.yml`** — opt-in overlay (ADR-0005), untouched.

Deleted: `docker-compose.{minimal,full,dev-public}.yml` (and `ai-sidecar.yml`, which goes with the sidecar in Phase 2). The dev file standardizes on **public Alpine images** (Chainguard via build-arg), retiring `DEV_COMPOSE_VARIANT`. The `ORKESTRA_PROFILE` env, its backend seeder branch, and the `orkestra.sh` profile menu are all removed. This finishes the simplification v0.2.0 started (5 SKUs → 2 profiles → **none**: one app file per env).

### What stays

- **Backend:** 7 core modules (`user`, `notification`, `tenant`, `authz`, `auth`, `navigation`, `logging`), the module system, `pkg/sdk` as a local package, a single `go.mod`.
- **frontend-admin:** core pages (users, auth, authz, tenants, navigation, observability, module-admin shell) **and the shared component library** (`src/components/common/`, ~2,329 LOC used 1,000+ times) — the asset we intend to grow.
- **frontend-client:** kept as a **minimal Tier-2 demo** — login + account + `billing-identity` (already core, per the ADR-0003 Phase-6 move to the personal-tenant aggregate). The subscribe / transactions / payment-methods flows are stripped.
- **mobile:** unchanged — already core-only.

### What goes

All 14 addon directories + 14 `catalog_<addon>.go` files + `cmd/ai-service/` + `Dockerfile.ai-service` + `.github/workflows/ai-service.yml` + `shared/remote/*` + the multi-repo machinery (`go.work`, 15 `replace`, 14 satellite `go.mod`, the `user/models` re-export shim).

## Consequences

### What we enable

- **One Go module, one repo, one source of truth.** End of `replace`/`go.work`/version-skew/public-mirror-CI. `policycoverage` and every other analyzer see the whole tree again.
- **A base that is trivial to fork.** One repo to clone; the `Module` contract documents how to add a vertical. Far easier than vendoring a constellation of versioned addon repos.
- **~64k backend + ~31k frontend LOC removed**, OpenAPI spec roughly halved (330 → 156 paths), CI shed of 13 addon make-target chains and the sidecar build.
- **Sharper product story.** "Orkestra = users, auth, RBAC, multi-tenancy, navigation, logging — the SaaS plumbing, done — fork it and build your product on top."
- **One app file per environment.** No more `minimal`/`full`/`dev-public` variants or `ORKESTRA_PROFILE` (D4): `infra.yml` base + `dev`/`staging`/`prod` app files (+ opt-in `observability.yml`), no profile menu in `orkestra.sh` — one deploy path per environment.

### What we give up

- **Built-in Tier-2 monetization.** No subscriptions/payments out of the box; a fork that sells services to external clients rebuilds that layer.
- **The addon catalog as a product.** `/admin/modules` ships with nothing to toggle (the surface is retained for forks that add their own optional modules).
- **The extraction investment.** Phase-5 multi-repo work is reverted to archived snapshots.
- **The completed addons as living code.** billing/SDI, RAG, agents, marketing, etc. stop receiving maintenance in the monorepo.

### Forbidden patterns (post-cutover)

- **Re-introducing a satellite `go.mod` / `replace` / `go.work` for "just one addon."** The whole point is a single module. A fork's addons live in its own copy of the monorepo, in-tree.
- **Core importing from `internal/addons/`.** The directory should not exist in the base; if a fork re-adds it, the one-way dependency rule from this ADR still holds.

## Implementation

Single branch `feat/core-only`, each phase gated by CI green before the next. Reversible until merge.

| Phase | Scope | Gate |
|---|---|---|
| **1** | Dismantle multi-repo: fold `pkg/sdk` + `openapiauth` into the backend module, delete `go.work`, 14 satellite `go.mod`, 15 `replace`, and the `internal/core/user/models` re-export shim. Rewrite imports to in-tree paths. Addons still present. | `go build ./...` |
| **2** | Delete 14 addon dirs + 14 `catalog_*.go` + `cmd/ai-service` + `shared/remote` + `Dockerfile.ai-service` + `docker-compose.ai-sidecar.yml` + `ai-service.yml`. `catalog.go` `optionalModules` → empty. | `go build ./...` |
| **3** | Remove `compliance` back-references in `cmd/server/main.go` (the `AuditSink`/`KMSProvider` setters on auth/tenant) and confirm those consumers are nil-safe. **This is the only non-mechanical edit.** | `make ci-backend` |
| **4** | frontend-admin: delete addon pages/manifests/API-slices, clean `baseApi` `tagTypes`, prune addon i18n namespaces. | `npm run typecheck` |
| **5** | frontend-client: strip subscribe/transactions/payment-methods; keep login + account + billing-identity. | `make ci-frontend-client` |
| **6a** | **Docker & profile collapse (D4):** delete `docker-compose.{minimal,full,dev-public}.yml`; fold dev onto public Alpine in `docker-compose.dev.yml` (retire `DEV_COMPOSE_VARIANT`); drop `ORKESTRA_PROFILE` from compose + the backend seeder branch; strip addon infra from `docker-compose.infra.yml` (Gotenberg, Memgraph/Hindsight `manual-only`) and addon env from dev/staging/prod (`GOTENBERG_URL`, `GRAPH_*`, `HINDSIGHT_*`, `CONTAINER_CONTROL_ENABLED`). End state: `infra.yml` base + `dev`/`staging`/`prod` + opt-in `observability.yml`. | compose up clean |
| **6b** | **Scripts:** strip the profile machinery from `orkestra.sh` (`SKU_PROFILES`, `PROFILE_*` arrays, `apply_profile_source_mode`, source-mode "Option C", profile menu); simplify `scripts/init.sh` profile language; reduce `scripts/tenantscope-baseline-audit.sh` baseline to core; refresh `scripts/CLAUDE.md`. | `./orkestra.sh` smoke |
| **6c** | **Docs & CI:** Makefile (−13 addon targets + sidecar build), regenerate `openapi/enterprise.json` (330→156 paths), delete 14 addon `CLAUDE.md` + `docs/plans/marketing-addon/` + addon refs across the tree, refresh root + module CLAUDE.md. **Rewrite (not delete) the addon-authoring docs-site pages** — `docs/site/sdk/build-your-first-addon.mdx`, `docs/site/contributing/{forking-a-core-addon,adding-an-addon}.mdx`, `docs/site/modules/addons/` — to reflect "add a `Module` in your fork" instead of "publish an addon repo". | `make ci-all` |
| **7** | Archive the 14 addon repos + `orkestra-sdk` (`gh repo archive`); update README/ROADMAP/CHANGELOG; tag **`v0.3.0`** (breaking). | — |

### Open follow-ups (not blocking)

- **`/admin/modules` with an empty catalog** — keep the surface for forks, or hide it behind a "no optional modules installed" empty state. Decide during Phase 4.
- **Docs-site mirror** — ADRs and module docs propagate to docs.orkestra.cc via `npm run sync:site`; confirm the addon-page pruning lands there too.

## Alternatives considered

1. **Keep the status quo (modular + addons + multi-repo SDK).** Rejected — the coordination cost (drift, blind analyzers, deferred mirror CI) is the very thing this ADR removes, and the contributor base (one author) cannot justify it.

2. **Keep `subscriptions` + `payments` in core (retain two-tier consumption).** Considered and initially chosen, then reversed by decision D1: the author does not want Stripe/billing complexity in the base. A fork that needs it rebuilds it.

3. **Keep `orkestra-sdk` as a published, versioned repo** so forks import a stable contract via the Go proxy. Rejected (D2) — reintroduces the multi-repo coordination this ADR exists to kill. An in-tree package gives the same contract with none of the skew.

4. **Rip out the `Module` interface / registry entirely** and wire core modules with plain Go. Rejected — core *is* built on the registry; removing it is a large, risky rewrite of boot for no benefit, and it would deprive forks of the clean extension seam.

5. **Delete `frontend-client` outright.** Rejected (D3-adjacent) — kept as a thin auth/profile demo so a fork inherits a Tier-2 SPA skeleton; only the addon-coupled flows are stripped.

6. **Move addons to an `examples/` dir or a `git submodule` catalog instead of archiving.** Rejected — either choice keeps a maintenance obligation. Archived public repos preserve history with zero ongoing cost; a fork copies what it wants.
