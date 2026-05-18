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

// PersonHandler exposes CRUD on marketing_persons.
type PersonHandler struct {
	svc *services.PersonService
}

// NewPersonHandler binds the handler to its service.
func NewPersonHandler(svc *services.PersonService) *PersonHandler {
	return &PersonHandler{svc: svc}
}

// --- DTOs ---

type PersonPayload struct {
	FirstName    string              `json:"firstName,omitempty"`
	LastName     string              `json:"lastName,omitempty"`
	Title        string              `json:"title,omitempty"`
	Emails       []models.EmailEntry `json:"emails,omitempty"`
	Phones       []models.PhoneEntry `json:"phones,omitempty"`
	Language     string              `json:"language,omitempty" doc:"BCP-47 tag (e.g. en, it, it-IT)"`
	Birthdate    *time.Time          `json:"birthdate,omitempty"`
	Tags         []string            `json:"tags,omitempty"`
	CustomFields map[string]any      `json:"customFields,omitempty"`
	Consent      *models.Consent     `json:"consent,omitempty"`
	Notes        string              `json:"notes,omitempty"`
}

type PersonView struct {
	UUID            string                    `json:"uuid"`
	TenantID        string                    `json:"tenantId"`
	FirstName       string                    `json:"firstName,omitempty"`
	LastName        string                    `json:"lastName,omitempty"`
	Title           string                    `json:"title,omitempty"`
	Emails          []models.EmailEntry       `json:"emails,omitempty"`
	Phones          []models.PhoneEntry       `json:"phones,omitempty"`
	Language        string                    `json:"language,omitempty"`
	Birthdate       *time.Time                `json:"birthdate,omitempty"`
	Tags            []string                  `json:"tags,omitempty"`
	CustomFields    map[string]any            `json:"customFields,omitempty"`
	Consent         *models.Consent           `json:"consent,omitempty"`
	ActiveCardUUIDs []string                  `json:"activeCardUuids,omitempty"`
	Sources         []models.ProvenanceSource `json:"sources,omitempty"`
	Notes           string                    `json:"notes,omitempty"`
	timestampedView
}

func toPersonView(p *models.Person) PersonView {
	return PersonView{
		UUID:            p.UUID,
		TenantID:        p.TenantID,
		FirstName:       p.FirstName,
		LastName:        p.LastName,
		Title:           p.Title,
		Emails:          p.Emails,
		Phones:          p.Phones,
		Language:        p.Language,
		Birthdate:       p.Birthdate,
		Tags:            p.Tags,
		CustomFields:    p.CustomFields,
		Consent:         p.Consent,
		ActiveCardUUIDs: p.ActiveCardUUIDs,
		Sources:         p.Sources,
		Notes:           p.Notes,
		timestampedView: timestampedView{
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
		},
	}
}

// --- Request/response wrappers ---

type ListPersonsInput struct {
	PaginatedQuery
	Tags     []string `query:"tag"`
	HasEmail bool     `query:"hasEmail"`
	Source   string   `query:"source"`
}

type ListPersonsResponse struct {
	Body struct {
		Items []PersonView `json:"items"`
		Meta  ListMeta     `json:"meta"`
	}
}

type GetPersonInput struct {
	ID string `path:"id"`
}

type GetPersonResponse struct {
	Body PersonView
}

type CreatePersonInput struct {
	Body PersonPayload
}

type CreatePersonResponse struct {
	Body PersonView
}

type UpdatePersonInput struct {
	ID   string `path:"id"`
	Body map[string]any
}

type UpdatePersonResponse struct {
	Body PersonView
}

type DeletePersonInput struct {
	ID string `path:"id"`
}

// --- Handler methods ---

func (h *PersonHandler) List(ctx context.Context, in *ListPersonsInput) (*ListPersonsResponse, error) {
	got, err := h.svc.List(ctx, repository.PersonListFilter{
		TagUUIDs: in.Tags,
		HasEmail: in.HasEmail,
		Source:   in.Source,
		Limit:    in.Limit,
		Skip:     in.Skip,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]PersonView, 0, len(got))
	for i := range got {
		items = append(items, toPersonView(&got[i]))
	}
	resp := &ListPersonsResponse{}
	resp.Body.Items = items
	resp.Body.Meta = ListMeta{Limit: in.Limit, Skip: in.Skip, Count: len(items)}
	return resp, nil
}

func (h *PersonHandler) Get(ctx context.Context, in *GetPersonInput) (*GetPersonResponse, error) {
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrPersonNotFound) {
			return nil, huma.Error404NotFound("person not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetPersonResponse{Body: toPersonView(got)}, nil
}

func (h *PersonHandler) Create(ctx context.Context, in *CreatePersonInput) (*CreatePersonResponse, error) {
	p := &models.Person{
		FirstName:    in.Body.FirstName,
		LastName:     in.Body.LastName,
		Title:        in.Body.Title,
		Emails:       in.Body.Emails,
		Phones:       in.Body.Phones,
		Language:     in.Body.Language,
		Birthdate:    in.Body.Birthdate,
		Tags:         in.Body.Tags,
		CustomFields: in.Body.CustomFields,
		Consent:      in.Body.Consent,
		Notes:        in.Body.Notes,
	}
	got, err := h.svc.Create(ctx, p)
	if err != nil {
		if errors.Is(err, services.ErrInvalidPerson) || errors.Is(err, services.ErrCustomFieldValidation) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreatePersonResponse{Body: toPersonView(got)}, nil
}

func (h *PersonHandler) Update(ctx context.Context, in *UpdatePersonInput) (*UpdatePersonResponse, error) {
	got, err := h.svc.Update(ctx, in.ID, in.Body)
	if err != nil {
		if errors.Is(err, repository.ErrPersonNotFound) {
			return nil, huma.Error404NotFound("person not found")
		}
		if errors.Is(err, services.ErrInvalidPerson) || errors.Is(err, services.ErrCustomFieldValidation) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &UpdatePersonResponse{Body: toPersonView(got)}, nil
}

func (h *PersonHandler) Delete(ctx context.Context, in *DeletePersonInput) (*SuccessResponse, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		if errors.Is(err, repository.ErrPersonNotFound) {
			return nil, huma.Error404NotFound("person not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &SuccessResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- Route registration ---

// RegisterPersonReadRoutes — gate with `marketing.contact.read`.
func RegisterPersonReadRoutes(api huma.API, h *PersonHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-persons",
		Method:      http.MethodGet, Path: "/v1/marketing/persons",
		Summary: "List persons", Tags: []string{"Marketing - Persons"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-person",
		Method:      http.MethodGet, Path: "/v1/marketing/persons/{id}",
		Summary: "Get a person", Tags: []string{"Marketing - Persons"},
	}, h.Get)
}

// RegisterPersonWriteRoutes — gate with `marketing.contact.write`.
func RegisterPersonWriteRoutes(api huma.API, h *PersonHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-create-person",
		Method:      http.MethodPost, Path: "/v1/marketing/persons",
		Summary: "Create a person", Tags: []string{"Marketing - Persons"},
		DefaultStatus: http.StatusCreated,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-update-person",
		Method:      http.MethodPatch, Path: "/v1/marketing/persons/{id}",
		Summary: "Update a person", Tags: []string{"Marketing - Persons"},
	}, h.Update)
}

// RegisterPersonDeleteRoutes — gate with `marketing.contact.delete`.
func RegisterPersonDeleteRoutes(api huma.API, h *PersonHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-delete-person",
		Method:      http.MethodDelete, Path: "/v1/marketing/persons/{id}",
		Summary: "Delete a person", Tags: []string{"Marketing - Persons"},
	}, h.Delete)
}
