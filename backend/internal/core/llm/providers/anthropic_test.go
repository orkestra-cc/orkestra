package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// antSSE writes Messages-API server-sent events ("event:" + "data:" pairs).
func antSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		_, _ = w.Write([]byte(e + "\n\n"))
	}
}

func antStart(in, out int) string {
	return ev("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":`+itoa(in)+`,"output_tokens":`+itoa(out)+`}}}`)
}

func antTextStart(i int) string {
	return ev("content_block_start", `{"type":"content_block_start","index":`+itoa(i)+`,"content_block":{"type":"text","text":""}}`)
}

func antTextDelta(i int, s string) string {
	b, _ := json.Marshal(s)
	return ev("content_block_delta", `{"type":"content_block_delta","index":`+itoa(i)+`,"delta":{"type":"text_delta","text":`+string(b)+`}}`)
}

func antToolStart(i int, id, name string) string {
	return ev("content_block_start", `{"type":"content_block_start","index":`+itoa(i)+`,"content_block":{"type":"tool_use","id":"`+id+`","name":"`+name+`","input":{}}}`)
}

func antJSONDelta(i int, s string) string {
	b, _ := json.Marshal(s)
	return ev("content_block_delta", `{"type":"content_block_delta","index":`+itoa(i)+`,"delta":{"type":"input_json_delta","partial_json":`+string(b)+`}}`)
}

func antStop(i int) string {
	return ev("content_block_stop", `{"type":"content_block_stop","index":`+itoa(i)+`}`)
}

func antMsgDelta(reason string, out int) string {
	return ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+reason+`","stop_sequence":null},"usage":{"output_tokens":`+itoa(out)+`}}`)
}

func antMsgStop() string { return ev("message_stop", `{"type":"message_stop"}`) }

func newAnt(t *testing.T, h http.HandlerFunc) Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := NewAnthropic(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "sk-ant-test", BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const antModelsBody = `{"data":[
 {"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","type":"model","capabilities":{"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":false},"max":{"supported":false}},"thinking":{"supported":true,"types":{"adaptive":{"supported":true},"enabled":{"supported":false},"disabled":{"supported":true}}},"structured_outputs":{"supported":true}}},
 {"id":"claude-old","display_name":"Claude Old","type":"model","capabilities":{"effort":{"supported":false},"thinking":{"supported":false,"types":{"adaptive":{"supported":false}}}}}
],"has_more":false,"first_id":"claude-opus-5-5","last_id":"claude-old"}`

func TestAnthropic_MessagesStreamTextToolsUsage(t *testing.T) {
	var captured map[string]any
	var hdr http.Header
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		hdr = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&captured)
		antSSE(w,
			antStart(9, 1),
			antTextStart(0), antTextDelta(0, "Hel"), antTextDelta(0, "lo"), antStop(0),
			antToolStart(1, "toolu_1", "lookup"), antJSONDelta(1, `{"q":`), antJSONDelta(1, `"x"}`), antStop(1),
			antMsgDelta("tool_use", 4), antMsgStop(),
		)
	})
	var text strings.Builder
	var tcs []*iface.ChatToolCall
	var done *iface.LLMUsage
	var order []string
	temp := 0.3
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "claude-x", Request: iface.ChatRequest{
		Caller: "t", Instructions: "be brief",
		Messages: []iface.ChatMessage{{Role: "developer", Content: "rule"}, {Role: "user", Content: "hi"}},
		Tools: []iface.ChatToolSpec{{Name: "lookup", Description: "d", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}, "required": []any{"q"}, "additionalProperties": false}}},
		ResponseFormat: &iface.JSONSchemaFormat{Name: "out", Schema: map[string]any{"type": "object"}, Strict: true},
		Options:        iface.ChatOptions{Temperature: &temp},
	}}, func(e iface.ChatEvent) error {
		text.WriteString(e.Delta)
		switch {
		case e.Delta != "":
			order = append(order, "text")
		case e.ToolCall != nil:
			order = append(order, "tool")
		case e.Done:
			order = append(order, "done")
		}
		if e.ToolCall != nil {
			tcs = append(tcs, e.ToolCall)
		}
		if e.Done {
			done = e.Usage
		}
		return nil
	})
	if got := strings.Join(order, ","); got != "text,text,tool,done" {
		t.Fatalf("event order = %s", got)
	}
	if err != nil || text.String() != "Hello" || len(tcs) != 1 || tcs[0].ID != "toolu_1" || tcs[0].Name != "lookup" || tcs[0].Arguments != `{"q":"x"}` {
		t.Fatalf("text=%q tcs=%+v err=%v", text.String(), tcs, err)
	}
	if outcome.Usage.InputTokens != 9 || outcome.Usage.OutputTokens != 4 || outcome.FinishReason != "tool_calls" || done == nil || *done != outcome.Usage {
		t.Fatalf("outcome = %+v done=%v", outcome, done)
	}
	if hdr.Get("X-Api-Key") != "sk-ant-test" || hdr.Get("Anthropic-Version") == "" || hdr.Get("Authorization") != "" {
		t.Fatalf("headers = %v", hdr)
	}
	if captured["stream"] != true || captured["model"] != "claude-x" || captured["max_tokens"] != float64(8192) || captured["temperature"] != 0.3 {
		t.Fatalf("request = %v", captured)
	}
	sys, _ := json.Marshal(captured["system"])
	if !strings.Contains(string(sys), "be brief") || !strings.Contains(string(sys), "rule") {
		t.Errorf("system = %s", sys)
	}
	msgs, _ := captured["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", captured["messages"])
	}
	tool := captured["tools"].([]any)[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	if tool["name"] != "lookup" || tool["description"] != "d" || schema["type"] != "object" || schema["additionalProperties"] != false ||
		schema["properties"] == nil || len(schema["required"].([]any)) != 1 {
		t.Fatalf("tool = %v", tool)
	}
	cfg, _ := captured["output_config"].(map[string]any)
	format, _ := cfg["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Fatalf("output_config = %v", cfg)
	}
	if _, has := captured["thinking"]; has {
		t.Error("thinking must not be sent without a declared capability")
	}
}

func TestAnthropic_ConversationMapping(t *testing.T) {
	var captured map[string]any
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		antSSE(w, antStart(1, 1), antMsgDelta("end_turn", 1), antMsgStop())
	})
	maxOut := 256
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Defaults: ChatDefaults{MaxOutputTokens: &maxOut}, Request: iface.ChatRequest{Caller: "t", Messages: []iface.ChatMessage{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "calling", ToolCalls: []iface.ChatToolCall{{ID: "c1", Name: "lookup", Arguments: ""}, {ID: "c2", Name: "other", Arguments: `{"a":1}`}}},
		{Role: "tool", ToolCallID: "c1", Content: "r1"},
		{Role: "tool", ToolCallID: "c2", Content: "r2"},
		{Role: "user", Content: "thanks"},
	}}}, noopSink)
	if err != nil {
		t.Fatal(err)
	}
	if captured["max_tokens"] != float64(256) {
		t.Errorf("max_tokens = %v (model default must apply)", captured["max_tokens"])
	}
	msgs := captured["messages"].([]any)
	if len(msgs) != 4 { // user, assistant, ONE user turn with both results, user
		t.Fatalf("messages = %d: %v", len(msgs), msgs)
	}
	b, _ := json.Marshal(msgs)
	for _, w := range []string{
		`"type":"tool_use"`, `"id":"c1"`, `"name":"lookup"`, `"input":{}`, `"input":{"a":1}`,
		`"type":"tool_result"`, `"tool_use_id":"c1"`, `"tool_use_id":"c2"`,
	} {
		if !strings.Contains(string(b), w) {
			t.Errorf("messages lack %s: %s", w, b)
		}
	}
	if results := msgs[2].(map[string]any)["content"].([]any); len(results) != 2 {
		t.Errorf("tool results must share one user turn: %v", msgs[2])
	}
	// Local refusals send nothing.
	for name, m := range map[string]iface.ChatMessage{
		"unknown role":   {Role: "wizard", Content: "x"},
		"empty user":     {Role: "user", Content: "  "},
		"tool no id":     {Role: "tool", Content: "x"},
		"bad args array": {Role: "assistant", ToolCalls: []iface.ChatToolCall{{ID: "c", Name: "n", Arguments: `[1]`}}},
	} {
		_, err = p.Chat(context.Background(), ChatCall{ModelID: "m", Request: iface.ChatRequest{Messages: []iface.ChatMessage{m}}}, noopSink)
		if !errors.Is(err, iface.ErrLLMInvalidRequest) {
			t.Errorf("%s err = %v", name, err)
		}
	}
}

func TestAnthropic_StopReasonsMapFinishReason(t *testing.T) {
	cases := map[string]string{
		"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length", "refusal": "content_filter",
		"pause_turn": "other", "model_context_window_exceeded": "length",
	}
	for reason, want := range cases {
		p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
			antSSE(w, antStart(3, 1), antTextStart(0), antTextDelta(0, "x"), antStop(0), antMsgDelta(reason, 2), antMsgStop())
		})
		out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
		if err != nil || out.FinishReason != want {
			t.Errorf("%s: out=%+v err=%v, want %s", reason, out, err, want)
		}
	}
}

func TestAnthropic_MaxTokensDropsTruncatedToolCallButKeepsCompleteOnes(t *testing.T) {
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		antSSE(w, antStart(3, 1),
			antToolStart(0, "t1", "ok"), antJSONDelta(0, `{"a":1}`), antStop(0),
			antToolStart(1, "t2", "cut"), antJSONDelta(1, `{"a":"unfin`), antStop(1),
			antMsgDelta("max_tokens", 5), antMsgStop())
	})
	var got []*iface.ChatToolCall
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			got = append(got, e.ToolCall)
		}
		return nil
	})
	if err != nil || out.FinishReason != "length" || len(got) != 1 || got[0].ID != "t1" {
		t.Fatalf("out=%+v got=%+v err=%v", out, got, err)
	}
	// The same cut-off call under a normal stop reason is a malformed response.
	bad := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		antSSE(w, antStart(3, 1), antToolStart(0, "t2", "cut"), antJSONDelta(0, `{"a":"unfin`), antStop(0), antMsgDelta("tool_use", 5), antMsgStop())
	})
	if _, err := bad.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("malformed args err = %v", err)
	}
	// A non-object argument is refused, never unwrapped.
	arr := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		antSSE(w, antStart(3, 1), antToolStart(0, "t3", "n"), antJSONDelta(0, `[1]`), antStop(0), antMsgDelta("tool_use", 5), antMsgStop())
	})
	if _, err := arr.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("array args err = %v", err)
	}
}

func TestAnthropic_NoArgumentToolAndMissingUsage(t *testing.T) {
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		// no usage on message_start (zeros) and none on message_delta
		antSSE(w,
			ev("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}`),
			antToolStart(0, "t1", "ping"), antStop(0),
			ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":0}}`), antMsgStop())
	})
	var got []*iface.ChatToolCall
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hello there")}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			got = append(got, e.ToolCall)
		}
		return nil
	})
	if err != nil || len(got) != 1 || got[0].Arguments != "{}" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if out.Usage.InputTokens <= 0 || out.Usage.OutputTokens <= 0 {
		t.Errorf("usage must never be zero: %+v", out.Usage)
	}
}

func TestAnthropic_CacheTokensCountAsInput(t *testing.T) {
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		antSSE(w,
			ev("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"usage":{"input_tokens":5,"cache_creation_input_tokens":10,"cache_read_input_tokens":20,"output_tokens":1}}}`),
			antMsgDelta("end_turn", 7), antMsgStop())
	})
	out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
	if err != nil || out.Usage.InputTokens != 35 || out.Usage.OutputTokens != 7 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestAnthropic_HTTPAndStreamErrorsAreClassifiedWithoutLeaks(t *testing.T) {
	mode := ""
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "529":
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"sk-live-x"}}`))
		case "401":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"sk-live-x"}}`))
		case "403":
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"permission_error","message":"sk-live-x"}}`))
		case "402":
			w.WriteHeader(402)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"billing_error","message":"sk-live-x"}}`))
		case "429":
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"sk-live-x"}}`))
		case "400":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"sk-live-x"}}`))
		case "404":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"not_found_error","message":"model: sk-live-x"}}`))
		case "502_html":
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`<html>sk-live-x</html>`))
		case "stream_overloaded":
			antSSE(w, antStart(1, 1), ev("error", `{"type":"error","error":{"type":"overloaded_error","message":"sk-live-x"}}`))
		case "stream_invalid":
			antSSE(w, antStart(1, 1), ev("error", `{"type":"error","error":{"type":"invalid_request_error","message":"sk-live-x"}}`))
		case "stream_billing":
			antSSE(w, ev("error", `{"type":"error","error":{"type":"billing_error","message":"sk-live-x"}}`))
		case "stream_garbage":
			antSSE(w, ev("error", `not json sk-live-x`))
		case "truncated":
			antSSE(w, antStart(1, 1), antTextStart(0), antTextDelta(0, "x"))
		}
	})
	for _, c := range []struct {
		mode string
		want []error
	}{
		{"529", []error{iface.ErrLLMProviderUnavailable}},
		{"401", []error{ErrUnauthorized, iface.ErrLLMProviderUnavailable}},
		{"403", []error{ErrUnauthorized}},
		{"402", []error{iface.ErrLLMQuotaExhausted}},
		{"429", []error{iface.ErrLLMProviderUnavailable}},
		{"400", []error{iface.ErrLLMInvalidRequest}},
		{"404", []error{iface.ErrLLMCapabilityMismatch}},
		{"502_html", []error{iface.ErrLLMProviderUnavailable}},
		{"stream_overloaded", []error{iface.ErrLLMProviderUnavailable}},
		{"stream_invalid", []error{iface.ErrLLMInvalidRequest}},
		{"stream_billing", []error{iface.ErrLLMQuotaExhausted}},
		{"stream_garbage", []error{iface.ErrLLMProviderUnavailable}},
		{"truncated", []error{iface.ErrLLMProviderUnavailable}},
	} {
		mode = c.mode
		_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
		for _, want := range c.want {
			if !errors.Is(err, want) {
				t.Errorf("mode %s: err = %v, want %v", c.mode, err, want)
			}
		}
		if err != nil && (strings.Contains(err.Error(), "sk-live-x") || strings.Contains(err.Error(), "sk-ant-test") || strings.Contains(err.Error(), "127.0.0.1")) {
			t.Errorf("mode %s: provider text or URL leaked into error: %v", c.mode, err)
		}
	}
	// 401 on a non-stream call (model discovery) is classified the same way.
	mode = "401"
	if err := p.Health(context.Background()); !errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "sk-live-x") {
		t.Errorf("health err = %v", err)
	}
}

func TestAnthropic_SinkErrorAndCancelPropagate(t *testing.T) {
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		antSSE(w, antStart(1, 1), antTextStart(0), antTextDelta(0, "x"), antStop(0), antMsgDelta("end_turn", 1), antMsgStop())
	})
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
func TestAnthropic_StreamIsMarkedStreamingOnTheRequest(t *testing.T) {
	big := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events := []string{antStart(1, 1), antTextStart(0)}
		for i := 0; i < 8; i++ {
			events = append(events, antTextDelta(0, big))
		}
		antSSE(w, append(events, antStop(0), antMsgDelta("end_turn", 1), antMsgStop())...)
	}))
	defer srv.Close()
	client := NewHTTPClient(TransportConfig{ProductionLike: false, MaxResponseBytes: 1024, IdleTimeout: 5 * time.Second})
	p, err := NewAnthropic(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k", BaseURL: srv.URL}, client)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	_, err = p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, func(e iface.ChatEvent) error { n += len(e.Delta); return nil })
	if err != nil || n != 8*4096 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestAnthropic_DoesNotLeakProcessEnvironment(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-env-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-token")
	t.Setenv("ANTHROPIC_BASE_URL", "http://env.invalid")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Env: leaked")
	var hdr http.Header
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		antSSE(w, antStart(1, 1), antMsgDelta("end_turn", 1), antMsgStop())
	})
	if _, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink); err != nil {
		t.Fatal(err)
	}
	if hdr.Get("X-Api-Key") != "sk-ant-test" || hdr.Get("Authorization") != "" || hdr.Get("X-Env") != "" {
		t.Fatalf("headers = %v", hdr)
	}
}

func TestAnthropic_EffortAndThinkingFollowDeclaredCapabilities(t *testing.T) {
	var listCalls int32
	var captured map[string]any
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			atomic.AddInt32(&listCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(antModelsBody))
			return
		}
		captured = nil
		_ = json.NewDecoder(r.Body).Decode(&captured)
		antSSE(w, antStart(1, 1), antMsgDelta("end_turn", 1), antMsgStop())
	})
	chat := func(model, effort string, tools bool) map[string]any {
		req := userReq("hi")
		req.Options.Effort = effort
		if tools {
			req.Tools = []iface.ChatToolSpec{{Name: "t", InputSchema: map[string]any{"type": "object"}}}
		}
		if _, err := p.Chat(context.Background(), ChatCall{ModelID: model, Request: req}, noopSink); err != nil {
			t.Fatal(err)
		}
		return captured
	}
	// Declared: effort and adaptive thinking are sent.
	c := chat("claude-opus-5-5", "high", false)
	if cfg, _ := c["output_config"].(map[string]any); cfg["effort"] != "high" {
		t.Errorf("effort missing: %v", c["output_config"])
	}
	if th, _ := c["thinking"].(map[string]any); th["type"] != "adaptive" {
		t.Errorf("thinking = %v", c["thinking"])
	}
	// Tool conversations get the effort but not thinking (blocks cannot be round-tripped).
	c = chat("claude-opus-5-5", "low", true)
	if cfg, _ := c["output_config"].(map[string]any); cfg["effort"] != "low" {
		t.Errorf("effort missing with tools: %v", c)
	}
	if _, has := c["thinking"]; has {
		t.Error("thinking sent in a tool conversation")
	}
	// A level the model does not declare, a model that declares nothing and an
	// unknown model send neither.
	for _, tc := range []struct{ model, effort string }{{"claude-opus-5-5", "max"}, {"claude-old", "high"}, {"claude-unknown", "high"}} {
		c = chat(tc.model, tc.effort, false)
		if _, has := c["output_config"]; has {
			t.Errorf("%s/%s: output_config sent: %v", tc.model, tc.effort, c["output_config"])
		}
		if _, has := c["thinking"]; has {
			t.Errorf("%s/%s: thinking sent", tc.model, tc.effort)
		}
	}
	// No effort requested: no discovery request is made for it.
	before := atomic.LoadInt32(&listCalls)
	chat("claude-opus-5-5", "", false)
	if atomic.LoadInt32(&listCalls) != before {
		t.Error("catalog consulted without an effort")
	}
	// The catalog is cached per credential: all of the above cost one listing.
	if n := atomic.LoadInt32(&listCalls); n != 1 {
		t.Errorf("model listings = %d, want 1 (cached for an hour)", n)
	}
}

func TestAnthropic_CatalogCacheExpiresAndFailureDegradesToNoEffort(t *testing.T) {
	var listCalls int32
	fail := true
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			atomic.AddInt32(&listCalls, 1)
			if fail {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":{"type":"permission_error","message":"sk-live-x"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(antModelsBody))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, hasCfg := body["output_config"]
		if hasCfg {
			t.Errorf("effort sent although discovery failed: %v", body)
		}
		antSSE(w, antStart(1, 1), antMsgDelta("end_turn", 1), antMsgStop())
	})
	req := userReq("hi")
	req.Options.Effort = "high"
	for i := 0; i < 3; i++ { // chat still works; failure is remembered briefly
		if _, err := p.Chat(context.Background(), ChatCall{ModelID: "claude-opus-5-5", Request: req}, noopSink); err != nil {
			t.Fatal(err)
		}
	}
	if n := atomic.LoadInt32(&listCalls); n != 1 {
		t.Errorf("failed discovery re-tried %d times inside the failure TTL", n)
	}
	// After the failure TTL the catalog is fetched again.
	real := anthropicNow
	t.Cleanup(func() { anthropicNow = real })
	anthropicNow = func() time.Time { return real().Add(2 * time.Minute) }
	fail = false
	var captured map[string]any
	p2 := p.(*Anthropic)
	params, err := p2.buildParams(context.Background(), ChatCall{ModelID: "claude-opus-5-5", Request: req})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(params)
	_ = json.Unmarshal(b, &captured)
	if cfg, _ := captured["output_config"].(map[string]any); cfg["effort"] != "high" {
		t.Errorf("effort not sent after the catalog recovered: %s", b)
	}
	if n := atomic.LoadInt32(&listCalls); n != 2 {
		t.Errorf("listings = %d, want 2", n)
	}
	// A canceled caller is an error, not a silent downgrade.
	anthropicNow = func() time.Time { return real().Add(3 * time.Hour) }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Chat(ctx, ChatCall{ModelID: "claude-opus-5-5", Request: req}, noopSink); !errors.Is(err, context.Canceled) || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Errorf("cancel during discovery err = %v", err)
	}
}

func TestAnthropic_EmbedIsACapabilityMismatchAndListModels(t *testing.T) {
	var pages int32
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			atomic.AddInt32(&pages, 1)
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","type":"model"}],"has_more":true,"first_id":"claude-opus-5-5","last_id":"claude-opus-5-5"}`))
			return
		}
		if r.URL.Query().Get("after_id") != "claude-opus-5-5" {
			t.Errorf("after_id = %q", r.URL.Query().Get("after_id"))
		}
		atomic.AddInt32(&pages, 1)
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-5","display_name":"","type":"model"}],"has_more":false,"first_id":"claude-sonnet-5","last_id":"claude-sonnet-5"}`))
	})
	caps := p.Capabilities()
	if caps.Embeddings || !caps.Chat || !caps.Streaming || !caps.Tools || !caps.StructuredOutput {
		t.Errorf("capabilities = %+v", caps)
	}
	if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "m", Inputs: []string{"a"}}); !errors.Is(err, iface.ErrLLMCapabilityMismatch) {
		t.Errorf("embed err = %v", err)
	}
	ms, err := p.ListModels(context.Background())
	if err != nil || len(ms) != 2 || ms[0].ID != "claude-opus-5-5" || ms[0].DisplayName != "Claude Opus 5.5" || ms[1].ID != "claude-sonnet-5" || ms[1].DisplayName != "claude-sonnet-5" {
		t.Fatalf("models = %+v, %v", ms, err)
	}
	if atomic.LoadInt32(&pages) != 2 {
		t.Errorf("pages = %d", pages)
	}
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("health = %v", err)
	}
}

func TestAnthropic_ConstructorValidationAndRegistry(t *testing.T) {
	if _, err := NewAnthropic(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k"}, nil); err == nil {
		t.Error("nil client must be refused (it would bypass the SSRF guard)")
	}
	if _, err := NewAnthropic(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k", BaseURL: "ftp://x"}, http.DefaultClient); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Errorf("bad scheme err = %v", err)
	}
	r := NewRegistry()
	RegisterAll(r, func(string) *http.Client { return http.DefaultClient })
	p, err := r.Build("anthropic", Credential{Kind: iface.LLMCredentialKindOrg, Secret: "k"}, nil)
	if err != nil || p.Name() != "anthropic" {
		t.Fatalf("build: %v", err)
	}
}

func TestAnthropic_TemperatureIsDroppedWhenThinkingIsOn(t *testing.T) {
	var captured map[string]any
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(antModelsBody))
			return
		}
		captured = nil
		_ = json.NewDecoder(r.Body).Decode(&captured)
		antSSE(w, antStart(1, 1), antMsgDelta("end_turn", 1), antMsgStop())
	})
	def := 0.7
	run := func(model, effort string, reqTemp *float64) map[string]any {
		req := userReq("hi")
		req.Options.Effort = effort
		req.Options.Temperature = reqTemp
		if _, err := p.Chat(context.Background(), ChatCall{ModelID: model, Defaults: ChatDefaults{Temperature: &def}, Request: req}, noopSink); err != nil {
			t.Fatal(err)
		}
		return captured
	}
	// Thinking on: neither the model default nor a request temperature is sent.
	reqTemp := 0.2
	for _, rt := range []*float64{nil, &reqTemp} {
		c := run("claude-opus-5-5", "high", rt)
		if _, has := c["thinking"]; !has {
			t.Fatalf("thinking not sent: %v", c)
		}
		if _, has := c["temperature"]; has {
			t.Errorf("temperature sent together with thinking: %v", c["temperature"])
		}
	}
	// No thinking (model without the capability): the default still applies.
	if c := run("claude-old", "high", nil); c["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want the model default", c["temperature"])
	}
	// Effort without thinking (tool-free rule does not apply, no effort): default applies.
	if c := run("claude-opus-5-5", "", nil); c["temperature"] != 0.7 {
		t.Errorf("temperature without effort = %v", c["temperature"])
	}
}

func TestAnthropic_EmptyToolResultHasNoContentBlock(t *testing.T) {
	var captured map[string]any
	p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		antSSE(w, antStart(1, 1), antMsgDelta("end_turn", 1), antMsgStop())
	})
	_, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: iface.ChatRequest{Caller: "t", Messages: []iface.ChatMessage{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []iface.ChatToolCall{{ID: "c1", Name: "a"}, {ID: "c2", Name: "b"}}},
		{Role: "tool", ToolCallID: "c1", Content: ""},
		{Role: "tool", ToolCallID: "c2", Content: "ok"},
	}}}, noopSink)
	if err != nil {
		t.Fatal(err)
	}
	results := captured["messages"].([]any)[2].(map[string]any)["content"].([]any)
	empty, full := results[0].(map[string]any), results[1].(map[string]any)
	if empty["tool_use_id"] != "c1" || empty["type"] != "tool_result" {
		t.Fatalf("empty result = %v", empty)
	}
	if c, has := empty["content"]; has {
		t.Errorf("empty tool result carries content %v (an empty text block is rejected by the API)", c)
	}
	if b, _ := json.Marshal(full["content"]); !strings.Contains(string(b), `"text":"ok"`) {
		t.Errorf("non-empty result = %v", full)
	}
}

func TestAnthropic_DeltaUsageNeverShrinksTheStartTotal(t *testing.T) {
	cases := []struct {
		name, delta string
		wantIn      int
	}{
		{"smaller delta input keeps start total", `{"input_tokens":5,"output_tokens":7}`, 35},
		{"larger delta input wins", `{"input_tokens":50,"output_tokens":7}`, 50},
		{"delta cache fields count too", `{"input_tokens":2,"cache_read_input_tokens":60,"output_tokens":7}`, 62},
	}
	for _, c := range cases {
		p := newAnt(t, func(w http.ResponseWriter, r *http.Request) {
			antSSE(w,
				ev("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"usage":{"input_tokens":5,"cache_creation_input_tokens":10,"cache_read_input_tokens":20,"output_tokens":1}}}`),
				ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":`+c.delta+`}`), antMsgStop())
		})
		out, err := p.Chat(context.Background(), ChatCall{ModelID: "m", Request: userReq("hi")}, noopSink)
		if err != nil || out.Usage.InputTokens != c.wantIn || out.Usage.OutputTokens != 7 {
			t.Errorf("%s: out=%+v err=%v, want input %d", c.name, out, err, c.wantIn)
		}
	}
}
