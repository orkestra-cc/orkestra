package compliance

import (
	"slices"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestPolicyEngineModuleSurface(t *testing.T) {
	m := NewModule()
	for _, key := range []string{"system.compliance.policy.read", "system.compliance.policy.manage"} {
		found := false
		for _, p := range m.Permissions() {
			if p.Key == key {
				found = p.System && p.Module == "compliance"
			}
		}
		if !found {
			t.Fatalf("permission %s missing or not a system permission", key)
		}
	}
	var fourEyes *module.ConfigField
	schema := m.ConfigSchema()
	for i := range schema {
		if schema[i].Key == "four_eyes_enabled" {
			fourEyes = &schema[i]
		}
	}
	if fourEyes == nil || fourEyes.Type != module.FieldBool || fourEyes.Default != "true" || fourEyes.Group != "policy" {
		t.Fatalf("four_eyes_enabled = %+v", fourEyes)
	}
	if !slices.Contains(m.ProvidedServices(), module.ServiceCompliancePolicy) {
		t.Fatal("compliance does not publish the policy resolver")
	}
}
