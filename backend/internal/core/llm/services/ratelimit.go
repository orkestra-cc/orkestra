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

// scriptRunner is the optional atomic path: the backend's Redis adapter
// implements Eval, though module.RedisClient (a frozen SDK contract) does
// not declare it — the auth maintenance-lease precedent.
type scriptRunner interface {
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
}

// incrWithTTL increments the counter and gives it the window TTL when it
// has none, in one server-side step: no crash, timeout or failed command
// between the two can leave a counter without expiry (ruling 5.10).
const incrWithTTL = `local n = redis.call('INCR', KEYS[1])
if redis.call('PTTL', KEYS[1]) < 0 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n`

// RateLimiter is a fixed-window counter in Redis, shared by every replica
// (the auth attempt-counter model; the in-process limiter of shared/errors
// stays reserved to api:general). Increment and expiry are one atomic
// script when the store can run one (the backend adapter can); otherwise
// INCR then EXPIRE, deleting the counter if EXPIRE fails. The window index
// is part of the key either way, so a counter is never reused across
// windows and even one left without expiry stops mattering when its window
// ends. A Redis outage, or no Redis at all, allows the call (fail open, as
// auth does), with a WARN.
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
	ttl := window + time.Second
	n, err := l.incr(ctx, k, ttl)
	if err != nil {
		l.logger.WarnContext(ctx, "llm: rate limiter store unavailable; allowing", slog.String("error", err.Error()))
		return true, 0
	}
	if n > int64(limit) {
		return false, max(reset.Sub(now), time.Second)
	}
	return true, 0
}

func (l *RateLimiter) incr(ctx context.Context, k string, ttl time.Duration) (int64, error) {
	if sr, ok := l.redis.(scriptRunner); ok {
		v, err := sr.Eval(ctx, incrWithTTL, []string{k}, ttl.Milliseconds())
		if err != nil {
			return 0, err
		}
		n, ok := v.(int64)
		if !ok {
			return 0, fmt.Errorf("llm: rate limiter script returned %T", v)
		}
		return n, nil
	}
	n, err := l.redis.Incr(ctx, k)
	if err != nil {
		return 0, err
	}
	if n == 1 {
		if err := l.redis.Expire(ctx, k, ttl); err != nil {
			// Never leave a counter without expiry: drop it and let the
			// call through.
			_ = l.redis.Del(ctx, k)
			return 0, fmt.Errorf("set window expiry: %w", err)
		}
	}
	return n, nil
}

// Check is Allow as an error: nil, or a *RateLimitedError.
func (l *RateLimiter) Check(ctx context.Context, key string, limit int, window time.Duration) error {
	if ok, wait := l.Allow(ctx, key, limit, window); !ok {
		return &RateLimitedError{RetryAfter: wait}
	}
	return nil
}
