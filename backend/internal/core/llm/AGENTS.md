# Module: LLM — credentials, models, grants and the gateway for language models

_Path: `/backend/internal/core/llm`_
_Parent: [../AGENTS.md](../AGENTS.md)_

[← Core](../AGENTS.md) | [☰ Backend](../../../AGENTS.md) | [Root](../../../../AGENTS.md)

## Purpose

Owns everything an installation needs to let its modules call a language model: the org-scoped **API-key credentials** (secrets sealed in an envelope), the **model catalog** (capabilities, purposes with priority, who pays), the per-user **grants** that decide who may use a model, and `iface.LLMGateway`, the only surface other modules use to call a model ([ADR-0022](../../../../docs/adr/0022-core-llm-module.md)). Tier 1 only: every row belongs to an **internal** organization.

Delivery is in three steps and this file documents what is built. **Built (foundations):** catalog, access resolver, envelope vault, admin and self routes, PIIProducer, late KMS/audit wiring, `Resolve` and `ListUsable`. **Not yet built:** the provider adapters (the registry is empty, so `Chat` and `Embed` validate, resolve access and end in `ErrLLMProviderUnavailable`), routing with fallback, the circuit breaker, the usage ledger and monthly budget, connectivity tests and model discovery, and Sign in with ChatGPT (`user_account` credentials). Do not document or depend on those as present.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration (`NewModule()` takes no arguments), `Collections()`, `ConfigSchema()`, `Permissions()`, `NavItems()`, `Init`, `RegisterRoutes`; builds the vault, catalog, access resolver, gateway and handlers |
| `routes.go` | Huma operations: `RegisterAdminReadRoutes`, `RegisterCredentialWriteRoutes`, `RegisterModelWriteRoutes`, `RegisterGrantRoutes`, `RegisterSelfRoutes` (one function per middleware group) |
| `handlers/admin_credentials.go`, `handlers/admin_models.go` | Admin HTTP ↔ `CatalogService`; request/response wrappers |
| `handlers/self.go` | `GET /v1/llm/me/models` over the same `iface.LLMGateway` addons use |
| `handlers/errors.go` | `MapError`: sentinels → stable `llm.*` codes with written (never `err.Error()`) details; `failure` logs server-side causes |
| `services/vault.go` | `Vault`: `Seal`/`Open` with KMS-first, local AES-256-GCM fallback; AAD binding; `SetKMSProvider` |
| `services/catalog.go` | `CatalogService`: credentials, models, grants (and access), the hosted/mock/endpoint gates (`providerGate`, shared with the resolver), audit emission, `SetAuditSink` |
| `services/endpoint.go` | `ValidateEndpoint` (SSRF string checks) and `IsPublicIP` (the check PR 2's dial-time guard must reuse) |
| `services/access.go` | `AccessResolver`: live models (active + `providerGate` under the live config + active org credential), usable models, candidates per purpose and capability need |
| `services/gateway.go` | `Gateway` implementing `iface.LLMGateway`, `iface.KMSProviderSetter`, `iface.AuditSinkSetter`; request limits |
| `services/pii_producer.go` | `PIIProducer` (subject `llm`) over the DSR repository slices |
| `services/errors.go` | Service sentinels |
| `repository/{credentials,models,grants}.go` | `tenantrepo`-scoped Mongo access, plus the DSR methods |
| `models/` | `Credential`, `Model`, `LLMGrant`, `Envelope`, DTOs/bodies, validation, `ErasedActor` |
| `providers/provider.go` | Internal adapter contract (`Provider`, `ChatCall`, `ChatOutcome`, `EmbedCall`, …) and an empty `Registry` |

## MongoDB collections

All three carry `tenantId` (the internal org) and are read and written only through `pkg/sdk/tenantrepo` (`Scope` on every query, `StampInsert` on every insert).

| Collection | Key fields | Indexes |
|---|---|---|
| `llm_credentials` | `uuid`, `tenantId`, `name`, `provider`, `baseUrl`, `secret` (envelope), `secretLast4`, `status`, `lastTestedAt`/`lastTestStatus`/`lastTestError` (reserved, not written yet), `createdBy`, timestamps | unique `(tenantId, name)`; unique `(tenantId, uuid)`; `(tenantId, provider)` |
| `llm_models` | `uuid`, `tenantId`, `name`, `provider`, `modelId`, `capabilities`, `credentialRef{kind, credentialUuid}`, `defaults`, `budgetReserveOutputTokens`, `purposes[]{purpose, priority}`, `access`, `status`, `createdBy`, timestamps | unique `(tenantId, name)`; unique `(tenantId, uuid)`; `(tenantId, status, purposes.purpose)`; `(tenantId, credentialRef.credentialUuid)` |
| `llm_grants` | `uuid`, `tenantId`, `modelUuid`, `userUuid`, `grantedBy`, `createdAt` | unique `(tenantId, modelUuid, userUuid)`; `(tenantId, userUuid)` |

The unique `(tenantId, name)` index backs `repository.ErrDuplicateName`; the unique grant triple makes a racing insert in `Grants.Replace` a tolerated duplicate.

**`//tenantscope:allow dsr:` — there are exactly eight**, all subject-scoped data-subject-rights queries that cross organizations, reached only from the compliance pipeline through `iface.PIIProducer`:

| Repository | Methods |
|---|---|
| `Grants` | `DeleteByUser`, `ListByUserAllTenants`, `ListByGranter`, `PseudonymizeGranter` |
| `Credentials` | `ListByCreator`, `PseudonymizeCreator` |
| `Models` | `ListByCreator`, `PseudonymizeCreator` |

Resolving the subject's organizations through memberships is not an option: the tenant producer runs first and deletes the memberships, which would orphan the grants (the same reasoning as the authz precedent). No other query, and no baseline entry, is exempt from `tenantscope`.

### Secret envelope

`{alg: "kms"|"local", keyId, keyVersion, schemaVersion, ciphertext}` (all `json:"-"`). `ciphertext` is base64 of the sealed bytes. The display tail lives only on the document as top-level `secretLast4` (set by `models.Last4`, empty below 8 characters), never inside the envelope. AAD is `tenantId|resourceUuid|field|schemaVersion`; `Seal`/`Open` refuse an empty part or one containing `|` (`ErrInvalidEnvelopeContext`). The algorithm is chosen at write time and `Open` dispatches on the stored `alg`. On the KMS path `iface.ErrKMSCiphertextInvalid` (truncated, tampered, foreign key) becomes `ErrEnvelopeCorrupt`; `iface.ErrKMSKeyDeleted` and infrastructure errors pass through unchanged.

## Dependencies

- **Modules** (`Dependencies()`): `user`, `tenant`, `notification`. (`notification` is reserved for the budget notifications of the next step.)
- **Required services**: `ServiceTenantDirectoryReader` (`iface.TenantDirectoryReader`) — `ReplaceGrants` uses `ListTenantMembers` to check that every grantee is a member. Init fails if it is absent.
- **Optional services**: `ServicePIIProducerRegistry` (the producer is registered when present).
- **Provides**: `ServiceLLMGateway` → `*services.Gateway`, which satisfies `iface.LLMGateway`, `iface.KMSProviderSetter` and `iface.AuditSinkSetter`.
- **Late-injected by compliance** (compliance initializes after this module and does not declare a dependency on it): the KMS provider and the audit sink arrive through the two setters. Until they do, audit emission is a no-op and secrets are sealed with the local key if one exists. `cmd/server/core_load_order_test.go` pins `logging → llm → compliance`; do not reorder the catalog.
- **Environment**: `LLM_SECRET_ENCRYPTION_KEY` (64 hex), read with `os.Getenv` in `Init`.
- Imports no other module's `services/` or `repository/`; everything cross-module goes through `pkg/sdk/iface`.

## Lifecycle

- **Init**: builds the vault from `LLM_SECRET_ENCRYPTION_KEY`. A **missing** key logs a WARN and leaves the vault without a local key; a **malformed** key logs a WARN and is treated as absent. Neither stops the boot (a core `Init` error is fatal, and an unconfigured optional capability must not be). The value is never logged. It then resolves the tenant directory, builds the three repositories, `CatalogService`, `Registry`, `AccessResolver` and `Gateway`, registers the gateway under `ServiceLLMGateway`, registers the PIIProducer, and builds the handlers.
- **`HotReloadConfig()`** is `true`. `allow_hosted` is read through `deps.GetConfigBool` on every catalog decision and every resolver read (one `cfg` accessor shared by `CatalogService` and `AccessResolver`). The other keys are declared for the next steps.
- **No `Start`/`Stop`** in this step (no background work yet).
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
| POST | `/v1/admin/llm/credentials` | `llm.credentials.admin` + step-up | 201 |
| PATCH | `/v1/admin/llm/credentials/{uuid}` | `llm.credentials.admin` + step-up | 200 |
| POST | `/v1/admin/llm/credentials/{uuid}/rotate` | `llm.credentials.admin` + step-up | 200 |
| DELETE | `/v1/admin/llm/credentials/{uuid}` | `llm.credentials.admin` + step-up | 204; 409 `llm.credential_in_use` |
| GET | `/v1/admin/llm/models` | `llm.admin.read` | 200 `{items}`, each with `grants` |
| GET | `/v1/admin/llm/models/{uuid}` | `llm.admin.read` | 200 |
| POST | `/v1/admin/llm/models` | `llm.models.admin` | 201; always `access: granted`, no grants (the body has no `access`) |
| PATCH | `/v1/admin/llm/models/{uuid}` | `llm.models.admin` | 200, **partial** (pointer fields; validated on the merged model); `{status}` enables/disables; no `access` field |
| DELETE | `/v1/admin/llm/models/{uuid}` | `llm.models.admin` | 204; deletes the model's grants first |
| PUT | `/v1/admin/llm/models/{uuid}/grants` | `llm.grants.admin` + step-up | body `{access, userUuids}` (`access` required: `granted`\|`everyone`); sets access and replaces the complete grant list together; 200 the model view with its grants |
| GET | `/v1/llm/me/models` | org permission `llm.models.self` | 200 `{items}` |

Step-up is `RequireStepUp(5 * time.Minute)`. Credential `PATCH` is included on purpose: re-pointing `baseUrl` sends the stored key to another host. Model writes have no step-up.

Error codes (`shared/errcode`, `llm.` prefix) and statuses: `invalid_request` 422, `not_found` 404, `name_in_use` 409, `credential_in_use` 409, `secret_key_missing` 503, `endpoint_not_allowed` 422, `hosted_disabled` 422, `mock_not_allowed` 422, `grant_not_member` 422, plus the gateway's `not_configured` 503 (FeatureNotConfigured pattern), `no_eligible_model` 403, `model_access_denied` 403, `capability_mismatch` 422, `provider_unavailable` 503. A wrong-tier tenant is `403` with no code; an unknown error is a generic `500` whose cause is logged, with no `llm.internal` code. Audit actions: `llm.credential.{created,updated,rotated,deleted}`, `llm.model.{created,updated,deleted}`, `llm.grants.replaced` (metadata `access`, `granted` count).

## RBAC

Three rules, no role name anywhere in the module:

1. Every `/v1/admin/llm/*` route uses `RequireSystemPermission(<key>)` on a `System: true` permission.
2. Mutations use a `.admin` key (Cedar's admin-suffix rule requires an enrolled second factor; a production `developer` never inherits it). Each mutation is gated by the permission of **its own resource** — `llm.credentials.admin`, `llm.models.admin`, `llm.grants.admin`.
3. Reads use `llm.admin.read`, which a `developer` keeps in production.

Only five permissions are declared, because the policy-coverage baseline rejects a declared permission with no route: `llm.admin.read`, `llm.credentials.admin`, `llm.models.admin`, `llm.grants.admin` (System) and `llm.models.self`. `authz/cedar/policies/llm.cedar` names the four System actions; `llm.models.self` is covered by the `self`-suffix rule. Later steps add their keys (usage, budgets, accounts) together with their routes and name them in `llm.cedar`.

The nav item `/admin/llm` has `MinRole: administrator`; the console additionally gates tabs on `llm.admin.read` and disables mutations without the matching `.admin` key.

## Gateway contract (`pkg/sdk/iface/llm.go`)

`Chat`, `Embed`, `Resolve`, `ListUsable`. Every entry point first requires an internal tenant. `ChatRequest.Caller` is required and `Purpose` defaults to `default`; the request limits are enforced before any resolution (`ValidateChatRequest`/`ValidateEmbedRequest`). Every rule below works on **live** models only: status `active`, `providerGate(cfg(), provider)` passes under the config read on this call (`allow_hosted`, no `mock` when production-like), and for `credentialRef.kind = org` the credential exists in the org and is `active` (`user_account` models are not credential-checked yet; an unknown kind is never live). A model that is not live is invisible to `Usable`/`ListUsable`, `Resolve`, `Candidates` and the purpose fallback, as if disabled. The routing rules in `AccessResolver.Candidates`:

1. no live model in the org → `ErrLLMNotConfigured`;
2. the purpose falls back to `default` **only if no live model of the org declares it**; if some model declares it but the caller may not use it or it lacks a needed capability, the result is empty (`ErrLLMNoEligibleModel`);
3. candidates are ordered by the purpose's priority (lower wins), then name;
4. a `ModelUUID` pin must be among the candidates (`ErrLLMModelAccessDenied` otherwise) — pinning never widens access.

## Invariants

- **Secrets never leak.** The API key is write-only; it appears in no response, log line, error message or audit metadata (audit metadata also never carries `baseUrl`). `Open` happens just-in-time via `CatalogService.OpenCredentialSecret`; the plaintext is not retained past the provider call.
- **The envelope is context-bound.** Always `Seal`/`Open` with the same `(tenantID, resourceUUID, "secret")`; the tenant must be the stored `TenantID`, not whatever is in a test fake's context.
- **Managing ≠ using.** Access is a grant or `access = everyone`; a model manager has no implicit use. Do not add a role-based shortcut.
- **Access changes only on the grants route.** `access` is not in `LLMModelBody`, `LLMModelPatchBody` or `ModelInput`; `CreateModel` always stores `granted`; `ReplaceGrants(ctx, model, access, userUUIDs)` validates access and membership first (a refusal writes nothing, access included), then sets both. With `access = everyone` the grants are kept but dormant and apply again when access returns to `granted`. `TestModelBodiesCarryNoAccess` pins the bodies.
- **`user_account` models** (reserved for the linked-account step) must use `openai`, cannot offer embeddings and cannot set temperature or max tokens; they are validated now.
- **Fallback never changes the payer** (ADR-0022 D12).
- **Hosted/mock gates apply on create, edit and re-enable** of credentials and models, and on every read through the resolver; disabling is always allowed.
- **A live model needs an active credential.** `CreateModel` and a `PatchModel` that leaves the model active refuse a disabled org credential (`llm.invalid_request`, sentinel `ErrCredentialDisabled`); a model that ends up disabled may keep pointing at one.
- **Model defaults are bounded like request options:** `defaults.temperature` 0–2 (NaN refused), `defaults.maxOutputTokens` 1–`models.MaxOutputTokens` (131072, the `budget_reserve_output_tokens` maximum).
- **Endpoints**: fixed vendor URLs for `openai`/`anthropic`/`gemini`; `ollama`/`openai_compatible` need a `baseUrl`; `mock` takes none. In production-like environments only public HTTPS is accepted. The string check is the first line only — PR 2's transport **must** re-validate every resolved address at dial time with `IsPublicIP`.
- **Data-subject rights.** Export uses explicit projections (never a raw model). Erasure deletes the grants the subject holds and rewrites `createdBy`/`grantedBy` to the fixed constant `models.ErasedActor` (`erased-subject`), not derived from the user. An empty subject is refused (`ErrEmptySubject`) before any query, in export and purge. Credentials, models and grants issued to others are org assets and stay.
- **Huma schema names are package-less**, and Huma panics on duplicates. Every HTTP-visible type here carries the `LLM` prefix (`LLMCredentialView`, `LLMModelBody`, `LLMGrant`, …) because `auth` already owns `CredentialView`. Methods on `Credential` and `Model` have pointer receivers for the same family of reasons (Huma's schema-link transformer cannot rebuild a struct embedding a type with a value method set).
- **Capabilities are persisted through `models.LLMModelCapabilities`** (explicit `bson` tags), not `iface.LLMCapabilities` (json-only, it would be stored lower-cased).
- **No cross-module imports.** Tenant membership comes through `iface.TenantDirectoryReader`; compliance reaches this module only through the registered service and its two setter interfaces.

## Known limitations

- Granting from the console also needs `system.tenants.admin`, because the member picker uses the tenant API; a member endpoint scoped to `llm` is a candidate for a later step.
- `lastTest*` credential fields exist but nothing writes them yet.

## Verification

```bash
cd backend && go test ./internal/core/llm/... ./pkg/sdk/iface/...
# Repository tests need a Mongo (they skip without MONGO_TEST_URI). Use a throwaway
# database on the DEV instance, never another stack's port:
MONGO_TEST_URI='mongodb://admin:<pw>@127.0.0.1:27018/?authSource=admin&directConnection=true' \
  go test ./internal/core/llm/repository/...
make ci-backend   # lint, tenantscope, policycoverage, piiscan, logscope, vuln, tests, build, openapi-check
```

When you add a route, re-run `make openapi-dump` from `backend/` (the spec is committed) and keep `llm.cedar` in step with `Permissions()`. When you change this module, update [the published page](../../../../docs/site/modules/core/llm.mdx) too.
