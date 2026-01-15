package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
)

// SecureRandomString generates a cryptographically secure random string
func SecureRandomString(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be greater than 0")
	}

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Convert to base64url (URL-safe base64 without padding)
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// SecureRandomStringAlphanumeric generates a secure random alphanumeric string
func SecureRandomStringAlphanumeric(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be greater than 0")
	}

	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)

	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("failed to generate random character: %w", err)
		}
		result[i] = charset[num.Int64()]
	}

	return string(result), nil
}

// SecureRandomHex generates a secure random hexadecimal string
func SecureRandomHex(byteLength int) (string, error) {
	if byteLength <= 0 {
		return "", fmt.Errorf("byte length must be greater than 0")
	}

	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	return hex.EncodeToString(bytes), nil
}

// GenerateSessionID creates a secure session identifier
func GenerateSessionID() (string, error) {
	return SecureRandomHex(16) // 32 character hex string
}

// GenerateDeviceID creates a secure device identifier
func GenerateDeviceID() (string, error) {
	return SecureRandomHex(12) // 24 character hex string
}

// GenerateState generates a secure OAuth state parameter
func GenerateState() (string, error) {
	return SecureRandomString(32) // 43 character base64url string
}

// GenerateNonce generates a secure nonce for OpenID Connect
func GenerateNonce() (string, error) {
	return SecureRandomString(24) // 32 character base64url string
}

// PKCE (Proof Key for Code Exchange) implementation for OAuth 2.1

// PKCEChallenge contains the code verifier and challenge
type PKCEChallenge struct {
	CodeVerifier  string `json:"codeVerifier"`
	CodeChallenge string `json:"codeChallenge"`
	Method        string `json:"method"`
}

// GeneratePKCEChallenge generates a PKCE code verifier and challenge pair
func GeneratePKCEChallenge() (*PKCEChallenge, error) {
	// Generate code verifier: 43-128 characters, base64url-encoded string
	// RFC 7636 recommends 32 bytes (43 chars after base64url encoding)
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}

	codeVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Generate code challenge: BASE64URL(SHA256(codeVerifier))
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return &PKCEChallenge{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		Method:        "S256", // SHA256
	}, nil
}

// GeneratePKCEChallengeFromVerifier generates a PKCE challenge from a verifier
func GeneratePKCEChallengeFromVerifier(codeVerifier string) (string, error) {
	if codeVerifier == "" {
		return "", fmt.Errorf("code verifier cannot be empty")
	}

	// Generate challenge: BASE64URL(SHA256(codeVerifier))
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return codeChallenge, nil
}

// ValidatePKCEChallenge validates a code verifier against a code challenge
func ValidatePKCEChallenge(codeVerifier, codeChallenge string) bool {
	if codeVerifier == "" || codeChallenge == "" {
		return false
	}

	// Generate challenge from verifier
	expectedChallenge, err := GeneratePKCEChallengeFromVerifier(codeVerifier)
	if err != nil {
		return false
	}

	return expectedChallenge == codeChallenge
}

// Token generation utilities

// GenerateRefreshToken creates a secure refresh token
func GenerateRefreshToken() (string, error) {
	// 64 bytes = 512 bits of entropy
	return SecureRandomString(64)
}

// GenerateAccessTokenJTI creates a secure JTI (JWT ID) for access tokens
func GenerateAccessTokenJTI() (string, error) {
	return SecureRandomHex(16) // 32 character hex string
}

// GenerateMFAToken creates a secure temporary MFA token
func GenerateMFAToken() (string, error) {
	return SecureRandomStringAlphanumeric(32)
}

// Device fingerprinting utilities

// GenerateDeviceFingerprint creates a fingerprint from device characteristics
func GenerateDeviceFingerprint(characteristics ...string) string {
	// Combine all characteristics
	combined := strings.Join(characteristics, "|")

	// Hash the combined string
	hash := sha256.Sum256([]byte(combined))

	// Return first 16 bytes as hex (32 characters)
	return hex.EncodeToString(hash[:16])
}

// Validation utilities

// IsValidUUID checks if a string is a valid UUID format
func IsValidUUID(uuid string) bool {
	// Simple UUID format validation (8-4-4-4-12 hex digits)
	if len(uuid) != 36 {
		return false
	}

	parts := strings.Split(uuid, "-")
	if len(parts) != 5 {
		return false
	}

	expectedLengths := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expectedLengths[i] {
			return false
		}
		// Check if all characters are valid hex
		for _, char := range part {
			if !((char >= '0' && char <= '9') ||
				(char >= 'a' && char <= 'f') ||
				(char >= 'A' && char <= 'F')) {
				return false
			}
		}
	}

	return true
}

// IsValidBase64URL checks if a string is valid base64url encoding
func IsValidBase64URL(s string) bool {
	// Base64URL uses A-Z, a-z, 0-9, -, _ (no padding)
	for _, char := range s {
		if !((char >= 'A' && char <= 'Z') ||
			(char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return false
		}
	}
	return true
}

// Secure comparison utilities to prevent timing attacks

// SecureCompare performs constant-time string comparison
func SecureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}

	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}

	return result == 0
}

// Hash utilities for storing sensitive data

// HashToken creates a SHA256 hash of a token for storage
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// HashRefreshToken creates a hash suitable for storing refresh tokens
func HashRefreshToken(token string) string {
	return HashToken(token)
}

// Entropy checker for password/token strength

// CalculateEntropy estimates the entropy bits of a string
func CalculateEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}

	// Count unique characters
	charSet := make(map[rune]bool)
	for _, char := range s {
		charSet[char] = true
	}

	// Calculate entropy: length * log2(charset_size)
	charsetSize := float64(len(charSet))
	if charsetSize <= 1 {
		return 0
	}

	// Using natural log and converting to log2
	entropy := float64(len(s)) * (log2(charsetSize))
	return entropy
}

// log2 calculates log base 2
func log2(x float64) float64 {
	return 0.693147180559945309417232121458 * logN(x) // ln(2) * ln(x)
}

// Simple natural logarithm approximation
func logN(x float64) float64 {
	if x <= 0 {
		return 0
	}

	// Simple approximation for log(x)
	// More accurate implementation would use math.Log
	n := 0.0
	for x >= 2 {
		x /= 2
		n++
	}

	// Linear approximation for 1 <= x < 2
	return n*0.693147180559945309417232121458 + (x - 1)
}

// Security constants

const (
	// Minimum entropy bits for secure tokens
	MinTokenEntropy = 128

	// Recommended PKCE verifier length (RFC 7636)
	PKCEVerifierMinLength = 43
	PKCEVerifierMaxLength = 128

	// Standard token lengths
	SessionIDLength    = 32 // hex characters
	DeviceIDLength     = 24 // hex characters
	RefreshTokenLength = 86 // base64url characters (64 bytes)
	StateLength        = 43 // base64url characters (32 bytes)
	NonceLength        = 32 // base64url characters (24 bytes)
)

// Convenience functions with standard lengths

// NewSessionID generates a standard session ID
func NewSessionID() (string, error) {
	return GenerateSessionID()
}

// NewDeviceID generates a standard device ID
func NewDeviceID() (string, error) {
	return GenerateDeviceID()
}

// NewState generates a standard OAuth state
func NewState() (string, error) {
	return GenerateState()
}

// NewNonce generates a standard OIDC nonce
func NewNonce() (string, error) {
	return GenerateNonce()
}

// NewRefreshToken generates a standard refresh token
func NewRefreshToken() (string, error) {
	return GenerateRefreshToken()
}

// NewPKCEChallenge generates a standard PKCE challenge
func NewPKCEChallenge() (*PKCEChallenge, error) {
	return GeneratePKCEChallenge()
}

// OAuth Token Encryption/Decryption utilities
// These functions use AES-GCM for authenticated encryption of sensitive OAuth tokens

var (
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
	ErrKeyNotSet         = errors.New("encryption key not set")
)

// getEncryptionKey retrieves the encryption key from environment variable
func getEncryptionKey() ([]byte, error) {
	keyHex := os.Getenv("OAUTH_TOKEN_ENCRYPTION_KEY")
	if keyHex == "" {
		return nil, ErrKeyNotSet
	}

	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key format: %w", err)
	}

	if len(key) != 32 { // 256-bit key
		return nil, fmt.Errorf("encryption key must be 32 bytes (256 bits), got %d bytes", len(key))
	}

	return key, nil
}

// EncryptOAuthToken encrypts an OAuth token using AES-GCM
func EncryptOAuthToken(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil // Don't encrypt empty strings
	}

	key, err := getEncryptionKey()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Return as base64
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptOAuthToken decrypts an OAuth token using AES-GCM
func DecryptOAuthToken(encryptedText string) (string, error) {
	if encryptedText == "" {
		return "", nil // Return empty string for empty input
	}

	key, err := getEncryptionKey()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	// Decode from base64
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedText)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Check minimum length
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	// Extract nonce and encrypted data
	nonce, encryptedData := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// GenerateEncryptionKey generates a new 256-bit encryption key for OAuth tokens
func GenerateEncryptionKey() (string, error) {
	key := make([]byte, 32) // 256 bits
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("failed to generate encryption key: %w", err)
	}

	return hex.EncodeToString(key), nil
}
