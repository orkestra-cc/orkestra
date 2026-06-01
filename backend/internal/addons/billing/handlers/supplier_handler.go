package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/addons/billing/services"
)

// SupplierHandler handles supplier-related HTTP requests
type SupplierHandler struct {
	supplierService services.SupplierService
}

// NewSupplierHandler creates a new SupplierHandler
func NewSupplierHandler(supplierService services.SupplierService) *SupplierHandler {
	return &SupplierHandler{
		supplierService: supplierService,
	}
}

// ========================================
// Request/Response Types
// ========================================

// CreateSupplierRequest represents the request to create a supplier
type CreateSupplierRequest struct {
	Body models.CreateSupplierInput `json:"supplier" doc:"Supplier data to create"`
}

// CreateSupplierResponse represents the response after creating a supplier
type CreateSupplierResponse struct {
	Body models.Supplier `json:"supplier" doc:"Created supplier"`
}

// GetSupplierRequest represents the request to get a supplier
type GetSupplierRequest struct {
	ID string `path:"id" doc:"Supplier UUID"`
}

// GetSupplierResponse represents the response with supplier details
type GetSupplierResponse struct {
	Body models.Supplier `json:"supplier" doc:"Supplier details"`
}

// ListSuppliersRequest represents the request to list suppliers
type ListSuppliersRequest struct {
	Search   string `query:"search" doc:"Search in name, denomination, fiscal ID, email"`
	Page     int    `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// ListSuppliersResponse represents the paginated list of suppliers
type ListSuppliersResponse struct {
	Body models.SupplierListResponse `json:"suppliers" doc:"Paginated list of suppliers"`
}

// UpdateSupplierRequest represents the request to update a supplier
type UpdateSupplierRequest struct {
	ID   string                     `path:"id" doc:"Supplier UUID"`
	Body models.UpdateSupplierInput `json:"supplier" doc:"Supplier update data"`
}

// UpdateSupplierResponse represents the response after updating a supplier
type UpdateSupplierResponse struct {
	Body models.Supplier `json:"supplier" doc:"Updated supplier"`
}

// DeleteSupplierRequest represents the request to delete a supplier
type DeleteSupplierRequest struct {
	ID string `path:"id" doc:"Supplier UUID"`
}

// DeleteSupplierResponse represents the response after deleting a supplier
type DeleteSupplierResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// ========================================
// Handler Methods
// ========================================

// CreateSupplier creates a new supplier
func (h *SupplierHandler) CreateSupplier(ctx context.Context, req *CreateSupplierRequest) (*CreateSupplierResponse, error) {
	userID := getUserIDFromContext(ctx)

	supplier, err := h.supplierService.CreateSupplier(ctx, &req.Body, userID)
	if err != nil {
		if err == repository.ErrSupplierAlreadyExists {
			return nil, huma.Error409Conflict("Supplier with this fiscal ID already exists", err)
		}
		return nil, huma.Error500InternalServerError("Failed to create supplier", err)
	}

	return &CreateSupplierResponse{Body: *supplier}, nil
}

// GetSupplier retrieves a supplier by ID
func (h *SupplierHandler) GetSupplier(ctx context.Context, req *GetSupplierRequest) (*GetSupplierResponse, error) {
	supplier, err := h.supplierService.GetSupplier(ctx, req.ID)
	if err != nil {
		if err == services.ErrSupplierNotFound {
			return nil, huma.Error404NotFound("Supplier not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get supplier", err)
	}

	return &GetSupplierResponse{Body: *supplier}, nil
}

// ListSuppliers lists suppliers with filtering and pagination
func (h *SupplierHandler) ListSuppliers(ctx context.Context, req *ListSuppliersRequest) (*ListSuppliersResponse, error) {
	pagination := models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	result, err := h.supplierService.ListSuppliers(ctx, req.Search, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list suppliers", err)
	}

	return &ListSuppliersResponse{Body: *result}, nil
}

// UpdateSupplier updates an existing supplier
func (h *SupplierHandler) UpdateSupplier(ctx context.Context, req *UpdateSupplierRequest) (*UpdateSupplierResponse, error) {
	supplier, err := h.supplierService.UpdateSupplier(ctx, req.ID, &req.Body)
	if err != nil {
		if err == services.ErrSupplierNotFound {
			return nil, huma.Error404NotFound("Supplier not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to update supplier", err)
	}

	return &UpdateSupplierResponse{Body: *supplier}, nil
}

// DeleteSupplier deletes a supplier (soft delete)
func (h *SupplierHandler) DeleteSupplier(ctx context.Context, req *DeleteSupplierRequest) (*DeleteSupplierResponse, error) {
	err := h.supplierService.DeleteSupplier(ctx, req.ID)
	if err != nil {
		if err == repository.ErrSupplierNotFound {
			return nil, huma.Error404NotFound("Supplier not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to delete supplier", err)
	}

	resp := &DeleteSupplierResponse{}
	resp.Body.Message = "Supplier deleted successfully"
	return resp, nil
}
