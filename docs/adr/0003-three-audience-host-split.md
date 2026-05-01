# ADR-0003 — Three-audience API host split (`operator` / `client` / `service`)

| Field | Value |
|---|---|
| **Status** | Proposed |
| **Date** | 2026-04-30 |
| **Authors** | @salvatore.balestrino |
| **Supersedes** | — |
| **Related** | [ADR-0001](0001-unified-tenant-model.md), [ADR-0002](0002-metrics-label-schema.md) |

## Context

Orkestra exposes one HTTP surface today (port 3000, host-agnostic). Every endpoint — operator-only admin endpoints, client-facing subscription/payment flows, and service-to-service AI sidecar calls — shares the same `huma.API` instance, the same OpenAPI document, the same JWT validation rule, the same CORS allowlist, and the same rate-limit policy. The 2026-04-18 tenancy work (ADR-0001) made the **two-tier** mental model explicit in the data layer (`TenantKind = internal | external`); the API surface still does not reflect that distinction.

Concrete problems with a single surface:

1. **Audience leakage.** A stolen Tier-2 client token is structurally able to hit operator endpoints; the only thing stopping it is per-route RBAC, which is opt-in and easy to forget on a new endpoint.
2. **OpenAPI bloat.** A future external SDK consumer would receive a spec that documents `/v1/admin/modules`, `/v1/dev/token`, and `/v1/internal/ai/*` alongside the public client API — a leaky abstraction that exposes operator surface area to clients reading `/openapi.json`.
3. **Coupled CORS / rate-limit / WAF policy.** Tightening rate limits for a noisy public client unavoidably tightens them for the operator console SPA, and vice versa.
4. **Single login endpoint.** `/v1/auth/login` cannot tell operator from client at issuance time — a Tier-2 sign-in and a Tier-1 sign-in produce indistinguishable tokens, which complicates session telemetry, abuse detection, and forensics.

Tier-2 service consumption (clients invoking AI agents, RAG, document generation) is documented in `CLAUDE.md` but **not yet implemented** as client-facing endpoints; today only the lifecycle modules (subscriptions, payments) have client-relevant routes. This ADR locks the audience split now so the runtime endpoints land cleanly in a follow-up phase rather than retrofitting one.

We remain in pre-GA development with permission to make breaking schema and API changes.

## Decision

Split the API surface into **three audiences**, each with its own JWT `aud` claim, its own OpenAPI document, its own CORS allowlist, its own rate-limit policy, and (for the two public audiences) its own DNS hostname. **One Go binary, three `huma.API` instances**, dispatched by `Host` header at the application layer.

### Audiences

| Audience | Host (prod) | Host (dev) | JWT `aud` | Purpose |
|---|---|---|---|---|
| `operator` | `console.orkestra.com` | `console.localhost:3000` | `operator` | Tier-1 operator dashboard — module admin, system config, internal billing/SDI, Tier-1 self-invoicing, dev tooling |
| `client` | `api.orkestra.com` | `api.localhost:3000` | `client` | Tier-2 client tenants — onboarding, subscriptions, payments (Phase E adds AI runtime) |
| `service` | *(none — internal network only)* | *(internal docker network)* | `service` | Service-to-service AI sidecar (`/v1/internal/*`); not exposed by ingress |

`service` continues to be reached via `AI_SERVICE_URL` (e.g. `http://orkestra-ai-dev:3100`) on the docker network. Ingress publishes only `console.*` and `api.*`.

### Module bucketing

| Module | Operator | Client | Notes |
|---|:---:|:---:|---|
| `auth` | ✓ | ✓ | Login paths split per audience (see below); same module, two registrations |
| `user` (`/me`) | ✓ | ✓ | Tier-aware; the underlying user record lives in the audience's collection |
| `notification` (prefs) | ✓ | ✓ | Per-user preferences relevant to both tiers |
| `navigation` | ✓ | ✓ | Each audience gets its own menu, sourced from modules registered on that audience |
| `subscriptions` | | ✓ | Client-only — Tier-2 catalog browse, subscribe, view activity |
| `payments` | | ✓ | Client-only — Stripe checkout, payment methods, invoices |
| `onboarding` | | ✓ | Client-only — anonymous Tier-2 signup |
| `billing` | ✓ | | Operator-only — Tier-1 SDI/FatturaPA self-invoicing |
| `documents`, `company`, `graph`, `aimodels`, `rag`, `agents`, `sales`, `dev` | ✓ | | Operator-only today; `agents`/`rag`/`documents` gain client-facing runtime routes in Phase E |

**Phase E (deferred)** adds entitlement-gated client routes for AI consumption: `/v1/agents/run`, `/v1/rag/query`, `/v1/documents/generate` mounted on the `client` API, gated by `subscriptions` entitlement, dispatched internally to the `service` audience.

### Contract change — `RouteInfo` becomes per-audience

The current `module.RouteInfo` (`backend/internal/shared/module/module.go:337`) carries one `PublicAPI huma.API` plus one `ProtectedRouter chi.Router`. Replace with:

```go
type APISurface struct {
    Audience       Audience            // operator | client
    PublicAPI      huma.API            // unauthenticated routes for this audience
    ProtectedRouter chi.Router         // auth-mw-applied chi router for this audience
    AuthMW         RoleMiddleware      // tier-aware: rejects tokens whose aud != Audience
}

type RouteInfo struct {
    Operator      *APISurface          // nil if module does not register on operator
    Client        *APISurface          // nil if module does not register on client
    Router        chi.Router           // root router (SSE / dev / unbucketed)
    APIConfig     huma.Config
    ConfigService *ModuleConfigService
}

type Audience string
const (
    AudienceOperator Audience = "operator"
    AudienceClient   Audience = "client"
    AudienceService  Audience = "service"
)
```

A module's `RegisterRoutes(ri *RouteInfo)` chooses which surface(s) to register on. Operator-only modules ignore `ri.Client`; client-only modules ignore `ri.Operator`; "both" modules register the same handler on both surfaces. The handler reads audience from request context (stamped by the host-mux) when behavior must diverge.

### Host mux

`cmd/server/main.go` constructs two `chi.Mux`es and two `huma.API`s, wraps them in a `hostRouter`, and serves a single port:

```go
operatorMux := chi.NewRouter()
clientMux   := chi.NewRouter()
operatorAPI := humachi.New(operatorMux, operatorHumaConfig())
clientAPI   := humachi.New(clientMux,   clientHumaConfig())

// Audience pre-handler baked in at construction — non-skippable
operatorAPI.UseMiddleware(requireAudience(AudienceOperator))
clientAPI.UseMiddleware(requireAudience(AudienceClient))

routeInfo := &module.RouteInfo{
    Operator: &module.APISurface{Audience: AudienceOperator, PublicAPI: operatorAPI, ...},
    Client:   &module.APISurface{Audience: AudienceClient,   PublicAPI: clientAPI,   ...},
}
registry.RegisterAllRoutes(routeInfo)

hostMux := newHostRouter(map[string]http.Handler{
    cfg.ConsoleHost:    operatorMux,
    cfg.ClientAPIHost:  clientMux,
}, /* default */ operatorMux /* dev convenience */)

http.ListenAndServe(":"+cfg.Port, hostMux)
```

`newHostRouter` strips the port from `r.Host`, looks up the matching mux, and 421-Misdirected-Request if no host matches in non-dev mode. In dev (`ENV=development`), an unmatched host falls through to `operatorMux` to preserve `localhost:3000` ergonomics.

### JWT audience enforcement

JWTs gain a mandatory `aud` claim (`operator` | `client` | `service`). The `requireAudience(expected)` middleware mounted at `huma.API` construction time:

1. Extracts the bearer token, validates signature & expiry.
2. Compares `claims.aud` to `expected`. Mismatch ⇒ `401 Unauthorized` with code `AUDIENCE_MISMATCH`.
3. Stamps audience into request context for handlers that need it.

This is **non-skippable**: it lives at the `huma.API` middleware level, not on individual routes. A route mistakenly mounted on the wrong API still fails closed.

`service` audience tokens are issued by the monolith for sidecar calls only; the AI sidecar (`cmd/ai-service/main.go`) validates `aud=service` exclusively via `JWTValidator` (already public-key-only, no auth module dep).

### Auth path split

Two distinct login paths per ADR decision:

| Path | API | Token issued |
|---|---|---|
| `POST /v1/auth/operator/login` | operator | `aud=operator` |
| `POST /v1/auth/operator/refresh` | operator | `aud=operator` |
| `POST /v1/auth/operator/logout` | operator | — |
| `POST /v1/auth/client/login` | client | `aud=client` |
| `POST /v1/auth/client/register` | client (public) | `aud=client` (post-onboarding) |
| `POST /v1/auth/client/refresh` | client | `aud=client` |
| `POST /v1/auth/client/logout` | client | — |
| `GET /v1/auth/{operator|client}/oauth/{provider}/start` | per-audience | — |
| `GET /v1/auth/oauth/{provider}/callback` | both (state-routed) | per `state.tier` |

OAuth callback decodes `state` (CSRF + `tier` field signed) and routes the resulting session to the matching tier. There is **no** `/v1/auth/operator/register` — operator users are provisioned exclusively through `/v1/admin/users` by an existing operator (per decision: no operator self-signup).

Password reset, MFA enrolment, WebAuthn registration follow the same `/{operator|client}/...` prefix discipline.

### Split user/auth collections

Hard isolation at the data layer. No shared user record across tiers.

#### Collections

| Existing | New | Notes |
|---|---|---|
| `users` | `operator_users`, `client_users` | Wholesale migrate existing rows → `operator_users` (all current users are internal staff) |
| `sessions` | `operator_sessions`, `client_sessions` | Refresh tokens audience-scoped |
| `password_reset_tokens` | `operator_password_reset_tokens`, `client_password_reset_tokens` | |
| `oauth_identities` | `operator_oauth_identities`, `client_oauth_identities` | Same `(provider, providerUserID)` may legitimately exist on both tiers as separate identities |
| `mfa_secrets` / `webauthn_credentials` | per-tier pairs | |
| `roles` | `operator_roles`, `client_roles` | Operator role catalog (super_admin, administrator, developer, manager, operator, guest) ≠ client role catalog (owner, admin, member, viewer) |
| `unsubscribe_tokens` | per-tier pairs | Notification module reads from the matching tier |

Documents, audit logs, capabilities, entitlements, tenant collections (per ADR-0001) remain single — they reference `tenantUUID` which already disambiguates tier via `Tenant.Kind`.

#### Schema additions

`operator_users` and `client_users` are nearly identical in shape, but each carries a constant `tier` discriminator field for defense-in-depth (so a misrouted query against the wrong collection fails an integrity assertion):

```go
type OperatorUser struct {
    UUID            string
    Tier            string             // const "operator" — guard field
    TenantUUID      string             // FK → tenants where Kind=internal
    Email           string             // unique within (Tier=operator)
    PasswordHash    string             // argon2id
    Roles           []string           // FK → operator_roles
    // ... standard user fields (CreatedAt, UpdatedAt, MFA flags, etc.)
}

type ClientUser struct {
    UUID            string
    Tier            string             // const "client" — guard field
    TenantUUID      string             // FK → tenants where Kind=external
    Email           string             // unique within (Tier=client)
    PasswordHash    string
    Roles           []string           // FK → client_roles
    // ... standard user fields
}
```

A repository invariant test (`backend/internal/core/auth/repository/integrity_test.go`) asserts every read from `operator_users` returns `Tier == "operator"` and likewise for clients.

### `shared/iface` — provider split

The current `iface.UserProvider` is replaced by two providers. Consumers pick which they need:

```go
// backend/internal/shared/iface/user.go
type OperatorUserProvider interface {
    GetByUUID(ctx context.Context, uuid string) (*OperatorUser, error)
    GetByEmail(ctx context.Context, email string) (*OperatorUser, error)
    ListByTenant(ctx context.Context, tenantUUID string) ([]OperatorUser, error)
    // ...
}

type ClientUserProvider interface {
    GetByUUID(ctx context.Context, uuid string) (*ClientUser, error)
    GetByEmail(ctx context.Context, email string) (*ClientUser, error)
    ListByTenant(ctx context.Context, tenantUUID string) ([]ClientUser, error)
    // ...
}
```

`ServiceRegistry` keys: `iface.SvcOperatorUserProvider`, `iface.SvcClientUserProvider`. Modules consume via `module.MustGetTyped[iface.OperatorUserProvider](registry, iface.SvcOperatorUserProvider)`. A module that legitimately needs both (rare — `notification` for delivering emails to either tier) injects both.

### Hard cutover

JWT structural version bumps from `v1` → `v2`. `v2` tokens carry `aud`. `v1` tokens are rejected at the boundary as soon as the new code lands. All sessions invalidated; every user re-authenticates once. Acceptable because:

- We are pre-GA and Tier-2 has effectively zero live users.
- All current users are operators who can re-login through the operator console without coordination.
- Avoids weeks of dual-validation logic and the risk of grace-period bugs.

The frontend operator dashboard is updated to point at `console.*` in the same release; the existing client-facing onboarding flow (if pointed at the legacy host) gets a one-shot redirect via ingress.

### OAuth state encoding

The `state` parameter sent to OAuth providers becomes a signed JWT (HS256 with a short-lived secret rotated daily) carrying:

```json
{
  "csrf": "<random>",
  "tier": "operator" | "client",
  "exp": <now + 10min>
}
```

The single callback `/v1/auth/oauth/{provider}/callback` decodes `state`, validates CSRF, and dispatches to the tier-appropriate user-creation/login flow. This avoids per-tier callback URL registration with the OAuth provider, which would require duplicating every OAuth app.

## Consequences

### What we enable

- **Operator-only OpenAPI** at `console.orkestra.com/openapi.json` is the authoritative operator surface — narrow, audit-friendly, never leaks to public SDK consumers.
- **Client-only OpenAPI** at `api.orkestra.com/openapi.json` is the SDK-able external API. SDK generation (typescript-fetch, openapi-go) becomes meaningful.
- **Independent rate-limit / CORS / WAF policy per audience.** Operator can be tight (small known origin set) while client can be generous (wildcard subdomains for white-label client SPAs).
- **JWT-level isolation.** A leaked client token cannot exercise operator routes even if RBAC is misconfigured. Defense in depth.
- **Login-source forensics.** Operator vs client login attempts are distinguishable in logs and metrics from request 1.
- **Phase E lands cleanly.** Adding `/v1/agents/run` etc. to the `client` API later is a per-route decision, not an architecture change.
- **Future Kubernetes split.** Operator and client APIs can be sharded onto separate `Deployment` objects with independent HPA without code changes — `hostRouter` becomes ingress routing.

### What we give up

- **One PR-week of refactoring.** Every module's `RegisterRoutes` signature changes; all consumers of `iface.UserProvider` change. Mechanical but pervasive.
- **Login UX coordination.** Two login pages, two refresh flows, two cookie scopes (`Domain=console.orkestra.com` vs `Domain=api.orkestra.com`). Cross-tier users (rare) maintain two sessions.
- **Cookie domain split.** No cookie sharing between hosts — by design.
- **Operations cost.** Two TLS certs, two DNS records, two ingress routes. Minor on Let's Encrypt + Traefik.

### Forbidden patterns (non-negotiable post-cutover)

- **Single shared user collection.** Re-introducing a `users` table that mixes tiers reverses the ADR.
- **Audience-routing at the route-decorator level.** Audience must be enforced at `huma.API` construction time, not per-handler.
- **Reading the wrong tier's collection from a tier-scoped service.** Operator services must not read `client_users`; the integrity test catches this.

## Implementation

### PR sequence

Land **after** the in-flight tenant-consolidation series (current branch: `feat/tenant-consolidation-pr4-billing-customer-tenant-link`) finishes merging to `main`. Reason: `client_users.TenantUUID` binds to the consolidated `Tenant{Kind=external}` from ADR-0001; doing both in parallel will create merge conflicts in `auth/repository` and `subscriptions`.

| PR | Title | Scope | Mergeable independently |
|---|---|---|---|
| **PR-A** | `module.APISurface` and per-audience `RouteInfo` | Mechanical signature change across 12 modules. Both `Operator` and `Client` initially point at the same mux — no behavior change. Ship as a no-op refactor. | Yes — fully reversible |
| **PR-B** | Split user/auth collections + tier-aware providers | Heavy. New collections, new providers (`OperatorUserProvider`, `ClientUserProvider`), data migration script (`backend/scripts/migrate_user_split.go`), integrity tests, repository tier guards. Behavior preserved (single login path still works). | Yes — was gated by feature flag `USER_TIER_SPLIT_ENABLED`; flag removed in PR-D D-8 hard cutover |
| **PR-C** | Host mux + audience middleware + new env vars | `hostRouter`, two `huma.API`s, two `chi.Mux`es, `requireAudience` middleware. New env: `CONSOLE_HOST`, `CLIENT_API_HOST`, per-audience `*_CORS_ORIGINS`, `*_RATE_LIMIT_*`. Frontend operator dashboard moves to `console.*`. JWT issuance still single-aud (`operator` for everything; clients still issued via legacy path). | Yes |
| **PR-D** | Auth path split + JWT v2 + hard cutover | `/v1/auth/{operator|client}/login` + per-tier refresh/logout/MFA/WebAuthn. JWT version bump, `v1` rejection. OAuth state-encoded tier. Onboarding module moves to `client` API. Sessions invalidated. | **Coordinated release** — frontend + backend land together |
| **PR-E** *(future)* | Client-facing AI runtime endpoints | `/v1/agents/run`, `/v1/rag/query`, `/v1/documents/generate` on `client` API, gated by `subscriptions` entitlement, dispatched to `service` audience via existing remote providers (`shared/remote/`). | Yes |

PR-A and PR-B can land concurrently with the post-tenant-consolidation `main`. PR-C depends on PR-A. PR-D depends on PR-B + PR-C. PR-E depends on PR-D + a `subscriptions` entitlement-enforcement primitive.

### Migration script outline

`backend/scripts/migrate_user_split.go` (run once, pre-deploy of PR-D):

```
1. Open Mongo with admin credentials from MONGO_URI.
2. For each existing collection:
   a. users                   → operator_users    (set tier="operator", validate every row)
   b. sessions                → operator_sessions
   c. password_reset_tokens   → operator_password_reset_tokens
   d. oauth_identities        → operator_oauth_identities
   e. mfa_secrets             → operator_mfa_secrets
   f. webauthn_credentials    → operator_webauthn_credentials
   g. roles                   → operator_roles
   h. unsubscribe_tokens      → operator_unsubscribe_tokens
3. Create empty client_* collections with declared indexes (registry handles this on next boot).
4. Drop original collections only after a successful row-count assertion (operator_users.count == users.count).
5. Write a sentinel doc to system_init.user_split_migration with timestamp + counts.
6. Refuse to run twice (sentinel check).
```

The script is idempotent given the sentinel and re-runnable in dev. Production guardrail: `--confirm-prod=I-understand` flag required when `ENV=production`.

### Files touched (non-exhaustive)

| File | Change |
|---|---|
| `backend/internal/shared/module/module.go` | `RouteInfo` reshape; new `Audience`, `APISurface` |
| `backend/cmd/server/main.go` | Two `huma.API`s, `hostRouter`, audience middleware wiring |
| `backend/cmd/server/catalog.go` | No structural change; modules updated in PR-A |
| `backend/internal/shared/iface/user.go` | Split `UserProvider` → `OperatorUserProvider` + `ClientUserProvider` |
| `backend/internal/core/user/` | Tier-aware repos; two services |
| `backend/internal/core/auth/` | Tier-aware login flows; new path layout; JWT v2 issuance |
| `backend/internal/shared/middleware/jwt_validator.go` | `aud` claim required; mismatch ⇒ 401 |
| `backend/internal/addons/onboarding/` | Routes move to `Client` surface only |
| `backend/internal/addons/subscriptions/`, `payments/` | Routes move to `Client` surface only |
| `backend/internal/addons/billing/`, `documents/`, `company/`, `graph/`, `aimodels/`, `rag/`, `agents/`, `sales/`, `dev/` | Routes move to `Operator` surface only |
| `frontend/` | Operator dashboard base URL → `console.*`; client SPA(s) → `api.*` |
| `docker/.env.example`, `docker-compose.{dev,staging,prod}.yml` | New `CONSOLE_HOST`, `CLIENT_API_HOST`, per-audience CORS / rate limits |
| `docker/CLAUDE.md` | Document the host split + per-audience env vars |
| `scripts/devtoken.sh` | Add `--audience operator|client` flag |

### Backwards compatibility

None within the cutover. Pre-GA permits this. Post-cutover, the JWT `aud` claim is mandatory and the audience contract is locked — changes require a new ADR.

## Alternatives considered

1. **Path-based split on a single host (`/v1/operator/*` vs `/v1/client/*`).** Rejected: bundles OpenAPI, CORS, rate-limit, and WAF policy. Same JWT serves both surfaces. Already in use for `/v1/internal/*` (sidecar) — fine for service-to-service, insufficient for human-facing tier separation.

2. **Two separate Go binaries.** Rejected for now: doubles build/deploy/observability surface for negligible isolation gain over a host-mux in one binary. The `service` sidecar (`cmd/ai-service/`) is already a separate binary by necessity (different deployment topology); operator + client do not warrant the same. Revisit if independent release cadence becomes a real requirement.

3. **Single shared user collection with `tier` array.** Rejected per decision: cross-tier privilege escalation through DB tampering is one corrupted row away. Hard isolation at the collection level eliminates the class.

4. **Soft cutover with grace-period dual-validation (`v1`-without-`aud` accepted as `operator` for 7 days).** Rejected: pre-GA, dual-validation code paths are a long-tail bug source, and current user count makes coordination cheap.

5. **Per-tier OAuth callback URLs (e.g. `/v1/auth/operator/oauth/google/callback`).** Rejected: requires registering twice as many redirect URIs with each OAuth provider and complicates IdP config. Signed `state.tier` achieves the same dispatch with a single callback.

6. **Audience as an HTTP header (`X-Orkestra-Audience: client`) instead of host-based.** Rejected: trivially spoofable from a misconfigured client, and offers no DNS/CORS/WAF separation. The host *is* the trust boundary.
