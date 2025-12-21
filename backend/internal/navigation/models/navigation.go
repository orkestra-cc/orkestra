package models

// Badge represents a menu item badge
type Badge struct {
	Type string `json:"type" doc:"Badge type (success, warning, danger, info, etc.)"`
	Text string `json:"text" doc:"Badge display text"`
}

// NavItem represents a single navigation item
// Internal fields (Roles, Permissions) are not serialized to JSON
type NavItem struct {
	Name     string    `json:"name" doc:"Display name of the navigation item"`
	To       string    `json:"to,omitempty" doc:"Route path for navigation"`
	Icon     any       `json:"icon,omitempty" doc:"Icon identifier (string or array for FontAwesome)"`
	Active   bool      `json:"active" doc:"Whether the item is active/enabled"`
	Exact    bool      `json:"exact,omitempty" doc:"Require exact path match for active state"`
	Newtab   bool      `json:"newtab,omitempty" doc:"Open link in new tab"`
	Badge    *Badge    `json:"badge,omitempty" doc:"Optional badge to display"`
	Label    string    `json:"label,omitempty" doc:"Additional label text"`
	Children []NavItem `json:"children,omitempty" doc:"Nested navigation items"`

	// Internal fields - NOT sent to frontend (used for filtering)
	Roles       []string `json:"-"` // Required roles to access this item
	Permissions []string `json:"-"` // Required permissions to access this item
}

// RouteGroup represents a group of navigation items with a label
type RouteGroup struct {
	Label        string    `json:"label" doc:"Group label displayed in navigation"`
	LabelDisable bool      `json:"labelDisable,omitempty" doc:"Hide the group label"`
	Children     []NavItem `json:"children" doc:"Navigation items in this group"`

	// Internal fields - NOT sent to frontend (used for filtering)
	Roles       []string `json:"-"` // Required roles for entire group
	Permissions []string `json:"-"` // Required permissions for entire group
}

// NavigationResponse is the API response for navigation
type NavigationResponse struct {
	Groups    []RouteGroup `json:"groups" doc:"Navigation route groups"`
	UserRole  string       `json:"userRole" doc:"Current user's role"`
	CacheKey  string       `json:"cacheKey" doc:"Cache invalidation key (based on role)"`
	ExpiresIn int          `json:"expiresIn" doc:"Cache TTL in seconds"`
}
