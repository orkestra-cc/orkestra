package tenant

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestConfigGroups_DeclarationsValidate(t *testing.T) {
	m := &Module{}
	if err := module.ValidateConfigDeclarations(m.ConfigSchema(), m.ConfigGroups()); err != nil {
		t.Errorf("ValidateConfigDeclarations: %v", err)
	}
}

func TestConfigGroups_FieldsSplitByTier(t *testing.T) {
	want := map[string]string{
		"provisioning.internal.mode": "provisioning.internal",
		"provisioning.external.mode": "provisioning.external",
	}
	declared := map[string]bool{}
	for _, g := range (&Module{}).ConfigGroups() {
		declared[g.Key] = true
	}
	for _, f := range (&Module{}).ConfigSchema() {
		if g, ok := want[f.Key]; ok && f.Group != g {
			t.Errorf("field %q Group = %q, want %q", f.Key, f.Group, g)
		}
		if !declared[f.Group] {
			t.Errorf("field %q references undeclared group %q", f.Key, f.Group)
		}
	}
}
