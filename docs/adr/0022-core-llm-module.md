---
title: ADR-0022 — Core `llm` module for language-model credentials, models, grants, budget and gateway
status: accepted
public: true
---

# ADR-0022 — Core `llm` module for language-model credentials, models, grants, budget and gateway

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — delivered in three steps: foundations (credentials, models, grants, encrypted secrets, data-subject rights) first; the gateway (provider adapters, routing, usage ledger, budget) and Sign in with ChatGPT follow |
| **Date** | 2026-10-10 |
| **Amends** | [ADR-0006](0006-collapse-to-core-only-base.md) — the core-only base gains a **ninth** core module. Access to a language model is horizontal plumbing every product rebuilds (whose key, who pays, who may use which model, how much), not a domain vertical. It passes the same "is this a domain vertical?" test [ADR-0009](0009-core-compliance-module.md) applied to compliance. |
| **Uses** | [ADR-0001](0001-unified-tenant-model.md) (every row is scoped to an internal organization through `tenantrepo`), [ADR-0003](0003-three-audience-host-split.md) (the admin and self surfaces live on the operator host), [ADR-0009](0009-core-compliance-module.md) (per-tenant KMS, audit sink and the data-subject-rights pipeline the module plugs into), [ADR-0012](0012-module-config-group-contract.md) (settings are declared as config groups) |

## Context

Every product built on Orkestra that calls a language model ends up rebuilding the same four things: where the API key lives, which models exist, who is allowed to use them, and how much they may spend. When each consuming module keeps its own provider layer, each of them re-implements key storage, endpoint validation, retry and circuit breaking, quota classification and a hosted-provider opt-in, and none of them can enforce a budget that spans the organization.

The SDK already carried provider interfaces from an earlier, global (not tenant-scoped) model registry, but nothing implemented them in the base. They were unused and tied to a different shape of the problem.

Access through a consumer subscription changes the shape further. The position as of this decision:

- **Anthropic.** No supported way for a third-party backend to use a Claude consumer plan is documented by Anthropic. The module does not support consumer-plan access for Anthropic; the supported path is an API key.
- **Google.** Google documents the use of the Gemini CLI OAuth client by other applications as a violation of its terms, and has deprecated consumer access to Code Assist. The supported path is an API key (AI Studio or Vertex).
- **OpenAI.** "Sign in with ChatGPT" is an official program for open-source and locally hosted applications: OpenID Connect with PKCE and a loopback callback on `127.0.0.1`. For remotely hosted or paid applications OpenAI points to an interest form instead.

Two credential scopes follow. An **API key belongs to an organization** and any authorized user of that organization can spend it. A **linked ChatGPT account belongs to one user**: it is that user's plan, and it may only serve that user's own requests.

## Decision

Add a core module `internal/core/llm/` that owns credentials, the model catalog, access grants, routing, usage and budget, and executes the calls itself. Consumers ask for a *purpose*; they never see a credential or a provider SDK. Acceptance test: *with the module present, an administrator registers a key, defines a model and grants it to a user, and that user (and only that user) can resolve it through `iface.LLMGateway`; no consumer imports a provider package.*

### D1 — Core, always-on (amends ADR-0006)

Registered in `coreModules()` after `logging` and before `compliance`; `Category()` is `CategoryCore`, so an Init failure is fatal. Compliance does not declare a dependency on it (the gateway is optional there), so the position in the catalog is the contract, and a test pins it.

### D2 — Tier 1 only

Every row is scoped to an **internal** organization through `tenantrepo`, and every route and every gateway entry point refuses a tenant of another kind. Bring-your-own-key for external client tenants is a possible extension that needs no change of data model; it is not part of this decision.

### D3 — Two credential scopes

An **organization API key** (`openai`, `anthropic`, `gemini`, `ollama`, `openai_compatible`) and a **per-user linked ChatGPT account** (`openai` only, through Sign in with ChatGPT). A model names which one pays through its `credentialRef`.

### D4 — The module executes the calls

The module is a gateway, not a registry that hands out clients. Budget, access and the rules of a subscription are applied in exactly one place. Rejected: a registry plus a client factory (a budget cannot be enforced centrally when callers hold the client), and an external proxy container (an always-on container in a core module, secrets kept outside the database and KMS, and per-user OAuth does not fit a shared proxy).

### D5 — Managing a model is not using it

Defining credentials, models, grants and budget is an administrative surface. Every `/v1/admin/llm/*` route uses `RequireSystemPermission` on an `llm.*` permission declared `System: true`, so only the platform system roles inherit them; no role name appears in the code. **Mutations** carry the `.admin` suffix, which the policy engine requires an enrolled second factor for and which the `developer` role does not inherit in production; **reads** carry `.read`, so a production developer still sees the configuration. **Using** a model requires an explicit per-user grant or `access = everyone`; being allowed to manage a model never grants its use.

### D6 — Usage ledger and monthly budget

Every call writes a ledger row, and each organization can have a monthly token budget applied to the sum of consumption on organization keys and on linked accounts. The cap is an admission control: consumption of requests already admitted may exceed the declared reservation.

### D7 — Providers

`openai`, `anthropic`, `gemini`, `ollama`, `openai_compatible`, plus `mock`, which can only be selected outside production-like environments. There is never a silent mock: with no configured model the gateway answers `ErrLLMNotConfigured`.

### D8 — Secrets are sealed in an envelope

Secrets are stored in an authenticated envelope bound to its context. When the compliance module has injected a KMS provider, the per-tenant key seals the secret and a tenant purge crypto-shreds it. Otherwise AES-256-GCM is used with a **dedicated** key, `LLM_SECRET_ENCRYPTION_KEY` (32 bytes, 64 hex characters), kept separate from the platform keys so rotating one never invalidates the other. The associated data is `tenantId|resourceUuid|field|schemaVersion`, so an envelope copied to another tenant, record or field fails to open. Secrets are write-only over the API, decrypted only at the moment a provider request is built, and never reach logs, responses or errors. A missing or malformed local key never stops the boot: secret writes are refused with `llm.secret_key_missing` until a key or a KMS exists, and the deployment tooling is the hard gate for staging and production.

### D9 — `LLMGateway` replaces the old provider interfaces

`iface.LLMGateway` and `module.ServiceLLMGateway` replace the unused `AIModelProvider` family (`AIModelProvider`, `LLMProvider`, `EmbeddingProvider`, `LLMProviderWithUsage`, the `Batch*` types, `LLMConfig`, `CompletionOptions`, `StreamChunk`) and `module.ServiceAIModelProvider`. The new contract is message-based and purpose-routed: `Chat`, `Embed`, `Resolve`, `ListUsable`, with `ErrLLM*` sentinels consumers map to their own codes.

### D10 — Several ChatGPT registrations per user

A user may keep several separate registrations (account or workspace) in the same organization, told apart by the client id OpenAI issues for each; exactly one is selected per organization, user and provider. An email address or a subject does not identify a workspace. An administrator sees only the registrations of the organization they are acting in.

### D11 — PKCE helper in `shared/utils`; the module has its own OIDC state store

The four-function PKCE helper moves from the auth module to `internal/shared/utils`, and the module keeps its own Redis store for the OIDC state. No module imports another's internals.

### D12 — Fallback never changes who pays

By default the fallback chain stays within the same credential kind. A quota or plan exhaustion does not fall back to a different payer. A future cross-billing fallback would require an explicit policy, a fresh step-up and a UI that shows who pays; it is not part of this decision.

### D13 — Sign in with ChatGPT through a loopback callback, automatic or pasted

`siwc_mode` is `disabled`, `loopback_local` or `hosted_paste`, and defaults to `disabled`. The `redirect_uri` is always `http://127.0.0.1:<port>/v1/llm/accounts/openai/callback`, as the official guide requires: never `localhost`, never a remote host, never a transfer of tokens. In `loopback_local` the callback reaches the local API listener. In `hosted_paste` nobody listens on that port: the browser shows an error page whose address bar holds the full callback URL, the user pastes it into the console, and an authenticated endpoint completes the exchange. The authorization code is always exchanged on the server, by the installation that will actually use the tokens.

The `hosted_paste` reading is stated plainly: it follows the technical contract of the guide to the letter, but OpenAI's overview directs applications that are remotely hosted to an interest form. Using it on a hosted installation is therefore a **terms-of-service grey zone**. It is off by default, flagged in the settings rail, and an OpenAI-approved hosted or partner redirect is left to a separate decision. Enabling either mode implies `allow_hosted`.

## Consequences

- **Load order** becomes `user → notification → tenant → authz → auth → navigation → logging → llm → compliance`. The module declares `user`, `tenant` and `notification` as dependencies and requires the tenant directory reader at runtime to check that every grantee is a member of the organization.
- **Late wiring.** The module registers its gateway under `module.ServiceLLMGateway`, which satisfies `iface.KMSProviderSetter` and `iface.AuditSinkSetter`. Compliance, which initializes after it, pushes the KMS provider and the audit sink in through those seams. Until then audit is a no-op and new secrets use the local key if one is configured.
- **Data subject rights.** The module registers an `iface.PIIProducer` for the subject `llm`. Subject-scoped queries across organizations (the grants a user holds or issued, the credentials and models they created) are the only exceptions to the rule that every query is tenant-scoped; each is marked `//tenantscope:allow dsr:`, following the authz precedent, because the memberships are already deleted by the time the producer runs.
- **New environment variable `LLM_SECRET_ENCRYPTION_KEY`**, generated by the init script, validated by the environment checker (64 hex characters, refused as a placeholder in staging and production) and passed to the backend by the compose files. A later step adds `OPERATOR_API_URL` for the Sign in with ChatGPT callback, symmetric to the existing client API URL.
- **Hosted providers are an explicit privacy opt-in.** `allow_hosted` defaults to `false`; until it is enabled only `ollama` and `mock` are selectable. It is a technical control, not a substitute for the deployer's records of processing, processor agreements and transfer mechanism.
- **Three delivery steps**, each leaving the module usable on its own: foundations (catalog, access, envelope, data-subject rights, admin UI), then the gateway (adapters, routing, ledger, budget, notifications, usage UI), then Sign in with ChatGPT.
- A fork consumes the module through `module.GetTyped[iface.LLMGateway](reg, module.ServiceLLMGateway)` and no longer needs its own provider layer.

## Alternatives considered

- **Keep a provider layer per consumer.** Rejected: duplicates key storage, endpoint validation and quota handling, and makes an organization-wide budget impossible.
- **Registry plus client factory.** Rejected under D4: the budget and the subscription rules cannot be enforced centrally.
- **External proxy.** Rejected under D4.
- **Use a consumer plan through another application's OAuth client.** Rejected: documented as a terms violation for Gemini and unsupported for Anthropic; for OpenAI only the official program is used.
- **One shared credential scope.** Rejected: a subscription is personal and must serve only its owner's requests.

## Out of scope

Monetary cost, per-user budgets, CSV export, bring-your-own-key for external tenants, grants by tenant role, batch APIs, Gemini through Vertex, Anthropic through cloud marketplaces, mobile, an OpenAI-approved hosted or partner redirect for Sign in with ChatGPT, and a local CLI with bundle import as a fallback for it.
