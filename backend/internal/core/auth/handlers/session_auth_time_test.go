package handlers

import (
	"context"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
)

// currentSessionSecurity is the single seam through which every re-mint
// that proves a factor against an EXISTING session gets its security
// context: the MFA step-up (/me/mfa/verify), the passkey step-up
// (/me/webauthn/verify/finish) and the password reconfirm
// (/me/password-confirm, which copies the struct it is handed).
//
// None of those is an authentication that CREATES a session, so all three
// must carry the caller's auth_time through unchanged. If this seam dropped
// it, a user who has just re-proved their password would present a token
// claiming the session began at the epoch.
func TestCurrentSessionSecurity_CarriesAuthTimeFromTheCallersToken(t *testing.T) {
	origin := time.Now().Add(-2 * time.Hour).Unix()
	ctx := context.WithValue(context.Background(), "claims", &authModels.JWTClaims{ //nolint:staticcheck // the handler seam reads this untyped key
		UserUUID:  "u-1",
		SessionID: "s-1",
		DeviceID:  "d-1",
		AuthTime:  origin,
	})

	_, security, ok := currentSessionSecurity(ctx)
	if !ok {
		t.Fatal("currentSessionSecurity refused a well-formed claims context")
	}
	if security.AuthTime != origin {
		t.Fatalf("AuthTime = %d, want the caller's original %d — a step-up is not a new session", security.AuthTime, origin)
	}
}
