package module

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Profile sections ------------------------------------------------------
//
// A module can contribute a section to the user profile pages (the user's
// own profile, the admin's view of a user, the user's settings). The SPA
// renders contributions through its slot system (frontend-admin
// `src/modules`, slot ids `user.profile.sections`, `user.adminProfile.sections`,
// `user.settings.sections`); the backend keeps the declarative list so other
// clients (the mobile app, integrations) can discover the sections a user
// has, filtered by module state and the caller's permissions:
// GET /v1/me/profile-sections and GET /v1/users/{id}/profile-sections.

// ProfileSurfaceSelf is the user's own profile and settings pages;
// ProfileSurfaceAdmin is an administrator's view of another user.
const (
	ProfileSurfaceSelf  = "self"
	ProfileSurfaceAdmin = "admin"
)

// ProfileSectionSpec declares one section a module contributes to the
// profile pages. Mirrors the SPA's SlotContribution: Key is unique app-wide
// and MUST be prefixed `<module name>.`; LabelKey is an i18n key in the
// module's own namespace; Permission, when set, hides the section from a
// caller who lacks it. Surfaces lists where the section appears ("self",
// "admin"; empty means both). Order sorts within a surface (then by Key).
type ProfileSectionSpec struct {
	Key        string   `json:"key"`
	Module     string   `json:"module"`
	LabelKey   string   `json:"labelKey"`
	Permission string   `json:"permission,omitempty"`
	Surfaces   []string `json:"surfaces,omitempty"`
	Order      int      `json:"order,omitempty"`
}

// HasProfileSections lets a module declare the profile sections it
// contributes. The registry collects them after Init; invalid declarations
// are rejected for the owning module only.
type HasProfileSections interface {
	ProfileSections() []ProfileSectionSpec
}

// ProfileSectionsOf returns the module's profile sections, or nil.
func ProfileSectionsOf(m Module) []ProfileSectionSpec {
	if p, ok := m.(HasProfileSections); ok {
		return p.ProfileSections()
	}
	return nil
}

// --- Per-user preferences --------------------------------------------------
//
// A module can declare per-user preferences with the same ConfigField
// vocabulary the admin config uses (flat key → string values with a type,
// default, options, min/max, pattern). The user module stores one document
// per (user, module) and serves them at GET/PUT /v1/me/preferences/{module};
// the SPA renders the declared fields with the generic config form, so a
// module ships no component for its settings. Values are personal
// presentation choices; nothing here is read by the module's own backend
// paths unless it asks the store.

// HasUserPreferences lets a module declare its per-user preference fields.
// Allowed types: string, bool, int, duration, enum, stringList. Secret and
// recordList fields are refused (secrets are not preferences; record lists
// need the roster machinery the admin config owns).
type HasUserPreferences interface {
	UserPreferenceFields() []ConfigField
}

// UserPreferenceFieldsOf returns the module's declared preference fields,
// or nil.
func UserPreferenceFieldsOf(m Module) []ConfigField {
	if p, ok := m.(HasUserPreferences); ok {
		return p.UserPreferenceFields()
	}
	return nil
}

// UserPreferenceRegistry is the boot-time catalog of preference schemas and
// profile sections, keyed by module. Populated by the module registry after
// Init (registerUserPreferences) and read by the user module's handlers.
// Registered under ServiceUserPreferenceRegistry.
type UserPreferenceRegistry struct {
	mu       sync.RWMutex
	fields   map[string][]ConfigField
	sections []ProfileSectionSpec
}

// NewUserPreferenceRegistry returns an empty registry.
func NewUserPreferenceRegistry() *UserPreferenceRegistry {
	return &UserPreferenceRegistry{fields: map[string][]ConfigField{}}
}

var preferenceKeyRE = regexp.MustCompile(`^[a-z][a-zA-Z0-9_.]*$`)

// RegisterFields validates and stores a module's preference schema. A
// second registration for the same module is refused.
func (r *UserPreferenceRegistry) RegisterFields(moduleName string, fields []ConfigField) error {
	if moduleName == "" {
		return fmt.Errorf("user preferences: empty module name")
	}
	if len(fields) == 0 {
		return fmt.Errorf("user preferences %q: no fields declared", moduleName)
	}
	seen := map[string]bool{}
	for _, f := range fields {
		switch {
		case f.Key == "" || !preferenceKeyRE.MatchString(f.Key):
			return fmt.Errorf("user preferences %q: field key %q is not a valid key", moduleName, f.Key)
		case seen[f.Key]:
			return fmt.Errorf("user preferences %q: duplicate field %q", moduleName, f.Key)
		}
		seen[f.Key] = true
		switch f.Type {
		case FieldString, FieldBool, FieldInt, FieldDuration, FieldEnum, FieldStringList:
		case FieldSecret, FieldRecordList:
			return fmt.Errorf("user preferences %q: field %q has type %q, not allowed in preferences", moduleName, f.Key, f.Type)
		default:
			return fmt.Errorf("user preferences %q: field %q has unknown type %q", moduleName, f.Key, f.Type)
		}
		if f.Type == FieldEnum && len(f.Options) == 0 {
			return fmt.Errorf("user preferences %q: enum field %q declares no options", moduleName, f.Key)
		}
		// The settings form hides a field whose DependsOn is unmet, while
		// ValidateValues judges every field: a hidden required field could
		// never be filled, so conditional fields are refused here.
		if len(f.DependsOn) > 0 {
			return fmt.Errorf("user preferences %q: field %q declares dependsOn, not supported in preferences", moduleName, f.Key)
		}
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				return fmt.Errorf("user preferences %q: field %q pattern: %w", moduleName, f.Key, err)
			}
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return fmt.Errorf("user preferences %q: field %q has min > max", moduleName, f.Key)
		}
		if f.Default != "" {
			if err := validateFieldValue(f, f.Default); err != nil {
				return fmt.Errorf("user preferences %q: field %q default: %w", moduleName, f.Key, err)
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fields[moduleName]; exists {
		return fmt.Errorf("user preferences %q: already registered", moduleName)
	}
	out := make([]ConfigField, len(fields))
	copy(out, fields)
	r.fields[moduleName] = out
	return nil
}

// Fields returns the declared schema of a module, or false when the module
// declares no preferences.
func (r *UserPreferenceRegistry) Fields(moduleName string) ([]ConfigField, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.fields[moduleName]
	if !ok {
		return nil, false
	}
	out := make([]ConfigField, len(f))
	copy(out, f)
	return out, true
}

// Modules returns the names of the modules with a preference schema, sorted.
func (r *UserPreferenceRegistry) Modules() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.fields))
	for name := range r.fields {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// RegisterSections validates and stores a module's profile sections. Every
// key must be prefixed `<module>.` and be unique across modules; surfaces
// must be "self" or "admin".
func (r *UserPreferenceRegistry) RegisterSections(moduleName string, specs []ProfileSectionSpec) error {
	if moduleName == "" {
		return fmt.Errorf("profile sections: empty module name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := map[string]string{}
	for _, s := range r.sections {
		existing[s.Key] = s.Module
	}
	local := map[string]bool{}
	for _, s := range specs {
		if !strings.HasPrefix(s.Key, moduleName+".") || len(s.Key) == len(moduleName)+1 {
			return fmt.Errorf("profile sections %q: key %q must be prefixed %q", moduleName, s.Key, moduleName+".")
		}
		if s.LabelKey == "" {
			return fmt.Errorf("profile sections %q: section %q has no labelKey", moduleName, s.Key)
		}
		if owner, dup := existing[s.Key]; dup {
			return fmt.Errorf("profile sections %q: key %q already registered by %q", moduleName, s.Key, owner)
		}
		if local[s.Key] {
			return fmt.Errorf("profile sections %q: duplicate key %q", moduleName, s.Key)
		}
		local[s.Key] = true
		for _, surface := range s.Surfaces {
			if surface != ProfileSurfaceSelf && surface != ProfileSurfaceAdmin {
				return fmt.Errorf("profile sections %q: section %q has unknown surface %q", moduleName, s.Key, surface)
			}
		}
	}
	for _, s := range specs {
		s.Module = moduleName
		r.sections = append(r.sections, s)
	}
	sort.SliceStable(r.sections, func(i, j int) bool {
		if r.sections[i].Order != r.sections[j].Order {
			return r.sections[i].Order < r.sections[j].Order
		}
		return r.sections[i].Key < r.sections[j].Key
	})
	return nil
}

// Sections returns every registered section, sorted by Order then Key. The
// caller filters by surface, module state and permission.
func (r *UserPreferenceRegistry) Sections() []ProfileSectionSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProfileSectionSpec, len(r.sections))
	copy(out, r.sections)
	return out
}

// SectionsFor returns the sections shown on a surface ("self" or "admin").
func (r *UserPreferenceRegistry) SectionsFor(surface string) []ProfileSectionSpec {
	var out []ProfileSectionSpec
	for _, s := range r.Sections() {
		if len(s.Surfaces) == 0 {
			out = append(out, s)
			continue
		}
		for _, sf := range s.Surfaces {
			if sf == surface {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// --- Value validation ------------------------------------------------------

// ValueError reports one invalid submitted value. Field is the schema key.
type ValueError struct {
	Field   string
	Message string
}

func (e *ValueError) Error() string { return e.Field + ": " + e.Message }

// ValueErrors is every problem found by ValidateValues, in schema order then
// unknown keys sorted — collected together so a form can show them all.
type ValueErrors []*ValueError

func (es ValueErrors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

// ValidateValues checks a flat key → value map against a declared schema:
// unknown keys are refused, required fields must be present and non-empty,
// and every present value must satisfy its field's type, options, min/max
// and pattern. Absent optional fields are fine (the default applies on
// read). Returns nil or a ValueErrors listing every problem.
func ValidateValues(schema []ConfigField, values map[string]string) error {
	var errs ValueErrors
	declared := map[string]bool{}
	for _, f := range schema {
		declared[f.Key] = true
		v, present := values[f.Key]
		if !present || v == "" {
			if f.Required {
				errs = append(errs, &ValueError{Field: f.Key, Message: "is required"})
			}
			continue
		}
		if err := validateFieldValue(f, v); err != nil {
			errs = append(errs, &ValueError{Field: f.Key, Message: err.Error()})
		}
	}
	var unknown []string
	for k := range values {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		errs = append(errs, &ValueError{Field: k, Message: "is not a declared field"})
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validateFieldValue checks one non-empty value against its field.
func validateFieldValue(f ConfigField, v string) error {
	switch f.Type {
	case FieldBool:
		if v != "true" && v != "false" {
			return fmt.Errorf("must be true or false")
		}
	case FieldInt:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("must be an integer")
		}
		if f.Min != nil && n < *f.Min {
			return fmt.Errorf("must be at least %d", *f.Min)
		}
		if f.Max != nil && n > *f.Max {
			return fmt.Errorf("must be at most %d", *f.Max)
		}
	case FieldDuration:
		if _, ok := parseHumanDuration(v); !ok {
			return fmt.Errorf("must be a duration such as 30m, 2h or 7d")
		}
	case FieldEnum:
		ok := false
		for _, o := range f.Options {
			if o == v {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("must be one of %s", strings.Join(f.Options, ", "))
		}
	case FieldString, FieldStringList:
		if f.Min != nil && len(v) < *f.Min {
			return fmt.Errorf("must be at least %d characters", *f.Min)
		}
		if f.Max != nil && len(v) > *f.Max {
			return fmt.Errorf("must be at most %d characters", *f.Max)
		}
		if f.Pattern != "" {
			re, err := regexp.Compile(f.Pattern)
			if err != nil {
				return fmt.Errorf("has an invalid pattern")
			}
			if !re.MatchString(v) {
				return fmt.Errorf("does not match the required pattern")
			}
		}
	}
	return nil
}

// parseHumanDuration accepts what time.ParseDuration does plus a bare
// "<number>d" for days — the same grammar the settings form and the
// backend's utils.ParseDuration accept, kept here so the SDK imports no
// backend package. Compound forms ("1d12h") are refused, not half-read.
func parseHumanDuration(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(n * float64(24*time.Hour)), true
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false
	}
	return d, true
}
