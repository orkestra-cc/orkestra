package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
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

	// StoreOAuthRelay persists the IdP half of a client-tier login (see
	// OAuthRelayRecord) under a fresh one-shot id for OAuthRelayTTL and
	// returns the id. The record is encrypted at rest.
	StoreOAuthRelay(ctx context.Context, rec *OAuthRelayRecord) (string, error)
	// TakeOAuthRelay atomically returns and removes a relay record. A
	// second call with the same id, an unknown id or a record past its
	// ExpiresAt is an error.
	TakeOAuthRelay(ctx context.Context, id string) (*OAuthRelayRecord, error)
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
	// Mode + LinkUserUUID — see OAuthStateClaims. Mirrored on the
	// Redis side-data row so the callback can cross-check against the
	// signed-state JWT (defeats tampering with one half in isolation).
	Mode         string `json:"mode,omitempty"`
	LinkUserUUID string `json:"linkUserUuid,omitempty"`
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
	// Mode + LinkUserUUID — see StoreOAuthStateRequest.
	Mode         string `json:"mode,omitempty"`
	LinkUserUUID string `json:"linkUserUuid,omitempty"`
}

// OAuthRelayRecord carries the IdP half of a client-tier web login from the
// operator-host callback — the only place the provider's redirect URI
// points — to the client API host, the only host that can set the client
// refresh cookie. It holds everything the application half needs and the
// state's CSRF nonce so the relay endpoint can verify the browser binding
// against the cookie the client API host set at start. Stored encrypted
// (utils.EncryptOAuthToken) under oauth:relay:<id> for OAuthRelayTTL.
type OAuthRelayRecord struct {
	Tier         string               `json:"tier"`
	Provider     models.OAuthProvider `json:"provider"`
	CSRF         string               `json:"csrf"`
	Mode         string               `json:"mode,omitempty"`
	LinkUserUUID string               `json:"linkUserUuid,omitempty"`
	// FailureCode, when non-empty, is the allowlisted login-callback error
	// the operator-host callback already decided (IdP denial, missing
	// code, provider unavailable…). The relay endpoint still verifies the
	// binding and clears the start-host cookie, then redirects with it
	// instead of running the application half. Empty means "complete".
	FailureCode     string                      `json:"failureCode,omitempty"`
	UserInfo        map[string]interface{}      `json:"userInfo,omitempty"`
	Tokens          *models.OAuthProviderTokens `json:"tokens,omitempty"`
	SecurityContext *models.SecurityContext     `json:"securityContext,omitempty"`
	DeviceInfo      *models.DeviceInfo          `json:"deviceInfo,omitempty"`
	CreatedAt       time.Time                   `json:"createdAt"`
	ExpiresAt       time.Time                   `json:"expiresAt"`
}

// OAuthRelayTTL bounds the hop between the operator-host callback and the
// client API host: one browser redirect, so seconds, not minutes.
const OAuthRelayTTL = 60 * time.Second

// OAuthStateStore defines the storage interface for OAuth states
type OAuthStateStore interface {
	Set(ctx context.Context, key string, value []byte, expiry time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	// Take atomically returns and removes a value. Login-continuation
	// challenges use this one-winner primitive after factor verification;
	// a separate Get followed by Delete permits concurrent replay.
	Take(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	DeleteByPattern(ctx context.Context, pattern string) error
	// Incr atomically increments an integer counter and returns the new
	// value, applying expiry when the counter is first created. Needed
	// wherever a read-modify-write would lose concurrent updates — the
	// MFA per-challenge attempt cap is the motivating case: with RMW,
	// N parallel guesses cost one attempt instead of N.
	Incr(ctx context.Context, key string, expiry time.Duration) (int64, error)
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
		Mode:            request.Mode,
		LinkUserUUID:    request.LinkUserUUID,
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
	// ONE-SHOT: Take (Redis GETDEL) returns the row to exactly one caller.
	// The previous Get + deferred Delete let two concurrent callbacks both
	// read the same state — the replay window the signed JWT alone cannot
	// close, because the JWT is valid for ten minutes.
	stateData, err := s.store.Take(ctx, s.buildStateKey(state))
	if err != nil {
		return nil, fmt.Errorf("OAuth state not found, expired or already used: %w", err)
	}
	var stateInfo OAuthStateInfo
	if err := json.Unmarshal(stateData, &stateInfo); err != nil {
		return nil, fmt.Errorf("failed to deserialize OAuth state: %w", err)
	}
	// Belt and braces: the store's TTL should have evicted it already.
	if time.Now().After(stateInfo.ExpiresAt) {
		return nil, fmt.Errorf("OAuth state has expired")
	}
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

func (s *oAuthStateService) StoreOAuthRelay(ctx context.Context, rec *OAuthRelayRecord) (string, error) {
	if rec == nil || rec.CSRF == "" || rec.Tier == "" || rec.Provider == "" {
		return "", fmt.Errorf("oauth relay: tier, provider and csrf are required")
	}
	id, err := GenerateOAuthCSRF()
	if err != nil {
		return "", fmt.Errorf("oauth relay: mint id: %w", err)
	}
	now := time.Now()
	rec.CreatedAt = now
	if rec.ExpiresAt.IsZero() {
		rec.ExpiresAt = now.Add(OAuthRelayTTL)
	}
	plain, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("oauth relay: serialize: %w", err)
	}
	// The record carries the IdP tokens and the user's email: encrypted at
	// rest with the same AES-256-GCM helper the provider tokens use.
	sealed, err := utils.EncryptOAuthToken(string(plain))
	if err != nil {
		return "", fmt.Errorf("oauth relay: encrypt: %w", err)
	}
	if err := s.store.Set(ctx, s.buildRelayKey(id), []byte(sealed), OAuthRelayTTL); err != nil {
		return "", fmt.Errorf("oauth relay: store: %w", err)
	}
	return id, nil
}

func (s *oAuthStateService) TakeOAuthRelay(ctx context.Context, id string) (*OAuthRelayRecord, error) {
	if id == "" {
		return nil, fmt.Errorf("oauth relay: id is required")
	}
	sealed, err := s.store.Take(ctx, s.buildRelayKey(id))
	if err != nil {
		return nil, fmt.Errorf("oauth relay not found, expired or already used: %w", err)
	}
	plain, err := utils.DecryptOAuthToken(string(sealed))
	if err != nil {
		return nil, fmt.Errorf("oauth relay: decrypt: %w", err)
	}
	var rec OAuthRelayRecord
	if err := json.Unmarshal([]byte(plain), &rec); err != nil {
		return nil, fmt.Errorf("oauth relay: deserialize: %w", err)
	}
	if time.Now().After(rec.ExpiresAt) {
		return nil, fmt.Errorf("oauth relay has expired")
	}
	return &rec, nil
}

func (s *oAuthStateService) buildRelayKey(id string) string {
	return fmt.Sprintf("oauth:relay:%s", id)
}

// Redis implementation of OAuthStateStore
type RedisOAuthStateStore struct {
	client AtomicTakeRedisClient
}

// RedisClient interface for Redis operations (to be implemented separately)
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
	// Incr / Expire have no in-tree caller — RedisOAuthStateStore.Incr
	// moved onto attemptScript (one EVAL, atomic with its TTL) so the
	// MFA per-challenge cap can't orphan a counter with no expiry. Kept
	// on the interface only because RedisClient is a contract a fork's
	// own client type may implement or consume directly; removing a
	// method from it is not an additive change.
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, expiration time.Duration) error
}

// AtomicTakeRedisClient is the narrow extension required by the state
// store: GETDEL for one-winner takes, EVAL for the attempt script.
type AtomicTakeRedisClient interface {
	RedisClient
	GetDel(ctx context.Context, key string) (string, error)
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
}

// NewRedisOAuthStateStore creates a Redis-backed OAuth state store
func NewRedisOAuthStateStore(client AtomicTakeRedisClient) OAuthStateStore {
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

func (r *RedisOAuthStateStore) Take(ctx context.Context, key string) ([]byte, error) {
	result, err := r.client.GetDel(ctx, key)
	if err != nil {
		return nil, err
	}
	return []byte(result), nil
}

func (r *RedisOAuthStateStore) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key)
}

// Incr increments the counter and stamps the TTL in ONE round trip,
// through the same script the attempt counters use. The previous shape
// sent INCR and EXPIRE separately and only on the creating call, so a
// failure between them — or a key created by any other path — left a
// counter with no expiry. For the MFA per-challenge cap that is a
// budget a recycled challenge id inherits; for a lockout counter it
// would be a permanent 429.
func (r *RedisOAuthStateStore) Incr(ctx context.Context, key string, expiry time.Duration) (int64, error) {
	if expiry <= 0 {
		expiry = MFAChallengeTTL
	}
	raw, err := r.client.Eval(ctx, attemptScript, []string{key}, expiry.Milliseconds(), "1")
	if err != nil {
		return 0, err
	}
	n, _, err := parseAttemptResult(raw)
	if err != nil {
		return 0, err
	}
	return n, nil
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
// MemoryOAuthStateStore is the in-process stand-in used by tests. It is
// mutex-guarded: the production store is Redis (already atomic per
// command), and a test double that races would make concurrency tests
// report the double's bug instead of the code under test.
type MemoryOAuthStateStore struct {
	mu     sync.Mutex
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[key] = value
	m.expiry[key] = time.Now().Add(expiry)
	return nil
}

func (m *MemoryOAuthStateStore) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func (m *MemoryOAuthStateStore) Take(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if expTime, exists := m.expiry[key]; exists && time.Now().After(expTime) {
		delete(m.states, key)
		delete(m.expiry, key)
		return nil, fmt.Errorf("key expired")
	}
	value, exists := m.states[key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}
	delete(m.states, key)
	delete(m.expiry, key)
	return append([]byte(nil), value...), nil
}

func (m *MemoryOAuthStateStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, key)
	delete(m.expiry, key)
	return nil
}

func (m *MemoryOAuthStateStore) DeleteByPattern(ctx context.Context, pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// Incr atomically increments an in-memory counter stored alongside the
// regular values, mirroring the Redis semantics (TTL applied on create).
func (m *MemoryOAuthStateStore) Incr(_ context.Context, key string, expiry time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if exp, ok := m.expiry[key]; ok && time.Now().After(exp) {
		delete(m.states, key)
		delete(m.expiry, key)
	}
	var n int64
	if raw, ok := m.states[key]; ok {
		parsed, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("memory store: counter %q is not an integer: %w", key, err)
		}
		n = parsed
	}
	n++
	m.states[key] = []byte(strconv.FormatInt(n, 10))
	if n == 1 && expiry > 0 {
		m.expiry[key] = time.Now().Add(expiry)
	}
	return n, nil
}
