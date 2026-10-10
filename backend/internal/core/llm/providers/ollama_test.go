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

func ollamaChat(t *testing.T, p Provider, req iface.ChatRequest) (*ChatOutcome, []iface.ChatEvent, error) {
	t.Helper()
	var evs []iface.ChatEvent
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "llama3.1", Request: req}, func(e iface.ChatEvent) error {
		evs = append(evs, e)
		return nil
	})
	return out, evs, err
}

func userReq(text string) iface.ChatRequest {
	return iface.ChatRequest{Caller: "t", Messages: []iface.ChatMessage{{Role: "user", Content: text}}}
}

func TestOllama_ChatStreamsNDJSONAndCountsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true || body["model"] != "llama3.1" {
			t.Errorf("body = %v", body)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, line := range []string{
			`{"message":{"role":"assistant","content":"Hel"},"done":false}`,
			`{"message":{"role":"assistant","content":"lo"},"done":false}`,
			`{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":7,"eval_count":2}`,
		} {
			_, _ = w.Write([]byte(line + "\n"))
		}
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	var text strings.Builder
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "llama3.1", Request: userReq("hi")}, func(e iface.ChatEvent) error {
		text.WriteString(e.Delta)
		return nil
	})
	if err != nil || text.String() != "Hello" || outcome.Usage.InputTokens != 7 || outcome.Usage.OutputTokens != 2 || outcome.FinishReason != "stop" {
		t.Fatalf("text=%q outcome=%+v err=%v", text.String(), outcome, err)
	}
}

func TestOllama_ToolCallAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"lookup","arguments":{"q":"x"}}}]},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}` + "\n"))
		case "/api/embed":
			_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2],[0.3,0.4]],"prompt_eval_count":4}`))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.1:latest"}]}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"model not found"}`))
		}
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	var tc *iface.ChatToolCall
	req := userReq("x")
	req.Tools = []iface.ChatToolSpec{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "llama3.1", Request: req}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			tc = e.ToolCall
		}
		return nil
	})
	if err != nil || tc == nil || tc.Name != "lookup" || tc.Arguments != `{"q":"x"}` || tc.ID == "" || outcome.FinishReason != "tool_calls" {
		t.Fatalf("tc=%+v outcome=%+v err=%v", tc, outcome, err)
	}
	emb, err := p.Embed(context.Background(), EmbedCall{ModelID: "nomic", Inputs: []string{"a", "b"}})
	if err != nil || len(emb.Vectors) != 2 || emb.Usage.InputTokens != 4 {
		t.Fatalf("embed = %+v, %v", emb, err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "llama3.1:latest" {
		t.Fatalf("models = %+v, %v", models, err)
	}
	// Unsupported explicit option is refused, not ignored.
	eff := userReq("x")
	eff.Options.Effort = "high"
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "llama3.1", Request: eff}, func(iface.ChatEvent) error { return nil }); !errors.Is(err, iface.ErrLLMCapabilityMismatch) {
		t.Fatalf("effort err = %v", err)
	}
	// ...and so is a model-level default effort.
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "llama3.1", Request: userReq("x"), Defaults: ChatDefaults{Effort: "low"}}, func(iface.ChatEvent) error { return nil }); !errors.Is(err, iface.ErrLLMCapabilityMismatch) {
		t.Fatalf("default effort err = %v", err)
	}
}

func TestOllama_RequestMappingAndAuth(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"message":{"content":"ok"},"done":true,"done_reason":"length","prompt_eval_count":1,"eval_count":1}` + "\n"))
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL + "/", Secret: "k-123"}, srv.Client())
	temp, maxTok := 0.2, 64
	req := iface.ChatRequest{Caller: "t", Instructions: "be brief", Options: iface.ChatOptions{Temperature: &temp},
		ResponseFormat: &iface.JSONSchemaFormat{Name: "r", Schema: map[string]any{"type": "object"}},
		Messages: []iface.ChatMessage{
			{Role: "developer", Content: "dev note"},
			{Role: "user", Content: "call it"},
			{Role: "assistant", ToolCalls: []iface.ChatToolCall{{ID: "c1", Name: "lookup", Arguments: `{"q":1}`}}},
			{Role: "tool", ToolCallID: "c1", Name: "lookup", Content: "42"},
		}}
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req, Defaults: ChatDefaults{MaxOutputTokens: &maxTok}}, func(iface.ChatEvent) error { return nil })
	if err != nil || outcome.FinishReason != "length" {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if auth != "Bearer k-123" {
		t.Fatalf("auth = %q", auth)
	}
	opts := got["options"].(map[string]any)
	if opts["temperature"] != 0.2 || opts["num_predict"] != float64(64) {
		t.Fatalf("options = %v", opts)
	}
	if got["format"].(map[string]any)["type"] != "object" {
		t.Fatalf("format = %v", got["format"])
	}
	msgs := got["messages"].([]any)
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "system,system,user,assistant,tool" {
		t.Fatalf("roles = %v", roles)
	}
	asst := msgs[3].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if asst["name"] != "lookup" || asst["arguments"].(map[string]any)["q"] != float64(1) {
		t.Fatalf("assistant tool call = %v", asst)
	}
	if msgs[4].(map[string]any)["tool_name"] != "lookup" {
		t.Fatalf("tool message = %v", msgs[4])
	}
}

func TestOllama_ToolCallArgumentsNormalised(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"tool_calls":[{"id":"srv-1","function":{"name":"a"}},{"function":{"name":"b","arguments":"{\"x\":1}"}}]},"done":true}` + "\n"))
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	var calls []iface.ChatToolCall
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			calls = append(calls, *e.ToolCall)
		}
		return nil
	})
	if err != nil || len(calls) != 2 || calls[0].ID != "srv-1" || calls[0].Arguments != "{}" || calls[1].ID != "call_2" || calls[1].Arguments != `{"x":1}` {
		t.Fatalf("calls=%+v err=%v", calls, err)
	}
}

func TestOllama_StreamFailuresAreClassified(t *testing.T) {
	cases := map[string]string{
		"truncated":    `{"message":{"content":"par"},"done":false}` + "\n",
		"malformed":    "not json\n",
		"inline error": `{"error":"SECRET-DETAIL boom"}` + "\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer srv.Close()
			p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
			_, _, err := ollamaChat(t, p, userReq("x"))
			if !errors.Is(err, iface.ErrLLMProviderUnavailable) || strings.Contains(err.Error(), "SECRET-DETAIL") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestOllama_HTTPErrorsAreClassifiedWithoutBodyText(t *testing.T) {
	for status, want := range map[int]error{
		401: ErrUnauthorized, 404: iface.ErrLLMCapabilityMismatch, 429: iface.ErrLLMProviderUnavailable, 503: iface.ErrLLMProviderUnavailable, 400: iface.ErrLLMInvalidRequest,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"LEAK secret prompt text"}`))
		}))
		p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
		_, _, err := ollamaChat(t, p, userReq("x"))
		srv.Close()
		if !errors.Is(err, want) || strings.Contains(err.Error(), "LEAK") {
			t.Fatalf("status %d: err = %v", status, err)
		}
		if status == 401 && !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Fatalf("401 must also wrap ErrLLMProviderUnavailable: %v", err)
		}
	}
}

func TestOllama_NetworkErrorAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	client := srv.Client()
	srv.Close() // connection refused from now on
	p, _ := NewOllama(Credential{BaseURL: url}, client)
	_, _, err := ollamaChat(t, p, userReq("x"))
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || strings.Contains(err.Error(), url) {
		t.Fatalf("network err = %v (must not echo the URL)", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Chat(ctx, ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return nil })
	if !errors.Is(err, context.Canceled) || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("cancel err = %v (must be context.Canceled and not a provider fault)", err)
	}
}

func TestOllama_SinkErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"content":"a"},"done":false}` + "\n" + `{"done":true}` + "\n"))
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	boom := errors.New("consumer gone")
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(iface.ChatEvent) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestOllama_ConstructorRequiresClientAndBaseURL(t *testing.T) {
	if _, err := NewOllama(Credential{BaseURL: "http://x"}, nil); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := NewOllama(Credential{}, http.DefaultClient); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("empty base url err = %v", err)
	}
}
