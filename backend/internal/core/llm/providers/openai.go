package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// OpenAIProfile selects the request dialect of the OpenAI adapter.
type OpenAIProfile int

const (
	// ProfileAPIKey: org API key, full Responses + embeddings + models.
	ProfileAPIKey OpenAIProfile = iota
	// ProfileSIWC: a user's ChatGPT token (PR 3). Responses only, store:false,
	// stream:true, no temperature / max_output_tokens / metadata / user /
	// previous_response_id; no embeddings; account-specific catalog.
	ProfileSIWC
)

const openAIDefaultBase = "https://api.openai.com/v1"

// OpenAI talks to the Responses API through the official SDK. The SDK client
// is built only from OpenAIClientOptions (explicit identity, no environment
// defaults, no SDK retries) over the SSRF-safe http.Client, and the SDK's own
// error values are never surfaced: they embed the raw response body. HTTP
// failures are reduced to allowlisted identifiers by a per-call middleware
// (see captureHTTPError) and classified by Classify.
type OpenAI struct {
	client  openai.Client
	profile OpenAIProfile
}

// NewOpenAI builds the adapter. The client MUST be the SSRF-safe one from
// NewHTTPClient; nil is refused (the SDK would otherwise fall back to
// http.DefaultClient and bypass the destination guard). cred.Kind
// user_account selects the SIWC profile.
func NewOpenAI(cred Credential, client *http.Client) (Provider, error) {
	if client == nil {
		return nil, errors.New("llm providers: openai requires an http client")
	}
	base := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if base == "" {
		base = openAIDefaultBase
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: openai base URL is not a valid http(s) URL", iface.ErrLLMInvalidRequest)
	}
	profile := ProfileAPIKey
	if cred.Kind == iface.LLMCredentialKindUserAccount {
		profile = ProfileSIWC
	}
	return &OpenAI{client: openai.NewClient(OpenAIClientOptions(base, cred.Secret, client)...), profile: profile}, nil
}

func (*OpenAI) Name() string { return "openai" }

func (o *OpenAI) Capabilities() iface.LLMCapabilities {
	return iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: o.profile == ProfileAPIKey}
}

// Profile reports the request dialect (PR 3 uses it for the SIWC path).
func (o *OpenAI) Profile() OpenAIProfile { return o.profile }

// captureHTTPError is the per-call SDK middleware that keeps only the
// allowlisted error identifiers (see captureErrorMiddleware).
func captureHTTPError(dst *ErrorInfo) option.RequestOption {
	return option.WithMiddleware(captureErrorMiddleware(dst))
}

func (o *OpenAI) buildInput(req iface.ChatRequest) (responses.ResponseInputParam, error) {
	var items responses.ResponseInputParam
	message := func(role responses.EasyInputMessageRole, content string) responses.ResponseInputItemUnionParam {
		return responses.ResponseInputItemUnionParam{OfMessage: &responses.EasyInputMessageParam{
			Role: role, Content: responses.EasyInputMessageContentUnionParam{OfString: openai.String(content)}}}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "tool":
			items = append(items, responses.ResponseInputItemUnionParam{OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
				CallID: m.ToolCallID, Output: m.Content}})
		case "assistant":
			if m.Content != "" {
				items = append(items, message(responses.EasyInputMessageRoleAssistant, m.Content))
			}
			for _, tc := range m.ToolCalls {
				args, err := normalizeArgs([]byte(tc.Arguments))
				if err != nil {
					return nil, fmt.Errorf("%w: tool call arguments are not valid JSON", iface.ErrLLMInvalidRequest)
				}
				items = append(items, responses.ResponseInputItemUnionParam{OfFunctionCall: &responses.ResponseFunctionToolCallParam{
					CallID: tc.ID, Name: tc.Name, Arguments: args}})
			}
		case "user":
			items = append(items, message(responses.EasyInputMessageRoleUser, m.Content))
		case "developer":
			items = append(items, message(responses.EasyInputMessageRoleDeveloper, m.Content))
		case "system":
			items = append(items, message(responses.EasyInputMessageRoleSystem, m.Content))
		default:
			return nil, fmt.Errorf("%w: unsupported message role", iface.ErrLLMInvalidRequest)
		}
	}
	return items, nil
}

func (o *OpenAI) buildParams(call ChatCall) (responses.ResponseNewParams, error) {
	req := call.Request
	input, err := o.buildInput(req)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(call.ModelID),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Store: openai.Bool(false),
	}
	if req.Instructions != "" {
		params.Instructions = openai.String(req.Instructions)
	}
	temp := firstFloat(req.Options.Temperature, call.Defaults.Temperature)
	maxOut := firstInt(req.Options.MaxOutputTokens, call.Defaults.MaxOutputTokens)
	if o.profile == ProfileSIWC && (temp != nil || maxOut != nil) {
		return params, fmt.Errorf("%w: temperature and maxOutputTokens are not available on the ChatGPT plan route", iface.ErrLLMCapabilityMismatch)
	}
	if temp != nil {
		params.Temperature = openai.Float(*temp)
	}
	if maxOut != nil {
		params.MaxOutputTokens = openai.Int(int64(*maxOut))
	}
	if effort := firstString(req.Options.Effort, call.Defaults.Effort); effort != "" {
		params.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(effort)}
	}
	for _, t := range req.Tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		params.Tools = append(params.Tools, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{
			Name: t.Name, Description: openai.String(t.Description), Parameters: schema, Strict: openai.Bool(false)}})
	}
	if rf := req.ResponseFormat; rf != nil {
		params.Text = responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{Name: rf.Name, Schema: rf.Schema, Strict: openai.Bool(rf.Strict)}}}
	}
	return params, nil
}

func (o *OpenAI) Chat(ctx context.Context, call ChatCall, sink iface.ChatSink) (*ChatOutcome, error) {
	params, err := o.buildParams(call)
	if err != nil {
		return nil, err
	}
	var httpErr ErrorInfo
	stream := o.client.Responses.NewStreaming(WithStreaming(ctx), params, captureHTTPError(&httpErr))
	defer func() { _ = stream.Close() }()

	var (
		outcome    = &ChatOutcome{FinishReason: "stop"}
		pending    = map[string]*iface.ChatToolCall{} // output item id -> call being assembled
		emitted    int                                // tool calls handed to the sink
		outBytes   int                                // text + emitted tool-call bytes, for the usage fallback
		finished   bool
		incomplete string
	)
	emitCall := func(tc *iface.ChatToolCall, rawArgs string) error {
		args, err := normalizeArgs([]byte(rawArgs))
		if err != nil || len(args) > oaiMaxToolArgsLen || tc.Name == "" {
			return malformed("tool call")
		}
		tc.Arguments = args
		emitted++
		outBytes += len(tc.Name) + len(args)
		return sink(iface.ChatEvent{ToolCall: tc})
	}
	for stream.Next() && !finished {
		e := stream.Current()
		switch e.Type {
		case "response.output_text.delta", "response.refusal.delta":
			d := e.Delta.OfString
			if d == "" {
				continue
			}
			outBytes += len(d)
			if err := sink(iface.ChatEvent{Delta: d}); err != nil {
				return nil, err
			}
		case "response.output_item.added":
			item := e.AsResponseOutputItemAdded().Item
			if item.Type != "function_call" {
				continue
			}
			if len(pending)+emitted >= oaiMaxToolCalls {
				return nil, malformed("tool call")
			}
			fc := item.AsFunctionCall()
			pending[item.ID] = &iface.ChatToolCall{ID: fc.CallID, Name: fc.Name}
		case "response.function_call_arguments.done":
			done := e.AsResponseFunctionCallArgumentsDone()
			if tc, ok := pending[done.ItemID]; ok {
				delete(pending, done.ItemID)
				if err := emitCall(tc, done.Arguments); err != nil {
					return nil, err
				}
			}
		case "response.output_item.done":
			// A call whose arguments.done never came (no-argument tools)
			// is completed from the finished item.
			item := e.AsResponseOutputItemDone().Item
			if tc, ok := pending[item.ID]; ok && item.Type == "function_call" {
				delete(pending, item.ID)
				if err := emitCall(tc, item.AsFunctionCall().Arguments); err != nil {
					return nil, err
				}
			}
		case "response.completed", "response.incomplete":
			var r responses.Response
			if e.Type == "response.completed" {
				r = e.AsResponseCompleted().Response
			} else {
				r = e.AsResponseIncomplete().Response
				incomplete = "unknown" // response.incomplete is never a plain stop
			}
			if r.IncompleteDetails.Reason != "" {
				incomplete = r.IncompleteDetails.Reason
			}
			outcome.Usage = iface.LLMUsage{InputTokens: int(r.Usage.InputTokens), OutputTokens: int(r.Usage.OutputTokens)}
			finished = true
		case "response.failed":
			return nil, Classify(failedInfo(string(e.AsResponseFailed().Response.Error.Code)))
		case "error":
			return nil, Classify(failedInfo(e.AsError().Code))
		}
	}
	if httpErr.Status != 0 {
		return nil, Classify(httpErr)
	}
	if err := stream.Err(); err != nil && !finished {
		return nil, classifySDKError(ctx, err)
	}
	if !finished {
		return nil, fmt.Errorf("%w: stream ended before completion", iface.ErrLLMProviderUnavailable)
	}

	switch {
	case incomplete == "max_output_tokens":
		outcome.FinishReason = "length"
	case incomplete == "content_filter":
		outcome.FinishReason = "content_filter"
	case incomplete != "":
		outcome.FinishReason = "other"
	case emitted > 0:
		outcome.FinishReason = "tool_calls"
	}
	outcome.Usage = ensureUsage(call.Request, outcome.Usage, outBytes)
	if err := sink(iface.ChatEvent{Done: true, Usage: &outcome.Usage}); err != nil {
		return nil, err
	}
	return outcome, nil
}

// failedInfo maps a response.failed / error event code onto an ErrorInfo.
// Only a short identifier is kept; the event's message is discarded. Codes
// that describe the request itself are invalid-request/capability (never
// retried, never trip the breaker); everything else is a provider fault.
func failedInfo(code string) ErrorInfo {
	code = identifier(code)
	status := http.StatusBadGateway
	for _, p := range []string{"invalid_", "image_", "unsupported_", "empty_", "failed_to_download", "context_length", "model_not_found"} {
		if strings.HasPrefix(code, p) {
			status = http.StatusBadRequest
			break
		}
	}
	return ErrorInfo{Status: status, Type: code, Code: code}
}

const sdkStreamErrorPrefix = "received error while streaming: "

// classifySDKError turns a non-HTTP SDK failure into a sentinel without ever
// returning the SDK's text (it can carry the request URL or provider body).
func classifySDKError(ctx context.Context, err error) error {
	if e := sdkTransportFailure(ctx, err); e != nil {
		return e
	}
	var apierr *openai.Error
	if errors.As(err, &apierr) {
		return Classify(ErrorInfo{Status: apierr.StatusCode, Type: identifier(apierr.Type), Code: identifier(apierr.Code), Param: identifier(apierr.Param)})
	}
	if msg := err.Error(); strings.HasPrefix(msg, sdkStreamErrorPrefix) {
		// An error object delivered inside a 200 stream: keep only its ids.
		var p struct {
			Type any `json:"type"`
			Code any `json:"code"`
		}
		_ = json.NewDecoder(bytes.NewReader([]byte(strings.TrimPrefix(msg, sdkStreamErrorPrefix)))).Decode(&p)
		return Classify(ErrorInfo{Status: http.StatusBadGateway, Type: identifier(p.Type), Code: identifier(p.Code)})
	}
	return sdkFallbackFailure(ctx, err)
}

func (o *OpenAI) Embed(ctx context.Context, call EmbedCall) (*EmbedOutcome, error) {
	if o.profile == ProfileSIWC {
		return nil, fmt.Errorf("%w: embeddings are not available on the ChatGPT plan route", iface.ErrLLMCapabilityMismatch)
	}
	var httpErr ErrorInfo
	res, err := o.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(call.ModelID),
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: call.Inputs},
	}, captureHTTPError(&httpErr))
	if httpErr.Status != 0 {
		return nil, Classify(httpErr)
	}
	if err != nil {
		return nil, classifySDKError(ctx, err)
	}
	if len(res.Data) != len(call.Inputs) {
		return nil, malformed("embeddings response")
	}
	out := &EmbedOutcome{Usage: ensureEmbedUsage(call.Inputs, iface.LLMUsage{InputTokens: int(res.Usage.PromptTokens)}), Vectors: make([][]float32, len(res.Data))}
	for _, d := range res.Data {
		i := int(d.Index)
		if i < 0 || i >= len(out.Vectors) || out.Vectors[i] != nil || len(d.Embedding) == 0 {
			return nil, malformed("embeddings response")
		}
		vec := make([]float32, len(d.Embedding))
		for k, f := range d.Embedding {
			vec[k] = float32(f)
		}
		out.Vectors[i] = vec
	}
	return out, nil
}

func (o *OpenAI) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
	if o.profile == ProfileSIWC {
		return nil, fmt.Errorf("%w: SIWC catalog not wired", iface.ErrLLMProviderUnavailable) // PR 3
	}
	var httpErr ErrorInfo
	page, err := o.client.Models.List(ctx, captureHTTPError(&httpErr))
	if httpErr.Status != 0 {
		return nil, Classify(httpErr)
	}
	if err != nil {
		return nil, classifySDKError(ctx, err)
	}
	// /models is not paginated at the API level; the SDK page never has a next page.
	out := make([]ModelDescriptor, 0, len(page.Data))
	for _, m := range page.Data {
		if m.ID != "" {
			out = append(out, ModelDescriptor{ID: m.ID, DisplayName: m.ID})
		}
	}
	return out, nil
}

func (o *OpenAI) Health(ctx context.Context) error {
	_, err := o.ListModels(ctx)
	return err
}

func firstString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
