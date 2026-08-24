package module

import (
	"strings"
	"testing"
)

func declErr(t *testing.T, f ConfigField) string {
	t.Helper()
	err := ValidateConfigDeclarations([]ConfigField{f}, nil)
	if err == nil {
		t.Fatalf("expected a declaration error for %+v", f)
	}
	return err.Error()
}

func TestItemsRequiredOnRecordList(t *testing.T) {
	if got := declErr(t, ConfigField{Key: "p", Label: "P", Type: FieldRecordList}); !strings.Contains(got, "items") {
		t.Fatalf("error should mention items: %s", got)
	}
}

func TestItemsRejectedOnScalar(t *testing.T) {
	f := ConfigField{Key: "p", Label: "P", Type: FieldString,
		Items: []ConfigItemField{{Key: "a", Label: "A", Type: FieldString}}}
	if got := declErr(t, f); !strings.Contains(got, "items") {
		t.Fatalf("error should mention items: %s", got)
	}
}

func TestSubFieldMayNotDeclareTheListType(t *testing.T) {
	f := ConfigField{Key: "p", Label: "P", Type: FieldRecordList,
		Items: []ConfigItemField{{Key: "a", Label: "A", Type: FieldRecordList}}}
	if got := declErr(t, f); !strings.Contains(got, "recordList") {
		t.Fatalf("error should reject a nested list: %s", got)
	}
}

func TestSubFieldKeysMustBeUniqueAndUnreserved(t *testing.T) {
	dup := ConfigField{Key: "p", Label: "P", Type: FieldRecordList,
		Items: []ConfigItemField{
			{Key: "a", Label: "A", Type: FieldString},
			{Key: "a", Label: "A2", Type: FieldString},
		}}
	if got := declErr(t, dup); !strings.Contains(got, "duplicate") {
		t.Fatalf("error should reject duplicates: %s", got)
	}

	reserved := ConfigField{Key: "p", Label: "P", Type: FieldRecordList,
		Items: []ConfigItemField{{Key: "__label", Label: "L", Type: FieldString}}}
	if got := declErr(t, reserved); !strings.Contains(got, "__") {
		t.Fatalf("error should reject the reserved prefix: %s", got)
	}
}

func TestItemConditionMayOnlyReferenceSiblings(t *testing.T) {
	f := ConfigField{Key: "p", Label: "P", Type: FieldRecordList,
		Items: []ConfigItemField{
			{Key: "host", Label: "H", Type: FieldString,
				DependsOn: []FieldCondition{{Key: "somethingOutside", In: []string{"x"}}}},
		}}
	if got := declErr(t, f); !strings.Contains(got, "sibling") {
		t.Fatalf("error should reject a cross-element condition: %s", got)
	}
}

func TestValidRecordListDeclarationPasses(t *testing.T) {
	f := ConfigField{Key: "email.profiles", Label: "Profiles", Type: FieldRecordList,
		Items: []ConfigItemField{
			{Key: "provider", Label: "Provider", Type: FieldEnum, Options: []string{"smtp", "noop"}},
			{Key: "host", Label: "Host", Type: FieldString, Required: true,
				DependsOn: []FieldCondition{{Key: "provider", In: []string{"smtp"}}}},
		}}
	if err := ValidateConfigDeclarations([]ConfigField{f}, nil); err != nil {
		t.Fatalf("valid declaration rejected: %v", err)
	}
}
