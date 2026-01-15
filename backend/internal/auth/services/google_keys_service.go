package services

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GoogleKeysService handles fetching and caching Google's public keys for JWT verification
type GoogleKeysService interface {
	GetPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error)
	RefreshKeys(ctx context.Context) error
	ValidateIDToken(ctx context.Context, idToken string, audience string) (*GoogleIDTokenClaims, error)
}

type GoogleIDTokenClaims struct {
	Issuer          string `json:"iss"`
	Audience        string `json:"aud"`
	Subject         string `json:"sub"`
	Email           string `json:"email"`
	EmailVerified   bool   `json:"email_verified"`
	Name            string `json:"name"`
	Picture         string `json:"picture"`
	GivenName       string `json:"given_name"`
	FamilyName      string `json:"family_name"`
	Locale          string `json:"locale"`
	IssuedAt        int64  `json:"iat"`
	ExpiresAt       int64  `json:"exp"`
	AuthorizedParty string `json:"azp,omitempty"`
	Nonce           string `json:"nonce,omitempty"`
	HostedDomain    string `json:"hd,omitempty"`
}

type GoogleJWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type GoogleJWKS struct {
	Keys []GoogleJWK `json:"keys"`
}

type googleKeysService struct {
	httpClient    *http.Client
	keysCache     map[string]*rsa.PublicKey
	cacheExpiry   time.Time
	cacheMutex    sync.RWMutex
	keysURL       string
	cacheDuration time.Duration
}

const (
	GoogleJWKSURL        = "https://www.googleapis.com/oauth2/v3/certs"
	DefaultCacheDuration = 1 * time.Hour
	GoogleIssuer1        = "https://accounts.google.com"
	GoogleIssuer2        = "accounts.google.com"
)

// NewGoogleKeysService creates a new Google keys service
func NewGoogleKeysService() GoogleKeysService {
	return &googleKeysService{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		keysCache:     make(map[string]*rsa.PublicKey),
		keysURL:       GoogleJWKSURL,
		cacheDuration: DefaultCacheDuration,
	}
}

func (g *googleKeysService) GetPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	// Check cache first
	g.cacheMutex.RLock()
	if key, exists := g.keysCache[kid]; exists && time.Now().Before(g.cacheExpiry) {
		g.cacheMutex.RUnlock()
		return key, nil
	}
	g.cacheMutex.RUnlock()

	// Cache miss or expired, refresh keys
	err := g.RefreshKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh Google keys: %w", err)
	}

	// Try cache again
	g.cacheMutex.RLock()
	key, exists := g.keysCache[kid]
	g.cacheMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("public key with kid '%s' not found", kid)
	}

	return key, nil
}

func (g *googleKeysService) RefreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.keysURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch Google keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var jwks GoogleJWKS
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("failed to unmarshal JWKS: %w", err)
	}

	// Parse and cache the keys
	newCache := make(map[string]*rsa.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Kty != "RSA" {
			continue // Skip non-RSA keys
		}

		publicKey, err := g.parseRSAPublicKey(jwk)
		if err != nil {
			// Log error but continue with other keys
			fmt.Printf("Warning: failed to parse RSA key %s: %v\n", jwk.Kid, err)
			continue
		}

		newCache[jwk.Kid] = publicKey
	}

	// Update cache atomically
	g.cacheMutex.Lock()
	g.keysCache = newCache
	g.cacheExpiry = time.Now().Add(g.cacheDuration)
	g.cacheMutex.Unlock()

	return nil
}

func (g *googleKeysService) parseRSAPublicKey(jwk GoogleJWK) (*rsa.PublicKey, error) {
	// Decode base64url-encoded modulus and exponent
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	// Create RSA public key
	publicKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: e,
	}

	return publicKey, nil
}

func (g *googleKeysService) ValidateIDToken(ctx context.Context, idToken string, audience string) (*GoogleIDTokenClaims, error) {
	// Parse the token header to get the key ID
	token, err := jwt.Parse(idToken, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Get the key ID from the header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid in token header")
		}

		// Get the public key
		return g.GetPublicKey(ctx, kid)
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse/validate token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims format")
	}

	// Validate issuer
	iss, _ := claims["iss"].(string)
	if iss != GoogleIssuer1 && iss != GoogleIssuer2 {
		return nil, fmt.Errorf("invalid issuer: %s", iss)
	}

	// Validate audience if provided
	if audience != "" {
		aud, _ := claims["aud"].(string)
		if aud != audience {
			return nil, fmt.Errorf("invalid audience: expected %s, got %s", audience, aud)
		}
	}

	// Convert to structured claims
	idTokenClaims := &GoogleIDTokenClaims{
		Issuer:          getStringClaim(claims, "iss"),
		Audience:        getStringClaim(claims, "aud"),
		Subject:         getStringClaim(claims, "sub"),
		Email:           getStringClaim(claims, "email"),
		EmailVerified:   getBoolClaim(claims, "email_verified"),
		Name:            getStringClaim(claims, "name"),
		Picture:         getStringClaim(claims, "picture"),
		GivenName:       getStringClaim(claims, "given_name"),
		FamilyName:      getStringClaim(claims, "family_name"),
		Locale:          getStringClaim(claims, "locale"),
		IssuedAt:        int64(getFloatClaim(claims, "iat")),
		ExpiresAt:       int64(getFloatClaim(claims, "exp")),
		AuthorizedParty: getStringClaim(claims, "azp"),
		Nonce:           getStringClaim(claims, "nonce"),
		HostedDomain:    getStringClaim(claims, "hd"),
	}

	// Validate token expiration
	if time.Now().Unix() > idTokenClaims.ExpiresAt {
		return nil, fmt.Errorf("token has expired")
	}

	// Validate issued at time (not too far in the future)
	if idTokenClaims.IssuedAt > time.Now().Add(5*time.Minute).Unix() {
		return nil, fmt.Errorf("token issued too far in the future")
	}

	return idTokenClaims, nil
}

// Helper functions for claim extraction (reused from enhanced_google_oauth_service.go)

// Google Keys Service implementation complete
