package auth

import (
	"testing"
)

func TestNavItemsDeclareServiceAccounts(t *testing.T) {
	m := &AuthModule{}
	items := m.NavItems()
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 nav item, got %d", len(items))
	}
	it := items[0]
	if it.Path != "/admin/service-accounts" || it.Realm != "platform" || it.Tier != "internal" ||
		it.MinRole != "administrator" || !it.Active || it.ItemKey == "" {
		t.Fatalf("unexpected nav item: %+v", it)
	}
}
