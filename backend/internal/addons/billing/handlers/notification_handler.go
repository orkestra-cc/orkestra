package handlers

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/services"
)

// NotificationHandler handles notification-related HTTP requests
type NotificationHandler struct {
	notificationService services.NotificationService
}

// NewNotificationHandler creates a new NotificationHandler
func NewNotificationHandler(notificationService services.NotificationService) *NotificationHandler {
	return &NotificationHandler{
		notificationService: notificationService,
	}
}

// ========================================
// Request/Response Types
// ========================================

// GetNotificationRequest represents the request to get a notification
type GetNotificationRequest struct {
	ID string `path:"id" doc:"Notification UUID"`
}

// GetNotificationResponse represents the response with notification details
type GetNotificationResponse struct {
	Body models.SDINotification `json:"notification" doc:"Notification details"`
}

// ListSDINotificationsRequest represents the request to list notifications
type ListSDINotificationsRequest struct {
	InvoiceUUID      string `query:"invoiceId" doc:"Filter by invoice UUID"`
	NotificationType string `query:"type" enum:"RC,NS,MC,NE,DT,AT" doc:"Filter by notification type"`
	Processed        string `query:"processed" enum:"true,false" doc:"Filter by processed status"`
	FromDate         string `query:"fromDate" doc:"Filter notifications from this date (YYYY-MM-DD)"`
	ToDate           string `query:"toDate" doc:"Filter notifications to this date (YYYY-MM-DD)"`
	Page             int    `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize         int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// ListSDINotificationsResponse represents the paginated list of notifications
type ListSDINotificationsResponse struct {
	Body struct {
		Notifications []models.SDINotification `json:"notifications" doc:"List of notifications"`
		Total         int64                    `json:"total" doc:"Total count"`
		Page          int                      `json:"page" doc:"Current page"`
		PageSize      int                      `json:"pageSize" doc:"Page size"`
		TotalPages    int                      `json:"totalPages" doc:"Total pages"`
	}
}

// MarkProcessedRequest represents the request to mark a notification as processed
type MarkProcessedRequest struct {
	ID string `path:"id" doc:"Notification UUID"`
}

// MarkProcessedResponse represents the response after marking as processed
type MarkProcessedResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// GetSummaryRequest represents the request to get notification summary
type GetSummaryRequest struct {
	FromDate string `query:"fromDate" doc:"Filter notifications from this date (YYYY-MM-DD)"`
	ToDate   string `query:"toDate" doc:"Filter notifications to this date (YYYY-MM-DD)"`
}

// GetSummaryResponse represents the response with notification summary
type GetSummaryResponse struct {
	Body models.NotificationSummary `json:"summary" doc:"Notification summary"`
}

// ========================================
// Handler Methods
// ========================================

// GetNotification retrieves a notification by ID
func (h *NotificationHandler) GetNotification(ctx context.Context, req *GetNotificationRequest) (*GetNotificationResponse, error) {
	notification, err := h.notificationService.GetNotification(ctx, req.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Notification not found", err)
	}

	return &GetNotificationResponse{Body: *notification}, nil
}

// ListNotifications lists notifications with filtering and pagination
func (h *NotificationHandler) ListNotifications(ctx context.Context, req *ListSDINotificationsRequest) (*ListSDINotificationsResponse, error) {
	filters := &models.NotificationFilters{
		InvoiceUUID: req.InvoiceUUID,
	}

	// Parse processed filter
	if req.Processed != "" {
		processed := req.Processed == "true"
		filters.Processed = &processed
	}

	// Parse notification type
	if req.NotificationType != "" {
		filters.NotificationType = models.NotificationType(req.NotificationType)
	}

	// Parse dates
	if req.FromDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.FromDate); err == nil {
			filters.FromDate = &parsed
		}
	}
	if req.ToDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.ToDate); err == nil {
			endOfDay := parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			filters.ToDate = &endOfDay
		}
	}

	pagination := models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	notifications, total, err := h.notificationService.ListNotifications(ctx, filters, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list notifications", err)
	}

	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	resp := &ListSDINotificationsResponse{}
	resp.Body.Notifications = notifications
	resp.Body.Total = total
	resp.Body.Page = pagination.Page
	resp.Body.PageSize = pagination.PageSize
	resp.Body.TotalPages = totalPages

	return resp, nil
}

// MarkAsProcessed marks a notification as processed
func (h *NotificationHandler) MarkAsProcessed(ctx context.Context, req *MarkProcessedRequest) (*MarkProcessedResponse, error) {
	userID := getUserIDFromContext(ctx)

	err := h.notificationService.MarkAsProcessed(ctx, req.ID, userID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to mark notification as processed", err)
	}

	resp := &MarkProcessedResponse{}
	resp.Body.Message = "Notification marked as processed"
	return resp, nil
}

// GetSummary returns notification summary statistics
func (h *NotificationHandler) GetSummary(ctx context.Context, req *GetSummaryRequest) (*GetSummaryResponse, error) {
	var fromDate, toDate *time.Time
	if req.FromDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.FromDate); err == nil {
			fromDate = &parsed
		}
	}
	if req.ToDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.ToDate); err == nil {
			end := parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			toDate = &end
		}
	}

	summary, err := h.notificationService.GetSummary(ctx, fromDate, toDate)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get notification summary", err)
	}

	return &GetSummaryResponse{Body: *summary}, nil
}
