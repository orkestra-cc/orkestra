package handlers

// Unit tests for the cookie-candidate picker the refresh endpoints use
// to avoid firing family revocation on stale parent-domain leftovers
// when the browser also carries the current cookie. The picker is the
// production fix for the PR-D D-9 cookie-domain-split regression.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
)

// peekTable maps a raw token value to the doc the fake should return,
// or to a sentinel error when err is non-nil.
type peekRow struct {
	doc *authModels.RefreshTokenDoc
	err error
}

func peekerFromTable(t map[string]peekRow) func(context.Context, string) (*authModels.RefreshTokenDoc, error) {
	return func(_ context.Context, raw string) (*authModels.RefreshTokenDoc, error) {
		row, ok := t[raw]
		if !ok {
			return nil, errors.New("unknown token")
		}
		return row.doc, row.err
	}
}

func freshDoc() *authModels.RefreshTokenDoc {
	return &authModels.RefreshTokenDoc{
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func rotatedDoc() *authModels.RefreshTokenDoc {
	return &authModels.RefreshTokenDoc{
		ExpiresAt:     time.Now().Add(time.Hour),
		IsRevoked:     true,
		RevokedReason: authModels.RevokeReasonRotated,
	}
}

func expiredDoc() *authModels.RefreshTokenDoc {
	return &authModels.RefreshTokenDoc{
		ExpiresAt: time.Now().Add(-time.Hour),
	}
}

func revokedForLogout() *authModels.RefreshTokenDoc {
	return &authModels.RefreshTokenDoc{
		ExpiresAt:     time.Now().Add(time.Hour),
		IsRevoked:     true,
		RevokedReason: authModels.RevokeReasonLogout,
	}
}

func TestPickRefreshCandidate_PrefersValidOverStaleRotated(t *testing.T) {
	// PR-D D-9 production failure mode: browser carries a stale
	// parent-domain rotated cookie AND the current valid cookie.
	// Picker must select the valid one and ignore the rotated sibling.
	peek := peekerFromTable(map[string]peekRow{
		"stale":   {doc: rotatedDoc()},
		"current": {doc: freshDoc()},
	})

	for _, order := range [][]string{
		{"stale", "current"},
		{"current", "stale"},
	} {
		chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, order)
		if chosen != "current" {
			t.Errorf("order %v: chosen = %q, want %q", order, chosen, "current")
		}
		if fallback != "" {
			t.Errorf("order %v: fallback = %q, want empty when a valid sibling exists", order, fallback)
		}
		if lookupErr != nil {
			t.Errorf("order %v: lookupErr = %v, want nil for readable candidates", order, lookupErr)
		}
	}
}

func TestPickRefreshCandidate_OnlyRotated_FallsBackForReplay(t *testing.T) {
	// Only candidate the browser holds is rotated → genuine replay
	// signal. Picker returns it as the fallback so the caller fires
	// family revocation.
	peek := peekerFromTable(map[string]peekRow{
		"only-rotated": {doc: rotatedDoc()},
	})
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, []string{"only-rotated"})
	if chosen != "" {
		t.Errorf("chosen = %q, want empty (no valid candidate)", chosen)
	}
	if fallback != "only-rotated" {
		t.Errorf("fallback = %q, want %q (replay signal)", fallback, "only-rotated")
	}
	if lookupErr != nil {
		t.Errorf("lookupErr = %v, want nil — the row was readable, it is simply rotated", lookupErr)
	}
}

func TestPickRefreshCandidate_SkipsExpiredAndForeignRevocations(t *testing.T) {
	// Expired rows and revoked-not-rotated rows must be ignored
	// entirely — they're neither valid candidates nor replay signals.
	peek := peekerFromTable(map[string]peekRow{
		"expired":     {doc: expiredDoc()},
		"logged-out":  {doc: revokedForLogout()},
		"current":     {doc: freshDoc()},
		"unknown-jwt": {err: errors.New("invalid")},
	})
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek,
		[]string{"expired", "logged-out", "unknown-jwt", "current"})
	if chosen != "current" {
		t.Errorf("chosen = %q, want %q", chosen, "current")
	}
	if fallback != "" {
		t.Errorf("fallback = %q, want empty", fallback)
	}
	if lookupErr != nil {
		t.Errorf("lookupErr = %v, want nil — none of these errors is the outage sentinel", lookupErr)
	}
}

func TestPickRefreshCandidate_AllInvalid_BothEmpty(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"expired":    {doc: expiredDoc()},
		"logged-out": {doc: revokedForLogout()},
		"unknown":    {err: errors.New("invalid")},
	})
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek,
		[]string{"expired", "logged-out", "unknown"})
	if chosen != "" || fallback != "" {
		t.Errorf("chosen=%q fallback=%q, want both empty for all-invalid input", chosen, fallback)
	}
	if lookupErr != nil {
		t.Errorf("lookupErr = %v, want nil — all-invalid is a verdict, not an outage", lookupErr)
	}
}

func TestPickRefreshCandidate_NoCandidates(t *testing.T) {
	peek := peekerFromTable(nil)
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, nil)
	if chosen != "" || fallback != "" {
		t.Errorf("empty input: chosen=%q fallback=%q, want both empty", chosen, fallback)
	}
	if lookupErr != nil {
		t.Errorf("empty input: lookupErr = %v, want nil", lookupErr)
	}
}

var errPeekOutage = fmt.Errorf("mongo down: %w", services.ErrRefreshLookupUnavailable)

// An outage on the ONLY candidate is not "no candidate": it is "could not
// look", and the handler must say so instead of inventing a 401.
func TestPickRefreshCandidate_LookupOutage_OnlyCandidate_ReportsError(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{"only": {err: errPeekOutage}})
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, []string{"only"})
	if chosen != "" || fallback != "" {
		t.Errorf("chosen=%q fallback=%q, want both empty", chosen, fallback)
	}
	if !errors.Is(lookupErr, services.ErrRefreshLookupUnavailable) {
		t.Fatalf("lookupErr = %v, want ErrRefreshLookupUnavailable", lookupErr)
	}
}

// A valid sibling is proof enough on its own, in either order: the rotation it
// leads to will 503 by itself if the store is really down.
func TestPickRefreshCandidate_LookupOutage_ValidSiblingStillWins(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"broken":  {err: errPeekOutage},
		"current": {doc: freshDoc()},
	})
	for _, order := range [][]string{{"broken", "current"}, {"current", "broken"}} {
		chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, order)
		if chosen != "current" || fallback != "" || lookupErr != nil {
			t.Errorf("order %v: chosen=%q fallback=%q err=%v, want current/\"\"/nil", order, chosen, fallback, lookupErr)
		}
	}
}

// THE case that keeps incomplete classification from revoking a family: the
// candidate we could not read may have been the valid successor. No fallback.
func TestPickRefreshCandidate_LookupOutage_SuppressesRotatedFallback(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"broken":  {err: errPeekOutage},
		"rotated": {doc: rotatedDoc()},
	})
	for _, order := range [][]string{{"broken", "rotated"}, {"rotated", "broken"}} {
		chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, order)
		if chosen != "" {
			t.Errorf("order %v: chosen=%q, want empty", order, chosen)
		}
		if fallback != "" {
			t.Errorf("order %v: fallback=%q — replay detection would fire on a family whose successor we could not read", order, fallback)
		}
		if !errors.Is(lookupErr, services.ErrRefreshLookupUnavailable) {
			t.Errorf("order %v: lookupErr=%v, want the sentinel", order, lookupErr)
		}
	}
}

// The existing meaning survives: a NON-sentinel error is an invalid candidate,
// skipped silently, and produces no lookupErr.
func TestPickRefreshCandidate_NonSentinelError_StillSkippedSilently(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"bad-jwt": {err: errors.New("invalid refresh token: signature")},
		"current": {doc: freshDoc()},
	})
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, []string{"bad-jwt", "current"})
	if chosen != "current" || fallback != "" || lookupErr != nil {
		t.Errorf("chosen=%q fallback=%q err=%v, want current/\"\"/nil", chosen, fallback, lookupErr)
	}
}
