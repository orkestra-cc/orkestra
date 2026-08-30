# Password-Login Toggle — PR 1: SDK Config Integrity — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make module-config mutations atomic, optimistically guarded, validated against the exact target snapshot (secrets included, as presence only), audited, and immune to lazy re-seeding for the `auth` document — so PR 3's anti-lockout invariant has a substrate that cannot be raced, bypassed by profile switching, or silently reset.

**Architecture:** Every SDK change is additive or repairs an existing persistence guarantee. `ModuleConfig` gains a monotone `configRevision`; the repository gains ONE compare-and-swap `UpdateOne` shaped by an explicit `ConfigMutation` (environment write + legacy mirror, or activation), and the three service mutation paths stop composing the old two-write methods. Validation dispatches to a new optional `HasConfigSnapshotValidator` fed a `ConfigValidationSnapshot` (raw values, effective values, secret presence) built from the *target* profile before encryption; modules without it keep the two older seams untouched. `SeedFromModules` backfills schema keys an existing document lacks. `RequirePersistedConfig` disables lazy re-seed for named modules and the module list reports them as a `missing` row instead of failing. `ModuleAdminHandler` emits one best-effort audit event per actual mutation result, config write before lifecycle side effect. The console distinguishes the stale-revision 409 by body code and latches a Reload & review affordance.

**Tech Stack:** Go 1.25.13, Huma v2, MongoDB 8 (`UpdateOne` + pipeline update), `pkg/sdk/module` fake repository tests + `MONGO_TEST_URI`-guarded integration tests, React 19 / RTK Query / react-hook-form, Vitest + RTL + MSW (`onUnhandledRequest: 'error'`), i18n EN/IT parity test.

**Spec:** `docs/superpowers/specs/2026-08-29-password-login-toggle-design.md` — this plan implements the **PR 1** row of §7 (§4.1 `needsRestart` in the same write, §4.2 required-persisted-config + `missing` row, §4.4 boot backfill, §4.5 snapshot validation + atomic CAS writes, §4.10 `useModuleConfigController` 409-by-code, §4.11 audit, §4.12 docs, and the §6 tests for `pkg/sdk/module` + the admin controller). PRs 2–4 get their own plans.

## Global Constraints

- **Branch:** work on the current branch `feat/auth-password-login-toggle` (it carries the spec + this plan), rebased onto `origin/dev`. PR 1 targets `dev`. PRs 2–4 branch from `dev` after PR 1 merges.
- **`ConfigRepository` is provided TO the config service, not implemented BY modules** — the same posture `backend/pkg/sdk/CLAUDE.md` records for `module.RedisClient`, and the one its own doc comment already states ("the surface is exactly what `ModuleConfigService` calls"). Changing it (new `CompareAndSwapConfig`, new signatures for `CompareAndSwapEnvironment` / `MigrateToEnvironments`, four methods removed) is therefore a **declared exception to additive-only**, recorded in the versioning policy (Task 3 Step 7) and in the PR body; it ripples only to a fork that substitutes its own repository (a test double). The spec itself decides the widening (§4.5: "the repository interface gains one service-facing `CompareAndSwapConfig` operation"). An optional sub-interface was rejected because the service would have to keep the non-atomic two-write path alive as the fallback — the very defect this PR removes.
- **SDK self-containment:** no file under `backend/pkg/sdk/` may import `backend/internal/*`. `pkg/sdk/module` MAY import `pkg/sdk/iface` (it already does). Verify with `grep -rn "internal/" backend/pkg/sdk/ --include="*.go"` before every commit — doc-comment hits only.
- **The `Module` interface is frozen** at `Name / Category / Init`. `HasConfigSnapshotValidator` is a new optional sub-interface; `HasConfigValidator` and `HasConfigActivationValidator` stay source-compatible and keep their current dispatch when a module omits the new one.
- **`module.config_revision_stale` is SDK-owned:** the constant `CodeConfigRevisionStale` lives in `pkg/sdk/module/recordlist_mutation.go` next to `ErrRevisionStale`; `internal/shared/errcode/codes.go` gets NO `module.*` constant, and `codes_test.go` asserts the non-collision.
- **`configRevision` is the compare token, never `updatedAt`** (BSON datetimes are millisecond-precision). Absent field ≡ 0.
- **One Mongo `UpdateOne` per mutation.** No path may log-and-swallow a second write, because there is no second write. `needsRestart` is set in that same update to `!SupportsHotReload(name)`; the handler never calls `ClearNeedsRestart` after a config/environment/activation write any more.
- **Two request lanes, enforced server-side:** a key in `config` must be a declared non-secret field (or a record-list label / non-secret sub-field); a key in `secrets` a declared secret (or secret sub-field); the roster key and unknown keys are refused — `422 module.config_key_invalid`, before validation, encryption or persistence (Task 3 Step 3c). Modules with no schema keep accepting anything.
- **No secret value ever crosses the validator boundary, enters a log line, a revision error, a validation error, or an audit event.** `SecretPresent` is names → booleans; audit metadata carries key *names* (schema-derived, sorted, de-duplicated, ≤ 64) and never values.
- **Audit is best-effort under the existing `iface.AuditSink.Emit` contract** (no error return, detached 2 s write). Documentation says "emits", never "guarantees"/"transactional".
- **Config CAS before lifecycle side effect** in `PATCH /v1/admin/modules/{name}`: a 422 / stale 409 / infrastructure error on the config half never starts or stops the module. If config succeeds and the lifecycle step fails, config stays changed and both events report their actual results.
- **Lazy self-heal stays the default**; only modules named in `RequirePersistedConfig` (in-tree: `auth`) lose it, and only after boot seeding has run. `RequirePersistedConfig` is also the **boot gate** for them: a missing document or a recorded seeding/backfill failure makes `cmd/server` exit before serving. `GetAllConfigs`/`ListConfigs` never fail the whole list because one required document is missing.
- **Docs move in the same commit as the code** (repo rule, `feedback_commit_doc_hygiene`): `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/config-service.mdx`, `backend/internal/core/CLAUDE.md`, `backend/internal/core/compliance/CLAUDE.md`, `docs/site/modules/core/compliance.mdx`, `backend/CLAUDE.md` as each task touches them. `openapi/enterprise.json` is regenerated in the task that changes a response shape.
- **Test commands** (run from `backend/` unless stated): `go test ./pkg/sdk/module/ -count=1` after every backend step; `go vet ./...` before every commit (a `go build` does not compile `_test.go`); `MONGO_TEST_URI=mongodb://localhost:27017 go test ./pkg/sdk/module/ -run 'Mongo|Repository|CAS' -count=1` when the infra stack is up (`grep "^ENV=" docker/.env` first — the local stack is the **staging** stack, `orkestra-public-*-staging`). Frontend: `cd frontend-admin && npx vitest run src/pages/admin/modules` then `npm run typecheck && npm run lint`.
- **Never start servers manually**; never `git push --tags`; commit with `git commit` (the `pre-push` hook runs `make ci` — a push that looks stuck is usually still running).

## Declared deviations from the spec (read before executing)

1. **`GetRawValueRequiredModule` lands here, not in PR 3.** The spec lists it under §4.2 (PR 3), but it is a 12-line SDK accessor with no auth dependency and it is what gives `RequirePersistedConfig` a strict reader to serve. Landing it in Task 4 keeps PR 3 free of `pkg/sdk` changes.
2. **`ErrRevisionStale` is reused as the document-CAS sentinel** instead of a second `ErrConfigRevisionStale`. Its message generalizes from "the environment changed" to "the configuration changed since it was read"; nothing in-tree asserts the old text. One sentinel, one code, one 409 envelope — a record-list removal that loses its race and an ordinary config write that loses its race are the same class of failure and get the same `module.config_revision_stale` body code. Record-list *roster* conflicts (`ErrSlugExists`, `ErrSlugMissing`, `ErrUnknownSlug`) stay codeless 409s.
3. **Snapshot tests live in a new `config_snapshot_test.go`**, not appended to `config_validate_test.go` (that file tests `ValidateConfigDeclarations`, a different subject). The spec's §6 list names the older file.
4. **The record-list path (`UpdateEnvironmentConfigWithRecordLists`) also migrates to the snapshot dispatch and persists `needsRestart` in its CAS.** The spec only names the three plain paths, but `PATCH …/environments/{env}` is served by the record-list path in the handler, so leaving it on the old dispatch would make the named-environment PATCH a validation bypass — exactly what §4.5 forbids.
5. **`CompareAndSwapEnvironment` gains a `needsRestart bool` parameter and an `$inc configRevision`** (spec §4.5: "the record-list CAS path increments `ConfigRevision` in the same update"). In-tree signature change only; the fake and its two direct tests are updated.
6. **The `missing` row is a `ModuleConfigStatus{Name, Missing, Config}` returned by a new `ListConfigs`;** `GetAllConfigs` keeps its `[]ModuleConfig` signature as a thin wrapper (present documents only) so a fork calling it still compiles.
7. **A sink that *panics* is the testable stand-in for "sink failure"** in the handler: `iface.AuditSink.Emit` returns nothing, so the only handler-observable failure is a panic; the compliance sink already WARNs its own insert failures. The handler recovers, WARNs, and leaves the HTTP result unchanged.
8. **`User-Agent` reaches the actor resolver through a 10-line `RequestMeta` middleware** in `internal/shared/middleware`, mounted on the admin mutation groups in `main.go`. Huma handlers see only `context.Context`; declaring a `header:"User-Agent"` input would put the header into the OpenAPI contract.
9. **Reload & review re-applies only the DIRTY fields and clears staged record-list removals.** Capturing every form value would put the other operator's changes back to the old value as a "local edit"; only fields the operator actually touched are captured, non-secret ones keep an intentional clear to `""`, secrets are re-applied only when non-empty (a typed-then-cleared secret is no change), and `Save` is unlocked only after the refetch succeeded. A staged removal is a destructive decision made against a state the operator saw; re-arming it against a state they have not seen is the exact failure the record-list revision rule exists to prevent.
10. **The boot backfill writes only schema keys whose `EnvVar`/`Default` is non-empty, and rebuilds the mirror from the active profile.** Absence is a signal to `GetRawValue` readers (ADR-0017 D1: an absent `sessionAbsoluteTTL` means the default cap, a present `""` means "cap disabled"), so inventing `""` for every empty-fallback key would silently change policy. The spec's "every schema key present" wording is **amended in Task 5 Step 5** (§4.4, §5 #14, §6) so spec and code agree; it is no longer a deviation once that commit lands.
11. **The backfill writes `needsRestart=false`, not the resolver's answer.** Seeding runs inside `InitAll` before any module's `Init`, so every module reads the backfilled document — no restart is owed; the resolver governs post-boot edits. Writing `false` there folds the `ClearNeedsRestart` that follows for loaded modules into the same update instead of adding a second write.
12. **The `MigrateToEnvironments` legacy migration is itself a compare-and-swap** (matches only a document that still has no profiles at the read revision; a loser re-reads). Without it two concurrent `UpdateConfig`s on a legacy document could both migrate and the second would copy its stale legacy snapshot over the first's fresh profile — outside the revision guard.
13. **`ConfigRepository` changes are a declared additive-only exception**, not a sub-interface: see Global Constraints. A compile-time assertion `var _ ConfigRepository = (*ModuleConfigRepository)(nil)` pins the concrete repository to the contract.
14. **Lane validation is enforced only for modules that declare a schema.** A module with no `ConfigSchema` has nothing to classify a key against and keeps today's accept-anything behaviour (the existing `TestConfigUpdate_ModuleValidatorOptional` depends on it); every in-tree module declares one.
15. **`InvalidateCache` leaves the config-write paths.** Redis caches only the `enabled` flag (`module:enabled:*`), which no config/environment/activation write changes, so the call could only turn a committed write into a reported failure. `UpdateEnabled` keeps it as best-effort (WARN): the gate self-corrects within the 30 s TTL, and the persisted state is the truth.

## File Structure

**Backend — `backend/pkg/sdk/module/` (all in package `module`)**

| File | Responsibility | Task |
|---|---|---|
| `config_model.go` | + `ModuleConfig.ConfigRevision int64` | 1 |
| `recordlist_mutation.go` | `ErrRevisionStale` message generalized; + `CodeConfigRevisionStale` | 1 |
| `config_repository.go` | + `ConfigMutation`, `CompareAndSwapConfig`; `CompareAndSwapEnvironment` gains `needsRestart` + `$inc configRevision`; − `UpdateConfigValues`, `UpdateEnvironmentConfig`, `SetActiveEnvironment`, `ActivateEnvironment` | 1, 3 |
| `config_repository_iface.go` | interface mirrors the above | 1, 3 |
| `recordlist_fake_repo_test.go` | fake mirrors the above (+ `docCasFailures`, `docCasCalls`, `beforeDocCAS`, real `FindAll`) | 1, 3, 4 |
| `config_revision_test.go` (new) | fake-repo CAS unit tests | 1 |
| `config_repository_cas_test.go` (new) | `MONGO_TEST_URI`-guarded single-`UpdateOne` integration tests | 1 |
| `config_validator.go` | + `ConfigValidationSnapshot`, `HasConfigSnapshotValidator` | 2 |
| `config_snapshot.go` (new) | `buildValidationSnapshot`, `effectiveValues`, secret-presence rules, `ErrConfigSecretUnreadable`, `candidate` + `validateCandidate` dispatch | 2 |
| `config_snapshot_test.go` (new) | pure snapshot + dispatch tests | 2 |
| `config_service.go` | three mutation paths on snapshot + CAS; `SetHotReloadResolver`; `RequirePersistedConfig`, `ListConfigs`, `ModuleConfigStatus`, `GetRawValueRequiredModule`; backfill in `SeedFromModules` | 3, 4, 5 |
| `config_service_cas_test.go` (new) | fake-repo service tests: atomic env+legacy, race → 409 → invariant, target-secret activation | 3 |
| `config_required_test.go` (new) | required-module semantics | 4 |
| `config_backfill_test.go` (new) | boot backfill | 5 |
| `registry.go` | `InitAll` installs the hot-reload resolver before seeding | 3 |
| `config_error_envelope.go` | `mapConfigServiceError` maps `ErrRevisionStale` → 409 envelope + code, `ErrRequiredConfigMissing` → 503 | 3, 4 |
| `handler.go` | no `ClearNeedsRestart` after config writes; `ListModules` renders `missing`; `UpdateModule` config-before-lifecycle; `Missing` on response | 3, 4, 6 |
| `admin_audit.go` (new) | `AdminActor`, action constants, `SetAuditSink`, `SetActorResolver`, `emitAudit`, `auditKeyNames`, `auditErrorCode` | 6 |
| `admin_audit_test.go` (new) | handler audit tests | 6 |

**Backend — elsewhere**

| File | Responsibility | Task |
|---|---|---|
| `internal/shared/errcode/codes_test.go` | non-collision with `module.*` | 1 |
| `pkg/sdk/CLAUDE.md` | snapshot contract, CAS, required-persisted, audit setters | 3, 4, 6 |
| `internal/core/CLAUDE.md` | lazy-heal table row gains the `auth` exception | 4 |
| `cmd/server/admin_wiring.go` (new) + `admin_wiring_test.go` (new) | `requiredPersistedModules`, `adminActorResolver`, `wireModuleAdminAudit` | 4, 6 |
| `cmd/server/main.go` | `RequirePersistedConfig` after `InitAll`; `RequestMeta` + audit wiring on the admin handler | 4, 6 |
| `internal/shared/middleware/request_meta.go` (new) + test | `RequestMeta`, `UserAgentFromContext` | 6 |
| `internal/core/compliance/CLAUDE.md`, `docs/site/modules/core/compliance.mdx` | module-config event vocabulary, minimized actor, best-effort limitation | 6 |
| `docs/site/sdk/config-service.mdx` | snapshot validation, atomic writes, revision, required-persisted, audit | 3, 4, 6 |
| `backend/CLAUDE.md` | Runtime-config paragraph: revision + audit | 6 |
| `backend/openapi/enterprise.json` | regenerated (`missing` on `ModuleConfigResponse`) | 4 |

**Frontend — `frontend-admin/src/`**

| File | Responsibility | Task |
|---|---|---|
| `store/api/moduleApi.ts` | `missing?`, `'missing'` status, `CONFIG_REVISION_STALE` | 4, 7 |
| `pages/admin/modules/ModuleTable.tsx` (+ `ModuleTable.test.tsx` new) | `missing` badge | 4 |
| `pages/admin/modules/useModuleConfigController.ts` | 409-by-code latch, `conflict`, `reloadAndReview`, draft re-apply | 7 |
| `pages/admin/modules/detail/ModuleSaveBar.tsx` | `conflict` / `onReload` props | 7 |
| `pages/admin/modules/detail/ModuleConfigSection.tsx`, `detail/index.tsx` | pass the two props | 7 |
| `pages/admin/modules/detail/ModuleConfigSection.test.tsx` | conflict tests | 7 |
| `locales/en.json`, `locales/it.json` | `status.missing`, `missingConfig`, `configCard.revisionConflict`, `saveBar.reloadReview` | 4, 7 |

---

### Task 1: `configRevision` + one compare-and-swap `UpdateOne`

**Files:**
- Modify: `backend/pkg/sdk/module/config_model.go:25-45` (add `ConfigRevision`)
- Modify: `backend/pkg/sdk/module/recordlist_mutation.go:10-14` (generalize `ErrRevisionStale`, add `CodeConfigRevisionStale`)
- Modify: `backend/pkg/sdk/module/config_repository.go` (add `ConfigMutation`, `CompareAndSwapConfig`; change `CompareAndSwapEnvironment`)
- Modify: `backend/pkg/sdk/module/config_repository_iface.go`
- Modify: `backend/pkg/sdk/module/recordlist_fake_repo_test.go`
- Modify: `backend/pkg/sdk/module/recordlist_cas_test.go` (new `needsRestart` argument)
- Create: `backend/pkg/sdk/module/config_revision_test.go`
- Create: `backend/pkg/sdk/module/config_repository_cas_test.go`
- Modify: `backend/internal/shared/errcode/codes_test.go`

**Interfaces:**
- Produces: `ModuleConfig.ConfigRevision int64`; `const CodeConfigRevisionStale = "module.config_revision_stale"`; `type ConfigMutation struct{ExpectedRevision int64; Env string; EnvValues, EnvSecrets map[string]string; EnvRevision int64; WriteLegacy bool; LegacyValues, LegacySecrets map[string]string; Activate string; NeedsRestart bool}`; `ConfigRepository.CompareAndSwapConfig(ctx, name string, m ConfigMutation) (bool, error)`; `ConfigRepository.CompareAndSwapEnvironment(ctx, name, envName string, expectedRevision int64, next EnvironmentConfig, needsRestart bool) (bool, error)`; fake fields `docCasFailures int`, `docCasCalls int`, `beforeDocCAS func()`.
- Consumed by: Task 3 (service paths), Task 5 (backfill), Task 6 (error code in audit metadata).

- [ ] **Step 1: Add the revision field and the SDK-owned code**

In `config_model.go`, after `UpdatedAt` (line 40) add:

```go
	// ConfigRevision is the document-level optimistic-concurrency token.
	// Every values/secrets or activation mutation reads it, filters its
	// write on it and writes it back incremented, so two operators who each
	// read a valid document cannot combine into an invalid one — the write
	// skew a per-field invariant (PR 3's anti-lockout rule) would otherwise
	// be exposed to. A document written before this field existed carries
	// none; absent and 0 are the same value. updatedAt is deliberately NOT
	// the token: BSON datetimes have millisecond precision.
	ConfigRevision int64 `bson:"configRevision,omitempty" json:"configRevision"`
```

In `recordlist_mutation.go` replace lines 10-14 with:

```go
var (
	ErrRevisionRequired = errors.New("recordlist: a revision is required to remove elements")
	// ErrRevisionStale is returned by every compare-and-swap that lost its
	// race — an environment revision (record lists) or the document's
	// configRevision (ordinary config writes and activation). The caller's
	// view of the document no longer holds; it reloads and retries.
	ErrRevisionStale          = errors.New("module: the configuration changed since it was read")
	ErrDuplicateMutationField = errors.New("recordlist: the same field appears twice in one request")
)

// CodeConfigRevisionStale is the stable body code the admin API carries on
// the 409 produced by ErrRevisionStale. It is SDK-owned: pkg/sdk cannot
// import internal/shared/errcode, so the constant lives here and errcode's
// golden test asserts that no internal code collides with the module.*
// namespace. A client that sees it reloads the document and re-reviews its
// diff — it must never auto-retry.
const CodeConfigRevisionStale = "module.config_revision_stale"
```

- [ ] **Step 2: Write the failing fake-repo tests**

Create `config_revision_test.go`:

```go
package module

import (
	"context"
	"testing"
)

func revisionDoc(rev int64) *ModuleConfig {
	return &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "production",
		ConfigValues:      map[string]string{"a": "old"},
		EncryptedValues:   map[string]string{},
		ConfigRevision:    rev,
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"a": "old"}, EncryptedValues: map[string]string{}, Revision: 4},
			"sandbox":    {ConfigValues: map[string]string{"a": "sb"}, EncryptedValues: map[string]string{"s": "ct"}, Revision: 1},
		},
	}
}

func TestCompareAndSwapConfig_RejectsStaleAndAcceptsCurrent(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(7)
	mut := ConfigMutation{
		ExpectedRevision: 6, Env: "production",
		EnvValues: map[string]string{"a": "new"}, EnvSecrets: map[string]string{}, EnvRevision: 4,
		WriteLegacy: true, LegacyValues: map[string]string{"a": "new"}, LegacySecrets: map[string]string{},
		NeedsRestart: false,
	}
	won, err := repo.CompareAndSwapConfig(context.Background(), "demo", mut)
	if err != nil || won {
		t.Fatalf("stale expectation: won=%v err=%v, want (false, nil)", won, err)
	}
	if repo.docs["demo"].Environments["production"].ConfigValues["a"] != "old" {
		t.Fatal("a stale writer changed the document")
	}

	mut.ExpectedRevision = 7
	won, err = repo.CompareAndSwapConfig(context.Background(), "demo", mut)
	if err != nil || !won {
		t.Fatalf("current expectation lost: won=%v err=%v", won, err)
	}
	doc := repo.docs["demo"]
	if doc.ConfigRevision != 8 {
		t.Errorf("configRevision = %d, want 8", doc.ConfigRevision)
	}
	if doc.Environments["production"].Revision != 5 {
		t.Errorf("environment revision = %d, want 5", doc.Environments["production"].Revision)
	}
	if doc.Environments["production"].ConfigValues["a"] != "new" || doc.ConfigValues["a"] != "new" {
		t.Errorf("profile and legacy mirror must both carry the write: env=%v legacy=%v",
			doc.Environments["production"].ConfigValues, doc.ConfigValues)
	}
	if doc.NeedsRestart {
		t.Error("needsRestart was not persisted as given (false)")
	}
	if doc.Environments["sandbox"].ConfigValues["a"] != "sb" {
		t.Error("a production write touched the sandbox profile")
	}
}

// A document written before configRevision existed has no field. Absent and
// 0 are the same value, or the first mutation on every pre-existing module
// would fail against nothing.
func TestCompareAndSwapConfig_AbsentRevisionIsZero(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(0)
	won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{
		ExpectedRevision: 0, WriteLegacy: true,
		LegacyValues: map[string]string{"a": "x"}, LegacySecrets: map[string]string{},
	})
	if err != nil || !won {
		t.Fatalf("legacy document rejected an expected revision of 0: won=%v err=%v", won, err)
	}
	if repo.docs["demo"].ConfigRevision != 1 {
		t.Errorf("configRevision = %d, want 1", repo.docs["demo"].ConfigRevision)
	}
}

// Activation copies the target profile's STORED maps at execution time —
// never a snapshot the caller took earlier — and bumps configRevision.
func TestCompareAndSwapConfig_ActivationCopiesStoredMaps(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(2)
	repo.duringActivate = func() {
		env := repo.docs["demo"].Environments["sandbox"]
		delete(env.EncryptedValues, "s")
		repo.docs["demo"].Environments["sandbox"] = env
	}
	won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{
		ExpectedRevision: 2, Activate: "sandbox", NeedsRestart: true,
	})
	if err != nil || !won {
		t.Fatalf("activation lost: won=%v err=%v", won, err)
	}
	doc := repo.docs["demo"]
	if doc.ActiveEnvironment != "sandbox" || doc.ConfigValues["a"] != "sb" {
		t.Errorf("activation did not switch + copy: active=%q legacy=%v", doc.ActiveEnvironment, doc.ConfigValues)
	}
	if _, ok := doc.EncryptedValues["s"]; ok {
		t.Error("activation copied a stale snapshot and resurrected a removed secret")
	}
	if doc.ConfigRevision != 3 || !doc.NeedsRestart {
		t.Errorf("configRevision=%d needsRestart=%v, want 3/true", doc.ConfigRevision, doc.NeedsRestart)
	}
}

func TestCompareAndSwapConfig_RejectsUnknownProfileAndMalformedShape(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(0)
	if won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{Activate: "nope"}); err != nil || won {
		t.Errorf("activating an unknown profile: won=%v err=%v, want (false, nil)", won, err)
	}
	if won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{Env: "nope", EnvValues: map[string]string{}, EnvSecrets: map[string]string{}}); err != nil || won {
		t.Errorf("writing an unknown profile: won=%v err=%v, want (false, nil)", won, err)
	}
	if _, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{Activate: "sandbox", Env: "production"}); err == nil {
		t.Error("activation combined with a values write must be rejected as a programming error")
	}
	if _, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{}); err == nil {
		t.Error("an empty mutation must be rejected")
	}
	if repo.docs["demo"].ConfigRevision != 0 {
		t.Error("a rejected mutation moved the revision")
	}
}

// The record-list CAS increments configRevision in the same update, so an
// ordinary config write that read the document before a roster change loses
// its own CAS instead of silently passing it.
func TestCompareAndSwapEnvironment_BumpsConfigRevisionAndPersistsNeedsRestart(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(5)
	won, err := repo.CompareAndSwapEnvironment(context.Background(), "demo", "production", 4,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "rl"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("record-list CAS lost: won=%v err=%v", won, err)
	}
	if repo.docs["demo"].ConfigRevision != 6 {
		t.Errorf("configRevision = %d, want 6", repo.docs["demo"].ConfigRevision)
	}
	if repo.docs["demo"].NeedsRestart {
		t.Error("needsRestart was not persisted as given (false)")
	}
	won, err = repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{
		ExpectedRevision: 5, WriteLegacy: true, LegacyValues: map[string]string{}, LegacySecrets: map[string]string{},
	})
	if err != nil || won {
		t.Errorf("a config write that read revision 5 must lose after the roster write: won=%v err=%v", won, err)
	}
}
```

Update the two existing calls in `recordlist_cas_test.go` (lines 20, 28, 49) to pass a trailing `true`:

```go
	won, err := repo.CompareAndSwapEnvironment(context.Background(), "demo", "production", 3, next, true)
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd backend && go test ./pkg/sdk/module/ -run 'CompareAndSwap' -count=1`
Expected: compile errors — `undefined: ConfigMutation`, `repo.CompareAndSwapConfig undefined`, `too many arguments in call to repo.CompareAndSwapEnvironment`.

- [ ] **Step 4: Implement the Mongo repository**

In `config_repository.go`, replace `CompareAndSwapEnvironment` (lines 204-261) with:

```go
// CompareAndSwapEnvironment replaces ONE environment sub-document, and only
// while its revision still matches. Returns false — not an error — when the
// revision has moved: losing a race is an expected outcome the caller decides
// what to do about, not a failure.
//
// Scoping the swap to the sub-document matters: Environments is a nested map,
// so guarding a whole-document replace with one environment's revision would
// silently discard concurrent edits to a sibling environment.
//
// It is a PIPELINE update: the "is this the active profile?" decision is
// taken by the server against the document's CURRENT activeEnvironment, in
// the same operation that writes the profile. The previous read-then-update
// let a concurrent activation land between the two and left the legacy
// mirror carrying the wrong profile. $literal stops the aggregation engine
// from interpreting stored maps (dotted keys, "$"-prefixed values) as
// expressions. configRevision advances in the same update, so an ordinary
// config write that read the document before this roster change loses its
// own compare-and-swap instead of passing it unseen; needsRestart is
// persisted as given — the inverse of the module's hot-reload capability.
//
// A document written before record lists existed carries no revision field at
// all. Absent and 0 are the same value, so an expectation of 0 also matches a
// missing field — otherwise the first mutation on every pre-existing module
// would fail against nothing.
func (r *ModuleConfigRepository) CompareAndSwapEnvironment(
	ctx context.Context, name, envName string, expectedRevision int64, next EnvironmentConfig, needsRestart bool,
) (bool, error) {
	envPath := "environments." + envName
	next.Revision = expectedRevision + 1
	next.UpdatedAt = time.Now().UTC()
	if next.ConfigValues == nil {
		next.ConfigValues = map[string]string{}
	}
	if next.EncryptedValues == nil {
		next.EncryptedValues = map[string]string{}
	}

	filter := bson.M{"moduleName": name}
	if expectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{envPath + ".revision": expectedRevision},
			bson.M{envPath + ".revision": bson.M{"$exists": false}},
		}
	} else {
		filter[envPath+".revision"] = expectedRevision
	}

	isActive := bson.M{"$eq": bson.A{
		bson.M{"$ifNull": bson.A{"$activeEnvironment", "production"}},
		envName,
	}}
	update := mongo.Pipeline{{{Key: "$set", Value: bson.M{
		envPath:           bson.M{"$literal": next},
		"configValues":    bson.M{"$cond": bson.A{isActive, bson.M{"$literal": next.ConfigValues}, "$configValues"}},
		"encryptedValues": bson.M{"$cond": bson.A{isActive, bson.M{"$literal": next.EncryptedValues}, "$encryptedValues"}},
		"needsRestart":    needsRestart,
		"configRevision":  bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$configRevision", 0}}, 1}},
		"updatedAt":       next.UpdatedAt,
	}}}}

	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("compare-and-swap environment %q/%q: %w", name, envName, err)
	}
	return res.MatchedCount == 1, nil
}

// ConfigMutation is the explicit shape of ONE atomic module_configs write.
// Exactly one of two forms is used:
//
//   - a values write: Env names the profile whose maps are REPLACED by
//     EnvValues/EnvSecrets (callers merge first) and whose revision becomes
//     EnvRevision+1; WriteLegacy additionally replaces the top-level legacy
//     maps with LegacyValues/LegacySecrets — the active-profile write and
//     the boot backfill both use this, the inactive-profile write does not;
//   - an activation: Activate names the profile to make active, and its
//     STORED maps are copied server-side into the legacy fields.
//
// Every form filters on ExpectedRevision, writes ExpectedRevision+1 back,
// and persists NeedsRestart as given. Secrets are ciphertext by the time
// they reach here.
type ConfigMutation struct {
	ExpectedRevision int64

	Env         string
	EnvValues   map[string]string
	EnvSecrets  map[string]string
	EnvRevision int64

	WriteLegacy   bool
	LegacyValues  map[string]string
	LegacySecrets map[string]string

	Activate string

	NeedsRestart bool
}

// validate rejects a shape the repository cannot express in one update.
// These are programming errors, not runtime conditions.
func (m *ConfigMutation) validate() error {
	switch {
	case m.Activate != "" && (m.Env != "" || m.WriteLegacy):
		return errors.New("config mutation: activation cannot be combined with a values write")
	case m.Activate == "" && m.Env == "" && !m.WriteLegacy:
		return errors.New("config mutation: nothing to write")
	}
	if m.Env != "" {
		if m.EnvValues == nil {
			m.EnvValues = map[string]string{}
		}
		if m.EnvSecrets == nil {
			m.EnvSecrets = map[string]string{}
		}
	}
	if m.WriteLegacy {
		if m.LegacyValues == nil {
			m.LegacyValues = map[string]string{}
		}
		if m.LegacySecrets == nil {
			m.LegacySecrets = map[string]string{}
		}
	}
	return nil
}

// CompareAndSwapConfig applies one ConfigMutation to a module document in a
// SINGLE UpdateOne, and only while the document's configRevision still equals
// m.ExpectedRevision (an absent field matches 0). Returns (false, nil) when
// nothing matched — a lost race, or a profile that no longer exists — which
// the service reports as ErrRevisionStale.
//
// Because it is one update, either every target field lands or none does:
// there is no legacy/environment partial state and no second write whose
// failure could be logged and swallowed. An activation is a pipeline update
// so the copied values are read server-side at execution time, never from a
// snapshot the process took earlier.
func (r *ModuleConfigRepository) CompareAndSwapConfig(ctx context.Context, name string, m ConfigMutation) (bool, error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	next := m.ExpectedRevision + 1

	filter := bson.M{"moduleName": name}
	if m.ExpectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{"configRevision": int64(0)},
			bson.M{"configRevision": bson.M{"$exists": false}},
		}
	} else {
		filter["configRevision"] = m.ExpectedRevision
	}

	var update any
	if m.Activate != "" {
		filter["environments."+m.Activate] = bson.M{"$exists": true}
		envPath := "$environments." + m.Activate
		update = mongo.Pipeline{{{Key: "$set", Value: bson.M{
			"activeEnvironment": m.Activate,
			"configValues":      bson.M{"$ifNull": bson.A{envPath + ".configValues", bson.M{}}},
			"encryptedValues":   bson.M{"$ifNull": bson.A{envPath + ".encryptedValues", bson.M{}}},
			"needsRestart":      m.NeedsRestart,
			"configRevision":    next,
			"updatedAt":         now,
		}}}}
	} else {
		set := bson.M{
			"needsRestart":   m.NeedsRestart,
			"configRevision": next,
			"updatedAt":      now,
		}
		if m.Env != "" {
			p := "environments." + m.Env
			filter[p] = bson.M{"$exists": true}
			set[p+".configValues"] = m.EnvValues
			set[p+".encryptedValues"] = m.EnvSecrets
			set[p+".revision"] = m.EnvRevision + 1
			set[p+".updatedAt"] = now
		}
		if m.WriteLegacy {
			set["configValues"] = m.LegacyValues
			set["encryptedValues"] = m.LegacySecrets
		}
		update = bson.M{"$set": set}
	}

	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("compare-and-swap config %q: %w", name, err)
	}
	return res.MatchedCount == 1, nil
}
```

In `config_repository_iface.go` change the `CompareAndSwapEnvironment` line, add the new method (the four legacy write methods are removed in Task 3, not here), and pin the concrete type to the contract:

```go
	CompareAndSwapEnvironment(ctx context.Context, name, envName string, expectedRevision int64, next EnvironmentConfig, needsRestart bool) (bool, error)
	CompareAndSwapConfig(ctx context.Context, name string, m ConfigMutation) (bool, error)
}

// ConfigRepository is provided TO ModuleConfigService by the host, never
// implemented BY a module — so, like RedisClient, it is outside the SDK's
// additive-only rule for consumer interfaces (see pkg/sdk/CLAUDE.md,
// "Versioning policy"). A fork that substitutes its own repository (a test
// double, typically) tracks it.
var _ ConfigRepository = (*ModuleConfigRepository)(nil)
```

(The doc comment above the interface, lines 5-11, already states the "exactly what the service calls" contract; keep it.)

- [ ] **Step 5: Update the fake repository**

In `recordlist_fake_repo_test.go`, extend the struct and replace `CompareAndSwapEnvironment`; add `CompareAndSwapConfig`:

```go
type fakeConfigRepo struct {
	docs        map[string]*ModuleConfig
	casFailures int // fail this many environment-CAS attempts before allowing one through
	casCalls    int
	// docCasFailures / docCasCalls are the document-level twins for
	// CompareAndSwapConfig, kept separate so a record-list test's counters
	// are never disturbed by a config-write test and vice versa.
	docCasFailures int
	docCasCalls    int
	// beforeDocCAS runs inside CompareAndSwapConfig before the revision is
	// compared — the window in which a concurrent writer lands. It is how a
	// two-writer race is modelled without a second goroutine.
	beforeDocCAS func()
	// duringActivate runs inside an activation, modelling a concurrent
	// write landing in the window a two-step activation leaves open.
	duringActivate func()
}
```

```go
// CompareAndSwapEnvironment mirrors the Mongo implementation's contract: a
// mismatched revision is a lost race (false, nil), not an error; the
// document-level configRevision advances in the same write. casFailures
// forces the first N attempts to lose.
func (f *fakeConfigRepo) CompareAndSwapEnvironment(_ context.Context, name, env string, expected int64, next EnvironmentConfig, needsRestart bool) (bool, error) {
	f.casCalls++
	if f.casFailures > 0 {
		f.casFailures--
		return false, nil
	}
	doc, ok := f.docs[name]
	if !ok {
		return false, nil
	}
	cur := doc.Environments[env]
	if cur.Revision != expected {
		return false, nil
	}
	next.Revision = cur.Revision + 1
	doc.Environments[env] = next
	if doc.ActiveEnv() == env {
		doc.ConfigValues, doc.EncryptedValues = next.ConfigValues, next.EncryptedValues
	}
	doc.NeedsRestart = needsRestart
	doc.ConfigRevision++
	return true, nil
}

// CompareAndSwapConfig mirrors the Mongo single-update contract: the whole
// mutation lands or nothing does, the revision is compared at execution time
// (after beforeDocCAS, so a modelled concurrent write is visible), and an
// activation copies the STORED profile maps rather than a caller snapshot.
func (f *fakeConfigRepo) CompareAndSwapConfig(_ context.Context, name string, m ConfigMutation) (bool, error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	f.docCasCalls++
	if f.beforeDocCAS != nil {
		f.beforeDocCAS()
	}
	if f.docCasFailures > 0 {
		f.docCasFailures--
		return false, nil
	}
	doc, ok := f.docs[name]
	if !ok || doc.ConfigRevision != m.ExpectedRevision {
		return false, nil
	}
	if m.Activate != "" {
		if _, ok := doc.Environments[m.Activate]; !ok {
			return false, nil
		}
		if f.duringActivate != nil {
			f.duringActivate()
		}
		cfg := doc.Environments[m.Activate]
		doc.ActiveEnvironment = m.Activate
		doc.ConfigValues = copyStrings(cfg.ConfigValues)
		doc.EncryptedValues = copyStrings(cfg.EncryptedValues)
	} else {
		if m.Env != "" {
			cur, ok := doc.Environments[m.Env]
			if !ok {
				return false, nil
			}
			cur.ConfigValues = copyStrings(m.EnvValues)
			cur.EncryptedValues = copyStrings(m.EnvSecrets)
			cur.Revision = m.EnvRevision + 1
			doc.Environments[m.Env] = cur
		}
		if m.WriteLegacy {
			doc.ConfigValues = copyStrings(m.LegacyValues)
			doc.EncryptedValues = copyStrings(m.LegacySecrets)
		}
	}
	doc.NeedsRestart = m.NeedsRestart
	doc.ConfigRevision = m.ExpectedRevision + 1
	return true, nil
}
```

Leave the fake's `ActivateEnvironment`/`UpdateConfigValues`/`UpdateEnvironmentConfig`/`SetActiveEnvironment` in place for now (Task 3 deletes them together with the interface methods). Also update `recordlist_mutation.go:141` to pass `true` for now (`s.repo.CompareAndSwapEnvironment(ctx, name, envName, cur.Revision, next, true)`) — Task 3 replaces it with the resolver value.

- [ ] **Step 6: Run the unit tests**

Run: `cd backend && go test ./pkg/sdk/module/ -count=1`
Expected: PASS (all of `config_revision_test.go`, the two updated `recordlist_cas_test.go` tests, and everything else).

- [ ] **Step 7: Write the Mongo integration tests**

Create `config_repository_cas_test.go` (guarded like `config_validator_test.go`; skips without `MONGO_TEST_URI`/`MONGO_URI`):

```go
package module

import (
	"context"
	"testing"
	"time"
)

func seedRevisionDoc(t *testing.T, repo *ModuleConfigRepository) {
	t.Helper()
	doc := &ModuleConfig{
		ModuleName:        "cas",
		Category:          CategoryCore,
		ActiveEnvironment: "production",
		ConfigValues:      map[string]string{"a": "old"},
		EncryptedValues:   map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"a": "old"}, EncryptedValues: map[string]string{}, UpdatedAt: time.Now()},
			"sandbox":    {ConfigValues: map[string]string{"a": "sb"}, EncryptedValues: map[string]string{"s": "ct"}, UpdatedAt: time.Now()},
		},
	}
	if err := repo.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// Values, secrets and metadata land in ONE UpdateOne; the legacy mirror and
// the profile are identical afterwards; a document seeded without the field
// compares as revision 0 and becomes 1.
func TestMongoCompareAndSwapConfig_SingleUpdatePersistsEverything(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)

	won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{
		ExpectedRevision: 0, Env: "production",
		EnvValues: map[string]string{"a": "new"}, EnvSecrets: map[string]string{"k": "ciphertext"}, EnvRevision: 0,
		WriteLegacy: true, LegacyValues: map[string]string{"a": "new"}, LegacySecrets: map[string]string{"k": "ciphertext"},
		NeedsRestart: false,
	})
	if err != nil || !won {
		t.Fatalf("CAS: won=%v err=%v", won, err)
	}
	doc, err := repo.FindByName(ctx, "cas")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ConfigRevision != 1 {
		t.Errorf("configRevision = %d, want 1", doc.ConfigRevision)
	}
	prod := doc.Environments["production"]
	if prod.ConfigValues["a"] != "new" || prod.EncryptedValues["k"] != "ciphertext" || prod.Revision != 1 {
		t.Errorf("profile not written: %+v", prod)
	}
	if doc.ConfigValues["a"] != "new" || doc.EncryptedValues["k"] != "ciphertext" {
		t.Errorf("legacy mirror not synced: %v %v", doc.ConfigValues, doc.EncryptedValues)
	}
	if doc.NeedsRestart {
		t.Error("needsRestart = true, want the given false")
	}
	if doc.Environments["sandbox"].ConfigValues["a"] != "sb" {
		t.Error("sibling profile disturbed")
	}
}

func TestMongoCompareAndSwapConfig_StaleWriterChangesNothing(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	// First writer moves the document to revision 1.
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{
		ExpectedRevision: 0, WriteLegacy: true, LegacyValues: map[string]string{"a": "first"},
	}); err != nil || !won {
		t.Fatalf("first writer: won=%v err=%v", won, err)
	}
	// Second writer still expects 0.
	won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{
		ExpectedRevision: 0, WriteLegacy: true, LegacyValues: map[string]string{"a": "second"},
	})
	if err != nil || won {
		t.Fatalf("stale writer: won=%v err=%v, want (false, nil)", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.ConfigValues["a"] != "first" || doc.ConfigRevision != 1 {
		t.Errorf("stale writer changed the document: %v rev=%d", doc.ConfigValues, doc.ConfigRevision)
	}
}

// A write error leaves the document untouched — there is no first half that
// can land without the second. A cancelled context is the injectable failure.
func TestMongoCompareAndSwapConfig_WriteErrorLeavesDocumentUnchanged(t *testing.T) {
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repo.CompareAndSwapConfig(cancelled, "cas", ConfigMutation{
		ExpectedRevision: 0, Env: "production", EnvValues: map[string]string{"a": "never"},
		WriteLegacy: true, LegacyValues: map[string]string{"a": "never"},
	})
	if err == nil {
		t.Fatal("a cancelled context must surface as an error")
	}
	doc, _ := repo.FindByName(context.Background(), "cas")
	if doc.ConfigValues["a"] != "old" || doc.Environments["production"].ConfigValues["a"] != "old" || doc.ConfigRevision != 0 {
		t.Errorf("a failed write left partial state: %+v", doc)
	}
}

func TestMongoCompareAndSwapConfig_ActivationCopiesServerSide(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 0, Activate: "sandbox", NeedsRestart: true})
	if err != nil || !won {
		t.Fatalf("activation: won=%v err=%v", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.ActiveEnv() != "sandbox" || doc.ConfigValues["a"] != "sb" || doc.EncryptedValues["s"] != "ct" {
		t.Errorf("activation did not copy the stored sandbox maps: %+v", doc)
	}
	if doc.ConfigRevision != 1 || !doc.NeedsRestart {
		t.Errorf("configRevision=%d needsRestart=%v, want 1/true", doc.ConfigRevision, doc.NeedsRestart)
	}
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 1, Activate: "nope"}); err != nil || won {
		t.Errorf("unknown profile: won=%v err=%v, want (false, nil)", won, err)
	}
}

// The active-profile decision is taken server-side in the same update, so
// the legacy mirror always tracks the profile that is active AT WRITE TIME —
// a caller who read the document before a concurrent activation cannot
// leave the mirror carrying the wrong profile.
func TestMongoCompareAndSwapEnvironment_MirrorFollowsStoredActiveEnv(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo) // active: production
	// Another writer activates sandbox; the mirror is now sandbox's.
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 0, Activate: "sandbox"}); err != nil || !won {
		t.Fatalf("activation: won=%v err=%v", won, err)
	}
	// A record-list write to production, decided by a caller who read the
	// document BEFORE that activation (env revision still 0).
	won, err := repo.CompareAndSwapEnvironment(ctx, "cas", "production", 0,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "prod-new"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("production env CAS: won=%v err=%v", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.Environments["production"].ConfigValues["a"] != "prod-new" {
		t.Error("production profile not written")
	}
	if doc.ConfigValues["a"] != "sb" {
		t.Errorf("mirror = %v, want the ACTIVE (sandbox) values untouched", doc.ConfigValues)
	}
	// A write to the now-active sandbox does update the mirror.
	won, err = repo.CompareAndSwapEnvironment(ctx, "cas", "sandbox", 0,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "sb-new"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("sandbox env CAS: won=%v err=%v", won, err)
	}
	doc, _ = repo.FindByName(ctx, "cas")
	if doc.ConfigValues["a"] != "sb-new" || doc.ConfigRevision != 3 {
		t.Errorf("mirror=%v configRevision=%d, want sb-new / 3", doc.ConfigValues, doc.ConfigRevision)
	}
}

// Record-list and ordinary writes cannot pass each other unseen: the
// environment CAS bumps configRevision, so an ordinary writer that read the
// document earlier loses.
func TestMongoCompareAndSwapEnvironment_BumpsConfigRevision(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	won, err := repo.CompareAndSwapEnvironment(ctx, "cas", "production", 0,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "rl"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("env CAS: won=%v err=%v", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.ConfigRevision != 1 || doc.NeedsRestart {
		t.Errorf("configRevision=%d needsRestart=%v after env CAS, want 1/false", doc.ConfigRevision, doc.NeedsRestart)
	}
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 0, WriteLegacy: true, LegacyValues: map[string]string{}}); err != nil || won {
		t.Errorf("ordinary writer at revision 0 must lose after a roster write: won=%v err=%v", won, err)
	}
}
```

- [ ] **Step 8: Run the integration tests against the local Mongo**

Run: `grep "^ENV=" docker/.env` (expect `staging`), then `cd backend && MONGO_TEST_URI="$(sed -n 's/^MONGO_URI=//p' ../docker/.env)" go test ./pkg/sdk/module/ -run 'TestMongo' -count=1 -v`
Expected: 6 PASS lines. If the URI is not exposed on the host, the tests `SKIP` — record that in the commit message and rely on CI (`MONGO_URI` is set on the backend job).

- [ ] **Step 9: Assert the errcode namespace never collides**

Append to `backend/internal/shared/errcode/codes_test.go`:

```go
// TestNoCollisionWithSDKOwnedCodes: pkg/sdk/module owns
// module.config_revision_stale (it cannot import this package to share a
// constant), so the whole "module." namespace is reserved to the SDK. A
// constant here that reused it would give two different failures one wire
// identity.
func TestNoCollisionWithSDKOwnedCodes(t *testing.T) {
	for name, value := range goldenCodes {
		if value == module.CodeConfigRevisionStale || strings.HasPrefix(value, "module.") {
			t.Errorf("%s = %q collides with the SDK-owned module.* namespace", name, value)
		}
	}
}
```

Add `"strings"` and `"github.com/orkestra/backend/pkg/sdk/module"` to that file's imports.

Run: `cd backend && go test ./internal/shared/errcode/ -count=1`
Expected: PASS.

- [ ] **Step 10: Vet and commit**

Run: `cd backend && go vet ./pkg/sdk/... ./internal/shared/errcode/ && grep -rn "internal/" pkg/sdk/ --include="*.go" | grep -v "^.*//"`
Expected: vet clean; the grep prints nothing (or only comment lines).

```bash
git add backend/pkg/sdk/module/config_model.go backend/pkg/sdk/module/recordlist_mutation.go \
  backend/pkg/sdk/module/config_repository.go backend/pkg/sdk/module/config_repository_iface.go \
  backend/pkg/sdk/module/recordlist_fake_repo_test.go backend/pkg/sdk/module/recordlist_cas_test.go \
  backend/pkg/sdk/module/config_revision_test.go backend/pkg/sdk/module/config_repository_cas_test.go \
  backend/internal/shared/errcode/codes_test.go
git commit -m "feat(sdk): configRevision + single-UpdateOne CompareAndSwapConfig on module_configs

Adds the document-level optimistic-concurrency token and the one atomic
repository write (environment + legacy mirror, or server-side activation)
the config service migrates to next. The record-list CAS now bumps
configRevision and persists needsRestart in the same update. The 409 code
module.config_revision_stale is SDK-owned; errcode asserts non-collision.

Refs: docs/superpowers/specs/2026-08-29-password-login-toggle-design.md §4.5"
```

### Task 2: `ConfigValidationSnapshot` + `HasConfigSnapshotValidator` + the snapshot builder and dispatch

**Files:**
- Modify: `backend/pkg/sdk/module/config_validator.go` (append the two types)
- Create: `backend/pkg/sdk/module/config_snapshot.go`
- Create: `backend/pkg/sdk/module/config_snapshot_test.go`
- Modify: `backend/pkg/sdk/module/config_service.go:382-396` (`validateModuleConfig` → `validateCandidate`)

**Interfaces:**
- Consumes: `decryptSecret` (`secrets.go:70`), `ParseRoster`/`ItemKey` (`recordlist_roster.go`, `recordlist_keys.go`), `mergeStringMaps` (`config_service.go:467`).
- Produces: `type ConfigValidationSnapshot struct{Environment string; Values, EffectiveValues map[string]string; SecretPresent map[string]bool}`; `type HasConfigSnapshotValidator interface{ ValidateConfigSnapshot(context.Context, ConfigValidationSnapshot) error }`; `var ErrConfigSecretUnreadable`; `func buildValidationSnapshot(schema []ConfigField, env string, values, storedEncrypted, submittedSecrets map[string]string) (ConfigValidationSnapshot, error)`; `func schemaFallbackValue(f ConfigField) string`; `type candidate struct{schema []ConfigField; env string; values, storedEncrypted, submittedSecrets map[string]string; activation bool}`; `func (s *ModuleConfigService) validateCandidate(ctx, name string, c candidate) error`.
- Consumed by: Task 3 (all four mutation paths), Task 5 (`schemaFallbackValue`).

- [ ] **Step 1: Declare the contract**

Append to `config_validator.go`:

```go
// ConfigValidationSnapshot is what a HasConfigSnapshotValidator judges: the
// exact profile that would become effective after the mutation, on
// whichever of the three mutation surfaces produced it (active-config
// PATCH, named-environment PATCH, activation).
//
//   - Values is the raw merged target profile. Presence and explicit
//     emptiness are preserved, so a strict boolean or duration rule can
//     tell "absent" from "cleared".
//   - EffectiveValues applies the same fallback the runtime GetValue
//     applies — empty → schema EnvVar → schema Default — so a rule about
//     what the module will actually run with reads this map, and validation
//     agrees with runtime even when a credential is supplied by the
//     deployment environment rather than stored config.
//   - SecretPresent reports, per secret key, whether a NON-EMPTY value would
//     be in force after the write: a secret submitted in this request first,
//     else the target profile's OWN stored ciphertext (decrypted only inside
//     ConfigService, only to test emptiness), else the schema
//     EnvVar/Default. Names and booleans only: no plaintext crosses the
//     validator boundary, and no other profile's secrets are ever consulted
//     — an inactive environment is judged from its own secrets, never the
//     active environment's.
type ConfigValidationSnapshot struct {
	Environment     string
	Values          map[string]string
	EffectiveValues map[string]string
	SecretPresent   map[string]bool
}

// HasConfigSnapshotValidator is the OPTIONAL successor to HasConfigValidator
// and HasConfigActivationValidator: one policy function that sees the
// complete target snapshot on all three mutation surfaces, so a cross-field
// rule that depends on secret presence (an OAuth provider being "fully
// configured") cannot be bypassed by editing the other half of the pair on
// a different surface. A module that implements it is judged through it
// everywhere and its older hooks are NOT called; a module that omits it
// keeps the two older seams exactly as they were. Return a
// *ConfigValidationError for a 422 naming the field; any other error
// propagates as an ordinary failure.
type HasConfigSnapshotValidator interface {
	ValidateConfigSnapshot(ctx context.Context, snapshot ConfigValidationSnapshot) error
}
```

- [ ] **Step 2: Write the failing builder tests**

Create `config_snapshot_test.go`:

```go
package module

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

var snapshotSchema = []ConfigField{
	{Key: "clientId", Type: FieldString, EnvVar: "SNAP_TEST_CLIENT_ID", Default: "default-id"},
	{Key: "redirect", Type: FieldString},
	{Key: "clientSecret", Type: FieldSecret, EnvVar: "SNAP_TEST_CLIENT_SECRET"},
	{Key: "otherSecret", Type: FieldSecret},
	{Key: "flag", Type: FieldBool, Default: "false"},
	{Key: "profiles", Type: FieldRecordList, Items: []ConfigItemField{
		{Key: "host", Type: FieldString, Default: "h-default"},
		{Key: "password", Type: FieldSecret},
		{Key: "token", Type: FieldSecret, Default: "tok-default"},
	}},
}

func TestBuildValidationSnapshot_RawVersusEffective(t *testing.T) {
	t.Setenv("SNAP_TEST_CLIENT_ID", "")
	values := map[string]string{"redirect": "", "profiles.__items": "a,b", "profiles.a.host": "h"}
	snap, err := buildValidationSnapshot(snapshotSchema, "sandbox", values, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Environment != "sandbox" {
		t.Errorf("Environment = %q", snap.Environment)
	}
	// Raw: absent vs explicit empty are distinguishable.
	if _, ok := snap.Values["clientId"]; ok {
		t.Error("raw Values must not invent an absent key")
	}
	if v, ok := snap.Values["redirect"]; !ok || v != "" {
		t.Error("raw Values must keep an explicit empty value")
	}
	// Effective: schema Default applied to the absent key, and to the bool.
	if snap.EffectiveValues["clientId"] != "default-id" || snap.EffectiveValues["flag"] != "false" {
		t.Errorf("EffectiveValues did not apply schema defaults: %v", snap.EffectiveValues)
	}
	if _, ok := snap.EffectiveValues["redirect"]; !ok || snap.EffectiveValues["redirect"] != "" {
		t.Error("a field with no fallback stays empty in EffectiveValues")
	}
	// Record-list element keys pass through verbatim; a roster element's
	// absent sub-field takes the item Default, exactly as assignRecordList
	// resolves it at runtime; an element outside the roster gets nothing.
	if snap.EffectiveValues["profiles.a.host"] != "h" || snap.EffectiveValues["profiles.b.host"] != "h-default" {
		t.Errorf("record-list effective values: %v", snap.EffectiveValues)
	}
	if _, ok := snap.Values["profiles.b.host"]; ok {
		t.Error("raw Values must not carry an item default")
	}
	if _, ok := snap.EffectiveValues["profiles.zzz.host"]; ok {
		t.Error("a non-roster element must not be resolved")
	}
	for _, m := range []map[string]string{snap.Values, snap.EffectiveValues} {
		if _, ok := m["clientSecret"]; ok {
			t.Error("a secret key leaked into a values map")
		}
	}
}

func TestBuildValidationSnapshot_EnvVarBeatsDefault(t *testing.T) {
	t.Setenv("SNAP_TEST_CLIENT_ID", "from-env")
	snap, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.EffectiveValues["clientId"] != "from-env" {
		t.Errorf("EffectiveValues[clientId] = %q, want the EnvVar value", snap.EffectiveValues["clientId"])
	}
}

func TestBuildValidationSnapshot_SecretPresence(t *testing.T) {
	withEncryptionKey(t)
	t.Setenv("SNAP_TEST_CLIENT_SECRET", "")
	stored, _ := encryptSecret("stored-plaintext")
	storedEmpty, _ := encryptSecret("")
	cases := []struct {
		name      string
		stored    map[string]string
		submitted map[string]string
		env       string
		want      map[string]bool
	}{
		{"stored ciphertext is present", map[string]string{"clientSecret": stored}, nil, "", map[string]bool{"clientSecret": true, "otherSecret": false}},
		{"submitted non-empty is present without anything stored", nil, map[string]string{"otherSecret": "new"}, "", map[string]bool{"clientSecret": false, "otherSecret": true}},
		{"submitted empty is NOT presence", map[string]string{"otherSecret": stored}, map[string]string{"otherSecret": ""}, "", map[string]bool{"clientSecret": false, "otherSecret": false}},
		{"submitted empty falls back to the EnvVar", nil, map[string]string{"clientSecret": ""}, "env-secret", map[string]bool{"clientSecret": true, "otherSecret": false}},
		{"schema EnvVar alone is presence", nil, nil, "env-secret", map[string]bool{"clientSecret": true, "otherSecret": false}},
		{"stored empty ciphertext is absence", map[string]string{"clientSecret": storedEmpty}, nil, "", map[string]bool{"clientSecret": false, "otherSecret": false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SNAP_TEST_CLIENT_SECRET", c.env)
			snap, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, c.stored, c.submitted)
			if err != nil {
				t.Fatal(err)
			}
			for k, want := range c.want {
				if snap.SecretPresent[k] != want {
					t.Errorf("SecretPresent[%s] = %v, want %v", k, snap.SecretPresent[k], want)
				}
			}
			// Plaintext never reaches the snapshot.
			for _, m := range []map[string]string{snap.Values, snap.EffectiveValues} {
				for _, v := range m {
					if strings.Contains(v, "plaintext") || v == "new" || v == "env-secret" {
						t.Errorf("secret material leaked into a values map: %q", v)
					}
				}
			}
		})
	}
}

func TestBuildValidationSnapshot_CorruptCiphertext(t *testing.T) {
	withEncryptionKey(t)
	corrupt := map[string]string{"clientSecret": "not-a-ciphertext"}
	_, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, corrupt, nil)
	if !errors.Is(err, ErrConfigSecretUnreadable) {
		t.Fatalf("corrupt stored secret: err = %v, want ErrConfigSecretUnreadable", err)
	}
	// A submitted replacement is judged BEFORE the stored ciphertext is
	// touched, so the operator can repair a corrupt secret in one PATCH.
	snap, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, corrupt, map[string]string{"clientSecret": "replacement"})
	if err != nil {
		t.Fatalf("a submitted replacement must repair a corrupt secret: %v", err)
	}
	if !snap.SecretPresent["clientSecret"] {
		t.Error("replacement not counted as present")
	}
}

func TestBuildValidationSnapshot_RecordListElementSecrets(t *testing.T) {
	withEncryptionKey(t)
	ct, _ := encryptSecret("x")
	values := map[string]string{"profiles.__items": "a,b", "profiles.a.host": "h"}
	stored := map[string]string{"profiles.a.password": ct}
	snap, err := buildValidationSnapshot(snapshotSchema, "production", values, stored, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.SecretPresent["profiles.a.password"] || snap.SecretPresent["profiles.b.password"] {
		t.Errorf("element secrets: %v", snap.SecretPresent)
	}
	// An item Default counts for a roster element (runtime resolves it) and
	// for nothing else.
	if !snap.SecretPresent["profiles.a.token"] || !snap.SecretPresent["profiles.b.token"] {
		t.Errorf("item secret defaults not applied to roster elements: %v", snap.SecretPresent)
	}
	if _, listed := snap.SecretPresent["profiles.zzz.token"]; listed {
		t.Error("a non-roster element's secret must not be listed")
	}
	stored["profiles.zzz.token"] = ct
	snap, err = buildValidationSnapshot(snapshotSchema, "production", values, stored, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.SecretPresent["profiles.zzz.token"] {
		t.Error("a stored ciphertext is presence even for a non-roster key (it is a stored value, listed by name)")
	}
}

// --- dispatch ---

type snapshotModule struct {
	BaseModule
	saw *ConfigValidationSnapshot
	err error
}

func (m *snapshotModule) Name() string             { return "snap" }
func (m *snapshotModule) Init(*Dependencies) error { return nil }
func (m *snapshotModule) ValidateConfigSnapshot(_ context.Context, s ConfigValidationSnapshot) error {
	m.saw = &s
	return m.err
}

// A snapshot module that ALSO implements the legacy hooks must be judged
// through the snapshot alone.
func (m *snapshotModule) ValidateConfig(context.Context, map[string]string) error {
	return errors.New("legacy PATCH hook must not be called on a snapshot module")
}
func (m *snapshotModule) ValidateConfigActivation(context.Context, map[string]string) error {
	return errors.New("legacy activation hook must not be called on a snapshot module")
}

func TestValidateCandidate_Dispatch(t *testing.T) {
	withEncryptionKey(t)
	ctx := context.Background()
	// activationValidatingModule embeds validatingModule, so both answer
	// Name() == "validating"; each gets its own service so the lookup is
	// unambiguous.
	sm := &snapshotModule{}
	vm := &validatingModule{}
	avm := &activationValidatingModule{}
	svcSnap := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svcSnap.RegisterKnownModules([]Module{sm})
	svcPatch := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svcPatch.RegisterKnownModules([]Module{vm})
	svcAct := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svcAct.RegisterKnownModules([]Module{avm})

	patch := candidate{schema: snapshotSchema, env: "sandbox", values: map[string]string{"strict": "bad", "mode": "bad"}}
	activation := patch
	activation.activation = true

	// Snapshot module: judged through the snapshot on PATCH and on
	// activation, sees the target env, and its legacy hooks are never called.
	if err := svcSnap.validateCandidate(ctx, "snap", patch); err != nil || sm.saw == nil || sm.saw.Environment != "sandbox" {
		t.Fatalf("snapshot dispatch on PATCH: err=%v saw=%+v", err, sm.saw)
	}
	sm.saw = nil
	if err := svcSnap.validateCandidate(ctx, "snap", activation); err != nil || sm.saw == nil {
		t.Fatalf("snapshot dispatch on activation: err=%v saw=%+v", err, sm.saw)
	}

	// Legacy PATCH hook still sees Values and still rejects on PATCH …
	var typed *ConfigValidationError
	if err := svcPatch.validateCandidate(ctx, "validating", patch); !errors.As(err, &typed) || typed.Field != "strict" {
		t.Errorf("legacy PATCH dispatch: %v", err)
	}
	// … and is NOT consulted on activation (legacy-recovery behaviour).
	if err := svcPatch.validateCandidate(ctx, "validating", activation); err != nil {
		t.Errorf("a module with only HasConfigValidator must activate unconditionally: %v", err)
	}

	// Legacy activation hook still runs on activation, with the raw map.
	if err := svcAct.validateCandidate(ctx, "validating", activation); !errors.As(err, &typed) || typed.Code != "x.mode_invalid" {
		t.Errorf("legacy activation dispatch: %v", err)
	}

	// Unknown module: accepted.
	if err := svcSnap.validateCandidate(ctx, "unknown", patch); err != nil {
		t.Errorf("unknown module: %v", err)
	}
}

// Decryption happens only for a module that asks for the snapshot: a legacy
// module with a corrupt stored secret must remain editable.
func TestValidateCandidate_LegacyModuleNeverDecrypts(t *testing.T) {
	withEncryptionKey(t)
	svc := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{&validatingModule{}})
	c := candidate{schema: snapshotSchema, values: map[string]string{"strict": "good"}, storedEncrypted: map[string]string{"clientSecret": "corrupt"}}
	if err := svc.validateCandidate(context.Background(), "validating", c); err != nil {
		t.Fatalf("legacy module must not be blocked by undecryptable secrets: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd backend && go test ./pkg/sdk/module/ -run 'Snapshot|ValidateCandidate' -count=1`
Expected: compile errors — `undefined: buildValidationSnapshot`, `undefined: candidate`, `svc.validateCandidate undefined`.

- [ ] **Step 4: Implement the builder and the dispatch**

Create `config_snapshot.go`:

```go
package module

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
)

// ErrConfigSecretUnreadable reports a stored secret in the target profile
// that cannot be decrypted. The mutation aborts rather than guessing whether
// the credential exists; a request that submits a replacement for that key
// is judged on the replacement and can repair the ciphertext in one PATCH.
var ErrConfigSecretUnreadable = errors.New("module: stored secret cannot be decrypted")

// buildValidationSnapshot assembles the snapshot for one target profile.
// values is the raw merged non-secret map that would be written;
// storedEncrypted is the target profile's ciphertext BEFORE this request;
// submittedSecrets is this request's plaintext secrets (nil for activation).
// Plaintext is consulted only to decide presence and never stored on the
// result. Pure apart from reading EnvVar fallbacks and decrypting stored
// ciphertext.
func buildValidationSnapshot(
	schema []ConfigField, env string,
	values, storedEncrypted, submittedSecrets map[string]string,
) (ConfigValidationSnapshot, error) {
	snap := ConfigValidationSnapshot{
		Environment:     env,
		Values:          mergeStringMaps(values, nil),
		EffectiveValues: effectiveValues(schema, values),
		SecretPresent:   map[string]bool{},
	}
	for _, key := range secretKeys(schema, values, storedEncrypted, submittedSecrets) {
		present, err := secretPresent(schema, values, key, storedEncrypted, submittedSecrets)
		if err != nil {
			return ConfigValidationSnapshot{}, err
		}
		snap.SecretPresent[key] = present
	}
	return snap, nil
}

// effectiveValues copies values and applies the fallback the runtime applies
// (config_unmarshal.go resolveValue / assignRecordList): a non-secret scalar
// field whose stored value is empty or absent takes EnvVar, then Default;
// every ROSTER element's non-secret sub-field takes the item Default (items
// have no EnvVar by construction). Keys the schema does not declare are
// copied verbatim; secrets never appear.
func effectiveValues(schema []ConfigField, values map[string]string) map[string]string {
	out := mergeStringMaps(values, nil)
	for _, f := range schema {
		switch f.Type {
		case FieldSecret:
			continue
		case FieldRecordList:
			for _, slug := range ParseRoster(values, f.Key) {
				for _, it := range f.Items {
					if it.Type == FieldSecret {
						continue
					}
					key := ItemKey(f.Key, slug, it.Key)
					if out[key] == "" && it.Default != "" {
						out[key] = it.Default
					}
				}
			}
		default:
			if out[f.Key] != "" {
				continue
			}
			if v := schemaFallbackValue(f); v != "" {
				out[f.Key] = v
			}
		}
	}
	return out
}

// schemaFallbackValue is the EnvVar-then-Default rule for one field — the
// same rule ModuleConfigService.schemaFallback and buildInitialConfig apply.
func schemaFallbackValue(f ConfigField) string {
	if f.EnvVar != "" {
		if v := os.Getenv(f.EnvVar); v != "" {
			return v
		}
	}
	return f.Default
}

// secretKeys is the union of every declared secret (scalar fields plus each
// roster element's secret sub-fields) and every key carrying a stored or
// submitted secret, sorted for deterministic output.
func secretKeys(schema []ConfigField, values, stored, submitted map[string]string) []string {
	set := map[string]bool{}
	for _, f := range schema {
		switch f.Type {
		case FieldSecret:
			set[f.Key] = true
		case FieldRecordList:
			for _, slug := range ParseRoster(values, f.Key) {
				for _, it := range f.Items {
					if it.Type == FieldSecret {
						set[ItemKey(f.Key, slug, it.Key)] = true
					}
				}
			}
		}
	}
	for k := range stored {
		set[k] = true
	}
	for k := range submitted {
		set[k] = true
	}
	return sortedKeys(set)
}

// secretPresent decides one key. Precedence: submitted → stored ciphertext →
// schema fallback — the order resolveValue uses at runtime, with the request
// layered on top. A submitted value wins even when empty (the request is
// clearing the key, and an empty secret is not presence), and it is consulted
// BEFORE the stored ciphertext so a corrupt secret can be replaced. A stored
// ciphertext that decrypts to "" is "" — like the runtime, it does NOT fall
// through to the schema default.
func secretPresent(schema []ConfigField, values map[string]string, key string, stored, submitted map[string]string) (bool, error) {
	if v, ok := submitted[key]; ok {
		if v != "" {
			return true, nil
		}
		return secretFallbackPresent(schema, values, key), nil
	}
	if enc, ok := stored[key]; ok && enc != "" {
		plain, err := decryptSecret(enc)
		if err != nil {
			return false, fmt.Errorf("%w: %q", ErrConfigSecretUnreadable, key)
		}
		return plain != "", nil
	}
	return secretFallbackPresent(schema, values, key), nil
}

// secretFallbackPresent mirrors the runtime's default for a secret with no
// stored value: a top-level secret's EnvVar/Default, or — for a key under a
// ROSTER element — the item's Default (an element outside the roster is never
// loaded, so its default counts for nothing).
func secretFallbackPresent(schema []ConfigField, values map[string]string, key string) bool {
	for _, f := range schema {
		switch f.Type {
		case FieldSecret:
			if f.Key == key {
				return schemaFallbackValue(f) != ""
			}
		case FieldRecordList:
			slug, sub, ok := SplitElementKey(f.Key, key)
			if !ok {
				continue
			}
			inRoster := false
			for _, r := range ParseRoster(values, f.Key) {
				if r == slug {
					inRoster = true
					break
				}
			}
			if !inRoster {
				return false
			}
			for _, it := range f.Items {
				if it.Key == sub && it.Type == FieldSecret {
					return it.Default != ""
				}
			}
			return false
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// candidate is one mutation's target as the service sees it before
// encryption or persistence. It carries what a snapshot needs; the snapshot
// itself is built only when the module asks for one, so a legacy module with
// an undecryptable stored secret stays editable.
type candidate struct {
	schema           []ConfigField
	env              string
	values           map[string]string // raw merged non-secret target
	storedEncrypted  map[string]string // target profile's ciphertext before this request
	submittedSecrets map[string]string // this request's plaintext secrets; nil on activation
	activation       bool
}

// validateCandidate runs the module's validation seam against exactly what
// would be written. Dispatch is source-compatible: a module implementing
// HasConfigSnapshotValidator is judged through it on every surface;
// otherwise a PATCH goes through HasConfigValidator and an activation
// through HasConfigActivationValidator, both with the raw merged map, as
// before. Modules unknown to this service, or without any seam, are
// accepted unchanged.
func (s *ModuleConfigService) validateCandidate(ctx context.Context, name string, c candidate) error {
	m, ok := s.knownModules[name]
	if !ok {
		return nil
	}
	if v, ok := m.(HasConfigSnapshotValidator); ok {
		snap, err := buildValidationSnapshot(c.schema, c.env, c.values, c.storedEncrypted, c.submittedSecrets)
		if err != nil {
			return err
		}
		return v.ValidateConfigSnapshot(ctx, snap)
	}
	values := c.values
	if values == nil {
		values = map[string]string{}
	}
	if c.activation {
		if v, ok := m.(HasConfigActivationValidator); ok {
			return v.ValidateConfigActivation(ctx, values)
		}
		return nil
	}
	if v, ok := m.(HasConfigValidator); ok {
		return v.ValidateConfig(ctx, values)
	}
	return nil
}
```

Delete `validateModuleConfig` from `config_service.go` (lines 382-396) and change its two callers for now: `config_service.go:423` and `:501` become `s.validateCandidate(ctx, name, candidate{schema: existing.ConfigSchema, env: existing.ActiveEnv(), values: mergedValues, storedEncrypted: existing.ActiveEncryptedValues(), submittedSecrets: secrets})` / the env-scoped equivalent; `recordlist_mutation.go:137` becomes `s.validateCandidate(ctx, name, candidate{schema: doc.ConfigSchema, env: envName, values: next.ConfigValues, storedEncrypted: next.EncryptedValues, submittedSecrets: secrets})`; the activation call at `config_service.go:555-561` becomes `s.validateCandidate(ctx, name, candidate{schema: doc.ConfigSchema, env: envName, values: cv, storedEncrypted: doc.Environments[envName].EncryptedValues, activation: true})`. (Task 3 rewrites these paths wholesale; this keeps the package compiling and the existing tests green in between.)

- [ ] **Step 5: Run the tests**

Run: `cd backend && go test ./pkg/sdk/module/ -count=1`
Expected: PASS, including the pre-existing `config_validator_test.go` live tests when Mongo is reachable (they skip otherwise).

- [ ] **Step 6: Commit**

```bash
cd backend && go vet ./pkg/sdk/... && cd ..
git add backend/pkg/sdk/module/config_validator.go backend/pkg/sdk/module/config_snapshot.go \
  backend/pkg/sdk/module/config_snapshot_test.go backend/pkg/sdk/module/config_service.go \
  backend/pkg/sdk/module/recordlist_mutation.go
git commit -m "feat(sdk): HasConfigSnapshotValidator + target-profile validation snapshot

One optional seam sees the complete target snapshot — raw merged values,
runtime-effective values, and secret presence computed from the TARGET
profile's own ciphertext/submitted secrets/schema fallback — on every
mutation surface. Modules without it keep HasConfigValidator and
HasConfigActivationValidator dispatch unchanged. No plaintext crosses the
validator boundary; a corrupt stored secret aborts unless the request
replaces it.

Refs: spec §4.5"
```

### Task 3: Migrate the mutation paths to snapshot + CAS; `needsRestart` in the same write

**Files:**
- Modify: `backend/pkg/sdk/module/config_service.go` (`ensureEnvironments`, `UpdateConfig`, `UpdateEnvironmentConfig`, `SetActiveEnvironment`, `SetHotReloadResolver`, `needsRestartFor`, `encryptAll`)
- Modify: `backend/pkg/sdk/module/recordlist_mutation.go:141` (resolver value into the CAS)
- Modify: `backend/pkg/sdk/module/registry.go:157-159` (install the resolver before seeding)
- Modify: `backend/pkg/sdk/module/config_repository.go` (`MigrateToEnvironments` becomes a CAS; delete `UpdateConfigValues`, `UpdateEnvironmentConfig`, `SetActiveEnvironment`, `ActivateEnvironment`)
- Modify: `backend/pkg/sdk/module/config_repository_iface.go` (same four removed)
- Modify: `backend/pkg/sdk/module/recordlist_fake_repo_test.go` (same four removed)
- Modify: `backend/pkg/sdk/module/config_error_envelope.go` (`ErrRevisionStale` → 409 envelope)
- Modify: `backend/pkg/sdk/module/handler.go:303-306, 438-440, 490-493` (no `ClearNeedsRestart` after writes)
- Create: `backend/pkg/sdk/module/config_service_cas_test.go`
- Modify: `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/config-service.mdx`

**Interfaces:**
- Consumes: Task 1 `ConfigMutation`/`CompareAndSwapConfig`; Task 2 `candidate`/`validateCandidate`.
- Produces: `ConfigRepository.MigrateToEnvironments(ctx, name string, configValues, encryptedValues map[string]string, expectedRevision int64) (bool, error)`; fake field `beforeMigrate func()`; `func (s *ModuleConfigService) SetHotReloadResolver(fn func(name string) bool)`; `func (s *ModuleConfigService) needsRestartFor(name string) bool`; `func encryptAll(secrets map[string]string) (map[string]string, error)`; unchanged public signatures of the three mutation methods (external callers `internal/shared/setup/finalize.go:541`, `internal/core/tenant/reconcile.go:401,410` keep compiling).
- Consumed by: Task 5 (backfill CAS), Task 6 (handler ordering relies on `UpdateConfig` being validate-then-CAS).

- [ ] **Step 1: Write the failing service tests**

Create `config_service_cas_test.go`:

```go
package module

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// invariantModule models PR 3's anti-lockout rule in miniature:
// password may be "false" only while provider is "true" AND the provider
// secret is present in the TARGET profile. It exists to prove the race and
// the target-secret rules end to end; the real rule lives in auth.
type invariantModule struct{ BaseModule }

func (invariantModule) Name() string             { return "inv" }
func (invariantModule) Init(*Dependencies) error { return nil }
func (invariantModule) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "password", Type: FieldBool, Default: "true"},
		{Key: "provider", Type: FieldBool, Default: "false"},
		{Key: "providerSecret", Type: FieldSecret},
		{Key: "extraSecret", Type: FieldSecret},
	}
}
func (invariantModule) HotReloadConfig() bool { return true }
func (invariantModule) ValidateConfigSnapshot(_ context.Context, s ConfigValidationSnapshot) error {
	if s.Values["password"] == "false" && !(s.EffectiveValues["provider"] == "true" && s.SecretPresent["providerSecret"]) {
		return &ConfigValidationError{Field: "password", Message: "would lock the surface out", Code: "x.lockout"}
	}
	return nil
}

func invDoc(ct string) *ModuleConfig {
	return &ModuleConfig{
		ModuleName: "inv", ActiveEnvironment: "production",
		ConfigSchema:    invariantModule{}.ConfigSchema(),
		ConfigValues:    map[string]string{"password": "true", "provider": "true"},
		EncryptedValues: map[string]string{"providerSecret": ct},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"password": "true", "provider": "true"}, EncryptedValues: map[string]string{"providerSecret": ct}, Revision: 2},
			"sandbox":    {ConfigValues: map[string]string{"password": "true", "provider": "true"}, EncryptedValues: map[string]string{}, Revision: 0},
		},
		ConfigRevision: 3,
	}
}

func newInvService(t *testing.T) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	withEncryptionKey(t)
	ct, err := encryptSecret("shh")
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeConfigRepo()
	repo.docs["inv"] = invDoc(ct)
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{invariantModule{}})
	svc.SetHotReloadResolver(func(string) bool { return true })
	return svc, repo
}

func TestUpdateConfig_AtomicProfileAndMirror(t *testing.T) {
	svc, repo := newInvService(t)
	if err := svc.UpdateConfig(context.Background(), "inv", map[string]string{"password": "false"}, map[string]string{"extraSecret": "v"}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	doc := repo.docs["inv"]
	prod := doc.Environments["production"]
	if prod.ConfigValues["password"] != "false" || doc.ConfigValues["password"] != "false" {
		t.Errorf("profile/mirror diverged: %v / %v", prod.ConfigValues, doc.ConfigValues)
	}
	if prod.EncryptedValues["extraSecret"] == "" || prod.EncryptedValues["extraSecret"] != doc.EncryptedValues["extraSecret"] {
		t.Error("secret not written identically to profile and mirror")
	}
	if prod.EncryptedValues["providerSecret"] == "" {
		t.Error("a config write wiped a secret it did not mention")
	}
	if doc.ConfigRevision != 4 || prod.Revision != 3 {
		t.Errorf("revisions: doc=%d env=%d, want 4/3", doc.ConfigRevision, prod.Revision)
	}
	if doc.NeedsRestart {
		t.Error("hot-reloadable module must persist needsRestart=false in the same write")
	}
	if repo.docCasCalls != 1 {
		t.Errorf("docCasCalls = %d, want exactly one write", repo.docCasCalls)
	}
}

func TestUpdateConfig_NeedsRestartWithoutResolverOrForColdModule(t *testing.T) {
	svc, repo := newInvService(t)
	svc.SetHotReloadResolver(nil)
	_ = svc.UpdateConfig(context.Background(), "inv", map[string]string{"provider": "true"}, nil)
	if !repo.docs["inv"].NeedsRestart {
		t.Error("without a resolver every write must mark needsRestart (pre-existing behaviour)")
	}
	svc.SetHotReloadResolver(func(string) bool { return false })
	_ = svc.UpdateConfig(context.Background(), "inv", map[string]string{"provider": "true"}, nil)
	if !repo.docs["inv"].NeedsRestart {
		t.Error("a module that does not hot-reload must mark needsRestart")
	}
}

// Two operators read revision 3. A disables password (valid: provider on).
// B disables the provider (valid on its own read). B's CAS lands after A's
// write: it must lose with ErrRevisionStale, and B's retry against the
// reloaded document must fail the invariant — two individually valid
// snapshots never combine into an invalid one.
func TestUpdateConfig_ConcurrentWritersCannotSkew(t *testing.T) {
	svc, repo := newInvService(t)
	ctx := context.Background()
	fired := false
	repo.beforeDocCAS = func() {
		if fired {
			return
		}
		fired = true
		// Writer A lands inside B's window. Detach the hook so A's own CAS
		// does not recurse.
		hook := repo.beforeDocCAS
		repo.beforeDocCAS = nil
		if err := svc.UpdateConfig(ctx, "inv", map[string]string{"password": "false"}, nil); err != nil {
			t.Fatalf("writer A: %v", err)
		}
		repo.beforeDocCAS = hook
	}
	err := svc.UpdateConfig(ctx, "inv", map[string]string{"provider": "false"}, nil)
	if !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("writer B: err = %v, want ErrRevisionStale", err)
	}
	if repo.docs["inv"].ConfigValues["provider"] != "true" {
		t.Fatal("the loser's write reached the document")
	}
	// B reloads (UpdateConfig re-reads) and retries the same intent.
	repo.beforeDocCAS = nil
	err = svc.UpdateConfig(ctx, "inv", map[string]string{"provider": "false"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) || typed.Code != "x.lockout" {
		t.Fatalf("retry must fail the invariant: %v", err)
	}
}

func TestUpdateEnvironmentConfig_InactiveProfileLeavesMirrorAlone(t *testing.T) {
	svc, repo := newInvService(t)
	err := svc.UpdateEnvironmentConfig(context.Background(), "inv", "sandbox", map[string]string{"provider": "false"}, nil)
	if err != nil {
		t.Fatalf("inactive-profile PATCH: %v", err)
	}
	doc := repo.docs["inv"]
	if doc.Environments["sandbox"].ConfigValues["provider"] != "false" {
		t.Error("sandbox not written")
	}
	if doc.ConfigValues["provider"] != "true" || doc.Environments["production"].ConfigValues["provider"] != "true" {
		t.Error("an inactive-profile write must not touch the mirror or the active profile")
	}
	if doc.ConfigRevision != 4 || doc.Environments["sandbox"].Revision != 1 {
		t.Errorf("revisions: doc=%d sandbox=%d", doc.ConfigRevision, doc.Environments["sandbox"].Revision)
	}
	// Active-profile PATCH through the same method syncs the mirror.
	if err := svc.UpdateEnvironmentConfig(context.Background(), "inv", "production", map[string]string{"provider": "true"}, map[string]string{"providerSecret": "new"}); err != nil {
		t.Fatal(err)
	}
	if repo.docs["inv"].EncryptedValues["providerSecret"] != repo.docs["inv"].Environments["production"].EncryptedValues["providerSecret"] {
		t.Error("active-profile write did not sync the legacy mirror")
	}
}

// The validator judges the TARGET's own secrets: sandbox has no provider
// secret, so activating it while its password is off must be refused even
// though the active production profile does hold one. Submitting the
// sandbox secret plus the flip in one PATCH is accepted atomically.
func TestSnapshot_TargetSecretsNotActiveSecrets(t *testing.T) {
	svc, repo := newInvService(t)
	ctx := context.Background()
	err := svc.UpdateEnvironmentConfig(ctx, "inv", "sandbox", map[string]string{"password": "false"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) || typed.Code != "x.lockout" {
		t.Fatalf("sandbox without its own secret must be refused: %v", err)
	}
	if err := svc.UpdateEnvironmentConfig(ctx, "inv", "sandbox", map[string]string{"password": "false"}, map[string]string{"providerSecret": "sb"}); err != nil {
		t.Fatalf("secret + flip in one PATCH must be accepted: %v", err)
	}
	if repo.docs["inv"].Environments["sandbox"].EncryptedValues["providerSecret"] == "" {
		t.Error("submitted secret not persisted with the flip")
	}
	// Activation judges the stored target: sandbox is now valid → allowed;
	// a profile made invalid out of band is refused and nothing moves.
	if err := svc.SetActiveEnvironment(ctx, "inv", "sandbox"); err != nil {
		t.Fatalf("activating a valid sandbox: %v", err)
	}
	if repo.docs["inv"].ActiveEnvironment != "sandbox" || repo.docs["inv"].NeedsRestart {
		t.Errorf("activation state: active=%q needsRestart=%v", repo.docs["inv"].ActiveEnvironment, repo.docs["inv"].NeedsRestart)
	}
	env := repo.docs["inv"].Environments["production"]
	delete(env.EncryptedValues, "providerSecret")
	env.ConfigValues["password"] = "false"
	repo.docs["inv"].Environments["production"] = env
	rev := repo.docs["inv"].ConfigRevision
	if err := svc.SetActiveEnvironment(ctx, "inv", "production"); !errors.As(err, &typed) {
		t.Fatalf("activating an invalid production profile must be refused: %v", err)
	}
	if repo.docs["inv"].ActiveEnvironment != "sandbox" || repo.docs["inv"].ConfigRevision != rev {
		t.Error("a refused activation moved state")
	}
}

// failingRedisClient models a Redis outage: every Del errors.
type failingRedisClient struct{ fakeRedisClient }

func (failingRedisClient) Del(context.Context, ...string) error { return errors.New("redis down") }

// A committed compare-and-swap is a success, whatever Redis does afterwards:
// the cache holds only the enabled flag, which these writes never change.
func TestConfigWrites_DoNotReportRedisFailures(t *testing.T) {
	withEncryptionKey(t)
	ct, _ := encryptSecret("shh")
	repo := newFakeConfigRepo()
	repo.docs["inv"] = invDoc(ct)
	svc := NewModuleConfigService(repo, failingRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{invariantModule{}})
	ctx := context.Background()
	if err := svc.UpdateConfig(ctx, "inv", map[string]string{"provider": "true"}, nil); err != nil {
		t.Errorf("UpdateConfig reported a Redis failure after committing: %v", err)
	}
	if err := svc.UpdateEnvironmentConfig(ctx, "inv", "sandbox", map[string]string{"provider": "true"}, nil); err != nil {
		t.Errorf("UpdateEnvironmentConfig reported a Redis failure after committing: %v", err)
	}
	if err := svc.SetActiveEnvironment(ctx, "inv", "production"); err != nil {
		t.Errorf("SetActiveEnvironment reported a Redis failure after committing: %v", err)
	}
	if repo.docs["inv"].ConfigRevision != 6 {
		t.Errorf("configRevision = %d, want 6 — the three writes must all have landed", repo.docs["inv"].ConfigRevision)
	}
	// The enabled flag IS cached; its invalidation is best-effort.
	if err := svc.UpdateEnabled(ctx, "inv", false); err != nil {
		t.Errorf("UpdateEnabled reported a Redis failure after persisting: %v", err)
	}
}

func TestSetActiveEnvironment_StaleRevision(t *testing.T) {
	svc, repo := newInvService(t)
	repo.docCasFailures = 1
	if err := svc.SetActiveEnvironment(context.Background(), "inv", "sandbox"); !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("err = %v, want ErrRevisionStale", err)
	}
}

// A legacy document (no profiles) is migrated first, then written to both
// the new production profile and the mirror — one source of truth.
func TestUpdateConfig_LegacyDocumentMigratesFirst(t *testing.T) {
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["plain"] = &ModuleConfig{ModuleName: "plain", ConfigValues: map[string]string{"a": "old"}, EncryptedValues: map[string]string{}}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{plainModule{}})
	if err := svc.UpdateConfig(context.Background(), "plain", map[string]string{"a": "new"}, nil); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["plain"]
	if doc.ActiveEnv() != "production" || doc.Environments["production"].ConfigValues["a"] != "new" || doc.ConfigValues["a"] != "new" {
		t.Errorf("legacy document not migrated + written: %+v", doc)
	}
	if doc.ConfigRevision != 2 {
		t.Errorf("configRevision = %d, want 2 (migration + write)", doc.ConfigRevision)
	}
}

// Two writers both read a legacy document. A migrates and writes. B's
// migration must LOSE (the document now has profiles) rather than re-copy
// B's stale legacy snapshot over A's freshly written profile; B then re-reads
// and its write lands on top of A's — both keys survive, revision 3.
func TestUpdateConfig_ConcurrentLegacyMigrationsCannotClobber(t *testing.T) {
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["plain"] = &ModuleConfig{ModuleName: "plain", ConfigValues: map[string]string{"a": "old", "b": "old"}, EncryptedValues: map[string]string{}}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{plainModule{}})
	ctx := context.Background()
	repo.beforeMigrate = func() {
		// Writer A lands inside B's migration window (the fake detaches the
		// hook before calling it, so A's own migration does not recurse).
		if err := svc.UpdateConfig(ctx, "plain", map[string]string{"a": "A"}, nil); err != nil {
			t.Fatalf("writer A: %v", err)
		}
	}
	if err := svc.UpdateConfig(ctx, "plain", map[string]string{"b": "B"}, nil); err != nil {
		t.Fatalf("writer B: %v", err)
	}
	doc := repo.docs["plain"]
	prod := doc.Environments["production"].ConfigValues
	if prod["a"] != "A" || prod["b"] != "B" || doc.ConfigValues["a"] != "A" || doc.ConfigValues["b"] != "B" {
		t.Errorf("B's stale migration clobbered A: profile=%v mirror=%v", prod, doc.ConfigValues)
	}
	if doc.ConfigRevision != 3 {
		t.Errorf("configRevision = %d, want 3 (migration, A, B)", doc.ConfigRevision)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./pkg/sdk/module/ -run 'UpdateConfig_|UpdateEnvironmentConfig_|TargetSecrets|SetActiveEnvironment_Stale' -count=1`
Expected: compile errors `svc.SetHotReloadResolver undefined`, `repo.beforeMigrate undefined`; after stubbing, `docCasCalls = 0`, `NeedsRestart` assertions fail, `ErrRevisionStale` not returned, the clobber test sees `a=old`.

- [ ] **Step 3: Rewrite the three service paths**

In `config_service.go`, add to the struct and constructor:

```go
	// hotReload answers "does this module re-read its config at request
	// time" — installed by the registry from SupportsHotReload. Every
	// config/environment/activation write persists needsRestart =
	// !hotReload(name) in the same update as the values, so the flag can
	// never diverge through a later best-effort clear. Nil means every
	// write marks needsRestart (the pre-resolver behaviour).
	hotReload func(name string) bool
```

```go
// SetHotReloadResolver installs the registry's hot-reload answer. See the
// hotReload field.
func (s *ModuleConfigService) SetHotReloadResolver(fn func(name string) bool) { s.hotReload = fn }

func (s *ModuleConfigService) needsRestartFor(name string) bool {
	if s.hotReload == nil {
		return true
	}
	return !s.hotReload(name)
}

// encryptAll encrypts every submitted secret; an empty plaintext encrypts to
// "" (clearing the key), which GetSecret then reads as "fall back".
func encryptAll(secrets map[string]string) (map[string]string, error) {
	encrypted := make(map[string]string, len(secrets))
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return nil, fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		encrypted[k] = enc
	}
	return encrypted, nil
}
```

Replace `UpdateConfig` (lines 398-460) with:

```go
// UpdateConfig merges values and secrets into the ACTIVE environment profile
// and mirrors the result into the legacy top-level fields, in one
// compare-and-swap write. Keys not present in the call are preserved, never
// wiped — a toggle flip carries no secrets and must not blank the module's
// credentials.
//
// Profiles are the source of truth; the legacy maps are a compatibility
// mirror. The merge, the validation snapshot and the write all use the
// active profile, so a pre-existing divergence between profile and mirror
// is repaired by the write rather than perpetuated. A legacy document with
// no profiles completes its lazy migration first — itself a compare-and-swap
// — so the revision the write is judged against is the migrated document's.
func (s *ModuleConfigService) UpdateConfig(ctx context.Context, name string, values map[string]string, secrets map[string]string) error {
	doc, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}
	// ensureEnvironments migrates under its own compare-and-swap and leaves
	// doc current — profiles, activeEnvironment and configRevision — whether
	// this call won the migration or re-read after another writer did.
	if err := s.ensureEnvironments(ctx, doc); err != nil {
		return err
	}
	env := doc.ActiveEnv()
	cur, ok := doc.Environments[env]
	if !ok {
		return fmt.Errorf("environment %q not found for module %q", env, name)
	}
	mergedValues := mergeStringMaps(cur.ConfigValues, values)

	if err := s.validateCandidate(ctx, name, candidate{
		schema: doc.ConfigSchema, env: env, values: mergedValues,
		storedEncrypted: cur.EncryptedValues, submittedSecrets: secrets,
	}); err != nil {
		return err
	}

	encrypted, err := encryptAll(secrets)
	if err != nil {
		return err
	}
	mergedSecrets := mergeStringMaps(cur.EncryptedValues, encrypted)

	won, err := s.repo.CompareAndSwapConfig(ctx, name, ConfigMutation{
		ExpectedRevision: doc.ConfigRevision,
		Env: env, EnvValues: mergedValues, EnvSecrets: mergedSecrets, EnvRevision: cur.Revision,
		WriteLegacy: true, LegacyValues: mergedValues, LegacySecrets: mergedSecrets,
		NeedsRestart: s.needsRestartFor(name),
	})
	if err != nil {
		return err
	}
	if !won {
		return ErrRevisionStale
	}
	// No cache invalidation: Redis caches only the enabled flag, which a
	// config write does not change. The CAS is the commit; nothing after it
	// may turn a committed write into a reported failure.
	return nil
}
```

Replace `UpdateEnvironmentConfig` (lines 478-529) with:

```go
// UpdateEnvironmentConfig merges values and secrets into ONE named profile
// in one compare-and-swap write. When that profile is the active one the
// legacy mirror is synced in the same update; otherwise the mirror and the
// active profile are untouched. The module's validation seam sees the
// merged target profile — this surface must not be a bypass around the
// active-config PATCH.
func (s *ModuleConfigService) UpdateEnvironmentConfig(ctx context.Context, name, envName string, values map[string]string, secrets map[string]string) error {
	doc, err := s.GetConfig(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}
	cur, ok := doc.Environments[envName]
	if !ok {
		return fmt.Errorf("environment %q not found for module %q", envName, name)
	}
	mergedValues := mergeStringMaps(cur.ConfigValues, values)

	if err := s.validateCandidate(ctx, name, candidate{
		schema: doc.ConfigSchema, env: envName, values: mergedValues,
		storedEncrypted: cur.EncryptedValues, submittedSecrets: secrets,
	}); err != nil {
		return err
	}

	encrypted, err := encryptAll(secrets)
	if err != nil {
		return err
	}
	mergedSecrets := mergeStringMaps(cur.EncryptedValues, encrypted)

	mut := ConfigMutation{
		ExpectedRevision: doc.ConfigRevision,
		Env: envName, EnvValues: mergedValues, EnvSecrets: mergedSecrets, EnvRevision: cur.Revision,
		NeedsRestart: s.needsRestartFor(name),
	}
	if envName == doc.ActiveEnv() {
		mut.WriteLegacy, mut.LegacyValues, mut.LegacySecrets = true, mergedValues, mergedSecrets
	}
	won, err := s.repo.CompareAndSwapConfig(ctx, name, mut)
	if err != nil {
		return err
	}
	if !won {
		return ErrRevisionStale
	}
	return nil
}
```

Replace `SetActiveEnvironment` (lines 531-574) with:

```go
// SetActiveEnvironment switches the active profile in one compare-and-swap
// pipeline update that also copies the target's STORED values and secrets
// into the legacy mirror server-side. The module's validation seam judges
// the stored target profile as a whole — with the target's own secret
// presence, never the currently active profile's — strictly before the
// write, so a refused activation leaves the active name, the mirror and
// needsRestart exactly as they were.
func (s *ModuleConfigService) SetActiveEnvironment(ctx context.Context, name, envName string) error {
	doc, err := s.GetConfig(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}
	target, ok := doc.Environments[envName]
	if !ok {
		return fmt.Errorf("environment %q not found for module %q", envName, name)
	}
	cv := target.ConfigValues
	if cv == nil {
		cv = make(map[string]string)
	}
	if err := s.validateCandidate(ctx, name, candidate{
		schema: doc.ConfigSchema, env: envName, values: cv,
		storedEncrypted: target.EncryptedValues, activation: true,
	}); err != nil {
		return err
	}
	won, err := s.repo.CompareAndSwapConfig(ctx, name, ConfigMutation{
		ExpectedRevision: doc.ConfigRevision, Activate: envName,
		NeedsRestart: s.needsRestartFor(name),
	})
	if err != nil {
		return err
	}
	if !won {
		return ErrRevisionStale
	}
	return nil
}
```

In `recordlist_mutation.go:141-147` pass the resolver value and drop the invalidation — `won, err := s.repo.CompareAndSwapEnvironment(ctx, name, envName, cur.Revision, next, s.needsRestartFor(name))` and, on `won`, `return nil`. Replace `UpdateEnabled` (lines 620-629) with:

```go
// UpdateEnabled persists a module's enabled state. The Redis-cached enabled
// flag is invalidated best-effort: the persisted state is the truth, the
// ModuleGate self-corrects within the 30-second cache TTL, and a Redis
// hiccup must not report a committed write as a failure.
func (s *ModuleConfigService) UpdateEnabled(ctx context.Context, name string, enabled bool) error {
	if s.coreModules[name] {
		return fmt.Errorf("cannot disable core module %q", name)
	}
	if err := s.repo.UpdateEnabled(ctx, name, enabled); err != nil {
		return err
	}
	if err := s.InvalidateCache(ctx, name); err != nil {
		s.logger.Warn("UpdateEnabled: failed to invalidate the enabled-flag cache; the gate converges within the cache TTL",
			slog.String("module", name), slog.String("error", err.Error()))
	}
	return nil
}
```

In `registry.go`, inside `InitAll` before `SeedFromModules` (line 158):

```go
	if r.configService != nil {
		// Installed before seeding so the boot backfill (and every later
		// write) persists needsRestart from the module's own declaration.
		r.configService.SetHotReloadResolver(r.SupportsHotReload)
```

- [ ] **Step 3b: Put the legacy profile migration under the compare-and-swap**

Today `MigrateToEnvironments` is a plain `$set` of the caller's legacy-map snapshot, and every reader that saw a document without profiles runs it. Two concurrent `UpdateConfig`s on a legacy document would both migrate; the second would copy its stale snapshot over the profile the first had just written, without moving `configRevision`. The migration therefore becomes a CAS that matches only a document that **still has no profiles** at the read revision; a loser re-reads.

`config_repository.go` — replace `MigrateToEnvironments`:

```go
// MigrateToEnvironments copies the legacy top-level maps into a "production"
// profile (plus an empty "sandbox") and sets activeEnvironment, in one
// compare-and-swap that matches only while the document STILL has no
// profiles and its configRevision is the one the caller read. Returns
// (false, nil) when another writer migrated or moved the document first —
// the caller re-reads instead of copying a stale legacy snapshot over a
// profile that was just written. Advances configRevision.
func (r *ModuleConfigRepository) MigrateToEnvironments(
	ctx context.Context, name string, configValues, encryptedValues map[string]string, expectedRevision int64,
) (bool, error) {
	now := time.Now()
	noProfiles := bson.M{"$or": bson.A{
		bson.M{"environments": bson.M{"$exists": false}},
		bson.M{"environments": bson.M{}},
	}}
	revision := bson.M{"configRevision": expectedRevision}
	if expectedRevision == 0 {
		revision = bson.M{"$or": bson.A{
			bson.M{"configRevision": int64(0)},
			bson.M{"configRevision": bson.M{"$exists": false}},
		}}
	}
	filter := bson.M{"moduleName": name, "$and": bson.A{noProfiles, revision}}
	update := bson.M{"$set": bson.M{
		"activeEnvironment":       "production",
		"environments.production": EnvironmentConfig{ConfigValues: configValues, EncryptedValues: encryptedValues, UpdatedAt: now},
		"environments.sandbox":    EnvironmentConfig{ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}, UpdatedAt: now},
		"configRevision":          expectedRevision + 1,
		"updatedAt":               now,
	}}
	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("migrate to environments for %q: %w", name, err)
	}
	return res.MatchedCount == 1, nil
}
```

`config_repository_iface.go`: `MigrateToEnvironments(ctx context.Context, name string, configValues, encryptedValues map[string]string, expectedRevision int64) (bool, error)`.

`recordlist_fake_repo_test.go` — add `beforeMigrate func()` (doc: "runs inside MigrateToEnvironments before the no-profiles check — the window in which a concurrent writer migrates first") and `migrateErr error` (doc: "when set, MigrateToEnvironments fails with it") to the struct and replace the fake:

```go
func (f *fakeConfigRepo) MigrateToEnvironments(_ context.Context, name string, cv, ev map[string]string, expectedRevision int64) (bool, error) {
	if f.migrateErr != nil {
		return false, f.migrateErr
	}
	if f.beforeMigrate != nil {
		hook := f.beforeMigrate
		f.beforeMigrate = nil
		hook()
	}
	doc, ok := f.docs[name]
	if !ok {
		return false, fmt.Errorf("module %q not found", name)
	}
	if len(doc.Environments) > 0 || doc.ConfigRevision != expectedRevision {
		return false, nil
	}
	doc.ActiveEnvironment = "production"
	doc.Environments = map[string]EnvironmentConfig{
		"production": {ConfigValues: copyStrings(cv), EncryptedValues: copyStrings(ev)},
		"sandbox":    {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
	}
	doc.ConfigRevision = expectedRevision + 1
	return true, nil
}
```

`config_service.go` — replace `ensureEnvironments` (lines 266-295):

```go
// ensureEnvironments lazily migrates a legacy document (no Environments map)
// by copying the top-level maps into a "production" profile and creating an
// empty "sandbox" profile, under a compare-and-swap. On success doc is
// updated in memory to match what was written (profiles AND the advanced
// configRevision). If another writer migrated or moved the document first,
// doc is REPLACED by a fresh read so the caller judges its own write against
// the current document rather than a stale legacy snapshot.
func (s *ModuleConfigService) ensureEnvironments(ctx context.Context, doc *ModuleConfig) error {
	if len(doc.Environments) > 0 {
		return nil // already migrated
	}
	cv := doc.ConfigValues
	if cv == nil {
		cv = make(map[string]string)
	}
	ev := doc.EncryptedValues
	if ev == nil {
		ev = make(map[string]string)
	}
	won, err := s.repo.MigrateToEnvironments(ctx, doc.ModuleName, cv, ev, doc.ConfigRevision)
	if err != nil {
		return err
	}
	if won {
		now := time.Now()
		doc.ActiveEnvironment = "production"
		doc.Environments = map[string]EnvironmentConfig{
			"production": {ConfigValues: cv, EncryptedValues: ev, UpdatedAt: now},
			"sandbox":    {ConfigValues: make(map[string]string), EncryptedValues: make(map[string]string), UpdatedAt: now},
		}
		doc.ConfigRevision++
		return nil
	}
	fresh, err := s.repo.FindByName(ctx, doc.ModuleName)
	if err != nil {
		return err
	}
	if fresh == nil {
		return fmt.Errorf("module %q not found", doc.ModuleName)
	}
	if len(fresh.Environments) == 0 {
		// The revision moved without a migration (a legacy-only backfill
		// landed). Retryable by the caller; never loop here.
		return fmt.Errorf("module %q: %w (profile migration)", doc.ModuleName, ErrRevisionStale)
	}
	*doc = *fresh
	return nil
}
```

`GetConfig` (lines 256-262) today logs a failed migration and serves the unmigrated document. With the migration under the CAS, `ensureEnvironments` already absorbs the only expected non-error outcome (a lost race, by re-reading); what is left is a real failure, and a read must not paper over it:

```go
	if doc != nil {
		if err := s.ensureEnvironments(ctx, doc); err != nil {
			return nil, fmt.Errorf("module %q: migrate legacy config: %w", name, err)
		}
		return doc, nil
	}
```

(`ListConfigs`, written in Task 4, propagates the same way.) Add `migrateErr error` to the fake (returned by `MigrateToEnvironments` when set) and this test to `config_service_cas_test.go`:

```go
func TestGetConfig_MigrationFailureIsNotSwallowed(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["plain"] = &ModuleConfig{ModuleName: "plain", ConfigValues: map[string]string{"a": "old"}}
	repo.migrateErr = errors.New("write refused")
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{plainModule{}})
	if _, err := svc.GetConfig(context.Background(), "plain"); err == nil {
		t.Fatal("a failed legacy migration must surface, not be logged and served as if migrated")
	}
}
```

Run: `cd backend && go test ./pkg/sdk/module/ -run 'Legacy|Migrat' -count=1`
Expected: PASS, including `TestUpdateConfig_ConcurrentLegacyMigrationsCannotClobber` and `TestGetConfig_MigrationFailureIsNotSwallowed`.

- [ ] **Step 3c: Refuse a key filed in the wrong lane before anything sees it**

`UpdateConfig` merges `values` verbatim, so `config: {"clientSecret": "…"}` would today land in `ConfigValues` in plaintext — and, in this PR, inside the non-secret snapshot too. The admin API has two lanes for a reason; enforce them server-side, before validation, encryption or persistence.

Create `config_lanes.go`:

```go
package module

// CodeConfigKeyInvalid is the stable 422 code for a submitted key the
// module's schema does not accept in that lane. SDK-owned, like
// CodeConfigRevisionStale.
const CodeConfigKeyInvalid = "module.config_key_invalid"

// validateSubmittedKeys enforces the lane rule the admin API's two request
// blocks imply: a key in `config` must be a declared non-secret field, a
// declared record-list field's label key, or one of its non-secret sub-field
// keys; a key in `secrets` must be a declared secret field or a declared
// secret sub-field key. Anything else — undeclared, the SDK-owned roster, or
// a key filed in the other lane — is refused BEFORE validation, encryption or
// persistence. This is what keeps a secret submitted in the config block out
// of the non-secret validation snapshot and out of ConfigValues in plaintext.
//
// A module that declares no schema has nothing to classify against and keeps
// accepting anything (pre-existing behaviour; every in-tree module declares
// one). Keys are checked in sorted order so the reported field is
// deterministic.
func validateSubmittedKeys(schema []ConfigField, values, secrets map[string]string) error {
	if len(schema) == 0 {
		return nil
	}
	for _, key := range sortedKeys(keySet(values)) {
		if !keyAllowedInLane(schema, key, false) {
			return &ConfigValidationError{
				Field: key, Code: CodeConfigKeyInvalid,
				Message: "is not a non-secret field of this module (send secrets in `secrets`; unknown keys are refused)",
			}
		}
	}
	for _, key := range sortedKeys(keySet(secrets)) {
		if !keyAllowedInLane(schema, key, true) {
			return &ConfigValidationError{
				Field: key, Code: CodeConfigKeyInvalid,
				Message: "is not a secret field of this module (send non-secret values in `config`; unknown keys are refused)",
			}
		}
	}
	return nil
}

// keyAllowedInLane reports whether key may appear in the secrets (true) or
// config (false) lane. The roster key (<field>.__items) is never accepted
// from a request: it is SDK-owned, and the record-list path strips it
// before this runs.
func keyAllowedInLane(schema []ConfigField, key string, secret bool) bool {
	for _, f := range schema {
		if f.Type != FieldRecordList {
			if f.Key == key {
				return (f.Type == FieldSecret) == secret
			}
			continue
		}
		_, sub, ok := SplitElementKey(f.Key, key)
		if !ok {
			continue
		}
		if sub == labelSuffix {
			return !secret
		}
		for _, it := range f.Items {
			if it.Key == sub {
				return (it.Type == FieldSecret) == secret
			}
		}
		return false
	}
	return false
}

func keySet(m map[string]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}
```

Call it in all three service paths, right after the target profile is located and before the merge — in `UpdateConfig` and `UpdateEnvironmentConfig`:

```go
	if err := validateSubmittedKeys(doc.ConfigSchema, values, secrets); err != nil {
		return err
	}
	mergedValues := mergeStringMaps(cur.ConfigValues, values)
```

and in `UpdateEnvironmentConfigWithRecordLists`, inside the loop right after the `cur := doc.Environments[envName]` line (the roster keys were already stripped by `withoutRosterKeys` above the loop):

```go
			if err := validateSubmittedKeys(doc.ConfigSchema, values, secrets); err != nil {
				return err
			}
```

`mapConfigServiceError` already turns a code-bearing `ConfigValidationError` into the 422 envelope, so the wire shape needs nothing new.

Create `config_lanes_test.go`:

```go
package module

import (
	"context"
	"errors"
	"testing"
)

var laneSchema = []ConfigField{
	{Key: "flag", Type: FieldBool},
	{Key: "apiKey", Type: FieldSecret},
	{Key: "profiles", Type: FieldRecordList, Items: []ConfigItemField{
		{Key: "host", Type: FieldString},
		{Key: "password", Type: FieldSecret},
	}},
}

func TestValidateSubmittedKeys(t *testing.T) {
	cases := []struct {
		name    string
		values  map[string]string
		secrets map[string]string
		wantErr string // offending field, "" for accepted
	}{
		{"declared keys in their lanes", map[string]string{"flag": "true", "profiles.a.host": "h", "profiles.a.__label": "A"}, map[string]string{"apiKey": "k", "profiles.a.password": "p"}, ""},
		{"secret in the config lane", map[string]string{"apiKey": "leak"}, nil, "apiKey"},
		{"element secret in the config lane", map[string]string{"profiles.a.password": "leak"}, nil, "profiles.a.password"},
		{"non-secret in the secrets lane", nil, map[string]string{"flag": "true"}, "flag"},
		{"label in the secrets lane", nil, map[string]string{"profiles.a.__label": "A"}, "profiles.a.__label"},
		{"unknown scalar", map[string]string{"bogus": "x"}, nil, "bogus"},
		{"unknown sub-field", map[string]string{"profiles.a.port": "25"}, nil, "profiles.a.port"},
		{"roster key from a request", map[string]string{"profiles.__items": "a"}, nil, "profiles.__items"},
		{"unknown secret", nil, map[string]string{"nope": "x"}, "nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSubmittedKeys(laneSchema, c.values, c.secrets)
			var typed *ConfigValidationError
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected: %v", err)
			case c.wantErr != "" && (!errors.As(err, &typed) || typed.Field != c.wantErr || typed.Code != CodeConfigKeyInvalid):
				t.Fatalf("err = %v, want ConfigValidationError on %q with %s", err, c.wantErr, CodeConfigKeyInvalid)
			}
		})
	}
	if err := validateSubmittedKeys(nil, map[string]string{"anything": "goes"}, map[string]string{"too": "x"}); err != nil {
		t.Errorf("a schema-less module must keep accepting anything: %v", err)
	}
}

// The refusal happens before the validator, before encryption and before the
// write: nothing observes the misfiled secret.
func TestUpdateConfig_SecretInConfigLaneNeverReachesValidatorOrDocument(t *testing.T) {
	svc, repo := newInvService(t)
	err := svc.UpdateConfig(context.Background(), "inv", map[string]string{"providerSecret": "plaintext-leak"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) || typed.Code != CodeConfigKeyInvalid {
		t.Fatalf("err = %v, want %s", err, CodeConfigKeyInvalid)
	}
	if repo.docCasCalls != 0 {
		t.Error("a refused key reached the repository")
	}
	for _, m := range []map[string]string{repo.docs["inv"].ConfigValues, repo.docs["inv"].Environments["production"].ConfigValues} {
		if _, ok := m["providerSecret"]; ok {
			t.Error("plaintext secret persisted in ConfigValues")
		}
	}
	// Same rule on the named-environment path and the record-list path.
	if err := svc.UpdateEnvironmentConfig(context.Background(), "inv", "sandbox", nil, map[string]string{"password": "misfiled"}); !errors.As(err, &typed) {
		t.Errorf("env PATCH: %v", err)
	}
	if err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "inv", "sandbox", map[string]string{"bogus": "x"}, nil, nil, nil); !errors.As(err, &typed) {
		t.Errorf("record-list path: %v", err)
	}
}
```

Update Task 3's `invariantModule` schema so its tests submit only declared keys (already done in the fixture above: `extraSecret`). If any pre-existing record-list test submits a key its fixture schema does not declare, fix the **fixture** — the rule is the point.

Run: `cd backend && go test ./pkg/sdk/module/ -run 'Lane|SubmittedKeys' -count=1`
Expected: PASS.

- [ ] **Step 4: Delete the four superseded repository methods**

Remove `UpdateConfigValues`, `UpdateEnvironmentConfig`, `SetActiveEnvironment`, `ActivateEnvironment` from `config_repository.go`, from `ConfigRepository` in `config_repository_iface.go`, and from the fake in `recordlist_fake_repo_test.go` (the fake's `duringActivate` is now consumed only by `CompareAndSwapConfig`). Keep `MigrateToEnvironments`, `Upsert`, `UpdateEnabled`, `ClearNeedsRestart`, `RefreshMetadata`, `FindByName`, `FindAll`.

Run: `cd backend && go build ./... && go vet ./pkg/sdk/...`
Expected: clean — nothing outside the service composed those methods (verified: only `finalize.go` and `reconcile.go` call the *service*).

- [ ] **Step 5: Handler — stop clearing `needsRestart` after writes; map the stale 409**

In `handler.go`:
- delete lines 303-306 (`// Modules that read config lazily ...` block in `UpdateModule`);
- delete lines 438-440 (`if h.registry.SupportsHotReload ... ClearNeedsRestart` in `UpdateEnvironment`);
- in `SetActiveEnvironment` (lines 490-493) keep `needsRestart := !h.registry.SupportsHotReload(input.Name)` for the response and delete the `if !needsRestart { _ = h.configService.ClearNeedsRestart(...) }` block.

In `config_error_envelope.go`, extend `mapConfigServiceError`:

```go
func mapConfigServiceError(err error, fallback func(error) error) error {
	var invalid *ConfigValidationError
	if errors.As(err, &invalid) {
		if invalid.Code != "" {
			return &configValidationHTTPError{
				Status: http.StatusUnprocessableEntity,
				Title:  http.StatusText(http.StatusUnprocessableEntity),
				Detail: invalid.Error(),
				Code:   invalid.Code,
			}
		}
		return huma.Error422UnprocessableEntity(invalid.Error())
	}
	// A lost compare-and-swap is a 409 with a stable code on every surface:
	// the client reloads the document and re-reviews its diff. It is
	// deliberately never retried server-side — a retry would re-decide the
	// operator's change against a state they never saw.
	if errors.Is(err, ErrRevisionStale) {
		return &configValidationHTTPError{
			Status: http.StatusConflict,
			Title:  http.StatusText(http.StatusConflict),
			Detail: "The module configuration changed after it was loaded. Reload and review your changes before saving again.",
			Code:   CodeConfigRevisionStale,
		}
	}
	return fallback(err)
}
```

Add to `config_error_envelope_test.go`:

```go
func TestMapConfigServiceError_StaleRevisionIs409WithCode(t *testing.T) {
	err := mapConfigServiceError(fmt.Errorf("wrapped: %w", ErrRevisionStale), func(e error) error { return e })
	typed, ok := err.(*configValidationHTTPError)
	if !ok || typed.Status != http.StatusConflict || typed.Code != CodeConfigRevisionStale {
		t.Fatalf("got %#v, want 409 envelope with %q", err, CodeConfigRevisionStale)
	}
}
```

- [ ] **Step 6: Run everything**

Run: `cd backend && go test ./pkg/sdk/module/ -count=1 && go test ./internal/core/tenant/... ./internal/shared/setup/... -count=1`
Expected: PASS. (`TestActivationDoesNotResurrectARemovedSecret` and `TestActivationSurfacesAnUnknownEnvironment` still pass through the fake's `CompareAndSwapConfig` activation branch.) If Mongo is reachable, also `MONGO_TEST_URI=… go test ./pkg/sdk/module/ -count=1` — the live `config_validator_test.go` assertions (`needsRestart` untouched on rejected activation, legacy sync on success) must still hold.

- [ ] **Step 7: Docs**

`backend/pkg/sdk/CLAUDE.md` — add to **Versioning policy**, right after the `module.RedisClient` bullet:

```markdown
- **`module.ConfigRepository` is provided TO `ModuleConfigService`, not
  implemented BY modules** — the same category as `RedisClient`, and its own
  doc comment says so ("exactly what the service calls — no more"). It is
  therefore outside the additive-only rule: it changed shape for atomic
  module-config writes (`CompareAndSwapConfig` added; `CompareAndSwapEnvironment`
  and `MigrateToEnvironments` re-signed; the four two-step write methods
  removed). The only thing that tracks it is a fork's substitute repository (a
  test double); `var _ ConfigRepository = (*ModuleConfigRepository)(nil)` pins
  the in-tree one.
```

Then add these bullets to **Rules**, right after the `HasConfigActivationValidator` bullet:

```markdown
- **`module.HasConfigSnapshotValidator` is the successor seam that sees the
  whole target snapshot.** `ValidateConfigSnapshot(ctx, module.ConfigValidationSnapshot)`
  runs on all three mutation surfaces — active-config PATCH, named-environment
  PATCH (record-list path included) and activation — with `Values` (raw merged
  target, absent ≠ empty), `EffectiveValues` (the runtime EnvVar/Default
  fallback applied) and `SecretPresent` (names → booleans, computed from the
  **target** profile's own stored ciphertext, this request's submitted secrets,
  and the schema fallback — never another profile's secrets, never plaintext).
  A module that implements it is judged through it everywhere and its older
  hooks are not called; a module that omits it keeps `HasConfigValidator` /
  `HasConfigActivationValidator` exactly as before. A stored secret that cannot
  be decrypted aborts the mutation (`ErrConfigSecretUnreadable`) unless the
  request submits a replacement for that key.
- **Every config mutation is ONE compare-and-swap `UpdateOne` on
  `ModuleConfig.ConfigRevision`** (`ConfigRepository.CompareAndSwapConfig` with
  an explicit `ConfigMutation`: profile write + legacy mirror, or server-side
  activation). Profiles are the source of truth; the legacy top-level maps are
  a mirror written in the same update. A lost race is `ErrRevisionStale` → 409
  with body code `module.CodeConfigRevisionStale` (`"module.config_revision_stale"`,
  SDK-owned — `errcode` must never declare a `module.*` code); the client
  reloads and re-reviews, nothing auto-retries. The record-list CAS increments
  `configRevision` in its own update, so record-list and ordinary writes cannot
  pass each other unseen. `needsRestart` is persisted in that same write as
  `!SupportsHotReload(name)` (`SetHotReloadResolver`, installed by the registry
  before seeding); the admin handler no longer clears it afterwards.
```

`docs/site/sdk/config-service.mdx` — insert after the "Stable validation codes" section (before "Repeatable fields"):

~~~markdown
## Snapshot validation

A module that needs a cross-field rule involving secrets — "this OAuth provider is fully configured" — implements the optional `HasConfigSnapshotValidator` instead of the two hooks above:

```go
type ConfigValidationSnapshot struct {
    Environment     string
    Values          map[string]string // raw merged target; absent and "" are distinct
    EffectiveValues map[string]string // runtime EnvVar/Default fallback applied
    SecretPresent   map[string]bool   // per secret key: would a non-empty value be in force?
}

type HasConfigSnapshotValidator interface {
    ValidateConfigSnapshot(ctx context.Context, snapshot ConfigValidationSnapshot) error
}
```

It runs on every mutation surface with the exact profile that would become effective: the active profile merged with a `PATCH /v1/admin/modules/{name}`, the named profile merged with a `PATCH …/environments/{env}`, or the stored target of a `PUT …/active-environment`. `SecretPresent` is computed from the **target** profile's own stored secrets, the secrets submitted in the same request, and the schema's `EnvVar`/`Default` — never from another profile, and never as plaintext. A request may therefore save a provider secret and flip a dependent switch atomically, and an inactive profile is never judged with the active profile's credentials. Modules that keep `HasConfigValidator` / `HasConfigActivationValidator` are unaffected.

## Atomic writes and optimistic concurrency

Every module-config mutation is a single MongoDB `UpdateOne` guarded by the document's monotone `configRevision`: the service reads the document, validates the candidate, and writes profile values, secrets, the legacy mirror, `needsRestart` and `configRevision + 1` together — or nothing. A write that lost the race returns **409** with body code `module.config_revision_stale`; reload the document and review the diff before saving again. The operator console does exactly that ("Reload & review"). `needsRestart` is persisted in the same write from the module's `HotReloadConfig()` declaration, so a successful edit to a hot-reloadable module never leaves a stale restart hint.
~~~

- [ ] **Step 8: Commit**

```bash
cd backend && go vet ./... && cd ..
git add backend/pkg/sdk/module backend/pkg/sdk/CLAUDE.md docs/site/sdk/config-service.mdx
git commit -m "feat(sdk): module-config mutations are one CAS write validated on the target snapshot

UpdateConfig / UpdateEnvironmentConfig / SetActiveEnvironment and the
record-list path now build one candidate from the target profile, dispatch
to HasConfigSnapshotValidator (or the legacy hooks), and persist through
CompareAndSwapConfig — profile + legacy mirror in the same update,
needsRestart from the module's hot-reload declaration, stale revision → 409
module.config_revision_stale. The four two-step repository methods are
removed. Two concurrent operators can no longer combine two valid writes
into an invalid document.

Refs: spec §4.1, §4.5"
```

### Task 4: `RequirePersistedConfig` — no lazy re-seed for `auth`, a `missing` row instead of a failed list

**Files:**
- Modify: `backend/pkg/sdk/module/config_service.go` (struct fields, `RequirePersistedConfig`, `IsRequiredPersisted`, `GetConfig`, `ListConfigs`, `GetAllConfigs`, `GetRawValueRequiredModule`)
- Modify: `backend/pkg/sdk/module/recordlist_fake_repo_test.go` (`FindAll` returns the docs)
- Create: `backend/pkg/sdk/module/config_required_test.go`
- Modify: `backend/pkg/sdk/module/handler.go` (`ModuleConfigResponse.Missing`, `ListModules`, `missingConfigResponse`, `mapConfigReadError` in `GetModule`/`UpdateModule`/`ListEnvironments`)
- Modify: `backend/pkg/sdk/module/config_error_envelope.go` (`ErrRequiredConfigMissing` → 503)
- Create: `backend/cmd/server/admin_wiring.go`, `backend/cmd/server/admin_wiring_test.go`
- Modify: `backend/cmd/server/main.go` (after `InitAll`, line ~281)
- Modify: `backend/internal/core/CLAUDE.md:109` (table row), `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/config-service.mdx`
- Modify: `frontend-admin/src/store/api/moduleApi.ts`, `frontend-admin/src/pages/admin/modules/ModuleTable.tsx`, `frontend-admin/src/locales/{en,it}.json`; Create: `frontend-admin/src/pages/admin/modules/ModuleTable.test.tsx`
- Regenerate: `backend/openapi/enterprise.json`

**Interfaces:**
- Produces: `var ErrRequiredConfigMissing`, `var ErrRequiredSetSealed`; `func (s *ModuleConfigService) RequirePersistedConfig(ctx context.Context, names ...string) error` (verifies the document exists and boot seeding/backfill succeeded for each name — the boot gate); `seedFailures map[string]error` recorded by `SeedFromModules`; `func (s *ModuleConfigService) IsRequiredPersisted(name string) bool`; `type ModuleConfigStatus struct{Name string; Missing bool; Config *ModuleConfig}`; `func (s *ModuleConfigService) ListConfigs(ctx) ([]ModuleConfigStatus, error)`; `func (s *ModuleConfigService) GetRawValueRequiredModule(ctx, moduleName, key string) (string, bool, error)`; `ModuleConfigResponse.Missing bool` + `Status: "missing"`; `var requiredPersistedModules = []string{"auth"}` in `cmd/server`.
- Consumed by: PR 3 (`PasswordLoginEnabled` reads `GetRawValueRequiredModule`), Task 6 (handler helpers).

- [ ] **Step 1: Write the failing tests**

Create `config_required_test.go`:

```go
package module

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func requiredService(t *testing.T) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	repo := newFakeConfigRepo()
	repo.docs["user"] = &ModuleConfig{ModuleName: "user", ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{minimalModule{name: "auth"}, minimalModule{name: "user"}, plainModule{}})
	return svc, repo
}

func TestRequirePersistedConfig_GetConfigFailsClosedInsteadOfReseeding(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)

	// Before the mark: a known module with no document is lazily rebuilt.
	if doc, err := svc.GetConfig(ctx, "auth"); err != nil || doc == nil {
		t.Fatalf("pre-mark lazy seed: doc=%v err=%v", doc, err)
	}
	delete(repo.docs, "auth")

	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"} // the gate verifies the document exists
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatal(err)
	}
	delete(repo.docs, "auth")
	if !svc.IsRequiredPersisted("auth") || svc.IsRequiredPersisted("user") {
		t.Error("IsRequiredPersisted did not reflect the mark")
	}
	_, err := svc.GetConfig(ctx, "auth")
	if !errors.Is(err, ErrRequiredConfigMissing) {
		t.Fatalf("GetConfig after the mark: err = %v, want ErrRequiredConfigMissing", err)
	}
	if _, seeded := repo.docs["auth"]; seeded {
		t.Fatal("a required module was lazily re-seeded with schema defaults")
	}
	// Ordinary modules keep their self-healing.
	if doc, err := svc.GetConfig(ctx, "plain"); err != nil || doc == nil {
		t.Errorf("non-required module lost lazy seed: doc=%v err=%v", doc, err)
	}
}

func TestRequirePersistedConfig_SealedAfterFirstCall(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequirePersistedConfig(ctx, "user"); !errors.Is(err, ErrRequiredSetSealed) {
		t.Fatalf("second call: err = %v, want ErrRequiredSetSealed", err)
	}
	if svc.IsRequiredPersisted("user") {
		t.Error("a sealed set accepted a late addition")
	}
}

// The mark is the boot gate: a required module whose document is missing,
// or whose seeding/backfill failed, must stop the server before traffic —
// an incomplete auth document is exactly what a strict policy reader must
// never be handed.
func TestRequirePersistedConfig_RefusesAFailedOrMissingSeed(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	if err := svc.RequirePersistedConfig(ctx, "auth"); !errors.Is(err, ErrRequiredConfigMissing) {
		t.Fatalf("missing document: err = %v, want ErrRequiredConfigMissing", err)
	}
	if svc.requiredSealed {
		t.Fatal("a refused mark must not seal the set — boot is aborting")
	}
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	svc.seedFailures["auth"] = errors.New("backfill: write refused")
	if err := svc.RequirePersistedConfig(ctx, "auth"); err == nil || !strings.Contains(err.Error(), "backfill") {
		t.Fatalf("failed seed: err = %v, want the recorded seeding failure", err)
	}
	delete(svc.seedFailures, "auth")
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatalf("healthy document: %v", err)
	}
}

func TestListConfigs_ReportsMissingRequiredRowAndServesTheRest(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatal(err)
	}
	delete(repo.docs, "auth")
	statuses, err := svc.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("ListConfigs must not fail because one required document is missing: %v", err)
	}
	byName := map[string]ModuleConfigStatus{}
	for _, st := range statuses {
		byName[st.Name] = st
	}
	if st := byName["auth"]; !st.Missing || st.Config != nil {
		t.Errorf("auth row = %+v, want Missing with nil Config", st)
	}
	if st := byName["user"]; st.Missing || st.Config == nil {
		t.Errorf("user row = %+v, want a present config", st)
	}
	if st := byName["plain"]; st.Missing || st.Config == nil {
		t.Errorf("plain (non-required, missing) must be lazily seeded: %+v", st)
	}
	if _, seeded := repo.docs["auth"]; seeded {
		t.Fatal("ListConfigs re-seeded the required module")
	}
	// GetAllConfigs keeps its shape: present documents only.
	docs, err := svc.GetAllConfigs(ctx)
	if err != nil || len(docs) != 2 {
		t.Errorf("GetAllConfigs = %d docs err=%v, want 2 (user, plain)", len(docs), err)
	}
}

func TestGetRawValueRequiredModule(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth", ActiveEnvironment: "production",
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{"present": "x", "cleared": ""}}}}
	if v, ok, err := svc.GetRawValueRequiredModule(ctx, "auth", "present"); err != nil || !ok || v != "x" {
		t.Errorf("present: (%q,%v,%v)", v, ok, err)
	}
	if v, ok, err := svc.GetRawValueRequiredModule(ctx, "auth", "cleared"); err != nil || !ok || v != "" {
		t.Errorf("cleared: (%q,%v,%v)", v, ok, err)
	}
	if _, ok, err := svc.GetRawValueRequiredModule(ctx, "auth", "absent"); err != nil || ok {
		t.Errorf("absent key in a present document is not an error: ok=%v err=%v", ok, err)
	}
	delete(repo.docs, "auth")
	if _, _, err := svc.GetRawValueRequiredModule(ctx, "auth", "present"); !errors.Is(err, ErrRequiredConfigMissing) {
		t.Errorf("missing document: err = %v, want ErrRequiredConfigMissing", err)
	}
	// The permissive sibling is unchanged: nil document is "absent", not an error.
	if _, ok, err := svc.GetRawValue(ctx, "auth", "present"); err != nil || ok {
		t.Errorf("GetRawValue contract changed: ok=%v err=%v", ok, err)
	}
}

func TestListModules_RendersMissingRow(t *testing.T) {
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	_ = svc.RequirePersistedConfig(context.Background(), "auth")
	delete(repo.docs, "auth")
	reg := NewModuleRegistry(slog.Default())
	reg.Register(minimalModule{name: "auth"})
	h := NewModuleAdminHandler(svc, reg)
	out, err := h.ListModules(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var auth *ModuleConfigResponse
	for i := range out.Body.Modules {
		if out.Body.Modules[i].ModuleName == "auth" {
			auth = &out.Body.Modules[i]
		}
	}
	if auth == nil || !auth.Missing || auth.Status != "missing" {
		t.Fatalf("auth row = %+v, want Missing=true Status=missing", auth)
	}
	for name, call := range map[string]func() error{
		"GetModule":        func() error { _, err := h.GetModule(context.Background(), &GetModuleInput{Name: "auth"}); return err },
		"GetEnvironment":   func() error { _, err := h.GetEnvironment(context.Background(), &GetEnvironmentInput{Name: "auth", Env: "production"}); return err },
		"ListEnvironments": func() error { _, err := h.ListEnvironments(context.Background(), &ListEnvironmentsInput{Name: "auth"}); return err },
	} {
		err := call()
		se, ok := err.(huma.StatusError)
		if !ok || se.GetStatus() != http.StatusServiceUnavailable {
			t.Errorf("%s on a missing required document: err=%v, want a 503 (never a 404 that reads as 'no such environment')", name, err)
		}
	}
}
```

Make the fake's `FindAll` real (replace the one-liner at `recordlist_fake_repo_test.go:77`):

```go
// FindAll returns deep copies in name order, like the Mongo repository
// would (order is not part of the contract; determinism is convenient).
func (f *fakeConfigRepo) FindAll(ctx context.Context) ([]ModuleConfig, error) {
	names := make([]string, 0, len(f.docs))
	for n := range f.docs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ModuleConfig, 0, len(names))
	for _, n := range names {
		cp, _ := f.FindByName(ctx, n)
		out = append(out, *cp)
	}
	return out, nil
}
```
(add `"sort"` to that file's imports).

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./pkg/sdk/module/ -run 'RequirePersisted|ListConfigs|GetRawValueRequired|RendersMissing' -count=1`
Expected: compile errors for the missing methods/types.

- [ ] **Step 3: Implement the service side**

In `config_service.go` add to the struct:

```go
	// requiredPersisted names modules whose config document must exist for
	// the rest of the process: after boot seeding, a missing document for
	// one of them is an outage — GetConfig fails and the list shows a
	// `missing` row — never a reason to rebuild it from schema defaults.
	// The strict credential-policy readers in auth depend on this: a lazy
	// re-seed from an admin page read would recreate a permissive default
	// exactly when the reader had correctly observed the outage. Populated
	// once by RequirePersistedConfig before traffic and read-only after.
	requiredPersisted map[string]bool
	requiredSealed    bool
	// seedFailures records, per module, the last boot-seeding or backfill
	// failure SeedFromModules logged (nil-free when everything succeeded).
	// RequirePersistedConfig consults it: a required module whose seed
	// failed must stop the boot, not serve a possibly incomplete document.
	seedFailures map[string]error
```

Initialize `requiredPersisted: make(map[string]bool)` and `seedFailures: make(map[string]error)` in `NewModuleConfigService`. In `SeedFromModules` record every per-module failure that affects the document's content — the `FindByName` error at line 129, the first-boot `Upsert` error at line 162, and (Task 5) the backfill error — with `s.seedFailures[m.Name()] = err` next to the existing `logger.Error`. Then add:

```go
var (
	// ErrRequiredConfigMissing: a module marked by RequirePersistedConfig has
	// no document. Recovery is restore the document, or fix Mongo and perform
	// a controlled restart so boot seeding can run — never a lazy re-seed.
	ErrRequiredConfigMissing = errors.New("module: required config document is missing")
	// ErrRequiredSetSealed: RequirePersistedConfig was already called. The set
	// is decided once before traffic; pass every name in that one call.
	ErrRequiredSetSealed = errors.New("module: required persisted-config set is already sealed")
)

// RequirePersistedConfig marks modules whose config document must exist
// from now on (see requiredPersisted), and is the BOOT GATE for them: it
// refuses — so cmd/server can abort before serving traffic — when a named
// module's document is missing or its boot seeding/backfill recorded a
// failure. A required module is one whose strict readers may never be
// handed an incomplete document; "log and continue" is not an option for
// it. Call it ONCE, after boot seeding has run; a second call fails with
// ErrRequiredSetSealed so the set cannot drift while the process serves.
// A refused call seals nothing (the caller is about to exit).
func (s *ModuleConfigService) RequirePersistedConfig(ctx context.Context, names ...string) error {
	if s.requiredSealed {
		return ErrRequiredSetSealed
	}
	for _, n := range names {
		if err := s.seedFailures[n]; err != nil {
			return fmt.Errorf("module %q: boot seeding failed, refusing to serve: %w", n, err)
		}
		doc, err := s.repo.FindByName(ctx, n)
		if err != nil {
			return fmt.Errorf("module %q: verify config document: %w", n, err)
		}
		if doc == nil {
			return fmt.Errorf("%w: %q", ErrRequiredConfigMissing, n)
		}
	}
	s.requiredSealed = true
	for _, n := range names {
		s.requiredPersisted[n] = true
	}
	return nil
}

// IsRequiredPersisted reports whether name was marked by RequirePersistedConfig.
func (s *ModuleConfigService) IsRequiredPersisted(name string) bool { return s.requiredPersisted[name] }

// ModuleConfigStatus is one row of ListConfigs: a present document, or a
// required module whose document is missing (Config nil).
type ModuleConfigStatus struct {
	Name    string
	Missing bool
	Config  *ModuleConfig
}
```

Change `GetConfig`'s tail (line 263):

```go
	if s.requiredPersisted[name] {
		s.logger.Error("GetConfig: required module config document is missing — restore it or restart",
			slog.String("module", name))
		return nil, fmt.Errorf("%w: %q", ErrRequiredConfigMissing, name)
	}
	return s.lazySeed(ctx, name)
```

Replace `GetAllConfigs` (lines 297-337) with `ListConfigs` + a wrapper:

```go
// ListConfigs returns one row per registered module: the document when it
// exists (lazily migrated to profiles), a lazily re-seeded document for an
// ordinary module that lost its document (dev DB wipe), and a `missing` row
// for a REQUIRED module that lost its document. The required-missing case
// never fails the whole list — the list is the page an operator repairs
// from — and never re-seeds; a failed profile migration, being a write
// failure, does fail it. Orphan documents (modules not compiled into this
// binary) are dropped, non-destructively, as before.
func (s *ModuleConfigService) ListConfigs(ctx context.Context) ([]ModuleConfigStatus, error) {
	docs, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	var present map[string]bool
	if len(s.knownModules) > 0 {
		docs, present = filterKnown(docs, s.knownModules)
	}
	for i := range docs {
		// A failed migration is a real write failure (the lost-race case is
		// absorbed inside ensureEnvironments by re-reading); a read does not
		// paper over it by serving the unmigrated document.
		if err := s.ensureEnvironments(ctx, &docs[i]); err != nil {
			return nil, fmt.Errorf("module %q: migrate legacy config: %w", docs[i].ModuleName, err)
		}
	}
	out := make([]ModuleConfigStatus, 0, len(docs)+len(s.knownModules))
	for i := range docs {
		out = append(out, ModuleConfigStatus{Name: docs[i].ModuleName, Config: &docs[i]})
	}
	names := make([]string, 0, len(s.knownModules))
	for name := range s.knownModules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if present[name] {
			continue
		}
		if s.requiredPersisted[name] {
			s.logger.Error("ListConfigs: required module config document is missing — restore it or restart",
				slog.String("module", name))
			out = append(out, ModuleConfigStatus{Name: name, Missing: true})
			continue
		}
		if seeded, err := s.lazySeed(ctx, name); err == nil && seeded != nil {
			out = append(out, ModuleConfigStatus{Name: name, Config: seeded})
		}
	}
	return out, nil
}

// GetAllConfigs is ListConfigs without the missing rows — the pre-existing
// shape, kept for callers that only want documents.
func (s *ModuleConfigService) GetAllConfigs(ctx context.Context) ([]ModuleConfig, error) {
	statuses, err := s.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	docs := make([]ModuleConfig, 0, len(statuses))
	for _, st := range statuses {
		if st.Config != nil {
			docs = append(docs, *st.Config)
		}
	}
	return docs, nil
}
```

Add after `GetRawValue`:

```go
// GetRawValueRequiredModule is GetRawValue for a module whose document must
// exist: the three outcomes are preserved, but a missing document is the
// ERROR outcome (ErrRequiredConfigMissing), not "absent". It never calls
// GetConfig's lazy-seed path. A caller governing credentials — auth's
// per-surface password policy — reads through this so an outage can never
// be mistaken for "the operator said nothing here" and fall back to a
// permissive default. GetRawValue itself is unchanged: changing it would
// alter SessionAbsoluteTTL's compatibility contract.
func (s *ModuleConfigService) GetRawValueRequiredModule(ctx context.Context, moduleName, key string) (string, bool, error) {
	doc, err := s.repo.FindByName(ctx, moduleName)
	if err != nil {
		return "", false, err
	}
	if doc == nil {
		return "", false, fmt.Errorf("%w: %q", ErrRequiredConfigMissing, moduleName)
	}
	v, ok := doc.ActiveConfigValues()[key]
	return v, ok, nil
}
```

Add `"errors"` and `"sort"` to `config_service.go` imports.

- [ ] **Step 4: Handler — the `missing` row and 503 on required-missing reads**

In `handler.go`, add to `ModuleConfigResponse` after `Error`:

```go
	// Missing is true for a required module whose config document is absent
	// (see ModuleConfigService.RequirePersistedConfig). Status is "missing",
	// ConfigValues/SecretStatus are empty, and every mutation on the module
	// returns 503 until the document is restored or the backend restarted.
	Missing bool `json:"missing,omitempty"`
```

Replace `ListModules`:

```go
func (h *ModuleAdminHandler) ListModules(ctx context.Context, _ *struct{}) (*ListModulesOutput, error) {
	statuses, err := h.configService.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	resp := make([]ModuleConfigResponse, 0, len(statuses))
	for _, st := range statuses {
		if st.Missing {
			resp = append(resp, h.missingConfigResponse(st.Name))
			continue
		}
		resp = append(resp, h.toConfigResponse(*st.Config))
	}
	return &ListModulesOutput{
		Body: struct {
			Modules []ModuleConfigResponse `json:"modules"`
		}{Modules: resp},
	}, nil
}

// missingConfigResponse renders a required module whose document is gone:
// identity and schema come from the registered module (the binary is the
// source of truth for those), state is "missing".
func (h *ModuleAdminHandler) missingConfigResponse(name string) ModuleConfigResponse {
	resp := ModuleConfigResponse{
		ModuleName:            name,
		DisplayName:           name,
		Status:                "missing",
		Missing:               true,
		ConfigValues:          map[string]string{},
		SecretStatus:          map[string]bool{},
		ActiveEnvironment:     "production",
		AvailableEnvironments: DefaultEnvironments,
	}
	for _, m := range h.registry.AllModules() {
		if m.Name() != name {
			continue
		}
		resp.DisplayName = DisplayNameOf(m)
		resp.Description = DescriptionOf(m)
		resp.Category = m.Category()
		resp.ConfigSchema = ConfigSchemaOf(m)
		resp.ConfigGroups = ConfigGroupsOf(m)
		resp.DependsOn = DependenciesOf(m)
		resp.Enabled = m.Category() == CategoryCore || h.registry.IsStarted(name)
		break
	}
	return resp
}
```

Add to `config_error_envelope.go`:

```go
// mapConfigReadError turns a required-module outage into a 503 the SPA can
// render as retryable; every other error passes through unchanged.
func mapConfigReadError(err error) error {
	if errors.Is(err, ErrRequiredConfigMissing) {
		return huma.Error503ServiceUnavailable(
			"Module configuration is unavailable: the stored document is missing. Restore it or restart the backend.")
	}
	return err
}
```

and in `mapConfigServiceError`, before the fallback: `if errors.Is(err, ErrRequiredConfigMissing) { return mapConfigReadError(err) }`. Use `mapConfigReadError(err)` in `GetModule` (line 216), `UpdateModule`'s first `GetConfig` (line 231), `ListEnvironments` (line 373), and `UpdateEnvironment`'s trailing `GetEnvironmentConfig` (line 444). `GetEnvironment` (line 392) today turns EVERY error into a 404 — make the required-missing case win first:

```go
	envConfig, secretStatus, err := h.configService.GetEnvironmentConfig(ctx, input.Name, input.Env)
	if err != nil {
		if errors.Is(err, ErrRequiredConfigMissing) {
			return nil, mapConfigReadError(err)
		}
		return nil, huma.Error404NotFound(err.Error())
	}
```

- [ ] **Step 5: Run the SDK tests**

Run: `cd backend && go test ./pkg/sdk/module/ -count=1`
Expected: PASS.

- [ ] **Step 6: Wire `cmd/server`**

Create `backend/cmd/server/admin_wiring.go`:

```go
package main

// requiredPersistedModules are the modules whose module_configs document is
// REQUIRED once boot seeding has run: a missing document is an outage that
// fails closed (503) and shows as a `missing` row on /admin/modules, never a
// reason to rebuild it from schema defaults. auth is here because its
// per-surface credential policy (password login on/off) is read strictly —
// a lazy re-seed from an admin page read would silently re-enable password
// sign-in with the schema default. Recovery: restore the document, or fix
// Mongo and restart so normal boot seeding runs.
var requiredPersistedModules = []string{"auth"}
```

(Task 6 adds the audit resolver to this file.) Create `admin_wiring_test.go`:

```go
package main

import "testing"

func TestRequiredPersistedModules_AuthIsRequired(t *testing.T) {
	found := false
	for _, n := range requiredPersistedModules {
		if n == "auth" {
			found = true
		}
	}
	if !found {
		t.Fatal("auth must be a required persisted config: its strict password-policy reader depends on it")
	}
}
```

In `main.go`, right after the `InitAll` fatal check (line ~281) and before the level-resolver swap:

```go
	// Boot seeding has run inside InitAll. From here on the documents of
	// requiredPersistedModules must exist: GetConfig fails closed and the
	// module list shows a `missing` row instead of lazily re-seeding them.
	// This is also the boot gate for those modules: a missing document, or a
	// seeding/backfill failure SeedFromModules recorded, aborts here rather
	// than serving a strict policy reader an incomplete auth document.
	if err := configService.RequirePersistedConfig(ctx, requiredPersistedModules...); err != nil {
		log.Fatalf("Required module config is not serviceable: %v", err)
	}
```

Run: `cd backend && go build ./... && go test ./cmd/server/ -count=1`
Expected: build clean, PASS.

- [ ] **Step 7: Console — the `missing` badge**

`store/api/moduleApi.ts`: change `status: 'running' | 'failed' | 'disabled' | 'stopped';` to `status: 'running' | 'failed' | 'disabled' | 'stopped' | 'missing';` and add `/** A required module whose stored config document is absent. */ missing?: boolean;` after `error?: string;`.

`ModuleTable.tsx`: add `missing: 'danger'` to `statusColors` and `missing: 'bg-danger'` to `healthDotColors`; in the status cell, after the `{mod.error && (...)}` block add:

```tsx
                      {mod.missing && (
                        <div
                          className="text-danger fs-11 mt-1"
                          role="status"
                          data-testid={`module-missing-${mod.moduleName}`}
                        >
                          {t('adminModules.missingConfig')}
                        </div>
                      )}
```

`locales/en.json` under `adminModules`: add `"missing": "missing"` to `status` (line 2305 block) and, as a sibling of `loadError`, `"missingConfig": "Configuration document missing — restore it or restart the backend."`. `locales/it.json`: `"missing": "mancante"` and `"missingConfig": "Documento di configurazione mancante — ripristinalo o riavvia il backend."`.

Create `ModuleTable.test.tsx`:

```tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import ModuleTable from './ModuleTable';

const present = {
  moduleName: 'user', displayName: 'User', description: '', category: 'core', enabled: true,
  status: 'running', needsRestart: false, configValues: {}, secretStatus: {}, configSchema: [],
  dependsOn: [], providedServices: [], requiredServices: [], optionalServices: [],
  activeEnvironment: 'production', availableEnvironments: ['production'], createdAt: '', updatedAt: ''
};
const missing = { ...present, moduleName: 'auth', displayName: 'Auth', status: 'missing', missing: true };

describe('ModuleTable missing row', () => {
  beforeEach(() => {
    server.use(
      http.get(url('/v1/admin/modules'), () => HttpResponse.json({ modules: [present, missing] })),
      http.get(url('/v1/admin/modules/health'), () => HttpResponse.json({ modules: [] }))
    );
  });

  it('flags a required module whose document is missing, and only that one', async () => {
    renderWithProviders(<ModuleTable scope="core" />);
    expect(await screen.findByTestId('module-missing-auth')).toHaveTextContent(/Configuration document missing/);
    expect(screen.queryByTestId('module-missing-user')).not.toBeInTheDocument();
    expect(screen.getByText('missing')).toBeInTheDocument();
  });
});
```

Run: `cd frontend-admin && npx vitest run src/pages/admin/modules/ModuleTable.test.tsx src/locales && npm run typecheck`
Expected: PASS (parity test included).

- [ ] **Step 8: Docs + OpenAPI**

`backend/internal/core/CLAUDE.md` line 109 — replace the `module_configs documents` row with:

```markdown
| `module_configs` documents | `pkg/sdk/module/config_service.go::SeedFromModules` (+ schema-key backfill for existing documents) | ✅ `GetConfig`/`ListConfigs` lazy-rebuild from the in-memory spec cache — **except modules named in `RequirePersistedConfig` (in-tree: `auth`)**: after boot seeding, a missing `auth` document is an outage — `GetConfig("auth")` fails closed, `/admin/modules` renders a `missing` row, and the strict password-policy readers return 503 rather than a schema default. Recovery is restore-or-restart, never lazy re-seed. |
```

`backend/pkg/sdk/CLAUDE.md` — add a Rules bullet:

```markdown
- **`RequirePersistedConfig(ctx, names...)` turns off lazy self-heal for the
  named modules and is their boot gate** — call it once, after `InitAll`,
  before serving; it fails (and `cmd/server` exits) when a named module's
  document is missing or `SeedFromModules` recorded a seeding/backfill failure
  for it, because a strict reader must never be handed an incomplete
  document. For those modules
  a missing document makes `GetConfig` / `GetRawValueRequiredModule` return
  `ErrRequiredConfigMissing` (503 on the admin API) and `ListConfigs` emit a
  `ModuleConfigStatus{Missing: true}` row instead of re-seeding; every other
  module keeps today's rebuild-from-schema behaviour. The set is sealed after
  the first call. In-tree the server marks `auth` (`cmd/server/admin_wiring.go`).
```

`docs/site/sdk/config-service.mdx` — append to the "Atomic writes and optimistic concurrency" section:

```markdown
### Required documents

Lazy self-healing (an admin read rebuilding a wiped document from schema defaults) is the default, but a deployment can mark modules whose document must **not** be rebuilt at runtime — Orkestra marks `auth`, because its per-surface password-login policy is read strictly and a rebuilt default would silently re-enable password sign-in. For a marked module a missing document is an outage: `GET /v1/admin/modules/{name}` and every mutation return **503**, `GET /v1/admin/modules` still lists every other module and shows the marked one as `"status": "missing"` with `"missing": true`. Restore the document, or fix the database and restart so boot seeding runs.
```

Regenerate the OpenAPI dump (needs the infra stack): `cd backend && make openapi-dump && git diff --stat openapi/enterprise.json` — expect `missing` added to `ModuleConfigResponse`.

- [ ] **Step 9: Commit**

```bash
cd backend && go vet ./... && cd ..
git add backend/pkg/sdk/module backend/cmd/server/admin_wiring.go backend/cmd/server/admin_wiring_test.go \
  backend/cmd/server/main.go backend/internal/core/CLAUDE.md backend/pkg/sdk/CLAUDE.md \
  docs/site/sdk/config-service.mdx backend/openapi/enterprise.json \
  frontend-admin/src/store/api/moduleApi.ts frontend-admin/src/pages/admin/modules/ModuleTable.tsx \
  frontend-admin/src/pages/admin/modules/ModuleTable.test.tsx frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "feat(sdk): RequirePersistedConfig — auth document fails closed, list shows a missing row

After boot seeding cmd/server marks auth as required: GetConfig and the new
strict GetRawValueRequiredModule return ErrRequiredConfigMissing (503)
instead of lazily rebuilding the document with schema defaults, and
ListConfigs serves every other module plus a per-module missing row that
the console renders as a badge. Ordinary modules keep their self-heal.

Refs: spec §4.2, §5 #10 #30"
```

---

### Task 5: Boot backfill — every schema key with a non-empty EnvVar/Default present after `SeedFromModules`

**Files:**
- Modify: `backend/pkg/sdk/module/config_service.go` (`SeedFromModules` existing-document branch; `backfillSchemaKeys`, `missingSchemaKeys`)
- Create: `backend/pkg/sdk/module/config_backfill_test.go`
- Modify: `backend/CLAUDE.md` (First-boot seeding paragraph), `backend/pkg/sdk/CLAUDE.md`

**Interfaces:**
- Consumes: Task 1 `CompareAndSwapConfig`, Task 2 `schemaFallbackValue`.
- Produces: `func (s *ModuleConfigService) backfillSchemaKeys(ctx, m Module, doc *ModuleConfig) (keys []string, wrote bool, err error)`; `func (s *ModuleConfigService) buildBackfill(m Module, schema []ConfigField, doc *ModuleConfig) (ConfigMutation, []string, bool)`; `func missingSchemaKeys(schema []ConfigField, values, encrypted map[string]string, encryptOnce func(ConfigField, string) (string, bool)) (map[string]string, map[string]string, []string)`; `const backfillMaxAttempts = 3`.

- [ ] **Step 1: Write the failing tests**

Create `config_backfill_test.go`:

```go
package module

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
)

type backfillModule struct{ BaseModule }

func (backfillModule) Name() string             { return "bf" }
func (backfillModule) Init(*Dependencies) error { return nil }
func (backfillModule) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "existing", Type: FieldString, Default: "d"},
		{Key: "cleared", Type: FieldString, Default: "d"},
		{Key: "toggle", Type: FieldBool, Default: "false"},
		{Key: "fromEnv", Type: FieldString, EnvVar: "BF_TEST_FROM_ENV", Default: "envdefault"},
		{Key: "noDefault", Type: FieldString},
		{Key: "secret", Type: FieldSecret, Default: "s3cr3t"},
		{Key: "list", Type: FieldRecordList, Items: []ConfigItemField{{Key: "host", Type: FieldString, Default: "h"}}},
	}
}

func backfillSvc(t *testing.T, doc *ModuleConfig) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["bf"] = doc
	return NewModuleConfigService(repo, fakeRedisClient{}, slog.Default()), repo
}

func TestSeedFromModules_BackfillsAbsentSchemaKeys(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "from-env")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "sandbox", ConfigRevision: 5,
		ConfigValues:    map[string]string{"existing": "legacy", "cleared": ""},
		EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
			"sandbox":    {ConfigValues: map[string]string{"existing": "sb", "cleared": ""}, EncryptedValues: map[string]string{}, Revision: 2},
		},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	sb := doc.Environments["sandbox"]
	// Absent keys gained their EnvVar/Default in the ACTIVE profile; the
	// mirror is an exact copy of the result.
	for _, m := range []map[string]string{sb.ConfigValues, doc.ConfigValues} {
		if m["toggle"] != "false" || m["fromEnv"] != "from-env" {
			t.Errorf("backfill missing: %v", m)
		}
		if _, ok := m["noDefault"]; ok {
			t.Error("a field with no EnvVar/Default must not be invented")
		}
		if v, ok := m["cleared"]; !ok || v != "" {
			t.Error("an explicitly empty stored value must be left alone")
		}
	}
	if sb.ConfigValues["existing"] != "sb" {
		t.Error("a present profile value was overwritten")
	}
	// The mirror is rebuilt from the ACTIVE profile, not backfilled on its
	// own: its stale "legacy" value is replaced by the profile's "sb".
	if doc.ConfigValues["existing"] != "sb" {
		t.Errorf("mirror existing = %q, want the profile's %q", doc.ConfigValues["existing"], "sb")
	}
	// Secrets go through the encrypted path, encrypted ONCE: profile and
	// mirror carry the identical ciphertext.
	plain, err := decryptSecret(sb.EncryptedValues["secret"])
	if err != nil || plain != "s3cr3t" || doc.EncryptedValues["secret"] == "" {
		t.Errorf("secret backfill: %q %v", plain, err)
	}
	if sb.EncryptedValues["secret"] != doc.EncryptedValues["secret"] {
		t.Error("secret was encrypted twice — profile and mirror must be identical")
	}
	// Record lists are schema-level constructs with nothing to seed.
	if _, ok := sb.ConfigValues["list"]; ok {
		t.Error("record list key must not be backfilled")
	}
	// The inactive profile is untouched; the revision advanced exactly once.
	if len(doc.Environments["production"].ConfigValues) != 0 {
		t.Error("inactive profile was backfilled")
	}
	if doc.ConfigRevision != 6 || sb.Revision != 3 || repo.docCasCalls != 1 {
		t.Errorf("revision=%d sbRevision=%d casCalls=%d, want 6/3/1", doc.ConfigRevision, sb.Revision, repo.docCasCalls)
	}
}

func TestSeedFromModules_CompleteDocumentIsNotRewritten(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	full := map[string]string{"existing": "x", "cleared": "", "toggle": "true", "fromEnv": "e", "noDefault": ""}
	withEncryptionKey(t)
	ct, _ := encryptSecret("s")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production", ConfigRevision: 9,
		ConfigValues: mergeStringMaps(full, nil), EncryptedValues: map[string]string{"secret": ct},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: mergeStringMaps(full, nil), EncryptedValues: map[string]string{"secret": ct}},
		},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	if repo.docs["bf"].ConfigRevision != 9 || repo.docCasCalls != 0 {
		t.Errorf("a complete document was rewritten: revision=%d casCalls=%d", repo.docs["bf"].ConfigRevision, repo.docCasCalls)
	}
}

// A legacy document with no profiles gets its mirror backfilled; the later
// lazy migration copies the complete mirror into the production profile.
func TestSeedFromModules_LegacyDocumentBackfillsMirrorOnly(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{ModuleName: "bf", ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	want := map[string]string{"existing": "d", "cleared": "d", "toggle": "false", "fromEnv": "envdefault"}
	if !reflect.DeepEqual(doc.ConfigValues, want) {
		t.Errorf("mirror = %v, want %v", doc.ConfigValues, want)
	}
	if len(doc.Environments) != 0 {
		t.Error("backfill must not invent profiles — that is the lazy migration's job")
	}
}

// A lost CAS means a concurrently booting replica wrote first — possibly an
// older binary that knew fewer keys. The document is re-read and the missing
// set recomputed, never assumed complete.
func TestSeedFromModules_LostCASIsRetriedAgainstTheFreshDocument(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production", ConfigRevision: 1,
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}},
	})
	// The other replica's write lands inside our window: it knew only
	// "toggle" (mirror only) and moved the revision. The hook is idempotent,
	// so firing again on the retry changes nothing.
	repo.beforeDocCAS = func() {
		d := repo.docs["bf"]
		d.ConfigValues["toggle"] = "false"
		d.ConfigRevision = 2
	}
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	if repo.docCasCalls != 2 {
		t.Errorf("docCasCalls = %d, want 2 (one lost, one won)", repo.docCasCalls)
	}
	if doc.ConfigRevision != 3 {
		t.Errorf("configRevision = %d, want 3", doc.ConfigRevision)
	}
	if doc.ConfigValues["existing"] != "d" || doc.ConfigValues["toggle"] != "false" {
		t.Errorf("mirror after retry: %v", doc.ConfigValues)
	}
	if doc.Environments["production"].ConfigValues["toggle"] != "false" || doc.Environments["production"].ConfigValues["existing"] != "d" {
		t.Errorf("profile after retry: %v", doc.Environments["production"].ConfigValues)
	}
}

// Keys whose EnvVar/Default is empty stay ABSENT: absence is meaningful to
// GetRawValue readers (ADR-0017 — an absent sessionAbsoluteTTL is the default
// cap, a present "" disables it), so inventing "" would change policy.
func TestSeedFromModules_EmptyFallbackKeysStayAbsent(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production",
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []map[string]string{repo.docs["bf"].ConfigValues, repo.docs["bf"].Environments["production"].ConfigValues} {
		if _, ok := m["noDefault"]; ok {
			t.Error("a key with an empty fallback was invented as \"\"")
		}
	}
}

func TestBackfillSchemaKeys_ReturnsSortedNamesWrittenOnce(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production",
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{"toggle": "true"}, EncryptedValues: map[string]string{}}},
	})
	keys, wrote, err := svc.backfillSchemaKeys(context.Background(), backfillModule{}, repo.docs["bf"])
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	// The keys added to the ACTIVE profile (toggle was already there); the
	// mirror is then a copy, so its own emptiness adds nothing to the list.
	want := []string{"cleared", "existing", "fromEnv", "secret"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if repo.docs["bf"].ConfigValues["toggle"] != "true" {
		t.Error("mirror must carry the profile's value, not a default")
	}
}

// A mirror that diverged from a complete profile is realigned to the
// profile — the profile is what the runtime and the admin UI read.
func TestSeedFromModules_MirrorIsRebuiltFromTheActiveProfile(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	withEncryptionKey(t)
	ct, _ := encryptSecret("s")
	full := map[string]string{"existing": "custom", "cleared": "", "toggle": "true", "fromEnv": "e", "noDefault": ""}
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production", ConfigRevision: 4,
		// Mirror: stale value, an extra key the profile does not have, and no secret.
		ConfigValues:    map[string]string{"existing": "stale", "orphan": "x", "toggle": "true", "fromEnv": "e", "cleared": "", "noDefault": ""},
		EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: mergeStringMaps(full, nil), EncryptedValues: map[string]string{"secret": ct}},
		},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	if !reflect.DeepEqual(doc.ConfigValues, full) || doc.EncryptedValues["secret"] != ct {
		t.Errorf("mirror = %v / %v, want an exact copy of the profile", doc.ConfigValues, doc.EncryptedValues)
	}
	if !reflect.DeepEqual(doc.Environments["production"].ConfigValues, full) {
		t.Error("a complete profile must not be touched")
	}
	if doc.ConfigRevision != 5 || repo.docCasCalls != 1 {
		t.Errorf("revision=%d casCalls=%d, want 5/1", doc.ConfigRevision, repo.docCasCalls)
	}
}

// A backfill failure is recorded so the required-module gate can refuse
// to serve; the boot itself continues (non-required modules degrade).
func TestSeedFromModules_BackfillFailureIsRecorded(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production",
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}},
	})
	repo.docCasFailures = backfillMaxAttempts // every attempt loses
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.seedFailures["bf"]; err == nil || !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("seedFailures[bf] = %v, want the recorded ErrRevisionStale", err)
	}
	if err := svc.RequirePersistedConfig(context.Background(), "bf"); err == nil {
		t.Fatal("the required-module gate must refuse a module whose backfill failed")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./pkg/sdk/module/ -run 'Backfill|SeedFromModules_' -count=1`
Expected: compile error `svc.backfillSchemaKeys undefined`; the SeedFromModules tests fail on the missing keys, the identical-ciphertext assertion and the retry count.

- [ ] **Step 3: Implement**

In `config_service.go`, inside `SeedFromModules`'s `existing != nil` branch, replace the `ClearNeedsRestart` block (lines 148-156) with:

```go
			// Backfill: RefreshMetadata refreshes the SCHEMA but has never
			// added the keys a schema gained after the document was created,
			// so a runtime read of such a key had to guess a default. After
			// this, every schema key with a non-empty fallback is present in
			// the active profile and the legacy mirror, and the runtime, the
			// validator and the admin UI all read the same document.
			keys, wrote, err := s.backfillSchemaKeys(ctx, m, existing)
			switch {
			case err != nil:
				// Recorded so RequirePersistedConfig can refuse to serve a
				// required module whose document may be incomplete.
				s.seedFailures[m.Name()] = err
				s.logger.Error("SeedFromModules: failed to backfill schema keys",
					slog.String("module", m.Name()),
					slog.String("error", err.Error()),
				)
			case len(keys) > 0:
				s.logger.Info("Module config backfilled with schema defaults",
					slog.String("module", m.Name()),
					slog.Any("keys", keys),
				)
			case wrote:
				s.logger.Info("Module config legacy mirror realigned to the active profile",
					slog.String("module", m.Name()),
				)
			}
			// Clear needsRestart for loaded modules — this flag should only
			// remain set for modules that are enabled in DB but not loaded. A
			// backfill write already persisted needsRestart=false in its own
			// update, so the second write is owed only when nothing was written.
			if !wrote {
				if err := s.repo.ClearNeedsRestart(ctx, m.Name()); err != nil {
					s.logger.Error("SeedFromModules: failed to clear needsRestart",
						slog.String("module", m.Name()),
						slog.String("error", err.Error()),
					)
				}
			}
			continue
```

Add `"maps"` to `config_service.go` imports, then the functions:

```go
// backfillMaxAttempts bounds the boot backfill's compare-and-swap retry. A
// lost race means a replica booted concurrently; re-reading and recomputing
// converges in one step, and three is plenty.
const backfillMaxAttempts = 3

// backfillSchemaKeys writes every schema key that is absent from the ACTIVE
// profile AND has a non-empty EnvVar/Default, then rewrites the legacy
// mirror to exactly the resulting profile, in ONE compare-and-swap that
// advances configRevision once. The profile is the source of truth (it is
// what ActiveConfigValues serves to the runtime and the admin UI); the
// mirror is never backfilled on its own, so a value present only in the
// profile reaches the mirror as that value, not as a schema default, and a
// mirror that had diverged is realigned. Secrets go through the same
// encrypted path first-boot seeding uses, each encrypted ONCE so the profile
// and the mirror carry identical ciphertext. Present profile keys, explicit
// empty strings included, are never touched.
//
// Keys whose fallback is empty stay ABSENT on purpose: absence is meaningful
// to GetRawValue readers (ADR-0017 D1 — an absent sessionAbsoluteTTL is the
// default cap, a present "" is "cap disabled"), so inventing "" would
// silently change policy. Record lists are schema-level constructs with
// nothing to seed. A document with no profiles gets only its mirror
// backfilled — the lazy migration copies it into the production profile
// later.
//
// NeedsRestart is written false: seeding runs before any module's Init, so
// every module reads the backfilled document and no restart is owed — the
// hot-reload resolver governs post-boot edits, not this one. Writing it here
// folds the ClearNeedsRestart that would otherwise follow into the same
// update.
//
// A lost compare-and-swap means a concurrently booting replica wrote first;
// the document is re-read and the missing set recomputed — that replica may
// run an older binary whose schema knows fewer keys. Returns the sorted,
// de-duplicated key names written; nil when nothing was missing (no write).
func (s *ModuleConfigService) backfillSchemaKeys(ctx context.Context, m Module, doc *ModuleConfig) (keys []string, wrote bool, err error) {
	schema := ConfigSchemaOf(m)
	for attempt := 0; attempt < backfillMaxAttempts; attempt++ {
		mut, added, write := s.buildBackfill(m, schema, doc)
		if !write {
			return nil, false, nil
		}
		won, err := s.repo.CompareAndSwapConfig(ctx, m.Name(), mut)
		if err != nil {
			return nil, false, err
		}
		if won {
			return added, true, nil
		}
		fresh, err := s.repo.FindByName(ctx, m.Name())
		if err != nil {
			return nil, false, err
		}
		if fresh == nil {
			return nil, false, fmt.Errorf("module %q: document disappeared during backfill", m.Name())
		}
		doc = fresh
	}
	return nil, false, fmt.Errorf("module %q: %w (backfill)", m.Name(), ErrRevisionStale)
}

// buildBackfill computes the mutation for one document. Profiles are the
// source of truth: the candidate is the ACTIVE profile plus its missing
// defaults, and the legacy mirror is rewritten to exactly that candidate —
// never backfilled on its own, which would hand a key present only in the
// profile a schema default instead of the profile's value. A document with
// no profiles (not yet migrated) backfills its mirror alone. Every secret
// is encrypted once. Returns the mutation, the keys added to the profile
// (or mirror, for a legacy document), and whether anything needs writing —
// a mirror that merely diverged from a complete profile is realigned too.
func (s *ModuleConfigService) buildBackfill(m Module, schema []ConfigField, doc *ModuleConfig) (mut ConfigMutation, added []string, write bool) {
	ciphertext := map[string]string{} // key → ciphertext, encrypted once per key
	encryptOnce := func(f ConfigField, plain string) (string, bool) {
		if enc, ok := ciphertext[f.Key]; ok {
			return enc, true
		}
		enc, err := encryptSecret(plain)
		if err != nil {
			// Same posture as first-boot seeding: warn and skip the secret,
			// never fail the boot over a missing OAUTH_TOKEN_ENCRYPTION_KEY.
			s.logger.Warn("SeedFromModules: failed to encrypt backfilled secret, skipping",
				slog.String("module", m.Name()), slog.String("field", f.Key), slog.String("error", err.Error()))
			return "", false
		}
		ciphertext[f.Key] = enc
		return enc, true
	}
	mut = ConfigMutation{ExpectedRevision: doc.ConfigRevision, NeedsRestart: false}

	if len(doc.Environments) == 0 {
		values, secrets, keys := missingSchemaKeys(schema, doc.ConfigValues, doc.EncryptedValues, encryptOnce)
		if len(keys) == 0 {
			return mut, nil, false
		}
		mut.WriteLegacy, mut.LegacyValues, mut.LegacySecrets = true, values, secrets
		sort.Strings(keys)
		return mut, keys, true
	}

	env := doc.ActiveEnv()
	cur, ok := doc.Environments[env]
	if !ok {
		return mut, nil, false
	}
	values, secrets, keys := missingSchemaKeys(schema, cur.ConfigValues, cur.EncryptedValues, encryptOnce)
	mirrorDiverged := !maps.Equal(doc.ConfigValues, values) || !maps.Equal(doc.EncryptedValues, secrets)
	if len(keys) == 0 && !mirrorDiverged {
		return mut, nil, false
	}
	mut.Env, mut.EnvValues, mut.EnvSecrets, mut.EnvRevision = env, values, secrets, cur.Revision
	mut.WriteLegacy, mut.LegacyValues, mut.LegacySecrets = true, values, secrets
	sort.Strings(keys) // missingSchemaKeys adds each schema key at most once
	return mut, keys, true
}

// missingSchemaKeys returns copies of values/secrets with every absent
// schema key whose EnvVar/Default is non-empty added, plus the keys added.
// Secrets are obtained through encryptOnce so a key missing from both the
// profile and the mirror is encrypted a single time.
func missingSchemaKeys(schema []ConfigField, values, encrypted map[string]string, encryptOnce func(ConfigField, string) (string, bool)) (map[string]string, map[string]string, []string) {
	outValues := mergeStringMaps(values, nil)
	outSecrets := mergeStringMaps(encrypted, nil)
	var added []string
	for _, f := range schema {
		if f.Type == FieldRecordList {
			continue
		}
		v := schemaFallbackValue(f)
		if v == "" {
			continue
		}
		if f.Type == FieldSecret {
			if _, ok := outSecrets[f.Key]; ok {
				continue
			}
			enc, ok := encryptOnce(f, v)
			if !ok {
				continue
			}
			outSecrets[f.Key] = enc
		} else {
			if _, ok := outValues[f.Key]; ok {
				continue
			}
			outValues[f.Key] = v
		}
		added = append(added, f.Key)
	}
	return outValues, outSecrets, added
}
```

- [ ] **Step 4: Run**

Run: `cd backend && go test ./pkg/sdk/module/ -count=1`
Expected: PASS.

- [ ] **Step 5: Amend the spec's wording, docs, commit**

The spec (§4.4 line 370, §5 #14, §6 `config_backfill_test.go` row) says "every schema key absent … is written with its `EnvVar`/`Default` value" and "a document in which every schema key is present". That is not what ships, on purpose (deviation #10): a key with an empty fallback stays absent. Amend `docs/superpowers/specs/2026-08-29-password-login-toggle-design.md` in this commit so the contract and the code say the same thing:

- §4.4, the `SeedFromModules` backfill sentence → "every schema key absent from the active environment **whose `EnvVar`/`Default` is non-empty** is written with that value, secrets included through the same encrypted path, **and the legacy mirror is rewritten as an exact copy of the resulting profile** (profiles are the source of truth; a mirror is never backfilled on its own), `configRevision` advanced once, and the backfilled key names logged at INFO. A key whose fallback is empty stays absent: absence is meaningful to `GetRawValue` readers (ADR-0017 D1 — an absent `sessionAbsoluteTTL` is the default cap, a present empty one disables it), and a runtime read of such a key needs no guess because its answer is empty either way."
- §4.4, "a document in which every schema key is present" → "a document in which every schema key that has a value to be present with is present".
- §5 #14, "writes every other absent schema key with its default before traffic" → "writes every other absent schema key that has a non-empty default before traffic".
- §6, the `config_backfill_test.go` row: "gains them with `EnvVar`/`Default` values" → "gains those with a non-empty `EnvVar`/`Default`"; add "the legacy mirror is rebuilt from the active profile; a lost CAS is re-read and retried; a failure is recorded and refused by the required-module gate".
- §0 revision log: append "v4.1 (planning, 2026-08-30): backfill scoped to non-empty fallbacks and mirror-from-profile; `RequirePersistedConfig` is the boot gate for required modules; `ConfigRepository` change declared as an additive-only exception (§4.5); server-side lane validation `module.config_key_invalid` (§4.5)."

`backend/CLAUDE.md` — in the **First-boot seeding** paragraph, after "Subsequent boots ignore env defaults; admin-set values in `module_configs` are authoritative." insert: "Subsequent boots also **backfill** any schema key an existing document lacks (a key the schema gained after the document was created) with its `EnvVar`/`Default` **when that fallback is non-empty** — present values, explicit empty strings included, are never touched, and a key whose fallback is empty stays absent because absence is meaningful to `GetRawValue` readers (ADR-0017: an absent `sessionAbsoluteTTL` is the default cap, a present empty one disables it). The backfilled key names are logged at INFO. So a runtime read never has to guess a default for a key the document was created without."

`backend/pkg/sdk/CLAUDE.md` — add to the `RequirePersistedConfig` bullet's neighbourhood: "**`SeedFromModules` backfills absent schema keys with a non-empty `EnvVar`/`Default` on existing documents** (the active profile gains its defaults and the legacy mirror is rewritten as an exact copy of it — never backfilled on its own; each secret encrypted once; one CAS retried on a lost race; `configRevision +1`; `needsRestart=false` in the same write; INFO log of the names; a failure is recorded for `RequirePersistedConfig` to refuse a required module). Empty-fallback keys stay absent — presence is a signal to `GetRawValue` readers (ADR-0017). A schema default therefore never has to be re-implemented as a runtime guess."

```bash
cd backend && go vet ./pkg/sdk/... && cd ..
git add backend/pkg/sdk/module/config_service.go backend/pkg/sdk/module/config_backfill_test.go backend/CLAUDE.md backend/pkg/sdk/CLAUDE.md docs/superpowers/specs/2026-08-29-password-login-toggle-design.md
git commit -m "feat(sdk): SeedFromModules backfills schema keys with a non-empty fallback

Every schema key absent from the ACTIVE profile whose EnvVar/Default is
non-empty is written with that value (secrets encrypted once), and the
legacy mirror is rewritten as an exact copy of the resulting profile, in
one CAS before traffic — retried on a lost race, recorded on failure so
RequirePersistedConfig refuses to serve a required module. Empty-fallback
keys stay absent (ADR-0017 presence semantics). Closes the drift where
provider toggles default false in the schema but were absent (and read
as true) in pre-release documents. Spec §4.4/§5/§6 amended to match.

Refs: spec §4.4, §5 #14"
```

---

### Task 6: One best-effort audit event per mutation result; config before lifecycle

**Files:**
- Create: `backend/pkg/sdk/module/admin_audit.go`, `backend/pkg/sdk/module/admin_audit_test.go`
- Modify: `backend/pkg/sdk/module/handler.go` (`ModuleAdminHandler` fields, `UpdateModule` reorder + `enableModule`/`disableModule`, `UpdateEnvironment` + `SetActiveEnvironment` emit, `schemaFor`, `fmt.Printf` → logger)
- Create: `backend/internal/shared/middleware/request_meta.go`, `backend/internal/shared/middleware/request_meta_test.go`
- Modify: `backend/internal/core/compliance/services/sink.go:58-63` (WARN carries action/resource/outcome), `backend/internal/core/compliance/services/sink_test.go` (failure-path test)
- Modify: `backend/cmd/server/admin_wiring.go`, `backend/cmd/server/admin_wiring_test.go`, `backend/cmd/server/main.go:523-541`
- Modify: `backend/internal/core/compliance/CLAUDE.md`, `docs/site/modules/core/compliance.mdx`, `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/config-service.mdx`, `backend/CLAUDE.md`

**Interfaces:**
- Consumes: `iface.AuditSink` / `iface.AuditEvent` (`pkg/sdk/iface/interfaces.go:966-993`), `ctxauth.GetUserUUID/GetTenantID/GetClientIP/TenantKindFromContext`, `chiMiddleware.GetReqID`, `module.GetTyped[iface.AuditSink](svcs, module.ServiceAuditSink)`.
- Produces: `type AdminActor struct{UserID, TenantID, TenantKind, IP, UserAgent, RequestID string}`; `func (h *ModuleAdminHandler) SetAuditSink(iface.AuditSink)`; `func (h *ModuleAdminHandler) SetActorResolver(func(context.Context) AdminActor)`; constants `ActionModuleConfigUpdated = "module.config.updated"`, `ActionModuleEnvironmentActivated = "module.config.environment_activated"`, `ActionModuleEnabled = "module.enabled"`, `ActionModuleDisabled = "module.disabled"`; `func auditKeyNames(schema []ConfigField, submitted map[string]string) ([]string, int)`; `middleware.RequestMeta` + `middleware.UserAgentFromContext(ctx) string`; `func adminActorResolver(ctx) module.AdminActor`, `func wireModuleAdminAudit(h *module.ModuleAdminHandler, svcs *module.ServiceRegistry) error` in `cmd/server`.

- [ ] **Step 1: Write the failing handler tests**

Create `admin_audit_test.go`:

```go
package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

type recordingSink struct {
	events []iface.AuditEvent
	panics bool
}

func (r *recordingSink) Emit(_ context.Context, ev iface.AuditEvent) {
	if r.panics {
		panic("sink exploded")
	}
	r.events = append(r.events, ev)
}

// auditDemoModule is a toggleable module with a scalar, a secret and a
// record list, whose Start can be made to fail.
type auditDemoModule struct {
	BaseModule
	startErr error
}

func (m *auditDemoModule) Name() string             { return "demo" }
func (m *auditDemoModule) Category() ModuleCategory { return CategoryToggleable }
func (m *auditDemoModule) Init(*Dependencies) error { return nil }
func (m *auditDemoModule) Start(context.Context) error {
	return m.startErr
}
func (m *auditDemoModule) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "flag", Type: FieldBool, Default: "false"},
		{Key: "apiKey", Type: FieldSecret},
		{Key: "email.profiles", Type: FieldRecordList, Items: []ConfigItemField{
			{Key: "host", Type: FieldString}, {Key: "password", Type: FieldSecret},
		}},
	}
}
func (m *auditDemoModule) ValidateConfig(_ context.Context, v map[string]string) error {
	if v["flag"] == "bad" {
		return &ConfigValidationError{Field: "flag", Message: "no", Code: "demo.flag_invalid"}
	}
	return nil
}

func newAuditHandler(t *testing.T, mod *auditDemoModule) (*ModuleAdminHandler, *fakeConfigRepo, *recordingSink) {
	t.Helper()
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName: "demo", Category: CategoryToggleable, Enabled: false, ActiveEnvironment: "production",
		ConfigSchema:    mod.ConfigSchema(),
		ConfigValues:    map[string]string{"flag": "false"},
		EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"flag": "false"}, EncryptedValues: map[string]string{}},
			"sandbox":    {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
		},
	}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{mod})
	reg := NewModuleRegistry(slog.Default())
	reg.Register(mod)
	h := NewModuleAdminHandler(svc, reg)
	sink := &recordingSink{}
	h.SetAuditSink(sink)
	h.SetActorResolver(func(context.Context) AdminActor {
		return AdminActor{UserID: "u-1", TenantID: "t-1", TenantKind: "internal", IP: "203.0.113.9", UserAgent: "UA/1", RequestID: "req-42"}
	})
	return h, repo, sink
}

func patchConfig(cfg, secrets map[string]string) *UpdateModuleInput {
	in := &UpdateModuleInput{Name: "demo"}
	in.Body.Config, in.Body.Secrets = cfg, secrets
	return in
}

func TestAudit_ConfigUpdateSuccessCarriesNamesNeverValues(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	_, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, map[string]string{"apiKey": "hunter2-value"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Action != ActionModuleConfigUpdated || ev.Outcome != "success" || ev.ResourceType != "module" || ev.ResourceID != "demo" {
		t.Errorf("event shape: %+v", ev)
	}
	if ev.ActorUserID != "u-1" || ev.TenantID != "t-1" || ev.TenantKind != "internal" || ev.IPAddress != "203.0.113.9" || ev.UserAgent != "UA/1" || ev.ActorType != "user" {
		t.Errorf("actor: %+v", ev)
	}
	if ev.ActorEmail != "" {
		t.Error("actor email must never be recorded")
	}
	if !reflect.DeepEqual(ev.Metadata["keys"], []string{"flag"}) || !reflect.DeepEqual(ev.Metadata["secretKeys"], []string{"apiKey"}) {
		t.Errorf("metadata keys: %v", ev.Metadata)
	}
	if ev.Metadata["env"] != "production" || ev.Metadata["requestId"] != "req-42" {
		t.Errorf("metadata env/requestId: %v", ev.Metadata)
	}
	if _, ok := ev.Metadata["code"]; ok {
		t.Error("a success carries no code")
	}
	dump := fmt.Sprintf("%v", ev)
	if strings.Contains(dump, "hunter2") || strings.Contains(dump, "true") {
		t.Errorf("a value reached the audit event: %s", dump)
	}
}

func TestAudit_ValidationAndStaleFailuresCarryCodes(t *testing.T) {
	h, repo, sink := newAuditHandler(t, &auditDemoModule{})
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "bad"}, nil)); err == nil {
		t.Fatal("expected 422")
	}
	repo.docCasFailures = 1
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil)); err == nil {
		t.Fatal("expected 409")
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Outcome != "failure" || sink.events[0].Metadata["code"] != "demo.flag_invalid" {
		t.Errorf("422 event: %+v", sink.events[0])
	}
	if sink.events[1].Outcome != "failure" || sink.events[1].Metadata["code"] != CodeConfigRevisionStale {
		t.Errorf("409 event: %+v", sink.events[1])
	}
}

func TestAudit_RecordListKeysCollapseAndUnknownKeysAreCounted(t *testing.T) {
	schema := (&auditDemoModule{}).ConfigSchema()
	keys, unknown := auditKeyNames(schema, map[string]string{
		"email.profiles.acme.host":    "h",
		"email.profiles.acme.__label": "Acme",
		"email.profiles.other.host":   "h2",
		"email.profiles.__items":      "acme,other",
		"flag":                        "true",
		"totally-made-up<script>":     "x",
	}, false)
	want := []string{"email.profiles", "email.profiles.__label", "email.profiles.host", "flag"}
	if !reflect.DeepEqual(keys, want) || unknown != 1 {
		t.Errorf("keys=%v unknown=%d, want %v / 1", keys, unknown, want)
	}
	// A key filed in the wrong block is counted, never reported under the
	// name it borrows: a secret in the config block, a value in secrets.
	if keys, unknown := auditKeyNames(schema, map[string]string{"apiKey": "x", "email.profiles.acme.password": "p"}, false); len(keys) != 0 || unknown != 2 {
		t.Errorf("secrets in the config block: keys=%v unknown=%d, want [] / 2", keys, unknown)
	}
	keys, unknown = auditKeyNames(schema, map[string]string{"apiKey": "x", "email.profiles.acme.password": "p", "flag": "misfiled", "email.profiles.__items": "a"}, true)
	if !reflect.DeepEqual(keys, []string{"apiKey", "email.profiles.password"}) || unknown != 2 {
		t.Errorf("secrets block: keys=%v unknown=%d, want [apiKey email.profiles.password] / 2", keys, unknown)
	}
	big := map[string]string{}
	for i := 0; i < 100; i++ {
		big[fmt.Sprintf("email.profiles.p%03d.host", i)] = "x"
	}
	keys, _ = auditKeyNames(schema, big, false)
	if len(keys) != 1 {
		t.Errorf("100 element keys collapse to one schema name, got %v", keys)
	}
	if keys, _ := auditKeyNames(nil, map[string]string{}, false); len(keys) != 0 || keys == nil {
		t.Errorf("empty submission must yield an empty (non-nil) list, got %v", keys)
	}
	summary, fields, unknown := auditRecordLists(schema, []RecordListMutation{
		{Field: "email.profiles", Create: []string{"a", "b"}, Remove: []string{"c"}},
		{Field: "not-declared", Remove: []string{"x"}},
	})
	if len(summary) != 1 || summary[0]["created"] != 2 || summary[0]["removed"] != 1 || !reflect.DeepEqual(fields, []string{"email.profiles"}) || unknown != 1 {
		t.Errorf("record-list summary: %v fields=%v unknown=%d", summary, fields, unknown)
	}
	// Counts only: the summary row carries exactly field/created/removed,
	// so no slug can be in it.
	if len(summary[0]) != 3 {
		t.Errorf("summary row must carry field/created/removed only, got %v", summary[0])
	}
}

// A membership-only removal touches no value key; the event still names the
// field and carries the counts.
func TestAudit_MembershipOnlyRemovalIsVisible(t *testing.T) {
	h, repo, sink := newAuditHandler(t, &auditDemoModule{})
	prod := repo.docs["demo"].Environments["production"]
	prod.ConfigValues["email.profiles.__items"] = "acme"
	prod.ConfigValues["email.profiles.acme.host"] = "h"
	repo.docs["demo"].Environments["production"] = prod
	rev := int64(0)
	in := &UpdateEnvironmentInput{Name: "demo", Env: "production"}
	in.Body.RecordLists = []RecordListMutationDTO{{Field: "email.profiles", Remove: []string{"acme"}}}
	in.Body.Revision = &rev
	if _, err := h.UpdateEnvironment(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	ev := sink.events[len(sink.events)-1]
	if !reflect.DeepEqual(ev.Metadata["keys"], []string{"email.profiles"}) {
		t.Errorf("keys = %v, want the record-list field", ev.Metadata["keys"])
	}
	lists, _ := ev.Metadata["recordLists"].([]map[string]any)
	if len(lists) != 1 || lists[0]["removed"] != 1 {
		t.Errorf("recordLists = %v", ev.Metadata["recordLists"])
	}
	if strings.Contains(fmt.Sprint(ev.Metadata), "acme") {
		t.Error("a slug reached the audit event")
	}
}

// G7: a mutation that reaches the handler is audited even when it cannot be
// dispatched — the document read fails, or the module does not exist.
func TestAudit_AbortBeforeDispatchStillEmits(t *testing.T) {
	h, repo, sink := newAuditHandler(t, &auditDemoModule{})
	enabled := true
	in := patchConfig(map[string]string{"flag": "true"}, nil)
	in.Body.Enabled = &enabled
	repo.findErr = errors.New("mongo down")
	if _, err := h.UpdateModule(context.Background(), in); err == nil {
		t.Fatal("expected the read failure to surface")
	}
	repo.findErr = nil
	if len(sink.events) != 2 || sink.events[0].Action != ActionModuleConfigUpdated || sink.events[1].Action != ActionModuleEnabled {
		t.Fatalf("read failure must audit both intended halves as failures, got %+v", sink.events)
	}
	for _, ev := range sink.events {
		if ev.Outcome != "failure" {
			t.Errorf("outcome = %q, want failure", ev.Outcome)
		}
	}
	sink.events = nil
	unknown := patchConfig(map[string]string{"flag": "true"}, nil)
	unknown.Name = "no-such-module"
	if _, err := h.UpdateModule(context.Background(), unknown); err == nil {
		t.Fatal("expected 404")
	}
	if len(sink.events) != 1 || sink.events[0].ResourceID != "no-such-module" || sink.events[0].Outcome != "failure" {
		t.Errorf("404 must still be audited, got %+v", sink.events)
	}
}

func TestAudit_UserAgentIsBounded(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	h.SetActorResolver(func(context.Context) AdminActor {
		return AdminActor{UserID: "u-1", UserAgent: strings.Repeat("x", 1000)}
	})
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil)); err != nil {
		t.Fatal(err)
	}
	if got := len(sink.events[0].UserAgent); got != auditMaxUserAgent {
		t.Errorf("UserAgent length = %d, want %d", got, auditMaxUserAgent)
	}
}

func TestAudit_ConfigFailureNeverStartsTheModule(t *testing.T) {
	mod := &auditDemoModule{}
	h, _, sink := newAuditHandler(t, mod)
	enabled := true
	in := patchConfig(map[string]string{"flag": "bad"}, nil)
	in.Body.Enabled = &enabled
	if _, err := h.UpdateModule(context.Background(), in); err == nil {
		t.Fatal("expected 422")
	}
	if h.registry.IsStarted("demo") {
		t.Fatal("a rejected config change still started the module")
	}
	if len(sink.events) != 1 || sink.events[0].Action != ActionModuleConfigUpdated {
		t.Errorf("only the config failure is audited, got %+v", sink.events)
	}
}

func TestAudit_ConfigSucceedsThenLifecycleFails_BothReported(t *testing.T) {
	mod := &auditDemoModule{startErr: errors.New("boom")}
	h, repo, sink := newAuditHandler(t, mod)
	enabled := true
	in := patchConfig(map[string]string{"flag": "true"}, nil)
	in.Body.Enabled = &enabled
	if _, err := h.UpdateModule(context.Background(), in); err == nil {
		t.Fatal("expected the start failure to surface")
	}
	if repo.docs["demo"].ConfigValues["flag"] != "true" {
		t.Error("config must remain changed when the later lifecycle step fails")
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Action != ActionModuleConfigUpdated || sink.events[0].Outcome != "success" {
		t.Errorf("config event: %+v", sink.events[0])
	}
	if sink.events[1].Action != ActionModuleEnabled || sink.events[1].Outcome != "failure" {
		t.Errorf("enable event: %+v", sink.events[1])
	}
}

func TestAudit_EnableAndDisableAreSeparateEvents(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	on, off := true, false
	in := &UpdateModuleInput{Name: "demo"}
	in.Body.Enabled = &on
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.Body.Enabled = &off
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 || sink.events[0].Action != ActionModuleEnabled || sink.events[1].Action != ActionModuleDisabled {
		t.Errorf("events: %+v", sink.events)
	}
	if sink.events[0].Outcome != "success" || sink.events[1].Outcome != "success" {
		t.Errorf("outcomes: %+v", sink.events)
	}
}

func TestAudit_EnvironmentAndActivationSurfaces(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	env := &UpdateEnvironmentInput{Name: "demo", Env: "sandbox"}
	env.Body.Config = map[string]string{"flag": "true"}
	if _, err := h.UpdateEnvironment(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	act := &SetActiveEnvironmentInput{Name: "demo"}
	act.Body.Environment = "sandbox"
	if _, err := h.SetActiveEnvironment(context.Background(), act); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Action != ActionModuleConfigUpdated || sink.events[0].Metadata["env"] != "sandbox" {
		t.Errorf("env PATCH event: %+v", sink.events[0])
	}
	if sink.events[1].Action != ActionModuleEnvironmentActivated || sink.events[1].Metadata["env"] != "sandbox" || sink.events[1].Outcome != "success" {
		t.Errorf("activation event: %+v", sink.events[1])
	}
}

func TestAudit_NilSinkAndResolverTolerated_PanickingSinkContained(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	h.SetAuditSink(nil)
	h.SetActorResolver(nil)
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil)); err != nil {
		t.Fatalf("nil sink/resolver: %v", err)
	}
	sink.panics = true
	h.SetAuditSink(sink)
	out, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "false"}, nil))
	if err != nil || out == nil || out.Body.ConfigValues["flag"] != "false" {
		t.Fatalf("a failing sink changed the HTTP result: out=%v err=%v", out, err)
	}
}
```

Add `findErr error` to `fakeConfigRepo` (doc: "when set, FindByName fails with it — models a repository outage") and make `FindByName` start with `if f.findErr != nil { return nil, f.findErr }`.

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./pkg/sdk/module/ -run 'TestAudit' -count=1`
Expected: compile errors (`h.SetAuditSink undefined`, `AdminActor` undefined, constants undefined, `auditRecordLists` undefined).

- [ ] **Step 3: Implement `admin_audit.go`**

```go
package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Module-config audit vocabulary. Dotted so a reader filters by prefix;
// outcomes reuse the compliance model's existing "success" / "failure".
const (
	ActionModuleConfigUpdated        = "module.config.updated"
	ActionModuleEnvironmentActivated = "module.config.environment_activated"
	ActionModuleEnabled              = "module.enabled"
	ActionModuleDisabled             = "module.disabled"

	auditResourceTypeModule = "module"
	auditOutcomeSuccess     = "success"
	auditOutcomeFailure     = "failure"
	// auditMaxKeys bounds the key lists an event carries; auditMaxUserAgent
	// bounds the one free-text header the event keeps.
	auditMaxKeys      = 64
	auditMaxUserAgent = 256
)

// AdminActor is the request principal recorded on a module-config audit
// event. The host resolves it (SetActorResolver) because pkg/sdk cannot
// import the backend's auth middleware. Email is deliberately absent: the
// immutable user UUID is sufficient attribution and avoids duplicating
// mutable PII into a two-year audit store.
type AdminActor struct {
	UserID     string
	TenantID   string
	TenantKind string
	IP         string
	UserAgent  string
	RequestID  string
}

// SetAuditSink installs the platform audit sink. Nil-tolerant: SDK
// embedders and isolated tests may run without one; the in-tree server
// wiring requires it (cmd/server/admin_wiring.go).
func (h *ModuleAdminHandler) SetAuditSink(sink iface.AuditSink) { h.auditSink = sink }

// SetActorResolver installs the host's principal resolver. Nil-tolerant;
// without it events carry a "system" actor.
func (h *ModuleAdminHandler) SetActorResolver(fn func(context.Context) AdminActor) { h.actorResolver = fn }

// auditRecord is one mutation attempt's outcome as the handler observed it.
type auditRecord struct {
	action      string
	module      string
	env         string
	err         error                // nil on success
	config      map[string]string    // submitted non-secret values — names only are recorded
	secrets     map[string]string    // submitted secrets — names only are recorded
	recordLists []RecordListMutation // membership intents — field + counts only are recorded
}

// emitAudit hands one event to the sink. Best-effort by contract: Emit
// returns nothing, the compliance sink logs its own insert failures, and a
// sink that panics is recovered here so the HTTP result of a mutation that
// already happened never changes. Values never enter the event — only
// schema-derived key names, the stable error code and request provenance.
func (h *ModuleAdminHandler) emitAudit(ctx context.Context, rec auditRecord) {
	if h.auditSink == nil {
		return
	}
	var actor AdminActor
	if h.actorResolver != nil {
		actor = h.actorResolver(ctx)
	}
	schema := h.schemaFor(rec.module)
	keys, unknown := auditKeyNames(schema, rec.config, false)
	secretKeys, unknownSecrets := auditKeyNames(schema, rec.secrets, true)
	lists, listFields, unknownLists := auditRecordLists(schema, rec.recordLists)
	if len(listFields) > 0 {
		// A membership-only save touches no value key; the record-list field
		// itself is what changed.
		merged := map[string]bool{}
		for _, k := range append(keys, listFields...) {
			merged[k] = true
		}
		keys = boundedNames(merged)
	}

	meta := map[string]any{"keys": keys, "secretKeys": secretKeys}
	if len(lists) > 0 {
		meta["recordLists"] = lists
	}
	if rec.env != "" {
		meta["env"] = rec.env
	}
	if n := unknown + unknownSecrets + unknownLists; n > 0 {
		meta["unknownKeyCount"] = n
	}
	if code := auditErrorCode(rec.err); code != "" {
		meta["code"] = code
	}
	if actor.RequestID != "" {
		meta["requestId"] = actor.RequestID
	}
	outcome := auditOutcomeSuccess
	if rec.err != nil {
		outcome = auditOutcomeFailure
	}
	actorType := "system"
	if actor.UserID != "" {
		actorType = "user"
	}
	ev := iface.AuditEvent{
		TenantID:     actor.TenantID,
		TenantKind:   actor.TenantKind,
		ActorUserID:  actor.UserID,
		ActorType:    actorType,
		Action:       rec.action,
		ResourceType: auditResourceTypeModule,
		ResourceID:   rec.module,
		Outcome:      outcome,
		IPAddress:    actor.IP,
		UserAgent:    boundString(actor.UserAgent, auditMaxUserAgent),
		Metadata:     meta,
	}
	defer func() {
		if r := recover(); r != nil {
			// The panic value is the sink's own text; only its type is logged.
			h.logger().Warn("module admin audit: sink failed",
				slog.String("action", rec.action),
				slog.String("module", rec.module),
				slog.String("outcome", outcome),
				slog.String("panic", fmt.Sprintf("%T", r)))
		}
	}()
	h.auditSink.Emit(ctx, ev)
}

func boundString(v string, max int) string {
	if len(v) <= max {
		return v
	}
	return v[:max]
}

// auditKeyNames reduces a submitted key set to schema-derived names. secret
// selects which half of the schema the keys must belong to: a key submitted
// in the config block must be declared as a non-secret field (or a
// record-list roster / label / non-secret sub-field); a key in the secrets
// block must be a declared secret (or a secret sub-field). Anything else —
// undeclared, or declared with the other type — contributes only to the
// returned count, so a misfiled key is never reported as if it were the
// field it names. An element key <field>.<slug>.<sub> collapses to
// <field>.<sub>, the roster key to <field>, a label key to <field>.__label.
// Sorted, de-duplicated, capped — operator-supplied text never reaches the
// audit store.
func auditKeyNames(schema []ConfigField, submitted map[string]string, secret bool) ([]string, int) {
	if len(submitted) == 0 {
		return []string{}, 0
	}
	set := map[string]bool{}
	declared := map[string]bool{}
	var lists []ConfigField
	for _, f := range schema {
		switch {
		case f.Type == FieldRecordList:
			lists = append(lists, f)
		case (f.Type == FieldSecret) == secret:
			declared[f.Key] = true
		}
	}
	unknown := 0
	for key := range submitted {
		if declared[key] {
			set[key] = true
			continue
		}
		if !collapseRecordListKey(lists, key, secret, set) {
			unknown++
		}
	}
	return boundedNames(set), unknown
}

func collapseRecordListKey(lists []ConfigField, key string, secret bool, set map[string]bool) bool {
	for _, f := range lists {
		if !secret && key == RosterKey(f.Key) {
			set[f.Key] = true
			return true
		}
		_, sub, ok := SplitElementKey(f.Key, key)
		if !ok {
			continue
		}
		if !secret && sub == labelSuffix {
			set[f.Key+"."+labelSuffix] = true
			return true
		}
		for _, it := range f.Items {
			if it.Key == sub && (it.Type == FieldSecret) == secret {
				set[f.Key+"."+sub] = true
				return true
			}
		}
	}
	return false
}

// auditRecordLists summarizes membership intents: per declared record-list
// field, how many elements were created and removed. Slugs are operator
// text and are never recorded; an intent on an undeclared field counts as
// unknown.
func auditRecordLists(schema []ConfigField, mutations []RecordListMutation) (summary []map[string]any, fields []string, unknown int) {
	declared := map[string]bool{}
	for _, f := range schema {
		if f.Type == FieldRecordList {
			declared[f.Key] = true
		}
	}
	summary = []map[string]any{}
	for _, m := range mutations {
		if !declared[m.Field] {
			unknown++
			continue
		}
		fields = append(fields, m.Field)
		summary = append(summary, map[string]any{"field": m.Field, "created": len(m.Create), "removed": len(m.Remove)})
	}
	return summary, fields, unknown
}

func boundedNames(set map[string]bool) []string {
	names := sortedKeys(set)
	if len(names) > auditMaxKeys {
		names = names[:auditMaxKeys]
	}
	return names
}


func auditErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var invalid *ConfigValidationError
	if errors.As(err, &invalid) {
		return invalid.Code
	}
	if errors.Is(err, ErrRevisionStale) {
		return CodeConfigRevisionStale
	}
	return ""
}

// schemaFor reads the declared schema from the registered module — the
// binary's schema, not the stored copy — so key collapsing never depends on
// a document read that may itself have failed.
func (h *ModuleAdminHandler) schemaFor(name string) []ConfigField {
	for _, m := range h.registry.AllModules() {
		if m.Name() == name {
			return ConfigSchemaOf(m)
		}
	}
	return nil
}

func (h *ModuleAdminHandler) logger() *slog.Logger {
	if h.configService != nil && h.configService.logger != nil {
		return h.configService.logger
	}
	return slog.Default()
}
```

- [ ] **Step 4: Rewire `handler.go`**

Struct + constructor:

```go
type ModuleAdminHandler struct {
	configService *ModuleConfigService
	registry      *ModuleRegistry
	auditSink     iface.AuditSink
	actorResolver func(context.Context) AdminActor
}
```

(add the `iface` import). Replace `UpdateModule` (lines 227-315) with:

```go
// UpdateModule updates a module's configuration and/or enabled state.
//
// Order is the substance: the config half — candidate validation AND the
// compare-and-swap write — completes before any lifecycle side effect, so a
// 422, a stale-revision 409 or an infrastructure failure can never still
// start or stop the module. If config succeeds and the later lifecycle step
// fails, the config stays changed and the two audit events report those
// distinct actual results; the response is still an error.
func (h *ModuleAdminHandler) UpdateModule(ctx context.Context, input *UpdateModuleInput) (*UpdateModuleOutput, error) {
	existing, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		h.auditAborted(ctx, input, err)
		return nil, mapConfigReadError(err)
	}
	if existing == nil {
		notFound := huma.Error404NotFound(fmt.Sprintf("module %q not found", input.Name))
		h.auditAborted(ctx, input, notFound)
		return nil, notFound
	}

	if len(input.Body.Config) > 0 || len(input.Body.Secrets) > 0 {
		// UpdateConfig merges into the stored config — keys the caller omits
		// are preserved, so a config-only change never wipes the module's secrets.
		err := h.configService.UpdateConfig(ctx, input.Name, input.Body.Config, input.Body.Secrets)
		h.emitAudit(ctx, auditRecord{
			action: ActionModuleConfigUpdated, module: input.Name, env: existing.ActiveEnv(),
			config: input.Body.Config, secrets: input.Body.Secrets, err: err,
		})
		if err != nil {
			return nil, mapConfigServiceError(err, func(e error) error { return e })
		}
	}

	if input.Body.Enabled != nil {
		if *input.Body.Enabled {
			err = h.enableModule(ctx, input.Name, existing)
		} else {
			err = h.disableModule(ctx, input.Name, existing)
		}
		if err != nil {
			return nil, err
		}
	}

	updated, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, mapConfigReadError(err)
	}
	return &UpdateModuleOutput{Body: h.toConfigResponse(*updated)}, nil
}

// auditAborted records a mutation that reached the handler but could not be
// dispatched — the document could not be read, or the module does not exist.
// One failure event per intended half, so G7's "every mutation that reaches
// the handler" holds on the abort path too.
func (h *ModuleAdminHandler) auditAborted(ctx context.Context, input *UpdateModuleInput, err error) {
	if len(input.Body.Config) > 0 || len(input.Body.Secrets) > 0 {
		h.emitAudit(ctx, auditRecord{
			action: ActionModuleConfigUpdated, module: input.Name,
			config: input.Body.Config, secrets: input.Body.Secrets, err: err,
		})
	}
	if input.Body.Enabled != nil {
		action := ActionModuleDisabled
		if *input.Body.Enabled {
			action = ActionModuleEnabled
		}
		h.emitAudit(ctx, auditRecord{action: action, module: input.Name, err: err})
	}
}

// enableModule persists enabled=true, retries a failed Init, starts the
// module, and audits the actual result.
func (h *ModuleAdminHandler) enableModule(ctx context.Context, name string, existing *ModuleConfig) error {
	err := h.doEnable(ctx, name, existing)
	h.emitAudit(ctx, auditRecord{action: ActionModuleEnabled, module: name, err: err})
	return err
}

func (h *ModuleAdminHandler) doEnable(ctx context.Context, name string, existing *ModuleConfig) error {
	if err := h.configService.UpdateEnabled(ctx, name, true); err != nil {
		if existing.Category == CategoryCore {
			return huma.Error400BadRequest(err.Error())
		}
		return err
	}
	if _, isFailed := h.registry.FailedModules()[name]; isFailed {
		if err := h.registry.RetryInit(name); err != nil {
			return huma.Error422UnprocessableEntity(fmt.Sprintf("module %q init failed: %s", name, err.Error()))
		}
	}
	if err := h.registry.StartModule(ctx, name); err != nil {
		return huma.Error422UnprocessableEntity(fmt.Sprintf("module %q failed to start: %s", name, err.Error()))
	}
	_ = h.configService.ClearNeedsRestart(ctx, name)
	return nil
}

// disableModule persists enabled=false, stops the module, and audits.
func (h *ModuleAdminHandler) disableModule(ctx context.Context, name string, existing *ModuleConfig) error {
	err := h.doDisable(ctx, name, existing)
	h.emitAudit(ctx, auditRecord{action: ActionModuleDisabled, module: name, err: err})
	return err
}

func (h *ModuleAdminHandler) doDisable(ctx context.Context, name string, existing *ModuleConfig) error {
	if existing.Category == CategoryCore {
		return huma.Error400BadRequest("core modules cannot be disabled")
	}
	if err := h.registry.CheckCanDisable(name); err != nil {
		return huma.Error409Conflict(err.Error())
	}
	if err := h.configService.UpdateEnabled(ctx, name, false); err != nil {
		return err
	}
	if err := h.registry.StopModule(ctx, name); err != nil {
		// The module is disabled regardless; the stop error is diagnostic.
		h.logger().Warn("module stop error", slog.String("module", name), slog.String("error", err.Error()))
	}
	_ = h.configService.ClearNeedsRestart(ctx, name)
	return nil
}
```

(add `"log/slog"` to imports; drop the `fmt.Printf`). In `UpdateEnvironment`, after the `UpdateEnvironmentConfigWithRecordLists` call, emit before mapping:

```go
	err := h.configService.UpdateEnvironmentConfigWithRecordLists(
		ctx, input.Name, input.Env,
		input.Body.Config, input.Body.Secrets, mutations, input.Body.Revision,
	)
	h.emitAudit(ctx, auditRecord{
		action: ActionModuleConfigUpdated, module: input.Name, env: input.Env,
		config: input.Body.Config, secrets: input.Body.Secrets, recordLists: mutations, err: err,
	})
	if err != nil {
		// mapConfigServiceError owns ConfigValidationError and the stale
		// 409; everything else falls through to the record-list mapping —
		// 409 means the client acted on a view of the world that no longer
		// holds; 422 means the request itself is malformed.
		return nil, mapConfigServiceError(err, func(e error) error {
			if code := recordListStatus(e); code != 0 {
				return huma.NewError(code, e.Error())
			}
			return huma.Error400BadRequest(e.Error())
		})
	}
```

In `SetActiveEnvironment`:

```go
	err := h.configService.SetActiveEnvironment(ctx, input.Name, input.Body.Environment)
	h.emitAudit(ctx, auditRecord{action: ActionModuleEnvironmentActivated, module: input.Name, env: input.Body.Environment, err: err})
	if err != nil {
		return nil, mapConfigServiceError(err, func(e error) error { return huma.Error400BadRequest(e.Error()) })
	}
```

Run: `cd backend && go test ./pkg/sdk/module/ -count=1`
Expected: PASS (all `TestAudit_*` plus everything before).

- [ ] **Step 5: `RequestMeta` middleware**

Create `backend/internal/shared/middleware/request_meta.go`:

```go
package middleware

import (
	"context"
	"net/http"
)

type requestMetaKey struct{}

// RequestMeta stores request provenance that a Huma handler cannot read
// from its context.Context — today the User-Agent — for the module-admin
// audit actor resolver (cmd/server/admin_wiring.go). Mounted on the admin
// mutation groups only.
func RequestMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), requestMetaKey{}, r.UserAgent())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserAgentFromContext returns the User-Agent RequestMeta stored, or "".
func UserAgentFromContext(ctx context.Context) string {
	ua, _ := ctx.Value(requestMetaKey{}).(string)
	return ua
}
```

`request_meta_test.go`:

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestMeta_StoresUserAgent(t *testing.T) {
	var got string
	h := RequestMeta(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = UserAgentFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodPatch, "/v1/admin/modules/auth", nil)
	req.Header.Set("User-Agent", "Console/2.0")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "Console/2.0" {
		t.Fatalf("UserAgentFromContext = %q", got)
	}
	if UserAgentFromContext(req.Context()) != "" {
		t.Fatal("a context RequestMeta never saw must yield empty")
	}
}
```

- [ ] **Step 5b: The compliance sink's WARN names action, resource and outcome**

Spec §4.11: "every sink failure produces a structured WARN with action/resource/outcome (never values or secrets)". Today `sink.go:58-63` logs `action`, `tenantId` and `error` only. Replace that `Warn` call:

```go
	if err := s.repo.Insert(writeCtx, event); err != nil {
		// Names the event, never its payload: Metadata may carry key names
		// but must not be logged wholesale.
		s.logger.Warn("audit sink insert failed",
			slog.String("action", event.Action),
			slog.String("resourceType", event.ResourceType),
			slog.String("resourceId", event.ResourceID),
			slog.String("outcome", event.Outcome),
			slog.String("tenantId", event.TenantID),
			slog.String("error", err.Error()),
		)
	}
```

Append to `sink_test.go` (the unreachable-Mongo pattern from `pkg/sdk/module/config_rawvalue_test.go`; `repository.New` performs no I/O):

```go
// TestEmit_InsertFailureWarnsWithActionResourceOutcome pins the best-effort
// contract's one guarantee: a failed insert is visible in a structured WARN
// that names the event — action, resource type/id, outcome — and never its
// metadata payload.
func TestEmit_InsertFailureWarnsWithActionResourceOutcome(t *testing.T) {
	client, err := mongo.NewClient(options.Client().
		ApplyURI("mongodb://127.0.0.1:1/").
		SetServerSelectionTimeout(50 * time.Millisecond).
		SetConnectTimeout(50 * time.Millisecond))
	if err != nil {
		t.Fatalf("mongo.NewClient: %v", err)
	}
	// Deliberately NOT connected: the insert fails server selection.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := NewSink(repository.New(client.Database("unreachable")), logger)

	sink.Emit(context.Background(), iface.AuditEvent{
		Action: "module.config.updated", ResourceType: "module", ResourceID: "auth",
		Outcome: "failure", TenantID: "t-1",
		Metadata: map[string]any{"keys": []string{"passwordLoginEnabledAdmin"}, "code": "auth.login_method_lockout"},
	})

	out := buf.String()
	for _, want := range []string{"level=WARN", "audit sink insert failed", "action=module.config.updated", "resourceType=module", "resourceId=auth", "outcome=failure"} {
		if !strings.Contains(out, want) {
			t.Errorf("WARN missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "passwordLoginEnabledAdmin") || strings.Contains(out, "login_method_lockout") {
		t.Errorf("WARN must not carry the metadata payload:\n%s", out)
	}
}
```

Imports for that file: `bytes`, `context`, `log/slog`, `strings`, `time`, `go.mongodb.org/mongo-driver/mongo`, `go.mongodb.org/mongo-driver/mongo/options`, `github.com/orkestra/backend/internal/core/compliance/repository`, `github.com/orkestra/backend/pkg/sdk/iface`. (The existing tests in the file use `t.Parallel()`; this one does not need to.)

Run: `cd backend && go test ./internal/core/compliance/services/ -run 'TestEmit' -count=1`
Expected: PASS in well under a second (the 2 s sink timeout never elapses — server selection fails at 50 ms).

- [ ] **Step 6: Server wiring + test**

Replace `cmd/server/admin_wiring.go` (created in Task 4) with the complete file:

```go
package main

import (
	"context"
	"errors"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// requiredPersistedModules are the modules whose module_configs document is
// REQUIRED once boot seeding has run: a missing document is an outage that
// fails closed (503) and shows as a `missing` row on /admin/modules, never a
// reason to rebuild it from schema defaults. auth is here because its
// per-surface credential policy (password login on/off) is read strictly —
// a lazy re-seed from an admin page read would silently re-enable password
// sign-in with the schema default. Recovery: restore the document, or fix
// Mongo and restart so normal boot seeding runs.
var requiredPersistedModules = []string{"auth"}

// adminActorResolver reads the module-admin audit actor off the request
// context: the JWT-derived principal AuthMiddleware stamped, the trusted-
// proxy-resolved client IP, the User-Agent RequestMeta stored, and chi's
// request ID. No email — the UUID is the attribution.
func adminActorResolver(ctx context.Context) module.AdminActor {
	var a module.AdminActor
	a.UserID, _ = ctxauth.GetUserUUID(ctx)
	a.TenantID, _ = ctxauth.GetTenantID(ctx)
	a.TenantKind = ctxauth.TenantKindFromContext(ctx)
	a.IP, _ = ctxauth.GetClientIP(ctx)
	a.UserAgent = authMiddleware.UserAgentFromContext(ctx)
	a.RequestID = chiMiddleware.GetReqID(ctx)
	return a
}

// wireModuleAdminAudit installs the compliance audit sink and the actor
// resolver on the module admin handler. Both are nil-tolerated by the SDK
// for embedders; the in-tree server refuses to boot without them, because
// compliance is a core module and a silently unaudited config surface is a
// misconfiguration, not a degraded mode.
func wireModuleAdminAudit(h *module.ModuleAdminHandler, svcs *module.ServiceRegistry) error {
	sink, ok := module.GetTyped[iface.AuditSink](svcs, module.ServiceAuditSink)
	if !ok || sink == nil {
		return errors.New("module admin audit: compliance audit sink is not registered")
	}
	h.SetAuditSink(sink)
	h.SetActorResolver(adminActorResolver)
	return nil
}
```

Replace `cmd/server/admin_wiring_test.go` (created in Task 4) with the complete file:

```go
package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestRequiredPersistedModules_AuthIsRequired(t *testing.T) {
	found := false
	for _, n := range requiredPersistedModules {
		if n == "auth" {
			found = true
		}
	}
	if !found {
		t.Fatal("auth must be a required persisted config: its strict password-policy reader depends on it")
	}
}

type noopSink struct{}

func (noopSink) Emit(context.Context, iface.AuditEvent) {}

func TestWireModuleAdminAudit_RequiresTheSink(t *testing.T) {
	h := module.NewModuleAdminHandler(nil, module.NewModuleRegistry(slog.Default()))
	if err := wireModuleAdminAudit(h, module.NewServiceRegistry()); err == nil {
		t.Fatal("wiring without a registered audit sink must fail — the in-tree server never runs unaudited")
	}
	svcs := module.NewServiceRegistry()
	svcs.Register(module.ServiceAuditSink, iface.AuditSink(noopSink{}))
	if err := wireModuleAdminAudit(h, svcs); err != nil {
		t.Fatalf("wiring with the sink: %v", err)
	}
}

func TestAdminActorResolver_ReadsContextWithoutEmail(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "u-1")
	ctx = context.WithValue(ctx, ctxauth.KeyUserEmail, "someone@example.com")
	ctx = context.WithValue(ctx, ctxauth.KeyTenantID, "t-1")
	ctx = ctxauth.WithTenantKind(ctx, "internal")
	ctx = ctxauth.WithClientIP(ctx, "203.0.113.9")
	a := adminActorResolver(ctx)
	if a.UserID != "u-1" || a.TenantID != "t-1" || a.TenantKind != "internal" || a.IP != "203.0.113.9" {
		t.Fatalf("actor = %+v", a)
	}
}
```

In `main.go`, after `moduleAdminHandler := module.NewModuleAdminHandler(configService, modRegistry)` (line 523):

```go
	if err := wireModuleAdminAudit(moduleAdminHandler, svcRegistry); err != nil {
		log.Fatalf("Failed to wire module admin audit: %v", err)
	}
```

and in the mutation group add `r.Use(authMiddleware.RequestMeta)` immediately after `r.Use(authMW.RequireLowRisk(riskStepUpThreshold()))`.

Run: `cd backend && go build ./... && go test ./cmd/server/ ./internal/shared/middleware/ -run 'Wire|ActorResolver|RequestMeta|RequiredPersisted' -count=1`
Expected: PASS.

- [ ] **Step 7: Docs**

`backend/internal/core/compliance/CLAUDE.md` — add to **Key contracts & invariants**:

```markdown
- **Module-config mutations are audited by the SDK admin handler, not by this module.** `pkg/sdk/module.ModuleAdminHandler` emits one event per actual mutation result through `SetAuditSink`/`SetActorResolver` (wired in `cmd/server/admin_wiring.go`): `module.config.updated` (`PATCH /v1/admin/modules/{name}` and `…/environments/{env}`, `Metadata.env`), `module.config.environment_activated` (`PUT …/active-environment`), `module.enabled` / `module.disabled` (the `enabled` half of a PATCH — a separate event from the config half, each with its own outcome). `ResourceType: "module"`, `ResourceID: <name>`, `Outcome` uses the existing `success`/`failure` vocabulary. **Metadata carries key NAMES only** (`keys`, `secretKeys`, schema-derived, sorted, ≤ 64; record-list element keys collapsed to their schema item names; unknown request keys only as `unknownKeyCount`), the stable `code` on failure (validation codes, `module.config_revision_stale`) and `requestId` — never a value, never a secret. The actor is `ActorUserID` + tenant context + IP + User-Agent; **`ActorEmail` is deliberately empty** (the UUID is the attribution; email is mutable PII). Persistence is **best-effort** under this sink's contract — `Emit` returns nothing, may add its bounded 2 s insert latency, and a failed insert is a structured WARN naming `action`/`resourceType`/`resourceId`/`outcome` (never the payload), not a rolled-back config change. This is not complete SOC2 evidence; guaranteed evidence needs the durable-outbox follow-up in the password-login-toggle spec §8. The events reuse `compliance_audit_events` and its existing two-year TTL; actor UUID, tenant context, IP and User-Agent are retained only for privileged-change forensics. **Deployers remain responsible for documenting the lawful basis and retention of these records in their RoPA/privacy materials** — the module records them, it does not decide the legal basis.
```

`docs/site/modules/core/compliance.mdx` — add a bullet under **What it owns** after the audit-trail bullet:

```markdown
- **Module-config change events** — every authenticated mutation of a module's configuration (`PATCH /v1/admin/modules/{name}`, `PATCH …/environments/{env}`, `PUT …/active-environment`, enable/disable) emits `module.config.updated`, `module.config.environment_activated`, `module.enabled` or `module.disabled` into the same trail with the acting user's UUID, tenant context, IP, User-Agent, the changed key **names** and the failure code — never values or secrets, never the actor's email. Emission is best-effort (a sink failure is logged, not rolled back) and events share the trail's existing two-year retention; deployments needing guaranteed evidence should plan a durable outbox. The records hold an actor UUID, tenant context, IP and User-Agent for privileged-change forensics — the deployer documents the applicable lawful basis and retention for them in its RoPA / privacy materials.
```

`backend/pkg/sdk/CLAUDE.md` — Rules bullet:

```markdown
- **`ModuleAdminHandler` audits every mutation it serves** through the
  nil-tolerant `SetAuditSink(iface.AuditSink)` + `SetActorResolver(func(ctx) module.AdminActor)`
  seams. Config validation and the CAS write happen **before** the
  enable/disable side effect; each half emits its own event with its actual
  result. Metadata is key names (schema-derived, bounded), `code`, `env`,
  `requestId` — never values. A panicking sink is recovered and WARNed; the
  HTTP result never changes because of the sink.
```

`docs/site/sdk/config-service.mdx` — append a short section:

```markdown
## Audit

Every mutation through the admin API emits one audit event per actual result into the compliance trail — `module.config.updated`, `module.config.environment_activated`, `module.enabled`, `module.disabled` — carrying the acting user's UUID, tenant, IP and User-Agent, the changed key names (and, for record lists, per-field created/removed counts), and the failure code when there is one. Values, secrets and slugs are never recorded. Emission is best-effort: a failed insert is logged and never rolls back the mutation, and the sink's own insert runs under a two-second timeout — the most latency an emit can add to the request.
```

`backend/CLAUDE.md` — **Runtime config** paragraph, append: "Every mutation is a single compare-and-swap `UpdateOne` on `configRevision` (stale → `409 module.config_revision_stale`) validated on the target profile's snapshot, and is audited (`module.config.*`, key names only) through the compliance sink."

- [ ] **Step 8: Commit**

```bash
cd backend && go vet ./... && grep -rn "internal/" pkg/sdk/ --include="*.go" | grep -v "//" ; cd ..
git add backend/pkg/sdk/module/admin_audit.go backend/pkg/sdk/module/admin_audit_test.go backend/pkg/sdk/module/handler.go \
  backend/internal/shared/middleware/request_meta.go backend/internal/shared/middleware/request_meta_test.go \
  backend/internal/core/compliance/services/sink.go backend/internal/core/compliance/services/sink_test.go \
  backend/cmd/server/admin_wiring.go backend/cmd/server/admin_wiring_test.go backend/cmd/server/main.go \
  backend/internal/core/compliance/CLAUDE.md docs/site/modules/core/compliance.mdx backend/pkg/sdk/CLAUDE.md \
  docs/site/sdk/config-service.mdx backend/CLAUDE.md
git commit -m "feat(sdk): audit every module-config mutation; config CAS before lifecycle side effect

ModuleAdminHandler emits one best-effort event per actual result
(module.config.updated / environment_activated / enabled / disabled) with
actor UUID + tenant + IP + UA, schema-derived key names, the stable code and
request ID — never values or email. The config half of a PATCH validates
and writes before enable/disable can run. cmd/server wires the compliance
sink and refuses to boot without it.

Refs: spec §4.11, G7"
```

---

### Task 7: Console — distinguish `module.config_revision_stale`, latch Reload & review

**Files:**
- Modify: `frontend-admin/src/store/api/moduleApi.ts` (export `CONFIG_REVISION_STALE`)
- Modify: `frontend-admin/src/pages/admin/modules/useModuleConfigController.ts:63-134` (interface) and `:164-231, 415-543` (behaviour)
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleSaveBar.tsx` (`conflict`, `onReload`)
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.tsx:40-58, ~140` and `detail/index.tsx:95-115, ~530` (pass the props)
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.test.tsx` (new `describe`)
- Modify: `frontend-admin/src/locales/en.json`, `frontend-admin/src/locales/it.json`

**Interfaces:**
- Consumes: the backend's 409 body `{status:409, title, detail, code:"module.config_revision_stale"}` (Task 3) on `PATCH …/environments/{env}`.
- Produces: `ModuleConfigController.conflict: boolean`, `ModuleConfigController.reloadAndReview: () => Promise<void>`; `ModuleSaveBarProps.conflict?: boolean`, `ModuleSaveBarProps.onReload?: () => void`; i18n `adminModules.detail.configCard.revisionConflict`, `adminModules.detail.configCard.reloadFailed`, `adminModules.detail.saveBar.reloadReview`.

- [ ] **Step 1: Write the failing tests**

Append to `ModuleConfigSection.test.tsx`:

```tsx
describe('ModuleConfigSection revision conflict', () => {
  const envGet = (hits: { n: number }, configValues: Record<string, string>) =>
    http.get(url('/v1/admin/modules/:name/environments/:env'), () => {
      hits.n += 1;
      return HttpResponse.json({
        environment: 'production',
        configValues,
        secretStatus: {},
        updatedAt: '',
        revision: hits.n
      });
    });

  it('latches on module.config_revision_stale, re-applies only the dirty draft, and reloads on demand without retrying', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'a', label: 'Alpha' }),
        field({ key: 'b', label: 'Beta' }),
        field({ key: 'c', label: 'Gamma' }),
        field({ key: 's', label: 'API Key', type: 'secret' })
      ],
      { availableEnvironments: ['production'] }
    );
    const hits = { n: 0 };
    let patches = 0;
    server.use(
      envGet(hits, { a: 'server-old', b: 'b-old', c: 'c-old' }),
      http.patch(url('/v1/admin/modules/:name/environments/:env'), () => {
        patches += 1;
        return HttpResponse.json(
          { status: 409, title: 'Conflict', detail: 'moved', code: 'module.config_revision_stale' },
          { status: 409 }
        );
      })
    );

    renderWithProviders(<TestHost mod={mod} />);
    const alpha = await screen.findByLabelText('Alpha');
    await waitFor(() => expect(alpha).toHaveValue('server-old'));
    await user.clear(alpha);
    await user.type(alpha, 'mine');
    await user.clear(screen.getByLabelText('Gamma')); // an intentional clear to ''
    await user.type(screen.getByLabelText('API Key'), 'unsent-secret');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));

    // The conflict copy, not the record-list one; Save disabled; Reload offered.
    expect(await screen.findByText(/changed this module's configuration/)).toBeInTheDocument();
    expect(screen.queryByText(/changed this list/)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled();
    expect(patches).toBe(1);

    // Meanwhile the other operator changed a, b (untouched here) and c
    // (cleared here). Reload adopts their baseline and re-applies ONLY the
    // fields this operator touched.
    server.use(envGet(hits, { a: 'server-new', b: 'b-new', c: 'c-new' }));
    await user.click(screen.getByRole('button', { name: 'Reload & review' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
    );
    expect(screen.getByLabelText('Alpha')).toHaveValue('mine');      // dirty: re-applied
    expect(screen.getByLabelText('Beta')).toHaveValue('b-new');      // untouched: theirs, NOT reverted to b-old
    expect(screen.getByLabelText('Gamma')).toHaveValue('');          // intentional clear survives
    expect(screen.getByLabelText('API Key')).toHaveValue('unsent-secret'); // unsent secret kept in memory
    expect(screen.getByText(/3 unsaved changes/)).toBeInTheDocument();
    // Nothing was auto-submitted.
    expect(patches).toBe(1);
  });

  it('keeps Save disabled and the conflict latched when the reload itself fails', async () => {
    const user = userEvent.setup();
    const mod = moduleWith([field({ key: 'a', label: 'Alpha' })], {
      availableEnvironments: ['production']
    });
    const hits = { n: 0 };
    server.use(
      envGet(hits, { a: 'x' }),
      http.patch(url('/v1/admin/modules/:name/environments/:env'), () =>
        HttpResponse.json(
          { status: 409, title: 'Conflict', detail: 'moved', code: 'module.config_revision_stale' },
          { status: 409 }
        )
      )
    );
    renderWithProviders(<TestHost mod={mod} />);
    const alpha = await screen.findByLabelText('Alpha');
    await waitFor(() => expect(alpha).toHaveValue('x'));
    await user.clear(alpha);
    await user.type(alpha, 'y');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));
    await screen.findByRole('button', { name: 'Reload & review' });

    server.use(
      http.get(url('/v1/admin/modules/:name/environments/:env'), () =>
        HttpResponse.json({ status: 503, title: 'Service Unavailable', detail: 'down' }, { status: 503 })
      )
    );
    await user.click(screen.getByRole('button', { name: 'Reload & review' }));
    expect(await screen.findByText(/Reloading the latest configuration failed/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Reload & review' })).toBeInTheDocument();
    expect(alpha).toHaveValue('y'); // the draft is untouched
  });

  it('keeps the record-list wording for a codeless 409', async () => {
    const user = userEvent.setup();
    const mod = moduleWith([field({ key: 'a', label: 'Alpha' })], {
      availableEnvironments: ['production']
    });
    server.use(
      envGet({ n: 0 }, { a: 'x' }),
      http.patch(url('/v1/admin/modules/:name/environments/:env'), () =>
        HttpResponse.json({ status: 409, title: 'Conflict', detail: 'slug exists' }, { status: 409 })
      )
    );
    renderWithProviders(<TestHost mod={mod} />);
    const alpha = await screen.findByLabelText('Alpha');
    await waitFor(() => expect(alpha).toHaveValue('x'));
    await user.clear(alpha);
    await user.type(alpha, 'y');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));
    expect(await screen.findByText(/changed this list/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reload & review' })).not.toBeInTheDocument();
  });
});
```

Run: `cd frontend-admin && npx vitest run src/pages/admin/modules/detail/ModuleConfigSection.test.tsx`
Expected: the three new tests FAIL (conflict copy not found; no Reload button).

- [ ] **Step 2: i18n**

`en.json` — in `adminModules.detail.configCard` add `"revisionConflict": "Another operator changed this module's configuration after you loaded it. Reload to review your edits against the latest values before saving."` and `"reloadFailed": "Reloading the latest configuration failed. Try again before saving."`; in `adminModules.detail.saveBar` add `"reloadReview": "Reload & review"`.
`it.json` — `"revisionConflict": "Un altro operatore ha modificato la configurazione di questo modulo dopo il caricamento. Ricarica per rivedere le tue modifiche rispetto ai valori più recenti prima di salvare."`, `"reloadFailed": "Il ricaricamento della configurazione più recente non è riuscito. Riprova prima di salvare."`; `"reloadReview": "Ricarica e rivedi"`.

`moduleApi.ts` — after the types: 

```ts
/**
 * Body `code` the backend sends on a 409 when a module-config write lost its
 * compare-and-swap: the document changed after it was loaded. Distinct from
 * the codeless record-list roster conflicts, which keep their own copy.
 */
export const CONFIG_REVISION_STALE = 'module.config_revision_stale';
```

- [ ] **Step 3: Controller**

In `useModuleConfigController.ts`:

Imports: add `useRef` to the React import; import `CONFIG_REVISION_STALE` from `store/api/moduleApi`.

Interface (`ModuleConfigController`), after `success: boolean;`:

```ts
  /**
   * True after a save lost the backend's compare-and-swap
   * (`module.config_revision_stale`). Save stays disabled until a reload has
   * SUCCEEDED: nothing is auto-retried, because a retry would re-send a
   * typed secret and re-decide the change against a state the operator
   * never saw.
   */
  conflict: boolean;
  /**
   * Refetches the environment baseline and re-applies ONLY the operator's
   * dirty fields on top of it — non-secret edits (an intentional clear to
   * '' included) and unsent non-empty secrets — so the diff is recomputed
   * against what the server holds now. Fields the operator never touched
   * adopt the other writer's values. Staged record-list removals are
   * cleared (they were decided against the old state). A failed refetch
   * leaves the conflict latched.
   */
  reloadAndReview: () => Promise<void>;
```

Query hook (line 164): `const { data: envConfig, isLoading: envLoading, refetch: refetchEnv } = useGetModuleEnvironmentQuery(…)`.

State, after `success`:

```ts
  const [conflict, setConflict] = useState(false);
  /** One dirty field captured by reloadAndReview, re-applied by the re-seed effect. */
  interface DraftEntry {
    name: string;
    value: string;
    secret: boolean;
  }
  // The draft captured by reloadAndReview, consumed by the re-seed effect
  // once the fresh baseline lands. A ref, not state: it must survive the
  // render the refetch triggers without itself causing one. Tagged with the
  // environment it belongs to so a switch mid-reload can never inject it
  // into another profile.
  const pendingDraft = useRef<{ environment: string; entries: DraftEntry[] } | null>(null);
```

Re-seed effect: after `form.reset(defaults);` and the three `setX(EMPTY_…)` calls, before `setError(null)`:

```ts
    // Reload & review: put the operator's DIRTY fields back on top of the
    // fresh baseline. A non-secret edit is re-applied only while it still
    // differs from the new baseline — including an intentional clear to ''
    // when the baseline is non-empty; an edit the other writer already made
    // is no longer a change. A secret's baseline is always '' (never
    // echoed), so a typed secret is always a change.
    const draft = pendingDraft.current;
    pendingDraft.current = null;
    if (draft && draft.environment === environment) {
      for (const { name, value, secret } of draft.entries) {
        if (secret || value !== (defaults[name] ?? '')) {
          form.setValue(name, value, { shouldDirty: true });
        }
      }
    }
```

Save error branch (replace lines 513-520):

```ts
      const status =
        err && typeof err === 'object' && 'status' in err
          ? (err as { status?: number }).status
          : undefined;
      const code =
        err && typeof err === 'object' && 'data' in err
          ? (err as { data?: { code?: string } }).data?.code
          : undefined;
      if (code === CONFIG_REVISION_STALE) {
        // The document moved under this save — another operator, or a
        // record-list write. Latch until a reload has succeeded.
        setConflict(true);
        setError(t('adminModules.detail.configCard.revisionConflict'));
        return;
      }
      if (status === 409) {
        // Codeless 409: the record-list roster moved (slug exists / missing).
        setError(t('adminModules.recordList.conflict'));
        return;
      }
```

At the start of `onSave`, right after the `pendingDeletion` check, add `if (conflict) return;` as a belt-and-braces guard.

`captureDirtyDraft` + `reloadAndReview`, placed after `handleDiscard`:

```ts
  // Only fields react-hook-form marks dirty — never the whole form, which
  // would turn the other operator's changes into "local edits" pointing back
  // at the old values. Register names are flat (`buildFieldNames`), so
  // `dirtyFields[name]` is a plain boolean.
  const captureDirtyDraft = (): DraftEntry[] => {
    const values = form.getValues();
    const secretNames = new Set(
      expandedSchema
        .filter(f => f.type === 'secret')
        .map(f => fieldNameOf(fieldNames, f.key))
    );
    return Object.keys(form.formState.dirtyFields)
      .filter(name => Boolean(form.formState.dirtyFields[name]))
      .map(name => ({
        name,
        value: String(values[name] ?? ''),
        secret: secretNames.has(name)
      }))
      // A secret typed and then cleared is not a change: nothing to re-send.
      .filter(d => !d.secret || d.value !== '');
  };

  const reloadAndReview = async () => {
    const baselineRevision = envConfig?.revision;
    pendingDraft.current = { environment, entries: captureDirtyDraft() };
    try {
      const fresh = await refetchEnv().unwrap();
      if (fresh.revision === baselineRevision) {
        // Same profile revision ⇒ identical data ⇒ RTK Query keeps the same
        // reference and the re-seed effect will not run (the 409 came from
        // an activation or another profile's write, which move only
        // configRevision). The form still holds the draft; nothing to do.
        pendingDraft.current = null;
      }
      // Otherwise the data reference changes, the re-seed effect runs, and it
      // consumes the draft — whichever of that render and this continuation
      // comes first, the draft is applied exactly once.
      setConflict(false);
      setError(null);
    } catch {
      pendingDraft.current = null;
      setError(t('adminModules.detail.configCard.reloadFailed'));
      // conflict stays true: Save must not be usable against a baseline the
      // operator never got to review.
    }
  };
```

(`refetchEnv` on a skipped query — a module that declares no environments — throws synchronously into the `catch`, which is the honest outcome: there is no baseline to reload; every in-tree module declares both profiles.)

In `handleDiscard` add `setConflict(false);` and `pendingDraft.current = null;`. Return `conflict` and `reloadAndReview` from the hook.

- [ ] **Step 4: Save bar + hosts**

`ModuleSaveBar.tsx` props:

```ts
  /** Save lost its compare-and-swap; Save is disabled until `onReload` runs. */
  conflict?: boolean;
  onReload?: () => void;
```

In the right-hand button group, before the Save button:

```tsx
        {conflict && onReload && (
          <Button type="button" variant="warning" size="sm" onClick={onReload} disabled={saving}>
            {t('adminModules.detail.saveBar.reloadReview')}
          </Button>
        )}
```

and `disabled={saving || dirtyCount === 0 || conflict}` on Save.

`ModuleConfigSection.tsx`: destructure `conflict, reloadAndReview` from `controller` and pass `conflict={conflict} onReload={reloadAndReview}` to its `<ModuleSaveBar …>`. `detail/index.tsx`: same for the page-level `<ModuleSaveBar …>` (line ~532).

- [ ] **Step 5: Run**

Run: `cd frontend-admin && npx vitest run src/pages/admin/modules src/locales && npm run typecheck && npm run lint`
Expected: PASS, including every pre-existing `ModuleConfigSection`, `index`, `recordList.e2e` test (the re-seed effect's new block is a no-op when `pendingDraft.current` is null).

- [ ] **Step 6: Commit**

```bash
git add frontend-admin/src/store/api/moduleApi.ts frontend-admin/src/pages/admin/modules/useModuleConfigController.ts \
  frontend-admin/src/pages/admin/modules/detail/ModuleSaveBar.tsx frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.tsx \
  frontend-admin/src/pages/admin/modules/detail/index.tsx frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.test.tsx \
  frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "feat(admin): latch module.config_revision_stale with Reload & review, never auto-retry

The config controller reads the 409 body code instead of treating every
409 as a record-list conflict. A stale-revision save disables Save and
offers Reload & review, which refetches the baseline and re-applies the
unsaved draft (unsent secrets included, in memory only) so the diff is
recomputed against current values. Codeless roster 409s keep their copy.

Refs: spec §4.10"
```

---

### Task 8: PR gate

**Files:** none new — verification and the PR.

- [ ] **Step 1: Backend CI locally**

Run: `make ci-backend` (from the repo root; needs the infra stack for `openapi-check` and `backend-mongo-config` — `grep "^ENV=" docker/.env` first, and if the stack is down bring infra up with `cd docker && docker compose -f docker-compose.infra.yml up -d`).
Expected: every target green. If `openapi-check` reports drift, you forgot Task 4 Step 8 — run `cd backend && make openapi-dump` and amend that commit.

- [ ] **Step 2: Frontend CI locally**

Run: `make ci-frontend-admin`
Expected: green (typecheck, eslint, vitest incl. `parity.test.ts`, audit, build).

- [ ] **Step 3: Docs-site render** (mandatory for a `docs/site/**` change — nothing in this repo's CI builds the site; `docs/site/README.md` "Checking a change before you merge it")

```bash
git clone --depth 1 https://github.com/orkestra-cc/orkestra-docs ../orkestra-docs-pr1   # the ~/orkestra-docs checkout is stale; clone fresh
cd ../orkestra-docs-pr1 && npm ci
MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync:site
CI=true npm run build
```

Expected: build succeeds (`onBrokenLinks: 'throw'`), no anchor warnings for `config-service.mdx` / `compliance.mdx`. Note `sync:adrs` reads ADRs from GitHub `main`, not the local tree — irrelevant here (no ADR), but do not read a missing-ADR warning as a failure of this PR.

- [ ] **Step 4: Live smoke on the local staging stack** (optional but cheap; skip if the stack is not up)

Rebuild the backend through the sanctioned lifecycle (`./orkestra.sh` → rebuild backend, or the `make` target per `docker/CLAUDE.md`), then with `B=http://localhost:3000` and `T=$(ORKESTRA_API_URL=$B ./scripts/devtoken.sh administrator --quiet)`:

```bash
H=(-H "Authorization: Bearer $T" -H 'Content-Type: application/json')
E=$B/v1/admin/modules/notification/environments/sandbox

# 1. Deterministic stale-revision 409 with the SDK-owned code. The bare
#    config PATCH carries no client revision (the server does its own
#    read-CAS-write), so the only race a client can force from curl is the
#    record-list one: read the profile revision, move it with a value write,
#    then submit a removal against the revision you first read.
R=$(curl -s "${H[@]}" "$E" | jq .revision)
curl -s "${H[@]}" -X PATCH "$E" -d '{"config":{"email.from_name":"A"}}' | jq .revision        # → R+1
curl -s "${H[@]}" -X PATCH "$E" -d "{\"recordLists\":[{\"field\":\"email.senders\",\"remove\":[\"nope\"]}],\"revision\":$R}" \
  | jq '{status, code}'                                                                        # → {"status":409,"code":"module.config_revision_stale"}
#    The intra-server two-writer race on ordinary PATCHes is exercised by the
#    fake-repository test (beforeDocCAS); it cannot be forced from curl.

# 2. Backfill ran once: the first boot after deploy logs the key names, the
#    next restart logs nothing.
docker logs "$(docker ps --format '{{.Names}}' | grep backend)" 2>&1 | grep "backfilled with schema defaults"

# 3. The audit rows landed, names only:
curl -s "${H[@]}" "$B/v1/admin/audit-events?action=module.config.updated&limit=2" \
  | jq '.events[] | {outcome, metadata}'   # keys == ["email.from_name"] on the success; code == "module.config_revision_stale" on the failure; no "A" anywhere
```

Record what you observed in the PR body; do not claim the smoke passed if you skipped it.

- [ ] **Step 5: Self-review against the spec's PR 1 row**

Tick each: `ConfigValidationSnapshot` + dispatch (T2/T3) · `configRevision` CAS + single `UpdateOne` (T1/T3) · `needsRestart` in the same write (T1/T3) · `RequirePersistedConfig` + `missing` row (T4) · backfill (T5) · audit setters + generic events + config-before-lifecycle (T6) · controller 409-by-code (T7) · §6 tests: `config_required_test.go` ✓, `config_backfill_test.go` ✓, `config_snapshot_test.go` (≙ spec's `config_validate_test.go` row) ✓, repository/handler integration ✓ (`config_repository_cas_test.go`, `config_service_cas_test.go`), `HotReloadConfig` persistence ✓ (T3 tests), `admin_audit_test.go` ✓, `codes_test.go` non-collision ✓, `useModuleConfigController` conflict tests ✓ (via `ModuleConfigSection.test.tsx`).

- [ ] **Step 6: Open the PR**

```bash
git push -u origin feat/auth-password-login-toggle
gh pr create --base dev --title "feat(sdk): module-config integrity — snapshot validation, atomic CAS writes, required auth document, audit (password-login toggle PR 1/4)" --body-file - <<'PRBODY'
## What

PR 1 of 4 for the per-surface password-login toggle (spec: `docs/superpowers/specs/2026-08-29-password-login-toggle-design.md`, §7). Ships the SDK substrate the toggle's anti-lockout invariant needs; **no operator-visible behaviour changes** except:
- `409 module.config_revision_stale` when two operators race on a module's config (was: silent last-writer-wins / write skew) — the console shows **Reload & review**;
- `/admin/modules` shows a `missing` badge if the `auth` document is gone instead of silently rebuilding it with defaults;
- every module-config mutation is audited (`module.config.*`, key names only).

- `ModuleConfig.configRevision` + one compare-and-swap `UpdateOne` per mutation (profile + legacy mirror, or server-side activation); `needsRestart` persisted in the same write from `HotReloadConfig()`.
- `HasConfigSnapshotValidator` + `ConfigValidationSnapshot` (raw values, effective values, **target-profile** secret presence) dispatched on all three surfaces; legacy validators untouched.
- `RequirePersistedConfig(ctx, "auth")` after boot seeding — also the boot gate (missing document or recorded seed/backfill failure → the server exits); `ListConfigs` missing row; strict `GetRawValueRequiredModule`.
- `SeedFromModules` backfills absent schema keys on existing documents (one CAS, INFO log).
- `ModuleAdminHandler.SetAuditSink/SetActorResolver`; config CAS before enable/disable; `cmd/server` refuses to boot unaudited.
- Server-side lane validation: a secret key in `config` (or a non-secret / unknown key in `secrets`) is `422 module.config_key_invalid` — no plaintext secret can reach `ConfigValues` or the validator.

**SDK interface change (declared additive-only exception):** `module.ConfigRepository` is provided to the service, not implemented by modules (same category as `RedisClient`); it gains `CompareAndSwapConfig`, re-signs `CompareAndSwapEnvironment`/`MigrateToEnvironments`, and drops the four two-step write methods. Only a fork's substitute repository tracks it. Recorded in `pkg/sdk/CLAUDE.md` Versioning policy.

## Evidence

- `make ci-backend`: <paste tail>
- `make ci-frontend-admin`: <paste tail>
- Mongo-guarded tests ran locally with `MONGO_TEST_URI`: yes/no
- Docs-site render (Task 8 Step 3): <build output tail>
- Staging smoke (Task 8 Step 4): <observed / skipped>

## Docs

`pkg/sdk/CLAUDE.md`, `docs/site/sdk/config-service.mdx`, `internal/core/CLAUDE.md`, `compliance/CLAUDE.md` + `compliance.mdx`, `backend/CLAUDE.md`, `openapi/enterprise.json` — each in the commit that changed the behaviour.

https://claude.ai/code/session_01BEWccDH4gcNX7EMbPWoWmk
PRBODY
```

(`Closes #N` never fires on this repo — PRs target `dev`; link issues by hand if any.)
