package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/modulegate"
)

// AdminNavigationService returns the full unfiltered nav tree with the
// metadata the admin console needs to render the visibility matrix and
// reorder UI. It is intentionally NOT role/tier-filtered: operators must
// see every item every module declared, regardless of who would actually
// see it in the sidebar.
type AdminNavigationService interface {
	GetAdminTree(ctx context.Context) (*models.AdminNavigationResponse, error)
}

type adminNavigationService struct {
	navItems       []module.NavItemSpec
	enabledChecker modulegate.ModuleEnabledChecker
	overrides      OverrideService // may be nil — admin tree then reports declared order
	itemsIndex     navItemsIndex
}

// NewAdminNavigationService builds a read-only admin view over the same
// nav-items slice the public NavigationService reads. The enabled checker
// is optional — when nil, every item is reported as moduleEnabled=true.
// The overrides service is optional — when nil, EffectiveOrder == DeclaredOrder
// for every item.
func NewAdminNavigationService(items []module.NavItemSpec, checker modulegate.ModuleEnabledChecker, overrides OverrideService) AdminNavigationService {
	return &adminNavigationService{
		navItems:       items,
		enabledChecker: checker,
		overrides:      overrides,
		itemsIndex:     buildNavItemsIndex(items),
	}
}

// systemRoles is the role hierarchy echoed in the admin response, ordered
// from most to least privileged. Mirrors roleRank() above and the frontend
// ROLE_HIERARCHY constant.
var systemRoles = []string{
	"super_admin",
	"administrator",
	"developer",
	"manager",
	"operator",
	"guest",
}

// tenantKinds is the closed set of tenant-kind values the Tier filter
// compares against. Empty Tier = visible to both.
var tenantKinds = []string{"internal", "external"}

func (s *adminNavigationService) GetAdminTree(ctx context.Context) (*models.AdminNavigationResponse, error) {
	overrideMap := map[string][]string{}
	if s.overrides != nil {
		if m, err := s.overrides.LoadMap(ctx, s.itemsIndex.parentKeys, s.itemsIndex.childrenByParent); err == nil {
			overrideMap = m
		}
	}

	// Group top-level items by (realm, section) in declaration order so the
	// admin view mirrors the sidebar's layout exactly (minus role/tier
	// filtering). DeclaredOrder is the sibling index inside each parent;
	// EffectiveOrder is filled in once we know each section's override.
	type sectionBucket struct {
		label string
		items []models.AdminNavItem
	}
	type realmBucket struct {
		sectionOrder []string
		sections     map[string]*sectionBucket
	}
	realmBuckets := map[string]*realmBucket{}
	var realmEncounter []string

	for _, spec := range s.navItems {
		realm := spec.Realm
		if realm == "" {
			realm = realmShared
		}
		section := spec.Section
		if section == "" {
			section = spec.Group
		}
		if section == "" {
			section = "Other"
		}

		rb, ok := realmBuckets[realm]
		if !ok {
			rb = &realmBucket{sections: map[string]*sectionBucket{}}
			realmBuckets[realm] = rb
			realmEncounter = append(realmEncounter, realm)
		}
		sec, ok := rb.sections[section]
		if !ok {
			sec = &sectionBucket{label: section}
			rb.sections[section] = sec
			rb.sectionOrder = append(rb.sectionOrder, section)
		}
		idx := len(sec.items)
		sec.items = append(sec.items, s.toAdminItem(ctx, spec, idx, overrideMap))
	}

	// Apply (realm, section) overrides to top-level items: reorder
	// in-place and stamp EffectiveOrder + Overridden.
	for _, rb := range realmBuckets {
		for _, sec := range rb.sections {
			ordered := applyOrderToAdminItems(sec.items, overrideMap[sectionRootKey(realmKeyOf(sec.items), sec.label)])
			// realmKeyOf is a no-op when sec.items is empty; otherwise
			// every item in the section shares the same realm.
			for i := range ordered {
				ordered[i].EffectiveOrder = i
				ordered[i].Overridden = ordered[i].DeclaredOrder != i
			}
			sec.items = ordered
		}
	}

	// Realms in canonical order first; unknown realm keys append in
	// encounter order. Matches buildRealms() in dynamic_navigation.go so
	// the admin view does not reshuffle realms compared to the sidebar.
	seen := map[string]bool{}
	var realms []models.AdminNavRealm
	emit := func(key string) {
		rb, ok := realmBuckets[key]
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		sections := make([]models.AdminNavSection, 0, len(rb.sectionOrder))
		for _, label := range rb.sectionOrder {
			sec := rb.sections[label]
			sections = append(sections, models.AdminNavSection{
				Label: sec.label,
				Items: sec.items,
			})
		}
		realms = append(realms, models.AdminNavRealm{
			Key:      key,
			Label:    realmLabel(key),
			Sections: sections,
		})
	}
	for _, k := range realmOrder {
		emit(k)
	}
	for _, k := range realmEncounter {
		emit(k)
	}

	// Snapshot the canonical (pre-override) realm-key order so we can
	// compare against the post-override order to set RealmsOverridden.
	canonical := make([]string, 0, len(realms))
	for _, r := range realms {
		canonical = append(canonical, r.Key)
	}
	realms = applyOrderToAdminRealms(realms, overrideMap[RealmsParentKey])
	realmsOverridden := false
	for i, r := range realms {
		if i >= len(canonical) || canonical[i] != r.Key {
			realmsOverridden = true
			break
		}
	}

	return &models.AdminNavigationResponse{
		Realms:           realms,
		Roles:            append([]string(nil), systemRoles...),
		TenantKinds:      append([]string(nil), tenantKinds...),
		RealmsParentKey:  RealmsParentKey,
		RealmsOverridden: realmsOverridden,
	}, nil
}

// toAdminItem projects one NavItemSpec into an AdminNavItem, recursing
// into children. ItemKey is whatever the registry stamped (the SDK default
// or the module's explicit value). ModuleEnabled is resolved per-item via
// the checker so the admin UI can dim disabled-module entries without
// hiding them. Children are reordered per the override map (keyed by the
// parent's ItemKey); EffectiveOrder + Overridden are stamped post-reorder
// so the UI can render the "moved" indicator on each row.
func (s *adminNavigationService) toAdminItem(ctx context.Context, spec module.NavItemSpec, declaredOrder int, overrideMap map[string][]string) models.AdminNavItem {
	// Resolve the two role/tenant-independent gates once, sharing the exact
	// logic the public sidebar uses (resolveGates), so the role-matrix and the
	// live menu never disagree.
	moduleEnabled, configSatisfied := resolveGates(ctx, s.enabledChecker, spec)

	// Build children first — the parent-collapse leg of the visibility table
	// reads each child's computed cells.
	var children []models.AdminNavItem
	if len(spec.Children) > 0 {
		children = make([]models.AdminNavItem, 0, len(spec.Children))
		for i, child := range spec.Children {
			children = append(children, s.toAdminItem(ctx, child, i, overrideMap))
		}
		children = applyOrderToAdminItems(children, overrideMap[spec.ItemKey])
		for i := range children {
			children[i].EffectiveOrder = i
			children[i].Overridden = children[i].DeclaredOrder != i
		}
	}

	return models.AdminNavItem{
		ItemKey:         spec.ItemKey,
		Name:            spec.Name,
		Path:            spec.Path,
		Icon:            spec.Icon,
		ModuleName:      spec.ModuleName,
		ModuleEnabled:   moduleEnabled,
		Realm:           spec.Realm,
		Section:         spec.Section,
		Group:           spec.Group,
		Tier:            spec.Tier,
		MinRole:         spec.MinRole,
		Active:          spec.Active,
		DeclaredOrder:   declaredOrder,
		EffectiveOrder:  declaredOrder, // overwritten by the caller if reordered
		RequiresConfig:  spec.RequiresConfig,
		ConfigSatisfied: configSatisfied,
		Visibility:      s.computeVisibility(spec, moduleEnabled, configSatisfied, children),
		Children:        children,
	}
}

// computeVisibility builds the per-role × tenant-kind truth table for one item.
// Each cell is the shared self-gate result (evalSelfVisible) with parent-collapse
// layered on: a no-path group item is hidden (reason parent_collapsed) for a
// (role, kind) when none of its children are visible there — mirroring the
// collapse rule in dynamicNavigationService.convert.
func (s *adminNavigationService) computeVisibility(spec module.NavItemSpec, moduleEnabled, configSatisfied bool, children []models.AdminNavItem) map[string]models.NavRoleVisibility {
	vis := make(map[string]models.NavRoleVisibility, len(systemRoles))
	for _, role := range systemRoles {
		var rv models.NavRoleVisibility
		for _, kind := range tenantKinds {
			visible, reason := evalSelfVisible(moduleEnabled, configSatisfied, spec.Tier, role, spec.MinRole, kind)
			if visible && spec.Path == "" {
				anyChild := false
				for _, ch := range children {
					if cellFor(ch.Visibility[role], kind).Visible {
						anyChild = true
						break
					}
				}
				if !anyChild {
					visible = false
					reason = ReasonParentCollapsed
				}
			}
			cell := models.NavVisibilityCell{Visible: visible, Reason: reason}
			if kind == "external" {
				rv.External = cell
			} else {
				rv.Internal = cell
			}
		}
		vis[role] = rv
	}
	return vis
}

// realmKeyOf returns the realm key shared by every item in a section
// bucket. Every item in the same section comes from the same realm
// (that's the grouping invariant); we just read the first one. Returns
// "shared" for an empty bucket to keep the synthetic key well-formed.
func realmKeyOf(items []models.AdminNavItem) string {
	if len(items) == 0 {
		return realmShared
	}
	r := items[0].Realm
	if r == "" {
		return realmShared
	}
	return r
}
