package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

type RedisConfig struct {
	URL             string
	MaxRetries      int
	MinIdleConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
}

// redisConnectMaxAttempts bounds the retry loop at startup. Like
// MongoDB, Redis containers can accept TCP connections before the
// `--requirepass` flag finishes being applied, producing WRONGPASS
// errors on the first few pings. Retrying with backoff closes the
// window without needing a wait-for wrapper in the container command.
const redisConnectMaxAttempts = 20

func NewRedisConnection(ctx context.Context, config RedisConfig) (*redis.Client, error) {
	opts, err := redis.ParseURL(config.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	opts.MaxRetries = config.MaxRetries
	opts.MinIdleConns = config.MinIdleConns
	opts.MaxIdleConns = config.MaxIdleConns
	opts.ConnMaxLifetime = config.ConnMaxLifetime
	opts.ReadTimeout = config.ReadTimeout
	opts.WriteTimeout = config.WriteTimeout

	// go-redis v9 feature-detects `CLIENT MAINT_NOTIFICATIONS` against the
	// server on every handshake. Stock Redis (community edition) and
	// Redis Stack 8.2 don't implement the subcommand yet, so the client
	// logs a noisy "auto mode fallback" warning on every connection.
	// Orkestra doesn't use Redis Enterprise / Cloud (where the feature
	// applies), so disable it outright and reclaim the boot logs.
	opts.MaintNotificationsConfig = &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}

	client := redis.NewClient(opts)

	var lastErr error
	for attempt := 1; attempt <= redisConnectMaxAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := client.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			return client, nil
		}
		lastErr = err

		if attempt == redisConnectMaxAttempts {
			break
		}
		backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
		slog.Info("Redis not ready, retrying",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", redisConnectMaxAttempts),
			slog.Duration("backoff", backoff),
			slog.String("error", err.Error()),
		)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled waiting for Redis: %w", ctx.Err())
		case <-time.After(backoff):
		}
	}

	return nil, fmt.Errorf("failed to ping Redis after %d attempts: %w", redisConnectMaxAttempts, lastErr)
}

func DisconnectRedis(client *redis.Client) error {
	return client.Close()
}

// RedisClientAdapter adapts *redis.Client to match the RedisClient interface
type RedisClientAdapter struct {
	client *redis.Client
}

// NewRedisClientAdapter creates a new adapter for the Redis client
func NewRedisClientAdapter(client *redis.Client) *RedisClientAdapter {
	return &RedisClientAdapter{client: client}
}

// Set implements the RedisClient interface
func (r *RedisClientAdapter) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

// Get implements the RedisClient interface
func (r *RedisClientAdapter) Get(ctx context.Context, key string) (string, error) {
	result := r.client.Get(ctx, key)
	if result.Err() != nil {
		return "", result.Err()
	}
	return result.Val(), nil
}

// GetDel atomically returns and removes a key. Redis 6.2+ provides this
// primitive directly; Orkestra deploys Redis 8.2.
func (r *RedisClientAdapter) GetDel(ctx context.Context, key string) (string, error) {
	result := r.client.GetDel(ctx, key)
	if result.Err() != nil {
		return "", result.Err()
	}
	return result.Val(), nil
}

// Del implements the RedisClient interface
func (r *RedisClientAdapter) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

// Incr implements the RedisClient interface — atomic counter increment.
func (r *RedisClientAdapter) Incr(ctx context.Context, key string) (int64, error) {
	result := r.client.Incr(ctx, key)
	if result.Err() != nil {
		return 0, result.Err()
	}
	return result.Val(), nil
}

// Expire implements the RedisClient interface.
func (r *RedisClientAdapter) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return r.client.Expire(ctx, key, expiration).Err()
}

// MGet reads several keys in ONE round trip. The authz effective-
// permission cache needs it to read its global and its per-user
// generation counter together: two separate GETs could observe the two
// counters at two different moments and compose a cache key that never
// existed. Missing keys come back as nil entries in the slice, not as
// an error.
//
// Deliberately NOT on module.RedisClient: that interface is an SDK
// contract forks implement, and adding a method to it breaks every one
// of them. Consumers type-assert for this method through their own
// narrow optional interface (see authz/services.MultiGetRedisClient,
// and the AtomicTakeRedisClient precedent in auth/services).
func (r *RedisClientAdapter) MGet(ctx context.Context, keys ...string) ([]interface{}, error) {
	result := r.client.MGet(ctx, keys...)
	if result.Err() != nil {
		return nil, result.Err()
	}
	return result.Val(), nil
}

// Keys implements the RedisClient interface
func (r *RedisClientAdapter) Keys(ctx context.Context, pattern string) ([]string, error) {
	result := r.client.Keys(ctx, pattern)
	if result.Err() != nil {
		return nil, result.Err()
	}
	return result.Val(), nil
}

// SetNX sets the key only if it does not exist, reporting whether this
// caller created it. The primitive behind the maintenance-lease election.
func (r *RedisClientAdapter) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	result := r.client.SetNX(ctx, key, value, expiration)
	if result.Err() != nil {
		return false, result.Err()
	}
	return result.Val(), nil
}

// Eval runs a Lua script server-side. The maintenance lease uses it so
// compare-and-expire and compare-and-delete are atomic: without it, one
// replica could renew or delete a lease another replica owns in the gap
// between reading the owner token and acting on it.
func (r *RedisClientAdapter) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	result := r.client.Eval(ctx, script, keys, args...)
	if result.Err() != nil {
		return nil, result.Err()
	}
	return result.Val(), nil
}
