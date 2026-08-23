# Orkestra SDK

_Path: `/backend/pkg/sdk`_
_Parent: [../../CLAUDE.md](../../CLAUDE.md)_

## What this is

The **contract layer** between the Orkestra kernel and every module — the
`Module` interface, registry, `ServiceRegistry`, `ConfigService`, and the
`iface` consumer interfaces a fork's addon builds against.

Per [ADR-0006](../../../docs/adr/0006-collapse-to-core-only-base.md) D2 this
is an **in-tree package** of the single `github.com/orkestra/backend` Go
module — imported as `github.com/orkestra/backend/pkg/sdk/...`. There is no
separate `go.mod`, no `go.work`, no `replace`, and nothing published to the
Go proxy. (The old `github.com/orkestra-cc/orkestra-sdk` published module is
archived; the multi-repo SDK split was reverted.)

For the conceptual / new-developer walkthrough see
[../../../docs/onboarding/orkestra-sdk.md](../../../docs/onboarding/orkestra-sdk.md).

## Load-bearing invariant: SDK self-containment

**No file in `pkg/sdk/` may import anything from `backend/internal/`.**

This is the single rule that keeps the SDK publishable. Anything inside
this tree must compile against ONLY:

- the Go standard library
- the third-party modules listed in `go.mod` (huma/v2, chi/v5, google/uuid,
  prometheus, mongo-driver)
- other packages inside `pkg/sdk/`

Verify before any PR that touches this tree:

```bash
grep -rn "internal/" backend/pkg/sdk/ --include="*.go"
```

A clean run shows only doc-comment hits ("see internal/shared/X for the
backend impl"), never an actual `import` line. CI does not gate this
explicitly yet — the grep is the gate.

## Package map

| Package | Purpose | Stability |
| --- | --- | --- |
| `module/` | Module interface + 17 optional sub-interfaces, BaseModule, ModuleRegistry, ServiceRegistry, ConfigService, RouteInfo, RedisClient, secrets (AES-256-GCM helpers), `ConfigGroup`, `HasConfigGroups`. The boot kernel. | Required surface frozen at v1 |
| `iface/` | Cross-module interfaces (UserProvider, TenantProvider, AuthzProvider, NotificationSender, JWTProvider, PDFProvider, AIModelProvider, RAGQueryProvider, AuditSink, SessionTerminator, BillingTenantProvider, PaymentProvider, …) + their DTOs (User, OAuthLink, Tenant, NotificationRequest, …). | Additive-only |
| `ctxauth/` | Request-context getters: `GetUserUUID`, `GetTenantID`, `GetTenantRoles`, `GetClientIP`, `IsImpersonating`, `TenantKindFromContext`. Plus the exported `Key*` string constants the backend AuthMiddleware writes against. | Frozen |
| `modulegate/` | `ModuleGate(checker, name)` HTTP middleware (503 when disabled) + `ModuleEnabledChecker` interface. | Frozen |
| `tenantrepo/` | Fail-closed Mongo query helpers (`Scope`, `MustScope`, `StampInsert`, `StampInsertM`, `ScopeAggregate`, `RequireInternalTenant`, `RequireExternalTenant`) + `ErrTenantScopeMissing` / `ErrTenantKindMismatch` sentinels. | Frozen |
| `capability/` | `Capability` struct + `Registry`. The unit a tenant subscribes to. | Frozen |
| `metrics/` | Default Prometheus registry + a `Default` snapshot. | Frozen |

## Versioning policy

The SDK is on the path to v1.0 publication. Until then:

- **Additive-only changes** to existing interfaces and DTOs. Adding a new
  method to `iface.UserProvider` is a breaking change for every consumer
  that implements it — instead add a new sub-interface (`HasFooProvider`)
  and have callers type-assert.
- **The `Module` interface is frozen at 3 methods** (`Name`, `Category`,
  `Init`). New module capabilities go behind optional sub-interfaces in
  `module/module.go` — see the existing `HasConfigSchema`,
  `HasNavItems`, `Startable`, … pattern. Never widen `Module`.
- **`module.RedisClient` is provided TO modules, not implemented BY them.**
  It is the one SDK interface the backend satisfies on the consumer's
  behalf (`deps.RedisAdapter`), so widening it does not ripple out to
  module authors the way widening `iface.UserProvider` would. It gained
  `Incr` + `Expire` for callers that cap attempts: a read-modify-write
  counter over `Get`/`Set` loses concurrent increments, which silently
  turns "N tries" into "N tries per serial caller". A fork that
  substitutes its own `RedisClient` (a test double, typically) must add
  the two methods.
- **DTO field additions** in `iface/` should be optional (pointer types
  or `omitempty`) so older implementations keep compiling. Required
  fields are major-version bumps.
- **No new third-party dependencies** without a deliberate decision. The
  current set is intentional. Pulling in another large module
  (especially one with its own transitive driver/encoder/etc.) becomes
  a forced transitive dep on every future external addon.

## When code goes here vs `internal/`

| Concern | Goes in `pkg/sdk/` | Goes in `internal/shared/` or `internal/core/` |
| --- | --- | --- |
| Interface every module needs to consume or implement | ✅ | ❌ |
| Concrete implementation of one of those interfaces | ❌ | ✅ (in the producing module) |
| Pure data manipulation, no I/O or framework deps | ✅ if module-author-facing | ✅ if backend-internal |
| Database client wrapper, OAuth flow, cookie helpers, geoip | ❌ | ✅ |
| Anything that imports `shared/config` or auth-internal types | ❌ | ✅ |
| HTTP middleware tied to AuthMiddleware lifecycle | ❌ | ✅ (`internal/shared/middleware`) |
| HTTP middleware addons need to wrap their own routes with | ✅ | ❌ |

When in doubt, ask: **"Could an addon extracted to its own GitHub repo
import this?"** If yes, it belongs here. If it references config.Config,
auth's `*models.JWTClaims`, or any backend-private package, it doesn't.

## Import path

Every Go file in this tree (and every consumer) imports SDK packages via
the in-tree module path:

```go
import (
    "github.com/orkestra/backend/pkg/sdk/iface"
    "github.com/orkestra/backend/pkg/sdk/ctxauth"
    "github.com/orkestra/backend/pkg/sdk/module"
)
```

The old `github.com/orkestra-cc/orkestra-sdk` identity no longer exists
(ADR-0006 D2 folded the SDK back into the single backend module); a
regression grep for `orkestra-cc/orkestra-sdk` should find nothing.

## go.mod hygiene

`pkg/sdk` has no `go.mod` of its own — it is part of `backend/go.mod`. When
you add a third-party import inside `pkg/sdk/`, add it to `backend/go.mod`
and run `cd backend && go mod tidy` (the `backend-deps` make target).

## Rules

- **Never import `backend/internal/*` from inside `pkg/sdk/`.** This is
  the one rule that, if broken, makes the whole split pointless.
- **Never widen the `Module` interface.** New capabilities go behind a
  new `HasFoo` / `Fooable` sub-interface in `module/module.go` and the
  registry calls them via type-assertion accessors (`FooOf(m)`).
- **Never add a required method to an existing `iface` interface.**
  Doing so breaks every external implementor at compile time. Add a new
  interface and have the registry probe with `module.GetTyped[T]`.
- **Encryption helpers live here, not via `shared/utils`.** The SDK has
  its own `secrets.go` reading `OAUTH_TOKEN_ENCRYPTION_KEY` — the
  algorithm matches `internal/shared/utils.{Encrypt,Decrypt}OAuthToken`
  so secrets are interchangeable, but the SDK never imports utils.
- **`tenantrepo` returns SDK-native sentinel errors** (`ErrTenantScopeMissing`,
  `ErrTenantKindMismatch`), never the `internal/shared/errors` builders.
  Backend code that wants HTTP-shaped responses wraps further with its
  own typed errors at the boundary.
- **There is no `Dependencies.Config` field.** The legacy `any`-typed
  handle was retired in Phase 1c. If a core module legitimately needs
  the backend's app-wide config (today only auth qualifies), thread it
  in through that module's own `NewModule(cfg *config.Config, ...)`
  constructor at the catalog factory — see
  `cmd/server/catalog.go::coreModules` for the closure-capture pattern.
  Addons should never need this; if you reach for it, write an iface
  contract instead.
- **Config groups are presentation-only and never persisted.** `ConfigSchema`
  is snapshotted into `module_configs` and refreshed by `RefreshMetadata` on
  every boot; `ConfigGroups()` is resolved live from the registry by the admin
  handler. Do not add `bson` tags to `ConfigGroup`.
- **`ConfigField.Advanced` and `ConfigField.DependsOn` are honoured by the
  operator console.** `Advanced: true` collapses a field behind an
  "Advanced (N)" toggle on `/admin/modules/{name}`; `DependsOn`
  (`[]FieldCondition{{Key, In}}`) hides a field until another field of the
  *same* module matches — by default AND across entries, OR within one
  entry's `In` (see the matching contract documented on `FieldCondition` in
  `types.go`: a `FieldBool` target compares both sides via the `parseBool`
  rule, everything else is case-insensitive, whitespace-trimmed string
  equality). Set `DependsOnMatch: "any"` to OR across entries instead — for a
  capability with more than one independent enable switch (e.g. an OAuth
  provider's separate operator-console and client-app toggles), that is the
  only way to show the field as soon as either is on; AND would require both,
  and a single entry is wrong for the other switch. `ValidateConfigDeclarations`
  rejects an unknown `DependsOnMatch` value and a `DependsOnMatch` set without
  any `DependsOn` to combine. Both `Advanced` and `DependsOn` ride on the
  `configSchema` the admin handler already serializes, so an addon that
  declares them gets the behavior with no frontend code to write.
- **`module.HasConfigValidator` is the optional module config-validation
  seam (ADR-0017 D6).** A module implements
  `ValidateConfig(ctx context.Context, mergedValues map[string]string) error`
  to reject config values `UpdateConfig`/`UpdateEnvironmentConfig` would
  otherwise persist unchecked — `ConfigField.Min`/`Max` are `*int` and
  cannot express a bound on a duration, and teaching the service to
  interpret every schema constraint generically is a separate contract
  change with its own ADR. It runs on **both** PATCH surfaces —
  `PATCH /v1/admin/modules/{name}` and
  `PATCH /v1/admin/modules/{name}/environments/{env}` — always **before**
  encryption or persistence, and always with the module's stored non-secret
  values **merged** with the PATCH body, not just the submitted keys, so a
  cross-field rule can't be bypassed by patching one half of a pair.
  Secrets are never passed to it. Return a `*module.ConfigValidationError{Field,
  Message}` to have the admin handler map the failure to
  `422 Unprocessable Entity` naming the offending field; any other error
  propagates as an ordinary failure. Omitting the interface preserves
  today's behaviour exactly — `UpdateConfig` persists whatever it is given.
  `SetActiveEnvironment` deliberately does **not** invoke it: switching to
  an already-stored (possibly legacy-invalid) profile must stay possible so
  the defensive readers keep the deployment operable until the operator
  repairs the value on the next PATCH. See `HasConfigActivationValidator`
  below for the separate, optional seam that *does* run on activation.
- **`module.HasConfigActivationValidator` is the optional activation-veto
  seam**, distinct from `HasConfigValidator` above: a module implements
  `ValidateConfigActivation(ctx context.Context, targetValues map[string]string) error`
  to refuse `PUT /v1/admin/modules/{name}/active-environment` when the
  *complete* target profile — not a PATCH-merged one — is no longer
  satisfiable as a whole (the motivating case: a tenant provisioning policy
  stored in a profile that a later config edit elsewhere made
  inconsistent). It runs inside `SetActiveEnvironment`, after the
  "environment exists" check and strictly **before**
  `repo.SetActiveEnvironment` — the point of no return, since that write
  also flips `needsRestart: true`. A rejection therefore leaves both the
  active profile name and `needsRestart` exactly as they were.
  `targetValues` is the target profile's non-secret map only; secrets are
  never passed. Modules that omit the interface keep today's
  validation-free activation — this is deliberate legacy-recovery
  behaviour (see the note above), not an oversight: a module that only
  implements `HasConfigValidator` still activates a legacy-invalid stored
  profile unconditionally.
- **A `ConfigValidationError` with a non-empty `Code`** (e.g.
  `"tenant.single_mode_conflict"`) upgrades the admin API's response from
  the legacy text-only `422` to the `{status,title,detail,code}` envelope
  shared with `internal/shared/errcode` — reproduced SDK-locally in
  `config_error_envelope.go` since `pkg/sdk` cannot import
  `internal/shared/errcode` (SDK self-containment). `mapConfigServiceError`
  is the single mapper for all three module-admin mutation surfaces
  (`UpdateModule`, `UpdateEnvironment`, `SetActiveEnvironment`): a
  code-bearing error becomes the stable envelope, a codeless one keeps the
  pre-existing text-only `huma.Error422UnprocessableEntity`, and anything
  else falls through to the caller's own fallback status. Leave `Code`
  empty for a validator that has no reason to add a stable machine-readable
  identity yet — the legacy 422 remains correct and requires no opt-in.

## CI

`pkg/sdk` is part of the backend Go module, so `cd backend && go test ./...`
(the `backend-test` / `backend-test-ci` targets) and `golangci-lint`
(`backend-lint`) cover it with no separate target. `ci-backend` runs all of
the above plus `backend-tenantscope` and a single binary build.

## Related

- [README.md](README.md) — external-facing intro (install, Module
  contract, hello-world example, versioning policy). Keep in sync with
  this file.
- [Onboarding doc](../../../docs/onboarding/orkestra-sdk.md) — narrative
  walkthrough aimed at new contributors
- [Backend module system](../../CLAUDE.md#module-system) — how the
  registry consumes the SDK at boot
- [Core modules](../../internal/core/CLAUDE.md) — the eight always-loaded
  modules, all of which implement `module.Module`
