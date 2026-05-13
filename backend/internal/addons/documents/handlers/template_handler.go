package handlers

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/documents/models"
	"github.com/orkestra/backend/internal/addons/documents/repository"
	"github.com/orkestra/backend/internal/addons/documents/services"
)

// stringToBoolPtr converts a query string ("true"/"false") to *bool.
// Returns nil if the string is empty (parameter not provided).
func stringToBoolPtr(s string) *bool {
	if s == "" {
		return nil
	}
	v := strings.ToLower(s) == "true"
	return &v
}

// TemplateHandler handles template-related HTTP requests
type TemplateHandler struct {
	templateService services.TemplateService
}

// NewTemplateHandler creates a new template handler
func NewTemplateHandler(templateService services.TemplateService) *TemplateHandler {
	return &TemplateHandler{
		templateService: templateService,
	}
}

// -------- Request/Response types --------

// CreateTemplateRequest is the request for creating a template
type CreateTemplateRequest struct {
	Body models.CreateTemplateInput `json:"template" doc:"Template data to create"`
}

// CreateTemplateResponse is the response for template creation
type CreateTemplateResponse struct {
	Body models.Template `json:"template" doc:"Created template"`
}

// GetTemplateRequest is the request for getting a template by ID
type GetTemplateRequest struct {
	ID string `path:"id" doc:"Template UUID"`
}

// GetTemplateResponse is the response containing a single template
type GetTemplateResponse struct {
	Body models.Template `json:"template" doc:"Template details"`
}

// ListTemplatesRequest is the request for listing templates
type ListTemplatesRequest struct {
	Page      int                 `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize  int                 `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
	Type      models.TemplateType `query:"type" doc:"Filter by template type"`
	IsDefault string              `query:"isDefault" enum:"true,false" doc:"Filter by default flag"`
	IsBuiltIn string              `query:"isBuiltIn" enum:"true,false" doc:"Filter by built-in flag"`
	IsActive  string              `query:"isActive" enum:"true,false" doc:"Filter by active flag"`
	Search    string              `query:"search" doc:"Search in name and description"`
}

// ListTemplatesResponse is the response for template listing
type ListTemplatesResponse struct {
	Body models.TemplateListResponse `json:"templates" doc:"List of templates with pagination"`
}

// UpdateTemplateRequest is the request for updating a template
type UpdateTemplateRequest struct {
	ID   string                     `path:"id" doc:"Template UUID"`
	Body models.UpdateTemplateInput `json:"template" doc:"Template update data"`
}

// UpdateTemplateResponse is the response for template update
type UpdateTemplateResponse struct {
	Body models.Template `json:"template" doc:"Updated template"`
}

// DeleteTemplateRequest is the request for deleting a template
type DeleteTemplateRequest struct {
	ID string `path:"id" doc:"Template UUID"`
}

// DeleteTemplateResponse is the response for template deletion
type DeleteTemplateResponse struct {
	Body struct {
		Success bool   `json:"success" doc:"Whether the deletion was successful"`
		Message string `json:"message" doc:"Status message"`
	}
}

// SetDefaultTemplateRequest is the request for setting a template as default
type SetDefaultTemplateRequest struct {
	ID string `path:"id" doc:"Template UUID"`
}

// SetDefaultTemplateResponse is the response for setting default template
type SetDefaultTemplateResponse struct {
	Body struct {
		Success bool   `json:"success" doc:"Whether the operation was successful"`
		Message string `json:"message" doc:"Status message"`
	}
}

// DuplicateTemplateRequest is the request for duplicating a template
type DuplicateTemplateRequest struct {
	ID   string `path:"id" doc:"Template UUID to duplicate"`
	Body struct {
		Name string `json:"name" validate:"required,min=1,max=100" doc:"Name for the new template"`
	}
}

// DuplicateTemplateResponse is the response for template duplication
type DuplicateTemplateResponse struct {
	Body models.Template `json:"template" doc:"Duplicated template"`
}

// GetTemplateVariablesRequest is the request for getting template variables
type GetTemplateVariablesRequest struct {
	Type models.TemplateType `path:"type" doc:"Template type (invoice, offer, receipt, custom)"`
}

// GetTemplateVariablesResponse is the response containing template variables
type GetTemplateVariablesResponse struct {
	Body models.TemplateVariablesResponse `json:"variables" doc:"Available template variables"`
}

// -------- Handler methods --------

// CreateTemplate creates a new template
func (h *TemplateHandler) CreateTemplate(ctx context.Context, req *CreateTemplateRequest) (*CreateTemplateResponse, error) {
	userID := getUserIDFromContext(ctx)

	template, err := h.templateService.Create(ctx, &req.Body, userID)
	if err != nil {
		if err == repository.ErrTemplateAlreadyExists {
			return nil, huma.Error409Conflict("A template with this name already exists", err)
		}
		if err == services.ErrTemplateNameRequired ||
			err == services.ErrTemplateContentRequired ||
			err == services.ErrInvalidTemplateType ||
			err == services.ErrInvalidPageSize ||
			err == services.ErrInvalidOrientation {
			return nil, huma.Error400BadRequest(err.Error(), err)
		}
		if err == services.ErrTemplateParseError {
			return nil, huma.Error400BadRequest("Invalid template HTML content", err)
		}
		return nil, huma.Error500InternalServerError("Failed to create template", err)
	}

	return &CreateTemplateResponse{Body: *template}, nil
}

// GetTemplate retrieves a template by ID
func (h *TemplateHandler) GetTemplate(ctx context.Context, req *GetTemplateRequest) (*GetTemplateResponse, error) {
	template, err := h.templateService.GetByUUID(ctx, req.ID)
	if err != nil {
		if err == repository.ErrTemplateNotFound {
			return nil, huma.Error404NotFound("Template not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get template", err)
	}

	return &GetTemplateResponse{Body: *template}, nil
}

// ListTemplates lists templates with optional filters
func (h *TemplateHandler) ListTemplates(ctx context.Context, req *ListTemplatesRequest) (*ListTemplatesResponse, error) {
	// Build filters, converting value types to pointers for optional fields
	var typePtr *models.TemplateType
	if req.Type != "" {
		typePtr = &req.Type
	}

	var searchPtr *string
	if req.Search != "" {
		searchPtr = &req.Search
	}

	filters := &models.TemplateFilters{
		Type:      typePtr,
		IsDefault: stringToBoolPtr(req.IsDefault),
		IsBuiltIn: stringToBoolPtr(req.IsBuiltIn),
		IsActive:  stringToBoolPtr(req.IsActive),
		Search:    searchPtr,
	}

	pagination := models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	result, err := h.templateService.List(ctx, filters, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list templates", err)
	}

	return &ListTemplatesResponse{Body: *result}, nil
}

// UpdateTemplate updates an existing template
func (h *TemplateHandler) UpdateTemplate(ctx context.Context, req *UpdateTemplateRequest) (*UpdateTemplateResponse, error) {
	userID := getUserIDFromContext(ctx)

	template, err := h.templateService.Update(ctx, req.ID, &req.Body, userID)
	if err != nil {
		if err == repository.ErrTemplateNotFound {
			return nil, huma.Error404NotFound("Template not found", err)
		}
		if err == services.ErrTemplateNameRequired ||
			err == services.ErrTemplateContentRequired ||
			err == services.ErrInvalidPageSize ||
			err == services.ErrInvalidOrientation {
			return nil, huma.Error400BadRequest(err.Error(), err)
		}
		if err == services.ErrTemplateParseError {
			return nil, huma.Error400BadRequest("Invalid template HTML content", err)
		}
		return nil, huma.Error500InternalServerError("Failed to update template", err)
	}

	return &UpdateTemplateResponse{Body: *template}, nil
}

// DeleteTemplate deletes a template
func (h *TemplateHandler) DeleteTemplate(ctx context.Context, req *DeleteTemplateRequest) (*DeleteTemplateResponse, error) {
	err := h.templateService.Delete(ctx, req.ID)
	if err != nil {
		if err == repository.ErrTemplateNotFound {
			return nil, huma.Error404NotFound("Template not found", err)
		}
		if err == repository.ErrCannotDeleteBuiltIn {
			return nil, huma.Error400BadRequest("Cannot delete built-in templates", err)
		}
		return nil, huma.Error500InternalServerError("Failed to delete template", err)
	}

	return &DeleteTemplateResponse{
		Body: struct {
			Success bool   `json:"success" doc:"Whether the deletion was successful"`
			Message string `json:"message" doc:"Status message"`
		}{
			Success: true,
			Message: "Template deleted successfully",
		},
	}, nil
}

// SetDefaultTemplate sets a template as the default for its type
func (h *TemplateHandler) SetDefaultTemplate(ctx context.Context, req *SetDefaultTemplateRequest) (*SetDefaultTemplateResponse, error) {
	err := h.templateService.SetDefault(ctx, req.ID)
	if err != nil {
		if err == repository.ErrTemplateNotFound {
			return nil, huma.Error404NotFound("Template not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to set default template", err)
	}

	return &SetDefaultTemplateResponse{
		Body: struct {
			Success bool   `json:"success" doc:"Whether the operation was successful"`
			Message string `json:"message" doc:"Status message"`
		}{
			Success: true,
			Message: "Template set as default successfully",
		},
	}, nil
}

// DuplicateTemplate creates a copy of an existing template
func (h *TemplateHandler) DuplicateTemplate(ctx context.Context, req *DuplicateTemplateRequest) (*DuplicateTemplateResponse, error) {
	userID := getUserIDFromContext(ctx)

	template, err := h.templateService.Duplicate(ctx, req.ID, req.Body.Name, userID)
	if err != nil {
		if err == repository.ErrTemplateNotFound {
			return nil, huma.Error404NotFound("Template not found", err)
		}
		if err == repository.ErrTemplateAlreadyExists {
			return nil, huma.Error409Conflict("A template with this name already exists", err)
		}
		return nil, huma.Error500InternalServerError("Failed to duplicate template", err)
	}

	return &DuplicateTemplateResponse{Body: *template}, nil
}

// GetTemplateVariables returns available variables for a template type
func (h *TemplateHandler) GetTemplateVariables(ctx context.Context, req *GetTemplateVariablesRequest) (*GetTemplateVariablesResponse, error) {
	if !req.Type.IsValid() {
		return nil, huma.Error400BadRequest("Invalid template type", services.ErrInvalidTemplateType)
	}

	vars, err := h.templateService.GetVariables(ctx, req.Type)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get template variables", err)
	}

	return &GetTemplateVariablesResponse{Body: *vars}, nil
}

// getUserIDFromContext extracts the user ID from the context
func getUserIDFromContext(ctx context.Context) string {
	if userID, ok := ctx.Value("userID").(string); ok {
		return userID
	}
	return ""
}
