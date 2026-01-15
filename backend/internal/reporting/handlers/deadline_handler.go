package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/reporting/models"
	"github.com/orkestra/backend/internal/reporting/services"
)

// DeadlineHandler handles HTTP requests for deadline reports
type DeadlineHandler struct {
	deadlineService services.DeadlineService
}

// NewDeadlineHandler creates a new deadline handler
func NewDeadlineHandler(deadlineService services.DeadlineService) *DeadlineHandler {
	return &DeadlineHandler{
		deadlineService: deadlineService,
	}
}

// GetDeadlinesRequest represents the request to get deadlines
type GetDeadlinesRequest struct {
	// Query parameters for filtering
	EntityType string `query:"entityType" enum:"vehicle,user,medical" doc:"Filter by entity type"`
	Status     string `query:"status" enum:"expired,warning,ok" doc:"Filter by status"`
	Search     string `query:"search" doc:"Search by entity name"`

	// Pagination
	Page     int `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// GetDeadlinesResponse represents the response for deadlines
type GetDeadlinesResponse struct {
	Body models.DeadlineReportResponse `json:"report" doc:"Deadline report data"`
}

// GetDeadlines handles GET /api/v1/reports/deadlines
func (h *DeadlineHandler) GetDeadlines(ctx context.Context, req *GetDeadlinesRequest) (*GetDeadlinesResponse, error) {
	// Build the filters
	filters := models.DeadlineFilters{
		Search: req.Search,
	}

	if req.EntityType != "" {
		filters.EntityType = models.EntityType(req.EntityType)
	}

	if req.Status != "" {
		filters.Status = models.DeadlineStatus(req.Status)
	}

	// Build the pagination parameters
	pagination := models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	// Retrieve the deadlines
	report, err := h.deadlineService.GetAllDeadlines(ctx, filters, pagination)
	if err != nil {
		switch err {
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid input data", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to get deadlines", err)
		}
	}

	return &GetDeadlinesResponse{Body: *report}, nil
}
