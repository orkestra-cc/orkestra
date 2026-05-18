package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// CustomFieldSchemaHandler exposes CRUD on
// marketing_custom_field_schemas.
type CustomFieldSchemaHandler struct {
	svc *services.CustomFieldService
}

// NewCustomFieldSchemaHandler binds the handler.
func NewCustomFieldSchemaHandler(svc *services.CustomFieldService) *CustomFieldSchemaHandler {
	return &CustomFieldSchemaHandler{svc: svc}
}

// --- DTOs ---

type CustomFieldSchemaPayload struct {
	TargetCollection   models.CustomFieldTarget `json:"targetCollection" doc:"persons | organizations"`
	Fields             []models.FieldDef        `json:"fields"`
	AllowUnknownFields bool                     `json:"allowUnknownFields,omitempty"`
}

type CustomFieldSchemaView struct {
	UUID               string                   `json:"uuid"`
	TenantID           string                   `json:"tenantId"`
	TargetCollection   models.CustomFieldTarget `json:"targetCollection"`
	Fields             []models.FieldDef        `json:"fields"`
	AllowUnknownFields bool                     `json:"allowUnknownFields"`
	Version            int                      `json:"version"`
	timestampedView
}

func toSchemaView(s *models.CustomFieldSchema) CustomFieldSchemaView {
	return CustomFieldSchemaView{
		UUID:               s.UUID,
		TenantID:           s.TenantID,
		TargetCollection:   s.TargetCollection,
		Fields:             s.Fields,
		AllowUnknownFields: s.AllowUnknownFields,
		Version:            s.Version,
		timestampedView: timestampedView{
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
		},
	}
}

// --- Request/response wrappers ---

type ListSchemasResponse struct {
	Body struct {
		Items []CustomFieldSchemaView `json:"items"`
	}
}

type GetSchemaInput struct {
	Target string `path:"target" doc:"persons | organizations"`
}

type GetSchemaResponse struct {
	Body CustomFieldSchemaView
}

type UpsertSchemaInput struct {
	Body CustomFieldSchemaPayload
}

type UpsertSchemaResponse struct {
	Body CustomFieldSchemaView
}

type DeleteSchemaInput struct {
	Target string `path:"target"`
}

// --- Handler methods ---

func (h *CustomFieldSchemaHandler) List(ctx context.Context, _ *struct{}) (*ListSchemasResponse, error) {
	got, err := h.svc.List(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]CustomFieldSchemaView, 0, len(got))
	for i := range got {
		items = append(items, toSchemaView(&got[i]))
	}
	resp := &ListSchemasResponse{}
	resp.Body.Items = items
	return resp, nil
}

func (h *CustomFieldSchemaHandler) Get(ctx context.Context, in *GetSchemaInput) (*GetSchemaResponse, error) {
	got, err := h.svc.GetForTarget(ctx, models.CustomFieldTarget(in.Target))
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if got == nil {
		return nil, huma.Error404NotFound("no schema configured for target " + in.Target)
	}
	return &GetSchemaResponse{Body: toSchemaView(got)}, nil
}

func (h *CustomFieldSchemaHandler) Upsert(ctx context.Context, in *UpsertSchemaInput) (*UpsertSchemaResponse, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	got, err := h.svc.Upsert(ctx, in.Body.TargetCollection, in.Body.Fields, in.Body.AllowUnknownFields, actor)
	if err != nil {
		if errors.Is(err, services.ErrCustomFieldValidation) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &UpsertSchemaResponse{Body: toSchemaView(got)}, nil
}

func (h *CustomFieldSchemaHandler) Delete(ctx context.Context, in *DeleteSchemaInput) (*SuccessResponse, error) {
	if err := h.svc.Delete(ctx, models.CustomFieldTarget(in.Target)); err != nil {
		if errors.Is(err, repository.ErrCustomFieldSchemaNotFound) {
			return nil, huma.Error404NotFound("no schema configured for target " + in.Target)
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &SuccessResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- Route registration ---

// RegisterCustomFieldReadRoutes — gate with `marketing.contact.read`.
func RegisterCustomFieldReadRoutes(api huma.API, h *CustomFieldSchemaHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-custom-field-schemas",
		Method:      http.MethodGet, Path: "/v1/marketing/custom-field-schemas",
		Summary: "List custom-field schemas", Tags: []string{"Marketing - Custom Fields"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-custom-field-schema",
		Method:      http.MethodGet, Path: "/v1/marketing/custom-field-schemas/{target}",
		Summary: "Get the custom-field schema for a target collection", Tags: []string{"Marketing - Custom Fields"},
	}, h.Get)
}

// RegisterCustomFieldWriteRoutes — gate with `marketing.contact.write`.
//
// Upsert lives at PUT /v1/marketing/custom-field-schemas/{target} so
// the admin UI does not need to know whether the schema exists yet —
// idempotent replace covers both create and update.
func RegisterCustomFieldWriteRoutes(api huma.API, h *CustomFieldSchemaHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-upsert-custom-field-schema",
		Method:      http.MethodPut, Path: "/v1/marketing/custom-field-schemas",
		Summary: "Create or replace the schema for a target collection", Tags: []string{"Marketing - Custom Fields"},
	}, h.Upsert)
}

// RegisterCustomFieldDeleteRoutes — gate with `marketing.contact.delete`.
func RegisterCustomFieldDeleteRoutes(api huma.API, h *CustomFieldSchemaHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-delete-custom-field-schema",
		Method:      http.MethodDelete, Path: "/v1/marketing/custom-field-schemas/{target}",
		Summary: "Delete the schema for a target collection", Tags: []string{"Marketing - Custom Fields"},
	}, h.Delete)
}
