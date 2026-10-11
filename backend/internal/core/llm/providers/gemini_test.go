package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// gemSSE writes streamGenerateContent?alt=sse events ("data:" lines).
func gemSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		_, _ = w.Write([]byte("data: " + e + "\n\n"))
	}
}

func gemText(s string) string {
	b, _ := json.Marshal(s)
	return `{"candidates":[{"content":{"parts":[{"text":` + string(b) + `}],"role":"model"}}]}`
}

func gemFinal(reason string, in, out int) string {
	return `{"candidates":[{"content":{"parts":[{"text":""}],"role":"model"},"finishReason":"` + reason + `"}],"usageMetadata":{"promptTokenCount":` + strconv.Itoa(in) + `,"candidatesTokenCount":` + strconv.Itoa(out) + `}}`
}

func newGem(t *testing.T, h http.HandlerFunc) Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := NewGemini(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "AIza-test", BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGemini_StreamTextFunctionCallUsage(t *testing.T) {
	var captured map[string]any
	var hdr http.Header
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-x:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		hdr = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&captured)
		gemSSE(w,
			gemText("Hel"), gemText("lo"),
			`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}`)
	})
	var text strings.Builder
	var tcs []*iface.ChatToolCall
	var done *iface.LLMUsage
	var order []string
	temp := 0.3
	maxOut := 77
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "gemini-x", Request: iface.ChatRequest{
		Caller: "t", Instructions: "be brief",
		Messages: []iface.ChatMessage{{Role: "developer", Content: "rule"}, {Role: "user", Content: "hi"}},
		Tools: []iface.ChatToolSpec{{Name: "lookup", Description: "d", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}, "required": []any{"q"}, "additionalProperties": false}}},
		ResponseFormat: &iface.JSONSchemaFormat{Name: "out", Schema: map[string]any{"type": "object"}, Strict: true},
		Options:        iface.ChatOptions{Temperature: &temp, MaxOutputTokens: &maxOut},
	}}, func(e iface.ChatEvent) error {
		text.WriteString(e.Delta)
		switch {
		case e.Delta != "":
			order = append(order, "text")
		case e.ToolCall != nil:
			order = append(order, "tool")
			tcs = append(tcs, e.ToolCall)
		case e.Done:
			order = append(order, "done")
			done = e.Usage
		}
		return nil
	})
	if got := strings.Join(order, ","); got != "text,text,tool,done" {
		t.Fatalf("event order = %s", got)
	}
	if err != nil || text.String() != "Hello" || len(tcs) != 1 || tcs[0].ID != "call_0" || tcs[0].Name != "lookup" || tcs[0].Arguments != `{"q":"x"}` {
		t.Fatalf("text=%q tcs=%+v err=%v", text.String(), tcs, err)
	}
	if outcome.Usage.InputTokens != 8 || outcome.Usage.OutputTokens != 3 || outcome.FinishReason != "tool_calls" || done == nil || *done != outcome.Usage {
		t.Fatalf("outcome = %+v done=%v", outcome, done)
	}
	if hdr.Get("X-Goog-Api-Key") != "AIza-test" || hdr.Get("Authorization") != "" {
		t.Fatalf("headers = %v", hdr)
	}
	sys, _ := json.Marshal(captured["systemInstruction"])
	if !strings.Contains(string(sys), "be brief") || !strings.Contains(string(sys), "rule") {
		t.Errorf("systemInstruction = %s", sys)
	}
	contents, _ := captured["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents = %v", captured["contents"])
	}
	gc, _ := captured["generationConfig"].(map[string]any)
	if gc["responseMimeType"] != "application/json" || gc["maxOutputTokens"] != float64(77) || gc["responseJsonSchema"] == nil {
		t.Errorf("generationConfig = %v", gc)
	}
	if tmp, _ := gc["temperature"].(float64); tmp < 0.29 || tmp > 0.31 {
		t.Errorf("temperature = %v", gc["temperature"])
	}
	tools, _ := json.Marshal(captured["tools"])
	if !strings.Contains(string(tools), `"functionDeclarations"`) || !strings.Contains(string(tools), `"parametersJsonSchema"`) || !strings.Contains(string(tools), `"additionalProperties":false`) {
		t.Errorf("tools = %s", tools)
	}
}

func TestGemini_ConversationMapping(t *testing.T) {
	var captured map[string]any
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		gemSSE(w, gemFinal("STOP", 1, 1))
	})
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "gemini-x", Request: iface.ChatRequest{Messages: []iface.ChatMessage{
		{Role: "user", Content: "weather in two cities?"},
		{Role: "assistant", Content: "checking", ToolCalls: []iface.ChatToolCall{
			{ID: "call_0", Name: "weather", Arguments: `{"city":"Rome"}`},
			{ID: "call_1", Name: "weather", Arguments: ``}}},
		{Role: "tool", ToolCallID: "call_0", Content: "sunny"},
		{Role: "tool", ToolCallID: "call_1", Content: "rain"},
		{Role: "user", Content: "thanks"},
	}}}, noopSink)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := captured["contents"].([]any)
	if len(contents) != 4 {
		t.Fatalf("contents = %v", captured["contents"])
	}
	raw, _ := json.Marshal(contents)
	s := string(raw)
	for _, want := range []string{
		`"role":"model"`, `"functionCall":{"args":{"city":"Rome"},"id":"call_0","name":"weather"}`,
		`"functionResponse":{"id":"call_0","name":"weather","response":{"result":"sunny"}}`,
		`"functionResponse":{"id":"call_1","name":"weather","response":{"result":"rain"}}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("contents lack %s in %s", want, s)
		}
	}
	// Both tool results share one user turn.
	results, _ := contents[2].(map[string]any)["parts"].([]any)
	if contents[2].(map[string]any)["role"] != "user" || len(results) != 2 {
		t.Errorf("tool results turn = %v", contents[2])
	}
	for _, bad := range [][]iface.ChatMessage{
		{{Role: "user", Content: " "}},
		{{Role: "user", Content: "x"}, {Role: "tool", ToolCallID: "unknown", Content: "r"}},
		{{Role: "user", Content: "x"}, {Role: "tool", Content: "r", Name: "n"}},
		{{Role: "user", Content: "x"}, {Role: "assistant", ToolCalls: []iface.ChatToolCall{{ID: "a", Name: "n", Arguments: `[1]`}}}},
		{{Role: "wizard", Content: "x"}},
	} {
		if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: iface.ChatRequest{Messages: bad}}, noopSink); !errors.Is(err, iface.ErrLLMInvalidRequest) {
			t.Errorf("messages %+v: err = %v", bad, err)
		}
	}
}

func TestGemini_FinishReasonsAreNotProviderFaults(t *testing.T) {
	mode := ""
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		if mode == "blocked" {
			gemSSE(w, `{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":5}}`)
			return
		}
		gemSSE(w, gemText("part"), gemFinal(mode, 5, 2))
	})
	for m, want := range map[string]string{
		"STOP": "stop", "MAX_TOKENS": "length", "SAFETY": "content_filter", "RECITATION": "content_filter",
		"PROHIBITED_CONTENT": "content_filter", "OTHER": "other", "MALFORMED_FUNCTION_CALL": "other", "blocked": "content_filter",
	} {
		mode = m
		outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
		if err != nil || outcome.FinishReason != want {
			t.Errorf("%s: outcome=%+v err=%v, want %s", m, outcome, err, want)
		}
	}
}

func TestGemini_UsageFallbackAndThoughtsCountAsOutput(t *testing.T) {
	mode := "none"
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		if mode == "thoughts" {
			gemSSE(w, `{"candidates":[{"content":{"parts":[{"text":"a"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":30}}`)
			return
		}
		gemSSE(w, `{"candidates":[{"content":{"parts":[{"text":"abcdefghi"}],"role":"model"},"finishReason":"STOP"}]}`)
	})
	req := userReq("hello there")
	o, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink)
	if err != nil || o.Usage.InputTokens != EstimateTokens(req) || o.Usage.OutputTokens != 3 {
		t.Fatalf("estimate: %+v err=%v", o, err)
	}
	mode = "thoughts"
	o, err = p.Chat(context.Background(), ChatCall{ModelID: "m", Request: req}, noopSink)
	if err != nil || o.Usage.InputTokens != 10 || o.Usage.OutputTokens != 32 {
		t.Fatalf("thoughts: %+v err=%v", o, err)
	}
}

func TestGemini_ThoughtPartsAreNotEmittedAndCallIDsPassThrough(t *testing.T) {
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		gemSSE(w, `{"candidates":[{"content":{"parts":[{"text":"secret thinking","thought":true},{"text":"answer"},{"functionCall":{"id":"fc-9","name":"noargs"}},{"functionCall":{"name":"second","args":{}}}],"role":"model"},"finishReason":"STOP"}]}`)
	})
	var text strings.Builder
	var tcs []iface.ChatToolCall
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error {
		text.WriteString(e.Delta)
		if e.ToolCall != nil {
			tcs = append(tcs, *e.ToolCall)
		}
		return nil
	})
	if err != nil || text.String() != "answer" || len(tcs) != 2 {
		t.Fatalf("text=%q tcs=%+v err=%v", text.String(), tcs, err)
	}
	if tcs[0] != (iface.ChatToolCall{ID: "fc-9", Name: "noargs", Arguments: "{}"}) || tcs[1] != (iface.ChatToolCall{ID: "call_1", Name: "second", Arguments: "{}"}) {
		t.Errorf("tool calls = %+v", tcs)
	}
}

func TestGemini_HTTPAndStreamErrorsAreClassifiedWithoutLeaks(t *testing.T) {
	mode := ""
	body := func(code int, status string) string {
		return `{"error":{"code":` + strconv.Itoa(code) + `,"status":"` + status + `","message":"sk-live-x AIza-test"}}`
	}
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		fail := func(code int, status string) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body(code, status)))
		}
		switch mode {
		case "503":
			fail(503, "UNAVAILABLE")
		case "500":
			fail(500, "INTERNAL")
		case "401":
			fail(401, "UNAUTHENTICATED")
		case "403":
			fail(403, "PERMISSION_DENIED")
		case "400_key":
			fail(400, "INVALID_ARGUMENT")
		case "402":
			w.WriteHeader(402)
			_, _ = w.Write([]byte(`{"error":{"message":"sk-live-x"}}`))
		case "429":
			fail(429, "RESOURCE_EXHAUSTED")
		case "404":
			fail(404, "NOT_FOUND")
		case "502_html":
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`<html>sk-live-x</html>`))
		case "huge_error":
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":{"status":"INTERNAL","message":"` + strings.Repeat("sk-live-x", 20000) + `"}}`))
		case "stream_error_line":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: " + gemText("x") + "\n\n" + body(503, "UNAVAILABLE") + "\n\n"))
		case "stream_error_quota_line":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`{"error":{"code":402,"message":"sk-live-x"}}` + "\n\n"))
		case "stream_error_invalid_line":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(body(400, "INVALID_ARGUMENT") + "\n\n"))
		case "stream_error_rpc_only":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`{"error":{"status":"UNAUTHENTICATED","message":"sk-live-x"}}` + "\n\n"))
		case "stream_garbage_line":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("not json sk-live-x\n\n"))
		case "stream_garbage_data":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {not json sk-live-x\n\n"))
		case "stream_error_in_data":
			gemSSE(w, `{"error":{"code":503,"status":"UNAVAILABLE","message":"sk-live-x"}}`)
		case "truncated":
			gemSSE(w, gemText("x"))
		case "bad_tool":
			gemSSE(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"args":{"a":1}}}],"role":"model"},"finishReason":"STOP"}]}`)
		case "empty_stream":
			gemSSE(w)
		}
	})
	for _, c := range []struct {
		mode string
		want []error
	}{
		{"503", []error{iface.ErrLLMProviderUnavailable}},
		{"500", []error{iface.ErrLLMProviderUnavailable}},
		{"401", []error{ErrUnauthorized, iface.ErrLLMProviderUnavailable}},
		{"403", []error{ErrUnauthorized}},
		{"400_key", []error{iface.ErrLLMInvalidRequest}},
		{"402", []error{iface.ErrLLMQuotaExhausted}},
		{"429", []error{iface.ErrLLMProviderUnavailable}}, // bare RESOURCE_EXHAUSTED is rate limiting (5.12)
		{"404", []error{iface.ErrLLMCapabilityMismatch}},
		{"502_html", []error{iface.ErrLLMProviderUnavailable}},
		{"huge_error", []error{iface.ErrLLMProviderUnavailable}},
		{"stream_error_line", []error{iface.ErrLLMProviderUnavailable}},
		{"stream_error_quota_line", []error{iface.ErrLLMQuotaExhausted}},
		{"stream_error_invalid_line", []error{iface.ErrLLMInvalidRequest}},
		{"stream_error_rpc_only", []error{ErrUnauthorized}},
		{"stream_garbage_line", []error{iface.ErrLLMProviderUnavailable}},
		{"stream_garbage_data", []error{iface.ErrLLMProviderUnavailable}},
		{"stream_error_in_data", []error{iface.ErrLLMProviderUnavailable}},
		{"truncated", []error{iface.ErrLLMProviderUnavailable}},
		{"bad_tool", []error{iface.ErrLLMProviderUnavailable}},
		{"empty_stream", []error{iface.ErrLLMProviderUnavailable}},
	} {
		mode = c.mode
		_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
		if err == nil {
			t.Errorf("mode %s: no error", c.mode)
			continue
		}
		for _, want := range c.want {
			if !errors.Is(err, want) {
				t.Errorf("mode %s: err = %v, want %v", c.mode, err, want)
			}
		}
		for _, leak := range []string{"sk-live-x", "AIza-test", "127.0.0.1", "not json"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("mode %s: %q leaked into error: %v", c.mode, leak, err)
			}
		}
	}
	// Non-stream calls (model discovery, embeddings) are classified the same way.
	mode = "401"
	if err := p.Health(context.Background()); !errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "sk-live-x") {
		t.Errorf("health err = %v", err)
	}
	mode = "429"
	if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "e", Inputs: []string{"a"}}); !errors.Is(err, iface.ErrLLMProviderUnavailable) || strings.Contains(err.Error(), "sk-live-x") {
		t.Errorf("embed err = %v", err)
	}
}

func TestGemini_SinkErrorAndCancelPropagate(t *testing.T) {
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) { gemSSE(w, gemText("x"), gemFinal("STOP", 1, 1)) })
	boom := errors.New("sink boom")
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(iface.ChatEvent) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("sink err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Chat(ctx, ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); !errors.Is(err, context.Canceled) || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("cancel err = %v", err)
	}
	// Cancelling in the middle of a stream is the caller's cancel, not a
	// provider fault. (The SDK logs the stream read error through the std logger.)
	defer log.SetOutput(log.Writer())
	log.SetOutput(io.Discard)
	started := make(chan struct{})
	release := make(chan struct{})
	p2 := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		gemSSE(w, gemText("x"))
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ctx2, cancel2 := context.WithCancel(context.Background())
	_, err := p2.Chat(ctx2, ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error {
		if e.Delta != "" {
			<-started
			cancel2()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("mid-stream cancel err = %v", err)
	}
}

// The stream must be opted in on the request side: with a tiny response cap
// on the shared client a long stream still passes, which only holds when the
// adapter marks the call with WithStreaming.
func TestGemini_StreamIsMarkedStreamingOnTheRequest(t *testing.T) {
	big := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var events []string
		for i := 0; i < 8; i++ {
			events = append(events, gemText(big))
		}
		gemSSE(w, append(events, gemFinal("STOP", 1, 1))...)
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, MaxResponseBytes: 1024, IdleTimeout: 5 * time.Second})
	p, err := NewGemini(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k", BaseURL: srv.URL}, client)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	_, err = p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error { n += len(e.Delta); return nil })
	if err != nil || n != 8*4096 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// ...while the non-stream calls stay capped by the same client.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"models/` + strings.Repeat("a", 4096) + `","supportedGenerationMethods":["generateContent"]}]}`))
	}))
	defer srv2.Close()
	p2, _ := NewGemini(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k", BaseURL: srv2.URL}, client)
	if _, err := p2.ListModels(context.Background()); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("oversized non-stream response err = %v", err)
	}
}

// The SDK's own stream scanner would buffer up to 256 MiB for one event.
func TestGemini_OversizedStreamEventIsRefused(t *testing.T) {
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		gemSSE(w, gemText(strings.Repeat("a", oaiMaxEvent+1024)), gemFinal("STOP", 1, 1))
	})
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestGemini_DoesNotLeakProcessEnvironment(t *testing.T) {
	// One of the two key variables at a time: the SDK logs a warning when
	// both are set.
	t.Setenv("GEMINI_API_KEY", "env-gemini-key")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "http://env.invalid")
	t.Setenv("GOOGLE_VERTEX_BASE_URL", "http://env.invalid")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "env-location")
	var hdr http.Header
	var path string
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		hdr, path = r.Header.Clone(), r.URL.Path
		gemSSE(w, gemFinal("STOP", 1, 1))
	})
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); err != nil {
		t.Fatal(err)
	}
	if hdr.Get("X-Goog-Api-Key") != "AIza-test" || hdr.Get("Authorization") != "" || !strings.HasPrefix(path, "/v1beta/models/") {
		t.Fatalf("headers = %v path = %s", hdr, path)
	}
}

func TestGemini_EffortFollowsDeclaredThinkingSupport(t *testing.T) {
	var listCalls int32
	var captured map[string]any
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			atomic.AddInt32(&listCalls, 1)
			_, _ = w.Write([]byte(`{"models":[
 {"name":"models/gem-think","displayName":"Think","supportedGenerationMethods":["generateContent"],"thinking":true},
 {"name":"models/gem-plain","displayName":"Plain","supportedGenerationMethods":["generateContent"]}]}`))
			return
		}
		captured = nil
		_ = json.NewDecoder(r.Body).Decode(&captured)
		gemSSE(w, gemFinal("STOP", 1, 1))
	})
	thinkingLevel := func(model, effort string) any {
		_, err := p.Chat(context.Background(), ChatCall{ModelID: model, Request: iface.ChatRequest{
			Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}, Options: iface.ChatOptions{Effort: effort}}}, noopSink)
		if err != nil {
			t.Fatal(err)
		}
		gc, _ := captured["generationConfig"].(map[string]any)
		tc, _ := gc["thinkingConfig"].(map[string]any)
		return tc["thinkingLevel"]
	}
	if got := thinkingLevel("gem-think", "high"); got != "HIGH" {
		t.Errorf("thinking model: level = %v", got)
	}
	if got := thinkingLevel("gem-think", "low"); got != "LOW" {
		t.Errorf("thinking model: level = %v", got)
	}
	if got := thinkingLevel("gem-plain", "high"); got != nil {
		t.Errorf("non-thinking model must not get a thinking level, got %v", got)
	}
	if got := thinkingLevel("gem-unknown", "high"); got != nil {
		t.Errorf("unknown model must not get a thinking level, got %v", got)
	}
	if got := thinkingLevel("gem-think", ""); got != nil {
		t.Errorf("no effort requested but level = %v", got)
	}
	if n := atomic.LoadInt32(&listCalls); n != 1 {
		t.Errorf("catalog fetched %d times, want 1 (cached)", n)
	}
}

func TestGemini_ModelDiscoveryFailureDegradesToNoEffort(t *testing.T) {
	var listCalls int32
	var captured map[string]any
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			atomic.AddInt32(&listCalls, 1)
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"error":{"code":403,"status":"PERMISSION_DENIED"}}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		gemSSE(w, gemFinal("STOP", 1, 1))
	})
	for i := 0; i < 2; i++ {
		if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: iface.ChatRequest{
			Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}, Options: iface.ChatOptions{Effort: "high"}}}, noopSink); err != nil {
			t.Fatal(err)
		}
	}
	if gc, _ := captured["generationConfig"].(map[string]any); gc["thinkingConfig"] != nil {
		t.Errorf("thinkingConfig sent although discovery failed: %v", gc)
	}
	if n := atomic.LoadInt32(&listCalls); n != 1 {
		t.Errorf("failed discovery fetched %d times, want 1 (remembered)", n)
	}
}

func TestGemini_EmbedBatchesAndPreservesOrder(t *testing.T) {
	var calls [][]any
	var path string
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		var req struct {
			Requests []map[string]any `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reqs := make([]any, len(req.Requests))
		var embs []string
		for i, rq := range req.Requests {
			reqs[i] = rq
			text := rq["content"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string)
			embs = append(embs, `{"values":[`+strconv.Itoa(len(text))+`,0.5]}`)
		}
		calls = append(calls, reqs)
		_, _ = w.Write([]byte(`{"embeddings":[` + strings.Join(embs, ",") + `]}`))
	})
	inputs := make([]string, 0, 101)
	for i := 0; i < 101; i++ {
		inputs = append(inputs, strings.Repeat("a", i+1))
	}
	out, err := p.Embed(context.Background(), EmbedCall{ModelID: "text-embed", Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1beta/models/text-embed:batchEmbedContents" || len(calls) != 2 || len(calls[0]) != 100 || len(calls[1]) != 1 {
		t.Fatalf("path=%s batches=%d", path, len(calls))
	}
	if len(out.Vectors) != 101 || out.Vectors[0][0] != 1 || out.Vectors[100][0] != 101 || out.Usage.InputTokens <= 0 {
		t.Fatalf("out = %+v", out.Usage)
	}
}

// A failure on a later batch keeps the usage of the batches already
// embedded (the provider billed them); a failure on the first has none.
func TestGemini_EmbedFailedLaterBatchKeepsEarlierUsage(t *testing.T) {
	batch := 0
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		batch++
		if batch == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"status":"UNAVAILABLE","message":"x"}}`))
			return
		}
		embs := make([]string, 100)
		for i := range embs {
			embs[i] = `{"values":[1,0.5]}`
		}
		_, _ = w.Write([]byte(`{"embeddings":[` + strings.Join(embs, ",") + `]}`))
	})
	inputs := make([]string, 150)
	for i := range inputs {
		inputs[i] = "abcdef" // 6 bytes = 2 estimated tokens each
	}
	out, err := p.Embed(context.Background(), EmbedCall{ModelID: "text-embed", Inputs: inputs})
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if out == nil || out.Usage.InputTokens != 200 || len(out.Vectors) != 0 {
		t.Fatalf("partial outcome = %+v, want 200 input tokens for the first 100 inputs and no vectors", out)
	}

	batch = 1 // the next call fails on its first batch
	out, err = p.Embed(context.Background(), EmbedCall{ModelID: "text-embed", Inputs: inputs})
	if err == nil || out != nil {
		t.Fatalf("first-batch failure = %+v, %v; want no outcome", out, err)
	}
}

func TestGemini_EmbedRejectsShortOrEmptyResponses(t *testing.T) {
	body := ""
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	for _, b := range []string{`{"embeddings":[{"values":[1]}]}`, `{"embeddings":[{"values":[]},{"values":[1]}]}`, `{}`} {
		body = b
		if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "e", Inputs: []string{"a", "b"}}); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Errorf("body %s: err = %v", b, err)
		}
	}
}

func TestGemini_ListModelsFiltersAndPages(t *testing.T) {
	var pages int32
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pages, 1)
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"models":[
 {"name":"models/gemini-2.5-pro","displayName":"Gemini 2.5 Pro","supportedGenerationMethods":["generateContent","countTokens"]},
 {"name":"models/aqa","displayName":"AQA","supportedGenerationMethods":["generateAnswer"]}],"nextPageToken":"p2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[
 {"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]}]}`))
	})
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 2 || atomic.LoadInt32(&pages) != 2 {
		t.Fatalf("models=%+v pages=%d err=%v", models, pages, err)
	}
	if models[0] != (ModelDescriptor{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro"}) || models[1] != (ModelDescriptor{ID: "text-embedding-004", DisplayName: "text-embedding-004"}) {
		t.Errorf("models = %+v", models)
	}
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("health: %v", err)
	}
}

func TestGemini_CapabilitiesConstructorAndRegistry(t *testing.T) {
	if _, err := NewGemini(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k"}, nil); err == nil {
		t.Error("nil client must be refused (it would bypass the SSRF guard)")
	}
	if _, err := NewGemini(Credential{Kind: iface.LLMCredentialKindOrg}, http.DefaultClient); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Errorf("empty key err = %v (the SDK would fall back to GEMINI_API_KEY)", err)
	}
	if _, err := NewGemini(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k", BaseURL: "ftp://x"}, http.DefaultClient); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Errorf("bad scheme err = %v", err)
	}
	r := NewRegistry()
	RegisterAll(r, func(string) *http.Client { return http.DefaultClient })
	p, err := r.Build("gemini", Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k"}, nil)
	if err != nil || p.Name() != "gemini" {
		t.Fatalf("build: %v", err)
	}
	if c := p.Capabilities(); !c.Chat || !c.Streaming || !c.Tools || !c.StructuredOutput || !c.Embeddings {
		t.Errorf("capabilities = %+v", c)
	}
}

// ---- thought signatures (Gemini 3 function calling) ----

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestGemini_ThoughtSignatureIsCapturedFromTheStream(t *testing.T) {
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		gemSSE(w, `{"candidates":[{"content":{"parts":[
 {"functionCall":{"name":"a","args":{}},"thoughtSignature":"`+b64("sig-one")+`"},
 {"functionCall":{"name":"b","args":{}}}],"role":"model"},"finishReason":"STOP"}]}`)
	})
	var tcs []iface.ChatToolCall
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			tcs = append(tcs, *e.ToolCall)
		}
		return nil
	})
	if err != nil || len(tcs) != 2 || tcs[0].Signature != b64("sig-one") || tcs[1].Signature != "" {
		t.Fatalf("tcs=%+v err=%v", tcs, err)
	}
}

func TestGemini_OversizedOutputSignatureIsDroppedButTheCallIsDelivered(t *testing.T) {
	big := base64.StdEncoding.EncodeToString(make([]byte, geminiMaxSignature)) // encodes past the cap
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		gemSSE(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"a","args":{}},"thoughtSignature":"`+big+`"}],"role":"model"},"finishReason":"STOP"}]}`)
	})
	var got *iface.ChatToolCall
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			got = e.ToolCall
		}
		return nil
	})
	if err != nil || got == nil || got.Name != "a" || got.Signature != "" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

// wireParts returns the parts of every content as sent to the provider.
func wireParts(captured map[string]any) [][]map[string]any {
	var out [][]map[string]any
	contents, _ := captured["contents"].([]any)
	for _, c := range contents {
		var parts []map[string]any
		ps, _ := c.(map[string]any)["parts"].([]any)
		for _, p := range ps {
			parts = append(parts, p.(map[string]any))
		}
		out = append(out, parts)
	}
	return out
}

func TestGemini_SignatureIsEchoedAndCurrentTurnFallsBackToTheBypass(t *testing.T) {
	var captured map[string]any
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		captured = nil
		_ = json.NewDecoder(r.Body).Decode(&captured)
		gemSSE(w, gemFinal("STOP", 1, 1))
	})
	call := func(id, name string) iface.ChatToolCall {
		return iface.ChatToolCall{ID: id, Name: name, Arguments: `{}`}
	}
	signed := call("c1", "a")
	signed.Signature = b64("sig-one")
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: iface.ChatRequest{Messages: []iface.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", ToolCalls: []iface.ChatToolCall{call("h1", "old")}}, // before the last user message: not validated
		{Role: "tool", ToolCallID: "h1", Content: "r"},
		{Role: "user", Content: "second"},
		{Role: "assistant", ToolCalls: []iface.ChatToolCall{signed, call("c2", "b")}}, // signed first call, parallel second
		{Role: "tool", ToolCallID: "c1", Content: "r1"},
		{Role: "tool", ToolCallID: "c2", Content: "r2"},
		{Role: "assistant", ToolCalls: []iface.ChatToolCall{call("c3", "c"), call("c4", "d")}}, // unsigned step in the current turn
		{Role: "tool", ToolCallID: "c3", Content: "r3"},
		{Role: "tool", ToolCallID: "c4", Content: "r4"},
	}}}, noopSink)
	if err != nil {
		t.Fatal(err)
	}
	contents := wireParts(captured)
	sigOf := func(ci, pi int) any { return contents[ci][pi]["thoughtSignature"] }
	if sigOf(1, 0) != nil {
		t.Errorf("history call got a signature: %v", sigOf(1, 0))
	}
	if sigOf(4, 0) != b64("sig-one") || sigOf(4, 1) != nil {
		t.Errorf("signed step: %v / %v", sigOf(4, 0), sigOf(4, 1))
	}
	bypass, _ := sigOf(6, 0).(string)
	// Proto-JSON accepts both base64 alphabets: compare after normalising to the URL-safe one.
	if strings.NewReplacer("+", "-", "/", "_").Replace(bypass) != "skip_thought_signature_validator" || sigOf(6, 1) != nil {
		t.Errorf("unsigned current-turn step: %v / %v", sigOf(6, 0), sigOf(6, 1))
	}
}

func TestGemini_SignatureInputIsValidatedAndNeverLeaked(t *testing.T) {
	mode := "ok"
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		if mode == "400" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad signature ` + b64("sig-secret") + `"}}`))
			return
		}
		gemSSE(w, gemFinal("STOP", 1, 1))
	})
	withSig := func(sig string) iface.ChatRequest {
		return iface.ChatRequest{Messages: []iface.ChatMessage{
			{Role: "user", Content: "x"},
			{Role: "assistant", ToolCalls: []iface.ChatToolCall{{ID: "a", Name: "n", Arguments: `{}`, Signature: sig}}},
			{Role: "tool", ToolCallID: "a", Content: "r"}}}
	}
	for name, sig := range map[string]string{
		"too large":  strings.Repeat("A", geminiMaxSignature+4),
		"not base64": "***not base64***",
	} {
		if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: withSig(sig)}, noopSink); !errors.Is(err, iface.ErrLLMInvalidRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	mode = "400"
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: withSig(b64("sig-secret"))}, noopSink)
	if !errors.Is(err, iface.ErrLLMInvalidRequest) || strings.Contains(err.Error(), "sig-secret") || strings.Contains(err.Error(), b64("sig-secret")) {
		t.Errorf("err = %v", err)
	}
}

// ---- model listing without a "models" key, quota ids ----

func TestGemini_EmptyModelListIsNotLoggedByTheSDK(t *testing.T) {
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Secret-Header", "must-not-be-logged")
		_, _ = w.Write([]byte(`{}`))
	})
	var buf strings.Builder
	defer log.SetOutput(log.Writer())
	log.SetOutput(&buf)
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 0 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	if buf.Len() != 0 {
		t.Errorf("SDK logged: %s", buf.String())
	}
}

func TestGemini_QuotaIDSplitsDailyFromPerMinuteRateLimits(t *testing.T) {
	mode := ""
	details := func(id string) string {
		return `"details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaId":"` + id + `"}]}]`
	}
	p := newGem(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "day":
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED",` + details("GenerateRequestsPerDayPerProjectPerModel-FreeTier") + `}}`))
		case "minute":
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED",` + details("GenerateRequestsPerMinutePerProjectPerModel-FreeTier") + `}}`))
		case "stream_day":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED",` + details("GenerateRequestsPerDayPerProjectPerModel") + `}}` + "\n\n"))
		}
	})
	for m, want := range map[string]error{"day": iface.ErrLLMQuotaExhausted, "minute": iface.ErrLLMProviderUnavailable, "stream_day": iface.ErrLLMQuotaExhausted} {
		mode = m
		_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", m, err, want)
		}
	}
}
