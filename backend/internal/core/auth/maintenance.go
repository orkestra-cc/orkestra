package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/metrics"
)

// Sweep cadence. The CADENCE adapts to the backlog; the batch never does.
//
// A fixed 6-hour interval cannot drain an upgraded installation: 5,000
// rows per tier every six hours is 20,000 a day, so a one-million-row
// backlog takes fifty days and a five-million-row one over eight months.
// At the drain cadence the same batch clears ~1.4M rows per tier per day
// — a sustained ~17 deletes per second, unremarkable load — so a
// million-row backlog clears in under a day and a five-million-row one in
// under four, while every individual pass stays bounded and the loop
// returns to idle on its own.
//
// Self-draining is also what removes the need for an operator-triggered
// sweep: a design that drains too slowly has to tell operators to run
// extra cycles in a maintenance window, which means an admin endpoint or
// a manual procedure to build, secure and document.
//
// Deliberately not config fields: these are maintenance constants, not
// policy, and a knob here is surface to document and get wrong.
const (
	sweepIdleInterval  = 6 * time.Hour
	sweepDrainInterval = 5 * time.Minute
	// sweepStartupDelay keeps the first pass off the boot path so index
	// builds and module initialisation are not competing with a drain.
	sweepStartupDelay = 3 * time.Minute
	// tokenSweepLeaseKey names the cluster-wide scheduler lease.
	tokenSweepLeaseKey = "auth:maintenance:token-sweep"
	// sweepLeaseReleaseTimeout bounds the handover on shutdown. Release
	// is best-effort: the lease expires on its own TTL anyway, and a
	// hung Redis must not hold up process shutdown.
	sweepLeaseReleaseTimeout = 5 * time.Second
)

// sweepTier pairs a repository with the label its metrics carry.
type sweepTier struct {
	name string // "operator" | "client" — the closed metric label
	repo repository.RefreshTokenRepository
	// backlog is the local estimate: seeded by one indexed count on entry
	// to drain mode, decremented by successful deletions, reset when the
	// tier reports hasMore=false, recomputed if leadership changes.
	backlog        int64
	backlogCounted bool
}

// Start launches the refresh-token retention sweep. Idempotent, and a
// stopped module can start again — the registry calls these per hot
// enable/disable, not only at boot. Mirrors the logging module's
// lifecycle mutex/cancel/done pattern so no second ticker survives a
// toggle cycle.
//
// It never returns a non-nil error. auth is a core module, and
// ModuleRegistry.StartAll propagates a core module's Start error to
// main.go's log.Fatalf — so reporting a degraded Redis here would turn a
// maintenance-only dependency into a hard boot dependency, exactly the
// inversion ADR-0017 D7 forbids. Every recoverable condition (no lease,
// no tiers, Redis unreachable) skips maintenance and returns nil;
// leadership is acquired inside the goroutine, after Start has returned.
func (m *AuthModule) Start(ctx context.Context) error {
	if len(m.sweepTiers) == 0 || m.sweepLease == nil {
		// Nothing to sweep, or Redis did not satisfy the lease contract
		// at Init. Maintenance is skipped; authentication is untouched.
		return nil
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.logger == nil {
		m.logger = slog.Default()
	}
	if m.sweepCancel != nil {
		select {
		case <-m.sweepDone:
			m.sweepCancel = nil
			m.sweepDone = nil
		default:
			return nil // already running
		}
	}

	// WithoutCancel: the caller's context may be request-scoped — the
	// admin enable/disable endpoint hands StartModule the HTTP request
	// context — and a maintenance loop that dies the moment that
	// response is written would be a silent no-op. Stop owns the loop's
	// lifetime instead.
	sweepCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	m.sweepCancel = cancel
	m.sweepDone = done
	go m.tokenSweepLoop(sweepCtx, done, sweepStartupDelay, services.LeaseRenewInterval, services.LeaseRetryInterval)
	return nil
}

// Stop cancels the sweep loop and waits for it to exit, releasing the
// scheduler lease so another replica can take over immediately rather
// than after the lease TTL.
func (m *AuthModule) Stop(ctx context.Context) error {
	m.lifecycleMu.Lock()
	cancel := m.sweepCancel
	done := m.sweepDone
	m.lifecycleMu.Unlock()
	if cancel == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
		m.lifecycleMu.Lock()
		if m.sweepDone == done {
			m.sweepCancel = nil
			m.sweepDone = nil
		}
		m.lifecycleMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// tokenSweepLoop is the elected scheduler.
//
// startDelay, renewEvery and retryEvery are parameters rather than reads
// of the constants so the leadership tests can drive real iterations in
// milliseconds — the same shape the logging module's maintenanceLoop
// uses for its two intervals. Start always passes the production
// values; nothing else calls this.
//
// retryEvery joined them so RECOVERY is testable and not merely asserted
// in a comment. Every step-down path reschedules by it, so with the
// production 5-minute constant hardcoded a test could prove the loop
// survives a Redis outage but never that it re-acquires afterwards —
// which is the entire justification for stepping down instead of exiting.
func (m *AuthModule) tokenSweepLoop(ctx context.Context, done chan<- struct{}, startDelay, renewEvery, retryEvery time.Duration) {
	defer close(done)

	// Every database call below runs on this context, so returning from
	// the loop — notably on lost leadership — cancels the in-flight
	// query as well as the schedule.
	ctx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()

	leader := false
	defer func() {
		if !leader {
			// Not ours to release. A non-owner Release is already a
			// no-op server-side, but skipping it keeps the shutdown of
			// a follower free of Redis round-trips entirely.
			return
		}
		// The loop context is usually already cancelled here (Stop is the
		// common exit), and a cancelled context cannot carry the release
		// round-trip — so the handover uses a fresh one, time-bounded so
		// an unresponsive Redis cannot hold up shutdown.
		releaseCtx, cancel := context.WithTimeout(context.Background(), sweepLeaseReleaseTimeout)
		defer cancel()
		if err := m.sweepLease.Release(releaseCtx); err != nil {
			m.logger.Warn("auth: failed to release token-sweep lease", slog.String("error", err.Error()))
		}
	}()

	renewTicker := time.NewTicker(renewEvery)
	defer renewTicker.Stop()

	// next is an absolute deadline, not a duration re-derived on each
	// wake. Renewal fires every 30s against waits of 5m and 6h, so a
	// loop that rebuilt its timer from the full interval after every
	// renew tick would never reach a pass at all.
	next := time.Now().Add(startDelay)

	for {
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-renewTicker.C:
			timer.Stop()
			if !leader {
				continue
			}
			ok, err := m.sweepLease.Renew(ctx)
			if ctx.Err() != nil {
				// Stop cancelled us mid-renew. That is a shutdown, not a
				// lost election: leaving `leader` set lets the deferred
				// release hand the lease over immediately instead of
				// making the successor wait out the 2m TTL, and keeps the
				// log from reporting a loss that never happened.
				return
			}
			if err != nil || !ok {
				// Step down and re-contend — never exit. Nothing calls
				// Start again for the life of the process, so returning
				// here would end refresh-token retention until the next
				// restart: one Redis failover, or one lapsed key on a
				// single-replica install, and a backlog that needs days of
				// uninterrupted draining stops draining with a single Warn
				// line to show for it. Stepping down is safe — the
				// `if !leader` guard below means this replica cannot sweep
				// unelected, and SetNX keeps it a follower for as long as a
				// successor holds the key.
				m.logger.Warn("auth: token-sweep leadership relinquished, re-contending",
					slog.String("outcome", errOutcome(err)))
				leader = false
				next = time.Now().Add(retryEvery)
				continue
			}
			continue
		case <-timer.C:
		}

		if !leader {
			acquired, err := m.sweepLease.Acquire(ctx)
			if err != nil {
				// Redis unavailable is never leadership. Bounded warning,
				// skip maintenance, never touch authentication.
				m.logger.Warn("auth: token-sweep lease unavailable, skipping maintenance",
					slog.String("error", err.Error()))
				next = time.Now().Add(retryEvery)
				continue
			}
			if !acquired {
				next = time.Now().Add(retryEvery)
				continue
			}
			leader = true
			// Leadership changed: the previous leader's backlog estimate
			// is not ours, so recompute on the next drain entry.
			for i := range m.sweepTiers {
				m.sweepTiers[i].backlogCounted = false
			}
		}

		anyHasMore := false
		for i := range m.sweepTiers {
			if m.sweepOneTier(ctx, &m.sweepTiers[i]) {
				anyHasMore = true
			}
		}

		// Reschedule on the hasMore bit the batch just reported.
		next = time.Now().Add(nextSweepInterval(anyHasMore))
	}
}

// nextSweepInterval is the whole adaptive mechanism, isolated so it can be
// asserted without a clock: while there is a backlog the loop runs every
// five minutes; once there is not, it costs one query per tier every six
// hours and nothing else.
func nextSweepInterval(anyHasMore bool) time.Duration {
	if anyHasMore {
		return sweepDrainInterval
	}
	return sweepIdleInterval
}

// sweepOneTier performs at most one bounded batch and returns hasMore.
func (m *AuthModule) sweepOneTier(ctx context.Context, tier *sweepTier) bool {
	started := time.Now()
	deleted, hasMore, err := tier.repo.CleanupExpiredTokens(ctx, repository.SweepBatchLimit)
	elapsed := time.Since(started)
	if err != nil {
		m.logger.Error("auth: token sweep batch failed",
			slog.String("tier", tier.name),
			slog.String("error", err.Error()))
		return false
	}

	metrics.Default().RecordTokenSweep(tier.name, deleted, elapsed)

	switch {
	case hasMore && !tier.backlogCounted:
		// The ONE indexed count, taken on entry to drain mode. Not per
		// cycle: at the five-minute cadence that would scan the whole
		// eligible range 288 times a day to publish a number that is an
		// estimate either way.
		if total, cErr := tier.repo.CountExpiredTokens(ctx); cErr == nil {
			tier.backlog = total
			tier.backlogCounted = true
		} else {
			m.logger.Warn("auth: token-sweep backlog count failed, gauge will lag",
				slog.String("tier", tier.name),
				slog.String("error", cErr.Error()))
		}
	case hasMore:
		tier.backlog -= deleted
		if tier.backlog < 0 {
			tier.backlog = 0
		}
	default:
		tier.backlog = 0
		tier.backlogCounted = false
	}
	metrics.Default().SetTokenSweepBacklog(tier.name, tier.backlog)

	m.logger.Info("auth: token sweep batch",
		slog.String("tier", tier.name),
		slog.Int64("deleted", deleted),
		slog.Bool("hasMore", hasMore),
		slog.Int64("backlogEstimate", tier.backlog),
		slog.Duration("duration", elapsed))
	return hasMore
}

// errOutcome distinguishes the two ways a renewal fails. Both mean "not
// the leader", but only one means Redis is unwell, and an operator
// reading the log line needs to know which.
func errOutcome(err error) string {
	if err != nil {
		return "renew_error"
	}
	return "not_owner"
}
