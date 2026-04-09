package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/addons/billing/services"
)

// CustomerHandler handles customer-related HTTP requests
type CustomerHandler struct {
	customerService services.CustomerService
}

// NewCustomerHandler creates a new CustomerHandler
func NewCustomerHandler(customerService services.CustomerService) *CustomerHandler {
	return &CustomerHandler{
		customerService: customerService,
	}
}

// ========================================
// Request/Response Types
// ========================================

// CreateCustomerRequest represents the request to create a customer
type CreateCustomerRequest struct {
	Body models.CreateCustomerInput `json:"customer" doc:"Customer data to create"`
}

// CreateCustomerResponse represents the response after creating a customer
type CreateCustomerResponse struct {
	Body models.Customer `json:"customer" doc:"Created customer"`
}

// GetCustomerRequest represents the request to get a customer
type GetCustomerRequest struct {
	ID string `path:"id" doc:"Customer UUID"`
}

// GetCustomerResponse represents the response with customer details
type GetCustomerResponse struct {
	Body models.Customer `json:"customer" doc:"Customer details"`
}

// ListCustomersRequest represents the request to list customers
type ListCustomersRequest struct {
	Search   string `query:"search" doc:"Search in name, denomination, fiscal ID, email"`
	Page     int    `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// ListCustomersResponse represents the paginated list of customers
type ListCustomersResponse struct {
	Body models.CustomerListResponse `json:"customers" doc:"Paginated list of customers"`
}

// UpdateCustomerRequest represents the request to update a customer
type UpdateCustomerRequest struct {
	ID   string                     `path:"id" doc:"Customer UUID"`
	Body models.UpdateCustomerInput `json:"customer" doc:"Customer update data"`
}

// UpdateCustomerResponse represents the response after updating a customer
type UpdateCustomerResponse struct {
	Body models.Customer `json:"customer" doc:"Updated customer"`
}

// DeleteCustomerRequest represents the request to delete a customer
type DeleteCustomerRequest struct {
	ID string `path:"id" doc:"Customer UUID"`
}

// DeleteCustomerResponse represents the response after deleting a customer
type DeleteCustomerResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// ========================================
// Handler Methods
// ========================================

// CreateCustomer creates a new customer
func (h *CustomerHandler) CreateCustomer(ctx context.Context, req *CreateCustomerRequest) (*CreateCustomerResponse, error) {
	userID := getUserIDFromContext(ctx)

	customer, err := h.customerService.CreateCustomer(ctx, &req.Body, userID)
	if err != nil {
		if err == repository.ErrCustomerAlreadyExists {
			return nil, huma.Error409Conflict("Customer with this fiscal ID already exists", err)
		}
		return nil, huma.Error500InternalServerError("Failed to create customer", err)
	}

	return &CreateCustomerResponse{Body: *customer}, nil
}

// GetCustomer retrieves a customer by ID
func (h *CustomerHandler) GetCustomer(ctx context.Context, req *GetCustomerRequest) (*GetCustomerResponse, error) {
	customer, err := h.customerService.GetCustomer(ctx, req.ID)
	if err != nil {
		if err == services.ErrCustomerNotFound {
			return nil, huma.Error404NotFound("Customer not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get customer", err)
	}

	return &GetCustomerResponse{Body: *customer}, nil
}

// ListCustomers lists customers with filtering and pagination
func (h *CustomerHandler) ListCustomers(ctx context.Context, req *ListCustomersRequest) (*ListCustomersResponse, error) {
	pagination := models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	result, err := h.customerService.ListCustomers(ctx, req.Search, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list customers", err)
	}

	return &ListCustomersResponse{Body: *result}, nil
}

// UpdateCustomer updates an existing customer
func (h *CustomerHandler) UpdateCustomer(ctx context.Context, req *UpdateCustomerRequest) (*UpdateCustomerResponse, error) {
	customer, err := h.customerService.UpdateCustomer(ctx, req.ID, &req.Body)
	if err != nil {
		if err == services.ErrCustomerNotFound {
			return nil, huma.Error404NotFound("Customer not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to update customer", err)
	}

	return &UpdateCustomerResponse{Body: *customer}, nil
}

// DeleteCustomer deletes a customer (soft delete)
func (h *CustomerHandler) DeleteCustomer(ctx context.Context, req *DeleteCustomerRequest) (*DeleteCustomerResponse, error) {
	err := h.customerService.DeleteCustomer(ctx, req.ID)
	if err != nil {
		if err == repository.ErrCustomerNotFound {
			return nil, huma.Error404NotFound("Customer not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to delete customer", err)
	}

	resp := &DeleteCustomerResponse{}
	resp.Body.Message = "Customer deleted successfully"
	return resp, nil
}
