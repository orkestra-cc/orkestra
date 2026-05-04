package services

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestSignOAuthStateTokenRoundtrip locks in the ADR-0003 PR-D D-6
// happy path: a state JWT minted by SignOAuthStateToken parses back
// through ValidateOAuthStateToken with the same tier + csrf claims.
func TestSignOAuthStateTokenRoundtrip(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-bytes-aaaaaaaaaaaa")

	cases := []struct {
		name string
		tier string
	}{
		{"operator", AudienceOperator},
		{"client", AudienceClient},
		{"legacy", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			csrf, err := GenerateOAuthCSRF()
			if err != nil {
				t.Fatalf("GenerateOAuthCSRF: %v", err)
			}
			signed, err := SignOAuthStateToken(secret, tc.tier, csrf, 10*time.Minute)
			if err != nil {
				t.Fatalf("SignOAuthStateToken: %v", err)
			}
			if signed == "" {
				t.Fatal("signed token must not be empty")
			}
			// Three-segment JWT.
			if got := strings.Count(signed, "."); got != 2 {
				t.Fatalf("expected 3-segment JWT, got %d separators", got)
			}
			claims, err := ValidateOAuthStateToken(secret, signed)
			if err != nil {
				t.Fatalf("ValidateOAuthStateToken: %v", err)
			}
			if claims.Tier != tc.tier {
				t.Errorf("tier: got %q, want %q", claims.Tier, tc.tier)
			}
			if claims.CSRF != csrf {
				t.Errorf("csrf: got %q, want %q", claims.CSRF, csrf)
			}
		})
	}
}

// TestValidateOAuthStateTokenRejectsTamper proves that swapping the
// signing secret rejects an otherwise-valid token. This is the
// integrity guarantee the tier-dispatch pivot relies on — without it,
// an attacker could rewrite the tier claim and steer a flow to the
// wrong authService.
func TestValidateOAuthStateTokenRejectsTamper(t *testing.T) {
	t.Parallel()
	secretA := []byte("secret-a-32-bytes-aaaaaaaaaaaaaaa")
	secretB := []byte("secret-b-32-bytes-bbbbbbbbbbbbbbb")

	csrf, _ := GenerateOAuthCSRF()
	signed, err := SignOAuthStateToken(secretA, AudienceOperator, csrf, 10*time.Minute)
	if err != nil {
		t.Fatalf("SignOAuthStateToken: %v", err)
	}

	if _, err := ValidateOAuthStateToken(secretB, signed); !errors.Is(err, ErrInvalidStateToken) {
		t.Fatalf("expected ErrInvalidStateToken on tampered secret, got %v", err)
	}
}

// TestValidateOAuthStateTokenRejectsExpired ensures the 10-minute
// OAuth state window is enforced at the JWT layer too — a stolen
// state from a long-running browser tab cannot be replayed past its
// natural lifetime, even if the side-data row in Redis is still
// present (Redis TTL races aside).
func TestValidateOAuthStateTokenRejectsExpired(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-bytes-aaaaaaaaaaaa")

	csrf, _ := GenerateOAuthCSRF()
	// Tiny TTL that elapses before validation. Sub-second TTLs are
	// not a real-world flow but they exercise the `exp` check
	// without introducing a longer-running test.
	signed, err := SignOAuthStateToken(secret, AudienceClient, csrf, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("SignOAuthStateToken: %v", err)
	}
	time.Sleep(2 * time.Second)

	if _, err := ValidateOAuthStateToken(secret, signed); !errors.Is(err, ErrInvalidStateToken) {
		t.Fatalf("expected ErrInvalidStateToken on expired token, got %v", err)
	}
}

// TestValidateOAuthStateTokenRejectsEmpty rejects the empty-state
// case the callback hands in when the OAuth provider drops the
// state query parameter (e.g. a misconfigured Apple Service ID).
// The error must surface as ErrInvalidStateToken so the dev-only
// fallback in HandleAppleCallbackHTTP cannot accidentally accept
// a missing JWT as a successful validation.
func TestValidateOAuthStateTokenRejectsEmpty(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-bytes-aaaaaaaaaaaa")
	if _, err := ValidateOAuthStateToken(secret, ""); !errors.Is(err, ErrInvalidStateToken) {
		t.Fatalf("expected ErrInvalidStateToken on empty token, got %v", err)
	}
}

// TestDeriveOAuthStateSecretIsDeterministic ensures every replica of
// the monolith reaches the same secret from the same JWT private key.
// If this test ever flips, callbacks served by a different replica
// from the start endpoint will reject every state — a silent outage.
func TestDeriveOAuthStateSecretIsDeterministic(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	a, err := DeriveOAuthStateSecret(key)
	if err != nil {
		t.Fatalf("DeriveOAuthStateSecret: %v", err)
	}
	b, err := DeriveOAuthStateSecret(key)
	if err != nil {
		t.Fatalf("DeriveOAuthStateSecret: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("derivation must be deterministic across calls")
	}
	if len(a) != 32 {
		t.Fatalf("expected 32-byte secret (sha256), got %d", len(a))
	}
}

// TestDeriveOAuthStateSecretDistinctKeys: two different private keys
// produce two different secrets. Defense in depth — if a deployment
// rotates JWT keys the OAuth state secret rotates with them, in-flight
// flows fail closed instead of silently being honoured by the new key.
func TestDeriveOAuthStateSecretDistinctKeys(t *testing.T) {
	t.Parallel()
	keyA, _ := rsa.GenerateKey(rand.Reader, 2048)
	keyB, _ := rsa.GenerateKey(rand.Reader, 2048)
	a, _ := DeriveOAuthStateSecret(keyA)
	b, _ := DeriveOAuthStateSecret(keyB)
	if string(a) == string(b) {
		t.Fatal("distinct keys must produce distinct secrets")
	}
}
