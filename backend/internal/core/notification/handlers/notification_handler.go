package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// NotificationHandler exposes admin + user endpoints for the notification module.
type NotificationHandler struct {
	svc *services.NotificationService
}

func NewNotificationHandler(svc *services.NotificationService) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

// --- Admin endpoints ---

type listNotificationsRequest struct {
	Category string `query:"category" doc:"Filter by category"`
	Status   string `query:"status" doc:"Filter by status"`
	Sender   string `query:"sender" doc:"Filter by sender profile slug"`
	Limit    int64  `query:"limit" doc:"Max rows (default 100)"`
}

type listNotificationsResponse struct {
	Body struct {
		Items []*models.NotificationDoc `json:"items"`
	}
}

func (h *NotificationHandler) ListNotifications(ctx context.Context, req *listNotificationsRequest) (*listNotificationsResponse, error) {
	items, err := h.svc.LogRepo().List(ctx, repository.Filter{
		Category:   req.Category,
		Status:     req.Status,
		SenderSlug: req.Sender,
	}, req.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("list notifications failed", err)
	}
	resp := &listNotificationsResponse{}
	resp.Body.Items = items
	return resp, nil
}

type testEmailRequest struct {
	Body struct {
		To       string `json:"to" doc:"Recipient email address"`
		Subject  string `json:"subject,omitempty" doc:"Optional subject override"`
		BodyText string `json:"bodyText,omitempty" doc:"Optional body override"`
		Sender   string `json:"sender,omitempty" doc:"Sender profile slug to test; empty uses the default (*) profile"`
	}
}

type testEmailResponse struct {
	Body struct {
		Success  bool   `json:"success"`
		Provider string `json:"provider"`
		Sender   string `json:"sender" doc:"Slug of the profile that carried the message"`
		Message  string `json:"message"`
	}
}

func (h *NotificationHandler) SendTestEmail(ctx context.Context, req *testEmailRequest) (*testEmailResponse, error) {
	if req.Body.To == "" {
		return nil, huma.Error400BadRequest("recipient required", nil)
	}
	subject := req.Body.Subject
	if subject == "" {
		subject = "Orkestra test email"
	}
	body := req.Body.BodyText
	if body == "" {
		body = "This is a test email sent from the Orkestra notification module at " + time.Now().Format(time.RFC3339)
	}
	res, err := h.svc.SendTest(ctx, services.TestSendInput{To: req.Body.To, Subject: subject, BodyText: body, Sender: req.Body.Sender})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrSenderNotFound):
			return nil, errcode.NotFound(errcode.NotificationSenderNotFound, "No sender profile has that slug.")
		case errors.Is(err, services.ErrNoSenderForCategory), errors.Is(err, services.ErrUnknownDriver), errors.Is(err, services.ErrSenderNotConfigured):
			return nil, errcode.UnprocessableEntity(errcode.NotificationSenderIncomplete, "The sender profile cannot send yet: "+res.Diagnostic)
		default:
			return nil, errcode.New(http.StatusBadGateway, errcode.NotificationSendFailed, "The sender did not accept the test message: "+res.Diagnostic)
		}
	}
	resp := &testEmailResponse{}
	resp.Body.Success = true
	resp.Body.Provider = res.Provider
	resp.Body.Sender = res.SenderSlug
	resp.Body.Message = "Test email dispatched"
	return resp, nil
}

type listTemplatesResponse struct {
	Body struct {
		Items []*models.TemplateDoc `json:"items"`
	}
}

func (h *NotificationHandler) ListTemplates(ctx context.Context, _ *struct{}) (*listTemplatesResponse, error) {
	items, err := h.svc.TemplateService().List(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list templates failed", err)
	}
	resp := &listTemplatesResponse{}
	resp.Body.Items = items
	return resp, nil
}

type getTemplateRequest struct {
	TemplateID string `path:"templateId"`
	Locale     string `query:"locale"`
}

type getTemplateResponse struct {
	Body *models.TemplateDoc `json:"body"`
}

func (h *NotificationHandler) GetTemplate(ctx context.Context, req *getTemplateRequest) (*getTemplateResponse, error) {
	doc, err := h.svc.TemplateService().Get(ctx, req.TemplateID, req.Locale)
	if err != nil {
		return nil, huma.Error404NotFound("template not found", err)
	}
	return &getTemplateResponse{Body: doc}, nil
}

type updateTemplateRequest struct {
	TemplateID string `path:"templateId"`
	Body       struct {
		Locale      string   `json:"locale"`
		Subject     string   `json:"subject"`
		BodyText    string   `json:"bodyText"`
		BodyHTML    string   `json:"bodyHtml"`
		Description string   `json:"description,omitempty"`
		Variables   []string `json:"variables,omitempty"`
	}
}

func (h *NotificationHandler) UpdateTemplate(ctx context.Context, req *updateTemplateRequest) (*getTemplateResponse, error) {
	doc := &models.TemplateDoc{
		TemplateID:  req.TemplateID,
		Locale:      req.Body.Locale,
		Channel:     models.ChannelEmail,
		Subject:     req.Body.Subject,
		BodyText:    req.Body.BodyText,
		BodyHTML:    req.Body.BodyHTML,
		Description: req.Body.Description,
		Variables:   req.Body.Variables,
		IsSystem:    false,
	}
	if err := h.svc.TemplateService().Upsert(ctx, doc); err != nil {
		return nil, huma.Error500InternalServerError("update template failed", err)
	}
	updated, _ := h.svc.TemplateService().Get(ctx, req.TemplateID, req.Body.Locale)
	return &getTemplateResponse{Body: updated}, nil
}

type deleteTemplateRequest struct {
	TemplateID string `path:"templateId"`
	Locale     string `query:"locale"`
}

type emptyResponse struct{}

func (h *NotificationHandler) DeleteTemplate(ctx context.Context, req *deleteTemplateRequest) (*emptyResponse, error) {
	if err := h.svc.TemplateService().Delete(ctx, req.TemplateID, req.Locale); err != nil {
		return nil, huma.Error500InternalServerError("delete template failed", err)
	}
	// Reseed defaults so system templates come back if the deleted one was a system default.
	_ = h.svc.TemplateService().SeedDefaults(ctx)
	return &emptyResponse{}, nil
}

// --- User-facing endpoints ---

type listPreferencesResponse struct {
	Body struct {
		Items []*models.PreferenceDoc `json:"items"`
	}
}

func (h *NotificationHandler) ListMyPreferences(ctx context.Context, _ *struct{}) (*listPreferencesResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required", nil)
	}
	items, err := h.svc.PreferenceService().List(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list preferences failed", err)
	}
	resp := &listPreferencesResponse{}
	resp.Body.Items = items
	return resp, nil
}

type updatePreferenceRequest struct {
	Body struct {
		Category string `json:"category"`
		Channel  string `json:"channel"`
		OptedIn  bool   `json:"optedIn"`
	}
}

func (h *NotificationHandler) UpdateMyPreference(ctx context.Context, req *updatePreferenceRequest) (*emptyResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required", nil)
	}
	channel := req.Body.Channel
	if channel == "" {
		channel = models.ChannelEmail
	}
	if err := h.svc.PreferenceService().Set(ctx, userUUID, req.Body.Category, channel, req.Body.OptedIn); err != nil {
		return nil, huma.Error500InternalServerError("update preference failed", err)
	}
	return &emptyResponse{}, nil
}

// --- Public endpoint ---

type unsubscribeRequest struct {
	Token string `query:"token"`
}

type unsubscribeResponse struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

func (h *NotificationHandler) Unsubscribe(ctx context.Context, req *unsubscribeRequest) (*unsubscribeResponse, error) {
	doc, err := h.svc.UnsubscribeService().ConsumeToken(ctx, req.Token)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid or expired token", err)
	}
	// If a category is set, opt out of just that category; otherwise opt out
	// of all marketing for this user.
	category := doc.Category
	if category == "" {
		category = "marketing"
	}
	if doc.UserUUID != "" {
		_ = h.svc.PreferenceService().Set(ctx, doc.UserUUID, category, models.ChannelEmail, false)
	}
	_ = h.svc.UnsubscribeService().MarkUsed(ctx, req.Token)

	resp := &unsubscribeResponse{}
	resp.Body.Success = true
	resp.Body.Message = "You have been unsubscribed. Security-related emails will still be delivered."
	return resp, nil
}

// RegisterAdminRoutes registers the admin-only endpoints (delivery log,
// templates, test email) on an API that has already been gated by the
// administrator role middleware upstream.
func (h *NotificationHandler) RegisterAdminRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "notifications-list",
		Method:      http.MethodGet,
		Path:        "/v1/notifications",
		Summary:     "List notification delivery log",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ListNotifications)

	huma.Register(api, huma.Operation{
		OperationID: "notifications-test",
		Method:      http.MethodPost,
		Path:        "/v1/notifications/test",
		Summary:     "Send a test email using the current SMTP settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.SendTestEmail)

	huma.Register(api, huma.Operation{
		OperationID: "notifications-list-templates",
		Method:      http.MethodGet,
		Path:        "/v1/notifications/templates",
		Summary:     "List notification templates",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ListTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "notifications-get-template",
		Method:      http.MethodGet,
		Path:        "/v1/notifications/templates/{templateId}",
		Summary:     "Get a notification template",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.GetTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "notifications-update-template",
		Method:      http.MethodPut,
		Path:        "/v1/notifications/templates/{templateId}",
		Summary:     "Update or override a notification template",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.UpdateTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "notifications-delete-template",
		Method:      http.MethodDelete,
		Path:        "/v1/notifications/templates/{templateId}",
		Summary:     "Delete (and reseed) a notification template",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.DeleteTemplate)
}

// RegisterUserRoutes registers the per-user endpoints (preferences) on an
// API gated by a plain authentication check.
func (h *NotificationHandler) RegisterUserRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "notifications-my-preferences",
		Method:      http.MethodGet,
		Path:        "/v1/notifications/preferences",
		Summary:     "Get current user's notification preferences",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ListMyPreferences)

	huma.Register(api, huma.Operation{
		OperationID: "notifications-update-preference",
		Method:      http.MethodPut,
		Path:        "/v1/notifications/preferences",
		Summary:     "Update a notification preference",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.UpdateMyPreference)
}

// RegisterPublicRoutes registers the public unsubscribe endpoint.
func (h *NotificationHandler) RegisterPublicRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "notifications-unsubscribe",
		Method:      http.MethodGet,
		Path:        "/v1/notifications/unsubscribe",
		Summary:     "Consume an unsubscribe token",
		Tags:        []string{"Notifications"},
	}, h.Unsubscribe)
}
