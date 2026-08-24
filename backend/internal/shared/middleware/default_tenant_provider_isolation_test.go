package middleware

// TestMiddlewareNeverResolvesThePlatformDefaultTenant pins a Task 6.2 spec
// invariant: the platform-wide default Tier-1 tenant pointer (tenant
// module PR 3; its resolver type lives in pkg/sdk/iface) may influence a
// request only indirectly — folded into TenantFallbackID at operator token
// ISSUANCE time, after membership validation. Middleware resolves the
// CURRENT tenant purely from already-issued claims (see
// resolveCurrentTenant's doc block in auth.go) and must never resolve the
// global pointer itself; doing so would let a request grant itself a
// tenant that was never checked against the caller's own memberships.
//
// This is a source-scan, not a mock-based unit test, because the property
// under test is "this package doesn't reference that resolver type at
// all" — no amount of behavioural testing proves an absence the way
// grepping the source does. The forbidden identifier is deliberately
// built from two half-strings (see `forbidden` below), and neither half
// nor the joined form appears anywhere else in this file's own source —
// so a plain `grep -rn` for the joined identifier over this whole
// directory (the sanity check this test exists to satisfy at review time
// too) finds nothing here, only a real violation elsewhere.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMiddlewareNeverResolvesThePlatformDefaultTenant(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve test file path")
	}
	dir := filepath.Dir(testFile)

	half1, half2 := "Default", "TenantProvider"
	forbidden := half1 + half2

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read middleware dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(content), forbidden) {
			t.Errorf("%s references the platform-default tenant resolver — middleware must resolve the current tenant only from already-issued claims, never the global pointer directly", path)
			found = true
		}
	}
	if found {
		t.Fatal("a forbidden reference was found under internal/shared/middleware — see resolveCurrentTenant's doc block in auth.go")
	}
}
