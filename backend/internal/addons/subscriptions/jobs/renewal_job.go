package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-addon-subscriptions/services"
)

// RenewalJob ticks every Interval and processes due subscriptions via
// RenewalService. Lifecycle mirrors billing/jobs/polling_job.go — Start
// spawns a goroutine, Stop closes the stop channel.
type RenewalJob struct {
	renewal  *services.RenewalService
	interval time.Duration
	logger   *slog.Logger
	stopChan chan struct{}
	running  bool
}

func NewRenewalJob(renewal *services.RenewalService, interval time.Duration, logger *slog.Logger) *RenewalJob {
	if interval <= 0 {
		interval = time.Hour
	}
	return &RenewalJob{
		renewal:  renewal,
		interval: interval,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

func (j *RenewalJob) Start(ctx context.Context) {
	if j.running {
		j.logger.Warn("renewal job already running")
		return
	}
	j.running = true
	j.logger.Info("starting subscription renewal job",
		slog.Duration("interval", j.interval),
	)
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	// Immediate tick on start so a fresh boot catches anything already due.
	j.tick(ctx)

	for {
		select {
		case <-ticker.C:
			j.tick(ctx)
		case <-j.stopChan:
			j.logger.Info("stopping subscription renewal job")
			j.running = false
			return
		case <-ctx.Done():
			j.logger.Info("renewal job stopped due to context cancellation")
			j.running = false
			return
		}
	}
}

func (j *RenewalJob) Stop() {
	if j.running {
		close(j.stopChan)
	}
}

func (j *RenewalJob) tick(ctx context.Context) {
	processed, charged, failed := j.renewal.ProcessDue(ctx, time.Now().UTC())
	if processed > 0 {
		j.logger.Info("renewal tick",
			slog.Int("processed", processed),
			slog.Int("charged", charged),
			slog.Int("failed", failed),
		)
	}
}
