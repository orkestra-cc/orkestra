package services

import (
	"context"

	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// ctxFor returns a context acting as an org administrator of an internal
// tenant, the way AuthMiddleware would leave it.
func ctxFor(tenant string) context.Context {
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant(tenant, []string{"org_owner"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), tenant), "internal")
}
