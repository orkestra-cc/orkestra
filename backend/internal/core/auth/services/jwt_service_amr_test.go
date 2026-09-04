package services

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
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

// --- auth_time (D11) and mfae (D16) ---------------------------------------
//
// auth_time is the OIDC name for "when the interactive authentication that
// created this session happened". It is what lets a user with NO factor
// prove freshness for a first enrolment; a refresh must carry it unchanged,
// because a refresh is not an authentication.
//
// mfae is the MFA epoch the token was minted under. A request whose token
// epoch is behind the user's has its MFA markers ignored, so authority
// proven by a removed factor dies at once in every session.

// The honest round trip is sign → parse: claimsToMap writes Go integers but
// a parsed token delivers JSON numbers (float64), so calling mapToClaims on
// claimsToMap's own output would exercise a shape production never sees.
func TestClaims_AuthTimeAndMFAEpochSurviveTheMint(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", 15*time.Minute, 30*24*time.Hour)

	authTime := time.Now().Add(-90 * time.Second).Unix()
	user := &iface.User{UUID: "u-1", Email: "alice@example.com", Role: "administrator", MFAEpoch: 7}

	token, err := svc.GenerateEnhancedAccessToken(user,
		&authModels.DeviceInfo{DeviceID: "d-1"},
		&authModels.SecurityContext{SessionID: "s-1", AuthTime: authTime})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.AuthTime != authTime {
		t.Errorf("AuthTime = %d, want %d (the session's interactive authentication)", claims.AuthTime, authTime)
	}
	if claims.MFAEpoch != 7 {
		t.Errorf("MFAEpoch = %d, want 7 (the user's epoch at mint time)", claims.MFAEpoch)
	}
}

// Zero must be OMITTED, not written as 0 — a pre-deploy token has no
// auth_time and no mfae, and a freshly minted one carrying literal zeroes
// would be indistinguishable from it in a log.
func TestClaimsToMap_OmitsAuthTimeAndMFAEpochWhenZero(t *testing.T) {
	s := &jwtService{}
	m := s.claimsToMap(&authModels.JWTClaims{UserUUID: "u-1"})
	if _, present := m["auth_time"]; present {
		t.Error("auth_time must be omitted when zero")
	}
	if _, present := m["mfae"]; present {
		t.Error("mfae must be omitted when zero")
	}
}

// A parsed token hands the claim map JSON numbers, so the reader must take
// float64 — the shape getFloatClaim already asserts for exp/iat.
func TestMapToClaims_ReadsAuthTimeAndMFAEpochAsJSONNumbers(t *testing.T) {
	s := &jwtService{}
	out := s.mapToClaims(jwt.MapClaims{
		"sub":       "u-1",
		"auth_time": float64(1735689600),
		"mfae":      float64(3),
	})
	if out.AuthTime != 1735689600 {
		t.Errorf("AuthTime = %d, want 1735689600", out.AuthTime)
	}
	if out.MFAEpoch != 3 {
		t.Errorf("MFAEpoch = %d, want 3", out.MFAEpoch)
	}
}

// An absent claim reads as 0, which is what matches every pre-deploy token
// against a user document that has no mfaEpoch either: the deploy downgrades
// nobody, and the missing auth_time costs one re-login.
func TestMapToClaims_AbsentAuthTimeAndMFAEpochReadAsZero(t *testing.T) {
	s := &jwtService{}
	out := s.mapToClaims(jwt.MapClaims{"sub": "u-1"})
	if out.AuthTime != 0 || out.MFAEpoch != 0 {
		t.Fatalf("AuthTime=%d MFAEpoch=%d, want 0/0", out.AuthTime, out.MFAEpoch)
	}
}

// mfae is stamped centrally, so it reaches even the mints that must NOT
// carry auth_time (service accounts, dev tokens). That is deliberate: an
// epoch is a fact about the subject, a freshness proof is not.
func TestMFAEpoch_StampedByTheCentralAccessTokenMint(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", 15*time.Minute, 30*24*time.Hour)

	token, err := svc.GenerateAccessToken(&iface.User{UUID: "u-2", Email: "b@c.com", Role: "operator", MFAEpoch: 4})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.MFAEpoch != 4 {
		t.Fatalf("MFAEpoch = %d, want 4", claims.MFAEpoch)
	}
}
