package utils

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

// RFC 7636: 43–128 characters from the unreserved set.
func TestGenerateCodeVerifier_LengthAndCharset(t *testing.T) {
	const allowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	v, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Fatalf("length = %d, want 43..128", len(v))
	}
	for _, c := range v {
		if !strings.ContainsRune(allowed, c) {
			t.Fatalf("character %q is outside the unreserved set", c)
		}
	}
}

func TestGenerateCodeVerifier_IsRandom(t *testing.T) {
	a, _ := GenerateCodeVerifier()
	b, _ := GenerateCodeVerifier()
	if a == b {
		t.Fatal("two verifiers must differ")
	}
}

func TestGenerateCodeChallenge_IsBase64URLUnpaddedSHA256(t *testing.T) {
	const v = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(v))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := GenerateCodeChallenge(v); got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
	if strings.Contains(GenerateCodeChallenge(v), "=") {
		t.Fatal("the challenge must be unpadded")
	}
}
