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

const geminiAPIURL = "https://generativelanguage.googleapis.com"

// geminiEmbeddingProvider implements EmbeddingProvider for Google Gemini
type geminiEmbeddingProvider struct {
	apiKey     string
	modelName  string
	dimensions int
	client     *http.Client
}

// geminiLLMProvider implements LLMProvider for Google Gemini
type geminiLLMProvider struct {
	apiKey    string
	modelName string
	client    *http.Client
}

// NewGeminiEmbeddingProvider creates an EmbeddingProvider backed by Google Gemini
func NewGeminiEmbeddingProvider(apiKey, modelName string, dimensions int) EmbeddingProvider {
	return &geminiEmbeddingProvider{
		apiKey:     apiKey,
		modelName:  modelName,
		dimensions: dimensions,
		client:     &http.Client{Timeout: 120 * time.Second},
	}
}

// NewGeminiLLMProvider creates an LLMProvider backed by Google Gemini
func NewGeminiLLMProvider(apiKey, modelName string) LLMProvider {
	return &geminiLLMProvider{
		apiKey:    apiKey,
		modelName: modelName,
		client:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// --- Embedding ---

func (p *geminiEmbeddingProvider) ModelName() string { return p.modelName }
func (p *geminiEmbeddingProvider) Dimensions() int   { return p.dimensions }

type geminiEmbedRequest struct {
	Model   string `json:"model"`
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}

func (p *geminiEmbeddingProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody := geminiEmbedRequest{Model: "models/" + p.modelName}
	reqBody.Content.Parts = []struct {
		Text string `json:"text"`
	}{{Text: text}}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/v1beta/models/%s:embedContent?key=%s", geminiAPIURL, p.modelName, p.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embed call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini embed error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result geminiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("gemini embed decode: %w", err)
	}
	return result.Embedding.Values, nil
}

func (p *geminiEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, text := range texts {
		embedding, err := p.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("batch embed [%d]: %w", i, err)
		}
		results[i] = embedding
	}
	return results, nil
}

// --- LLM ---

func (p *geminiLLMProvider) ModelName() string { return p.modelName }

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerateRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type geminiCandidate struct {
	Content struct {
		Parts []geminiPart `json:"parts"`
	} `json:"content"`
}

type geminiGenerateResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

func (p *geminiLLMProvider) Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error) {
	reqBody := geminiGenerateRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: prompt}}},
		},
	}

	if opts.SystemPrompt != "" {
		reqBody.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: opts.SystemPrompt}},
		}
	}

	genConfig := &geminiGenerationConfig{}
	hasConfig := false
	if opts.Temperature > 0 {
		genConfig.Temperature = &opts.Temperature
		hasConfig = true
	}
	if opts.MaxTokens > 0 {
		genConfig.MaxOutputTokens = &opts.MaxTokens
		hasConfig = true
	}
	if hasConfig {
		reqBody.GenerationConfig = genConfig
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", geminiAPIURL, p.modelName, p.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gemini generate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini generate call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini generate error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result geminiGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("gemini generate decode: %w", err)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini generate: empty response")
	}
	return result.Candidates[0].Content.Parts[0].Text, nil
}

func (p *geminiLLMProvider) StreamComplete(ctx context.Context, prompt string, opts CompletionOptions) (<-chan StreamChunk, error) {
	reqBody := geminiGenerateRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: prompt}}},
		},
	}

	if opts.SystemPrompt != "" {
		reqBody.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: opts.SystemPrompt}},
		}
	}

	genConfig := &geminiGenerationConfig{}
	hasConfig := false
	if opts.Temperature > 0 {
		genConfig.Temperature = &opts.Temperature
		hasConfig = true
	}
	if opts.MaxTokens > 0 {
		genConfig.MaxOutputTokens = &opts.MaxTokens
		hasConfig = true
	}
	if hasConfig {
		reqBody.GenerationConfig = genConfig
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", geminiAPIURL, p.modelName, p.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini stream call: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini stream error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		// Gemini SSE stream format: "data: {json}\n\n"
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var result geminiGenerateResponse
			if err := json.Unmarshal([]byte(data), &result); err != nil {
				continue // skip malformed chunks
			}
			if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
				ch <- StreamChunk{Text: result.Candidates[0].Content.Parts[0].Text}
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("gemini stream read: %w", err)}
			return
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// fetchGeminiModels lists models available via the Gemini API.
// The returned RemoteModel.OwnedBy encodes the supported methods as a comma-separated
// string (e.g. "google | embedContent") so the frontend can filter by capability.
func fetchGeminiModels(ctx context.Context, apiKey string) ([]RemoteModel, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("%s/v1beta/models?key=%s", geminiAPIURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini API not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("Gemini decode: %w", err)
	}

	models := make([]RemoteModel, 0, len(result.Models))
	for _, m := range result.Models {
		// name comes as "models/gemini-2.0-flash", strip the prefix
		id := m.Name
		if strings.HasPrefix(id, "models/") {
			id = id[7:]
		}

		// Encode capabilities so frontend can filter by model type
		capabilities := strings.Join(m.SupportedGenerationMethods, ",")

		models = append(models, RemoteModel{ID: id, OwnedBy: "google", Capabilities: capabilities})
	}
	return models, nil
}

// TestGeminiConnectivity verifies the API key is valid by listing models
func TestGeminiConnectivity(ctx context.Context, apiKey string) error {
	_, err := fetchGeminiModels(ctx, apiKey)
	return err
}
