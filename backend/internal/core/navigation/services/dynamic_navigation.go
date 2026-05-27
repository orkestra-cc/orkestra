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
// After filtering, persisted ordering overrides (Phase 2) are applied:
// top-level items inside each (realm, section) bucket are reordered by
// the sectionRootKey entry, and nested children are reordered by their
// parent's ItemKey entry. Missing items (filtered out upstream) are
// skipped in the override list silently.
//
// Permission-scoped filtering (per-org effective permissions) is still
// delegated to the frontend until the authz permission-domain-tag refactor
// lands.
type dynamicNavigationService struct {
	navItems       []module.NavItemSpec
	enabledChecker modulegate.ModuleEnabledChecker
	overrides      OverrideService // may be nil (overrides disabled / boot-time fallback)
	navItemsIndex  navItemsIndex   // pre-computed parent/child key sets for self-heal
}

// NewDynamicNavigationService creates a navigation service that derives its
// menu from module NavItemSpec declarations. overrides may be nil — in
// which case the menu always renders in declared order.
func NewDynamicNavigationService(items []module.NavItemSpec, checker modulegate.ModuleEnabledChecker, overrides OverrideService) NavigationService {
	return &dynamicNavigationService{
		navItems:       items,
		enabledChecker: checker,
		overrides:      overrides,
		navItemsIndex:  buildNavItemsIndex(items),
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

	// Load persisted ordering overrides up front, self-heal against the
	// known parent/child key sets. Failure here degrades to declared
	// order — the menu must always render even when Mongo is having a
	// bad day.
	overrideMap := map[string][]string{}
	if s.overrides != nil {
		if m, err := s.overrides.LoadMap(ctx, s.navItemsIndex.parentKeys, s.navItemsIndex.childrenByParent); err == nil {
			overrideMap = m
		}
	}

	// Reorder the source spec slice ITSELF before filtering so the
	// sibling order in the rendered tree honours the override.
	// (Filtering preserves the input order, so reordering before filter
	// is equivalent to reordering after, with one fewer pass.)
	orderedSpecs := applyOrderToSpecs(s.navItems, overrideMap[topLevelOrderKey])

	// Build one flat classified list that feeds both the v1 flat-groups shape
	// and the v2 realms → sections shape in a single pass.
	var flat []classified
	for _, spec := range orderedSpecs {
		// Recursively apply per-parent override to this spec's children.
		spec = withOrderedChildren(spec, overrideMap)

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

		flat = append(flat, classified{realm: realm, section: section, group: group, itemKey: spec.ItemKey, item: item})
	}

	// Apply per-(realm, section) overrides to the top-level positions.
	flat = applyOrderToFlatBySectionRoot(flat, overrideMap)

	groups := buildLegacyGroups(flat)
	realms := buildRealms(flat)
	// Apply the realms-level override last so it operates on the final
	// realm slice (post role/tier filter, with empty realms already
	// excluded by buildRealms).
	realms = applyOrderToRealms(realms, overrideMap[RealmsParentKey])

	return &models.NavigationResponse{
		Groups:     groups,
		Realms:     realms,
		UserRole:   userRole,
		TenantKind: tenantKind,
		CacheKey:   cacheKey(userRole, tenantKind),
		ExpiresIn:  300,
	}, nil
}

// topLevelOrderKey is a sentinel that LoadMap never returns — left in
// to make the call-site grep-friendly. Top-level reordering is always
// keyed by sectionRootKey, never by this constant.
const topLevelOrderKey = "__not_used__"

// withOrderedChildren returns a copy of spec whose Children slice has
// been reordered by overrideMap[spec.ItemKey], and recursively so for
// nested grandchildren. spec.ItemKey is always populated by the
// registry's stamping pass.
func withOrderedChildren(spec module.NavItemSpec, overrideMap map[string][]string) module.NavItemSpec {
	if len(spec.Children) == 0 {
		return spec
	}
	children := applyOrderToSpecs(spec.Children, overrideMap[spec.ItemKey])
	out := spec
	out.Children = make([]module.NavItemSpec, len(children))
	for i, c := range children {
		out.Children[i] = withOrderedChildren(c, overrideMap)
	}
	return out
}

// applyOrderToFlatBySectionRoot reorders items inside each (realm,
// section) bucket by overrideMap[sectionRootKey(realm, section)].
// Buckets that have no override stay in encounter order. Self-heals
// missing items silently.
func applyOrderToFlatBySectionRoot(flat []classified, overrideMap map[string][]string) []classified {
	if len(flat) == 0 {
		return flat
	}
	// Group entries by (realm, section) preserving first-seen order.
	type bucket struct {
		realm, section string
		entries        []classified
	}
	var bucketOrder []string
	buckets := map[string]*bucket{}
	keyOf := func(realm, section string) string { return realm + "\x00" + section }
	for _, c := range flat {
		k := keyOf(c.realm, c.section)
		b, ok := buckets[k]
		if !ok {
			b = &bucket{realm: c.realm, section: c.section}
			buckets[k] = b
			bucketOrder = append(bucketOrder, k)
		}
		b.entries = append(b.entries, c)
	}
	// Apply override per bucket, then re-flatten.
	out := make([]classified, 0, len(flat))
	for _, k := range bucketOrder {
		b := buckets[k]
		ordered := b.entries
		if keys := overrideMap[sectionRootKey(b.realm, b.section)]; len(keys) > 0 {
			byKey := make(map[string]int, len(b.entries))
			for i, e := range b.entries {
				byKey[e.itemKey] = i
			}
			used := make(map[int]bool, len(b.entries))
			reordered := make([]classified, 0, len(b.entries))
			for _, key := range keys {
				if idx, ok := byKey[key]; ok && !used[idx] {
					reordered = append(reordered, b.entries[idx])
					used[idx] = true
				}
			}
			for i, e := range b.entries {
				if !used[i] {
					reordered = append(reordered, e)
				}
			}
			ordered = reordered
		}
		out = append(out, ordered...)
	}
	return out
}

// navItemsIndex pre-computes the set of valid parentKeys and the
// children of each, used by self-heal on read and validation on write.
type navItemsIndex struct {
	parentKeys       map[string]struct{}
	childrenByParent map[string]map[string]struct{}
}

func buildNavItemsIndex(items []module.NavItemSpec) navItemsIndex {
	idx := navItemsIndex{
		parentKeys:       map[string]struct{}{},
		childrenByParent: map[string]map[string]struct{}{},
	}
	// Top-level items index by (realm, section) bucket → sectionRootKey.
	sectionItems := map[string]map[string]struct{}{}
	// Track every realm key the registry actually emits so RealmsParentKey
	// has a valid child set for write validation + self-heal.
	realmSet := map[string]struct{}{}
	for _, spec := range items {
		realm := spec.Realm
		if realm == "" {
			realm = realmShared
		}
		realmSet[realm] = struct{}{}
		section := spec.Section
		if section == "" {
			section = spec.Group
		}
		if section == "" {
			section = "Other"
		}
		root := sectionRootKey(realm, section)
		idx.parentKeys[root] = struct{}{}
		set, ok := sectionItems[root]
		if !ok {
			set = map[string]struct{}{}
			sectionItems[root] = set
		}
		set[spec.ItemKey] = struct{}{}
		// Recurse into children — every item with declared children is
		// itself a valid parent for nested reorder.
		indexChildren(&idx, spec)
	}
	for k, v := range sectionItems {
		idx.childrenByParent[k] = v
	}
	// Always register RealmsParentKey, even if no module declared any
	// realm — keeps the parent reachable for an empty-tree install (the
	// override would be a no-op but the PATCH wouldn't 404).
	idx.parentKeys[RealmsParentKey] = struct{}{}
	idx.childrenByParent[RealmsParentKey] = realmSet
	return idx
}

func indexChildren(idx *navItemsIndex, spec module.NavItemSpec) {
	if len(spec.Children) == 0 {
		return
	}
	idx.parentKeys[spec.ItemKey] = struct{}{}
	set, ok := idx.childrenByParent[spec.ItemKey]
	if !ok {
		set = map[string]struct{}{}
		idx.childrenByParent[spec.ItemKey] = set
	}
	for _, c := range spec.Children {
		set[c.ItemKey] = struct{}{}
		indexChildren(idx, c)
	}
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
// itemKey carries the SDK ItemKey of the underlying NavItemSpec so the
// per-section override layer can reorder this slice without polluting the
// public NavItem DTO with an internal identifier.
type classified struct {
	realm   string
	section string
	group   string
	itemKey string
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
