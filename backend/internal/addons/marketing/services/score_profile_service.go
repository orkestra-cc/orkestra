package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ErrScoreProfileInvalid wraps caller-error responses on profile
// writes — failed Validate, malformed decay fn, etc. ErrIs friendly.
var ErrScoreProfileInvalid = errors.New("marketing: invalid score profile")

// ScoreProfileService is the CRUD layer for marketing_score_profiles.
// Two non-trivial responsibilities beyond the repository pass-through:
//
//   - Save() bumps Version + persists, then calls
//     ScoreService.InvalidateProfile to flip every downstream
//     snapshot to stale=true. The nightly job + the next eager hit
//     on each person settle the recompute.
//
//   - Delete() cascades to snapshots via
//     ScoreService.DeleteForProfile — orphan snapshots would
//     otherwise survive on disk.
//
// The eager + nightly recompute paths read profiles directly through
// the repository (ScoreService.OnActivityInserted, RecomputeStaleBatch);
// this service is the operator-facing admin surface.
type ScoreProfileService struct {
	repo   *repository.ScoreProfileRepository
	score  *ScoreService
	logger *slog.Logger
}

// NewScoreProfileService wires the service. The ScoreService
// dependency is non-optional because Save/Delete have to invalidate
// snapshots — a missing dependency would silently leak stale data.
func NewScoreProfileService(repo *repository.ScoreProfileRepository, score *ScoreService, logger *slog.Logger) *ScoreProfileService {
	return &ScoreProfileService{repo: repo, score: score, logger: logger}
}

// Create persists a fresh profile. Runs Validate before any DB work;
// mints the UUID and stamps audit fields from ctxauth.
func (s *ScoreProfileService) Create(ctx context.Context, p *models.ScoreProfile) (*models.ScoreProfile, error) {
	if p == nil {
		return nil, ErrScoreProfileInvalid
	}
	if err := p.Validate(); err != nil {
		return nil, errors.Join(ErrScoreProfileInvalid, err)
	}
	if p.UUID == "" {
		p.UUID = uuid.New().String()
	}
	if p.Version == 0 {
		p.Version = 1
	}
	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		p.CreatedBy = actor
		p.UpdatedBy = actor
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, p.UUID)
}

// Get returns the profile by UUID.
func (s *ScoreProfileService) Get(ctx context.Context, uuid string) (*models.ScoreProfile, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

// List returns profiles in the caller's tenant. activeOnly mirrors
// the eager-recompute filter (the engine only computes against
// active profiles).
func (s *ScoreProfileService) List(ctx context.Context, activeOnly bool) ([]models.ScoreProfile, error) {
	return s.repo.List(ctx, repository.ScoreProfileListFilter{ActiveOnly: activeOnly})
}

// Save persists an updated profile. The caller passes the full
// profile body — rule arrays are full-replace semantics
// (operators editing rules expect the new list to be authoritative).
//
// Flow:
//   - Validate.
//   - Load existing to preserve CreatedAt/CreatedBy + carry forward
//     the version counter.
//   - BumpVersion(now) — guarantees monotonic version + fresh
//     UpdatedAt.
//   - Replace in the repository.
//   - Invalidate downstream snapshots in one bulk write.
//
// Returns the persisted profile (with the bumped version) so the
// caller can render it back without an extra round-trip.
func (s *ScoreProfileService) Save(ctx context.Context, p *models.ScoreProfile) (*models.ScoreProfile, error) {
	if p == nil || p.UUID == "" {
		return nil, ErrScoreProfileInvalid
	}
	if err := p.Validate(); err != nil {
		return nil, errors.Join(ErrScoreProfileInvalid, err)
	}

	existing, err := s.repo.GetByUUID(ctx, p.UUID)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = existing.CreatedAt
	p.CreatedBy = existing.CreatedBy
	p.Version = existing.Version
	p.BumpVersion(time.Now().UTC())
	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		p.UpdatedBy = actor
	}

	if err := s.repo.Replace(ctx, p); err != nil {
		return nil, err
	}

	if _, err := s.score.InvalidateProfile(ctx, p.UUID); err != nil {
		// Best-effort: the version bump on disk is the durable
		// invalidation signal — IsStaleAgainst(profileVersion) will
		// flip the snapshot to stale at read time even if MarkStale
		// didn't run. Log so operators see the failure but don't
		// fail the save.
		if s.logger != nil {
			s.logger.WarnContext(ctx, "marketing: snapshot invalidation failed (version bump is the durable signal)",
				slog.String("profileUuid", p.UUID),
				slog.Any("err", err),
			)
		}
	}

	return s.repo.GetByUUID(ctx, p.UUID)
}

// Delete removes the profile + cascades to snapshots. The repository
// returns ErrScoreProfileNotFound when the profile is already gone;
// the caller maps to 404.
func (s *ScoreProfileService) Delete(ctx context.Context, uuid string) error {
	if err := s.repo.Delete(ctx, uuid); err != nil {
		return err
	}
	if err := s.score.DeleteForProfile(ctx, uuid); err != nil {
		// Same trade-off as Save — operator sees the orphan rows in
		// the leaderboard until the next nightly drain catches them
		// (RecomputeStaleBatch deletes orphan snapshots for missing
		// profiles).
		if s.logger != nil {
			s.logger.WarnContext(ctx, "marketing: snapshot cascade-delete failed (nightly job will catch orphans)",
				slog.String("profileUuid", uuid),
				slog.Any("err", err),
			)
		}
	}
	return nil
}
