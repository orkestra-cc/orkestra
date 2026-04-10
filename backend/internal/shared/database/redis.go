package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
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

// Del implements the RedisClient interface
func (r *RedisClientAdapter) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

// Keys implements the RedisClient interface
func (r *RedisClientAdapter) Keys(ctx context.Context, pattern string) ([]string, error) {
	result := r.client.Keys(ctx, pattern)
	if result.Err() != nil {
		return nil, result.Err()
	}
	return result.Val(), nil
}
