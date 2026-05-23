package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"go.mongodb.org/mongo-driver/bson"
)

// ErrConflictReviewInvalid wraps malformed inputs to Park / Resolve.
// Surfaces as 400 at the handler.
var ErrConflictReviewInvalid = errors.New("marketing: invalid conflict review input")

// ParkInput is the canonical request shape the pipeline (in the
// importers package) passes to ConflictReviewService.Park. The type
// lives in importers because it's part of the pipeline's outbound
// contract — defining it here would force importers to import
// services, which would create an import cycle.
type ParkInput = importers.ParkInput

// ResolveInput is the payload of POST /v1/marketing/conflict-reviews/{id}/resolve.
// Action drives the apply path; FieldOverrides is required when
// Action == manual_merge.
type ResolveInput struct {
	Action         models.ConflictAction
	FieldOverrides map[string]any
	Notes          string
}

// DismissInput is the payload of POST /v1/marketing/conflict-reviews/{id}/dismiss.
type DismissInput struct {
	Notes string
}

// ConflictReviewService orchestrates the lifecycle of a single review
// row. The pipeline calls Park during the import; the handler calls
// Resolve / Dismiss when the operator clicks through the resolver
// modal.
//
// Job-state transitions live here so the handler doesn't need to
// re-query the pending count:
//   - Park transitions parent job running → paused_for_review on
//     first park.
//   - Resolve / Dismiss transitions parent job paused_for_review →
//     done when the last pending review for that job closes.
type ConflictReviewService struct {
	reviewRepo  *repository.ConflictReviewRepository
	personRepo  *repository.PersonRepository
	orgRepo     *repository.OrganizationRepository
	jobRepo     *repository.ImportJobRepository
	activitySvc *ActivityService
	emitter     *ActivityEmitter
	logger      *slog.Logger
}

// NewConflictReviewService wires the orchestrator. ActivityService is
// the path through which IncomingActivities commit on resolve — keep
// the dependency direction one-way (review service → activity service)
// to avoid a cycle. ActivityEmitter is the auto-emission helper that
// fires the `merged` Activity when a resolve closes a review.
func NewConflictReviewService(
	reviewRepo *repository.ConflictReviewRepository,
	personRepo *repository.PersonRepository,
	orgRepo *repository.OrganizationRepository,
	jobRepo *repository.ImportJobRepository,
	activitySvc *ActivityService,
	emitter *ActivityEmitter,
	logger *slog.Logger,
) *ConflictReviewService {
	return &ConflictReviewService{
		reviewRepo:  reviewRepo,
		personRepo:  personRepo,
		orgRepo:     orgRepo,
		jobRepo:     jobRepo,
		activitySvc: activitySvc,
		emitter:     emitter,
		logger:      logger,
	}
}

// Park persists a review row + transitions the parent import job to
// paused_for_review when this is the first park for that job.
//
// Non-conflict fields on the incoming record are NOT applied here —
// per the schema, those land via the pipeline's auto-merge step
// BEFORE park. Park only writes the parked-row audit + freezes the
// blocking fields for the operator to decide on.
//
// Returns only error so the type matches the importers.ReviewParker
// interface — the pipeline does not need the persisted row.
func (s *ConflictReviewService) Park(ctx context.Context, in ParkInput) error {
	if in.ImportJobUUID == "" {
		return fmt.Errorf("%w: missing importJobUuid", ErrConflictReviewInvalid)
	}
	if !models.IsKnownConflictTargetKind(in.TargetKind) {
		return fmt.Errorf("%w: invalid targetKind", ErrConflictReviewInvalid)
	}
	if len(in.Conflicts) == 0 {
		return fmt.Errorf("%w: empty conflicts", ErrConflictReviewInvalid)
	}
	if len(in.IncomingPayload) == 0 {
		return fmt.Errorf("%w: empty incomingPayload", ErrConflictReviewInvalid)
	}

	review := &models.ConflictReview{
		ImportJobUUID:      in.ImportJobUUID,
		TargetKind:         in.TargetKind,
		ExistingUUID:       in.ExistingUUID,
		ExistingSnapshot:   in.ExistingSnapshot,
		IncomingPayload:    in.IncomingPayload,
		IncomingActivities: in.IncomingActivities,
		Conflicts:          in.Conflicts,
		Status:             models.ConflictReviewStatusPending,
	}
	if err := s.reviewRepo.Create(ctx, review); err != nil {
		return fmt.Errorf("marketing: persist review: %w", err)
	}

	// Back-ref + first-park job transition.
	if err := s.jobRepo.AppendConflictReviewUUID(ctx, in.ImportJobUUID, review.UUID); err != nil {
		s.warnf(ctx, "marketing: append conflict review uuid to job: %v", err)
	}
	pending, err := s.reviewRepo.CountPendingForJob(ctx, in.ImportJobUUID)
	if err == nil && pending == 1 {
		if err := s.jobRepo.UpdateStatus(ctx, in.ImportJobUUID, models.ImportJobStatusPausedForReview, models.ImportJobStats{}, ""); err != nil {
			s.warnf(ctx, "marketing: transition job to paused_for_review: %v", err)
		}
	}
	return nil
}

// Get returns one review by UUID.
func (s *ConflictReviewService) Get(ctx context.Context, uuid string) (*models.ConflictReview, error) {
	return s.reviewRepo.GetByUUID(ctx, uuid)
}

// List returns reviews matching filter.
func (s *ConflictReviewService) List(ctx context.Context, f repository.ConflictReviewListFilter) ([]models.ConflictReview, error) {
	return s.reviewRepo.List(ctx, f)
}

// Resolve applies the chosen action to the existing record + commits
// any parked activities + transitions the parent job to done when
// this was the last pending review.
//
//	keep_existing — no-op on existing record; parked activities still commit.
//	take_incoming — every Conflicts[].Field overwrites existing with IncomingValue.
//	manual_merge  — every FieldOverrides[k] applied as a $set on existing.
//	dismiss       — discard incoming entirely; routes to DismissInternal.
func (s *ConflictReviewService) Resolve(ctx context.Context, reviewUUID string, in ResolveInput) error {
	if !models.IsKnownConflictAction(in.Action) {
		return fmt.Errorf("%w: invalid action", ErrConflictReviewInvalid)
	}
	if in.Action == models.ConflictActionDismiss {
		return s.Dismiss(ctx, reviewUUID, DismissInput{Notes: in.Notes})
	}

	resolution := models.ConflictResolution{
		Action:         in.Action,
		FieldOverrides: in.FieldOverrides,
	}
	if err := resolution.Validate(); err != nil {
		return err
	}

	review, err := s.reviewRepo.GetByUUID(ctx, reviewUUID)
	if err != nil {
		return err
	}
	if review.Status != models.ConflictReviewStatusPending {
		return repository.ErrConflictReviewNotPending
	}

	// Apply the resolution to the existing record. Errors abort the
	// resolve — the review stays pending so the operator can retry.
	if err := s.applyResolution(ctx, review, resolution); err != nil {
		return fmt.Errorf("marketing: apply resolution: %w", err)
	}

	// Commit parked activities (best-effort but failures surface).
	if err := s.commitIncomingActivities(ctx, review); err != nil {
		return fmt.Errorf("marketing: commit incoming activities: %w", err)
	}

	// Mark resolved + transition job if last.
	actor := actorUUID(ctx)
	if err := s.reviewRepo.MarkResolved(ctx, reviewUUID, resolution, actor, in.Notes); err != nil {
		return err
	}
	s.emitMergedFor(ctx, review)
	s.transitionJobIfLast(ctx, review.ImportJobUUID)
	return nil
}

// emitMergedFor fires the auto-emission `merged` Activity for a closed
// review. Person target → personUuid is the review's existingUuid;
// Organization target → personUuid extracted from the
// incomingPayload when present (Phase 3 organization resolutions don't
// always carry a person reference, so missing personUuid silently
// skips emission).
func (s *ConflictReviewService) emitMergedFor(ctx context.Context, review *models.ConflictReview) {
	if s.emitter == nil {
		return
	}
	switch review.TargetKind {
	case models.ConflictTargetPerson:
		s.emitter.EmitMerged(ctx, review.ExistingUUID, "", review.UUID)
	case models.ConflictTargetOrganization:
		// Skip — `merged` is conventionally a Person-scoped activity.
		// Phase 4+ may add a per-Organization merged kind when the
		// schema needs it.
	}
}

// Dismiss closes the review without applying anything to the existing
// record + without committing the parked activities. Used for false-
// positive matcher hits.
func (s *ConflictReviewService) Dismiss(ctx context.Context, reviewUUID string, in DismissInput) error {
	review, err := s.reviewRepo.GetByUUID(ctx, reviewUUID)
	if err != nil {
		return err
	}
	if review.Status != models.ConflictReviewStatusPending {
		return repository.ErrConflictReviewNotPending
	}
	actor := actorUUID(ctx)
	if err := s.reviewRepo.MarkDismissed(ctx, reviewUUID, actor, in.Notes); err != nil {
		return err
	}
	s.transitionJobIfLast(ctx, review.ImportJobUUID)
	return nil
}

// applyResolution turns the chosen action into a bson patch + applies
// it to the right repository. keep_existing is a no-op; manual_merge
// uses the operator-supplied FieldOverrides; take_incoming derives
// the patch from the recorded conflicts.
func (s *ConflictReviewService) applyResolution(ctx context.Context, review *models.ConflictReview, res models.ConflictResolution) error {
	patch := bson.M{}
	switch res.Action {
	case models.ConflictActionKeepExisting:
		return nil
	case models.ConflictActionTakeIncoming:
		for _, c := range review.Conflicts {
			if c.IncomingValue == nil {
				continue
			}
			patch[c.Field] = c.IncomingValue
		}
	case models.ConflictActionManualMerge:
		for k, v := range res.FieldOverrides {
			patch[k] = v
		}
	default:
		return fmt.Errorf("%w: action %q is not applyable", ErrConflictReviewInvalid, res.Action)
	}
	if len(patch) == 0 {
		return nil
	}
	switch review.TargetKind {
	case models.ConflictTargetPerson:
		return s.personRepo.Update(ctx, review.ExistingUUID, patch)
	case models.ConflictTargetOrganization:
		return s.orgRepo.Update(ctx, review.ExistingUUID, patch)
	default:
		return fmt.Errorf("%w: unknown targetKind %q", ErrConflictReviewInvalid, review.TargetKind)
	}
}

// commitIncomingActivities walks the parked IncomingActivities map +
// turns each entry into a models.Activity submitted through
// ActivityService.Create. ActivityService validates the kind + computes
// the dedup key so re-running the resolve is idempotent.
func (s *ConflictReviewService) commitIncomingActivities(ctx context.Context, review *models.ConflictReview) error {
	for _, raw := range review.IncomingActivities {
		a, err := activityFromMap(raw, review)
		if err != nil {
			s.warnf(ctx, "marketing: decode parked activity for review %s: %v", review.UUID, err)
			continue
		}
		if _, err := s.activitySvc.Create(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

// transitionJobIfLast checks whether the resolve/dismiss we just did
// drained the last pending review for the job. If so, the job moves
// paused_for_review → done. Logged-and-swallowed errors — the review
// itself already succeeded, and a missed transition will be reconciled
// the next time someone reads the job status (the count is recomputed).
func (s *ConflictReviewService) transitionJobIfLast(ctx context.Context, jobUUID string) {
	pending, err := s.reviewRepo.CountPendingForJob(ctx, jobUUID)
	if err != nil {
		s.warnf(ctx, "marketing: count pending reviews for job %s: %v", jobUUID, err)
		return
	}
	if pending > 0 {
		return
	}
	job, err := s.jobRepo.GetByUUID(ctx, jobUUID)
	if err != nil {
		s.warnf(ctx, "marketing: fetch job for transition %s: %v", jobUUID, err)
		return
	}
	if job.Status != models.ImportJobStatusPausedForReview {
		return
	}
	if err := s.jobRepo.UpdateStatus(ctx, jobUUID, models.ImportJobStatusDone, job.Stats, ""); err != nil {
		s.warnf(ctx, "marketing: transition job to done %s: %v", jobUUID, err)
	}
}

func (s *ConflictReviewService) warnf(ctx context.Context, format string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(ctx, fmt.Sprintf(format, args...))
}

// activityFromMap is the inverse of "ActivityService persists Activity"
// for the slice the pipeline parked. The map shape mirrors the bson
// tags on models.Activity (camelCase keys). Missing personUuid is
// filled from the review's existingUuid (Person target only); other
// missing required fields surface as a validation error from
// ActivityService.Create.
func activityFromMap(raw map[string]any, review *models.ConflictReview) (*models.Activity, error) {
	if raw == nil {
		return nil, errors.New("nil activity map")
	}
	a := &models.Activity{}
	if v, ok := stringField(raw, "personUuid"); ok {
		a.PersonUUID = v
	} else if review.TargetKind == models.ConflictTargetPerson {
		a.PersonUUID = review.ExistingUUID
	}
	if v, ok := stringField(raw, "orgUuid"); ok {
		a.OrgUUID = v
	}
	if v, ok := stringField(raw, "kind"); ok {
		a.Kind = models.ActivityKind(v)
	}
	if v, ok := stringField(raw, "source"); ok {
		a.Source = models.ActivitySource(v)
	}
	if v, ok := stringField(raw, "externalId"); ok {
		a.ExternalID = v
	}
	if v, ok := raw["payload"].(map[string]any); ok {
		a.Payload = v
	}
	if v, ok := raw["occurredAt"]; ok {
		if t, err := coerceTime(v); err == nil {
			a.OccurredAt = t
		}
	}
	return a, nil
}

func stringField(m map[string]any, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok && s != ""
}

func coerceTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		return time.Parse(time.RFC3339, t)
	default:
		return time.Time{}, fmt.Errorf("unsupported time type %T", v)
	}
}

// _ pins ctxauth as imported when actorUUID is the only consumer
// elsewhere — keeps the import block honest under linter sweeps.
var _ = ctxauth.GetTenantID
