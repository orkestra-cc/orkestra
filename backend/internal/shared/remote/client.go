// Package remote provides HTTP client implementations of cross-module interfaces
// for communicating with the AI service sidecar.
//
// When AI_SERVICE_URL is set, the monolith registers these remote implementations
// in the ServiceRegistry instead of the local module implementations:
//
//   - RemoteAIModelProvider  → iface.AIModelProvider
//   - RemoteRAGQueryProvider → iface.RAGQueryProvider
//
// The remote clients call the AI service's internal API endpoints (/v1/internal/...).
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// client is a shared HTTP client for all remote providers.
type client struct {
	baseURL    string
	httpClient *http.Client
}

func newClient(baseURL string) *client {
	return &client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Long timeout for LLM operations
		},
	}
}

func (c *client) post(ctx context.Context, path string, body interface{}, result interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("remote: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("remote: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	forwardAuth(ctx, req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote: %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remote: %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("remote: decode response: %w", err)
		}
	}
	return nil
}

func (c *client) get(ctx context.Context, path string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("remote: create request: %w", err)
	}
	forwardAuth(ctx, req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote: %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remote: %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("remote: decode response: %w", err)
		}
	}
	return nil
}

// forwardAuth passes the caller's JWT from context to the outgoing request.
// The AI service validates JWTs with the same public key.
func forwardAuth(ctx context.Context, req *http.Request) {
	// The HTTP request is stored in context by the Huma middleware
	if httpReq, ok := ctx.Value("http_request").(*http.Request); ok {
		if auth := httpReq.Header.Get("Authorization"); auth != "" {
			req.Header.Set("Authorization", auth)
		}
	}
}
