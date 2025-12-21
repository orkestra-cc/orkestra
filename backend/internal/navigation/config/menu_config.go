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
		Label: "Operators",
		Roles: []string{"operator"},
		Children: []models.NavItem{
			{
				Name:   "Dashboard",
				Icon:   "chart-pie",
				To:     "/user/dashboard",
				Active: true,
				Exact:  true,
				Roles:  []string{"operator"},
			},
			{
				Name:   "Profile",
				Icon:   "user",
				To:     "/user/profile",
				Active: true,
				Roles:  []string{"operator"},
			},
			{
				Name:   "Calendar",
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
		Label: "Administration",
		Roles: []string{"administrator"},
		Children: []models.NavItem{
			{
				Name:   "Fleet Management",
				Icon:   "truck",
				Active: true,
				Roles:  []string{"administrator"},
				Children: []models.NavItem{
					{
						Name:   "Vehicles",
						To:     "/fleet/vehicles",
						Active: true,
						Roles:  []string{"administrator"},
					},
					{
						Name:   "Cranes",
						To:     "/fleet/cranes",
						Active: true,
						Roles:  []string{"administrator"},
					},
					{
						Name:   "Tachographs",
						To:     "/fleet/tachographs",
						Active: true,
						Roles:  []string{"administrator"},
					},
				},
			},
			{
				Name:   "Deadlines",
				To:     "/admin/reports/deadlines",
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
		Label: "System Administration",
		Roles: []string{"administrator"},
		Children: []models.NavItem{
			{
				Name:   "User Management",
				Icon:   "users",
				To:     "/admin/users",
				Active: true,
				Roles:  []string{"administrator"},
			},
			{
				Name:   "Settings",
				Icon:   "cog",
				To:     "/admin/settings",
				Active: true,
				Roles:  []string{"administrator"},
			},
		},
	}
}

// buildDevelopmentRoutes creates development-only navigation with all reference pages
// Accessible by: developer only
func buildDevelopmentRoutes() models.RouteGroup {
	return models.RouteGroup{
		Label: "Development",
		Roles: []string{"developer"},
		Children: []models.NavItem{
			// Dashboards
			{
				Name:   "Dashboards",
				Icon:   "chart-pie",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{Name: "Default", To: "/reference/dashboards/default", Active: true, Roles: []string{"developer"}},
					{Name: "Analytics", To: "/reference/dashboards/analytics", Active: true, Roles: []string{"developer"}},
					{Name: "CRM", To: "/reference/dashboards/crm", Active: true, Roles: []string{"developer"}},
					{Name: "SaaS", To: "/reference/dashboards/saas", Active: true, Roles: []string{"developer"}},
					{Name: "Project Management", To: "/reference/dashboards/project-management", Active: true, Roles: []string{"developer"}},
					{Name: "Support Desk", To: "/reference/dashboards/support-desk", Active: true, Roles: []string{"developer"}},
					{Name: "User Management", To: "/reference/dashboards/user-management", Active: true, Roles: []string{"developer"}},
					{Name: "Vehicle Management", To: "/reference/dashboards/vehicle-management", Active: true, Roles: []string{"developer"}},
					{Name: "Crane Management", To: "/reference/dashboards/crane-management", Active: true, Roles: []string{"developer"}},
					{Name: "Tachograph Management", To: "/reference/dashboards/tachograph-management", Active: true, Roles: []string{"developer"}},
				},
			},
			// Components
			{
				Name:   "Components",
				Icon:   "puzzle-piece",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					// UI Elements
					{
						Name:   "UI Elements",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Accordion", To: "/reference/components/accordion", Active: true, Roles: []string{"developer"}},
							{Name: "Alerts", To: "/reference/components/alerts", Active: true, Roles: []string{"developer"}},
							{Name: "Avatar", To: "/reference/components/avatar", Active: true, Roles: []string{"developer"}},
							{Name: "Backgrounds", To: "/reference/components/backgrounds", Active: true, Roles: []string{"developer"}},
							{Name: "Badges", To: "/reference/components/badges", Active: true, Roles: []string{"developer"}},
							{Name: "Breadcrumbs", To: "/reference/components/breadcrumb", Active: true, Roles: []string{"developer"}},
							{Name: "Buttons", To: "/reference/components/buttons", Active: true, Roles: []string{"developer"}},
							{Name: "Cards", To: "/reference/components/cards", Active: true, Roles: []string{"developer"}},
							{Name: "Collapse", To: "/reference/components/collapse", Active: true, Roles: []string{"developer"}},
							{Name: "CountUp", To: "/reference/components/countup", Active: true, Roles: []string{"developer"}},
							{Name: "Dropdowns", To: "/reference/components/dropdowns", Active: true, Roles: []string{"developer"}},
							{Name: "List Groups", To: "/reference/components/list-group", Active: true, Roles: []string{"developer"}},
							{Name: "Modals", To: "/reference/components/modals", Active: true, Roles: []string{"developer"}},
							{Name: "Offcanvas", To: "/reference/components/offcanvas", Active: true, Roles: []string{"developer"}},
							{Name: "Pagination", To: "/reference/components/pagination", Active: true, Roles: []string{"developer"}},
							{Name: "Placeholder", To: "/reference/components/placeholder", Active: true, Roles: []string{"developer"}},
							{Name: "Popovers", To: "/reference/components/popovers", Active: true, Roles: []string{"developer"}},
							{Name: "Progress Bar", To: "/reference/components/progress-bar", Active: true, Roles: []string{"developer"}},
							{Name: "Search", To: "/reference/components/search", Active: true, Roles: []string{"developer"}},
							{Name: "Spinners", To: "/reference/components/spinners", Active: true, Roles: []string{"developer"}},
							{Name: "Timeline", To: "/reference/components/timeline", Active: true, Roles: []string{"developer"}},
							{Name: "Toasts", To: "/reference/components/toasts", Active: true, Roles: []string{"developer"}},
							{Name: "Tooltips", To: "/reference/components/tooltips", Active: true, Roles: []string{"developer"}},
							{Name: "Treeview", To: "/reference/components/treeview", Active: true, Roles: []string{"developer"}},
							{Name: "Typed Text", To: "/reference/components/typed-text", Active: true, Roles: []string{"developer"}},
						},
					},
					// Forms
					{
						Name:   "Forms",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Form Control", To: "/reference/forms/basic/form-control", Active: true, Roles: []string{"developer"}},
							{Name: "Input Group", To: "/reference/forms/basic/input-group", Active: true, Roles: []string{"developer"}},
							{Name: "Select", To: "/reference/forms/basic/select", Active: true, Roles: []string{"developer"}},
							{Name: "Checks", To: "/reference/forms/basic/checks", Active: true, Roles: []string{"developer"}},
							{Name: "Range", To: "/reference/forms/basic/range", Active: true, Roles: []string{"developer"}},
							{Name: "Form Layout", To: "/reference/forms/basic/layout", Active: true, Roles: []string{"developer"}},
							{Name: "Advance Select", To: "/reference/forms/advance/advance-select", Active: true, Roles: []string{"developer"}},
							{Name: "Date Picker", To: "/reference/forms/advance/date-picker", Active: true, Roles: []string{"developer"}},
							{Name: "Editor", To: "/reference/forms/advance/editor", Active: true, Roles: []string{"developer"}},
							{Name: "Emoji Picker", To: "/reference/forms/advance/emoji-button", Active: true, Roles: []string{"developer"}},
							{Name: "File Uploader", To: "/reference/forms/advance/file-uploader", Active: true, Roles: []string{"developer"}},
							{Name: "Input Mask", To: "/reference/forms/advance/input-mask", Active: true, Roles: []string{"developer"}},
							{Name: "Range Slider", To: "/reference/forms/advance/range-slider", Active: true, Roles: []string{"developer"}},
							{Name: "Rating", To: "/reference/forms/advance/rating", Active: true, Roles: []string{"developer"}},
							{Name: "Floating Labels", To: "/reference/forms/floating-labels", Active: true, Roles: []string{"developer"}},
							{Name: "Wizard Forms", To: "/reference/forms/wizard", Active: true, Roles: []string{"developer"}},
							{Name: "Validation", To: "/reference/forms/validation", Active: true, Roles: []string{"developer"}},
						},
					},
					// Tables
					{
						Name:   "Tables",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Basic Tables", To: "/reference/tables/basic-tables", Active: true, Roles: []string{"developer"}},
							{Name: "Advance Tables", To: "/reference/tables/advance-tables", Active: true, Roles: []string{"developer"}},
						},
					},
					// Navigation
					{
						Name:   "Navigation",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Navs", To: "/reference/components/navs-and-tabs/navs", Active: true, Roles: []string{"developer"}},
							{Name: "Tabs", To: "/reference/components/navs-and-tabs/tabs", Active: true, Roles: []string{"developer"}},
							{Name: "Navbar", To: "/reference/components/navs-and-tabs/navbar", Active: true, Roles: []string{"developer"}},
							{Name: "Vertical Navbar", To: "/reference/components/navs-and-tabs/vertical-navbar", Active: true, Roles: []string{"developer"}},
							{Name: "Top Navbar", To: "/reference/components/navs-and-tabs/top-navbar", Active: true, Roles: []string{"developer"}},
							{Name: "Combo Navbar", To: "/reference/components/navs-and-tabs/combo-navbar", Active: true, Roles: []string{"developer"}},
							{Name: "Double Top Navbar", To: "/reference/components/navs-and-tabs/double-top-navbar", Active: true, Roles: []string{"developer"}},
						},
					},
					// Media
					{
						Name:   "Media",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Images", To: "/reference/components/pictures/images", Active: true, Roles: []string{"developer"}},
							{Name: "Figures", To: "/reference/components/pictures/figures", Active: true, Roles: []string{"developer"}},
							{Name: "Hoverbox", To: "/reference/components/pictures/hoverbox", Active: true, Roles: []string{"developer"}},
							{Name: "Lightbox", To: "/reference/components/pictures/lightbox", Active: true, Roles: []string{"developer"}},
							{Name: "Bootstrap Carousel", To: "/reference/components/carousel/bootstrap", Active: true, Roles: []string{"developer"}},
							{Name: "Slick Carousel", To: "/reference/components/carousel/slick", Active: true, Roles: []string{"developer"}},
							{Name: "Embed Video", To: "/reference/components/videos/embed", Active: true, Roles: []string{"developer"}},
							{Name: "React Player", To: "/reference/components/videos/react-player", Active: true, Roles: []string{"developer"}},
						},
					},
					// Icons
					{
						Name:   "Icons",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Font Awesome", To: "/reference/icons/font-awesome", Active: true, Roles: []string{"developer"}},
							{Name: "React Icons", To: "/reference/icons/react-icons", Active: true, Roles: []string{"developer"}},
							{Name: "Animated Icons", To: "/reference/components/animated-icons", Active: true, Roles: []string{"developer"}},
						},
					},
					// Maps
					{
						Name:   "Maps",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Google Maps", To: "/reference/maps/google-map", Active: true, Roles: []string{"developer"}},
							{Name: "Leaflet Maps", To: "/reference/maps/leaflet-map", Active: true, Roles: []string{"developer"}},
						},
					},
					// Miscellaneous
					{
						Name:   "Miscellaneous",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "Calendar", To: "/reference/components/calendar", Active: true, Roles: []string{"developer"}},
							{Name: "Cookie Notice", To: "/reference/components/cookie-notice", Active: true, Roles: []string{"developer"}},
							{Name: "Draggable", To: "/reference/components/draggable", Active: true, Roles: []string{"developer"}},
							{Name: "Scrollbar", To: "/reference/utilities/scroll-bar", Active: true, Roles: []string{"developer"}},
							{Name: "Scrollspy", To: "/reference/components/scrollspy", Active: true, Roles: []string{"developer"}},
						},
					},
					// Widgets
					{Name: "Widgets", To: "/reference/widgets", Active: true, Roles: []string{"developer"}},
				},
			},
			// Charts
			{
				Name:   "Charts",
				Icon:   "chart-line",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{Name: "Chart.js", To: "/reference/charts/chartjs", Active: true, Roles: []string{"developer"}},
					{Name: "D3.js", To: "/reference/charts/d3js", Active: true, Roles: []string{"developer"}},
					{
						Name:   "ECharts",
						Active: true,
						Roles:  []string{"developer"},
						Children: []models.NavItem{
							{Name: "How To Use", To: "/reference/charts/echarts/how-to-use", Active: true, Roles: []string{"developer"}},
							{Name: "Line Charts", To: "/reference/charts/echarts/line-charts", Active: true, Roles: []string{"developer"}},
							{Name: "Bar Charts", To: "/reference/charts/echarts/bar-charts", Active: true, Roles: []string{"developer"}},
							{Name: "Candlestick Charts", To: "/reference/charts/echarts/candlestick-charts", Active: true, Roles: []string{"developer"}},
							{Name: "Geo Map", To: "/reference/charts/echarts/geo-map", Active: true, Roles: []string{"developer"}},
							{Name: "Scatter Charts", To: "/reference/charts/echarts/scatter-charts", Active: true, Roles: []string{"developer"}},
							{Name: "Pie Charts", To: "/reference/charts/echarts/pie-charts", Active: true, Roles: []string{"developer"}},
							{Name: "Radar Charts", To: "/reference/charts/echarts/radar-charts", Active: true, Roles: []string{"developer"}},
							{Name: "Heatmap Charts", To: "/reference/charts/echarts/heatmap-charts", Active: true, Roles: []string{"developer"}},
						},
					},
				},
			},
			// Utilities
			{
				Name:   "Utilities",
				Icon:   "tools",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{Name: "Background", To: "/reference/utilities/background", Active: true, Roles: []string{"developer"}},
					{Name: "Borders", To: "/reference/utilities/borders", Active: true, Roles: []string{"developer"}},
					{Name: "Colors", To: "/reference/utilities/colors", Active: true, Roles: []string{"developer"}},
					{Name: "Colored Links", To: "/reference/utilities/colored-links", Active: true, Roles: []string{"developer"}},
					{Name: "Display", To: "/reference/utilities/display", Active: true, Roles: []string{"developer"}},
					{Name: "Flex", To: "/reference/utilities/flex", Active: true, Roles: []string{"developer"}},
					{Name: "Float", To: "/reference/utilities/float", Active: true, Roles: []string{"developer"}},
					{Name: "Grid", To: "/reference/utilities/grid", Active: true, Roles: []string{"developer"}},
					{Name: "Position", To: "/reference/utilities/position", Active: true, Roles: []string{"developer"}},
					{Name: "Sizing", To: "/reference/utilities/sizing", Active: true, Roles: []string{"developer"}},
					{Name: "Spacing", To: "/reference/utilities/spacing", Active: true, Roles: []string{"developer"}},
					{Name: "Stretched Link", To: "/reference/utilities/stretched-link", Active: true, Roles: []string{"developer"}},
					{Name: "Text Truncation", To: "/reference/utilities/text-truncation", Active: true, Roles: []string{"developer"}},
					{Name: "Typography", To: "/reference/utilities/typography", Active: true, Roles: []string{"developer"}},
					{Name: "Vertical Align", To: "/reference/utilities/vertical-align", Active: true, Roles: []string{"developer"}},
					{Name: "Visibility", To: "/reference/utilities/visibility", Active: true, Roles: []string{"developer"}},
				},
			},
			// Documentation
			{
				Name:   "Documentation",
				Icon:   "book",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{Name: "Getting Started", To: "/reference/documentation/getting-started", Active: true, Roles: []string{"developer"}},
					{Name: "Configuration", To: "/reference/documentation/configuration", Active: true, Roles: []string{"developer"}},
					{Name: "Styling", To: "/reference/documentation/styling", Active: true, Roles: []string{"developer"}},
					{Name: "Dark Mode", To: "/reference/documentation/dark-mode", Active: true, Roles: []string{"developer"}},
					{Name: "Plugins", To: "/reference/documentation/plugin", Active: true, Roles: []string{"developer"}},
					{Name: "FAQ", To: "/reference/documentation/faq", Active: true, Roles: []string{"developer"}},
					{Name: "Design File", To: "/reference/documentation/design-file", Active: true, Roles: []string{"developer"}},
					{Name: "Changelog", To: "/reference/changelog", Active: true, Roles: []string{"developer"}},
					{Name: "Migration", To: "/reference/migration", Active: true, Roles: []string{"developer"}},
				},
			},
			// Test Pages
			{
				Name:   "Test Pages",
				Icon:   "vial",
				Active: true,
				Roles:  []string{"developer"},
				Children: []models.NavItem{
					{Name: "Auth Test", To: "/reference/test/auth", Active: true, Roles: []string{"developer"}},
					{Name: "Role Navigation", To: "/reference/test/role-navigation", Active: true, Roles: []string{"developer"}},
				},
			},
		},
	}
}
