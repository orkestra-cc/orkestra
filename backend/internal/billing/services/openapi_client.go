package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/billing/config"
	"github.com/orkestra/backend/internal/billing/models"
)

// Common errors
var (
	ErrOpenAPIRequestFailed  = errors.New("OpenAPI request failed")
	ErrOpenAPIUnauthorized   = errors.New("OpenAPI authentication failed")
	ErrOpenAPINotFound       = errors.New("resource not found")
	ErrOpenAPIRateLimited    = errors.New("rate limit exceeded")
	ErrCircuitBreakerOpen    = errors.New("circuit breaker is open")
	ErrInvoiceSendFailed     = errors.New("failed to send invoice to SDI")
)

// OpenAPIClient defines the interface for OpenAPI SDI operations
type OpenAPIClient interface {
	// Configuration
	ConfigureBusinessRegistry(ctx context.Context, cfg BusinessRegistryConfig) error
	GetBusinessRegistryConfig(ctx context.Context, fiscalID string) (*BusinessRegistryConfig, error)

	// Issued invoices (fatture attive)
	SendInvoice(ctx context.Context, invoice *models.Invoice, xmlContent string) (*SendInvoiceResponse, error)
	GetInvoiceStatus(ctx context.Context, uuid string) (*InvoiceStatusResponse, error)
	DownloadInvoicePDF(ctx context.Context, uuid string) ([]byte, error)
	DownloadInvoiceXML(ctx context.Context, uuid string) ([]byte, error)
	DownloadInvoiceHTML(ctx context.Context, uuid string) ([]byte, error)

	// Received invoices (fatture passive)
	GetSupplierInvoices(ctx context.Context, fromDate time.Time, page, pageSize int) (*SupplierInvoicesResponse, error)
	ImportInvoice(ctx context.Context, input *ImportInvoiceInput) (*ImportInvoiceResponse, error)

	// Notifications
	GetNotifications(ctx context.Context, fromDate time.Time) ([]OpenAPINotification, error)

	// Legal storage / preserved documents
	GetPreservedDocument(ctx context.Context, uuid string) (*PreservedDocumentResponse, error)

	// Statistics
	GetInvoiceStats(ctx context.Context) (*InvoiceStatsResponse, error)

	// Health check
	Ping(ctx context.Context) error
}

// BusinessRegistryConfig represents the configuration for a business registry
type BusinessRegistryConfig struct {
	FiscalID         string `json:"fiscal_id"`
	Email            string `json:"email"`
	ApplySignature   bool   `json:"apply_signature"`
	ApplyLegalStorage bool  `json:"apply_legal_storage"`
	Active           bool   `json:"active"`
}

// SendInvoiceResponse represents the response from sending an invoice
type SendInvoiceResponse struct {
	UUID          string `json:"uuid"`
	SDIIdentifier string `json:"sdi_identifier,omitempty"`
	Status        string `json:"status"`
	Message       string `json:"message,omitempty"`
}

// InvoiceStatusResponse represents the status of an invoice
type InvoiceStatusResponse struct {
	UUID           string    `json:"uuid"`
	SDIIdentifier  string    `json:"sdi_identifier,omitempty"`
	Status         string    `json:"status"`
	LastNotification string  `json:"last_notification,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	ReceivedAt     *time.Time `json:"received_at,omitempty"`
}

// SupplierInvoicesResponse represents paginated supplier invoices
type SupplierInvoicesResponse struct {
	Invoices   []OpenAPIInvoice `json:"invoices"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
}

// OpenAPIInvoice represents an invoice from OpenAPI
type OpenAPIInvoice struct {
	UUID              string    `json:"uuid"`
	SDIIdentifier     string    `json:"sdi_identifier"`
	Number            string    `json:"number"`
	Date              time.Time `json:"date"`
	SupplierFiscalID  string    `json:"supplier_fiscal_id"`
	SupplierName      string    `json:"supplier_name"`
	TotalAmount       float64   `json:"total_amount"`
	ReceivedAt        time.Time `json:"received_at"`
}

// OpenAPINotification represents a notification from OpenAPI
type OpenAPINotification struct {
	UUID             string    `json:"uuid"`
	InvoiceUUID      string    `json:"invoice_uuid"`
	Type             string    `json:"type"` // RC, NS, MC, NE, DT, AT
	Date             time.Time `json:"date"`
	Outcome          string    `json:"outcome,omitempty"` // EC01, EC02 for NE
	ErrorCode        string    `json:"error_code,omitempty"`
	ErrorDescription string    `json:"error_description,omitempty"`
	RawContent       string    `json:"raw_content,omitempty"`
}

// InvoiceStatsResponse represents invoice statistics
type InvoiceStatsResponse struct {
	TotalSent      int     `json:"total_sent"`
	TotalDelivered int     `json:"total_delivered"`
	TotalRejected  int     `json:"total_rejected"`
	TotalReceived  int     `json:"total_received"`
	TotalAmount    float64 `json:"total_amount"`
}

// ImportInvoiceInput represents the input for importing supplier invoices
// Per OpenAPI SDI spec: POST /invoices/import
type ImportInvoiceInput struct {
	Invoice         string                 `json:"invoice"`           // Base64-encoded FatturaPA XML
	InvoiceFileName string                 `json:"invoice_file_name,omitempty"`
	SDIID           string                 `json:"sdi_id,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// ImportInvoiceResponse represents the response from importing invoices
type ImportInvoiceResponse struct {
	UUIDs   []string `json:"uuids"`
	Count   int      `json:"count"`
	Message string   `json:"message,omitempty"`
}

// PreservedDocumentResponse represents the status of a preserved document
// Per OpenAPI SDI spec: GET /preserved_documents/{uuid}
type PreservedDocumentResponse struct {
	UUID             string     `json:"uuid"`
	Status           string     `json:"status"` // to_be_stored, sent, stored, error
	ReceiptTimestamp *time.Time `json:"receipt_timestamp,omitempty"`
	Weight           int        `json:"weight,omitempty"`
	ObjectID         string     `json:"object_id,omitempty"`
	ObjectType       string     `json:"object_type,omitempty"`
}

// openAPIClient implements the OpenAPIClient interface
type openAPIClient struct {
	httpClient     *http.Client
	config         *config.OpenAPIConfig
	circuitBreaker *circuitBreaker
	logger         *slog.Logger
}

// NewOpenAPIClient creates a new OpenAPI client
func NewOpenAPIClient(cfg *config.OpenAPIConfig, logger *slog.Logger) OpenAPIClient {
	return &openAPIClient{
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		config:         cfg,
		circuitBreaker: newCircuitBreaker(5, 30*time.Second), // 5 failures, 30s reset
		logger:         logger,
	}
}

func (c *openAPIClient) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	// Check circuit breaker
	if !c.circuitBreaker.Allow() {
		return nil, 0, ErrCircuitBreakerOpen
	}

	url := c.config.GetEndpoint(path)

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.circuitBreaker.RecordFailure()
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	// Record success/failure for circuit breaker
	if resp.StatusCode >= 500 {
		c.circuitBreaker.RecordFailure()
	} else {
		c.circuitBreaker.RecordSuccess()
	}

	return respBody, resp.StatusCode, nil
}

func (c *openAPIClient) doRequestWithRetry(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var lastErr error
	var respBody []byte
	var statusCode int

	for attempt := 0; attempt < c.config.RetryAttempts; attempt++ {
		respBody, statusCode, lastErr = c.doRequest(ctx, method, path, body)

		if lastErr == nil && statusCode < 500 {
			return respBody, statusCode, nil
		}

		// Don't retry on client errors
		if statusCode >= 400 && statusCode < 500 {
			break
		}

		// Wait before retry with exponential backoff
		if attempt < c.config.RetryAttempts-1 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
		}

		c.logger.Warn("retrying OpenAPI request",
			"attempt", attempt+1,
			"method", method,
			"path", path,
			"error", lastErr,
		)
	}

	if lastErr != nil {
		return nil, statusCode, lastErr
	}

	return respBody, statusCode, nil
}

func (c *openAPIClient) ConfigureBusinessRegistry(ctx context.Context, cfg BusinessRegistryConfig) error {
	body := map[string]interface{}{
		"fiscal_id":           cfg.FiscalID,
		"email":               cfg.Email,
		"apply_signature":     cfg.ApplySignature,
		"apply_legal_storage": cfg.ApplyLegalStorage,
	}

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPost, "/business_registry_configurations", body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("%w: status %d, body: %s", ErrOpenAPIRequestFailed, statusCode, string(respBody))
	}

	return nil
}

func (c *openAPIClient) GetBusinessRegistryConfig(ctx context.Context, fiscalID string) (*BusinessRegistryConfig, error) {
	path := fmt.Sprintf("/business_registry_configurations/%s", fiscalID)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, ErrOpenAPINotFound
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	var result BusinessRegistryConfig
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *openAPIClient) SendInvoice(ctx context.Context, invoice *models.Invoice, xmlContent string) (*SendInvoiceResponse, error) {
	// Determine endpoint based on storage/signature config
	var endpoint string
	if c.config.ApplySignature && c.config.ApplyStorage {
		endpoint = "/invoices_signature_legal_storage"
	} else if c.config.ApplySignature {
		endpoint = "/invoices_signature"
	} else if c.config.ApplyStorage {
		endpoint = "/invoices_legal_storage"
	} else {
		endpoint = "/invoices"
	}

	body := map[string]interface{}{
		"fattura_elettronica": xmlContent,
	}

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvoiceSendFailed, err)
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated && statusCode != http.StatusAccepted {
		c.logger.Error("invoice send failed",
			"statusCode", statusCode,
			"response", string(respBody),
			"invoiceNumber", invoice.Number,
		)
		return nil, fmt.Errorf("%w: status %d, response: %s", ErrInvoiceSendFailed, statusCode, string(respBody))
	}

	var result SendInvoiceResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	c.logger.Info("invoice sent successfully",
		"invoiceNumber", invoice.Number,
		"openApiUUID", result.UUID,
	)

	return &result, nil
}

func (c *openAPIClient) GetInvoiceStatus(ctx context.Context, uuid string) (*InvoiceStatusResponse, error) {
	path := fmt.Sprintf("/invoices/%s", uuid)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, ErrOpenAPINotFound
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	var result InvoiceStatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *openAPIClient) DownloadInvoicePDF(ctx context.Context, uuid string) ([]byte, error) {
	path := fmt.Sprintf("/invoices/%s/pdf", uuid)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, ErrOpenAPINotFound
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	return respBody, nil
}

func (c *openAPIClient) DownloadInvoiceXML(ctx context.Context, uuid string) ([]byte, error) {
	path := fmt.Sprintf("/invoices/%s", uuid)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, ErrOpenAPINotFound
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	return respBody, nil
}

// DownloadInvoiceHTML downloads the HTML view of an invoice from OpenAPI SDI
// Per OpenAPI SDI spec: GET /invoices/{uuid}/html
func (c *openAPIClient) DownloadInvoiceHTML(ctx context.Context, uuid string) ([]byte, error) {
	path := fmt.Sprintf("/invoices/%s/html", uuid)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, ErrOpenAPINotFound
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	return respBody, nil
}

// ImportInvoice imports a supplier invoice via base64-encoded XML
// Per OpenAPI SDI spec: POST /invoices/import
func (c *openAPIClient) ImportInvoice(ctx context.Context, input *ImportInvoiceInput) (*ImportInvoiceResponse, error) {
	body := map[string]interface{}{
		"invoice": input.Invoice,
	}

	if input.InvoiceFileName != "" {
		body["invoice_file_name"] = input.InvoiceFileName
	}
	if input.SDIID != "" {
		body["sdi_id"] = input.SDIID
	}
	if input.Metadata != nil {
		body["metadata"] = input.Metadata
	}

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPost, "/invoices/import", body)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated && statusCode != http.StatusAccepted {
		c.logger.Error("invoice import failed",
			"statusCode", statusCode,
			"response", string(respBody),
		)
		return nil, fmt.Errorf("%w: status %d, response: %s", ErrOpenAPIRequestFailed, statusCode, string(respBody))
	}

	var result ImportInvoiceResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	c.logger.Info("invoice imported successfully",
		"uuids", result.UUIDs,
		"count", result.Count,
	)

	return &result, nil
}

// GetPreservedDocument retrieves the preservation status of a document
// Per OpenAPI SDI spec: GET /preserved_documents/{uuid}
func (c *openAPIClient) GetPreservedDocument(ctx context.Context, uuid string) (*PreservedDocumentResponse, error) {
	path := fmt.Sprintf("/preserved_documents/%s", uuid)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, ErrOpenAPINotFound
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	var result PreservedDocumentResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *openAPIClient) GetSupplierInvoices(ctx context.Context, fromDate time.Time, page, pageSize int) (*SupplierInvoicesResponse, error) {
	path := fmt.Sprintf("/invoices?direction=received&from_date=%s&page=%d&page_size=%d",
		fromDate.Format("2006-01-02"), page, pageSize)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	var result SupplierInvoicesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *openAPIClient) GetNotifications(ctx context.Context, fromDate time.Time) ([]OpenAPINotification, error) {
	path := fmt.Sprintf("/notifications?from_date=%s", fromDate.Format("2006-01-02T15:04:05Z"))

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	var result struct {
		Notifications []OpenAPINotification `json:"notifications"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Notifications, nil
}

func (c *openAPIClient) GetInvoiceStats(ctx context.Context) (*InvoiceStatsResponse, error) {
	path := "/invoices/stats"

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	var result InvoiceStatsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *openAPIClient) Ping(ctx context.Context) error {
	_, statusCode, err := c.doRequest(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", statusCode)
	}

	return nil
}

// circuitBreaker implements a simple circuit breaker pattern
type circuitBreaker struct {
	mu              sync.Mutex
	failures        int
	maxFailures     int
	resetTimeout    time.Duration
	lastFailureTime time.Time
	state           string // closed, open, half-open
}

func newCircuitBreaker(maxFailures int, resetTimeout time.Duration) *circuitBreaker {
	return &circuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        "closed",
	}
}

func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "closed":
		return true
	case "open":
		// Check if reset timeout has passed
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return true
	}
}

func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = "closed"
}

func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.failures >= cb.maxFailures {
		cb.state = "open"
	}
}
