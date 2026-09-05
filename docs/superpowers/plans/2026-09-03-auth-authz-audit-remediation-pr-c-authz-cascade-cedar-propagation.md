# Auth/Authz Audit Remediation — PR C: Role Cascade, Cedar and Propagation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a custom role incapable of carrying more than its editor holds or any platform permission (H-4), make `system.*` actions require a platform system role under both evaluators (H-5), and make a system-role change honoured by the next authorization decision in every session the user holds — refusing the change outright when that cannot be guaranteed (M-13, plus M-17).

**Architecture:** `CreateRole` and `UpdateRole` gain an actor and run every supplied permission list through one shared validator: catalog membership, no platform key, no wildcard, and the same cascade check bindings already use. `GetEffectivePermissions` stops letting a tenant-scoped binding contribute a platform key at all, which makes the documented evaluator rule true rather than incidental. A single Cedar `forbid` on `context.action_module == "system"` outranks every present and future tenant-role permit, and `shadowEvaluate` stops stamping JWT tenant roles on checks with no tenant. The authz cache becomes **generation-keyed** — two Redis counters folded into the key, invalidation is one atomic `INCR` with no `KEYS` scan — and every mutation that changes effective permissions runs pre-invalidate → write → post-invalidate, refusing the change with 503 when the pre-invalidation cannot be performed.

**Tech Stack:** Go 1.26.8, Cedar (`cedar-policy/cedar-go` via `internal/core/authz/cedar`), Redis 8.2 (`MGET` + `INCR`), MongoDB 8, `miniredis` for cache tests, Huma v2.39.1.

**Spec:** `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` **v1.14** (§4.4–§4.6 unchanged from v1.12) — this plan implements the **PR C** row of §7, i.e. §4.4 (**D21, D22**), §4.5 (**D23, D24**; **D25 is explicitly out of scope**) and §4.6 (**D26–D29**), plus the §4.11 documentation lines and the §6 "PR C — authz" test list.

**Independent of PRs A and B** — it may run in parallel with PR B.

## Global Constraints

- **`CEDAR_ENFORCE_ACTIONS` is NOT flipped by this PR.** D25: this spec makes the flip *safe*; §7 keeps it a separate operational step, gated on `orkestra_cedar_shadow_divergence_total` being quiet for `system.*` suffixes for one release cycle on staging.
- **Cedar forbids outrank permits.** The single `system_actions.require_platform_role` rule is what closes every present *and future* tenant-role permit; do not attempt to fix `tenant_roles.cedar` permit-by-permit.
- **`developer` stays in the exempt set.** The role table already bounds it to read-only in production and `platform.developer.prod_readonly` mirrors that. The forbid is about *who may hold* system actions; the permits are about *which*.
- 🔴 **SUPERSEDED DURING EXECUTION (rulings P22/P24/P25, architect-decided) — D27 splits by DIRECTION.** ~~A refused pre-invalidation is a refused change. 503, nothing written. A change whose effect cannot be guaranteed is not applied. An invalidator absent from the registry is treated the same way **for mutations** — the "degrade, do not fail" note on the interface applies to consumers that only *read* verdicts.~~ The uniform rule inverts for revocations: a 503 on a revoke leaves the privilege granted **indefinitely**, where writing it leaves at most a 60s stale allow — and with Redis fully down the cache is bypassed on reads anyway, so a written revocation takes effect immediately. As shipped: **grants are gated** (an `UpdateRole` patch that takes nothing away, and `CreateBinding`/`EnsureBinding` from a real actor → 503 `authz.cache_unavailable`, nothing written); **revocations, platform-issued grants, and the user module's system-role change write first and report** (logged, counted, and for the role change recorded in the audit row as `cache_invalidated` / `sessions_terminated`) — never a refusal. `CreateRole` is wrapped in neither: a role with no bindings changes nobody's verdicts. **This amendment is owed to spec §4.6 D27**, which still states the uniform rule.
- **A failed post-invalidation is logged, counted, audited — and the change stands.** It retires only a repopulation that landed in the sub-second window between pre-invalidate and write.
- **No `KEYS` scan, ever.** The generation key is what removes it. `KEYS` built from a request body was also the audit's L-11.
- **Tier guards read the database, never the `srole` claim.** A lookup error is a 500, never a fallback to the claim.
- **`iface.AuthzCacheInvalidator` is additive.** `iface.AuthzProvider` is implemented by forks and stays additive-only; the new interface is narrow, on the `UserLifecycleStateProvider` precedent, resolved with `module.GetTyped` against `ServiceAuthzProvider`.
- **`DeleteRole` is a revocation.** Under the amended D27 above it takes the write-then-report shape (it must still retire the verdicts its cascade invalidates), and it can never answer 503 — `mapRoleDeleteError` deliberately carries no cache row.
- **`IsActive`-only patches do not re-validate permissions** (edge case 13): an existing role that already holds a system key can still be disabled.
- **Docs move in the same commit as the code**, except the `authz/CLAUDE.md` sweep, which is consolidated into Task 8 so two tasks do not fight over the same regions. The full surface turned out to be wider than this list: `backend/internal/core/{authz,user,tenant}/CLAUDE.md`, `backend/internal/shared/setup/CLAUDE.md`, `backend/pkg/sdk/CLAUDE.md`, `docs/site/modules/core/{authz,user}.mdx`, `docs/site/sdk/shared-iface.mdx`, `docs/site/architecture/authentication-flow.mdx` — **plus code comments**, which are in scope for the truth sweep exactly as `.md` files are.
- **Test commands** (from `/home/tore/orkestra/backend`):
  - `go test ./internal/core/authz/... ./internal/core/user/... -count=1`
  - `go vet ./...` before every commit
  - `make -C /home/tore/orkestra ci-backend` (runs `policycoverage` — a new `.cedar` file is regex-scanned, no baseline change needed)
  - live Mongo where guarded: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`
- **Never start servers manually.** **Commit trailer:** `Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1`

## Declared deviations from the spec (read before executing)

1. **`withGeneration` is a method on `*Service`, not a free function.** It needs `s.redis` and `s.logger`; the spec's `withGeneration(ctx, scope, mutate)` shape is kept as the parameter list.
2. **The generation counters are read with one `MGET` per cache *read*, and cached for the duration of that read only.** The spec says "every cache read fetches both with one MGET". This plan does not memoise them across requests: a stale generation would defeat the whole mechanism, and one `MGET` is cheaper than the Mongo read it saves.
3. **`GetEffectivePermissions`'s system-key filter applies to tenant-scoped bindings only** (spec D22). Global bindings are untouched — that is where a platform role legitimately grants a platform key.
4. **The `errcode` constants for the two new 503s land in this PR.** The spec names `user.role_change_unavailable` and `authz.cache_unavailable` but not where they are declared; both go in `internal/shared/errcode/codes.go` with golden rows, like every other code.
5. **D29 (client-user PATCH guards) is included** and flagged in the spec itself as outside the agreed perimeter. It is one guard on ten lines this PR already rewrites. Strike Task 7's client half if the architect declines it; nothing else depends on it.

## File Structure

**Backend — `backend/internal/core/authz/`**

| File | Responsibility | Task |
|---|---|---|
| `services/role_validation.go` (new) | `validateCustomRolePermissions`, the five sentinels | 1 |
| `services/role_validation_test.go` (new) | catalog / system-key / cascade / actor tests | 1 |
| `services/service.go` | `CreateRole`/`UpdateRole` actors + validation; `GetEffectivePermissions` system-key filter; generation cache; `withGeneration`; `InvalidateUserPermissions`; `shadowEvaluate` tenant-role gating | 1, 2, 4, 5 |
| `handlers/handler.go` | pass the actor; map the new sentinels | 1 |
| `cedar/policies/system_actions.cedar` (new) | the platform-role forbid | 3 |
| `cedar/engine_test.go`, `services/service_test.go`, `services/cache_test.go`, `services/tier1_crud_test.go`, `services/tier1_test.go` | the §6 list | 1–5 |
| `CLAUDE.md` | cascade, rule 4, cache, new policy row | 1, 2, 3, 5 |

**Backend — elsewhere**

| File | Responsibility | Task |
|---|---|---|
| `pkg/sdk/iface/interfaces.go` | + `AuthzCacheInvalidator` | 5 |
| `pkg/sdk/CLAUDE.md` | record it as additive | 5 |
| `internal/shared/errcode/codes.go` + `codes_test.go` | 5 new codes | 1, 5 |
| `internal/core/user/handlers/user_handler.go` | pre/write/post + DB-read tier guard | 6, 7 |
| `internal/core/user/handlers/admin_client_handler.go` | the same, + D29 guards | 7 |
| `internal/shared/setup/service.go` | recovery gate reads the DB role | 7 |
| `internal/core/user/CLAUDE.md` | `:121-122`, `:125` | 6, 7 |

---

## Task 1: Roles are validated like bindings (D21)

`UpdateRole` and `CreateRole` take no actor and write `permissions` verbatim: no catalog check, no platform-key check, no cascade. The audit's H-4 probe is create → bind → update-with-`tenant.delete`, and it succeeds today.

**Files:**
- Create: `backend/internal/core/authz/services/role_validation.go`
- Create: `backend/internal/core/authz/services/role_validation_test.go`
- Modify: `backend/internal/core/authz/services/service.go` (`CreateRole` `:840-854`, `UpdateRole` `:859-910`)
- Modify: `backend/internal/core/authz/handlers/handler.go` (`:241`, `:252`, and the two `errors.New` paths at `:248-264`)
- Modify: `backend/internal/shared/errcode/codes.go` + `codes_test.go`
- Modify: `backend/internal/core/authz/CLAUDE.md`
- Test: `backend/internal/core/authz/services/tier1_crud_test.go`

**Interfaces:**
- Consumes: `s.allPermissionSet` / `s.systemPermissionSet` (`service.go:183-184`, populated by `RegisterPermissions` `:667-695`), `GetEffectivePermissions`, `validateBindingCascade` (`:1268-1291`), `granterSystem`.
- Produces:
  - `func (s *Service) validateCustomRolePermissions(ctx context.Context, tenantID, actor string, keys []string) ([]string, error)`
  - `ErrRolePermissionsRequired`, `ErrUnknownPermission`, `ErrSystemPermissionInCustomRole`, `ErrGranterRequired`, `ErrRoleNameRequired`
  - `CreateRole(ctx, tenantID, actor string, input)` and `UpdateRole(ctx, tenantID, roleUUID, actor string, input)`
  - `errcode.AuthzPermissionUnknown`, `errcode.AuthzSystemPermissionForbidden`
- Later tasks rely on: `UpdateRole`'s new signature (Task 5 wraps it in `withGeneration`).

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/authz/services/role_validation_test.go`:

```go
package services

// H-4: UpdateRole replaced `permissions` verbatim with no catalog check,
// no platform-key check and no cascade, so any caller who could edit a
// custom role could write ANY string into it — including a permission
// nobody holds and a platform key no tenant role may carry — and then
// bind themselves to it. The audit's probe is create → bind → update
// with tenant.delete, and it succeeds today.

import (
	"context"
	"errors"
	"testing"
)

func TestValidateCustomRolePermissions_RejectsAnUnknownKey(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.update"))
	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"tenant.read", "tenant.obliterate"})
	if !errors.Is(err, ErrUnknownPermission) {
		t.Fatalf("err = %v, want ErrUnknownPermission", err)
	}
	if !strings.Contains(err.Error(), "tenant.obliterate") {
		t.Errorf("the error must name the offending key, got %q", err)
	}
}

func TestValidateCustomRolePermissions_RejectsASystemKey(t *testing.T) {
	svc := newValidationTestService(t,
		registered("tenant.read"), systemRegistered("system.users.admin"))
	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"tenant.read", "system.users.admin"})
	if !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole", err)
	}
}

func TestValidateCustomRolePermissions_RejectsWildcard(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"))
	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1", []string{"*"})
	if !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole for the wildcard", err)
	}
}

func TestValidateCustomRolePermissions_RejectsAnEmptyList(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"))
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1", []string{}); !errors.Is(err, ErrRolePermissionsRequired) {
		t.Fatalf("err = %v, want ErrRolePermissionsRequired", err)
	}
	// …and a list that is empty only after trimming.
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1", []string{"  ", ""}); !errors.Is(err, ErrRolePermissionsRequired) {
		t.Fatalf("err = %v for a whitespace-only list, want ErrRolePermissionsRequired", err)
	}
}

func TestValidateCustomRolePermissions_TrimsAndDeduplicates(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.update"))
	svc.grantActor("actor-1", "tenant-1", "tenant.read", "tenant.update")

	got, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{" tenant.read ", "tenant.read", "tenant.update"})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 de-duplicated keys", got)
	}
}

// The cascade: a role can never carry more than its editor holds.
func TestValidateCustomRolePermissions_RefusesAKeyTheActorLacks(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.delete"))
	svc.grantActor("actor-1", "tenant-1", "tenant.read")

	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"tenant.read", "tenant.delete"})
	if !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Fatalf("err = %v, want ErrInsufficientPermissionsToGrant", err)
	}
}

// Edge case 14: a super_admin's wildcard covers everything in the
// CASCADE, but the catalog and system-key checks still apply — a
// super_admin cannot put system.users.admin into a TENANT role either.
func TestValidateCustomRolePermissions_WildcardActorStillCannotAddASystemKey(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"), systemRegistered("system.users.admin"))
	svc.grantActor("actor-1", "tenant-1", "*")

	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"system.users.admin"}); !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole even for a wildcard actor", err)
	}
}

func TestValidateCustomRolePermissions_EmptyActorIsRefused(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"))
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "", []string{"tenant.read"}); !errors.Is(err, ErrGranterRequired) {
		t.Fatalf("err = %v, want ErrGranterRequired", err)
	}
}

// granterSystem bypasses the CASCADE only — seeding and internal callers
// still cannot write an unknown or platform key into a tenant role.
func TestValidateCustomRolePermissions_GranterSystemBypassesOnlyTheCascade(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.delete"), systemRegistered("system.users.admin"))

	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", granterSystem,
		[]string{"tenant.read", "tenant.delete"}); err != nil {
		t.Fatalf("granterSystem must bypass the cascade: %v", err)
	}
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", granterSystem,
		[]string{"system.users.admin"}); !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatal("granterSystem must NOT bypass the system-key check")
	}
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", granterSystem,
		[]string{"tenant.nonexistent"}); !errors.Is(err, ErrUnknownPermission) {
		t.Fatal("granterSystem must NOT bypass the catalog check")
	}
}

// allPermissionSet must actually be populated, or every key looks
// unknown and role editing breaks entirely. The registry calls
// RegisterPermissions once with the union of every module's
// Permissions(), and ensureSeeded replays the cached specs.
func TestAllPermissionSet_IsPopulatedAfterRegisterPermissions(t *testing.T) {
	svc := newValidationTestService(t)
	if err := svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant"},
		{Key: "system.users.admin", Module: "user", System: true},
	}); err != nil {
		t.Fatalf("RegisterPermissions: %v", err)
	}
	if _, ok := svc.allPermissionSet["tenant.read"]; !ok {
		t.Error("allPermissionSet must contain every registered key")
	}
	if _, ok := svc.systemPermissionSet["system.users.admin"]; !ok {
		t.Error("systemPermissionSet must contain every System:true key")
	}
	if _, ok := svc.allPermissionSet["system.users.admin"]; !ok {
		t.Error("a System key is ALSO in allPermissionSet — the checks are ordered, not exclusive")
	}
}
```

And the H-4 probe itself, appended to `backend/internal/core/authz/services/tier1_crud_test.go`:

```go
// The audit's H-4 probe, inverted: it must now FAIL at the update.
func TestH4Probe_CannotEscalateACustomRoleAfterBinding(t *testing.T) {
	svc, ctx := newTier1Service(t)
	registerPermissions(t, svc, "tenant.read", "tenant.delete")
	grantActor(t, svc, "actor-1", "tenant-1", "tenant.read")

	role, err := svc.CreateRole(ctx, "tenant-1", "actor-1", models.CreateRoleInput{
		Name: "harmless", Permissions: []string{"tenant.read"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if _, err := svc.CreateBinding(ctx, bindingFor("actor-1", role.UUID, "tenant-1")); err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}

	perms := []string{"tenant.read", "tenant.delete"}
	_, err = svc.UpdateRole(ctx, "tenant-1", role.UUID, "actor-1", models.UpdateRoleInput{Permissions: perms})
	if !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Fatalf("the probe must fail at the update, got %v", err)
	}
}

// Edge case 13: an IsActive-only patch does not re-validate, so a role
// that already holds a stale key can still be disabled.
func TestUpdateRole_IsActiveOnlyPatchSkipsValidation(t *testing.T) {
	svc, ctx := newTier1Service(t)
	role := seedCustomRoleWithStaleKey(t, svc, "tenant-1", "system.users.admin")

	active := false
	if _, err := svc.UpdateRole(ctx, "tenant-1", role.UUID, "actor-1", models.UpdateRoleInput{IsActive: &active}); err != nil {
		t.Fatalf("disabling a role with a stale key must still work: %v", err)
	}
}

// An empty or whitespace name is a 400, not the 500 the two errors.New
// paths produce today.
func TestUpdateRole_EmptyNameIsARoleNameError(t *testing.T) {
	svc, ctx := newTier1Service(t)
	role := seedCustomRole(t, svc, "tenant-1")
	name := "   "
	if _, err := svc.UpdateRole(ctx, "tenant-1", role.UUID, "actor-1", models.UpdateRoleInput{Name: &name}); !errors.Is(err, ErrRoleNameRequired) {
		t.Fatalf("err = %v, want ErrRoleNameRequired", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/authz/services/ -run 'ValidateCustomRole|H4Probe|UpdateRole_IsActive|UpdateRole_EmptyName|AllPermissionSet' -count=1`
Expected: FAIL to compile — `validateCustomRolePermissions` undefined, `CreateRole`/`UpdateRole` take no actor.

- [ ] **Step 3: Write the validator**

Create `backend/internal/core/authz/services/role_validation.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/orkestra/backend/internal/core/authz/models"
)

var (
	// ErrRolePermissionsRequired — a supplied permission list that is
	// empty, or empty after trimming. Distinct from "not supplied": an
	// UpdateRoleInput with a nil Permissions field is a patch that does
	// not touch permissions at all.
	ErrRolePermissionsRequired = errors.New("authz: permissions cannot be empty")

	// ErrUnknownPermission — a key that is not in the registered
	// catalog. The catalog is the union of every module's Permissions()
	// registered once at boot; a key outside it can never be checked by
	// anything, so writing it into a role is at best dead weight and at
	// worst a typo hiding a real grant.
	ErrUnknownPermission = errors.New("authz: unknown permission")

	// ErrSystemPermissionInCustomRole — a platform-reserved key, or the
	// wildcard, in a custom (tenant) role. Platform permissions are
	// granted through platform system roles and global bindings; a
	// tenant role that carried one would be an escalation path out of
	// its own tenant.
	ErrSystemPermissionInCustomRole = errors.New("authz: system permissions cannot be granted through a custom role")

	// ErrGranterRequired — no actor was supplied. The cascade cannot be
	// evaluated without one, and defaulting to "allow" is exactly the
	// hole this closes.
	ErrGranterRequired = errors.New("authz: granter is required")

	// ErrRoleNameRequired — an empty or whitespace-only name. Replaces
	// the bare errors.New paths that surfaced as 500.
	ErrRoleNameRequired = errors.New("authz: name cannot be empty")
)

// validateCustomRolePermissions is the single gate every supplied
// permission list passes, on create and on update alike.
//
// H-4: UpdateRole replaced `permissions` verbatim, so a caller who could
// edit a custom role could write any string into it — a platform key, a
// permission nobody holds, the wildcard — and then bind themselves to
// it. Bindings have had a cascade check since the org-role split; roles
// did not, which made the role editor the way around it.
//
// Four checks, in this order, because each explains a different refusal:
//
//  1. non-empty after trim and de-duplication;
//  2. every key exists in the registered catalog;
//  3. no key is "*" or a platform-reserved key — this one binds even a
//     super_admin (edge case 14): the wildcard is about what the ACTOR
//     may grant, not about what a TENANT role may carry;
//  4. the cascade: the actor must already hold every key, unless the
//     actor is granterSystem (seeding and internal callers), which
//     bypasses ONLY this step.
//
// Returns the cleaned list to persist.
func (s *Service) validateCustomRolePermissions(ctx context.Context, tenantID, actor string, keys []string) ([]string, error) {
	cleaned := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		cleaned = append(cleaned, k)
	}
	if len(cleaned) == 0 {
		return nil, ErrRolePermissionsRequired
	}

	s.mu.RLock()
	for _, k := range cleaned {
		if _, ok := s.allPermissionSet[k]; !ok && k != "*" {
			s.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrUnknownPermission, k)
		}
	}
	for _, k := range cleaned {
		if k == "*" {
			s.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSystemPermissionInCustomRole, k)
		}
		if _, ok := s.systemPermissionSet[k]; ok {
			s.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSystemPermissionInCustomRole, k)
		}
	}
	s.mu.RUnlock()

	if actor == "" {
		return nil, ErrGranterRequired
	}
	if actor == granterSystem {
		return cleaned, nil
	}

	granterPerms, err := s.GetEffectivePermissions(ctx, actor, tenantID)
	if err != nil {
		return nil, err
	}
	if err := validateBindingCascade(&models.Role{Permissions: cleaned}, granterPerms); err != nil {
		return nil, err
	}
	return cleaned, nil
}
```

> The catalog check runs before the system-key check so an unknown key is
> reported as unknown rather than as forbidden. `"*"` is exempted from
> the catalog check because it is never a registered key; check 3 refuses
> it a line later.

- [ ] **Step 4: Wire it into `CreateRole` and `UpdateRole`**

```go
// CreateRole builds a custom role for one tenant. actor is the UUID of
// the caller whose effective permissions bound what the role may carry
// (D21) — pass granterSystem for seeding and internal callers.
func (s *Service) CreateRole(ctx context.Context, tenantID, actor string, input models.CreateRoleInput) (*models.Role, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrRoleNameRequired
	}
	perms, err := s.validateCustomRolePermissions(ctx, tenantID, actor, input.Permissions)
	if err != nil {
		return nil, err
	}
	role := &models.Role{
		UUID:        uuid.NewString(),
		TenantID:    tenantID,
		Name:        name,
		Description: input.Description,
		Permissions: perms,
		IsSystem:    false,
		IsActive:    true,
	}
	if err := s.repo.UpsertRole(ctx, role); err != nil {
		return nil, err
	}
	return role, nil
}
```

In `UpdateRole`, replace the two `errors.New` paths and validate whenever `Permissions` is supplied:

```go
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, ErrRoleNameRequired
		}
		fields["name"] = name
	}
	…
	if input.Permissions != nil {
		// An IsActive-only patch never reaches here, so a role that
		// already holds a stale key can still be disabled (edge case 13).
		perms, err := s.validateCustomRolePermissions(ctx, tenantID, actor, input.Permissions)
		if err != nil {
			return nil, err
		}
		fields["permissions"] = perms
	}
```

- [ ] **Step 5: Add the wire codes and map the sentinels**

`backend/internal/shared/errcode/codes.go`:

```go
// AuthzPermissionUnknown — 422; a role edit named a permission key that
// is not in the registered catalog. The detail names the key.
const AuthzPermissionUnknown = "authz.permission_unknown"

// AuthzSystemPermissionForbidden — 422; a role edit tried to put a
// platform-reserved key (or the wildcard) into a tenant-scoped custom
// role. Platform permissions are granted through platform system roles
// and global bindings only.
const AuthzSystemPermissionForbidden = "authz.system_permission_forbidden"
```

plus golden rows. In `handlers/handler.go`, pass the actor (`ctxauth.GetUserUUID(ctx)`, exactly as `createBinding` does at `:298`) and map:

| Sentinel | Status | Code |
|---|---|---|
| `ErrRoleNameRequired` | 400 | — |
| `ErrRolePermissionsRequired` | 400 | — |
| `ErrGranterRequired` | 400 | — |
| `ErrUnknownPermission` | 422 | `authz.permission_unknown` |
| `ErrSystemPermissionInCustomRole` | 422 | `authz.system_permission_forbidden` |
| `ErrInsufficientPermissionsToGrant` | 403 | existing message |

- [ ] **Step 6: Fix every other caller**

`grep -rn "CreateRole(\|UpdateRole(" --include="*.go" backend/ | grep -v _test` and pass `granterSystem` at every internal call site (seeding, the tenant module's provisioning hook if it creates roles). A test that constructs a role directly is unaffected.

- [ ] **Step 7: Run and commit**

```bash
go vet ./... && go test ./internal/core/authz/... ./internal/shared/errcode/ -count=1
cd /home/tore/orkestra && git add backend/internal/core/authz backend/internal/shared/errcode
git commit -m "$(cat <<'EOF'
fix(authz): validate custom-role permissions like bindings (H-4)

CreateRole and UpdateRole took no actor and wrote `permissions`
verbatim: no catalog check, no platform-key check, no cascade. Bindings
have had a cascade check since the org-role split, so the role editor
was the way around it — create a harmless role, bind yourself to it,
then rewrite its permission list to anything at all.

Both now take an actor and run every supplied list through one gate:
non-empty after trim and de-duplication, every key in the registered
catalog, no wildcard and no platform-reserved key, and the same cascade
check bindings use. The system-key rule binds even a super_admin: the
wildcard governs what an ACTOR may grant, not what a TENANT role may
carry. granterSystem bypasses the cascade alone.

The two errors.New paths that surfaced as 500 become 400
ErrRoleNameRequired / ErrRolePermissionsRequired; unknown and
platform-reserved keys answer 422 naming the key.

Spec §4.4 D21. Closes H-4.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 2: A tenant-scoped binding never contributes a platform permission (D22)

`authz/CLAUDE.md:204` documents this as evaluator rule 4. It is incidental today, not enforced.

**Files:**
- Modify: `backend/internal/core/authz/services/service.go` (`GetEffectivePermissions` `:642-657`)
- Modify: `backend/internal/core/authz/CLAUDE.md` (`:204`)
- Test: `backend/internal/core/authz/services/tier1_test.go`

**Interfaces:**
- Consumes: `s.systemPermissionSet`.
- Produces: no new exports.

- [ ] **Step 1: Write the failing test**

```go
// Rule 4 (authz/CLAUDE.md): a tenant-scoped binding never grants a
// platform permission. It was true incidentally — no seeded tenant role
// carries one — which is not the same as enforced. Existing data can
// carry a stale system key (edge case 13), and until D21 anyone could
// write one in.
func TestGetEffectivePermissions_TenantBindingCannotGrantASystemKey(t *testing.T) {
	svc, ctx := newTier1Service(t)
	registerPermissions(t, svc, "tenant.read")
	registerSystemPermissions(t, svc, "system.users.admin")

	role := seedCustomRoleWithPermissions(t, svc, "tenant-1", "tenant.read", "system.users.admin")
	bindTenantScoped(t, svc, "u-1", role.UUID, "tenant-1")

	perms, err := svc.GetEffectivePermissions(ctx, "u-1", "tenant-1")
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if contains(perms, "system.users.admin") {
		t.Fatal("a tenant-scoped binding must never contribute a platform permission")
	}
	if !contains(perms, "tenant.read") {
		t.Fatal("its ordinary permissions must still come through")
	}
}

// A GLOBAL binding is where a platform role legitimately grants a
// platform key — that path is untouched.
func TestGetEffectivePermissions_GlobalBindingStillGrantsSystemKeys(t *testing.T) {
	svc, ctx := newTier1Service(t)
	registerSystemPermissions(t, svc, "system.users.admin")
	role := seedSystemRoleWithPermissions(t, svc, "administrator", "system.users.admin")
	bindGlobal(t, svc, "u-1", role.UUID)

	perms, err := svc.GetEffectivePermissions(ctx, "u-1", "tenant-1")
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if !contains(perms, "system.users.admin") {
		t.Fatal("a global binding must still grant platform keys")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/authz/services/ -run TenantBindingCannot -count=1`
Expected: FAIL — the system key comes through.

- [ ] **Step 3: Filter the tenant-scoped union**

```go
	// Union of tenant-scoped bindings.
	//
	// Platform-reserved keys are skipped here, which makes the
	// documented evaluator rule 4 ("a tenant-scoped binding never grants
	// a platform permission") enforced rather than incidental. Existing
	// data can carry a stale system key on a custom role — D21 refuses
	// to write new ones, and this makes the ones already there inert —
	// and a global binding, where a platform role legitimately grants a
	// platform key, is untouched. Closes the audit's L-9 at zero cost.
	if tenantID != "" {
		scoped, err := s.repo.ListActiveBindingsForUser(ctx, userUUID, tenantID)
		if err != nil {
			return nil, err
		}
		s.mu.RLock()
		for _, b := range scoped {
			role, err := s.repo.GetRoleByUUID(ctx, b.RoleUUID)
			if err != nil || !role.IsActive {
				continue
			}
			for _, p := range role.Permissions {
				if _, isSystem := s.systemPermissionSet[p]; isSystem {
					continue
				}
				perms[p] = struct{}{}
			}
		}
		s.mu.RUnlock()
	}
```

> Hold `s.mu.RLock()` across the loop, not per key — the repository
> calls inside it do not touch `s.mu`, and re-acquiring per permission
> would be gratuitous. If the surrounding style forbids holding a lock
> across I/O, snapshot `systemPermissionSet` into a local set before the
> loop instead.

- [ ] **Step 4: Run, document, commit**

Update `backend/internal/core/authz/CLAUDE.md:204` to say rule 4 is enforced by `GetEffectivePermissions`, and `:97`, `:146-153`, `:157-162`, `:177`, `:181`, `:222` where they describe the cascade's reach.

```bash
go vet ./... && go test ./internal/core/authz/... -count=1
git add backend/internal/core/authz
git commit -m "$(cat <<'EOF'
fix(authz): enforce rule 4 — tenant bindings grant no platform keys

authz/CLAUDE.md documented it; nothing enforced it. It held only
because no seeded tenant role carries a platform key, which is not a
guarantee: existing data can carry a stale one, and until the previous
commit anyone able to edit a custom role could write one in.

GetEffectivePermissions now skips platform-reserved keys when unioning
TENANT-scoped bindings. Global bindings — where a platform role
legitimately grants a platform key — are untouched.

Spec §4.4 D22. Closes L-9.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 3: One Cedar forbid for every system action (D23)

Every `tenant_roles.*` permit fires on `system.*` actions for internal tenants. Under `CEDAR_ENFORCE_ACTIONS` an `org_owner` gets `system.users.admin`. Cedar forbids outrank permits, so one rule closes all of them — present and future.

**Files:**
- Create: `backend/internal/core/authz/cedar/policies/system_actions.cedar`
- Modify: `backend/internal/core/authz/CLAUDE.md` (`:250`, `:261`, new policy row)
- Test: `backend/internal/core/authz/cedar/engine_test.go`

**Interfaces:**
- Consumes: `context.action_module` (already populated — `policycoverage`'s `cedarModuleRE` proves the org_billing policy uses it), `principal.system_role`.
- Produces: policy id `system_actions.require_platform_role`.

- [ ] **Step 1: Write the failing engine tests**

Append to `backend/internal/core/authz/cedar/engine_test.go`:

```go
// H-5: every tenant_roles.* permit fires on system.* actions for
// internal tenants, so under enforce an org_owner — a TENANT role —
// would hold system.users.admin. Cedar forbids outrank permits, so one
// rule closes every present and future tenant-role permit at once.
func TestSystemActions_TenantRolesCannotHoldSystemActions(t *testing.T) {
	cases := []struct {
		name        string
		tenantRoles []string
		action      string
	}{
		{"org_owner + system.users.admin", []string{"org_owner"}, "system.users.admin"},
		{"org_admin + system.modules.admin", []string{"org_admin"}, "system.modules.admin"},
		{"legacy administrator tenant role", []string{"administrator"}, "system.users.admin"},
		{"org_member", []string{"org_member"}, "system.users.admin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := evaluate(t, request{
				SystemRole:  "", // no platform role
				TenantRoles: tc.tenantRoles,
				TenantKind:  "internal",
				Action:      tc.action,
			})
			if d.Allowed {
				t.Fatalf("%s must be DENIED: a tenant role is not a platform role", tc.action)
			}
		})
	}
}

// The three platform roles keep their system actions.
func TestSystemActions_PlatformRolesStillAllowed(t *testing.T) {
	for _, role := range []string{"super_admin", "administrator", "developer"} {
		t.Run(role, func(t *testing.T) {
			d := evaluate(t, request{
				SystemRole: role, TenantKind: "internal", Action: "system.users.admin",
			})
			if !d.Allowed {
				t.Fatalf("%s must keep system.users.admin", role)
			}
		})
	}
}

// The forbid is about WHO may hold system actions. developer stays
// exempt because the role table already bounds it to read-only in
// production and platform.developer.prod_readonly mirrors that
// (edge case 15).
func TestSystemActions_DeveloperUnchangedInNonProd(t *testing.T) {
	d := evaluate(t, request{SystemRole: "developer", TenantKind: "internal", Action: "system.modules.admin", Env: "staging"})
	if !d.Allowed {
		t.Fatal("developer's non-prod behaviour is unchanged by this rule")
	}
}

// Non-system actions are untouched: an org_owner still owns its tenant.
func TestSystemActions_NonSystemActionsUnaffected(t *testing.T) {
	d := evaluate(t, request{TenantRoles: []string{"org_owner"}, TenantKind: "internal", Action: "tenant.update"})
	if !d.Allowed {
		t.Fatal("the forbid must fire only on system.* actions")
	}
}

// A principal with NO system_role attribute at all (not merely an empty
// one) must also be forbidden — `principal has system_role` is the
// guard, and getting it backwards would open the whole rule.
func TestSystemActions_MissingSystemRoleAttributeIsForbidden(t *testing.T) {
	d := evaluate(t, request{OmitSystemRole: true, TenantRoles: []string{"org_owner"}, TenantKind: "internal", Action: "system.users.admin"})
	if d.Allowed {
		t.Fatal("a principal with no system_role attribute must be forbidden")
	}
}
```

Update `TestPolicyLoadingNonEmpty`'s expected policy count by one.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/authz/cedar/ -run SystemActions -count=1`
Expected: FAIL — the tenant-role cases are allowed.

- [ ] **Step 3: Write the policy**

Create `backend/internal/core/authz/cedar/policies/system_actions.cedar`:

```cedar
// system_actions.cedar — platform-reserved actions need a platform role.
//
// Every system.* permission is declared System: true and gated with
// RequireSystemPermission (tenantID == ""). Tenant-scoped role permits in
// tenant_roles.cedar are written for tenant resources and must never be
// the reason a system.* action is allowed: Cedar forbids win over permits,
// so this single rule closes every present and future tenant-role permit.
//
// This is the H-5 fix. Fixing tenant_roles.cedar permit-by-permit would
// have to be repeated for every permit added later; a forbid cannot be
// forgotten.
//
// developer stays in the exempt set: the role table already bounds it to
// read-only in production and platform.developer.prod_readonly mirrors
// that. The forbid is about WHO may hold system actions; the permits are
// about WHICH.
@id("system_actions.require_platform_role")
forbid (
    principal,
    action,
    resource
) when {
    context has action_module &&
    context.action_module == "system" &&
    !(principal has system_role &&
      (principal.system_role == "super_admin" ||
       principal.system_role == "administrator" ||
       principal.system_role == "developer"))
};
```

- [ ] **Step 4: Run, verify policycoverage, document, commit**

```bash
go vet ./... && go test ./internal/core/authz/... -count=1
make -C /home/tore/orkestra ci-backend
```

`policycoverage` regex-scans the directory (`tools/policycoverage/scanner.go:300-319`), so a new file needs **no baseline change**. If it reports one, read why before adding an entry — the memory note `project_policycoverage_addon_scan` says the "DO NOT add by hand" comment is not to be trusted, but a *core* file appearing in the scan is normal.

Add a `system_actions.cedar` row to the policy table in `backend/internal/core/authz/CLAUDE.md` and correct the enforce note at `:250` and the `tenant_roles` description at `:261`.

```bash
git add backend/internal/core/authz
git commit -m "$(cat <<'EOF'
fix(authz): forbid system.* actions without a platform role (H-5)

Every tenant_roles.* permit fires on system.* actions for internal
tenants, so under CEDAR_ENFORCE_ACTIONS an org_owner — a TENANT role —
would hold system.users.admin.

Cedar forbids outrank permits, so one rule on
context.action_module == "system" closes every present and future
tenant-role permit at once. Fixing the permits individually would have
to be repeated for every permit added later; a forbid cannot be
forgotten.

developer stays exempt: the role table already bounds it to read-only in
production and platform.developer.prod_readonly mirrors that. The forbid
governs WHO may hold system actions, the permits WHICH.

CEDAR_ENFORCE_ACTIONS is NOT flipped here — that stays a separate
operational step (spec §7), gated on shadow divergence being quiet.

Spec §4.5 D23.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 4: Global checks carry no tenant roles (D24)

`shadowEvaluate` stamps `principal.tenant_roles` from the JWT even when `tenantID == ""`. For `RequireSystemPermission` and the impersonation pre-check, a membership role in whatever tenant the request happened to resolve is not an input to the decision.

**Files:**
- Modify: `backend/internal/core/authz/services/service.go` (`:396`)
- Test: `backend/internal/core/authz/services/service_test.go`

**Interfaces:**
- Consumes: `ctxauth.GetTenantRoles`.
- Produces: no new exports.

- [ ] **Step 1: Write the failing tests**

```go
// A GLOBAL check has no tenant, so a membership role in whatever tenant
// the request happened to resolve is not an input to the decision. The
// callers with an empty tenant are RequireSystemPermission and the
// impersonation pre-check (auth.go:549, :967).
func TestShadowEvaluate_GlobalCheckStampsNoTenantRoles(t *testing.T) {
	svc, _ := newShadowTestService(t)
	ctx := ctxauth.WithTenantRoles(context.Background(), []string{"org_owner"})

	captured := captureCedarRequest(t, svc, func() {
		svc.shadowEvaluate(ctx, "u-1", "" /* tenantID */, "system.users.admin")
	})
	if len(captured.TenantRoles) != 0 {
		t.Fatalf("TenantRoles = %v on a global check, want none", captured.TenantRoles)
	}
}

func TestShadowEvaluate_TenantScopedCheckStillStampsThem(t *testing.T) {
	svc, _ := newShadowTestService(t)
	ctx := ctxauth.WithTenantRoles(context.Background(), []string{"org_owner"})

	captured := captureCedarRequest(t, svc, func() {
		svc.shadowEvaluate(ctx, "u-1", "tenant-1", "tenant.update")
	})
	if len(captured.TenantRoles) != 1 || captured.TenantRoles[0] != "org_owner" {
		t.Fatalf("TenantRoles = %v, want [org_owner]", captured.TenantRoles)
	}
}

// The H-5 probe, inverted: under enforce, the role-table deny must not
// be overridden by a tenant-role permit.
func TestShadowEvaluate_EnforceDoesNotOverrideTheRoleTableDeny(t *testing.T) {
	svc, _ := newShadowTestService(t, withEnforceActions("system.users.admin"))
	ctx := ctxauth.WithTenantRoles(context.Background(), []string{"org_owner"})

	allowed := svc.HasPermission(ctx, "u-1", "" /* global */, "system.users.admin")
	if allowed {
		t.Fatal("an org_owner must not obtain system.users.admin under enforce")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/authz/services/ -run ShadowEvaluate -count=1`
Expected: FAIL — the roles are stamped regardless.

- [ ] **Step 3: Gate the stamp**

At `service.go:396`:

```go
	// Tenant roles belong to a TENANT-scoped decision. A global check
	// (tenantID == "") is made by RequireSystemPermission and by the
	// impersonation pre-check (auth.go:549, :967), and for those a
	// membership role in whatever tenant the request happened to resolve
	// is not an input to the decision — stamping it is how a tenant
	// permit came to fire on a platform action.
	var tenantRoles []string
	if tenantID != "" {
		tenantRoles, _ = ctxauth.GetTenantRoles(ctx)
	}
```

- [ ] **Step 4: Run and commit**

```bash
go vet ./... && go test ./internal/core/authz/... -count=1
git add backend/internal/core/authz
git commit -m "$(cat <<'EOF'
fix(authz): stop stamping tenant roles on global authorization checks

shadowEvaluate stamped principal.tenant_roles from the JWT even when the
check had no tenant at all. The callers with an empty tenant are
RequireSystemPermission and the impersonation pre-check, and for those a
membership role in whatever tenant the request happened to resolve is
not an input to the decision — it is how a tenant-scoped permit came to
fire on a platform action in the first place.

Spec §4.5 D24. The second half of H-5, with the forbid in the previous
commit.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 5: A generation-keyed cache and one invalidation contract (D26, D27 service half)

`cacheInvalidate` runs `KEYS authz:cache:<user>:*` + `DEL`: it scans, it can partially fail, it races a concurrent repopulation, and the glob is built from a request body (L-11). A generation key makes invalidation one atomic `INCR`.

**Files:**
- Modify: `backend/pkg/sdk/iface/interfaces.go`
- Modify: `backend/internal/core/authz/services/service.go` (`cacheKey` `:1199-1204`, `cacheGet` `:1206`, `cacheSet` `:1221`, `cacheInvalidate` `:1232-1241`, `flushCache` `:1186-1195`)
- Modify: `backend/internal/shared/errcode/codes.go` + `codes_test.go`
- Modify: `backend/pkg/sdk/CLAUDE.md`, `backend/internal/core/authz/CLAUDE.md` (`:179`)
- Test: `backend/internal/core/authz/services/cache_test.go`

**Interfaces:**
- Consumes: `s.redis` (`database.RedisClientAdapter`, which has `MGet`? — **verify**: if the adapter has no `MGet`, add it there and to the narrow client interface the service holds, in this task).
- Produces:
  - `iface.AuthzCacheInvalidator interface { InvalidateUserPermissions(ctx context.Context, userUUID string) error }`
  - `func (s *Service) withGeneration(ctx context.Context, scope generationScope, mutate func() error) error`
  - `errcode.AuthzCacheUnavailable = "authz.cache_unavailable"`
- Later tasks rely on: `InvalidateUserPermissions` (Task 6's handler), `withGeneration` (wrapping the authz mutations).

- [ ] **Step 1: Write the failing cache tests**

Append to `backend/internal/core/authz/services/cache_test.go`:

```go
// The old invalidation ran KEYS authz:cache:<user>:* + DEL. It scans,
// it can partially fail, it races a repopulation that lands between the
// scan and the delete, and the glob came from a request body (L-11).
// A generation key makes it one atomic INCR.

func TestCacheKey_CarriesBothGenerations(t *testing.T) {
	svc, _, mr := newCacheTestService(t, nil)
	mr.Set("authz:gen", "4")
	mr.Set("authz:gen:u-1", "7")

	key := svc.cacheKey(context.Background(), "u-1", "tenant-1")
	if key != "authz:cache:4:u-1:7:tenant-1" {
		t.Fatalf("cacheKey = %q", key)
	}
}

func TestCacheKey_MissingGenerationsReadAsZero(t *testing.T) {
	svc, _, _ := newCacheTestService(t, nil)
	key := svc.cacheKey(context.Background(), "u-1", "")
	if key != "authz:cache:0:u-1:0:-" {
		t.Fatalf("cacheKey = %q, want zeros and the '-' tenant placeholder", key)
	}
}

// One INCR, no KEYS. The command log is the assertion: a KEYS here is
// the defect, not an implementation detail.
func TestInvalidateUserPermissions_IsOneIncrAndNoScan(t *testing.T) {
	svc, _, mr := newCacheTestService(t, nil)
	mr.ResetCommandLog()

	if err := svc.InvalidateUserPermissions(context.Background(), "u-1"); err != nil {
		t.Fatalf("InvalidateUserPermissions: %v", err)
	}
	cmds := mr.CommandLog()
	if countCommand(cmds, "KEYS") != 0 {
		t.Fatalf("a KEYS scan was issued: %v", cmds)
	}
	if countCommand(cmds, "INCR") != 1 {
		t.Fatalf("want exactly one INCR, got: %v", cmds)
	}
}

// An entry written under the previous generation can never be READ
// again — that is what makes the invalidation total instead of
// best-effort.
func TestGenerationBump_RetiresTheOldEntry(t *testing.T) {
	svc, _, _ := newCacheTestService(t, nil)
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "tenant-1", []string{"tenant.read"})

	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-1"); !ok {
		t.Fatal("precondition: the entry must be readable")
	}
	if err := svc.InvalidateUserPermissions(ctx, "u-1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-1"); ok {
		t.Fatal("an entry written under the previous generation must not be readable")
	}
}

// The global flush retires EVERY user's entries with one INCR.
func TestFlushCache_RetiresEveryUser(t *testing.T) {
	svc, _, mr := newCacheTestService(t, nil)
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	svc.cacheSet(ctx, "u-2", "t-1", []string{"b"})
	mr.ResetCommandLog()

	if err := svc.flushCache(ctx); err != nil {
		t.Fatalf("flushCache: %v", err)
	}
	if countCommand(mr.CommandLog(), "KEYS") != 0 {
		t.Fatal("the global flush must not scan either")
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Error("u-1's entry survived the global flush")
	}
	if _, ok := svc.cacheGet(ctx, "u-2", "t-1"); ok {
		t.Error("u-2's entry survived the global flush")
	}
}

// A generation read that fails is a cache MISS — the evaluator goes to
// Mongo, which is the fresh answer. It must never be a stale hit and
// never an error to the caller.
func TestCacheGet_GenerationReadFailureIsAMiss(t *testing.T) {
	svc, _, mr := newCacheTestService(t, nil)
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	mr.Close()

	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Fatal("a generation read failure must read as a miss")
	}
}

// Retired entries are not deleted; they expire on their own 60s TTL.
func TestRetiredEntries_ExpireOnTheirOwnTTL(t *testing.T) {
	svc, _, mr := newCacheTestService(t, nil)
	ctx := context.Background()
	svc.cacheSet(ctx, "u-1", "t-1", []string{"a"})
	oldKey := svc.cacheKey(ctx, "u-1", "t-1")

	_ = svc.InvalidateUserPermissions(ctx, "u-1")
	if !mr.Exists(oldKey) {
		t.Fatal("the retired entry is not deleted — it is simply unreachable")
	}
	mr.FastForward(61 * time.Second)
	if mr.Exists(oldKey) {
		t.Fatal("the retired entry must expire on its own TTL")
	}
}

// An INCR failure is RETURNED, because the caller decides what it means
// (D27: a failed pre-invalidation refuses the change).
func TestInvalidateUserPermissions_ErrorIsReturned(t *testing.T) {
	svc, _, mr := newCacheTestService(t, nil)
	mr.Close()
	if err := svc.InvalidateUserPermissions(context.Background(), "u-1"); err == nil {
		t.Fatal("an INCR failure must be returned, not swallowed")
	}
}

// A nil Redis means no cache at all: reads miss, invalidation is a
// no-op success. A test setup without Redis must not fail every role
// edit.
func TestCache_NilRedisDegradesCleanly(t *testing.T) {
	svc := &Service{}
	if _, ok := svc.cacheGet(context.Background(), "u-1", "t-1"); ok {
		t.Error("a nil Redis must read as a miss")
	}
	if err := svc.InvalidateUserPermissions(context.Background(), "u-1"); err != nil {
		t.Errorf("a nil Redis must make invalidation a no-op success, got %v", err)
	}
}

// withGeneration: pre-invalidate, write, post-invalidate.
func TestWithGeneration_PreInvalidationFailureSkipsTheWrite(t *testing.T) {
	svc, _, mr := newCacheTestService(t, nil)
	mr.Close()
	written := false

	err := svc.withGeneration(context.Background(), userScope("u-1"), func() error {
		written = true
		return nil
	})
	if err == nil {
		t.Fatal("a pre-invalidation failure must refuse the mutation")
	}
	if written {
		t.Fatal("the mutation must not run when the pre-invalidation failed")
	}
}

// The post step retires an entry repopulated by a concurrent read
// between the pre-invalidation and the write — that read wrote the OLD
// verdict under the NEW generation.
func TestWithGeneration_PostInvalidationRetiresARepopulation(t *testing.T) {
	svc, _, _ := newCacheTestService(t, nil)
	ctx := context.Background()

	err := svc.withGeneration(ctx, userScope("u-1"), func() error {
		// Simulate the racing read that repopulates with the old answer.
		svc.cacheSet(ctx, "u-1", "t-1", []string{"stale"})
		return nil
	})
	if err != nil {
		t.Fatalf("withGeneration: %v", err)
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "t-1"); ok {
		t.Fatal("the post-invalidation must retire an entry repopulated during the write")
	}
}

func TestWithGeneration_MutationErrorPropagates(t *testing.T) {
	svc, _, _ := newCacheTestService(t, nil)
	sentinel := errors.New("write failed")
	if err := svc.withGeneration(context.Background(), userScope("u-1"), func() error {
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the mutation's own error", err)
	}
}

func TestService_ImplementsAuthzCacheInvalidator(t *testing.T) {
	var _ iface.AuthzCacheInvalidator = (*Service)(nil)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/authz/services/ -run 'CacheKey|Invalidate|Generation|FlushCache|WithGeneration|NilRedis' -count=1`
Expected: FAIL — `cacheKey` takes no context, `InvalidateUserPermissions` and `withGeneration` do not exist.

- [ ] **Step 3: Add the SDK seam**

In `backend/pkg/sdk/iface/interfaces.go`:

```go
// ---------------------------------------------------------------------------
// AuthzCacheInvalidator — consumed by: the user module after a system-role
// change. Narrow on purpose (the UserLifecycleStateProvider precedent):
// AuthzProvider is implemented by forks and stays additive-only.
//
// Resolve it with module.GetTyped against ServiceAuthzProvider. For a
// consumer that only READS verdicts a missing value means the cached verdict
// expires on its own TTL (60 s) — degrade, do not fail. For a consumer that
// MUTATES a role, a missing or failing invalidator means the effect of the
// change cannot be guaranteed, and the change is refused instead (D27).
// ---------------------------------------------------------------------------

type AuthzCacheInvalidator interface {
	// InvalidateUserPermissions retires every cached verdict for one
	// user, in every tenant, atomically. Returns an error the caller is
	// expected to act on.
	InvalidateUserPermissions(ctx context.Context, userUUID string) error
}
```

`module.GetTyped[T]` asserts the stored `iface.AuthzProvider(m.svc)` (`authz/module.go:228`) to `T`, which succeeds because the dynamic type is `*Service`.

- [ ] **Step 4: Make the cache generation-keyed**

```go
// --- Cache ---
//
// The key carries two generation counters — a global one and a per-user
// one — so invalidation is a single atomic INCR rather than a KEYS scan
// followed by a DEL.
//
// The scan version had four problems: it enumerated keys on the hot
// path, it could partially fail leaving some verdicts live, it raced a
// concurrent read that repopulated between the scan and the delete, and
// its glob was built from a request body (L-11). An entry written under
// an older generation simply becomes unreachable and dies on its own
// 60 s TTL.

const (
	authzGlobalGenKey = "authz:gen"
	authzUserGenKey   = "authz:gen:" // + userUUID
	authzCacheTTL     = 60 * time.Second
)

// generations reads both counters in ONE MGET. A failure returns
// ok=false and the caller treats it as a cache miss: going to Mongo is
// the fresh answer, so a degraded Redis costs latency, never
// correctness.
func (s *Service) generations(ctx context.Context, userUUID string) (global, user int64, ok bool) {
	if s.redis == nil {
		return 0, 0, false
	}
	vals, err := s.redis.MGet(ctx, authzGlobalGenKey, authzUserGenKey+userUUID)
	if err != nil {
		return 0, 0, false
	}
	return parseGen(vals, 0), parseGen(vals, 1), true
}

// cacheKey folds both generations in. A missing counter reads as 0.
func (s *Service) cacheKey(ctx context.Context, userUUID, tenantID string) string {
	if tenantID == "" {
		tenantID = "-"
	}
	g, u, _ := s.generations(ctx, userUUID)
	return fmt.Sprintf("authz:cache:%d:%s:%d:%s", g, userUUID, u, tenantID)
}

// InvalidateUserPermissions implements iface.AuthzCacheInvalidator: one
// atomic INCR of the user's generation. Every entry written under the
// previous value becomes unreachable at once.
func (s *Service) InvalidateUserPermissions(ctx context.Context, userUUID string) error {
	if s.redis == nil {
		// No cache to invalidate. A test or a minimal deployment must not
		// have every role edit refused.
		return nil
	}
	if _, err := s.redis.Incr(ctx, authzUserGenKey+userUUID); err != nil {
		return fmt.Errorf("authz: invalidate user permissions: %w", err)
	}
	return nil
}

// flushCache retires EVERY user's entries with one INCR of the global
// generation. Used by role update/delete, binding delete and the tenant
// cascades, where the set of affected users is not enumerable cheaply.
func (s *Service) flushCache(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}
	if _, err := s.redis.Incr(ctx, authzGlobalGenKey); err != nil {
		return fmt.Errorf("authz: flush cache: %w", err)
	}
	return nil
}
```

`cacheGet` and `cacheSet` take `ctx` through to `cacheKey`, and `cacheGet` returns a miss when `generations` reports `ok=false`.

> **Verify `MGet` exists** on `database.RedisClientAdapter` and on the
> narrow client interface `*Service` holds. If it does not, add it in
> this task next to `Get`: `MGet(ctx context.Context, keys ...string)
> ([]interface{}, error)`. Do not simulate it with two `Get`s — the
> whole point is one round trip.

- [ ] **Step 5: Add `withGeneration`**

```go
// generationScope names which counter a mutation retires.
type generationScope struct {
	user string // "" means the global counter
}

func userScope(userUUID string) generationScope { return generationScope{user: userUUID} }
func globalScope() generationScope              { return generationScope{} }

// withGeneration wraps a mutation that changes effective permissions in
// pre-invalidate → write → post-invalidate.
//
// The PRE step is a gate: a generation the store cannot bump means the
// change's effect cannot be guaranteed, so the change is REFUSED and
// nothing is written. Redis being unavailable already stops sessions,
// MFA challenges and OAuth state; refusing a permission change in that
// state is consistent, and the retry is the admin's.
//
// The POST step retires the only race left: a read that repopulated the
// cache between the pre-invalidation and the write stored the OLD
// verdict under the NEW generation. A failure here is logged, counted
// and reported by the caller — the change stands, and the stale entry
// dies within its own 60 s TTL.
func (s *Service) withGeneration(ctx context.Context, scope generationScope, mutate func() error) error {
	bump := func() error {
		if scope.user != "" {
			return s.InvalidateUserPermissions(ctx, scope.user)
		}
		return s.flushCache(ctx)
	}

	if err := bump(); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthzCacheUnavailable, err)
	}
	if err := mutate(); err != nil {
		return err
	}
	if err := bump(); err != nil {
		metrics.Default().RecordAuthzCacheInvalidationFailure()
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "authz: post-write cache invalidation failed; a verdict cached during the write may survive up to its TTL",
				slog.String("user_uuid", scope.user),
				slog.String("error", err.Error()))
		}
	}
	return nil
}
```

Add `ErrAuthzCacheUnavailable`, the `errcode.AuthzCacheUnavailable = "authz.cache_unavailable"` constant with its golden row (503 in the handler), and the metric family `orkestra_authz_cache_invalidation_failures_total` in `pkg/sdk/metrics` following PR A Task 2's shape (unlabelled).

Wrap `UpdateRole`, `DeleteRole`, `CreateBinding`/`EnsureBinding`, `DeleteBinding` and the tenant cascades in `withGeneration` — `globalScope()` where the affected user set is not enumerable, `userScope(u)` where it is.

- [ ] **Step 6: Run, document, commit**

```bash
go vet ./... && go test ./internal/core/authz/... ./pkg/sdk/... ./internal/shared/errcode/ -count=1
git add backend/internal/core/authz backend/pkg/sdk backend/internal/shared/errcode
git commit -m "$(cat <<'EOF'
fix(authz): make the permission cache generation-keyed

cacheInvalidate ran KEYS authz:cache:<user>:* followed by DEL. It
enumerated keys on the hot path, could partially fail leaving some
verdicts live, raced any read that repopulated between the scan and the
delete, and built its glob from a request body — the audit's L-11.

The key now folds in a global and a per-user generation counter read
with one MGET, so invalidation is a single atomic INCR: every entry
written under the previous value becomes unreachable at once and dies on
its own 60s TTL. No scan, no glob, no partial failure.

withGeneration wraps every mutation that changes effective permissions
in pre-invalidate → write → post-invalidate. The pre step is a GATE — a
counter the store cannot bump means the change's effect cannot be
guaranteed, so the change is refused with 503 and nothing is written.
The post step retires a repopulation that landed during the write; its
failure is logged, counted and non-fatal.

iface gains the narrow additive AuthzCacheInvalidator.

Spec §4.6 D26, D27 (service half). Closes L-11.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 6: The role branch does what the deactivate branch does (D27, handler half)

A system-role change flushes no cache and ends no session, and the tier guards trust the `srole` claim — so a demoted admin keeps admin verdicts for up to 60 s of cache plus a whole access-token lifetime of stale claim (M-13).

**Files:**
- Modify: `backend/internal/core/user/handlers/user_handler.go` (`UpdateUser` `:209-309`, `terminateSessions` `:62-75`)
- Modify: `backend/internal/shared/errcode/codes.go` + `codes_test.go`
- Modify: `backend/internal/core/user/CLAUDE.md` (`:121-122`)
- Test: `backend/internal/core/user/handlers/user_handler_test.go`

**Interfaces:**
- Consumes: `iface.AuthzCacheInvalidator` (Task 5), `iface.SessionTerminator`, `module.GetTyped`, `module.ServiceAuthzProvider`.
- Produces:
  - `func (h *UserHandler) invalidateAuthz(ctx context.Context, userUUID string) error`
  - `errcode.UserRoleChangeUnavailable = "user.role_change_unavailable"`

- [ ] **Step 1: Write the failing tests**

```go
// M-13: a role change flushed no cache and ended no session, so a
// demoted admin kept admin verdicts for up to the cache TTL plus a
// whole access-token lifetime of stale `srole`.
func TestUpdateUser_RoleChangeInvalidatesThenWritesThenInvalidates(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.seed(t, "u-1", "administrator")

	if _, err := callUpdateUser(t, h, "actor-super", "u-1", withRole("user")); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got := deps.invalidator.calls("u-1"); got != 2 {
		t.Fatalf("invalidator called %d times, want 2 (pre and post)", got)
	}
	if !deps.invalidator.orderIsPreWritePost() {
		t.Fatal("the order must be invalidate → write → invalidate")
	}
	if deps.sessions.terminated("u-1") != 1 {
		t.Fatal("a role change must end the sessions minted under the old role")
	}
}

// A change whose effect cannot be guaranteed is NOT applied.
func TestUpdateUser_PreInvalidationFailureRefusesTheChange(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.seed(t, "u-1", "administrator")
	deps.invalidator.failNext()

	_, err := callUpdateUser(t, h, "actor-super", "u-1", withRole("user"))
	assertStatusAndCode(t, err, http.StatusServiceUnavailable, "user.role_change_unavailable")
	if deps.users.updateCalls() != 0 {
		t.Fatal("UpdateUser must never be called when the pre-invalidation failed")
	}
}

// An invalidator absent from the registry is treated the same way FOR A
// MUTATION — the "degrade, do not fail" note applies to consumers that
// only read verdicts.
func TestUpdateUser_MissingInvalidatorRefusesARoleChange(t *testing.T) {
	h, deps := newUserHandlerForTest(t, withoutInvalidator())
	deps.users.seed(t, "u-1", "administrator")

	_, err := callUpdateUser(t, h, "actor-super", "u-1", withRole("user"))
	assertStatusAndCode(t, err, http.StatusServiceUnavailable, "user.role_change_unavailable")
}

// A post-invalidation failure is logged, counted and audited — and the
// change STANDS. Only an entry written in the sub-second window between
// the two bumps can carry the old verdict, and it dies within 60s.
func TestUpdateUser_PostInvalidationFailureKeepsTheChange(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.seed(t, "u-1", "administrator")
	deps.invalidator.failOnCall(2)

	if _, err := callUpdateUser(t, h, "actor-super", "u-1", withRole("user")); err != nil {
		t.Fatalf("the change must stand: %v", err)
	}
	if deps.users.roleOf("u-1") != "user" {
		t.Fatal("the role change must be persisted")
	}
	if !deps.audit.metadataFalse("u-1", "cache_invalidated") {
		t.Fatal("the audit row must record cache_invalidated: false")
	}
}

// The post step is what retires a read that repopulated the cache
// between the pre-invalidation and the write.
func TestUpdateUser_RepopulationDuringTheWriteIsRetired(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.seed(t, "u-1", "administrator")
	deps.users.beforeUpdate(func() { deps.invalidator.simulateRepopulation("u-1") })

	if _, err := callUpdateUser(t, h, "actor-super", "u-1", withRole("user")); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if deps.invalidator.repopulationSurvived("u-1") {
		t.Fatal("the post-invalidation must retire it")
	}
}

// A NON-role patch must not pay any of this: no invalidation, no
// termination beyond what deactivation already does.
func TestUpdateUser_NonRolePatchDoesNotInvalidate(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.seed(t, "u-1", "user")
	if _, err := callUpdateUser(t, h, "actor-super", "u-1", withFullName("New Name")); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if deps.invalidator.calls("u-1") != 0 {
		t.Fatal("a name change must not touch the authz cache")
	}
}

// Edge case 16: a role change on ONESELF terminates the actor's own
// sessions, the response is still 200, and the audit row is written
// first.
func TestUpdateUser_SelfRoleChangeStillSucceeds(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.seed(t, "actor-super", "super_admin")
	if _, err := callUpdateUser(t, h, "actor-super", "actor-super", withRole("administrator")); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if deps.sessions.terminated("actor-super") != 1 {
		t.Fatal("the actor's own sessions end too")
	}
}

// Edge case 17: with the auth module unwired (tests), termination
// degrades to a no-op with a WARN — as it does today.
func TestUpdateUser_UnwiredTerminatorDegrades(t *testing.T) {
	h, deps := newUserHandlerForTest(t, withoutTerminator())
	deps.users.seed(t, "u-1", "administrator")
	if _, err := callUpdateUser(t, h, "actor-super", "u-1", withRole("user")); err != nil {
		t.Fatalf("termination is best-effort: %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/user/handlers/ -run UpdateUser_ -count=1`
Expected: FAIL — no invalidator, no termination on the role branch.

- [ ] **Step 3: Add the code and the helper**

`errcode`:

```go
// UserRoleChangeUnavailable — 503; a system-role change was refused
// because its effect could not be guaranteed. The authz permission
// cache could not be invalidated (or no invalidator is wired), so the
// old role's cached verdicts would have outlived the change. Nothing was
// written; the operator retries.
const UserRoleChangeUnavailable = "user.role_change_unavailable"
```

`user_handler.go`:

```go
// invalidateAuthz retires every cached authorization verdict for one
// user. Unlike terminateSessions this is NOT best-effort on the way in:
// a role change whose effect cannot be guaranteed is refused rather than
// applied, because a stale "administrator" verdict is exactly the thing
// the change was meant to remove.
func (h *UserHandler) invalidateAuthz(ctx context.Context, userUUID string) error {
	if h.services == nil {
		return fmt.Errorf("service registry unavailable")
	}
	inv, ok := module.GetTyped[iface.AuthzCacheInvalidator](h.services, module.ServiceAuthzProvider)
	if !ok || inv == nil {
		return fmt.Errorf("authz cache invalidator not registered")
	}
	return inv.InvalidateUserPermissions(ctx, userUUID)
}
```

- [ ] **Step 4: Wrap the role branch**

In `UpdateUser`, once the escalation and quorum guards have passed and a role change is actually happening:

```go
	roleChanging := req.Body.Role != "" && previous != nil && previous.Role != req.Body.Role
	if roleChanging {
		// PRE. A change whose effect cannot be guaranteed is not
		// applied: refuse with 503 and write nothing.
		if err := h.invalidateAuthz(ctx, req.ID); err != nil {
			slog.ErrorContext(ctx, "user: refusing a role change, authz cache cannot be invalidated",
				slog.String("user_uuid", req.ID), slog.String("error", err.Error()))
			return nil, errcode.New(http.StatusServiceUnavailable, errcode.UserRoleChangeUnavailable,
				"The role change was not applied: the authorization cache could not be invalidated. Try again shortly.")
		}
	}

	updated, err := h.userService.UpdateUser(ctx, req.ID, patch)
	if err != nil {
		return nil, mapUserError(err)
	}

	cacheInvalidated := true
	sessionsTerminated := true
	if roleChanging {
		// POST. Retires the only race left: a read that repopulated the
		// cache between the pre-invalidation and the write stored the
		// OLD verdict under the NEW generation. A failure is recorded;
		// the change stands and the stale entry dies within 60s.
		if err := h.invalidateAuthz(ctx, req.ID); err != nil {
			cacheInvalidated = false
			metrics.Default().RecordAuthzCacheInvalidationFailure()
			slog.ErrorContext(ctx, "user: post-write authz invalidation failed",
				slog.String("user_uuid", req.ID), slog.String("error", err.Error()))
		}
		// A role change closes the sessions minted under the old role —
		// one invariant, both directions. Best-effort, exactly as
		// deactivation is.
		sessionsTerminated = h.terminateSessionsReporting(ctx, req.ID)
	}
```

and carry both flags in the existing `user.role.changed` event's metadata. Give `terminateSessions` a `…Reporting` sibling that returns whether it succeeded, leaving the silent original for the deactivate path.

- [ ] **Step 5: Run, document, commit**

Update `backend/internal/core/user/CLAUDE.md:121-122` to state that a role change invalidates the authz cache and terminates sessions, and that a failed pre-invalidation refuses the change.

```bash
go vet ./... && go test ./internal/core/user/... ./internal/shared/errcode/ -count=1
git add backend/internal/core/user backend/internal/shared/errcode
git commit -m "$(cat <<'EOF'
fix(user): make a system-role change take effect on the next decision

A role change flushed no authz cache and ended no session, so a demoted
administrator kept administrator verdicts for up to the 60s cache TTL
plus a whole access-token lifetime of stale `srole`.

The role branch now does what the deactivate branch does, in an order
that cannot leave a stale verdict: pre-invalidate, write, post-invalidate,
terminate. The pre step is a gate — a change whose effect cannot be
guaranteed is refused with 503 user.role_change_unavailable and nothing
is written, and an invalidator missing from the registry counts as the
same failure for a MUTATION. The post step retires a read that
repopulated the cache during the write; its failure is logged, counted,
recorded in the audit row, and the change stands.

Terminating on every change rather than on demotions alone keeps one
invariant instead of two code paths, and makes a promotion visible
immediately instead of at the next refresh.

Spec §4.6 D27. Closes M-13's cache and session halves.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 7: Tier guards read the database (D28), and the client PATCH gets them (D29)

`canAssignRole` takes the caller's role from the `srole` claim, which the previous task just proved can be stale. And the client-user PATCH has no role guards at all (M-17).

**Files:**
- Modify: `backend/internal/core/user/handlers/user_handler.go` (`:121`, `:211`)
- Modify: `backend/internal/core/user/handlers/admin_client_handler.go` (`:242-267`)
- Modify: `backend/internal/shared/setup/service.go` (`:363-369`, `evaluateAccess`/`Finalize`)
- Test: `backend/internal/core/user/handlers/user_handler_test.go`, `backend/internal/shared/setup/service_test.go`

**Interfaces:**
- Consumes: `h.userService.GetUser`, `iface.UserLifecycleStateProvider` (setup already performs this lookup).
- Produces: `evaluateAccess` / `Finalize` lose their `srole` parameter.

- [ ] **Step 1: Write the failing tests**

```go
// D28: the guard that decides whether a caller may assign a role must
// not read the role from the token the previous change was supposed to
// invalidate.
func TestCanAssignRole_ReadsTheCallerRoleFromTheDatabase(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.seed(t, "actor-1", "user")     // the truth
	deps.users.seed(t, "target-1", "user")
	ctx := ctxWithSystemRoleClaim(t, "actor-1", "super_admin") // a stale claim

	_, err := callUpdateUserCtx(t, ctx, h, "target-1", withRole("administrator"))
	assertStatusAndCode(t, err, http.StatusForbidden, "user.role_escalation_forbidden")
}

// A lookup failure is a 500, NEVER a fallback to the claim: falling back
// is how a stale claim becomes the authority again exactly when the
// database cannot contradict it.
func TestCanAssignRole_LookupFailureIsAnInternalError(t *testing.T) {
	h, deps := newUserHandlerForTest(t)
	deps.users.failGetUser("actor-1")
	ctx := ctxWithSystemRoleClaim(t, "actor-1", "super_admin")

	_, err := callUpdateUserCtx(t, ctx, h, "target-1", withRole("administrator"))
	assertStatus(t, err, http.StatusInternalServerError)
}

// D29 (flagged addition): the client-user PATCH ran no role guards at
// all, so a client admin could promote themselves.
func TestUpdateClientUserAdmin_RefusesEscalation(t *testing.T) {
	h, deps := newAdminClientHandlerForTest(t)
	deps.users.seed(t, "client-actor", "user")
	deps.users.seed(t, "client-target", "user")

	_, err := callUpdateClientUser(t, h, "client-actor", "client-target", withRole("administrator"))
	assertStatusAndCode(t, err, http.StatusForbidden, "user.role_escalation_forbidden")
	if !deps.audit.sawAction("user.update.refused") {
		t.Fatal("a refused client role change must emit user.update.refused, like the operator handler")
	}
}

func TestUpdateClientUserAdmin_RefusesAServiceAccountRole(t *testing.T) {
	h, deps := newAdminClientHandlerForTest(t)
	deps.users.seed(t, "client-actor", "administrator")
	deps.users.seed(t, "client-target", "user")

	_, err := callUpdateClientUser(t, h, "client-actor", "client-target", withRole(serviceAccountRole))
	if err == nil {
		t.Fatal("serviceAccountRoleAllowed must gate the client PATCH too")
	}
}

// The last-admin quorum stays OPERATOR-only: a client user is never a
// platform administrator.
func TestUpdateClientUserAdmin_NoLastAdminQuorum(t *testing.T) {
	h, deps := newAdminClientHandlerForTest(t)
	deps.users.seed(t, "client-actor", "super_admin")
	deps.users.seedSoleClientAdmin(t, "client-target")

	if _, err := callUpdateClientUser(t, h, "client-actor", "client-target", withRole("user")); err != nil {
		t.Fatalf("the client PATCH must not run the platform quorum check: %v", err)
	}
}

// The setup recovery gate compares the role from the lifecycle lookup it
// already performs, not the srole parameter.
func TestSetupFinalize_UsesTheDatabaseRole(t *testing.T) {
	svc, deps := newSetupServiceForTest(t)
	deps.users.seed(t, "actor-1", "user")

	if err := svc.Finalize(context.Background(), "actor-1"); err == nil {
		t.Fatal("a caller whose DATABASE role is 'user' must not finalize setup")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/user/handlers/ ./internal/shared/setup/ -run 'CanAssignRole|UpdateClientUserAdmin|SetupFinalize' -count=1`
Expected: FAIL — the claim is trusted and the client handler has no guards.

- [ ] **Step 3: Read the role from the database**

At `user_handler.go:121` and `:211`, replace `ctxauth.GetSystemRole(ctx)`:

```go
	// The caller's role comes from the DATABASE, not from `srole`. The
	// claim can be up to one access-token lifetime stale — that is
	// precisely the window the role-change propagation above exists to
	// close, so trusting it here would put the hole straight back.
	//
	// A lookup failure is a 500. Falling back to the claim would make it
	// authoritative again exactly when the database cannot contradict it.
	actor, err := h.userService.GetUser(ctx, actorUUID)
	if err != nil || actor == nil {
		return nil, errcode.Internal("Could not resolve the calling user's role")
	}
	callerRole := actor.Role
```

`GetSystemRole` keeps its remaining consumers — logging, navigation shaping, the dev-token fallback — none of which authorises. Note that in the doc comment where it is defined.

- [ ] **Step 4: Guard the client PATCH**

In `admin_client_handler.go`'s `UpdateClientUserAdmin`, run `canAssignRole` (with the DB-read role) and `serviceAccountRoleAllowed`, emitting the same `user.update.refused` audit row the operator handler emits. The last-admin quorum stays operator-only.

- [ ] **Step 5: Fix the setup gate**

In `internal/shared/setup/service.go:363-369`, compare the role returned by the `userLifecycleState` lookup the function already performs, and remove the `srole` parameter from `evaluateAccess` and `Finalize` along with every caller.

- [ ] **Step 6: Run, document, commit**

```bash
go vet ./... && go test ./... -count=1
git add backend/internal/core/user backend/internal/shared/setup
git commit -m "$(cat <<'EOF'
fix(user): read the caller's role from the database in the tier guards

canAssignRole took the caller's role from the `srole` claim, which can
be up to one access-token lifetime stale — precisely the window the
role-change propagation exists to close, so trusting it here put the
hole straight back. A lookup failure is now a 500, never a fallback to
the claim: falling back makes the claim authoritative again exactly when
the database cannot contradict it.

The setup recovery gate compares the role from the lifecycle lookup it
already performs, and evaluateAccess/Finalize lose their srole
parameter.

The client-user PATCH gains the same role guards as the operator one
(M-17): it ran none at all, so a client admin could promote themselves.
The last-admin quorum stays operator-only — a client user is never a
platform administrator.

GetSystemRole keeps its non-authorising consumers: logging, navigation
shaping, the dev-token fallback.

Spec §4.6 D28, D29.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 8: Documentation, OpenAPI and the staging drill

- [ ] **Step 1: Finish the docs sweep**

- `backend/internal/core/authz/CLAUDE.md` — `:90` (stale gate heading), `:97`, `:146-153`, `:157-162`, `:177`, `:179` (cache key + invalidation are generation-based), `:181`, `:204`, `:222`, `:250`, `:261`, plus the `system_actions.cedar` row.
- `backend/internal/core/user/CLAUDE.md` — `:121-122`.
- `backend/pkg/sdk/CLAUDE.md` — `AuthzCacheInvalidator`.
- `docs/site/modules/core/authz.mdx` — `:12-14`, `:51-58`, `:62-67`, `:78`, `:91`.
- `docs/site/modules/core/user.mdx` — `:15`, `:50`.

- [ ] **Step 2: Regenerate and gate**

```bash
make -C /home/tore/orkestra openapi-dump
make -C /home/tore/orkestra ci-backend
git diff --check
```

- [ ] **Step 3: Commit and open the PR**

```bash
cd /home/tore/orkestra && git add docs backend
git commit -m "$(cat <<'EOF'
docs(authz): document the role cascade, the forbid and the generation cache

Spec §4.11. Regenerates the OpenAPI dump for the new 422/503 codes.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
git push -u origin feat/auth-authz-audit-remediation-pr-c
gh pr create --base dev --title "PR C: role cascade, Cedar and propagation (auth/authz audit remediation)" --body "$(cat <<'EOF'
Implements §4.4 (D21, D22), §4.5 (D23, D24) and §4.6 (D26–D29) of `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md`, the third of five deliverables in §7. Independent of PRs A and B.

**Closes:** H-4, H-5, M-13, M-17, L-9, L-11.

**Does NOT flip `CEDAR_ENFORCE_ACTIONS`** — spec D25 keeps that a separate operational step, gated on `orkestra_cedar_shadow_divergence_total` being quiet for `system.*` suffixes for one release cycle on staging.

Plan: `docs/superpowers/plans/2026-09-03-auth-authz-audit-remediation-pr-c-authz-cascade-cedar-propagation.md`

https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

- [ ] **Step 4: Staging drill (spec §7, PR C row)**

1. Run the H-4 probe against the API: create a custom role with `tenant.read`, bind yourself, then `PATCH` it to add `tenant.delete` → **403** at the update.
2. `PATCH` a custom role with `system.users.admin` → **422 `authz.system_permission_forbidden`**, naming the key.
3. `PATCH` a custom role with a made-up key → **422 `authz.permission_unknown`**.
4. Demote a test administrator → their session ends, and their next request gets the new role's verdicts, not the old ones.
5. Stop Redis, attempt a role change → **503 `user.role_change_unavailable`**, and the user's role is unchanged in Mongo. Restart Redis, retry → succeeds.
6. Watch `orkestra_cedar_shadow_divergence_total` for `system.*` suffixes over a release cycle — that is the gate on the `CEDAR_ENFORCE_ACTIONS` flip, which is **not** part of this PR.

---

## Self-review

**Spec coverage (§4.4 D21–D22, §4.5 D23–D24, §4.6 D26–D29 + §6 "PR C — authz"):**

| Spec item | Task |
|---|---|
| D21 actor, catalog, system-key, cascade, name; handler mapping | 1 |
| D22 tenant bindings contribute no platform keys | 2 |
| D23 `system_actions.cedar` forbid | 3 |
| D24 `shadowEvaluate` gates the tenant-role stamp | 4 |
| D25 (not flipped) | stated in Global Constraints; drill step 6 |
| D26 `AuthzCacheInvalidator`, generation-keyed cache, one `INCR`, no `KEYS` | 5 |
| D27 pre/write/post on the role change; `withGeneration` for authz mutations | 5 (service), 6 (handler) |
| D28 tier guards read the DB; setup gate | 7 |
| D29 client PATCH guards | 7 |
| §6 `tier1_crud_test.go`, `tier1_test.go`, `engine_test.go`, `service_test.go`, handler tests, `cache_test.go`, role-change ordering, `setup/service_test.go` | 1, 2, 3, 4, 5, 6, 7 |
| §4.11 docs | 1, 2, 3, 5, 6, 7, 8 |

**Placeholder scan:** none. Two places ask the executor to *verify* something in the tree before writing (`MGet` on the Redis adapter in Task 5 Step 4; whether `policycoverage` needs a baseline entry in Task 3 Step 4) — both state what to do in either case, which is the opposite of a placeholder.

**Type consistency:** `validateCustomRolePermissions(ctx, tenantID, actor string, keys []string) ([]string, error)` is used with that signature in Task 1's tests and implementation and referenced nowhere else. `CreateRole(ctx, tenantID, actor, input)` and `UpdateRole(ctx, tenantID, roleUUID, actor, input)` are consistent in Tasks 1 and 5. `InvalidateUserPermissions(ctx, userUUID) error` is identical in the interface (Task 5), the implementation (Task 5) and the consumer (Task 6). `cacheKey(ctx, userUUID, tenantID)` gains its context in Task 5 and every caller is updated there. `withGeneration(ctx, scope, mutate)` matches the spec's shape.

**One risk worth naming:** Task 5 changes `flushCache` from `func(ctx)` to `func(ctx) error`, so every existing caller must decide what to do with the error. Task 5 Step 5 says to wrap them in `withGeneration`, which is where that decision lives — a reviewer should check that no caller silently discards it with `_ =`, because a discarded pre-invalidation error is the whole defect D27 closes.
