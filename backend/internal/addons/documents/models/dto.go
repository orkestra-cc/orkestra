package models

// PaginationParams holds pagination parameters for list operations
type PaginationParams struct {
	Page     int `query:"page" json:"page" default:"1" minimum:"1" doc:"Page number (1-indexed)"`
	PageSize int `query:"pageSize" json:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Number of items per page"`
}

// TemplateFilters holds filter parameters for template listing
type TemplateFilters struct {
	Type      *TemplateType `query:"type" json:"type,omitempty" doc:"Filter by template type"`
	IsDefault *bool         `query:"isDefault" json:"isDefault,omitempty" doc:"Filter by default flag"`
	IsBuiltIn *bool         `query:"isBuiltIn" json:"isBuiltIn,omitempty" doc:"Filter by built-in flag"`
	IsActive  *bool         `query:"isActive" json:"isActive,omitempty" doc:"Filter by active flag"`
	Search    *string       `query:"search" json:"search,omitempty" doc:"Search in name and description"`
}

// CreateTemplateInput holds input for creating a new template
type CreateTemplateInput struct {
	Name        string          `json:"name" validate:"required,min=1,max=100" doc:"Template name"`
	Description string          `json:"description,omitempty" validate:"max=500" doc:"Template description"`
	Type        TemplateType    `json:"type" validate:"required" doc:"Template type (invoice, offer, receipt, custom)"`
	HTMLContent string          `json:"htmlContent" validate:"required" doc:"HTML template content"`
	CSSContent  string          `json:"cssContent,omitempty" doc:"CSS styles for the template"`
	PageSize    PageSize        `json:"pageSize" default:"A4" doc:"Page size (A4, A3, Letter, Legal)"`
	Orientation PageOrientation `json:"orientation" default:"portrait" doc:"Page orientation (portrait, landscape)"`
	Margins     *PageMargins    `json:"margins,omitempty" doc:"Page margins in millimeters"`
	HeaderHTML  string          `json:"headerHtml,omitempty" doc:"Optional HTML header template"`
	FooterHTML  string          `json:"footerHtml,omitempty" doc:"Optional HTML footer template"`
}

// UpdateTemplateInput holds input for updating a template
type UpdateTemplateInput struct {
	Name        *string          `json:"name,omitempty" validate:"omitempty,min=1,max=100" doc:"Template name"`
	Description *string          `json:"description,omitempty" validate:"omitempty,max=500" doc:"Template description"`
	HTMLContent *string          `json:"htmlContent,omitempty" doc:"HTML template content"`
	CSSContent  *string          `json:"cssContent,omitempty" doc:"CSS styles for the template"`
	PageSize    *PageSize        `json:"pageSize,omitempty" doc:"Page size (A4, A3, Letter, Legal)"`
	Orientation *PageOrientation `json:"orientation,omitempty" doc:"Page orientation (portrait, landscape)"`
	Margins     *PageMargins     `json:"margins,omitempty" doc:"Page margins in millimeters"`
	HeaderHTML  *string          `json:"headerHtml,omitempty" doc:"Optional HTML header template"`
	FooterHTML  *string          `json:"footerHtml,omitempty" doc:"Optional HTML footer template"`
	IsActive    *bool            `json:"isActive,omitempty" doc:"Whether template is active"`
}

// TemplateListResponse holds the response for template list operations
type TemplateListResponse struct {
	Templates  []TemplateListItem `json:"templates"`
	Total      int64              `json:"total"`
	Page       int                `json:"page"`
	PageSize   int                `json:"pageSize"`
	TotalPages int                `json:"totalPages"`
}

// GeneratePDFInput holds input for PDF generation
type GeneratePDFInput struct {
	TemplateUUID string                 `json:"templateUuid" validate:"required" doc:"UUID of template to use"`
	Data         map[string]interface{} `json:"data" validate:"required" doc:"Data to populate the template"`
	FileName     string                 `json:"fileName,omitempty" doc:"Optional custom filename (without extension)"`
	SourceType   SourceType             `json:"sourceType,omitempty" doc:"Source document type (for tracking)"`
	SourceUUID   string                 `json:"sourceUuid,omitempty" doc:"Source document UUID (for tracking)"`
}

// GeneratePDFFromSourceInput holds input for generating PDF from a specific source
type GeneratePDFFromSourceInput struct {
	SourceType   SourceType `json:"sourceType" validate:"required" doc:"Source document type"`
	SourceUUID   string     `json:"sourceUuid" validate:"required" doc:"Source document UUID"`
	TemplateUUID string     `json:"templateUuid,omitempty" doc:"Optional template UUID (uses default if not specified)"`
}

// PreviewHTMLInput holds input for HTML preview
type PreviewHTMLInput struct {
	TemplateUUID string                 `json:"templateUuid" validate:"required" doc:"UUID of template to preview"`
	Data         map[string]interface{} `json:"data" validate:"required" doc:"Data to populate the template"`
}

// PreviewHTMLFromContentInput allows previewing without a saved template
type PreviewHTMLFromContentInput struct {
	HTMLContent string                 `json:"htmlContent" validate:"required" doc:"HTML template content"`
	CSSContent  string                 `json:"cssContent,omitempty" doc:"CSS styles for the template"`
	Data        map[string]interface{} `json:"data" validate:"required" doc:"Data to populate the template"`
}

// GeneratedDocumentListResponse holds the response for document list operations
type GeneratedDocumentListResponse struct {
	Documents  []GeneratedDocumentMeta `json:"documents"`
	Total      int64                   `json:"total"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"pageSize"`
	TotalPages int                     `json:"totalPages"`
}

// TemplateVariableInfo describes a variable available in a template
type TemplateVariableInfo struct {
	Name        string `json:"name" doc:"Variable name (e.g., .number)"`
	Description string `json:"description" doc:"Description of the variable"`
	Type        string `json:"type" doc:"Data type (string, number, date, object, array)"`
	Required    bool   `json:"required" doc:"Whether the variable is required"`
}

// TemplateVariableGroup groups related variables
type TemplateVariableGroup struct {
	Name        string                 `json:"name" doc:"Group name (e.g., Document Info, Seller)"`
	Description string                 `json:"description" doc:"Group description"`
	Variables   []TemplateVariableInfo `json:"variables" doc:"Variables in this group"`
}

// TemplateVariablesResponse returns available variables for a template type
type TemplateVariablesResponse struct {
	TemplateType TemplateType            `json:"templateType"`
	Groups       []TemplateVariableGroup `json:"groups"`
}
