package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
)

// ImportsHandler exposes the operator-facing import surface. POST
// runs the import synchronously and returns the final job view; GET
// endpoints are read-only audit lookups.
type ImportsHandler struct {
	svc *services.ImportService
}

// NewImportsHandler binds the handler to the import service.
func NewImportsHandler(svc *services.ImportService) *ImportsHandler {
	return &ImportsHandler{svc: svc}
}

// --- DTOs ---

// ImportJobView is the read shape returned to operators.
type ImportJobView struct {
	UUID                string                 `json:"uuid"`
	TenantID            string                 `json:"tenantId"`
	Importer            string                 `json:"importer"`
	SourceName          string                 `json:"sourceName,omitempty"`
	Status              models.ImportJobStatus `json:"status"`
	Stats               models.ImportJobStats  `json:"stats"`
	Error               string                 `json:"error,omitempty"`
	IdempotencyKey      string                 `json:"idempotencyKey,omitempty"`
	ConflictReviewUUIDs []string               `json:"conflictReviewUuids,omitempty"`
	CreatedAt           time.Time              `json:"createdAt"`
	StartedAt           *time.Time             `json:"startedAt,omitempty"`
	CompletedAt         *time.Time             `json:"completedAt,omitempty"`
	CreatedBy           string                 `json:"createdBy,omitempty"`
}

func toImportJobView(j *models.ImportJob) ImportJobView {
	return ImportJobView{
		UUID:                j.UUID,
		TenantID:            j.TenantID,
		Importer:            j.Importer,
		SourceName:          j.SourceName,
		Status:              j.Status,
		Stats:               j.Stats,
		Error:               j.Error,
		IdempotencyKey:      j.IdempotencyKey,
		ConflictReviewUUIDs: j.ConflictReviewUUIDs,
		CreatedAt:           j.CreatedAt,
		StartedAt:           j.StartedAt,
		CompletedAt:         j.CompletedAt,
		CreatedBy:           j.CreatedBy,
	}
}

// --- Request/response wrappers ---

type ListImportsInput struct {
	PaginatedQuery
}

type ListImportsResponse struct {
	Body struct {
		Items []ImportJobView `json:"items"`
		Meta  ListMeta        `json:"meta"`
	}
}

type GetImportInput struct {
	ID string `path:"id"`
}

type GetImportResponse struct {
	Body ImportJobView
}

// RunImportResponse is returned by POST /v1/marketing/imports.
// Phase 3 moved execution behind a background worker; the handler
// returns 202 Accepted with the queued job's UUID + Location header so
// the caller can poll GET /v1/marketing/imports/{id} for completion.
type RunImportResponse struct {
	Status   int    `header:"-"`
	Location string `header:"Location"`
	Body     ImportJobView
}

// --- Handler methods ---

func (h *ImportsHandler) List(ctx context.Context, in *ListImportsInput) (*ListImportsResponse, error) {
	got, err := h.svc.ListImports(ctx, in.Limit, in.Skip)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]ImportJobView, 0, len(got))
	for i := range got {
		items = append(items, toImportJobView(&got[i]))
	}
	resp := &ListImportsResponse{}
	resp.Body.Items = items
	resp.Body.Meta = ListMeta{Limit: in.Limit, Skip: in.Skip, Count: len(items)}
	return resp, nil
}

func (h *ImportsHandler) Get(ctx context.Context, in *GetImportInput) (*GetImportResponse, error) {
	got, err := h.svc.GetImport(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrImportJobNotFound) {
			return nil, huma.Error404NotFound("import job not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetImportResponse{Body: toImportJobView(got)}, nil
}

// Run accepts a multipart upload (`file` + `mapping` JSON string +
// optional `sourceName` + optional Idempotency-Key header) and
// enqueues the import. Returns 202 Accepted with the queued job's
// UUID; the caller polls GET /v1/marketing/imports/{id} until status
// is done / failed / paused_for_review.
//
// The handler extracts the raw HTTP request from context (the same
// pattern the rag addon uses for document uploads) because Huma does
// not yet have a first-class multipart-form binding.
func (h *ImportsHandler) Run(ctx context.Context, _ *struct{}) (*RunImportResponse, error) {
	httpReq, ok := ctx.Value("http_request").(*http.Request)
	if !ok || httpReq == nil {
		return nil, huma.Error500InternalServerError("http request not available on context")
	}

	// 32MB cap on the in-memory portion; larger files spill to a
	// temp directory courtesy of mime/multipart.
	if err := httpReq.ParseMultipartForm(32 << 20); err != nil {
		return nil, huma.Error400BadRequest("failed to parse multipart form: " + err.Error())
	}

	file, header, err := httpReq.FormFile("file")
	if err != nil {
		return nil, huma.Error400BadRequest("file is required (multipart form field 'file'): " + err.Error())
	}
	defer file.Close()

	body, err := io.ReadAll(file)
	if err != nil {
		return nil, huma.Error400BadRequest("failed to read uploaded file: " + err.Error())
	}

	mappingRaw := httpReq.FormValue("mapping")
	if mappingRaw == "" {
		return nil, huma.Error400BadRequest("mapping is required (multipart form field 'mapping' — JSON-encoded ColumnMapping)")
	}
	var mapping importers.ColumnMapping
	if err := json.Unmarshal([]byte(mappingRaw), &mapping); err != nil {
		return nil, huma.Error400BadRequest("invalid mapping JSON: " + err.Error())
	}

	importerName := httpReq.FormValue("importer")
	if importerName == "" {
		importerName = "csv"
	}
	sourceName := httpReq.FormValue("sourceName")
	if sourceName == "" {
		sourceName = header.Filename
	}

	explicitKey := httpReq.Header.Get("Idempotency-Key")

	job, err := h.svc.Enqueue(ctx, importerName, sourceName, body, mapping, []byte(mappingRaw), explicitKey)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrUnknownImporter):
			return nil, huma.Error400BadRequest(err.Error())
		case errors.Is(err, services.ErrImportFailed):
			return nil, huma.Error500InternalServerError(err.Error())
		default:
			return nil, huma.Error500InternalServerError(err.Error())
		}
	}
	return &RunImportResponse{
		Status:   http.StatusAccepted,
		Location: "/v1/marketing/imports/" + job.UUID,
		Body:     toImportJobView(job),
	}, nil
}

// --- Route registration ---

// RegisterImportReadRoutes — gate with `marketing.contact.read`.
func RegisterImportReadRoutes(api huma.API, h *ImportsHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-imports",
		Method:      http.MethodGet, Path: "/v1/marketing/imports",
		Summary: "List import jobs", Tags: []string{"Marketing - Imports"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-import",
		Method:      http.MethodGet, Path: "/v1/marketing/imports/{id}",
		Summary: "Get an import job", Tags: []string{"Marketing - Imports"},
	}, h.Get)
}

// RegisterImportRunRoutes mounts the multipart upload. Gate the
// subgroup with `marketing.import.run`. SkipValidateBody is set
// because huma cannot validate multipart inputs through its
// generated schema — the handler validates manually.
//
// The route returns 202 Accepted with the queued job UUID + a
// Location header pointing at GET /v1/marketing/imports/{id}; the
// caller polls that endpoint until status transitions to done /
// failed / paused_for_review.
func RegisterImportRunRoutes(api huma.API, h *ImportsHandler) {
	huma.Register(api, huma.Operation{
		OperationID:      "marketing-run-import",
		Method:           http.MethodPost,
		Path:             "/v1/marketing/imports",
		Summary:          "Enqueue an import job",
		Description:      "Upload a CSV/Excel/Odoo payload with a column mapping JSON; the import runs asynchronously in the background worker. Returns 202 Accepted with the queued job UUID. Multipart fields: file (required), mapping (required, JSON ColumnMapping), importer (optional, defaults to csv), sourceName (optional, defaults to filename). Optional Idempotency-Key request header dedups identical submissions within 24h.",
		Tags:             []string{"Marketing - Imports"},
		SkipValidateBody: true,
		DefaultStatus:    http.StatusAccepted,
	}, h.Run)
}
