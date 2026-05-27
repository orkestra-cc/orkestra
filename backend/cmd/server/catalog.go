package main

import (
	"log/slog"

	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/auth"
	"github.com/orkestra/backend/internal/core/authz"
	"github.com/orkestra/backend/internal/core/logging"
	"github.com/orkestra/backend/internal/core/navigation"
	"github.com/orkestra/backend/internal/core/notification"
	"github.com/orkestra/backend/internal/core/tenant"
	"github.com/orkestra/backend/internal/core/user"
	"github.com/orkestra/backend/internal/shared/config"
)

// coreModules returns the always-loaded module factories — user,
// notification, tenant, authz, auth, navigation — bound to the live
// application config. Order matters: each entry below depends on the
// previous ones.
//
//   - user: base identity (no deps)
//   - notification: email delivery (no hard deps)
//   - tenant: orgs + memberships (depends on user)
//   - authz: permissions + roles (depends on user + tenant)
//   - auth: JWT + OAuth + password login (depends on user, notification, tenant, authz) —
//     also the only core module that takes *config.Config at construction
//     time, retired from Dependencies.Config in Phase 1c
//   - navigation: menu aggregation (no deps; reads others' NavItems at runtime)
//   - logging: ADR-0005 Phase F admin surface for runtime log-level mutation
//     (no deps; its own service is read by main.go AFTER InitAll to hot-swap
//     the slog handler's resolver).
func coreModules(cfg *config.Config) []func() module.Module {
	return []func() module.Module{
		func() module.Module { return user.NewModule() },
		func() module.Module { return notification.NewModule() },
		func() module.Module { return tenant.NewModule() },
		func() module.Module { return authz.NewModule() },
		func() module.Module { return auth.NewModule(cfg) },
		func() module.Module { return navigation.NewModule() },
		func() module.Module { return logging.NewModule() },
	}
}

// optionalModules is the catalog of addons the binary can boot. It is
// populated at init time by the per-addon catalog_<name>.go files. Every
// addon compiles into every binary; runtime enable/disable is owned by
// the module_configs collection and surfaced at /admin/modules. To run a
// lean deployment, set ORKESTRA_PROFILE=minimal on first boot so the
// seeder leaves all addons disabled; ORKESTRA_PROFILE=full pre-enables
// every non-dev addon.
var optionalModules = map[string]func() module.Module{}

// allOptionalModuleNames returns the names of all optional modules.
// All optional modules are always instantiated and initialized at boot
// so they can be enabled/disabled at runtime without a restart.
func allOptionalModuleNames(logger *slog.Logger) []string {
	names := make([]string, 0, len(optionalModules))
	for name := range optionalModules {
		names = append(names, name)
	}
	logger.Info("All optional modules will be initialized (hot-reload enabled)",
		slog.Int("count", len(names)),
	)
	return names
}
