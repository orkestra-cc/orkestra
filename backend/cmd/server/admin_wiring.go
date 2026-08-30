package main

import (
	"context"
	"errors"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// requiredPersistedModules are the modules whose module_configs document is
// REQUIRED once boot seeding has run: a missing document is an outage that
// fails closed (503) and shows as a `missing` row on /admin/modules, never a
// reason to rebuild it from schema defaults. auth is here because its
// per-surface credential policy (password login on/off) is read strictly —
// a lazy re-seed from an admin page read would silently re-enable password
// sign-in with the schema default. Recovery: restore the document, or fix
// Mongo and restart so normal boot seeding runs.
var requiredPersistedModules = []string{"auth"}

// adminActorResolver reads the module-admin audit actor off the request
// context: the JWT-derived principal AuthMiddleware stamped, the trusted-
// proxy-resolved client IP, the User-Agent RequestMeta stored, and chi's
// request ID. No email — the UUID is the attribution.
func adminActorResolver(ctx context.Context) module.AdminActor {
	var a module.AdminActor
	a.UserID, _ = ctxauth.GetUserUUID(ctx)
	a.TenantID, _ = ctxauth.GetTenantID(ctx)
	a.TenantKind = ctxauth.TenantKindFromContext(ctx)
	a.IP, _ = ctxauth.GetClientIP(ctx)
	a.UserAgent = authMiddleware.UserAgentFromContext(ctx)
	a.RequestID = chiMiddleware.GetReqID(ctx)
	return a
}

// wireModuleAdminAudit installs the compliance audit sink and the actor
// resolver on the module admin handler. Both are nil-tolerated by the SDK
// for embedders; the in-tree server refuses to boot without them, because
// compliance is a core module and a silently unaudited config surface is a
// misconfiguration, not a degraded mode.
func wireModuleAdminAudit(h *module.ModuleAdminHandler, svcs *module.ServiceRegistry) error {
	sink, ok := module.GetTyped[iface.AuditSink](svcs, module.ServiceAuditSink)
	if !ok || sink == nil {
		return errors.New("module admin audit: compliance audit sink is not registered")
	}
	h.SetAuditSink(sink)
	h.SetActorResolver(adminActorResolver)
	return nil
}
