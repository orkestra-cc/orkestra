package module

import (
	"strings"
	"testing"
)

func TestValidateConfigDeclarations_Valid(t *testing.T) {
	groups := []ConfigGroup{
		{Key: "oauth", Label: "OAuth Providers"},
		{Key: "oauth.google", Label: "Google", Parent: "oauth"},
	}
	schema := []ConfigField{
		{Key: "googleEnabled", Label: "Enable Google", Group: "oauth", Type: FieldBool},
		{Key: "googleClientId", Label: "Client ID", Group: "oauth.google", Type: FieldString,
			DependsOn: []FieldCondition{{Key: "googleEnabled", In: []string{"true"}}}},
	}
	if err := ValidateConfigDeclarations(schema, groups); err != nil {
		t.Errorf("ValidateConfigDeclarations = %v, want nil", err)
	}
}

func TestValidateConfigDeclarations_LegacyUngroupedModule(t *testing.T) {
	// No ConfigGroups() declared: Group values are legacy display labels and
	// must not be validated as references. This is the state of every module
	// in the tree before its migration, and of every un-migrated fork addon.
	schema := []ConfigField{
		{Key: "passwordMinLength", Label: "Minimum length", Group: "Password Policy", Type: FieldInt},
	}
	if err := ValidateConfigDeclarations(schema, nil); err != nil {
		t.Errorf("ValidateConfigDeclarations with no groups = %v, want nil", err)
	}
}

func TestValidateConfigDeclarations_UndeclaredGroup(t *testing.T) {
	groups := []ConfigGroup{{Key: "oauth", Label: "OAuth Providers"}}
	schema := []ConfigField{
		{Key: "googleClientId", Label: "Client ID", Group: "oauth.googel", Type: FieldString},
	}
	err := ValidateConfigDeclarations(schema, groups)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for the typo'd group")
	}
	if !strings.Contains(err.Error(), "oauth.googel") {
		t.Errorf("error %q does not name the offending group", err)
	}
}

func TestValidateConfigDeclarations_UndeclaredDependsOnKey(t *testing.T) {
	schema := []ConfigField{
		{Key: "googleClientId", Label: "Client ID", Type: FieldString,
			DependsOn: []FieldCondition{{Key: "googleEnabld", In: []string{"true"}}}},
	}
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for the typo'd DependsOn key")
	}
	if !strings.Contains(err.Error(), "googleEnabld") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

func TestValidateConfigDeclarations_EmptyInList(t *testing.T) {
	// An empty In can never be satisfied, so the field would be permanently
	// invisible — always a mistake, never an intent.
	schema := []ConfigField{
		{Key: "a", Label: "A", Type: FieldBool},
		{Key: "b", Label: "B", Type: FieldString, DependsOn: []FieldCondition{{Key: "a"}}},
	}
	if err := ValidateConfigDeclarations(schema, nil); err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for an empty In list")
	}
}

func TestValidateConfigDeclarations_UngroupedFieldInGroupedModule(t *testing.T) {
	// A module that declares groups has opted into the sectioned rail, so a
	// field with no Group has no panel to render in — it silently disappears
	// from the admin UI. This is the shape a hand-migration typo takes.
	groups := []ConfigGroup{{Key: "oauth", Label: "OAuth Providers"}}
	schema := []ConfigField{
		{Key: "googleEnabled", Label: "Enable Google", Group: "oauth", Type: FieldBool},
		{Key: "orphanedField", Label: "Forgotten", Type: FieldString},
	}
	err := ValidateConfigDeclarations(schema, groups)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for the ungrouped field")
	}
	if !strings.Contains(err.Error(), "orphanedField") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

func TestValidateConfigDeclarations_EnumConditionValueNotAnOption(t *testing.T) {
	schema := []ConfigField{
		{Key: "provider", Label: "Provider", Type: FieldEnum, Options: []string{"noop", "smtp"}},
		{Key: "smtpHost", Label: "SMTP host", Type: FieldString,
			DependsOn: []FieldCondition{{Key: "provider", In: []string{"smtps"}}}},
	}
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for an In value outside Options")
	}
	if !strings.Contains(err.Error(), "smtps") {
		t.Errorf("error %q does not name the offending value", err)
	}
}

func TestValidateConfigDeclarations_EnumConditionValueMatchesCaseInsensitively(t *testing.T) {
	// "SMTP" is the same option as "smtp" under the matching contract, so it
	// must not be reported — the rule catches typos, not casing.
	schema := []ConfigField{
		{Key: "provider", Label: "Provider", Type: FieldEnum, Options: []string{"noop", "smtp"}},
		{Key: "smtpHost", Label: "SMTP host", Type: FieldString,
			DependsOn: []FieldCondition{{Key: "provider", In: []string{"SMTP"}}}},
	}
	if err := ValidateConfigDeclarations(schema, nil); err != nil {
		t.Errorf("ValidateConfigDeclarations = %v, want nil for a case-differing enum option", err)
	}
}

func TestValidateConfigDeclarations_BoolConditionValue(t *testing.T) {
	// "1" is a legal bool literal under the parseBool-shaped matching
	// contract; "enabled" is not and would evaluate false forever.
	schema := []ConfigField{
		{Key: "autoCleanup", Label: "Auto cleanup", Type: FieldBool},
		{Key: "retentionDays", Label: "Retention", Type: FieldInt,
			DependsOn: []FieldCondition{{Key: "autoCleanup", In: []string{"1"}}}},
	}
	if err := ValidateConfigDeclarations(schema, nil); err != nil {
		t.Errorf("ValidateConfigDeclarations = %v, want nil for In: [\"1\"] against a bool", err)
	}

	schema[1].DependsOn = []FieldCondition{{Key: "autoCleanup", In: []string{"enabled"}}}
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for a non-boolean In value")
	}
	if !strings.Contains(err.Error(), "enabled") {
		t.Errorf("error %q does not name the offending value", err)
	}
}

func TestValidateConfigDeclarations_UncompilablePattern(t *testing.T) {
	// The pattern is handed to the admin UI's new RegExp(); a broken one
	// throws inside a render path rather than failing here.
	schema := []ConfigField{
		{Key: "slug", Label: "Slug", Type: FieldString, Pattern: "^[a-z"},
	}
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for an uncompilable Pattern")
	}
	if !strings.Contains(err.Error(), "slug") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

func TestValidateConfigDeclarations_InvertedMinMax(t *testing.T) {
	min, max := 128, 8
	schema := []ConfigField{
		{Key: "passwordMinLength", Label: "Minimum length", Type: FieldInt, Min: &min, Max: &max},
	}
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for Min > Max")
	}
	if !strings.Contains(err.Error(), "passwordMinLength") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

func TestValidateConfigDeclarations_DuplicateFieldKey(t *testing.T) {
	// Config values are a flat map keyed by ConfigField.Key, so the second
	// declaration silently wins and the first field's edits go nowhere.
	schema := []ConfigField{
		{Key: "smtpPort", Label: "SMTP port", Type: FieldInt},
		{Key: "smtpPort", Label: "SMTP port (copy-paste)", Type: FieldString},
	}
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for a duplicate field key")
	}
	if !strings.Contains(err.Error(), "smtpPort") {
		t.Errorf("error %q does not name the duplicated key", err)
	}
}

func TestValidateConfigDeclarations_ParentCycle(t *testing.T) {
	groups := []ConfigGroup{
		{Key: "a", Label: "A", Parent: "b"},
		{Key: "b", Label: "B", Parent: "a"},
	}
	err := ValidateConfigDeclarations(nil, groups)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for a Parent cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention a cycle", err)
	}
}

func TestValidateConfigDeclarations_UndeclaredParentAndDuplicateKey(t *testing.T) {
	groups := []ConfigGroup{
		{Key: "child", Label: "Child", Parent: "ghost"},
		{Key: "child", Label: "Child again"},
	}
	err := ValidateConfigDeclarations(nil, groups)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want errors")
	}
	for _, want := range []string{"ghost", "duplicate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
