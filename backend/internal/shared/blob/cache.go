package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
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
// A cached entry records the TTL its URL was signed for, because two
// callers may presign the SAME key at different TTLs (a long-lived
// operator preview and a short-lived public redirect, say). Reusing the
// longer signature for the shorter caller would silently extend that
// caller's revocation window, so PresignGet reuses an entry only when it
// does not outlive what was asked for. The TTL lives in the VALUE, not
// the cache key: one object key still maps to exactly one entry, so
// Put, Delete and InvalidateGet stay single-DEL operations and the cache
// never has to enumerate keys to invalidate.
//
// PresignPut and Exists are pass-through — only PresignGet benefits
// from caching, the rest mutate or HEAD and must hit the origin.
type CachedStore struct {
	inner   Store
	redis   *redis.Client
	cacheFn func(key string) string
	getTTL  time.Duration
	buffer  time.Duration
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
		buffer:  cfg.CacheBuffer,
	}
}

func (c *CachedStore) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (*PresignedPut, error) {
	return c.inner.PresignPut(ctx, key, contentType, ttl)
}

// Put is a pass-through that also drops any cached presigned-GET URL
// for the key — the object's content just changed underneath it, so a
// cached URL would still resolve but the invalidation keeps the cache
// honest on key reuse. Best-effort, mirroring Delete.
func (c *CachedStore) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	if err := c.redis.Del(ctx, c.cacheFn(key)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		_ = err
	}
	return c.inner.Put(ctx, key, contentType, body)
}

func (c *CachedStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = c.getTTL
	}
	cacheKey := c.cacheFn(key)
	if raw, err := c.redis.Get(ctx, cacheKey).Result(); err == nil && raw != "" {
		// Reuse only a URL that does not outlive what this caller asked
		// for. A longer-signed entry would hand this caller a URL that
		// stays valid past its own revocation window; a shorter one is
		// always safe — the caller simply re-presigns sooner.
		if cachedTTL, cachedURL, ok := decodeCachedGet(raw); ok && cachedTTL <= ttl {
			return cachedURL, nil
		}
	} else if err != nil && !errors.Is(err, redis.Nil) {
		// Degrade to direct presign on a Redis fault. The URL will
		// still work; the SPA will just refresh more often than ideal.
	}
	url, err := c.inner.PresignGet(ctx, key, ttl)
	if err != nil {
		return "", err
	}
	cacheTTL := ttl - c.buffer
	if cacheTTL <= 0 {
		cacheTTL = ttl / 2
	}
	if cacheTTL > 0 {
		// Overwrite unconditionally rather than leave a longer entry in
		// place: the cache converges on the shortest TTL any caller asks
		// for, which is safe for every caller and still yields a stable
		// URL for the SPA's <img> tag.
		if setErr := c.redis.Set(ctx, cacheKey, encodeCachedGet(ttl, url), cacheTTL).Err(); setErr != nil {
			// Silent — the next call will just re-presign.
			_ = setErr
		}
	}
	return url, nil
}

// encodeCachedGet renders a cache entry: the TTL the URL was signed for,
// then the URL. Nanoseconds keep the round-trip exact.
func encodeCachedGet(ttl time.Duration, url string) string {
	return strconv.FormatInt(int64(ttl), 10) + "|" + url
}

// decodeCachedGet splits an entry written by encodeCachedGet. ok is false
// for anything else — including a bare URL written by an older build,
// which the caller then re-presigns and overwrites, so the cache
// self-heals within one entry lifetime rather than needing a flush.
func decodeCachedGet(raw string) (ttl time.Duration, url string, ok bool) {
	sep := strings.IndexByte(raw, '|')
	if sep <= 0 {
		return 0, "", false
	}
	n, err := strconv.ParseInt(raw[:sep], 10, 64)
	if err != nil || n <= 0 {
		return 0, "", false
	}
	return time.Duration(n), raw[sep+1:], true
}

// PresignGetDownload delegates to the inner store's download-presign capability
// when it has one, else falls back to a plain presigned GET (the object-key
// filename). Download URLs are NOT cached — they vary by filename and are
// low-frequency compared to the avatar-preview reads PresignGet caches.
func (c *CachedStore) PresignGetDownload(ctx context.Context, key, downloadAs string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = c.getTTL
	}
	if dl, ok := c.inner.(ObjectDownloadPresigner); ok {
		return dl.PresignGetDownload(ctx, key, downloadAs, ttl)
	}
	return c.inner.PresignGet(ctx, key, ttl)
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
