package ctxauth

import (
	"context"
	"sync"
	"testing"
)

func TestRequestAnnotations_RoundTrip(t *testing.T) {
	ctx, a := WithRequestAnnotations(context.Background())
	if RequestAnnotationsFrom(ctx) != a {
		t.Fatal("RequestAnnotationsFrom did not return the installed holder")
	}
	a.SetPrincipal("t-1", "external", "u-1", "administrator")
	a.SetAudience("operator")
	want := AnnotationSnapshot{TenantID: "t-1", TenantKind: "external", UserID: "u-1", UserRole: "administrator", Audience: "operator"}
	if got := a.Snapshot(); got != want {
		t.Fatalf("Snapshot = %+v, want %+v", got, want)
	}
	// An empty value never erases one already recorded.
	a.SetPrincipal("", "", "", "")
	if got := a.Snapshot(); got != want {
		t.Fatalf("empty SetPrincipal erased values: %+v", got)
	}
}

func TestRequestAnnotations_NilSafe(t *testing.T) {
	var a *RequestAnnotations
	a.SetPrincipal("t", "k", "u", "r")
	a.SetAudience("x")
	if got := a.Snapshot(); got != (AnnotationSnapshot{}) {
		t.Fatalf("nil Snapshot = %+v", got)
	}
	if RequestAnnotationsFrom(context.Background()) != nil {
		t.Fatal("RequestAnnotationsFrom on a bare context must be nil")
	}
}

func TestRequestAnnotations_ConcurrentAccess(t *testing.T) {
	_, a := WithRequestAnnotations(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); a.SetPrincipal("t", "k", "u", "r") }()
		go func() { defer wg.Done(); _ = a.Snapshot() }()
	}
	wg.Wait()
}
