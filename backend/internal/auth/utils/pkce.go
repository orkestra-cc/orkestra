package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// GenerateCodeVerifier generates a cryptographically random code verifier
// for PKCE as per RFC 7636. Length must be between 43 and 128 characters.
func GenerateCodeVerifier() (string, error) {
	// Generate 32 random bytes (256 bits)
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Base64URL encode without padding
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// GenerateCodeChallenge generates a code challenge from a code verifier
// using SHA256 method as required by OAuth 2.1
func GenerateCodeChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// ValidateCodeVerifier validates a code verifier against a code challenge
// using SHA256 method
func ValidateCodeVerifier(codeVerifier, codeChallenge string) bool {
	if codeVerifier == "" || codeChallenge == "" {
		return false
	}

	expectedChallenge := GenerateCodeChallenge(codeVerifier)
	return expectedChallenge == codeChallenge
}

// GenerateNonce generates a cryptographically random nonce for OIDC
func GenerateNonce() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random nonce: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
