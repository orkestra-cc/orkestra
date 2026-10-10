package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		_, _ = w.Write([]byte("data: " + e + "\n\n"))
	}
}

func oaiServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, Provider) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := NewOpenAICompatible(Credential{BaseURL: srv.URL + "/v1", Secret: "sk-test"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return srv, p
}

func TestOpenAICompatible_StreamsTextAndUsage(t *testing.T) {
	var body map[string]any
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sse(w,
			`{"choices":[{"delta":{"role":"assistant","content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":3}}`,
			`[DONE]`)
	})
	temp, maxTok := 0.5, 100
	req := userReq("hi")
	req.Instructions = "sys"
	req.Options.Temperature = &temp
	var text strings.Builder
	var done *iface.LLMUsage
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "qwen", Request: req, Defaults: ChatDefaults{MaxOutputTokens: &maxTok}}, func(e iface.ChatEvent) error {
		text.WriteString(e.Delta)
		if e.Done {
			done = e.Usage
		}
		return nil
	})
	if err != nil || text.String() != "Hello" || outcome.FinishReason != "stop" || outcome.Usage.InputTokens != 9 || outcome.Usage.OutputTokens != 3 || done == nil || *done != outcome.Usage {
		t.Fatalf("text=%q outcome=%+v done=%+v err=%v", text.String(), outcome, done, err)
	}
	if body["stream"] != true || body["stream_options"].(map[string]any)["include_usage"] != true ||
		body["temperature"] != 0.5 || body["max_tokens"] != float64(100) || body["model"] != "qwen" {
		t.Fatalf("body = %v", body)
	}
	msgs := body["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["role"] != "user" {
		t.Fatalf("messages = %v", msgs)
	}
}

func TestOpenAICompatible_AccumulatesStreamedToolCallsByIndex(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"lookup","arguments":""}},{"index":1,"id":"call_b","function":{"name":"other","arguments":"{\"z\""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}},{"index":1,"function":{"arguments":":2}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":7}}`,
			`[DONE]`)
	})
	var calls []iface.ChatToolCall
	var order []string
	req := userReq("x")
	req.Tools = []iface.ChatToolSpec{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			calls = append(calls, *e.ToolCall)
			order = append(order, "tool")
		}
		if e.Done {
			order = append(order, "done")
		}
		return nil
	})
	if err != nil || len(calls) != 2 || outcome.FinishReason != "tool_calls" || outcome.Usage.OutputTokens != 7 {
		t.Fatalf("calls=%+v outcome=%+v err=%v", calls, outcome, err)
	}
	if calls[0] != (iface.ChatToolCall{ID: "call_a", Name: "lookup", Arguments: `{"q":"x"}`}) ||
		calls[1] != (iface.ChatToolCall{ID: "call_b", Name: "other", Arguments: `{"z":2}`}) {
		t.Fatalf("calls = %+v", calls)
	}
	if strings.Join(order, ",") != "tool,tool,done" {
		t.Fatalf("event order = %v", order)
	}
}

func TestOpenAICompatible_ToolCallsFlushedWhenFinishReasonMissing(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"noargs"}}]}}]}`, `[DONE]`)
	})
	var calls []iface.ChatToolCall
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			calls = append(calls, *e.ToolCall)
		}
		return nil
	})
	if err != nil || len(calls) != 1 || calls[0].Arguments != "{}" || calls[0].ID != "call_0" || outcome.FinishReason != "tool_calls" {
		t.Fatalf("calls=%+v outcome=%+v err=%v", calls, outcome, err)
	}
	// No usage reported: the module-wide estimate is used, never zero.
	if outcome.Usage.InputTokens == 0 {
		t.Fatalf("usage = %+v", outcome.Usage)
	}
}

func TestOpenAICompatible_FinishReasons(t *testing.T) {
	for in, want := range map[string]string{"stop": "stop", "length": "length", "content_filter": "content_filter", "weird": "other"} {
		_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
			sse(w, `{"choices":[{"delta":{"content":"x"},"finish_reason":"`+in+`"}]}`, `[DONE]`)
		})
		outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return nil })
		if err != nil || outcome.FinishReason != want {
			t.Fatalf("%s: outcome=%+v err=%v", in, outcome, err)
		}
	}
}

func TestOpenAICompatible_RequestMapping(t *testing.T) {
	var body map[string]any
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		sse(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, `[DONE]`)
	})
	req := iface.ChatRequest{Caller: "t",
		Tools:          []iface.ChatToolSpec{{Name: "lookup", Description: "d", InputSchema: map[string]any{"type": "object"}}},
		ResponseFormat: &iface.JSONSchemaFormat{Name: "out", Schema: map[string]any{"type": "object"}, Strict: true},
		Messages: []iface.ChatMessage{
			{Role: "developer", Content: "dev"},
			{Role: "assistant", ToolCalls: []iface.ChatToolCall{{ID: "c1", Name: "lookup", Arguments: `{"q":1}`}}},
			{Role: "tool", ToolCallID: "c1", Name: "lookup", Content: "42"},
		}}
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, func(iface.ChatEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	msgs := body["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("developer not mapped: %v", msgs[0])
	}
	tc := msgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if tc["id"] != "c1" || tc["type"] != "function" || tc["function"].(map[string]any)["arguments"] != `{"q":1}` {
		t.Fatalf("assistant tool call = %v", tc)
	}
	if m := msgs[2].(map[string]any); m["role"] != "tool" || m["tool_call_id"] != "c1" || m["content"] != "42" {
		t.Fatalf("tool message = %v", m)
	}
	fn := body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "lookup" || fn["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("tools = %v", body["tools"])
	}
	rf := body["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if rf["type"] != "json_schema" || js["name"] != "out" || js["strict"] != true {
		t.Fatalf("response_format = %v", rf)
	}
	if _, ok := body["temperature"]; ok {
		t.Fatal("temperature sent although unset")
	}
}

func TestOpenAICompatible_RefusesEffort(t *testing.T) {
	_, p := oaiServer(t, func(http.ResponseWriter, *http.Request) { t.Error("must not call the server") })
	req := userReq("x")
	req.Options.Effort = "low"
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, func(iface.ChatEvent) error { return nil }); !errors.Is(err, iface.ErrLLMCapabilityMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAICompatible_HTTPErrorsAreClassified(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   []error
	}{
		{"unauthorized", 401, `{"error":{"message":"LEAK"}}`, []error{ErrUnauthorized, iface.ErrLLMProviderUnavailable}},
		{"quota", 429, `{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"LEAK"}}`, []error{iface.ErrLLMQuotaExhausted}},
		{"rate limit", 429, `{"error":{"type":"rate_limit_error","message":"LEAK"}}`, []error{iface.ErrLLMProviderUnavailable}},
		{"context length", 400, `{"error":{"code":"context_length_exceeded","message":"LEAK"}}`, []error{iface.ErrLLMCapabilityMismatch}},
		{"server", 502, `<html>LEAK</html>`, []error{iface.ErrLLMProviderUnavailable}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return nil })
			for _, w := range tc.want {
				if !errors.Is(err, w) {
					t.Fatalf("err = %v, want %v", err, w)
				}
			}
			if err == nil || strings.Contains(err.Error(), "LEAK") {
				t.Fatalf("error leaks body text: %v", err)
			}
		})
	}
}

func TestOpenAICompatible_StreamFailures(t *testing.T) {
	cases := map[string][]string{
		"truncated":    {`{"choices":[{"delta":{"content":"par"}}]}`},
		"malformed":    {`{not json`},
		"inline error": {`{"error":{"message":"SECRET-DETAIL"}}`},
		"bad tool args": {
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"{\"q\":"}}]},"finish_reason":"tool_calls"}]}`,
		},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) { sse(w, events...) })
			_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return nil })
			if !errors.Is(err, iface.ErrLLMProviderUnavailable) || strings.Contains(err.Error(), "SECRET-DETAIL") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestOpenAICompatible_SinkErrorAndOversizedEvent(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"a"}}]}`, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, `[DONE]`)
	})
	boom := errors.New("consumer gone")
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("sink err = %v", err)
	}
	_, big := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"`+strings.Repeat("a", oaiMaxEvent+10)+`"}}]}`)
	})
	if _, err := big.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return nil }); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("oversized err = %v", err)
	}
}

func TestOpenAICompatible_EmbedAndModels(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["model"] != "bge" || len(b["input"].([]any)) != 2 {
				t.Errorf("embed body = %v", b)
			}
			// out of order on purpose: index must win
			_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0.3,0.4]},{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":6}}`))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen"},{"id":"bge"}]}`))
		default:
			w.WriteHeader(404)
		}
	})
	emb, err := p.Embed(context.Background(), EmbedCall{ModelID: "bge", Inputs: []string{"a", "b"}})
	if err != nil || len(emb.Vectors) != 2 || emb.Vectors[0][0] != 0.1 || emb.Vectors[1][1] != 0.4 || emb.Usage.InputTokens != 6 {
		t.Fatalf("embed = %+v, %v", emb, err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "qwen" {
		t.Fatalf("models = %+v, %v", models, err)
	}
	if err := p.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompatible_EmbedRejectsBadShapes(t *testing.T) {
	for name, body := range map[string]string{
		"count mismatch": `{"data":[{"embedding":[0.1]}]}`,
		"dup index":      `{"data":[{"index":0,"embedding":[0.1]},{"index":0,"embedding":[0.2]}]}`,
		"empty vector":   `{"data":[{"embedding":[]},{"embedding":[0.2]}]}`,
		"not json":       `oops`,
	} {
		t.Run(name, func(t *testing.T) {
			_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"a", "b"}}); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestOpenAICompatible_NoSecretSendsNoAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			t.Errorf("Authorization = %q", h)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	p, _ := NewOpenAICompatible(Credential{BaseURL: srv.URL}, srv.Client())
	if err := p.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompatible_CancelIsNotAProviderFault(t *testing.T) {
	_, p := oaiServer(t, func(http.ResponseWriter, *http.Request) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Chat(ctx, ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return nil })
	if !errors.Is(err, context.Canceled) || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterAll_BuildsEveryAdapterKind(t *testing.T) {
	r := NewRegistry()
	RegisterAll(r, func(string) *http.Client { return &http.Client{} })
	for kind, cred := range map[string]Credential{
		"mock": {}, "ollama": {BaseURL: "http://o"}, "openai_compatible": {BaseURL: "http://c", Secret: "k"},
	} {
		p, err := r.Build(kind, cred, nil)
		if err != nil || p.Name() != kind {
			t.Fatalf("%s: %v %v", kind, p, err)
		}
	}
}
