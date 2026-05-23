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
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ActivityListener is the callback registered on ActivityService for
// downstream consumers (notably ScoreService.OnActivityInserted for
// the eager-recompute hook). Listeners receive the persisted Activity
// after Create succeeds; the activity is passed by value so listeners
// can't mutate the canonical row.
type ActivityListener func(ctx context.Context, activity models.Activity)

// ActivityService is the write boundary of the append-only event log.
// Responsibilities:
//
//   - Validate the activity payload against the static enum.
//   - Mint a UUID if the caller did not supply one.
//   - Derive OrgUUID from the person's primary+active membership
//     (decision D12) so the per-org analytics query stays fast.
//   - Compute DedupKey (decision D21) — the unique index in the
//     repository deduplicates re-imports server-side.
//   - Fire listeners (ScoreService eager hook today; agents-addon
//     scoring listeners in Phase 5) synchronously, with panic
//     recovery so a listener failure does not roll back the source
//     mutation.
//
// Read paths (List, Get) are thin pass-throughs — the timeline
// handler in PR-4 layers pagination on top.
type ActivityService struct {
	repo       *repository.ActivityRepository
	memberRepo *repository.MembershipRepository
	logger     *slog.Logger

	listenersMu sync.RWMutex
	listeners   []ActivityListener
}

// NewActivityService wires the service. The membership repository is
// needed by Create to look up the person's primary+active membership
// at write time (the OrgUUID denormalisation flow).
func NewActivityService(repo *repository.ActivityRepository, members *repository.MembershipRepository, logger *slog.Logger) *ActivityService {
	return &ActivityService{repo: repo, memberRepo: members, logger: logger}
}

// RegisterListener appends a callback to the listener slice. Called
// during Module.Init to wire ScoreService.OnActivityInserted; safe
// to call from multiple goroutines but typically a single-shot at
// boot.
func (s *ActivityService) RegisterListener(l ActivityListener) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	s.listeners = append(s.listeners, l)
}

// Create persists the activity and notifies listeners. Returns the
// persisted Activity (UUID + derived fields filled in) so the handler
// can render it back to the operator.
//
// Idempotence: the repository swallows E11000 on dedupKey collision
// (decision D21), so re-creating the same activity is a no-op
// success. In that case listeners are still fired against the
// freshly-built activity — that's an explicit trade-off: a re-import
// of an old activity should NOT trigger a recompute, but distinguishing
// that case here requires a pre-check Mongo round-trip per write.
// PR-3 keeps the simpler "always fire" semantics because the eager
// recompute is itself idempotent (same activities → same score).
func (s *ActivityService) Create(ctx context.Context, a *models.Activity) (*models.Activity, error) {
	if a == nil {
		return nil, errors.New("marketing: nil activity")
	}

	if a.UUID == "" {
		a.UUID = uuid.New().String()
	}
	if a.OccurredAt.IsZero() {
		a.OccurredAt = time.Now().UTC()
	}
	if a.RecordedAt.IsZero() {
		a.RecordedAt = time.Now().UTC()
	}

	// Manual activities mint an ExternalID so the DedupKey unique
	// index doesn't fold two distinct hand-typed entries into one.
	// Importer / webhook paths supply ExternalID themselves.
	if a.Source == models.ActivitySourceManual && a.ExternalID == "" {
		a.ExternalID = "manual:" + uuid.New().String()
	}

	// CreatedBy from caller context when not set explicitly.
	if a.CreatedBy == "" {
		if actor, ok := ctxauth.GetUserUUID(ctx); ok {
			a.CreatedBy = actor
		}
	}

	// Validate before any DB work so a malformed kind never reaches
	// the unique index.
	if err := a.Validate(); err != nil {
		return nil, err
	}

	// Derive OrgUUID from the person's primary+active membership when
	// the caller hasn't already populated it (importers may already
	// know the org and stamp it directly).
	if a.OrgUUID == "" {
		mship, err := s.memberRepo.FindActivePrimaryForPerson(ctx, a.PersonUUID)
		if err == nil && mship != nil {
			a.OrgUUID = mship.OrgUUID
		}
		// FindActivePrimaryForPerson missing-membership case returns
		// no error and (nil, nil) — leaving OrgUUID empty is correct
		// for unaffiliated persons.
	}

	// Compute DedupKey last so it incorporates the final OccurredAt /
	// ExternalID values.
	if a.DedupKey == "" {
		a.DedupKey = models.ComputeDedupKey(a.PersonUUID, a.Kind, a.OccurredAt, a.ExternalID)
	}

	if err := s.repo.Create(ctx, a); err != nil {
		return nil, err
	}

	s.notifyListeners(ctx, *a)
	return a, nil
}

// notifyListeners iterates listeners with panic recovery. A listener
// crash is logged but never propagated — activity-log writes must
// not roll back because a downstream recompute hit a corrupt profile.
func (s *ActivityService) notifyListeners(ctx context.Context, a models.Activity) {
	s.listenersMu.RLock()
	listeners := make([]ActivityListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.listenersMu.RUnlock()

	for _, listener := range listeners {
		s.invokeListener(ctx, listener, a)
	}
}

func (s *ActivityService) invokeListener(ctx context.Context, listener ActivityListener, a models.Activity) {
	defer func() {
		if r := recover(); r != nil && s.logger != nil {
			s.logger.ErrorContext(ctx, "marketing: activity listener panic",
				slog.String("activityUuid", a.UUID),
				slog.Any("panic", r),
			)
		}
	}()
	listener(ctx, a)
}

// Get returns the activity by UUID in the caller's tenant.
func (s *ActivityService) Get(ctx context.Context, uuid string) (*models.Activity, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

// Correct inserts a KindCorrectedBy row pointing at originalUUID. The
// scoring engine de-applies the original on the next recompute (the
// correction itself contributes zero). Returns the newly-created
// correction row.
//
// The original activity is looked up first to:
//   - 404 cleanly when the UUID doesn't exist in the tenant.
//   - Stamp the correction's PersonUUID + OrgUUID from the original
//     so the timeline view of the corrected person shows both rows.
//
// A reason is required — corrections without explanation are
// audit-hostile and the scoring engine doesn't surface them in any
// useful way.
func (s *ActivityService) Correct(ctx context.Context, originalUUID, reason string) (*models.Activity, error) {
	if originalUUID == "" {
		return nil, errors.New("marketing: empty originalUuid for correction")
	}
	if reason == "" {
		return nil, errors.New("marketing: correction reason is required")
	}
	original, err := s.repo.GetByUUID(ctx, originalUUID)
	if err != nil {
		return nil, err
	}
	correction := &models.Activity{
		PersonUUID: original.PersonUUID,
		OrgUUID:    original.OrgUUID, // pin to the historical org, not the current one
		Kind:       models.KindCorrectedBy,
		OccurredAt: time.Now().UTC(),
		Source:     models.ActivitySourceManual,
		Payload: map[string]any{
			"reason":             reason,
			"previousActivityId": originalUUID,
		},
		Refs: models.ActivityRefs{CorrectsActivityUUID: originalUUID},
	}
	return s.Create(ctx, correction)
}

// ListForPerson returns the activity timeline for the given person.
// Filter parameters layer on top of the per-person scope (kinds,
// source, since/until, pagination).
func (s *ActivityService) ListForPerson(ctx context.Context, personUUID string, f repository.ActivityListFilter) ([]models.Activity, error) {
	f.PersonUUID = personUUID
	return s.repo.List(ctx, f)
}

// CorrectionEntry is the per-correction projection the contact-detail
// Timeline expander renders against a `corrected_by`-chained activity.
// Each entry refers to one correcting Activity (Kind == KindCorrectedBy
// with refs.correctsActivityUuid pointing back at the original) and
// surfaces just the fields the UI sidebar needs — keep the shape
// stable so the React renderer doesn't have to walk Activity.Payload
// blindly.
type CorrectionEntry struct {
	CorrectingActivityUUID string    `json:"correctingActivityUuid"`
	RecordedAt             time.Time `json:"recordedAt"`
	RecordedBy             string    `json:"recordedBy,omitempty"`
	Reason                 string    `json:"reason"`
}

// ListCorrectionsForActivity returns every CorrectionEntry that
// supersedes the original activity, ordered oldest-first. The
// scoring engine already excludes the original via the
// refs.correctsActivityUuid index; this helper is purely for the
// audit-trail surface in the Timeline UI.
//
// A missing reason in the payload surfaces as an empty string — the
// API contract requires a reason on Correct() but Phase 2 rows from
// before the requirement may carry an empty value; the caller can
// render "(no reason recorded)" rather than have this method reject.
func (s *ActivityService) ListCorrectionsForActivity(ctx context.Context, originalUUID string) ([]CorrectionEntry, error) {
	rows, err := s.repo.ListCorrectionsForActivity(ctx, originalUUID)
	if err != nil {
		return nil, err
	}
	out := make([]CorrectionEntry, 0, len(rows))
	for i := range rows {
		entry := CorrectionEntry{
			CorrectingActivityUUID: rows[i].UUID,
			RecordedAt:             rows[i].RecordedAt,
			RecordedBy:             rows[i].CreatedBy,
		}
		if reason, ok := rows[i].Payload["reason"].(string); ok {
			entry.Reason = reason
		}
		out = append(out, entry)
	}
	return out, nil
}
