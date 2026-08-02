package module

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// groupedModule implements Module + HasConfigGroups. It mirrors the shape
// core auth will take: a parent group with nested provider children.
type groupedModule struct{}

func (groupedModule) Name() string               { return "grouped" }
func (groupedModule) Category() ModuleCategory   { return CategoryToggleable }
func (groupedModule) Init(_ *Dependencies) error { return nil }
func (groupedModule) ConfigGroups() []ConfigGroup {
	return []ConfigGroup{
		{Key: "oauth", Label: "OAuth Providers", Order: 1},
		{Key: "oauth.google", Label: "Google", Parent: "oauth", Order: 2},
	}
}

var _ HasConfigGroups = groupedModule{}

func TestConfigGroupsOf_Declared(t *testing.T) {
	got := ConfigGroupsOf(groupedModule{})
	if len(got) != 2 {
		t.Fatalf("ConfigGroupsOf returned %d groups, want 2", len(got))
	}
	if got[0].Key != "oauth" {
		t.Errorf("first group Key = %q, want %q", got[0].Key, "oauth")
	}
	if got[1].Parent != "oauth" {
		t.Errorf("nested group Parent = %q, want %q", got[1].Parent, "oauth")
	}
}

func TestConfigGroupsOf_NotDeclared(t *testing.T) {
	// A module that does not implement the sub-interface must degrade to nil,
	// not panic — this is the path every un-migrated fork addon takes.
	if got := ConfigGroupsOf(minimalModule{name: "minimal"}); got != nil {
		t.Errorf("ConfigGroupsOf = %v, want nil for a module without the sub-interface", got)
	}
}

// TestConfigField_MetadataTagsRoundTrip covers the actual defect surface of
// the six new fields: their struct tags. Assigning a field and reading it
// back would only test the compiler.
func TestConfigField_MetadataTagsRoundTrip(t *testing.T) {
	min, max := 8, 128
	f := ConfigField{
		Key:         "passwordMinLength",
		Label:       "Minimum length",
		Group:       "password",
		Type:        FieldInt,
		Advanced:    true,
		DependsOn:   []Condition{{Key: "passwordPolicyEnabled", In: []string{"true"}}},
		Min:         &min,
		Max:         &max,
		Pattern:     "^[0-9]+$",
		Placeholder: "12",
		HelpURL:     "https://docs.orkestra.cc/auth/password",
	}

	// JSON: the admin API is the only consumer, so the wire names are the contract.
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	for _, want := range []string{
		`"advanced":true`,
		`"dependsOn":[{"key":"passwordPolicyEnabled","in":["true"]}]`,
		`"min":8`,
		`"max":128`,
		`"pattern":"^[0-9]+$"`,
		`"placeholder":"12"`,
		`"helpUrl":"https://docs.orkestra.cc/auth/password"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("json payload %s missing %s", raw, want)
		}
	}

	// omitempty: a field declaring none of the new metadata must not bloat
	// every schema entry of every admin response with empty keys.
	bare, err := json.Marshal(ConfigField{Key: "k", Type: FieldString})
	if err != nil {
		t.Fatalf("json.Marshal bare = %v", err)
	}
	for _, absent := range []string{
		"advanced", "dependsOn", "min", "max", "pattern", "placeholder", "helpUrl",
	} {
		if strings.Contains(string(bare), absent) {
			t.Errorf("bare field payload %s should omit %q", bare, absent)
		}
	}

	// BSON: ConfigSchema is persisted into module_configs and rewritten by
	// RefreshMetadata on every boot. A missing bson tag drops the field
	// silently — it would serve correctly and then vanish across a restart.
	encoded, err := bson.Marshal(f)
	if err != nil {
		t.Fatalf("bson.Marshal = %v", err)
	}
	var back ConfigField
	if err := bson.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("bson.Unmarshal = %v", err)
	}
	if !back.Advanced {
		t.Error("Advanced did not survive the bson round-trip")
	}
	if back.Min == nil || *back.Min != 8 || back.Max == nil || *back.Max != 128 {
		t.Errorf("Min/Max after bson round-trip = %v/%v, want 8/128", back.Min, back.Max)
	}
	if len(back.DependsOn) != 1 ||
		back.DependsOn[0].Key != "passwordPolicyEnabled" ||
		len(back.DependsOn[0].In) != 1 ||
		back.DependsOn[0].In[0] != "true" {
		t.Errorf("DependsOn after bson round-trip = %+v", back.DependsOn)
	}
	if back.Pattern != "^[0-9]+$" || back.Placeholder != "12" ||
		back.HelpURL != "https://docs.orkestra.cc/auth/password" {
		t.Errorf("string metadata after bson round-trip = %q / %q / %q",
			back.Pattern, back.Placeholder, back.HelpURL)
	}
}

func TestModuleConfigResponse_SerialisesConfigGroups(t *testing.T) {
	resp := ModuleConfigResponse{
		ModuleName:   "auth",
		ConfigGroups: []ConfigGroup{{Key: "oauth", Label: "OAuth Providers", Order: 5}},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	got := string(raw)
	for _, want := range []string{`"configGroups"`, `"key":"oauth"`, `"order":5`} {
		if !strings.Contains(got, want) {
			t.Errorf("payload %s missing %s", got, want)
		}
	}
}

func TestModuleConfigResponse_OmitsEmptyConfigGroups(t *testing.T) {
	// A module without groups must not ship an empty key — the frontend
	// treats "absent" as the flat-form degradation path.
	raw, err := json.Marshal(ModuleConfigResponse{ModuleName: "compliance"})
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	if strings.Contains(string(raw), "configGroups") {
		t.Errorf("payload %s should omit configGroups when empty", raw)
	}
}
