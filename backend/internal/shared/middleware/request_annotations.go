package middleware

import (
	"context"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// annotatePrincipal copies the principal an auth middleware just stamped on
// ctx into the request annotations RequestLogger installed, so the outer
// http_request line and the log PolicyHandler see tenant and user. Call it
// on the final ctx, right before handing it to the next handler.
func annotatePrincipal(ctx context.Context) {
	a := ctxauth.RequestAnnotationsFrom(ctx)
	if a == nil {
		return
	}
	tenantID, _ := ctxauth.GetTenantID(ctx)
	userID, _ := ctxauth.GetUserUUID(ctx)
	role, _ := ctxauth.GetSystemRole(ctx)
	a.SetPrincipal(tenantID, ctxauth.TenantKindFromContext(ctx), userID, role)
}
