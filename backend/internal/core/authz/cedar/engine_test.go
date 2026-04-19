package cedar

import "testing"

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
	// tenant_roles.cedar has 4, capability_grants.cedar has 1.
	// Sanity check the count is in the expected range so silent
	// drop-outs don't go unnoticed.
	if got := e.PolicyCount(); got < 11 {
		t.Fatalf("policy count too low: got %d, want >= 11", got)
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
// actions on internal tenants remain permitted.
func TestSystemActionAllowedOnInternalTenant(t *testing.T) {
	e := newTestEngine(t, "development")
	d := e.IsAuthorized(
		Principal{UserUUID: "u1", SystemRole: "super_admin"},
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
