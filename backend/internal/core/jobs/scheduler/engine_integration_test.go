package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/internal/core/jobs/repository"
	"github.com/orkestra/backend/pkg/sdk/jobs"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const mongoTestURIEnv = "MONGO_TEST_URI"

func newTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv(mongoTestURIEnv)
	if uri == "" {
		t.Skipf("skipping integration test: set %s to run", mongoTestURIEnv)
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
	db := client.Database("orkestra_test_jobs_sched_" + hex.EncodeToString(suffix))
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(c)
		_ = client.Disconnect(c)
	})
	return db
}

type mongoPair struct {
	states repository.StateRepository
	runs   repository.RunRepository
	clock  *fakeClock
	cat    *jobs.Catalog
}

func newMongoPair(t *testing.T, defs ...jobs.Definition) *mongoPair {
	t.Helper()
	db := newTestDB(t)
	cat := jobs.NewCatalog()
	if err := cat.Register("m", defs); err != nil {
		t.Fatal(err)
	}
	return &mongoPair{
		states: repository.NewStateRepository(db.Collection(models.StatesCollection)),
		runs:   repository.NewRunRepository(db.Collection(models.RunsCollection)),
		clock:  newFakeClock(t0),
		cat:    cat,
	}
}

func (p *mongoPair) engine(t *testing.T, replica string, opt Options) *Engine {
	t.Helper()
	opt.Replica, opt.Clock = replica, p.clock
	if opt.Jitter == nil {
		opt.Jitter = func() float64 { return 0.5 }
	}
	e := New(p.cat, p.states, p.runs, opt)
	if err := e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { boundedDrain(t, e) })
	return e
}

func TestTwoEnginesOneRun(t *testing.T) {
	var calls atomic.Int32
	p := newMongoPair(t, jobs.Definition{Name: "a", Title: "a", Schedule: jobs.Every(time.Hour), Run: func(context.Context, *jobs.Run) error {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return nil
	}})
	a := p.engine(t, "replica-a", Options{})
	b := p.engine(t, "replica-b", Options{})
	p.clock.Add(time.Hour)

	var launched atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, e := range []*Engine{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			launched.Add(int32(e.Tick(context.Background())))
		}()
	}
	close(start)
	wg.Wait()
	a.drain()
	b.drain()
	if launched.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("launched %d, executions %d; want exactly 1", launched.Load(), calls.Load())
	}
	// a second round at the same instant finds nothing due
	if a.Tick(context.Background())+b.Tick(context.Background()) != 0 {
		t.Fatal("second round launched")
	}
	st, err := p.states.Get(context.Background(), "m.a")
	if err != nil {
		t.Fatal(err)
	}
	if st.Lease != nil || st.LastRun == nil || st.LastRun.Status != models.StatusSucceeded || !st.NextRunAt.After(p.clock.Now()) {
		t.Fatalf("state: %+v", st)
	}
	recs, _, _ := p.runs.List(context.Background(), "m.a", "", nil, 10)
	if len(recs) != 1 || recs[0].Status != models.StatusSucceeded {
		t.Fatalf("runs: %+v", recs)
	}
}

func TestAbandonedRunTakenOver(t *testing.T) {
	var calls atomic.Int32
	release, closeRelease := releaser(t)
	p := newMongoPair(t, jobs.Definition{
		Name: "a", Title: "a", Schedule: jobs.Every(time.Hour),
		Retry: jobs.RetryPolicy{MaxAttempts: 2, Backoff: time.Minute, MaxBackoff: time.Hour},
		Run: func(context.Context, *jobs.Run) error {
			if calls.Add(1) == 1 {
				<-release // replica A hangs (its process is "dead")
			}
			return nil
		},
	})
	ma, mb := newFakeMetrics(), newFakeMetrics()
	zero := func() float64 { return 0 }
	a := p.engine(t, "replica-a", Options{RenewInterval: time.Hour, Metrics: ma, Jitter: zero}) // never renews
	b := p.engine(t, "replica-b", Options{Metrics: mb, Jitter: zero})
	p.clock.Add(time.Hour)
	if n := a.Tick(context.Background()); n != 1 {
		t.Fatalf("A launched %d", n)
	}
	ctx := context.Background()
	recs, _, _ := p.runs.List(ctx, "m.a", "", nil, 10)
	if len(recs) != 1 {
		t.Fatalf("runs after A: %+v", recs)
	}
	oldID := recs[0].ID

	if n := b.Tick(ctx); n != 0 {
		t.Fatalf("B claimed a live lease: %d", n)
	}
	p.clock.Add(3 * time.Minute) // lease (2m) expired
	if n := b.Tick(ctx); n != 1 {
		t.Fatalf("B launched %d after expiry", n)
	}
	b.drain()

	recs, _, _ = p.runs.List(ctx, "m.a", "", nil, 10)
	by := map[string]models.RunRecord{}
	for _, r := range recs {
		by[r.ID] = r
	}
	if len(recs) != 2 || by[oldID].Status != models.StatusAbandoned {
		t.Fatalf("old run not abandoned: %+v", recs)
	}
	var newer models.RunRecord
	for _, r := range recs {
		if r.ID != oldID {
			newer = r
		}
	}
	if newer.Status != models.StatusSucceeded || newer.Trigger != string(jobs.TriggerRetry) || newer.Attempt != 2 || newer.Replica != "replica-b" {
		t.Fatalf("takeover run: %+v", newer)
	}
	st, _ := p.states.Get(ctx, "m.a")
	if st.Lease != nil || st.Attempt != 0 || st.RetryAt != nil || st.LastRun.RunID != newer.ID || !st.NextRunAt.After(p.clock.Now()) {
		t.Fatalf("state after takeover: %+v", st)
	}

	// A's body finally returns: its bookkeeping must not undo B's work.
	closeRelease()
	a.drain()
	got, _, _ := p.runs.List(ctx, "m.a", "", nil, 10)
	for _, r := range got {
		if r.ID == oldID && r.Status != models.StatusAbandoned {
			t.Fatalf("late finisher overwrote abandoned: %+v", r)
		}
	}
	st2, _ := p.states.Get(ctx, "m.a")
	if st2.LastRun.RunID != newer.ID || st2.Revision != st.Revision {
		t.Fatalf("late finisher wrote state: %+v", st2)
	}
	if ma.lostCount("m.a") != 1 {
		t.Fatalf("A LeaseLost = %d, want 1", ma.lostCount("m.a"))
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}
