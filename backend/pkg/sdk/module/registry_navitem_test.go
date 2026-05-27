package module

import "testing"

func TestSlugifyNavName(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"Invoices":          "invoices",
		"SDI Notifications": "sdi-notifications",
		"  Trim Me  ":       "trim-me",
		"A/B C-D":           "a-b-c-d",
		"Già pronto":        "gi-pronto",
		"---":               "",
	}
	for in, want := range cases {
		if got := slugifyNavName(in); got != want {
			t.Errorf("slugifyNavName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultNavItemKey(t *testing.T) {
	if got := defaultNavItemKey("billing", "", "Invoices Issued"); got != "billing.invoices-issued" {
		t.Errorf("root key = %q", got)
	}
	if got := defaultNavItemKey("billing", "billing.invoices", "Issued"); got != "billing.invoices.issued" {
		t.Errorf("child key = %q", got)
	}
	if got := defaultNavItemKey("", "", "Lone"); got != "module.lone" {
		t.Errorf("missing module fallback = %q", got)
	}
	if got := defaultNavItemKey("billing", "", ""); got != "billing.item" {
		t.Errorf("missing name fallback = %q", got)
	}
}

func TestStampNavItem_FillsModuleAndItemKey(t *testing.T) {
	item := NavItemSpec{
		Name: "Invoicing",
		Children: []NavItemSpec{
			{Name: "Invoices Issued"},
			{Name: "Invoices Received", ItemKey: "billing.invoices.received"}, // explicit pin
		},
	}

	stampNavItem(&item, "billing", "")

	if item.ModuleName != "billing" || item.ItemKey != "billing.invoicing" {
		t.Fatalf("root stamp: module=%q key=%q", item.ModuleName, item.ItemKey)
	}
	if c := item.Children[0]; c.ModuleName != "billing" || c.ItemKey != "billing.invoicing.invoices-issued" {
		t.Errorf("child[0] stamp: module=%q key=%q", c.ModuleName, c.ItemKey)
	}
	// Explicit ItemKey on the second child must NOT be overwritten.
	if c := item.Children[1]; c.ItemKey != "billing.invoices.received" {
		t.Errorf("child[1] explicit key clobbered: %q", c.ItemKey)
	}
	// But ModuleName is registry-controlled and always stamped.
	if c := item.Children[1]; c.ModuleName != "billing" {
		t.Errorf("child[1] module not stamped: %q", c.ModuleName)
	}
}

func TestStampNavItem_DeepNestingUsesParentKey(t *testing.T) {
	item := NavItemSpec{
		Name: "Admin",
		Children: []NavItemSpec{
			{Name: "Modules", Children: []NavItemSpec{
				{Name: "Navigation"},
			}},
		},
	}
	stampNavItem(&item, "platform", "")
	got := item.Children[0].Children[0].ItemKey
	want := "platform.admin.modules.navigation"
	if got != want {
		t.Errorf("deep key = %q, want %q", got, want)
	}
}
