package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/shared/middleware"
)

// assertOrgOwnsClient returns a 404 when the caller's active org does not
// match the client's `orgUUID`. Clients with an empty `orgUUID` are treated
// as operator-managed (not tenant-bound) and are allowed — this preserves
// the v1 single-tenant admin model while preventing cross-tenant access
// once client→org binding is in use. Returning 404 (not 403) avoids leaking
// existence of out-of-scope records.
func assertOrgOwnsClient(ctx context.Context, clientOrgUUID string) error {
	if clientOrgUUID == "" {
		return nil
	}
	orgID, hasOrg := middleware.GetOrgID(ctx)
	if !hasOrg {
		return nil
	}
	if clientOrgUUID != orgID {
		return huma.Error404NotFound("not found", nil)
	}
	return nil
}
