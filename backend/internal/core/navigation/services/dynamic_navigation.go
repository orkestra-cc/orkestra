package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

// dynamicNavigationService builds navigation from module-declared NavItems
// and filters by user role + module enabled status at request time.
type dynamicNavigationService struct {
	navItems       []module.NavItemSpec
	enabledChecker middleware.ModuleEnabledChecker
	roleHierarchy  middleware.RoleHierarchy
}

// NewDynamicNavigationService creates a navigation service that derives its menu
// from module NavItemSpec declarations instead of a hardcoded menu config.
func NewDynamicNavigationService(items []module.NavItemSpec, checker middleware.ModuleEnabledChecker) NavigationService {
	return &dynamicNavigationService{
		navItems:       items,
		enabledChecker: checker,
		roleHierarchy:  middleware.DefaultRoleHierarchy,
	}
}

func (s *dynamicNavigationService) GetNavigationForUser(ctx context.Context, userRole string) (*models.NavigationResponse, error) {
	// Group NavItemSpecs by their Group field.
	groupMap := make(map[string][]module.NavItemSpec)
	var groupOrder []string
	for _, item := range s.navItems {
		group := item.Group
		if group == "" {
			group = "Other"
		}
		if _, exists := groupMap[group]; !exists {
			groupOrder = append(groupOrder, group)
		}
		groupMap[group] = append(groupMap[group], item)
	}

	// Build RouteGroups, filtering by role and enabled status.
	var groups []models.RouteGroup
	for _, groupLabel := range groupOrder {
		items := groupMap[groupLabel]
		children := s.filterAndConvert(ctx, items, userRole)
		if len(children) > 0 {
			groups = append(groups, models.RouteGroup{
				Label:    groupLabel,
				Children: children,
			})
		}
	}

	return &models.NavigationResponse{
		Groups:    groups,
		UserRole:  userRole,
		CacheKey:  "nav:" + userRole,
		ExpiresIn: 300,
	}, nil
}

// filterAndConvert converts NavItemSpecs to NavItems, filtering by module enabled status and role access.
func (s *dynamicNavigationService) filterAndConvert(ctx context.Context, specs []module.NavItemSpec, userRole string) []models.NavItem {
	var result []models.NavItem
	for _, spec := range specs {
		// Check module enabled status (skip items belonging to disabled modules).
		if spec.ModuleName != "" && s.enabledChecker != nil {
			if !s.enabledChecker.IsEnabled(ctx, spec.ModuleName) {
				continue
			}
		}

		// Check role access.
		if spec.MinRole != "" && !s.roleHierarchy.HasPermission(userRole, spec.MinRole) {
			continue
		}

		item := models.NavItem{
			Name:   spec.Name,
			To:     spec.Path,
			Icon:   spec.Icon,
			Active: spec.Active,
		}

		// Recursively convert children.
		if len(spec.Children) > 0 {
			item.Children = s.filterAndConvert(ctx, spec.Children, userRole)
			if len(item.Children) == 0 && item.To == "" {
				continue // Skip parent with no visible children and no direct route.
			}
		}

		result = append(result, item)
	}
	return result
}
