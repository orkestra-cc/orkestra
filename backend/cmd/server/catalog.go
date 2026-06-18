package main

import (
	"log/slog"

	"github.com/orkestra/backend/internal/core/auth"
	"github.com/orkestra/backend/internal/core/authz"
	"github.com/orkestra/backend/internal/core/compliance"
	"github.com/orkestra/backend/internal/core/logging"
	"github.com/orkestra/backend/internal/core/navigation"
	"github.com/orkestra/backend/internal/core/notification"
	"github.com/orkestra/backend/internal/core/tenant"
	"github.com/orkestra/backend/internal/core/user"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/module"
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
//   - compliance: ADR-0009 — audit sink + GDPR DSR pipeline + per-tenant KMS
//     crypto-shred + SOC2 evidence. Depends on user/auth/tenant; inits last so
//     their PII producers and concrete services are already registered when it
//     resolves the registry and pushes the audit sink in.
func coreModules(cfg *config.Config) []func() module.Module {
	return []func() module.Module{
		func() module.Module { return user.NewModule() },
		func() module.Module { return notification.NewModule() },
		func() module.Module { return tenant.NewModule() },
		func() module.Module { return authz.NewModule() },
		func() module.Module { return auth.NewModule(cfg) },
		func() module.Module { return navigation.NewModule() },
		func() module.Module { return logging.NewModule() },
		func() module.Module { return compliance.NewModule() },
	}
}

// optionalModules is the catalog of optional modules the binary can boot.
// ADR-0006 collapsed Orkestra to a core-only base, so the catalog ships
// empty — a fork registers its own optional modules here via per-module
// catalog_<name>.go files (one init() each), and runtime enable/disable is
// owned by the module_configs collection surfaced at /admin/modules.
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
