package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/navigation/models"
)

// NavigationService handles navigation business logic.
type NavigationService interface {
	GetNavigationForUser(ctx context.Context, userRole string) (*models.NavigationResponse, error)
}
