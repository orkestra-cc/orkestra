package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// --- Gemini Batch API types ---

type geminiBatchInlineRequest struct {
	Request  geminiGenerateRequest `json:"request"`
	Metadata struct {
		Key string `json:"key"`
	} `json:"metadata"`
}

type geminiBatchInputConfig struct {
	Requests struct {
		Requests []geminiBatchInlineRequest `json:"requests"`
	} `json:"requests"`
}

type geminiBatchCreateRequest struct {
	Batch struct {
		DisplayName string                 `json:"display_name"`
		InputConfig geminiBatchInputConfig `json:"input_config"`
	} `json:"batch"`
}

type geminiBatchResponse struct {
	Name  string `json:"name"`  // operation/batch name for polling
	State string `json:"state"` // JOB_STATE_PENDING, JOB_STATE_RUNNING, JOB_STATE_SUCCEEDED, JOB_STATE_FAILED, etc.
	Dest  *struct {
		InlineResponses []struct {
			Response struct {
				Candidates    []geminiCandidate `json:"candidates"`
				UsageMetadata *struct {
					PromptTokenCount     int `json:"promptTokenCount"`
					CandidatesTokenCount int `json:"candidatesTokenCount"`
				} `json:"usageMetadata"`
			} `json:"response"`
			Metadata struct {
				Key string `json:"key"`
			} `json:"metadata"`
		} `json:"inlineResponses"`
	} `json:"dest"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// SubmitBatch creates a Gemini batch via batchGenerateContent
func (p *geminiLLMProvider) SubmitBatch(ctx context.Context, requests []BatchRequest) (*BatchSubmission, error) {
	batchReq := geminiBatchCreateRequest{}
	batchReq.Batch.DisplayName = fmt.Sprintf("orkestra-batch-%d", time.Now().Unix())

	for _, r := range requests {
		genReq := geminiGenerateRequest{
			Contents: []geminiContent{
				{Role: "user", Parts: []geminiPart{{Text: r.Prompt}}},
			},
		}
		if r.Options.SystemPrompt != "" {
			genReq.SystemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: r.Options.SystemPrompt}},
			}
		}
		genConfig := &geminiGenerationConfig{}
		hasConfig := false
		if r.Options.Temperature > 0 {
			genConfig.Temperature = &r.Options.Temperature
			hasConfig = true
		}
		if r.Options.MaxTokens > 0 {
			genConfig.MaxOutputTokens = &r.Options.MaxTokens
			hasConfig = true
		}
		if hasConfig {
			genReq.GenerationConfig = genConfig
		}

		batchReq.Batch.InputConfig.Requests.Requests = append(
			batchReq.Batch.InputConfig.Requests.Requests,
			geminiBatchInlineRequest{
				Request: genReq,
				Metadata: struct {
					Key string `json:"key"`
				}{Key: r.CustomID},
			},
		)
	}

	body, err := json.Marshal(batchReq)
	if err != nil {
		return nil, fmt.Errorf("gemini batch marshal: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:batchGenerateContent?key=%s", geminiAPIURL, p.modelName, p.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini batch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini batch call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini batch error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result geminiBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("gemini batch decode: %w", err)
	}

	ids := make([]string, len(requests))
	for i, r := range requests {
		ids[i] = r.CustomID
	}

	return &BatchSubmission{
		BatchID:    result.Name,
		Provider:   "gemini",
		RequestIDs: ids,
		CreatedAt:  time.Now(),
	}, nil
}

// PollBatch checks the Gemini batch operation status
func (p *geminiLLMProvider) PollBatch(ctx context.Context, batchID string) (*BatchStatus, error) {
	url := fmt.Sprintf("%s/v1beta/%s?key=%s", geminiAPIURL, batchID, p.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini poll batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini poll error (status %d): %s", resp.StatusCode, string(body))
	}

	var result geminiBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	status := &BatchStatus{BatchID: batchID}

	switch result.State {
	case "JOB_STATE_SUCCEEDED":
		status.State = "completed"
		if result.Dest != nil {
			for _, r := range result.Dest.InlineResponses {
				br := BatchResult{CustomID: r.Metadata.Key}
				if len(r.Response.Candidates) > 0 && len(r.Response.Candidates[0].Content.Parts) > 0 {
					br.Text = r.Response.Candidates[0].Content.Parts[0].Text
				}
				if r.Response.UsageMetadata != nil {
					br.InputTokens = r.Response.UsageMetadata.PromptTokenCount
					br.OutputTokens = r.Response.UsageMetadata.CandidatesTokenCount
				}
				status.Results = append(status.Results, br)
			}
		}

	case "JOB_STATE_FAILED", "JOB_STATE_CANCELLED", "JOB_STATE_EXPIRED":
		status.State = "failed"
		if result.Error != nil {
			status.Error = result.Error.Message
		} else {
			status.Error = "batch " + result.State
		}

	default: // JOB_STATE_PENDING, JOB_STATE_RUNNING
		status.State = "processing"
	}

	return status, nil
}

// CancelBatch cancels a Gemini batch operation
func (p *geminiLLMProvider) CancelBatch(ctx context.Context, batchID string) error {
	url := fmt.Sprintf("%s/v1beta/%s:cancel?key=%s", geminiAPIURL, batchID, p.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("gemini cancel batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gemini cancel error (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
