package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// ErrRateLimited: an llm admin operation exceeded its per-window limit.
// The concrete error is a *RateLimitedError carrying the wait.
var ErrRateLimited = errors.New("llm: rate limited")

// RateLimitedError carries how long the caller must wait (Retry-After).
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("%s (retry after %s)", ErrRateLimited, e.RetryAfter)
}
func (e *RateLimitedError) Unwrap() error { return ErrRateLimited }

// RateLimiter is a fixed-window counter in Redis, shared by every replica
// (the auth attempt-counter model; the in-process limiter of shared/errors
// stays reserved to api:general). The window index is part of the key, so
// a counter is never reused across windows: even a counter whose TTL could
// not be set stops mattering when its window ends, and none can pin a
// permanent 429. A Redis outage, or no Redis at all, allows the call (fail
// open, as auth does), with a WARN.
type RateLimiter struct {
	redis  module.RedisClient
	logger *slog.Logger
	now    func() time.Time
}

func NewRateLimiter(redis module.RedisClient, logger *slog.Logger) *RateLimiter {
	if logger == nil {
		logger = slog.Default()
	}
	return &RateLimiter{redis: redis, logger: logger, now: time.Now}
}

// Allow counts one call on key and reports whether it fits limit per
// window; when it does not, the second value is the time left in the
// window.
func (l *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration) {
	if l == nil || l.redis == nil || window <= 0 {
		return true, 0
	}
	now := l.now()
	slot := now.UnixNano() / int64(window)
	reset := time.Unix(0, (slot+1)*int64(window))
	k := "llm:rl:" + key + ":" + strconv.FormatInt(slot, 10)
	n, err := l.redis.Incr(ctx, k)
	if err != nil {
		l.logger.WarnContext(ctx, "llm: rate limiter store unavailable; allowing", slog.String("error", err.Error()))
		return true, 0
	}
	if n == 1 {
		if err := l.redis.Expire(ctx, k, window+time.Second); err != nil {
			// Never leave a counter without expiry (ruling 5.10): drop it
			// and let this call through.
			l.logger.WarnContext(ctx, "llm: rate limiter could not set the window expiry; allowing", slog.String("error", err.Error()))
			_ = l.redis.Del(ctx, k)
			return true, 0
		}
	}
	if n > int64(limit) {
		return false, max(reset.Sub(now), time.Second)
	}
	return true, 0
}

// Check is Allow as an error: nil, or a *RateLimitedError.
func (l *RateLimiter) Check(ctx context.Context, key string, limit int, window time.Duration) error {
	if ok, wait := l.Allow(ctx, key, limit, window); !ok {
		return &RateLimitedError{RetryAfter: wait}
	}
	return nil
}
