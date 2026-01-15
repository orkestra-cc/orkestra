package handlers

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/billing/models"
	"github.com/orkestra/backend/internal/billing/services"
)

// InvoiceHandler handles invoice-related HTTP requests
type InvoiceHandler struct {
	invoiceService services.InvoiceService
}

// NewInvoiceHandler creates a new InvoiceHandler
func NewInvoiceHandler(invoiceService services.InvoiceService) *InvoiceHandler {
	return &InvoiceHandler{
		invoiceService: invoiceService,
	}
}

// ========================================
// Request/Response Types
// ========================================

// CreateInvoiceRequest represents the request to create an invoice
type CreateInvoiceRequest struct {
	Body models.CreateInvoiceInput `json:"invoice" doc:"Invoice data to create"`
}

// CreateInvoiceResponse represents the response after creating an invoice
type CreateInvoiceResponse struct {
	Body models.Invoice `json:"invoice" doc:"Created invoice"`
}

// GetInvoiceRequest represents the request to get an invoice
type GetInvoiceRequest struct {
	ID string `path:"id" doc:"Invoice UUID"`
}

// GetInvoiceResponse represents the response with invoice details
type GetInvoiceResponse struct {
	Body models.Invoice `json:"invoice" doc:"Invoice details"`
}

// ListInvoicesRequest represents the request to list invoices
type ListInvoicesRequest struct {
	Direction    string `query:"direction" enum:"issued,received" doc:"Filter by direction (issued/received)"`
	Status       string `query:"status" enum:"draft,pending,sent,delivered,rejected,accepted,paid,cancelled" doc:"Filter by status"`
	SDIStatus    string `query:"sdiStatus" enum:"RC,NS,MC,NE,DT,AT" doc:"Filter by SDI status"`
	CustomerID   string `query:"customerId" doc:"Filter by customer ID"`
	SupplierID   string `query:"supplierId" doc:"Filter by supplier ID"`
	FromDate     string `query:"fromDate" doc:"Filter invoices from this date (YYYY-MM-DD)"`
	ToDate       string `query:"toDate" doc:"Filter invoices to this date (YYYY-MM-DD)"`
	Search       string `query:"search" doc:"Search in invoice number and party name"`
	DocumentType string `query:"documentType" doc:"Filter by document type (TD01, TD04, etc.)"`
	Page         int    `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize     int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" doc:"Items per page"`
}

// ListInvoicesResponse represents the paginated list of invoices
type ListInvoicesResponse struct {
	Body models.InvoiceListResponse `json:"invoices" doc:"Paginated list of invoices"`
}

// UpdateInvoiceRequest represents the request to update an invoice
type UpdateInvoiceRequest struct {
	ID   string                   `path:"id" doc:"Invoice UUID"`
	Body models.UpdateInvoiceInput `json:"invoice" doc:"Invoice update data"`
}

// UpdateInvoiceResponse represents the response after updating an invoice
type UpdateInvoiceResponse struct {
	Body models.Invoice `json:"invoice" doc:"Updated invoice"`
}

// DeleteInvoiceRequest represents the request to delete an invoice
type DeleteInvoiceRequest struct {
	ID string `path:"id" doc:"Invoice UUID"`
}

// DeleteInvoiceResponse represents the response after deleting an invoice
type DeleteInvoiceResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// SendInvoiceRequest represents the request to send an invoice to SDI
type SendInvoiceRequest struct {
	ID string `path:"id" doc:"Invoice UUID"`
}

// SendInvoiceResponse represents the response after sending an invoice
type SendInvoiceResponse struct {
	Body models.SendInvoiceResponse `json:"result" doc:"Send result"`
}

// GetInvoicePDFRequest represents the request to download invoice PDF
type GetInvoicePDFRequest struct {
	ID string `path:"id" doc:"Invoice UUID"`
}

// GetInvoiceXMLRequest represents the request to download invoice XML
type GetInvoiceXMLRequest struct {
	ID string `path:"id" doc:"Invoice UUID"`
}

// GetInvoiceXMLResponse represents the response with invoice XML
type GetInvoiceXMLResponse struct {
	Body struct {
		XML string `json:"xml" doc:"Invoice XML content"`
	}
}

// AcceptInvoiceRequest represents the request to accept a received invoice
type AcceptInvoiceRequest struct {
	ID string `path:"id" doc:"Invoice UUID"`
}

// AcceptInvoiceResponse represents the response after accepting an invoice
type AcceptInvoiceResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// RejectInvoiceRequest represents the request to reject a received invoice
type RejectInvoiceRequest struct {
	ID   string `path:"id" doc:"Invoice UUID"`
	Body struct {
		Reason string `json:"reason" doc:"Rejection reason"`
	}
}

// RejectInvoiceResponse represents the response after rejecting an invoice
type RejectInvoiceResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// GetStatsRequest represents the request to get billing statistics
type GetStatsRequest struct {
	FromDate string `query:"fromDate" doc:"Start date (YYYY-MM-DD)"`
	ToDate   string `query:"toDate" doc:"End date (YYYY-MM-DD)"`
}

// GetStatsResponse represents the response with billing statistics
type GetStatsResponse struct {
	Body models.BillingStats `json:"stats" doc:"Billing statistics"`
}

// GetInvoiceHTMLRequest represents the request to get invoice HTML view
type GetInvoiceHTMLRequest struct {
	ID string `path:"id" doc:"Invoice UUID"`
}

// GetInvoiceHTMLResponse represents the response with invoice HTML
type GetInvoiceHTMLResponse struct {
	Body struct {
		HTML string `json:"html" doc:"Invoice HTML content"`
	}
}

// ImportInvoiceRequest represents the request to import a supplier invoice
type ImportInvoiceRequest struct {
	Body models.ImportInvoiceInput `json:"input" doc:"Import invoice data"`
}

// ImportInvoiceResponse represents the response after importing an invoice
type ImportInvoiceResponse struct {
	Body models.ImportInvoiceResponse `json:"result" doc:"Import result"`
}

// GetPreservedDocumentRequest represents the request to get preserved document status
type GetPreservedDocumentRequest struct {
	ID string `path:"id" doc:"Document UUID"`
}

// GetPreservedDocumentResponse represents the response with preserved document status
type GetPreservedDocumentResponse struct {
	Body models.PreservedDocument `json:"document" doc:"Preserved document status"`
}

// ========================================
// Handler Methods
// ========================================

// CreateInvoice creates a new invoice
func (h *InvoiceHandler) CreateInvoice(ctx context.Context, req *CreateInvoiceRequest) (*CreateInvoiceResponse, error) {
	// Get user ID from context (set by auth middleware)
	userID := getUserIDFromContext(ctx)

	invoice, err := h.invoiceService.CreateInvoice(ctx, &req.Body, userID)
	if err != nil {
		switch err {
		case services.ErrCustomerNotFound:
			return nil, huma.Error404NotFound("Customer not found", err)
		case services.ErrInvalidInvoiceData:
			return nil, huma.Error400BadRequest("Invalid invoice data", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to create invoice", err)
		}
	}

	return &CreateInvoiceResponse{Body: *invoice}, nil
}

// GetInvoice retrieves an invoice by ID
func (h *InvoiceHandler) GetInvoice(ctx context.Context, req *GetInvoiceRequest) (*GetInvoiceResponse, error) {
	invoice, err := h.invoiceService.GetInvoice(ctx, req.ID)
	if err != nil {
		if err == services.ErrInvoiceNotFound {
			return nil, huma.Error404NotFound("Invoice not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get invoice", err)
	}

	return &GetInvoiceResponse{Body: *invoice}, nil
}

// ListInvoices lists invoices with filtering and pagination
func (h *InvoiceHandler) ListInvoices(ctx context.Context, req *ListInvoicesRequest) (*ListInvoicesResponse, error) {
	filters := &models.InvoiceFilters{
		Search: req.Search,
	}

	// Parse direction
	if req.Direction != "" {
		direction := models.InvoiceDirection(req.Direction)
		filters.Direction = &direction
	}

	// Parse status
	if req.Status != "" {
		status := models.InvoiceStatus(req.Status)
		filters.Status = &status
	}

	// Parse SDI status
	if req.SDIStatus != "" {
		sdiStatus := models.SDIStatus(req.SDIStatus)
		filters.SDIStatus = &sdiStatus
	}

	// Parse document type
	if req.DocumentType != "" {
		docType := models.DocumentType(req.DocumentType)
		filters.DocumentType = &docType
	}

	// Parse customer/supplier IDs
	if req.CustomerID != "" {
		filters.CustomerID = req.CustomerID
	}
	if req.SupplierID != "" {
		filters.SupplierID = req.SupplierID
	}

	// Parse dates
	if req.FromDate != "" {
		fromDate, err := time.Parse("2006-01-02", req.FromDate)
		if err == nil {
			filters.FromDate = &fromDate
		}
	}
	if req.ToDate != "" {
		toDate, err := time.Parse("2006-01-02", req.ToDate)
		if err == nil {
			// Set to end of day
			toDate = toDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			filters.ToDate = &toDate
		}
	}

	pagination := models.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	result, err := h.invoiceService.ListInvoices(ctx, filters, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list invoices", err)
	}

	return &ListInvoicesResponse{Body: *result}, nil
}

// UpdateInvoice updates an existing invoice
func (h *InvoiceHandler) UpdateInvoice(ctx context.Context, req *UpdateInvoiceRequest) (*UpdateInvoiceResponse, error) {
	invoice, err := h.invoiceService.UpdateInvoice(ctx, req.ID, &req.Body)
	if err != nil {
		switch err {
		case services.ErrInvoiceNotFound:
			return nil, huma.Error404NotFound("Invoice not found", err)
		case services.ErrInvoiceCannotEdit:
			return nil, huma.Error409Conflict("Invoice cannot be edited in current status", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to update invoice", err)
		}
	}

	return &UpdateInvoiceResponse{Body: *invoice}, nil
}

// DeleteInvoice deletes an invoice (only drafts)
func (h *InvoiceHandler) DeleteInvoice(ctx context.Context, req *DeleteInvoiceRequest) (*DeleteInvoiceResponse, error) {
	err := h.invoiceService.DeleteInvoice(ctx, req.ID)
	if err != nil {
		switch err {
		case services.ErrInvoiceNotFound:
			return nil, huma.Error404NotFound("Invoice not found", err)
		case services.ErrInvoiceCannotDelete:
			return nil, huma.Error409Conflict("Invoice cannot be deleted in current status", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to delete invoice", err)
		}
	}

	resp := &DeleteInvoiceResponse{}
	resp.Body.Message = "Invoice deleted successfully"
	return resp, nil
}

// SendInvoice sends an invoice to SDI
func (h *InvoiceHandler) SendInvoice(ctx context.Context, req *SendInvoiceRequest) (*SendInvoiceResponse, error) {
	userID := getUserIDFromContext(ctx)

	result, err := h.invoiceService.SendInvoice(ctx, req.ID, userID)
	if err != nil {
		switch err {
		case services.ErrInvoiceNotFound:
			return nil, huma.Error404NotFound("Invoice not found", err)
		case services.ErrInvoiceCannotSend:
			return nil, huma.Error409Conflict("Invoice cannot be sent in current status", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to send invoice", err)
		}
	}

	return &SendInvoiceResponse{Body: *result}, nil
}

// GetInvoiceXML returns the invoice XML content
func (h *InvoiceHandler) GetInvoiceXML(ctx context.Context, req *GetInvoiceXMLRequest) (*GetInvoiceXMLResponse, error) {
	xml, err := h.invoiceService.GetInvoiceXML(ctx, req.ID)
	if err != nil {
		if err == services.ErrInvoiceNotFound {
			return nil, huma.Error404NotFound("Invoice not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get invoice XML", err)
	}

	resp := &GetInvoiceXMLResponse{}
	resp.Body.XML = xml
	return resp, nil
}

// AcceptReceivedInvoice accepts a received invoice
func (h *InvoiceHandler) AcceptReceivedInvoice(ctx context.Context, req *AcceptInvoiceRequest) (*AcceptInvoiceResponse, error) {
	userID := getUserIDFromContext(ctx)

	err := h.invoiceService.AcceptReceivedInvoice(ctx, req.ID, userID)
	if err != nil {
		if err == services.ErrInvoiceNotFound {
			return nil, huma.Error404NotFound("Invoice not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to accept invoice", err)
	}

	resp := &AcceptInvoiceResponse{}
	resp.Body.Message = "Invoice accepted successfully"
	return resp, nil
}

// RejectReceivedInvoice rejects a received invoice
func (h *InvoiceHandler) RejectReceivedInvoice(ctx context.Context, req *RejectInvoiceRequest) (*RejectInvoiceResponse, error) {
	userID := getUserIDFromContext(ctx)

	err := h.invoiceService.RejectReceivedInvoice(ctx, req.ID, req.Body.Reason, userID)
	if err != nil {
		if err == services.ErrInvoiceNotFound {
			return nil, huma.Error404NotFound("Invoice not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to reject invoice", err)
	}

	resp := &RejectInvoiceResponse{}
	resp.Body.Message = "Invoice rejected successfully"
	return resp, nil
}

// GetStats returns billing statistics
func (h *InvoiceHandler) GetStats(ctx context.Context, req *GetStatsRequest) (*GetStatsResponse, error) {
	// Default to current month
	now := time.Now()
	fromDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	toDate := fromDate.AddDate(0, 1, -1).Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	if req.FromDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.FromDate); err == nil {
			fromDate = parsed
		}
	}
	if req.ToDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.ToDate); err == nil {
			toDate = parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		}
	}

	stats, err := h.invoiceService.GetStats(ctx, fromDate, toDate)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get statistics", err)
	}

	return &GetStatsResponse{Body: *stats}, nil
}

// GetInvoiceHTML returns the HTML representation of an invoice
func (h *InvoiceHandler) GetInvoiceHTML(ctx context.Context, req *GetInvoiceHTMLRequest) (*GetInvoiceHTMLResponse, error) {
	htmlBytes, err := h.invoiceService.GetInvoiceHTML(ctx, req.ID)
	if err != nil {
		if err == services.ErrInvoiceNotFound {
			return nil, huma.Error404NotFound("Invoice not found", err)
		}
		return nil, huma.Error500InternalServerError("Failed to get invoice HTML", err)
	}

	resp := &GetInvoiceHTMLResponse{}
	resp.Body.HTML = string(htmlBytes)
	return resp, nil
}

// ImportInvoice imports a supplier invoice via base64-encoded XML
func (h *InvoiceHandler) ImportInvoice(ctx context.Context, req *ImportInvoiceRequest) (*ImportInvoiceResponse, error) {
	result, err := h.invoiceService.ImportInvoice(ctx, &req.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to import invoice", err)
	}

	return &ImportInvoiceResponse{Body: *result}, nil
}

// GetPreservedDocument returns the preservation status of a document
func (h *InvoiceHandler) GetPreservedDocument(ctx context.Context, req *GetPreservedDocumentRequest) (*GetPreservedDocumentResponse, error) {
	document, err := h.invoiceService.GetPreservedDocument(ctx, req.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get preserved document", err)
	}

	return &GetPreservedDocumentResponse{Body: *document}, nil
}

// Helper function to get user ID from context
func getUserIDFromContext(ctx context.Context) string {
	// This would be set by auth middleware
	if userID, ok := ctx.Value("userID").(string); ok {
		return userID
	}
	return "system"
}
