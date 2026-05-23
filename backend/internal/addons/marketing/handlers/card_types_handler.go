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

// CardTypeHandler exposes CRUD on marketing_card_types plus the
// preview-code helper-route the admin UI consumes to show what a
// code-format template will render.
//
// Reads fold into marketing.contact.read (consistent with how Phase 2
// folded activities/snapshots and Phase 3 folded the conflict-review
// queue — granting "see contacts" without "see card types" makes no
// operational sense). Writes (create / update / delete) sit on the
// dedicated marketing.card_type.write permission so an operator
// running emit-only flows does not also inherit type-management
// authority.
type CardTypeHandler struct {
	svc *services.CardTypeService
}

// NewCardTypeHandler binds the handler.
func NewCardTypeHandler(svc *services.CardTypeService) *CardTypeHandler {
	return &CardTypeHandler{svc: svc}
}

// --- DTOs ---

// CardTypePayload is the create-input shape. Update uses a separate
// pointer-field DTO so partial patches don't accidentally clear
// fields the caller omitted.
type CardTypePayload struct {
	Key                    string   `json:"key" doc:"Lowercase slug, unique per tenant (e.g. premium_member)"`
	DisplayName            string   `json:"displayName" doc:"Operator-facing name"`
	Description            string   `json:"description,omitempty" doc:"Markdown blurb"`
	Tiers                  []string `json:"tiers,omitempty" doc:"Allowed tier strings; empty = type carries no tier"`
	CodeFormat             string   `json:"codeFormat" doc:"Template — supports {YYYY}/{YY}/{MM}/{DD}, {seq:N}, {rand:N}"`
	DefaultBenefits        []string `json:"defaultBenefits,omitempty"`
	AllowMultiplePerPerson bool     `json:"allowMultiplePerPerson"`
}

// CardTypeUpdatePayload uses pointer fields so callers can patch a
// subset without clobbering the rest. Slice pointers distinguish
// "clear" (empty slice) from "unchanged" (nil).
type CardTypeUpdatePayload struct {
	DisplayName            *string   `json:"displayName,omitempty"`
	Description            *string   `json:"description,omitempty"`
	Tiers                  *[]string `json:"tiers,omitempty"`
	CodeFormat             *string   `json:"codeFormat,omitempty"`
	DefaultBenefits        *[]string `json:"defaultBenefits,omitempty"`
	AllowMultiplePerPerson *bool     `json:"allowMultiplePerPerson,omitempty"`
	Active                 *bool     `json:"active,omitempty"`
}

// CardTypeView is the read response shape.
type CardTypeView struct {
	UUID                   string    `json:"uuid"`
	Key                    string    `json:"key"`
	DisplayName            string    `json:"displayName"`
	Description            string    `json:"description,omitempty"`
	Tiers                  []string  `json:"tiers,omitempty"`
	CodeFormat             string    `json:"codeFormat"`
	DefaultBenefits        []string  `json:"defaultBenefits,omitempty"`
	AllowMultiplePerPerson bool      `json:"allowMultiplePerPerson"`
	Active                 bool      `json:"active"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
	CreatedBy              string    `json:"createdBy,omitempty"`
	UpdatedBy              string    `json:"updatedBy,omitempty"`
}

func toCardTypeView(t *models.CardType) CardTypeView {
	return CardTypeView{
		UUID:                   t.UUID,
		Key:                    t.Key,
		DisplayName:            t.DisplayName,
		Description:            t.Description,
		Tiers:                  t.Tiers,
		CodeFormat:             t.CodeFormat,
		DefaultBenefits:        t.DefaultBenefits,
		AllowMultiplePerPerson: t.AllowMultiplePerPerson,
		Active:                 t.Active,
		CreatedAt:              t.CreatedAt,
		UpdatedAt:              t.UpdatedAt,
		CreatedBy:              t.CreatedBy,
		UpdatedBy:              t.UpdatedBy,
	}
}

// --- Request/response wrappers ---

type ListCardTypesInput struct {
	ActiveOnly bool `query:"activeOnly" doc:"When true, return only active card types"`
}

type ListCardTypesResponse struct {
	Body struct {
		Items []CardTypeView `json:"items"`
	}
}

type GetCardTypeInput struct {
	ID string `path:"id"`
}

type GetCardTypeResponse struct {
	Body CardTypeView
}

type CreateCardTypeInput struct {
	Body CardTypePayload
}

type CreateCardTypeResponse struct {
	Body CardTypeView
}

type UpdateCardTypeInput struct {
	ID   string `path:"id"`
	Body CardTypeUpdatePayload
}

type UpdateCardTypeResponse struct {
	Body CardTypeView
}

type DeleteCardTypeInput struct {
	ID string `path:"id"`
}

// PreviewCodeInput is the helper-route the admin UI consumes to
// render a sample code as the operator edits the code_format field.
// Server-side rendering eliminates client-side duplication of the
// grammar parser.
type PreviewCodeInput struct {
	Format string `query:"format" doc:"Template to render"`
	Seq    int64  `query:"seq" doc:"Sequence value used when the template carries {seq:N}; defaults to 0"`
}

type PreviewCodeResponse struct {
	Body struct {
		Code string `json:"code"`
	}
}

// --- Handler methods ---

func (h *CardTypeHandler) List(ctx context.Context, in *ListCardTypesInput) (*ListCardTypesResponse, error) {
	got, err := h.svc.List(ctx, repository.CardTypeFilter{ActiveOnly: in.ActiveOnly})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]CardTypeView, 0, len(got))
	for i := range got {
		items = append(items, toCardTypeView(&got[i]))
	}
	resp := &ListCardTypesResponse{}
	resp.Body.Items = items
	return resp, nil
}

func (h *CardTypeHandler) Get(ctx context.Context, in *GetCardTypeInput) (*GetCardTypeResponse, error) {
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrCardTypeNotFound) {
			return nil, huma.Error404NotFound("card type not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetCardTypeResponse{Body: toCardTypeView(got)}, nil
}

func (h *CardTypeHandler) Create(ctx context.Context, in *CreateCardTypeInput) (*CreateCardTypeResponse, error) {
	t := &models.CardType{
		Key:                    in.Body.Key,
		DisplayName:            in.Body.DisplayName,
		Description:            in.Body.Description,
		Tiers:                  in.Body.Tiers,
		CodeFormat:             in.Body.CodeFormat,
		DefaultBenefits:        in.Body.DefaultBenefits,
		AllowMultiplePerPerson: in.Body.AllowMultiplePerPerson,
	}
	got, err := h.svc.Create(ctx, t)
	if err != nil {
		if errors.Is(err, models.ErrInvalidCardType) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if isCodeFormatError(err) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreateCardTypeResponse{Body: toCardTypeView(got)}, nil
}

func (h *CardTypeHandler) Update(ctx context.Context, in *UpdateCardTypeInput) (*UpdateCardTypeResponse, error) {
	got, err := h.svc.Update(ctx, in.ID, services.UpdateCardTypePatch{
		DisplayName:            in.Body.DisplayName,
		Description:            in.Body.Description,
		Tiers:                  in.Body.Tiers,
		CodeFormat:             in.Body.CodeFormat,
		DefaultBenefits:        in.Body.DefaultBenefits,
		AllowMultiplePerPerson: in.Body.AllowMultiplePerPerson,
		Active:                 in.Body.Active,
	})
	if err != nil {
		if errors.Is(err, repository.ErrCardTypeNotFound) {
			return nil, huma.Error404NotFound("card type not found")
		}
		if errors.Is(err, models.ErrInvalidCardType) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if isCodeFormatError(err) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &UpdateCardTypeResponse{Body: toCardTypeView(got)}, nil
}

func (h *CardTypeHandler) Delete(ctx context.Context, in *DeleteCardTypeInput) (*SuccessResponse, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		if errors.Is(err, repository.ErrCardTypeNotFound) {
			return nil, huma.Error404NotFound("card type not found")
		}
		if errors.Is(err, services.ErrCardTypeInUse) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &SuccessResponse{}
	resp.Body.Success = true
	return resp, nil
}

// PreviewCode renders the supplied template against an injected
// (now, seq) tuple so the admin UI can show operators what the
// generated code will look like. Reads use deterministic-source-free
// crypto/rand for the {rand:N} placeholder, so each preview call
// produces a fresh sample. No DB writes; safe to fold into
// marketing.contact.read.
func (h *CardTypeHandler) PreviewCode(ctx context.Context, in *PreviewCodeInput) (*PreviewCodeResponse, error) {
	if in.Format == "" {
		return nil, huma.Error400BadRequest("format query parameter is required")
	}
	ast, err := services.ParseCardCodeFormat(in.Format)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	code, err := services.RenderCardCode(ast, time.Now().UTC(), in.Seq, nil)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &PreviewCodeResponse{}
	resp.Body.Code = code
	return resp, nil
}

// isCodeFormatError is a thin type check that avoids importing the
// concrete type into the handler scope just for an errors.As call.
func isCodeFormatError(err error) bool {
	var cfe *services.CodeFormatError
	return errors.As(err, &cfe)
}

// --- Route registration ---

// RegisterCardTypeReadRoutes — gate with marketing.contact.read.
// Includes the preview-code helper-route since it is a read-only
// computation against a template the operator is editing.
func RegisterCardTypeReadRoutes(api huma.API, h *CardTypeHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-card-types",
		Method:      http.MethodGet, Path: "/v1/marketing/card-types",
		Summary: "List card types", Tags: []string{"Marketing - Card Types"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-card-type",
		Method:      http.MethodGet, Path: "/v1/marketing/card-types/{id}",
		Summary: "Get a card type", Tags: []string{"Marketing - Card Types"},
	}, h.Get)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-preview-card-code",
		Method:      http.MethodGet, Path: "/v1/marketing/card-types/preview-code",
		Summary: "Preview a card code from a template", Tags: []string{"Marketing - Card Types"},
	}, h.PreviewCode)
}

// RegisterCardTypeWriteRoutes — gate with marketing.card_type.write.
func RegisterCardTypeWriteRoutes(api huma.API, h *CardTypeHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-create-card-type",
		Method:      http.MethodPost, Path: "/v1/marketing/card-types",
		Summary: "Create a card type", Tags: []string{"Marketing - Card Types"},
		DefaultStatus: http.StatusCreated,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-update-card-type",
		Method:      http.MethodPatch, Path: "/v1/marketing/card-types/{id}",
		Summary: "Update a card type", Tags: []string{"Marketing - Card Types"},
	}, h.Update)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-delete-card-type",
		Method:      http.MethodDelete, Path: "/v1/marketing/card-types/{id}",
		Summary: "Delete a card type (only when no cards exist)", Tags: []string{"Marketing - Card Types"},
	}, h.Delete)
}
