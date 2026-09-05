package middleware

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// The sidecar validator parses claims independently of the auth module's
// jwt_service, so drift between the two parsers is how a gate ends up
// reading a claim the minter writes under a different key. auth_time (D11)
// and mfae (D16) are the two newest, and both are read by middleware.
func TestJWTValidator_ParsesAuthTimeAndMFAEpoch(t *testing.T) {
	c := parseClaims(jwt.MapClaims{
		"sub":       "u-1",
		"auth_time": float64(1735689600),
		"mfae":      float64(3),
	})
	if c.AuthTime != 1735689600 {
		t.Errorf("AuthTime = %d, want 1735689600", c.AuthTime)
	}
	if c.MFAEpoch != 3 {
		t.Errorf("MFAEpoch = %d, want 3", c.MFAEpoch)
	}
}

// A token minted before this shipped carries neither claim. Both must read
// as zero: an absent mfae matches a user document with no mfaEpoch (so the
// deploy downgrades nobody) and an absent auth_time reads as stale.
//
// R17 — regression pin, not a TDD driver. Deleting the two parseClaims
// branches it nominally covers leaves it green: the zero is Go's zero value,
// not a statement here. Kept as a pin on the pre-deploy-token contract.
func TestJWTValidator_AbsentAuthTimeAndMFAEpochReadAsZero(t *testing.T) {
	c := parseClaims(jwt.MapClaims{"sub": "u-1"})
	if c.AuthTime != 0 || c.MFAEpoch != 0 {
		t.Fatalf("AuthTime=%d MFAEpoch=%d, want 0/0", c.AuthTime, c.MFAEpoch)
	}
}
