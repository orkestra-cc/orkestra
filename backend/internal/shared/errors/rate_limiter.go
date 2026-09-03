package errors

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/shared/utils"
)

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*TokenBucket
	configs map[string]*RateLimitConfig
	cleaner *time.Ticker
	done    chan struct{}
}

// TokenBucket implements a token bucket algorithm
type TokenBucket struct {
	tokens     float64
	capacity   float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

// RateLimitConfig defines rate limiting configuration
type RateLimitConfig struct {
	Capacity   int           `json:"capacity"`    // Maximum tokens
	RefillRate int           `json:"refill_rate"` // Tokens per second
	Window     time.Duration `json:"window"`      // Time window
	MaxBurst   int           `json:"max_burst"`   // Maximum burst size
}

// RateLimitResult represents the result of a rate limit check
type RateLimitResult struct {
	Allowed    bool          `json:"allowed"`
	Remaining  int           `json:"remaining"`
	ResetTime  time.Time     `json:"reset_time"`
	RetryAfter time.Duration `json:"retry_after"`
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*TokenBucket),
		configs: make(map[string]*RateLimitConfig),
		cleaner: time.NewTicker(5 * time.Minute), // Clean old buckets every 5 minutes
		done:    make(chan struct{}),
	}

	// Set default configurations
	rl.setDefaultConfigs()

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// setDefaultConfigs sets up default rate limiting configurations
func (rl *RateLimiter) setDefaultConfigs() {
	// Authentication endpoints - more restrictive
	rl.configs["auth:login"] = &RateLimitConfig{
		Capacity:   5,
		RefillRate: 1,
		Window:     time.Minute,
		MaxBurst:   2,
	}

	rl.configs["auth:refresh"] = &RateLimitConfig{
		Capacity:   10,
		RefillRate: 2,
		Window:     time.Minute,
		MaxBurst:   5,
	}

	// General API endpoints
	rl.configs["api:general"] = &RateLimitConfig{
		Capacity:   100,
		RefillRate: 10,
		Window:     time.Minute,
		MaxBurst:   20,
	}

	// Security-sensitive operations
	rl.configs["security:sensitive"] = &RateLimitConfig{
		Capacity:   3,
		RefillRate: 1,
		Window:     time.Minute * 5,
		MaxBurst:   1,
	}

	// Global rate limit per IP
	rl.configs["global:ip"] = &RateLimitConfig{
		Capacity:   1000,
		RefillRate: 100,
		Window:     time.Minute,
		MaxBurst:   200,
	}

	// Failed authentication attempts
	rl.configs["auth:failed"] = &RateLimitConfig{
		Capacity:   3,
		RefillRate: 1,
		Window:     time.Minute * 15,
		MaxBurst:   1,
	}
}

// Check checks if a request should be rate limited
func (rl *RateLimiter) Check(ctx context.Context, key string, configName string) *RateLimitResult {
	config, exists := rl.configs[configName]
	if !exists {
		// If no config exists, default to general API limits
		config = rl.configs["api:general"]
	}

	bucketKey := fmt.Sprintf("%s:%s", configName, key)
	bucket := rl.getBucket(bucketKey, config)

	allowed := bucket.consume(1)
	remaining := int(bucket.tokens)

	result := &RateLimitResult{
		Allowed:   allowed,
		Remaining: remaining,
		ResetTime: time.Now().Add(config.Window),
	}

	if !allowed {
		result.RetryAfter = time.Duration(float64(time.Second) / float64(config.RefillRate))
	}

	return result
}

// CheckMultiple checks multiple rate limits for a single request
func (rl *RateLimiter) CheckMultiple(ctx context.Context, checks []RateLimitCheck) *RateLimitResult {
	var mostRestrictive *RateLimitResult

	for _, check := range checks {
		result := rl.Check(ctx, check.Key, check.ConfigName)

		if !result.Allowed {
			return result // Fail fast on first violation
		}

		// Track the most restrictive limit (lowest remaining)
		if mostRestrictive == nil || result.Remaining < mostRestrictive.Remaining {
			mostRestrictive = result
		}
	}

	return mostRestrictive
}

// RecordFailedAuth records a failed authentication attempt
func (rl *RateLimiter) RecordFailedAuth(ctx context.Context, identifier string) {
	// Consume a token from the failed auth bucket
	key := fmt.Sprintf("auth_failed:%s", identifier)
	rl.Check(ctx, key, "auth:failed")
}

// SetAuthFailedConfig replaces the "auth:failed" lockout configuration
// at runtime so admin-managed values can drive the per-IP / per-email
// brute-force guard. Password login no longer calls it — that lockout
// moved to the auth module's Redis attempt counters, which compare
// against the live threshold per attempt instead of freezing capacity
// into a bucket; the remaining caller is the service-account
// client-credentials grant, which calls it on every grant using the
// live AuthPolicyService values. The swap is cheap because it only
// mutates a map entry. New token buckets pick up the new
// capacity / window immediately; already-allocated buckets keep their
// current refill rate until they are evicted by the cleanup ticker (5
// min). Threshold < 1 or window <= 0 is ignored to avoid a misedit
// turning the limiter into a deny-all.
func (rl *RateLimiter) SetAuthFailedConfig(threshold int, window time.Duration) {
	if threshold < 1 || window <= 0 {
		return
	}
	cfg := &RateLimitConfig{
		Capacity:   threshold,
		RefillRate: 1,
		Window:     window,
		MaxBurst:   1,
	}
	rl.mu.Lock()
	rl.configs["auth:failed"] = cfg
	rl.mu.Unlock()
}

// IsBlocked checks if an identifier is currently blocked due to failed attempts
func (rl *RateLimiter) IsBlocked(ctx context.Context, identifier string) bool {
	key := fmt.Sprintf("auth_failed:%s", identifier)
	result := rl.Check(ctx, key, "auth:failed")
	return !result.Allowed
}

// IsLockedOut reports whether an identifier is CURRENTLY locked out due
// to prior failed attempts against the "auth:failed" bucket, WITHOUT
// recording an attempt of its own.
//
// Contrast with IsBlocked: IsBlocked calls Check, and Check's
// TokenBucket.consume deducts a token on every call — so IsBlocked
// itself counts toward the very lockout it is checking for. A caller
// that pre-checks IsBlocked before every request (successful or not)
// can therefore trip its own lockout purely by asking, with no failure
// ever recorded. IsLockedOut applies the identical elapsed-time refill
// accounting against the same bucket/config, but only reads the
// resulting token count (TokenBucket.peek) — it never mutates the
// bucket. Calling IsLockedOut any number of times has no effect on
// whether a subsequent legitimate attempt is allowed; only
// RecordFailedAuth (or Check/IsBlocked) can push a bucket toward
// lockout.
//
// This is an additive peek — IsBlocked, Check, RecordFailedAuth, and
// their existing callers (e.g. the login path) are unchanged.
func (rl *RateLimiter) IsLockedOut(ctx context.Context, identifier string) bool {
	const configName = "auth:failed"
	key := fmt.Sprintf("auth_failed:%s", identifier)

	config, exists := rl.configs[configName]
	if !exists {
		// Mirrors Check's fallback; unreachable in practice since
		// setDefaultConfigs always seeds "auth:failed".
		config = rl.configs["api:general"]
	}

	bucketKey := fmt.Sprintf("%s:%s", configName, key)
	bucket := rl.getBucket(bucketKey, config)

	return !bucket.peek(1)
}

// getBucket gets or creates a token bucket for the given key
func (rl *RateLimiter) getBucket(key string, config *RateLimitConfig) *TokenBucket {
	rl.mu.RLock()
	bucket, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if exists {
		return bucket
	}

	// Create new bucket
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check in case another goroutine created it
	if bucket, exists := rl.buckets[key]; exists {
		return bucket
	}

	bucket = &TokenBucket{
		tokens:     float64(config.Capacity),
		capacity:   float64(config.Capacity),
		refillRate: float64(config.RefillRate),
		lastRefill: time.Now(),
	}

	rl.buckets[key] = bucket
	return bucket
}

// consume attempts to consume tokens from the bucket
func (tb *TokenBucket) consume(tokens float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens = min(tb.capacity, tb.tokens+elapsed*tb.refillRate)
	tb.lastRefill = now

	// Check if we have enough tokens
	if tb.tokens >= tokens {
		tb.tokens -= tokens
		return true
	}

	return false
}

// peek reports whether `tokens` would currently be available, applying
// the same elapsed-time refill accounting as consume — but it never
// subtracts from the bucket and never advances lastRefill. Purely
// read-only: any number of peek calls leave the bucket exactly as a
// caller who never called it at all would find it.
func (tb *TokenBucket) peek(tokens float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	available := min(tb.capacity, tb.tokens+elapsed*tb.refillRate)

	return available >= tokens
}

// cleanup removes old buckets to prevent memory leaks
func (rl *RateLimiter) cleanup() {
	for {
		select {
		case <-rl.cleaner.C:
			rl.cleanupOldBuckets()
		case <-rl.done:
			return
		}
	}
}

// cleanupOldBuckets removes buckets that haven't been used recently
func (rl *RateLimiter) cleanupOldBuckets() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-time.Hour) // Remove buckets older than 1 hour

	for key, bucket := range rl.buckets {
		bucket.mu.Lock()
		if bucket.lastRefill.Before(cutoff) {
			delete(rl.buckets, key)
		}
		bucket.mu.Unlock()
	}
}

// Close stops the rate limiter and cleanup goroutine
func (rl *RateLimiter) Close() {
	close(rl.done)
	rl.cleaner.Stop()
}

// Middleware returns a Chi middleware for rate limiting
func (rl *RateLimiter) Middleware(configName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get client identifier (IP address)
			clientIP := getClientIP(r)

			// Check rate limit
			result := rl.Check(r.Context(), clientIP, configName)

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.configs[configName].Capacity))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetTime.Unix(), 10))

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))

				// Create rate limit error
				rateLimitErr := RateLimitError("rate limit exceeded").
					WithDetail("limit", rl.configs[configName].Capacity).
					WithDetail("window", rl.configs[configName].Window.String()).
					WithDetail("retry_after", result.RetryAfter.String()).
					WithCorrelationID(GetCorrelationID(r.Context())).
					Build()

				// Convert to HTTP error
				humaErr := rateLimitErr.ToHumaError()
				w.WriteHeader(humaErr.GetStatus())
				w.Write([]byte(humaErr.Error()))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware returns middleware specifically for authentication endpoints
func (rl *RateLimiter) AuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := getClientIP(r)

			// Check if IP is currently blocked due to failed auth attempts
			if rl.IsBlocked(r.Context(), clientIP) {
				securityErr := SecurityViolationError("too many failed authentication attempts").
					WithDetail("ip", clientIP).
					WithDetail("blocked_until", time.Now().Add(15*time.Minute)).
					WithCorrelationID(GetCorrelationID(r.Context())).
					Build()

				humaErr := securityErr.ToHumaError()
				w.WriteHeader(humaErr.GetStatus())
				w.Write([]byte(humaErr.Error()))
				return
			}

			// Apply general auth rate limiting
			result := rl.Check(r.Context(), clientIP, "auth:login")

			w.Header().Set("X-RateLimit-Limit", "5")
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetTime.Unix(), 10))

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))

				rateLimitErr := RateLimitError("authentication rate limit exceeded").
					WithDetail("retry_after", result.RetryAfter.String()).
					WithCorrelationID(GetCorrelationID(r.Context())).
					Build()

				humaErr := rateLimitErr.ToHumaError()
				w.WriteHeader(humaErr.GetStatus())
				w.Write([]byte(humaErr.Error()))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Supporting types and functions

// RateLimitCheck represents a single rate limit check
type RateLimitCheck struct {
	Key        string
	ConfigName string
}

// getClientIP returns the address used as the rate-limit bucket key.
//
// It reads only what shared/middleware.RealIP already resolved under the
// deployment's trusted-proxy policy. This used to take the leftmost
// X-Forwarded-For entry, which let any caller rotate a header value to
// get a fresh bucket — i.e. opt out of the global API rate limit
// entirely — while also letting them exhaust another client's bucket by
// claiming its address.
func getClientIP(r *http.Request) string {
	return utils.GetClientIP(r)
}

// extractFirstIP gets the first IP from a comma-separated list
func extractFirstIP(xff string) string {
	// Split by comma and get the first entry
	parts := strings.Split(xff, ",")
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return ""
}

// isValidIP performs basic IP address validation
func isValidIP(ip string) bool {
	// Basic validation: must not be empty, must not contain suspicious characters
	if ip == "" {
		return false
	}

	// Block common injection attempts
	if strings.ContainsAny(ip, ";\n\r\t<>\"'") {
		return false
	}

	// Check for reasonable length (max IPv6 with zone: ~45 chars)
	if len(ip) > 50 {
		return false
	}

	// Allow IPv4 and IPv6 patterns (basic check, not full validation)
	// IPv4: digits and dots
	// IPv6: hex digits, colons, and optional dots for mapped addresses
	for _, c := range ip {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') ||
			c == '.' || c == ':' || c == '%') {
			return false
		}
	}

	return true
}

// cleanRemoteAddr removes port from RemoteAddr if present
func cleanRemoteAddr(addr string) string {
	// RemoteAddr format is typically "IP:port" for IPv4 or "[IP]:port" for IPv6
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		// Check if this is IPv6 without port (contains multiple colons but no brackets)
		if strings.Count(addr, ":") > 1 && !strings.Contains(addr, "[") {
			return addr // IPv6 without port
		}
		// Strip port for IPv4 or bracketed IPv6
		if strings.HasPrefix(addr, "[") {
			// IPv6 with brackets: [::1]:8080 -> ::1
			if bracketEnd := strings.Index(addr, "]"); bracketEnd != -1 {
				return addr[1:bracketEnd]
			}
		}
		return addr[:idx]
	}
	return addr
}
