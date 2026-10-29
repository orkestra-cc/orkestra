package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
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

// fakeDecrypt treats "enc:<plain>" as decryptable and anything else as
// a value encrypted under a lost key.
func fakeDecrypt(s string) (string, error) {
	if p, ok := strings.CutPrefix(s, "enc:"); ok {
		return p, nil
	}
	return "", errors.New("cipher: message authentication failed")
}

func fakeHash(s string) (string, error) { return "argon:" + s, nil }

func TestMigrateLegacyPIN(t *testing.T) {
	ctx, coll := testColl(t)
	if _, err := coll.InsertMany(ctx, []any{
		bson.M{"uuid": "ok", "pin": "enc:1234"},
		bson.M{"uuid": "has-hash", "pin": "enc:9999", "pinHash": "argon:5555"},
		bson.M{"uuid": "bad-key", "pin": "garbage"},
		bson.M{"uuid": "not-digits", "pin": "enc:12a4"},
		bson.M{"uuid": "empty", "pin": ""},
		bson.M{"uuid": "untouched", "username": "x"},
	}); err != nil {
		t.Fatal(err)
	}

	dry, err := migrate(ctx, coll, fakeHash, fakeDecrypt, true)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := coll.CountDocuments(ctx, bson.M{"pin": bson.M{"$exists": true}}); n != 5 {
		t.Fatalf("dry run wrote: %d rows still carry pin, want 5", n)
	}

	res, err := migrate(ctx, coll, fakeHash, fakeDecrypt, false)
	if err != nil {
		t.Fatal(err)
	}
	want := result{Candidates: 4, Hashed: 1, KeptExisting: 1, Dropped: 2}
	if res != want || dry != want {
		t.Fatalf("result=%+v dry=%+v, want %+v", res, dry, want)
	}
	if n, _ := coll.CountDocuments(ctx, bson.M{"pin": bson.M{"$exists": true}}); n != 0 {
		t.Fatalf("%d rows still carry the legacy pin", n)
	}
	hashOf := func(id string) string {
		var row struct {
			PINHash string `bson:"pinHash"`
		}
		_ = coll.FindOne(ctx, bson.M{"uuid": id}).Decode(&row)
		return row.PINHash
	}
	for id, h := range map[string]string{"ok": "argon:1234", "has-hash": "argon:5555", "bad-key": "", "not-digits": "", "empty": "", "untouched": ""} {
		if got := hashOf(id); got != h {
			t.Fatalf("%s: pinHash=%q, want %q", id, got, h)
		}
	}

	again, err := migrate(ctx, coll, fakeHash, fakeDecrypt, false)
	if err != nil || again != (result{}) {
		t.Fatalf("re-run: %+v err=%v, want nothing to do", again, err)
	}
}
