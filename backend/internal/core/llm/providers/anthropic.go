package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	anthropicDefaultBase      = "https://api.anthropic.com"
	anthropicDefaultMaxTokens = 8192
	anthropicModelsPageLimit  = 1000
	anthropicMaxModelPages    = 10
	anthropicMaxModels        = 5000
	anthropicCatalogTTL       = time.Hour
	anthropicCatalogFailTTL   = time.Minute
	anthropicCatalogMaxKeys   = 128
)

// Anthropic talks to the Messages API through the official SDK. As with the
// OpenAI adapter the client is built only from AnthropicClientOptions over
// the SSRF-safe http.Client, and the SDK's own error values (which embed the
// request URL and the raw response body) are never surfaced: HTTP failures
// are reduced to allowlisted identifiers by captureErrorMiddleware and
// classified by Classify.
type Anthropic struct {
	client anthropic.Client
	// catalogKey identifies the credential+endpoint in the capability cache.
	// It is a hash: the secret itself is never stored in the cache.
	catalogKey string
}

// NewAnthropic builds the adapter. The client MUST be the SSRF-safe one from
// NewHTTPClient; nil is refused (the SDK would otherwise fall back to
// http.DefaultClient and bypass the destination guard).
func NewAnthropic(cred Credential, client *http.Client) (Provider, error) {
	if client == nil {
		return nil, errors.New("llm providers: anthropic requires an http client")
	}
	base := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if base == "" {
		base = anthropicDefaultBase
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: anthropic base URL is not a valid http(s) URL", iface.ErrLLMInvalidRequest)
	}
	sum := sha256.Sum256([]byte(base + "\x00" + cred.Secret))
	return &Anthropic{
		client:     anthropic.NewClient(AnthropicClientOptions(base, cred.Secret, client)...),
		catalogKey: hex.EncodeToString(sum[:]),
	}, nil
}

func (*Anthropic) Name() string { return "anthropic" }

// Capabilities: Anthropic has no embeddings endpoint.
func (*Anthropic) Capabilities() iface.LLMCapabilities {
	return iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true}
}

func captureAnthropicError(dst *ErrorInfo) option.RequestOption {
	return option.WithMiddleware(captureErrorMiddleware(dst))
}

// ---- model catalog (capabilities per discovered model) ----

// anthropicModelCaps is the slice of ModelInfo.Capabilities the adapter acts
// on. Behaviour is driven by what the provider declares for the model, never
// by its name.
type anthropicModelCaps struct {
	effort   map[string]bool // supported effort levels
	adaptive bool            // accepts thinking {type: adaptive}
}

type anthropicCatalog struct {
	fetched time.Time
	ttl     time.Duration
	models  map[string]anthropicModelCaps
}

var (
	anthropicCatalogMu    sync.Mutex
	anthropicCatalogCache = map[string]*anthropicCatalog{}
	anthropicNow          = time.Now
)

func capsFromInfo(m anthropic.ModelInfo) anthropicModelCaps {
	c := anthropicModelCaps{adaptive: m.Capabilities.Thinking.Supported && m.Capabilities.Thinking.Types.Adaptive.Supported}
	if e := m.Capabilities.Effort; e.Supported {
		c.effort = map[string]bool{
			"low": e.Low.Supported, "medium": e.Medium.Supported, "high": e.High.Supported,
			"xhigh": e.Xhigh.Supported, "max": e.Max.Supported,
		}
	}
	return c
}

// listModelInfos pages through GET /v1/models with an explicit cursor, within
// fixed page and size bounds.
func (a *Anthropic) listModelInfos(ctx context.Context) ([]anthropic.ModelInfo, error) {
	var (
		out   []anthropic.ModelInfo
		after string
	)
	for i := 0; i < anthropicMaxModelPages; i++ {
		params := anthropic.ModelListParams{Limit: anthropic.Int(anthropicModelsPageLimit)}
		if after != "" {
			params.AfterID = anthropic.String(after)
		}
		var httpErr ErrorInfo
		page, err := a.client.Models.List(ctx, params, captureAnthropicError(&httpErr))
		if httpErr.Status != 0 {
			return nil, Classify(httpErr)
		}
		if err != nil {
			return nil, classifyAnthropicSDKError(ctx, err)
		}
		out = append(out, page.Data...)
		if len(out) > anthropicMaxModels {
			return nil, malformed("models response")
		}
		if !page.HasMore || page.LastID == "" || page.LastID == after {
			return out, nil
		}
		after = page.LastID
	}
	return out, nil
}

// modelCaps returns the declared capabilities of one model from the per
// credential catalog cache (1 h; a failed discovery is remembered for a
// minute so a key without model-listing access does not add a request to every
// chat). ok=false means the model is unknown or discovery failed: callers then
// omit the optional parameters. Only a caller cancellation is returned as an
// error.
func (a *Anthropic) modelCaps(ctx context.Context, modelID string) (caps anthropicModelCaps, ok bool, err error) {
	now := anthropicNow()
	anthropicCatalogMu.Lock()
	cat := anthropicCatalogCache[a.catalogKey]
	anthropicCatalogMu.Unlock()
	if cat == nil || now.Sub(cat.fetched) >= cat.ttl {
		cat = &anthropicCatalog{fetched: now, ttl: anthropicCatalogTTL, models: map[string]anthropicModelCaps{}}
		infos, lerr := a.listModelInfos(ctx)
		switch {
		case lerr == nil:
			for _, m := range infos {
				cat.models[m.ID] = capsFromInfo(m)
			}
		case errors.Is(lerr, context.Canceled):
			return caps, false, lerr
		default:
			cat.ttl = anthropicCatalogFailTTL
		}
		anthropicCatalogMu.Lock()
		if len(anthropicCatalogCache) >= anthropicCatalogMaxKeys {
			for k, v := range anthropicCatalogCache {
				if now.Sub(v.fetched) >= v.ttl {
					delete(anthropicCatalogCache, k)
				}
			}
			if len(anthropicCatalogCache) >= anthropicCatalogMaxKeys {
				for k := range anthropicCatalogCache { // still full: drop an arbitrary entry
					delete(anthropicCatalogCache, k)
					break
				}
			}
		}
		anthropicCatalogCache[a.catalogKey] = cat
		anthropicCatalogMu.Unlock()
	}
	caps, ok = cat.models[modelID]
	return caps, ok, nil
}

// ---- request mapping ----

func anthropicToolSchema(spec iface.ChatToolSpec) (anthropic.ToolInputSchemaParam, error) {
	out := anthropic.ToolInputSchemaParam{}
	if len(spec.InputSchema) == 0 {
		return out, nil
	}
	if t, ok := spec.InputSchema["type"]; ok && t != "object" {
		return out, fmt.Errorf("%w: tool input schema must be an object schema", iface.ErrLLMInvalidRequest)
	}
	extra := map[string]any{}
	for k, v := range spec.InputSchema {
		switch k {
		case "type":
		case "properties":
			out.Properties = v
		case "required":
			switch r := v.(type) {
			case []string:
				out.Required = r
			case []any:
				for _, e := range r {
					s, ok := e.(string)
					if !ok {
						return out, fmt.Errorf("%w: tool input schema required must list strings", iface.ErrLLMInvalidRequest)
					}
					out.Required = append(out.Required, s)
				}
			default:
				return out, fmt.Errorf("%w: tool input schema required must be a list", iface.ErrLLMInvalidRequest)
			}
		default:
			extra[k] = v // $defs, additionalProperties, ... pass through verbatim
		}
	}
	if len(extra) > 0 {
		out.ExtraFields = extra
	}
	return out, nil
}

func buildAnthropicMessages(req iface.ChatRequest) ([]anthropic.TextBlockParam, []anthropic.MessageParam, error) {
	var (
		system   []anthropic.TextBlockParam
		messages []anthropic.MessageParam
		results  []anthropic.ContentBlockParamUnion // consecutive tool results share one user turn
	)
	addSystem := func(s string) {
		if strings.TrimSpace(s) != "" {
			system = append(system, anthropic.TextBlockParam{Text: s})
		}
	}
	flush := func() {
		if len(results) > 0 {
			messages = append(messages, anthropic.NewUserMessage(results...))
			results = nil
		}
	}
	addSystem(req.Instructions)
	for _, m := range req.Messages {
		if m.Role != "tool" {
			flush()
		}
		switch m.Role {
		case "system", "developer":
			addSystem(m.Content)
		case "user":
			if strings.TrimSpace(m.Content) == "" {
				return nil, nil, fmt.Errorf("%w: empty user message", iface.ErrLLMInvalidRequest)
			}
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				if tc.ID == "" || tc.Name == "" {
					return nil, nil, fmt.Errorf("%w: tool call needs an id and a name", iface.ErrLLMInvalidRequest)
				}
				args, err := normalizeArgs([]byte(tc.Arguments))
				if err != nil {
					return nil, nil, fmt.Errorf("%w: tool call arguments are not valid JSON", iface.ErrLLMInvalidRequest)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, json.RawMessage(args), tc.Name))
			}
			if len(blocks) > 0 { // the API refuses empty assistant content
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}
		case "tool":
			if m.ToolCallID == "" {
				return nil, nil, fmt.Errorf("%w: tool result needs a tool call id", iface.ErrLLMInvalidRequest)
			}
			// An empty result must carry no content at all: the helper would
			// emit an empty text block, which the API rejects.
			tr := anthropic.ToolResultBlockParam{ToolUseID: m.ToolCallID}
			if m.Content != "" {
				tr.Content = []anthropic.ToolResultBlockParamContentUnion{{OfText: &anthropic.TextBlockParam{Text: m.Content}}}
			}
			results = append(results, anthropic.ContentBlockParamUnion{OfToolResult: &tr})
		default:
			return nil, nil, fmt.Errorf("%w: unsupported message role", iface.ErrLLMInvalidRequest)
		}
	}
	flush()
	return system, messages, nil
}

func (a *Anthropic) buildParams(ctx context.Context, call ChatCall) (anthropic.MessageNewParams, error) {
	req := call.Request
	system, messages, err := buildAnthropicMessages(req)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	maxTokens := anthropicDefaultMaxTokens
	if v := firstInt(req.Options.MaxOutputTokens, call.Defaults.MaxOutputTokens); v != nil {
		maxTokens = *v
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(call.ModelID),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
		System:    system,
	}
	hasToolTurns := len(req.Tools) > 0
	for _, t := range req.Tools {
		schema, err := anthropicToolSchema(t)
		if err != nil {
			return params, err
		}
		tp := &anthropic.ToolParam{Name: t.Name, InputSchema: schema}
		if t.Description != "" {
			tp.Description = anthropic.String(t.Description)
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: tp})
	}
	for _, m := range req.Messages {
		if len(m.ToolCalls) > 0 || m.Role == "tool" {
			hasToolTurns = true
		}
	}
	if rf := req.ResponseFormat; rf != nil {
		params.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: rf.Schema}
	}
	// Effort / adaptive thinking only for models whose discovered
	// capabilities declare them: both are sent only when an effort was
	// requested (per request or model default) and the model declares that
	// level; adaptive thinking additionally needs the adaptive type. Thinking
	// is further limited to tool-free conversations: a tool loop would have to
	// carry the model's thinking blocks back, which the neutral message type
	// cannot hold.
	if effort := firstString(req.Options.Effort, call.Defaults.Effort); effort != "" {
		caps, ok, err := a.modelCaps(ctx, call.ModelID)
		if err != nil {
			return params, err
		}
		if ok && caps.effort[effort] {
			params.OutputConfig.Effort = anthropic.OutputConfigEffort(effort)
			if caps.adaptive && !hasToolTurns {
				params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
			}
		}
	}
	// Thinking is incompatible with temperature modification (the API answers
	// 400), so a temperature, including a model default, is dropped whenever
	// thinking is on.
	if params.Thinking.OfAdaptive == nil {
		if t := firstFloat(req.Options.Temperature, call.Defaults.Temperature); t != nil {
			params.Temperature = anthropic.Float(*t)
		}
	}
	return params, nil
}

// ---- chat ----

type anthropicToolAcc struct {
	id, name string
	args     strings.Builder
}

func (a *Anthropic) Chat(ctx context.Context, call ChatCall, sink iface.ChatSink) (*ChatOutcome, error) {
	params, err := a.buildParams(ctx, call)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, transportErr(ctx, err)
		}
		return nil, err
	}
	var httpErr ErrorInfo
	stream := a.client.Messages.NewStreaming(WithStreaming(ctx), params, captureAnthropicError(&httpErr))
	defer func() { _ = stream.Close() }()

	var (
		usage      iface.LLMUsage
		stopReason anthropic.StopReason
		outBytes   int
		tools      = map[int64]*anthropicToolAcc{} // content block index -> call being assembled
		order      []int64                         // block indexes in start order
		argBytes   int
		finished   bool
	)
	for stream.Next() && !finished {
		switch v := stream.Current().AsAny().(type) {
		case anthropic.MessageStartEvent:
			u := v.Message.Usage
			usage.InputTokens = int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens)
			usage.OutputTokens = int(u.OutputTokens)
		case anthropic.ContentBlockStartEvent:
			tu, ok := v.ContentBlock.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}
			if len(tools) >= oaiMaxToolCalls || tools[v.Index] != nil {
				return nil, malformed("tool call")
			}
			tools[v.Index] = &anthropicToolAcc{id: tu.ID, name: tu.Name}
			order = append(order, v.Index)
		case anthropic.ContentBlockDeltaEvent:
			switch d := v.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if d.Text == "" {
					continue
				}
				outBytes += len(d.Text)
				if err := sink(iface.ChatEvent{Delta: d.Text}); err != nil {
					return nil, err
				}
			case anthropic.InputJSONDelta:
				acc := tools[v.Index]
				if acc == nil {
					continue
				}
				if argBytes += len(d.PartialJSON); argBytes > oaiMaxToolArgsLen {
					return nil, malformed("tool call")
				}
				acc.args.WriteString(d.PartialJSON)
			}
		case anthropic.MessageDeltaEvent:
			if v.Delta.StopReason != "" {
				stopReason = v.Delta.StopReason
			}
			u := v.Usage // cumulative; absent fields stay zero
			// Never replace the start total (input + cache) with a smaller
			// delta value: the larger of the two is the conservative count.
			if in := int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens); in > usage.InputTokens {
				usage.InputTokens = in
			}
			if u.OutputTokens > 0 {
				usage.OutputTokens = int(u.OutputTokens)
			}
		case anthropic.MessageStopEvent:
			finished = true
		}
	}
	if httpErr.Status != 0 {
		return nil, Classify(httpErr)
	}
	if err := stream.Err(); err != nil && !finished {
		return nil, classifyAnthropicSDKError(ctx, err)
	}
	if !finished {
		return nil, fmt.Errorf("%w: stream ended before completion", iface.ErrLLMProviderUnavailable)
	}

	// Tool calls are handed over once the stop reason is known: on
	// max_tokens a call whose arguments were cut off is dropped (the model hit
	// its output cap, not a provider fault); otherwise it is a malformed
	// response.
	lenient := stopReason == anthropic.StopReasonMaxTokens
	emitted := 0
	for _, idx := range order {
		acc := tools[idx]
		args, err := normalizeArgs([]byte(acc.args.String()))
		if err != nil || acc.name == "" || acc.id == "" {
			if lenient {
				continue
			}
			return nil, malformed("tool call")
		}
		outBytes += len(acc.name) + len(args)
		emitted++
		if err := sink(iface.ChatEvent{ToolCall: &iface.ChatToolCall{ID: acc.id, Name: acc.name, Arguments: args}}); err != nil {
			return nil, err
		}
	}

	outcome := &ChatOutcome{FinishReason: anthropicFinishReason(stopReason, emitted)}
	outcome.Usage = ensureUsage(call.Request, usage, outBytes)
	if err := sink(iface.ChatEvent{Done: true, Usage: &outcome.Usage}); err != nil {
		return nil, err
	}
	return outcome, nil
}

func anthropicFinishReason(r anthropic.StopReason, emittedTools int) string {
	switch r {
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		return "length"
	case anthropic.StopReasonRefusal:
		return "content_filter"
	case anthropic.StopReasonPauseTurn:
		return "other"
	}
	if emittedTools > 0 {
		return "tool_calls"
	}
	return "stop" // end_turn, stop_sequence, or no stop reason at all
}

// anthropicStreamErrorStatus maps the error type of an error event delivered
// inside a 200 stream onto the HTTP status the same error carries on a plain
// response, so Classify treats both alike.
func anthropicStreamErrorStatus(typ string) int {
	switch typ {
	case "invalid_request_error":
		return http.StatusBadRequest
	case "authentication_error":
		return http.StatusUnauthorized
	case "billing_error":
		return http.StatusPaymentRequired
	case "permission_error":
		return http.StatusForbidden
	case "not_found_error":
		return http.StatusNotFound
	case "request_too_large":
		return http.StatusRequestEntityTooLarge
	case "rate_limit_error":
		return http.StatusTooManyRequests
	case "timeout_error":
		return http.StatusRequestTimeout
	}
	return http.StatusBadGateway // api_error, overloaded_error and anything unknown
}

// classifyAnthropicSDKError turns a non-HTTP SDK failure into a sentinel
// without ever returning the SDK's text (it can carry the request URL or the
// provider body).
func classifyAnthropicSDKError(ctx context.Context, err error) error {
	if e := sdkTransportFailure(ctx, err); e != nil {
		return e
	}
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		typ := identifier(string(apierr.Type()))
		status := apierr.StatusCode
		if status < 400 { // an error event inside a 200 stream
			status = anthropicStreamErrorStatus(typ)
		}
		return Classify(ErrorInfo{Status: status, Type: typ})
	}
	return sdkFallbackFailure(ctx, err)
}

// Embed: Anthropic has no embeddings endpoint.
func (*Anthropic) Embed(context.Context, EmbedCall) (*EmbedOutcome, error) {
	return nil, fmt.Errorf("%w: anthropic has no embeddings", iface.ErrLLMCapabilityMismatch)
}

func (a *Anthropic) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
	infos, err := a.listModelInfos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelDescriptor, 0, len(infos))
	for _, m := range infos {
		if m.ID == "" {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		out = append(out, ModelDescriptor{ID: m.ID, DisplayName: name})
	}
	return out, nil
}

func (a *Anthropic) Health(ctx context.Context) error {
	_, err := a.ListModels(ctx)
	return err
}
