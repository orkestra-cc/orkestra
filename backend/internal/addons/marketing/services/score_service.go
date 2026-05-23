package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/scoring"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ScoreService is the orchestration layer between the activity log,
// the scoring profiles, the pure engine, and the snapshot cache.
//
// Two write paths:
//
//   - Eager (OnActivityInserted) — registered as a listener on
//     ActivityService, fires after every successful Activity.Create.
//     Iterates every active profile for the activity's tenant and
//     recomputes its snapshot for the affected person.
//
//   - Nightly (RecomputeStaleBatch) — driven by RecomputeJob's
//     ticker. Drains stale=true snapshots across every tenant in
//     bounded batches; each snapshot is recomputed under a context
//     stamped with the snapshot's own tenantID.
//
// Both paths fan through recomputeOne, which is the single
// authoritative recompute. Concurrency safety is handled by a
// per-(tenant, person) mutex (sync.Map of *sync.Mutex) so two
// concurrent activity inserts on the same person serialise on their
// snapshot upsert.
type ScoreService struct {
	snapRepo   *repository.ScoreSnapshotRepository
	profRepo   *repository.ScoreProfileRepository
	actRepo    *repository.ActivityRepository
	personRepo *repository.PersonRepository
	engine     *scoring.Engine
	logger     *slog.Logger

	eagerEnabled bool

	// locks keys "tenantID\x00personUUID" → *sync.Mutex. Unbounded
	// growth is acceptable: typical deployment is 10k persons × 10
	// tenants = 100k entries × ~24 bytes = ~2.4 MB. A future
	// eviction pass can land if this gets bigger.
	locks sync.Map
}

// NewScoreService wires the service. eagerEnabled mirrors the
// scoreEagerOnInsert module config — operators flip it off for
// import bursts and let the nightly job catch up.
func NewScoreService(
	snap *repository.ScoreSnapshotRepository,
	prof *repository.ScoreProfileRepository,
	act *repository.ActivityRepository,
	person *repository.PersonRepository,
	engine *scoring.Engine,
	eagerEnabled bool,
	logger *slog.Logger,
) *ScoreService {
	return &ScoreService{
		snapRepo:     snap,
		profRepo:     prof,
		actRepo:      act,
		personRepo:   person,
		engine:       engine,
		eagerEnabled: eagerEnabled,
		logger:       logger,
	}
}

// OnActivityInserted is the eager-recompute hook. Registered on
// ActivityService via RegisterListener in Module.Init. When
// eagerEnabled is false the call is a no-op — the nightly job is
// the only recompute path.
//
// Errors are logged, not returned: a recompute failure must not
// roll back the activity write that triggered it (the activity log
// is the source of truth; the snapshot is recoverable).
func (s *ScoreService) OnActivityInserted(ctx context.Context, activity models.Activity) {
	if !s.eagerEnabled {
		return
	}
	profiles, err := s.profRepo.List(ctx, repository.ScoreProfileListFilter{ActiveOnly: true})
	if err != nil {
		s.logf(ctx, slog.LevelError, "eager recompute: list profiles failed",
			slog.String("activityUuid", activity.UUID),
			slog.Any("err", err),
		)
		return
	}
	for i := range profiles {
		if err := s.recomputeOne(ctx, activity.PersonUUID, &profiles[i]); err != nil {
			s.logf(ctx, slog.LevelError, "eager recompute: recomputeOne failed",
				slog.String("personUuid", activity.PersonUUID),
				slog.String("profileUuid", profiles[i].UUID),
				slog.Any("err", err),
			)
		}
	}
}

// RecomputeStaleBatch drains up to `limit` stale snapshots across
// every tenant and recomputes them. Returns the number of snapshots
// actually processed (matched + recomputed); the caller loops until
// 0 to fully drain.
//
// Cross-tenant scan: the repository call is ListStaleAcrossTenants
// (the one tenantrepo bypass in this package — see repository
// comments). For each snapshot we stamp tenantID onto a fresh
// context before delegating to scope-aware repository methods.
func (s *ScoreService) RecomputeStaleBatch(ctx context.Context, limit int64) (int, error) {
	snapshots, err := s.snapRepo.ListStaleAcrossTenants(ctx, limit)
	if err != nil {
		return 0, err
	}
	var processed int
	for _, snap := range snapshots {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		tenantCtx := context.WithValue(ctx, ctxauth.KeyTenantID, snap.TenantID)
		profile, err := s.profRepo.GetByUUID(tenantCtx, snap.ProfileUUID)
		if err != nil {
			if errors.Is(err, repository.ErrScoreProfileNotFound) {
				// Profile was deleted but the snapshot survived —
				// cascade-delete the orphan.
				_, _ = s.snapRepo.DeleteByProfileUUID(tenantCtx, snap.ProfileUUID)
				continue
			}
			s.logf(ctx, slog.LevelError, "stale recompute: profile fetch failed",
				slog.String("profileUuid", snap.ProfileUUID),
				slog.Any("err", err),
			)
			continue
		}
		if err := s.recomputeOne(tenantCtx, snap.PersonUUID, profile); err != nil {
			s.logf(ctx, slog.LevelError, "stale recompute: recomputeOne failed",
				slog.String("personUuid", snap.PersonUUID),
				slog.String("profileUuid", profile.UUID),
				slog.Any("err", err),
			)
			continue
		}
		processed++
	}
	return processed, nil
}

// InvalidateProfile marks every snapshot for the profile as
// stale=true. Called by ScoreProfileService.Save after BumpVersion.
// Returns the number of snapshots flipped. Errors propagate so the
// caller can surface them to the operator UI.
func (s *ScoreService) InvalidateProfile(ctx context.Context, profileUUID string) (int64, error) {
	return s.snapRepo.MarkStaleByProfile(ctx, profileUUID)
}

// DeleteForPerson removes every snapshot for the person. Called by
// the GDPR right-to-be-forgotten cascade (PersonService.Delete in
// PR-4 wiring).
func (s *ScoreService) DeleteForPerson(ctx context.Context, personUUID string) error {
	_, err := s.snapRepo.DeleteByPersonUUID(ctx, personUUID)
	return err
}

// DeleteForProfile removes every snapshot for the profile. Called
// by ScoreProfileService.Delete.
func (s *ScoreService) DeleteForProfile(ctx context.Context, profileUUID string) error {
	_, err := s.snapRepo.DeleteByProfileUUID(ctx, profileUUID)
	return err
}

// recomputeOne is the single authoritative recompute path. Loads the
// person, evaluates the profile filter, conditionally runs the
// engine, and upserts the snapshot. Wrapped in withPersonLock so
// concurrent activity inserts on the same person serialise on the
// upsert and never lose updates.
func (s *ScoreService) recomputeOne(ctx context.Context, personUUID string, profile *models.ScoreProfile) error {
	return s.withPersonLock(ctx, personUUID, func() error {
		person, err := s.personRepo.GetByUUID(ctx, personUUID)
		if err != nil {
			if errors.Is(err, repository.ErrPersonNotFound) {
				// Person was deleted; the GDPR cascade should remove
				// snapshots elsewhere, but tidy up defensively.
				_, _ = s.snapRepo.DeleteByPersonUUID(ctx, personUUID)
				return nil
			}
			return err
		}

		now := time.Now().UTC()
		applicable := EvaluatePersonFilter(profile.Filters, person)

		// Preserve UUID when a snapshot already exists for this pair
		// (the Upsert key is (tenant, person, profile); we want the
		// snapshot's own UUID to stay stable across recomputes so
		// links from the breakdown drawer don't 404).
		existing, _ := s.snapRepo.GetByPersonProfile(ctx, personUUID, profile.UUID)

		snap := &models.ScoreSnapshot{
			PersonUUID:     personUUID,
			ProfileUUID:    profile.UUID,
			ProfileVersion: profile.Version,
			AsOf:           now,
			ComputedAt:     now,
			Applicable:     applicable,
		}
		if existing != nil {
			snap.UUID = existing.UUID
		} else {
			snap.UUID = uuid.New().String()
		}

		if applicable {
			activities, err := s.actRepo.ListAllForPerson(ctx, personUUID)
			if err != nil {
				return err
			}
			result := s.engine.Compute(activities, profile, now)
			snap.Value = result.Value
			snap.Breakdown = result.Breakdown
			snap.ActivityCount = result.ActivityCount
			snap.LastActivityAt = result.LastActivityAt
		}
		// applicable=false snapshots keep Value=0, Breakdown empty —
		// the leaderboard handler filters these out unless explicitly
		// requested.

		return s.snapRepo.Upsert(ctx, snap)
	})
}

// withPersonLock serialises the body for the (tenant, person) pair.
// Different persons (same or different tenants) can execute the
// body concurrently. Lock acquisition is cheap (sync.Map LoadOrStore
// is lock-free in the common case).
func (s *ScoreService) withPersonLock(ctx context.Context, personUUID string, fn func() error) error {
	key := lockKey(ctx, personUUID)
	actual, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

func lockKey(ctx context.Context, personUUID string) string {
	tid, _ := ctxauth.GetTenantID(ctx)
	return tid + "\x00" + personUUID
}

// logf is a no-op when logger is nil — lets tests construct the
// service without wiring a slog.Handler.
func (s *ScoreService) logf(ctx context.Context, level slog.Level, msg string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Log(ctx, level, msg, args...)
}
