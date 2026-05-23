package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
)

// SnapshotHandler exposes read-only access to marketing_score_snapshots:
// the per-person score listing (every active profile's snapshot for
// the contact) and the single-snapshot read used by the breakdown
// drawer in the admin UI.
//
// Both endpoints serve cached data — the engine + ScoreService have
// already produced the rows we surface here. No service indirection
// because the read shape maps 1:1 onto the repository.
type SnapshotHandler struct {
	snapRepo *repository.ScoreSnapshotRepository
}

// NewSnapshotHandler binds the handler.
func NewSnapshotHandler(snapRepo *repository.ScoreSnapshotRepository) *SnapshotHandler {
	return &SnapshotHandler{snapRepo: snapRepo}
}

// --- DTOs ---

// BreakdownEntryView mirrors models.BreakdownEntry. The UI's
// "why does Jane have 95.5?" drawer renders these as a table — one
// row per activity that contributed (plus a synthetic "aggregate"
// row when the activityBreakdownMax cap was hit).
type BreakdownEntryView struct {
	ActivityUUID      string              `json:"activityUuid"`
	ActivityKind      models.ActivityKind `json:"activityKind"`
	OccurredAt        time.Time           `json:"occurredAt"`
	RuleIndex         int                 `json:"ruleIndex"`
	RawPoints         float64             `json:"rawPoints"`
	AppliedDecay      float64             `json:"appliedDecay"`
	PointsContributed float64             `json:"pointsContributed"`
}

func toBreakdownView(b models.BreakdownEntry) BreakdownEntryView {
	return BreakdownEntryView{
		ActivityUUID:      b.ActivityUUID,
		ActivityKind:      b.ActivityKind,
		OccurredAt:        b.OccurredAt,
		RuleIndex:         b.RuleIndex,
		RawPoints:         b.RawPoints,
		AppliedDecay:      b.AppliedDecay,
		PointsContributed: b.PointsContributed,
	}
}

// SnapshotView is the response shape for both single-snapshot reads
// and per-person listings.
type SnapshotView struct {
	UUID           string               `json:"uuid"`
	TenantID       string               `json:"tenantId"`
	PersonUUID     string               `json:"personUuid"`
	ProfileUUID    string               `json:"profileUuid"`
	ProfileVersion int                  `json:"profileVersion"`
	Value          float64              `json:"value"`
	Breakdown      []BreakdownEntryView `json:"breakdown,omitempty"`
	AsOf           time.Time            `json:"asOf"`
	ComputedAt     time.Time            `json:"computedAt"`
	Applicable     bool                 `json:"applicable"`
	Stale          bool                 `json:"stale"`
	ActivityCount  int                  `json:"activityCount"`
	LastActivityAt *time.Time           `json:"lastActivityAt,omitempty"`
}

func toSnapshotView(s *models.ScoreSnapshot) SnapshotView {
	breakdown := make([]BreakdownEntryView, 0, len(s.Breakdown))
	for _, b := range s.Breakdown {
		breakdown = append(breakdown, toBreakdownView(b))
	}
	return SnapshotView{
		UUID:           s.UUID,
		TenantID:       s.TenantID,
		PersonUUID:     s.PersonUUID,
		ProfileUUID:    s.ProfileUUID,
		ProfileVersion: s.ProfileVersion,
		Value:          s.Value,
		Breakdown:      breakdown,
		AsOf:           s.AsOf,
		ComputedAt:     s.ComputedAt,
		Applicable:     s.Applicable,
		Stale:          s.Stale,
		ActivityCount:  s.ActivityCount,
		LastActivityAt: s.LastActivityAt,
	}
}

// --- Request/response wrappers ---

type GetSnapshotInput struct {
	ID string `path:"id"`
}

type GetSnapshotResponse struct {
	Body SnapshotView
}

type ListPersonScoresInput struct {
	PersonID string `path:"personId"`
}

type ListPersonScoresResponse struct {
	Body struct {
		Items []SnapshotView `json:"items"`
	}
}

// --- Handler methods ---

// Get serves GET /v1/marketing/score-snapshots/{id}. Returns the
// snapshot with full breakdown — the breakdown drawer's data source.
func (h *SnapshotHandler) Get(ctx context.Context, in *GetSnapshotInput) (*GetSnapshotResponse, error) {
	got, err := h.snapRepo.GetByUUID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrScoreSnapshotNotFound) {
			return nil, huma.Error404NotFound("score snapshot not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetSnapshotResponse{Body: toSnapshotView(got)}, nil
}

// ListForPerson serves GET /v1/marketing/persons/{personId}/scores.
// Returns every snapshot for the person, one per profile. Ordering
// is unspecified at the API level — the UI sorts client-side by
// value or by profile name as the operator chooses.
func (h *SnapshotHandler) ListForPerson(ctx context.Context, in *ListPersonScoresInput) (*ListPersonScoresResponse, error) {
	got, err := h.snapRepo.ListForPerson(ctx, in.PersonID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]SnapshotView, 0, len(got))
	for i := range got {
		items = append(items, toSnapshotView(&got[i]))
	}
	resp := &ListPersonScoresResponse{}
	resp.Body.Items = items
	return resp, nil
}

// --- Route registration ---

// RegisterSnapshotReadRoutes — folds into `marketing.contact.read`
// (Phase 2 plan §2.3). Snapshots have no write surface — the engine
// produces them, never an operator directly. Stale-flipping happens
// indirectly via ScoreProfileService.Save; recomputes happen via
// the eager listener or the nightly job.
func RegisterSnapshotReadRoutes(api huma.API, h *SnapshotHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-score-snapshot",
		Method:      http.MethodGet, Path: "/v1/marketing/score-snapshots/{id}",
		Summary: "Get a single score snapshot with full breakdown",
		Tags:    []string{"Marketing - Scoring"},
	}, h.Get)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-person-scores",
		Method:      http.MethodGet, Path: "/v1/marketing/persons/{personId}/scores",
		Summary: "List all score snapshots for a person across profiles",
		Tags:    []string{"Marketing - Scoring"},
	}, h.ListForPerson)
}
