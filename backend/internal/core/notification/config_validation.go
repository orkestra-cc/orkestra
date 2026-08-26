package notification

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var (
	_ module.HasConfigValidator           = (*NotificationModule)(nil)
	_ module.HasConfigActivationValidator = (*NotificationModule)(nil)
)

// ValidateConfig rejects a broken sender routing map at the PATCH boundary
// (active-config and named-environment PATCH both funnel here). The rules
// are vacuous while no profile declares a pattern — see
// services.ValidateSenderConfig for the three states (ADR-0019 D5).
func (m *NotificationModule) ValidateConfig(_ context.Context, merged map[string]string) error {
	return services.ValidateSenderConfig(merged, m.driverRegistry())
}

// ValidateConfigActivation applies the same policy to the whole target
// profile before an environment switch, so sandbox → production cannot
// activate a map that is broken in the third state.
func (m *NotificationModule) ValidateConfigActivation(_ context.Context, target map[string]string) error {
	return services.ValidateSenderConfig(target, m.driverRegistry())
}

// driverRegistry returns the registry Init built, or a default one for a
// module that was never initialized (declaration tests, validation before
// Init). Both carry the same driver names and requirements; only the
// logger differs.
func (m *NotificationModule) driverRegistry() *services.DriverRegistry {
	if m.drivers == nil {
		m.drivers = services.NewDriverRegistry(services.CoreDrivers(slog.Default())...)
	}
	return m.drivers
}
