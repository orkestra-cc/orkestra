package services

// The dispatcher's whole job is to be BOUNDED. These tests measure the
// bounds rather than asserting them in a comment: concurrency, queue
// capacity, goroutine count, enqueue latency and drain behaviour.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingSender counts how many sends are in flight at once and holds
// each one until release is closed.
type blockingSender struct {
	release  chan struct{}
	inFlight atomic.Int64
	maxSeen  atomic.Int64
	started  chan struct{}
	done     atomic.Int64
}

func newBlockingSender() *blockingSender {
	return &blockingSender{release: make(chan struct{}), started: make(chan struct{}, 1024)}
}

func (b *blockingSender) send(ctx context.Context) error {
	n := b.inFlight.Add(1)
	for {
		m := b.maxSeen.Load()
		if n <= m || b.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	b.inFlight.Add(-1)
	b.done.Add(1)
	return nil
}

func (b *blockingSender) job() MailJob {
	return MailJob{TemplateID: "auth.reset_password", Send: b.send}
}

func TestMailDispatcher_ConcurrencyIsBoundedByWorkers(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	// Enqueue more than the worker count; only MailWorkers may run.
	for i := 0; i < MailWorkers+8; i++ {
		if !d.Enqueue(sender.job()) {
			t.Fatalf("enqueue %d dropped while the queue has room", i)
		}
	}
	// Wait for the workers to pick their jobs up.
	deadline := time.After(2 * time.Second)
	for sender.inFlight.Load() < int64(MailWorkers) {
		select {
		case <-deadline:
			t.Fatalf("only %d workers started, want %d", sender.inFlight.Load(), MailWorkers)
		case <-time.After(time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond) // give a 17th worker a chance to appear
	if got := sender.maxSeen.Load(); got != int64(MailWorkers) {
		t.Fatalf("max concurrent sends = %d, want exactly %d", got, MailWorkers)
	}
}

// A full queue must DROP, immediately. A blocking enqueue would make the
// handler's latency depend on the mail backlog — and, for
// forgot-password, on whether the account existed.
func TestMailDispatcher_FullQueueDropsWithoutBlocking(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	// Fill every worker first, and wait for all of them to actually be
	// running before filling the queue itself. Each worker pops exactly
	// one job and then blocks inside Send() until sender.release closes
	// (in t.Cleanup, after this test body returns), so once all
	// MailWorkers report started, no further channel slot can ever be
	// freed for the rest of this test — without that synchronization, a
	// worker popping between "fill" and the extra enqueue below
	// nondeterministically frees a slot the extra enqueue can claim,
	// making the assertion flaky rather than a real bound.
	for i := 0; i < MailWorkers; i++ {
		if !d.Enqueue(sender.job()) {
			t.Fatalf("enqueue %d dropped while the pool still has idle workers", i)
		}
	}
	deadline := time.After(2 * time.Second)
	for sender.inFlight.Load() < int64(MailWorkers) {
		select {
		case <-deadline:
			t.Fatalf("only %d workers started, want %d", sender.inFlight.Load(), MailWorkers)
		case <-time.After(time.Millisecond):
		}
	}

	// Every worker is now permanently busy — fill every queue slot.
	accepted := 0
	for i := 0; i < MailQueueCapacity; i++ {
		if d.Enqueue(sender.job()) {
			accepted++
		}
	}
	if accepted != MailQueueCapacity {
		t.Fatalf("accepted %d, want exactly the queue capacity %d", accepted, MailQueueCapacity)
	}

	start := time.Now()
	ok := d.Enqueue(sender.job())
	elapsed := time.Since(start)
	if ok {
		t.Fatal("an enqueue past capacity must be dropped")
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("a dropped enqueue took %v — it must return at once", elapsed)
	}
	if got := d.Dropped(); got == 0 {
		t.Fatal("the drop must be counted")
	}
}

// No goroutine per request. This is the property a bare `go send(...)`
// would violate, and the one a flood would exploit.
func TestMailDispatcher_NoGoroutinePerEnqueue(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 10000; i++ {
		d.Enqueue(sender.job())
	}
	runtime.GC()
	after := runtime.NumGoroutine()
	if after-before > 2 { // slack for the runtime's own bookkeeping
		t.Fatalf("goroutines grew by %d over 10000 enqueues; the dispatcher must create none", after-before)
	}
}

// Enqueue latency must not depend on how full the queue is: that
// difference is exactly the timing oracle the detach exists to remove.
func TestMailDispatcher_EnqueueLatencyIndependentOfBacklog(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	empty := time.Now()
	d.Enqueue(sender.job())
	emptyCost := time.Since(empty)

	for i := 0; i < MailQueueCapacity-2; i++ {
		d.Enqueue(sender.job())
	}

	full := time.Now()
	d.Enqueue(sender.job())
	fullCost := time.Since(full)

	if fullCost > 10*time.Millisecond || emptyCost > 10*time.Millisecond {
		t.Fatalf("enqueue costs: empty %v, near-full %v — both must be sub-millisecond in practice", emptyCost, fullCost)
	}
}

func TestMailDispatcher_StopDrainsQueuedJobs(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()

	var sent atomic.Int64
	for i := 0; i < 32; i++ {
		d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
			sent.Add(1)
			return nil
		}})
	}
	d.Stop(context.Background())
	if got := sent.Load(); got != 32 {
		t.Fatalf("sent %d of 32 queued jobs; Stop must drain", got)
	}
}

func TestMailDispatcher_StopAbandonsAfterTimeout(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()

	for i := 0; i < MailWorkers*2; i++ {
		d.Enqueue(sender.job())
	}
	// Wait for the workers to be busy, then Stop with a context that is
	// already done so the drain gives up at once.
	<-sender.started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	d.Stop(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop took %v with a cancelled context; it must abandon", elapsed)
	}
	close(sender.release)
}

// A send that panics must not take a worker — or the process — with it.
func TestMailDispatcher_RecoversFromPanickingSend(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()

	var after atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		panic("sender exploded")
	}})
	d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		after.Add(1)
		wg.Done()
		return nil
	}})
	wg.Wait()
	d.Stop(context.Background())
	if after.Load() != 1 {
		t.Fatal("a panicking job must not stop the pool serving the next one")
	}
}

// A job whose Send returns an error is a lost mail, logged, not a crash.
func TestMailDispatcher_SendErrorIsSwallowed(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	var wg sync.WaitGroup
	wg.Add(1)
	d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		defer wg.Done()
		return errors.New("smtp down")
	}})
	wg.Wait()
	d.Stop(context.Background())
}

// Enqueue on a dispatcher that was never started, or already stopped,
// must not panic on a closed channel — a module toggled off mid-request
// is an ordinary state.
func TestMailDispatcher_EnqueueAfterStopIsSafe(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	d.Stop(context.Background())
	if d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error { return nil }}) {
		t.Fatal("Enqueue after Stop must report the job as not accepted")
	}

	var never *MailDispatcher
	if never.Enqueue(MailJob{}) {
		t.Fatal("Enqueue on a nil dispatcher must be a safe no-op")
	}
}

// A concurrent Stop must never let a racing Enqueue panic on a closed
// channel. TestMailDispatcher_EnqueueAfterStopIsSafe above only proves
// the SEQUENTIAL order (Stop returns, then Enqueue runs); this proves
// the INTERLEAVING is safe too — Enqueue's "not stopped" check and its
// send onto the channel must be one atomic section, not a check that
// can go stale before the send runs. `go test -race` cannot catch a
// regression here on its own: channel operations are self-synchronizing,
// so an Enqueue racing a Stop's close is a logic race, not a data race,
// and the detector has nothing to flag. Only a crash proves the bug is
// gone, which is why this hammers the interleaving across many
// iterations instead of asserting anything about which calls succeeded.
func TestMailDispatcher_ConcurrentStopDoesNotPanicEnqueue(t *testing.T) {
	const iterations = 200
	const enqueuers = 20
	const perEnqueuer = 25

	for iter := 0; iter < iterations; iter++ {
		// slog.DiscardHandler: this test's concern is the panic, not
		// the drop log — TestMailDispatcher_DropWarningIsRateLimited
		// covers that separately, and 200 fresh dispatcher instances
		// each logging their first drop would otherwise flood the
		// test output.
		d := NewMailDispatcher(slog.New(slog.DiscardHandler))
		d.Start()

		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < enqueuers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for j := 0; j < perEnqueuer; j++ {
					d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error { return nil }})
				}
			}()
		}
		// Release every enqueuer at once so they race Stop below,
		// rather than serialising ahead of it by construction.
		close(start)
		d.Stop(context.Background())
		wg.Wait()
	}
}

// A Stop followed by a Start must resume REAL delivery — not merely
// stop panicking while silently accepting and dropping everything
// after. This is the module's own documented lifecycle invariant
// (module.go: "so Start is idempotent, a stopped module can start
// again, and no second ticker survives a hot enable/disable cycle")
// that the registry's StartModule/StopModule hot toggle
// (pkg/sdk/module/registry.go) relies on for every module, mail
// dispatcher included.
func TestMailDispatcher_RestartAfterStopResumesDelivery(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()

	var firstGen atomic.Int64
	var wg1 sync.WaitGroup
	wg1.Add(1)
	if !d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		firstGen.Add(1)
		wg1.Done()
		return nil
	}}) {
		t.Fatal("first-generation enqueue must be accepted")
	}
	wg1.Wait()

	d.Stop(context.Background())

	// While stopped, Enqueue must be rejected — not silently accepted
	// into a channel nothing will ever drain.
	if d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error { return nil }}) {
		t.Fatal("enqueue while stopped must be rejected")
	}

	d.Start()

	var secondGen atomic.Int64
	var wg2 sync.WaitGroup
	wg2.Add(1)
	if !d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		secondGen.Add(1)
		wg2.Done()
		return nil
	}}) {
		t.Fatal("enqueue after restart must be accepted")
	}
	wg2.Wait()
	d.Stop(context.Background())

	if firstGen.Load() != 1 || secondGen.Load() != 1 {
		t.Fatalf("first-generation sent=%d, second-generation sent=%d; both must be 1 — a restart must deliver, not just accept",
			firstGen.Load(), secondGen.Load())
	}
}

// The drop WARN is throttled to at most one per mailDropWarningInterval
// (1 minute) — mirroring the report()/attemptWarningInterval shape
// attempt_counter.go already ships in this package, and the
// session_revocation_service_test.go rate-limit-assertion pattern
// (bytes.Buffer + a text handler + counting "level=WARN" occurrences).
// Every drop still increments Dropped(); only the log line is
// rate-limited, so a flood of drops — the exact scenario the
// queue-full path exists to survive — produces one WARN, not one per
// dropped request.
func TestMailDispatcher_DropWarningIsRateLimited(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// Deliberately never started: every Enqueue takes the
	// "dispatcher_stopped" branch of recordDrop, which shares the same
	// throttle state as the "queue_full" branch exercised elsewhere
	// (TestMailDispatcher_NoGoroutinePerEnqueue's ~9,700 drops collapse
	// to the same one line).
	d := NewMailDispatcher(logger)

	const drops = 5
	for i := 0; i < drops; i++ {
		if d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error { return nil }}) {
			t.Fatal("enqueue on a never-started dispatcher must be rejected")
		}
	}
	if got := d.Dropped(); got != drops {
		t.Fatalf("dropped count = %d, want %d — every drop must be counted regardless of log throttling", got, drops)
	}
	if got := strings.Count(logs.String(), "level=WARN"); got != 1 {
		t.Errorf("warning count = %d, want 1 within the rate-limit window", got)
	}
}
