package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/navigation/models"
	"github.com/orkestra/backend/internal/navigation/services"
	"github.com/orkestra/backend/internal/shared/middleware"
)

// NavigationHandler handles navigation HTTP requests
type NavigationHandler struct {
	navigationService services.NavigationService
}

// NewNavigationHandler creates a new navigation handler
func NewNavigationHandler(navigationService services.NavigationService) *NavigationHandler {
	return &NavigationHandler{
		navigationService: navigationService,
	}
}

// GetNavigationRequest is the request for getting navigation
type GetNavigationRequest struct{}

// GetNavigationResponse wraps the navigation response
type GetNavigationResponse struct {
	Body models.NavigationResponse `json:"navigation" doc:"Navigation data"`
}

// GetNavigation handles GET /api/v1/navigation
func (h *NavigationHandler) GetNavigation(ctx context.Context, req *GetNavigationRequest) (*GetNavigationResponse, error) {
	// Get user role from context (set by auth middleware)
	userRole, ok := middleware.GetUserRole(ctx)
	if !ok || userRole == "" {
		userRole = "guest" // Default to guest if no role found
	}

	navigation, err := h.navigationService.GetNavigationForUser(ctx, userRole)
	if err != nil {
		return nil, err
	}

	return &GetNavigationResponse{Body: *navigation}, nil
}
