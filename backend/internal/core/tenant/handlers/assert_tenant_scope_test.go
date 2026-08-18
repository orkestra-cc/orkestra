package handlers

import (
	"context"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// TestAssertTenantScope covers the guard that closes the cross-tenant IDOR on
// the per-tenant member surface: a permission is checked against the resolved
// tenant, but the handler body acts on the {tenantId} path segment, so the two
// must match. A mismatch (or a missing/empty resolved tenant) is a 404, never a
// silent pass or a 403 existence oracle.
func TestAssertTenantScope(t *testing.T) {
	tests := []struct {
		name       string
		setTenant  bool
		ctxTenant  string
		pathTenant string
		wantErr    bool
	}{
		{"match passes", true, "tenant-A", "tenant-A", false},
		{"cross-tenant path is 404", true, "tenant-A", "tenant-B", true},
		{"no resolved tenant is 404", false, "", "tenant-A", true},
		{"empty resolved tenant is 404", true, "", "tenant-A", true},
		{"empty path is 404", true, "tenant-A", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.setTenant {
				ctx = context.WithValue(ctx, ctxauth.KeyTenantID, tc.ctxTenant)
			}
			err := assertTenantScope(ctx, tc.pathTenant)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != 404 {
				t.Errorf("expected a 404 huma.StatusError, got %v", err)
			}
		})
	}
}
