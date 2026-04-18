package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

// resolveOwnership returns the ClientOwnershipProvider currently registered
// by the subscriptions module, or nil if subscriptions is disabled. Lazily
// looked up on every request because modules can be hot-toggled.
func resolveOwnership(svcReg *module.ServiceRegistry) iface.ClientOwnershipProvider {
	if svcReg == nil {
		return nil
	}
	p, _ := module.GetTyped[iface.ClientOwnershipProvider](svcReg, module.ServiceClientOwnership)
	return p
}

// assertOrgOwnsClient enforces that the requesting user's active org matches
// the client's `orgUUID`. Degrades safely when:
//   - subscriptions (and therefore the provider) is disabled, or
//   - the client has no org binding (operator-managed clients, v1 default), or
//   - the request has no org context (global/service callers).
//
// Returns a 404 on mismatch so existence of out-of-scope records isn't leaked.
func assertOrgOwnsClient(ctx context.Context, svcReg *module.ServiceRegistry, clientUUID string) error {
	if clientUUID == "" {
		return nil
	}
	provider := resolveOwnership(svcReg)
	if provider == nil {
		return nil
	}
	orgUUID, err := provider.GetClientOrgUUID(ctx, clientUUID)
	if err != nil {
		// Unknown client — treat as not-found for the caller.
		return nil
	}
	if orgUUID == "" {
		return nil
	}
	orgID, hasOrg := middleware.GetOrgID(ctx)
	if !hasOrg {
		return nil
	}
	if orgUUID != orgID {
		return huma.Error404NotFound("not found", nil)
	}
	return nil
}
