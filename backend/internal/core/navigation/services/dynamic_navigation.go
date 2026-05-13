package services

import (
	"context"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
	"github.com/orkestra/backend/internal/core/navigation/models"
)

// dynamicNavigationService builds navigation from module-declared NavItems
// and filters by:
//  1. module enabled state (runtime toggle via /admin/modules)
//  2. tenant kind (NavItemSpec.Tier vs. caller's tenant kind in ctx)
//  3. system role (NavItemSpec.MinRole vs. caller's system role)
//
// Permission-scoped filtering (per-org effective permissions) is still
// delegated to the frontend until the authz permission-domain-tag refactor
// lands.
type dynamicNavigationService struct {
	navItems       []module.NavItemSpec
	enabledChecker modulegate.ModuleEnabledChecker
}

// NewDynamicNavigationService creates a navigation service that derives its
// menu from module NavItemSpec declarations.
func NewDynamicNavigationService(items []module.NavItemSpec, checker modulegate.ModuleEnabledChecker) NavigationService {
	return &dynamicNavigationService{
		navItems:       items,
		enabledChecker: checker,
	}
}

// Realm keys. Realm labels are canonicalized by realmLabel below so every
// module that sets Realm="platform" renders under the same "Operator" header.
const (
	realmPersonal = "personal"
	realmPlatform = "platform"
	realmBusiness = "business"
	realmShared   = "shared"
)

// realmOrder fixes the display order of realms in the v2 response. Any realm
// key not listed here falls back to the shared realm.
var realmOrder = []string{realmPersonal, realmPlatform, realmBusiness, realmShared}

// realmLabel maps a realm key to its canonical display label.
func realmLabel(key string) string {
	switch key {
	case realmPersonal:
		return "My workspace"
	case realmPlatform:
		return "Administration"
	case realmBusiness:
		return "Business"
	case realmShared:
		return "Tools"
	default:
		return key
	}
}

// roleRank returns the priority of a system role; higher = more privileged.
// Matches frontend ROLE_HIERARCHY and the authz system roles.
func roleRank(role string) int {
	switch role {
	case "super_admin":
		return 6
	case "administrator":
		return 5
	case "developer":
		return 4
	case "manager":
		return 3
	case "operator":
		return 2
	case "guest":
		return 1
	default:
		return 0
	}
}

// meetsMinRole returns true when userRole is at or above minRole in the
// hierarchy. An empty minRole means no restriction.
func meetsMinRole(userRole, minRole string) bool {
	if minRole == "" {
		return true
	}
	return roleRank(userRole) >= roleRank(minRole)
}

// tierAllows returns true when an item with the given tier should be visible
// to a caller acting in tenantKind. An empty tier means visible to both.
// An empty tenantKind (no tenant context) only sees untiered items.
func tierAllows(tier, tenantKind string) bool {
	if tier == "" {
		return true
	}
	return tier == tenantKind
}

func (s *dynamicNavigationService) GetNavigationForUser(ctx context.Context, userRole string) (*models.NavigationResponse, error) {
	tenantKind := ctxauth.TenantKindFromContext(ctx)

	// Build one flat classified list that feeds both the v1 flat-groups shape
	// and the v2 realms → sections shape in a single pass.
	var flat []classified
	for _, spec := range s.navItems {
		item, ok := s.convert(ctx, spec, userRole, tenantKind)
		if !ok {
			continue
		}

		realm := spec.Realm
		if realm == "" {
			realm = realmShared
		}
		section := spec.Section
		if section == "" {
			section = spec.Group // fall back to legacy group as section label
		}
		group := spec.Group
		if group == "" {
			group = section
		}
		if group == "" {
			group = "Other"
		}

		flat = append(flat, classified{realm: realm, section: section, group: group, item: item})
	}

	groups := buildLegacyGroups(flat)
	realms := buildRealms(flat)

	return &models.NavigationResponse{
		Groups:     groups,
		Realms:     realms,
		UserRole:   userRole,
		TenantKind: tenantKind,
		CacheKey:   cacheKey(userRole, tenantKind),
		ExpiresIn:  300,
	}, nil
}

// convert applies the visibility filters and returns the rendered NavItem.
// Returns ok=false when the item (or all its children) should be hidden.
func (s *dynamicNavigationService) convert(ctx context.Context, spec module.NavItemSpec, userRole, tenantKind string) (models.NavItem, bool) {
	if spec.ModuleName != "" && s.enabledChecker != nil {
		if !s.enabledChecker.IsEnabled(ctx, spec.ModuleName) {
			return models.NavItem{}, false
		}
	}
	if !tierAllows(spec.Tier, tenantKind) {
		return models.NavItem{}, false
	}
	if !meetsMinRole(userRole, spec.MinRole) {
		return models.NavItem{}, false
	}

	item := models.NavItem{
		Name:   spec.Name,
		To:     spec.Path,
		Icon:   spec.Icon,
		Active: spec.Active,
	}

	for _, child := range spec.Children {
		if converted, ok := s.convert(ctx, child, userRole, tenantKind); ok {
			item.Children = append(item.Children, converted)
		}
	}

	// A parent with no link and no visible children collapses away.
	if item.To == "" && len(item.Children) == 0 {
		return models.NavItem{}, false
	}
	return item, true
}

// buildLegacyGroups preserves the v1 flat-groups shape using each item's
// Group (or its Section fallback), in discovery order.
func buildLegacyGroups(flat []classified) []models.RouteGroup {
	groupMap := make(map[string][]models.NavItem)
	var order []string
	for _, c := range flat {
		if _, seen := groupMap[c.group]; !seen {
			order = append(order, c.group)
		}
		groupMap[c.group] = append(groupMap[c.group], c.item)
	}

	groups := make([]models.RouteGroup, 0, len(order))
	for _, label := range order {
		groups = append(groups, models.RouteGroup{
			Label:    label,
			Children: groupMap[label],
		})
	}
	return groups
}

// buildRealms groups items into Realm → Section for the v2 shape. Realms are
// emitted in realmOrder; within a realm, sections appear in the order they
// were first encountered.
func buildRealms(flat []classified) []models.NavRealm {
	type sectionBucket struct {
		label string
		items []models.NavItem
	}
	realmBuckets := map[string]*struct {
		order    []string
		sections map[string]*sectionBucket
	}{}

	for _, c := range flat {
		rb, ok := realmBuckets[c.realm]
		if !ok {
			rb = &struct {
				order    []string
				sections map[string]*sectionBucket
			}{sections: map[string]*sectionBucket{}}
			realmBuckets[c.realm] = rb
		}
		sec, ok := rb.sections[c.section]
		if !ok {
			sec = &sectionBucket{label: c.section}
			rb.sections[c.section] = sec
			rb.order = append(rb.order, c.section)
		}
		sec.items = append(sec.items, c.item)
	}

	// Emit realms in canonical order, then any unknown realm in discovery order.
	seen := map[string]bool{}
	var realms []models.NavRealm
	emit := func(key string) {
		rb, ok := realmBuckets[key]
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		sections := make([]models.NavSection, 0, len(rb.order))
		for _, label := range rb.order {
			sec := rb.sections[label]
			sections = append(sections, models.NavSection{
				Label:    sec.label,
				Children: sec.items,
			})
		}
		realms = append(realms, models.NavRealm{
			Key:      key,
			Label:    realmLabel(key),
			Sections: sections,
		})
	}
	for _, k := range realmOrder {
		emit(k)
	}
	// Unknown realms keep encounter order: iterate flat[] again.
	for _, c := range flat {
		emit(c.realm)
	}
	return realms
}

// classified pairs a visible NavItem with the grouping keys used to render it
// in both the v1 flat-groups shape and the v2 realms-and-sections shape.
type classified struct {
	realm   string
	section string
	group   string
	item    models.NavItem
}

func cacheKey(userRole, tenantKind string) string {
	if tenantKind == "" {
		return "nav:" + userRole
	}
	return "nav:" + userRole + ":" + tenantKind
}

// Compile-time check that the constants in iface match what we filter against.
// Forces a build break if iface renames them.
var _ = iface.TenantKindInternal
var _ = iface.TenantKindExternal
