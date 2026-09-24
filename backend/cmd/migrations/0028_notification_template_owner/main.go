// Binary 0028_notification_template_owner prepares notification_templates for
// per-tenant ownership (TemplateDoc.OwnerTenantID): it drops the legacy
// unique index templateId_1_locale_1, which would otherwise refuse a
// tenant-owned snapshot sharing (templateId, locale) with a legacy row. The
// registry creates the new (ownerTenantId, templateId, locale) index at boot.
// No document is rewritten: rows without ownerTenantId keep answering as
// system templates until a follow-up data migration of the consuming module
// assigns their owner.
//
// Idempotent: a missing index is not an error. Runbook:
//
//  1. go run ./cmd/migrations/0028_notification_template_owner --dry-run
//  2. go run ./cmd/migrations/0028_notification_template_owner
//  3. mongosh: db.notification_templates.getIndexes().map(i => i.name)   // no templateId_1_locale_1
//
// MONGO_URI / MONGO_DATABASE.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const legacyIndex = "templateId_1_locale_1"

func main() { os.Exit(cli()) }

func cli() int {
	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	uri, dbName := os.Getenv("MONGO_URI"), os.Getenv("MONGO_DATABASE")
	if uri == "" || dbName == "" {
		logger.Error("MONGO_URI and MONGO_DATABASE are required")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		logger.Error("connect failed", "database", dbName) // never log err: URI echoes credentials
		return 1
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	dropped, err := run(ctx, client.Database(dbName), *dryRun, logger)
	if err != nil {
		logger.Error("migration failed", "error", err.Error())
		return 1
	}
	logger.Info("done", "dryRun", *dryRun, "legacyIndexDropped", dropped)
	return 0
}

// run returns true when the legacy index was (or, in dry-run, would be) dropped.
func run(ctx context.Context, db *mongo.Database, dryRun bool, logger *slog.Logger) (bool, error) {
	coll := db.Collection("notification_templates")
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		return false, err
	}
	found := false
	for cur.Next(ctx) {
		var ix bson.M
		if err := cur.Decode(&ix); err != nil {
			return false, err
		}
		if ix["name"] == legacyIndex {
			found = true
		}
	}
	if err := cur.Err(); err != nil {
		return false, err
	}
	if !found {
		logger.Info("legacy index absent, nothing to do")
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if _, err := coll.Indexes().DropOne(ctx, legacyIndex); err != nil {
		return false, err
	}
	return true, nil
}
