---
title: ADR-0004 — External services integration framework
status: proposed
public: true
---

# ADR-0004 — External services integration framework

| Field | Value |
|---|---|
| **Status** | Proposed |
| **Date** | 2026-05-14 |
| **Authors** | @salvatore.balestrino |
| **Supersedes** | — |
| **Related** | [ADR-0001](0001-unified-tenant-model.md) (Tenant aggregate), [ADR-0003](0003-three-audience-host-split.md) (Host split) |

## Context

Orkestra is intended as the **control plane that a company runs to manage its own self-hosted fleet** — internal users, RBAC, billing, *and* the set of supporting services (transcription, OCR, document parsing, workflow automation, object storage, web crawling, ...) that live on the operator's infrastructure under company control. Every new self-hosted service that joins the fleet should slot into the same provisioning / auth / metering / lifecycle plumbing as the previous one. Today there is no formal pattern.

Three forms of integration with code outside the Orkestra monolith already coexist or are imminent, and they need to be told apart:

1. **Addon (in-process)** — Go module compiled into the monolith binary. Cross-module access via `iface` interfaces + `ServiceRegistry`. Examples: `billing`, `documents`, `subscriptions`, `payments`.
2. **Sidecar (out-of-process, same `go.mod`)** — separate binary cut from the same backend module. The AI service split (`cmd/ai-service/`, monolith communicates via `iface.AIModelProvider` over HTTP). Used for runtime separation, not boundary separation.
3. **External service (out-of-process, self-hosted under operator control, independent deployment lifecycle)** — a separate product running on its own host or VM on the operator's internal network, with its own release cycle, that Orkestra acts as the control plane for. May be stateful (holds customer data) or a stateless processor — both shapes belong here.

Category 3 has no formal pattern today. Near-term cases drive the work:

- **octo-stt** — Python/CUDA speech-to-text service on `ai-1`, multi-tenant by design (`workspace_id` on every collection). To be consumed by both Tier-1 (operator's own use) and Tier-2 (sold as `octo-stt — 50h` subscriptions via `subscriptions`/`payments`). Its `AuthProvider` abstraction is already built to delegate to Orkestra (`AUTH_BACKEND=orkestra`).
- **n8n self-hosted** — Node.js workflow automation. **NOT** multi-tenant in Community Edition. Intended for Tier-1 operator use only (each internal tenant gets its own workflows). Per-tenant instance topology is the only sane fit.

Additional candidates the framework must accommodate as the fleet grows:

- **docling self-hosted** — IBM-led Python document parser running on its own VM. Stateless converter (upload → Markdown/JSON); consumed primarily by `rag` ingestion, possibly sold as a `docling — 1M-pages` capability.
- **crawl4ai self-hosted** — Python LLM-friendly web crawler on its own VM. Stateless per request; consumed by `agents` and `sales`, possibly sold as a web-research capability.
- **rustfs self-hosted** — Rust S3-compatible object store on its own host. Stateful (objects = customer data); usable as substrate for `documents`/`rag` uploads and (later) as a sellable storage product. See §Out of scope on gauge-based metering.

Without a uniform pattern each integration would reinvent provisioning, key issuance, usage tracking, GDPR cascade, and outbound webhook delivery — leaving inconsistent security boundaries and ~80% duplicated plumbing.

## Decision

Establish **external service** as a formal class of integration distinct from addon and sidecar, with a dedicated broker module (`external_services`) and a generic framework that classifies products along four orthogonal axes.

### 1. Boundary definition

A component is an *external service* if it satisfies all of:

- Runs in a separate process with a runtime/toolchain Orkestra cannot embed (non-Go; GPU-bound; upstream OSS we don't control; etc.).
- Has independent deployment, versioning, and release cycle from Orkestra.
- **Is part of the operator's self-hosted fleet** — the operator deploys, monitors, upgrades, and decommissions it on their own infrastructure (separate VM, container, or host on the internal network). Orkestra is the control plane for its lifecycle.
- Communicates with Orkestra over HTTP under a versioned contract; never via direct DB or in-process call.

Whether the service **holds** customer-relevant data (transcripts, workflows, objects) versus **passes it through** statelessly (document parsing, web crawling) is captured by the `DataResidency` axis in §3, not by the boundary test. Both shapes are first-class members of the fleet and use the same broker.

Counter-examples (deliberately excluded):

- **AI sidecar** is not an external service — same `go.mod`, same team, deployment split for resource isolation only.
- **SaaS vendor APIs (Stripe, Mailgun, AssemblyAI cloud, Pexels/Unsplash stock-media)** are managed by the vendor, not the operator. They are consumed via their public APIs from feature addons (`payments`, `notification`, a future `media`, etc.) and are out of scope for this framework. See §11 for a worked brainstorm using Pexels as exemplar — recorded because the broker's pattern keeps tempting you to absorb anything HTTP-shaped.
- **Hindsight** is consumed via `iface.AIModelProvider` and is part of the AI sidecar addon chain.

### 2. Broker module: `external_services`

A new Go addon at `backend/internal/addons/external_services/`. Eventual SDK split (Phase 5+) when stable. Responsibilities:

- **Catalog** — registry of integrated products (`external_services_products`) with base URL (or URL template for per-tenant), shared secret, capabilities, classification on the three axes.
- **Workspace materialization** — per-tenant binding (`external_services_workspaces`) of `(tenantUUID, productCode) → remote workspace ref`. Unique on the pair.
- **API key issuance** — master copy in Orkestra (`external_services_apikeys`); data plane caches with short TTL.
- **Usage aggregation** — ingest `UsageEvent` rows (push or pull), enforce balance against subscription tier.
- **System webhooks** — outbound HMAC-signed lifecycle events to push-mode data planes.
- **Driver dispatch** — each product has a `ProductDriver` implementation that adapts the standard broker contract to that product's native API.

The broker is intentionally **stateless about product behavior**: every product-specific decision lives in its driver.

### 3. Four classification axes

```go
type Product struct {
    Code             string    // "octo-stt", "n8n", "docling"
    Audience         Audience  // operator-only | sellable
    MeteringProtocol Protocol  // push | pull
    Topology         Topology  // shared | per-tenant | byo
    DataResidency    Residency // resident | passthrough
    BaseURL          string    // or URL template for per-tenant ("{slug}.n8n.tuodominio.it")
    SharedSecret     string    // HMAC, encrypted at rest
    CapabilityID     string    // empty for operator-only
    UnitTypes        []string  // ["seconds", "tokens_input", "tokens_output", "pages"]
    DataRegion       string    // "eu"
}

type Audience  string  // "operator-only" | "sellable"
type Protocol  string  // "push" | "pull"
type Topology  string  // "shared" | "per-tenant" | "byo"
type Residency string  // "resident" | "passthrough"
```

**Axis 1 — Audience**: determines which Orkestra modules wire into the lifecycle.

- `operator-only` — Tier-1 admin provisions per internal tenant. No subscription/payment wiring, no Tier-2 self-service routes, no capability emission. Usage tracking optional (internal chargeback / visibility).
- `sellable` — Tier-2 buys via subscription tier; tier's `Capabilities[]` includes the product's `CapabilityID`; `EntitlementSyncer` triggers provisioning on activation, deprovisioning on cancel/suspend.

**Axis 2 — Metering protocol**: determines who initiates usage reporting.

- `push` — data plane calls `POST /internal/v1/external-services/usage` with HMAC signature and idempotency key. Used when we control the data plane.
- `pull` — broker polls the product's native API on a schedule, computes diffs, emits `UsageEvent`. Used when we cannot modify the data plane.

**Axis 3 — Topology**: determines deployment shape.

- `shared` — one multi-tenant instance, broker materializes a workspace per `(tenantUUID, productCode)`. The product must enforce tenant isolation internally.
- `per-tenant` — broker provisions one dedicated instance per tenant. The product is single-tenant or lacks native isolation we trust.
- `byo` — the tenant runs their own instance, registers URL + credentials as a `Connection`. **Out of scope for v1**; reserved for future commercial tier.

**Axis 4 — Data residency**: determines whether the GDPR cascade (§10) applies.

- `resident` — the product stores customer-relevant data (transcripts, workflows, objects, files) that outlives any individual request. Tenant deletion **must** cascade to the product per §10.
- `passthrough` — the product is a stateless processor (document parser, web crawler, format converter). Each call is self-contained; there is no remote customer data to delete on `tenant.purged`. The workspace lifecycle still applies (key revocation, catalog unbinding), but Phase 1/2 of §10 are short-circuited — see §10 for the exemption rules. Passthrough services are still part of the fleet and still go through the broker for provisioning, monitoring, key issuance, usage metering, and SSO.

### 4. Reference classifications

| Product | Audience | Protocol | Topology | DataResidency |
|---|---|---|---|---|
| `octo-stt` | sellable | push | shared | resident |
| `n8n` self-hosted | operator-only | pull | per-tenant | resident |
| `docling` self-hosted | operator-only / sellable | push | shared | passthrough |
| `crawl4ai` self-hosted | operator-only / sellable | push | shared | passthrough |
| `rustfs` self-hosted | operator-only (substrate) / sellable (storage product) | pull (gauge — see Out of scope) | shared (bucket-per-tenant) | resident |
| `octo-ocr` (future) | sellable | push | shared | resident |

Cloud-only services (e.g. AssemblyAI as a burst fallback) are deliberately **not** in this table — they are managed by the vendor, not the operator, and fail the boundary test in §1. If used, they are consumed from a feature addon as a vendor API, not registered in the broker catalog.

### 5. `ProductDriver` contract

```go
type ProductDriver interface {
    // Lifecycle. Idempotent on (tenantUUID, productCode). For per-tenant
    // topology, Provision calls ProvisionInstance first; for shared, it
    // skips it. ALWAYS invoked from a background worker, never from an
    // HTTP request handler — see "Asynchronous lifecycle" below.
    Provision(ctx context.Context, tenantUUID string, tier *PricingTier) (WorkspaceRef, error)
    Deprovision(ctx context.Context, ref WorkspaceRef) error

    // Soft lifecycle for tenant.archived (data retained, access blocked)
    // and tenant.reactivated. Distinct from Deprovision which is a hard
    // delete. See §10 "Data and GDPR" for the grace-period semantics.
    SuspendWorkspace(ctx context.Context, ref WorkspaceRef) error
    ResumeWorkspace(ctx context.Context, ref WorkspaceRef) error

    // Key material. The returned plaintext is shown to the user ONCE;
    // the broker persists only the hash + prefix.
    IssueAPIKey(ctx context.Context, ref WorkspaceRef, name string, scopes []string) (KeyMaterial, error)
    RevokeAPIKey(ctx context.Context, ref WorkspaceRef, keyID string) error

    // Pull-mode only. Push-mode drivers return ErrNotApplicable.
    // The driver MUST paginate internally when the upstream API requires
    // it and stream batches back via the returned iterator. The broker
    // commits each batch to external_services_usage as it arrives so
    // partial failures don't lose accumulated work.
    FetchUsageSince(ctx context.Context, ref WorkspaceRef, since time.Time) (UsageIterator, error)

    // Build a deep-link or SSO URL into the product's UI for the given user.
    BuildLoginURL(ctx context.Context, ref WorkspaceRef, userUUID string) (LoginURL, error)

    // Per-tenant topology only. Shared drivers return ErrNotApplicable.
    ProvisionInstance(ctx context.Context, tenantUUID string) (InstanceRef, error)
    DestroyInstance(ctx context.Context, ref InstanceRef) error
}
```

**Idempotency**: `Provision` and `ProvisionInstance` MUST return the existing reference if called twice with the same arguments. `WorkspaceRef` and `InstanceRef` are persisted in `external_services_workspaces` BEFORE the underlying resource is created (status `pending`), so a partial failure can be retried safely against the same UUID.

**Asynchronous lifecycle**: `Provision` for a per-tenant Docker stack can take 30-120 seconds (image pull, container start, health check). It is therefore invoked **only from background workers**, never from HTTP handlers:

- For `sellable` products: the subscription activation path enqueues a `provision_workspace` job; the existing renewal-job worker pool consumes it.
- For `operator-only` products: the admin endpoint `POST /v1/admin/external-services/{code}/workspaces` returns `202 Accepted` immediately with `workspaceUUID` and `status: "pending"`; a background worker performs the actual provisioning.

`external_services_workspaces.status` carries the lifecycle state machine:

```
pending ──provision_ok──▶ active ──tenant.archived──▶ suspended
   │                         │                            │
   │                         │                            └─tenant.reactivated─▶ active
   │                         │
   └──provision_failed──▶ failed                          ┌─purge_deadline_passed─▶ purged
                              │                            │
active | suspended ──tenant.purged──▶ archived ───────────┘
```

Clients (UI, SDK) poll `GET /v1/me/external-services/workspaces/{uuid}` or `/v1/admin/external-services/workspaces/{uuid}` for status transitions. Provisioning failures land in `failed` with a structured `error{code, message}`; the admin UI offers a "retry" action that re-enqueues the job.

**Timeout discipline**: every driver call carries a context timeout. Defaults: `Provision` 5 minutes, `Deprovision` 2 minutes, `IssueAPIKey` 30 seconds, `FetchUsageSince` 2 minutes per batch, `BuildLoginURL` 5 seconds. Drivers MUST honor `ctx.Done()` and surface partial progress where possible.

**Failure isolation**: drivers MUST NOT exhaust broker resources on a misbehaving product. Each driver invocation is wrapped in a circuit breaker (per `(productCode, instance)` for per-tenant, per `productCode` for shared). When the breaker opens, the broker fails fast on subsequent calls for a configurable cooldown (default 5 minutes) and emits an alert via the `notification` module. Implementation: a vetted library such as `sony/gobreaker` — the choice is not architectural.

### 6. Versioned internal contract

Push-mode drivers expose three endpoints on the **service surface** (per ADR-0003 host split):

```
POST /internal/v1/external-services/keys/verify
POST /internal/v1/external-services/jobs/authorize
POST /internal/v1/external-services/usage
```

Auth: HMAC-SHA256 over the request body with the product's shared secret. Headers `X-Orkestra-Product` + `X-Orkestra-Signature: sha256=...` + `X-Orkestra-Key-ID: primary|secondary` (see "Secret rotation" below).

Outbound webhooks **from** the broker **to** the data plane use the symmetric protocol (same HMAC scheme, product-side secret) on the product's `/internal/v1/system-webhooks` endpoint. Events: `workspace.created`, `workspace.suspended`, `workspace.resumed`, `workspace.deleted`, `key.revoked`, `gdpr.changed`, `subscription.suspended`, `subscription.reactivated`, `credits.granted`.

This contract is treated as **public API**: OpenAPI spec generated and committed; breaking changes go to `/internal/v2/*` in parallel with documented deprecation for v1. Same discipline as the `/v1/*` public surface.

#### Webhook delivery semantics

Outbound webhooks are **at-least-once** and asynchronous:

- Every outbound event is persisted to `external_services_outbound_webhooks` (status `pending`) inside the same DB transaction that drove the event (capability grant, key revocation, etc.). The transaction either commits both the state change and the webhook row or neither — no event can be lost because the state change committed before enqueue.
- A worker drains pending rows and POSTs to the product's `/internal/v1/system-webhooks` with the HMAC signature. Success (HTTP 2xx) → `status=delivered, deliveredAt`. Failure → `status=pending, attempt++, nextAttemptAt = now + backoff(attempt)`.
- Backoff: exponential with jitter. Schedule `1m, 2m, 4m, 8m, 16m, 32m` (six attempts over ~63 min). After attempt 6 → `status=dead_letter`, alert via `notification` to the operator, surfaced in the admin UI for manual replay.
- Idempotency on the receiver: each event carries a `X-Orkestra-Event-ID` UUID. The product MUST deduplicate by event ID before applying state changes — guaranteed possible because every state change documented in the contract is idempotent (workspace deletion, key revocation, GDPR cascade are all naturally re-runnable).
- Ordering: NOT guaranteed across events. Consumers MUST treat each event as a self-contained instruction. Where ordering matters (`workspace.suspended` before `workspace.deleted`), the broker delays enqueueing the second event until the first is `delivered`.
- Replay: the admin UI exposes a "redeliver" button on dead-letter rows. Replay creates a new row with `attempt=1, originalEventID` carried forward; the receiver dedupes on the original event ID.

The push-mode contract from the data plane to the broker (`/internal/v1/external-services/usage`, etc.) follows the symmetric pattern: idempotency on `idempotencyKey`, broker returns 200 for already-processed events.

#### Secret rotation

The HMAC `SharedSecret` between Orkestra and a product is rotated via **dual-key overlap**:

- `Product.PrimarySecret` and `Product.SecondarySecret` (both encrypted at rest) coexist in the catalog. Either signs/verifies a valid request during the overlap window.
- Rotation procedure (admin-triggered at `POST /v1/admin/external-services/products/{code}/rotate-secret`):
  1. Generate `newSecret`. Move existing `PrimarySecret` to `SecondarySecret`. Set `PrimarySecret = newSecret`.
  2. Broker emits `secret.rotated` system webhook to the product carrying `newSecret` (one-time, the only event that carries a secret in the body, MUST use the existing — still-valid — `SecondarySecret` to sign).
  3. Product persists `newSecret` as its primary, keeps the previous one as fallback, acks.
  4. Broker waits for ack OR an operator-confirmed overlap timeout (default 24h), then clears `SecondarySecret`.
- Verification on incoming requests: the broker tries `PrimarySecret` first, then `SecondarySecret`. The `X-Orkestra-Key-ID` header is informational (helps debugging which key signed) but verification falls back through both regardless.
- For credential-pass products that can't accept a `secret.rotated` webhook (octo-stt v1): the operator manually rotates the secret via the product's admin UI and confirms ack in Orkestra. Same dual-key state machine.

Secret material is never logged in cleartext; the `compliance` audit log records `secret.rotation_started`, `secret.rotation_acked`, `secret.rotation_completed` with timestamps and actor IDs but no secret bytes.

### 7. Lifecycle integration

**`sellable` products**:

1. Operator registers a `Service` in `subscriptions_services` with `PricingTier.Capabilities = [product.CapabilityID]` and a new `PricingTier.GrantedUnits map[string]float64` (mapping tier price → quota, e.g. `{"seconds": 180000}` for `octo-stt-50h`).
2. Tier-2 client subscribes via `POST /v1/me/subscriptions` (existing flow).
3. `EntitlementSyncer.OnActivate` → `AccessProvider.GrantCapability(tenantUUID, product.CapabilityID)`.
4. `external_services` listens for capability grants (via a new `AccessProvider` event hook or by polling `ListCapabilityIDs` on subscription activation), calls `driver.Provision(tenantUUID, tier)`.
5. Workspace materialized; for push-mode products a `workspace.created` outbound webhook fires.
6. On renewal: balance topped up via `subscriptions_invoices` paid event → `external_services` increments `Workspace.CreditsBalance` by `tier.GrantedUnits`.
7. Cancel/suspend → reverse path: revoke capability → `driver.Deprovision`.

**`operator-only` products**:

1. Operator-admin at `/v1/admin/external-services/{code}/workspaces` selects an internal tenant.
2. Broker calls `driver.Provision(tenantUUID, nil)` (no tier).
3. Workspace materialized with `Plan="internal_unlimited"`, no balance enforcement.
4. Removal via admin endpoint; no automated lifecycle.

### 8. Deployment topology (per-tenant)

For `per-tenant` topology, the v1 implementation provisions each tenant's instance as a **Docker Compose service stamped from a template**. The driver:

- Holds a per-product template at `docker/templates/<product>/compose.yml` parameterized by `{tenantSlug, internalDNSName, dbPath, resourceClass, ...}`.
- `ProvisionInstance` writes a stamped compose file under `/var/lib/orkestra/external-services/<product>/<tenantSlug>/compose.yml`, runs `docker compose up -d`, returns `InstanceRef{InternalDNSName, Subdomain, DataPath}`.
- `DestroyInstance` runs `docker compose down --volumes` and removes the directory.
- Routing exposed via the existing edge nginx — subdomain `<tenantSlug>.<product>.tuodominio.it` proxied to `<internalDNSName>:<productInternalPort>` on a shared `external-services` Docker network.

#### Network model: no host port allocation

The default v1 design **does not expose any host ports** for per-tenant instances. All per-tenant stacks attach to a single shared Docker network (`external-services`) that the edge nginx container also joins. Inside that network, each instance is reachable by its container DNS name (`n8n-<tenantSlug>:5678`) — every n8n instance can use the same internal port because the host port-space is never involved.

Routing flow:
```
client browser
  └─▶ <slug>.n8n.tuodominio.it:443
        └─▶ edge nginx (TLS termination, subdomain → upstream lookup)
              └─▶ n8n-<slug>:5678  (resolved via Docker DNS on external-services network)
```

The subdomain → upstream mapping is held in a small `external_services_routes` collection consulted by an nginx-aware sidecar (or a Lua-scripted nginx variant); the broker writes a row at `ProvisionInstance` time and removes it at `DestroyInstance`. Edge nginx reads the table on reload — no static config rewrites needed.

#### When host ports ARE required (escape hatch)

Some products may require protocols that can't be fronted by HTTP (raw TCP, SSH, custom binary). For those a stateful allocation registry is mandatory:

- Collection `external_services_port_allocations` with unique constraint on `(host, port)` and `(workspaceUUID, productCode, role)`.
- Allocation is a `findOneAndUpdate` over a configured port range (default `30000-32767`, configurable per host) that picks the first free port — purely deterministic from DB state, never from hashing.
- Released on `DestroyInstance` (delete row); orphan reclamation via a TTL-driven cleanup job (default 24h after instance gone).
- No v1 product uses this path — it is reserved infrastructure.

#### Constraints the topology layer MUST enforce

- **Idempotent provisioning** — the directory path is `<base>/<product>/<tenantSlug>`, where `tenantSlug` is the stable, immutable slug on the `tenant` aggregate; re-running `ProvisionInstance` returns the existing instance without recreating it.
- **Network isolation** — each per-tenant compose stack runs on its own private Docker network for inter-container traffic (DB + app); only the public-facing container joins the shared `external-services` network. No direct port exposure on the host.
- **Resource limits** — compose service declares `mem_limit` and `cpus` derived from `tier.ResourceClass` (or a fixed value for `operator-only`).
- **Backup** — instance `DataPath` lives on the host filesystem at a tenant-namespaced subdirectory and is included in the host backup schedule.
- **Health gate** — `ProvisionInstance` returns success only after the instance passes a driver-defined health probe (HTTP 200 on `/healthz`, container `healthy` status, etc.) within the provisioning timeout. Otherwise it transitions the workspace to `failed`.

Out of scope (deferred to a follow-up ADR if/when scale demands):

- **Fleet upgrades / Day-2 reconciliation** — when n8n ships a new version, no automated drift detection or rolling restart is provided in v1. Operator-triggered upgrade per instance via a future admin endpoint `POST /v1/admin/external-services/instances/{id}/upgrade`; bulk and reconciliation patterns come with the K8s migration.
- Migration to Kubernetes (namespace-per-tenant, NetworkPolicy, PVC).
- Multi-host scheduling (compose is single-host by design).
- Per-tenant resource autoscaling.
- Live-resize of `ResourceClass` (today: stop + reprovision with new tier).

The choice of Docker Compose is intentional: it matches the existing Orkestra stack and avoids ops investment until per-tenant instance count justifies it.

### 9. SSO operator → remote UI

Two flows accepted, picked per product:

**Orkestra-as-IdP** (preferred where the product supports OIDC):

- The auth module gains a JWKS publish endpoint at `/.well-known/jwks.json` (new — does not exist today; pre-requisite for Phase 4).
- `iface.JWTProvider` (existing, RS256, key-rotated) issues a short-lived token with `aud = "external-service:<productCode>"`, custom claims `workspaceRef`, `tenantUUID`, and a `scopes` array translated from Orkestra roles to product roles.
- The product validates the JWT signature against the published JWKS, maps the `workspaceRef` claim to its native workspace/project, applies the role from `scopes`.
- `driver.BuildLoginURL` returns the product's OIDC entry point with the token attached.
- For n8n Enterprise: OIDC integration consumes the token, maps `workspaceRef` claim to the n8n project.

**Credential-pass** (where the product only supports API key auth, e.g. octo-stt v1):

- `driver.BuildLoginURL` returns `LoginURL{ProductURL, KeyPlaintext}` where `KeyPlaintext` is non-empty only on first issuance.
- Orkestra UI shows the key once (admin-facing modal with copy button), operator pastes into the product's login.
- Same UX pattern Orkestra uses today for Stripe API keys in `/admin/modules/payments`.

**Revocation**:

- Orkestra-as-IdP: JWT TTL kept short (15 min default, configurable per product). Refresh denial on key revocation. Push-mode products also receive `key.revoked` webhook for cache invalidation.
- Credential-pass: data plane cache TTL ≤ 60s (octo-stt's existing design). `key.revoked` webhook invalidates cache immediately for push-mode products.

**Audit**: every SSO token issuance writes to the `compliance` audit log with `(actorUserUUID, tenantUUID, productCode, issuedAt, expiresAt)`.

Out of scope:

- Per-product OIDC scope/claim mapping → driver-specific implementation detail.
- UI design for the credential-pass modal → frontend concern.
- SAML support → can be added per-driver if needed; OIDC suffices for n8n.

### 10. Data and GDPR

**Applicability**: the cascade described below applies only to products with `DataResidency = resident`. For `passthrough` products (stateless processors like docling, crawl4ai), there is no remote customer data to delete; instead `tenant.archived` revokes the workspace's API keys (immediate) and `tenant.purged` removes the catalog binding (`external_services_workspaces` row marked `purged`). The workspace lifecycle state machine (§5) still applies, but Phase 1/2 below are short-circuited and no `workspace.deleted` cascade webhook is emitted. Passthrough drivers therefore implement `SuspendWorkspace` as "revoke API keys" and `Deprovision` as "revoke keys + unbind catalog row"; both are idempotent and cheap.

Customer-relevant data (transcripts, workflows, embeddings, audio files) of `resident` products **resides in the external service**, never in Orkestra. Orkestra stores only:

- Tenant identity and metadata (`tenant` aggregate from ADR-0001).
- Integration binding (`external_services_workspaces`).
- API key material (hashed) and key metadata (`external_services_apikeys`).
- Usage events (`external_services_usage`, append-only).

GDPR cascade is **two-phase with a recovery grace period** — a hard delete cannot be triggered by a single admin click or an accidental tenant deletion, because external-service data (audio, transcripts, workflows) is unrecoverable even from an Orkestra DB backup.

Phase 1 — soft delete on `tenant.archived`:

1. `compliance` addon raises `tenant.archived` (tenant marked deleted, sub-state of ADR-0001's status field).
2. `external_services` listens, iterates the tenant's workspaces.
3. For each, calls `driver.SuspendWorkspace(ref)`. Effect per driver:
   - Push-mode shared (octo-stt): emit `workspace.suspended` webhook → product blocks all API key auth for the workspace and stops accepting new jobs; data retained.
   - Pull-mode per-tenant (n8n): driver stops the compose stack (`docker compose stop`) but does not remove volumes; subdomain returns 503.
4. Workspace transitions to `suspended`. Audit row written. **Data is preserved**.

Phase 2 — hard delete on `tenant.purged`, after grace period:

1. Default grace period is **30 days** from `tenant.archived` to `tenant.purged`, configurable per product (`Product.PurgeGracePeriodDays`) and per tenant (operator can extend on request, never shorten below 7 days without a documented reason in the audit log).
2. A daily reconciliation job in `compliance` finds tenants archived past the grace window and transitions them to `purged`.
3. `external_services` reacts: calls `driver.Deprovision` for each workspace → emits `workspace.deleted` webhook to push-mode products → they cascade-delete data and ack with HTTP 200.
4. For per-tenant topology: `DestroyInstance` runs `docker compose down --volumes` and removes the data directory.
5. After ack (or after the dead-letter window for push-mode products that don't respond), the broker marks `external_services_workspaces.purged_at`. Workspace transitions to `purged`.

**Pull-mode products without a delete API**: the broker emits a manual operator task in the compliance audit trail at the start of Phase 2. The operator performs the deletion out of band (via the product's admin UI or DB) and confirms in Orkestra; only then does the workspace move to `purged`. Grace period applies normally.

**Restore from archived state**: while a workspace is `suspended` (before grace expiry), an operator can transition it back to `active` via `POST /v1/admin/external-services/workspaces/{uuid}/restore`. This emits `workspace.resumed` to push-mode products (re-enable API keys) or `docker compose start` for per-tenant (resume the existing stack). After `purged`, restore is impossible by definition.

Data residency: each product declares its `DataRegion` in the catalog. Tenants with strict residency requirements only see products matching their region. Initial release: all products are `eu` (octo-stt on `ai-1`, n8n on the Orkestra host).

### 11. Brainstorm — stock-media vendor APIs (Pexels et al.)

**Status: brainstorm only — not part of v1.** Recorded here to make the boundary explicit and to capture which patterns survive when a future `media` addon lands.

The Pexels API (free, attribution-required) and peers (Unsplash, Pixabay, Shutterstock, Getty) are an obvious recurring need across consumer modules:

- `agents` composing marketing copy or social drafts needs imagery.
- `documents` / PDF templates want hero photos in generated material.
- `sales` outbound drafts may want illustrative attachments.
- A future Tier-2 content-pack subscription (e.g. "AI content — 1000 generations + curated stock imagery") would expose search to clients as a sellable feature.

The question is whether stock-media providers belong in the `external_services` broker. **They do not** — but walking through why is useful because the broker's pattern keeps tempting you to absorb anything HTTP-shaped, and Pexels is the cleanest negative example.

#### Why the boundary test (§1) fails

Pexels is a SaaS vendor API, not a fleet member:

- The operator cannot deploy, upgrade, monitor, or decommission Pexels — fails the "self-hosted fleet" criterion outright.
- There is no per-tenant workspace at Pexels. Every tenant calls the same global, read-only catalog with the same operator-issued API key (or per-tenant keys if the operator chooses, but the *catalog itself* is shared, public, and not managed by us).
- Provisioning, key issuance against the *product*, SSO into a remote UI, and GDPR cascade across remote tenant data all become trivial or meaningless: the only customer-relevant artifact is the `(URL, attribution)` pair we embed in tenant-generated content, and that lives in Orkestra-side storage.
- The four classification axes degenerate when projected onto Pexels: Topology would be `shared` with no remote workspace to materialize; DataResidency would be `passthrough` but with no operator-controlled processor to provision; Metering reduces to "count outbound calls", with no symmetric webhook contract to define.

This is the same conclusion §1 already reaches for Stripe and Mailgun; stock-media APIs are another instance of the same class.

#### What the integration *does* look like

A new feature addon — call it `media` — mirrors the `payments`/`notification` pattern:

- ConfigService-managed credentials per vendor (`PEXELS_API_KEY`, `UNSPLASH_ACCESS_KEY`, ...) with the existing sandbox/production environment switching.
- A small `iface.MediaProvider` (search, by-id, attribution-string) implemented per vendor; the addon picks a primary and an ordered fallback chain so Pexels exhaustion falls through to Unsplash, then Pixabay.
- Per-tenant rate-limit accounting in Redis. Pexels free tier is 200 req/h per key; if Tier-2 clients share one operator key, the addon must throttle per `tenantUUID` so one client cannot drain the global quota for everyone.
- Attribution persisted alongside any image URL embedded in tenant-generated content (in `documents` output, in `agents` traces). Pexels and Unsplash both require photographer credit on display — failing to record attribution on first fetch makes compliance retroactively impossible.
- Asset proxying / cache layer optional, decided per-vendor in the driver. Pexels CDN URLs are stable enough to embed directly; Pixabay rotates URLs within ~24h so embedding raw URLs in long-lived PDFs is unsafe and the addon must cache to its own object store (RustFS substrate, when available).

#### Patterns worth reusing from this ADR (without joining the broker)

Three patterns from `external_services` are independently useful for `media`:

1. **Capability gating** — if stock-media access becomes a Tier-2 subscription feature, the `subscriptions` capability mechanism applies regardless of whether the provider is fleet-resident or vendor-hosted. Capability IDs are the gating primitive, not the broker.
2. **Usage metering** — counter-based `mediaSearches` quota per tenant, fed by the addon's outbound HTTP middleware. The storage shape of `external_services_usage` is reusable; the broker plumbing around webhooks and idempotency keys is not.
3. **Audit logging** — every search emitted to the `compliance` audit log with `(tenantUUID, vendor, query, resultCount)`. Defends license-compliance audits and "you let our brand name be searched for stock photos" claims.

If a fourth or fifth vendor-API addon accumulates these same three needs (translation, weather, geocoding, sentiment APIs), extracting a shared `vendor_apis` SDK package — **distinct from `external_services`** — becomes worthwhile. v1 is too early; revisit when the second vendor-API integration is concrete.

#### Decision (for this ADR)

- Stock-media vendors are explicitly out of scope for the `external_services` broker.
- A future `media` addon consumes them through standard ConfigService + per-vendor HTTP client patterns, not through the `ProductDriver` contract.
- The three reusable patterns above (capability gating, metering shape, audit logging) cross the boundary at the SDK level if and when a second vendor-API addon justifies extracting them.

## Consequences

### Positive

- **One pattern for all external integrations** — octo-stt, n8n, future octo-ocr, and even cloud bursts (AssemblyAI) follow the same broker.
- **Reuses existing infrastructure** — `subscriptions` for sellable products, `iface.JWTProvider` for SSO, edge nginx for instance routing, `compliance` for audit, `notification` channels for system webhook delivery.
- **Aligns with octo-stt's existing design** — its `AuthProvider` abstraction is exactly what Orkestra plugs into. Zero refactor on octo-stt side beyond implementing `OrkestraAuthProvider`.
- **Clean Tier-1/Tier-2 distinction** — operator-only products skip the entire subscription chain; sellable products opt in via a single capability ID.
- **Forces idempotency and audit discipline** — provisioning, key issuance, usage reporting all require it. Failures become recoverable.

### Negative / costs

- **New addon to build and maintain** — ~3-5k LOC initial, plus ~500 LOC per driver.
- **Two sources of truth for API keys** — master in Orkestra, cache in data plane. Discipline required on revocation propagation; mitigated by short cache TTL and explicit `key.revoked` webhook.
- **Polling complexity for pull-mode** — scheduled job, backoff per instance, watermarks. Manageable but not free.
- **Operational coupling on per-tenant topology** — every n8n instance is an extra service to monitor, back up, upgrade. Mitigated by Compose template automation but doesn't disappear.
- **Versioned contract becomes public surface** — changing `/internal/v1/external-services/*` now requires the same care as changing the public `/v1/*` API.
- **Dead-letter management is a recurring operator task** — webhook delivery is at-least-once with retries, but products that go down for >1h produce dead-letter entries that need human attention. The admin UI must surface them prominently or they will rot.
- **Grace-period management is a recurring compliance task** — the 30-day window is a safety net but also a retention obligation. Operators must respond to legitimate "delete now" requests by manually shortening the grace period through the audit-logged endpoint.

### Out of scope (future ADRs if needed)

- **Day-2 fleet upgrade reconciliation** — per-tenant Compose stack drift detection, rolling restart with new image tag, version-pinning policy. Needed once n8n fleet exceeds ~5 instances.
- Migration of per-tenant topology to Kubernetes when single-host stops scaling.
- Cross-region routing for tenants with non-EU residency requirements.
- Tier-change live-resize for per-tenant instances (v1: stop + reprovision with new `ResourceClass`).
- Cloud burst / fallback routing (e.g. octo-stt → AssemblyAI when GPU saturated).
- BYO topology (`byo` axis value).
- **Storage-as-product gauge metering** — sellable storage products (RustFS exposed to Tier-2 as e.g. `storage — 100GB`) require gauge-based usage tracking (`bytes_stored × time`, integrated over the billing period) rather than the counter-based `GrantedUnits` model in §7, and request-time `jobs/authorize` is impractical for every S3 op. Deferred to a follow-up ADR. RustFS as a Tier-1 substrate (no per-tenant quota enforcement) is *not* blocked by this — it can ship under v1 as an `operator-only` product.

## Alternatives considered

**Alternative 1 — One addon per product** (`addon_octo_stt`, `addon_n8n`).
Rejected. Each product would reimplement key issuance, usage aggregation, GDPR cascade, system webhook delivery. ~80% code duplication and inconsistent semantics across products. The third product makes the duplication obvious; doing it from product #1 is cheaper.

**Alternative 2 — Bake external service support into `subscriptions`**.
Rejected. Couples billing logic with provisioning logic; cannot accommodate `operator-only` products (no subscription); `subscriptions` is already its own extracted module and shouldn't grow this responsibility.

**Alternative 3 — Point-to-point integration in each consumer module**.
Rejected. No central place for API keys, no GDPR cascade, no shared metering. Every consumer reinvents the boundary.

**Alternative 4 — Defer until the third external service appears**.
Rejected. octo-stt and n8n already differ along all three axes (audience, protocol, topology). Without the framework, integrating each is an ad-hoc design; the third one (octo-ocr or AssemblyAI burst) is foregone. Designing the framework now costs the same as designing octo-stt's integration in isolation.

**Alternative 5 — Reuse the AI sidecar's `RemoteAIModelProvider` pattern**.
Rejected. That pattern splits a single product's deployment for resource reasons; both halves share a `go.mod` and a team. External services have a separate repo, team, customer-facing surface, and release cycle. Different boundary, different concerns.

## Implementation phases

| Phase | Scope | Outcome |
|---|---|---|
| **1 — Broker skeleton** | `external_services` addon: models, catalog CRUD, admin routes (`/v1/admin/external-services/products`, `/workspaces`, `/keys`), capability registration, `ProductDriver` interface in `pkg/sdk/iface/`. No concrete drivers yet. | Operator-admin UI exists; products can be registered but nothing is provisioned. |
| **2 — octo-stt driver (push, shared, sellable, resident)** | `driver_octo_stt.go` with `Provision`/`Deprovision`/`IssueAPIKey`. Internal endpoints `/internal/v1/external-services/{keys/verify, usage}`. Outbound `workspace.created/deleted` webhooks. Side of octo-stt: implement `OrkestraAuthProvider`. | Tier-1 internal tenant can use octo-stt end-to-end with Orkestra as control plane. |
| **3 — Subscription wiring** | `EntitlementSyncer` observer in `external_services`, capability `external-services.octo-stt.workspace`, Tier-2 self-service routes `/v1/me/external-services/*`. `PricingTier.GrantedUnits` field added. Quota enforcement via `/internal/v1/external-services/jobs/authorize`. | Tier-2 client can buy and consume octo-stt subscription end-to-end. |
| **4 — n8n driver (pull, per-tenant, operator-only, resident) + JWKS + SSO** | `driver_n8n.go`, Docker Compose template at `docker/templates/n8n/`, edge nginx subdomain routing, polling job for usage. Auth module exposes `/.well-known/jwks.json`. Orkestra-as-IdP SSO via `iface.JWTProvider` with `aud=external-service:n8n`. | Operator can provision n8n per internal tenant with SSO login from Orkestra UI. |
| **5 — Passthrough driver exemplar (docling — push, shared, passthrough)** | `driver_docling.go` with `Provision`/`Deprovision`/`IssueAPIKey` shaped for the passthrough path: no cascade webhook on delete, `SuspendWorkspace` revokes keys only. Exercises the §10 passthrough exemption end-to-end. Validates that stateless processors get the catalog, capability gating, key issuance, usage metering, and SSO without paying for the GDPR-cascade machinery. | Stateless services join the fleet under the same broker; framework is proven against both residency shapes before GA. |
| **6 — GDPR cascade + audit** | `compliance` listener for `tenant.purged`, cascade `Deprovision` across all `resident` workspaces (passthrough workspaces unbind without webhook per §10). SSO audit log entries. `external_services_workspaces.purged_at` lifecycle. Manual-task fallback for pull-mode resident products without a delete API. | Tenant erasure deletes data across all integrated services; audit trail complete; GA-ready. |

Each phase is independently deployable. Phases 2-3 unblock octo-stt; phase 4 unblocks n8n; phase 5 unblocks docling/crawl4ai and validates the passthrough path; phase 6 closes compliance gaps before GA.

## References

- octo-stt `SPECS.md` §9 — internal contract design that this ADR implements on the Orkestra side
- `backend/internal/addons/subscriptions/CLAUDE.md` — entitlement syncer pattern this ADR reuses
- `backend/internal/addons/payments/CLAUDE.md` — driver façade pattern this ADR mirrors
- [ADR-0001](0001-unified-tenant-model.md) — Tenant aggregate that owns the foreign key into external services
- [ADR-0003](0003-three-audience-host-split.md) — Host split that determines which surface each external-service route mounts on
