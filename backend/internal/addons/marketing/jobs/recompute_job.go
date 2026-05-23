// Package jobs hosts the marketing addon's background workers. Phase 2
// ships the score recompute ticker; future phases (card expiration in
// Phase 4, retention cleanup in Phase 5) follow the same lifecycle
// shape — goroutine started by Module.Start, stopped via a stopChan
// + context cancel.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/services"
)

// RecomputeJob drains stale score snapshots on an interval. Cloned
// from
// backend/internal/addons/subscriptions/jobs/renewal_job.go — same
// lifecycle (Start spawns a goroutine, Stop closes stopChan, ctx
// cancellation also exits) so the two addons present a uniform
// background-job pattern to operators.
//
// Each tick loops through ScoreService.RecomputeStaleBatch in chunks
// of 200 (the repository's default cap) until the batch returns 0.
// The default interval is 24 h — operators tune via
// MARKETING_SCORE_RECOMPUTE_INTERVAL or the admin UI.
type RecomputeJob struct {
	score    *services.ScoreService
	interval time.Duration
	logger   *slog.Logger
	stopChan chan struct{}
	running  bool
}

// NewRecomputeJob constructs the ticker. A non-positive interval
// falls back to 24h (matches the module config default and the plan
// §2.4 semantics).
func NewRecomputeJob(score *services.ScoreService, interval time.Duration, logger *slog.Logger) *RecomputeJob {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &RecomputeJob{
		score:    score,
		interval: interval,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// Start blocks on the ticker until Stop or ctx cancel. Intended to
// run in a goroutine spawned by Module.Start.
//
// An immediate tick fires on start so a fresh boot drains any
// snapshots that were marked stale by profile edits while the
// process was down.
func (j *RecomputeJob) Start(ctx context.Context) {
	if j.running {
		if j.logger != nil {
			j.logger.WarnContext(ctx, "marketing: recompute job already running")
		}
		return
	}
	j.running = true
	if j.logger != nil {
		j.logger.InfoContext(ctx, "marketing: starting score recompute job",
			slog.Duration("interval", j.interval),
		)
	}
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	j.tick(ctx)

	for {
		select {
		case <-ticker.C:
			j.tick(ctx)
		case <-j.stopChan:
			if j.logger != nil {
				j.logger.InfoContext(ctx, "marketing: stopping score recompute job")
			}
			j.running = false
			return
		case <-ctx.Done():
			if j.logger != nil {
				j.logger.InfoContext(ctx, "marketing: recompute job stopped (context cancelled)")
			}
			j.running = false
			return
		}
	}
}

// Stop signals the ticker to exit. Safe to call multiple times
// because we guard on `running`. Idempotency matters: Module.Stop
// may be invoked from both an admin disable and a process shutdown
// sequence.
func (j *RecomputeJob) Stop() {
	if j.running {
		close(j.stopChan)
	}
}

// tick loops RecomputeStaleBatch until the batch returns 0 (drain
// fully) or ctx cancels. Bounds the per-tick work so a multi-tenant
// backlog doesn't starve other ticks.
func (j *RecomputeJob) tick(ctx context.Context) {
	const batchSize int64 = 200
	const maxBatchesPerTick = 50 // bound: 50 × 200 = 10k recomputes per tick
	var total int
	for i := 0; i < maxBatchesPerTick; i++ {
		if ctx.Err() != nil {
			return
		}
		n, err := j.score.RecomputeStaleBatch(ctx, batchSize)
		if err != nil {
			if j.logger != nil {
				j.logger.ErrorContext(ctx, "marketing: stale recompute batch failed",
					slog.Any("err", err),
				)
			}
			return
		}
		if n == 0 {
			break
		}
		total += n
	}
	if total > 0 && j.logger != nil {
		j.logger.InfoContext(ctx, "marketing: score recompute tick",
			slog.Int("recomputed", total),
		)
	}
}
