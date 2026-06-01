package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/core/authz/cedar"
	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// newTestService constructs a Service with only the fields the Cedar paths
// touch — no Mongo, no Redis. Tests that exercise GetEffectivePermissions
// or any binding/role lookup need a different harness; this is for the
// shadow + enforce code paths only.
func newTestService(t *testing.T, enforce []string) (*Service, *bytes.Buffer) {
	t.Helper()
	enforceSet := make(map[string]struct{}, len(enforce))
	for _, a := range enforce {
		enforceSet[a] = struct{}{}
	}
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng, err := cedar.New("development")
	if err != nil {
		t.Fatalf("cedar engine: %v", err)
	}
	return &Service{
		logger:          logger,
		cedarEngine:     eng,
		enforcedActions: enforceSet,
		userRoles: func(_ context.Context, _ string) (string, error) {
			// Default lookup for tests that don't override; see WithUserRole.
			return "", nil
		},
	}, logBuf
}

// withUserRole replaces the userRoles closure so a test can pretend a
// principal carries a specific system role (super_admin, administrator,
// etc.) without going through the user repository.
func (s *Service) withUserRole(role string) *Service {
	s.userRoles = func(_ context.Context, _ string) (string, error) {
		return role, nil
	}
	return s
}

func TestActionSuffix(t *testing.T) {
	cases := map[string]string{
		"system.users.admin": "admin",
		"foo.bar.read":       "read",
		"singleword":         "singleword",
		"trailing.dot.":      "trailing.dot.", // trailing dot — no suffix to extract
		"":                   "",
	}
	for in, want := range cases {
		if got := actionSuffix(in); got != want {
			t.Errorf("actionSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyCedarEnforcement_AgreeAllow(t *testing.T) {
	svc, _ := newTestService(t, []string{"system.users.admin"})
	got := svc.applyCedarEnforcement("system.users.admin", true, cedar.Decision{Allowed: true}, true)
	if !got {
		t.Errorf("expected allow when both agree, got false")
	}
}

func TestApplyCedarEnforcement_AgreeDeny(t *testing.T) {
	svc, _ := newTestService(t, []string{"system.users.admin"})
	got := svc.applyCedarEnforcement("system.users.admin", false, cedar.Decision{Allowed: false}, true)
	if got {
		t.Errorf("expected deny when both agree, got true")
	}
}

func TestApplyCedarEnforcement_CedarOverridesAllow(t *testing.T) {
	svc, logBuf := newTestService(t, []string{"system.users.admin"})
	got := svc.applyCedarEnforcement("system.users.admin", false, cedar.Decision{Allowed: true, MatchedPolicy: "platform.super_admin.wildcard"}, true)
	if !got {
		t.Errorf("expected Cedar's allow verdict to win, got false")
	}
	// Override should log a Warn so operators can audit.
	if !bytes.Contains(logBuf.Bytes(), []byte("enforce-mode override")) {
		t.Errorf("expected enforce-mode override log, got: %s", logBuf.String())
	}
}

func TestApplyCedarEnforcement_CedarOverridesDeny(t *testing.T) {
	svc, logBuf := newTestService(t, []string{"system.users.admin"})
	// Role table allowed (super_admin shortcut), Cedar denied via tier-aware
	// forbid — the case the flip is supposed to fix.
	got := svc.applyCedarEnforcement("system.users.admin", true, cedar.Decision{Allowed: false, MatchedPolicy: "tenant_scope.system_users_admin_internal_only"}, true)
	if got {
		t.Errorf("expected Cedar's deny verdict to win, got true (would let super_admin act on external tenant)")
	}
	if !bytes.Contains(logBuf.Bytes(), []byte("enforce-mode override")) {
		t.Errorf("expected enforce-mode override log, got: %s", logBuf.String())
	}
}

func TestApplyCedarEnforcement_FallbackOnCedarError(t *testing.T) {
	svc, logBuf := newTestService(t, []string{"system.users.admin"})
	// cedarOK=false simulates an evaluation panic or error. Fall back to
	// roleDecision rather than denying — a Cedar bug must not lock out
	// admins. Loud error log so operators see it.
	got := svc.applyCedarEnforcement("system.users.admin", true, cedar.Decision{Errors: []string{"boom"}}, false)
	if !got {
		t.Errorf("expected role-table verdict (true) on Cedar fallback, got false")
	}
	if !bytes.Contains(logBuf.Bytes(), []byte("enforce-mode evaluation failed")) {
		t.Errorf("expected enforce-mode failure log, got: %s", logBuf.String())
	}
}

func TestShadowEvaluate_SuperAdminInternalTenantSystemAdmin(t *testing.T) {
	// super_admin acting on an internal tenant with system.users.admin —
	// platform.cedar's wildcard permits, no tier-aware forbid fires.
	// Section B item #4 Commit C added an MFA forbid on admin-suffix
	// actions for privileged roles, so the ctx must stamp an MFA-
	// enrolled AMR for the permit path to survive. Cedar still agrees
	// with the role-table (which would also allow).
	svc, _ := newTestService(t, nil)
	svc.withUserRole("super_admin")
	ctx := ctxauth.WithTenantKind(context.Background(), "internal")
	ctx = middleware.WithAMR(ctx, []string{"pwd", "otp"})
	decision, ok := svc.shadowEvaluate(ctx, "user-1", "tenant-int", "system.users.admin", true)
	if !ok {
		t.Fatalf("Cedar evaluation should succeed, errors: %+v", decision.Errors)
	}
	if !decision.Allowed {
		t.Errorf("expected allow on internal tenant, got deny (matched=%q)", decision.MatchedPolicy)
	}
}

func TestShadowEvaluate_SuperAdminExternalTenantBlockedByTierForbid(t *testing.T) {
	// super_admin acting on an external tenant with system.users.admin —
	// the tier-aware forbid in tenant_scope.cedar fires. This is the
	// scenario the Section B item #1 enforce flip is designed to catch:
	// today the role-table allows (super_admin shortcut), Cedar denies.
	svc, _ := newTestService(t, []string{"system.users.admin"})
	svc.withUserRole("super_admin")
	ctx := ctxauth.WithTenantKind(context.Background(), "external")
	decision, ok := svc.shadowEvaluate(ctx, "user-1", "tenant-ext", "system.users.admin", true)
	if !ok {
		t.Fatalf("Cedar evaluation should succeed, errors: %+v", decision.Errors)
	}
	if decision.Allowed {
		t.Errorf("expected forbid on external tenant, got allow (matched=%q)", decision.MatchedPolicy)
	}
	// Now confirm the enforce path returns Cedar's deny verdict.
	got := svc.applyCedarEnforcement("system.users.admin", true, decision, ok)
	if got {
		t.Errorf("enforce path should return false (Cedar deny wins), got true")
	}
}

// TestShadowEvaluate_TenantStatusFlowsFromLookup proves the Commit A
// plumbing: the LookupTenantStatus callback really drives Cedar's
// resource.status and therefore tenant_scope.inactive_tenant_denies_mutations
// now fires in shadow mode. Before this commit the shadow evaluator
// hardcoded "active" which silenced that forbid across every request.
func TestShadowEvaluate_TenantStatusFlowsFromLookup(t *testing.T) {
	svc, _ := newTestService(t, nil)
	svc.withUserRole("administrator")
	svc.lookupTenantStatus = func(_ context.Context, _ string) (string, error) {
		return "suspended", nil
	}
	ctx := ctxauth.WithTenantKind(context.Background(), "internal")
	decision, ok := svc.shadowEvaluate(ctx, "user-1", "tenant-1", "tenant.update", true)
	if !ok {
		t.Fatalf("evaluation should succeed, errors: %+v", decision.Errors)
	}
	if decision.Allowed {
		t.Fatalf("suspended tenant must reject tenant.update: %+v", decision)
	}
	if decision.MatchedPolicy != "tenant_scope.inactive_tenant_denies_mutations" {
		t.Errorf("expected inactive-tenant forbid to match, got %q", decision.MatchedPolicy)
	}
}

// TestRiskLevelForScore_MatchesAuthLadder asserts the local bucket ladder
// matches the one in auth/services.RiskLevelForScore. The two packages
// are independent (authz sits below auth), so drift is only catchable
// by test. If a future commit tunes the thresholds in auth/services,
// update this table to match — or the Cedar principal.risk_level
// attribute will silently drift from the source of truth.
func TestRiskLevelForScore_MatchesAuthLadder(t *testing.T) {
	cases := map[float64]string{
		0.0:  "low",
		0.29: "low",
		0.3:  "medium",
		0.49: "medium",
		0.5:  "high",
		0.69: "high",
		0.7:  "critical",
		1.0:  "critical",
	}
	for in, want := range cases {
		if got := riskLevelForScore(in); got != want {
			t.Errorf("riskLevelForScore(%v) = %q, want %q", in, got, want)
		}
	}
}

// TestShadowEvaluate_RiskLookupStampsCedarPrincipal verifies the C2
// wiring: when SetSessionRiskLookup is called, shadowEvaluate reads the
// current sid from context and stamps the score on the Cedar principal.
// Cedar still allows this request (platform.administrator.all permits
// tenant.update on an internal tenant) — the test just proves the
// plumbing fires without breaking existing policies.
func TestShadowEvaluate_RiskLookupStampsCedarPrincipal(t *testing.T) {
	var gotSID string
	svc, _ := newTestService(t, nil)
	svc.withUserRole("administrator")
	svc.SetSessionRiskLookup(func(_ context.Context, sid string) (float64, error) {
		gotSID = sid
		return 0.42, nil
	})
	ctx := ctxauth.WithTenantKind(context.Background(), "internal")
	ctx = middleware.WithSessionID(ctx, "sess-1")
	decision, ok := svc.shadowEvaluate(ctx, "user-1", "tenant-1", "tenant.update", true)
	if !ok {
		t.Fatalf("evaluation should succeed: errors=%+v", decision.Errors)
	}
	if !decision.Allowed {
		t.Fatalf("administrator on active internal tenant should be allowed: %+v", decision)
	}
	if gotSID != "sess-1" {
		t.Errorf("lookup was not called with the expected sid, got %q", gotSID)
	}
}

// TestShadowEvaluate_RiskLookupErrorDoesNotAbort: a failing lookup must
// not block the authorization decision. The score defaults to 0 and
// the request is evaluated without risk attributes.
func TestShadowEvaluate_RiskLookupErrorDoesNotAbort(t *testing.T) {
	svc, _ := newTestService(t, nil)
	svc.withUserRole("administrator")
	svc.SetSessionRiskLookup(func(_ context.Context, _ string) (float64, error) {
		return 0, errors.New("boom")
	})
	ctx := ctxauth.WithTenantKind(context.Background(), "internal")
	ctx = middleware.WithSessionID(ctx, "sess-1")
	decision, ok := svc.shadowEvaluate(ctx, "user-1", "tenant-1", "tenant.update", true)
	if !ok {
		t.Fatalf("evaluation should succeed despite lookup error: errors=%+v", decision.Errors)
	}
	if !decision.Allowed {
		t.Fatalf("administrator should still pass: %+v", decision)
	}
}

// TestShadowEvaluate_TenantStatusFallsBackToActive: when the lookup is
// unwired (nil) or returns an empty status, the evaluator falls back to
// "active" so legacy code paths and tests keep their previous outcome.
func TestShadowEvaluate_TenantStatusFallsBackToActive(t *testing.T) {
	svc, _ := newTestService(t, nil)
	svc.withUserRole("administrator")
	// Lookup deliberately left nil.
	ctx := ctxauth.WithTenantKind(context.Background(), "internal")
	decision, ok := svc.shadowEvaluate(ctx, "user-1", "tenant-1", "tenant.update", true)
	if !ok {
		t.Fatalf("evaluation should succeed, errors: %+v", decision.Errors)
	}
	if !decision.Allowed {
		t.Fatalf("active fallback must permit administrator update: %+v", decision)
	}
}

func TestShadowEvaluate_NilEngineReturnsNotOK(t *testing.T) {
	svc := &Service{logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	_, ok := svc.shadowEvaluate(context.Background(), "u", "t", "any.perm", true)
	if ok {
		t.Errorf("nil cedarEngine should return ok=false")
	}
}

// ===== Binding cascade + system/tenant separation (commit C) =====

func TestValidateBindingScope_PlatformRoleNeedsGlobal(t *testing.T) {
	role := &models.Role{Name: "administrator", IsSystem: true}
	if err := validateBindingScope(role, ""); err != nil {
		t.Errorf("administrator with empty tenantID should pass, got %v", err)
	}
	if err := validateBindingScope(role, "tenant-1"); !errors.Is(err, ErrSystemRoleNotGrantableInTenant) {
		t.Errorf("administrator with tenantID should reject with ErrSystemRoleNotGrantableInTenant, got %v", err)
	}
}

func TestValidateBindingScope_OrgRoleNeedsTenant(t *testing.T) {
	role := &models.Role{Name: "org_owner", IsSystem: true}
	if err := validateBindingScope(role, "tenant-1"); err != nil {
		t.Errorf("org_owner with tenantID should pass, got %v", err)
	}
	if err := validateBindingScope(role, ""); !errors.Is(err, ErrTenantRoleNotGrantableGlobally) {
		t.Errorf("org_owner without tenantID should reject with ErrTenantRoleNotGrantableGlobally, got %v", err)
	}
}

func TestValidateBindingScope_CustomRoleNeedsTenant(t *testing.T) {
	// Custom roles (IsSystem=false, arbitrary name) follow the org-role
	// branch: they always need a tenant scope.
	role := &models.Role{Name: "team-lead", IsSystem: false}
	if err := validateBindingScope(role, "tenant-1"); err != nil {
		t.Errorf("custom role with tenantID should pass, got %v", err)
	}
	if err := validateBindingScope(role, ""); !errors.Is(err, ErrTenantRoleNotGrantableGlobally) {
		t.Errorf("custom role without tenantID should reject, got %v", err)
	}
}

func TestValidateBindingScope_AllPlatformRoleNames(t *testing.T) {
	// Smoke test that every platform role name is recognized — drift here
	// would let an org_admin grant administrator via the wrong scope.
	for _, name := range []string{"super_admin", "administrator", "developer", "manager", "operator", "guest"} {
		role := &models.Role{Name: name, IsSystem: true}
		if err := validateBindingScope(role, "tenant-1"); !errors.Is(err, ErrSystemRoleNotGrantableInTenant) {
			t.Errorf("platform role %q with tenantID should reject, got %v", name, err)
		}
	}
}

func TestValidateBindingCascade_GranterHoldsAllPerms(t *testing.T) {
	role := &models.Role{Permissions: []string{"tenant.read", "tenant.update"}}
	granter := []string{"tenant.read", "tenant.update", "tenant.delete"}
	if err := validateBindingCascade(role, granter); err != nil {
		t.Errorf("expected pass, granter is a strict superset, got %v", err)
	}
}

func TestValidateBindingCascade_GranterMissingOnePerm(t *testing.T) {
	role := &models.Role{Permissions: []string{"tenant.read", "tenant.delete"}}
	granter := []string{"tenant.read"} // missing tenant.delete
	if err := validateBindingCascade(role, granter); !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Errorf("expected ErrInsufficientPermissionsToGrant, got %v", err)
	}
}

func TestValidateBindingCascade_GranterWildcardCoversEverything(t *testing.T) {
	role := &models.Role{Permissions: []string{"any.weird.perm", "another.one"}}
	granter := []string{"*"}
	if err := validateBindingCascade(role, granter); err != nil {
		t.Errorf("super_admin granter (*) should grant anything, got %v", err)
	}
}

func TestValidateBindingCascade_GranterCannotGrantWildcard(t *testing.T) {
	// Even an administrator (allKeys, no wildcard) cannot promote someone
	// to super_admin (which has wildcard).
	role := &models.Role{Permissions: []string{"*"}}
	granter := []string{"tenant.read", "tenant.update", "tenant.delete"} // no "*"
	if err := validateBindingCascade(role, granter); !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Errorf("non-wildcard granter must not grant wildcard role, got %v", err)
	}
}

func TestValidateBindingCascade_EmptyRoleAlwaysPasses(t *testing.T) {
	role := &models.Role{Permissions: nil}
	if err := validateBindingCascade(role, nil); err != nil {
		t.Errorf("empty role on empty granter should pass (vacuously true), got %v", err)
	}
}

// TestOrgRoleFilters mirrors the predicates SeedSystemRoles uses to derive
// each org-role's permission set, against a representative slice of the
// real catalog. SeedSystemRoles itself needs Mongo so it isn't unit-
// testable; this test fences the predicate semantics so a future refactor
// of the filter chain doesn't silently widen org_member or shrink
// org_billing.
func TestOrgRoleFilters(t *testing.T) {
	allKeys := []string{
		"system.modules.admin",        // system → excluded from every org role
		"system.users.admin",          // system → excluded
		"tenant.read",                 // non-system, .read suffix
		"tenant.update",               // non-system, .update suffix
		"tenant.delete",               // non-system, .delete suffix
		"user.self",                   // non-system, .self suffix
		"agents.project.read",         // non-system, .read suffix
		"billing.invoice.create",      // non-system, billing module
		"billing.invoice.delete",      // non-system, billing module + .delete
		"payments.transaction.refund", // non-system, payments module
		"subscriptions.client.view",   // non-system, subscriptions module + .view
		"rag.query",                   // non-system, no covered suffix or module
	}
	systemSet := map[string]struct{}{
		"system.modules.admin": {},
		"system.users.admin":   {},
	}
	nonSystem := filter(allKeys, func(p string) bool {
		_, isSystem := systemSet[p]
		return !isSystem
	})
	if len(nonSystem) != len(allKeys)-2 {
		t.Fatalf("nonSystem should drop 2 system keys, got %+v", nonSystem)
	}

	// org_owner: every non-system permission.
	orgOwner := nonSystem
	for _, k := range []string{"tenant.delete", "billing.invoice.delete", "rag.query"} {
		if !contains(orgOwner, k) {
			t.Errorf("org_owner missing %q", k)
		}
	}
	for _, k := range []string{"system.modules.admin", "system.users.admin"} {
		if contains(orgOwner, k) {
			t.Errorf("org_owner must NOT contain system permission %q", k)
		}
	}

	// org_admin: non-system minus .delete.
	orgAdmin := filter(nonSystem, func(p string) bool {
		return !endsWith(p, ".delete")
	})
	if contains(orgAdmin, "tenant.delete") || contains(orgAdmin, "billing.invoice.delete") {
		t.Errorf("org_admin must NOT contain .delete suffixes: %+v", orgAdmin)
	}
	if !contains(orgAdmin, "tenant.update") {
		t.Errorf("org_admin should contain tenant.update")
	}

	// org_member: .read/.view/.self/.own only.
	orgMember := filter(nonSystem, func(p string) bool {
		return endsWith(p, ".read") || endsWith(p, ".view") ||
			endsWith(p, ".self") || endsWith(p, ".own")
	})
	for _, k := range []string{"tenant.read", "user.self", "subscriptions.client.view"} {
		if !contains(orgMember, k) {
			t.Errorf("org_member missing %q", k)
		}
	}
	for _, k := range []string{"tenant.update", "billing.invoice.create", "rag.query"} {
		if contains(orgMember, k) {
			t.Errorf("org_member must NOT contain %q", k)
		}
	}

	// org_billing: scoped to the three finance modules.
	orgBilling := filter(nonSystem, func(p string) bool {
		return startsWith(p, "billing.") || startsWith(p, "payments.") || startsWith(p, "subscriptions.")
	})
	for _, k := range []string{"billing.invoice.create", "payments.transaction.refund", "subscriptions.client.view"} {
		if !contains(orgBilling, k) {
			t.Errorf("org_billing missing %q", k)
		}
	}
	for _, k := range []string{"tenant.read", "rag.query", "user.self"} {
		if contains(orgBilling, k) {
			t.Errorf("org_billing must NOT contain non-finance %q", k)
		}
	}

	// org_viewer: read+view only.
	orgViewer := filter(nonSystem, func(p string) bool {
		return endsWith(p, ".read") || endsWith(p, ".view")
	})
	if !contains(orgViewer, "tenant.read") || !contains(orgViewer, "subscriptions.client.view") {
		t.Errorf("org_viewer missing read/view entries: %+v", orgViewer)
	}
	if contains(orgViewer, "user.self") {
		t.Errorf("org_viewer must NOT contain .self: %+v", orgViewer)
	}
}

// helpers — local test-only predicates so the test stays import-light.
func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
func startsWith(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func TestNew_EnforceActionsTrimAndDrop(t *testing.T) {
	// New() trims whitespace and drops empty fragments so the env-var path
	// (CEDAR_ENFORCE_ACTIONS="a, ,b ,") doesn't introduce phantom entries.
	// Repo / Redis are nil here — New() stores them but doesn't dereference,
	// and the enforce-set construction runs before any of them is touched.
	svc := New(Config{
		EnforceActions: []string{"  system.users.admin  ", "", "system.tenants.admin", "   "},
		Environment:    "development",
	})
	if _, ok := svc.enforcedActions["system.users.admin"]; !ok {
		t.Errorf("expected trimmed entry to be present, got: %+v", svc.enforcedActions)
	}
	if _, ok := svc.enforcedActions["system.tenants.admin"]; !ok {
		t.Errorf("expected second entry, got: %+v", svc.enforcedActions)
	}
	if _, ok := svc.enforcedActions[""]; ok {
		t.Errorf("empty entry should be dropped, got: %+v", svc.enforcedActions)
	}
	if len(svc.enforcedActions) != 2 {
		t.Errorf("expected exactly 2 entries, got %d: %+v", len(svc.enforcedActions), svc.enforcedActions)
	}
}
