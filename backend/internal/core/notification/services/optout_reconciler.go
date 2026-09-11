package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ReconcileMaxAttempts is the retry budget. Past it a row is dead-lettered
// rather than retried: a downstream consumer that has refused the same opt-out
// eight times over a growing backoff is not going to accept the ninth, and a
// row nobody ever stops retrying is a background job that never drains.
const ReconcileMaxAttempts = 8

const (
	// defaultReconcileInterval is how often the ticker scans. It matches the
	// first backoff step: scanning faster than the shortest delay only finds
	// rows that are not due yet.
	defaultReconcileInterval = time.Minute
	// defaultReconcileBatch bounds one pass.
	defaultReconcileBatch = 100
	// reconcileBackoffBase is the delay after the first failure; it doubles
	// with every attempt up to reconcileBackoffMax. Over the eight-attempt
	// budget that is roughly two hours of retrying before a row is given up
	// on — long enough to ride out a consumer's deploy, short enough that an
	// operator sees the dead letter the same day.
	reconcileBackoffBase = time.Minute
	reconcileBackoffMax  = 2 * time.Hour
)

// ReconcileTokenStore is the slice of the unsubscribe repository the
// reconciler touches. Narrow on purpose: this job replays side effects, it
// never issues or claims a token. repository.UnsubscribeRepository satisfies
// it.
type ReconcileTokenStore interface {
	ListPending(ctx context.Context, now time.Time, limit int) ([]models.UnsubscribeTokenDoc, error)
	ClearSinkPending(ctx context.Context, hash string) error
	ClearPrefPending(ctx context.Context, hash string) error
	RecordFailedAttempt(ctx context.Context, hash string, retryAt time.Time) error
	MarkDeadLettered(ctx context.Context, hash string, now time.Time) error
}

// ReconcileStats is what one pass did. Retried and DeadLettered are the two
// an operator watches: the first is a consumer having a bad minute, the second
// is consent that core recorded and could not mirror.
type ReconcileStats struct {
	Scanned      int
	Preferences  int
	Sinks        int
	Retried      int
	DeadLettered int
}

// OptoutReconcilerDeps wires the reconciler.
type OptoutReconcilerDeps struct {
	// Tokens is the pending-work scan and the flag writes.
	Tokens ReconcileTokenStore
	// Prefs writes the recipient's preference row. Nil is treated as work
	// left undone, not as work that does not exist.
	Prefs PreferenceService
	// Sink mirrors the opt-out downstream. Nil means no consumer is
	// registered — the base ships none — so there is nothing to mirror and
	// the flag comes down on the first pass. In the module this is
	// FireMarketingUnsubscribe, which already folds a nil registered sink and
	// a panicking one into that same contract.
	Sink iface.MarketingUnsubscribeSink
	// Now makes the backoff assertable without sleeping. Defaults to time.Now.
	Now      func() time.Time
	Logger   *slog.Logger
	Interval time.Duration
	// BatchSize bounds one pass.
	BatchSize int
}

// OptoutReconciler replays the unsubscribe side effects a live request could
// not complete.
//
// Consume records the durable opt-out first and only then claims the token,
// raising sinkPending / prefPending in the same write as the claim. Everything
// after that point — the preference row, the downstream sink — may fail, or
// may never run because the process died, without failing the request. This
// job is what makes those marks mean something.
//
// Two rules shape every branch below:
//
//   - Every row the scan returns makes progress (a flag comes down) or moves
//     toward the budget (attempts grows). A row that could come back and be
//     left exactly as it was is an infinite loop that costs a downstream
//     system one call per tick.
//   - A flag comes down only when the thing it marks actually succeeded.
//     Clearing one otherwise is consent that core recorded and silently
//     dropped. The two flags are independent: a written preference says
//     nothing about the sink.
type OptoutReconciler struct {
	tokens    ReconcileTokenStore
	prefs     PreferenceService
	sink      iface.MarketingUnsubscribeSink
	now       func() time.Time
	logger    *slog.Logger
	interval  time.Duration
	batchSize int

	mu      sync.Mutex
	stopCh  chan struct{}
	done    chan struct{}
	stopped bool
	running bool
}

func NewOptoutReconciler(deps OptoutReconcilerDeps) *OptoutReconciler {
	r := &OptoutReconciler{
		tokens:    deps.Tokens,
		prefs:     deps.Prefs,
		sink:      deps.Sink,
		now:       deps.Now,
		logger:    deps.Logger,
		interval:  deps.Interval,
		batchSize: deps.BatchSize,
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.logger == nil {
		r.logger = slog.Default()
	}
	if r.interval <= 0 {
		r.interval = defaultReconcileInterval
	}
	if r.batchSize <= 0 {
		r.batchSize = defaultReconcileBatch
	}
	return r
}

// RunOnce replays one bounded batch. It returns an error only when the scan
// itself failed — a row that could not be replayed is that row's problem, is
// counted against its own budget, and must not abort the pass for the rest.
func (r *OptoutReconciler) RunOnce(ctx context.Context) (ReconcileStats, error) {
	var stats ReconcileStats
	if r.tokens == nil {
		// Boot-time misconfiguration. Reported rather than swallowed: a
		// reconciler that quietly does nothing looks exactly like one with
		// nothing to do.
		return stats, errors.New("notification: unsubscribe reconciler has no token store")
	}
	now := r.now()
	rows, err := r.tokens.ListPending(ctx, now, r.batchSize)
	if err != nil {
		return stats, err
	}
	for i := range rows {
		stats.Scanned++
		r.replay(ctx, &rows[i], now, &stats)
	}
	return stats, nil
}

// replay finishes one row, or moves it toward its budget.
func (r *OptoutReconciler) replay(ctx context.Context, doc *models.UnsubscribeTokenDoc, now time.Time, stats *ReconcileStats) {
	if !doc.PrefPending && !doc.SinkPending {
		// The scan filters these out, so reaching here means the query and
		// this loop disagree. Reported rather than dead-lettered, and checked
		// FIRST so a finished row can never be stamped with a terminal state
		// by the branch below: the row owes nothing. It costs no downstream
		// call either, so it is not the retry loop the two rules guard
		// against.
		r.logger.Warn("notification: the pending scan returned a row with nothing pending",
			slog.String("tokenUuid", doc.UUID))
		return
	}
	if strings.TrimSpace(doc.Address) == "" {
		// A claimed token with no address: Consume refuses to claim one, so
		// this row is malformed. There is no address to mirror and no number
		// of retries that would produce one — ending it is honest, and it
		// stops the scan returning it for ever.
		r.deadLetter(ctx, doc, now, stats, "the token carries no address")
		return
	}
	category := doc.Category
	if category == "" {
		category = models.TypeMarketing // empty on the token = all marketing
	}

	failed := false
	if doc.PrefPending {
		failed = !r.replayPreference(ctx, doc, category, stats)
	}
	if doc.SinkPending {
		// Deliberately not an else-if, and not short-circuited by the
		// preference above: a row can owe both, and the sink's outcome is
		// independent of the preference's.
		if !r.replaySink(ctx, doc, category, stats) {
			failed = true
		}
	}
	if !failed {
		return
	}
	r.recordFailure(ctx, doc, now, stats)
}

// replayPreference returns true when the row no longer owes a preference.
func (r *OptoutReconciler) replayPreference(ctx context.Context, doc *models.UnsubscribeTokenDoc, category string, stats *ReconcileStats) bool {
	if doc.UserUUID == "" {
		// The claim raises prefPending only for a token that names a user, so
		// this row is malformed too — but unlike a missing address it costs
		// nothing to resolve: there is no preference row to write. Lowering
		// the flag is the only outcome that does not loop for ever.
		r.logger.Warn("notification: unsubscribe row marked for a preference it has no user for",
			slog.String("tokenUuid", doc.UUID))
		return r.clear(ctx, r.tokens.ClearPrefPending, doc, "prefPending")
	}
	if r.prefs == nil {
		// Work left undone, not work that cannot exist. It consumes the
		// budget and ends in a dead letter, which is visible; clearing the
		// flag would drop it silently.
		r.logger.Warn("notification: unsubscribe preference not replayed, preference service not wired",
			slog.String("tokenUuid", doc.UUID))
		return false
	}
	if err := r.prefs.Set(ctx, doc.UserUUID, category, models.ChannelEmail, false); err != nil {
		r.logger.Warn("notification: replaying the unsubscribe preference failed",
			slog.String("tokenUuid", doc.UUID),
			slog.String("error", scrubAddress(err.Error(), doc.Address)))
		return false
	}
	if !r.clear(ctx, r.tokens.ClearPrefPending, doc, "prefPending") {
		return false
	}
	stats.Preferences++
	return true
}

// replaySink returns true when the row no longer owes the sink.
func (r *OptoutReconciler) replaySink(ctx context.Context, doc *models.UnsubscribeTokenDoc, category string, stats *ReconcileStats) bool {
	if r.sink == nil {
		// No consumer is registered, which is the ordinary case for this
		// base. There is nothing to mirror, so the row is finished — the same
		// ruling Consume makes on its own nil sink, and the reason an install
		// with no sink does not accumulate pending rows.
		if !r.clear(ctx, r.tokens.ClearSinkPending, doc, "sinkPending") {
			return false
		}
		stats.Sinks++
		return true
	}
	if err := r.sink.OnMarketingUnsubscribe(ctx, doc.Address, category, doc.Context); err != nil {
		// Scrubbed: core handed this sink the address, and a sink quoting it
		// back in its error is the ordinary case, not an exotic one.
		r.logger.Warn("notification: replaying the unsubscribe sink failed",
			slog.String("tokenUuid", doc.UUID),
			slog.String("error", scrubAddress(err.Error(), doc.Address)))
		return false
	}
	if !r.clear(ctx, r.tokens.ClearSinkPending, doc, "sinkPending") {
		return false
	}
	stats.Sinks++
	return true
}

// clear lowers one flag and reports whether it is really down. A clear that
// failed is NOT progress: the row comes back on the next scan and the work is
// replayed, so the attempt has to count — otherwise the downstream system
// takes one call per tick until someone notices.
func (r *OptoutReconciler) clear(ctx context.Context, clear func(context.Context, string) error, doc *models.UnsubscribeTokenDoc, flag string) bool {
	if err := clear(ctx, doc.TokenHash); err != nil {
		// Scrubbed like every other error here: a MongoDB write error quotes
		// the document it failed on, and this document carries the address.
		r.logger.Warn("notification: could not lower a pending flag after its work succeeded",
			slog.String("tokenUuid", doc.UUID), slog.String("flag", flag),
			slog.String("error", scrubAddress(err.Error(), doc.Address)))
		return false
	}
	return true
}

// recordFailure moves a row that could not be finished toward its budget, or
// ends it. Every failing outcome comes through here.
func (r *OptoutReconciler) recordFailure(ctx context.Context, doc *models.UnsubscribeTokenDoc, now time.Time, stats *ReconcileStats) {
	attempts := doc.Attempts + 1
	if attempts >= ReconcileMaxAttempts {
		r.deadLetter(ctx, doc, now, stats, "retry budget exhausted")
		return
	}
	if err := r.tokens.RecordFailedAttempt(ctx, doc.TokenHash, now.Add(reconcileBackoff(attempts))); err != nil {
		// The row was NOT deferred — the write that would have done it
		// failed — so it must not count as a retry: the pass log is the only
		// operator-visible signal for what a pass actually did.
		r.logger.Warn("notification: could not record a failed unsubscribe replay",
			slog.String("tokenUuid", doc.UUID),
			slog.String("error", scrubAddress(err.Error(), doc.Address)))
		return
	}
	stats.Retried++
}

// deadLetter gives up on a row: the flags come down and deadLetteredAt goes
// on, so the scan never returns it again. Logged at error because this is
// consent core recorded and could not mirror — the one outcome of this job
// that needs a human.
func (r *OptoutReconciler) deadLetter(ctx context.Context, doc *models.UnsubscribeTokenDoc, now time.Time, stats *ReconcileStats, reason string) {
	r.logger.Error("notification: giving up on replaying an unsubscribe, dead-lettered",
		slog.String("tokenUuid", doc.UUID),
		slog.String("reason", reason),
		slog.Int("attempts", doc.Attempts+1),
		slog.Bool("sinkPending", doc.SinkPending),
		slog.Bool("prefPending", doc.PrefPending))
	if err := r.tokens.MarkDeadLettered(ctx, doc.TokenHash, now); err != nil {
		// The row is still pending, so the scan will return it again. Defer
		// it the ordinary way rather than leaving it due: a row that cannot
		// be closed must not cost the sink a call on every tick.
		r.logger.Warn("notification: could not dead-letter an unsubscribe row",
			slog.String("tokenUuid", doc.UUID),
			slog.String("error", scrubAddress(err.Error(), doc.Address)))
		if err := r.tokens.RecordFailedAttempt(ctx, doc.TokenHash, now.Add(reconcileBackoff(doc.Attempts+1))); err != nil {
			// Neither write landed: the row is untouched, so nothing here
			// counts as progress.
			r.logger.Warn("notification: could not defer an unsubscribe row either",
				slog.String("tokenUuid", doc.UUID),
				slog.String("error", scrubAddress(err.Error(), doc.Address)))
			return
		}
		// The row could not be closed but was at least deferred — that is
		// the same outcome recordFailure's ordinary path counts, so it is
		// counted the same way; the pass log is the only operator-visible
		// signal for what actually happened to a dead-letter candidate.
		stats.Retried++
		return
	}
	stats.DeadLettered++
}

// reconcileBackoff is how long a row waits before its next attempt: the base
// doubled once per attempt, capped. The cap and the shift guard are not
// decoration — a corrupt attempts counter would otherwise shift the base into
// a negative duration, and a negative delay is a row that is always due.
func reconcileBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 16 {
		return reconcileBackoffMax
	}
	d := reconcileBackoffBase << (attempts - 1)
	if d <= 0 || d > reconcileBackoffMax {
		return reconcileBackoffMax
	}
	return d
}

// Start launches the ticker. Calling it twice while a loop is genuinely
// active is a no-op: a second loop would double every downstream call.
//
// A Start that lands in the narrow window between a Stop closing its stop
// channel and that generation's loop goroutine actually exiting waits for
// the previous generation to finish — via its `done` channel — rather than
// reading its still-`true` `running` flag as "already started" and silently
// declining to launch a new one.
func (r *OptoutReconciler) Start(ctx context.Context) {
	for {
		r.mu.Lock()
		if r.running {
			if !r.stopped {
				// A loop is genuinely active; Start is idempotent.
				r.mu.Unlock()
				return
			}
			prevDone := r.done
			r.mu.Unlock()
			if prevDone != nil {
				<-prevDone
			}
			continue
		}
		stop := make(chan struct{})
		done := make(chan struct{})
		r.stopCh, r.done, r.stopped, r.running = stop, done, false, true
		r.mu.Unlock()
		go r.loop(ctx, stop, done)
		return
	}
}

// Stop halts the ticker. It is idempotent and safe before Start — the module
// can be disabled at runtime, and a Stop that arrived first must not leave a
// panic or a reconciler that can never be started again.
//
// It deliberately does NOT clear the running state; the loop's own defer does
// that. Stop runs on whichever goroutine called it, so a Stop that reset the
// state itself would be undoing bookkeeping the loop has not finished with —
// and one arriving before the goroutine is scheduled would mark a running
// loop as stopped, after which Start would refuse to start a second one and
// the reconciler would be dead for the rest of the process.
func (r *OptoutReconciler) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopCh == nil || r.stopped {
		return
	}
	r.stopped = true
	close(r.stopCh)
}

func (r *OptoutReconciler) loop(ctx context.Context, stop <-chan struct{}, done chan struct{}) {
	// The running state is cleared here, when the goroutine is actually gone,
	// so a later Start knows it is free to launch one. `done` is closed last,
	// after the fields are reset, so a Start blocked on it always sees a
	// clean slate the moment it wakes up.
	defer func() {
		r.mu.Lock()
		r.running, r.stopCh, r.stopped, r.done = false, nil, false, nil
		r.mu.Unlock()
		close(done)
	}()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Background context: the host's ctx cancels the loop above, but
			// a pass already under way finishes its writes rather than
			// leaving a flag half-lowered.
			stats, err := r.RunOnce(context.Background())
			if err != nil {
				r.logger.Warn("notification: unsubscribe reconciler pass failed",
					slog.String("error", err.Error()))
				continue
			}
			if stats.Scanned > 0 {
				r.logger.Info("notification: unsubscribe reconciler pass",
					slog.Int("scanned", stats.Scanned),
					slog.Int("preferences", stats.Preferences),
					slog.Int("sinks", stats.Sinks),
					slog.Int("retried", stats.Retried),
					slog.Int("deadLettered", stats.DeadLettered))
			}
		}
	}
}

// isRunning reports whether the loop goroutine is alive. Test seam: the
// lifecycle is the one thing here that cannot be asserted off the injected
// clock.
func (r *OptoutReconciler) isRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
