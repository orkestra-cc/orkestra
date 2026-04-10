package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoConfig struct {
	URI             string
	Database        string
	MaxPoolSize     uint64
	MinPoolSize     uint64
	MaxConnIdleTime time.Duration
	ConnectTimeout  time.Duration
}

// mongoConnectMaxAttempts bounds the retry loop that waits for MongoDB
// to become reachable and authenticated at startup. MongoDB first-boot
// with `MONGO_INITDB_ROOT_*` provisions its root user asynchronously
// after the server starts accepting connections, so an early ping races
// the auth initialization and fails. Retrying with backoff closes the
// window cleanly without a shell wait-for wrapper.
const mongoConnectMaxAttempts = 20

func NewMongoConnection(ctx context.Context, config MongoConfig) (*mongo.Database, error) {
	opts := options.Client().
		ApplyURI(config.URI).
		SetMaxPoolSize(config.MaxPoolSize).
		SetMinPoolSize(config.MinPoolSize).
		SetMaxConnIdleTime(config.MaxConnIdleTime).
		SetConnectTimeout(config.ConnectTimeout).
		SetServerSelectionTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Readiness is probed with ListDatabaseNames rather than Ping because
	// Mongo's ping command can return OK against a server that has not
	// yet finished provisioning SCRAM credentials — the ping bypasses
	// the auth path. ListDatabaseNames requires a real authenticated
	// session, which is what the rest of the app actually needs.
	db := client.Database(config.Database)

	var lastErr error
	for attempt := 1; attempt <= mongoConnectMaxAttempts; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := client.ListDatabaseNames(probeCtx, bson.D{})
		cancel()
		if err == nil {
			return db, nil
		}
		lastErr = err

		if attempt == mongoConnectMaxAttempts {
			break
		}
		// Exponential backoff capped at 5s: 500ms, 1s, 2s, 4s, 5s, 5s, ...
		backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
		slog.Info("MongoDB not authenticated yet, retrying",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", mongoConnectMaxAttempts),
			slog.Duration("backoff", backoff),
			slog.String("error", err.Error()),
		)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled waiting for MongoDB: %w", ctx.Err())
		case <-time.After(backoff):
		}
	}

	return nil, fmt.Errorf("failed to authenticate with MongoDB after %d attempts: %w", mongoConnectMaxAttempts, lastErr)
}

func DisconnectMongo(ctx context.Context, db *mongo.Database) error {
	return db.Client().Disconnect(ctx)
}
