package tenant

import (
	"context"
	"fmt"
	"strings"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var (
	_ module.HasConfigValidator           = (*Module)(nil)
	_ module.HasConfigActivationValidator = (*Module)(nil)
)

// ValidateConfig rejects invalid Tier-1 provisioning policy at the PATCH
// boundary (active config and named-environment PATCH both funnel here via
// ModuleConfigService).
func (m *Module) ValidateConfig(ctx context.Context, mergedValues map[string]string) error {
	return m.validateProvisioningPolicy(ctx, mergedValues)
}

// ValidateConfigActivation applies the same policy to the complete target
// profile before an environment switch — a stored legacy `single` profile
// with too many occupied provisioning slots cannot be activated.
func (m *Module) ValidateConfigActivation(ctx context.Context, targetValues map[string]string) error {
	return m.validateProvisioningPolicy(ctx, targetValues)
}

// validateProvisioningPolicy is the one policy function shared by both
// hooks above (Task 3.3): the active-config PATCH, the named-environment
// PATCH, and the active-environment switch must all agree, or a stored
// legacy `single` profile that is no longer satisfiable could be smuggled
// in by switching profiles instead of PATCHing the active one.
//
//   - manual or absent/empty — accepted. Absent and legacy values (e.g. a
//     stored `open`) normalise to manual at runtime (Task 3.2), so there is
//     nothing to gate here.
//   - single — accepted only when at most one Tier-1 tenant currently
//     occupies a provisioning slot.
//   - anything else, including `open` — rejected: `open` was removed from
//     Tier-1 and is Tier-2-only.
func (m *Module) validateProvisioningPolicy(ctx context.Context, values map[string]string) error {
	mode := strings.TrimSpace(values["provisioning.internal.mode"])
	switch mode {
	case "", models.ProvisioningModeManual:
		return nil
	case models.ProvisioningModeSingle:
		n, err := m.slotCount(ctx, models.TenantKindInternal)
		if err != nil {
			// Infrastructure failure, not a validation failure: a count
			// that cannot be performed must never be reported to the
			// operator as if their input were bad.
			return fmt.Errorf("tenant: count provisioning slots for single-mode validation: %w", err)
		}
		if n > 1 {
			return &module.ConfigValidationError{
				Field:   "provisioning.internal.mode",
				Message: "cannot select single: more than one internal tenant currently occupies a provisioning slot",
				Code:    errcode.TenantSingleModeConflict,
			}
		}
		return nil
	default:
		return &module.ConfigValidationError{
			Field:   "provisioning.internal.mode",
			Message: "must be manual or single",
			Code:    errcode.TenantInternalModeInvalid,
		}
	}
}
