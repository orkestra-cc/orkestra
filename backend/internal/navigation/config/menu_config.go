package config

import (
	"os"

	"github.com/orkestra/backend/internal/navigation/models"
)

// MenuConfig holds all navigation menu definitions
type MenuConfig struct {
	groups []models.RouteGroup
}

// NewMenuConfig creates the application menu configuration
func NewMenuConfig() *MenuConfig {
	groups := []models.RouteGroup{
		buildSuperAdminRoutes(),
		buildAdminRoutes(),
		buildOperatorRoutes(),
	}

	// Add development routes only in development mode
	if isDevelopment() {
		groups = append(groups, buildDevelopmentRoutes())
	}

	return &MenuConfig{
		groups: groups,
	}
}

// GetGroups returns all menu groups
func (m *MenuConfig) GetGroups() []models.RouteGroup {
	return m.groups
}

// isDevelopment checks if running in development mode
func isDevelopment() bool {
	env := os.Getenv("APP_ENV")
	return env == "" || env == "development" || env == "dev"
}

// buildOperatorRoutes creates operator-level navigation
// Accessible by: operator, manager, administrator, ceo, developer
func buildOperatorRoutes() models.RouteGroup {
	return models.RouteGroup{
		Label: "Operatori",
		Roles: []string{"operator"},
		Children: []models.NavItem{
			{
				Name:   "Cruscotto",
				Icon:   "chart-pie",
				To:     "/user/dashboard",
				Active: true,
				Exact:  true,
				Roles:  []string{"operator"},
			},
			{
				Name:   "Profilo",
				Icon:   "user",
				To:     "/user/profile",
				Active: true,
				Roles:  []string{"operator"},
			},
			{
				Name:   "Calendario",
				Icon:   "calendar-alt",
				To:     "/user/calendar",
				Active: true,
				Roles:  []string{"operator"},
			},
		},
	}
}

// buildAdminRoutes creates administrator-level navigation
// Accessible by: administrator, ceo, developer
func buildAdminRoutes() models.RouteGroup {
	return models.RouteGroup{
		Label: "Amministrazione",
		Roles: []string{"administrator"},
		Children: []models.NavItem{
			{
				Name:   "Gestione flotta",
				Icon:   "truck",
				Active: true,
				Roles:  []string{"administrator"},
				Children: []models.NavItem{
					{
						Name:   "Mezzi",
						To:     "/fleet/vehicles",
						Active: true,
						Roles:  []string{"administrator"},
					},
					{
						Name:   "Gru",
						To:     "/fleet/cranes",
						Active: true,
						Roles:  []string{"administrator"},
					},
					{
						Name:   "Tachigrafi",
						To:     "/fleet/tachographs",
						Active: true,
						Roles:  []string{"administrator"},
					},
				},
			},
			{
				Name:   "Scadenze",
				To:     "/reports/deadlines",
				Icon:   "calendar-check",
				Active: true,
				Roles:  []string{"manager"},
			},
		},
	}
}

// buildSuperAdminRoutes creates super administrator system management navigation
// Accessible by: administrator, ceo, developer
func buildSuperAdminRoutes() models.RouteGroup {
	return models.RouteGroup{
		Label: "Amministrazione Sistema",
		Roles: []string{"administrator"},
		Children: []models.NavItem{
			{
				Name:   "Gestione utenti",
				Icon:   "users",
				To:     "/admin/users",
				Active: true,
				Roles:  []string{"administrator"},
			},
			{
				Name:   "Impostazioni",
				Icon:   "cog",
				To:     "/admin/settings",
				Active: true,
				Roles:  []string{"administrator"},
			},
		},
	}
}

// buildDevelopmentRoutes creates development-only navigation
// Accessible by: developer only
func buildDevelopmentRoutes() models.RouteGroup {
	return models.RouteGroup{
		Label: "Sviluppo",
		Roles: []string{"developer"},
		Children: []models.NavItem{
			{
				Name:   "Cruscotto",
				Icon:   "chart-pie",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{
						Name:   "Predefinito",
						To:     "/",
						Active: true,
						Exact:  true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Analisi",
						To:     "/dashboard/analytics",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "CRM",
						To:     "/dashboard/crm",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Gestione",
						To:     "/dashboard/project-management",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "SaaS",
						To:     "/dashboard/saas",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Supporto tecnico",
						To:     "/dashboard/support-desk",
						Active: true,
						Roles:  []string{"developer"},
					},
				},
			},
			{
				Name:   "Applicazioni",
				Icon:   "th",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{
						Name:   "Calendario",
						To:     "/app/calendar",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Chat",
						To:     "/app/chat",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Kanban",
						To:     "/app/kanban",
						Active: true,
						Roles:  []string{"developer"},
					},
				},
			},
			{
				Name:   "Componenti",
				Icon:   "puzzle-piece",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{
						Name:   "Accordion",
						To:     "/components/accordion",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Alerts",
						To:     "/components/alerts",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Buttons",
						To:     "/components/buttons",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Cards",
						To:     "/components/cards",
						Active: true,
						Roles:  []string{"developer"},
					},
					{
						Name:   "Modals",
						To:     "/components/modals",
						Active: true,
						Roles:  []string{"developer"},
					},
				},
			},
			{
				Name:   "Documentazione",
				Icon:   "book",
				To:     "/documentation/getting-started",
				Active: true,
				Roles:  []string{"developer"},
			},
		},
	}
}
