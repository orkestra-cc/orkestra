package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/addons/documents/config"
)

// Errors for Gotenberg client
var (
	ErrGotenbergUnavailable = errors.New("gotenberg service is unavailable")
	ErrGotenbergTimeout     = errors.New("gotenberg request timed out")
	ErrGotenbergConversion  = errors.New("gotenberg conversion failed")
	ErrCircuitBreakerOpen   = errors.New("circuit breaker is open, service temporarily unavailable")
)

// GotenbergClient defines the interface for Gotenberg PDF generation
type GotenbergClient interface {
	// ConvertHTMLToPDF converts HTML content to PDF
	ConvertHTMLToPDF(ctx context.Context, html string, opts *config.PDFOptions) ([]byte, error)
	// Ping checks if Gotenberg is available
	Ping(ctx context.Context) error
	// IsAvailable returns whether the service is available
	IsAvailable() bool
}

// circuitBreaker implements a simple circuit breaker pattern
type circuitBreaker struct {
	mu             sync.RWMutex
	failures       int
	lastFailure    time.Time
	state          string // closed, open, half-open
	threshold      int
	resetTimeout   time.Duration
	halfOpenMax    int
	halfOpenCalls  int
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{
		state:        "closed",
		threshold:    5,
		resetTimeout: 30 * time.Second,
		halfOpenMax:  3,
	}
}

func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "open":
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = "half-open"
			cb.halfOpenCalls = 0
			return true
		}
		return false
	case "half-open":
		if cb.halfOpenCalls < cb.halfOpenMax {
			cb.halfOpenCalls++
			return true
		}
		return false
	default: // closed
		return true
	}
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == "half-open" {
		cb.state = "closed"
	}
	cb.failures = 0
}

func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= cb.threshold {
		cb.state = "open"
	}
}

func (cb *circuitBreaker) isOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state == "open"
}

type gotenbergClient struct {
	baseURL        string
	httpClient     *http.Client
	retryAttempts  int
	logger         *slog.Logger
	circuitBreaker *circuitBreaker
}

// NewGotenbergClient creates a new Gotenberg client
func NewGotenbergClient(cfg *config.Config, logger *slog.Logger) GotenbergClient {
	return &gotenbergClient{
		baseURL: cfg.GotenbergURL,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		retryAttempts:  cfg.RetryAttempts,
		logger:         logger,
		circuitBreaker: newCircuitBreaker(),
	}
}

// ConvertHTMLToPDF converts HTML to PDF using Gotenberg's Chromium endpoint
func (c *gotenbergClient) ConvertHTMLToPDF(ctx context.Context, html string, opts *config.PDFOptions) ([]byte, error) {
	if !c.circuitBreaker.allow() {
		return nil, ErrCircuitBreakerOpen
	}

	if opts == nil {
		opts = config.DefaultPDFOptions()
	}

	var lastErr error
	for attempt := 1; attempt <= c.retryAttempts; attempt++ {
		pdf, err := c.doConversion(ctx, html, opts)
		if err == nil {
			c.circuitBreaker.recordSuccess()
			return pdf, nil
		}

		lastErr = err
		c.logger.Warn("Gotenberg conversion attempt failed",
			slog.Int("attempt", attempt),
			slog.Int("maxAttempts", c.retryAttempts),
			slog.String("error", err.Error()),
		)

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			break
		}

		// Exponential backoff
		if attempt < c.retryAttempts {
			backoff := time.Duration(attempt*attempt) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	c.circuitBreaker.recordFailure()
	return nil, lastErr
}

func (c *gotenbergClient) doConversion(ctx context.Context, html string, opts *config.PDFOptions) ([]byte, error) {
	// Build multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add the HTML file
	htmlPart, err := writer.CreateFormFile("files", "index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := htmlPart.Write([]byte(html)); err != nil {
		return nil, fmt.Errorf("failed to write HTML content: %w", err)
	}

	// Add PDF options
	// Paper size
	switch opts.PageSize {
	case "A4":
		writer.WriteField("paperWidth", "8.27")
		writer.WriteField("paperHeight", "11.69")
	case "A3":
		writer.WriteField("paperWidth", "11.69")
		writer.WriteField("paperHeight", "16.54")
	case "Letter":
		writer.WriteField("paperWidth", "8.5")
		writer.WriteField("paperHeight", "11")
	case "Legal":
		writer.WriteField("paperWidth", "8.5")
		writer.WriteField("paperHeight", "14")
	default:
		// Default to A4
		writer.WriteField("paperWidth", "8.27")
		writer.WriteField("paperHeight", "11.69")
	}

	// Orientation (swap dimensions for landscape)
	if opts.Orientation == "landscape" {
		writer.WriteField("landscape", "true")
	}

	// Margins (convert mm to inches)
	writer.WriteField("marginTop", fmt.Sprintf("%.2f", opts.Margins.Top/25.4))
	writer.WriteField("marginBottom", fmt.Sprintf("%.2f", opts.Margins.Bottom/25.4))
	writer.WriteField("marginLeft", fmt.Sprintf("%.2f", opts.Margins.Left/25.4))
	writer.WriteField("marginRight", fmt.Sprintf("%.2f", opts.Margins.Right/25.4))

	// Scale
	if opts.Scale > 0 {
		writer.WriteField("scale", strconv.FormatFloat(opts.Scale, 'f', 2, 64))
	}

	// Print background
	writer.WriteField("printBackground", "true")

	// Add header if provided
	if opts.HeaderHTML != "" {
		headerPart, err := writer.CreateFormFile("files", "header.html")
		if err != nil {
			return nil, fmt.Errorf("failed to create header form file: %w", err)
		}
		if _, err := headerPart.Write([]byte(opts.HeaderHTML)); err != nil {
			return nil, fmt.Errorf("failed to write header content: %w", err)
		}
	}

	// Add footer if provided
	if opts.FooterHTML != "" {
		footerPart, err := writer.CreateFormFile("files", "footer.html")
		if err != nil {
			return nil, fmt.Errorf("failed to create footer form file: %w", err)
		}
		if _, err := footerPart.Write([]byte(opts.FooterHTML)); err != nil {
			return nil, fmt.Errorf("failed to write footer content: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	url := fmt.Sprintf("%s/forms/chromium/convert/html", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrGotenbergTimeout
		}
		return nil, fmt.Errorf("%w: %s", ErrGotenbergUnavailable, err.Error())
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d, body: %s", ErrGotenbergConversion, resp.StatusCode, string(bodyBytes))
	}

	// Read PDF content
	pdfContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}

	return pdfContent, nil
}

// Ping checks if Gotenberg is available
func (c *gotenbergClient) Ping(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrGotenbergUnavailable, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: health check returned status %d", ErrGotenbergUnavailable, resp.StatusCode)
	}

	return nil
}

// IsAvailable returns whether the service is available (circuit breaker not open)
func (c *gotenbergClient) IsAvailable() bool {
	return !c.circuitBreaker.isOpen()
}
