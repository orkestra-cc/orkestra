package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// openaiBaseURL returns the API base for the provider (custom or default)
func (p *openaiProvider) openaiBaseURL() string {
	// The go-openai config is not exported, but we can access it through
	// the client's internal config. For batch we need raw HTTP calls.
	// Default to OpenAI API base; custom base URLs mean local servers which
	// don't support batch. We'll only attempt batch on the real OpenAI API.
	return "https://api.openai.com/v1"
}

// openaiAPIKey is stored in the client config but not exported.
// We store it separately for batch operations.
var openaiKeyStore = struct {
	keys map[*openaiProvider]string
}{keys: make(map[*openaiProvider]string)}

// NewOpenAILLMProviderWithBatch creates an LLMProvider backed by OpenAI with batch support
func NewOpenAILLMProviderWithBatch(apiKey, modelName string, baseURL string) LLMProvider {
	p := &openaiProvider{
		client:    newOpenAIClient(apiKey, baseURL),
		modelName: modelName,
	}
	openaiKeyStore.keys[p] = apiKey
	return p
}

func (p *openaiProvider) getAPIKey() string {
	if key, ok := openaiKeyStore.keys[p]; ok {
		return key
	}
	return ""
}

// --- OpenAI Batch API types ---

type openaiBatchJSONLLine struct {
	CustomID string `json:"custom_id"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Body     struct {
		Model       string               `json:"model"`
		Messages    []openaiBatchMessage `json:"messages"`
		MaxTokens   int                  `json:"max_tokens,omitempty"`
		Temperature float32              `json:"temperature,omitempty"`
	} `json:"body"`
}

type openaiBatchMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiBatchCreateRequest struct {
	InputFileID      string `json:"input_file_id"`
	Endpoint         string `json:"endpoint"`
	CompletionWindow string `json:"completion_window"`
}

type openaiBatchStatusResponse struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"` // validating, in_progress, completed, failed, expired, cancelled
	OutputFileID *string `json:"output_file_id"`
	ErrorFileID  *string `json:"error_file_id"`
}

type openaiBatchOutputLine struct {
	CustomID string `json:"custom_id"`
	Response *struct {
		StatusCode int             `json:"status_code"`
		Body       json.RawMessage `json:"body"`
	} `json:"response"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type openaiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// SubmitBatch uploads JSONL → creates batch job
func (p *openaiProvider) SubmitBatch(ctx context.Context, requests []BatchRequest) (*BatchSubmission, error) {
	apiKey := p.getAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("openai batch: API key not available")
	}
	baseURL := p.openaiBaseURL()

	// Build JSONL content
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range requests {
		line := openaiBatchJSONLLine{
			CustomID: r.CustomID,
			Method:   "POST",
			URL:      "/v1/chat/completions",
		}
		line.Body.Model = p.modelName
		var msgs []openaiBatchMessage
		if r.Options.SystemPrompt != "" {
			msgs = append(msgs, openaiBatchMessage{Role: "system", Content: r.Options.SystemPrompt})
		}
		msgs = append(msgs, openaiBatchMessage{Role: "user", Content: r.Prompt})
		line.Body.Messages = msgs
		if r.Options.MaxTokens > 0 {
			line.Body.MaxTokens = r.Options.MaxTokens
		}
		if r.Options.Temperature > 0 {
			line.Body.Temperature = float32(r.Options.Temperature)
		}
		if err := enc.Encode(line); err != nil {
			return nil, fmt.Errorf("openai batch jsonl encode: %w", err)
		}
	}

	// Upload file
	fileID, err := p.uploadBatchFile(ctx, baseURL, apiKey, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("openai batch file upload: %w", err)
	}

	// Create batch
	createBody, _ := json.Marshal(openaiBatchCreateRequest{
		InputFileID:      fileID,
		Endpoint:         "/v1/chat/completions",
		CompletionWindow: "24h",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/batches", bytes.NewReader(createBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai create batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai create batch error (status %d): %s", resp.StatusCode, string(body))
	}

	var batchResp openaiBatchStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, fmt.Errorf("openai batch decode: %w", err)
	}

	ids := make([]string, len(requests))
	for i, r := range requests {
		ids[i] = r.CustomID
	}

	return &BatchSubmission{
		BatchID:    batchResp.ID,
		Provider:   "openai",
		RequestIDs: ids,
		CreatedAt:  time.Now(),
	}, nil
}

// uploadBatchFile uploads JSONL content as a file with purpose="batch"
func (p *openaiProvider) uploadBatchFile(ctx context.Context, baseURL, apiKey string, content []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "batch_input.jsonl")
	if err != nil {
		return "", err
	}
	part.Write(content)
	writer.WriteField("purpose", "batch")
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/files", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("file upload error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// PollBatch checks batch status; downloads results when completed
func (p *openaiProvider) PollBatch(ctx context.Context, batchID string) (*BatchStatus, error) {
	apiKey := p.getAPIKey()
	baseURL := p.openaiBaseURL()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/batches/"+batchID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai poll batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai poll error (status %d): %s", resp.StatusCode, string(body))
	}

	var batchResp openaiBatchStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, err
	}

	status := &BatchStatus{BatchID: batchID}

	switch batchResp.Status {
	case "completed":
		if batchResp.OutputFileID == nil {
			status.State = "failed"
			status.Error = "batch completed without output file"
			return status, nil
		}
		results, err := p.downloadBatchResults(ctx, baseURL, apiKey, *batchResp.OutputFileID)
		if err != nil {
			return nil, fmt.Errorf("download batch results: %w", err)
		}
		status.State = "completed"
		status.Results = results

	case "failed", "expired", "cancelled":
		status.State = "failed"
		status.Error = "batch " + batchResp.Status

	default: // validating, in_progress, finalizing
		status.State = "processing"
	}

	return status, nil
}

// downloadBatchResults fetches and parses the output JSONL file
func (p *openaiProvider) downloadBatchResults(ctx context.Context, baseURL, apiKey, fileID string) ([]BatchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/files/"+fileID+"/content", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download file error (status %d): %s", resp.StatusCode, string(body))
	}

	var results []BatchResult
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item openaiBatchOutputLine
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}

		br := BatchResult{CustomID: item.CustomID}
		if item.Error != nil {
			br.Error = item.Error.Message
		} else if item.Response != nil && item.Response.StatusCode == 200 {
			var chatResp openaiChatResponse
			if err := json.Unmarshal(item.Response.Body, &chatResp); err == nil {
				if len(chatResp.Choices) > 0 {
					br.Text = chatResp.Choices[0].Message.Content
				}
				br.InputTokens = chatResp.Usage.PromptTokens
				br.OutputTokens = chatResp.Usage.CompletionTokens
			}
		} else if item.Response != nil {
			br.Error = fmt.Sprintf("request failed with status %d", item.Response.StatusCode)
		}
		results = append(results, br)
	}

	return results, scanner.Err()
}

// CancelBatch cancels a pending OpenAI batch
func (p *openaiProvider) CancelBatch(ctx context.Context, batchID string) error {
	apiKey := p.getAPIKey()
	baseURL := p.openaiBaseURL()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/batches/"+batchID+"/cancel", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("openai cancel batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai cancel error (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
