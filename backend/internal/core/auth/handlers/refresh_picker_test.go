package handlers

// Unit tests for the cookie-candidate picker the refresh endpoints use
// to avoid firing family revocation on stale parent-domain leftovers
// when the browser also carries the current cookie. The picker is the
// production fix for the PR-D D-9 cookie-domain-split regression.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
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
		chosen, fallback := pickRefreshCandidate(context.Background(), peek, order)
		if chosen != "current" {
			t.Errorf("order %v: chosen = %q, want %q", order, chosen, "current")
		}
		if fallback != "" {
			t.Errorf("order %v: fallback = %q, want empty when a valid sibling exists", order, fallback)
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
	chosen, fallback := pickRefreshCandidate(context.Background(), peek, []string{"only-rotated"})
	if chosen != "" {
		t.Errorf("chosen = %q, want empty (no valid candidate)", chosen)
	}
	if fallback != "only-rotated" {
		t.Errorf("fallback = %q, want %q (replay signal)", fallback, "only-rotated")
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
	chosen, fallback := pickRefreshCandidate(context.Background(), peek,
		[]string{"expired", "logged-out", "unknown-jwt", "current"})
	if chosen != "current" {
		t.Errorf("chosen = %q, want %q", chosen, "current")
	}
	if fallback != "" {
		t.Errorf("fallback = %q, want empty", fallback)
	}
}

func TestPickRefreshCandidate_AllInvalid_BothEmpty(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"expired":    {doc: expiredDoc()},
		"logged-out": {doc: revokedForLogout()},
		"unknown":    {err: errors.New("invalid")},
	})
	chosen, fallback := pickRefreshCandidate(context.Background(), peek,
		[]string{"expired", "logged-out", "unknown"})
	if chosen != "" || fallback != "" {
		t.Errorf("chosen=%q fallback=%q, want both empty for all-invalid input", chosen, fallback)
	}
}

func TestPickRefreshCandidate_NoCandidates(t *testing.T) {
	peek := peekerFromTable(nil)
	chosen, fallback := pickRefreshCandidate(context.Background(), peek, nil)
	if chosen != "" || fallback != "" {
		t.Errorf("empty input: chosen=%q fallback=%q, want both empty", chosen, fallback)
	}
}
