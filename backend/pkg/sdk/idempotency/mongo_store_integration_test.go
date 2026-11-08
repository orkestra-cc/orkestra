package idempotency

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
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
	spec := CollectionSpec("idem")
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
			_, rec, err := s.Begin(context.Background(), "s", "k", "fp", now, now.Add(time.Minute), now.Add(time.Hour))
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
	ownerA, rec, err := s.Begin(ctx, "s", "k", "fp1", now, now.Add(time.Minute), now.Add(time.Hour))
	if ownerA == "" || rec != nil || err != nil {
		t.Fatalf("first: %q %v %v", ownerA, rec, err)
	}
	// Lock still valid: not taken over.
	if o, rec, _ := s.Begin(ctx, "s", "k", "fp1", now.Add(time.Second), now.Add(2*time.Minute), now.Add(time.Hour)); o != "" || rec == nil || rec.Fingerprint != "fp1" {
		t.Fatalf("expected existing fp1, got %q %+v", o, rec)
	}
	later := now.Add(5 * time.Minute)
	// Expired lock, different fingerprint: no takeover, existing record returned.
	if o, rec, _ := s.Begin(ctx, "s", "k", "fp2", later, later.Add(time.Minute), later.Add(time.Hour)); o != "" || rec == nil || rec.Fingerprint != "fp1" {
		t.Fatalf("different fingerprint must not take over: %q %+v", o, rec)
	}
	// Expired lock, same fingerprint: taken over with a new owner.
	ownerB, rec, err := s.Begin(ctx, "s", "k", "fp1", later, later.Add(time.Minute), later.Add(time.Hour))
	if ownerB == "" || ownerB == ownerA || rec != nil || err != nil {
		t.Fatalf("takeover: %q %v %v", ownerB, rec, err)
	}
}

func TestMongoStaleOwnerCannotTouchNewOwnerRecord(t *testing.T) {
	s, coll := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	ownerA, _, _ := s.Begin(ctx, "s", "k", "fp", now, now.Add(time.Minute), now.Add(time.Hour))
	later := now.Add(5 * time.Minute)
	ownerB, rec, err := s.Begin(ctx, "s", "k", "fp", later, later.Add(time.Minute), later.Add(time.Hour))
	if ownerB == "" || rec != nil || err != nil {
		t.Fatalf("takeover: %q %v %v", ownerB, rec, err)
	}
	if ok, err := s.Renew(ctx, "s", "k", ownerA, later.Add(time.Hour)); ok || err != nil {
		t.Fatalf("stale renew: %v %v", ok, err)
	}
	if ok, err := s.Abandon(ctx, "s", "k", ownerA); ok || err != nil {
		t.Fatalf("stale abandon: %v %v", ok, err)
	}
	if ok, err := s.Finish(ctx, "s", "k", ownerA, 200, "", "", []byte("x"), true); ok || err != nil {
		t.Fatalf("stale finish: %v %v", ok, err)
	}
	var cur Record
	if err := coll.FindOne(ctx, bson.M{"scope": "s", "key": "k"}).Decode(&cur); err != nil {
		t.Fatalf("record gone: %v", err)
	}
	if cur.State != StateInProgress || cur.Owner != ownerB {
		t.Fatalf("record touched: %+v", cur)
	}
	// The new owner still works, and renewal extends the lock.
	if ok, _ := s.Renew(ctx, "s", "k", ownerB, later.Add(time.Hour)); !ok {
		t.Fatal("owner renew failed")
	}
	if ok, _ := s.Finish(ctx, "s", "k", ownerB, 201, "", "", []byte("y"), true); !ok {
		t.Fatal("owner finish failed")
	}
}

func TestMiddlewareRenewalBlocksConcurrentRetry(t *testing.T) {
	s, _ := newTestStore(t)
	n := 0
	var mu sync.Mutex
	release := make(chan struct{})
	h := Middleware(s, Options{LockTTL: 150 * time.Millisecond})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		<-release
		w.WriteHeader(201)
	}))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- do(t, h, "u1", goodKey, "a") }()
	// Wait well past several LockTTLs: without renewal the lock would expire.
	time.Sleep(600 * time.Millisecond)
	rr := do(t, h, "u1", goodKey, "a")
	if rr.Code != 409 || code(t, rr) != CodeInProgress {
		t.Fatalf("retry got %d %s", rr.Code, rr.Body)
	}
	close(release)
	if first := <-done; first.Code != 201 {
		t.Fatalf("first got %d", first.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	if n != 1 {
		t.Fatalf("handler ran %d times", n)
	}
	if rr := do(t, h, "u1", goodKey, "a"); rr.Code != 201 || rr.Header().Get(HeaderReplayed) != "true" {
		t.Fatalf("replay got %d", rr.Code)
	}
}

func TestMongoFinishThenBeginReturnsDone(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	owner, _, _ := s.Begin(ctx, "s", "k", "fp", now, now.Add(time.Minute), now.Add(time.Hour))
	if ok, err := s.Finish(ctx, "s", "k", owner, 201, "application/json", "/x/1", []byte(`{"a":1}`), true); err != nil || !ok {
		t.Fatal(ok, err)
	}
	_, rec, err := s.Begin(ctx, "s", "k", "fp", now.Add(time.Hour), now.Add(2*time.Hour), now.Add(3*time.Hour))
	if err != nil || rec == nil {
		t.Fatalf("got %v %v", rec, err)
	}
	if rec.State != StateDone || rec.Status != 201 || rec.ContentType != "application/json" || rec.Location != "/x/1" || string(rec.Body) != `{"a":1}` || !rec.BodyStored {
		t.Fatalf("bad record %+v", rec)
	}
	// Abandon after done is a no-op; abandon of an in-progress key frees it.
	if ok, _ := s.Abandon(ctx, "s", "k", owner); ok {
		t.Fatal("abandon touched a done record")
	}
	o2, _, _ := s.Begin(ctx, "s", "k2", "fp", now, now.Add(time.Minute), now.Add(time.Hour))
	if ok, err := s.Abandon(ctx, "s", "k2", o2); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if o, rec, _ := s.Begin(ctx, "s", "k2", "fp", now, now.Add(time.Minute), now.Add(time.Hour)); o == "" || rec != nil {
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
