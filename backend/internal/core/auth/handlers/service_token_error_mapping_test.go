package handlers

// Task 8: pure-function coverage for mapServiceTokenError, mirroring the
// style of error_mapping_test.go's TestMapPasswordError_KnownCodes /
// TestMapMFAError_KnownCodes tables. Task 10 added the LockedAfter(...)
// row and its own Retry-After test below: that is the arm Grant's own
// lockout branch actually returns (errors.Is-matches ErrAccountLocked).
// ErrClientRateLimited itself has no producer left in Grant — the row
// for it stays because the handler still recognizes the bare sentinel,
// a tolerance kept pending the shared limiter's removal.

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

func TestMapServiceTokenError(t *testing.T) {
	cases := []struct {
		in         error
		wantStatus int
	}{
		{services.ErrUnsupportedGrantType, http.StatusBadRequest},
		{services.ErrInvalidClientCredentials, http.StatusUnauthorized},
		{services.ErrClientRateLimited, http.StatusTooManyRequests},
		{services.LockedAfter(42 * time.Second), http.StatusTooManyRequests},
	}
	for _, c := range cases {
		err := mapServiceTokenError(c.in)
		var se interface{ GetStatus() int }
		if !errors.As(err, &se) || se.GetStatus() != c.wantStatus {
			t.Errorf("map(%v) status = %v, want %d", c.in, err, c.wantStatus)
		}
	}
}

// TestMapServiceTokenError_AccountLockedCarriesRetryAfter pins the arm
// Grant's own lockout branch actually produces on the wire: the live
// retry hint from LockedAfter(v.RetryAfter) must survive the map, not
// fall back to the generic 60s constant. RetryAfterFor extracts it via
// `stderrors.As(err, &le)` against the *concrete* `*lockedError` type —
// a sibling sentinel whose Is merely matched ErrClientRateLimited would
// be invisible to that extraction and would silently drop to the
// fallback, which is why mapServiceTokenError routes both sentinels
// through the same LockedAfter/RetryAfterFor pair rather than answering
// ErrClientRateLimited with a bare 429.
func TestMapServiceTokenError_AccountLockedCarriesRetryAfter(t *testing.T) {
	err := mapServiceTokenError(services.LockedAfter(42 * time.Second))

	var ce *errcode.Error
	if !errors.As(err, &ce) {
		t.Fatalf("want *errcode.Error, got %T", err)
	}
	ra := ce.GetHeaders().Get("Retry-After")
	if ra != "42" {
		t.Fatalf("Retry-After = %q, want %q", ra, "42")
	}
}
