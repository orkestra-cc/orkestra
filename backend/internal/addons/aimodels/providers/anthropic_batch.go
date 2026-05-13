package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// --- Anthropic Message Batches API types ---

type anthropicBatchRequestItem struct {
	CustomID string               `json:"custom_id"`
	Params   anthropicBatchParams `json:"params"`
}

type anthropicBatchParams struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`

	Temperature *float64 `json:"temperature,omitempty"`
}

type anthropicCreateBatchRequest struct {
	Requests []anthropicBatchRequestItem `json:"requests"`
}

type anthropicBatchResponse struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	ProcessingStatus string `json:"processing_status"` // "in_progress", "ended"
	RequestCounts    struct {
		Processing int `json:"processing"`
		Succeeded  int `json:"succeeded"`
		Errored    int `json:"errored"`
		Canceled   int `json:"canceled"`
		Expired    int `json:"expired"`
	} `json:"request_counts"`
	ResultsURL *string    `json:"results_url"`
	EndedAt    *time.Time `json:"ended_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type anthropicBatchResultLine struct {
	CustomID string `json:"custom_id"`
	Result   struct {
		Type    string             `json:"type"` // "succeeded", "errored", "canceled", "expired"
		Message *anthropicResponse `json:"message,omitempty"`
		Error   *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	} `json:"result"`
}

// SubmitBatch creates a Message Batch via POST /v1/messages/batches
func (p *anthropicProvider) SubmitBatch(ctx context.Context, requests []BatchRequest) (*BatchSubmission, error) {
	items := make([]anthropicBatchRequestItem, len(requests))
	for i, r := range requests {
		maxTokens := r.Options.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 4096
		}
		params := anthropicBatchParams{
			Model:     p.modelName,
			Messages:  []anthropicMessage{{Role: "user", Content: r.Prompt}},
			System:    r.Options.SystemPrompt,
			MaxTokens: maxTokens,
		}
		if r.Options.Temperature > 0 {
			params.Temperature = &r.Options.Temperature
		}
		items[i] = anthropicBatchRequestItem{
			CustomID: r.CustomID,
			Params:   params,
		}
	}

	body, err := json.Marshal(anthropicCreateBatchRequest{Requests: items})
	if err != nil {
		return nil, fmt.Errorf("anthropic batch marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL+"/messages/batches", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic batch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic batch call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic batch error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result anthropicBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("anthropic batch decode: %w", err)
	}

	ids := make([]string, len(requests))
	for i, r := range requests {
		ids[i] = r.CustomID
	}

	return &BatchSubmission{
		BatchID:    result.ID,
		Provider:   "anthropic",
		RequestIDs: ids,
		CreatedAt:  result.CreatedAt,
	}, nil
}

// PollBatch checks batch status; when ended, fetches and parses streamed results
func (p *anthropicProvider) PollBatch(ctx context.Context, batchID string) (*BatchStatus, error) {
	// GET /v1/messages/batches/{id}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, anthropicAPIURL+"/messages/batches/"+batchID, nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic poll request: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic poll call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic poll error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var batch anthropicBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, fmt.Errorf("anthropic poll decode: %w", err)
	}

	status := &BatchStatus{BatchID: batchID}

	switch batch.ProcessingStatus {
	case "ended":
		// Check if all errored/expired
		if batch.RequestCounts.Succeeded == 0 {
			status.State = "failed"
			status.Error = fmt.Sprintf("batch ended with 0 successes: %d errored, %d expired",
				batch.RequestCounts.Errored, batch.RequestCounts.Expired)
			return status, nil
		}

		// Fetch results
		results, err := p.fetchBatchResults(ctx, batchID)
		if err != nil {
			return nil, fmt.Errorf("fetch batch results: %w", err)
		}
		status.State = "completed"
		status.Results = results

	default:
		status.State = "processing"
	}

	return status, nil
}

// fetchBatchResults streams JSONL results from GET /v1/messages/batches/{id}/results
func (p *anthropicProvider) fetchBatchResults(ctx context.Context, batchID string) ([]BatchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, anthropicAPIURL+"/messages/batches/"+batchID+"/results", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("results error (status %d): %s", resp.StatusCode, string(body))
	}

	var results []BatchResult
	scanner := bufio.NewScanner(resp.Body)
	// Allow up to 10MB per line for large LLM responses
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item anthropicBatchResultLine
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}

		br := BatchResult{CustomID: item.CustomID}
		switch item.Result.Type {
		case "succeeded":
			if item.Result.Message != nil {
				var sb strings.Builder
				for _, block := range item.Result.Message.Content {
					if block.Type == "text" {
						sb.WriteString(block.Text)
					}
				}
				br.Text = sb.String()
				br.InputTokens = item.Result.Message.Usage.InputTokens
				br.OutputTokens = item.Result.Message.Usage.OutputTokens
			}
		default:
			if item.Result.Error != nil {
				br.Error = item.Result.Error.Message
			} else {
				br.Error = "request " + item.Result.Type
			}
		}
		results = append(results, br)
	}

	return results, scanner.Err()
}

// CancelBatch cancels a pending batch
func (p *anthropicProvider) CancelBatch(ctx context.Context, batchID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL+"/messages/batches/"+batchID+"/cancel", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic cancel batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("anthropic cancel error (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
