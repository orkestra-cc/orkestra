package handlers

import (
	"context"

	"github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/repository"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/services"
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

// PublicPricingTier mirrors the catalog-facing shape of a PricingTier
// with no admin-only fields. Named concretely so Huma's schema registry
// doesn't collapse it into the authenticated PricingTier — anonymous
// responses are public-documentation surface and shouldn't expose e.g.
// metadata blobs operators may use for internal accounting tags.
type PublicPricingTier struct {
	Code         string   `json:"code"`
	Cycle        string   `json:"cycle" enum:"monthly,quarterly,annual"`
	AmountCents  int64    `json:"amountCents"`
	Currency     string   `json:"currency"`
	Capabilities []string `json:"capabilities,omitempty" doc:"Capability IDs granted on activation"`
}

// PublicCatalogService is the anonymous-reader projection of a catalog
// Service. Drops Metadata (arbitrary operator tags), CreatedAt/UpdatedAt
// (admin-only bookkeeping), and any field that might carry internal
// pricing overrides in the future. Only `active=true` services are
// exposed via the public list.
type PublicCatalogService struct {
	Code          string              `json:"code"`
	Name          string              `json:"name"`
	Category      string              `json:"category,omitempty"`
	Description   string              `json:"description,omitempty"`
	PricingTiers  []PublicPricingTier `json:"pricingTiers"`
	SetupFeeCents int64               `json:"setupFeeCents,omitempty"`
}

// PublicListCatalogRequest is intentionally empty — the public catalog
// doesn't expose category filters to avoid leaking structure about
// disabled/internal categories via empty-result probes.
type PublicListCatalogRequest struct{}

// PublicListCatalogResponse wraps the list for the anonymous signup UI.
type PublicListCatalogResponse struct {
	Body struct {
		Items []PublicCatalogService `json:"items"`
		Total int                    `json:"total"`
	}
}

// PublicList returns every active catalog service, projected to the
// anonymous-safe shape defined above. No auth — the signup UI needs to
// show pricing before the caller has any credentials.
func (h *ServiceHandler) PublicList(ctx context.Context, _ *PublicListCatalogRequest) (*PublicListCatalogResponse, error) {
	active := true
	items, err := h.svc.List(ctx, repository.ServiceFilters{Active: &active})
	if err != nil {
		return nil, err
	}
	out := &PublicListCatalogResponse{}
	out.Body.Items = make([]PublicCatalogService, 0, len(items))
	for _, s := range items {
		tiers := make([]PublicPricingTier, 0, len(s.PricingTiers))
		for _, t := range s.PricingTiers {
			tiers = append(tiers, PublicPricingTier{
				Code:         t.Code,
				Cycle:        string(t.Cycle),
				AmountCents:  t.AmountCents,
				Currency:     t.Currency,
				Capabilities: t.Capabilities,
			})
		}
		out.Body.Items = append(out.Body.Items, PublicCatalogService{
			Code:          s.Code,
			Name:          s.Name,
			Category:      s.Category,
			Description:   s.Description,
			PricingTiers:  tiers,
			SetupFeeCents: s.SetupFeeCents,
		})
	}
	out.Body.Total = len(out.Body.Items)
	return out, nil
}

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
