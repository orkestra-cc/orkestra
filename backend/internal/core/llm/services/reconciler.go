package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// PendingRepo lists the orphaned reservations of every org.
type PendingRepo interface {
	ListPendingExpired(ctx context.Context, before time.Time, limit int) ([]models.UsageEvent, error)
}

var _ PendingRepo = (*repository.Usage)(nil)

const reconcileBatch = 200

// Reconciler closes reservations whose owner died before finalizing. The
// whole reservation is charged — the estimated input as input, the
// reserved output (persisted on the row) as output — because the real
// usage is unknown and under-counting would let a crash loop spend past
// the budget. Finalize's pending guard makes it race-free with a late
// router finalization: whichever closes the row first counts, the other
// gets ErrAlreadyFinalized.
type Reconciler struct {
	pending  PendingRepo
	budget   *BudgetService
	logger   *slog.Logger
	interval time.Duration
}

func NewReconciler(pending PendingRepo, budget *BudgetService, logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{pending: pending, budget: budget, logger: logger, interval: time.Minute}
}

// Run ticks RunOnce every minute until ctx is done.
func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := r.RunOnce(ctx); err != nil {
				r.logger.WarnContext(ctx, "llm: reconcile pass failed", slog.String("error", err.Error()))
			}
		}
	}
}

// RunOnce closes up to reconcileBatch expired pending rows and returns
// how many it examined.
func (r *Reconciler) RunOnce(ctx context.Context) (int, error) {
	rows, err := r.pending.ListPendingExpired(ctx, time.Now().UTC(), reconcileBatch)
	if err != nil {
		return 0, err
	}
	for i := range rows {
		ev := &rows[i]
		out := min(max(ev.ReservedOutputTokens, 0), ev.ReservedTokens)
		res := &Reservation{LedgerUUID: ev.UUID, Reserved: ev.ReservedTokens, ReservedOutput: out, Period: models.PeriodOf(ev.CreatedAt)}
		// The sweep spans every org; each row is closed under the tenant
		// stamped on it, through the scoped repository methods.
		err := r.budget.Finalize(ctxWithTenant(ctx, ev.TenantID), res, FinalizeInput{
			Outcome: models.OutcomeError, ErrorCode: "reconciled_timeout",
			Usage:   iface.LLMUsage{InputTokens: ev.ReservedTokens - out, OutputTokens: out},
			Latency: time.Since(ev.CreatedAt),
		})
		if err != nil && !errors.Is(err, ErrAlreadyFinalized) {
			r.logger.WarnContext(ctx, "llm: reconcile row failed", slog.String("row", ev.UUID), slog.String("error", err.Error()))
		}
	}
	return len(rows), nil
}

// ctxWithTenant scopes a system job's context (reconciler, outbox worker)
// to one internal org, the way the auth middleware would for a request in
// it. Only for rows whose tenantId the job read from the database: every
// llm org is internal (Tier 1). There is no SDK API for this, so it uses
// context.WithValue(ctx, ctxauth.KeyTenantID, …) directly.
func ctxWithTenant(ctx context.Context, tenantID string) context.Context {
	// ctxauth.KeyTenantID is the untyped string key AuthMiddleware writes.
	return ctxauth.WithTenantKind(context.WithValue(ctx, ctxauth.KeyTenantID, tenantID), "internal")
}
