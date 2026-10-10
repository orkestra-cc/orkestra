# Module: LLM — credentials, models, grants and the gateway for language models

_Path: `/backend/internal/core/llm`_
_Parent: [../AGENTS.md](../AGENTS.md)_

[← Core](../AGENTS.md) | [☰ Backend](../../../AGENTS.md) | [Root](../../../../AGENTS.md)

## Purpose

Owns everything an installation needs to let its modules call a language model: the org-scoped **API-key credentials** (secrets sealed in an envelope), the **model catalog** (capabilities, purposes with priority, who pays), the per-user **grants** that decide who may use a model, and `iface.LLMGateway`, the only surface other modules use to call a model ([ADR-0022](../../../../docs/adr/0022-core-llm-module.md)). Tier 1 only: every row belongs to an **internal** organization.

Delivery is in three steps and this file documents what is built. **Built:** catalog, access resolver, envelope vault, admin and self routes, late KMS/audit wiring, `Resolve` and `ListUsable`; the six provider adapters, the router (budget reservation, breaker, same-kind fallback, ledger finalization, metrics), the usage ledger, monthly rollup and budget, budget alerts through a durable outbox, the reconciler, the retention purge, credential test, model discovery, the admin prompt test, the usage report and budget routes; the PIIProducer over grants, actor ids, usage rows, budgets and delivered alerts. **Not yet built:** Sign in with ChatGPT (`user_account` credentials answer `ErrLLMUserAccountRequired`). Do not document or depend on it as present.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration (`NewModule()` takes no arguments), `Collections()`, `ConfigSchema()`, `Permissions()`, `NavItems()`, `Init`, `RegisterRoutes`; builds the vault, catalog, access resolver, gateway and handlers |
| `routes.go` | Huma operations: `RegisterAdminReadRoutes`, `RegisterCredentialWriteRoutes`, `RegisterModelWriteRoutes`, `RegisterGrantRoutes`, `RegisterSelfRoutes`, `RegisterCredentialOpsRoutes`, `RegisterPromptRoute`, `RegisterUsageReadRoutes`, `RegisterBudgetReadRoutes`, `RegisterBudgetWriteRoutes`, `RegisterSelfUsageRoutes` (one function per middleware group) |
| `handlers/admin_ops.go`, `handlers/admin_usage.go`, `handlers/self_usage.go` | `OpsHandler` (credential test, model discovery, prompt test) over `services.AdminOps`; `UsageHandler` (usage report, budget GET/PUT, `me/usage`) over `services.UsageService` |
| `services/admin_ops.go` | `AdminOps`: live checks with the live-path provider gate (ruling 5.3), Redis rate limits, `probeCode` (closed `llm.*` set persisted as `lastTestError`) |
| `services/usage_query.go` | `UsageService`: admin report (`groupBy` model/user/caller, ≤366 days), self usage, budget get/put (extra recipients must be members) |
| `services/ratelimit.go` | `RateLimiter`: fixed-window Redis counter, window index in the key; increment + expiry are one Lua script when the store implements `Eval` (the backend adapter does), so no counter is ever left without expiry (ruling 5.10); fail-open on Redis errors; `*RateLimitedError{RetryAfter}` |
| `services/notify.go` | `BudgetNotifier` + `BudgetNotificationTemplates()` (`llm.budget_warning`, `llm.budget_exhausted`, en/it, Go `text/template`) |
| `handlers/admin_credentials.go`, `handlers/admin_models.go` | Admin HTTP ↔ `CatalogService`; request/response wrappers |
| `handlers/self.go` | `GET /v1/llm/me/models` over the same `iface.LLMGateway` addons use |
| `handlers/errors.go` | `MapError`: sentinels → stable `llm.*` codes with written (never `err.Error()`) details; `failure` logs server-side causes |
| `services/vault.go` | `Vault`: `Seal`/`Open` with KMS-first, local AES-256-GCM fallback; AAD binding; `SetKMSProvider` |
| `services/catalog.go` | `CatalogService`: credentials, models, grants (and access), the hosted/mock/endpoint gates (`providerGate`, shared with the resolver), audit emission, `SetAuditSink` |
| `services/endpoint.go` | `ValidateEndpoint` (SSRF string checks; delegates to `netguard`) |
| `netguard/` | Leaf package: `IsPublicIP` / `PublicHost`, shared by `ValidateEndpoint` and the providers dial-time guard (no import cycle) |
| `providers/transport.go` | `NewHTTPClient`: dial-time `netguard.IsPublicIP` on every resolved address in production-like mode, no proxy, no redirects, response cap on every response unless the request opted into streaming with `providers.WithStreaming(ctx)` (then idle timeout instead; never inferred from the response Content-Type); `sdkopts.go` builds SDK clients with explicit options only (no env leakage) |
| `services/access.go` | `AccessResolver`: live models (active + `providerGate` under the live config + active org credential), usable models, candidates per purpose and capability need |
| `services/gateway.go` | `Gateway` implementing `iface.LLMGateway`, `iface.KMSProviderSetter`, `iface.AuditSinkSetter`; request limits |
| `services/router.go`, `services/breaker.go`, `services/credentials_org.go` | `Router`: candidates → reservation → breaker → attempt → same-kind fallback → finalization (see «Routing»); `Breaker` per provider identity; `OrgCredentialResolver` opens an org credential just in time |
| `services/budget.go`, `services/estimate.go` | `BudgetService`: `Reserve`/`Finalize` over `BudgetStore` (repository), the conservative token estimate (`ceil(bytes/3)`) |
| `services/reconciler.go`, `services/outbox.go` | `Reconciler` (closes expired pending rows), `OutboxWorker` (leased, at-least-once alert delivery) |
| `services/pii_producer.go` | `PIIProducer` (subject `llm`) over the DSR repository slices |
| `services/errors.go` | Service sentinels |
| `repository/{credentials,models,grants}.go` | `tenantrepo`-scoped Mongo access, plus the DSR methods |
| `repository/{usage,budgets,outbox}.go` | Ledger + monthly rollup (`Usage`, `Monthly`, `WithTxn`), `Budgets`, `Outbox`, and `BudgetStore` (the transactions of admission and finalization); the system sweeps and DSR methods |
| `providers/{openai,openai_compatible,anthropic,gemini,ollama,mock}.go` | The six adapters (see «Providers»); `classify.go` maps provider failures onto the `iface.ErrLLM*` sentinels; `sse.go` the bounded event-stream decoder |
| `models/` | `Credential`, `Model`, `LLMGrant`, `Envelope`, DTOs/bodies, validation, `ErasedActor` |
| `providers/provider.go` | Internal adapter contract (`Provider`, `ChatCall`, `ChatOutcome`, `EmbedCall`, …) and an empty `Registry` |

## MongoDB collections

All seven carry `tenantId` (the internal org) and are read and written only through `pkg/sdk/tenantrepo` (`Scope` on every query, `StampInsert` on every insert), except the system sweeps and DSR methods listed below.

| Collection | Key fields | Indexes |
|---|---|---|
| `llm_credentials` | `uuid`, `tenantId`, `name`, `provider`, `baseUrl`, `secret` (envelope), `secretLast4`, `status`, `lastTestedAt`/`lastTestStatus`/`lastTestError` (written only by `Credentials.SetTestResult`), `createdBy`, timestamps | unique `(tenantId, name)`; unique `(tenantId, uuid)`; `(tenantId, provider)` |
| `llm_models` | `uuid`, `tenantId`, `name`, `provider`, `modelId`, `capabilities`, `credentialRef{kind, credentialUuid}`, `defaults`, `budgetReserveOutputTokens`, `purposes[]{purpose, priority}`, `access`, `grantsRevision` (internal, `json:"-"`), `status`, `createdBy`, timestamps | unique `(tenantId, name)`; unique `(tenantId, uuid)`; `(tenantId, status, purposes.purpose)`; `(tenantId, credentialRef.credentialUuid)` |
| `llm_grants` | `uuid`, `tenantId`, `modelUuid`, `userUuid`, `grantedBy`, `createdAt` | unique `(tenantId, modelUuid, userUuid)`; `(tenantId, userUuid)` |
| `llm_usage` | one row per logical call: `uuid`, `userUuid`, `modelUuid`, `provider`, `modelId`, `credentialKind`, `credentialUuid`, `caller`, `purpose`, token counters, `reservedTokens`/`reservedOutputTokens`/`reservationExpiresAt` (pending only), `latencyMs`, `outcome` (`pending\|ok\|fallback\|error\|budget_blocked`), `errorCode`, `fallback`, `attempts[]` (≤8, counters only), `createdAt`, `finalizedAt`. Never a prompt or an output | unique `(tenantId, uuid)`; `(tenantId, createdAt)`, `(tenantId, userUuid, createdAt)`, `(tenantId, modelUuid, createdAt)`; partial `(outcome, reservationExpiresAt)` on pending; `createdAt` |
| `llm_usage_monthly` | per org and UTC month: `period`, `requests`, org/user-account input/output counters, `reservedTokens`, `warnedAt`, `exhaustedAt` | unique `(tenantId, period)` |
| `llm_budgets` | `monthlyTokenLimit`, `warnPct`, `action` (`block\|warn_only`), `notifyExtraUserUuids[]`, `updatedBy`, `updatedAt`; no document = no budget | unique `tenantId` |
| `llm_budget_outbox` | `uuid`, `period`, `threshold` (`warn\|exhausted`), `payload` (counters only), `status` (`pending\|sending\|sent\|failed`), `attempts`, `nextAttemptAt`, `leaseUntil`, `deliveredTo[]` (user UUIDs), `createdAt`, `sentAt` | unique `(tenantId, period, threshold)`; unique `(tenantId, uuid)`; `(status, nextAttemptAt)` |

The unique `(tenantId, name)` index backs `repository.ErrDuplicateName`; the unique grant triple is a last line of defence (concurrent `Grants.Replace` calls are serialized by their transaction, so a duplicate is a real error and aborts it).

**`//tenantscope:allow system:` — there are exactly five**, all ticker jobs that find work across organizations and then handle each row under its own stamped `tenantId`:

| Method | Job |
|---|---|
| `Usage.ListPendingExpired` | reconciler (every minute): pending rows whose reservation expired |
| `Usage.PurgeBefore` | daily retention: finalized rows older than `usage_retention_days` |
| `Monthly.PurgeOlderThan` | daily retention: rollups older than 24 months |
| `Outbox.LeaseDue` | outbox worker (every 30s): the next due alert |
| `Outbox.PurgeSettledBefore` | daily retention: `sent`/`failed` alerts older than the ledger cutoff |

**`//tenantscope:allow dsr:` — there are exactly sixteen**, all subject-scoped data-subject-rights queries that cross organizations, reached only from the compliance pipeline through `iface.PIIProducer`:

| Repository | Methods |
|---|---|
| `Grants` | `DeleteByUser`, `ListByUserAllTenants`, `ListByGranter`, `PseudonymizeGranter` |
| `Credentials` | `ListByCreator`, `PseudonymizeCreator` |
| `Models` | `ListByCreator`, `PseudonymizeCreator` |
| `Usage` | `ListByUserAllTenants`, `DeleteByUser` (skips pending rows), `AnonymizeByUser` (one random replacement per row) |
| `Budgets` | `ListReferencingUser`, `RemoveRecipient`, `PseudonymizeUpdater` |
| `Outbox` | `ListDeliveredTo`, `RemoveDelivered` |

Resolving the subject's organizations through memberships is not an option: the tenant producer runs first and deletes the memberships, which would orphan the grants (the same reasoning as the authz precedent). No other query, and no baseline entry, is exempt from `tenantscope`. The usage DSR queries filter on `userUuid` alone (no index leads with it): a scan per erasure request, accepted because requests are rare and the ledger is bounded by retention.

### Secret envelope

`{alg: "kms"|"local", keyId, keyVersion, schemaVersion, ciphertext}` (all `json:"-"`). `ciphertext` is base64 of the sealed bytes. The display tail lives only on the document as top-level `secretLast4` (set by `models.Last4`, empty below 8 characters), never inside the envelope. AAD is `tenantId|resourceUuid|field|schemaVersion`; `Seal`/`Open` refuse an empty part or one containing `|` (`ErrInvalidEnvelopeContext`). The algorithm is chosen at write time and `Open` dispatches on the stored `alg`. On the KMS path `iface.ErrKMSCiphertextInvalid` (truncated, tampered, foreign key) becomes `ErrEnvelopeCorrupt`; `iface.ErrKMSKeyDeleted` and infrastructure errors pass through unchanged. On the KMS path `Seal` obtains the key with `CreateKey(tenantID)` and records its ID only in the envelope; the tenant row's `KMSKeyID` may stay empty (an org created before KMS was wired), and `tenant.PurgeTenant` resolves the key the same way so the crypto-shred still covers these secrets.

## Dependencies

- **Modules** (`Dependencies()`): `user`, `tenant`, `notification` (budget alerts).
- **Optional services**: `ServiceNotificationSender` (`iface.NotificationSender`) and `ServiceOperatorUserProvider` (`iface.UserProvider`, for the recipient's address and language) for budget alerts — without either, each alert is a logged no-op and the outbox row is marked sent; `deps.RedisAdapter` for the admin rate limits (absent → unlimited).
- **Required services**: `ServiceTenantDirectoryReader` (`iface.TenantDirectoryReader`) — `ReplaceGrants` uses `ListTenantMembers` to check that every grantee is a member. Init fails if it is absent.
- **Optional services**: `ServicePIIProducerRegistry` (the producer is registered when present).
- **Provides**: `ServiceLLMGateway` → `*services.Gateway`, which satisfies `iface.LLMGateway`, `iface.KMSProviderSetter` and `iface.AuditSinkSetter`.
- **Late-injected by compliance** (compliance initializes after this module and does not declare a dependency on it): the KMS provider and the audit sink arrive through the two setters. Until they do, audit emission is a no-op and secrets are sealed with the local key if one exists. `cmd/server/core_load_order_test.go` pins `logging → llm → compliance`; do not reorder the catalog.
- **Environment**: `LLM_SECRET_ENCRYPTION_KEY` (64 hex), read with `os.Getenv` in `Init`.
- Imports no other module's `services/` or `repository/`; everything cross-module goes through `pkg/sdk/iface`.

## Lifecycle

- **Init**: builds the vault from `LLM_SECRET_ENCRYPTION_KEY`. A **missing** key logs a WARN and leaves the vault without a local key; a **malformed** key logs a WARN and is treated as absent. Neither stops the boot (a core `Init` error is fatal, and an unconfigured optional capability must not be). The value is never logged. It then resolves the tenant directory, builds the three repositories, `CatalogService`, `Registry`, `AccessResolver` and `Gateway`, registers the gateway under `ServiceLLMGateway`, registers the PIIProducer, and builds the handlers.
- **`HotReloadConfig()`** is `true`. `allow_hosted` is read through `deps.GetConfigBool` on every catalog decision and every resolver read (one `cfg` accessor shared by `CatalogService` and `AccessResolver`). The other keys are read per call through `readSettings`, which **clamps** them (`clampSettings`): `request_timeout <= 0` → 60s, `usage_retention_days` 1–3650, `budget_warn_pct` 0–100, `budget_reserve_output_tokens` 1–131072. The reservation TTL is derived, never configured: `2 × request_timeout + 60s` — two full attempts (a fallback) plus finalization, always far above the router's finalization window, so a reservation never expires under a call that may still run.
- **Init** also registers the six adapters (`providers.RegisterAll`) with one SSRF-safe client per provider kind (`clientFor`: `Timeout 0`, default `HeaderTimeout` for non-stream responses, `IdleTimeout 30s` for requests marked `WithStreaming`), builds the ledger/rollup/budget/outbox repositories and `BudgetStore`, the `BudgetService`, the router (`NewOrgCredentialResolver`, breaker 3 faults / 30s, `metrics.Default()`) wired with `Gateway.SetRouter`, the notifier, outbox worker, reconciler and the retention job, and the ops/usage handlers.
- **`Start`/`Stop`** (logging-module pattern, `lifecycleMu`, idempotent): `Start` launches the outbox worker (30s), the reconciler (1 min) and the retention purge (first run after 1 min, then daily: `llm_usage` older than `usage_retention_days`, rollups older than 24 months, `sent`/`failed` outbox rows older than the usage cutoff); each sweep handles every row under the row's own tenant context (rulings 3.5/5.4). `Stop` cancels and waits.
- **`HealthCheck`** returns `nil` unconditionally: a missing key only disables secret writes (already warned once at Init), the rest of the module works.

### Configuration

| Key | Default | Env var |
|---|---|---|
| `allow_hosted` | `false` | `LLM_ALLOW_HOSTED` |
| `request_timeout` | `60s` | `LLM_REQUEST_TIMEOUT` |
| `usage_retention_days` | `90` (1–3650) | `LLM_USAGE_RETENTION_DAYS` |
| `budget_warn_pct` | `80` (0–100) | `LLM_BUDGET_WARN_PCT` |
| `budget_reserve_output_tokens` | `4096` (1–131072) | `LLM_BUDGET_RESERVE_OUTPUT_TOKENS` |

Groups: `providers`, `limits` ([ADR-0012](../../../../docs/adr/0012-module-config-group-contract.md)). The `siwc_*` keys do not exist yet.

## HTTP endpoints

Operator host. Every group runs `RequireInternalTenant` first; a tenant of another kind is refused with `403`.

| Method | Path | Gate | Status |
|---|---|---|---|
| GET | `/v1/admin/llm/credentials` | `llm.admin.read` | 200 `{items}` |
| GET | `/v1/admin/llm/credentials/{uuid}` | `llm.admin.read` | 200 |
| POST | `/v1/admin/llm/credentials` | `llm.credentials.admin` + MFA + step-up | 201 |
| PATCH | `/v1/admin/llm/credentials/{uuid}` | `llm.credentials.admin` + MFA + step-up | 200 |
| POST | `/v1/admin/llm/credentials/{uuid}/rotate` | `llm.credentials.admin` + MFA + step-up | 200 |
| DELETE | `/v1/admin/llm/credentials/{uuid}` | `llm.credentials.admin` + MFA + step-up | 204; 409 `llm.credential_in_use` |
| GET | `/v1/admin/llm/models` | `llm.admin.read` | 200 `{items}`, each with `grants` |
| GET | `/v1/admin/llm/models/{uuid}` | `llm.admin.read` | 200 |
| POST | `/v1/admin/llm/models` | `llm.models.admin` + MFA | 201; always `access: granted`, no grants (the body has no `access`) |
| PATCH | `/v1/admin/llm/models/{uuid}` | `llm.models.admin` + MFA | 200, **partial** (pointer fields; validated on the merged model); `{status}` enables/disables; no `access` field |
| DELETE | `/v1/admin/llm/models/{uuid}` | `llm.models.admin` + MFA | 204; deletes the model's grants first |
| PUT | `/v1/admin/llm/models/{uuid}/grants` | `llm.grants.admin` + MFA + step-up | body `{access, userUuids}` (`access` required: `granted`\|`everyone`); sets access and replaces the complete grant list together; 200 the model view with its grants |
| GET | `/v1/llm/me/models` | org permission `llm.models.self` | 200 `{items}` |
| POST | `/v1/admin/llm/credentials/{uuid}/test` | `llm.credentials.admin` + MFA | 200 `{outcome: ok\|error, code?, testedAt}`; records `lastTestedAt`/`lastTestStatus`/`lastTestError` (an `llm.*` code, never provider text; fenced on the tested secret and base URL, so a rotation or re-point racing the test keeps its cleared state); audit `llm.credential.tested`; 10/min per org+credential |
| POST | `/v1/admin/llm/credentials/{uuid}/models` | `llm.credentials.admin` + MFA | 200 `{items: [{id, displayName}]}` (≤1000); nothing persisted; audit `llm.credential.models_discovered`; 10/min per org+credential |
| POST | `/v1/admin/llm/models/{uuid}/prompt` | `llm.models.admin` + MFA | body `{prompt}` (1–4000 characters, counted in runes like the schema's `maxLength`); 200 `{text, truncated?, usage, finishReason, fallback}` (≤64 KiB collected); skips grants only — a non-live model is refused like on the live path; reserved and ledgered with caller `llm.admin_test` and the admin's user UUID; 10/min per org+user |
| GET | `/v1/admin/llm/usage` | `llm.usage.read` | `from`, `to` (RFC 3339, default last 30 days, ≤366 days), `groupBy=model\|user\|caller`; 200 `{from, to, groupBy, rows, month, budget?}` — **`llm.usage.read` therefore also reads the budget** (both read-only System permissions; the console may take the budget from the report) |
| GET | `/v1/admin/llm/budget` | `llm.admin.read` | 200 the budget, **204** when the org has none (no limit) |
| PUT | `/v1/admin/llm/budget` | `llm.budgets.admin` + MFA + step-up | body `{monthlyTokenLimit ≥ 1, warnPct? 0–100 (default budget_warn_pct), action block\|warn_only, notifyExtraUserUuids[] ≤ 50, all members}`; audit `llm.budget.updated` |
| GET | `/v1/llm/me/usage` | org permission `llm.usage.self` | 200 the user's month: `{period, requests, orgInputTokens, orgOutputTokens, userAccountInputTokens, userAccountOutputTokens}` |

Step-up is `RequireStepUp(5 * time.Minute)`. Credential `PATCH` is included on purpose: re-pointing `baseUrl` sends the stored key to another host. Model writes have no step-up. **Every write group carries `RequireMFA()`** (the authz precedent): with `CEDAR_ENFORCE_ACTIONS` unset the role table decides and the Cedar admin-suffix refusal is only logged, so the route gate is what keeps a password-only administrator (one in the MFA enrollment grace period included) out; on the credential and grant groups it sits before the step-up because step-up alone accepts a password reconfirm from an operator with no enrolled factor whose role does not require one. `TestModule_RouteGuards` pins the order.

Error codes (`shared/errcode`, `llm.` prefix) and statuses: `budget_exceeded` 429 (`Retry-After: 3600`), `rate_limited` 429 (`Retry-After` = rest of the window), `plan_quota_exhausted` 429, `credential_rejected` 422 (provider 401/403; mapped before `provider_unavailable`, which it also wraps), `partial_output` 502, `request_canceled` 499 (the caller hung up; quiet, never a logged 5xx), `user_account_required` / `user_account_reauth_required` / `plan_usage_disabled` 409, `invalid_request` 422, `not_found` 404, `name_in_use` 409, `credential_in_use` 409, `secret_key_missing` 503, `endpoint_not_allowed` 422, `hosted_disabled` 422, `mock_not_allowed` 422, `grant_not_member` 422, plus the gateway's `not_configured` 503 (FeatureNotConfigured pattern), `no_eligible_model` 403, `model_access_denied` 403, `capability_mismatch` 422, `provider_unavailable` 503. A wrong-tier tenant is `403` with no code; an unknown error is a generic `500` whose cause is logged, with no `llm.internal` code. Audit actions: `llm.credential.{created,updated,rotated,deleted}`, `llm.model.{created,updated,deleted}`, `llm.grants.replaced` (metadata `access`, `granted` count).

## RBAC

Three rules, no role name anywhere in the module:

1. Every `/v1/admin/llm/*` route uses `RequireSystemPermission(<key>)` on a `System: true` permission.
2. Mutations use a `.admin` key (Cedar's admin-suffix rule requires an enrolled second factor; a production `developer` never inherits it). Each mutation is gated by the permission of **its own resource** — `llm.credentials.admin`, `llm.models.admin`, `llm.grants.admin`.
3. Reads use `llm.admin.read`, which a `developer` keeps in production.

Only permissions with a route are declared, because the policy-coverage baseline rejects a declared permission with no route: `llm.admin.read`, `llm.credentials.admin`, `llm.models.admin`, `llm.grants.admin`, `llm.usage.read`, `llm.budgets.admin` (System) and `llm.models.self`, `llm.usage.self` (org). `llm.usage.read` implies reading the budget, which the usage report embeds. `authz/cedar/policies/llm.cedar` names the six System actions (`llm.usage.read` with the platform-roles read permit, `llm.budgets.admin` with the mutations permit); the two `self` keys are covered by the `self`-suffix rule. The linked-account keys arrive with their routes in PR 3. `TestModule_Declarations` pins 6 + 2.

The nav item `/admin/llm` has `MinRole: administrator`; the console additionally gates tabs on `llm.admin.read` and disables mutations without the matching `.admin` key.

## Gateway contract (`pkg/sdk/iface/llm.go`)

`Chat`, `Embed`, `Resolve`, `ListUsable`. Every entry point first requires an internal tenant. `ChatRequest.Caller` is required and `Purpose` defaults to `default`; the request limits are enforced before any resolution (`ValidateChatRequest`/`ValidateEmbedRequest`). Every rule below works on **live** models only: status `active`, `providerGate(cfg(), provider)` passes under the config read on this call (`allow_hosted`, no `mock` when production-like), and for `credentialRef.kind = org` the credential exists in the org and is `active` (`user_account` models are not credential-checked yet; an unknown kind is never live). A model that is not live is invisible to `Usable`/`ListUsable`, `Resolve`, `Candidates` and the purpose fallback, as if disabled. The routing rules in `AccessResolver.Candidates`:

1. no live model in the org → `ErrLLMNotConfigured`;
2. the purpose falls back to `default` **only if no live model of the org declares it**; if some model declares it but the caller may not use it or it lacks a needed capability, the result is empty (`ErrLLMNoEligibleModel`);
3. candidates are ordered by the purpose's priority (lower wins), then name;
4. a `ModelUUID` pin must be among the candidates (`ErrLLMModelAccessDenied` otherwise) — pinning never widens access.

## Routing

`Router.run` for one logical `Chat`/`Embed`:

1. **Candidates** from `AccessResolver.Candidates` (live models, purpose, capabilities, pin).
2. **Reservation**: `BudgetService.Reserve` inserts the `pending` ledger row and reserves estimated input + reserved output against the month (see «Budget semantics»). A refusal is a `budget_blocked` row and `ErrLLMBudgetExceeded`, before any provider call. The reservation TTL is `2 × request_timeout + 60s`; the call must end by `TTL − 5s` (the finalization window), so a reservation never expires under a running call, and an attempt that would not fit is not started.
3. **Breaker** per `provider|baseUrl|modelId|credentialUuid` (3 consecutive provider faults → open 30s, then a single half-open probe; `Release` frees a probe that ended without a verdict). Only provider faults count (ruling 5.2): unavailability, provider timeouts, 5xx, rejected credentials, exhausted quota — never a caller cancel, a deadline the caller lowered below `request_timeout`, an invalid request, a capability gap, the budget or the linked account.
4. **Attempt** with the provider adapter under `min(request_timeout, caller deadline)`.
5. **Fallback** to the next candidate **with the same `credentialKind`** (D12: the payer never changes) unless the failure is final (`fallbackable`): a caller cancel or a deadline (an attempt that ran out its own deadline included), the budget, the linked-account errors, an exhausted quota (and a circuit it opened), an invalid request and partial output are final. An unavailable provider, a rejected credential, an adapter that cannot be built and an open circuit — one opened by timeouts included — move on; a capability gap skips the candidate. At most 8 attempts are ledgered.
6. **Finalization** on `context.WithoutCancel` with a 5s bound: the row's outcome (`ok`, `fallback`, `error`), attempts, real tokens and the model that answered; the rollup releases the reservation and adds the tokens to the payer's counters; thresholds crossed enqueue alerts. Metrics `orkestra_llm_requests_total{provider,outcome}` and `orkestra_llm_tokens_total{provider,direction}`.
7. **Reconciler** closes any `pending` row whose owner died after its TTL, charging the whole reservation (outcome `error`, code `reconciled_timeout`, through `Finalize`; a late router finalize then gets `ErrAlreadyFinalized` and never counts twice).

## Providers

All adapters go through `providers.NewHTTPClient` (dial-time `netguard`, no proxy, no redirects, bounded bodies, SDK retries off) and never surface provider text: failures are reduced to allowlisted identifiers and classified onto the `iface.ErrLLM*` sentinels.

| Provider | Implementation | Not supported |
|---|---|---|
| `openai` | `github.com/openai/openai-go` (Responses API) | embeddings on the `user_account` profile (PR 3) |
| `anthropic` | `github.com/anthropics/anthropic-sdk-go` (Messages API) | embeddings (no endpoint) |
| `gemini` | `google.golang.org/genai` (Developer API) | — (thought signatures travel in `ChatToolCall.Signature`; the bypass fallback is unverified live) |
| `ollama` | hand-written, native `/api/chat` (ndjson), `/api/embed`, `/api/tags` | — (key optional, sent as Bearer for proxied deployments) |
| `openai_compatible` | hand-written Chat Completions (SSE), `/embeddings`, `/models` | — (key optional, for keyless vLLM / llama.cpp; the catalog still decides whether a credential needs one) |
| `mock` | deterministic echo / tool call / hash embedding | never selectable in production-like environments |

After any SDK bump run `make backend-vulncheck`: the three SDKs pull transitive dependencies (`go.yaml.in/yaml/v4` among them).

## Operational

- **Tickers** (`Start`): outbox worker every 30s (lease 2 min, backoff 1 → 64 min, 20 attempts then `failed`), reconciler every minute, retention first after 1 min then daily. All stop with `Stop`.
- **Metrics** (ADR-0002): `orkestra_llm_requests_total{provider,outcome}` (`ok\|fallback\|error\|budget_blocked`) and `orkestra_llm_tokens_total{provider,direction}` (`input\|output`, as ledgered, partial-output charges included). Labels are collapsed to closed sets.
- **Notification templates**: `llm.budget_warning`, `llm.budget_exhausted` (en/it, category `llm.budget`, transactional). With the notification module in `noop` the rendered alert is logged on the backend.
- **Admin ops limits**: credential test and model discovery 10/min per org + credential, prompt test 10/min per org + user (`llm.rate_limited`, `Retry-After`).

## Budget semantics

- **Admission, not a kill switch.** A call reserves estimated input + reserved output in one transaction that admits it only if `consumed + reserved + requested <= limit`; real usage can exceed the reservation (a long answer). Under `warn_only` it is admitted anyway; an unknown action blocks (fail closed).
- **Exhausted alert (ruling 4.1).** Under `block` consumption never reaches 100%, so the `exhausted` alert fires on the **first refused reservation** of the period (`budget_blocked` row + `exhaustedAt` mark + outbox row in the refusal's transaction), or when finalized consumption reaches the limit; either way one per org and period (the rollup mark, backed by the unique `(tenantId, period, threshold)` outbox index). The email says a call was refused (block) or that calls continue (warn_only).
- **Partial output (ruling 5.5).** An attempt that already streamed output to the caller and then failed is never retried on another model (`llm.partial_output`) and is charged conservatively: the larger of the reported usage and the reservation's share (estimated input; reserved output, or the emitted bytes / 3 if more). An orphan the reconciler closes is charged its whole reservation.
- **Per-org serialization.** Every reservation and every finalization of one org writes the same monthly rollup document inside a transaction, so concurrent calls of one org serialize through write-conflict retries (the driver retries for up to 120s). Correct by design, but it bounds one org's call throughput to what one document's transactions sustain; orgs do not contend with each other.
- **Alerts.** Recipients: members with `IsOwner`, role `org_owner` or `org_admin` (notification targeting, not authorization — ruling 4.12), plus `notifyExtraUserUuids` still in the org. One `SendTemplated` per recipient (`Type transactional`, `Category llm.budget`, locale from the user's language), each with its own 15s timeout so one slow mail server costs only its recipient. **Progress is per recipient**: each delivery is recorded on the outbox row (`deliveredTo`, `$addToSet` fenced by the worker's lease) and a retry sends only to the recipients still missing, however late it runs. The idempotency key is attempt-scoped, `llm-budget-<eventUuid>-<recipientUuid>-<attempts>`: the notification module replays a key's cached result for an hour, failures included, so reusing one key across retries would replay the failure instead of sending; within one attempt (a worker that crashed between a send and recording it, re-leased without an attempt increment) the same key replays the delivery instead of duplicating it. A delivery whose record fails may be sent twice (at-least-once). The admin prompt-test path that skips grants is not on the registered `Gateway`: `services.NewPinnedRunner(gateway)` hands it to `AdminOps` only.

## Invariants

- **Secrets never leak.** The API key is write-only; it appears in no response, log line, error message or audit metadata (audit metadata also never carries `baseUrl`). `Open` happens just-in-time via `CatalogService.OpenCredentialSecret`; the plaintext is not retained past the provider call.
- **The envelope is context-bound.** Always `Seal`/`Open` with the same `(tenantID, resourceUUID, "secret")`; the tenant must be the stored `TenantID`, not whatever is in a test fake's context.
- **Managing ≠ using.** Access is a grant or `access = everyone`; a model manager has no implicit use. Do not add a role-based shortcut.
- **Access changes only on the grants route.** `access` is not in `LLMModelBody`, `LLMModelPatchBody` or `ModelInput`; `CreateModel` always stores `granted`; `ReplaceGrants(ctx, model, access, userUUIDs)` validates access and membership first (a refusal writes nothing, access included), then sets both in **one Mongo transaction** (`Grants.Replace(ctx, model, access, actor, users)`): it first writes the model document — `$set {access, updatedAt}` + `$inc {grantsRevision: 1}` — then deletes and inserts the grant rows. Because every replacement of a model writes that same document, concurrent replacements write-conflict and `WithTransaction` retries the loser after the winner commits: the end state is always exactly one request (its access and its list), never a union, and a failure writes nothing. Access is always written, never skipped on a value read before the transaction. `Grants.Replace` is the only writer of `access`; `Models.Update` (used by `PatchModel`) is a targeted `$set` of every editable field **except** `access` and `grantsRevision`, so a patch and a grants change cannot revert each other. A replica set is required (as everywhere in Orkestra). With `access = everyone` the grants are kept but dormant and apply again when access returns to `granted`. `TestModelBodiesCarryNoAccess` pins the bodies.
- **Credential writes are field-owned.** `Credentials.Patch` (used by `PatchCredential`) is a targeted `$set` of only the fields the request provided (`name`, `baseUrl` — `$unset` when the provided value is empty —, `status`) plus `updatedAt`, so a rename built on a stale read cannot revert a concurrent disable or endpoint change; both `Patch` and `SetSecret` return the `updatedAt` they wrote, which the service reports back; `Credentials.SetSecret` (used by `RotateCredential`) is the only writer of `secret` and `secretLast4` (and clears `lastTest*`); `Credentials.SetTestResult` is the only writer of `lastTest*` (no `updatedAt` bump) and writes only while the stored ciphertext and base URL are the ones tested. Neither ever writes `createdBy`, `createdAt`, `tenantId` or `uuid`, so a rename racing a rotation keeps the new secret, a stale rotation cannot undo a disable, and no write restores a creator the DSR pipeline pseudonymized. Do not go back to `ReplaceOne` of a loaded document.
- **`user_account` models** (reserved for the linked-account step) must use `openai`, cannot offer embeddings and cannot set temperature or max tokens; they are validated now.
- **Fallback never changes the payer** (ADR-0022 D12).
- **Hosted/mock gates apply on create, edit and re-enable** of credentials and models, and on every read through the resolver; disabling is always allowed.
- **A live model needs an active credential.** `CreateModel` and a `PatchModel` that leaves the model active refuse a disabled org credential (`llm.invalid_request`, sentinel `ErrCredentialDisabled`); a model that ends up disabled may keep pointing at one.
- **Model defaults are bounded like request options:** `defaults.temperature` 0–2 (NaN refused), `defaults.maxOutputTokens` 1–`models.MaxOutputTokens` (131072, the `budget_reserve_output_tokens` maximum). The per-model override `budgetReserveOutputTokens` is stored as 1–131072 or absent: on create, absent or `0` means no override; on `PATCH`, absent keeps the stored value and `0` clears it (back to the module default) — a pointer field cannot tell absent from `null`, so `0` is the clear value and both schemas accept `minimum: 0` (`models.budgetOverride`).
- **Endpoints**: fixed vendor URLs for `openai`/`anthropic`/`gemini`; `ollama`/`openai_compatible` need a `baseUrl`; `mock` takes none. In production-like environments only public HTTPS is accepted. The string check is the first line only — `providers.NewHTTPClient` re-validates every resolved address at dial time with `netguard.IsPublicIP`; adapters must build their SDK clients through `providers.OpenAIClientOptions` / `AnthropicClientOptions` / `GeminiClientConfig`.
- **Tool-call signatures**: `iface.ChatToolCall.Signature` is opaque provider state (Gemini 3 thought signatures, base64, ≤64 KiB) that the producing adapter sets on output and echoes on replay; never parse, log or show it, and other adapters ignore it. An unsigned first call of a replayed step in the current turn gets Google's documented bypass value (unverified against the live API).
- **Data-subject rights.** Export uses explicit projections (never a raw model): grants held and issued, credentials and models created (references only), usage rows (counters, codes, model, purpose, caller, attempts, dates — never a prompt, `credentialUuid` or reservation fields), budgets naming the subject (as extra recipient or `updatedBy`) and the alerts delivered to them. Erasure deletes the grants the subject holds; rewrites `createdBy`/`grantedBy`/budget `updatedBy` to the fixed constant `models.ErasedActor` (`erased-subject`), not derived from the user; removes the subject from `notifyExtraUserUuids` and `deliveredTo`. Usage rows: `EraseHardDelete` deletes the finalized ones; `EraseAnonymize` keeps them with `userUuid` replaced and `credentialUuid` removed; pending rows (their reservation is still in the rollup) are anonymized in both modes and left to retention. An empty subject is refused (`ErrEmptySubject`) before any query, in export and purge. Credentials, models, budgets and grants issued to others are org assets and stay.
- **Usage anonymization is per row (ruling 5.8).** Each kept row gets its own `erased-<random uuid>`, never one pseudonym per subject: a per-subject value would keep the rows linkable into a behavioural profile (times, models, callers, purposes) that, in a small org and joined with other records, can single the person out. Per row, each is one isolated event with counters; the residual risk (matching one row's timestamp against an external log) reveals one call's token counts and ends with retention. The cost is that anonymized rows appear as separate keys in the by-user report. Do not switch to a per-subject value without a new documented assessment in the spec.
- **Huma schema names are package-less**, and Huma panics on duplicates. Every HTTP-visible type here carries the `LLM` prefix (`LLMCredentialView`, `LLMModelBody`, `LLMGrant`, …) because `auth` already owns `CredentialView`. Methods on `Credential` and `Model` have pointer receivers for the same family of reasons (Huma's schema-link transformer cannot rebuild a struct embedding a type with a value method set).
- **Capabilities are persisted through `models.LLMModelCapabilities`** (explicit `bson` tags), not `iface.LLMCapabilities` (json-only, it would be stored lower-cased).
- **No cross-module imports.** Tenant membership comes through `iface.TenantDirectoryReader`; compliance reaches this module only through the registered service and its two setter interfaces.

## Known limitations

- Granting from the console, and choosing extra budget recipients, also need `system.tenants.admin`, because both member pickers use the tenant API; a member endpoint scoped to `llm` is a candidate for a later step.
- The usage report grouped by user shows user UUIDs, not names (and anonymized rows as `erased-…` keys).
- The admin rate limits need Redis; without `deps.RedisAdapter` (or during a Redis outage) they allow every call (fail open, as auth does).

## Verification

```bash
cd backend && go test ./internal/core/llm/... ./pkg/sdk/iface/...
# Repository tests need a Mongo (they skip without MONGO_TEST_URI). Use a throwaway
# database on the DEV instance, never another stack's port:
MONGO_TEST_URI='mongodb://admin:<pw>@127.0.0.1:27018/?authSource=admin&directConnection=true' \
  go test ./internal/core/llm/repository/...
make ci-backend   # lint, tenantscope, policycoverage, piiscan, logscope, vuln, tests, build, openapi-check
make backend-vulncheck   # after every provider SDK bump
```

The budget tests (`repository/budget_txn_test.go`, `services/budget_mongo_test.go`) exercise real multi-document transactions: they need `MONGO_TEST_URI` pointing at a replica set and skip without it, so a green run without the variable proves nothing about admission or finalization.

When you add a route, re-run `make openapi-dump` from `backend/` (the spec is committed) and keep `llm.cedar` in step with `Permissions()`. When you change this module, update [the published page](../../../../docs/site/modules/core/llm.mdx) too.
