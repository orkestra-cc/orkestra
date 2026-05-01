package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
)

// OAuthStateService manages OAuth state validation and temporary storage
type OAuthStateService interface {
	// Store OAuth state with associated data
	StoreOAuthState(ctx context.Context, request *StoreOAuthStateRequest) (*OAuthStateInfo, error)

	// Validate and retrieve OAuth state
	ValidateOAuthState(ctx context.Context, state string) (*OAuthStateInfo, error)

	// Clear expired OAuth states
	CleanupExpiredStates(ctx context.Context) error
}

// StoreOAuthStateRequest contains parameters for storing OAuth state.
//
// Tier (ADR-0003 PR-D D-6) records which audience initiated the flow —
// "operator", "client", or "" for legacy pre-cutover paths. Stored
// alongside the side data so the callback can cross-check the value
// against the tier claim in the signed-state JWT; mismatch is rejected
// the same as a forged state.
//
// State (ADR-0003 PR-D D-6) is the externally-visible OAuth state value
// returned to the caller. When empty, StoreOAuthState mints a fresh
// random nonce as before; D-6 supplies the JWT-signed value here so the
// Redis row is keyed by the same CSRF nonce embedded in the JWT.
type StoreOAuthStateRequest struct {
	Provider        models.OAuthProvider    `json:"provider"`
	Tier            string                  `json:"tier"`
	State           string                  `json:"state"`
	RedirectURI     string                  `json:"redirectUri"`
	CodeVerifier    string                  `json:"codeVerifier"`  // PKCE code verifier
	CodeChallenge   string                  `json:"codeChallenge"` // PKCE code challenge
	DeviceInfo      *models.DeviceInfo      `json:"deviceInfo"`
	SecurityContext *models.SecurityContext `json:"securityContext"`
	ExpiryDuration  time.Duration           `json:"expiryDuration"` // Default: 10 minutes
}

// OAuthStateInfo contains stored OAuth state information. Tier mirrors
// StoreOAuthStateRequest.Tier so the callback can confirm the
// signed-state JWT's tier matches what the start endpoint stamped here.
type OAuthStateInfo struct {
	State           string                  `json:"state"`
	Tier            string                  `json:"tier,omitempty"`
	Provider        models.OAuthProvider    `json:"provider"`
	RedirectURI     string                  `json:"redirectUri"`
	CodeVerifier    string                  `json:"codeVerifier"`
	CodeChallenge   string                  `json:"codeChallenge"`
	DeviceInfo      *models.DeviceInfo      `json:"deviceInfo"`
	SecurityContext *models.SecurityContext `json:"securityContext"`
	CreatedAt       time.Time               `json:"createdAt"`
	ExpiresAt       time.Time               `json:"expiresAt"`
}

// OAuthStateStore defines the storage interface for OAuth states
type OAuthStateStore interface {
	Set(ctx context.Context, key string, value []byte, expiry time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	DeleteByPattern(ctx context.Context, pattern string) error
}

type oAuthStateService struct {
	store OAuthStateStore
}

// NewOAuthStateService creates a new OAuth state service
func NewOAuthStateService(store OAuthStateStore) OAuthStateService {
	return &oAuthStateService{
		store: store,
	}
}

func (s *oAuthStateService) StoreOAuthState(ctx context.Context, request *StoreOAuthStateRequest) (*OAuthStateInfo, error) {
	// ADR-0003 PR-D D-6: callers signing a state JWT supply the CSRF
	// nonce as request.State so the Redis row is keyed by the same
	// value embedded in the JWT. Pre-D-6 callers leave it empty and the
	// service mints an opaque random state (legacy behaviour).
	state := request.State
	if state == "" {
		stateBytes := make([]byte, 32)
		if _, err := rand.Read(stateBytes); err != nil {
			return nil, fmt.Errorf("failed to generate OAuth state: %w", err)
		}
		state = base64.RawURLEncoding.EncodeToString(stateBytes)
	}

	// Set default expiry if not provided
	expiry := request.ExpiryDuration
	if expiry == 0 {
		expiry = 10 * time.Minute // Default OAuth state expiry
	}

	// Create state info
	stateInfo := &OAuthStateInfo{
		State:           state,
		Tier:            request.Tier,
		Provider:        request.Provider,
		RedirectURI:     request.RedirectURI,
		CodeVerifier:    request.CodeVerifier,
		CodeChallenge:   request.CodeChallenge,
		DeviceInfo:      request.DeviceInfo,
		SecurityContext: request.SecurityContext,
		CreatedAt:       time.Now(),
		ExpiresAt:       time.Now().Add(expiry),
	}

	// Serialize state info
	stateData, err := json.Marshal(stateInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize OAuth state: %w", err)
	}

	// Store in cache with expiry
	storeKey := s.buildStateKey(state)
	if err := s.store.Set(ctx, storeKey, stateData, expiry); err != nil {
		return nil, fmt.Errorf("failed to store OAuth state: %w", err)
	}

	return stateInfo, nil
}

func (s *oAuthStateService) ValidateOAuthState(ctx context.Context, state string) (*OAuthStateInfo, error) {
	if state == "" {
		return nil, fmt.Errorf("OAuth state is required")
	}

	// Retrieve from store
	storeKey := s.buildStateKey(state)
	stateData, err := s.store.Get(ctx, storeKey)
	if err != nil {
		return nil, fmt.Errorf("OAuth state not found or expired: %w", err)
	}

	// Deserialize state info
	var stateInfo OAuthStateInfo
	if err := json.Unmarshal(stateData, &stateInfo); err != nil {
		return nil, fmt.Errorf("failed to deserialize OAuth state: %w", err)
	}

	// Validate expiry (double-check even though store should handle this)
	if time.Now().After(stateInfo.ExpiresAt) {
		// Delete expired state
		s.store.Delete(ctx, storeKey)
		return nil, fmt.Errorf("OAuth state has expired")
	}

	// Delete state after successful validation (one-time use)
	go func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.store.Delete(deleteCtx, storeKey)
	}()

	return &stateInfo, nil
}

func (s *oAuthStateService) CleanupExpiredStates(ctx context.Context) error {
	// Delete all expired OAuth states using pattern matching
	pattern := s.buildStateKey("*")
	return s.store.DeleteByPattern(ctx, pattern)
}

func (s *oAuthStateService) buildStateKey(state string) string {
	return fmt.Sprintf("oauth:state:%s", state)
}

// Redis implementation of OAuthStateStore
type RedisOAuthStateStore struct {
	client RedisClient
}

// RedisClient interface for Redis operations (to be implemented separately)
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
}

// NewRedisOAuthStateStore creates a Redis-backed OAuth state store
func NewRedisOAuthStateStore(client RedisClient) OAuthStateStore {
	return &RedisOAuthStateStore{
		client: client,
	}
}

func (r *RedisOAuthStateStore) Set(ctx context.Context, key string, value []byte, expiry time.Duration) error {
	return r.client.Set(ctx, key, value, expiry)
}

func (r *RedisOAuthStateStore) Get(ctx context.Context, key string) ([]byte, error) {
	result, err := r.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return []byte(result), nil
}

func (r *RedisOAuthStateStore) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key)
}

func (r *RedisOAuthStateStore) DeleteByPattern(ctx context.Context, pattern string) error {
	keys, err := r.client.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return nil
	}

	return r.client.Del(ctx, keys...)
}

// Memory implementation for testing
type MemoryOAuthStateStore struct {
	states map[string][]byte
	expiry map[string]time.Time
}

func NewMemoryOAuthStateStore() OAuthStateStore {
	return &MemoryOAuthStateStore{
		states: make(map[string][]byte),
		expiry: make(map[string]time.Time),
	}
}

func (m *MemoryOAuthStateStore) Set(ctx context.Context, key string, value []byte, expiry time.Duration) error {
	m.states[key] = value
	m.expiry[key] = time.Now().Add(expiry)
	return nil
}

func (m *MemoryOAuthStateStore) Get(ctx context.Context, key string) ([]byte, error) {
	// Check expiry
	if expTime, exists := m.expiry[key]; exists && time.Now().After(expTime) {
		delete(m.states, key)
		delete(m.expiry, key)
		return nil, fmt.Errorf("key expired")
	}

	value, exists := m.states[key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}

	return value, nil
}

func (m *MemoryOAuthStateStore) Delete(ctx context.Context, key string) error {
	delete(m.states, key)
	delete(m.expiry, key)
	return nil
}

func (m *MemoryOAuthStateStore) DeleteByPattern(ctx context.Context, pattern string) error {
	// Simple pattern matching (for production use proper pattern matching)
	for key := range m.states {
		if key == pattern || (pattern == "oauth:state:*" && len(key) > 12 && key[:12] == "oauth:state:") {
			delete(m.states, key)
			delete(m.expiry, key)
		}
	}
	return nil
}

// Helper functions for OAuth state validation

// ValidateOAuthCallback validates OAuth callback parameters against stored state
func ValidateOAuthCallback(stateInfo *OAuthStateInfo, code, state, codeVerifier string) error {
	// Validate state matches
	if stateInfo.State != state {
		return fmt.Errorf("invalid OAuth state")
	}

	// Validate authorization code is present
	if code == "" {
		return fmt.Errorf("authorization code is required")
	}

	// Validate PKCE code verifier if challenge was used
	if stateInfo.CodeChallenge != "" {
		if codeVerifier == "" {
			return fmt.Errorf("PKCE code verifier is required")
		}

		// Verify the code verifier matches the challenge
		expectedChallenge, err := utils.GeneratePKCEChallengeFromVerifier(codeVerifier)
		if err != nil {
			return fmt.Errorf("failed to verify PKCE challenge: %w", err)
		}

		if expectedChallenge != stateInfo.CodeChallenge {
			return fmt.Errorf("invalid PKCE code verifier")
		}
	}

	return nil
}

// GenerateSecureState generates a cryptographically secure OAuth state
func GenerateSecureState() (string, error) {
	return utils.SecureRandomString(32)
}
