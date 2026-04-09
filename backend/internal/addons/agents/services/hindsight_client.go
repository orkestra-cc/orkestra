package services

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	hindsight "github.com/vectorize-io/hindsight/hindsight-clients/go"
)

// HindsightClient wraps the Hindsight Go SDK with a clean, mockable interface.
type HindsightClient interface {
	// Bank management
	CreateOrUpdateBank(ctx context.Context, bankID, name, mission string, disposition *DispositionConfig) error
	DeleteBank(ctx context.Context, bankID string) error
	GetBankInfo(ctx context.Context, bankID string) (*BankInfo, error)

	// Memory operations
	Retain(ctx context.Context, bankID string, items []MemoryEntry, tags []string) error
	Recall(ctx context.Context, bankID, query string, maxTokens int32, tags []string) (*RecallResult, error)
	Reflect(ctx context.Context, bankID, query, extraContext string, maxTokens int32, tags []string) (*ReflectResult, error)

	// Health
	HealthCheck(ctx context.Context) error
}

// DispositionConfig mirrors Hindsight's disposition traits (1-5 scale)
type DispositionConfig struct {
	Skepticism int32
	Literalism int32
	Empathy    int32
}

// BankInfo holds summary info about a Hindsight memory bank
type BankInfo struct {
	BankID  string `json:"bankId"`
	Name    string `json:"name"`
	Mission string `json:"mission"`
}

// MemoryEntry represents a single item to retain
type MemoryEntry struct {
	Content  string
	Context  string
	Tags     []string
	Metadata map[string]string
}

// RecallResult holds memories retrieved by a recall query
type RecallResult struct {
	Results []RecallMemory
}

// RecallMemory is a single recalled memory
type RecallMemory struct {
	ID       string
	Text     string
	Type     string
	Tags     []string
	Metadata map[string]string
}

// ReflectResult holds the synthesized answer from Hindsight reflect
type ReflectResult struct {
	Text         string
	InputTokens  int32
	OutputTokens int32
	TotalTokens  int32
}

// TokenUsageInfo holds token counts from a Hindsight operation
type TokenUsageInfo struct {
	InputTokens  int32
	OutputTokens int32
	TotalTokens  int32
}

type hindsightClient struct {
	api    *hindsight.APIClient
	logger *slog.Logger
}

// NewHindsightClient creates a new HindsightClient connected to the given base URL.
func NewHindsightClient(baseURL string, logger *slog.Logger) HindsightClient {
	cfg := hindsight.NewConfiguration()
	cfg.Servers = hindsight.ServerConfigurations{
		{URL: baseURL},
	}
	cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}

	return &hindsightClient{
		api:    hindsight.NewAPIClient(cfg),
		logger: logger.With(slog.String("module", "hindsight-client")),
	}
}

func (c *hindsightClient) CreateOrUpdateBank(ctx context.Context, bankID, name, mission string, disposition *DispositionConfig) error {
	req := hindsight.NewCreateBankRequest()
	req.Name = *hindsight.NewNullableString(&name)
	req.Mission = *hindsight.NewNullableString(&mission)

	if disposition != nil {
		req.DispositionSkepticism = *hindsight.NewNullableInt32(&disposition.Skepticism)
		req.DispositionLiteralism = *hindsight.NewNullableInt32(&disposition.Literalism)
		req.DispositionEmpathy = *hindsight.NewNullableInt32(&disposition.Empathy)
	}

	_, _, err := c.api.BanksAPI.CreateOrUpdateBank(ctx, bankID).CreateBankRequest(*req).Execute()
	if err != nil {
		return fmt.Errorf("create/update bank %s: %w", bankID, err)
	}

	c.logger.Info("Hindsight bank created/updated", slog.String("bankId", bankID))
	return nil
}

func (c *hindsightClient) DeleteBank(ctx context.Context, bankID string) error {
	_, _, err := c.api.BanksAPI.DeleteBank(ctx, bankID).Execute()
	if err != nil {
		return fmt.Errorf("delete bank %s: %w", bankID, err)
	}
	c.logger.Info("Hindsight bank deleted", slog.String("bankId", bankID))
	return nil
}

func (c *hindsightClient) GetBankInfo(ctx context.Context, bankID string) (*BankInfo, error) {
	resp, _, err := c.api.BanksAPI.GetBankProfile(ctx, bankID).Execute()
	if err != nil {
		return nil, fmt.Errorf("get bank %s: %w", bankID, err)
	}
	return &BankInfo{
		BankID:  resp.BankId,
		Name:    resp.Name,
		Mission: resp.Mission,
	}, nil
}

func (c *hindsightClient) Retain(ctx context.Context, bankID string, items []MemoryEntry, tags []string) error {
	sdkItems := make([]hindsight.MemoryItem, 0, len(items))
	for _, item := range items {
		mi := hindsight.NewMemoryItem(item.Content)
		if item.Context != "" {
			mi.Context = *hindsight.NewNullableString(&item.Context)
		}
		if len(item.Tags) > 0 {
			mi.Tags = item.Tags
		}
		if len(item.Metadata) > 0 {
			mi.Metadata = item.Metadata
		}
		sdkItems = append(sdkItems, *mi)
	}

	req := hindsight.NewRetainRequest(sdkItems)
	async := true
	req.Async = &async
	if len(tags) > 0 {
		req.DocumentTags = tags
	}

	_, _, err := c.api.MemoryAPI.RetainMemories(ctx, bankID).RetainRequest(*req).Execute()
	if err != nil {
		return fmt.Errorf("retain to bank %s: %w", bankID, err)
	}
	return nil
}

func (c *hindsightClient) Recall(ctx context.Context, bankID, query string, maxTokens int32, tags []string) (*RecallResult, error) {
	req := hindsight.NewRecallRequest(query)
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}
	if len(tags) > 0 {
		req.Tags = tags
	}

	resp, _, err := c.api.MemoryAPI.RecallMemories(ctx, bankID).RecallRequest(*req).Execute()
	if err != nil {
		return nil, fmt.Errorf("recall from bank %s: %w", bankID, err)
	}

	result := &RecallResult{}
	for _, r := range resp.Results {
		mem := RecallMemory{
			ID:       r.Id,
			Text:     r.Text,
			Tags:     r.Tags,
			Metadata: r.Metadata,
		}
		if r.Type.IsSet() {
			mem.Type = *r.Type.Get()
		}
		result.Results = append(result.Results, mem)
	}
	return result, nil
}

func (c *hindsightClient) Reflect(ctx context.Context, bankID, query, extraContext string, maxTokens int32, tags []string) (*ReflectResult, error) {
	// Inline context into query (context param is deprecated in Hindsight v0.4+)
	fullQuery := query
	if extraContext != "" {
		fullQuery = query + "\n\n" + extraContext
	}

	req := hindsight.NewReflectRequest(fullQuery)

	// Use low budget to minimize LLM iterations and token usage
	budget := hindsight.LOW
	req.Budget = &budget

	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}
	if len(tags) > 0 {
		req.Tags = tags
	}

	resp, _, err := c.api.MemoryAPI.Reflect(ctx, bankID).ReflectRequest(*req).Execute()
	if err != nil {
		return nil, fmt.Errorf("reflect from bank %s: %w", bankID, err)
	}

	result := &ReflectResult{Text: resp.Text}
	if usage, ok := resp.GetUsageOk(); ok && usage != nil {
		if v := usage.InputTokens; v != nil {
			result.InputTokens = *v
		}
		if v := usage.OutputTokens; v != nil {
			result.OutputTokens = *v
		}
		if v := usage.TotalTokens; v != nil {
			result.TotalTokens = *v
		}
	}
	return result, nil
}

func (c *hindsightClient) HealthCheck(ctx context.Context) error {
	_, _, err := c.api.MonitoringAPI.HealthEndpointHealthGet(ctx).Execute()
	if err != nil {
		return fmt.Errorf("hindsight health check failed: %w", err)
	}
	return nil
}
