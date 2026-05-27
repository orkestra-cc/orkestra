package handlers

import (
	"context"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/internal/core/navigation/services"
)

// AdminNavigationHandler exposes the unfiltered nav tree at
// /v1/admin/navigation. Gated by the administrator security scope at the
// route declaration — no per-request role check needed here.
type AdminNavigationHandler struct {
	svc       services.AdminNavigationService
	overrides services.OverrideService
	itemsIdx  services.NavItemsIndexAccessor
}

func NewAdminNavigationHandler(svc services.AdminNavigationService, overrides services.OverrideService, idx services.NavItemsIndexAccessor) *AdminNavigationHandler {
	return &AdminNavigationHandler{svc: svc, overrides: overrides, itemsIdx: idx}
}

type GetAdminNavigationRequest struct{}

// GetAdminNavigationResponse — Huma serializes Body flat, so the wire
// JSON is the AdminNavigationResponse itself. `json:"-"` matches the
// convention used by the log-levels handler and avoids implying a
// wrapper key that does not exist on the wire.
type GetAdminNavigationResponse struct {
	Body models.AdminNavigationResponse `json:"-"`
}

func (h *AdminNavigationHandler) GetAdminNavigation(ctx context.Context, _ *GetAdminNavigationRequest) (*GetAdminNavigationResponse, error) {
	tree, err := h.svc.GetAdminTree(ctx)
	if err != nil {
		return nil, err
	}
	return &GetAdminNavigationResponse{Body: *tree}, nil
}

// PatchOrderRequest carries the override payload. ParentKey must be a
// key the registry recognises (an item.ItemKey with children or the
// synthetic "__root.<realm>.<section-slug>" key). OrderedChildren is
// the desired sibling order — items not listed keep their declared
// order and append after.
type PatchOrderRequest struct {
	Body struct {
		ParentKey       string   `json:"parentKey" doc:"Parent ItemKey or synthetic root key"`
		OrderedChildren []string `json:"orderedChildren" doc:"Desired sibling order"`
	}
}

type PatchOrderResponse struct {
	Body models.NavOverride `json:"-"`
}

func (h *AdminNavigationHandler) PatchOrder(ctx context.Context, req *PatchOrderRequest) (*PatchOrderResponse, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	parentKeys, children := h.itemsIdx.Snapshot()
	doc, err := h.overrides.SetOrder(ctx, req.Body.ParentKey, req.Body.OrderedChildren, parentKeys, children, actor)
	if err != nil {
		return nil, err
	}
	return &PatchOrderResponse{Body: *doc}, nil
}

// DeleteOrderRequest references the parent whose override is being
// cleared. Missing-doc is a no-op (idempotent).
type DeleteOrderRequest struct {
	ParentKey string `query:"parentKey" required:"true" doc:"Parent ItemKey or synthetic root key whose override to clear"`
}

type DeleteOrderResponse struct{}

func (h *AdminNavigationHandler) DeleteOrder(ctx context.Context, req *DeleteOrderRequest) (*DeleteOrderResponse, error) {
	if err := h.overrides.ClearOrder(ctx, req.ParentKey); err != nil {
		return nil, err
	}
	return &DeleteOrderResponse{}, nil
}
