package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// AdminAPI is the admin service the handlers call (*services.AdminService).
type AdminAPI interface {
	Corpora(ctx context.Context) (*models.RetrievalCorporaReport, error)
	TenantIndexing(ctx context.Context) (*models.RetrievalTenantIndexingReport, error)
	Diagnose(ctx context.Context, corpus string, in models.RetrievalDiagnosticsRequest) (*models.RetrievalDiagnosticsReport, error)
	DropOrphanCorpus(ctx context.Context, corpus string) error
	SearchLog(ctx context.Context, q models.SearchLogQuery) (*models.RetrievalSearchLogPage, error)
}

// AdminHandler serves the five operator-host retrieval admin routes. A nil
// service (no database) answers every route 503 retrieval.unavailable.
type AdminHandler struct {
	svc    AdminAPI
	logger *slog.Logger
}

func NewAdminHandler(svc AdminAPI, logger *slog.Logger) *AdminHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AdminHandler{svc: svc, logger: logger}
}

var errNoService = errors.New("retrieval admin: service not wired")

// failure maps err for the client and logs the cause of a server-side
// failure, which the response never carries. The expected not_configured 503
// and every 4xx are not logged.
func (h *AdminHandler) failure(ctx context.Context, op string, err error) error {
	if errors.Is(err, errNoService) {
		return errcode.ServiceUnavailable(errcode.RetrievalUnavailable,
			"Retrieval administration needs a database, and none is configured.")
	}
	mapped := mapError(err)
	var e *errcode.Error
	if !errors.As(mapped, &e) || e.Status < http.StatusInternalServerError || e.ExpectedUnavailable() {
		return mapped
	}
	h.logger.ErrorContext(ctx, "retrieval admin: request failed",
		slog.String("op", op), slog.String("code", e.Code), slog.String("error", err.Error()))
	return mapped
}

func (h *AdminHandler) wired() error {
	if h.svc == nil {
		return errNoService
	}
	return nil
}

type RetrievalCorporaResponse struct {
	Body *models.RetrievalCorporaReport
}

// ListCorpora reports every registered corpus and every stored state, with
// engine and inference health. It never carries content or scores.
func (h *AdminHandler) ListCorpora(ctx context.Context, _ *struct{}) (*RetrievalCorporaResponse, error) {
	if err := h.wired(); err != nil {
		return nil, h.failure(ctx, "corpora", err)
	}
	rep, err := h.svc.Corpora(ctx)
	if err != nil {
		return nil, h.failure(ctx, "corpora", err)
	}
	return &RetrievalCorporaResponse{Body: rep}, nil
}

type RetrievalTenantIndexingResponse struct {
	Body *models.RetrievalTenantIndexingReport
}

// GetTenantIndexing reports the calling organization's indexing state.
func (h *AdminHandler) GetTenantIndexing(ctx context.Context, _ *struct{}) (*RetrievalTenantIndexingResponse, error) {
	if err := h.wired(); err != nil {
		return nil, h.failure(ctx, "tenant indexing", err)
	}
	rep, err := h.svc.TenantIndexing(ctx)
	if err != nil {
		return nil, h.failure(ctx, "tenant indexing", err)
	}
	return &RetrievalTenantIndexingResponse{Body: rep}, nil
}

// RetrievalCorpusPath is the path parameter of the per-corpus routes; the
// pattern is the corpus name rule of the registry.
type RetrievalCorpusPath struct {
	Corpus string `path:"corpus" pattern:"^[a-z][a-z0-9_]{0,31}$" doc:"Corpus name"`
}

type RetrievalDiagnosticsInput struct {
	RetrievalCorpusPath
	Body models.RetrievalDiagnosticsRequest
}

type RetrievalDiagnosticsResponse struct {
	Body *models.RetrievalDiagnosticsReport
}

// Diagnose runs one search over the normal pipeline and reports per-stage
// scores. The service audits it.
func (h *AdminHandler) Diagnose(ctx context.Context, in *RetrievalDiagnosticsInput) (*RetrievalDiagnosticsResponse, error) {
	if err := h.wired(); err != nil {
		return nil, h.failure(ctx, "diagnose", err)
	}
	rep, err := h.svc.Diagnose(ctx, in.Corpus, in.Body)
	if err != nil {
		return nil, h.failure(ctx, "diagnose", err)
	}
	return &RetrievalDiagnosticsResponse{Body: rep}, nil
}

type RetrievalDropInput struct {
	RetrievalCorpusPath
}

// DropCorpus deletes the state and collections of an unregistered corpus.
func (h *AdminHandler) DropCorpus(ctx context.Context, in *RetrievalDropInput) (*struct{}, error) {
	if err := h.wired(); err != nil {
		return nil, h.failure(ctx, "drop corpus", err)
	}
	if err := h.svc.DropOrphanCorpus(ctx, in.Corpus); err != nil {
		return nil, h.failure(ctx, "drop corpus", err)
	}
	return nil, nil
}

type RetrievalSearchLogInput struct {
	UserUUID string    `query:"userUuid" format:"uuid" doc:"Only searches by this user"`
	Since    time.Time `query:"since" doc:"Only searches at or after this time (RFC 3339)"`
	Until    time.Time `query:"until" doc:"Only searches before this time (RFC 3339)"`
	Limit    int       `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset   int       `query:"offset" default:"0" minimum:"0" maximum:"10000"`
}

type RetrievalSearchLogResponse struct {
	Body *models.RetrievalSearchLogPage
}

// ListSearchLog is one page of the calling organization's audited searches.
// A zero since or until means unset.
func (h *AdminHandler) ListSearchLog(ctx context.Context, in *RetrievalSearchLogInput) (*RetrievalSearchLogResponse, error) {
	if err := h.wired(); err != nil {
		return nil, h.failure(ctx, "search log", err)
	}
	q := models.SearchLogQuery{UserUUID: in.UserUUID, Limit: in.Limit, Offset: in.Offset}
	if !in.Since.IsZero() {
		q.Since = &in.Since
	}
	if !in.Until.IsZero() {
		q.Until = &in.Until
	}
	page, err := h.svc.SearchLog(ctx, q)
	if err != nil {
		return nil, h.failure(ctx, "search log", err)
	}
	return &RetrievalSearchLogResponse{Body: page}, nil
}
