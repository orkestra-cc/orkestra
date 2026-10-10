// Package providers defines the internal adapter contract the llm gateway
// routes to, and the registry that maps a provider kind to its factory.
// PR 1 ships only the contract and an empty registry; the adapters land in
// PR 2.
package providers

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Provider is the internal adapter contract. PR 2 ships the six
// implementations; this PR ships only the registry so the gateway can be
// wired and fail honestly with "provider unavailable".
type Provider interface {
	Name() string
	Capabilities() iface.LLMCapabilities
	Chat(ctx context.Context, call ChatCall, sink iface.ChatSink) (*ChatOutcome, error)
	Embed(ctx context.Context, call EmbedCall) (*EmbedOutcome, error)
	ListModels(ctx context.Context) ([]ModelDescriptor, error)
	Health(ctx context.Context) error
}

// Credential is the decrypted material an adapter needs for one call. It
// lives in memory for the duration of the call and is never logged.
type Credential struct {
	Kind    string // iface.LLMCredentialKindOrg | iface.LLMCredentialKindUserAccount
	Secret  string // API key or bearer token; "" for ollama/mock
	BaseURL string
}

// ChatCall is one resolved chat invocation.
type ChatCall struct {
	ModelID  string
	Request  iface.ChatRequest
	Defaults ChatDefaults
}

// ChatDefaults are the model-level defaults applied when the request does
// not set the option.
type ChatDefaults struct {
	Temperature     *float64
	MaxOutputTokens *int
	Effort          string
}

// ChatOutcome is what an adapter reports after streaming finished.
type ChatOutcome struct {
	Usage        iface.LLMUsage
	FinishReason string
}

// EmbedCall is one resolved embedding invocation.
type EmbedCall struct {
	ModelID string
	Inputs  []string
}

// EmbedOutcome carries one vector per input, in order.
type EmbedOutcome struct {
	Vectors [][]float32
	Usage   iface.LLMUsage
}

// ModelDescriptor is a model id the provider reports it serves.
type ModelDescriptor struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
}

// Factory builds a Provider for one call from decrypted credential material.
type Factory func(cred Credential, client *http.Client) (Provider, error)

// ErrUnknownProvider: no adapter is registered for the requested kind.
var ErrUnknownProvider = errors.New("llm providers: no adapter registered for this provider")

// Registry maps a provider kind to its Factory. Safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry { return &Registry{factories: map[string]Factory{}} }

func (r *Registry) Register(kind string, f Factory) {
	r.mu.Lock()
	r.factories[kind] = f
	r.mu.Unlock()
}

func (r *Registry) Build(kind string, cred Credential, client *http.Client) (Provider, error) {
	r.mu.RLock()
	f, ok := r.factories[kind]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownProvider
	}
	return f(cred, client)
}

// Kinds lists the registered provider kinds (unordered).
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for k := range r.factories {
		out = append(out, k)
	}
	return out
}
