package services

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
)

// ErasureStore is the finalization state of retrieval_erasures
// (*repository.ErasureFinalization): idempotent tombstone creation, the claim
// and the claim-fenced transitions (ADR-0023 D12).
type ErasureStore interface {
	EnsureUser(ctx context.Context, userUUID string, now time.Time) (*models.Erasure, bool, error)
	EnsureSubject(ctx context.Context, tenantID, subject string, now time.Time) (*models.Erasure, bool, error)
	EnsureTenant(ctx context.Context, tenantID string, now time.Time) (*models.Erasure, bool, error)
	ListUser(ctx context.Context, userUUID string) ([]models.Erasure, error)
	Get(ctx context.Context, uuid string) (*models.Erasure, error)
	ClaimDue(ctx context.Context, now time.Time, hold time.Duration) (*models.Erasure, error)
	ClaimOne(ctx context.Context, uuid string, now time.Time, hold time.Duration) (*models.Erasure, error)
	MarkFencing(ctx context.Context, uuid string, claim, firstPassAt, fenceUntil time.Time) error
	MarkDone(ctx context.Context, uuid string, claim, at time.Time) error
	MarkRetry(ctx context.Context, uuid string, claim, next time.Time) error
	TenantTombstoneIDs(ctx context.Context, tenantIDs []string) (map[string]bool, error)
}

// SearchLogDSRRepo is the data-subject and tenant-purge view of the search
// log (*repository.SearchLogDSR).
type SearchLogDSRRepo interface {
	ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.SearchLog, error)
	DeleteByUserAllTenants(ctx context.Context, userUUID string) (int64, error)
	DeleteBySubject(ctx context.Context, tenantID, subject string) (int64, error)
	DeleteByTenant(ctx context.Context, tenantID string) (int64, error)
}

var (
	_ ErasureStore     = (*repository.ErasureFinalization)(nil)
	_ SearchLogDSRRepo = (*repository.SearchLogDSR)(nil)
)

// CollectionLister names the engine collections an erasure must reach.
type CollectionLister interface {
	// LiveCollections maps every corpus in corpus state to the collection of its
	// active generation (delivery step 1 records no other); a corpus with none
	// is absent.
	LiveCollections(ctx context.Context) (map[string][]string, error)
}

// FinalizerConfig is read on every sweep; zero values take the defaults.
type FinalizerConfig struct {
	Lease, WriteTimeout time.Duration // inputs of FencingWindow; MaxWorkerLease, 30s
	Interval            time.Duration // 15s
	Batch               int           // 50
	Hold                time.Duration // 2m
}

const (
	defaultFinalizerInterval = 15 * time.Second
	defaultFinalizerBatch    = 50
	defaultFinalizerHold     = 2 * time.Minute

	fenceDefaultLease = 2 * time.Minute
	fenceDefaultWrite = 30 * time.Second
	fenceMargin       = 30 * time.Second
	minFence          = time.Minute
	maxFence          = time.Hour

	erasureBackoffBase    = 15 * time.Second
	erasureBackoffMax     = 15 * time.Minute
	maxErasureBackoffStep = 10 // 15s<<10 is beyond the cap; bounds the shift

	// The pass runs under hold - passMargin and the transition under
	// markTimeout, so one step ends before its claim can be taken over.
	passMargin  = 10 * time.Second
	markTimeout = 10 * time.Second
)

// withDefaults fills the zero fields. A missing fence input fails safe: the
// lease defaults to MaxWorkerLease, the longest a worker can hold a key, not
// to FencingWindow's own 2m fallback.
func (c FinalizerConfig) withDefaults() FinalizerConfig {
	if c.Lease <= 0 {
		c.Lease = MaxWorkerLease
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = fenceDefaultWrite
	}
	if c.Interval <= 0 {
		c.Interval = defaultFinalizerInterval
	}
	if c.Batch <= 0 {
		c.Batch = defaultFinalizerBatch
	}
	if c.Hold <= 0 {
		c.Hold = defaultFinalizerHold
	}
	return c
}

// FencingWindow is how long after the first pass a worker that claimed before
// the tombstone may still write (ADR-0023 D12): lease + writeTimeout + 30s,
// clamped to [1m, 1h]. Non-positive inputs take 2m and 30s.
func FencingWindow(lease, writeTimeout time.Duration) time.Duration {
	if lease <= 0 {
		lease = fenceDefaultLease
	}
	if writeTimeout <= 0 {
		writeTimeout = fenceDefaultWrite
	}
	return min(max(lease+writeTimeout+fenceMargin, minFence), maxFence)
}

// ErasureBackoff is the delay after failed attempt number attempts (< 1 reads
// as 1): 15s·2^(attempts−1), capped at 15m.
func ErasureBackoff(attempts int) time.Duration {
	shift := min(max(attempts-1, 0), maxErasureBackoffStep)
	return min(erasureBackoffBase<<shift, erasureBackoffMax)
}

var (
	errMalformedErasure    = errors.New("retrieval: malformed erasure row")
	errEngineNotConfigured = errors.New("retrieval: engine not configured")
	errEraseNotConfirmed   = errors.New("retrieval: points remain after the delete")
)

const userSubjectPrefix = "user:"

// SelectorFor is the engine selector of a tombstone: a user reaches every
// tenant, the other kinds one tenant. A row missing a field its kind needs is
// refused, so it can never widen into a broader delete.
func SelectorFor(e models.Erasure) (engine.Selector, error) {
	switch e.Kind {
	case models.ErasureKindUser:
		if !strings.HasPrefix(e.Subject, userSubjectPrefix) || len(e.Subject) == len(userSubjectPrefix) {
			return engine.Selector{}, errMalformedErasure
		}
		return engine.Selector{Subject: e.Subject}, nil
	case models.ErasureKindSubject:
		if e.TenantID == "" || e.Subject == "" {
			return engine.Selector{}, errMalformedErasure
		}
		return engine.Selector{TenantID: e.TenantID, Subject: e.Subject}, nil
	case models.ErasureKindItem:
		if e.TenantID == "" || e.Corpus == "" || e.SourceID == "" {
			return engine.Selector{}, errMalformedErasure
		}
		return engine.Selector{TenantID: e.TenantID, SourceIDs: []string{e.SourceID}}, nil
	case models.ErasureKindTenant:
		if e.TenantID == "" {
			return engine.Selector{}, errMalformedErasure
		}
		return engine.Selector{TenantID: e.TenantID}, nil
	}
	return engine.Selector{}, errMalformedErasure
}

// Finalizer drives tombstones pending → fencing → done (ADR-0023 D12). A pass
// deletes the tombstone's search-log rows, then its points in every live
// collection, and confirms by count; the first confirmed pass starts the
// fence, a confirmed pass after it closes the row. A failed pass is retried
// with ErasureBackoff. Every transition is fenced on the claim, so replicas
// can sweep concurrently.
type Finalizer struct {
	store  ErasureStore
	logs   SearchLogDSRRepo
	cols   CollectionLister
	eng    engine.Engine // nil: engine not configured
	cfg    func() FinalizerConfig
	audit  *AuditRelay
	now    func() time.Time
	logger *slog.Logger
}

// NewFinalizer builds the sweep; eng nil means the engine is not configured.
func NewFinalizer(store ErasureStore, logs SearchLogDSRRepo, cols CollectionLister, eng engine.Engine,
	cfg func() FinalizerConfig, audit *AuditRelay, now func() time.Time, logger *slog.Logger) *Finalizer {
	if cfg == nil {
		cfg = func() FinalizerConfig { return FinalizerConfig{} }
	}
	if audit == nil {
		audit = NewAuditRelay()
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Finalizer{store: store, logs: logs, cols: cols, eng: eng, cfg: cfg, audit: audit, now: now, logger: logger}
}

func (f *Finalizer) config() FinalizerConfig { return f.cfg().withDefaults() }

// RunOnce claims and steps up to Batch due rows and returns how many it took.
// Only a claim failure is returned: a failed pass is recorded on its row.
func (f *Finalizer) RunOnce(ctx context.Context) (int, error) {
	cfg := f.config()
	n := 0
	for n < cfg.Batch {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		e, err := f.store.ClaimDue(ctx, f.now(), cfg.Hold)
		if err != nil {
			return n, err
		}
		if e == nil {
			return n, nil
		}
		n++
		if err := f.step(ctx, cfg, e); err != nil {
			f.logger.WarnContext(ctx, "retrieval: erasure finalization could not record its outcome",
				slog.String("erasure", e.UUID), slog.String("kind", e.Kind), slog.String("error", err.Error()))
		}
	}
	return n, nil
}

// Attempt runs one step on the row when it is due and returns the row after
// it (nil when absent). A row that is not due is returned unchanged.
func (f *Finalizer) Attempt(ctx context.Context, uuid string) (*models.Erasure, error) {
	cfg := f.config()
	e, err := f.store.ClaimOne(ctx, uuid, f.now(), cfg.Hold)
	if err != nil {
		return nil, err
	}
	if e != nil {
		if err := f.step(ctx, cfg, e); err != nil {
			return nil, err
		}
	}
	return f.store.Get(ctx, uuid)
}

// Run sweeps every Interval (first sweep one Interval after the call) until
// ctx is done.
func (f *Finalizer) Run(ctx context.Context) {
	t := time.NewTimer(f.config().Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := f.RunOnce(ctx); err != nil && ctx.Err() == nil {
				f.logger.WarnContext(ctx, "retrieval: erasure finalization sweep failed", slog.String("error", err.Error()))
			}
			t.Reset(f.config().Interval)
		}
	}
}

// step runs one pass on a claimed row and records its outcome. The returned
// error is a failure to record it; a lost claim is not one.
func (f *Finalizer) step(ctx context.Context, cfg FinalizerConfig, e *models.Erasure) error {
	if e.NextAttemptAt == nil {
		return errors.New("retrieval: claimed erasure carries no claim")
	}
	claim := *e.NextAttemptAt
	status := e.EffectiveStatus()
	if status == models.ErasureStatusDone {
		return nil
	}
	if status == models.ErasureStatusFencing && e.FenceUntil != nil && f.now().Before(*e.FenceUntil) {
		return f.refence(ctx, e, claim)
	}

	pctx, cancel := context.WithTimeout(ctx, passTimeout(cfg.Hold))
	targets, stage, err := f.pass(pctx, e)
	cancel()

	// The outcome is recorded even when ctx ended during the pass.
	mctx, mcancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer mcancel()
	now := f.now()
	if err != nil {
		f.warnFailed(ctx, e, stage, len(targets), err)
		return f.settle(ctx, e, f.store.MarkRetry(mctx, e.UUID, claim, now.Add(ErasureBackoff(e.Attempts+1))))
	}
	if status == models.ErasureStatusPending && f.eng != nil {
		fence := FencingWindow(cfg.Lease, cfg.WriteTimeout)
		return f.settle(ctx, e, f.store.MarkFencing(mctx, e.UUID, claim, now, now.Add(fence)))
	}
	// A confirmed pass after the fence, or no engine and no collection, so
	// nothing can have been written.
	if err := f.store.MarkDone(mctx, e.UUID, claim, now); err != nil {
		return f.settle(ctx, e, err)
	}
	f.audit.EmitSystem(mctx, e.TenantID, AuditErasureFinalized, "retrieval_erasure", e.UUID, map[string]any{
		"kind": e.Kind, "scope": e.Scope, "attempts": e.Attempts, "collections": len(targets),
	})
	return nil
}

// refence puts back a fencing row claimed before its fence ended (a clock
// behind the one that wrote fenceUntil): no pass runs and nothing is marked
// done; the row is next due at its own fenceUntil. MarkFencing with the row's
// own firstPassAt and fenceUntil reschedules without counting an attempt,
// which MarkRetry always does.
func (f *Finalizer) refence(ctx context.Context, e *models.Erasure, claim time.Time) error {
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()
	firstPass := *e.FenceUntil
	if e.FirstPassAt != nil {
		firstPass = *e.FirstPassAt
	}
	return f.settle(ctx, e, f.store.MarkFencing(mctx, e.UUID, claim, firstPass, *e.FenceUntil))
}

// pass is one finalization pass: the search-log delete (stage 0, every pass),
// then delete and count in every target collection. It returns the targets
// and, on failure, the stage that failed.
func (f *Finalizer) pass(ctx context.Context, e *models.Erasure) ([]string, string, error) {
	sel, err := SelectorFor(*e)
	if err != nil {
		return nil, "selector", err
	}
	if err := f.deleteSearchLog(ctx, e); err != nil {
		return nil, "search_log", err
	}
	targets, err := f.targets(ctx, e)
	if err != nil {
		return nil, "collections", err
	}
	if f.eng == nil {
		if len(targets) > 0 {
			return targets, "engine", errEngineNotConfigured
		}
		return targets, "", nil
	}
	for _, c := range targets {
		if err := f.eng.DeleteByFilter(ctx, c, sel); errors.Is(err, engine.ErrNotFound) {
			continue // a dropped generation holds nothing
		} else if err != nil {
			return targets, "engine", err
		}
		n, err := f.eng.Count(ctx, c, sel)
		if errors.Is(err, engine.ErrNotFound) {
			continue
		}
		if err != nil {
			return targets, "engine", err
		}
		if n > 0 {
			return targets, "engine", errEraseNotConfirmed
		}
	}
	return targets, "", nil
}

// deleteSearchLog deletes the search-log rows the tombstone covers by kind;
// an item tombstone deletes none.
func (f *Finalizer) deleteSearchLog(ctx context.Context, e *models.Erasure) error {
	var err error
	switch e.Kind {
	case models.ErasureKindUser:
		_, err = f.logs.DeleteByUserAllTenants(ctx, strings.TrimPrefix(e.Subject, userSubjectPrefix))
	case models.ErasureKindSubject:
		_, err = f.logs.DeleteBySubject(ctx, e.TenantID, e.Subject)
	case models.ErasureKindTenant:
		_, err = f.logs.DeleteByTenant(ctx, e.TenantID)
	}
	return err
}

// targets lists the collections of the item's corpus, or of every corpus for
// the other kinds, deduplicated and sorted.
func (f *Finalizer) targets(ctx context.Context, e *models.Erasure) ([]string, error) {
	live, err := f.cols.LiveCollections(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	if e.Kind == models.ErasureKindItem {
		out = slices.Clone(live[e.Corpus])
	} else {
		for _, cs := range live {
			out = append(out, cs...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// warnFailed logs a failed pass with the row's uuid and kind, never its
// subject; error text is logged only for the collection listing, whose errors
// cannot carry a selector value.
func (f *Finalizer) warnFailed(ctx context.Context, e *models.Erasure, stage string, collections int, err error) {
	if errors.Is(err, errEngineNotConfigured) {
		f.logger.WarnContext(ctx, "retrieval: engine not configured but corpus collections recorded; the erasure stays pending",
			slog.String("erasure", e.UUID), slog.String("kind", e.Kind), slog.Int("collections", collections))
		return
	}
	attrs := []any{slog.String("erasure", e.UUID), slog.String("kind", e.Kind), slog.String("stage", stage),
		slog.String("reason", failureReason(err))}
	if stage == "collections" {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	f.logger.WarnContext(ctx, "retrieval: erasure finalization pass failed; retrying with backoff", attrs...)
}

func failureReason(err error) string {
	switch {
	case errors.Is(err, errMalformedErasure):
		return "malformed"
	case errors.Is(err, errEraseNotConfirmed):
		return "not_confirmed"
	case errors.Is(err, engine.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "error"
}

// settle turns a lost claim into success: another sweep owns the row now.
func (f *Finalizer) settle(ctx context.Context, e *models.Erasure, err error) error {
	if errors.Is(err, repository.ErrErasureClaimLost) {
		f.logger.DebugContext(ctx, "retrieval: erasure claim lost to another sweep", slog.String("erasure", e.UUID))
		return nil
	}
	return err
}

func passTimeout(hold time.Duration) time.Duration {
	if d := hold - passMargin; d > 0 {
		return d
	}
	return hold
}
