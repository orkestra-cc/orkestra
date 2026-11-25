package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func testColl(t *testing.T) (context.Context, *mongo.Collection) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("skipping integration test: set MONGO_TEST_URI to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	db := client.Database("orkestra_test_legacy_pin_" + hex.EncodeToString(suffix))
	t.Cleanup(func() { _ = db.Drop(context.Background()) })
	return ctx, db.Collection("operator_users")
}

func TestDropLegacyPIN(t *testing.T) {
	ctx, coll := testColl(t)
	if _, err := coll.InsertMany(ctx, []any{
		bson.M{"uuid": "a", "pin": "ciphertext"},
		bson.M{"uuid": "b", "pin": "ciphertext", "pinHash": "argon:keep"},
		bson.M{"uuid": "c", "pin": ""},
		bson.M{"uuid": "d", "username": "untouched"},
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := drop(ctx, coll, true); err != nil || n != 3 {
		t.Fatalf("dry run = %d, %v; want 3", n, err)
	}
	if n, _ := coll.CountDocuments(ctx, bson.M{"pin": bson.M{"$exists": true}}); n != 3 {
		t.Fatalf("dry run wrote: %d rows still carry pin, want 3", n)
	}
	if n, err := drop(ctx, coll, false); err != nil || n != 3 {
		t.Fatalf("drop = %d, %v; want 3", n, err)
	}
	if n, _ := coll.CountDocuments(ctx, bson.M{"pin": bson.M{"$exists": true}}); n != 0 {
		t.Fatalf("%d rows still carry the legacy pin", n)
	}
	var b struct {
		PINHash string `bson:"pinHash"`
	}
	_ = coll.FindOne(ctx, bson.M{"uuid": "b"}).Decode(&b)
	if b.PINHash != "argon:keep" {
		t.Fatalf("an existing pinHash was touched: %q", b.PINHash)
	}
	if n, err := drop(ctx, coll, false); err != nil || n != 0 {
		t.Fatalf("re-run = %d, %v; want 0", n, err)
	}
}
