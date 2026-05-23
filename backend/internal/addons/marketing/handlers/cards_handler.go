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

// CardHandler exposes the §7 card lifecycle to HTTP. Reads on a card
// or on the per-person cards list fold into marketing.contact.read.
// Writes split across three permissions so the operator-role catalog
// can match real-world separation of duties:
//
//	marketing.card.issue   → POST /v1/marketing/persons/{personUuid}/cards
//	marketing.card.suspend → POST /v1/marketing/cards/{id}/suspend
//	                         POST /v1/marketing/cards/{id}/reinstate (inverse)
//	marketing.card.revoke  → POST /v1/marketing/cards/{id}/revoke    (terminal)
type CardHandler struct {
	svc *services.CardService
}

// NewCardHandler binds the handler.
func NewCardHandler(svc *services.CardService) *CardHandler {
	return &CardHandler{svc: svc}
}

// --- DTOs ---

// IssueCardPayload is the body for POST /persons/{id}/cards. The
// personUuid is taken from the URL — not the body — so the route
// alone scopes the operation to a person.
type IssueCardPayload struct {
	CardTypeUUID string     `json:"cardTypeUuid" doc:"Card type to emit"`
	Tier         string     `json:"tier,omitempty" doc:"Required when the type's tiers list is non-empty"`
	Benefits     []string   `json:"benefits,omitempty" doc:"Override the type's defaultBenefits for this instance"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	Notes        string     `json:"notes,omitempty"`
}

// ReasonPayload is the body shared by suspend / revoke. Reinstate
// takes no body — the state change carries no reason.
type ReasonPayload struct {
	Reason string `json:"reason,omitempty" doc:"Operator-supplied free-text reason recorded on the audit activity"`
}

// CardView is the read response shape.
type CardView struct {
	UUID          string            `json:"uuid"`
	CardTypeUUID  string            `json:"cardTypeUuid"`
	Code          string            `json:"code"`
	PersonUUID    string            `json:"personUuid"`
	Tier          string            `json:"tier,omitempty"`
	Status        models.CardStatus `json:"status"`
	Benefits      []string          `json:"benefits,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	ExpiresAt     *time.Time        `json:"expiresAt,omitempty"`
	IssuedAt      time.Time         `json:"issuedAt"`
	IssuedBy      string            `json:"issuedBy"`
	SuspendedAt   *time.Time        `json:"suspendedAt,omitempty"`
	SuspendedBy   string            `json:"suspendedBy,omitempty"`
	SuspendReason string            `json:"suspendReason,omitempty"`
	RevokedAt     *time.Time        `json:"revokedAt,omitempty"`
	RevokedBy     string            `json:"revokedBy,omitempty"`
	RevokeReason  string            `json:"revokeReason,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

func toCardView(c *models.Card) CardView {
	return CardView{
		UUID:          c.UUID,
		CardTypeUUID:  c.CardTypeUUID,
		Code:          c.Code,
		PersonUUID:    c.PersonUUID,
		Tier:          c.Tier,
		Status:        c.Status,
		Benefits:      c.Benefits,
		Notes:         c.Notes,
		ExpiresAt:     c.ExpiresAt,
		IssuedAt:      c.IssuedAt,
		IssuedBy:      c.IssuedBy,
		SuspendedAt:   c.SuspendedAt,
		SuspendedBy:   c.SuspendedBy,
		SuspendReason: c.SuspendReason,
		RevokedAt:     c.RevokedAt,
		RevokedBy:     c.RevokedBy,
		RevokeReason:  c.RevokeReason,
		UpdatedAt:     c.UpdatedAt,
	}
}

// --- Request/response wrappers ---

type ListPersonCardsInput struct {
	PersonID string `path:"personId"`
}

type ListPersonCardsResponse struct {
	Body struct {
		Items []CardView `json:"items"`
	}
}

type GetCardInput struct {
	ID string `path:"id"`
}

type GetCardResponse struct {
	Body CardView
}

type IssueCardInput struct {
	PersonID string `path:"personId"`
	Body     IssueCardPayload
}

type IssueCardResponse struct {
	Body CardView
}

type SuspendCardInput struct {
	ID   string `path:"id"`
	Body ReasonPayload
}

type SuspendCardResponse struct {
	Body CardView
}

type ReinstateCardInput struct {
	ID string `path:"id"`
}

type ReinstateCardResponse struct {
	Body CardView
}

type RevokeCardInput struct {
	ID   string `path:"id"`
	Body ReasonPayload
}

type RevokeCardResponse struct {
	Body CardView
}

// --- Repository dependency for the read paths ---
//
// The handler's writes flow through CardService, which already holds
// CardRepository. Reads do not need the service indirection, so the
// handler takes a thin repo reference. This matches the
// SnapshotHandler / ScoreProfileHandler pattern from Phase 2.

// CardReadDeps groups the read-path collaborators.
type CardReadDeps struct {
	CardRepo *repository.CardRepository
}

// CardHandlerWithReads is the read-aware handler variant. Constructed
// by the module when wiring read-only routes.
type CardHandlerWithReads struct {
	*CardHandler
	repo *repository.CardRepository
}

// NewCardHandlerWithReads binds both the lifecycle service and the
// read-path repository.
func NewCardHandlerWithReads(svc *services.CardService, repo *repository.CardRepository) *CardHandlerWithReads {
	return &CardHandlerWithReads{CardHandler: NewCardHandler(svc), repo: repo}
}

// --- Handler methods ---

func (h *CardHandlerWithReads) ListByPerson(ctx context.Context, in *ListPersonCardsInput) (*ListPersonCardsResponse, error) {
	got, err := h.repo.ListByPerson(ctx, in.PersonID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]CardView, 0, len(got))
	for i := range got {
		items = append(items, toCardView(&got[i]))
	}
	resp := &ListPersonCardsResponse{}
	resp.Body.Items = items
	return resp, nil
}

func (h *CardHandlerWithReads) Get(ctx context.Context, in *GetCardInput) (*GetCardResponse, error) {
	got, err := h.repo.GetByUUID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrCardNotFound) {
			return nil, huma.Error404NotFound("card not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetCardResponse{Body: toCardView(got)}, nil
}

func (h *CardHandler) Issue(ctx context.Context, in *IssueCardInput) (*IssueCardResponse, error) {
	got, err := h.svc.Issue(ctx, services.IssueParams{
		PersonUUID:   in.PersonID,
		CardTypeUUID: in.Body.CardTypeUUID,
		Tier:         in.Body.Tier,
		Benefits:     in.Body.Benefits,
		ExpiresAt:    in.Body.ExpiresAt,
		Notes:        in.Body.Notes,
	})
	if err != nil {
		return nil, mapCardError(err)
	}
	return &IssueCardResponse{Body: toCardView(got)}, nil
}

func (h *CardHandler) Suspend(ctx context.Context, in *SuspendCardInput) (*SuspendCardResponse, error) {
	got, err := h.svc.Suspend(ctx, in.ID, in.Body.Reason)
	if err != nil {
		return nil, mapCardError(err)
	}
	return &SuspendCardResponse{Body: toCardView(got)}, nil
}

func (h *CardHandler) Reinstate(ctx context.Context, in *ReinstateCardInput) (*ReinstateCardResponse, error) {
	got, err := h.svc.Reinstate(ctx, in.ID)
	if err != nil {
		return nil, mapCardError(err)
	}
	return &ReinstateCardResponse{Body: toCardView(got)}, nil
}

func (h *CardHandler) Revoke(ctx context.Context, in *RevokeCardInput) (*RevokeCardResponse, error) {
	got, err := h.svc.Revoke(ctx, in.ID, in.Body.Reason)
	if err != nil {
		return nil, mapCardError(err)
	}
	return &RevokeCardResponse{Body: toCardView(got)}, nil
}

// mapCardError translates CardService sentinel errors onto the
// canonical HTTP status set. The Phase-4 plan calls for two new
// wire-contract errcode constants
// (marketing.card_code_collision, marketing.card_invalid_transition)
// which land here when the corresponding service error fires; until
// every handler in the addon migrates to the errcode builders the
// detail field carries the human-readable message.
func mapCardError(err error) error {
	switch {
	case errors.Is(err, repository.ErrCardNotFound):
		return huma.Error404NotFound("card not found")
	case errors.Is(err, services.ErrCardInvalidTransition):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, services.ErrCardAlreadyExists):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, services.ErrCardCodeCollision):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, services.ErrTierRequired),
		errors.Is(err, services.ErrTierNotInType),
		errors.Is(err, services.ErrTierForbidden):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, models.ErrInvalidCard):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, repository.ErrCardTypeNotFound):
		return huma.Error404NotFound("card type not found")
	case errors.Is(err, repository.ErrPersonNotFound):
		return huma.Error404NotFound("person not found")
	}
	return huma.Error500InternalServerError(err.Error())
}

// --- Route registration ---

// RegisterCardReadRoutes — gate with marketing.contact.read.
func RegisterCardReadRoutes(api huma.API, h *CardHandlerWithReads) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-person-cards",
		Method:      http.MethodGet, Path: "/v1/marketing/persons/{personId}/cards",
		Summary: "List cards held by a person", Tags: []string{"Marketing - Cards"},
	}, h.ListByPerson)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-card",
		Method:      http.MethodGet, Path: "/v1/marketing/cards/{id}",
		Summary: "Get a card", Tags: []string{"Marketing - Cards"},
	}, h.Get)
}

// RegisterCardIssueRoutes — gate with marketing.card.issue.
func RegisterCardIssueRoutes(api huma.API, h *CardHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-issue-card",
		Method:      http.MethodPost, Path: "/v1/marketing/persons/{personId}/cards",
		Summary: "Issue a card to a person", Tags: []string{"Marketing - Cards"},
		DefaultStatus: http.StatusCreated,
	}, h.Issue)
}

// RegisterCardSuspendRoutes — gate with marketing.card.suspend (also
// owns reinstate as the inverse operation).
func RegisterCardSuspendRoutes(api huma.API, h *CardHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-suspend-card",
		Method:      http.MethodPost, Path: "/v1/marketing/cards/{id}/suspend",
		Summary: "Suspend an active card", Tags: []string{"Marketing - Cards"},
	}, h.Suspend)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-reinstate-card",
		Method:      http.MethodPost, Path: "/v1/marketing/cards/{id}/reinstate",
		Summary: "Reinstate a suspended card", Tags: []string{"Marketing - Cards"},
	}, h.Reinstate)
}

// RegisterCardRevokeRoutes — gate with marketing.card.revoke.
func RegisterCardRevokeRoutes(api huma.API, h *CardHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-revoke-card",
		Method:      http.MethodPost, Path: "/v1/marketing/cards/{id}/revoke",
		Summary: "Revoke a card (terminal)", Tags: []string{"Marketing - Cards"},
	}, h.Revoke)
}
