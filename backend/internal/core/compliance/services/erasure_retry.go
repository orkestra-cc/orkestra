package services

import (
	"context"
	"log/slog"
	"time"
)

const (
	erasureRetryInterval = time.Minute
	erasureRetryBatch    = 50
)

// ErasureRetryWorker finishes incomplete erasure requests: every tick it
// claims the requests whose retry is due and runs their remaining producers.
// Safe on every replica; the claim is a lease.
type ErasureRetryWorker struct {
	svc      *ErasureRequestService
	logger   *slog.Logger
	interval time.Duration
	batch    int
}

// NewErasureRetryWorker builds the worker (tick 1m, at most 50 requests per pass).
func NewErasureRetryWorker(svc *ErasureRequestService, logger *slog.Logger) *ErasureRetryWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ErasureRetryWorker{svc: svc, logger: logger, interval: erasureRetryInterval, batch: erasureRetryBatch}
}

// RunOnce retries due requests until none is due or the batch is spent. It
// returns the number of runs handled; a RetryDue error ends the pass.
func (w *ErasureRetryWorker) RunOnce(ctx context.Context) (int, error) {
	n := 0
	for n < w.batch {
		ran, err := w.svc.RetryDue(ctx)
		if err != nil {
			return n, err
		}
		if !ran {
			break
		}
		n++
	}
	return n, nil
}

// Loop runs RunOnce every interval until stop or ctx cancellation.
func (w *ErasureRetryWorker) Loop(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := w.RunOnce(ctx); err != nil {
				w.logger.Warn("compliance: erasure retry pass failed", slog.String("error", err.Error()))
			} else if n > 0 {
				w.logger.Info("compliance: erasure retry pass", slog.Int("runs", n))
			}
		}
	}
}
