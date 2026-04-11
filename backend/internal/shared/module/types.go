package module

import "time"

// ModuleCategory classifies a module's activation behavior.
type ModuleCategory string

const (
	// CategoryCore modules are always active and cannot be disabled.
	CategoryCore ModuleCategory = "core"
	// CategoryToggleable modules can be enabled/disabled via admin UI (no external deps).
	CategoryToggleable ModuleCategory = "toggleable"
	// CategoryExternal modules require external service credentials to function.
	CategoryExternal ModuleCategory = "external"
)

// ConfigFieldType defines the data type for a module configuration field.
type ConfigFieldType string

const (
	FieldString   ConfigFieldType = "string"
	FieldBool     ConfigFieldType = "bool"
	FieldInt      ConfigFieldType = "int"
	FieldDuration ConfigFieldType = "duration"
	FieldSecret   ConfigFieldType = "secret" // encrypted at rest with AES-256-GCM
)

// ConfigField describes a single configurable setting for a module.
// The admin UI renders forms from these declarations.
type ConfigField struct {
	Key         string          `json:"key" bson:"key"`
	Label       string          `json:"label" bson:"label"`
	Group       string          `json:"group,omitempty" bson:"group,omitempty"` // optional presentation group — the admin modal renders tabs when a schema has >=2 distinct groups
	Description string          `json:"description,omitempty" bson:"description,omitempty"`
	Type        ConfigFieldType `json:"type" bson:"type"`
	Required    bool            `json:"required" bson:"required"`
	Default     string          `json:"default,omitempty" bson:"default,omitempty"`
	EnvVar      string          `json:"envVar,omitempty" bson:"envVar,omitempty"` // source env var for seed
}

// CollectionSpec declares a MongoDB collection that a module owns.
type CollectionSpec struct {
	Name    string      `json:"name"`
	Indexes []IndexSpec `json:"indexes,omitempty"`
}

// IndexKey represents a single field in a compound index with deterministic ordering.
type IndexKey struct {
	Field     string `json:"field"`
	Direction int    `json:"direction"` // 1 = asc, -1 = desc
}

// IndexSpec declares a MongoDB index.
// For single-field indexes, use Keys map. For compound indexes where field order matters,
// use OrderedKeys instead (takes precedence over Keys when non-empty).
type IndexSpec struct {
	Keys        map[string]int `json:"keys,omitempty"`        // single-field shorthand
	OrderedKeys []IndexKey     `json:"orderedKeys,omitempty"` // compound indexes with deterministic order
	Unique      bool           `json:"unique,omitempty"`
	Sparse      bool           `json:"sparse,omitempty"`
	TTL         time.Duration  `json:"ttl,omitempty"`         // 0 = no TTL
	Text        bool           `json:"text,omitempty"`        // text index (overrides Keys)
}

// NavItemSpec declares a navigation menu entry that a module contributes.
// The base system collects these from all modules and builds the menu dynamically.
type NavItemSpec struct {
	Group      string        `json:"group"`              // menu group: "Administration", "AI", etc.
	Name       string        `json:"name"`
	Icon       string        `json:"icon,omitempty"`
	Path       string        `json:"path,omitempty"`     // frontend route
	MinRole    string        `json:"minRole,omitempty"`  // minimum role required
	Active     bool          `json:"active"`
	ModuleName string        `json:"moduleName,omitempty"` // owning module — set by registry for enabled filtering
	Children   []NavItemSpec `json:"children,omitempty"`
}
