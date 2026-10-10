package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// ctxFor returns a context acting as an org administrator of an internal
// tenant, the way AuthMiddleware would leave it.
func ctxFor(tenant string) context.Context {
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant(tenant, []string{"org_owner"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), tenant), "internal")
}

// ctxauthExternal returns a context acting inside an external client org,
// which the llm gateway must refuse (Tier-1 only).
func ctxauthExternal(t *testing.T) context.Context {
	t.Helper()
	id := testkit.NewIdentity("u-ext", "ext@example.test", "guest").WithTenant("ext-1", []string{"org_member"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), "ext-1"), "external")
}
