// Package openapiauth provides a shared OAuth-token minter for OpenAPI.com
// (openapi.it) APIs. The /token endpoint at oauth.openapi.it accepts HTTP
// Basic credentials (account email + API key) and returns a JWT bearer token
// with a caller-specified TTL and scope list. The minted JWT is what every
// downstream `Authorization: Bearer …` request expects.
//
// Modules that integrate with multiple OpenAPI.com products (currently
// `company` and `billing`) construct one Minter per product, configured with
// the scope list relevant to that product. Tokens are cached in Redis so
// repeated lookups across replicas reuse the same JWT until shortly before
// it expires.
package openapiauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Sentinel errors. Callers map these to user-facing responses (e.g. 502 Bad
// Gateway when ErrUpstreamAuth bubbles up — the operator must rotate the
// API key in /admin/modules).
var (
	ErrMissingCredentials  = errors.New("openapi auth: account email and api key are required")
	ErrUpstreamAuth        = errors.New("openapi auth: upstream rejected credentials")
	ErrUpstreamUnreachable = errors.New("openapi auth: upstream OAuth endpoint unreachable")
	ErrUpstreamMalformed   = errors.New("openapi auth: upstream returned malformed response")
)

// Cache is the subset of Redis the Minter needs. Either of the company or
// billing module's existing RedisClient interface satisfies this — pass the
// adapter directly. When nil the Minter falls back to in-process caching.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
}

// Config defines what a Minter needs to call oauth.openapi.it/token. All
// fields are required except OAuthBaseURL (which defaults to the production
// host) and TTL (defaults to one year).
type Config struct {
	AccountEmail string
	APIKey       string

	// OAuthBaseURL points at the OAuth host. Production: https://oauth.openapi.it.
	// Test/sandbox: https://test.oauth.openapi.it.
	OAuthBaseURL string

	// Scopes is the list passed to /token (e.g. "GET:company.openapi.com/IT-start/*").
	// The minted JWT is only valid for the scopes you request here, so the
	// list must enumerate every endpoint the caller intends to hit.
	Scopes []string

	// TTL is the requested token lifetime in seconds. Subject to whatever
	// caps the OpenAPI account allows. Zero defaults to 31536000 (1 year).
	TTL int

	// Tag identifies the minter when caching. Different products (e.g.
	// "company", "billing") with different scope lists must use different
	// tags so they don't cross-pollinate cached tokens.
	Tag string
}

// Validate ensures the Config can mint a token.
func (c *Config) Validate() error {
	if c.AccountEmail == "" || c.APIKey == "" {
		return ErrMissingCredentials
	}
	return nil
}

// Minter mints + caches OAuth bearer tokens for an OpenAPI.com product.
// Safe for concurrent use.
type Minter struct {
	cfg    Config
	cache  Cache
	http   *http.Client
	logger *slog.Logger

	// In-process fallback cache — used when no Redis Cache is supplied,
	// and as a tier in front of Redis to avoid a network round-trip per call.
	mu        sync.Mutex
	memToken  string
	memExpiry time.Time
}

// NewMinter returns a Minter ready to mint tokens. cache is optional —
// pass nil to use only in-process caching (acceptable for single-replica
// deployments). httpClient is optional — pass nil for a sensible default.
func NewMinter(cfg Config, cache Cache, httpClient *http.Client, logger *slog.Logger) *Minter {
	if cfg.OAuthBaseURL == "" {
		cfg.OAuthBaseURL = "https://oauth.openapi.it"
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 31536000
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Minter{cfg: cfg, cache: cache, http: httpClient, logger: logger}
}

// Token returns a valid bearer JWT, minting a fresh one when the cached
// token is missing or about to expire. Callers should pass the returned
// string in `Authorization: Bearer <token>`.
func (m *Minter) Token(ctx context.Context) (string, error) {
	if err := m.cfg.Validate(); err != nil {
		return "", err
	}

	now := time.Now()

	// Tier 1 — in-process cache.
	m.mu.Lock()
	if m.memToken != "" && now.Before(m.memExpiry) {
		tok := m.memToken
		m.mu.Unlock()
		return tok, nil
	}
	m.mu.Unlock()

	// Tier 2 — shared cache (Redis).
	if m.cache != nil {
		if val, err := m.cache.Get(ctx, m.cacheKey()); err == nil && val != "" {
			tok, ttl, parseErr := decodeCachedToken(val)
			if parseErr == nil && ttl > 0 {
				m.storeMem(tok, now.Add(ttl))
				return tok, nil
			}
		}
	}

	// Tier 3 — mint a fresh token.
	tok, ttl, err := m.mint(ctx)
	if err != nil {
		return "", err
	}

	expiresAt := now.Add(ttl)
	m.storeMem(tok, expiresAt)
	if m.cache != nil {
		_ = m.cache.Set(ctx, m.cacheKey(), encodeCachedToken(tok, expiresAt), ttl)
	}
	return tok, nil
}

func (m *Minter) storeMem(tok string, expiresAt time.Time) {
	m.mu.Lock()
	m.memToken = tok
	m.memExpiry = expiresAt
	m.mu.Unlock()
}

// Invalidate forgets the cached token in both tiers. Call when an upstream
// 401/403 suggests the cached JWT was revoked or rotated.
func (m *Minter) Invalidate(ctx context.Context) {
	m.mu.Lock()
	m.memToken = ""
	m.memExpiry = time.Time{}
	m.mu.Unlock()
	if m.cache != nil {
		_ = m.cache.Set(ctx, m.cacheKey(), "", time.Second)
	}
}

// cacheKey is namespaced by tag + scope-list digest + email, so changing
// any of those (e.g. operator rotates API key for a different account)
// produces a fresh cache slot rather than reusing a stale entry.
func (m *Minter) cacheKey() string {
	h := sha256.New()
	h.Write([]byte(m.cfg.AccountEmail))
	h.Write([]byte{0})
	for _, s := range m.cfg.Scopes {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return "openapiauth:" + m.cfg.Tag + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}

// mintResponse is permissive about the shape OpenAPI.com returns — observed
// variants include {token, ttl} at the top level and {data:{token, ttl}}.
type mintResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token"`
	TTL     int    `json:"ttl"`
	Message string `json:"message"`
	Data    *struct {
		Token string `json:"token"`
		TTL   int    `json:"ttl"`
	} `json:"data"`
}

func (m *Minter) mint(ctx context.Context) (string, time.Duration, error) {
	body := map[string]interface{}{
		"scopes": m.cfg.Scopes,
		"ttl":    m.cfg.TTL,
	}
	payload, _ := json.Marshal(body)

	url := strings.TrimRight(m.cfg.OAuthBaseURL, "/") + "/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrUpstreamUnreachable, err)
	}
	req.SetBasicAuth(m.cfg.AccountEmail, m.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := m.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrUpstreamUnreachable, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrUpstreamMalformed, err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		m.logger.Error("openapi auth: credentials rejected",
			"tag", m.cfg.Tag,
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return "", 0, fmt.Errorf("%w: status %d: %s", ErrUpstreamAuth, resp.StatusCode, truncate(string(respBody), 200))
	}
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("%w: status %d: %s", ErrUpstreamUnreachable, resp.StatusCode, truncate(string(respBody), 200))
	}

	var parsed mintResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrUpstreamMalformed, err)
	}

	tok, ttlSec := parsed.Token, parsed.TTL
	if tok == "" && parsed.Data != nil {
		tok = parsed.Data.Token
		ttlSec = parsed.Data.TTL
	}
	if tok == "" {
		return "", 0, fmt.Errorf("%w: empty token in response: %s", ErrUpstreamMalformed, truncate(string(respBody), 200))
	}
	if ttlSec <= 0 {
		ttlSec = m.cfg.TTL
	}

	// Subtract a 60-second safety buffer so we mint fresh ~1m before the
	// upstream considers the token expired. Important when the server clock
	// and the upstream clock drift slightly.
	ttl := time.Duration(ttlSec)*time.Second - 60*time.Second
	if ttl < 30*time.Second {
		ttl = 30 * time.Second
	}

	m.logger.Info("openapi auth: minted token",
		"tag", m.cfg.Tag,
		"ttlSec", ttlSec,
		"cachedFor", ttl.String(),
	)
	return tok, ttl, nil
}

// encodeCachedToken serialises a token + absolute expiry so a Redis HIT can
// re-derive the remaining TTL even when the server clock drifts. Format:
// `<unixExpiry>|<token>`.
func encodeCachedToken(tok string, expiresAt time.Time) string {
	return fmt.Sprintf("%d|%s", expiresAt.Unix(), tok)
}

func decodeCachedToken(s string) (string, time.Duration, error) {
	idx := strings.IndexByte(s, '|')
	if idx <= 0 {
		return "", 0, fmt.Errorf("malformed cache value")
	}
	var unix int64
	if _, err := fmt.Sscanf(s[:idx], "%d", &unix); err != nil {
		return "", 0, err
	}
	tok := s[idx+1:]
	if tok == "" {
		return "", 0, fmt.Errorf("empty token")
	}
	rem := time.Until(time.Unix(unix, 0))
	return tok, rem, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
