package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/addons/subscriptions/services"
)

type ServiceHandler struct {
	svc *services.ServiceService
}

func NewServiceHandler(svc *services.ServiceService) *ServiceHandler {
	return &ServiceHandler{svc: svc}
}

type ServiceInput struct {
	Code          string               `json:"code" doc:"Stable SKU, lowercase-hyphenated"`
	Name          string               `json:"name"`
	Category      string               `json:"category" doc:"e.g. workflow, database, agent, hosting"`
	Description   string               `json:"description,omitempty"`
	Active        bool                 `json:"active"`
	PricingTiers  []models.PricingTier `json:"pricingTiers"`
	SetupFeeCents int64                `json:"setupFeeCents,omitempty"`
	Metadata      map[string]any       `json:"metadata,omitempty"`
}

type CreateServiceRequest struct {
	Body ServiceInput
}
type ServiceResponse struct {
	Body models.Service
}
type GetServiceRequest struct {
	ID string `path:"id"`
}
type ListServicesRequest struct {
	Active   string `query:"active" enum:"true,false" doc:"Filter by active flag"`
	Category string `query:"category"`
}
type ListServicesResponse struct {
	Body struct {
		Items []models.Service `json:"items"`
		Total int              `json:"total"`
	}
}
type UpdateServiceRequest struct {
	ID   string `path:"id"`
	Body ServiceInput
}
type DeleteServiceRequest struct {
	ID string `path:"id"`
}
type EmptyResponse struct{}

func (h *ServiceHandler) Create(ctx context.Context, in *CreateServiceRequest) (*ServiceResponse, error) {
	svc := &models.Service{
		Code:          in.Body.Code,
		Name:          in.Body.Name,
		Category:      in.Body.Category,
		Description:   in.Body.Description,
		Active:        in.Body.Active,
		PricingTiers:  in.Body.PricingTiers,
		SetupFeeCents: in.Body.SetupFeeCents,
		Metadata:      in.Body.Metadata,
	}
	created, err := h.svc.Create(ctx, svc)
	if err != nil {
		return nil, err
	}
	return &ServiceResponse{Body: *created}, nil
}

func (h *ServiceHandler) Get(ctx context.Context, in *GetServiceRequest) (*ServiceResponse, error) {
	s, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &ServiceResponse{Body: *s}, nil
}

func (h *ServiceHandler) List(ctx context.Context, in *ListServicesRequest) (*ListServicesResponse, error) {
	f := repository.ServiceFilters{Category: in.Category}
	if in.Active != "" {
		b := in.Active == "true"
		f.Active = &b
	}
	items, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, err
	}
	resp := &ListServicesResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

func (h *ServiceHandler) Update(ctx context.Context, in *UpdateServiceRequest) (*ServiceResponse, error) {
	patch := &models.Service{
		Name:          in.Body.Name,
		Category:      in.Body.Category,
		Description:   in.Body.Description,
		Active:        in.Body.Active,
		PricingTiers:  in.Body.PricingTiers,
		SetupFeeCents: in.Body.SetupFeeCents,
		Metadata:      in.Body.Metadata,
	}
	updated, err := h.svc.Update(ctx, in.ID, patch)
	if err != nil {
		return nil, err
	}
	return &ServiceResponse{Body: *updated}, nil
}

func (h *ServiceHandler) Delete(ctx context.Context, in *DeleteServiceRequest) (*EmptyResponse, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		return nil, err
	}
	return &EmptyResponse{}, nil
}
