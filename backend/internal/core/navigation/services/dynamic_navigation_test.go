package services

import (
	"context"
	"testing"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/navigation/models"
)

// ctxTenantKindKey mirrors the unexported middleware.ctxTenantKind constant
// ("tenantKind"). We key off the same string because ctxauth.TenantKindFromContext
// reads it from ctx.Value("tenantKind"). Kept as a local const so a rename on
// the middleware side surfaces via the tenantKind tests failing loudly.
const ctxTenantKindKey = "tenantKind"

// stubEnabled implements modulegate.ModuleEnabledChecker for tests.
type stubEnabled struct {
	disabled map[string]bool
}

func (s *stubEnabled) IsEnabled(_ context.Context, moduleName string) bool {
	return !s.disabled[moduleName]
}

func withTenantKind(ctx context.Context, kind string) context.Context {
	return context.WithValue(ctx, ctxTenantKindKey, kind)
}

// findItem returns the first NavItem in the tree whose Name matches, or nil.
func findItem(items []models.NavItem, name string) *models.NavItem {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
		if child := findItem(items[i].Children, name); child != nil {
			return child
		}
	}
	return nil
}

// collectRealmKeys flattens realm keys in emission order.
func collectRealmKeys(realms []models.NavRealm) []string {
	keys := make([]string, len(realms))
	for i, r := range realms {
		keys[i] = r.Key
	}
	return keys
}

func TestTierFilter_InternalOnlyHiddenFromExternal(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmPlatform, Section: "Admin", Tier: iface.TenantKindInternal, Name: "Users", Path: "/admin/users", Active: true},
		{Realm: realmShared, Section: "Tools", Name: "Knowledge Base", Path: "/kb", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	ctx := withTenantKind(context.Background(), iface.TenantKindExternal)
	resp, err := svc.GetNavigationForUser(ctx, "administrator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must not see the internal-tier item.
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if findItem(s.Children, "Users") != nil {
				t.Fatalf("external tenant saw internal-only item 'Users'")
			}
		}
	}
	// Must still see untiered shared item.
	var sawKB bool
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if findItem(s.Children, "Knowledge Base") != nil {
				sawKB = true
			}
		}
	}
	if !sawKB {
		t.Fatalf("external tenant did not see untiered 'Knowledge Base'")
	}
	if resp.TenantKind != iface.TenantKindExternal {
		t.Fatalf("want TenantKind %q, got %q", iface.TenantKindExternal, resp.TenantKind)
	}
}

func TestTierFilter_ExternalOnlyHiddenFromInternal(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmBusiness, Section: "Portal", Tier: iface.TenantKindExternal, Name: "My Subscription", Path: "/my/sub", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	ctx := withTenantKind(context.Background(), iface.TenantKindInternal)
	resp, _ := svc.GetNavigationForUser(ctx, "administrator")

	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if findItem(s.Children, "My Subscription") != nil {
				t.Fatalf("internal tenant saw external-only item")
			}
		}
	}
}

func TestTierFilter_EmptyTierVisibleToAll(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmShared, Section: "Tools", Name: "Calendar", Path: "/cal", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	for _, kind := range []string{iface.TenantKindInternal, iface.TenantKindExternal, ""} {
		ctx := withTenantKind(context.Background(), kind)
		resp, _ := svc.GetNavigationForUser(ctx, "operator")
		found := false
		for _, r := range resp.Realms {
			for _, s := range r.Sections {
				if findItem(s.Children, "Calendar") != nil {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("untiered item hidden for tenantKind=%q", kind)
		}
	}
}

func TestMinRoleFilter(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmPlatform, Section: "Admin", MinRole: "administrator", Name: "Modules", Path: "/admin/modules", Active: true},
		{Realm: realmPersonal, Section: "My workspace", Name: "Dashboard", Path: "/user/dashboard", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	cases := []struct {
		role       string
		seeModules bool
	}{
		{"super_admin", true},
		{"administrator", true},
		{"developer", false},
		{"manager", false},
		{"operator", false},
		{"guest", false},
	}
	for _, tc := range cases {
		resp, _ := svc.GetNavigationForUser(context.Background(), tc.role)
		var sawModules bool
		for _, r := range resp.Realms {
			for _, s := range r.Sections {
				if findItem(s.Children, "Modules") != nil {
					sawModules = true
				}
			}
		}
		if sawModules != tc.seeModules {
			t.Errorf("role=%q: want seeModules=%v, got %v", tc.role, tc.seeModules, sawModules)
		}
	}
}

func TestModuleDisabled_HidesItem(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmBusiness, Section: "Revenue", ModuleName: "billing", Name: "Invoicing", Path: "/billing", Active: true},
		{Realm: realmShared, Section: "Tools", ModuleName: "rag", Name: "Knowledge Base", Path: "/kb", Active: true},
	}
	checker := &stubEnabled{disabled: map[string]bool{"billing": true}}
	svc := NewDynamicNavigationService(items, checker)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if findItem(s.Children, "Invoicing") != nil {
				t.Fatalf("disabled module's item still visible")
			}
		}
	}
	var sawKB bool
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if findItem(s.Children, "Knowledge Base") != nil {
				sawKB = true
			}
		}
	}
	if !sawKB {
		t.Fatalf("enabled module's item was filtered out")
	}
}

func TestRealms_CanonicalOrder(t *testing.T) {
	// Declared in jumbled order — emitted order should still be
	// personal → platform → business → shared.
	items := []module.NavItemSpec{
		{Realm: realmShared, Section: "Tools", Name: "Company", Path: "/company", Active: true},
		{Realm: realmBusiness, Section: "Revenue", Name: "Invoicing", Path: "/billing", Active: true},
		{Realm: realmPlatform, Section: "Admin", Name: "Users", Path: "/admin/users", Active: true},
		{Realm: realmPersonal, Section: "My workspace", Name: "Dashboard", Path: "/user/dashboard", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	got := collectRealmKeys(resp.Realms)
	want := []string{realmPersonal, realmPlatform, realmBusiness, realmShared}
	if len(got) != len(want) {
		t.Fatalf("want %d realms, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("realm order: want %v, got %v", want, got)
		}
	}
}

func TestRealms_LabelsCanonicalized(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmPlatform, Section: "Admin", Name: "Users", Path: "/admin/users", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	if resp.Realms[0].Key != realmPlatform {
		t.Fatalf("want key=%q, got %q", realmPlatform, resp.Realms[0].Key)
	}
	if resp.Realms[0].Label != "Administration" {
		t.Fatalf("want label %q, got %q", "Administration", resp.Realms[0].Label)
	}
}

func TestRealms_EmptyRealmFallsBackToShared(t *testing.T) {
	items := []module.NavItemSpec{
		{Section: "Tools", Name: "Company", Path: "/company", Active: true}, // no Realm
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	if len(resp.Realms) != 1 {
		t.Fatalf("want 1 realm, got %d", len(resp.Realms))
	}
	if resp.Realms[0].Key != realmShared {
		t.Fatalf("empty realm should fall back to shared, got %q", resp.Realms[0].Key)
	}
}

func TestLegacyV1GroupsStillEmitted(t *testing.T) {
	// A module that has not migrated to v2 should still render in Groups.
	items := []module.NavItemSpec{
		{Group: "Administration", Name: "Fatturazione", Path: "/billing", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	if len(resp.Groups) != 1 || resp.Groups[0].Label != "Administration" {
		t.Fatalf("legacy v1 Groups missing or mislabeled: %+v", resp.Groups)
	}
	// And it lands in the shared realm for v2 because Realm is empty.
	if len(resp.Realms) != 1 || resp.Realms[0].Key != realmShared {
		t.Fatalf("legacy item should render under shared realm in v2, got: %+v", resp.Realms)
	}
	// Section label falls back to Group.
	if resp.Realms[0].Sections[0].Label != "Administration" {
		t.Fatalf("want section label %q (from legacy Group), got %q", "Administration", resp.Realms[0].Sections[0].Label)
	}
}

func TestParentWithNoLinkAndNoVisibleChildrenCollapses(t *testing.T) {
	items := []module.NavItemSpec{
		{
			Realm:   realmBusiness,
			Section: "Revenue",
			Name:    "Payments",
			// No Path, so this parent only renders if a child survives.
			Active: true,
			Children: []module.NavItemSpec{
				{Name: "Transactions", Tier: iface.TenantKindExternal, Path: "/payments/txn", Active: true},
			},
		},
	}
	svc := NewDynamicNavigationService(items, nil)

	ctx := withTenantKind(context.Background(), iface.TenantKindInternal)
	resp, _ := svc.GetNavigationForUser(ctx, "administrator")

	// Child is external-only → hidden for internal; parent has no Path → must collapse.
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if findItem(s.Children, "Payments") != nil {
				t.Fatalf("parent with no path and no surviving children should be collapsed")
			}
		}
	}
}

// TestEmptyNavItems_ReturnsEmptyResponseWithoutPanic guards against a regression
// where an empty registry (no module declared NavItems) caused a nil deref in
// the realm builder. The response must still be non-nil with empty slices.
func TestEmptyNavItems_ReturnsEmptyResponseWithoutPanic(t *testing.T) {
	svc := NewDynamicNavigationService(nil, nil)

	resp, err := svc.GetNavigationForUser(context.Background(), "administrator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("response was nil")
	}
	if len(resp.Realms) != 0 || len(resp.Groups) != 0 {
		t.Errorf("expected empty Realms/Groups, got %d realms, %d groups", len(resp.Realms), len(resp.Groups))
	}
	if resp.UserRole != "administrator" {
		t.Errorf("UserRole not set: %q", resp.UserRole)
	}
}

// TestIconAndActiveForwarded verifies the four fields convert() actually
// copies from spec to rendered item: Name, Path (To), Icon, Active. A
// regression here would silently strip icons from the sidebar.
func TestIconAndActiveForwarded(t *testing.T) {
	items := []module.NavItemSpec{
		{
			Realm: realmShared, Section: "Tools",
			Name: "Inbox", Path: "/inbox",
			Icon:   "fa-inbox",
			Active: true,
		},
		{
			Realm: realmShared, Section: "Tools",
			Name: "Archive", Path: "/archive",
			Icon:   "fa-archive",
			Active: false,
		},
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	var inbox, archive *models.NavItem
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if item := findItem(s.Children, "Inbox"); item != nil {
				inbox = item
			}
			if item := findItem(s.Children, "Archive"); item != nil {
				archive = item
			}
		}
	}
	if inbox == nil || archive == nil {
		t.Fatalf("missing items: inbox=%v archive=%v", inbox, archive)
	}
	if inbox.Icon != "fa-inbox" {
		t.Errorf("Icon = %v, want %q", inbox.Icon, "fa-inbox")
	}
	if inbox.To != "/inbox" {
		t.Errorf("To = %q, want %q", inbox.To, "/inbox")
	}
	if !inbox.Active {
		t.Error("Active=true was dropped")
	}
	if archive.Active {
		t.Error("Active=false was flipped to true")
	}
}

// TestSectionsPreserveDiscoveryOrderWithinRealm pins the ordering contract: a
// realm's sections must appear in the order they were first encountered, not
// alphabetical. The frontend relies on this for stable sidebar layout.
func TestSectionsPreserveDiscoveryOrderWithinRealm(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmPlatform, Section: "Zeta", Name: "Z1", Path: "/z1", Active: true},
		{Realm: realmPlatform, Section: "Alpha", Name: "A1", Path: "/a1", Active: true},
		{Realm: realmPlatform, Section: "Mu", Name: "M1", Path: "/m1", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	if len(resp.Realms) != 1 {
		t.Fatalf("want 1 realm, got %d", len(resp.Realms))
	}
	got := make([]string, len(resp.Realms[0].Sections))
	for i, s := range resp.Realms[0].Sections {
		got[i] = s.Label
	}
	want := []string{"Zeta", "Alpha", "Mu"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("section order: want %v, got %v", want, got)
		}
	}
}

// TestParentWithPathSurvivesAllChildrenFiltered: a parent that has its own
// Path must remain visible even when every child is filtered out — the parent
// itself is still a destination.
func TestParentWithPathSurvivesAllChildrenFiltered(t *testing.T) {
	items := []module.NavItemSpec{
		{
			Realm: realmBusiness, Section: "Revenue",
			Name: "Sales", Path: "/sales", Active: true,
			Children: []module.NavItemSpec{
				{Name: "Reports", Tier: iface.TenantKindExternal, Path: "/sales/reports", Active: true},
			},
		},
	}
	svc := NewDynamicNavigationService(items, nil)

	ctx := withTenantKind(context.Background(), iface.TenantKindInternal)
	resp, _ := svc.GetNavigationForUser(ctx, "administrator")

	var sales *models.NavItem
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if item := findItem(s.Children, "Sales"); item != nil {
				sales = item
			}
		}
	}
	if sales == nil {
		t.Fatal("parent with Path must survive when all children are filtered")
	}
	if len(sales.Children) != 0 {
		t.Errorf("expected 0 visible children, got %d", len(sales.Children))
	}
}

// TestEmptyRoleStillFiltersByMinRole: an empty userRole should be treated as
// the lowest tier (rank 0) so MinRole-gated items are hidden. Guards against a
// regression where the handler's "guest" fallback was bypassed in service.
func TestEmptyRoleStillFiltersByMinRole(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmPlatform, Section: "Admin", MinRole: "guest", Name: "PublicHelp", Path: "/help", Active: true},
		{Realm: realmPlatform, Section: "Admin", MinRole: "administrator", Name: "Secret", Path: "/secret", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "")
	var sawSecret, sawHelp bool
	for _, r := range resp.Realms {
		for _, s := range r.Sections {
			if findItem(s.Children, "Secret") != nil {
				sawSecret = true
			}
			if findItem(s.Children, "PublicHelp") != nil {
				sawHelp = true
			}
		}
	}
	if sawSecret {
		t.Error("empty role saw administrator-gated item")
	}
	if sawHelp {
		// rank("") = 0, rank("guest") = 1 → guest gate excludes empty role too
		t.Error("empty role saw guest-gated item (rank 0 < 1)")
	}
}

// TestUnknownRealmAppendsAfterCanonicalOrder: items with a realm key not in
// realmOrder must still render, appended after the canonical realms in
// encounter order.
func TestUnknownRealmAppendsAfterCanonicalOrder(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmPlatform, Section: "Admin", Name: "Users", Path: "/admin/users", Active: true},
		{Realm: "experimental", Section: "Lab", Name: "Beta", Path: "/beta", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	resp, _ := svc.GetNavigationForUser(context.Background(), "administrator")
	keys := collectRealmKeys(resp.Realms)
	if len(keys) != 2 {
		t.Fatalf("want 2 realms, got %d (%v)", len(keys), keys)
	}
	if keys[0] != realmPlatform || keys[1] != "experimental" {
		t.Errorf("unknown realm should append after canonical: got %v", keys)
	}
}

func TestCacheKey_IncludesTenantKind(t *testing.T) {
	items := []module.NavItemSpec{
		{Realm: realmShared, Section: "Tools", Name: "Calendar", Path: "/cal", Active: true},
	}
	svc := NewDynamicNavigationService(items, nil)

	internal, _ := svc.GetNavigationForUser(withTenantKind(context.Background(), iface.TenantKindInternal), "administrator")
	external, _ := svc.GetNavigationForUser(withTenantKind(context.Background(), iface.TenantKindExternal), "administrator")
	untiered, _ := svc.GetNavigationForUser(context.Background(), "administrator")

	if internal.CacheKey == external.CacheKey {
		t.Fatalf("cache key should differ by tenant kind: internal=%q external=%q", internal.CacheKey, external.CacheKey)
	}
	if untiered.CacheKey != "nav:administrator" {
		t.Fatalf("expected untiered cache key to omit kind: got %q", untiered.CacheKey)
	}
}
