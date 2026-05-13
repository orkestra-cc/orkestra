package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// reasoningTagRe strips <think>...</think> blocks emitted by reasoning models
// (Qwen3, QwQ, DeepSeek-R1, etc.) when an OpenAI-compatible endpoint doesn't filter them.
var reasoningTagRe = regexp.MustCompile(`(?is)<think>.*?</think>`)

// AgentExecutor runs multiple sales agents in parallel with concurrency control
type AgentExecutor struct {
	maxConcurrency int
	maxTokens      int
	logger         *slog.Logger
}

// NewAgentExecutor creates a new parallel agent executor
func NewAgentExecutor(maxConcurrency, maxTokens int, logger *slog.Logger) *AgentExecutor {
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return &AgentExecutor{
		maxConcurrency: maxConcurrency,
		maxTokens:      maxTokens,
		logger:         logger.With(slog.String("component", "agent-executor")),
	}
}

// AgentDef defines an agent to execute: its name, weight, and system prompt
type AgentDef struct {
	Name   models.AgentName
	Weight float64
	Prompt string
}

// RunParallel executes all agents concurrently with a semaphore and returns results.
// Individual agent failures are captured in the result; other agents continue.
func (e *AgentExecutor) RunParallel(
	ctx context.Context,
	agents []AgentDef,
	input *models.AgentInput,
	llm iface.LLMProvider,
	progressFn func(models.AgentName, string, int), // optional: (agent, status, score)
) []*models.AgentResult {
	results := make([]*models.AgentResult, len(agents))
	sem := make(chan struct{}, e.maxConcurrency)

	g, ctx := errgroup.WithContext(ctx)

	for i, agent := range agents {
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			result := e.executeAgent(ctx, agent, input, llm)
			results[i] = result

			if progressFn != nil {
				status := "completed"
				if result.Error != "" {
					status = "failed"
				}
				progressFn(agent.Name, status, result.Score)
			}

			return nil // never cancel other agents
		})
	}

	g.Wait()
	return results
}

func (e *AgentExecutor) executeAgent(
	ctx context.Context,
	agent AgentDef,
	input *models.AgentInput,
	llm iface.LLMProvider,
) *models.AgentResult {
	start := time.Now()

	result := &models.AgentResult{
		AgentName: agent.Name,
	}

	// Build user message from scraped data
	userMessage := buildAgentUserMessage(input)

	// Execute LLM call with token tracking if available
	var text string
	var err error

	if usageProvider, ok := llm.(iface.LLMProviderWithUsage); ok {
		completionResult, callErr := usageProvider.CompleteWithUsage(ctx, userMessage, iface.CompletionOptions{
			SystemPrompt: agent.Prompt,
			Temperature:  0.3,
			MaxTokens:    e.maxTokens,
		})
		if callErr != nil {
			err = callErr
		} else {
			text = completionResult.Text
			result.InputTokens = completionResult.InputTokens
			result.OutputTokens = completionResult.OutputTokens
		}
	} else {
		text, err = llm.Complete(ctx, userMessage, iface.CompletionOptions{
			SystemPrompt: agent.Prompt,
			Temperature:  0.3,
			MaxTokens:    e.maxTokens,
		})
	}

	result.LatencyMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Error = err.Error()
		e.logger.Error("agent execution failed",
			slog.String("agent", string(agent.Name)),
			slog.String("error", err.Error()),
		)
		return result
	}

	rawLen := len(text)
	cleaned := strings.TrimSpace(reasoningTagRe.ReplaceAllString(text, ""))

	preview := cleaned
	if len(preview) > 500 {
		preview = preview[:500]
	}
	e.logger.Debug("agent raw response",
		slog.String("agent", string(agent.Name)),
		slog.Int("rawLen", rawLen),
		slog.Int("cleanedLen", len(cleaned)),
		slog.String("preview", preview),
	)

	if cleaned == "" {
		result.Error = fmt.Sprintf("empty LLM response (raw length %d, likely all reasoning tokens or model refused)", rawLen)
		e.logger.Error("agent returned empty response",
			slog.String("agent", string(agent.Name)),
			slog.Int("rawLen", rawLen),
		)
		return result
	}

	if json.Valid([]byte(cleaned)) {
		result.Findings = json.RawMessage(cleaned)
		var scoreHolder struct {
			Score int `json:"score"`
		}
		if json.Unmarshal([]byte(cleaned), &scoreHolder) == nil {
			result.Score = scoreHolder.Score
		}
	} else {
		wrapped, _ := json.Marshal(map[string]string{"response": cleaned})
		result.Findings = json.RawMessage(wrapped)
	}

	e.logger.Info("agent completed",
		slog.String("agent", string(agent.Name)),
		slog.Int("score", result.Score),
		slog.Int64("latencyMs", result.LatencyMs),
		slog.Int("inputTokens", result.InputTokens),
		slog.Int("outputTokens", result.OutputTokens),
	)

	return result
}

// BuildUserMessage constructs the user message from agent input (for batch submission)
func (e *AgentExecutor) BuildUserMessage(input *models.AgentInput) string {
	return buildAgentUserMessage(input)
}

func buildAgentUserMessage(input *models.AgentInput) string {
	var sb fmt.Stringer = &agentMessageBuilder{input: input}
	return sb.String()
}

type agentMessageBuilder struct {
	input *models.AgentInput
}

func (b *agentMessageBuilder) String() string {
	var sb fmt.Stringer = b
	_ = sb // prevent unused warning

	var buf []byte
	buf = append(buf, fmt.Sprintf("Analyze the company at: %s\n\n", b.input.CompanyURL)...)

	if b.input.ScrapedData != nil {
		d := b.input.ScrapedData
		if d.CompanyName != "" {
			buf = append(buf, fmt.Sprintf("Company Name: %s\n", d.CompanyName)...)
		}
		if d.Industry != "" {
			buf = append(buf, fmt.Sprintf("Industry: %s\n", d.Industry)...)
		}
		if d.Description != "" {
			buf = append(buf, fmt.Sprintf("Description: %s\n", d.Description)...)
		}
		if len(d.TechStack) > 0 {
			buf = append(buf, fmt.Sprintf("Tech Stack: %s\n", joinStrings(d.TechStack))...)
		}
		if len(d.TeamMembers) > 0 {
			buf = append(buf, fmt.Sprintf("Team Members: %s\n", joinStrings(d.TeamMembers))...)
		}
		if d.ContactInfo != "" {
			buf = append(buf, fmt.Sprintf("Contact Info: %s\n", d.ContactInfo)...)
		}
		if len(d.SocialLinks) > 0 {
			buf = append(buf, fmt.Sprintf("Social Links: %s\n", joinStrings(d.SocialLinks))...)
		}
		if d.AboutText != "" {
			buf = append(buf, fmt.Sprintf("\n--- About Page ---\n%s\n", truncate(d.AboutText, 3000))...)
		}
		if d.RawText != "" {
			buf = append(buf, fmt.Sprintf("\n--- Website Content ---\n%s\n", truncate(d.RawText, 8000))...)
		}
	}

	if b.input.RegistryData != nil {
		r := b.input.RegistryData
		buf = append(buf, "\n--- Business Registry ---\n"...)
		buf = append(buf, fmt.Sprintf("Name: %s\nTax Code: %s\nVAT: %s\nForm: %s\nAddress: %s %s %s\nATECO: %s (%s)\nEmployees: %s\nRevenue: %s\nFounded: %s\nStatus: %s\n",
			r.CompanyName, r.TaxCode, r.VATNumber, r.LegalForm,
			r.Address, r.City, r.Province,
			r.AtecoCode, r.AtecoDesc,
			r.EmployeeRange, r.RevenueRange, r.FoundedYear, r.Status,
		)...)
	}

	return string(buf)
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
