package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// OpenAICompatible speaks the Chat Completions wire format (SSE stream,
// /embeddings, /models) of self-hosted servers such as vLLM, llama.cpp or
// LiteLLM. It is hand-written over the SSRF-safe client rather than built on
// the OpenAI SDK, because compatible servers deviate from the vendor's
// schema in ways the SDK's strict decoders reject.
type OpenAICompatible struct {
	base   string
	secret string
	client *http.Client
}

const (
	oaiMaxEvent       = 1 << 20 // one SSE event
	oaiMaxModels      = 4 << 20 // /models response
	oaiMaxToolCalls   = 64      // distinct tool-call indexes per response
	oaiMaxToolArgsLen = 1 << 20 // cumulative tool-call argument bytes per response
)

// NewOpenAICompatible builds the adapter. The client MUST be the SSRF-safe
// one from NewHTTPClient; nil is refused. The secret is optional (many
// self-hosted servers run without one) and sent as a Bearer when present.
func NewOpenAICompatible(cred Credential, client *http.Client) (Provider, error) {
	if client == nil {
		return nil, errors.New("llm providers: openai_compatible requires an http client")
	}
	if strings.TrimSpace(cred.BaseURL) == "" {
		return nil, fmt.Errorf("%w: openai_compatible requires a base URL", iface.ErrLLMInvalidRequest)
	}
	return &OpenAICompatible{base: strings.TrimRight(cred.BaseURL, "/"), secret: cred.Secret, client: client}, nil
}

func (*OpenAICompatible) Name() string { return "openai_compatible" }

func (*OpenAICompatible) Capabilities() iface.LLMCapabilities {
	return iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: true}
}

func (o *OpenAICompatible) newRequest(ctx context.Context, method, path string, body []byte, accept string) (*http.Request, error) {
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
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if o.secret != "" {
		req.Header.Set("Authorization", "Bearer "+o.secret)
	}
	return req, nil
}

// send executes req and classifies an HTTP error status. The error body is
// reduced to allowlisted identifiers by ReadErrorInfo and never surfaces.
func (o *OpenAICompatible) send(ctx context.Context, req *http.Request) (*http.Response, error) {
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

func (o *OpenAICompatible) post(ctx context.Context, path, accept string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: request is not serialisable", iface.ErrLLMInvalidRequest)
	}
	req, err := o.newRequest(ctx, http.MethodPost, path, body, accept)
	if err != nil {
		return nil, err
	}
	return o.send(ctx, req)
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
}

// toolAcc accumulates one streamed tool call, keyed by its index.
type toolAcc struct {
	id, name string
	args     strings.Builder
}

func (o *OpenAICompatible) Chat(ctx context.Context, call ChatCall, sink iface.ChatSink) (*ChatOutcome, error) {
	req := call.Request
	if req.Options.Effort != "" || call.Defaults.Effort != "" {
		return nil, fmt.Errorf("%w: openai_compatible does not support reasoning effort", iface.ErrLLMCapabilityMismatch)
	}
	msgs := make([]oaiMessage, 0, len(req.Messages)+1)
	if req.Instructions != "" {
		msgs = append(msgs, oaiMessage{Role: "system", Content: req.Instructions})
	}
	for _, m := range req.Messages {
		role := m.Role
		if role == "developer" {
			role = "system"
		}
		om := oaiMessage{Role: role, Content: m.Content}
		if role == "tool" {
			om.ToolCallID = m.ToolCallID
		}
		for _, tc := range m.ToolCalls {
			args, err := normalizeArgs([]byte(tc.Arguments))
			if err != nil {
				return nil, fmt.Errorf("%w: tool call arguments are not valid JSON", iface.ErrLLMInvalidRequest)
			}
			var otc oaiToolCall
			otc.ID, otc.Type = tc.ID, "function"
			otc.Function.Name, otc.Function.Arguments = tc.Name, args
			om.ToolCalls = append(om.ToolCalls, otc)
		}
		msgs = append(msgs, om)
	}
	payload := map[string]any{
		"model": call.ModelID, "messages": msgs, "stream": true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if t := firstFloat(req.Options.Temperature, call.Defaults.Temperature); t != nil {
		payload["temperature"] = *t
	}
	if n := firstInt(req.Options.MaxOutputTokens, call.Defaults.MaxOutputTokens); n != nil {
		payload["max_tokens"] = *n
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
	if rf := req.ResponseFormat; rf != nil {
		payload["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": rf.Name, "schema": rf.Schema, "strict": rf.Strict,
		}}
	}
	resp, err := o.post(WithStreaming(ctx), "/chat/completions", "text/event-stream", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var (
		tools        = map[int]*toolAcc{}
		argBytes     int
		finishReason string
		usage        *iface.LLMUsage
		outBytes     int // text + emitted tool-call bytes, for the usage fallback
		sawDone      bool
		emittedTools bool
	)
	// flushTools emits every accumulated call, in index order, exactly once.
	// With lenient set (finish_reason=length) a call whose arguments were cut
	// off is dropped instead of failing the response: truncation is the
	// model hitting its output cap, not a provider fault.
	flushTools := func(lenient bool) error {
		if len(tools) == 0 {
			return nil
		}
		idx := make([]int, 0, len(tools))
		for i := range tools {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		for _, i := range idx {
			acc := tools[i]
			args, err := normalizeArgs([]byte(acc.args.String()))
			if err != nil || acc.name == "" {
				if lenient {
					continue
				}
				return malformed("tool call")
			}
			id := acc.id
			if id == "" {
				id = syntheticCallID()
			}
			emitted := iface.ChatToolCall{ID: id, Name: acc.name, Arguments: args}
			if err := sink(iface.ChatEvent{ToolCall: &emitted}); err != nil {
				return err
			}
			emittedTools = true
			outBytes += len(acc.name) + len(args)
		}
		tools = map[int]*toolAcc{}
		return nil
	}

	var cbErr error // set when the callback itself failed (sink or malformed data)
	err = ReadSSE(resp.Body, oaiMaxEvent, func(ev SSEEvent) (ferr error) {
		defer func() {
			if ferr != nil {
				cbErr = ferr
			}
		}()
		data := bytes.TrimSpace(ev.Data)
		if len(data) == 0 {
			return nil
		}
		if string(data) == "[DONE]" {
			sawDone = true
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return malformed("stream")
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return fmt.Errorf("%w: upstream reported an error", iface.ErrLLMProviderUnavailable)
		}
		if chunk.Usage != nil {
			usage = &iface.LLMUsage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		if len(chunk.Choices) == 0 {
			return nil
		}
		ch := chunk.Choices[0]
		if c := ch.Delta.Content; c != "" {
			outBytes += len(c)
			if err := sink(iface.ChatEvent{Delta: c}); err != nil {
				return err
			}
		}
		for _, tc := range ch.Delta.ToolCalls {
			acc, ok := tools[tc.Index]
			if !ok {
				if len(tools) >= oaiMaxToolCalls || tc.Index < 0 {
					return malformed("tool call")
				}
				acc = &toolAcc{}
				tools[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			if argBytes += len(tc.Function.Arguments); argBytes > oaiMaxToolArgsLen {
				return malformed("tool call")
			}
			acc.args.WriteString(tc.Function.Arguments)
		}
		if fr := ch.FinishReason; fr != nil && *fr != "" {
			finishReason = *fr
			if err := flushTools(*fr == "length"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if cbErr != nil {
			return nil, cbErr
		}
		if errors.Is(err, ErrSSEEventTooLarge) {
			return nil, malformed("stream")
		}
		return nil, transportErr(ctx, err)
	}
	if finishReason == "" && !sawDone {
		return nil, fmt.Errorf("%w: stream ended before completion", iface.ErrLLMProviderUnavailable)
	}
	if err := flushTools(false); err != nil { // servers that never send a finish_reason
		return nil, err
	}

	outcome := &ChatOutcome{FinishReason: mapFinishReason(finishReason, emittedTools)}
	var reported iface.LLMUsage
	if usage != nil {
		reported = *usage
	}
	// Servers may omit usage or send zeros: never under-count the budget.
	outcome.Usage = ensureUsage(req, reported, outBytes)
	if err := sink(iface.ChatEvent{Done: true, Usage: &outcome.Usage}); err != nil {
		return nil, err
	}
	return outcome, nil
}

func mapFinishReason(fr string, tools bool) string {
	switch fr {
	case "stop", "":
		if tools {
			return "tool_calls"
		}
		return "stop"
	case "tool_calls", "function_call":
		return "tool_calls"
	case "length":
		return "length"
	case "content_filter":
		return "content_filter"
	}
	return "other"
}

func (o *OpenAICompatible) Embed(ctx context.Context, call EmbedCall) (*EmbedOutcome, error) {
	// No encoding_format: compatible servers differ on whether they accept
	// it, so they answer floats; base64 is still decoded if one sends it.
	resp, err := o.post(WithResponseCap(ctx, MaxEmbedResponseBytes), "/embeddings", "", map[string]any{"model": call.ModelID, "input": call.Inputs})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ReadAllBounded(resp.Body, MaxEmbedResponseBytes)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	var out oaiEmbeddingResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, malformed("embeddings response")
	}
	vectors, err := out.vectors(len(call.Inputs))
	if err != nil {
		return nil, err
	}
	return &EmbedOutcome{Vectors: vectors, Usage: ensureEmbedUsage(call.Inputs, iface.LLMUsage{InputTokens: out.Usage.PromptTokens})}, nil
}

func (o *OpenAICompatible) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
	req, err := o.newRequest(ctx, http.MethodGet, "/models", nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := o.send(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ReadAllBounded(resp.Body, oaiMaxModels)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, malformed("models response")
	}
	res := make([]ModelDescriptor, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			res = append(res, ModelDescriptor{ID: m.ID, DisplayName: m.ID})
		}
	}
	return res, nil
}

func (o *OpenAICompatible) Health(ctx context.Context) error {
	_, err := o.ListModels(ctx)
	return err
}
