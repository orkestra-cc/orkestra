package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func adminFindItem(items []models.AdminNavItem, name string) *models.AdminNavItem {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
		if c := adminFindItem(items[i].Children, name); c != nil {
			return c
		}
	}
	return nil
}

func adminFindRealm(realms []models.AdminNavRealm, key string) *models.AdminNavRealm {
	for i := range realms {
		if realms[i].Key == key {
			return &realms[i]
		}
	}
	return nil
}

// adminFixture builds a small two-realm tree with items from two modules,
// one of which the test marks disabled. The internal-tier billing item
// must still appear in the admin response (the admin view does NOT filter
// by tier/role) but ModuleEnabled flips off.
func adminFixture() []module.NavItemSpec {
	return []module.NavItemSpec{
		{
			Realm: "platform", Section: "Administration", Tier: "internal",
			Name: "Modules", Path: "/admin/modules", MinRole: "administrator",
			ModuleName: "navigation", ItemKey: "navigation.modules",
		},
		{
			Realm: "business", Section: "Invoicing", Tier: "internal",
			Name: "Invoicing", Path: "/billing", MinRole: "manager",
			ModuleName: "billing", ItemKey: "billing.invoicing",
			Children: []module.NavItemSpec{
				{Name: "Invoices Issued", Path: "/billing/issued", ModuleName: "billing", ItemKey: "billing.invoicing.invoices-issued"},
				{Name: "Invoices Received", Path: "/billing/received", ModuleName: "billing", ItemKey: "billing.invoicing.invoices-received"},
			},
		},
	}
}

func TestAdminNavigationService_ReturnsUnfilteredTree(t *testing.T) {
	svc := NewAdminNavigationService(adminFixture(), &stubEnabled{disabled: map[string]bool{"billing": true}}, nil)
	resp, err := svc.GetAdminTree(context.Background())
	if err != nil {
		t.Fatalf("GetAdminTree: %v", err)
	}

	// Disabled-module items still appear (admin needs to see them to reorder).
	billingRealm := adminFindRealm(resp.Realms, "business")
	if billingRealm == nil {
		t.Fatal("business realm missing")
	}
	billing := adminFindItem(billingRealm.Sections[0].Items, "Invoicing")
	if billing == nil {
		t.Fatal("Invoicing item missing")
	}
	if billing.ModuleEnabled {
		t.Errorf("billing should be flagged disabled, got ModuleEnabled=true")
	}
	if len(billing.Children) != 2 {
		t.Errorf("expected 2 children, got %d", len(billing.Children))
	}
	// Internal-tier items still appear regardless of tenant kind.
	if billing.Tier != "internal" {
		t.Errorf("Tier metadata lost: %q", billing.Tier)
	}
	// MinRole is exposed so the role matrix can render.
	if billing.MinRole != "manager" {
		t.Errorf("MinRole metadata lost: %q", billing.MinRole)
	}
}

func TestAdminNavigationService_DeclaredOrderMatchesSiblingIndex(t *testing.T) {
	specs := []module.NavItemSpec{
		{Realm: "shared", Name: "A", Path: "/a", ModuleName: "m", ItemKey: "m.a"},
		{Realm: "shared", Name: "B", Path: "/b", ModuleName: "m", ItemKey: "m.b"},
		{Realm: "shared", Name: "C", Path: "/c", ModuleName: "m", ItemKey: "m.c"},
	}
	svc := NewAdminNavigationService(specs, nil, nil)
	resp, err := svc.GetAdminTree(context.Background())
	if err != nil {
		t.Fatalf("GetAdminTree: %v", err)
	}
	shared := adminFindRealm(resp.Realms, "shared")
	if shared == nil {
		t.Fatal("shared realm missing")
	}
	items := shared.Sections[0].Items
	for i, want := range []string{"A", "B", "C"} {
		if items[i].Name != want || items[i].DeclaredOrder != i {
			t.Errorf("items[%d] = (%q, declaredOrder=%d), want (%q, %d)", i, items[i].Name, items[i].DeclaredOrder, want, i)
		}
		if items[i].EffectiveOrder != items[i].DeclaredOrder {
			t.Errorf("phase-1: effectiveOrder must mirror declaredOrder; got %d vs %d", items[i].EffectiveOrder, items[i].DeclaredOrder)
		}
		if items[i].Overridden {
			t.Errorf("phase-1: Overridden must be false; item %q", items[i].Name)
		}
	}
}

// TestAdminNavigationService_VisibilityTruthTable exercises every gate the
// per-role × tenant-kind visibility map reflects: role floor, tenant tier,
// disabled module, config gate, and parent-collapse. The map must agree with
// what dynamicNavigationService.convert would render (shared evalSelfVisible).
func TestAdminNavigationService_VisibilityTruthTable(t *testing.T) {
	specs := []module.NavItemSpec{
		// administrator-only, untiered → operator/guest blocked by role.
		{Realm: "platform", Name: "Modules", Path: "/admin/modules", MinRole: "administrator", ModuleName: "navigation", ItemKey: "navigation.modules"},
		// internal-tier, no role floor → hidden for external tenants.
		{Realm: "business", Tier: "internal", Name: "Clients", Path: "/admin/clients", ModuleName: "tenant", ItemKey: "tenant.clients"},
		// disabled module → hidden for everyone, every kind.
		{Realm: "shared", Name: "Dead", Path: "/dead", ModuleName: "billing", ItemKey: "billing.dead"},
		// config-gated, gate OFF → hidden for everyone, every kind.
		{Realm: "platform", Name: "SOC2", Path: "/admin/compliance/soc2", MinRole: "administrator", RequiresConfig: "soc2_enabled", ModuleName: "compliance", ItemKey: "compliance.soc2"},
		// no-path group whose only child is administrator-only → parent collapses
		// for roles below administrator even though the group itself has no floor.
		{Realm: "shared", Name: "Group", ModuleName: "navigation", ItemKey: "navigation.group", Children: []module.NavItemSpec{
			{Name: "Child", Path: "/group/child", MinRole: "administrator", ModuleName: "navigation", ItemKey: "navigation.group.child"},
		}},
	}
	checker := &stubEnabledConfig{disabled: map[string]bool{"billing": true}, values: map[string]string{}}
	svc := NewAdminNavigationService(specs, checker, nil)
	resp, err := svc.GetAdminTree(context.Background())
	if err != nil {
		t.Fatalf("GetAdminTree: %v", err)
	}
	all := func() []models.AdminNavItem {
		var out []models.AdminNavItem
		for _, r := range resp.Realms {
			for _, s := range r.Sections {
				out = append(out, s.Items...)
			}
		}
		return out
	}()

	cell := func(name, role, kind string) models.NavVisibilityCell {
		it := adminFindItem(all, name)
		if it == nil {
			t.Fatalf("item %q missing", name)
		}
		return cellFor(it.Visibility[role], kind)
	}

	// Role floor: administrator sees Modules, operator does not.
	if c := cell("Modules", "administrator", "internal"); !c.Visible {
		t.Errorf("Modules/administrator should be visible, got %+v", c)
	}
	if c := cell("Modules", "operator", "internal"); c.Visible || c.Reason != ReasonRoleBelowMin {
		t.Errorf("Modules/operator want hidden role_below_min, got %+v", c)
	}

	// Tier: Clients visible internal, hidden external (tier_mismatch).
	if c := cell("Clients", "administrator", "internal"); !c.Visible {
		t.Errorf("Clients/internal should be visible, got %+v", c)
	}
	if c := cell("Clients", "administrator", "external"); c.Visible || c.Reason != ReasonTierMismatch {
		t.Errorf("Clients/external want hidden tier_mismatch, got %+v", c)
	}

	// Disabled module: hidden everywhere.
	if c := cell("Dead", "super_admin", "internal"); c.Visible || c.Reason != ReasonModuleDisabled {
		t.Errorf("Dead want hidden module_disabled, got %+v", c)
	}

	// Config gate OFF: hidden everywhere; the gate state is surfaced.
	if c := cell("SOC2", "administrator", "internal"); c.Visible || c.Reason != ReasonConfigOff {
		t.Errorf("SOC2 want hidden config_off, got %+v", c)
	}
	if soc2 := adminFindItem(all, "SOC2"); soc2.RequiresConfig != "soc2_enabled" || soc2.ConfigSatisfied {
		t.Errorf("SOC2 RequiresConfig/ConfigSatisfied not surfaced: %q / %v", soc2.RequiresConfig, soc2.ConfigSatisfied)
	}

	// Parent-collapse: the no-path Group is visible to administrator (its child
	// passes) but collapses for operator (child blocked by role).
	if c := cell("Group", "administrator", "internal"); !c.Visible {
		t.Errorf("Group/administrator should be visible (child passes), got %+v", c)
	}
	if c := cell("Group", "operator", "internal"); c.Visible || c.Reason != ReasonParentCollapsed {
		t.Errorf("Group/operator want hidden parent_collapsed, got %+v", c)
	}
}

func TestAdminNavigationService_EchoesRolesAndTenantKinds(t *testing.T) {
	svc := NewAdminNavigationService(nil, nil, nil)
	resp, err := svc.GetAdminTree(context.Background())
	if err != nil {
		t.Fatalf("GetAdminTree: %v", err)
	}
	wantRoles := []string{"super_admin", "administrator", "developer", "manager", "operator", "guest"}
	if len(resp.Roles) != len(wantRoles) {
		t.Fatalf("roles len = %d, want %d", len(resp.Roles), len(wantRoles))
	}
	for i, r := range wantRoles {
		if resp.Roles[i] != r {
			t.Errorf("roles[%d] = %q, want %q", i, resp.Roles[i], r)
		}
	}
	if len(resp.TenantKinds) != 2 || resp.TenantKinds[0] != "internal" || resp.TenantKinds[1] != "external" {
		t.Errorf("tenantKinds = %v, want [internal external]", resp.TenantKinds)
	}
}
