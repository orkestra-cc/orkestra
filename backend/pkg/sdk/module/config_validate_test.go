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
			DependsOn: []Condition{{Key: "googleEnabled", In: []string{"true"}}}},
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
			DependsOn: []Condition{{Key: "googleEnabld", In: []string{"true"}}}},
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
		{Key: "b", Label: "B", Type: FieldString, DependsOn: []Condition{{Key: "a"}}},
	}
	if err := ValidateConfigDeclarations(schema, nil); err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for an empty In list")
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
