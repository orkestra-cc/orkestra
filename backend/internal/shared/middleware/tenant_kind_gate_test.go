package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// runKindGate wires a minimal AuthMiddleware, seeds the request context with
// the given tenant kind, and invokes m.RequireTenantKind(expected). Returns
// (downstreamRan, httpStatus) so table tests stay terse.
func runKindGate(t *testing.T, expected string, ctxKind string) (bool, int) {
	t.Helper()
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	if ctxKind != "" {
		ctx := context.WithValue(req.Context(), ctxauth.KeyTenantKind, ctxKind)
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()

	called := false
	handler := m.RequireTenantKind(expected)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	handler.ServeHTTP(rec, req)
	return called, rec.Code
}

func TestRequireTenantKind_EnforceMode(t *testing.T) {
	t.Setenv("TENANT_KIND_ENFORCEMENT", "enforce")

	cases := []struct {
		name       string
		expected   string
		actual     string
		wantCalled bool
		wantStatus int
	}{
		{name: "matching kind passes", expected: iface.TenantKindInternal, actual: iface.TenantKindInternal, wantCalled: true, wantStatus: http.StatusOK},
		{name: "mismatched kind blocks", expected: iface.TenantKindInternal, actual: iface.TenantKindExternal, wantCalled: false, wantStatus: http.StatusForbidden},
		{name: "missing tenant blocks", expected: iface.TenantKindInternal, actual: "", wantCalled: false, wantStatus: http.StatusForbidden},
		{name: "external expected matches", expected: iface.TenantKindExternal, actual: iface.TenantKindExternal, wantCalled: true, wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called, status := runKindGate(t, tc.expected, tc.actual)
			if called != tc.wantCalled {
				t.Errorf("downstream called = %v, want %v", called, tc.wantCalled)
			}
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
		})
	}
}

func TestRequireTenantKind_WarnMode(t *testing.T) {
	t.Setenv("TENANT_KIND_ENFORCEMENT", "warn")

	// Mismatched kind in warn mode passes through.
	called, status := runKindGate(t, iface.TenantKindInternal, iface.TenantKindExternal)
	if !called {
		t.Fatalf("warn mode must pass mismatched requests through; downstream was not called (status %d)", status)
	}
	if status != http.StatusOK {
		t.Fatalf("warn mode must return 200 on mismatch; got %d", status)
	}

	// Matching kind in warn mode still passes.
	called, status = runKindGate(t, iface.TenantKindInternal, iface.TenantKindInternal)
	if !called || status != http.StatusOK {
		t.Fatalf("warn mode must pass matching requests; called=%v status=%d", called, status)
	}

	// Missing tenant still blocks — warn mode only covers mismatch.
	called, status = runKindGate(t, iface.TenantKindInternal, "")
	if called {
		t.Fatalf("warn mode must still block missing-tenant requests")
	}
	if status != http.StatusForbidden {
		t.Fatalf("missing tenant: want 403, got %d", status)
	}
}
