package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestDropsLegacyIndexOnceAndIsIdempotent(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	s := make([]byte, 4)
	_, _ = rand.Read(s)
	db := client.Database("orkestra_test_mig0028_" + hex.EncodeToString(s))
	t.Cleanup(func() { _ = db.Drop(context.Background()); _ = client.Disconnect(context.Background()) })
	coll := db.Collection("notification_templates")
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "templateId", Value: 1}, {Key: "locale", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if d, err := run(ctx, db, true, logger); err != nil || !d {
		t.Fatalf("dry-run should report a drop: %v %v", d, err)
	}
	// The dry-run must not have touched the index.
	if names, err := coll.Indexes().ListSpecifications(ctx); err != nil {
		t.Fatal(err)
	} else if !hasIndex(names, legacyIndex) {
		t.Fatal("dry-run must not drop the legacy index")
	}
	if d, err := run(ctx, db, false, logger); err != nil || !d {
		t.Fatalf("first run should drop: %v %v", d, err)
	}
	if d, err := run(ctx, db, false, logger); err != nil || d {
		t.Fatalf("second run must be a no-op: %v %v", d, err)
	}
}

func hasIndex(specs []*mongo.IndexSpecification, name string) bool {
	for _, s := range specs {
		if s.Name == name {
			return true
		}
	}
	return false
}
