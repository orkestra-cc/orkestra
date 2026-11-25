package syncdelta

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type doc struct {
	ID        string    `bson:"_id"`
	UUID      string    `bson:"uuid"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

func (d doc) SyncCursor() Cursor { return Cursor{UpdatedAt: d.UpdatedAt, ID: d.key()} }
func (d doc) key() string {
	if d.UUID != "" {
		return d.UUID
	}
	return d.ID
}
func (d doc) IsDeleted() bool { return false }
func (d doc) SyncID() string  { return d.key() }

// Skips unless MONGO_TEST_URI is set.
func newColl(t *testing.T) *mongo.Collection {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("skipping integration test: set MONGO_TEST_URI to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	db := client.Database("orkestra_test_syncdelta_" + hex.EncodeToString(suffix))
	t.Cleanup(func() {
		c, cn := context.WithTimeout(context.Background(), 5*time.Second)
		defer cn()
		_ = db.Drop(c)
		_ = client.Disconnect(c)
	})
	return db.Collection("items")
}

func fetch(t *testing.T, coll *mongo.Collection, cur *Cursor, now time.Time, limit int) []doc {
	return fetchBy(t, coll, cur, now, limit, "_id")
}

func fetchBy(t *testing.T, coll *mongo.Collection, cur *Cursor, now time.Time, limit int, idField string) []doc {
	t.Helper()
	ctx := context.Background()
	c, err := coll.Find(ctx, Filter(cur, now, idField), options.Find().SetSort(Sort(idField)).SetLimit(int64(limit+1)))
	if err != nil {
		t.Fatal(err)
	}
	var out []doc
	if err := c.All(ctx, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFilterPagesWithoutLossAtEqualTimestamps(t *testing.T) {
	coll := newColl(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	ts := now.Add(-time.Minute)
	for i := 0; i < 10; i++ {
		if _, err := coll.InsertOne(context.Background(), bson.M{"_id": fmt.Sprintf("id%02d", i), "updatedAt": ts}); err != nil {
			t.Fatal(err)
		}
	}
	var cur *Cursor
	var seen []string
	pages := 0
	for {
		rows := fetch(t, coll, cur, now, 3)
		p := BuildPage(rows, 3, cur, now)
		pages++
		for _, d := range p.Items {
			seen = append(seen, d.ID)
		}
		if !p.HasMore {
			break
		}
		cur, _ = DecodeCursor(p.Cursor)
		if pages > 10 {
			t.Fatal("too many pages")
		}
	}
	if pages != 4 || len(seen) != 10 {
		t.Fatalf("pages=%d seen=%v", pages, seen)
	}
	for i, id := range seen {
		if id != fmt.Sprintf("id%02d", i) {
			t.Fatalf("order: %v", seen)
		}
	}
}

func TestHorizonExcludesFreshWrites(t *testing.T) {
	coll := newColl(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, _ = coll.InsertOne(context.Background(), bson.M{"_id": "old", "updatedAt": now.Add(-10 * time.Second)})
	_, _ = coll.InsertOne(context.Background(), bson.M{"_id": "fresh", "updatedAt": now.Add(-time.Second)})
	rows := fetch(t, coll, nil, now, 10)
	if len(rows) != 1 || rows[0].ID != "old" {
		t.Fatalf("%+v", rows)
	}
	p := BuildPage(rows, 10, nil, now)
	cur, _ := DecodeCursor(p.Cursor)
	if cur.UpdatedAt.After(Horizon(now)) {
		t.Fatal("cursor past horizon")
	}
	if rows := fetch(t, coll, cur, now.Add(5*time.Second), 10); len(rows) != 1 || rows[0].ID != "fresh" {
		t.Fatalf("after horizon moved: %+v", rows)
	}
}

func TestFilterPagesObjectIDWithUUIDTieBreaker(t *testing.T) {
	coll := newColl(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	ts := now.Add(-time.Minute)
	// uuid order deliberately differs from ObjectID order.
	for i := 9; i >= 0; i-- {
		if _, err := coll.InsertOne(context.Background(), bson.M{"_id": primitive.NewObjectID(), "uuid": fmt.Sprintf("u%02d", i), "updatedAt": ts}); err != nil {
			t.Fatal(err)
		}
	}
	var cur *Cursor
	seen := map[string]int{}
	var order []string
	pages := 0
	for {
		rows := fetchBy(t, coll, cur, now, 3, "uuid")
		p := BuildPage(rows, 3, cur, now)
		pages++
		for _, d := range p.Items {
			seen[d.UUID]++
			order = append(order, d.UUID)
		}
		if !p.HasMore {
			break
		}
		cur, _ = DecodeCursor(p.Cursor)
		if pages > 10 {
			t.Fatal("too many pages")
		}
	}
	if pages != 4 || len(seen) != 10 {
		t.Fatalf("pages=%d seen=%v", pages, seen)
	}
	for i, u := range order {
		if seen[u] != 1 || u != fmt.Sprintf("u%02d", i) {
			t.Fatalf("loss/dup/order: %v", order)
		}
	}
}
