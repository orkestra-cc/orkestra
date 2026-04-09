package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/addons/billing/services"
)

// CompanyHandler handles company-related HTTP requests
type CompanyHandler struct {
	companyService services.CompanyService
}

// NewCompanyHandler creates a new CompanyHandler
func NewCompanyHandler(companyService services.CompanyService) *CompanyHandler {
	return &CompanyHandler{
		companyService: companyService,
	}
}

// ========================================
// Request/Response Types
// ========================================

// CreateCompanyRequest represents the request to create a company
type CreateCompanyRequest struct {
	Body models.CreateCompanyInput `json:"company" doc:"Company data to create"`
}

// CreateCompanyResponse represents the response after creating a company
type CreateCompanyResponse struct {
	Body models.Company `json:"company" doc:"Created company"`
}

// GetCompanyRequest represents the request to get a company
type GetCompanyRequest struct {
	ID string `path:"id" doc:"Company UUID"`
}

// GetCompanyResponse represents the response with company details
type GetCompanyResponse struct {
	Body models.Company `json:"company" doc:"Company details"`
}

// GetDefaultCompanyResponse represents the response with the default company
type GetDefaultCompanyResponse struct {
	Body models.Company `json:"company" doc:"Default company"`
}

// ListCompaniesRequest represents the request to list companies
type ListCompaniesRequest struct {
	Search   string `query:"search" doc:"Search in denomination, fiscal ID, email"`
	Page     int    `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// ListCompaniesResponse represents the paginated list of companies
type ListCompaniesResponse struct {
	Body models.CompanyListResponse `json:"companies" doc:"Paginated list of companies"`
}

// UpdateCompanyRequest represents the request to update a company
type UpdateCompanyRequest struct {
	ID   string                    `path:"id" doc:"Company UUID"`
	Body models.UpdateCompanyInput `json:"company" doc:"Company update data"`
}

// UpdateCompanyResponse represents the response after updating a company
type UpdateCompanyResponse struct {
	Body models.Company `json:"company" doc:"Updated company"`
}

// DeleteCompanyRequest represents the request to delete a company
type DeleteCompanyRequest struct {
	ID string `path:"id" doc:"Company UUID"`
}

// DeleteCompanyResponse represents the response after deleting a company
type DeleteCompanyResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// SetDefaultCompanyRequest represents the request to set a company as default
type SetDefaultCompanyRequest struct {
	ID string `path:"id" doc:"Company UUID to set as default"`
}

// SetDefaultCompanyResponse represents the response after setting a company as default
type SetDefaultCompanyResponse struct {
	Body models.Company `json:"company" doc:"Company set as default"`
}

// ========================================
// Handler Methods
// ========================================

// CreateCompany creates a new company
func (h *CompanyHandler) CreateCompany(ctx context.Context, req *CreateCompanyRequest) (*CreateCompanyResponse, error) {
	userID := getUserIDFromContext(ctx)

	company, err := h.companyService.CreateCompany(ctx, &req.Body, userID)
	if err != nil {
		if err == repository.ErrCompanyAlreadyExists {
			return nil, huma.Error409Conflict("Company with this fiscal ID already exists", err)
		}
		return nil, huma.Error400BadRequest(err.Error(), err)
	}

	return &CreateCompanyResponse{Body: *company}, nil
}

// GetCompany retrieves a company by ID
func (h *CompanyHandler) GetCompany(ctx context.Context, req *GetCompanyRequest) (*GetCompanyResponse, error) {
	company, err := h.companyService.GetCompany(ctx, req.ID)
	if err != nil {
		if err == services.ErrCompanyNotFound {
			return nil, huma.Error404NotFound("Company not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get company", err)
	}

	return &GetCompanyResponse{Body: *company}, nil
}

// GetDefaultCompany retrieves the default company
func (h *CompanyHandler) GetDefaultCompany(ctx context.Context, req *struct{}) (*GetDefaultCompanyResponse, error) {
	company, err := h.companyService.GetDefaultCompany(ctx)
	if err != nil {
		if err == services.ErrNoDefaultCompany {
			return nil, huma.Error404NotFound("No default company configured", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get default company", err)
	}

	return &GetDefaultCompanyResponse{Body: *company}, nil
}

// ListCompanies lists companies with filtering and pagination
func (h *CompanyHandler) ListCompanies(ctx context.Context, req *ListCompaniesRequest) (*ListCompaniesResponse, error) {
	pagination := models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	result, err := h.companyService.ListCompanies(ctx, req.Search, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list companies", err)
	}

	return &ListCompaniesResponse{Body: *result}, nil
}

// UpdateCompany updates an existing company
func (h *CompanyHandler) UpdateCompany(ctx context.Context, req *UpdateCompanyRequest) (*UpdateCompanyResponse, error) {
	company, err := h.companyService.UpdateCompany(ctx, req.ID, &req.Body)
	if err != nil {
		if err == services.ErrCompanyNotFound {
			return nil, huma.Error404NotFound("Company not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to update company", err)
	}

	return &UpdateCompanyResponse{Body: *company}, nil
}

// DeleteCompany deletes a company (soft delete)
func (h *CompanyHandler) DeleteCompany(ctx context.Context, req *DeleteCompanyRequest) (*DeleteCompanyResponse, error) {
	err := h.companyService.DeleteCompany(ctx, req.ID)
	if err != nil {
		if err == repository.ErrCompanyNotFound {
			return nil, huma.Error404NotFound("Company not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to delete company", err)
	}

	resp := &DeleteCompanyResponse{}
	resp.Body.Message = "Company deleted successfully"
	return resp, nil
}

// SetDefaultCompany sets a company as the default for new invoices
func (h *CompanyHandler) SetDefaultCompany(ctx context.Context, req *SetDefaultCompanyRequest) (*SetDefaultCompanyResponse, error) {
	company, err := h.companyService.SetDefaultCompany(ctx, req.ID)
	if err != nil {
		if err == services.ErrCompanyNotFound {
			return nil, huma.Error404NotFound("Company not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to set default company", err)
	}

	return &SetDefaultCompanyResponse{Body: *company}, nil
}
