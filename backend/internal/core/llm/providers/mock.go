package providers

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Mock is the deterministic adapter for tests and non-production stacks:
// it echoes the last user message, calls the first offered tool when the
// prompt contains "tool", and embeds with a hash. Production never selects
// it (the catalog refuses the provider kind in production-like
// environments).
type Mock struct{}

// NewMock builds the mock adapter; the credential and client are unused.
func NewMock(Credential, *http.Client) (Provider, error) { return &Mock{}, nil }

func (Mock) Name() string { return "mock" }

func (Mock) Capabilities() iface.LLMCapabilities {
	return iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: true, Dimensions: mockDimensions}
}

const mockDimensions = 8

func (Mock) Chat(ctx context.Context, call ChatCall, sink iface.ChatSink) (*ChatOutcome, error) {
	var last string
	for i := len(call.Request.Messages) - 1; i >= 0; i-- {
		if call.Request.Messages[i].Role == "user" {
			last = call.Request.Messages[i].Content
			break
		}
	}
	usage := iface.LLMUsage{InputTokens: EstimateTokens(call.Request)}
	if len(call.Request.Tools) > 0 && strings.Contains(strings.ToLower(last), "tool") {
		args, _ := json.Marshal(map[string]any{"query": last})
		tc := iface.ChatToolCall{ID: "call_mock_1", Name: call.Request.Tools[0].Name, Arguments: string(args)}
		if err := sink(iface.ChatEvent{ToolCall: &tc}); err != nil {
			return nil, err
		}
		usage.OutputTokens = 4
		if err := sink(iface.ChatEvent{Done: true, Usage: &usage}); err != nil {
			return nil, err
		}
		return &ChatOutcome{Usage: usage, FinishReason: "tool_calls"}, nil
	}
	text := "mock: " + last
	if call.Request.ResponseFormat != nil {
		b, _ := json.Marshal(map[string]any{"echo": last})
		text = string(b)
	}
	for _, chunk := range splitChunks(text, 4) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := sink(iface.ChatEvent{Delta: chunk}); err != nil {
			return nil, err
		}
	}
	usage.OutputTokens = (len(text) + 2) / 3
	if err := sink(iface.ChatEvent{Done: true, Usage: &usage}); err != nil {
		return nil, err
	}
	return &ChatOutcome{Usage: usage, FinishReason: "stop"}, nil
}

func (Mock) Embed(_ context.Context, call EmbedCall) (*EmbedOutcome, error) {
	out := &EmbedOutcome{}
	for _, in := range call.Inputs {
		sum := sha256.Sum256([]byte(in))
		vec := make([]float32, mockDimensions)
		for i := range vec {
			vec[i] = float32(binary.LittleEndian.Uint16(sum[i*2:])) / 65535
		}
		out.Vectors = append(out.Vectors, vec)
		out.Usage.InputTokens += (len(in) + 2) / 3
	}
	return out, nil
}

func (Mock) ListModels(context.Context) ([]ModelDescriptor, error) {
	return []ModelDescriptor{{ID: "mock-1", DisplayName: "Mock chat"}, {ID: "mock-embed", DisplayName: "Mock embeddings"}}, nil
}

func (Mock) Health(context.Context) error { return nil }

// splitChunks cuts s into pieces of at most n bytes on rune boundaries.
func splitChunks(s string, n int) []string {
	var out []string
	for len(s) > n {
		cut := n
		for cut > 0 && !isRuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = n
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	return append(out, s)
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// EstimateTokens is the module-wide conservative estimate: ceil(bytes/3)
// over instructions, messages and tool schemas. Shared with the budget
// reservation (services/estimate.go wraps it).
func EstimateTokens(req iface.ChatRequest) int {
	n := len(req.Instructions)
	for _, m := range req.Messages {
		n += len(m.Content) + len(m.Name) + len(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(tc.Arguments)
		}
	}
	for _, t := range req.Tools {
		b, _ := json.Marshal(t)
		n += len(b)
	}
	if req.ResponseFormat != nil {
		b, _ := json.Marshal(req.ResponseFormat)
		n += len(b)
	}
	return (n + 2) / 3
}
