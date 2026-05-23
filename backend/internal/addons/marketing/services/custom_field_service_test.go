package services

import (
	"errors"
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// TestValidateBag covers the schema-validation matrix end-to-end.
// ValidateBag is the pure function; the service Validate wraps it
// with a Mongo round-trip to fetch the schema, which is exercised
// implicitly through handlers in integration tests later.
func TestValidateBag(t *testing.T) {
	schema := &models.CustomFieldSchema{
		TargetCollection: models.CustomFieldTargetPersons,
		Fields: []models.FieldDef{
			{Key: "first", Type: models.FieldTypeString, Required: true},
			{Key: "age", Type: models.FieldTypeInt},
			{Key: "active", Type: models.FieldTypeBool},
			{Key: "joined", Type: models.FieldTypeDate},
			{Key: "tier", Type: models.FieldTypeEnum, Options: []models.FieldOption{
				{Value: "gold"}, {Value: "silver"},
			}},
			{Key: "interests", Type: models.FieldTypeMultiEnum, Options: []models.FieldOption{
				{Value: "music"}, {Value: "tech"}, {Value: "sport"},
			}},
		},
	}

	cases := []struct {
		name    string
		bag     map[string]any
		wantErr bool
	}{
		{name: "minimum required satisfied", bag: map[string]any{"first": "ok"}, wantErr: false},
		{name: "missing required rejected", bag: map[string]any{}, wantErr: true},
		{name: "int as float64 ok (JSON decoding)", bag: map[string]any{"first": "x", "age": float64(42)}, wantErr: false},
		{name: "int as string rejected", bag: map[string]any{"first": "x", "age": "42"}, wantErr: true},
		{name: "bool ok", bag: map[string]any{"first": "x", "active": true}, wantErr: false},
		{name: "bool as string rejected", bag: map[string]any{"first": "x", "active": "true"}, wantErr: true},
		{name: "date RFC3339 ok", bag: map[string]any{"first": "x", "joined": "2026-04-10T09:23:00Z"}, wantErr: false},
		{name: "date short form ok", bag: map[string]any{"first": "x", "joined": "2026-04-10"}, wantErr: false},
		{name: "date garbage rejected", bag: map[string]any{"first": "x", "joined": "yesterday"}, wantErr: true},
		{name: "enum allowed ok", bag: map[string]any{"first": "x", "tier": "gold"}, wantErr: false},
		{name: "enum not in options rejected", bag: map[string]any{"first": "x", "tier": "platinum"}, wantErr: true},
		{name: "multi_enum subset ok", bag: map[string]any{"first": "x", "interests": []any{"music", "tech"}}, wantErr: false},
		{name: "multi_enum bad entry rejected", bag: map[string]any{"first": "x", "interests": []any{"music", "art"}}, wantErr: true},
		{name: "unknown field rejected when AllowUnknownFields=false", bag: map[string]any{"first": "x", "rogue": 1}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateBag(schema, c.bag)
			if c.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if err != nil && !errors.Is(err, ErrCustomFieldValidation) {
				t.Fatalf("error not wrapped with ErrCustomFieldValidation: %v", err)
			}
		})
	}
}

// TestValidateBagAllowsUnknownWhenConfigured exercises the explicit
// opt-out from strict validation — useful when a tenant is iterating
// on its schema and wants to capture raw imported data before
// formalising every field.
func TestValidateBagAllowsUnknownWhenConfigured(t *testing.T) {
	schema := &models.CustomFieldSchema{
		AllowUnknownFields: true,
		Fields:             []models.FieldDef{{Key: "first", Type: models.FieldTypeString}},
	}
	bag := map[string]any{"first": "ok", "rogue": "anything goes"}
	if err := ValidateBag(schema, bag); err != nil {
		t.Fatalf("expected nil error with AllowUnknownFields, got %v", err)
	}
}

// TestValidateBagNilSchemaPasses verifies the call site's "no schema
// configured yet" path: until a tenant configures custom fields the
// bag flows through untouched, which is the new-tenant onboarding
// behavior we want.
func TestValidateBagNilSchemaPasses(t *testing.T) {
	if err := ValidateBag(nil, map[string]any{"anything": 123}); err != nil {
		t.Fatalf("expected nil error with nil schema, got %v", err)
	}
}
