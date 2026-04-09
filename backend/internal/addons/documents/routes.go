package documents

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/documents/handlers"
)

// RegisterRoutes registers all document-related routes
func RegisterRoutes(
	api huma.API,
	templateHandler *handlers.TemplateHandler,
	documentHandler *handlers.DocumentHandler,
) {
	// ============================================
	// Template Management Routes
	// ============================================

	// Create template
	huma.Register(api, huma.Operation{
		OperationID: "create-template",
		Method:      http.MethodPost,
		Path:        "/v1/documents/templates",
		Summary:     "Create template",
		Description: "Creates a new document template",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.CreateTemplate)

	// List templates
	huma.Register(api, huma.Operation{
		OperationID: "list-templates",
		Method:      http.MethodGet,
		Path:        "/v1/documents/templates",
		Summary:     "List templates",
		Description: "Retrieves a paginated list of document templates with optional filters",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.ListTemplates)

	// Get template by ID
	huma.Register(api, huma.Operation{
		OperationID: "get-template",
		Method:      http.MethodGet,
		Path:        "/v1/documents/templates/{id}",
		Summary:     "Get template",
		Description: "Retrieves a document template by UUID",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.GetTemplate)

	// Update template
	huma.Register(api, huma.Operation{
		OperationID: "update-template",
		Method:      http.MethodPatch,
		Path:        "/v1/documents/templates/{id}",
		Summary:     "Update template",
		Description: "Updates an existing document template",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.UpdateTemplate)

	// Delete template
	huma.Register(api, huma.Operation{
		OperationID: "delete-template",
		Method:      http.MethodDelete,
		Path:        "/v1/documents/templates/{id}",
		Summary:     "Delete template",
		Description: "Deletes a document template (soft delete, built-in templates cannot be deleted)",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.DeleteTemplate)

	// Set template as default
	huma.Register(api, huma.Operation{
		OperationID: "set-default-template",
		Method:      http.MethodPost,
		Path:        "/v1/documents/templates/{id}/default",
		Summary:     "Set default template",
		Description: "Sets a template as the default for its type",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.SetDefaultTemplate)

	// Duplicate template
	huma.Register(api, huma.Operation{
		OperationID: "duplicate-template",
		Method:      http.MethodPost,
		Path:        "/v1/documents/templates/{id}/duplicate",
		Summary:     "Duplicate template",
		Description: "Creates a copy of an existing template with a new name",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.DuplicateTemplate)

	// Get template variables
	huma.Register(api, huma.Operation{
		OperationID: "get-template-variables",
		Method:      http.MethodGet,
		Path:        "/v1/documents/templates/variables/{type}",
		Summary:     "Get template variables",
		Description: "Returns available variables for a template type (invoice, offer, receipt, custom)",
		Tags:        []string{"Documents - Templates"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, templateHandler.GetTemplateVariables)

	// ============================================
	// PDF Generation Routes
	// ============================================

	// Generate PDF
	huma.Register(api, huma.Operation{
		OperationID: "generate-pdf",
		Method:      http.MethodPost,
		Path:        "/v1/documents/generate",
		Summary:     "Generate PDF",
		Description: "Generates a PDF from a template and data",
		Tags:        []string{"Documents - PDF"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, documentHandler.GeneratePDF)

	// Preview HTML (from template)
	huma.Register(api, huma.Operation{
		OperationID: "preview-html",
		Method:      http.MethodPost,
		Path:        "/v1/documents/preview",
		Summary:     "Preview HTML",
		Description: "Renders a template to HTML without generating PDF (for live preview)",
		Tags:        []string{"Documents - PDF"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, documentHandler.PreviewHTML)

	// Preview HTML from raw content
	huma.Register(api, huma.Operation{
		OperationID: "preview-html-content",
		Method:      http.MethodPost,
		Path:        "/v1/documents/preview/content",
		Summary:     "Preview HTML from content",
		Description: "Renders raw HTML/CSS content with data (for template editor preview)",
		Tags:        []string{"Documents - PDF"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, documentHandler.PreviewHTMLFromContent)

	// Get document metadata
	huma.Register(api, huma.Operation{
		OperationID: "get-document",
		Method:      http.MethodGet,
		Path:        "/v1/documents/{id}",
		Summary:     "Get document",
		Description: "Retrieves document metadata by UUID",
		Tags:        []string{"Documents - PDF"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, documentHandler.GetDocument)

	// Download document
	huma.Register(api, huma.Operation{
		OperationID:   "download-document",
		Method:        http.MethodGet,
		Path:          "/v1/documents/{id}/download",
		Summary:       "Download document",
		Description:   "Downloads the PDF content of a generated document",
		Tags:          []string{"Documents - PDF"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusOK,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "PDF document",
				Content: map[string]*huma.MediaType{
					"application/pdf": {
						Schema: &huma.Schema{
							Type:   huma.TypeString,
							Format: "binary",
						},
					},
				},
			},
		},
	}, documentHandler.DownloadDocument)

	// ============================================
	// Service Status Routes
	// ============================================

	// Check PDF service status
	huma.Register(api, huma.Operation{
		OperationID: "documents-service-status",
		Method:      http.MethodGet,
		Path:        "/v1/documents/status",
		Summary:     "Service status",
		Description: "Checks if the PDF generation service (Gotenberg) is available",
		Tags:        []string{"Documents - PDF"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, documentHandler.GetServiceStatus)
}
