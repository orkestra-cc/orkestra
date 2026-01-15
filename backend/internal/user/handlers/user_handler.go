package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/user/models"
	"github.com/orkestra/backend/internal/user/services"
)

// UserHandler handles user HTTP requests
type UserHandler struct {
	userService services.UserService
}

// NewUserHandler creates a new user handler
func NewUserHandler(userService services.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// Create User Request
type CreateUserRequest struct {
	Body models.CreateUserInput `json:"user" doc:"User data to create"`
}

// Create User Response
type CreateUserResponse struct {
	Body models.UserManagementResponse `json:"user" doc:"Created user data"`
}

// CreateUser handles POST /api/users
func (h *UserHandler) CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
	user, err := h.userService.CreateUser(ctx, &req.Body)
	if err != nil {
		switch err {
		case services.ErrEmailNotUnique:
			return nil, huma.Error409Conflict("Email already in use", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid input data", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to create user", err)
		}
	}

	return &CreateUserResponse{Body: *user}, nil
}

// Get User Request
type GetUserRequest struct {
	ID string `path:"id" doc:"User ID (UUID)"`
}

// Get User Response
type GetUserResponse struct {
	Body models.UserManagementResponse `json:"user" doc:"User data"`
}

// GetUser handles GET /api/users/{id}
func (h *UserHandler) GetUser(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
	user, err := h.userService.GetUser(ctx, req.ID)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid user ID", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to get user", err)
		}
	}

	return &GetUserResponse{Body: *user}, nil
}

// Update User Request
type UpdateUserRequest struct {
	ID   string                 `path:"id" doc:"User ID (UUID)"`
	Body models.UpdateUserInput `json:"user" doc:"User data to update"`
}

// Update User Response
type UpdateUserResponse struct {
	Body models.UserManagementResponse `json:"user" doc:"Updated user data"`
}

// UpdateUser handles PUT /api/users/{id}
func (h *UserHandler) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
	user, err := h.userService.UpdateUser(ctx, req.ID, &req.Body)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrEmailNotUnique:
			return nil, huma.Error409Conflict("Email already in use", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid input data", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to update user", err)
		}
	}

	return &UpdateUserResponse{Body: *user}, nil
}

// Delete User Request
type DeleteUserRequest struct {
	ID string `path:"id" doc:"User ID (UUID)"`
}

// Delete User Response
type DeleteUserResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// DeleteUser handles DELETE /api/users/{id}
func (h *UserHandler) DeleteUser(ctx context.Context, req *DeleteUserRequest) (*DeleteUserResponse, error) {
	err := h.userService.DeleteUser(ctx, req.ID)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid user ID", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to delete user", err)
		}
	}

	return &DeleteUserResponse{
		Body: struct {
			Message string `json:"message" doc:"Success message"`
		}{
			Message: "User deleted successfully",
		},
	}, nil
}

// List Users Request
type ListUsersRequest struct {
	// Query parameters for filtering
	Role           string `query:"role" doc:"Filter by user role"`
	IsActive       bool   `query:"isActive" doc:"Filter by active status"`
	EmailVerified  bool   `query:"emailVerified" doc:"Filter by email verification status"`
	Search         string `query:"search" doc:"Search in name, email, username"`
	HasExpiredDocs bool   `query:"hasExpiredDocs" doc:"Filter users with expired documents"`

	// Pagination parameters
	Page     int `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int `query:"pageSize" default:"10" minimum:"1" maximum:"100" doc:"Number of items per page"`
}

// List Users Response
type ListUsersResponse struct {
	Body models.UserManagementListResponse `json:"users" doc:"Paginated list of users"`
}

// ListUsers handles GET /api/users
func (h *UserHandler) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	filters := &models.UserFilters{
		Role:           req.Role,
		Search:         req.Search,
		HasExpiredDocs: req.HasExpiredDocs,
	}

	// Handle optional boolean flags - only set if provided
	if req.IsActive {
		filters.IsActive = &req.IsActive
	}
	if req.EmailVerified {
		filters.EmailVerified = &req.EmailVerified
	}

	pagination := &models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	users, err := h.userService.ListUsers(ctx, filters, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list users", err)
	}

	return &ListUsersResponse{Body: *users}, nil
}

// Get Users by Role Request
type GetUsersByRoleRequest struct {
	Role string `path:"role" doc:"User role to filter by"`
}

// Get Users by Role Response
type GetUsersByRoleResponse struct {
	Body struct {
		Users []models.UserManagementResponse `json:"users" doc:"List of users with the specified role"`
		Total int                             `json:"total" doc:"Total number of users"`
	}
}

// GetUsersByRole handles GET /api/users/role/{role}
func (h *UserHandler) GetUsersByRole(ctx context.Context, req *GetUsersByRoleRequest) (*GetUsersByRoleResponse, error) {
	users, err := h.userService.GetUsersByRole(ctx, req.Role)
	if err != nil {
		switch err {
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid role", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to get users by role", err)
		}
	}

	// Convert to response format
	userResponses := make([]models.UserManagementResponse, len(users))
	for i, user := range users {
		userResponses[i] = *user
	}

	return &GetUsersByRoleResponse{
		Body: struct {
			Users []models.UserManagementResponse `json:"users" doc:"List of users with the specified role"`
			Total int                             `json:"total" doc:"Total number of users"`
		}{
			Users: userResponses,
			Total: len(userResponses),
		},
	}, nil
}

// Get Users with Expired Documents Response
type GetUsersWithExpiredDocumentsResponse struct {
	Body struct {
		Users []models.UserManagementResponse `json:"users" doc:"List of users with expired documents"`
		Total int                             `json:"total" doc:"Total number of users with expired documents"`
	}
}

// GetUsersWithExpiredDocuments handles GET /api/users/expired-documents
func (h *UserHandler) GetUsersWithExpiredDocuments(ctx context.Context, req *struct{}) (*GetUsersWithExpiredDocumentsResponse, error) {
	users, err := h.userService.GetUsersWithExpiredDocuments(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get users with expired documents", err)
	}

	// Convert to response format
	userResponses := make([]models.UserManagementResponse, len(users))
	for i, user := range users {
		userResponses[i] = *user
	}

	return &GetUsersWithExpiredDocumentsResponse{
		Body: struct {
			Users []models.UserManagementResponse `json:"users" doc:"List of users with expired documents"`
			Total int                             `json:"total" doc:"Total number of users with expired documents"`
		}{
			Users: userResponses,
			Total: len(userResponses),
		},
	}, nil
}

// Get Users with Expiring Soon Documents Request
type GetUsersWithExpiringSoonDocumentsRequest struct {
	Days int `query:"days" default:"30" minimum:"1" maximum:"365" doc:"Number of days to check for expiring documents"`
}

// Get Users with Expiring Soon Documents Response
type GetUsersWithExpiringSoonDocumentsResponse struct {
	Body struct {
		Users []models.UserManagementResponse `json:"users" doc:"List of users with documents expiring soon"`
		Total int                             `json:"total" doc:"Total number of users with expiring documents"`
		Days  int                             `json:"days" doc:"Number of days used for filtering"`
	}
}

// GetUsersWithExpiringSoonDocuments handles GET /api/users/expiring-soon-documents
func (h *UserHandler) GetUsersWithExpiringSoonDocuments(ctx context.Context, req *GetUsersWithExpiringSoonDocumentsRequest) (*GetUsersWithExpiringSoonDocumentsResponse, error) {
	users, err := h.userService.GetUsersWithExpiringSoonDocuments(ctx, req.Days)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get users with expiring documents", err)
	}

	// Convert to response format
	userResponses := make([]models.UserManagementResponse, len(users))
	for i, user := range users {
		userResponses[i] = *user
	}

	return &GetUsersWithExpiringSoonDocumentsResponse{
		Body: struct {
			Users []models.UserManagementResponse `json:"users" doc:"List of users with documents expiring soon"`
			Total int                             `json:"total" doc:"Total number of users with expiring documents"`
			Days  int                             `json:"days" doc:"Number of days used for filtering"`
		}{
			Users: userResponses,
			Total: len(userResponses),
			Days:  req.Days,
		},
	}, nil
}

// Get User by Email Request
type GetUserByEmailRequest struct {
	Email string `query:"email" doc:"User email address"`
}

// GetUserByEmail handles GET /api/users/by-email
func (h *UserHandler) GetUserByEmail(ctx context.Context, req *GetUserByEmailRequest) (*GetUserResponse, error) {
	if req.Email == "" {
		return nil, huma.Error400BadRequest("Email parameter is required", nil)
	}

	user, err := h.userService.GetUserByEmail(ctx, req.Email)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid email", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to get user", err)
		}
	}

	return &GetUserResponse{Body: *user}, nil
}

// Update User Documents Request
type UpdateUserDocumentsRequest struct {
	ID   string                 `path:"id" doc:"User ID (UUID)"`
	Body models.UpdateUserInput `json:"documents" doc:"Document data to update"`
}

// UpdateUserDocuments handles PATCH /api/users/{id}/documents
func (h *UserHandler) UpdateUserDocuments(ctx context.Context, req *UpdateUserDocumentsRequest) (*UpdateUserResponse, error) {
	user, err := h.userService.UpdateUserDocuments(ctx, req.ID, &req.Body)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid input data", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to update user documents", err)
		}
	}

	return &UpdateUserResponse{Body: *user}, nil
}

// Check Document Expiry Request
type CheckDocumentExpiryRequest struct {
	ID string `path:"id" doc:"User ID (UUID)"`
}

// Check Document Expiry Response
type CheckDocumentExpiryResponse struct {
	Body struct {
		UserID           string   `json:"userId" doc:"User ID"`
		ExpiredDocuments []string `json:"expiredDocuments" doc:"List of expired documents"`
		HasExpired       bool     `json:"hasExpired" doc:"Whether user has any expired documents"`
	}
}

// CheckDocumentExpiry handles GET /api/users/{id}/check-expiry
func (h *UserHandler) CheckDocumentExpiry(ctx context.Context, req *CheckDocumentExpiryRequest) (*CheckDocumentExpiryResponse, error) {
	expired, err := h.userService.CheckDocumentExpiry(ctx, req.ID)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid user ID", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to check document expiry", err)
		}
	}

	return &CheckDocumentExpiryResponse{
		Body: struct {
			UserID           string   `json:"userId" doc:"User ID"`
			ExpiredDocuments []string `json:"expiredDocuments" doc:"List of expired documents"`
			HasExpired       bool     `json:"hasExpired" doc:"Whether user has any expired documents"`
		}{
			UserID:           req.ID,
			ExpiredDocuments: expired,
			HasExpired:       len(expired) > 0,
		},
	}, nil
}

// Get User Count Request
type GetUserCountRequest struct {
	// Query parameters for filtering (same as ListUsersRequest)
	Role           string `query:"role" doc:"Filter by user role"`
	IsActive       bool   `query:"isActive" doc:"Filter by active status"`
	EmailVerified  bool   `query:"emailVerified" doc:"Filter by email verification status"`
	Search         string `query:"search" doc:"Search in name, email, username"`
	HasExpiredDocs bool   `query:"hasExpiredDocs" doc:"Filter users with expired documents"`
}

// Get User Count Response
type GetUserCountResponse struct {
	Body struct {
		Count int64 `json:"count" doc:"Total number of users matching the filters"`
	}
}

// GetUserCount handles GET /api/users/count
func (h *UserHandler) GetUserCount(ctx context.Context, req *GetUserCountRequest) (*GetUserCountResponse, error) {
	filters := &models.UserFilters{
		Role:           req.Role,
		Search:         req.Search,
		HasExpiredDocs: req.HasExpiredDocs,
	}

	// Handle optional boolean flags - only set if provided
	if req.IsActive {
		filters.IsActive = &req.IsActive
	}
	if req.EmailVerified {
		filters.EmailVerified = &req.EmailVerified
	}

	count, err := h.userService.GetUserCount(ctx, filters)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to count users", err)
	}

	return &GetUserCountResponse{
		Body: struct {
			Count int64 `json:"count" doc:"Total number of users matching the filters"`
		}{
			Count: count,
		},
	}, nil
}
