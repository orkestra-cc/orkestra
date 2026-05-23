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

// ActivityHandler exposes the Phase 2 event-log surface — the
// per-person timeline read, the manual-entry write, and the
// correction write that supersedes a prior row.
type ActivityHandler struct {
	svc *services.ActivityService
}

// NewActivityHandler binds the handler to its service.
func NewActivityHandler(svc *services.ActivityService) *ActivityHandler {
	return &ActivityHandler{svc: svc}
}

// --- DTOs ---

// ActivityRefsView mirrors models.ActivityRefs verbatim so the
// OpenAPI schema declares every optional cross-reference.
type ActivityRefsView struct {
	CampaignUUID         string `json:"campaignUuid,omitempty"`
	EventUUID            string `json:"eventUuid,omitempty"`
	FormUUID             string `json:"formUuid,omitempty"`
	ContentUUID          string `json:"contentUuid,omitempty"`
	ImportJobUUID        string `json:"importJobUuid,omitempty"`
	CardUUID             string `json:"cardUuid,omitempty"`
	CorrectsActivityUUID string `json:"correctsActivityUuid,omitempty"`
}

func toRefsView(r models.ActivityRefs) ActivityRefsView {
	return ActivityRefsView{
		CampaignUUID:         r.CampaignUUID,
		EventUUID:            r.EventUUID,
		FormUUID:             r.FormUUID,
		ContentUUID:          r.ContentUUID,
		ImportJobUUID:        r.ImportJobUUID,
		CardUUID:             r.CardUUID,
		CorrectsActivityUUID: r.CorrectsActivityUUID,
	}
}

func toRefsModel(v ActivityRefsView) models.ActivityRefs {
	return models.ActivityRefs{
		CampaignUUID:         v.CampaignUUID,
		EventUUID:            v.EventUUID,
		FormUUID:             v.FormUUID,
		ContentUUID:          v.ContentUUID,
		ImportJobUUID:        v.ImportJobUUID,
		CardUUID:             v.CardUUID,
		CorrectsActivityUUID: v.CorrectsActivityUUID,
	}
}

// ActivityView is the response shape for every read endpoint.
type ActivityView struct {
	UUID       string                `json:"uuid"`
	TenantID   string                `json:"tenantId"`
	PersonUUID string                `json:"personUuid"`
	OrgUUID    string                `json:"orgUuid,omitempty"`
	Kind       models.ActivityKind   `json:"kind"`
	OccurredAt time.Time             `json:"occurredAt"`
	RecordedAt time.Time             `json:"recordedAt"`
	Source     models.ActivitySource `json:"source"`
	Payload    map[string]any        `json:"payload,omitempty"`
	Refs       ActivityRefsView      `json:"refs,omitempty"`
	ExternalID string                `json:"externalId,omitempty"`
	CreatedBy  string                `json:"createdBy,omitempty"`
}

func toActivityView(a *models.Activity) ActivityView {
	return ActivityView{
		UUID:       a.UUID,
		TenantID:   a.TenantID,
		PersonUUID: a.PersonUUID,
		OrgUUID:    a.OrgUUID,
		Kind:       a.Kind,
		OccurredAt: a.OccurredAt,
		RecordedAt: a.RecordedAt,
		Source:     a.Source,
		Payload:    a.Payload,
		Refs:       toRefsView(a.Refs),
		ExternalID: a.ExternalID,
		CreatedBy:  a.CreatedBy,
	}
}

// ManualActivityPayload restricts the POST surface to the four kinds
// an operator can legitimately type by hand (call/meeting/note +
// corrected_by; the latter goes through /correct in practice but the
// enum check accepts it here too for completeness). The handler
// rejects every other kind with 400.
type ManualActivityPayload struct {
	PersonUUID string              `json:"personUuid" doc:"UUID of the person the activity is logged against"`
	Kind       models.ActivityKind `json:"kind" doc:"One of: call_made, meeting_held, note_added, corrected_by"`
	OccurredAt *time.Time          `json:"occurredAt,omitempty" doc:"When the event happened. Defaults to now when omitted."`
	Payload    map[string]any      `json:"payload,omitempty" doc:"Kind-specific free-form details (notes, duration, location, ...)"`
	Refs       ActivityRefsView    `json:"refs,omitempty"`
	ExternalID string              `json:"externalId,omitempty" doc:"Optional dedup key for integrations replaying manual rows; if empty the service mints one"`
}

// CorrectionPayload is the request body for POST /activities/{uuid}/correct.
type CorrectionPayload struct {
	Reason string `json:"reason" doc:"Human-readable explanation; required."`
}

// --- Request/response wrappers ---

type ListActivitiesInput struct {
	PersonID string   `path:"personId"`
	Kinds    []string `query:"kind" doc:"Filter by activity kind; repeat the param for multiple."`
	Source   string   `query:"source" doc:"Filter by source: importer | campaign_engine | webhook | manual | system"`
	Since    string   `query:"since" doc:"ISO-8601 timestamp; activities with occurredAt >= since are returned"`
	Until    string   `query:"until" doc:"ISO-8601 timestamp; activities with occurredAt < until are returned"`
	Limit    int64    `query:"limit" doc:"Page size, default 100, capped at 1000"`
	Skip     int64    `query:"skip"`
}

type ListActivitiesResponse struct {
	Body struct {
		Items []ActivityView `json:"items"`
		Meta  ListMeta       `json:"meta"`
	}
}

type CreateActivityInput struct {
	Body ManualActivityPayload
}

type CreateActivityResponse struct {
	Body ActivityView
}

type CorrectActivityInput struct {
	ID   string `path:"id"`
	Body CorrectionPayload
}

type CorrectActivityResponse struct {
	Body ActivityView
}

// ListCorrectionsInput is the request shape of GET /v1/marketing/activities/{id}/corrections.
type ListCorrectionsInput struct {
	ID string `path:"id"`
}

// CorrectionEntryView mirrors services.CorrectionEntry on the wire.
// Kept distinct from the service type so OpenAPI rendering stays
// inside the handlers package.
type CorrectionEntryView struct {
	CorrectingActivityUUID string    `json:"correctingActivityUuid"`
	RecordedAt             time.Time `json:"recordedAt"`
	RecordedBy             string    `json:"recordedBy,omitempty"`
	Reason                 string    `json:"reason"`
}

type ListCorrectionsResponse struct {
	Body struct {
		Items []CorrectionEntryView `json:"items"`
	}
}

// --- Handler methods ---

// ListForPerson serves GET /v1/marketing/persons/{personId}/activities.
// Returns the timeline ordered by OccurredAt descending (newest
// first). Kinds + source are server-side filters; since/until clamp
// the window.
func (h *ActivityHandler) ListForPerson(ctx context.Context, in *ListActivitiesInput) (*ListActivitiesResponse, error) {
	filter := repository.ActivityListFilter{
		Limit: in.Limit,
		Skip:  in.Skip,
	}
	if in.Source != "" {
		filter.Source = models.ActivitySource(in.Source)
		if !models.IsKnownSource(filter.Source) {
			return nil, huma.Error400BadRequest("unknown source: " + in.Source)
		}
	}
	for _, k := range in.Kinds {
		kind := models.ActivityKind(k)
		if !models.IsKnownKind(kind) {
			return nil, huma.Error400BadRequest("unknown kind: " + k)
		}
		filter.Kinds = append(filter.Kinds, kind)
	}
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339, in.Since)
		if err != nil {
			return nil, huma.Error400BadRequest("since: invalid RFC3339 timestamp")
		}
		filter.Since = t
	}
	if in.Until != "" {
		t, err := time.Parse(time.RFC3339, in.Until)
		if err != nil {
			return nil, huma.Error400BadRequest("until: invalid RFC3339 timestamp")
		}
		filter.Until = t
	}

	got, err := h.svc.ListForPerson(ctx, in.PersonID, filter)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]ActivityView, 0, len(got))
	for i := range got {
		items = append(items, toActivityView(&got[i]))
	}
	resp := &ListActivitiesResponse{}
	resp.Body.Items = items
	resp.Body.Meta = ListMeta{Limit: in.Limit, Skip: in.Skip, Count: len(items)}
	return resp, nil
}

// CreateManual serves POST /v1/marketing/activities. The kind is
// restricted to ManualKinds — operators can't forge an email_opened
// by hand because that would break the dedup_key invariant for ESP
// webhook ingestion. The corrected_by kind is accepted here too but
// /activities/{uuid}/correct is the preferred path because it
// pre-fills the refs.
func (h *ActivityHandler) CreateManual(ctx context.Context, in *CreateActivityInput) (*CreateActivityResponse, error) {
	if in.Body.PersonUUID == "" {
		return nil, huma.Error400BadRequest("personUuid is required")
	}
	if !models.IsKnownKind(in.Body.Kind) {
		return nil, huma.Error400BadRequest("unknown kind: " + string(in.Body.Kind))
	}
	if !models.IsManualKind(in.Body.Kind) {
		return nil, huma.Error400BadRequest("kind not allowed for manual creation: " + string(in.Body.Kind))
	}

	a := &models.Activity{
		PersonUUID: in.Body.PersonUUID,
		Kind:       in.Body.Kind,
		Payload:    in.Body.Payload,
		Refs:       toRefsModel(in.Body.Refs),
		ExternalID: in.Body.ExternalID,
		Source:     models.ActivitySourceManual,
	}
	if in.Body.OccurredAt != nil {
		a.OccurredAt = *in.Body.OccurredAt
	}

	got, err := h.svc.Create(ctx, a)
	if err != nil {
		if errors.Is(err, models.ErrInvalidActivity) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreateActivityResponse{Body: toActivityView(got)}, nil
}

// ListCorrections serves GET /v1/marketing/activities/{id}/corrections.
// Returns every corrected_by row that supersedes the activity,
// ordered oldest-first. Powers the Timeline tab's "↻ corrected"
// expander; folds under the same marketing.contact.read gate as the
// timeline list itself (no separate permission needed).
func (h *ActivityHandler) ListCorrections(ctx context.Context, in *ListCorrectionsInput) (*ListCorrectionsResponse, error) {
	got, err := h.svc.ListCorrectionsForActivity(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]CorrectionEntryView, 0, len(got))
	for _, e := range got {
		items = append(items, CorrectionEntryView{
			CorrectingActivityUUID: e.CorrectingActivityUUID,
			RecordedAt:             e.RecordedAt,
			RecordedBy:             e.RecordedBy,
			Reason:                 e.Reason,
		})
	}
	resp := &ListCorrectionsResponse{}
	resp.Body.Items = items
	return resp, nil
}

// Correct serves POST /v1/marketing/activities/{id}/correct.
// Inserts a KindCorrectedBy row pointing at the original via
// refs.correctsActivityUuid; the next eager + nightly recompute
// de-applies the original from the snapshot.
func (h *ActivityHandler) Correct(ctx context.Context, in *CorrectActivityInput) (*CorrectActivityResponse, error) {
	if in.Body.Reason == "" {
		return nil, huma.Error400BadRequest("reason is required")
	}
	got, err := h.svc.Correct(ctx, in.ID, in.Body.Reason)
	if err != nil {
		if errors.Is(err, repository.ErrActivityNotFound) {
			return nil, huma.Error404NotFound("activity not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CorrectActivityResponse{Body: toActivityView(got)}, nil
}

// --- Route registration ---

// RegisterActivityReadRoutes — folds into the `marketing.contact.read`
// gate (Phase 2 plan §2.3): any operator who can see contacts must
// also see the activity timeline that explains their score.
func RegisterActivityReadRoutes(api huma.API, h *ActivityHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-person-activities",
		Method:      http.MethodGet, Path: "/v1/marketing/persons/{personId}/activities",
		Summary: "List activities for a person", Tags: []string{"Marketing - Activities"},
	}, h.ListForPerson)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-activity-corrections",
		Method:      http.MethodGet, Path: "/v1/marketing/activities/{id}/corrections",
		Summary: "List corrected_by rows superseding the activity, oldest-first",
		Tags:    []string{"Marketing - Activities"},
	}, h.ListCorrections)
}

// RegisterActivityWriteRoutes — gate with `marketing.activity.write`.
// Distinct bucket from `marketing.contact.write` because logging
// real-world touchpoints (a call, a meeting note) is a different
// authority than editing the contact record itself.
func RegisterActivityWriteRoutes(api huma.API, h *ActivityHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-create-activity",
		Method:      http.MethodPost, Path: "/v1/marketing/activities",
		Summary:       "Log a manual activity (call, meeting, note, correction)",
		Tags:          []string{"Marketing - Activities"},
		DefaultStatus: http.StatusCreated,
	}, h.CreateManual)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-correct-activity",
		Method:      http.MethodPost, Path: "/v1/marketing/activities/{id}/correct",
		Summary:       "Insert a corrected_by row that supersedes the activity",
		Tags:          []string{"Marketing - Activities"},
		DefaultStatus: http.StatusCreated,
	}, h.Correct)
}
