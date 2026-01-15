package services

import (
	"context"

	"github.com/orkestra/backend/internal/navigation/config"
	"github.com/orkestra/backend/internal/navigation/models"
	"github.com/orkestra/backend/internal/shared/middleware"
)

// NavigationService handles navigation business logic
type NavigationService interface {
	GetNavigationForUser(ctx context.Context, userRole string) (*models.NavigationResponse, error)
}

type navigationService struct {
	menuConfig    *config.MenuConfig
	roleHierarchy middleware.RoleHierarchy
}

// NewNavigationService creates a new navigation service
func NewNavigationService(menuConfig *config.MenuConfig) NavigationService {
	return &navigationService{
		menuConfig:    menuConfig,
		roleHierarchy: middleware.DefaultRoleHierarchy,
	}
}

// GetNavigationForUser returns filtered navigation for a specific user role
func (s *navigationService) GetNavigationForUser(ctx context.Context, userRole string) (*models.NavigationResponse, error) {
	allGroups := s.menuConfig.GetGroups()
	filteredGroups := make([]models.RouteGroup, 0)

	for _, group := range allGroups {
		filteredGroup := s.filterGroup(group, userRole)
		if filteredGroup != nil && len(filteredGroup.Children) > 0 {
			filteredGroups = append(filteredGroups, *filteredGroup)
		}
	}

	return &models.NavigationResponse{
		Groups:    filteredGroups,
		UserRole:  userRole,
		CacheKey:  "nav:" + userRole,
		ExpiresIn: 300, // 5 minutes
	}, nil
}

// filterGroup filters a route group based on user role
func (s *navigationService) filterGroup(group models.RouteGroup, userRole string) *models.RouteGroup {
	// Check if user can access this group
	if !s.canAccess(userRole, group.Roles) {
		return nil
	}

	filteredChildren := s.filterNavItems(group.Children, userRole)
	if len(filteredChildren) == 0 {
		return nil
	}

	return &models.RouteGroup{
		Label:        group.Label,
		LabelDisable: group.LabelDisable,
		Children:     filteredChildren,
	}
}

// filterNavItems recursively filters navigation items based on user role
func (s *navigationService) filterNavItems(items []models.NavItem, userRole string) []models.NavItem {
	filtered := make([]models.NavItem, 0)

	for _, item := range items {
		if !s.canAccess(userRole, item.Roles) {
			continue
		}

		// Create a copy without internal fields (they're already excluded via json:"-")
		filteredItem := models.NavItem{
			Name:   item.Name,
			To:     item.To,
			Icon:   item.Icon,
			Active: item.Active,
			Exact:  item.Exact,
			Newtab: item.Newtab,
			Badge:  item.Badge,
			Label:  item.Label,
		}

		// Recursively filter children
		if len(item.Children) > 0 {
			filteredChildren := s.filterNavItems(item.Children, userRole)
			if len(filteredChildren) == 0 {
				// Skip parent if no children are visible (unless it has a direct route)
				if item.To == "" {
					continue
				}
			}
			filteredItem.Children = filteredChildren
		}

		filtered = append(filtered, filteredItem)
	}

	return filtered
}

// canAccess checks if user role can access items with required roles
// Uses the role hierarchy from auth middleware
func (s *navigationService) canAccess(userRole string, requiredRoles []string) bool {
	// If no roles specified, accessible to everyone
	if len(requiredRoles) == 0 {
		return true
	}

	// Check if user's role has permission for any of the required roles
	for _, required := range requiredRoles {
		if s.roleHierarchy.HasPermission(userRole, required) {
			return true
		}
	}

	return false
}
