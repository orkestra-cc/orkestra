package module

import (
	"context"
	"time"
)

// RedisClient is the SDK-visible subset of Redis operations addons (and the
// SDK's own ConfigService) need. The backend's *database.RedisClientAdapter
// satisfies it via structural typing; extracted addons can substitute their
// own implementation (test fakes, alternative clients) without depending on
// any specific Redis SDK version.
//
// Keep this surface minimal — every new method here freezes a contract
// across every addon. Methods present today:
//   - Get/Set/Del cover the cache primitives ConfigService uses.
//   - Keys is needed by auth's OAuth state store (pattern-scan during
//     CleanupExpiredStates) and is the smallest superset that doesn't
//     force the auth module to type-assert away from this interface.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
	// Incr / Expire provide an atomic counter. Consumers that cap
	// attempts (MFA challenge verification) need this: a read-modify-
	// write over Get/Set loses concurrent increments, which turns a
	// "5 tries" limit into "5 tries per serial attacker, unbounded for
	// a parallel one".
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, expiration time.Duration) error
}
