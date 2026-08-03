package compliance

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func complianceSchema() []module.ConfigField { return (&Module{}).ConfigSchema() }

func TestConfigGroups_DeclarationsValidate(t *testing.T) {
	if err := module.ValidateConfigDeclarations(complianceSchema(), (&Module{}).ConfigGroups()); err != nil {
		t.Errorf("ValidateConfigDeclarations: %v", err)
	}
}

func TestConfigGroups_EveryFieldGrouped(t *testing.T) {
	declared := map[string]bool{}
	for _, g := range (&Module{}).ConfigGroups() {
		declared[g.Key] = true
	}
	for _, f := range complianceSchema() {
		if !declared[f.Group] {
			t.Errorf("field %q references undeclared group %q", f.Key, f.Group)
		}
	}
}

func TestRetention_GatingMatchesConsumption(t *testing.T) {
	// retention_years means nothing while auto_cleanup_enabled is off — the
	// reaper reads the two together (module.go, RetentionConfig) — so it is
	// hidden until the toggle is on. export_retention_days governs how long a
	// generated DSR export stays downloadable, part of the always-on
	// right-of-access pipeline and independent of the cleanup job, so it stays
	// visible unconditionally. This freezes that distinction against a future
	// "gate everything under retention" edit.
	byKey := map[string]module.ConfigField{}
	for _, f := range complianceSchema() {
		byKey[f.Key] = f
	}

	ry, ok := byKey["retention_years"]
	if !ok {
		t.Fatal("retention_years missing from schema")
	}
	if len(ry.DependsOn) != 1 ||
		ry.DependsOn[0].Key != "auto_cleanup_enabled" ||
		len(ry.DependsOn[0].In) != 1 || ry.DependsOn[0].In[0] != "true" {
		t.Errorf("retention_years DependsOn = %+v, want [auto_cleanup_enabled in [true]]", ry.DependsOn)
	}

	if erd := byKey["export_retention_days"]; len(erd.DependsOn) != 0 {
		t.Errorf("export_retention_days DependsOn = %+v, want none — DSR export TTL is independent of retention auto-cleanup", erd.DependsOn)
	}
}
