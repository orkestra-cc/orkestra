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

// TagHandler exposes CRUD on marketing_tags.
type TagHandler struct {
	svc *services.TagService
}

// NewTagHandler binds the handler.
func NewTagHandler(svc *services.TagService) *TagHandler {
	return &TagHandler{svc: svc}
}

// --- DTOs ---

type TagPayload struct {
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty" doc:"Stable machine identifier; auto-derived from name when omitted"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
	ParentUUID  string `json:"parentUuid,omitempty" doc:"UUID of the parent tag; empty for root tags"`
}

type TagView struct {
	UUID        string `json:"uuid"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
	ParentUUID  string `json:"parentUuid,omitempty"`
	Path        string `json:"path"`
	timestampedView
}

func toTagView(t *models.Tag) TagView {
	return TagView{
		UUID:        t.UUID,
		TenantID:    t.TenantID,
		Name:        t.Name,
		Slug:        t.Slug,
		Description: t.Description,
		Color:       t.Color,
		ParentUUID:  t.ParentUUID,
		Path:        t.Path,
		timestampedView: timestampedView{
			CreatedAt: t.CreatedAt,
			UpdatedAt: t.UpdatedAt,
		},
	}
}

// --- Request/response wrappers ---

type ListTagsResponse struct {
	Body struct {
		Items []TagView `json:"items"`
	}
}

type GetTagInput struct {
	ID string `path:"id"`
}

type GetTagResponse struct {
	Body TagView
}

type CreateTagInput struct {
	Body TagPayload
}

type CreateTagResponse struct {
	Body TagView
}

type UpdateTagInput struct {
	ID   string `path:"id"`
	Body map[string]any
}

type UpdateTagResponse struct {
	Body TagView
}

type DeleteTagInput struct {
	ID string `path:"id"`
}

// --- Handler methods ---

func (h *TagHandler) List(ctx context.Context, _ *struct{}) (*ListTagsResponse, error) {
	got, err := h.svc.List(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]TagView, 0, len(got))
	for i := range got {
		items = append(items, toTagView(&got[i]))
	}
	resp := &ListTagsResponse{}
	resp.Body.Items = items
	return resp, nil
}

func (h *TagHandler) Get(ctx context.Context, in *GetTagInput) (*GetTagResponse, error) {
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrTagNotFound) {
			return nil, huma.Error404NotFound("tag not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetTagResponse{Body: toTagView(got)}, nil
}

func (h *TagHandler) Create(ctx context.Context, in *CreateTagInput) (*CreateTagResponse, error) {
	t := &models.Tag{
		Name:        in.Body.Name,
		Slug:        in.Body.Slug,
		Description: in.Body.Description,
		Color:       in.Body.Color,
		ParentUUID:  in.Body.ParentUUID,
	}
	got, err := h.svc.Create(ctx, t)
	if err != nil {
		if errors.Is(err, services.ErrInvalidTag) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &CreateTagResponse{Body: toTagView(got)}, nil
}

func (h *TagHandler) Update(ctx context.Context, in *UpdateTagInput) (*UpdateTagResponse, error) {
	got, err := h.svc.Update(ctx, in.ID, in.Body)
	if err != nil {
		if errors.Is(err, repository.ErrTagNotFound) {
			return nil, huma.Error404NotFound("tag not found")
		}
		if errors.Is(err, services.ErrInvalidTag) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &UpdateTagResponse{Body: toTagView(got)}, nil
}

func (h *TagHandler) Delete(ctx context.Context, in *DeleteTagInput) (*SuccessResponse, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		if errors.Is(err, repository.ErrTagNotFound) {
			return nil, huma.Error404NotFound("tag not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &SuccessResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- Route registration ---

// RegisterTagReadRoutes — gate with `marketing.contact.read`.
func RegisterTagReadRoutes(api huma.API, h *TagHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-tags",
		Method:      http.MethodGet, Path: "/v1/marketing/tags",
		Summary: "List tags", Tags: []string{"Marketing - Tags"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-tag",
		Method:      http.MethodGet, Path: "/v1/marketing/tags/{id}",
		Summary: "Get a tag", Tags: []string{"Marketing - Tags"},
	}, h.Get)
}

// RegisterTagWriteRoutes — gate with `marketing.contact.write`.
func RegisterTagWriteRoutes(api huma.API, h *TagHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-create-tag",
		Method:      http.MethodPost, Path: "/v1/marketing/tags",
		Summary: "Create a tag", Tags: []string{"Marketing - Tags"},
		DefaultStatus: http.StatusCreated,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-update-tag",
		Method:      http.MethodPatch, Path: "/v1/marketing/tags/{id}",
		Summary: "Update a tag", Tags: []string{"Marketing - Tags"},
	}, h.Update)
}

// RegisterTagDeleteRoutes — gate with `marketing.contact.delete`.
func RegisterTagDeleteRoutes(api huma.API, h *TagHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-delete-tag",
		Method:      http.MethodDelete, Path: "/v1/marketing/tags/{id}",
		Summary: "Delete a tag", Tags: []string{"Marketing - Tags"},
	}, h.Delete)
}
