package cedar

import (
	"testing"
	"time"
)

// newTestEngine builds an engine pinned to a specific environment. Tests
// that don't care about env can pass "development" which unlocks developer
// wildcards.
func newTestEngine(t *testing.T, env string) *Engine {
	t.Helper()
	e, err := New(env)
	if err != nil {
		t.Fatalf("New(%q): %v", env, err)
	}
	if e.PolicyCount() == 0 {
		t.Fatal("engine loaded zero policies — policies/ directory empty?")
	}
	return e
}

func TestPolicyLoadingNonEmpty(t *testing.T) {
	e := newTestEngine(t, "development")
	// platform.cedar has 4 policies, tenant_scope.cedar has 5,
	// tenant_roles.cedar has 9 (4 legacy + 5 org_*), capability_grants.cedar
	// has 1, abac.cedar has 2. Sanity check the count is in the expected
	// range so silent drop-outs don't go unnoticed.
	if got := e.PolicyCount(); got < 18 {
		t.Fatalf("policy count too low: got %d, want >= 18", got)
	}
}

func TestSuperAdminWildcardOnInternalTenant(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "super_admin"},
		"tenant.read",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if !d.Allowed {
		t.Fatalf("super_admin should be allowed: %+v", d)
	}
	if d.MatchedPolicy != "platform.super_admin.wildcard" {
		t.Fatalf("matched wrong policy: %s", d.MatchedPolicy)
	}
}

func TestAdministratorAllowedByDefault(t *testing.T) {
	e := newTestEngine(t, "production")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "administrator"},
		"authz.role.create",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if !d.Allowed {
		t.Fatalf("administrator should be allowed: %+v", d)
	}
}

func TestDeveloperProdReadOnly(t *testing.T) {
	e := newTestEngine(t, "production")

	readD := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "developer"},
		"tenant.read",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if !readD.Allowed {
		t.Fatalf("developer should read in prod: %+v", readD)
	}

	mutD := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "developer"},
		"tenant.delete",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if mutD.Allowed {
		t.Fatalf("developer must NOT mutate in prod: %+v", mutD)
	}
}

func TestDeveloperNonProdWildcard(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "developer"},
		"tenant.delete",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if !d.Allowed {
		t.Fatalf("developer should have wildcard in dev: %+v", d)
	}
}

// TestSystemActionForbiddenOnExternalTenant proves the ADR-0001 tier guard:
// even a super_admin cannot run system.modules.admin against an external
// (client) tenant. forbid beats permit in Cedar.
func TestSystemActionForbiddenOnExternalTenant(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "super_admin"},
		"system.modules.admin",
		Resource{TenantUUID: "t-external", TenantKind: "external", TenantStatus: "active"},
	)
	if d.Allowed {
		t.Fatalf("super_admin must be forbidden on external tenant: %+v", d)
	}
}

// TestSystemActionAllowedOnInternalTenant mirror of the above — system
// actions on internal tenants remain permitted for an MFA-enrolled
// super_admin. Pre-Commit-C of Section B item #4 the fixture didn't
// set MFAEnrolled; after the MFA ABAC forbid landed, privileged admin-
// suffix actions require a second factor. Enrolling MFA here preserves
// the original intent (permit path works on internal tenants).
func TestSystemActionAllowedOnInternalTenant(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "super_admin", MFAEnrolled: true, AMR: []string{"pwd", "otp"}},
		"system.modules.admin",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if !d.Allowed {
		t.Fatalf("super_admin on internal tenant should be allowed: %+v", d)
	}
}

// TestInactiveTenantRejectsMutations: suspended/archived/purged tenants
// accept reads but deny updates, even for admins.
func TestInactiveTenantRejectsMutations(t *testing.T) {
	e := newTestEngine(t, "development")
	for _, status := range []string{"suspended", "archived", "purged"} {
		t.Run(status+"_blocks_update", func(t *testing.T) {
			d := e.IsAuthorized(
				Principal{UserUUID: "u1", SystemRole: "administrator"},
				"tenant.update",
				Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: status},
			)
			if d.Allowed {
				t.Fatalf("administrator must not mutate %s tenant: %+v", status, d)
			}
		})
		t.Run(status+"_permits_read", func(t *testing.T) {
			d := e.IsAuthorized(
				Principal{UserUUID: "u1", SystemRole: "administrator"},
				"tenant.read",
				Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: status},
			)
			if !d.Allowed {
				t.Fatalf("administrator must be able to read %s tenant: %+v", status, d)
			}
		})
	}
}

// TestTenantRoleManagerReadWrite: a tenant member with the "manager" role
// can read/update but not delete or admin.
func TestTenantRoleManagerReadWrite(t *testing.T) {
	e := newTestEngine(t, "development")
	p := Principal{UserUUID: "u1", SystemRole: "operator", TenantRoles: []string{"manager"}}
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	if d := e.IsAuthorized(p, "tenant.update", r); !d.Allowed {
		t.Fatalf("manager should update: %+v", d)
	}
	if d := e.IsAuthorized(p, "tenant.delete", r); d.Allowed {
		t.Fatalf("manager must not delete: %+v", d)
	}
	if d := e.IsAuthorized(p, "system.modules.admin", r); d.Allowed {
		t.Fatalf("manager must not admin: %+v", d)
	}
}

// TestTenantRoleOperatorReadOnly: operator tenant-role = read + self only.
func TestTenantRoleOperatorReadOnly(t *testing.T) {
	e := newTestEngine(t, "development")
	p := Principal{UserUUID: "u1", SystemRole: "operator", TenantRoles: []string{"operator"}}
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	if d := e.IsAuthorized(p, "tenant.read", r); !d.Allowed {
		t.Fatalf("operator should read: %+v", d)
	}
	if d := e.IsAuthorized(p, "tenant.update", r); d.Allowed {
		t.Fatalf("operator must not update: %+v", d)
	}
}

// TestUnknownPrincipalDenied: no system role, no tenant roles, no grant.
func TestUnknownPrincipalDenied(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1"},
		"tenant.read",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if d.Allowed {
		t.Fatalf("unroled principal must be denied: %+v", d)
	}
}

// TestCapabilityGrantInert: when RequiredCapability is empty the
// defense-in-depth forbid rule must not fire — a super_admin still gets
// the wildcard allow even though the tenant has no capabilities wired.
func TestCapabilityGrantInert(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.Evaluate(Request{
		Principal: Principal{UserUUID: "u1", SystemRole: "super_admin"},
		Action:    "rag.query",
		Resource:  Resource{TenantUUID: "t1", TenantKind: "external", TenantStatus: "active"},
	})
	if !d.Allowed {
		t.Fatalf("capability forbid must be inert without RequiredCapability: %+v", d)
	}
}

// TestCapabilityGrantForbidsUnentitled: when RequiredCapability is set
// and the principal lacks that capability, the forbid rule beats every
// permit — even super_admin's wildcard.
func TestCapabilityGrantForbidsUnentitled(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.Evaluate(Request{
		Principal:          Principal{UserUUID: "u1", SystemRole: "super_admin"},
		Action:             "rag.query",
		Resource:           Resource{TenantUUID: "t1", TenantKind: "external", TenantStatus: "active"},
		RequiredCapability: "rag.query",
	})
	if d.Allowed {
		t.Fatalf("unentitled capability must be forbidden: %+v", d)
	}
}

// TestCapabilityGrantPermitsEntitled: same request as above but with the
// capability present on the principal — the forbid rule stays silent and
// an existing permit wins.
func TestCapabilityGrantPermitsEntitled(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.Evaluate(Request{
		Principal: Principal{
			UserUUID:     "u1",
			SystemRole:   "super_admin",
			Capabilities: []string{"rag.query", "agents.run"},
		},
		Action:             "rag.query",
		Resource:           Resource{TenantUUID: "t1", TenantKind: "external", TenantStatus: "active"},
		RequiredCapability: "rag.query",
	})
	if !d.Allowed {
		t.Fatalf("entitled capability must be allowed: %+v", d)
	}
}

// ----- Org-role permits (Section B item #3, 2026-04-24) -----

// TestOrgOwnerAllInTenant: org_owner permits any action on the tenant —
// the org-side mirror of administrator-as-tenant-role. No system role
// needed; the binding alone is sufficient.
func TestOrgOwnerAllInTenant(t *testing.T) {
	e := newTestEngine(t, "development")
	p := Principal{UserUUID: "u1", TenantRoles: []string{"org_owner"}}
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	for _, action := range []string{"tenant.read", "tenant.update", "tenant.delete", "billing.invoice.create"} {
		if d := e.IsAuthorized(p, action, r); !d.Allowed {
			t.Errorf("org_owner should be allowed for %s: %+v", action, d)
		}
	}
}

// TestOrgOwnerStillBlockedByTierForbid: even an org_owner cannot trigger
// system.* actions against an external tenant — tier-aware forbid wins.
func TestOrgOwnerStillBlockedByTierForbid(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1", TenantRoles: []string{"org_owner"}},
		"system.modules.admin",
		Resource{TenantUUID: "t-ext", TenantKind: "external", TenantStatus: "active"},
	)
	if d.Allowed {
		t.Fatalf("tier-aware forbid must beat org_owner permit on external tenant: %+v", d)
	}
}

// TestOrgAdminBlocksDelete: org_admin permits read/create/update but
// never .delete actions.
func TestOrgAdminBlocksDelete(t *testing.T) {
	e := newTestEngine(t, "development")
	p := Principal{UserUUID: "u1", TenantRoles: []string{"org_admin"}}
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	if d := e.IsAuthorized(p, "tenant.update", r); !d.Allowed {
		t.Errorf("org_admin should update: %+v", d)
	}
	if d := e.IsAuthorized(p, "billing.invoice.create", r); !d.Allowed {
		t.Errorf("org_admin should create: %+v", d)
	}
	if d := e.IsAuthorized(p, "billing.invoice.delete", r); d.Allowed {
		t.Errorf("org_admin must NOT delete: %+v", d)
	}
}

// TestOrgMemberReadAndSelf: org_member covers read/view/self/own suffixes
// and rejects everything else.
func TestOrgMemberReadAndSelf(t *testing.T) {
	e := newTestEngine(t, "development")
	p := Principal{UserUUID: "u1", TenantRoles: []string{"org_member"}}
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	for _, action := range []string{"tenant.read", "user.self", "billing.invoice.read"} {
		if d := e.IsAuthorized(p, action, r); !d.Allowed {
			t.Errorf("org_member should be allowed for %s: %+v", action, d)
		}
	}
	for _, action := range []string{"tenant.update", "billing.invoice.create", "billing.invoice.delete"} {
		if d := e.IsAuthorized(p, action, r); d.Allowed {
			t.Errorf("org_member must NOT be allowed for %s: %+v", action, d)
		}
	}
}

// TestOrgBillingScopedToFinanceModules: org_billing permits any action
// under billing.*, payments.*, subscriptions.* (via the new action_module
// context field) and denies actions under any other module.
func TestOrgBillingScopedToFinanceModules(t *testing.T) {
	e := newTestEngine(t, "development")
	p := Principal{UserUUID: "u1", TenantRoles: []string{"org_billing"}}
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	for _, action := range []string{
		"billing.invoice.create", "billing.invoice.delete", "billing.invoice.send",
		"payments.transaction.refund", "payments.method.manage",
		"subscriptions.client.manage", "subscriptions.subscription.view",
	} {
		if d := e.IsAuthorized(p, action, r); !d.Allowed {
			t.Errorf("org_billing should be allowed for finance action %s: %+v", action, d)
		}
	}
	for _, action := range []string{"tenant.read", "agents.query", "rag.query", "user.self"} {
		if d := e.IsAuthorized(p, action, r); d.Allowed {
			t.Errorf("org_billing must NOT touch non-finance action %s: %+v", action, d)
		}
	}
}

// TestOrgViewerReadOnly: org_viewer covers read+view suffixes only.
func TestOrgViewerReadOnly(t *testing.T) {
	e := newTestEngine(t, "development")
	p := Principal{UserUUID: "u1", TenantRoles: []string{"org_viewer"}}
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	if d := e.IsAuthorized(p, "tenant.read", r); !d.Allowed {
		t.Errorf("org_viewer should read: %+v", d)
	}
	if d := e.IsAuthorized(p, "billing.invoice.read", r); !d.Allowed {
		t.Errorf("org_viewer should view billing read action: %+v", d)
	}
	if d := e.IsAuthorized(p, "tenant.update", r); d.Allowed {
		t.Errorf("org_viewer must NOT update: %+v", d)
	}
}

// ----- ABAC attribute plumbing (Section B item #4, Commit A) -----

// TestPrincipalMFAAttributesDoNotBreakExistingPolicies: adding the
// mfa_enrolled + amr attributes must not change the outcome of any
// existing policy. The two signals exist only for new ABAC rules; legacy
// permits and forbids ignore them. Verifies by evaluating a representative
// slice with and without the attrs populated.
func TestPrincipalMFAAttributesDoNotBreakExistingPolicies(t *testing.T) {
	e := newTestEngine(t, "development")
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	cases := []struct {
		name  string
		p     Principal
		act   string
		allow bool
	}{
		{"super_admin_wildcard", Principal{UserUUID: "u", SystemRole: "super_admin"}, "tenant.read", true},
		{"org_owner_tenant_update", Principal{UserUUID: "u", TenantRoles: []string{"org_owner"}}, "tenant.update", true},
		{"org_viewer_denies_write", Principal{UserUUID: "u", TenantRoles: []string{"org_viewer"}}, "tenant.update", false},
		{"unknown_principal_denied", Principal{UserUUID: "u"}, "tenant.read", false},
	}
	for _, tc := range cases {
		t.Run(tc.name+"_no_mfa", func(t *testing.T) {
			d := e.IsAuthorized(tc.p, tc.act, r)
			if d.Allowed != tc.allow {
				t.Fatalf("without MFA attrs: want allow=%v, got %+v", tc.allow, d)
			}
		})
		t.Run(tc.name+"_with_mfa", func(t *testing.T) {
			p := tc.p
			p.MFAEnrolled = true
			p.AMR = []string{"pwd", "otp"}
			d := e.IsAuthorized(p, tc.act, r)
			if d.Allowed != tc.allow {
				t.Fatalf("with MFA attrs: want allow=%v, got %+v", tc.allow, d)
			}
			if len(d.Errors) > 0 {
				t.Fatalf("MFA attrs must not produce Cedar errors: %+v", d.Errors)
			}
		})
	}
}

// TestInactiveTenantStatusFlowsThrough: sanity checks that the resource's
// TenantStatus really drives tenant_scope.cedar now that the shadow
// evaluator threads the real value instead of hardcoding "active". Cedar
// engine tests have always been able to set status directly; this is a
// redundant assertion in the engine layer. The service-layer test in
// services/service_test.go covers the lookup wiring.
func TestInactiveTenantStatusFlowsThrough(t *testing.T) {
	e := newTestEngine(t, "development")
	suspended := e.IsAuthorized(
		Principal{UserUUID: "u", SystemRole: "administrator"},
		"tenant.update",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "suspended"},
	)
	if suspended.Allowed {
		t.Errorf("suspended tenant must reject update: %+v", suspended)
	}
	active := e.IsAuthorized(
		Principal{UserUUID: "u", SystemRole: "administrator"},
		"tenant.update",
		Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"},
	)
	if !active.Allowed {
		t.Errorf("active tenant must permit update: %+v", active)
	}
}

// ----- ABAC context plumbing (Section B item #4, Commit B) -----

func TestClassifyIP(t *testing.T) {
	cases := map[string]string{
		"":                "unknown",
		"not-an-ip":       "unknown",
		"127.0.0.1":       "loopback",
		"::1":             "loopback",
		"10.0.0.1":        "private",
		"192.168.1.1":     "private",
		"172.16.4.5":      "private",
		"169.254.1.1":     "private", // link-local
		"fd00::1":         "private",
		"8.8.8.8":         "public",
		"1.1.1.1":         "public",
		"203.0.113.5":     "public",
		"203.0.113.5:443": "public", // host:port strips correctly
		"[::1]:8080":      "loopback",
	}
	for in, want := range cases {
		if got := classifyIP(in); got != want {
			t.Errorf("classifyIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWeekdayShort(t *testing.T) {
	cases := map[time.Weekday]string{
		time.Sunday:    "sun",
		time.Monday:    "mon",
		time.Tuesday:   "tue",
		time.Wednesday: "wed",
		time.Thursday:  "thu",
		time.Friday:    "fri",
		time.Saturday:  "sat",
	}
	for d, want := range cases {
		if got := weekdayShort(d); got != want {
			t.Errorf("weekdayShort(%v) = %q, want %q", d, got, want)
		}
	}
}

// TestContextAttributesDoNotBreakExistingPolicies: stamping hour_utc,
// weekday, ip_bucket on every request must not change the outcome of any
// existing policy. Exercises the same representative slice as the MFA
// smoke test (Commit A) through a deterministic clock and a set of IPs
// covering each bucket.
func TestContextAttributesDoNotBreakExistingPolicies(t *testing.T) {
	e := newTestEngine(t, "development")
	// Pin the clock to a Tuesday 10:00 UTC so the output is reproducible.
	e.clock = func() time.Time { return time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC) }
	r := Resource{TenantUUID: "t1", TenantKind: "internal", TenantStatus: "active"}

	cases := []struct {
		name  string
		p     Principal
		act   string
		ip    string
		allow bool
	}{
		{"super_admin_loopback", Principal{UserUUID: "u", SystemRole: "super_admin"}, "tenant.read", "127.0.0.1", true},
		{"org_owner_private", Principal{UserUUID: "u", TenantRoles: []string{"org_owner"}}, "tenant.update", "10.0.0.1", true},
		{"org_viewer_public_denies_write", Principal{UserUUID: "u", TenantRoles: []string{"org_viewer"}}, "tenant.update", "8.8.8.8", false},
		{"unknown_principal_denied_regardless_of_ip", Principal{UserUUID: "u"}, "tenant.read", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := e.Evaluate(Request{
				Principal: tc.p,
				Action:    tc.act,
				Resource:  r,
				ClientIP:  tc.ip,
			})
			if d.Allowed != tc.allow {
				t.Fatalf("want allow=%v, got %+v", tc.allow, d)
			}
			if len(d.Errors) > 0 {
				t.Fatalf("context attributes must not produce Cedar errors: %+v", d.Errors)
			}
		})
	}
}

// ----- ABAC policy behavior (Section B item #4, Commit C) -----

// TestABACRequireMFAForAdminSuffix_BlocksPrivilegedWithoutMFA: a
// super_admin hitting a .admin action without a second factor is
// denied by abac.require_mfa_for_admin_suffix. The forbid wins over
// platform.super_admin.wildcard.
func TestABACRequireMFAForAdminSuffix_BlocksPrivilegedWithoutMFA(t *testing.T) {
	e := newTestEngine(t, "development")
	for _, role := range []string{"super_admin", "administrator"} {
		t.Run(role, func(t *testing.T) {
			d := e.IsAuthorized(
				Principal{UserUUID: "u", SystemRole: role, MFAEnrolled: false},
				"system.users.admin",
				Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
			)
			if d.Allowed {
				t.Fatalf("%s without MFA must be forbidden on admin action: %+v", role, d)
			}
			if d.MatchedPolicy != "abac.require_mfa_for_admin_suffix" {
				t.Errorf("expected abac MFA forbid, got %q", d.MatchedPolicy)
			}
		})
	}
}

// TestABACRequireMFAForAdminSuffix_PermitsWithMFA: same principal and
// action as above, but MFAEnrolled=true. The forbid stays silent and
// the wildcard permits. Captures the intended "enrolled session gets
// through" contract.
func TestABACRequireMFAForAdminSuffix_PermitsWithMFA(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u", SystemRole: "super_admin", MFAEnrolled: true, AMR: []string{"pwd", "otp"}},
		"system.users.admin",
		Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
	)
	if !d.Allowed {
		t.Fatalf("super_admin with MFA must be allowed: %+v", d)
	}
}

// TestABACRequireMFAForAdminSuffix_IgnoresNonAdminSuffix: the forbid is
// scoped to action_suffix == "admin" — a super_admin without MFA on a
// read action is not impacted.
func TestABACRequireMFAForAdminSuffix_IgnoresNonAdminSuffix(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u", SystemRole: "super_admin", MFAEnrolled: false},
		"tenant.read",
		Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
	)
	if !d.Allowed {
		t.Fatalf("non-admin suffix must not trigger MFA forbid: %+v", d)
	}
}

// TestABACRequireMFAForAdminSuffix_IgnoresNonPrivilegedRole: operator
// (not administrator/super_admin) on a .admin action is not blocked by
// this specific forbid — the existing RBAC gate is what denies them.
// This test documents the scope; a regression that widened the rule to
// all principals would break it.
func TestABACRequireMFAForAdminSuffix_IgnoresNonPrivilegedRole(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u", SystemRole: "operator", MFAEnrolled: false},
		"system.users.admin",
		Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
	)
	// operator doesn't have system.users.admin via any permit, so the
	// result is deny — but it must NOT be via abac.require_mfa_for_admin_suffix.
	if d.Allowed {
		t.Fatalf("operator should not reach system.users.admin: %+v", d)
	}
	if d.MatchedPolicy == "abac.require_mfa_for_admin_suffix" {
		t.Errorf("non-privileged role must not trip the MFA forbid, got matched=%q", d.MatchedPolicy)
	}
}

// TestABACDenySystemFromPublicIP_ProdBlocks: the four system.* actions
// originating from a public IP in production are forbidden. Checks each
// action in turn so a drift between the list in the policy and the one
// in tenant_scope.cedar shows up as a specific failure.
func TestABACDenySystemFromPublicIP_ProdBlocks(t *testing.T) {
	e := newTestEngine(t, "production")
	for _, act := range []string{
		"system.modules.admin",
		"system.tenants.admin",
		"system.users.admin",
		"system.users.mfa_reset",
	} {
		t.Run(act, func(t *testing.T) {
			d := e.Evaluate(Request{
				Principal: Principal{UserUUID: "u", SystemRole: "super_admin", MFAEnrolled: true},
				Action:    act,
				Resource:  Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
				ClientIP:  "8.8.8.8",
			})
			if d.Allowed {
				t.Fatalf("public-IP %s in prod must be forbidden: %+v", act, d)
			}
			if d.MatchedPolicy != "abac.deny_system_actions_from_public_ip_in_prod" {
				t.Errorf("expected abac IP forbid, got %q", d.MatchedPolicy)
			}
		})
	}
}

// TestABACDenySystemFromPublicIP_NonProdPermits: outside production the
// rule is inert. A super_admin on 8.8.8.8 calling system.modules.admin
// in development is allowed (subject to other policies).
func TestABACDenySystemFromPublicIP_NonProdPermits(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.Evaluate(Request{
		Principal: Principal{UserUUID: "u", SystemRole: "super_admin", MFAEnrolled: true},
		Action:    "system.modules.admin",
		Resource:  Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
		ClientIP:  "8.8.8.8",
	})
	if !d.Allowed {
		t.Fatalf("public-IP in dev must not trip the prod-only forbid: %+v", d)
	}
}

// TestABACDenySystemFromPublicIP_PrivateIPPermits: an operator coming
// in over the office VPN in production is permitted; the forbid is
// public-IP-only by design so internal mesh traffic isn't blocked.
func TestABACDenySystemFromPublicIP_PrivateIPPermits(t *testing.T) {
	e := newTestEngine(t, "production")
	d := e.Evaluate(Request{
		Principal: Principal{UserUUID: "u", SystemRole: "super_admin", MFAEnrolled: true},
		Action:    "system.modules.admin",
		Resource:  Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
		ClientIP:  "10.0.0.5",
	})
	if !d.Allowed {
		t.Fatalf("private-IP in prod must be allowed: %+v", d)
	}
}

// TestABACDenySystemFromPublicIP_UnknownBucketPermits: no IP on the
// request (background jobs, AI sidecar S2S) buckets as "unknown" and
// must not be blocked — only explicit "public" trips the forbid.
func TestABACDenySystemFromPublicIP_UnknownBucketPermits(t *testing.T) {
	e := newTestEngine(t, "production")
	d := e.Evaluate(Request{
		Principal: Principal{UserUUID: "u", SystemRole: "super_admin", MFAEnrolled: true},
		Action:    "system.modules.admin",
		Resource:  Resource{TenantUUID: "t", TenantKind: "internal", TenantStatus: "active"},
		ClientIP:  "", // unknown bucket
	})
	if !d.Allowed {
		t.Fatalf("unknown-bucket in prod must be allowed: %+v", d)
	}
}

// TestCapabilityGrantForbidsWhenWrongCapabilityHeld: holding a different
// capability than the one the request requires still trips forbid.
func TestCapabilityGrantForbidsWhenWrongCapabilityHeld(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.Evaluate(Request{
		Principal: Principal{
			UserUUID:     "u1",
			SystemRole:   "administrator",
			Capabilities: []string{"agents.run"},
		},
		Action:             "rag.query",
		Resource:           Resource{TenantUUID: "t1", TenantKind: "external", TenantStatus: "active"},
		RequiredCapability: "rag.query",
	})
	if d.Allowed {
		t.Fatalf("mismatched capability must be forbidden: %+v", d)
	}
}
