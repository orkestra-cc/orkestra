package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Ollama talks to the native API: POST /api/chat (ndjson stream, tools),
// POST /api/embed, GET /api/tags. No API key is required; when one is set
// it is sent as a Bearer for proxied deployments.
type Ollama struct {
	base   string
	secret string
	client *http.Client
}

const (
	ollamaMaxLine     = 1 << 20  // one ndjson line
	ollamaMaxEmbed    = 32 << 20 // /api/embed response
	ollamaMaxTags     = 4 << 20  // /api/tags response
	ollamaMaxToolArgs = 1 << 20  // cumulative tool-call argument bytes per call
)

// NewOllama builds the adapter. The client MUST be the SSRF-safe one from
// NewHTTPClient; a nil client is refused rather than silently replaced by
// http.DefaultClient.
func NewOllama(cred Credential, client *http.Client) (Provider, error) {
	if client == nil {
		return nil, errors.New("llm providers: ollama requires an http client")
	}
	if strings.TrimSpace(cred.BaseURL) == "" {
		return nil, fmt.Errorf("%w: ollama requires a base URL", iface.ErrLLMInvalidRequest)
	}
	return &Ollama{base: strings.TrimRight(cred.BaseURL, "/"), secret: cred.Secret, client: client}, nil
}

func (*Ollama) Name() string { return "ollama" }

func (*Ollama) Capabilities() iface.LLMCapabilities {
	return iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: true}
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolName  string           `json:"tool_name,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func (o *Ollama) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.base+path, rd)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base URL", iface.ErrLLMInvalidRequest)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if o.secret != "" {
		req.Header.Set("Authorization", "Bearer "+o.secret)
	}
	return req, nil
}

// send executes req and classifies an HTTP error status. The error body is
// reduced to allowlisted identifiers by ReadErrorInfo and never surfaces.
func (o *Ollama) send(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, Classify(ReadErrorInfo(resp))
	}
	return resp, nil
}

func (o *Ollama) post(ctx context.Context, path string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: request is not serialisable", iface.ErrLLMInvalidRequest)
	}
	req, err := o.newRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return o.send(ctx, req)
}

func (o *Ollama) Chat(ctx context.Context, call ChatCall, sink iface.ChatSink) (*ChatOutcome, error) {
	req := call.Request
	if req.Options.Effort != "" || call.Defaults.Effort != "" {
		return nil, fmt.Errorf("%w: ollama does not support reasoning effort", iface.ErrLLMCapabilityMismatch)
	}
	msgs := make([]ollamaMessage, 0, len(req.Messages)+1)
	if req.Instructions != "" {
		msgs = append(msgs, ollamaMessage{Role: "system", Content: req.Instructions})
	}
	for _, m := range req.Messages {
		role := m.Role
		if role == "developer" {
			role = "system"
		}
		om := ollamaMessage{Role: role, Content: m.Content}
		if role == "tool" {
			om.ToolName = m.Name
		}
		for _, tc := range m.ToolCalls {
			args, err := normalizeArgs([]byte(tc.Arguments))
			if err != nil {
				return nil, fmt.Errorf("%w: tool call arguments are not valid JSON", iface.ErrLLMInvalidRequest)
			}
			var otc ollamaToolCall
			otc.ID = tc.ID
			otc.Function.Name = tc.Name
			otc.Function.Arguments = json.RawMessage(args)
			om.ToolCalls = append(om.ToolCalls, otc)
		}
		msgs = append(msgs, om)
	}
	payload := map[string]any{"model": call.ModelID, "messages": msgs, "stream": true}
	options := map[string]any{}
	if t := firstFloat(req.Options.Temperature, call.Defaults.Temperature); t != nil {
		options["temperature"] = *t
	}
	if n := firstInt(req.Options.MaxOutputTokens, call.Defaults.MaxOutputTokens); n != nil {
		options["num_predict"] = *n
	}
	if len(options) > 0 {
		payload["options"] = options
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": t.InputSchema,
			}})
		}
		payload["tools"] = tools
	}
	if req.ResponseFormat != nil {
		payload["format"] = req.ResponseFormat.Schema // ollama accepts a JSON schema as format
	}
	resp, err := o.post(ctx, "/api/chat", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	outcome := &ChatOutcome{FinishReason: "stop"}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), ollamaMaxLine)
	toolCalls, argBytes := 0, 0
	finished := false
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var line struct {
			Message         ollamaMessage `json:"message"`
			Done            bool          `json:"done"`
			DoneReason      string        `json:"done_reason"`
			PromptEvalCount int           `json:"prompt_eval_count"`
			EvalCount       int           `json:"eval_count"`
			Error           string        `json:"error"`
		}
		if err := json.Unmarshal(raw, &line); err != nil {
			return nil, malformed("stream")
		}
		if line.Error != "" {
			return nil, fmt.Errorf("%w: upstream reported an error", iface.ErrLLMProviderUnavailable)
		}
		if line.Message.Content != "" {
			if err := sink(iface.ChatEvent{Delta: line.Message.Content}); err != nil {
				return nil, err
			}
		}
		for _, tc := range line.Message.ToolCalls {
			args, err := normalizeArgs(tc.Function.Arguments)
			if err != nil || tc.Function.Name == "" {
				return nil, malformed("tool call")
			}
			if argBytes += len(args); argBytes > ollamaMaxToolArgs {
				return nil, malformed("tool call")
			}
			toolCalls++
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", toolCalls)
			}
			emitted := iface.ChatToolCall{ID: id, Name: tc.Function.Name, Arguments: args}
			if err := sink(iface.ChatEvent{ToolCall: &emitted}); err != nil {
				return nil, err
			}
		}
		if line.Done {
			finished = true
			outcome.Usage = iface.LLMUsage{InputTokens: line.PromptEvalCount, OutputTokens: line.EvalCount}
			switch {
			case toolCalls > 0:
				outcome.FinishReason = "tool_calls"
			case line.DoneReason == "length":
				outcome.FinishReason = "length"
			}
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, transportErr(ctx, err)
	}
	if !finished {
		return nil, fmt.Errorf("%w: stream ended before completion", iface.ErrLLMProviderUnavailable)
	}
	if err := sink(iface.ChatEvent{Done: true, Usage: &outcome.Usage}); err != nil {
		return nil, err
	}
	return outcome, nil
}

func (o *Ollama) Embed(ctx context.Context, call EmbedCall) (*EmbedOutcome, error) {
	resp, err := o.post(ctx, "/api/embed", map[string]any{"model": call.ModelID, "input": call.Inputs})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ReadAllBounded(resp.Body, ollamaMaxEmbed)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	var out struct {
		Embeddings      [][]float32 `json:"embeddings"`
		PromptEvalCount int         `json:"prompt_eval_count"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Embeddings) != len(call.Inputs) {
		return nil, malformed("embeddings response")
	}
	return &EmbedOutcome{Vectors: out.Embeddings, Usage: iface.LLMUsage{InputTokens: out.PromptEvalCount}}, nil
}

func (o *Ollama) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
	req, err := o.newRequest(ctx, http.MethodGet, "/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.send(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ReadAllBounded(resp.Body, ollamaMaxTags)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, malformed("tags response")
	}
	res := make([]ModelDescriptor, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name != "" {
			res = append(res, ModelDescriptor{ID: m.Name, DisplayName: m.Name})
		}
	}
	return res, nil
}

func (o *Ollama) Health(ctx context.Context) error {
	_, err := o.ListModels(ctx)
	return err
}
