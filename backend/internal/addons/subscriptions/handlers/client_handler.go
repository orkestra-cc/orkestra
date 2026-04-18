package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/addons/subscriptions/services"
)

type ClientHandler struct {
	svc *services.ClientService
}

func NewClientHandler(svc *services.ClientService) *ClientHandler {
	return &ClientHandler{svc: svc}
}

type ClientInput struct {
	OrgUUID     string         `json:"orgUUID,omitempty"`
	LegalName   string         `json:"legalName"`
	DisplayName string         `json:"displayName,omitempty"`
	Email       string         `json:"email"`
	VATNumber   string         `json:"vatNumber,omitempty"`
	FiscalCode  string         `json:"fiscalCode,omitempty"`
	BillingAddr models.ClientAddress `json:"billingAddr,omitempty"`
	Notes       string         `json:"notes,omitempty"`
}

type CreateClientRequest struct {
	Body ClientInput
}
type ClientResponse struct {
	Body models.Client
}
type GetClientRequest struct {
	ID string `path:"id"`
}
type ListClientsRequest struct {
	Status string `query:"status" enum:"active,archived" doc:"Filter by status"`
	Search string `query:"search"`
}
type ListClientsResponse struct {
	Body struct {
		Items []models.Client `json:"items"`
		Total int             `json:"total"`
	}
}
type UpdateClientRequest struct {
	ID   string `path:"id"`
	Body ClientInput
}
type DeleteClientRequest struct {
	ID string `path:"id"`
}

func (h *ClientHandler) Create(ctx context.Context, in *CreateClientRequest) (*ClientResponse, error) {
	c := &models.Client{
		OrgUUID:     in.Body.OrgUUID,
		LegalName:   in.Body.LegalName,
		DisplayName: in.Body.DisplayName,
		Email:       in.Body.Email,
		VATNumber:   in.Body.VATNumber,
		FiscalCode:  in.Body.FiscalCode,
		BillingAddr: in.Body.BillingAddr,
		Notes:       in.Body.Notes,
	}
	created, err := h.svc.Create(ctx, c)
	if err != nil {
		return nil, err
	}
	return &ClientResponse{Body: *created}, nil
}

func (h *ClientHandler) Get(ctx context.Context, in *GetClientRequest) (*ClientResponse, error) {
	c, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := assertOrgOwnsClient(ctx, c.OrgUUID); err != nil {
		return nil, err
	}
	return &ClientResponse{Body: *c}, nil
}

func (h *ClientHandler) List(ctx context.Context, in *ListClientsRequest) (*ListClientsResponse, error) {
	items, err := h.svc.List(ctx, repository.ClientFilters{
		Status: models.ClientStatus(in.Status),
		Search: in.Search,
	})
	if err != nil {
		return nil, err
	}
	resp := &ListClientsResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

func (h *ClientHandler) Update(ctx context.Context, in *UpdateClientRequest) (*ClientResponse, error) {
	existing, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := assertOrgOwnsClient(ctx, existing.OrgUUID); err != nil {
		return nil, err
	}
	patch := &models.Client{
		OrgUUID:     in.Body.OrgUUID,
		LegalName:   in.Body.LegalName,
		DisplayName: in.Body.DisplayName,
		Email:       in.Body.Email,
		VATNumber:   in.Body.VATNumber,
		FiscalCode:  in.Body.FiscalCode,
		BillingAddr: in.Body.BillingAddr,
		Notes:       in.Body.Notes,
	}
	updated, err := h.svc.Update(ctx, in.ID, patch)
	if err != nil {
		return nil, err
	}
	return &ClientResponse{Body: *updated}, nil
}

func (h *ClientHandler) Archive(ctx context.Context, in *DeleteClientRequest) (*EmptyResponse, error) {
	existing, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := assertOrgOwnsClient(ctx, existing.OrgUUID); err != nil {
		return nil, err
	}
	if err := h.svc.Archive(ctx, in.ID); err != nil {
		return nil, err
	}
	return &EmptyResponse{}, nil
}
