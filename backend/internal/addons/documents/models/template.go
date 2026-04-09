package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Template represents a document template stored in the database
type Template struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID        string             `bson:"uuid" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Description string             `bson:"description,omitempty" json:"description,omitempty"`
	Type        TemplateType       `bson:"type" json:"type"`

	// Template content
	HTMLContent string `bson:"htmlContent" json:"htmlContent"`
	CSSContent  string `bson:"cssContent,omitempty" json:"cssContent,omitempty"`

	// Page settings
	PageSize    PageSize        `bson:"pageSize" json:"pageSize"`
	Orientation PageOrientation `bson:"orientation" json:"orientation"`
	Margins     PageMargins     `bson:"margins" json:"margins"`

	// Optional header and footer templates
	HeaderHTML string `bson:"headerHtml,omitempty" json:"headerHtml,omitempty"`
	FooterHTML string `bson:"footerHtml,omitempty" json:"footerHtml,omitempty"`

	// Flags
	IsDefault bool `bson:"isDefault" json:"isDefault"` // Default template for this type
	IsBuiltIn bool `bson:"isBuiltIn" json:"isBuiltIn"` // Ships with the application
	IsActive  bool `bson:"isActive" json:"isActive"`   // Can be used for generation

	// Versioning
	Version int `bson:"version" json:"version"`

	// Audit fields
	CreatedAt time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time  `bson:"updatedAt" json:"updatedAt"`
	DeletedAt *time.Time `bson:"deletedAt,omitempty" json:"deletedAt,omitempty"`
	CreatedBy string     `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	UpdatedBy string     `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}

// PageMargins represents the page margins in millimeters
type PageMargins struct {
	Top    float64 `bson:"top" json:"top"`       // Top margin in mm
	Bottom float64 `bson:"bottom" json:"bottom"` // Bottom margin in mm
	Left   float64 `bson:"left" json:"left"`     // Left margin in mm
	Right  float64 `bson:"right" json:"right"`   // Right margin in mm
}

// DefaultMargins returns the default page margins (20mm on all sides)
func DefaultMargins() PageMargins {
	return PageMargins{
		Top:    20.0,
		Bottom: 20.0,
		Left:   20.0,
		Right:  20.0,
	}
}

// TemplateListItem represents a template in list responses (lighter version)
type TemplateListItem struct {
	UUID        string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Type        TemplateType    `json:"type"`
	PageSize    PageSize        `json:"pageSize"`
	Orientation PageOrientation `json:"orientation"`
	IsDefault   bool            `json:"isDefault"`
	IsBuiltIn   bool            `json:"isBuiltIn"`
	IsActive    bool            `json:"isActive"`
	Version     int             `json:"version"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// ToListItem converts a Template to a TemplateListItem
func (t *Template) ToListItem() TemplateListItem {
	return TemplateListItem{
		UUID:        t.UUID,
		Name:        t.Name,
		Description: t.Description,
		Type:        t.Type,
		PageSize:    t.PageSize,
		Orientation: t.Orientation,
		IsDefault:   t.IsDefault,
		IsBuiltIn:   t.IsBuiltIn,
		IsActive:    t.IsActive,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}
