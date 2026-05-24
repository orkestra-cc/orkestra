package blob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CachedStore wraps a Store with a Redis-backed presigned-GET cache.
// Returning a stable URL for ~50 minutes lets the SPA's <img> tag
// honour HTTP caching across page navigations instead of re-fetching
// the body on every navbar render. Cached entries are invalidated by
// CachedStore on Delete and by the user module's avatar mutation
// paths via InvalidateGet.
//
// PresignPut and Exists are pass-through — only PresignGet benefits
// from caching, the rest mutate or HEAD and must hit the origin.
type CachedStore struct {
	inner   Store
	redis   *redis.Client
	cacheFn func(key string) string
	getTTL  time.Duration
}

// CachedConfig configures CachedStore. Defaults: SignedGetTTL = 60min,
// CacheBuffer = 10min, KeyPrefix = "blob:url:".
type CachedConfig struct {
	SignedGetTTL time.Duration
	CacheBuffer  time.Duration
	KeyPrefix    string
}

// NewCached wraps a Store with Redis caching. The cached entry's TTL
// is SignedGetTTL - CacheBuffer so the SPA never receives a URL that
// is about to expire. Both inner and redis must be non-nil; supplying
// a nil redis client returns inner unchanged so callers can degrade
// gracefully when Redis isn't available.
func NewCached(inner Store, rdb *redis.Client, cfg CachedConfig) Store {
	if inner == nil {
		return nil
	}
	if rdb == nil {
		return inner
	}
	if cfg.SignedGetTTL <= 0 {
		cfg.SignedGetTTL = time.Hour
	}
	if cfg.CacheBuffer <= 0 {
		cfg.CacheBuffer = 10 * time.Minute
	}
	if cfg.CacheBuffer >= cfg.SignedGetTTL {
		cfg.CacheBuffer = cfg.SignedGetTTL / 2
	}
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "blob:url:"
	}
	return &CachedStore{
		inner:   inner,
		redis:   rdb,
		cacheFn: func(key string) string { return prefix + key },
		getTTL:  cfg.SignedGetTTL,
	}
}

func (c *CachedStore) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (*PresignedPut, error) {
	return c.inner.PresignPut(ctx, key, contentType, ttl)
}

func (c *CachedStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = c.getTTL
	}
	cacheKey := c.cacheFn(key)
	if cached, err := c.redis.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
		return cached, nil
	} else if err != nil && !errors.Is(err, redis.Nil) {
		// Degrade to direct presign on a Redis fault. The URL will
		// still work; the SPA will just refresh more often than ideal.
	}
	url, err := c.inner.PresignGet(ctx, key, ttl)
	if err != nil {
		return "", err
	}
	cacheTTL := ttl - 10*time.Minute
	if cacheTTL <= 0 {
		cacheTTL = ttl / 2
	}
	if cacheTTL > 0 {
		if setErr := c.redis.Set(ctx, cacheKey, url, cacheTTL).Err(); setErr != nil {
			// Silent — the next call will just re-presign.
			_ = setErr
		}
	}
	return url, nil
}

func (c *CachedStore) Delete(ctx context.Context, key string) error {
	if err := c.redis.Del(ctx, c.cacheFn(key)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		// Best-effort — Mongo is the source of truth for which key is
		// "active"; a stale URL in Redis just resolves to a 404 GET.
		_ = err
	}
	return c.inner.Delete(ctx, key)
}

func (c *CachedStore) Exists(ctx context.Context, key string) (bool, error) {
	return c.inner.Exists(ctx, key)
}

// InvalidateGet drops the cached URL for one key without touching the
// underlying blob. Callers use this when the user's avatar source
// flips away from "uploaded" (Initials or OAuth) so the next read
// path serves the new source immediately.
func (c *CachedStore) InvalidateGet(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	if err := c.redis.Del(ctx, c.cacheFn(key)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("blob: cache invalidate: %w", err)
	}
	return nil
}
