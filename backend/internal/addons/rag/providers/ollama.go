package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ollamaProvider implements both EmbeddingProvider and LLMProvider for Ollama
type ollamaProvider struct {
	baseURL    string
	modelName  string
	dimensions int
	client     *http.Client
}

// NewOllamaEmbeddingProvider creates an EmbeddingProvider backed by Ollama
func NewOllamaEmbeddingProvider(baseURL, modelName string, dimensions int) EmbeddingProvider {
	return &ollamaProvider{
		baseURL:    baseURL,
		modelName:  modelName,
		dimensions: dimensions,
		client:     &http.Client{Timeout: 120 * time.Second},
	}
}

// NewOllamaLLMProvider creates an LLMProvider backed by Ollama
func NewOllamaLLMProvider(baseURL, modelName string) LLMProvider {
	return &ollamaProvider{
		baseURL:   baseURL,
		modelName: modelName,
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *ollamaProvider) ModelName() string { return p.modelName }
func (p *ollamaProvider) Dimensions() int   { return p.dimensions }

// --- Embedding ---

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

func (p *ollamaProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(ollamaEmbedRequest{Model: p.modelName, Prompt: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	return result.Embedding, nil
}

func (p *ollamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
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

type ollamaGenerateRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	System  string `json:"system,omitempty"`
	Stream  bool   `json:"stream"`
	Options struct {
		Temperature float64 `json:"temperature,omitempty"`
		NumPredict  int     `json:"num_predict,omitempty"`
	} `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func (p *ollamaProvider) Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error) {
	reqBody := ollamaGenerateRequest{
		Model:  p.modelName,
		Prompt: prompt,
		System: opts.SystemPrompt,
		Stream: false,
	}
	if opts.Temperature > 0 {
		reqBody.Options.Temperature = opts.Temperature
	}
	if opts.MaxTokens > 0 {
		reqBody.Options.NumPredict = opts.MaxTokens
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama generate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama generate error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ollama generate decode: %w", err)
	}
	return result.Response, nil
}

func (p *ollamaProvider) StreamComplete(ctx context.Context, prompt string, opts CompletionOptions) (<-chan StreamChunk, error) {
	reqBody := ollamaGenerateRequest{
		Model:  p.modelName,
		Prompt: prompt,
		System: opts.SystemPrompt,
		Stream: true,
	}
	if opts.Temperature > 0 {
		reqBody.Options.Temperature = opts.Temperature
	}
	if opts.MaxTokens > 0 {
		reqBody.Options.NumPredict = opts.MaxTokens
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama stream call: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama stream error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var chunk ollamaGenerateResponse
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				ch <- StreamChunk{Error: err}
				return
			}
			ch <- StreamChunk{Text: chunk.Response, Done: chunk.Done}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err}
		}
	}()

	return ch, nil
}

// fetchOllamaModels lists models available on an Ollama instance
func fetchOllamaModels(ctx context.Context, baseURL string) ([]RemoteModel, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Ollama not reachable at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}

	models := make([]RemoteModel, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = RemoteModel{ID: m.Name, OwnedBy: "ollama"}
	}
	return models, nil
}

// TestOllamaConnectivity checks if Ollama is reachable and the model exists
func TestOllamaConnectivity(ctx context.Context, baseURL, modelName string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	// Check Ollama is up
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("request creation failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama not reachable at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	// Check model exists
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return fmt.Errorf("failed to parse Ollama response: %w", err)
	}

	for _, m := range tags.Models {
		if m.Name == modelName || m.Name == modelName+":latest" {
			return nil
		}
	}
	return fmt.Errorf("model '%s' not found in Ollama (available: %d models)", modelName, len(tags.Models))
}
