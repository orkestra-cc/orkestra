package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/modulegate"
)

// cellFor selects the tenant-kind cell from a role's visibility entry.
func cellFor(v models.NavRoleVisibility, kind string) models.NavVisibilityCell {
	if kind == "external" {
		return v.External
	}
	return v.Internal
}

// Visibility reason codes. An empty reason means the item is visible. These
// values are part of the /v1/admin/navigation contract — the admin console
// maps them to localized "why is this hidden" copy. Keep them in sync with
// the frontend's adminNavigation.matrix.reason.* i18n keys.
const (
	VisibilityVisible     = ""
	ReasonModuleDisabled  = "module_disabled"
	ReasonConfigOff       = "config_off"
	ReasonTierMismatch    = "tier_mismatch"
	ReasonRoleBelowMin    = "role_below_min"
	ReasonParentCollapsed = "parent_collapsed"
)

// resolveGates reads the two role/tenant-independent gates for one item from
// the (possibly nil) enabled-checker: whether the owning module is enabled, and
// whether the item's RequiresConfig flag is currently truthy. A nil checker
// treats the module as enabled; a checker that does not expose GetValue treats
// the config gate as satisfied — both matching the public sidebar's fall-through
// behaviour. configSatisfied is always true for an ungated item.
func resolveGates(ctx context.Context, checker modulegate.ModuleEnabledChecker, spec module.NavItemSpec) (moduleEnabled, configSatisfied bool) {
	moduleEnabled = true
	if spec.ModuleName != "" && checker != nil {
		moduleEnabled = checker.IsEnabled(ctx, spec.ModuleName)
	}
	configSatisfied = true
	if spec.RequiresConfig != "" && spec.ModuleName != "" {
		if cr, ok := checker.(configValueReader); ok {
			configSatisfied = configTruthy(cr.GetValue(ctx, spec.ModuleName, spec.RequiresConfig))
		}
	}
	return moduleEnabled, configSatisfied
}

// evalSelfVisible answers "would (role, tenantKind) see this single item?",
// ignoring children. It is the single source of truth for the per-item gates
// the public sidebar applies in dynamicNavigationService.convert — both paths
// call this so the admin role-matrix can never drift from what users actually
// see. Parent-collapse (a no-path parent vanishing when all its children are
// hidden) is layered on top by the caller, since it depends on child results.
//
// The check order mirrors convert exactly: module-enabled, then the config
// gate, then tenant tier, then the role floor. The first failing gate wins the
// reason code. callers pass configSatisfied=true for items with no
// RequiresConfig gate.
func evalSelfVisible(moduleEnabled, configSatisfied bool, tier, role, minRole, kind string) (bool, string) {
	if !moduleEnabled {
		return false, ReasonModuleDisabled
	}
	if !configSatisfied {
		return false, ReasonConfigOff
	}
	if !tierAllows(tier, kind) {
		return false, ReasonTierMismatch
	}
	if !meetsMinRole(role, minRole) {
		return false, ReasonRoleBelowMin
	}
	return true, VisibilityVisible
}
