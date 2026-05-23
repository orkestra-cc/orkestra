package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
)

// ScoreProfileHandler exposes CRUD on marketing_score_profiles + the
// per-profile leaderboard read.
//
// Two services are injected:
//   - ScoreProfileService for the profile CRUD.
//   - ScoreService for the leaderboard read, which surfaces the cache
//     directly (snapshot read), so it lives on the snapshot
//     repository — exposed through ScoreService for symmetry with
//     the other Phase 2 surfaces.
type ScoreProfileHandler struct {
	svc      *services.ScoreProfileService
	snapRepo *repository.ScoreSnapshotRepository
}

// NewScoreProfileHandler binds the handler.
func NewScoreProfileHandler(svc *services.ScoreProfileService, snapRepo *repository.ScoreSnapshotRepository) *ScoreProfileHandler {
	return &ScoreProfileHandler{svc: svc, snapRepo: snapRepo}
}

// --- DTOs ---

// DecayView mirrors models.DecayFn 1:1. Pointer-typed fields stay
// pointer-typed so the OpenAPI schema captures the "omit means
// inherit" semantics.
type DecayView struct {
	Fn           string `json:"fn" doc:"One of: none, linear, exponential"`
	WindowDays   *int   `json:"windowDays,omitempty" doc:"linear: contribution drops to 0 after N days"`
	HalfLifeDays *int   `json:"halfLifeDays,omitempty" doc:"exponential: contribution halves every N days"`
}

func toDecayView(d *models.DecayFn) *DecayView {
	if d == nil {
		return nil
	}
	return &DecayView{Fn: d.Fn, WindowDays: d.WindowDays, HalfLifeDays: d.HalfLifeDays}
}

func toDecayModel(v *DecayView) *models.DecayFn {
	if v == nil {
		return nil
	}
	return &models.DecayFn{Fn: v.Fn, WindowDays: v.WindowDays, HalfLifeDays: v.HalfLifeDays}
}

// RuleView mirrors models.ScoreRule. ActivityKind is typed `any` to
// preserve the polymorphic shape (string | []string | "*").
type RuleView struct {
	ActivityKind any            `json:"activityKind" doc:"string, []string, or '*' wildcard"`
	MatchPayload map[string]any `json:"matchPayload,omitempty"`
	Points       float64        `json:"points"`
	Decay        *DecayView     `json:"decay,omitempty"`
	Cap          *float64       `json:"cap,omitempty"`
	WindowDays   *int           `json:"windowDays,omitempty"`
}

func toRuleView(r models.ScoreRule) RuleView {
	return RuleView{
		ActivityKind: r.ActivityKind,
		MatchPayload: r.MatchPayload,
		Points:       r.Points,
		Decay:        toDecayView(r.Decay),
		Cap:          r.Cap,
		WindowDays:   r.WindowDays,
	}
}

func toRuleModel(v RuleView) models.ScoreRule {
	return models.ScoreRule{
		ActivityKind: v.ActivityKind,
		MatchPayload: v.MatchPayload,
		Points:       v.Points,
		Decay:        toDecayModel(v.Decay),
		Cap:          v.Cap,
		WindowDays:   v.WindowDays,
	}
}

// FilterView mirrors models.ProfileFilter.
type FilterView struct {
	TagsInclude        []string       `json:"tagsInclude,omitempty"`
	TagsExclude        []string       `json:"tagsExclude,omitempty"`
	CustomFieldFilters map[string]any `json:"customFieldFilters,omitempty"`
}

func toFilterView(f *models.ProfileFilter) *FilterView {
	if f == nil {
		return nil
	}
	return &FilterView{
		TagsInclude:        f.TagsInclude,
		TagsExclude:        f.TagsExclude,
		CustomFieldFilters: f.CustomFieldFilters,
	}
}

func toFilterModel(v *FilterView) *models.ProfileFilter {
	if v == nil {
		return nil
	}
	return &models.ProfileFilter{
		TagsInclude:        v.TagsInclude,
		TagsExclude:        v.TagsExclude,
		CustomFieldFilters: v.CustomFieldFilters,
	}
}

// ScoreProfilePayload is the request body for create + replace.
type ScoreProfilePayload struct {
	Name         string      `json:"name" doc:"Slug-like identifier, unique per tenant"`
	Description  string      `json:"description,omitempty"`
	Active       bool        `json:"active"`
	Rules        []RuleView  `json:"rules"`
	Filters      *FilterView `json:"filters,omitempty"`
	DefaultDecay *DecayView  `json:"defaultDecay,omitempty"`
}

// ScoreProfileView is the response shape.
type ScoreProfileView struct {
	UUID         string      `json:"uuid"`
	TenantID     string      `json:"tenantId"`
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	Active       bool        `json:"active"`
	Rules        []RuleView  `json:"rules"`
	Filters      *FilterView `json:"filters,omitempty"`
	DefaultDecay *DecayView  `json:"defaultDecay,omitempty"`
	Version      int         `json:"version"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
	CreatedBy    string      `json:"createdBy,omitempty"`
	UpdatedBy    string      `json:"updatedBy,omitempty"`
}

func toScoreProfileView(p *models.ScoreProfile) ScoreProfileView {
	rules := make([]RuleView, 0, len(p.Rules))
	for _, r := range p.Rules {
		rules = append(rules, toRuleView(r))
	}
	return ScoreProfileView{
		UUID:         p.UUID,
		TenantID:     p.TenantID,
		Name:         p.Name,
		Description:  p.Description,
		Active:       p.Active,
		Rules:        rules,
		Filters:      toFilterView(p.Filters),
		DefaultDecay: toDecayView(p.DefaultDecay),
		Version:      p.Version,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
		CreatedBy:    p.CreatedBy,
		UpdatedBy:    p.UpdatedBy,
	}
}

func toScoreProfileModel(payload ScoreProfilePayload) *models.ScoreProfile {
	rules := make([]models.ScoreRule, 0, len(payload.Rules))
	for _, r := range payload.Rules {
		rules = append(rules, toRuleModel(r))
	}
	return &models.ScoreProfile{
		Name:         payload.Name,
		Description:  payload.Description,
		Active:       payload.Active,
		Rules:        rules,
		Filters:      toFilterModel(payload.Filters),
		DefaultDecay: toDecayModel(payload.DefaultDecay),
	}
}

// --- Request/response wrappers ---

type ListScoreProfilesInput struct {
	ActiveOnly bool `query:"activeOnly" doc:"When true, only profiles with active=true are returned"`
}

type ListScoreProfilesResponse struct {
	Body struct {
		Items []ScoreProfileView `json:"items"`
	}
}

type GetScoreProfileInput struct {
	ID string `path:"id"`
}

type GetScoreProfileResponse struct {
	Body ScoreProfileView
}

type CreateScoreProfileInput struct {
	Body ScoreProfilePayload
}

type CreateScoreProfileResponse struct {
	Body ScoreProfileView
}

type ReplaceScoreProfileInput struct {
	ID   string `path:"id"`
	Body ScoreProfilePayload
}

type ReplaceScoreProfileResponse struct {
	Body ScoreProfileView
}

type DeleteScoreProfileInput struct {
	ID string `path:"id"`
}

type LeaderboardInput struct {
	ID             string `path:"id"`
	ApplicableOnly bool   `query:"applicableOnly" doc:"Skip rows where the person fails the profile filter (applicable=false). Default false."`
	Limit          int64  `query:"limit" doc:"Default 50, capped at 500"`
	Skip           int64  `query:"skip"`
}

type LeaderboardEntryView struct {
	UUID           string     `json:"uuid"`
	PersonUUID     string     `json:"personUuid"`
	ProfileUUID    string     `json:"profileUuid"`
	ProfileVersion int        `json:"profileVersion"`
	Value          float64    `json:"value"`
	Applicable     bool       `json:"applicable"`
	Stale          bool       `json:"stale"`
	ActivityCount  int        `json:"activityCount"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	AsOf           time.Time  `json:"asOf"`
	ComputedAt     time.Time  `json:"computedAt"`
}

func toLeaderboardEntry(s *models.ScoreSnapshot) LeaderboardEntryView {
	return LeaderboardEntryView{
		UUID:           s.UUID,
		PersonUUID:     s.PersonUUID,
		ProfileUUID:    s.ProfileUUID,
		ProfileVersion: s.ProfileVersion,
		Value:          s.Value,
		Applicable:     s.Applicable,
		Stale:          s.Stale,
		ActivityCount:  s.ActivityCount,
		LastActivityAt: s.LastActivityAt,
		AsOf:           s.AsOf,
		ComputedAt:     s.ComputedAt,
	}
}

type LeaderboardResponse struct {
	Body struct {
		Items []LeaderboardEntryView `json:"items"`
		Meta  ListMeta               `json:"meta"`
	}
}

// --- Handler methods ---

func (h *ScoreProfileHandler) List(ctx context.Context, in *ListScoreProfilesInput) (*ListScoreProfilesResponse, error) {
	got, err := h.svc.List(ctx, in.ActiveOnly)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]ScoreProfileView, 0, len(got))
	for i := range got {
		items = append(items, toScoreProfileView(&got[i]))
	}
	resp := &ListScoreProfilesResponse{}
	resp.Body.Items = items
	return resp, nil
}

func (h *ScoreProfileHandler) Get(ctx context.Context, in *GetScoreProfileInput) (*GetScoreProfileResponse, error) {
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrScoreProfileNotFound) {
			return nil, huma.Error404NotFound("score profile not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetScoreProfileResponse{Body: toScoreProfileView(got)}, nil
}

func (h *ScoreProfileHandler) Create(ctx context.Context, in *CreateScoreProfileInput) (*CreateScoreProfileResponse, error) {
	p := toScoreProfileModel(in.Body)
	got, err := h.svc.Create(ctx, p)
	if err != nil {
		if errors.Is(err, services.ErrScoreProfileInvalid) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreateScoreProfileResponse{Body: toScoreProfileView(got)}, nil
}

// Replace is mounted as PATCH for surface symmetry with the rest of
// the marketing addon, but the semantics are full-replace
// (ScoreProfileService.Save runs ReplaceOne under the hood). Rule
// arrays are full-replace because operators editing rules expect
// the new list to be authoritative.
func (h *ScoreProfileHandler) Replace(ctx context.Context, in *ReplaceScoreProfileInput) (*ReplaceScoreProfileResponse, error) {
	p := toScoreProfileModel(in.Body)
	p.UUID = in.ID
	got, err := h.svc.Save(ctx, p)
	if err != nil {
		if errors.Is(err, repository.ErrScoreProfileNotFound) {
			return nil, huma.Error404NotFound("score profile not found")
		}
		if errors.Is(err, services.ErrScoreProfileInvalid) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &ReplaceScoreProfileResponse{Body: toScoreProfileView(got)}, nil
}

func (h *ScoreProfileHandler) Delete(ctx context.Context, in *DeleteScoreProfileInput) (*SuccessResponse, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		if errors.Is(err, repository.ErrScoreProfileNotFound) {
			return nil, huma.Error404NotFound("score profile not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &SuccessResponse{}
	resp.Body.Success = true
	return resp, nil
}

// Leaderboard reads marketing_score_snapshots directly via the
// repository — bypassing the service layer is intentional here:
// snapshots are a cache, and the leaderboard is the canonical
// read-only projection of that cache. Going through ScoreService
// would add a round-trip without changing the query.
func (h *ScoreProfileHandler) Leaderboard(ctx context.Context, in *LeaderboardInput) (*LeaderboardResponse, error) {
	got, err := h.snapRepo.Leaderboard(ctx, repository.LeaderboardFilter{
		ProfileUUID:    in.ID,
		ApplicableOnly: in.ApplicableOnly,
		Limit:          in.Limit,
		Skip:           in.Skip,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]LeaderboardEntryView, 0, len(got))
	for i := range got {
		items = append(items, toLeaderboardEntry(&got[i]))
	}
	resp := &LeaderboardResponse{}
	resp.Body.Items = items
	resp.Body.Meta = ListMeta{Limit: in.Limit, Skip: in.Skip, Count: len(items)}
	return resp, nil
}

// --- Route registration ---

// RegisterScoreProfileReadRoutes — folds into `marketing.contact.read`.
// Read access to scoring rides on contact-read because the two are
// semantically coupled (an operator who can see a contact must also
// see how they rank).
func RegisterScoreProfileReadRoutes(api huma.API, h *ScoreProfileHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-score-profiles",
		Method:      http.MethodGet, Path: "/v1/marketing/score-profiles",
		Summary: "List score profiles", Tags: []string{"Marketing - Scoring"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-score-profile",
		Method:      http.MethodGet, Path: "/v1/marketing/score-profiles/{id}",
		Summary: "Get a score profile", Tags: []string{"Marketing - Scoring"},
	}, h.Get)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-score-profile-leaderboard",
		Method:      http.MethodGet, Path: "/v1/marketing/score-profiles/{id}/leaderboard",
		Summary: "Top-scoring persons for a profile, ordered by value descending",
		Tags:    []string{"Marketing - Scoring"},
	}, h.Leaderboard)
}

// RegisterScoreProfileWriteRoutes — gate with
// `marketing.score_profile.write`. Save bumps the profile version
// and bulk-marks every downstream snapshot as stale; the nightly
// job + the next eager hit on each person settle the recompute.
func RegisterScoreProfileWriteRoutes(api huma.API, h *ScoreProfileHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-create-score-profile",
		Method:      http.MethodPost, Path: "/v1/marketing/score-profiles",
		Summary: "Create a score profile", Tags: []string{"Marketing - Scoring"},
		DefaultStatus: http.StatusCreated,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-replace-score-profile",
		Method:      http.MethodPatch, Path: "/v1/marketing/score-profiles/{id}",
		Summary: "Replace a score profile (full-body); bumps version and invalidates snapshots",
		Tags:    []string{"Marketing - Scoring"},
	}, h.Replace)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-delete-score-profile",
		Method:      http.MethodDelete, Path: "/v1/marketing/score-profiles/{id}",
		Summary: "Delete a score profile and cascade-delete its snapshots",
		Tags:    []string{"Marketing - Scoring"},
	}, h.Delete)
}
