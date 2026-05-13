// Package cryptoutil holds the small helper functions the identity
// addon needs that the in-tree codebase exposes from
// `backend/internal/shared/utils`. The helpers were inlined here as
// part of Phase 5k of the SDK split — once identity sits in its own
// Go module it can no longer reach across the backend's `internal/`
// boundary.
//
// Three buckets of helpers:
//
//   - OAuth-token encryption (`Encrypt` / `Decrypt`). AES-256-GCM
//     with a hex-encoded 32-byte key from `OAUTH_TOKEN_ENCRYPTION_KEY`.
//     Algorithm is byte-for-byte identical to the in-tree
//     `utils.{EncryptOAuthToken,DecryptOAuthToken}` and to the SDK's
//     private `module/secrets.go` — ciphertext written by any of the
//     three implementations is interchangeable.
//   - Random string generation (`SecureRandomString`, `GenerateState`,
//     `GenerateNonce`). Plain `crypto/rand` over base64url. State =
//     32 bytes; nonce = 24 bytes (matches the in-tree defaults).
//   - HTTP client-IP extraction (`GetClientIP`). Walks the same
//     header chain as the in-tree helper:
//     X-Forwarded-For → X-Real-IP → CF-Connecting-IP → X-Client-IP →
//     RemoteAddr.
//
// All three buckets are small enough to vendor; no need for a
// separate Go module. The package lives under `internal/` so other
// addons that might extract later cannot reach across boundaries
// the same way — each adds its own copy or its own iface contract.
package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// Sentinel errors. Mirror the shape of internal/shared/utils so
// downstream error checks behave identically.
var (
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
	ErrKeyNotSet         = errors.New("encryption key not set")
)

// getEncryptionKey reads the 32-byte AES-256 key from
// `OAUTH_TOKEN_ENCRYPTION_KEY` (hex-encoded).
func getEncryptionKey() ([]byte, error) {
	keyHex := os.Getenv("OAUTH_TOKEN_ENCRYPTION_KEY")
	if keyHex == "" {
		return nil, ErrKeyNotSet
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key format: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length: got %d bytes, want 32", len(key))
	}
	return key, nil
}

// Encrypt wraps plaintext in AES-256-GCM, base64-encoded. Empty input
// returns empty output (so a nil/blank field round-trips cleanly).
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := getEncryptionKey()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt reverses Encrypt. Empty input → empty output.
func Decrypt(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	key, err := getEncryptionKey()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrInvalidCiphertext
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}
	return string(plaintext), nil
}

// SecureRandomString returns a base64url-encoded random string of the
// requested *byte* length (output is ~4/3 of that, no padding).
func SecureRandomString(byteLength int) (string, error) {
	if byteLength <= 0 {
		return "", fmt.Errorf("length must be greater than 0")
	}
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// GenerateState returns a 32-byte (43-char base64url) OAuth state.
func GenerateState() (string, error) {
	return SecureRandomString(32)
}

// GenerateNonce returns a 24-byte (32-char base64url) OIDC nonce.
func GenerateNonce() (string, error) {
	return SecureRandomString(24)
}

// GetClientIP walks the standard proxy-header chain. Matches the
// behaviour of internal/shared/utils.GetClientIP so audit rows from
// identity OIDC logins look identical to the rows the auth module
// writes from password / OAuth logins.
func GetClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if first := strings.TrimSpace(strings.SplitN(v, ",", 2)[0]); first != "" && first != "unknown" {
			return first
		}
	}
	if v := r.Header.Get("X-Real-IP"); v != "" && v != "unknown" {
		return v
	}
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Client-IP"); v != "" && v != "unknown" {
		return v
	}
	if r.RemoteAddr != "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			return host
		}
		return r.RemoteAddr
	}
	return ""
}
