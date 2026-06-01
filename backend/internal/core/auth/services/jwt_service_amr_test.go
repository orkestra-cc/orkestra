package services

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TestAMRClaimRoundTrip verifies that amr and last_otp_at survive the full
// sign → parse round trip. These are the two new claims Block A introduces;
// Blocks B/D read them out of validated tokens to enforce MFA and step-up.
func TestAMRClaimRoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", 15*time.Minute, 30*24*time.Hour)

	user := &iface.User{UUID: "u-1", Email: "alice@example.com", Role: "administrator"}

	token, err := svc.GenerateAccessTokenWithAMR(user, []string{"pwd", "otp"}, 1_700_000_000)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if len(claims.AMR) != 2 || claims.AMR[0] != "pwd" || claims.AMR[1] != "otp" {
		t.Fatalf("amr not preserved: %+v", claims.AMR)
	}
	if claims.LastOTPAt != 1_700_000_000 {
		t.Fatalf("last_otp_at not preserved: %d", claims.LastOTPAt)
	}
}

// TestAccessTokenTTLHonoursConstructor asserts that the access-token TTL
// passed into NewJWTService reaches the minted token's exp claim. Guards
// the previous bug where NewJWTService hardcoded 15 minutes and silently
// ignored JWT_ACCESS_TOKEN_EXPIRY.
func TestAccessTokenTTLHonoursConstructor(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	const want = 42 * time.Minute
	svc := NewJWTService(priv, &priv.PublicKey, "test", want, 30*24*time.Hour)

	user := &iface.User{UUID: "u-1", Email: "a@b.com", Role: "administrator"}
	token, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	got := time.Duration(claims.ExpiresAt-claims.IssuedAt) * time.Second
	if got != want {
		t.Fatalf("access token ttl: want %v, got %v", want, got)
	}
}

// TestAMROmittedWhenEmpty ensures we don't emit a stray amr claim for
// pre-Block-A call sites (dev tokens, legacy refresh paths).
func TestAMROmittedWhenEmpty(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", 15*time.Minute, 30*24*time.Hour)
	user := &iface.User{UUID: "u-1", Email: "a@b.com", Role: "user"}

	token, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(claims.AMR) != 0 {
		t.Fatalf("unexpected amr on default token: %+v", claims.AMR)
	}
	if claims.LastOTPAt != 0 {
		t.Fatalf("unexpected last_otp_at: %d", claims.LastOTPAt)
	}
}
