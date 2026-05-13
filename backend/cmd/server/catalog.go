package main

import (
	"log/slog"

	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/auth"
	"github.com/orkestra/backend/internal/core/authz"
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
func coreModules(cfg *config.Config) []func() module.Module {
	return []func() module.Module{
		func() module.Module { return user.NewModule() },
		func() module.Module { return notification.NewModule() },
		func() module.Module { return tenant.NewModule() },
		func() module.Module { return authz.NewModule() },
		func() module.Module { return auth.NewModule(cfg) },
		func() module.Module { return navigation.NewModule() },
	}
}

// optionalModules is the catalog of addons the binary can boot. It is
// populated at init time by the per-addon catalog_<name>.go files, each
// gated by `//go:build !no_addons || addon_<name>`:
//
//   - default build (no tags): every catalog_<name>.go compiles, every
//     addon is registered — same behavior as before the split.
//   - `-tags "no_addons"`: only core modules ship; the addon packages
//     are unreachable from main and never compiled.
//   - `-tags "no_addons,addon_billing,addon_documents"`: ship a curated
//     subset — useful for per-customer SKUs and lean container images.
//
// Enabled state at runtime is still controlled by the module_configs
// collection via /admin/modules; build tags only decide what is *installable*.
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
