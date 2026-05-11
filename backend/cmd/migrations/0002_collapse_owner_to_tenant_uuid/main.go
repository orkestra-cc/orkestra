// Binary 0002_collapse_owner_to_tenant_uuid is the one-shot migration that
// renames the polymorphic-owner bson fields (`ownerKind`, `ownerUUID`) to
// the unified `tenantUUID` after Phase 4 of the Unified Client Aggregate
// refactor collapses the polymorphism. See
// docs/migrations/0002_collapse_owner_to_tenant_uuid.md for the operator
// runbook.
//
// Usage:
//
//	go run ./cmd/migrations/0002_collapse_owner_to_tenant_uuid --dry-run
//	go run ./cmd/migrations/0002_collapse_owner_to_tenant_uuid
//
// Connects to Mongo from MONGO_URI / MONGO_DATABASE so the binary stays
// independent of the full server config validation.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/cmd/migrations/0002_collapse_owner_to_tenant_uuid/migrator"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	mongoURI := flag.String("mongo-uri", "", "Mongo URI (default $MONGO_URI)")
	mongoDB := flag.String("mongo-db", "", "Mongo database name (default $MONGO_DATABASE)")
	flag.Parse()

	if *mongoURI == "" {
		*mongoURI = os.Getenv("MONGO_URI")
	}
	if *mongoDB == "" {
		*mongoDB = os.Getenv("MONGO_DATABASE")
	}
	if *mongoURI == "" || *mongoDB == "" {
		fmt.Fprintln(os.Stderr, "MONGO_URI and MONGO_DATABASE must be set (or pass --mongo-uri / --mongo-db)")
		os.Exit(2)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(*mongoURI))
	if err != nil {
		logger.Error("connect mongo", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dcancel()
		_ = client.Disconnect(dctx)
	}()
	if err := client.Ping(ctx, nil); err != nil {
		logger.Error("ping mongo", slog.String("error", err.Error()))
		os.Exit(1)
	}
	db := client.Database(*mongoDB)

	store := newMongoStore(db)
	m := &migrator.Migrator{Store: store, Logger: logger, DryRun: *dryRun}

	logger.Info("migration starting",
		slog.String("migration", migrator.MigrationName),
		slog.Bool("dryRun", *dryRun),
		slog.String("database", *mongoDB))

	sum, err := m.Run(ctx)
	logger.Info("migration finished",
		slog.Bool("skipped", sum.Skipped),
		slog.Int("collections", len(sum.Results)),
		slog.Int64("durationMs", sum.DurationMS))
	for _, r := range sum.Results {
		logger.Info("collection result",
			slog.String("collection", r.Collection),
			slog.Int64("matched", r.Matched),
			slog.Int64("modified", r.Modified))
	}
	if err != nil {
		logger.Error("migration failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
