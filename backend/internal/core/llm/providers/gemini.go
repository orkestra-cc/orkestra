package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"google.golang.org/genai"
)

const (
	geminiDefaultBase    = "https://generativelanguage.googleapis.com"
	geminiEmbedBatchSize = 100 // batchEmbedContents accepts at most 100 requests
	geminiModelsPageSize = 1000
	geminiMaxModelPages  = 10
	geminiMaxModels      = 5000
	geminiCatalogTTL     = time.Hour
	geminiCatalogFailTTL = time.Minute
	geminiCatalogMaxKeys = 128
	// geminiMaxSignature caps the base64 text of a thought signature.
	geminiMaxSignature = 64 << 10
	// geminiSkipSignature is Google's documented escape hatch for replaying a
	// function call that carries no signature (history from another provider,
	// a mid-loop fallback). UNVERIFIED against the live API. The documented
	// JSON string contains '_' (URL-safe base64) while genai encodes []byte
	// with the standard alphabet, so the bytes are the URL-decoded literal:
	// the wire form then differs only in '_' vs '/', which proto-JSON decoders
	// accept interchangeably.
	geminiSkipSignature = "skip_thought_signature_validator"
)

// Gemini talks to the Gemini Developer API through the official SDK. The
// client is built only from GeminiClientConfig (explicit key, base URL and
// HTTP client; no retries, no environment fallback). The SDK has no per-call
// middleware, so error capture is a RoundTripper wrapped around the SSRF-safe
// client: for an error status it keeps only the allowlisted identifiers (in a
// per-call destination carried by the request context) and hands the SDK a
// body without text, so the SDK's unbounded read and its Error() text (which
// embeds the provider message) are never involved.
type Gemini struct {
	client *genai.Client
	// catalogKey identifies the credential+endpoint in the capability cache.
	// It is a hash: the secret itself is never stored in the cache.
	catalogKey string
}

// NewGemini builds the adapter. The client MUST be the SSRF-safe one from
// NewHTTPClient; nil is refused.
func NewGemini(cred Credential, client *http.Client) (Provider, error) {
	if client == nil {
		return nil, errors.New("llm providers: gemini requires an http client")
	}
	if strings.TrimSpace(cred.Secret) == "" {
		return nil, fmt.Errorf("%w: gemini requires an API key", iface.ErrLLMInvalidRequest)
	}
	base := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if base == "" {
		base = geminiDefaultBase
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: gemini base URL is not a valid http(s) URL", iface.ErrLLMInvalidRequest)
	}
	wrapped := *client
	inner := client.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	wrapped.Transport = &geminiTransport{base: inner}
	cfg, err := GeminiClientConfig(base, cred.Secret, &wrapped)
	if err != nil {
		return nil, err
	}
	c, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: gemini client could not be built", iface.ErrLLMInvalidRequest)
	}
	sum := sha256.Sum256([]byte(base + "\x00" + cred.Secret))
	return &Gemini{client: c, catalogKey: hex.EncodeToString(sum[:])}, nil
}

func (*Gemini) Name() string { return "gemini" }

func (*Gemini) Capabilities() iface.LLMCapabilities {
	return iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: true}
}

// ---- transport: error capture + stream event bound ----

type geminiErrKey struct{}

// geminiCall returns a context that carries the destination for the captured
// error identifiers of one SDK call.
func geminiCall(ctx context.Context) (context.Context, *ErrorInfo) {
	dst := &ErrorInfo{}
	return context.WithValue(ctx, geminiErrKey{}, dst), dst
}

type geminiTransport struct{ base http.RoundTripper }

func (t *geminiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	dst, _ := req.Context().Value(geminiErrKey{}).(*ErrorInfo)
	if dst == nil {
		dst = &ErrorInfo{}
	}
	resp, err := captureErrorMiddleware(dst)(req, t.base.RoundTrip)
	if err != nil || resp == nil || resp.StatusCode >= 400 || resp.Body == nil || resp.Body == http.NoBody {
		return resp, err
	}
	switch {
	case isStreaming(req.Context()):
		// The SDK's own stream scanner grows to 256 MiB per event; bound it.
		resp.Body = &eventGuardBody{rc: resp.Body, max: oaiMaxEvent}
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/models"):
		// The SDK logs the whole response map (headers included) through the
		// std logger when a models list has no "models" key, e.g. a valid {}.
		body, rerr := ReadAllBounded(resp.Body, oaiMaxModels)
		_ = resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal(body, &probe) == nil {
			if _, ok := probe["models"]; !ok {
				body = []byte(`{"models":[]}`)
				resp.ContentLength = int64(len(body))
			}
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

// eventGuardBody fails with ErrSSEEventTooLarge when more than max bytes
// arrive without an event boundary (a blank line).
type eventGuardBody struct {
	rc  io.ReadCloser
	max int
	run int
	nl  int
	err error
}

func (b *eventGuardBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	n, err := b.rc.Read(p)
	for _, c := range p[:n] {
		switch c {
		case '\n':
			b.nl++
		case '\r':
		default:
			b.nl = 0
		}
		b.run++
		if b.nl >= 2 {
			b.run, b.nl = 0, 0
		}
		if b.run > b.max {
			b.err = ErrSSEEventTooLarge
			return 0, b.err
		}
	}
	return n, err
}

func (b *eventGuardBody) Close() error { return b.rc.Close() }

// ---- model catalog (Thinking capability per discovered model) ----

type geminiCatalog struct {
	fetched  time.Time
	ttl      time.Duration
	thinking map[string]bool
}

var (
	geminiCatalogMu    sync.Mutex
	geminiCatalogCache = map[string]*geminiCatalog{}
	geminiNow          = time.Now
)

func geminiModelID(name string) string { return strings.TrimPrefix(name, "models/") }

// listModelInfos pages through GET /models within fixed page and size bounds.
func (g *Gemini) listModelInfos(ctx context.Context) ([]*genai.Model, error) {
	cctx, httpErr := geminiCall(ctx)
	fail := func(err error) error {
		if httpErr.Status != 0 {
			return Classify(*httpErr)
		}
		return classifyGeminiSDKError(ctx, err)
	}
	page, err := g.client.Models.List(cctx, &genai.ListModelsConfig{PageSize: geminiModelsPageSize})
	if err != nil {
		return nil, fail(err)
	}
	var out []*genai.Model
	for i := 0; ; i++ {
		out = append(out, page.Items...)
		if len(out) > geminiMaxModels {
			return nil, malformed("models response")
		}
		if page.NextPageToken == "" || i+1 >= geminiMaxModelPages {
			return out, nil
		}
		if page, err = page.Next(cctx); err != nil {
			if errors.Is(err, genai.ErrPageDone) {
				return out, nil
			}
			return nil, fail(err)
		}
	}
}

// supportsThinking reports whether the model's discovered metadata declares
// thinking. ok=false means the model is unknown or discovery failed: callers
// then omit the optional parameter. Results are cached per credential for an
// hour (a failed discovery for a minute). Only a caller cancellation is
// returned as an error.
func (g *Gemini) supportsThinking(ctx context.Context, modelID string) (supported bool, err error) {
	now := geminiNow()
	geminiCatalogMu.Lock()
	cat := geminiCatalogCache[g.catalogKey]
	geminiCatalogMu.Unlock()
	if cat == nil || now.Sub(cat.fetched) >= cat.ttl {
		cat = &geminiCatalog{fetched: now, ttl: geminiCatalogTTL, thinking: map[string]bool{}}
		infos, lerr := g.listModelInfos(ctx)
		switch {
		case lerr == nil:
			for _, m := range infos {
				if m != nil {
					cat.thinking[geminiModelID(m.Name)] = m.Thinking
				}
			}
		case errors.Is(lerr, context.Canceled):
			return false, lerr
		default:
			cat.ttl = geminiCatalogFailTTL
		}
		geminiCatalogMu.Lock()
		if len(geminiCatalogCache) >= geminiCatalogMaxKeys {
			for k, v := range geminiCatalogCache {
				if now.Sub(v.fetched) >= v.ttl {
					delete(geminiCatalogCache, k)
				}
			}
			if len(geminiCatalogCache) >= geminiCatalogMaxKeys {
				for k := range geminiCatalogCache { // still full: drop an arbitrary entry
					delete(geminiCatalogCache, k)
					break
				}
			}
		}
		geminiCatalogCache[g.catalogKey] = cat
		geminiCatalogMu.Unlock()
	}
	return cat.thinking[geminiModelID(modelID)], nil
}

// ---- request mapping ----

func geminiThinkingLevel(effort string) (genai.ThinkingLevel, bool) {
	switch effort {
	case "low":
		return genai.ThinkingLevelLow, true
	case "medium":
		return genai.ThinkingLevelMedium, true
	case "high":
		return genai.ThinkingLevelHigh, true
	}
	return "", false
}

func geminiArgsMap(raw string) (map[string]any, error) {
	norm, err := normalizeArgs([]byte(raw))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(norm), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func geminiDecodeSignature(sig string) ([]byte, error) {
	if sig == "" {
		return nil, nil
	}
	if len(sig) > geminiMaxSignature {
		return nil, fmt.Errorf("%w: tool call signature is too large", iface.ErrLLMInvalidRequest)
	}
	b, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return nil, fmt.Errorf("%w: tool call signature is not valid base64", iface.ErrLLMInvalidRequest)
	}
	return b, nil
}

func geminiBypassSignature() []byte {
	b, _ := base64.URLEncoding.DecodeString(geminiSkipSignature)
	return b
}

func buildGeminiContents(req iface.ChatRequest) (*genai.Content, []*genai.Content, error) {
	var (
		system   []string
		contents []*genai.Content
		results  []*genai.Part // consecutive tool results share one user turn
		names    = map[string]string{}
	)
	flush := func() {
		if len(results) > 0 {
			contents = append(contents, genai.NewContentFromParts(results, genai.RoleUser))
			results = nil
		}
	}
	addSystem := func(s string) {
		if strings.TrimSpace(s) != "" {
			system = append(system, s)
		}
	}
	addSystem(req.Instructions)
	// Gemini 3 validates function-call signatures only in the current turn:
	// everything after the last user message.
	currentTurn := -1
	for i, m := range req.Messages {
		if m.Role == "user" {
			currentTurn = i
		}
	}
	for mi, m := range req.Messages {
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
			contents = append(contents, genai.NewContentFromText(m.Content, genai.RoleUser))
		case "assistant":
			var parts []*genai.Part
			if strings.TrimSpace(m.Content) != "" {
				parts = append(parts, genai.NewPartFromText(m.Content))
			}
			for ci, tc := range m.ToolCalls {
				if tc.ID == "" || tc.Name == "" {
					return nil, nil, fmt.Errorf("%w: tool call needs an id and a name", iface.ErrLLMInvalidRequest)
				}
				args, err := geminiArgsMap(tc.Arguments)
				if err != nil {
					return nil, nil, fmt.Errorf("%w: tool call arguments are not valid JSON", iface.ErrLLMInvalidRequest)
				}
				sig, err := geminiDecodeSignature(tc.Signature)
				if err != nil {
					return nil, nil, err
				}
				// Only the first call of a parallel step carries a signature;
				// in the current turn it must have one.
				if sig == nil && ci == 0 && mi > currentTurn {
					sig = geminiBypassSignature()
				}
				names[tc.ID] = tc.Name
				parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: tc.ID, Name: tc.Name, Args: args}, ThoughtSignature: sig})
			}
			if len(parts) > 0 { // the API refuses empty content
				contents = append(contents, genai.NewContentFromParts(parts, genai.RoleModel))
			}
		case "tool":
			if m.ToolCallID == "" {
				return nil, nil, fmt.Errorf("%w: tool result needs a tool call id", iface.ErrLLMInvalidRequest)
			}
			name := names[m.ToolCallID]
			if name == "" {
				name = m.Name
			}
			if name == "" { // Gemini answers a call by function name
				return nil, nil, fmt.Errorf("%w: tool result does not match a known tool call", iface.ErrLLMInvalidRequest)
			}
			part := genai.NewPartFromFunctionResponse(name, map[string]any{"result": m.Content})
			part.FunctionResponse.ID = m.ToolCallID
			results = append(results, part)
		default:
			return nil, nil, fmt.Errorf("%w: unsupported message role", iface.ErrLLMInvalidRequest)
		}
	}
	flush()
	var sys *genai.Content
	if len(system) > 0 {
		sys = genai.NewContentFromText(strings.Join(system, "\n\n"), genai.RoleUser)
	}
	return sys, contents, nil
}

func (g *Gemini) buildConfig(ctx context.Context, call ChatCall) (*genai.GenerateContentConfig, []*genai.Content, error) {
	req := call.Request
	system, contents, err := buildGeminiContents(req)
	if err != nil {
		return nil, nil, err
	}
	cfg := &genai.GenerateContentConfig{SystemInstruction: system}
	if t := firstFloat(req.Options.Temperature, call.Defaults.Temperature); t != nil {
		cfg.Temperature = genai.Ptr(float32(*t))
	}
	if v := firstInt(req.Options.MaxOutputTokens, call.Defaults.MaxOutputTokens); v != nil {
		cfg.MaxOutputTokens = int32(min(*v, math.MaxInt32))
	}
	if len(req.Tools) > 0 {
		decls := make([]*genai.FunctionDeclaration, 0, len(req.Tools))
		for _, t := range req.Tools {
			d := &genai.FunctionDeclaration{Name: t.Name, Description: t.Description}
			if len(t.InputSchema) > 0 {
				d.ParametersJsonSchema = t.InputSchema
			}
			decls = append(decls, d)
		}
		cfg.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
	}
	if rf := req.ResponseFormat; rf != nil {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseJsonSchema = rf.Schema
	}
	// Thinking level only for models whose discovered metadata declares
	// thinking; otherwise the parameter is omitted rather than risking a
	// rejected request.
	if effort := firstString(req.Options.Effort, call.Defaults.Effort); effort != "" {
		if level, ok := geminiThinkingLevel(effort); ok {
			supported, err := g.supportsThinking(ctx, call.ModelID)
			if err != nil {
				return nil, nil, err
			}
			if supported {
				cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: level}
			}
		}
	}
	return cfg, contents, nil
}

// ---- chat ----

func geminiFinishReason(r genai.FinishReason, emittedTools int) string {
	switch r {
	case genai.FinishReasonMaxTokens:
		return "length"
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonBlocklist,
		genai.FinishReasonProhibitedContent, genai.FinishReasonSPII, genai.FinishReasonImageSafety,
		genai.FinishReasonImageProhibitedContent, genai.FinishReasonImageRecitation:
		return "content_filter"
	case genai.FinishReasonStop, genai.FinishReasonUnspecified:
		if emittedTools > 0 {
			return "tool_calls"
		}
		return "stop"
	}
	return "other" // OTHER, MALFORMED_FUNCTION_CALL, LANGUAGE, ... : the model's outcome, not a provider fault
}

func (g *Gemini) Chat(ctx context.Context, call ChatCall, sink iface.ChatSink) (*ChatOutcome, error) {
	cfg, contents, err := g.buildConfig(ctx, call)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, transportErr(ctx, err)
		}
		return nil, err
	}
	sctx, httpErr := geminiCall(WithStreaming(ctx))

	var (
		usage    iface.LLMUsage
		finish   genai.FinishReason
		finished bool
		emitted  int
		outBytes int
		argBytes int
	)
	for resp, err := range g.client.Models.GenerateContentStream(sctx, call.ModelID, contents, cfg) {
		if err != nil {
			if httpErr.Status != 0 {
				return nil, Classify(*httpErr)
			}
			return nil, classifyGeminiSDKError(ctx, err)
		}
		if resp == nil {
			continue
		}
		if u := resp.UsageMetadata; u != nil {
			if in := int(u.PromptTokenCount) + int(u.ToolUsePromptTokenCount); in > usage.InputTokens {
				usage.InputTokens = in
			}
			if out := int(u.CandidatesTokenCount) + int(u.ThoughtsTokenCount); out > usage.OutputTokens {
				usage.OutputTokens = out
			}
		}
		if pf := resp.PromptFeedback; pf != nil && pf.BlockReason != "" && len(resp.Candidates) == 0 {
			finish, finished = genai.FinishReasonSafety, true // the prompt itself was refused
			continue
		}
		if len(resp.Candidates) == 0 || resp.Candidates[0] == nil {
			continue
		}
		cand := resp.Candidates[0]
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if part == nil || part.Thought {
					continue
				}
				if fc := part.FunctionCall; fc != nil {
					if emitted >= oaiMaxToolCalls || fc.Name == "" {
						return nil, malformed("tool call")
					}
					raw, merr := json.Marshal(fc.Args)
					args, nerr := normalizeArgs(raw)
					if merr != nil || nerr != nil {
						return nil, malformed("tool call")
					}
					if argBytes += len(args); argBytes > oaiMaxToolArgsLen {
						return nil, malformed("tool call")
					}
					id := fc.ID
					if id == "" {
						id = "call_" + strconv.Itoa(emitted)
					}
					emitted++
					outBytes += len(fc.Name) + len(args)
					tc := &iface.ChatToolCall{ID: id, Name: fc.Name, Arguments: args}
					if sig := base64.StdEncoding.EncodeToString(part.ThoughtSignature); len(part.ThoughtSignature) > 0 && len(sig) <= geminiMaxSignature {
						tc.Signature = sig // a larger one is dropped: the call is still delivered
					}
					if err := sink(iface.ChatEvent{ToolCall: tc}); err != nil {
						return nil, err
					}
					continue
				}
				if part.Text == "" {
					continue
				}
				outBytes += len(part.Text)
				if err := sink(iface.ChatEvent{Delta: part.Text}); err != nil {
					return nil, err
				}
			}
		}
		if cand.FinishReason != "" {
			finish, finished = cand.FinishReason, true
		}
	}
	if !finished {
		if ctx.Err() != nil {
			return nil, transportErr(ctx, ctx.Err())
		}
		return nil, fmt.Errorf("%w: stream ended before completion", iface.ErrLLMProviderUnavailable)
	}

	outcome := &ChatOutcome{FinishReason: geminiFinishReason(finish, emitted)}
	outcome.Usage = ensureUsage(call.Request, usage, outBytes)
	if err := sink(iface.ChatEvent{Done: true, Usage: &outcome.Usage}); err != nil {
		return nil, err
	}
	return outcome, nil
}

// geminiStatusFromRPC maps the gRPC-style status name of an error delivered
// inside a stream onto the HTTP status the same error carries on a plain
// response, so Classify treats both alike.
func geminiStatusFromRPC(code int, status string) int {
	if code >= 400 {
		return code
	}
	switch status {
	case "INVALID_ARGUMENT", "FAILED_PRECONDITION", "OUT_OF_RANGE":
		return http.StatusBadRequest
	case "UNAUTHENTICATED":
		return http.StatusUnauthorized
	case "PERMISSION_DENIED":
		return http.StatusForbidden
	case "NOT_FOUND":
		return http.StatusNotFound
	case "RESOURCE_EXHAUSTED":
		return http.StatusTooManyRequests
	case "DEADLINE_EXCEEDED":
		return http.StatusGatewayTimeout
	case "UNAVAILABLE":
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway // INTERNAL and anything unknown
}

// classifyGeminiSDKError turns a non-HTTP SDK failure into a sentinel without
// ever returning the SDK's text (it can carry the provider message or raw
// stream bytes). Only allowlisted identifiers of a genai.APIError are read.
func classifyGeminiSDKError(ctx context.Context, err error) error {
	if e := sdkTransportFailure(ctx, err); e != nil {
		return e
	}
	if errors.Is(err, ErrSSEEventTooLarge) {
		return malformed("stream event")
	}
	var apierr genai.APIError
	if errors.As(err, &apierr) {
		typ := identifier(apierr.Status)
		return Classify(ErrorInfo{Status: geminiStatusFromRPC(apierr.Code, typ), Type: typ, QuotaID: QuotaIDFromDetails(apierr.Details)})
	}
	return sdkFallbackFailure(ctx, err)
}

// ---- embeddings ----

func (g *Gemini) Embed(ctx context.Context, call EmbedCall) (*EmbedOutcome, error) {
	out := &EmbedOutcome{Vectors: make([][]float32, 0, len(call.Inputs))}
	for start := 0; start < len(call.Inputs); start += geminiEmbedBatchSize {
		end := min(start+geminiEmbedBatchSize, len(call.Inputs))
		contents := make([]*genai.Content, 0, end-start)
		for _, in := range call.Inputs[start:end] {
			contents = append(contents, genai.NewContentFromText(in, genai.RoleUser))
		}
		cctx, httpErr := geminiCall(ctx)
		res, err := g.client.Models.EmbedContent(cctx, call.ModelID, contents, nil)
		if httpErr.Status != 0 {
			return nil, Classify(*httpErr)
		}
		if err != nil {
			return nil, classifyGeminiSDKError(ctx, err)
		}
		if res == nil || len(res.Embeddings) != len(contents) {
			return nil, malformed("embeddings response")
		}
		for _, e := range res.Embeddings {
			if e == nil || len(e.Values) == 0 {
				return nil, malformed("embeddings response")
			}
			out.Vectors = append(out.Vectors, e.Values)
		}
	}
	// The embedding API reports no token count: conservative estimate.
	out.Usage = ensureEmbedUsage(call.Inputs, iface.LLMUsage{})
	return out, nil
}

// ---- models / health ----

func (g *Gemini) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
	infos, err := g.listModelInfos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelDescriptor, 0, len(infos))
	for _, m := range infos {
		if m == nil {
			continue
		}
		id := geminiModelID(m.Name)
		if id == "" || !geminiServesModel(m.SupportedActions) {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = id
		}
		out = append(out, ModelDescriptor{ID: id, DisplayName: name})
	}
	return out, nil
}

// geminiServesModel: the model can be used for chat or embeddings.
func geminiServesModel(actions []string) bool {
	for _, a := range actions {
		switch a {
		case "generateContent", "embedContent", "batchEmbedContents":
			return true
		}
	}
	return false
}

func (g *Gemini) Health(ctx context.Context) error {
	_, err := g.ListModels(ctx)
	return err
}
