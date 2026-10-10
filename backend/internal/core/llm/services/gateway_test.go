package services

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func newGatewayForTest(t *testing.T) *Gateway {
	t.Helper()
	svc, _, mods, grants, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	seedModels(mods, grants)
	v, err := NewVault(randomKeyHex(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return NewGateway(NewAccessResolver(mods, grants), svc, providers.NewRegistry(), v, slog.Default())
}

func TestGateway_ListUsableAndResolve(t *testing.T) {
	g := newGatewayForTest(t)
	ctx := ctxFor("t1") // identity u-admin, no grants
	list, err := g.ListUsable(ctx)
	if err != nil || len(list) != 2 { // m-everyone + m-embed
		t.Fatalf("ListUsable = %+v, %v", list, err)
	}
	info, err := g.Resolve(ctx, "default")
	if err != nil || info.UUID != "m-everyone" {
		t.Fatalf("Resolve = %+v, %v", info, err)
	}
	if _, err := g.Resolve(ctx, "Bad Purpose"); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("Resolve bad purpose err = %v", err)
	}
}

func TestGateway_ChatWithoutProvidersIsUnavailable(t *testing.T) {
	g := newGatewayForTest(t)
	_, err := g.Chat(ctxFor("t1"), iface.ChatRequest{Caller: "test", Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}}, func(iface.ChatEvent) error { return nil })
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("chat err = %v, want ErrLLMProviderUnavailable", err)
	}
	_, err = g.Embed(ctxFor("t1"), iface.EmbedRequest{Caller: "test", Inputs: []string{"hi"}})
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("embed err = %v, want ErrLLMProviderUnavailable", err)
	}
}

func TestGateway_ChatMapsEmptyCandidatesAndUnconfiguredOrgs(t *testing.T) {
	g := newGatewayForTest(t)
	msg := []iface.ChatMessage{{Role: "user", Content: "hi"}}
	// Models exist but none has tools for u-admin (granted model needs a grant).
	_, err := g.Chat(ctxFor("t1"), iface.ChatRequest{Caller: "test", Messages: msg, Tools: []iface.ChatToolSpec{{Name: "lookup", InputSchema: map[string]any{}}}}, nil)
	if !errors.Is(err, iface.ErrLLMNoEligibleModel) {
		t.Fatalf("no eligible err = %v", err)
	}
	// Another org with no active model at all.
	_, err = g.Chat(ctxFor("t9"), iface.ChatRequest{Caller: "test", Messages: msg}, nil)
	if !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("not configured err = %v", err)
	}
}

func TestGateway_ValidatesRequestBeforeResolving(t *testing.T) {
	g := newGatewayForTest(t)
	cases := []iface.ChatRequest{
		{Caller: "", Messages: []iface.ChatMessage{{Role: "user", Content: "x"}}},
		{Caller: "ok", Purpose: "Bad Purpose", Messages: []iface.ChatMessage{{Role: "user", Content: "x"}}},
		{Caller: "ok", Messages: []iface.ChatMessage{{Role: "system", Content: "x"}}},
		{Caller: "ok", Messages: []iface.ChatMessage{{Role: "tool", Content: "x"}}},
		{Caller: "ok"},
	}
	for i, req := range cases {
		if _, err := g.Chat(ctxFor("t1"), req, nil); !errors.Is(err, iface.ErrLLMInvalidRequest) {
			t.Errorf("case %d err = %v, want ErrLLMInvalidRequest", i, err)
		}
	}
	for i, req := range []iface.EmbedRequest{
		{Caller: "", Inputs: []string{"x"}},
		{Caller: "ok"},
		{Caller: "ok", Inputs: []string{""}},
	} {
		if _, err := g.Embed(ctxFor("t1"), req); !errors.Is(err, iface.ErrLLMInvalidRequest) {
			t.Errorf("embed case %d err = %v, want ErrLLMInvalidRequest", i, err)
		}
	}
}

func TestGateway_PinnedModelMustBeCandidate(t *testing.T) {
	g := newGatewayForTest(t)
	_, err := g.Chat(ctxFor("t1"), iface.ChatRequest{Caller: "test", ModelUUID: "m-granted", Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}}, nil)
	if !errors.Is(err, iface.ErrLLMModelAccessDenied) {
		t.Fatalf("err = %v, want ErrLLMModelAccessDenied", err)
	}
}

func TestGateway_RequiresInternalTenant(t *testing.T) {
	g := newGatewayForTest(t)
	ext := ctxauthExternal(t)
	if _, err := g.ListUsable(ext); err == nil {
		t.Fatal("ListUsable: external tenant must be refused")
	}
	if _, err := g.Resolve(ext, "default"); err == nil {
		t.Fatal("Resolve: external tenant must be refused")
	}
	_, err := g.Chat(ext, iface.ChatRequest{Caller: "test", Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}}, nil)
	if err == nil || errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("Chat: external tenant must be refused before routing, got %v", err)
	}
}

func TestGateway_ToolNamesMayBeMixedCase(t *testing.T) {
	good := iface.ChatRequest{Caller: "ok", Messages: []iface.ChatMessage{{Role: "user", Content: "x"}}, Tools: []iface.ChatToolSpec{{Name: "getWeather", InputSchema: map[string]any{}}}}
	if err := ValidateChatRequest(good); err != nil {
		t.Fatalf("mixed-case tool name rejected: %v", err)
	}
	bad := good
	bad.Tools = []iface.ChatToolSpec{{Name: "get weather", InputSchema: map[string]any{}}}
	if err := ValidateChatRequest(bad); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("tool name with a space err = %v", err)
	}
}

func TestProviderRegistry_UnknownKind(t *testing.T) {
	r := providers.NewRegistry()
	if _, err := r.Build("openai", providers.Credential{}, nil); !errors.Is(err, providers.ErrUnknownProvider) {
		t.Fatalf("err = %v", err)
	}
	if len(r.Kinds()) != 0 {
		t.Fatalf("kinds = %v", r.Kinds())
	}
}
