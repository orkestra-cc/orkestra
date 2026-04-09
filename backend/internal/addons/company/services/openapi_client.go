package services

import (
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

	"github.com/orkestra/backend/internal/addons/company/config"
	"github.com/orkestra/backend/internal/addons/company/models"
)

// Client errors
var (
	ErrCompanyNotFound     = errors.New("company not found")
	ErrAPIRequestFailed    = errors.New("company API request failed")
	ErrCircuitBreakerOpen  = errors.New("circuit breaker is open")
	ErrInvalidTaxCode      = errors.New("invalid tax code")
)

// RedisClient defines the interface for Redis operations
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
}

// CompanyAPIClient defines the interface for OpenAPI Company API operations
type CompanyAPIClient interface {
	LookupByTaxCode(ctx context.Context, taxCode string) (*models.OpenAPIBaseResponse, error)
	LookupByType(ctx context.Context, taxCode string, lookupType string) (*models.OpenAPIBaseResponse, error)
	SearchCompanies(ctx context.Context, params *models.CompanySearchParams) (*models.OpenAPISearchResponse, error)
	Ping(ctx context.Context) error
}

type companyAPIClient struct {
	config  *config.CompanyAPIConfig
	client  *http.Client
	logger  *slog.Logger
	redis   RedisClient
	breaker *circuitBreaker
}

// NewCompanyAPIClient creates a new CompanyAPIClient without Redis caching
func NewCompanyAPIClient(cfg *config.CompanyAPIConfig, logger *slog.Logger) CompanyAPIClient {
	return &companyAPIClient{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		logger:  logger,
		breaker: newCircuitBreaker(5, 30*time.Second),
	}
}

// NewCompanyAPIClientWithCache creates a new CompanyAPIClient with Redis caching
func NewCompanyAPIClientWithCache(cfg *config.CompanyAPIConfig, logger *slog.Logger, redis RedisClient) CompanyAPIClient {
	return &companyAPIClient{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		logger:  logger,
		redis:   redis,
		breaker: newCircuitBreaker(5, 30*time.Second),
	}
}

// LookupByTaxCode calls the OpenAPI Company API to look up a company by tax code
func (c *companyAPIClient) LookupByTaxCode(ctx context.Context, taxCode string) (*models.OpenAPIBaseResponse, error) {
	if taxCode == "" {
		return nil, ErrInvalidTaxCode
	}

	// Check Redis cache first
	if c.redis != nil {
		cacheKey := fmt.Sprintf("company:lookup:%s", taxCode)
		cached, err := c.redis.Get(ctx, cacheKey)
		if err == nil && cached != "" {
			var response models.OpenAPIBaseResponse
			if err := json.Unmarshal([]byte(cached), &response); err == nil {
				c.logger.Debug("company lookup cache hit",
					"taxCode", taxCode,
				)
				return &response, nil
			}
		}
	}

	// Call external API
	path := fmt.Sprintf("/IT-start/%s", taxCode)
	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	// Parse response
	c.logger.Debug("company API raw response",
		"taxCode", taxCode,
		"statusCode", statusCode,
		"body", string(respBody),
	)

	var response models.OpenAPIBaseResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse company API response: %w", err)
	}

	// Handle authentication errors
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		c.logger.Error("company API authentication failed",
			"taxCode", taxCode,
			"statusCode", statusCode,
			"message", response.Message,
		)
		return nil, fmt.Errorf("%w: %s", ErrAPIRequestFailed, response.Message)
	}

	// Handle API-level errors
	if statusCode == http.StatusNotFound || !response.Success {
		c.logger.Debug("company API lookup not found",
			"taxCode", taxCode,
			"statusCode", statusCode,
			"success", response.Success,
			"message", response.Message,
			"error", response.Error,
		)
		return nil, ErrCompanyNotFound
	}

	// Cache successful response in Redis
	if c.redis != nil {
		cacheKey := fmt.Sprintf("company:lookup:%s", taxCode)
		cacheData, _ := json.Marshal(response)
		if err := c.redis.Set(ctx, cacheKey, string(cacheData), c.config.CacheTTL); err != nil {
			c.logger.Warn("failed to cache company lookup",
				"taxCode", taxCode,
				"error", err,
			)
		}
	}

	return &response, nil
}

// LookupByType calls the OpenAPI Company API with a specific endpoint type
func (c *companyAPIClient) LookupByType(ctx context.Context, taxCode string, lookupType string) (*models.OpenAPIBaseResponse, error) {
	if taxCode == "" {
		return nil, ErrInvalidTaxCode
	}

	path, err := models.EndpointForType(lookupType, taxCode)
	if err != nil {
		return nil, fmt.Errorf("invalid lookup type: %w", err)
	}

	// Check Redis cache first (key includes type)
	if c.redis != nil {
		cacheKey := fmt.Sprintf("company:%s:%s", lookupType, taxCode)
		cached, err := c.redis.Get(ctx, cacheKey)
		if err == nil && cached != "" {
			var response models.OpenAPIBaseResponse
			if err := json.Unmarshal([]byte(cached), &response); err == nil {
				c.logger.Debug("company lookup cache hit",
					"taxCode", taxCode,
					"type", lookupType,
				)
				return &response, nil
			}
		}
	}

	// Call external API
	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	c.logger.Debug("company API raw response",
		"taxCode", taxCode,
		"type", lookupType,
		"statusCode", statusCode,
		"body", string(respBody),
	)

	var response models.OpenAPIBaseResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse company API response: %w", err)
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		c.logger.Error("company API authentication failed",
			"taxCode", taxCode,
			"type", lookupType,
			"statusCode", statusCode,
			"message", response.Message,
		)
		return nil, fmt.Errorf("%w: %s", ErrAPIRequestFailed, response.Message)
	}

	if statusCode == http.StatusNotFound || !response.Success {
		c.logger.Debug("company API lookup not found",
			"taxCode", taxCode,
			"type", lookupType,
			"statusCode", statusCode,
			"success", response.Success,
			"message", response.Message,
			"error", response.Error,
		)
		return nil, ErrCompanyNotFound
	}

	// Cache successful response in Redis
	if c.redis != nil {
		cacheKey := fmt.Sprintf("company:%s:%s", lookupType, taxCode)
		cacheData, _ := json.Marshal(response)
		if err := c.redis.Set(ctx, cacheKey, string(cacheData), c.config.CacheTTL); err != nil {
			c.logger.Warn("failed to cache company lookup",
				"taxCode", taxCode,
				"type", lookupType,
				"error", err,
			)
		}
	}

	return &response, nil
}

// SearchCompanies calls the OpenAPI IT-search endpoint with the given search parameters
func (c *companyAPIClient) SearchCompanies(ctx context.Context, params *models.CompanySearchParams) (*models.OpenAPISearchResponse, error) {
	// Build query string
	query := buildSearchQuery(params)
	path := "/IT-search"
	if query != "" {
		path += "?" + query
	}

	// Check Redis cache
	if c.redis != nil {
		cacheKey := fmt.Sprintf("company:search:%s", query)
		cached, err := c.redis.Get(ctx, cacheKey)
		if err == nil && cached != "" {
			var response models.OpenAPISearchResponse
			if err := json.Unmarshal([]byte(cached), &response); err == nil {
				c.logger.Debug("company search cache hit", "query", query)
				return &response, nil
			}
		}
	}

	// Call external API
	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	c.logger.Debug("company search API raw response",
		"statusCode", statusCode,
		"bodyLen", len(respBody),
	)

	// 204 No Content = no results found, return empty success response
	if statusCode == http.StatusNoContent || len(respBody) == 0 {
		zero := 0
		return &models.OpenAPISearchResponse{
			Success:      true,
			Data:         []models.OpenAPICompanyData{},
			TotalResults: &zero,
		}, nil
	}

	var response models.OpenAPISearchResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse company search response: %w", err)
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		c.logger.Error("company API authentication failed",
			"statusCode", statusCode,
			"message", response.Message,
		)
		return nil, fmt.Errorf("%w: %s", ErrAPIRequestFailed, response.Message)
	}

	if !response.Success {
		c.logger.Debug("company search API unsuccessful",
			"statusCode", statusCode,
			"message", response.Message,
			"error", response.Error,
		)
		return nil, fmt.Errorf("%w: %s", ErrAPIRequestFailed, response.Message)
	}

	// Cache with short TTL (5 min) — search results are more volatile
	if c.redis != nil {
		cacheKey := fmt.Sprintf("company:search:%s", query)
		cacheData, _ := json.Marshal(response)
		if err := c.redis.Set(ctx, cacheKey, string(cacheData), 5*time.Minute); err != nil {
			c.logger.Warn("failed to cache company search", "error", err)
		}
	}

	return &response, nil
}

// buildSearchQuery constructs a URL query string from non-zero CompanySearchParams fields
func buildSearchQuery(params *models.CompanySearchParams) string {
	q := make([]string, 0, 16)
	add := func(key, val string) {
		if val != "" {
			q = append(q, key+"="+val)
		}
	}
	addInt := func(key string, val *int) {
		if val != nil {
			q = append(q, fmt.Sprintf("%s=%d", key, *val))
		}
	}
	addInt64 := func(key string, val *int64) {
		if val != nil {
			q = append(q, fmt.Sprintf("%s=%d", key, *val))
		}
	}
	addFloat := func(key string, val *float64) {
		if val != nil {
			q = append(q, fmt.Sprintf("%s=%f", key, *val))
		}
	}

	add("companyName", params.CompanyName)
	add("autocomplete", params.Autocomplete)
	add("province", params.Province)
	add("townCode", params.TownCode)
	add("atecoCode", params.AtecoCode)
	add("cciaa", params.CCIAA)
	add("reaCode", params.REACode)
	addInt64("minTurnover", params.MinTurnover)
	addInt64("maxTurnover", params.MaxTurnover)
	addInt("minEmployees", params.MinEmployees)
	addInt("maxEmployees", params.MaxEmployees)
	add("sdiCode", params.SDICode)
	add("legalFormCode", params.LegalFormCode)
	add("pec", params.PEC)
	add("shareHolderTaxCode", params.ShareHolderTaxCode)
	addFloat("lat", params.Latitude)
	addFloat("long", params.Longitude)
	addInt("radius", params.Radius)
	add("activityStatus", params.ActivityStatus)
	add("dataEnrichment", params.DataEnrichment)
	addInt64("creationTimestamp", params.CreationTimestamp)
	addInt64("lastUpdateTimestamp", params.LastUpdateTimestamp)
	addInt("dryRun", params.DryRun)
	if params.Limit > 0 {
		q = append(q, fmt.Sprintf("limit=%d", params.Limit))
	}
	if params.Skip > 0 {
		q = append(q, fmt.Sprintf("skip=%d", params.Skip))
	}

	return strings.Join(q, "&")
}

// Ping checks connectivity to the Company API
func (c *companyAPIClient) Ping(ctx context.Context) error {
	// Use a known tax code to verify API connectivity
	_, statusCode, err := c.doRequest(ctx, http.MethodGet, "/IT-start/12485671007", nil)
	if err != nil {
		return fmt.Errorf("company API ping failed: %w", err)
	}
	if statusCode >= 500 {
		return fmt.Errorf("company API returned status %d", statusCode)
	}
	return nil
}

// doRequest performs a single HTTP request to the Company API
func (c *companyAPIClient) doRequest(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	if !c.breaker.Allow() {
		return nil, 0, ErrCircuitBreakerOpen
	}

	url := c.config.GetEndpoint(path)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		c.breaker.RecordFailure()
		return nil, 0, fmt.Errorf("%w: %v", ErrAPIRequestFailed, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.breaker.RecordFailure()
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 500 {
		c.breaker.RecordFailure()
		return respBody, resp.StatusCode, fmt.Errorf("%w: status %d", ErrAPIRequestFailed, resp.StatusCode)
	}

	c.breaker.RecordSuccess()
	return respBody, resp.StatusCode, nil
}

// doRequestWithRetry performs an HTTP request with exponential backoff retry
func (c *companyAPIClient) doRequestWithRetry(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	var lastErr error

	for attempt := 0; attempt <= c.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			c.logger.Debug("retrying company API request",
				"attempt", attempt,
				"backoff", backoff,
				"path", path,
			)

			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
		}

		respBody, statusCode, err := c.doRequest(ctx, method, path, body)
		if err == nil {
			return respBody, statusCode, nil
		}

		// Don't retry on 4xx client errors
		if statusCode >= 400 && statusCode < 500 {
			return respBody, statusCode, nil
		}

		// Don't retry on timeout errors — API is slow, retrying just adds more wait time
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
			return nil, 0, fmt.Errorf("%w: request timed out", ErrAPIRequestFailed)
		}

		lastErr = err
	}

	return nil, 0, fmt.Errorf("all retry attempts exhausted: %w", lastErr)
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
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	}
	return false
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
