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

// RouteGroup represents a group of navigation items with a label (v1 shape).
// Kept for back-compat with clients that have not yet migrated to the v2
// realms/sections shape.
type RouteGroup struct {
	Label        string    `json:"label" doc:"Group label displayed in navigation"`
	LabelDisable bool      `json:"labelDisable,omitempty" doc:"Hide the group label"`
	Children     []NavItem `json:"children" doc:"Navigation items in this group"`

	// Internal fields - NOT sent to frontend (used for filtering)
	Roles       []string `json:"-"` // Required roles for entire group
	Permissions []string `json:"-"` // Required permissions for entire group
}

// NavSection is a sub-group of items within a realm (v2 shape).
type NavSection struct {
	Label    string    `json:"label" doc:"Section label displayed under the realm header"`
	Children []NavItem `json:"children" doc:"Navigation items in this section"`
}

// NavRealm is a top-level audience grouping (v2 shape).
// Key identifies the realm category; Label is the localized display string.
type NavRealm struct {
	Key      string       `json:"key" doc:"Realm key (personal | platform | business | shared)"`
	Label    string       `json:"label" doc:"Realm display label"`
	Sections []NavSection `json:"sections" doc:"Sections within this realm, in display order"`
}

// NavigationResponse is the API response for navigation.
// It emits both the v1 flat groups[] and the v2 nested realms[] so clients
// can migrate at their own pace. Once all consumers are on v2, Groups can
// be dropped.
type NavigationResponse struct {
	Groups     []RouteGroup `json:"groups" doc:"Navigation route groups (v1, deprecated)"`
	Realms     []NavRealm   `json:"realms" doc:"Navigation grouped by realm → section (v2)"`
	UserRole   string       `json:"userRole" doc:"Current user's system role"`
	TenantKind string       `json:"tenantKind,omitempty" doc:"Current tenant kind: internal | external | ''"`
	CacheKey   string       `json:"cacheKey" doc:"Cache invalidation key"`
	ExpiresIn  int          `json:"expiresIn" doc:"Cache TTL in seconds"`
}

// AdminNavItem is the unfiltered, metadata-rich projection of a single
// NavItemSpec the admin console renders at /admin/modules/navigation.
// Unlike NavItem (the public sidebar DTO), the admin view exposes every
// classification field — MinRole, Tier, ModuleName, ModuleEnabled — so
// operators can audit visibility and reorder items.
type AdminNavItem struct {
	ItemKey        string         `json:"itemKey" doc:"Stable identifier; persisted overrides reference this"`
	Name           string         `json:"name" doc:"Display name"`
	Path           string         `json:"path,omitempty" doc:"Route the item navigates to"`
	Icon           string         `json:"icon,omitempty" doc:"Icon identifier"`
	ModuleName     string         `json:"moduleName" doc:"Owning module"`
	ModuleEnabled  bool           `json:"moduleEnabled" doc:"Whether the owning module is enabled right now"`
	Realm          string         `json:"realm,omitempty"`
	Section        string         `json:"section,omitempty"`
	Group          string         `json:"group,omitempty" doc:"Legacy v1 group label"`
	Tier           string         `json:"tier,omitempty" doc:"internal | external | '' (both)"`
	MinRole        string         `json:"minRole,omitempty" doc:"Lowest system role that sees this item"`
	Active         bool           `json:"active"`
	DeclaredOrder  int            `json:"declaredOrder" doc:"Sibling index as declared in the module's NavItems()"`
	EffectiveOrder int            `json:"effectiveOrder" doc:"Sibling index after applying persisted overrides"`
	Overridden     bool           `json:"overridden" doc:"True when EffectiveOrder differs from DeclaredOrder"`
	Children       []AdminNavItem `json:"children,omitempty"`
}

// AdminNavSection groups items under a section label inside a realm. Mirrors
// NavSection but carries AdminNavItem.
type AdminNavSection struct {
	Label string         `json:"label"`
	Items []AdminNavItem `json:"items"`
}

// AdminNavRealm groups sections under a canonical realm key. Mirrors
// NavRealm but carries AdminNavSection.
type AdminNavRealm struct {
	Key      string            `json:"key"`
	Label    string            `json:"label"`
	Sections []AdminNavSection `json:"sections"`
}

// AdminNavigationResponse is what GET /v1/admin/navigation returns. The
// tree is unfiltered — every item every module declared, regardless of the
// caller's role or tenant kind. Roles + TenantKinds are echoed so the
// admin UI can render the visibility matrix without hardcoding them.
type AdminNavigationResponse struct {
	Realms      []AdminNavRealm `json:"realms" doc:"Full unfiltered nav tree grouped by realm → section"`
	Roles       []string        `json:"roles" doc:"System role hierarchy, highest privilege first"`
	TenantKinds []string        `json:"tenantKinds" doc:"Tenant-kind values used by Tier filtering"`
	// RealmsParentKey is the synthetic parentKey clients pass to PATCH
	// /v1/admin/navigation/order to reorder the realm cards themselves.
	// Constant ("__realms__") but echoed so the frontend doesn't have to
	// hardcode it.
	RealmsParentKey string `json:"realmsParentKey" doc:"Synthetic parentKey for reordering the realm cards"`
	// RealmsOverridden is true when the emitted realms slice differs from
	// the canonical (personal → platform → business → shared) order. Drives
	// the "Reset realm order" affordance in the admin UI.
	RealmsOverridden bool `json:"realmsOverridden" doc:"True when a persisted override changed the realm order"`
}
