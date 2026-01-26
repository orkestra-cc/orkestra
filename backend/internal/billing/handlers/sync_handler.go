package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/billing/jobs"
)

// SyncHandler handles manual SDI synchronization endpoints
type SyncHandler struct {
	pollingJob *jobs.PollingJob
}

// NewSyncHandler creates a new sync handler
func NewSyncHandler(pollingJob *jobs.PollingJob) *SyncHandler {
	return &SyncHandler{
		pollingJob: pollingJob,
	}
}

// SyncRequest is the request body for sync operations (currently empty)
type SyncRequest struct{}

// SyncResponse represents the response from a sync operation
type SyncResponse struct {
	Body struct {
		Success bool   `json:"success" doc:"Whether the sync completed successfully"`
		Message string `json:"message" doc:"Status message describing the sync result"`
	}
}

// SyncAll handles POST /v1/billing/sync - performs full sync (invoices + notifications)
func (h *SyncHandler) SyncAll(ctx context.Context, req *struct{}) (*SyncResponse, error) {
	if h.pollingJob == nil {
		return &SyncResponse{
			Body: struct {
				Success bool   `json:"success" doc:"Whether the sync completed successfully"`
				Message string `json:"message" doc:"Status message describing the sync result"`
			}{
				Success: false,
				Message: "Billing polling job not initialized",
			},
		}, nil
	}

	// Run the full poll which includes invoice sync and notification processing
	err := h.pollingJob.Poll(ctx)
	if err != nil {
		return &SyncResponse{
			Body: struct {
				Success bool   `json:"success" doc:"Whether the sync completed successfully"`
				Message string `json:"message" doc:"Status message describing the sync result"`
			}{
				Success: false,
				Message: "Sync failed: " + err.Error(),
			},
		}, nil
	}

	return &SyncResponse{
		Body: struct {
			Success bool   `json:"success" doc:"Whether the sync completed successfully"`
			Message string `json:"message" doc:"Status message describing the sync result"`
		}{
			Success: true,
			Message: "Full sync completed successfully (invoices + notifications)",
		},
	}, nil
}

// SyncInvoices handles POST /v1/billing/sync/invoices - syncs only invoices
func (h *SyncHandler) SyncInvoices(ctx context.Context, req *struct{}) (*SyncResponse, error) {
	if h.pollingJob == nil {
		return &SyncResponse{
			Body: struct {
				Success bool   `json:"success" doc:"Whether the sync completed successfully"`
				Message string `json:"message" doc:"Status message describing the sync result"`
			}{
				Success: false,
				Message: "Billing polling job not initialized",
			},
		}, nil
	}

	// Run only the invoice sync
	err := h.pollingJob.SyncReceivedInvoices(ctx)
	if err != nil {
		return &SyncResponse{
			Body: struct {
				Success bool   `json:"success" doc:"Whether the sync completed successfully"`
				Message string `json:"message" doc:"Status message describing the sync result"`
			}{
				Success: false,
				Message: "Invoice sync failed: " + err.Error(),
			},
		}, nil
	}

	return &SyncResponse{
		Body: struct {
			Success bool   `json:"success" doc:"Whether the sync completed successfully"`
			Message string `json:"message" doc:"Status message describing the sync result"`
		}{
			Success: true,
			Message: "Invoice sync completed successfully",
		},
	}, nil
}
