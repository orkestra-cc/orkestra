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
	FieldString     ConfigFieldType = "string"
	FieldBool       ConfigFieldType = "bool"
	FieldInt        ConfigFieldType = "int"
	FieldDuration   ConfigFieldType = "duration"
	FieldSecret     ConfigFieldType = "secret"     // encrypted at rest with AES-256-GCM
	FieldEnum       ConfigFieldType = "enum"       // single value from Options
	FieldStringList ConfigFieldType = "stringList" // comma-separated list of strings; stored as a single comma-joined string
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
	EnvVar      string          `json:"envVar,omitempty" bson:"envVar,omitempty"`   // source env var for seed
	Options     []string        `json:"options,omitempty" bson:"options,omitempty"` // valid values for FieldEnum (ignored for other types)
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
	TTL         time.Duration  `json:"ttl,omitempty"`      // reap docs TTL after the indexed timestamp; 0 = no TTL
	ExpireAt    bool           `json:"expireAt,omitempty"` // reap docs *at* the indexed timestamp (expireAfterSeconds=0). Mutually exclusive with TTL; use for absolute-expiry fields like `expiresAt`.
	Text        bool           `json:"text,omitempty"`     // text index (overrides Keys)
}

// NavItemSpec declares a navigation menu entry that a module contributes.
// The base system collects these from all modules and builds the menu dynamically.
//
// v2 classification (Realm + Section + Tier) drives a two-level sidebar layout
// and tenant-kind-aware filtering:
//
//	Realm   — top-level audience. One of "personal", "platform", "business",
//	          or "shared". Defaults to "shared" when empty.
//	Section — sub-group label within the realm. Defaults to Group when empty.
//	Tier    — audience restriction. "internal" = visible only to internal
//	          (operator) tenants; "external" = visible only to external
//	          (client) tenants; "" = visible to both. Defaults to "".
//
// The legacy Group field is kept for back-compat with v1 consumers; new
// modules should set Realm + Section instead.
//
// ItemKey is a stable identifier for the item across renames. The registry
// fills it in if a module leaves it empty (slugified Name, prefixed with the
// owning module name and parent key). Persisted override docs reference items
// by ItemKey, so modules that want stability across label changes should set
// it explicitly — otherwise renaming `Name` rotates the default key and
// orphans any existing override.
type NavItemSpec struct {
	// Classification (v2) — prefer these for new modules.
	Realm   string `json:"realm,omitempty"`
	Section string `json:"section,omitempty"`
	Tier    string `json:"tier,omitempty"`

	// Legacy grouping (v1) — deprecated; kept so v1 clients still work.
	Group string `json:"group,omitempty"`

	Name       string        `json:"name"`
	Icon       string        `json:"icon,omitempty"`
	Path       string        `json:"path,omitempty"`
	MinRole    string        `json:"minRole,omitempty"`
	Active     bool          `json:"active"`
	ModuleName string        `json:"moduleName,omitempty"` // stamped by registry
	ItemKey    string        `json:"itemKey,omitempty"`    // stamped by registry if empty
	Children   []NavItemSpec `json:"children,omitempty"`
}
