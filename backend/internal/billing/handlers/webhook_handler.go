package handlers

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/orkestra/backend/internal/billing/jobs"
	"github.com/orkestra/backend/internal/billing/services"
)

// WebhookHandler handles incoming SDI webhook callbacks from OpenAPI.it
type WebhookHandler struct {
	pollingJob    *jobs.PollingJob
	webhookSecret string
	logger        *slog.Logger
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(
	pollingJob *jobs.PollingJob,
	webhookSecret string,
	logger *slog.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		pollingJob:    pollingJob,
		webhookSecret: webhookSecret,
		logger:        logger,
	}
}

// WebhookRequest represents the incoming webhook payload from OpenAPI SDI
type WebhookRequest struct {
	Body struct {
		Event   string `json:"event" doc:"Event type: supplier-invoice, customer-notification, legal-storage-receipt"`
		UUID    string `json:"uuid,omitempty" doc:"Invoice or document UUID"`
		Payload struct {
			// Supplier invoice fields
			InvoiceUUID string `json:"invoice_uuid,omitempty" doc:"Invoice UUID from OpenAPI"`
			SDIFileID   string `json:"sdi_file_id,omitempty" doc:"SDI file identifier"`

			// Notification fields
			Type             string `json:"type,omitempty" doc:"Notification type: RC, NS, MC, NE, DT, AT"`
			Outcome          string `json:"outcome,omitempty" doc:"Notification outcome (for NE)"`
			ErrorCode        string `json:"error_code,omitempty" doc:"Error code if rejected"`
			ErrorDescription string `json:"error_description,omitempty" doc:"Error description if rejected"`
			RawContent       string `json:"raw_content,omitempty" doc:"Raw notification content"`

			// Legal storage fields
			Status string `json:"status,omitempty" doc:"Preserved document status"`
		} `json:"payload,omitempty" doc:"Event-specific payload data"`
	}
	// Authorization header for secret verification
	Authorization string `header:"Authorization" doc:"Bearer token for webhook authentication"`
}

// WebhookResponse represents the response to a webhook callback
type WebhookResponse struct {
	Body struct {
		Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
		Message string `json:"message" doc:"Processing result message"`
	}
}

// HandleCallback handles POST /v1/billing/webhooks/sdi
func (h *WebhookHandler) HandleCallback(ctx context.Context, req *WebhookRequest) (*WebhookResponse, error) {
	// Verify authorization
	if h.webhookSecret != "" {
		expected := "Bearer " + h.webhookSecret
		if subtle.ConstantTimeCompare([]byte(req.Authorization), []byte(expected)) != 1 {
			h.logger.Warn("webhook authentication failed",
				"event", req.Body.Event,
			)
			return &WebhookResponse{
				Body: struct {
					Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
					Message string `json:"message" doc:"Processing result message"`
				}{
					Success: false,
					Message: "Unauthorized",
				},
			}, huma401Error("invalid webhook authorization")
		}
	}

	h.logger.Info("received SDI webhook callback",
		"event", req.Body.Event,
		"uuid", req.Body.UUID,
	)

	switch req.Body.Event {
	case "supplier-invoice":
		return h.handleSupplierInvoice(ctx, req)
	case "customer-notification":
		return h.handleCustomerNotification(ctx, req)
	case "legal-storage-receipt":
		return h.handleLegalStorageReceipt(ctx, req)
	default:
		h.logger.Warn("unknown webhook event type",
			"event", req.Body.Event,
		)
		return &WebhookResponse{
			Body: struct {
				Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
				Message string `json:"message" doc:"Processing result message"`
			}{
				Success: true,
				Message: "Unknown event type, ignored",
			},
		}, nil
	}
}

// handleSupplierInvoice processes a supplier-invoice webhook event
func (h *WebhookHandler) handleSupplierInvoice(ctx context.Context, req *WebhookRequest) (*WebhookResponse, error) {
	h.logger.Info("processing supplier-invoice webhook",
		"uuid", req.Body.UUID,
		"invoiceUUID", req.Body.Payload.InvoiceUUID,
	)

	// Trigger invoice sync to pick up the new invoice
	if h.pollingJob != nil {
		if err := h.pollingJob.SyncReceivedInvoices(ctx); err != nil {
			h.logger.Error("webhook: failed to sync invoices",
				"error", err,
			)
			return &WebhookResponse{
				Body: struct {
					Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
					Message string `json:"message" doc:"Processing result message"`
				}{
					Success: false,
					Message: "Failed to sync invoices: " + err.Error(),
				},
			}, nil
		}
	}

	return &WebhookResponse{
		Body: struct {
			Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
			Message string `json:"message" doc:"Processing result message"`
		}{
			Success: true,
			Message: "Supplier invoice processed",
		},
	}, nil
}

// handleCustomerNotification processes a customer-notification webhook event
func (h *WebhookHandler) handleCustomerNotification(ctx context.Context, req *WebhookRequest) (*WebhookResponse, error) {
	invoiceUUID := req.Body.UUID
	if req.Body.Payload.InvoiceUUID != "" {
		invoiceUUID = req.Body.Payload.InvoiceUUID
	}

	h.logger.Info("processing customer-notification webhook",
		"invoiceUUID", invoiceUUID,
		"notificationType", req.Body.Payload.Type,
	)

	if h.pollingJob != nil && req.Body.Payload.Type != "" {
		notification := &services.OpenAPINotification{
			UUID:             req.Body.UUID,
			InvoiceUUID:      invoiceUUID,
			Type:             req.Body.Payload.Type,
			Outcome:          req.Body.Payload.Outcome,
			ErrorCode:        req.Body.Payload.ErrorCode,
			ErrorDescription: req.Body.Payload.ErrorDescription,
			RawContent:       req.Body.Payload.RawContent,
		}

		if err := h.pollingJob.ProcessNotification(ctx, notification); err != nil {
			h.logger.Error("webhook: failed to process notification",
				"invoiceUUID", invoiceUUID,
				"type", req.Body.Payload.Type,
				"error", err,
			)
			return &WebhookResponse{
				Body: struct {
					Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
					Message string `json:"message" doc:"Processing result message"`
				}{
					Success: false,
					Message: "Failed to process notification: " + err.Error(),
				},
			}, nil
		}
	}

	return &WebhookResponse{
		Body: struct {
			Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
			Message string `json:"message" doc:"Processing result message"`
		}{
			Success: true,
			Message: "Customer notification processed",
		},
	}, nil
}

// handleLegalStorageReceipt processes a legal-storage-receipt webhook event
func (h *WebhookHandler) handleLegalStorageReceipt(ctx context.Context, req *WebhookRequest) (*WebhookResponse, error) {
	h.logger.Info("processing legal-storage-receipt webhook",
		"uuid", req.Body.UUID,
		"status", req.Body.Payload.Status,
	)

	// For legal storage receipts, trigger a full poll to update preserved document statuses
	if h.pollingJob != nil {
		if err := h.pollingJob.Poll(ctx); err != nil {
			h.logger.Error("webhook: failed to poll for legal storage updates",
				"error", err,
			)
			return &WebhookResponse{
				Body: struct {
					Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
					Message string `json:"message" doc:"Processing result message"`
				}{
					Success: false,
					Message: "Failed to process legal storage receipt: " + err.Error(),
				},
			}, nil
		}
	}

	return &WebhookResponse{
		Body: struct {
			Success bool   `json:"success" doc:"Whether the webhook was processed successfully"`
			Message string `json:"message" doc:"Processing result message"`
		}{
			Success: true,
			Message: "Legal storage receipt processed",
		},
	}, nil
}

// huma401Error creates an error that Huma will map to 401 status
func huma401Error(msg string) error {
	return huma401{message: msg}
}

type huma401 struct {
	message string
}

func (e huma401) Error() string {
	return e.message
}

func (e huma401) GetStatus() int {
	return http.StatusUnauthorized
}
