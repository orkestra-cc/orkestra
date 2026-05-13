package handlers

import (
	"context"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/internal/core/navigation/services"
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

// GetNavigation handles GET /v1/navigation
func (h *NavigationHandler) GetNavigation(ctx context.Context, req *GetNavigationRequest) (*GetNavigationResponse, error) {
	// Use the user's global system role as the filter key. In the future
	// this should be replaced with an authz.GetEffectivePermissions call
	// so the menu only shows items the user can actually access.
	userRole, ok := ctxauth.GetSystemRole(ctx)
	if !ok || userRole == "" {
		userRole = "guest"
	}

	navigation, err := h.navigationService.GetNavigationForUser(ctx, userRole)
	if err != nil {
		return nil, err
	}

	return &GetNavigationResponse{Body: *navigation}, nil
}
