package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
)

// OrganizationHandler exposes CRUD on marketing_organizations.
type OrganizationHandler struct {
	svc *services.OrganizationService
}

// NewOrganizationHandler binds the handler to its service.
func NewOrganizationHandler(svc *services.OrganizationService) *OrganizationHandler {
	return &OrganizationHandler{svc: svc}
}

// --- DTOs ---

// OrganizationPayload is the create / replace shape accepted from
// clients. tenantId, uuid, and timestamps are server-owned.
type OrganizationPayload struct {
	LegalName    string                  `json:"legalName" doc:"Official institution name"`
	DisplayName  string                  `json:"displayName,omitempty"`
	VAT          string                  `json:"vat,omitempty"`
	TaxCode      string                  `json:"taxCode,omitempty"`
	Kind         models.OrganizationKind `json:"kind,omitempty" doc:"company | public_administration | foundation | association | other (defaults to company)"`
	Website      string                  `json:"website,omitempty"`
	Emails       []models.EmailEntry     `json:"emails,omitempty"`
	Phones       []models.PhoneEntry     `json:"phones,omitempty"`
	Addresses    []models.PostalAddress  `json:"addresses,omitempty"`
	Tags         []string                `json:"tags,omitempty"`
	CustomFields map[string]any          `json:"customFields,omitempty"`
	Notes        string                  `json:"notes,omitempty"`
}

// OrganizationView is the read shape returned to clients.
type OrganizationView struct {
	UUID         string                    `json:"uuid"`
	TenantID     string                    `json:"tenantId"`
	LegalName    string                    `json:"legalName"`
	DisplayName  string                    `json:"displayName,omitempty"`
	VAT          string                    `json:"vat,omitempty"`
	TaxCode      string                    `json:"taxCode,omitempty"`
	Kind         models.OrganizationKind   `json:"kind"`
	Website      string                    `json:"website,omitempty"`
	Emails       []models.EmailEntry       `json:"emails,omitempty"`
	Phones       []models.PhoneEntry       `json:"phones,omitempty"`
	Addresses    []models.PostalAddress    `json:"addresses,omitempty"`
	Tags         []string                  `json:"tags,omitempty"`
	CustomFields map[string]any            `json:"customFields,omitempty"`
	Sources      []models.ProvenanceSource `json:"sources,omitempty"`
	Notes        string                    `json:"notes,omitempty"`
	timestampedView
}

func toOrgView(o *models.Organization) OrganizationView {
	return OrganizationView{
		UUID:         o.UUID,
		TenantID:     o.TenantID,
		LegalName:    o.LegalName,
		DisplayName:  o.DisplayName,
		VAT:          o.VAT,
		TaxCode:      o.TaxCode,
		Kind:         o.Kind,
		Website:      o.Website,
		Emails:       o.Emails,
		Phones:       o.Phones,
		Addresses:    o.Addresses,
		Tags:         o.Tags,
		CustomFields: o.CustomFields,
		Sources:      o.Sources,
		Notes:        o.Notes,
		timestampedView: timestampedView{
			CreatedAt: o.CreatedAt,
			UpdatedAt: o.UpdatedAt,
		},
	}
}

// --- Request / response wrappers ---

type ListOrgsInput struct {
	PaginatedQuery
	Kind   string   `query:"kind"`
	Tags   []string `query:"tag"`
	Source string   `query:"source"`
}

type ListOrgsResponse struct {
	Body struct {
		Items []OrganizationView `json:"items"`
		Meta  ListMeta           `json:"meta"`
	}
}

type GetOrgInput struct {
	ID string `path:"id"`
}

type GetOrgResponse struct {
	Body OrganizationView
}

type CreateOrgInput struct {
	Body OrganizationPayload
}

type CreateOrgResponse struct {
	Body OrganizationView
}

type UpdateOrgInput struct {
	ID   string `path:"id"`
	Body map[string]any
}

type UpdateOrgResponse struct {
	Body OrganizationView
}

type DeleteOrgInput struct {
	ID string `path:"id"`
}

// --- Handler methods ---

func (h *OrganizationHandler) List(ctx context.Context, in *ListOrgsInput) (*ListOrgsResponse, error) {
	got, err := h.svc.List(ctx, repository.ListFilter{
		Kind:     models.OrganizationKind(in.Kind),
		TagUUIDs: in.Tags,
		Source:   in.Source,
		Limit:    in.Limit,
		Skip:     in.Skip,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]OrganizationView, 0, len(got))
	for i := range got {
		items = append(items, toOrgView(&got[i]))
	}
	out := &ListOrgsResponse{}
	out.Body.Items = items
	out.Body.Meta = ListMeta{Limit: in.Limit, Skip: in.Skip, Count: len(items)}
	return out, nil
}

func (h *OrganizationHandler) Get(ctx context.Context, in *GetOrgInput) (*GetOrgResponse, error) {
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrOrgNotFound) {
			return nil, huma.Error404NotFound("organization not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetOrgResponse{Body: toOrgView(got)}, nil
}

func (h *OrganizationHandler) Create(ctx context.Context, in *CreateOrgInput) (*CreateOrgResponse, error) {
	org := &models.Organization{
		LegalName:    in.Body.LegalName,
		DisplayName:  in.Body.DisplayName,
		VAT:          in.Body.VAT,
		TaxCode:      in.Body.TaxCode,
		Kind:         in.Body.Kind,
		Website:      in.Body.Website,
		Emails:       in.Body.Emails,
		Phones:       in.Body.Phones,
		Addresses:    in.Body.Addresses,
		Tags:         in.Body.Tags,
		CustomFields: in.Body.CustomFields,
		Notes:        in.Body.Notes,
	}
	got, err := h.svc.Create(ctx, org)
	if err != nil {
		if errors.Is(err, services.ErrInvalidOrganization) || errors.Is(err, services.ErrCustomFieldValidation) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreateOrgResponse{Body: toOrgView(got)}, nil
}

func (h *OrganizationHandler) Update(ctx context.Context, in *UpdateOrgInput) (*UpdateOrgResponse, error) {
	got, err := h.svc.Update(ctx, in.ID, in.Body)
	if err != nil {
		if errors.Is(err, repository.ErrOrgNotFound) {
			return nil, huma.Error404NotFound("organization not found")
		}
		if errors.Is(err, services.ErrInvalidOrganization) || errors.Is(err, services.ErrCustomFieldValidation) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &UpdateOrgResponse{Body: toOrgView(got)}, nil
}

func (h *OrganizationHandler) Delete(ctx context.Context, in *DeleteOrgInput) (*SuccessResponse, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		if errors.Is(err, repository.ErrOrgNotFound) {
			return nil, huma.Error404NotFound("organization not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &SuccessResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- Route registration ---

// RegisterOrgReadRoutes mounts the read endpoints. Gate the chi
// subgroup with `marketing.contact.read` at the caller side.
func RegisterOrgReadRoutes(api huma.API, h *OrganizationHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-organizations",
		Method:      http.MethodGet, Path: "/v1/marketing/organizations",
		Summary: "List organizations", Tags: []string{"Marketing - Organizations"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-organization",
		Method:      http.MethodGet, Path: "/v1/marketing/organizations/{id}",
		Summary: "Get an organization", Tags: []string{"Marketing - Organizations"},
	}, h.Get)
}

// RegisterOrgWriteRoutes mounts create + update endpoints. Gate
// the subgroup with `marketing.contact.write`.
func RegisterOrgWriteRoutes(api huma.API, h *OrganizationHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-create-organization",
		Method:      http.MethodPost, Path: "/v1/marketing/organizations",
		Summary: "Create an organization", Tags: []string{"Marketing - Organizations"},
		DefaultStatus: http.StatusCreated,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-update-organization",
		Method:      http.MethodPatch, Path: "/v1/marketing/organizations/{id}",
		Summary: "Update an organization", Tags: []string{"Marketing - Organizations"},
	}, h.Update)
}

// RegisterOrgDeleteRoutes mounts the delete endpoint. Gate the
// subgroup with `marketing.contact.delete`.
func RegisterOrgDeleteRoutes(api huma.API, h *OrganizationHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-delete-organization",
		Method:      http.MethodDelete, Path: "/v1/marketing/organizations/{id}",
		Summary: "Delete an organization", Tags: []string{"Marketing - Organizations"},
	}, h.Delete)
}
