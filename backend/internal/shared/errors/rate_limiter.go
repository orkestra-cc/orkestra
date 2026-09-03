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

// RateLimiter is the per-IP request bound behind the api:general
// middleware, and nothing else.
//
// It used to carry the auth lockout too, through a config map that
// SetAuthFailedConfig rewrote on EVERY login and service-account grant
// while Check, IsBlocked and IsLockedOut read it without a lock. A
// concurrent map read and write is a fatal runtime error, so any
// anonymous caller could stop the process (H-1). Lockout now lives in
// the Redis attempt counters
// (internal/core/auth/services/attempt_counter.go), which are shared
// across replicas, honour the admin-managed window, and survive a
// restart — none of which a per-process token bucket could do.
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
	// General API endpoints — the only surviving config (H-1: the
	// auth-facing configs and their unlocked readers are gone).
	rl.configs["api:general"] = &RateLimitConfig{
		Capacity:   100,
		RefillRate: 10,
		Window:     time.Minute,
		MaxBurst:   20,
	}
}

// Check checks if a request should be rate limited
func (rl *RateLimiter) Check(ctx context.Context, key string, configName string) *RateLimitResult {
	config := rl.configFor(configName)

	bucketKey := fmt.Sprintf("%s:%s", configName, key)
	bucket := rl.getBucket(bucketKey, config)

	allowed := bucket.consume(1)
	// remaining is read under the bucket's own mutex: consume mutates
	// tokens, and a second goroutine's consume races this read.
	remaining := bucket.remaining()

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

// configFor resolves a config under the read lock, falling back to
// api:general. One lookup, one lock acquisition — the previous shape
// read the map twice, unlocked, and the fallback read could observe a
// map mid-write.
func (rl *RateLimiter) configFor(name string) *RateLimitConfig {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	if c, ok := rl.configs[name]; ok {
		return c
	}
	return rl.configs["api:general"]
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

// remaining reads the token count under the bucket's mutex.
func (tb *TokenBucket) remaining() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return int(tb.tokens)
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
			cfg := rl.configFor(configName)
			result := rl.Check(r.Context(), clientIP, configName)

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.Capacity))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetTime.Unix(), 10))

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))

				// Create rate limit error
				rateLimitErr := RateLimitError("rate limit exceeded").
					WithDetail("limit", cfg.Capacity).
					WithDetail("window", cfg.Window.String()).
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

// Supporting types and functions

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
