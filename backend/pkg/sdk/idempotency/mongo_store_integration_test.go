package idempotency

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Skips unless MONGO_TEST_URI is set.
func newTestStore(t *testing.T) (Store, *mongo.Collection) {
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
	db := client.Database("orkestra_test_idempotency_" + hex.EncodeToString(suffix))
	t.Cleanup(func() {
		c, cn := context.WithTimeout(context.Background(), 5*time.Second)
		defer cn()
		_ = db.Drop(c)
		_ = client.Disconnect(c)
	})
	coll := db.Collection("idem")
	spec := CollectionSpec("idem", time.Hour)
	for _, is := range spec.Indexes {
		keys := bson.D{}
		for _, k := range is.OrderedKeys {
			keys = append(keys, bson.E{Key: k.Field, Value: k.Direction})
		}
		for f, d := range is.Keys {
			keys = append(keys, bson.E{Key: f, Value: d})
		}
		o := options.Index().SetUnique(is.Unique)
		if is.ExpireAt {
			o.SetExpireAfterSeconds(0)
		}
		if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys, Options: o}); err != nil {
			t.Fatalf("create index: %v", err)
		}
	}
	return NewMongoStore(coll), coll
}

func TestMongoBeginRace(t *testing.T) {
	s, _ := newTestStore(t)
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	owners := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := s.Begin(context.Background(), "s", "k", "fp", now, now.Add(time.Minute), now.Add(time.Hour))
			if err != nil {
				t.Errorf("begin: %v", err)
				return
			}
			if rec == nil {
				mu.Lock()
				owners++
				mu.Unlock()
			} else if rec.State != StateInProgress {
				t.Errorf("state %s", rec.State)
			}
		}()
	}
	wg.Wait()
	if owners != 1 {
		t.Fatalf("owners=%d", owners)
	}
}

func TestMongoTakeOverExpiredLock(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if rec, err := s.Begin(ctx, "s", "k", "fp1", now, now.Add(time.Minute), now.Add(time.Hour)); rec != nil || err != nil {
		t.Fatalf("first: %v %v", rec, err)
	}
	// Lock still valid: not taken over.
	if rec, _ := s.Begin(ctx, "s", "k", "fp2", now.Add(time.Second), now.Add(2*time.Minute), now.Add(time.Hour)); rec == nil || rec.Fingerprint != "fp1" {
		t.Fatalf("expected existing fp1, got %+v", rec)
	}
	later := now.Add(5 * time.Minute)
	if rec, err := s.Begin(ctx, "s", "k", "fp2", later, later.Add(time.Minute), later.Add(time.Hour)); rec != nil || err != nil {
		t.Fatalf("takeover: %v %v", rec, err)
	}
	rec, _ := s.Begin(ctx, "s", "k", "fp3", later, later.Add(time.Minute), later.Add(time.Hour))
	if rec == nil || rec.Fingerprint != "fp2" {
		t.Fatalf("takeover did not update fingerprint: %+v", rec)
	}
}

func TestMongoFinishThenBeginReturnsDone(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	_, _ = s.Begin(ctx, "s", "k", "fp", now, now.Add(time.Minute), now.Add(time.Hour))
	if err := s.Finish(ctx, "s", "k", 201, "application/json", "/x/1", []byte(`{"a":1}`), true); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Begin(ctx, "s", "k", "fp", now.Add(time.Hour), now.Add(2*time.Hour), now.Add(3*time.Hour))
	if err != nil || rec == nil {
		t.Fatalf("got %v %v", rec, err)
	}
	if rec.State != StateDone || rec.Status != 201 || rec.ContentType != "application/json" || rec.Location != "/x/1" || string(rec.Body) != `{"a":1}` || !rec.BodyStored {
		t.Fatalf("bad record %+v", rec)
	}
	// Abandon after done is a no-op; abandon of an in-progress key frees it.
	_, _ = s.Begin(ctx, "s", "k2", "fp", now, now.Add(time.Minute), now.Add(time.Hour))
	if err := s.Abandon(ctx, "s", "k2"); err != nil {
		t.Fatal(err)
	}
	if rec, _ := s.Begin(ctx, "s", "k2", "fp", now, now.Add(time.Minute), now.Add(time.Hour)); rec != nil {
		t.Fatal("key not released")
	}
}

func TestCollectionSpecIndexes(t *testing.T) {
	_, coll := newTestStore(t)
	cur, err := coll.Indexes().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var idx []bson.M
	if err := cur.All(context.Background(), &idx); err != nil {
		t.Fatal(err)
	}
	var unique, ttl bool
	for _, i := range idx {
		if i["unique"] == true {
			unique = true
		}
		if _, ok := i["expireAfterSeconds"]; ok {
			ttl = true
		}
	}
	if !unique || !ttl {
		t.Fatalf("unique=%v ttl=%v indexes=%v", unique, ttl, idx)
	}
}
