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

// MembershipHandler exposes the Person↔Organization relation. Routes
// live under /v1/marketing/persons/{id}/memberships for the
// person-centric listing, plus /v1/marketing/memberships/{id} for
// direct mutations.
type MembershipHandler struct {
	svc *services.MembershipService
}

// NewMembershipHandler binds the handler.
func NewMembershipHandler(svc *services.MembershipService) *MembershipHandler {
	return &MembershipHandler{svc: svc}
}

// --- DTOs ---

type MembershipPayload struct {
	OrgUUID    string     `json:"orgUuid" doc:"UUID of the organization this person belongs to"`
	Role       string     `json:"role,omitempty"`
	Department string     `json:"department,omitempty"`
	Since      *time.Time `json:"since,omitempty"`
	Until      *time.Time `json:"until,omitempty" doc:"Set to a date to close the membership immediately; null while active"`
	Primary    bool       `json:"primary,omitempty" doc:"Mark this membership as the person's primary affiliation. Demotes any other active-primary in the same person."`
	Notes      string     `json:"notes,omitempty"`
}

type MembershipView struct {
	UUID       string     `json:"uuid"`
	TenantID   string     `json:"tenantId"`
	PersonUUID string     `json:"personUuid"`
	OrgUUID    string     `json:"orgUuid"`
	Role       string     `json:"role,omitempty"`
	Department string     `json:"department,omitempty"`
	Since      *time.Time `json:"since,omitempty"`
	Until      *time.Time `json:"until,omitempty"`
	Active     bool       `json:"active"`
	Primary    bool       `json:"primary"`
	Notes      string     `json:"notes,omitempty"`
	timestampedView
}

func toMembershipView(m *models.Membership) MembershipView {
	return MembershipView{
		UUID:       m.UUID,
		TenantID:   m.TenantID,
		PersonUUID: m.PersonUUID,
		OrgUUID:    m.OrgUUID,
		Role:       m.Role,
		Department: m.Department,
		Since:      m.Since,
		Until:      m.Until,
		Active:     m.Active,
		Primary:    m.Primary,
		Notes:      m.Notes,
		timestampedView: timestampedView{
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		},
	}
}

// --- Request/response wrappers ---

type ListPersonMembershipsInput struct {
	PersonID string `path:"id"`
}

type ListPersonMembershipsResponse struct {
	Body struct {
		Items []MembershipView `json:"items"`
	}
}

type CreatePersonMembershipInput struct {
	PersonID string `path:"id"`
	Body     MembershipPayload
}

type CreatePersonMembershipResponse struct {
	Body MembershipView
}

type UpdateMembershipInput struct {
	ID   string `path:"id"`
	Body map[string]any
}

type UpdateMembershipResponse struct {
	Body MembershipView
}

type DeleteMembershipInput struct {
	ID string `path:"id"`
}

// --- Handler methods ---

func (h *MembershipHandler) ListForPerson(ctx context.Context, in *ListPersonMembershipsInput) (*ListPersonMembershipsResponse, error) {
	got, err := h.svc.ListByPerson(ctx, in.PersonID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]MembershipView, 0, len(got))
	for i := range got {
		items = append(items, toMembershipView(&got[i]))
	}
	resp := &ListPersonMembershipsResponse{}
	resp.Body.Items = items
	return resp, nil
}

func (h *MembershipHandler) CreateForPerson(ctx context.Context, in *CreatePersonMembershipInput) (*CreatePersonMembershipResponse, error) {
	m := &models.Membership{
		PersonUUID: in.PersonID,
		OrgUUID:    in.Body.OrgUUID,
		Role:       in.Body.Role,
		Department: in.Body.Department,
		Since:      in.Body.Since,
		Until:      in.Body.Until,
		Primary:    in.Body.Primary,
		Notes:      in.Body.Notes,
	}
	got, err := h.svc.Create(ctx, m)
	if err != nil {
		if errors.Is(err, services.ErrInvalidMembership) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreatePersonMembershipResponse{Body: toMembershipView(got)}, nil
}

func (h *MembershipHandler) Update(ctx context.Context, in *UpdateMembershipInput) (*UpdateMembershipResponse, error) {
	got, err := h.svc.Update(ctx, in.ID, in.Body)
	if err != nil {
		if errors.Is(err, repository.ErrMembershipNotFound) {
			return nil, huma.Error404NotFound("membership not found")
		}
		if errors.Is(err, services.ErrInvalidMembership) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &UpdateMembershipResponse{Body: toMembershipView(got)}, nil
}

func (h *MembershipHandler) Delete(ctx context.Context, in *DeleteMembershipInput) (*SuccessResponse, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		if errors.Is(err, repository.ErrMembershipNotFound) {
			return nil, huma.Error404NotFound("membership not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &SuccessResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- Route registration ---

// RegisterMembershipReadRoutes — gate with `marketing.contact.read`.
func RegisterMembershipReadRoutes(api huma.API, h *MembershipHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-person-memberships",
		Method:      http.MethodGet, Path: "/v1/marketing/persons/{id}/memberships",
		Summary: "List a person's memberships", Tags: []string{"Marketing - Memberships"},
	}, h.ListForPerson)
}

// RegisterMembershipWriteRoutes — gate with `marketing.contact.write`.
func RegisterMembershipWriteRoutes(api huma.API, h *MembershipHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-create-person-membership",
		Method:      http.MethodPost, Path: "/v1/marketing/persons/{id}/memberships",
		Summary: "Add a membership to a person", Tags: []string{"Marketing - Memberships"},
		DefaultStatus: http.StatusCreated,
	}, h.CreateForPerson)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-update-membership",
		Method:      http.MethodPatch, Path: "/v1/marketing/memberships/{id}",
		Summary: "Update a membership", Tags: []string{"Marketing - Memberships"},
	}, h.Update)
}

// RegisterMembershipDeleteRoutes — gate with `marketing.contact.delete`.
func RegisterMembershipDeleteRoutes(api huma.API, h *MembershipHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-delete-membership",
		Method:      http.MethodDelete, Path: "/v1/marketing/memberships/{id}",
		Summary: "Delete a membership", Tags: []string{"Marketing - Memberships"},
	}, h.Delete)
}
