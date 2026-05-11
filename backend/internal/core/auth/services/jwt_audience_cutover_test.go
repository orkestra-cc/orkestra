package services

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// TestValidateRejectsTokenWithoutAudience locks in the ADR-0003 PR-D
// D-3 hard cutover: a JWT v1 token (no `aud` claim) presented to the
// validator must be rejected with ErrMissingAudience. Forging the token
// directly skips NewJWTService's stamping path.
func TestValidateRejectsTokenWithoutAudience(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", 15*time.Minute, 30*24*time.Hour).(*jwtService)

	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   "u-1",
		"email": "a@b.com",
		"srole": "administrator",
		"type":  "access",
		"iat":   now.Unix(),
		"exp":   now.Add(15 * time.Minute).Unix(),
		"iss":   svc.issuer,
		// deliberately no "aud" — this is the v1 shape
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := svc.ValidateAccessToken(signed); !errors.Is(err, ErrMissingAudience) {
		t.Fatalf("ValidateAccessToken: got %v, want ErrMissingAudience", err)
	}
}

// TestRefreshTokenCarriesAudience asserts that issued refresh tokens
// carry the same aud claim as access tokens. PR-D requires every
// monolith-issued token to be tier-pinned so the host-mux RequireAudience
// check can reject a refresh token presented on the wrong audience.
func TestRefreshTokenCarriesAudience(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", 15*time.Minute, 30*24*time.Hour)

	user := &userModels.User{UUID: "u-1", Email: "a@b.com", Role: "administrator"}
	refresh, err := svc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("issue refresh: %v", err)
	}

	claims, err := svc.ValidateRefreshToken(refresh)
	if err != nil {
		t.Fatalf("validate refresh: %v", err)
	}
	if claims.Audience == "" {
		t.Fatal("refresh token Audience claim is empty")
	}
	if claims.Audience != AudienceOperator {
		t.Fatalf("refresh aud = %q, want %q", claims.Audience, AudienceOperator)
	}
}

// TestNewJWTServiceWithAudienceStampsCustomValue covers the D-4/D-5
// per-tier issuer constructor: passing aud=client mints client tokens.
func TestNewJWTServiceWithAudienceStampsCustomValue(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceClient, 15*time.Minute, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	user := &userModels.User{UUID: "u-1", Email: "a@b.com", Role: "operator"}
	access, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("issue access: %v", err)
	}

	claims, err := svc.ValidateAccessToken(access)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.Audience != AudienceClient {
		t.Fatalf("access aud = %q, want %q", claims.Audience, AudienceClient)
	}

	if _, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", "", 15*time.Minute, 30*24*time.Hour); err == nil {
		t.Fatal("NewJWTServiceWithAudience(aud=\"\"): expected error, got nil")
	}
}
