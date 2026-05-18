package models

import "time"

// CustomFieldType enumerates the value shapes a custom field can hold.
// `ref:<collection>` is encoded as the literal string with a colon and
// the target collection name (e.g. "ref:marketing_tags"); the
// validator parses the prefix. Keeping it as a single string keeps
// MongoDB index expressions and JSON serialization simple.
type CustomFieldType string

const (
	FieldTypeString    CustomFieldType = "string"
	FieldTypeInt       CustomFieldType = "int"
	FieldTypeFloat     CustomFieldType = "float"
	FieldTypeBool      CustomFieldType = "bool"
	FieldTypeDate      CustomFieldType = "date"
	FieldTypeDateTime  CustomFieldType = "datetime"
	FieldTypeEnum      CustomFieldType = "enum"
	FieldTypeMultiEnum CustomFieldType = "multi_enum"
	// FieldTypeRef is the prefix; the full type string carries the
	// target collection (e.g. "ref:marketing_tags"). Use IsRefType /
	// RefTarget to inspect.
	FieldTypeRef CustomFieldType = "ref"
)

// FieldOption is one allowed value for an enum / multi_enum field.
// Value is what is persisted on Person/Organization records; Label is
// the admin-UI display text. When Label is empty the UI defaults to
// Value.
type FieldOption struct {
	Value string `bson:"value" json:"value"`
	Label string `bson:"label,omitempty" json:"label,omitempty"`
}

// FieldDef declares one column in the tenant's custom-fields bag for a
// given target collection. The shape is intentionally close to the
// JSON Schema subset needed for write-time validation: Type drives
// the validator; Options is consulted only for enum / multi_enum;
// Default seeds new records when the caller omits the field.
type FieldDef struct {
	Key         string          `bson:"key" json:"key"`
	Label       string          `bson:"label,omitempty" json:"label,omitempty"`
	Type        CustomFieldType `bson:"type" json:"type"`
	Required    bool            `bson:"required,omitempty" json:"required,omitempty"`
	Options     []FieldOption   `bson:"options,omitempty" json:"options,omitempty"`
	Default     any             `bson:"default,omitempty" json:"default,omitempty"`
	Description string          `bson:"description,omitempty" json:"description,omitempty"`
}

// CustomFieldTarget enumerates which marketing collection a schema
// document configures.
type CustomFieldTarget string

const (
	CustomFieldTargetPersons       CustomFieldTarget = "persons"
	CustomFieldTargetOrganizations CustomFieldTarget = "organizations"
)

// CustomFieldSchema is the per-tenant declaration of the custom-fields
// bag shape for a given target. One row per (tenantId,
// targetCollection) — enforced via the unique compound index declared
// in module.go::Collections().
//
// Schema mirrors docs/plans/marketing-addon/schemas/marketing_custom_field_schemas.md.
type CustomFieldSchema struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"tenantId"`

	TargetCollection   CustomFieldTarget `bson:"targetCollection" json:"targetCollection"`
	Fields             []FieldDef        `bson:"fields" json:"fields"`
	AllowUnknownFields bool              `bson:"allowUnknownFields" json:"allowUnknownFields"`

	// Version increments on every mutation. Useful for downstream
	// audit logging without diff-walking the field list.
	Version int `bson:"version" json:"version"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
	UpdatedBy string    `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}
