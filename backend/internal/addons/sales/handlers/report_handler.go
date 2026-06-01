package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/addons/sales/models"
	"github.com/orkestra/backend/internal/addons/sales/repository"
)

// ReportHandler handles report endpoints
type ReportHandler struct {
	repo      repository.ReportRepository
	jobRepo   repository.JobRepository
	reportGen interface {
		GenerateFromJob(job *models.Job) (*models.Report, error)
	}
}

// NewReportHandler creates a new ReportHandler
func NewReportHandler(repo repository.ReportRepository, jobRepo repository.JobRepository, reportGen interface {
	GenerateFromJob(job *models.Job) (*models.Report, error)
}) *ReportHandler {
	return &ReportHandler{repo: repo, jobRepo: jobRepo, reportGen: reportGen}
}

// ListReports handles GET /v1/sales/reports
func (h *ReportHandler) ListReports(ctx context.Context, req *models.ListReportsRequest) (*models.ListReportsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	reports, total, err := h.repo.ListByUser(ctx, userUUID, req.Page, req.PageSize)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list reports", err)
	}

	resp := &models.ListReportsResponse{}
	resp.Body.Reports = reports
	resp.Body.Total = total
	resp.Body.Page = req.Page
	resp.Body.PageSize = req.PageSize
	return resp, nil
}

// GetReport handles GET /v1/sales/reports/{uuid}
func (h *ReportHandler) GetReport(ctx context.Context, req *models.GetReportRequest) (*models.GetReportResponse, error) {
	report, err := h.repo.GetByUUID(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Report not found", err)
	}
	return &models.GetReportResponse{Body: *report}, nil
}

// GenerateReport handles POST /v1/sales/reports/generate/{jobUuid}
func (h *ReportHandler) GenerateReport(ctx context.Context, req *models.GenerateReportRequest) (*models.GetReportResponse, error) {
	// Check if report already exists for this job
	existing, _ := h.repo.GetByJobUUID(ctx, req.JobUUID)
	if existing != nil {
		return &models.GetReportResponse{Body: *existing}, nil
	}

	job, err := h.jobRepo.GetByUUID(ctx, req.JobUUID)
	if err != nil {
		return nil, huma.Error404NotFound("Job not found", err)
	}
	if job.Status != models.JobStatusCompleted {
		return nil, huma.Error422UnprocessableEntity("Job is not completed")
	}

	report, err := h.reportGen.GenerateFromJob(job)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to generate report", err)
	}

	return &models.GetReportResponse{Body: *report}, nil
}

// DeleteReport handles DELETE /v1/sales/reports/{uuid}
func (h *ReportHandler) DeleteReport(ctx context.Context, req *models.GetReportRequest) (*struct{}, error) {
	if err := h.repo.Delete(ctx, req.UUID); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete report", err)
	}
	return nil, nil
}

// DownloadMarkdown serves the Markdown content as a downloadable file.
// Registered directly on chi (not Huma) for raw HTTP response control.
func (h *ReportHandler) DownloadMarkdown(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")

	report, err := h.repo.GetByUUID(r.Context(), uuid)
	if err != nil {
		http.Error(w, "Report not found", http.StatusNotFound)
		return
	}

	filename := fmt.Sprintf("prospect-report-%s.md", report.CompanyName)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write([]byte(report.ContentMD))
}
