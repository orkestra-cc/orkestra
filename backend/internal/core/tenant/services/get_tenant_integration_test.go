package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// GetTenant must let a consumer tell "no such tenant" from an outage
// without importing the tenant module (compliance assigns policies by
// tenant UUID and answers 404 only for the first).
func TestGetTenant_MissingWrapsTheIfaceSentinel(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	svc := New(repository.New(db))
	_, err := svc.GetTenant(context.Background(), "missing-tenant")
	if !errors.Is(err, iface.ErrTenantNotFound) || !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetTenant(missing) = %v, want both iface.ErrTenantNotFound and repository.ErrNotFound", err)
	}
}
