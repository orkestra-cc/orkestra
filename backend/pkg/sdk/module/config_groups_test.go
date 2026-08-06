package module

import (
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
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
// the seven new fields: their struct tags. Assigning a field and reading it
// back would only test the compiler.
func TestConfigField_MetadataTagsRoundTrip(t *testing.T) {
	min, max := 8, 128
	f := ConfigField{
		Key:         "passwordMinLength",
		Label:       "Minimum length",
		Group:       "password",
		Type:        FieldInt,
		Advanced:    true,
		DependsOn:   []FieldCondition{{Key: "passwordPolicyEnabled", In: []string{"true"}}},
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

// TestPersistedConfigTypes_TagEveryField is the standing version of the
// round-trip test above: that one pins today's fields, this one pins the
// rule. ConfigSchema is persisted into module_configs and rewritten by
// RefreshMetadata on every boot, so a field added later without a bson tag
// would serve correctly and then vanish across a restart — the exact failure
// the round-trip test describes, one field too late to catch.
func TestPersistedConfigTypes_TagEveryField(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{"ConfigField", reflect.TypeOf(ConfigField{})},
		{"FieldCondition", reflect.TypeOf(FieldCondition{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.typ.NumField() {
				f := tc.typ.Field(i)
				if _, ok := f.Tag.Lookup("json"); !ok {
					t.Errorf("%s.%s has no json tag — the admin API wire name would default to the Go name",
						tc.name, f.Name)
				}
				if _, ok := f.Tag.Lookup("bson"); !ok {
					t.Errorf("%s.%s has no bson tag — it would silently vanish across a restart",
						tc.name, f.Name)
				}
			}
		})
	}
}

// TestConfigGroup_NeverPersisted is the inverse rule: groups are resolved
// live from the registry by the admin handler and deliberately never written
// to module_configs. A bson tag here is the defect — it would invite a
// snapshot that then goes stale against the running binary.
func TestConfigGroup_NeverPersisted(t *testing.T) {
	typ := reflect.TypeOf(ConfigGroup{})
	for i := range typ.NumField() {
		f := typ.Field(i)
		if _, ok := f.Tag.Lookup("json"); !ok {
			t.Errorf("ConfigGroup.%s has no json tag — the admin API wire name would default to the Go name", f.Name)
		}
		if tag, ok := f.Tag.Lookup("bson"); ok {
			t.Errorf("ConfigGroup.%s carries a bson tag %q — config groups are presentation-only and never persisted",
				f.Name, tag)
		}
	}
}

// TestToConfigResponse_ResolvesConfigGroupsFromRegistry exercises the one
// line of wiring this feature adds to the handler. toConfigResponse never
// touches the config service, and collectInfraStatus early-returns for a
// module declaring no containers, so a nil config service plus a registry
// holding the module is the whole fixture.
func TestToConfigResponse_ResolvesConfigGroupsFromRegistry(t *testing.T) {
	registry := NewModuleRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	registry.Register(groupedModule{})
	h := NewModuleAdminHandler(nil, registry)

	resp := h.toConfigResponse(ModuleConfig{ModuleName: "grouped", Enabled: true})
	if len(resp.ConfigGroups) != 2 {
		t.Fatalf("ConfigGroups = %+v, want the 2 groups the live module declares", resp.ConfigGroups)
	}
	if resp.ConfigGroups[0].Key != "oauth" || resp.ConfigGroups[1].Parent != "oauth" {
		t.Errorf("ConfigGroups = %+v, want them resolved from the module, not the stored doc", resp.ConfigGroups)
	}
}

func TestToConfigResponse_OrphanDocumentHasNoConfigGroups(t *testing.T) {
	// An orphan is a module_configs document whose module is no longer
	// compiled in (an addon a fork dropped). It has no live module to resolve
	// groups from, so the response must degrade to the flat form rather than
	// carry an empty key.
	registry := NewModuleRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	registry.Register(groupedModule{})
	h := NewModuleAdminHandler(nil, registry)

	resp := h.toConfigResponse(ModuleConfig{ModuleName: "departed-addon"})
	if resp.ConfigGroups != nil {
		t.Fatalf("ConfigGroups = %+v, want nil for a module absent from the registry", resp.ConfigGroups)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	if strings.Contains(string(raw), "configGroups") {
		t.Errorf("payload %s should omit configGroups for an orphan document", raw)
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

func TestConfigField_DependsOnMatchTag(t *testing.T) {
	f := ConfigField{
		Key:            "googleClientId",
		Label:          "Client ID",
		Type:           FieldString,
		DependsOnMatch: "any",
		DependsOn: []FieldCondition{
			{Key: "googleEnabledAdmin", In: []string{"true"}},
			{Key: "googleEnabledClient", In: []string{"true"}},
		},
	}

	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	if !strings.Contains(string(raw), `"dependsOnMatch":"any"`) {
		t.Errorf("json payload %s missing dependsOnMatch", raw)
	}

	// omitempty: a field using the default AND semantics must not carry the key.
	bare, err := json.Marshal(ConfigField{Key: "k", Type: FieldString})
	if err != nil {
		t.Fatalf("json.Marshal bare = %v", err)
	}
	if strings.Contains(string(bare), "dependsOnMatch") {
		t.Errorf("bare payload %s should omit dependsOnMatch", bare)
	}

	// bson: ConfigSchema is persisted and rewritten from the binary on every
	// boot. A missing bson tag serves correctly then vanishes across a restart.
	encoded, err := bson.Marshal(f)
	if err != nil {
		t.Fatalf("bson.Marshal = %v", err)
	}
	var back ConfigField
	if err := bson.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("bson.Unmarshal = %v", err)
	}
	if back.DependsOnMatch != "any" {
		t.Errorf("DependsOnMatch after bson round-trip = %q, want %q", back.DependsOnMatch, "any")
	}
}
