package module

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func intp(n int) *int { return &n }

func prefSchema() []ConfigField {
	return []ConfigField{
		{Key: "columns", Type: FieldStringList, Default: "name,email"},
		{Key: "pageSize", Type: FieldInt, Min: intp(10), Max: intp(100), Default: "25"},
		{Key: "digest", Type: FieldEnum, Options: []string{"daily", "weekly"}, Required: true},
		{Key: "compact", Type: FieldBool},
		{Key: "refresh", Type: FieldDuration},
		{Key: "initials", Type: FieldString, Pattern: "^[A-Z]{2,3}$", Max: intp(3)},
	}
}

func TestUserPreferenceRegistry_RegisterFields(t *testing.T) {
	reg := NewUserPreferenceRegistry()
	if err := reg.RegisterFields("crm", prefSchema()); err != nil {
		t.Fatalf("valid schema refused: %v", err)
	}
	if err := reg.RegisterFields("crm", prefSchema()); err == nil {
		t.Error("second registration for the same module must be refused")
	}
	got, ok := reg.Fields("crm")
	if !ok || len(got) != 6 {
		t.Errorf("Fields(crm) = %v, %v", got, ok)
	}
	if _, ok := reg.Fields("forms"); ok {
		t.Error("an unregistered module must report no fields")
	}
	if m := reg.Modules(); len(m) != 1 || m[0] != "crm" {
		t.Errorf("Modules() = %v", m)
	}

	bad := map[string][]ConfigField{
		"empty":       {},
		"secret":      {{Key: "token", Type: FieldSecret}},
		"recordList":  {{Key: "rows", Type: FieldRecordList}},
		"unknownType": {{Key: "x", Type: "blob"}},
		"enumNoOpts":  {{Key: "x", Type: FieldEnum}},
		"badKey":      {{Key: "Bad Key", Type: FieldString}},
		"dupKey":      {{Key: "a", Type: FieldString}, {Key: "a", Type: FieldBool}},
		"badPattern":  {{Key: "a", Type: FieldString, Pattern: "("}},
		"minGtMax":    {{Key: "a", Type: FieldInt, Min: intp(5), Max: intp(1)}},
		"badDefault":  {{Key: "a", Type: FieldInt, Default: "ten"}},
	}
	for name, schema := range bad {
		if err := NewUserPreferenceRegistry().RegisterFields(name, schema); err == nil {
			t.Errorf("%s: schema must be refused", name)
		}
	}
}

func TestUserPreferenceRegistry_RegisterSections(t *testing.T) {
	reg := NewUserPreferenceRegistry()
	err := reg.RegisterSections("people", []ProfileSectionSpec{
		{Key: "people.person", LabelKey: "profile.section", Surfaces: []string{"self", "admin"}, Order: 10},
		{Key: "people.hr", LabelKey: "profile.hr", Permission: "people.update", Surfaces: []string{"admin"}, Order: 5},
	})
	if err != nil {
		t.Fatalf("valid sections refused: %v", err)
	}
	all := reg.Sections()
	if len(all) != 2 || all[0].Key != "people.hr" || all[0].Module != "people" {
		t.Errorf("Sections() = %+v, want hr first (Order 5) with Module stamped", all)
	}
	if self := reg.SectionsFor(ProfileSurfaceSelf); len(self) != 1 || self[0].Key != "people.person" {
		t.Errorf("SectionsFor(self) = %+v", self)
	}
	if admin := reg.SectionsFor(ProfileSurfaceAdmin); len(admin) != 2 {
		t.Errorf("SectionsFor(admin) = %+v", admin)
	}
	// A section with no Surfaces appears on both.
	if err := reg.RegisterSections("crm", []ProfileSectionSpec{{Key: "crm.activity", LabelKey: "x"}}); err != nil {
		t.Fatalf("section without surfaces refused: %v", err)
	}
	if self := reg.SectionsFor(ProfileSurfaceSelf); len(self) != 2 {
		t.Errorf("SectionsFor(self) after crm = %+v", self)
	}

	cases := map[string][]ProfileSectionSpec{
		"wrong prefix": {{Key: "other.x", LabelKey: "l"}},
		"bare prefix":  {{Key: "forms.", LabelKey: "l"}},
		"no label":     {{Key: "forms.x"}},
		"bad surface":  {{Key: "forms.x", LabelKey: "l", Surfaces: []string{"mobile"}}},
		"dup local":    {{Key: "forms.x", LabelKey: "l"}, {Key: "forms.x", LabelKey: "l"}},
	}
	for name, specs := range cases {
		if err := reg.RegisterSections("forms", specs); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
	// A key another module already owns is refused even with the right prefix shape.
	if err := reg.RegisterSections("people", []ProfileSectionSpec{{Key: "people.person", LabelKey: "l"}}); err == nil {
		t.Error("duplicate key across registrations must be refused")
	}
}

func TestValidateValues(t *testing.T) {
	schema := prefSchema()
	ok := map[string]string{"columns": "name,phone", "pageSize": "50", "digest": "weekly", "compact": "true", "refresh": "30m", "initials": "AB"}
	if err := ValidateValues(schema, ok); err != nil {
		t.Fatalf("valid values refused: %v", err)
	}
	// Absent optional fields are fine; required missing is not.
	if err := ValidateValues(schema, map[string]string{"digest": "daily"}); err != nil {
		t.Errorf("optional fields may be absent: %v", err)
	}
	err := ValidateValues(schema, map[string]string{"pageSize": "5", "digest": "", "compact": "yes", "refresh": "soon", "initials": "abcd", "ghost": "1"})
	if err == nil {
		t.Fatal("expected errors")
	}
	errs, isList := err.(ValueErrors)
	if !isList {
		t.Fatalf("error type %T, want ValueErrors", err)
	}
	fields := map[string]string{}
	for _, e := range errs {
		fields[e.Field] = e.Message
	}
	for _, f := range []string{"pageSize", "digest", "compact", "refresh", "initials", "ghost"} {
		if _, found := fields[f]; !found {
			t.Errorf("missing error for %s in %v", f, errs)
		}
	}
	if !strings.Contains(fields["pageSize"], "at least 10") {
		t.Errorf("pageSize message = %q", fields["pageSize"])
	}
	if !strings.Contains(fields["digest"], "required") {
		t.Errorf("digest message = %q", fields["digest"])
	}
	if !strings.Contains(fields["ghost"], "not a declared field") {
		t.Errorf("ghost message = %q", fields["ghost"])
	}
	if ValidateValues(schema, map[string]string{"digest": "monthly"}) == nil {
		t.Error("enum outside options must be refused")
	}
}

type prefTestModule struct {
	minimalModule
	fields   []ConfigField
	sections []ProfileSectionSpec
}

func (m prefTestModule) UserPreferenceFields() []ConfigField   { return m.fields }
func (m prefTestModule) ProfileSections() []ProfileSectionSpec { return m.sections }

func TestModuleRegistryCollectsUserPreferences(t *testing.T) {
	registry := NewModuleRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	registry.initialized = []Module{
		prefTestModule{minimalModule: minimalModule{name: "crm"}, fields: prefSchema(),
			sections: []ProfileSectionSpec{{Key: "crm.activity", LabelKey: "profile.activity"}}},
		// Invalid declarations are isolated to their owner.
		prefTestModule{minimalModule: minimalModule{name: "broken"}, fields: []ConfigField{{Key: "t", Type: FieldSecret}},
			sections: []ProfileSectionSpec{{Key: "other.x", LabelKey: "l"}}},
		minimalModule{name: "forms"},
	}
	reg := NewUserPreferenceRegistry()
	registry.registerUserPreferences(reg)

	if m := reg.Modules(); len(m) != 1 || m[0] != "crm" {
		t.Errorf("Modules() = %v, want [crm]", m)
	}
	if s := reg.Sections(); len(s) != 1 || s[0].Key != "crm.activity" {
		t.Errorf("Sections() = %+v, want crm.activity only", s)
	}
}

// The settings form hides a field whose DependsOn is unmet while
// ValidateValues judges every field, so a conditional field is refused at
// registration instead of becoming a required field nobody can fill.
func TestRegisterFieldsRefusesDependsOn(t *testing.T) {
	reg := NewUserPreferenceRegistry()
	err := reg.RegisterFields("crm", []ConfigField{
		{Key: "digest", Type: FieldBool},
		{Key: "digestDay", Type: FieldString, Required: true,
			DependsOn: []FieldCondition{{Key: "digest", In: []string{"true"}}}},
	})
	if err == nil {
		t.Fatal("a field with dependsOn must be refused")
	}
	if _, ok := reg.Fields("crm"); ok {
		t.Error("a refused schema must not be stored")
	}
}

// A module recovered by RetryInit after a failed boot gets its preference
// schema and profile sections, which InitAll collected only at boot.
func TestRetryInitRegistersUserPreferences(t *testing.T) {
	registry := NewModuleRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	services := NewServiceRegistry()
	reg := NewUserPreferenceRegistry()
	services.Register(ServiceUserPreferenceRegistry, reg)
	registry.deps = &Dependencies{Services: services}
	m := prefTestModule{minimalModule: minimalModule{name: "crm"}, fields: prefSchema(),
		sections: []ProfileSectionSpec{{Key: "crm.activity", LabelKey: "profile.activity"}}}
	registry.moduleByName["crm"] = m
	registry.failed["crm"] = errors.New("boot failure")

	if err := registry.RetryInit("crm"); err != nil {
		t.Fatalf("RetryInit: %v", err)
	}
	if _, ok := reg.Fields("crm"); !ok {
		t.Error("the recovered module's preference schema is missing")
	}
	if s := reg.Sections(); len(s) != 1 || s[0].Key != "crm.activity" {
		t.Errorf("Sections() = %+v, want crm.activity", s)
	}
}

// Durations follow the settings form's grammar: Go durations plus a bare
// "<n>d", in values and in declared defaults alike.
func TestPreferenceDurationsAcceptDays(t *testing.T) {
	schema := []ConfigField{{Key: "window", Type: FieldDuration, Default: "2d"}}
	if err := NewUserPreferenceRegistry().RegisterFields("crm", schema); err != nil {
		t.Fatalf("a 2d default must register: %v", err)
	}
	for _, v := range []string{"2d", "0.5d", "90m", "36h"} {
		if err := ValidateValues(schema, map[string]string{"window": v}); err != nil {
			t.Errorf("%q refused: %v", v, err)
		}
	}
	for _, v := range []string{"1d12h", "d", "two days"} {
		if ValidateValues(schema, map[string]string{"window": v}) == nil {
			t.Errorf("%q accepted", v)
		}
	}
}
