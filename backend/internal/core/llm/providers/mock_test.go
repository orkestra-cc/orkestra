package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestMock_IsDeterministicAndStreams(t *testing.T) {
	p, _ := NewMock(Credential{Kind: iface.LLMCredentialKindOrg}, nil)
	var out []string
	var usage *iface.LLMUsage
	outcome, err := p.Chat(context.Background(), ChatCall{ModelID: "mock-1", Request: iface.ChatRequest{Caller: "t", Messages: []iface.ChatMessage{{Role: "user", Content: "ping"}}}}, func(e iface.ChatEvent) error {
		if e.Delta != "" {
			out = append(out, e.Delta)
		}
		if e.Done {
			usage = e.Usage
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out, "") != "mock: ping" || usage == nil || usage.InputTokens == 0 || outcome.FinishReason != "stop" {
		t.Fatalf("out=%q usage=%+v outcome=%+v", strings.Join(out, ""), usage, outcome)
	}
	emb, err := p.Embed(context.Background(), EmbedCall{ModelID: "mock-embed", Inputs: []string{"a", "b"}})
	if err != nil || len(emb.Vectors) != 2 || len(emb.Vectors[0]) != 8 {
		t.Fatalf("embed = %+v, %v", emb, err)
	}
	if got := p.Capabilities().Dimensions; got != 8 {
		t.Fatalf("Dimensions = %d", got)
	}
}

func TestMock_ToolCallWhenToolsOffered(t *testing.T) {
	p, _ := NewMock(Credential{}, nil)
	var tc *iface.ChatToolCall
	outcome, _ := p.Chat(context.Background(), ChatCall{ModelID: "mock-1", Request: iface.ChatRequest{Caller: "t",
		Messages: []iface.ChatMessage{{Role: "user", Content: "use the tool"}},
		Tools:    []iface.ChatToolSpec{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}}}, func(e iface.ChatEvent) error {
		if e.ToolCall != nil {
			tc = e.ToolCall
		}
		return nil
	})
	if tc == nil || tc.Name != "lookup" || outcome.FinishReason != "tool_calls" || !strings.HasPrefix(tc.Arguments, "{") {
		t.Fatalf("tool call = %+v, outcome = %+v", tc, outcome)
	}
}

func TestMock_ChunksKeepRunesIntact(t *testing.T) {
	for _, c := range splitChunks("héllo wörld ✓✓✓", 4) {
		if strings.ContainsRune(c, '�') || !strings.Contains("héllo wörld ✓✓✓", c) {
			t.Fatalf("chunk %q split a rune", c)
		}
	}
	if got := strings.Join(splitChunks("héllo wörld ✓✓✓", 4), ""); got != "héllo wörld ✓✓✓" {
		t.Fatalf("rejoined = %q", got)
	}
}

func TestMock_SinkErrorAndCancelAbort(t *testing.T) {
	p, _ := NewMock(Credential{}, nil)
	call := ChatCall{ModelID: "mock-1", Request: iface.ChatRequest{Messages: []iface.ChatMessage{{Role: "user", Content: "hello there"}}}}
	boom := errors.New("boom")
	if _, err := p.Chat(context.Background(), call, func(iface.ChatEvent) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("sink error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Chat(ctx, call, func(iface.ChatEvent) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}
