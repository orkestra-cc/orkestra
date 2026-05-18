package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
)

// ErrCustomFieldValidation is returned when a record's custom_fields
// bag fails the schema check. The error message names the failing
// field so the handler can surface it directly to the user.
var ErrCustomFieldValidation = errors.New("marketing: custom field validation failed")

// CustomFieldService owns schema upsert + per-record validation.
// Repositories store raw bags; this service is the canonical gate
// every write path consults before persistence.
type CustomFieldService struct {
	repo *repository.CustomFieldSchemaRepository
}

// NewCustomFieldService binds a service to the schema repository.
func NewCustomFieldService(repo *repository.CustomFieldSchemaRepository) *CustomFieldService {
	return &CustomFieldService{repo: repo}
}

// Upsert creates or replaces the schema for (tenant, target). The
// UUID is auto-assigned on first create — subsequent upserts keep
// the original UUID via the repo's $setOnInsert. Version is
// incremented by the repository.
func (s *CustomFieldService) Upsert(ctx context.Context, target models.CustomFieldTarget, fields []models.FieldDef, allowUnknown bool, updatedBy string) (*models.CustomFieldSchema, error) {
	if err := validateFieldDefs(fields); err != nil {
		return nil, err
	}
	doc := &models.CustomFieldSchema{
		UUID:               uuid.New().String(),
		TargetCollection:   target,
		Fields:             fields,
		AllowUnknownFields: allowUnknown,
		UpdatedBy:          updatedBy,
	}
	if err := s.repo.Upsert(ctx, doc); err != nil {
		return nil, err
	}
	return s.repo.GetForTarget(ctx, target)
}

// GetForTarget returns the active schema for the given target, or
// nil when no schema is configured. Returning nil instead of an
// error keeps the validation call site trivial: nil schema means
// "no constraints, accept whatever bag the caller provided".
func (s *CustomFieldService) GetForTarget(ctx context.Context, target models.CustomFieldTarget) (*models.CustomFieldSchema, error) {
	got, err := s.repo.GetForTarget(ctx, target)
	if err != nil {
		if errors.Is(err, repository.ErrCustomFieldSchemaNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return got, nil
}

// List returns every schema configured for the caller's tenant.
// Typically two rows max (persons + organizations).
func (s *CustomFieldService) List(ctx context.Context) ([]models.CustomFieldSchema, error) {
	return s.repo.List(ctx)
}

// Delete removes the schema for (tenant, target). Existing Person /
// Organization records keep their bag intact — they just stop being
// validated. Returns ErrCustomFieldValidation when no row exists
// (mapping to 404 at the handler).
func (s *CustomFieldService) Delete(ctx context.Context, target models.CustomFieldTarget) error {
	return s.repo.Delete(ctx, target)
}

// Validate checks a record's custom_fields bag against the schema
// for (tenant, target). When no schema is configured the call is a
// no-op — that matches the new-tenant onboarding flow where contacts
// can land before the schema is set up.
func (s *CustomFieldService) Validate(ctx context.Context, target models.CustomFieldTarget, fields map[string]any) error {
	schema, err := s.GetForTarget(ctx, target)
	if err != nil {
		return err
	}
	if schema == nil {
		return nil
	}
	return ValidateBag(schema, fields)
}

// ValidateBag is the pure-function half of Validate — exposed so
// callers that already hold the schema (importer batch validation)
// can skip the round-trip per row. Returns wrapped
// ErrCustomFieldValidation on any failure; the message names the
// failing key.
func ValidateBag(schema *models.CustomFieldSchema, bag map[string]any) error {
	if schema == nil {
		return nil
	}
	defs := make(map[string]models.FieldDef, len(schema.Fields))
	for _, f := range schema.Fields {
		defs[f.Key] = f
	}

	// Required-field presence check.
	for _, f := range schema.Fields {
		if !f.Required {
			continue
		}
		if _, ok := bag[f.Key]; !ok {
			return wrapValidation("missing required field %q", f.Key)
		}
	}

	for k, v := range bag {
		def, known := defs[k]
		if !known {
			if schema.AllowUnknownFields {
				continue
			}
			return wrapValidation("unknown field %q", k)
		}
		if err := validateValue(def, v); err != nil {
			return wrapValidation("field %q: %s", k, err.Error())
		}
	}
	return nil
}

func wrapValidation(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCustomFieldValidation, fmt.Sprintf(format, args...))
}

// validateFieldDefs sanity-checks a schema before upsert: keys must
// be non-empty, types must be known, enum/multi-enum must declare
// options.
func validateFieldDefs(defs []models.FieldDef) error {
	seen := make(map[string]bool, len(defs))
	for _, f := range defs {
		if f.Key == "" {
			return wrapValidation("field has empty key")
		}
		if seen[f.Key] {
			return wrapValidation("duplicate field key %q", f.Key)
		}
		seen[f.Key] = true
		if !isKnownFieldType(f.Type) {
			return wrapValidation("field %q: unknown type %q", f.Key, f.Type)
		}
		if (f.Type == models.FieldTypeEnum || f.Type == models.FieldTypeMultiEnum) && len(f.Options) == 0 {
			return wrapValidation("field %q: %s requires options", f.Key, f.Type)
		}
	}
	return nil
}

func isKnownFieldType(t models.CustomFieldType) bool {
	switch t {
	case models.FieldTypeString, models.FieldTypeInt, models.FieldTypeFloat,
		models.FieldTypeBool, models.FieldTypeDate, models.FieldTypeDateTime,
		models.FieldTypeEnum, models.FieldTypeMultiEnum:
		return true
	}
	// "ref:<collection>" form.
	return strings.HasPrefix(string(t), string(models.FieldTypeRef)+":")
}

// validateValue applies the per-type check to one bag entry. Returns
// a bare error — the caller wraps it with the field key.
func validateValue(def models.FieldDef, v any) error {
	if v == nil {
		if def.Required {
			return fmt.Errorf("value is null")
		}
		return nil
	}
	switch def.Type {
	case models.FieldTypeString:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("expected string, got %T", v)
		}
	case models.FieldTypeInt:
		switch v.(type) {
		case int, int32, int64, float64: // JSON numbers decode as float64
		default:
			return fmt.Errorf("expected int, got %T", v)
		}
	case models.FieldTypeFloat:
		switch v.(type) {
		case float32, float64, int, int32, int64:
		default:
			return fmt.Errorf("expected float, got %T", v)
		}
	case models.FieldTypeBool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected bool, got %T", v)
		}
	case models.FieldTypeDate, models.FieldTypeDateTime:
		switch x := v.(type) {
		case string:
			if _, err := time.Parse(time.RFC3339, x); err != nil {
				if _, err2 := time.Parse("2006-01-02", x); err2 != nil {
					return fmt.Errorf("expected RFC3339 or YYYY-MM-DD date string")
				}
			}
		case time.Time:
			// ok
		default:
			return fmt.Errorf("expected date string, got %T", v)
		}
	case models.FieldTypeEnum:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected enum string, got %T", v)
		}
		if !optionAllowed(def.Options, s) {
			return fmt.Errorf("value %q is not an allowed option", s)
		}
	case models.FieldTypeMultiEnum:
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("expected array of strings, got %T", v)
		}
		for _, e := range arr {
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("expected enum string in array, got %T", e)
			}
			if !optionAllowed(def.Options, s) {
				return fmt.Errorf("value %q is not an allowed option", s)
			}
		}
	default:
		// ref:<collection> — value is a UUID string. Existence of the
		// referenced row is not checked here; the caller may opt to
		// validate via the target repo if dangling refs are a concern.
		if strings.HasPrefix(string(def.Type), string(models.FieldTypeRef)+":") {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("expected reference UUID string, got %T", v)
			}
		}
	}
	return nil
}

func optionAllowed(opts []models.FieldOption, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}
