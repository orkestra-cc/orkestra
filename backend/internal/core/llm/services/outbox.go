package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
)

// OutboxRepo is the worker's view of llm_budget_outbox. LeaseDue sweeps
// every org; the other three are tenant-scoped and run under the event's
// own tenant.
type OutboxRepo interface {
	LeaseDue(ctx context.Context, now time.Time, leaseFor time.Duration) (*models.OutboxEvent, error) // nil when nothing is due
	MarkSent(ctx context.Context, uuid string) error
	Reschedule(ctx context.Context, uuid string, attempts int, next time.Time) error
	MarkFailed(ctx context.Context, uuid string) error
}

var _ OutboxRepo = (*repository.Outbox)(nil)

// Notifier turns an outbox event into the budget notification addressed
// to the org's owners, admins and the budget's extra recipients. It is
// called with a context scoped to the event's org (internal tier). It
// must derive its idempotency key from ev.UUID, so a re-delivery after a
// crash between send and MarkSent is one logical notification.
type Notifier interface {
	SendBudgetEvent(ctx context.Context, ev models.OutboxEvent) error
}

const (
	outboxMaxAttempts = 20
	outboxLease       = 2 * time.Minute
	outboxBatch       = 100
)

// OutboxWorker drains llm_budget_outbox at-least-once with a recoverable
// lease and bounded, growing retries; failed is terminal.
type OutboxWorker struct {
	repo     OutboxRepo
	notifier Notifier
	logger   *slog.Logger
	interval time.Duration
	lease    time.Duration
}

func NewOutboxWorker(repo OutboxRepo, notifier Notifier, logger *slog.Logger) *OutboxWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboxWorker{repo: repo, notifier: notifier, logger: logger, interval: 30 * time.Second, lease: outboxLease}
}

// Run ticks RunOnce every 30 seconds until ctx is done.
func (w *OutboxWorker) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := w.RunOnce(ctx); err != nil {
				w.logger.WarnContext(ctx, "llm: outbox pass failed", slog.String("error", err.Error()))
			}
		}
	}
}

// RunOnce drains due events one lease at a time (at most outboxBatch per
// pass) and returns how many it handled. Each event is handled under its
// own org's internal-tenant context (ruling 5.4).
func (w *OutboxWorker) RunOnce(ctx context.Context) (int, error) {
	n := 0
	for n < outboxBatch {
		ev, err := w.repo.LeaseDue(ctx, time.Now().UTC(), w.lease)
		if err != nil {
			return n, err
		}
		if ev == nil {
			return n, nil
		}
		n++
		tctx := ctxWithTenant(ctx, ev.TenantID)
		if err := w.notifier.SendBudgetEvent(tctx, *ev); err != nil {
			attempts := ev.Attempts + 1
			if attempts >= outboxMaxAttempts {
				w.logger.ErrorContext(ctx, "llm: budget notification abandoned", slog.String("event", ev.UUID), slog.Int("attempts", attempts), slog.String("error", err.Error()))
				if err := w.repo.MarkFailed(tctx, ev.UUID); err != nil {
					w.logger.WarnContext(ctx, "llm: outbox mark failed", slog.String("event", ev.UUID), slog.String("error", err.Error()))
				}
				continue
			}
			if err := w.repo.Reschedule(tctx, ev.UUID, attempts, time.Now().UTC().Add(outboxBackoff(attempts))); err != nil {
				w.logger.WarnContext(ctx, "llm: outbox reschedule failed; the lease will expire", slog.String("event", ev.UUID), slog.String("error", err.Error()))
			}
			continue
		}
		if err := w.repo.MarkSent(tctx, ev.UUID); err != nil {
			return n, err
		}
	}
	return n, nil
}

// outboxBackoff doubles from 1 minute after the first failure, capped at
// 64 minutes.
func outboxBackoff(attempts int) time.Duration {
	return time.Duration(1<<uint(min(attempts, 7))) * 30 * time.Second
}
