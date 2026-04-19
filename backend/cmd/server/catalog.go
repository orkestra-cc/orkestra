package main

import (
	"log/slog"

	"github.com/orkestra/backend/internal/addons/agents"
	"github.com/orkestra/backend/internal/addons/aimodels"
	"github.com/orkestra/backend/internal/addons/billing"
	"github.com/orkestra/backend/internal/addons/company"
	"github.com/orkestra/backend/internal/addons/compliance"
	"github.com/orkestra/backend/internal/addons/dev"
	"github.com/orkestra/backend/internal/addons/documents"
	"github.com/orkestra/backend/internal/addons/graph"
	"github.com/orkestra/backend/internal/addons/identity"
	"github.com/orkestra/backend/internal/addons/onboarding"
	"github.com/orkestra/backend/internal/addons/payments"
	"github.com/orkestra/backend/internal/addons/rag"
	"github.com/orkestra/backend/internal/addons/sales"
	"github.com/orkestra/backend/internal/addons/subscriptions"
	"github.com/orkestra/backend/internal/core/auth"
	"github.com/orkestra/backend/internal/core/authz"
	"github.com/orkestra/backend/internal/core/navigation"
	"github.com/orkestra/backend/internal/core/notification"
	"github.com/orkestra/backend/internal/core/tenant"
	"github.com/orkestra/backend/internal/core/user"
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
	"billing":       func() module.Module { return billing.NewModule() },
	"documents":     func() module.Module { return documents.NewModule() },
	"company":       func() module.Module { return company.NewModule() },
	"graph":         func() module.Module { return graph.NewModule() },
	"aimodels":      func() module.Module { return aimodels.NewModule() },
	"rag":           func() module.Module { return rag.NewModule() },
	"agents":        func() module.Module { return agents.NewModule() },
	"sales":         func() module.Module { return sales.NewModule() },
	"subscriptions": func() module.Module { return subscriptions.NewModule() },
	"payments":      func() module.Module { return payments.NewModule() },
	"onboarding":    func() module.Module { return onboarding.NewModule() },
	"identity":      func() module.Module { return identity.NewModule() },
	"compliance":    func() module.Module { return compliance.NewModule() },
	"dev":           func() module.Module { return dev.NewModule() },
}

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
