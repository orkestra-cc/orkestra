package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// CardExpirationJob drains expired-but-still-active cards on an
// interval. Cloned from RecomputeJob (same lifecycle shape, same
// ctx-cancel + stopChan idempotency) so the marketing addon presents
// a uniform background-job pattern.
//
// Each tick reads up to `batchSize` cards from
// CardRepository.ListExpiringAcrossTenants (the documented
// cross-tenant bypass for the scheduler) and calls CardService.Expire
// for each, after re-stamping the row's tenantId onto a fresh ctx via
// ctxauth.KeyTenantID. Subsequent downstream writes go through the
// normal tenant-scoped helpers.
//
// The default interval is 1 hour — operators tune via
// MARKETING_CARD_EXPIRATION_INTERVAL or the admin UI.
type CardExpirationJob struct {
	cardRepo *repository.CardRepository
	cardSvc  *services.CardService
	interval time.Duration
	logger   *slog.Logger
	stopChan chan struct{}
	running  bool
}

// NewCardExpirationJob constructs the ticker. A non-positive interval
// falls back to 1h (matches the module config default).
func NewCardExpirationJob(cardRepo *repository.CardRepository, cardSvc *services.CardService, interval time.Duration, logger *slog.Logger) *CardExpirationJob {
	if interval <= 0 {
		interval = time.Hour
	}
	return &CardExpirationJob{
		cardRepo: cardRepo,
		cardSvc:  cardSvc,
		interval: interval,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// Start blocks on the ticker until Stop or ctx cancel. Intended to
// run in a goroutine spawned by Module.Start. Fires an immediate
// tick on boot so any cards that expired during downtime are caught
// without waiting an interval.
func (j *CardExpirationJob) Start(ctx context.Context) {
	if j.running {
		if j.logger != nil {
			j.logger.WarnContext(ctx, "marketing: card expiration job already running")
		}
		return
	}
	j.running = true
	if j.logger != nil {
		j.logger.InfoContext(ctx, "marketing: starting card expiration job",
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
				j.logger.InfoContext(ctx, "marketing: stopping card expiration job")
			}
			j.running = false
			return
		case <-ctx.Done():
			if j.logger != nil {
				j.logger.InfoContext(ctx, "marketing: card expiration job stopped (context cancelled)")
			}
			j.running = false
			return
		}
	}
}

// Stop signals the ticker to exit. Safe to call multiple times.
func (j *CardExpirationJob) Stop() {
	if j.running {
		close(j.stopChan)
	}
}

// tick drains up to maxBatchesPerTick × batchSize expired cards per
// invocation so a multi-tenant backlog doesn't starve other ticks.
func (j *CardExpirationJob) tick(ctx context.Context) {
	const batchSize int64 = 200
	const maxBatchesPerTick = 10 // bound: 10 × 200 = 2000 expirations per tick
	asOf := time.Now().UTC()
	total := 0
	for i := 0; i < maxBatchesPerTick; i++ {
		if ctx.Err() != nil {
			return
		}
		batch, err := j.cardRepo.ListExpiringAcrossTenants(ctx, asOf, batchSize)
		if err != nil {
			if j.logger != nil {
				j.logger.ErrorContext(ctx, "marketing: card expiration list failed",
					slog.Any("err", err),
				)
			}
			return
		}
		if len(batch) == 0 {
			break
		}
		for _, card := range batch {
			if ctx.Err() != nil {
				return
			}
			scoped := context.WithValue(ctx, ctxauth.KeyTenantID, card.TenantID)
			if _, err := j.cardSvc.Expire(scoped, card.UUID); err != nil {
				if j.logger != nil {
					j.logger.WarnContext(ctx, "marketing: card expire failed",
						slog.String("tenantId", card.TenantID),
						slog.String("cardUuid", card.UUID),
						slog.Any("err", err),
					)
				}
				continue
			}
			total++
		}
	}
	if total > 0 && j.logger != nil {
		j.logger.InfoContext(ctx, "marketing: card expiration tick",
			slog.Int("expired", total),
		)
	}
}
