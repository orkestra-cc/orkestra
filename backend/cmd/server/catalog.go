package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/addons/agents"
	"github.com/orkestra/backend/internal/addons/aimodels"
	"github.com/orkestra/backend/internal/addons/billing"
	"github.com/orkestra/backend/internal/addons/company"
	"github.com/orkestra/backend/internal/addons/dev"
	"github.com/orkestra/backend/internal/addons/documents"
	"github.com/orkestra/backend/internal/addons/graph"
	"github.com/orkestra/backend/internal/addons/rag"
	"github.com/orkestra/backend/internal/addons/sales"
	"github.com/orkestra/backend/internal/core/auth"
	"github.com/orkestra/backend/internal/core/authz"
	"github.com/orkestra/backend/internal/core/navigation"
	"github.com/orkestra/backend/internal/core/notification"
	"github.com/orkestra/backend/internal/core/tenant"
	"github.com/orkestra/backend/internal/core/user"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/module"
)

// coreModules are always loaded — they provide the foundation
// (users, notifications, tenancy, authorization, auth, navigation).
// Order matters: each entry below depends on the previous ones.
//  - user: base identity (no deps)
//  - notification: email delivery (no hard deps)
//  - tenant: orgs + memberships (depends on user)
//  - authz: permissions + roles (depends on user + tenant)
//  - auth: JWT + OAuth + password login (depends on user, notification, tenant, authz)
//  - navigation: menu aggregation (no deps; reads others' NavItems at runtime)
var coreModules = []func() module.Module{
	func() module.Module { return user.NewModule() },
	func() module.Module { return notification.NewModule() },
	func() module.Module { return tenant.NewModule() },
	func() module.Module { return authz.NewModule() },
	func() module.Module { return auth.NewModule() },
	func() module.Module { return navigation.NewModule() },
}

// optionalModules can be loaded by the user via the MODULES env var
// or per-module env vars (BILLING_ENABLED, RAG_ENABLED, etc.).
// The registry resolves initialization order from Dependencies().
var optionalModules = map[string]func() module.Module{
	"billing":   func() module.Module { return billing.NewModule() },
	"documents": func() module.Module { return documents.NewModule() },
	"company":   func() module.Module { return company.NewModule() },
	"graph":     func() module.Module { return graph.NewModule() },
	"aimodels":  func() module.Module { return aimodels.NewModule() },
	"rag":       func() module.Module { return rag.NewModule() },
	"agents":    func() module.Module { return agents.NewModule() },
	"sales":     func() module.Module { return sales.NewModule() },
	"dev":       func() module.Module { return dev.NewModule() },
}

// selectOptionalModules determines which optional modules to load.
//
// Two modes:
//  1. Explicit: MODULES=billing,documents,sales → load exactly these
//  2. Auto (default): no MODULES set → instantiate each module and check
//     its Enabled(cfg) method (backward compatible with per-module env vars)
//
// In both modes, dependencies declared by selected modules are auto-included.
// Example: MODULES=billing auto-includes "documents" because billing depends on it.
func selectOptionalModules(cfg *config.Config, logger *slog.Logger, configRepo *module.ModuleConfigRepository) map[string]bool {
	selected := make(map[string]bool)

	if len(cfg.Server.Modules) > 0 {
		// Explicit mode: user listed which modules to load
		for _, name := range cfg.Server.Modules {
			if _, ok := optionalModules[name]; ok {
				selected[name] = true
			}
		}
		logger.Info("Modules loaded from MODULES config",
			slog.Any("modules", cfg.Server.Modules),
		)
	} else {
		// Auto mode: check each module's Enabled() method
		for name, factory := range optionalModules {
			m := factory()
			if m.Enabled(cfg) {
				selected[name] = true
			}
		}

		// Also load modules that an admin enabled via the UI (stored in
		// module_configs). This makes the admin toggle take effect after
		// a backend restart without requiring env var changes.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dbEnabled, err := configRepo.FindEnabledAddonNames(ctx)
		if err != nil {
			logger.Warn("Failed to query DB-enabled modules, using env vars only",
				slog.String("error", err.Error()),
			)
		} else {
			for _, name := range dbEnabled {
				if _, isOptional := optionalModules[name]; isOptional && !selected[name] {
					selected[name] = true
					logger.Info("Module enabled from admin config",
						slog.String("module", name),
					)
				}
			}
		}
	}

	// Auto-include dependencies of selected modules.
	// Iterate until stable (handles transitive deps like rag → graph).
	changed := true
	for changed {
		changed = false
		for name := range selected {
			factory := optionalModules[name]
			m := factory()
			for _, dep := range m.Dependencies() {
				// Skip core module deps (auth, user) — they're always loaded
				if _, isOptional := optionalModules[dep]; isOptional && !selected[dep] {
					selected[dep] = true
					changed = true
					logger.Info("Auto-included dependency",
						slog.String("module", dep),
						slog.String("required_by", name),
					)
				}
			}
		}
	}

	return selected
}
