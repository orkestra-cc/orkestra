package services

// Phase 12: ParseUnverifiedClaims tests. The middleware audience gate
// reads claims via this method *before* signature verification so it
// can dispatch to the right tier handler — which means a bug here
// could let a forged token influence routing even though the token
// is rejected later. Worth pinning explicitly.

import (
	"testing"

	userModels "github.com/orkestra/backend/internal/core/user/models"
)

func TestParseUnverifiedClaims_ReturnsClaimsWithoutVerifyingSignature(t *testing.T) {
	priv := testRSAKey()
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceClient, 0, 0)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	svc.SetTenantProvider(gateTenantProvider{})

	user := &userModels.User{UUID: "u-1", Email: "alice@example.com", Role: "operator"}
	token, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	got, err := svc.ParseUnverifiedClaims(token)
	if err != nil {
		t.Fatalf("ParseUnverifiedClaims: %v", err)
	}
	if got.UserUUID != "u-1" {
		t.Errorf("UserUUID = %q, want u-1", got.UserUUID)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email = %q", got.Email)
	}
	if got.Audience != AudienceClient {
		t.Errorf("Audience = %q, want %q", got.Audience, AudienceClient)
	}
}

func TestParseUnverifiedClaims_MalformedTokenReturnsError(t *testing.T) {
	priv := testRSAKey()
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, 0, 0)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}

	if _, err := svc.ParseUnverifiedClaims("definitely-not-a-jwt"); err == nil {
		t.Errorf("malformed token must return error")
	}
}

func TestParseUnverifiedClaims_TamperedSignatureStillReadsClaims(t *testing.T) {
	// The whole point of ParseUnverifiedClaims: the middleware can read
	// `aud` to dispatch routing even when the signature is broken. The
	// downstream signature verifier (RequireAuth) will reject the
	// token, so this is not a security gap — it's a deliberate
	// performance optimisation.
	priv := testRSAKey()
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, 0, 0)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	svc.SetTenantProvider(gateTenantProvider{})

	user := &userModels.User{UUID: "u-2", Email: "bob@example.com", Role: "operator"}
	token, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	// Mangle the signature aggressively: replace the last 8 chars with
	// "AAAAAAAA" so the decoded signature bytes definitely change.
	tampered := token[:len(token)-8] + "AAAAAAAA"

	got, err := svc.ParseUnverifiedClaims(tampered)
	if err != nil {
		t.Fatalf("tampered signature must still parse the claim payload, got %v", err)
	}
	if got.UserUUID != "u-2" {
		t.Errorf("UserUUID = %q, want u-2", got.UserUUID)
	}
	// And the verifier rejects the same token, confirming the trust
	// boundary lives at signature verification, not at parsing.
	if _, err := svc.ValidateAccessToken(tampered); err == nil {
		t.Errorf("ValidateAccessToken must reject tampered signature")
	}
}
