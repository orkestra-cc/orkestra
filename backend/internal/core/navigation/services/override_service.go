package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/internal/core/navigation/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// NavItemsIndexAccessor exposes the registry's pre-computed parent/child
// key sets to the handler without leaking the unexported navItemsIndex
// type. The handler hands the snapshot into SetOrder for validation.
type NavItemsIndexAccessor interface {
	Snapshot() (parentKeys map[string]struct{}, childrenByParent map[string]map[string]struct{})
}

// NewNavItemsIndexAccessor freezes a single index over the registry's
// nav-items slice. The returned snapshot is read-only; the maps are NOT
// defensively copied because callers (the override service) only read.
func NewNavItemsIndexAccessor(items []module.NavItemSpec) NavItemsIndexAccessor {
	return &navItemsIndexAccessor{idx: buildNavItemsIndex(items)}
}

type navItemsIndexAccessor struct {
	idx navItemsIndex
}

func (a *navItemsIndexAccessor) Snapshot() (map[string]struct{}, map[string]map[string]struct{}) {
	return a.idx.parentKeys, a.idx.childrenByParent
}

// OverrideService is the read+write surface for navigation_overrides.
// Reads are used by both the public and the admin trees; writes flow
// only from the admin endpoints.
type OverrideService interface {
	// LoadMap returns parentKey → orderedChildren. Self-heals stale
	// entries by dropping ItemKeys absent from the known-keys set
	// (logs a warn, never errors). validParents is the set of
	// parentKeys the running registry recognises; childrenByParent
	// lists each parent's actually-declared children (ItemKey set).
	LoadMap(ctx context.Context, validParents map[string]struct{}, childrenByParent map[string]map[string]struct{}) (map[string][]string, error)

	// SetOrder validates + persists a single parent's ordering. Rejects
	// duplicates, unknown parent, and child keys that aren't real
	// children of the parent.
	SetOrder(ctx context.Context, parentKey string, orderedChildren []string, validParents map[string]struct{}, childrenByParent map[string]map[string]struct{}, updatedBy string) (*models.NavOverride, error)

	// ClearOrder drops the override for parentKey. Missing doc is OK.
	ClearOrder(ctx context.Context, parentKey string) error
}

type overrideService struct {
	repo   repository.OverrideRepository
	logger *slog.Logger
}

func NewOverrideService(repo repository.OverrideRepository, logger *slog.Logger) OverrideService {
	if logger == nil {
		logger = slog.Default()
	}
	return &overrideService{repo: repo, logger: logger}
}

func (s *overrideService) LoadMap(ctx context.Context, validParents map[string]struct{}, childrenByParent map[string]map[string]struct{}) (map[string][]string, error) {
	docs, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(docs))
	for _, d := range docs {
		if _, ok := validParents[d.ParentKey]; !ok {
			s.logger.Warn("nav override references unknown parent; ignoring",
				slog.String("parentKey", d.ParentKey),
				slog.Int("orderedCount", len(d.OrderedChildren)))
			continue
		}
		known := childrenByParent[d.ParentKey]
		cleaned := make([]string, 0, len(d.OrderedChildren))
		for _, key := range d.OrderedChildren {
			if _, ok := known[key]; ok {
				cleaned = append(cleaned, key)
			} else {
				s.logger.Warn("nav override references unknown child; dropping",
					slog.String("parentKey", d.ParentKey),
					slog.String("itemKey", key))
			}
		}
		if len(cleaned) > 0 {
			out[d.ParentKey] = cleaned
		}
	}
	return out, nil
}

func (s *overrideService) SetOrder(ctx context.Context, parentKey string, orderedChildren []string, validParents map[string]struct{}, childrenByParent map[string]map[string]struct{}, updatedBy string) (*models.NavOverride, error) {
	parentKey = strings.TrimSpace(parentKey)
	if parentKey == "" {
		return nil, errcode.BadRequest(errcode.NavigationOverrideUnknownParent, "parentKey is required")
	}
	if _, ok := validParents[parentKey]; !ok {
		return nil, errcode.NotFound(errcode.NavigationOverrideUnknownParent, "unknown parentKey")
	}
	known := childrenByParent[parentKey]

	seen := make(map[string]struct{}, len(orderedChildren))
	cleaned := make([]string, 0, len(orderedChildren))
	for _, key := range orderedChildren {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, errcode.BadRequest(errcode.NavigationOverrideChildNotFound, "empty itemKey in orderedChildren")
		}
		if _, dup := seen[key]; dup {
			return nil, errcode.BadRequest(errcode.NavigationOverrideDuplicateChild, "duplicate itemKey: "+key)
		}
		if _, ok := known[key]; !ok {
			return nil, errcode.UnprocessableEntity(errcode.NavigationOverrideChildNotFound, "itemKey is not a child of parentKey: "+key)
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, key)
	}

	return s.repo.Upsert(ctx, parentKey, cleaned, updatedBy)
}

func (s *overrideService) ClearOrder(ctx context.Context, parentKey string) error {
	parentKey = strings.TrimSpace(parentKey)
	if parentKey == "" {
		return errcode.BadRequest(errcode.NavigationOverrideUnknownParent, "parentKey is required")
	}
	if err := s.repo.Delete(ctx, parentKey); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	return nil
}

// sectionRootKey returns the synthetic parentKey for the top-level items
// inside one (realm, section) bucket. Used by both the public and admin
// surfaces so the override layer keys off the same string everywhere.
//
// Keep in sync with the (realm, section) defaulting in
// dynamic_navigation.go::GetNavigationForUser and
// admin_navigation_service.go::GetAdminTree — same fallbacks
// (realm→"shared", section→spec.Group→"Other") must apply.
func sectionRootKey(realm, section string) string {
	if realm == "" {
		realm = realmShared
	}
	if section == "" {
		section = "Other"
	}
	return "__root." + realm + "." + slugifySection(section)
}

// RealmsParentKey is the synthetic parentKey for reordering the top-level
// realm cards themselves (personal/platform/business/shared). Exported so
// the handler validation set and the frontend can both reference one
// constant. Distinct prefix from sectionRootKey ("__root.") so the two
// can never collide.
const RealmsParentKey = "__realms__"

// slugifySection is the section-label slugifier — same shape as the SDK's
// slugifyNavName but kept local so the navigation services don't depend
// on SDK-internal helpers. Lowercase, non-alphanumeric → "-", trimmed.
func slugifySection(s string) string {
	out := make([]byte, 0, len(s))
	prevHyphen := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
			prevHyphen = false
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			out = append(out, c)
			prevHyphen = false
		default:
			if !prevHyphen && len(out) > 0 {
				out = append(out, '-')
				prevHyphen = true
			}
		}
	}
	if n := len(out); n > 0 && out[n-1] == '-' {
		out = out[:n-1]
	}
	if len(out) == 0 {
		return "other"
	}
	return string(out)
}

// applySiblingOrder reorders `siblings` in place: items present in
// `orderedKeys` (lookup by ItemKey) move to that position in the listed
// order; items missing from `orderedKeys` append after, in their
// original relative order. Self-heals: orderedKeys may legitimately
// reference items not present in `siblings` (e.g., role-filtered out on
// the public surface) — those are skipped silently.
//
// Generic-ish — works on any slice where the element has an ItemKey
// accessor. Kept as two concrete overloads for AdminNavItem and the SDK
// NavItemSpec siblings used by dynamic_navigation rather than a real
// generic because the call sites differ in zero meaningful way and
// concrete types read more clearly at the call site.
func applyOrderToAdminItems(siblings []models.AdminNavItem, orderedKeys []string) []models.AdminNavItem {
	if len(orderedKeys) == 0 || len(siblings) == 0 {
		return siblings
	}
	byKey := make(map[string]int, len(siblings))
	for i := range siblings {
		byKey[siblings[i].ItemKey] = i
	}
	used := make(map[int]bool, len(siblings))
	out := make([]models.AdminNavItem, 0, len(siblings))
	for _, key := range orderedKeys {
		if idx, ok := byKey[key]; ok && !used[idx] {
			out = append(out, siblings[idx])
			used[idx] = true
		}
	}
	for i := range siblings {
		if !used[i] {
			out = append(out, siblings[i])
		}
	}
	return out
}

func applyOrderToSpecs(siblings []module.NavItemSpec, orderedKeys []string) []module.NavItemSpec {
	if len(orderedKeys) == 0 || len(siblings) == 0 {
		return siblings
	}
	byKey := make(map[string]int, len(siblings))
	for i := range siblings {
		byKey[siblings[i].ItemKey] = i
	}
	used := make(map[int]bool, len(siblings))
	out := make([]module.NavItemSpec, 0, len(siblings))
	for _, key := range orderedKeys {
		if idx, ok := byKey[key]; ok && !used[idx] {
			out = append(out, siblings[idx])
			used[idx] = true
		}
	}
	for i := range siblings {
		if !used[i] {
			out = append(out, siblings[i])
		}
	}
	return out
}

// applyOrderToRealms reorders the realm slice (public NavRealm) per
// orderedKeys. Same self-heal semantics: keys not present in the realm
// slice are skipped; realms missing from orderedKeys keep their relative
// position and append after.
func applyOrderToRealms(realms []models.NavRealm, orderedKeys []string) []models.NavRealm {
	if len(orderedKeys) == 0 || len(realms) == 0 {
		return realms
	}
	byKey := make(map[string]int, len(realms))
	for i := range realms {
		byKey[realms[i].Key] = i
	}
	used := make(map[int]bool, len(realms))
	out := make([]models.NavRealm, 0, len(realms))
	for _, key := range orderedKeys {
		if idx, ok := byKey[key]; ok && !used[idx] {
			out = append(out, realms[idx])
			used[idx] = true
		}
	}
	for i := range realms {
		if !used[i] {
			out = append(out, realms[i])
		}
	}
	return out
}

// applyOrderToAdminRealms is the AdminNavRealm twin of applyOrderToRealms.
func applyOrderToAdminRealms(realms []models.AdminNavRealm, orderedKeys []string) []models.AdminNavRealm {
	if len(orderedKeys) == 0 || len(realms) == 0 {
		return realms
	}
	byKey := make(map[string]int, len(realms))
	for i := range realms {
		byKey[realms[i].Key] = i
	}
	used := make(map[int]bool, len(realms))
	out := make([]models.AdminNavRealm, 0, len(realms))
	for _, key := range orderedKeys {
		if idx, ok := byKey[key]; ok && !used[idx] {
			out = append(out, realms[idx])
			used[idx] = true
		}
	}
	for i := range realms {
		if !used[i] {
			out = append(out, realms[i])
		}
	}
	return out
}
