package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// respSSE writes Responses-API server-sent events ("event:" + "data:" pairs).
func respSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		_, _ = w.Write([]byte(e + "\n\n"))
	}
}

func ev(typ, data string) string { return "event: " + typ + "\ndata: " + data }

func completedEvent(in, out int) string {
	return ev("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":`+itoa(in)+`,"output_tokens":`+itoa(out)+`,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`)
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func newOAI(t *testing.T, h http.HandlerFunc, kind string) (Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := NewOpenAI(Credential{Kind: kind, Secret: "sk-test", BaseURL: srv.URL + "/v1"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return p, srv
}

func noopSink(iface.ChatEvent) error { return nil }

func TestOpenAI_ResponsesStreamTextToolsUsage(t *testing.T) {
	var captured map[string]any
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		respSSE(w,
			ev("response.created", `{"type":"response.created","response":{"id":"resp_1"}}`),
			ev("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hel"}`),
			ev("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"lo"}`),
			ev("response.output_item.added", `{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}`),
			ev("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"{\"q\""}`),
			ev("response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":1,"arguments":"{\"q\":\"x\"}"}`),
			completedEvent(12, 5),
		)
	}, iface.LLMCredentialKindOrg)
	var text strings.Builder
	var tcs []*iface.ChatToolCall
	var done *iface.LLMUsage
	temp := 0.3
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "gpt-5.6-terra", Request: iface.ChatRequest{
		Caller: "t", Instructions: "be brief",
		Messages:       []iface.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:          []iface.ChatToolSpec{{Name: "lookup", Description: "d", InputSchema: map[string]any{"type": "object"}}},
		ResponseFormat: &iface.JSONSchemaFormat{Name: "out", Schema: map[string]any{"type": "object"}, Strict: true},
		Options:        iface.ChatOptions{Temperature: &temp, Effort: "low"},
	}}, func(e iface.ChatEvent) error {
		text.WriteString(e.Delta)
		if e.ToolCall != nil {
			tcs = append(tcs, e.ToolCall)
		}
		if e.Done {
			done = e.Usage
		}
		return nil
	})
	if err != nil || text.String() != "Hello" || len(tcs) != 1 || tcs[0].Name != "lookup" || tcs[0].ID != "call_1" || tcs[0].Arguments != `{"q":"x"}` {
		t.Fatalf("text=%q tcs=%+v err=%v", text.String(), tcs, err)
	}
	if outcome.Usage.InputTokens != 12 || outcome.Usage.OutputTokens != 5 || outcome.FinishReason != "tool_calls" || done == nil || *done != outcome.Usage {
		t.Fatalf("outcome = %+v done=%v", outcome, done)
	}
	if captured["store"] != false || captured["stream"] != true || captured["instructions"] != "be brief" || captured["temperature"] != 0.3 || captured["model"] != "gpt-5.6-terra" {
		t.Fatalf("request = %v", captured)
	}
	if reasoning, _ := captured["reasoning"].(map[string]any); reasoning["effort"] != "low" {
		t.Fatalf("reasoning = %v", captured["reasoning"])
	}
	tools, _ := captured["tools"].([]any)
	tool, _ := tools[0].(map[string]any)
	if len(tools) != 1 || tool["type"] != "function" || tool["name"] != "lookup" || tool["strict"] != false {
		t.Fatalf("tools = %v", captured["tools"])
	}
	format := captured["text"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["name"] != "out" || format["strict"] != true {
		t.Fatalf("text.format = %v", format)
	}
}

func TestOpenAI_ConversationInputMapping(t *testing.T) {
	var captured map[string]any
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		respSSE(w, completedEvent(1, 1))
	}, iface.LLMCredentialKindOrg)
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: iface.ChatRequest{Caller: "t", Messages: []iface.ChatMessage{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "calling", ToolCalls: []iface.ChatToolCall{{ID: "c1", Name: "lookup", Arguments: ""}}},
		{Role: "tool", ToolCallID: "c1", Content: "result"},
		{Role: "developer", Content: "rule"},
	}}}, noopSink)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(captured["input"])
	want := []string{
		`"role":"user"`, `"content":"q"`, `"role":"assistant"`, `"content":"calling"`,
		`"type":"function_call"`, `"call_id":"c1"`, `"name":"lookup"`, `"arguments":"{}"`,
		`"type":"function_call_output"`, `"output":"result"`, `"role":"developer"`,
	}
	for _, w := range want {
		if !strings.Contains(string(b), w) {
			t.Errorf("input lacks %s: %s", w, b)
		}
	}
	// An unknown role is refused locally, nothing is sent.
	_, err = p.Chat(context.Background(), ChatCall{ModelID: "m", Request: iface.ChatRequest{Messages: []iface.ChatMessage{{Role: "wizard", Content: "x"}}}}, noopSink)
	if !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Errorf("unknown role err = %v", err)
	}
}

func TestOpenAI_IncompleteMapsFinishReason(t *testing.T) {
	cases := map[string]string{"max_output_tokens": "length", "content_filter": "content_filter", "other_thing": "other"}
	for reason, want := range cases {
		p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
			respSSE(w,
				ev("response.output_text.delta", `{"type":"response.output_text.delta","delta":"par"}`),
				ev("response.incomplete", `{"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"`+reason+`"},"output":[],"usage":{"input_tokens":7,"output_tokens":3,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`))
		}, iface.LLMCredentialKindOrg)
		out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
		if err != nil || out.FinishReason != want || out.Usage.InputTokens != 7 || out.Usage.OutputTokens != 3 {
			t.Errorf("%s: out=%+v err=%v want %s", reason, out, err, want)
		}
	}
}

func TestOpenAI_MissingUsageFallsBackToEstimate(t *testing.T) {
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		respSSE(w,
			ev("response.output_text.delta", `{"type":"response.output_text.delta","delta":"abcdef"}`),
			ev("response.completed", `{"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}`))
	}, iface.LLMCredentialKindOrg)
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hello world")}, noopSink)
	if err != nil || out.Usage.InputTokens <= 0 || out.Usage.OutputTokens <= 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestOpenAI_ToolCallWithoutArgumentsDoneUsesItemDone(t *testing.T) {
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		respSSE(w,
			ev("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"noargs","arguments":""}}`),
			ev("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"noargs","arguments":""}}`),
			completedEvent(1, 1))
	}, iface.LLMCredentialKindOrg)
	var got []*iface.ChatToolCall
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			got = append(got, e.ToolCall)
		}
		return nil
	})
	if err != nil || len(got) != 1 || got[0].Arguments != "{}" || got[0].ID != "call_1" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestOpenAI_FailedEventAndHTTPErrorsAreClassified(t *testing.T) {
	mode := "failed"
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "failed":
			respSSE(w, ev("response.failed", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"insufficient_quota","message":"no credits left for sk-live-x"}}}`))
		case "failed_invalid":
			respSSE(w, ev("response.failed", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"invalid_prompt","message":"sk-live-x"}}}`))
		case "failed_server":
			respSSE(w, ev("response.failed", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"server_error","message":"sk-live-x"}}}`))
		case "error_event":
			respSSE(w, ev("error", `{"type":"error","code":"insufficient_quota","message":"sk-live-x","sequence_number":1}`))
		case "429":
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"sk-live-x"}}`))
		case "401":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invalid_api_key","message":"sk-live-x"}}`))
		case "400":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"sk-live-x"}}`))
		case "500_html":
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`<html>sk-live-x</html>`))
		case "midstream":
			respSSE(w, `data: {"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"sk-live-x"}}`)
		case "truncated":
			respSSE(w, ev("response.output_text.delta", `{"type":"response.output_text.delta","delta":"x"}`))
		}
	}, iface.LLMCredentialKindOrg)
	req := userReq("hi")
	for _, c := range []struct {
		mode string
		want error
	}{
		{"failed", iface.ErrLLMQuotaExhausted},
		{"failed_invalid", iface.ErrLLMInvalidRequest},
		{"failed_server", iface.ErrLLMProviderUnavailable},
		{"error_event", iface.ErrLLMQuotaExhausted},
		{"429", iface.ErrLLMQuotaExhausted},
		{"401", ErrUnauthorized},
		{"401", iface.ErrLLMProviderUnavailable},
		{"400", iface.ErrLLMCapabilityMismatch},
		{"500_html", iface.ErrLLMProviderUnavailable},
		{"midstream", iface.ErrLLMQuotaExhausted},
		{"truncated", iface.ErrLLMProviderUnavailable},
	} {
		mode = c.mode
		_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink)
		if !errors.Is(err, c.want) {
			t.Errorf("mode %s: err = %v, want %v", c.mode, err, c.want)
		}
		if err != nil && (strings.Contains(err.Error(), "sk-live-x") || strings.Contains(err.Error(), "sk-test") || strings.Contains(err.Error(), "127.0.0.1")) {
			t.Errorf("mode %s: provider text or URL leaked into error: %v", c.mode, err)
		}
	}
}

func TestOpenAI_SinkErrorAndCancelPropagate(t *testing.T) {
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		respSSE(w, ev("response.output_text.delta", `{"type":"response.output_text.delta","delta":"x"}`), completedEvent(1, 1))
	}, iface.LLMCredentialKindOrg)
	boom := errors.New("sink boom")
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(iface.ChatEvent) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("sink err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Chat(ctx, ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); !errors.Is(err, context.Canceled) || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("cancel err = %v", err)
	}
}

// The stream must be opted in on the request side: with a tiny response cap
// on the shared client a long stream still passes, which only holds when the
// adapter marks the call with WithStreaming.
func TestOpenAI_StreamIsMarkedStreamingOnTheRequest(t *testing.T) {
	big := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events := []string{}
		for i := 0; i < 8; i++ {
			events = append(events, ev("response.output_text.delta", `{"type":"response.output_text.delta","delta":"`+big+`"}`))
		}
		respSSE(w, append(events, completedEvent(1, 1))...)
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, MaxResponseBytes: 1024, IdleTimeout: 5 * time.Second})
	p, err := NewOpenAI(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k", BaseURL: srv.URL + "/v1"}, client)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	_, err = p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error { n += len(e.Delta); return nil })
	if err != nil || n != 8*4096 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestOpenAI_DoesNotLeakProcessEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-env-key")
	t.Setenv("OPENAI_ORG_ID", "org-env")
	t.Setenv("OPENAI_PROJECT_ID", "proj-env")
	t.Setenv("OPENAI_BASE_URL", "http://env.invalid/v1")
	var hdr http.Header
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		respSSE(w, completedEvent(1, 1))
	}, iface.LLMCredentialKindOrg)
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); err != nil {
		t.Fatal(err)
	}
	if hdr.Get("Authorization") != "Bearer sk-test" || hdr.Get("OpenAI-Organization") != "" || hdr.Get("OpenAI-Project") != "" {
		t.Fatalf("headers = %v", hdr)
	}
}

func TestOpenAI_EmbedAndListModels(t *testing.T) {
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":1,"embedding":[0.3,0.4]},{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":6,"total_tokens":6}}`))
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-terra","object":"model","created":1,"owned_by":"openai"}]}`))
		default:
			w.WriteHeader(404)
		}
	}, iface.LLMCredentialKindOrg)
	emb, err := p.Embed(context.Background(), EmbedCall{ModelID: "text-embedding-3-small", Inputs: []string{"a", "b"}})
	if err != nil || len(emb.Vectors) != 2 || emb.Vectors[0][0] != float32(0.1) || emb.Vectors[1][1] != float32(0.4) || emb.Usage.InputTokens != 6 {
		t.Fatalf("embed = %+v, %v", emb, err)
	}
	if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"a", "b", "c"}}); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("count mismatch err = %v", err)
	}
	ms, err := p.ListModels(context.Background())
	if err != nil || len(ms) != 1 || ms[0].ID != "gpt-5.6-terra" {
		t.Fatalf("models = %+v, %v", ms, err)
	}
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("health = %v", err)
	}
}

func TestOpenAI_ListModelsErrorIsClassified(t *testing.T) {
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`not json sk-live-x`))
	}, iface.LLMCredentialKindOrg)
	err := p.Health(context.Background())
	if !errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "sk-live-x") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAI_SIWCProfile(t *testing.T) {
	var captured map[string]any
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		respSSE(w, completedEvent(1, 1))
	}, iface.LLMCredentialKindUserAccount)
	o, ok := p.(*OpenAI)
	if !ok || o.Profile() != ProfileSIWC || p.Capabilities().Embeddings {
		t.Fatalf("profile/capabilities wrong: %+v", p.Capabilities())
	}
	if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"a"}}); !errors.Is(err, iface.ErrLLMCapabilityMismatch) {
		t.Errorf("embed err = %v", err)
	}
	temp := 0.2
	req := userReq("hi")
	req.Options.Temperature = &temp
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink); !errors.Is(err, iface.ErrLLMCapabilityMismatch) {
		t.Errorf("temperature err = %v", err)
	}
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"temperature", "max_output_tokens", "metadata", "user", "previous_response_id"} {
		if _, present := captured[forbidden]; present {
			t.Errorf("SIWC request carries forbidden field %s", forbidden)
		}
	}
	if captured["store"] != false || captured["stream"] != true {
		t.Errorf("request = %v", captured)
	}
	api, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {}, iface.LLMCredentialKindOrg)
	if !api.Capabilities().Embeddings || api.(*OpenAI).Profile() != ProfileAPIKey {
		t.Error("org credential must use the API-key profile")
	}
}

func TestOpenAI_ConstructorValidation(t *testing.T) {
	if _, err := NewOpenAI(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k"}, nil); err == nil {
		t.Error("nil client must be refused (it would bypass the SSRF guard)")
	}
	if _, err := NewOpenAI(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k", BaseURL: "ftp://x"}, http.DefaultClient); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Errorf("bad scheme err = %v", err)
	}
	p, err := NewOpenAI(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k"}, http.DefaultClient)
	if err != nil || p.Name() != "openai" {
		t.Fatalf("default base: %v", err)
	}
}

func TestOpenAI_RegisteredInRegistry(t *testing.T) {
	r := NewRegistry()
	RegisterAll(r, func(string) *http.Client { return http.DefaultClient })
	p, err := r.Build("openai", Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k"}, nil)
	if err != nil || p.Name() != "openai" {
		t.Fatalf("build: %v", err)
	}
}
