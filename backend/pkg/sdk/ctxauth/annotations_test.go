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
	a.SetErrorCode("x.y")
	a.MarkExpectedUnavailable()
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

// The error code and the expected-unavailability opt-in are recorded
// independently: a code never implies the opt-in.
func TestRequestAnnotations_ErrorCodeAndExpectedUnavailable(t *testing.T) {
	_, a := WithRequestAnnotations(context.Background())
	a.SetErrorCode("x.y_not_configured")
	if got := a.Snapshot(); got.ErrorCode != "x.y_not_configured" || got.ExpectedUnavailable {
		t.Fatalf("after SetErrorCode: %+v", got)
	}
	a.MarkExpectedUnavailable()
	a.SetErrorCode("x.later")
	if got := a.Snapshot(); got.ErrorCode != "x.y_not_configured" || !got.ExpectedUnavailable {
		t.Fatalf("after MarkExpectedUnavailable: %+v (first code must win)", got)
	}
}
