package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/internal/core/auth/models"
)

// cachedPublicKey represents a cached Apple public key for Redis storage
type cachedPublicKey struct {
	KeyData   []byte    `json:"keyData"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// appleOAuthService implements OAuthProviderInterface for Apple Sign In
type appleOAuthService struct {
	config     *OAuthProviderConfig
	httpClient *http.Client
	privateKey *ecdsa.PrivateKey

	// Redis-based caching for improved performance and scalability
	redisClient RedisClient
	cacheTTL    time.Duration
}

// NewAppleOAuthService creates a new Apple OAuth service with full OAuth 2.1 support
func NewAppleOAuthService(config *OAuthProviderConfig, redisClient RedisClient) (OAuthProviderInterface, error) {
	service := &appleOAuthService{
		config: config,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		redisClient: redisClient,
		cacheTTL:    24 * time.Hour, // 24-hour cache TTL for Apple public keys
	}

	// Load Apple private key from config - check both direct key and file path
	fmt.Printf("[APPLE_DEBUG] Config keys available: %+v\n", config.AdditionalConfig)

	if privateKeyPEM, exists := config.AdditionalConfig["private_key"]; exists && privateKeyPEM != "" {
		// Direct PEM key provided
		fmt.Printf("[APPLE_DEBUG] Using direct private key, length: %d\n", len(privateKeyPEM))
		if key, err := service.parsePrivateKey(privateKeyPEM); err != nil {
			return nil, fmt.Errorf("failed to load apple private key: %w", err)
		} else {
			service.privateKey = key
		}
	} else if privateKeyPath, exists := config.AdditionalConfig["private_key_path"]; exists && privateKeyPath != "" {
		// Load from file path
		fmt.Printf("[APPLE_DEBUG] Using private key file path: %s\n", privateKeyPath)
		if keyData, err := os.ReadFile(privateKeyPath); err == nil {
			fmt.Printf("[APPLE_DEBUG] Successfully read key file, length: %d\n", len(keyData))
			if key, err := service.parsePrivateKey(string(keyData)); err == nil {
				service.privateKey = key
			} else {
				return nil, fmt.Errorf("failed to parse private key from file %s: %w", privateKeyPath, err)
			}
		} else {
			fmt.Printf("[APPLE_DEBUG] Failed to read key file: %v\n", err)
			return nil, fmt.Errorf("failed to read private key file %s: %w", privateKeyPath, err)
		}
	} else {
		fmt.Printf("[APPLE_DEBUG] No private key or private key path provided\n")
	}

	return service, nil
}

// Provider identification
func (s *appleOAuthService) GetProviderName() models.OAuthProvider {
	return models.OAuthProviderApple
}

func (s *appleOAuthService) GetClientID() string {
	return s.config.ClientID
}

// Web authentication flow (OAuth 2.1 with PKCE)
func (s *appleOAuthService) GetAuthURL(state string, codeChallenge string, redirectURI string) string {
	params := url.Values{}
	params.Set("client_id", s.config.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(s.getDefaultScopes(), " "))
	params.Set("state", state)
	params.Set("response_mode", "form_post")

	// OAuth 2.1 PKCE support
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	return fmt.Sprintf("https://appleid.apple.com/auth/authorize?%s", params.Encode())
}

func (s *appleOAuthService) ExchangeCodeForToken(ctx context.Context, request *CodeExchangeRequest) (*TokenResponse, error) {
	tokenURL := "https://appleid.apple.com/auth/token"

	// Apple requires client assertion (JWT) for authentication
	clientSecret, err := s.generateClientSecret()
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "code_exchange", err)
	}

	data := url.Values{}
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", clientSecret)
	data.Set("code", request.Code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", request.RedirectURI)

	// Add PKCE code verifier if provided
	if request.CodeVerifier != "" {
		data.Set("code_verifier", request.CodeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "code_exchange", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "code_exchange", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "code_exchange", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewProviderError(models.OAuthProviderApple, "code_exchange", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode).
			WithProviderMessage(string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error,omitempty"`
		ErrorDesc    string `json:"error_description,omitempty"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "code_exchange", err)
	}

	if tokenResp.Error != "" {
		return nil, NewProviderError(models.OAuthProviderApple, "code_exchange", fmt.Errorf("%s", tokenResp.Error)).
			WithProviderMessage(tokenResp.ErrorDesc)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    expiresAt,
		Scope:        s.getDefaultScopes(),
		IssuedAt:     time.Now(),
	}, nil
}

func (s *appleOAuthService) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	tokenURL := "https://appleid.apple.com/auth/token"

	clientSecret, err := s.generateClientSecret()
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "token_refresh", err)
	}

	data := url.Values{}
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "token_refresh", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "token_refresh", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "token_refresh", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewProviderError(models.OAuthProviderApple, "token_refresh", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode).
			WithProviderMessage(string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token,omitempty"`
		IDToken      string `json:"id_token,omitempty"`
		Error        string `json:"error,omitempty"`
		ErrorDesc    string `json:"error_description,omitempty"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "token_refresh", err)
	}

	if tokenResp.Error != "" {
		return nil, NewProviderError(models.OAuthProviderApple, "token_refresh", fmt.Errorf("%s", tokenResp.Error)).
			WithProviderMessage(tokenResp.ErrorDesc)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    expiresAt,
		Scope:        s.getDefaultScopes(),
		IssuedAt:     time.Now(),
	}, nil
}

// Mobile authentication flow (ID token validation)
func (s *appleOAuthService) ValidateIDToken(ctx context.Context, request *IDTokenValidationRequest) (*UserInfo, error) {
	// Parse the ID token without verification to get the header
	token, err := jwt.Parse(request.IDToken, func(token *jwt.Token) (interface{}, error) {
		// We'll return nil here as we need to fetch Apple's public keys
		return nil, fmt.Errorf("key fetching required")
	})

	if token == nil {
		return nil, NewProviderError(models.OAuthProviderApple, "id_token_validation", err)
	}

	// Extract key ID from token header
	keyID, ok := token.Header["kid"].(string)
	if !ok {
		return nil, NewProviderError(models.OAuthProviderApple, "id_token_validation", fmt.Errorf("missing key ID in token header"))
	}

	// Fetch Apple's public key for validation
	publicKey, err := s.fetchApplePublicKey(ctx, keyID)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "id_token_validation", err)
	}

	// Validate the token with the public key
	token, err = jwt.Parse(request.IDToken, func(token *jwt.Token) (interface{}, error) {
		// Apple uses RSA-based signing methods (RS256)
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return publicKey, nil
	})

	if err != nil {
		return nil, NewProviderError(models.OAuthProviderApple, "id_token_validation", err)
	}

	if !token.Valid {
		return nil, NewProviderError(models.OAuthProviderApple, "id_token_validation", fmt.Errorf("invalid token"))
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, NewProviderError(models.OAuthProviderApple, "id_token_validation", fmt.Errorf("invalid claims"))
	}

	// Validate audience
	if request.Audience != "" {
		if aud, ok := claims["aud"].(string); !ok || aud != request.Audience {
			return nil, NewProviderError(models.OAuthProviderApple, "id_token_validation", fmt.Errorf("invalid audience"))
		}
	}

	// Extract user information from claims
	userInfo := &UserInfo{
		ProviderID:    getStringClaimFromMap(claims, "sub"),
		Email:         getStringClaimFromMap(claims, "email"),
		EmailVerified: getBoolClaimFromMap(claims, "email_verified"),
		Name:          getStringClaimFromMap(claims, "name"),
		GivenName:     getStringClaimFromMap(claims, "given_name"),
		FamilyName:    getStringClaimFromMap(claims, "family_name"),
		AccessToken:   request.AccessToken,
		ProviderData: map[string]interface{}{
			"is_private_email": getBoolClaimFromMap(claims, "is_private_email"),
			"real_user_status": getIntClaimFromMap(claims, "real_user_status"),
		},
	}

	return userInfo, nil
}

// User information retrieval (Apple doesn't provide a user info endpoint)
func (s *appleOAuthService) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	// Apple doesn't provide a user info endpoint
	// User information is only available in the ID token during initial authentication
	return nil, NewProviderError(models.OAuthProviderApple, "user_info", fmt.Errorf("Apple doesn't provide user info endpoint - use ID token instead"))
}

// Token management
func (s *appleOAuthService) RevokeToken(ctx context.Context, token string) error {
	revokeURL := "https://appleid.apple.com/auth/revoke"

	clientSecret, err := s.generateClientSecret()
	if err != nil {
		return NewProviderError(models.OAuthProviderApple, "token_revocation", err)
	}

	data := url.Values{}
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", clientSecret)
	data.Set("token", token)
	data.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revokeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return NewProviderError(models.OAuthProviderApple, "token_revocation", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return NewProviderError(models.OAuthProviderApple, "token_revocation", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NewProviderError(models.OAuthProviderApple, "token_revocation", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode)
	}

	return nil
}

// Provider capabilities
func (s *appleOAuthService) GetSupportedScopes() []string {
	return s.getDefaultScopes()
}

func (s *appleOAuthService) getDefaultScopes() []string {
	if len(s.config.Scopes) > 0 {
		return s.config.Scopes
	}
	return []string{"name", "email"}
}

func (s *appleOAuthService) GetSupportedGrantTypes() []string {
	return []string{"authorization_code", "refresh_token"}
}

func (s *appleOAuthService) SupportsRefreshTokens() bool {
	return true
}

func (s *appleOAuthService) SupportsMobileFlow() bool {
	return true
}

// Helper methods for Apple-specific functionality

func (s *appleOAuthService) generateClientSecret() (string, error) {
	if s.privateKey == nil {
		return "", fmt.Errorf("Apple private key not configured")
	}

	teamID := s.config.AdditionalConfig["team_id"]
	keyID := s.config.AdditionalConfig["key_id"]

	if teamID == "" || keyID == "" {
		return "", fmt.Errorf("Apple team_id and key_id required in config")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(6 * 30 * 24 * time.Hour).Unix(), // Apple allows up to 6 months
		"aud": "https://appleid.apple.com",
		"sub": s.config.ClientID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID

	return token.SignedString(s.privateKey)
}

func (s *appleOAuthService) parsePrivateKey(pemData string) (*ecdsa.PrivateKey, error) {
	fmt.Printf("[APPLE_DEBUG] Parsing private key - length: %d\n", len(pemData))
	fmt.Printf("[APPLE_DEBUG] First 50 chars: %q\n", pemData[:min(50, len(pemData))])
	fmt.Printf("[APPLE_DEBUG] Last 50 chars: %q\n", pemData[max(0, len(pemData)-50):])

	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		fmt.Printf("[APPLE_DEBUG] ERROR: PEM decode failed - no block found\n")
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}
	fmt.Printf("[APPLE_DEBUG] PEM block type: %s, length: %d\n", block.Type, len(block.Bytes))

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an ECDSA private key")
	}

	return ecdsaKey, nil
}

func (s *appleOAuthService) fetchApplePublicKey(ctx context.Context, keyID string) (interface{}, error) {
	// Check Redis cache first
	cacheKey := s.buildCacheKey(keyID)
	if cachedData, err := s.redisClient.Get(ctx, cacheKey); err == nil {
		// Parse cached data
		var cached cachedPublicKey
		if err := json.Unmarshal([]byte(cachedData), &cached); err == nil {
			// Verify it hasn't expired (double-check)
			if time.Now().Before(cached.ExpiresAt) {
				// Deserialize the key
				if publicKey, err := s.deserializePublicKey(cached.KeyData); err == nil {
					return publicKey, nil
				}
			}
		}
	}

	// Cache miss or expired - fetch from Apple
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://appleid.apple.com/auth/keys", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Apple public keys: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Apple public keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch Apple public keys: HTTP %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
			Kty string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
		} `json:"keys"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Apple public keys response: %w", err)
	}

	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Apple public keys: %w", err)
	}

	// Process all keys and update Redis cache
	now := time.Now()
	expiresAt := now.Add(s.cacheTTL)
	var targetKey interface{}

	for _, key := range jwks.Keys {
		// Convert JWK to RSA public key
		publicKey, err := s.convertJWKToRSAPublicKey(key.N, key.E)
		if err != nil {
			continue // Skip invalid keys but don't fail the entire operation
		}

		// Serialize the key for caching
		keyData, err := s.serializePublicKey(publicKey)
		if err != nil {
			continue // Skip keys that can't be serialized
		}

		// Create cache entry
		cached := cachedPublicKey{
			KeyData:   keyData,
			CreatedAt: now,
			ExpiresAt: expiresAt,
		}

		// Store in Redis
		cachedJSON, err := json.Marshal(cached)
		if err == nil {
			keyCacheKey := s.buildCacheKey(key.Kid)
			s.redisClient.Set(ctx, keyCacheKey, cachedJSON, s.cacheTTL)
		}

		// If this is the key we're looking for, save it
		if key.Kid == keyID {
			targetKey = publicKey
		}
	}

	if targetKey == nil {
		return nil, fmt.Errorf("key with ID %s not found in Apple's JWKS", keyID)
	}

	return targetKey, nil
}

// invalidateKeyCache manually invalidates the public key cache
// This can be useful for testing or when you know Apple has rotated keys
func (s *appleOAuthService) invalidateKeyCache(ctx context.Context) error {
	// Delete all Apple public keys from Redis
	pattern := s.buildCacheKey("*")
	keys, err := s.redisClient.Keys(ctx, pattern)
	if err != nil {
		return fmt.Errorf("failed to list cache keys: %w", err)
	}

	if len(keys) > 0 {
		return s.redisClient.Del(ctx, keys...)
	}

	return nil
}

// getCacheStats returns cache statistics for monitoring and debugging
func (s *appleOAuthService) getCacheStats(ctx context.Context) (map[string]any, error) {
	// Get all Apple public key cache entries
	pattern := s.buildCacheKey("*")
	keys, err := s.redisClient.Keys(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to list cache keys: %w", err)
	}

	totalKeys := len(keys)
	activeKeys := 0
	expiredKeys := 0
	now := time.Now()

	// Check each key for expiration
	for _, key := range keys {
		cachedData, err := s.redisClient.Get(ctx, key)
		if err != nil {
			continue // Skip keys we can't read
		}

		var cached cachedPublicKey
		if err := json.Unmarshal([]byte(cachedData), &cached); err != nil {
			continue // Skip malformed cache entries
		}

		if now.Before(cached.ExpiresAt) {
			activeKeys++
		} else {
			expiredKeys++
		}
	}

	return map[string]any{
		"total_keys":      totalKeys,
		"active_keys":     activeKeys,
		"expired_keys":    expiredKeys,
		"cache_ttl_hours": s.cacheTTL.Hours(),
	}, nil
}

// buildCacheKey creates a Redis key for Apple public key caching
func (s *appleOAuthService) buildCacheKey(keyID string) string {
	return fmt.Sprintf("apple:pubkey:%s", keyID)
}

// serializePublicKey serializes an RSA public key for caching
func (s *appleOAuthService) serializePublicKey(key interface{}) ([]byte, error) {
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not an RSA public key")
	}

	// Convert exponent to bytes for consistent base64url encoding
	eBytes := make([]byte, 4)
	for i := 0; i < 4; i++ {
		eBytes[3-i] = byte(rsaKey.E >> (8 * i))
	}
	// Remove leading zeros
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}

	keyData := map[string]string{
		"n": base64.RawURLEncoding.EncodeToString(rsaKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(eBytes),
	}

	return json.Marshal(keyData)
}

// deserializePublicKey deserializes an RSA public key from cached data
func (s *appleOAuthService) deserializePublicKey(data []byte) (interface{}, error) {
	var keyData map[string]string
	if err := json.Unmarshal(data, &keyData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal key data: %w", err)
	}

	nStr, exists := keyData["n"]
	if !exists {
		return nil, fmt.Errorf("missing modulus in cached key")
	}

	eStr, exists := keyData["e"]
	if !exists {
		return nil, fmt.Errorf("missing exponent in cached key")
	}

	return s.convertJWKToRSAPublicKey(nStr, eStr)
}

// convertJWKToRSAPublicKey converts JWK components (n, e) to RSA public key
func (s *appleOAuthService) convertJWKToRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	// Decode base64url-encoded modulus (n)
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	// Decode base64url-encoded exponent (e)
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	// Convert bytes to big integers
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	// Validate that e fits in an int (RSA exponents are typically small)
	if !e.IsInt64() {
		return nil, fmt.Errorf("exponent too large")
	}
	exponent := int(e.Int64())

	// Create RSA public key
	return &rsa.PublicKey{
		N: n,
		E: exponent,
	}, nil
}

// Helper functions for claim extraction
func getStringClaimFromMap(claims jwt.MapClaims, key string) string {
	if val, ok := claims[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getBoolClaimFromMap(claims jwt.MapClaims, key string) bool {
	if val, ok := claims[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

func getIntClaimFromMap(claims jwt.MapClaims, key string) int {
	if val, ok := claims[key]; ok {
		if f, ok := val.(float64); ok {
			return int(f)
		}
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}
