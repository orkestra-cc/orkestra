// backend/pkg/sdk/iface/llm.go
package iface

import (
	"context"
	"errors"
	"time"
)

// LLMGateway is the cross-module contract of the core `llm` module: a
// consumer (a fork's addon) asks for a chat completion or an embedding by
// *purpose* and the gateway picks a model the caller may use, applies the
// org budget, executes the provider call and records usage. Consumers
// never see credentials or provider SDKs. Resolved with
// module.GetTyped[iface.LLMGateway](reg, module.ServiceLLMGateway).
type LLMGateway interface {
	// Chat streams a completion through sink and returns the final usage.
	// Errors are classified with the ErrLLM* sentinels below (errors.Is).
	Chat(ctx context.Context, req ChatRequest, sink ChatSink) (*ChatResult, error)
	// Embed returns one vector per input, in order.
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error)
	// Resolve returns the model Chat would pick for purpose without calling it.
	Resolve(ctx context.Context, purpose string) (*LLMModelInfo, error)
	// ListUsable lists every model the current user may use in the current org.
	ListUsable(ctx context.Context) ([]LLMModelInfo, error)
}

// LLMCredentialKind* name who pays for a call: the org's API key or the
// linked ChatGPT account of the user in context.
const (
	LLMCredentialKindOrg         = "org"
	LLMCredentialKindUserAccount = "user_account"
)

// LLMCapabilities is the operator-declared capability set of a model,
// intersected at runtime with what the provider adapter supports.
type LLMCapabilities struct {
	Chat             bool `json:"chat"`
	Streaming        bool `json:"streaming"`
	Tools            bool `json:"tools"`
	StructuredOutput bool `json:"structuredOutput"`
	Embeddings       bool `json:"embeddings"`
	Dimensions       int  `json:"dimensions,omitempty"`
}

// LLMModelInfo is the consumer-visible projection of a configured model.
// It never carries credentials.
type LLMModelInfo struct {
	UUID           string          `json:"uuid"`
	Name           string          `json:"name"`
	Provider       string          `json:"provider"`
	ModelID        string          `json:"modelId"`
	Capabilities   LLMCapabilities `json:"capabilities"`
	CredentialKind string          `json:"credentialKind"`
	Purposes       []string        `json:"purposes"`
}

// ChatMessage roles: "developer", "user", "assistant", "tool". There is no
// "system" role in this contract — put system text in ChatRequest.Instructions
// and the adapter maps it to what the provider supports.
type ChatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"toolCallId,omitempty"` // role=tool: the call being answered
	Name       string         `json:"name,omitempty"`       // role=tool: tool name
	ToolCalls  []ChatToolCall `json:"toolCalls,omitempty"`  // role=assistant: calls it emitted
}

type ChatToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON text, never parsed by the gateway
}

type ChatToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type JSONSchemaFormat struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict,omitempty"`
}

type ChatOptions struct {
	Temperature     *float64      `json:"temperature,omitempty"`
	MaxOutputTokens *int          `json:"maxOutputTokens,omitempty"`
	Effort          string        `json:"effort,omitempty"` // "", low, medium, high
	Timeout         time.Duration `json:"-"`                // consumer may only lower the module deadline
}

type ChatRequest struct {
	Purpose        string            `json:"purpose"`   // routing key; "" means "default"
	Caller         string            `json:"caller"`    // calling module name, required
	ModelUUID      string            `json:"modelUuid"` // optional pin chosen by the user
	Instructions   string            `json:"instructions,omitempty"`
	Messages       []ChatMessage     `json:"messages"`
	Tools          []ChatToolSpec    `json:"tools,omitempty"`
	ResponseFormat *JSONSchemaFormat `json:"responseFormat,omitempty"`
	Options        ChatOptions       `json:"options"`
}

// EffectivePurpose maps the empty purpose to "default".
func (r ChatRequest) EffectivePurpose() string {
	if r.Purpose == "" {
		return "default"
	}
	return r.Purpose
}

type ChatEvent struct {
	Delta    string        `json:"delta,omitempty"`
	ToolCall *ChatToolCall `json:"toolCall,omitempty"`
	Done     bool          `json:"done,omitempty"`
	Usage    *LLMUsage     `json:"usage,omitempty"`
}

// ChatSink receives streamed events. Returning an error aborts the call.
type ChatSink func(ChatEvent) error

type ChatResult struct {
	Model        LLMModelInfo `json:"model"`
	Usage        LLMUsage     `json:"usage"`
	FinishReason string       `json:"finishReason"` // stop | tool_calls | length | content_filter | other
	BillingScope string       `json:"billingScope"` // LLMCredentialKindOrg | LLMCredentialKindUserAccount
	Fallback     bool         `json:"fallback"`
}

type EmbedRequest struct {
	Purpose string   `json:"purpose"`
	Caller  string   `json:"caller"`
	Inputs  []string `json:"inputs"`
}

func (r EmbedRequest) EffectivePurpose() string {
	if r.Purpose == "" {
		return "default"
	}
	return r.Purpose
}

type EmbedResult struct {
	Vectors    [][]float32  `json:"vectors"`
	Dimensions int          `json:"dimensions"`
	Model      LLMModelInfo `json:"model"`
	Usage      LLMUsage     `json:"usage"`
}

type LLMUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// Sentinels. Consumers classify with errors.Is and map to their own codes.
var (
	ErrLLMNotConfigured       = errors.New("llm: no active model is configured for this organization")
	ErrLLMNoEligibleModel     = errors.New("llm: no model is eligible for this request")
	ErrLLMModelAccessDenied   = errors.New("llm: the pinned model is not usable by this caller")
	ErrLLMUserAccountRequired = errors.New("llm: the selected model requires a linked user account")
	ErrLLMPlanUsageDisabled   = errors.New("llm: the linked account did not grant plan usage")
	ErrLLMUserAccountReauth   = errors.New("llm: the linked account must be re-authorized")
	ErrLLMBudgetExceeded      = errors.New("llm: the organization budget is exhausted")
	ErrLLMQuotaExhausted      = errors.New("llm: the provider or plan quota is exhausted")
	ErrLLMCapabilityMismatch  = errors.New("llm: the request exceeds the model capabilities")
	ErrLLMPartialOutput       = errors.New("llm: the provider failed after partial output")
	ErrLLMProviderUnavailable = errors.New("llm: the provider is unavailable")
	ErrLLMInvalidRequest      = errors.New("llm: the request is malformed")
)
