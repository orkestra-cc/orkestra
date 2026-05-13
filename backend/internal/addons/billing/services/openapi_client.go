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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/addons/billing/config"
	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/shared/openapiauth"
)

// Common errors
var (
	ErrOpenAPIRequestFailed      = errors.New("OpenAPI request failed")
	ErrOpenAPIUnauthorized       = errors.New("OpenAPI authentication failed")
	ErrOpenAPINotFound           = errors.New("resource not found")
	ErrOpenAPIRateLimited        = errors.New("rate limit exceeded")
	ErrCircuitBreakerOpen        = errors.New("circuit breaker is open")
	ErrInvoiceSendFailed         = errors.New("failed to send invoice to SDI")
	ErrOpenAPIEmailAlreadyExists = errors.New("email already registered for another fiscal ID")
)

// OpenAPIErrorResponse represents JSON error from OpenAPI SDI
type OpenAPIErrorResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Error   int         `json:"error"`
	Data    interface{} `json:"data"`
}

// OpenAPIError wraps an OpenAPI SDI error
type OpenAPIError struct {
	Code       int
	Message    string
	StatusCode int
}

func (e *OpenAPIError) Error() string {
	return fmt.Sprintf("OpenAPI error %d: %s", e.Code, e.Message)
}

func (e *OpenAPIError) Is(target error) bool {
	if e.Code == 612 {
		return target == ErrOpenAPIEmailAlreadyExists
	}
	return false
}

// parseOpenAPIError parses an OpenAPI SDI JSON error response
func parseOpenAPIError(respBody []byte, httpStatusCode int) *OpenAPIError {
	var errResp OpenAPIErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		return nil
	}
	if errResp.Success || errResp.Error == 0 {
		return nil
	}
	return &OpenAPIError{
		Code:       errResp.Error,
		Message:    errResp.Message,
		StatusCode: httpStatusCode,
	}
}

// Cache configuration constants
const (
	invoiceStatusCachePrefix = "billing:invoice:status:"
	invoiceStatusCacheTTL    = 15 * time.Minute
)

// RedisClient defines the interface for Redis operations used by the billing module
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
}

// OpenAPIClient defines the interface for OpenAPI SDI operations
type OpenAPIClient interface {
	// Configuration
	ConfigureBusinessRegistry(ctx context.Context, cfg BusinessRegistryConfig) error
	GetBusinessRegistryConfig(ctx context.Context, fiscalID string) (*BusinessRegistryConfig, error)
	DeleteBusinessRegistry(ctx context.Context, fiscalID string) error

	// Issued invoices (fatture attive)
	SendInvoice(ctx context.Context, invoice *models.Invoice, xmlContent string) (*SendInvoiceResponse, error)
	GetInvoiceStatus(ctx context.Context, uuid string) (*InvoiceStatusResponse, error)
	DownloadInvoicePDF(ctx context.Context, uuid string) ([]byte, error)
	DownloadInvoiceXML(ctx context.Context, uuid string) ([]byte, error)
	DownloadInvoiceHTML(ctx context.Context, uuid string) ([]byte, error)

	// Received invoices (fatture passive)
	GetSupplierInvoices(ctx context.Context, fromDate time.Time, page, pageSize int) (*SupplierInvoicesResponse, error)
	ImportInvoice(ctx context.Context, input *ImportInvoiceInput) (*ImportInvoiceResponse, error)

	// All invoices (both sent and received) - for syncing from OpenAPI
	GetAllInvoices(ctx context.Context, fromDate time.Time, page, pageSize int) (*SupplierInvoicesResponse, error)

	// Legal storage / preserved documents
	GetPreservedDocument(ctx context.Context, uuid string) (*PreservedDocumentResponse, error)

	// Statistics
	GetInvoiceStats(ctx context.Context) (*InvoiceStatsResponse, error)

	// Health check
	Ping(ctx context.Context) error

	// Cache management
	InvalidateInvoiceStatusCache(ctx context.Context, uuid string) error

	// API Configuration (webhook callbacks)
	ConfigureAPICallbacks(ctx context.Context, cfg APICallbackConfig) error
	GetAPIConfigurations(ctx context.Context) (*APIConfigurationResponse, error)
}

// BusinessRegistryConfig represents the configuration for a business registry
type BusinessRegistryConfig struct {
	FiscalID          string `json:"fiscal_id"`
	Email             string `json:"email"`
	ApplySignature    bool   `json:"apply_signature"`
	ApplyLegalStorage bool   `json:"apply_legal_storage"`
	Active            bool   `json:"active"`
}

// SendInvoiceResponse represents the response from sending an invoice
type SendInvoiceResponse struct {
	UUID          string `json:"uuid"`
	SDIIdentifier string `json:"sdi_identifier,omitempty"`
	Status        string `json:"status"`
	Message       string `json:"message,omitempty"`
	// Applied settings (populated by SendInvoice based on business registry config)
	SignatureApplied    bool `json:"-"` // Whether digital signature was applied
	LegalStorageApplied bool `json:"-"` // Whether legal storage (conservazione) was applied
}

// InvoiceStatusResponse represents the status of an invoice
type InvoiceStatusResponse struct {
	UUID             string     `json:"uuid"`
	SDIIdentifier    string     `json:"sdi_identifier,omitempty"`
	Status           string     `json:"status"`
	LastNotification string     `json:"last_notification,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	ReceivedAt       *time.Time `json:"received_at,omitempty"`
	// Legal storage (conservazione digitale) fields
	LegallyStored     bool                       `json:"legally_stored,omitempty"`
	PreservedDocument *PreservedDocumentResponse `json:"preserved_document,omitempty"`
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
	UUID         string        `json:"uuid"`
	SDIFileID    string        `json:"sdi_file_id"`
	SDIFileName  string        `json:"sdi_file_name"`
	DocumentType string        `json:"document_type"`
	CreatedAt    time.Time     `json:"created_at"`
	Marking      string        `json:"marking"` // sent, received
	Sender       *OpenAPIParty `json:"sender"`
	Recipient    *OpenAPIParty `json:"recipient"`
	Payload      string        `json:"payload"` // Raw FatturaPA JSON
}

// OpenAPIParty represents a party (sender/recipient) in OpenAPI response
type OpenAPIParty struct {
	UUID                    string `json:"uuid"`
	BusinessVATNumberCode   string `json:"business_vat_number_code"`
	BusinessFiscalCode      string `json:"business_fiscal_code"`
	BusinessName            string `json:"business_name"`
	Name                    string `json:"name"`
	Surname                 string `json:"surname"`
	HeadOfficeAddressStreet string `json:"head_office_address_street"`
	HeadOfficeAddressCity   string `json:"head_office_address_city"`
	RecipientCode           string `json:"recipient_code"`
	PEC                     string `json:"pec"`
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
	Invoice         string                 `json:"invoice"` // Base64-encoded FatturaPA XML
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

// APICallbackConfig represents the configuration for POST /api_configurations
// Per OpenAPI.it spec, POST takes fiscal_id + callbacks array
type APICallbackConfig struct {
	FiscalID  string           `json:"fiscal_id"`
	Callbacks []CallbackConfig `json:"callbacks"`
}

// CallbackConfig represents a single callback configuration
type CallbackConfig struct {
	Event        string `json:"event"`
	URL          string `json:"url"`
	AuthHeader   string `json:"auth_header,omitempty"`
	Field        string `json:"field,omitempty"`
	ProviderUUID string `json:"provider_uuid,omitempty"` // Set by OpenAPI.it in responses
}

// APIConfigurationEntry represents a single entry from GET /api_configurations
type APIConfigurationEntry struct {
	ID       string         `json:"id"`
	FiscalID string         `json:"fiscal_id"`
	Callback CallbackConfig `json:"callback"`
}

// APIConfigurationResponse represents the full response from GET /api_configurations
type APIConfigurationResponse struct {
	Entries []APIConfigurationEntry
}

// openAPIClient implements the OpenAPIClient interface
type openAPIClient struct {
	httpClient     *http.Client
	configLoader   config.ConfigLoader
	circuitBreaker *circuitBreaker
	logger         *slog.Logger
	redisClient    RedisClient // Optional Redis client for caching

	// Lazily-built JWT minter. Cached across requests as long as the
	// credentials in `configLoader()` don't change; rebuilt when an
	// admin-UI hot-reload swaps the API key, email, or OAuth host.
	minterMu  sync.Mutex
	minter    *openapiauth.Minter
	minterSig string
}

// billingOAuthScopes is the scope set requested when minting JWTs for the
// SDI API. Covers every endpoint the billing client may hit. Adjust here
// (and re-mint, by rotating the API key) when adding new SDI calls.
var billingOAuthScopes = []string{
	"GET:sdi.openapi.it/*",
	"POST:sdi.openapi.it/*",
	"PUT:sdi.openapi.it/*",
	"PATCH:sdi.openapi.it/*",
	"DELETE:sdi.openapi.it/*",
	"GET:test.sdi.openapi.it/*",
	"POST:test.sdi.openapi.it/*",
	"PUT:test.sdi.openapi.it/*",
	"PATCH:test.sdi.openapi.it/*",
	"DELETE:test.sdi.openapi.it/*",
}

// NewOpenAPIClient creates a new OpenAPI client
func NewOpenAPIClient(loader config.ConfigLoader, logger *slog.Logger) OpenAPIClient {
	return NewOpenAPIClientWithCache(loader, logger, nil)
}

// NewOpenAPIClientWithCache creates a new OpenAPI client with optional Redis caching
func NewOpenAPIClientWithCache(loader config.ConfigLoader, logger *slog.Logger, redisClient RedisClient) OpenAPIClient {
	initialCfg := loader()
	return &openAPIClient{
		httpClient: &http.Client{
			Timeout: initialCfg.Timeout,
		},
		configLoader:   loader,
		circuitBreaker: newCircuitBreaker(5, 30*time.Second), // 5 failures, 30s reset
		logger:         logger,
		redisClient:    redisClient,
	}
}

// getMinter returns the cached JWT minter when the OAuth credentials
// haven't changed since it was constructed; otherwise rebuilds it. The
// signature digest covers email + API key + OAuth host so a hot-reload
// of any of those fields produces a fresh minter (and a fresh JWT).
// Returns nil when API-key credentials are not configured — callers fall
// back to the static BearerToken in that case.
func (c *openAPIClient) getMinter(cfg *config.OpenAPIConfig) *openapiauth.Minter {
	if !cfg.HasOAuthCredentials() {
		return nil
	}
	sig := cfg.AccountEmail + "|" + cfg.APIKey + "|" + cfg.OAuthBaseURL

	c.minterMu.Lock()
	defer c.minterMu.Unlock()
	if c.minter != nil && c.minterSig == sig {
		return c.minter
	}
	var cache openapiauth.Cache
	if c.redisClient != nil {
		cache = c.redisClient
	}
	c.minter = openapiauth.NewMinter(openapiauth.Config{
		AccountEmail: cfg.AccountEmail,
		APIKey:       cfg.APIKey,
		OAuthBaseURL: cfg.OAuthBaseURL,
		Scopes:       billingOAuthScopes,
		TTL:          31536000,
		Tag:          "billing",
	}, cache, &http.Client{Timeout: 15 * time.Second}, c.logger)
	c.minterSig = sig
	return c.minter
}

// resolveBearerToken returns the JWT to send in Authorization. Uses the
// OAuth minter when API-key credentials are set; otherwise falls back to
// the legacy static BearerToken.
func (c *openAPIClient) resolveBearerToken(ctx context.Context, cfg *config.OpenAPIConfig) (string, error) {
	if m := c.getMinter(cfg); m != nil {
		tok, err := m.Token(ctx)
		if err == nil {
			return tok, nil
		}
		if errors.Is(err, openapiauth.ErrUpstreamAuth) {
			return "", fmt.Errorf("%w: openapi auth rejected billing credentials", ErrOpenAPIRequestFailed)
		}
		return "", fmt.Errorf("%w: %v", ErrOpenAPIRequestFailed, err)
	}
	if cfg.BearerToken == "" {
		return "", fmt.Errorf("%w: no openapi credentials configured", ErrOpenAPIRequestFailed)
	}
	return cfg.BearerToken, nil
}

func (c *openAPIClient) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	cfg := c.configLoader()

	// Check circuit breaker
	if !c.circuitBreaker.Allow() {
		return nil, 0, ErrCircuitBreakerOpen
	}

	url := cfg.GetEndpoint(path)

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

	bearer, err := c.resolveBearerToken(ctx, cfg)
	if err != nil {
		return nil, 0, err
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+bearer)
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

	// Drop the cached JWT if the SDI API rejected it — common when the
	// operator rotates the API key in the OpenAPI console mid-flight.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if m := c.getMinter(cfg); m != nil {
			m.Invalidate(ctx)
		}
	}

	// Record success/failure for circuit breaker
	if resp.StatusCode >= 500 {
		c.circuitBreaker.RecordFailure()
	} else {
		c.circuitBreaker.RecordSuccess()
	}

	return respBody, resp.StatusCode, nil
}

// doXMLRequest sends a request with raw XML body
func (c *openAPIClient) doXMLRequest(ctx context.Context, method, path string, xmlContent string) ([]byte, int, error) {
	cfg := c.configLoader()

	// Check circuit breaker
	if !c.circuitBreaker.Allow() {
		return nil, 0, ErrCircuitBreakerOpen
	}

	url := cfg.GetEndpoint(path)

	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(xmlContent))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	bearer, err := c.resolveBearerToken(ctx, cfg)
	if err != nil {
		return nil, 0, err
	}

	// Set headers for XML request
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/xml")
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

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if m := c.getMinter(cfg); m != nil {
			m.Invalidate(ctx)
		}
	}

	// Record success/failure for circuit breaker
	if resp.StatusCode >= 500 {
		c.circuitBreaker.RecordFailure()
	} else {
		c.circuitBreaker.RecordSuccess()
	}

	return respBody, resp.StatusCode, nil
}

// doXMLRequestWithRetry performs an XML request with retry logic
func (c *openAPIClient) doXMLRequestWithRetry(ctx context.Context, method, path string, xmlContent string) ([]byte, int, error) {
	cfg := c.configLoader()
	var lastErr error
	var respBody []byte
	var statusCode int

	for attempt := 0; attempt < cfg.RetryAttempts; attempt++ {
		respBody, statusCode, lastErr = c.doXMLRequest(ctx, method, path, xmlContent)

		if lastErr == nil && statusCode < 500 {
			return respBody, statusCode, nil
		}

		// Don't retry on client errors
		if statusCode >= 400 && statusCode < 500 {
			break
		}

		// Wait before retry with exponential backoff
		if attempt < cfg.RetryAttempts-1 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
		}

		c.logger.Warn("retrying OpenAPI XML request",
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

func (c *openAPIClient) doRequestWithRetry(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	cfg := c.configLoader()
	var lastErr error
	var respBody []byte
	var statusCode int

	for attempt := 0; attempt < cfg.RetryAttempts; attempt++ {
		respBody, statusCode, lastErr = c.doRequest(ctx, method, path, body)

		if lastErr == nil && statusCode < 500 {
			return respBody, statusCode, nil
		}

		// Don't retry on client errors
		if statusCode >= 400 && statusCode < 500 {
			break
		}

		// Wait before retry with exponential backoff
		if attempt < cfg.RetryAttempts-1 {
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
		if apiErr := parseOpenAPIError(respBody, statusCode); apiErr != nil {
			return apiErr
		}
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

	// Parse wrapped response: {"data": {...}, "success": true}
	var wrapper struct {
		Data    BusinessRegistryConfig `json:"data"`
		Success bool                   `json:"success"`
		Message string                 `json:"message"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	c.logger.Info("business registry config parsed",
		"fiscalID", fiscalID,
		"applySignature", wrapper.Data.ApplySignature,
		"applyLegalStorage", wrapper.Data.ApplyLegalStorage,
		"active", wrapper.Data.Active,
	)

	return &wrapper.Data, nil
}

func (c *openAPIClient) DeleteBusinessRegistry(ctx context.Context, fiscalID string) error {
	path := fmt.Sprintf("/business_registry_configurations/%s", fiscalID)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}

	if statusCode == http.StatusNotFound {
		c.logger.Info("business registry config not found, nothing to delete",
			"fiscalID", fiscalID,
		)
		return nil
	}

	if statusCode != http.StatusOK {
		if apiErr := parseOpenAPIError(respBody, statusCode); apiErr != nil {
			return apiErr
		}
		return fmt.Errorf("%w: status %d, body: %s", ErrOpenAPIRequestFailed, statusCode, string(respBody))
	}

	c.logger.Info("business registry config deleted", "fiscalID", fiscalID)
	return nil
}

func (c *openAPIClient) SendInvoice(ctx context.Context, invoice *models.Invoice, xmlContent string) (*SendInvoiceResponse, error) {
	// Get the company's fiscal ID from the invoice
	var fiscalID string
	if invoice.CedentePrestatore != nil {
		fiscalID = invoice.CedentePrestatore.FiscalIDCode
	}

	// Fetch the Business Registry configuration for this fiscal ID
	var applySignature, applyStorage bool
	if fiscalID != "" {
		brConfig, err := c.GetBusinessRegistryConfig(ctx, fiscalID)
		if err != nil {
			c.logger.Warn("failed to get business registry config, using default settings",
				"fiscalID", fiscalID,
				"error", err,
			)
			// Fall back to loaded config
			cfg := c.configLoader()
			applySignature = cfg.ApplySignature
			applyStorage = cfg.ApplyStorage
		} else {
			applySignature = brConfig.ApplySignature
			applyStorage = brConfig.ApplyLegalStorage
			c.logger.Info("using business registry config for invoice",
				"fiscalID", fiscalID,
				"applySignature", applySignature,
				"applyLegalStorage", applyStorage,
			)
		}
	} else {
		// No fiscal ID, use loaded config
		cfg := c.configLoader()
		applySignature = cfg.ApplySignature
		applyStorage = cfg.ApplyStorage
	}

	// Determine endpoint based on storage/signature config
	var endpoint string
	if applySignature && applyStorage {
		endpoint = "/invoices_signature_legal_storage"
	} else if applySignature {
		endpoint = "/invoices_signature"
	} else if applyStorage {
		endpoint = "/invoices_legal_storage"
	} else {
		endpoint = "/invoices"
	}

	// Debug: log the XML content
	c.logger.Info("sending invoice XML to SDI",
		"endpoint", endpoint,
		"xmlLength", len(xmlContent),
	)
	// Write XML to file for debugging
	_ = writeDebugXML(xmlContent, invoice.Number)

	// Send raw XML with Content-Type: application/xml
	respBody, statusCode, err := c.doXMLRequestWithRetry(ctx, http.MethodPost, endpoint, xmlContent)
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

	// Populate the applied settings based on the business registry config
	result.SignatureApplied = applySignature
	result.LegalStorageApplied = applyStorage

	c.logger.Info("invoice sent successfully",
		"invoiceNumber", invoice.Number,
		"openApiUUID", result.UUID,
		"signatureApplied", applySignature,
		"legalStorageApplied", applyStorage,
	)

	return &result, nil
}

func (c *openAPIClient) GetInvoiceStatus(ctx context.Context, uuid string) (*InvoiceStatusResponse, error) {
	// Check cache first if Redis is available
	if c.redisClient != nil {
		cacheKey := invoiceStatusCachePrefix + uuid
		if cached, err := c.redisClient.Get(ctx, cacheKey); err == nil && cached != "" {
			var result InvoiceStatusResponse
			if err := json.Unmarshal([]byte(cached), &result); err == nil {
				c.logger.Debug("invoice status cache hit", "uuid", uuid)
				return &result, nil
			}
		}
	}

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

	// Cache the result if Redis is available
	if c.redisClient != nil {
		cacheKey := invoiceStatusCachePrefix + uuid
		if cacheData, err := json.Marshal(result); err == nil {
			if err := c.redisClient.Set(ctx, cacheKey, string(cacheData), invoiceStatusCacheTTL); err != nil {
				c.logger.Warn("failed to cache invoice status", "uuid", uuid, "error", err)
			} else {
				c.logger.Debug("invoice status cached", "uuid", uuid, "ttl", invoiceStatusCacheTTL)
			}
		}
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
	path := fmt.Sprintf("/invoices?type=1&createdAt[after]=%s&page=%d",
		fromDate.Format("2006-01-02"), page)

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d, response: %s", ErrOpenAPIRequestFailed, statusCode, string(respBody))
	}

	// Debug: log raw response
	c.logger.Info("supplier invoices raw response",
		"response", string(respBody),
	)

	// Try to parse wrapped response first: {"data": {...}, "success": true}
	var wrapper struct {
		Data    json.RawMessage `json:"data"`
		Success bool            `json:"success"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err == nil && wrapper.Success {
		// Parse the data field - it could be an array or object
		var invoices []OpenAPIInvoice
		if err := json.Unmarshal(wrapper.Data, &invoices); err == nil {
			return &SupplierInvoicesResponse{
				Invoices:   invoices,
				Total:      len(invoices),
				Page:       page,
				PageSize:   pageSize,
				TotalPages: 1,
			}, nil
		}
		// Try as SupplierInvoicesResponse
		var result SupplierInvoicesResponse
		if err := json.Unmarshal(wrapper.Data, &result); err == nil {
			return &result, nil
		}
	}

	// Try direct parse
	var result SupplierInvoicesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// GetAllInvoices fetches all invoices (both sent and received) from OpenAPI SDI.
// The API defaults to type=0 (sent) when no type is specified, so we must fetch
// both type=0 and type=1 separately and merge the results.
func (c *openAPIClient) GetAllInvoices(ctx context.Context, fromDate time.Time, page, pageSize int) (*SupplierInvoicesResponse, error) {
	var allInvoices []OpenAPIInvoice

	// Fetch both sent (type=0) and received (type=1) invoices
	// Per OpenAPI SDI spec: type=0 (sent to customer), type=1 (received from supplier)
	// Date filter uses createdAt[after], pagination uses page
	for _, invoiceType := range []int{0, 1} {
		path := fmt.Sprintf("/invoices?type=%d&createdAt[after]=%s&page=%d",
			invoiceType, fromDate.Format("2006-01-02"), page)

		respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}

		if statusCode != http.StatusOK {
			return nil, fmt.Errorf("%w: status %d, response: %s", ErrOpenAPIRequestFailed, statusCode, string(respBody))
		}

		typeName := "sent"
		if invoiceType == 1 {
			typeName = "received"
		}
		c.logger.Debug("invoices raw response",
			"type", typeName,
			"response", string(respBody),
		)

		invoices, err := c.parseInvoicesResponse(respBody, page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s invoices: %w", typeName, err)
		}

		c.logger.Info("fetched invoices from OpenAPI",
			"type", typeName,
			"count", len(invoices.Invoices),
		)

		allInvoices = append(allInvoices, invoices.Invoices...)
	}

	return &SupplierInvoicesResponse{
		Invoices:   allInvoices,
		Total:      len(allInvoices),
		Page:       page,
		PageSize:   pageSize,
		TotalPages: 1,
	}, nil
}

// parseInvoicesResponse parses an OpenAPI SDI invoices response body.
func (c *openAPIClient) parseInvoicesResponse(respBody []byte, page, pageSize int) (*SupplierInvoicesResponse, error) {
	// Try to parse wrapped response first: {"data": [...], "success": true}
	var wrapper struct {
		Data    json.RawMessage `json:"data"`
		Success bool            `json:"success"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err == nil && wrapper.Success {
		var invoices []OpenAPIInvoice
		if err := json.Unmarshal(wrapper.Data, &invoices); err == nil {
			return &SupplierInvoicesResponse{
				Invoices:   invoices,
				Total:      len(invoices),
				Page:       page,
				PageSize:   pageSize,
				TotalPages: 1,
			}, nil
		}
		var result SupplierInvoicesResponse
		if err := json.Unmarshal(wrapper.Data, &result); err == nil {
			return &result, nil
		}
	}

	var result SupplierInvoicesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
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

// InvalidateInvoiceStatusCache removes the cached invoice status for a given UUID
func (c *openAPIClient) InvalidateInvoiceStatusCache(ctx context.Context, uuid string) error {
	if c.redisClient == nil {
		return nil // No-op if Redis is not configured
	}

	cacheKey := invoiceStatusCachePrefix + uuid
	if err := c.redisClient.Del(ctx, cacheKey); err != nil {
		c.logger.Warn("failed to invalidate invoice status cache", "uuid", uuid, "error", err)
		return err
	}

	c.logger.Debug("invoice status cache invalidated", "uuid", uuid)
	return nil
}

// ConfigureAPICallbacks configures webhook callbacks on OpenAPI SDI
// Per OpenAPI.it spec: POST /api_configurations with fiscal_id + callbacks array
func (c *openAPIClient) ConfigureAPICallbacks(ctx context.Context, cfg APICallbackConfig) error {
	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPost, "/api_configurations", cfg)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		if apiErr := parseOpenAPIError(respBody, statusCode); apiErr != nil {
			return apiErr
		}
		return fmt.Errorf("%w: status %d, body: %s", ErrOpenAPIRequestFailed, statusCode, string(respBody))
	}

	c.logger.Info("API callbacks configured",
		"fiscalID", cfg.FiscalID,
		"callbackCount", len(cfg.Callbacks),
	)

	return nil
}

// GetAPIConfigurations retrieves all API configurations from OpenAPI SDI
// Per OpenAPI.it spec: GET /api_configurations
// Response: {"data": [{id, fiscal_id, callback: {event, url, ...}}, ...], "success": true}
func (c *openAPIClient) GetAPIConfigurations(ctx context.Context) (*APIConfigurationResponse, error) {
	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, "/api_configurations", nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, ErrOpenAPINotFound
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrOpenAPIRequestFailed, statusCode)
	}

	// Parse wrapped response: {"data": [...], "success": true}
	var wrapper struct {
		Data    []APIConfigurationEntry `json:"data"`
		Success bool                    `json:"success"`
		Message string                  `json:"message"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse API configurations response: %w", err)
	}

	return &APIConfigurationResponse{Entries: wrapper.Data}, nil
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

// writeDebugXML writes XML to a file for debugging purposes
func writeDebugXML(xmlContent string, invoiceNumber string) error {
	filename := fmt.Sprintf("/tmp/invoice_%s.xml", strings.ReplaceAll(invoiceNumber, "/", "_"))
	return os.WriteFile(filename, []byte(xmlContent), 0o600)
}
