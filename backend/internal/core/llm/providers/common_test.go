package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestNormalizeArgs_RequiresAnObject(t *testing.T) {
	for in, want := range map[string]string{"": "{}", "  ": "{}", "null": "{}", `{}`: "{}", ` {"a":1} `: `{"a":1}`} {
		if got, err := normalizeArgs([]byte(in)); err != nil || got != want {
			t.Errorf("normalizeArgs(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{`5`, `[]`, `"x"`, `"{\"a\":1}"`, `true`, `{"a":`, `{"a":1} x`} {
		if got, err := normalizeArgs([]byte(in)); err == nil {
			t.Errorf("normalizeArgs(%q) = %q, want error", in, got)
		}
	}
}

func TestEnsureUsage(t *testing.T) {
	req := userReq("hello world")
	got := ensureUsage(req, iface.LLMUsage{}, 10)
	if got.InputTokens != EstimateTokens(req) || got.OutputTokens != 4 {
		t.Fatalf("zero usage = %+v", got)
	}
	// A reported side is kept; only the zero side is estimated.
	got = ensureUsage(req, iface.LLMUsage{InputTokens: 50}, 9)
	if got.InputTokens != 50 || got.OutputTokens != 3 {
		t.Fatalf("partial usage = %+v", got)
	}
	if got = ensureUsage(req, iface.LLMUsage{InputTokens: 5, OutputTokens: 6}, 99); got != (iface.LLMUsage{InputTokens: 5, OutputTokens: 6}) {
		t.Fatalf("reported usage altered: %+v", got)
	}
	if e := ensureEmbedUsage([]string{"abcd", "efg"}, iface.LLMUsage{}); e.InputTokens != 3 {
		t.Fatalf("embed usage = %+v", e)
	}
	if e := ensureEmbedUsage([]string{"abcd"}, iface.LLMUsage{InputTokens: 9}); e.InputTokens != 9 {
		t.Fatalf("embed usage = %+v", e)
	}
}

func TestOllama_CachedPromptAndToolOutputAreNotUndercounted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"content":"abc","tool_calls":[{"function":{"name":"lookup","arguments":{"q":"xyz"}}}]},"done":true}` + "\n"))
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	req := userReq("a fairly long prompt that was cached by the server")
	out, _, err := ollamaChat(t, p, req)
	if err != nil {
		t.Fatal(err)
	}
	wantOut := (3 + len("lookup") + len(`{"q":"xyz"}`) + 2) / 3
	if out.Usage.InputTokens != EstimateTokens(req) || out.Usage.OutputTokens != wantOut {
		t.Fatalf("usage = %+v, want in=%d out=%d", out.Usage, EstimateTokens(req), wantOut)
	}
}

func TestOllama_EmbedUsageFallbackAndEmptyVector(t *testing.T) {
	body := `{"embeddings":[[0.1],[0.2]]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	emb, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"abcdef", "ghi"}})
	if err != nil || emb.Usage.InputTokens != 3 {
		t.Fatalf("embed = %+v, %v", emb, err)
	}
	body = `{"embeddings":[[0.1],[]]}`
	if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"a", "b"}}); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("empty vector err = %v", err)
	}
}

func TestOllama_ToolArgumentsMustBeObjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"a","arguments":[1]}}]},"done":true}` + "\n"))
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	if _, _, err := ollamaChat(t, p, userReq("x")); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	// History with a non-object argument is the caller's invalid request.
	req := userReq("x")
	req.Messages = append(req.Messages, iface.ChatMessage{Role: "assistant", ToolCalls: []iface.ChatToolCall{{ID: "c", Name: "a", Arguments: `"str"`}}})
	if _, _, err := ollamaChat(t, p, req); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("history err = %v", err)
	}
}

func TestOllama_DeltasReachSinkBeforeMidStreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"content":"par"},"done":false}` + "\n" + "garbage\n"))
	}))
	defer srv.Close()
	p, _ := NewOllama(Credential{BaseURL: srv.URL}, srv.Client())
	_, evs, err := ollamaChat(t, p, userReq("x"))
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || len(evs) != 1 || evs[0].Delta != "par" {
		t.Fatalf("events=%+v err=%v", evs, err)
	}
}

func TestOpenAICompatible_ZeroUsageIsEstimated(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"content":"abc","tool_calls":[{"index":0,"id":"c","function":{"name":"lookup","arguments":"{\"q\":\"xyz\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`, `[DONE]`)
	})
	req := userReq("some prompt")
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink)
	if err != nil {
		t.Fatal(err)
	}
	wantOut := (3 + len("lookup") + len(`{"q":"xyz"}`) + 2) / 3
	if out.Usage.InputTokens != EstimateTokens(req) || out.Usage.OutputTokens != wantOut {
		t.Fatalf("usage = %+v, want in=%d out=%d", out.Usage, EstimateTokens(req), wantOut)
	}
	// Half-reported usage keeps the reported side.
	_, p2 := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"abcdef"},"finish_reason":"stop"}]}`, `{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":0}}`, `[DONE]`)
	})
	out, err = p2.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink)
	if err != nil || out.Usage.InputTokens != 40 || out.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v err=%v", out.Usage, err)
	}
}

func TestOpenAICompatible_EmbedUsageFallback(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1]},{"embedding":[0.2]}]}`))
	})
	emb, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"abcdef", "ghi"}})
	if err != nil || emb.Usage.InputTokens != 3 {
		t.Fatalf("embed = %+v, %v", emb, err)
	}
}

func TestOpenAICompatible_TruncatedToolCallOnLengthIsNotAProviderFault(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"content":"thinking"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"{\"q\":\"par"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"length"}]}`, `[DONE]`)
	})
	var calls int
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			calls++
		}
		return nil
	})
	if err != nil || calls != 0 || out.FinishReason != "length" {
		t.Fatalf("out=%+v calls=%d err=%v", out, calls, err)
	}
}

func TestOpenAICompatible_DeltasReachSinkBeforeMidStreamFailure(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"par"}}]}`, `{broken`)
	})
	var got []string
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, func(e iface.ChatEvent) error {
		got = append(got, e.Delta)
		return nil
	})
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || strings.Join(got, "") != "par" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestOpenAICompatible_ToolArgumentsMustBeObjects(t *testing.T) {
	_, p := oaiServer(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"[1]"}}]},"finish_reason":"tool_calls"}]}`, `[DONE]`)
	})
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("x")}, noopSink); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAI_ZeroUsageAndEmbedFallbacks(t *testing.T) {
	p, _ := newOAI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m"}`))
			return
		}
		respSSE(w,
			ev("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}`),
			ev("response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"arguments":"{\"q\":\"xyz\"}"}`),
			completedEvent(0, 0))
	}, iface.LLMCredentialKindOrg)
	req := userReq("some prompt")
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink)
	wantOut := (len("lookup") + len(`{"q":"xyz"}`) + 2) / 3
	if err != nil || out.Usage.InputTokens != EstimateTokens(req) || out.Usage.OutputTokens != wantOut {
		t.Fatalf("usage = %+v err=%v want out=%d", out, err, wantOut)
	}
	emb, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"abcdef"}})
	if err != nil || emb.Usage.InputTokens != 2 {
		t.Fatalf("embed = %+v, %v", emb, err)
	}
}

var syntheticCallIDRE = regexp.MustCompile(`^call_[0-9a-f]{24}$`)

// isSyntheticCallID reports whether id is one syntheticCallID minted.
func isSyntheticCallID(id string) bool { return syntheticCallIDRE.MatchString(id) }

// A per-response counter repeats on every step of a tool loop (call_0,
// call_0, ...) and Gemini maps tool results back to calls by ID, so the
// IDs minted for calls a provider returned without one must not repeat.
func TestSyntheticCallIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := syntheticCallID()
		if !isSyntheticCallID(id) {
			t.Fatalf("id %q", id)
		}
		if seen[id] {
			t.Fatalf("id %q repeated", id)
		}
		seen[id] = true
	}
}
