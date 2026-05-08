// Binary 0001_unify_clients is the one-shot migration that retires the
// clientbilling_customers collection by folding every row into the
// matching personal Tenant. See docs/migrations/0001_unified_clients.md
// for the operator runbook.
//
// Usage:
//
//	go run ./cmd/migrations/0001_unify_clients --dry-run
//	go run ./cmd/migrations/0001_unify_clients
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

	"github.com/orkestra/backend/cmd/migrations/0001_unify_clients/migrator"
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
		slog.Int("rows", sum.Rows),
		slog.Int("skipped", sum.Skipped),
		slog.Int("tenantsCreated", sum.TenantsCreated),
		slog.Int("memberships", sum.Memberships),
		slog.Int("patched", sum.Patched),
		slog.Int64("subs", sum.Pivots.Subscriptions),
		slog.Int64("invoices", sum.Pivots.Invoices),
		slog.Int64("transactions", sum.Pivots.Transactions),
		slog.Int64("paymentMethods", sum.Pivots.PaymentMethods),
		slog.Int64("entitlements", sum.Pivots.Entitlements),
		slog.Int64("durationMs", sum.DurationMS))

	if err != nil {
		logger.Error("migration failed (some rows did not complete)", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
