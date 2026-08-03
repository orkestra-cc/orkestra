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

// ConfigGroup describes one section of the admin settings rail. Groups form
// a tree through Parent, so a module can nest (e.g. "oauth" → "oauth.google")
// instead of flattening every section into one row of tabs.
//
// Deliberately NOT persisted: unlike ConfigSchema, which is snapshotted into
// module_configs and refreshed by RefreshMetadata on every boot, groups are
// pure presentation and fully code-derived. The admin handler resolves them
// from the live registry on each request, so there is nothing to keep in sync
// and no bson tags here.
//
// Label is the literal EN fallback. The admin UI prefers the derived i18n key
// config.groups.<Key>.label and falls back to this string.
type ConfigGroup struct {
	Key         string `json:"key"`                   // stable identifier, never translated
	Label       string `json:"label"`                 // literal EN fallback
	Description string `json:"description,omitempty"` // panel subtitle
	Icon        string `json:"icon,omitempty"`        // FontAwesome name
	Parent      string `json:"parent,omitempty"`      // Key of the parent group
	Order       int    `json:"order,omitempty"`
}

// FieldCondition gates a field's visibility on the value of another field of
// the SAME module. Semantics: AND across a field's DependsOn slice, OR within
// a single condition's In list.
//
// A struct rather than an expression string on purpose — there is no parser to
// write, ship, and keep behaviourally identical between Go and TypeScript.
//
// Named FieldCondition rather than Condition because huma derives OpenAPI
// schema names from the bare Go type name, ignoring the package path, and
// panics on a duplicate named schema (registry.go). "Condition" is a plausible
// request/response body name for a fork's addon; a collision would take the
// server down at boot.
//
// Unlike ConfigGroup this IS persisted: it nests inside ConfigField.DependsOn,
// which is part of the stored configSchema. Hence the bson tags.
//
// # Matching contract
//
// This is the rule every evaluator — the Go side and the admin UI's
// TypeScript one — must implement. Matching is TYPE-AWARE, resolved against
// the Type of the field named by Key, not by raw string equality:
//
//   - FieldBool target: both the stored value and each In entry are
//     interpreted as booleans by the same rule as parseBool
//     (config_unmarshal.go) — "true", "1", "yes" (case-insensitive,
//     whitespace-trimmed) are true, everything else is false. So
//     In: []string{"true"} matches a stored value of "1".
//   - every other type: case-insensitive, whitespace-trimmed exact string
//     match.
//
// Bool values are not stored normalized: buildInitialConfig seeds a config
// value as the raw env string, so COMPLIANCE_AUTO_CLEANUP_ENABLED=1 is stored
// as "1". An exact-match evaluator would leave every field depending on it
// permanently invisible while the setting itself is on — the failure mode is
// silent and only visible as missing UI.
type FieldCondition struct {
	Key string   `json:"key" bson:"key"` // another field key of the same module
	In  []string `json:"in" bson:"in"`   // values that satisfy the condition, matched per the contract above
}

// ConfigField describes a single configurable setting for a module.
// The admin UI renders forms from these declarations.
type ConfigField struct {
	Key         string           `json:"key" bson:"key"`
	Label       string           `json:"label" bson:"label"`
	Group       string           `json:"group,omitempty" bson:"group,omitempty"` // ConfigGroup.Key when the module declares ConfigGroups(); a legacy display label when it does not
	Description string           `json:"description,omitempty" bson:"description,omitempty"`
	Type        ConfigFieldType  `json:"type" bson:"type"`
	Required    bool             `json:"required" bson:"required"`
	Default     string           `json:"default,omitempty" bson:"default,omitempty"`
	EnvVar      string           `json:"envVar,omitempty" bson:"envVar,omitempty"`   // source env var for seed
	Options     []string         `json:"options,omitempty" bson:"options,omitempty"` // valid values for FieldEnum (ignored for other types)
	Advanced    bool             `json:"advanced,omitempty" bson:"advanced,omitempty"`
	DependsOn   []FieldCondition `json:"dependsOn,omitempty" bson:"dependsOn,omitempty"`
	// DependsOnMatch selects how the DependsOn conditions combine.
	// "" and "all" (the default) require every condition to hold; "any"
	// requires at least one.
	//
	// "any" exists because a capability can legitimately be enabled from more
	// than one independent switch. Every OAuth provider in auth has two —
	// one per audience surface — and its credentials are needed as soon as
	// either is on. Without this, the only expressible rules are "both
	// surfaces" (which strands a client-only deployment) or a single surface
	// (which is simply wrong for the other).
	DependsOnMatch string `json:"dependsOnMatch,omitempty" bson:"dependsOnMatch,omitempty"`
	Min            *int   `json:"min,omitempty" bson:"min,omitempty"`
	Max            *int   `json:"max,omitempty" bson:"max,omitempty"`
	Pattern        string `json:"pattern,omitempty" bson:"pattern,omitempty"`
	Placeholder    string `json:"placeholder,omitempty" bson:"placeholder,omitempty"`
	HelpURL        string `json:"helpUrl,omitempty" bson:"helpUrl,omitempty"`
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
	// PartialFilter scopes a (usually unique) index to only the documents
	// matching this filter — the correct replacement for Sparse on a
	// *compound* unique index. A compound sparse index still indexes a
	// document when ANY keyed field is present, so a unique index like
	// (tenantId, vat) over a sometimes-absent `vat` collides on
	// (tenantId, <missing>) once `tenantId` is always present. A partial
	// filter such as {"vat": {"$exists": true}} indexes only the docs that
	// actually carry the field. Mutually exclusive with Sparse (Mongo
	// rejects both together). Supports the partialFilterExpression operator
	// subset: equality, $exists:true, $gt/$gte/$lt/$lte, $type, $in, $and.
	PartialFilter map[string]any `json:"partialFilter,omitempty"`
	TTL           time.Duration  `json:"ttl,omitempty"`      // reap docs TTL after the indexed timestamp; 0 = no TTL
	ExpireAt      bool           `json:"expireAt,omitempty"` // reap docs *at* the indexed timestamp (expireAfterSeconds=0). Mutually exclusive with TTL; use for absolute-expiry fields like `expiresAt`.
	Text          bool           `json:"text,omitempty"`     // text index (overrides Keys)
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

	Name    string `json:"name"`
	Icon    string `json:"icon,omitempty"`
	Path    string `json:"path,omitempty"`
	MinRole string `json:"minRole,omitempty"`
	Active  bool   `json:"active"`

	// RequiresConfig gates the item on a boolean config value of its OWN
	// module, evaluated per request by the navigation filter — so a runtime
	// toggle at /admin/modules takes effect on the next nav fetch, no restart.
	// The value is the config key (e.g. "soc2_enabled"); the owning module is
	// taken from ModuleName. Empty = always visible. Emit the item
	// unconditionally and set this rather than gating inside NavItems() on
	// Init-set state — NavItems() is collected before any module's Init runs.
	RequiresConfig string `json:"requiresConfig,omitempty"`

	ModuleName string        `json:"moduleName,omitempty"` // stamped by registry
	ItemKey    string        `json:"itemKey,omitempty"`    // stamped by registry if empty
	Children   []NavItemSpec `json:"children,omitempty"`
}
