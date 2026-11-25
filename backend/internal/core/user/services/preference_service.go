package services

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// Sentinels the preference handler maps to wire codes.
var (
	// ErrPreferenceModuleUnknown: no module declares preferences under that
	// name, or the module is disabled — the schema is not available, so the
	// store refuses to hold values for it. 404 user.preferences.module_unknown.
	ErrPreferenceModuleUnknown = errors.New("preferences: module declares no preferences or is disabled")
	// ErrPreferenceInvalid wraps a module.ValueErrors: 422 user.preferences.invalid.
	ErrPreferenceInvalid = errors.New("preferences: invalid values")
)

// ModulePreferences is what the API returns for one module: the declared
// fields (so a client can render the form), the user's stored values with
// the declared defaults filled in for absent keys, and when they were last
// saved (zero when the user never saved).
type ModulePreferences struct {
	Module    string               `json:"module"`
	Fields    []module.ConfigField `json:"fields"`
	Values    map[string]string    `json:"values"`
	UpdatedAt *time.Time           `json:"updatedAt,omitempty"`
}

// PreferenceCatalog answers "which modules declare preferences and what are
// their fields" (the SDK registry) and "is this module enabled" (the config
// service). Both are interfaces so the service is testable without boot.
type PreferenceCatalog interface {
	Fields(moduleName string) ([]module.ConfigField, bool)
	Modules() []string
}

// ModuleEnabledChecker reports whether a module is currently enabled.
type ModuleEnabledChecker func(ctx context.Context, moduleName string) bool

// PreferenceService is the per-user, per-module preference store.
type PreferenceService interface {
	// Get returns the module's fields and the user's values (defaults filled).
	Get(ctx context.Context, userUUID, moduleName string) (*ModulePreferences, error)
	// GetAll returns one entry per module that declares preferences and is
	// enabled, sorted by module name.
	GetAll(ctx context.Context, userUUID string) ([]*ModulePreferences, error)
	// Put validates values against the module's fields and replaces the
	// stored document (PUT semantics: absent keys fall back to defaults).
	Put(ctx context.Context, userUUID, moduleName string, values map[string]string) (*ModulePreferences, error)
}

type preferenceService struct {
	repo    repository.PreferenceRepository
	catalog PreferenceCatalog
	enabled ModuleEnabledChecker
}

// NewPreferenceService builds the store. enabled may be nil, in which case
// every module with a schema counts as enabled (tests, minimal boots).
func NewPreferenceService(repo repository.PreferenceRepository, catalog PreferenceCatalog, enabled ModuleEnabledChecker) PreferenceService {
	if enabled == nil {
		enabled = func(context.Context, string) bool { return true }
	}
	return &preferenceService{repo: repo, catalog: catalog, enabled: enabled}
}

// schemaFor resolves the fields of an available module or
// ErrPreferenceModuleUnknown.
func (s *preferenceService) schemaFor(ctx context.Context, moduleName string) ([]module.ConfigField, error) {
	fields, ok := s.catalog.Fields(moduleName)
	if !ok || !s.enabled(ctx, moduleName) {
		return nil, ErrPreferenceModuleUnknown
	}
	return fields, nil
}

// withDefaults returns the stored values with every absent or empty field
// replaced by its declared default (an empty default stays empty).
func withDefaults(fields []module.ConfigField, stored map[string]string) map[string]string {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		if v, ok := stored[f.Key]; ok && v != "" {
			out[f.Key] = v
			continue
		}
		out[f.Key] = f.Default
	}
	return out
}

func (s *preferenceService) build(fields []module.ConfigField, moduleName string, doc *repository.UserPreferenceDoc) *ModulePreferences {
	out := &ModulePreferences{Module: moduleName, Fields: fields}
	if doc != nil {
		out.Values = withDefaults(fields, doc.Values)
		t := doc.UpdatedAt
		out.UpdatedAt = &t
	} else {
		out.Values = withDefaults(fields, nil)
	}
	return out
}

func (s *preferenceService) Get(ctx context.Context, userUUID, moduleName string) (*ModulePreferences, error) {
	fields, err := s.schemaFor(ctx, moduleName)
	if err != nil {
		return nil, err
	}
	doc, err := s.repo.Get(ctx, userUUID, moduleName)
	if err != nil {
		return nil, err
	}
	return s.build(fields, moduleName, doc), nil
}

func (s *preferenceService) GetAll(ctx context.Context, userUUID string) ([]*ModulePreferences, error) {
	docs, err := s.repo.ListByUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	byModule := map[string]*repository.UserPreferenceDoc{}
	for _, d := range docs {
		byModule[d.Module] = d
	}
	names := s.catalog.Modules()
	sort.Strings(names)
	out := make([]*ModulePreferences, 0, len(names))
	for _, name := range names {
		fields, err := s.schemaFor(ctx, name)
		if err != nil {
			continue // disabled: not shown
		}
		out = append(out, s.build(fields, name, byModule[name]))
	}
	return out, nil
}

func (s *preferenceService) Put(ctx context.Context, userUUID, moduleName string, values map[string]string) (*ModulePreferences, error) {
	fields, err := s.schemaFor(ctx, moduleName)
	if err != nil {
		return nil, err
	}
	if values == nil {
		values = map[string]string{}
	}
	if err := module.ValidateValues(fields, values); err != nil {
		return nil, errors.Join(ErrPreferenceInvalid, err)
	}
	doc, err := s.repo.Upsert(ctx, userUUID, moduleName, values)
	if err != nil {
		return nil, err
	}
	return s.build(fields, moduleName, doc), nil
}
