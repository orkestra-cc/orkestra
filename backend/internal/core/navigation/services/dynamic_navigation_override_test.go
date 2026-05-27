package services

import (
	"context"
	"testing"

	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/navigation/models"
)

// TestDynamicNavigation_AppliesRealmsOverride checks that the public
// sidebar honours an override keyed by RealmsParentKey, swapping the
// canonical realm order.
func TestDynamicNavigation_AppliesRealmsOverride(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: "personal", Section: "Workspace", Name: "Home", Path: "/", ModuleName: "core", ItemKey: "core.home", Active: true},
		{Realm: "platform", Section: "Administration", Name: "Modules", Path: "/admin/modules", ModuleName: "navigation", ItemKey: "navigation.modules", Active: true},
		{Realm: "business", Section: "Invoicing", Name: "Invoicing", Path: "/billing", ModuleName: "billing", ItemKey: "billing.invoicing", Active: true},
	}
	repo := &fakeOverrideRepo{docs: []models.NavOverride{
		{ParentKey: RealmsParentKey, OrderedChildren: []string{"business", "platform", "personal"}},
	}}
	overrideSvc := NewOverrideService(repo, nil)
	svc := NewDynamicNavigationService(items, nil, overrideSvc)

	resp, err := svc.GetNavigationForUser(context.Background(), "administrator")
	if err != nil {
		t.Fatalf("GetNavigationForUser: %v", err)
	}
	wantKeys := []string{"business", "platform", "personal"}
	if len(resp.Realms) != len(wantKeys) {
		t.Fatalf("got %d realms, want %d", len(resp.Realms), len(wantKeys))
	}
	for i, key := range wantKeys {
		if resp.Realms[i].Key != key {
			t.Errorf("realm[%d] = %q, want %q", i, resp.Realms[i].Key, key)
		}
	}
}

// TestAdminNavigation_RealmsOverriddenFlag verifies the admin response
// flips RealmsOverridden when persisted order diverges from canonical,
// and exposes the constant RealmsParentKey for the frontend.
func TestAdminNavigation_RealmsOverriddenFlag(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: "personal", Section: "W", Name: "A", Path: "/a", ModuleName: "m", ItemKey: "m.a"},
		{Realm: "platform", Section: "W", Name: "B", Path: "/b", ModuleName: "m", ItemKey: "m.b"},
	}
	// First: no override → canonical → RealmsOverridden=false.
	noOverride := &fakeOverrideRepo{}
	svc1 := NewAdminNavigationService(items, nil, NewOverrideService(noOverride, nil))
	resp1, _ := svc1.GetAdminTree(context.Background())
	if resp1.RealmsOverridden {
		t.Errorf("canonical order should not flag RealmsOverridden")
	}
	if resp1.RealmsParentKey != RealmsParentKey {
		t.Errorf("RealmsParentKey echoed = %q, want %q", resp1.RealmsParentKey, RealmsParentKey)
	}

	// Then: override that swaps order → flag flips.
	withOverride := &fakeOverrideRepo{docs: []models.NavOverride{
		{ParentKey: RealmsParentKey, OrderedChildren: []string{"platform", "personal"}},
	}}
	svc2 := NewAdminNavigationService(items, nil, NewOverrideService(withOverride, nil))
	resp2, _ := svc2.GetAdminTree(context.Background())
	if !resp2.RealmsOverridden {
		t.Errorf("swapped order should flag RealmsOverridden")
	}
	if resp2.Realms[0].Key != "platform" || resp2.Realms[1].Key != "personal" {
		t.Errorf("realms not reordered: %v", []string{resp2.Realms[0].Key, resp2.Realms[1].Key})
	}
}

// TestDynamicNavigation_AppliesSectionRootOverride checks that the
// public sidebar honours a top-level override on (realm, section).
func TestDynamicNavigation_AppliesSectionRootOverride(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: "platform", Section: "Administration", Name: "Modules", Path: "/admin/modules", ModuleName: "navigation", ItemKey: "navigation.modules", Active: true},
		{Realm: "platform", Section: "Administration", Name: "Log levels", Path: "/admin/observability/log-levels", ModuleName: "logging", ItemKey: "logging.log-levels", Active: true},
		{Realm: "platform", Section: "Administration", Name: "Navigation", Path: "/admin/modules/navigation", ModuleName: "navigation", ItemKey: "navigation.navigation", Active: true},
	}
	repo := &fakeOverrideRepo{docs: []models.NavOverride{
		{ParentKey: sectionRootKey("platform", "Administration"), OrderedChildren: []string{
			"logging.log-levels", "navigation.navigation",
		}},
	}}
	overrideSvc := NewOverrideService(repo, nil)
	svc := NewDynamicNavigationService(items, nil, overrideSvc)

	resp, err := svc.GetNavigationForUser(context.Background(), "administrator")
	if err != nil {
		t.Fatalf("GetNavigationForUser: %v", err)
	}
	var section *models.NavSection
	for i := range resp.Realms {
		if resp.Realms[i].Key == "platform" {
			for j := range resp.Realms[i].Sections {
				if resp.Realms[i].Sections[j].Label == "Administration" {
					section = &resp.Realms[i].Sections[j]
				}
			}
		}
	}
	if section == nil {
		t.Fatal("platform/Administration section missing from response")
	}
	want := []string{"Log levels", "Navigation", "Modules"}
	if len(section.Children) != len(want) {
		t.Fatalf("got %d items, want %d", len(section.Children), len(want))
	}
	for i, name := range want {
		if section.Children[i].Name != name {
			t.Errorf("position %d = %q, want %q", i, section.Children[i].Name, name)
		}
	}
}

// TestDynamicNavigation_NestedChildOverride checks that a child-level
// override (keyed by the parent's ItemKey) reorders nested items.
func TestDynamicNavigation_NestedChildOverride(t *testing.T) {
	items := []module.NavItemSpec{
		{
			Realm: "business", Section: "Invoicing", Name: "Invoicing", Path: "/billing", ModuleName: "billing", ItemKey: "billing.invoicing", Active: true,
			Children: []module.NavItemSpec{
				{Name: "Issued", Path: "/billing/issued", ModuleName: "billing", ItemKey: "billing.invoicing.issued", Active: true},
				{Name: "Received", Path: "/billing/received", ModuleName: "billing", ItemKey: "billing.invoicing.received", Active: true},
				{Name: "Suppliers", Path: "/billing/suppliers", ModuleName: "billing", ItemKey: "billing.invoicing.suppliers", Active: true},
			},
		},
	}
	repo := &fakeOverrideRepo{docs: []models.NavOverride{
		{ParentKey: "billing.invoicing", OrderedChildren: []string{"billing.invoicing.received", "billing.invoicing.suppliers"}},
	}}
	overrideSvc := NewOverrideService(repo, nil)
	svc := NewDynamicNavigationService(items, nil, overrideSvc)

	resp, err := svc.GetNavigationForUser(context.Background(), "administrator")
	if err != nil {
		t.Fatalf("GetNavigationForUser: %v", err)
	}
	if len(resp.Realms) == 0 {
		t.Fatal("no realms in response")
	}
	var invoicing *models.NavItem
	for i := range resp.Realms {
		if resp.Realms[i].Key != "business" {
			continue
		}
		for j := range resp.Realms[i].Sections {
			for k := range resp.Realms[i].Sections[j].Children {
				if resp.Realms[i].Sections[j].Children[k].Name == "Invoicing" {
					invoicing = &resp.Realms[i].Sections[j].Children[k]
				}
			}
		}
	}
	if invoicing == nil {
		t.Fatal("Invoicing parent missing")
	}
	want := []string{"Received", "Suppliers", "Issued"}
	if len(invoicing.Children) != len(want) {
		t.Fatalf("children len = %d, want %d", len(invoicing.Children), len(want))
	}
	for i, name := range want {
		if invoicing.Children[i].Name != name {
			t.Errorf("child[%d] = %q, want %q", i, invoicing.Children[i].Name, name)
		}
	}
}
