package services

import (
	"errors"
	"log/slog"
	"math"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

func newGatewayForTest(t *testing.T) *Gateway {
	g, _ := newGatewayWithModels(t)
	return g
}

func newGatewayWithModels(t *testing.T) (*Gateway, *memModels) {
	t.Helper()
	svc, creds, mods, grants, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	seedModels(mods, grants, creds)
	v, err := NewVault(randomKeyHex(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	cfg := func() CatalogConfig { return svc.cfg() }
	return NewGateway(NewAccessResolver(mods, grants, creds, cfg), svc, providers.NewRegistry(), v, slog.Default()), mods
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

// I1: the gateway reads allow_hosted on every call, so turning it off stops
// Resolve, ListUsable and Chat from offering a hosted model right away.
func TestGateway_AllowHostedOffHidesHostedModels(t *testing.T) {
	svc, creds, mods, grants, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	seedModels(mods, grants, creds)
	v, err := NewVault(randomKeyHex(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	g := NewGateway(NewAccessResolver(mods, grants, creds, func() CatalogConfig { return svc.cfg() }), svc, providers.NewRegistry(), v, slog.Default())
	ctx := ctxFor("t1")
	if info, err := g.Resolve(ctx, "default"); err != nil || info.UUID != "m-everyone" {
		t.Fatalf("Resolve with allow_hosted on = %+v, %v", info, err)
	}

	svc.cfg = func() CatalogConfig { return CatalogConfig{AllowHosted: false} }
	if list, err := g.ListUsable(ctx); err != nil || len(list) != 0 {
		t.Fatalf("ListUsable with allow_hosted off = %+v, %v", list, err)
	}
	if _, err := g.Resolve(ctx, "default"); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("Resolve with allow_hosted off err = %v", err)
	}
	_, err = g.Chat(ctx, iface.ChatRequest{Caller: "test", Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}}, nil)
	if !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("Chat with allow_hosted off err = %v", err)
	}
	if _, err := g.Embed(ctx, iface.EmbedRequest{Caller: "test", Inputs: []string{"hi"}}); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("Embed with allow_hosted off err = %v", err)
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
	g, mods := newGatewayWithModels(t)
	// Give the external org a usable model so that a missing guard would
	// surface as success or provider-unavailable, not as "not configured".
	mods.rows["m-ext"] = &models.Model{UUID: "m-ext", TenantID: "ext-1", Name: "Ext", Provider: "openai", Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Chat: true, Embeddings: true, Dimensions: 8}, Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 1}}}
	ext := ctxauthExternal(t)
	msg := []iface.ChatMessage{{Role: "user", Content: "hi"}}

	if _, err := g.ListUsable(ext); !errors.Is(err, tenantrepo.ErrTenantKindMismatch) {
		t.Errorf("ListUsable err = %v, want ErrTenantKindMismatch", err)
	}
	if _, err := g.Resolve(ext, "default"); !errors.Is(err, tenantrepo.ErrTenantKindMismatch) {
		t.Errorf("Resolve err = %v, want ErrTenantKindMismatch", err)
	}
	if _, err := g.Chat(ext, iface.ChatRequest{Caller: "test", Messages: msg}, nil); !errors.Is(err, tenantrepo.ErrTenantKindMismatch) {
		t.Errorf("Chat err = %v, want ErrTenantKindMismatch", err)
	}
	if _, err := g.Embed(ext, iface.EmbedRequest{Caller: "test", Inputs: []string{"hi"}}); !errors.Is(err, tenantrepo.ErrTenantKindMismatch) {
		t.Errorf("Embed err = %v, want ErrTenantKindMismatch", err)
	}
}

func TestGateway_PurposeWithoutUsableModelIsNotRoutedToDefault(t *testing.T) {
	g := newGatewayForTest(t)
	// "code" exists (m-granted) but u-admin holds no grant: no silent fallback.
	msg := []iface.ChatMessage{{Role: "user", Content: "hi"}}
	if _, err := g.Chat(ctxFor("t1"), iface.ChatRequest{Caller: "test", Purpose: "code", Messages: msg}, nil); !errors.Is(err, iface.ErrLLMNoEligibleModel) {
		t.Fatalf("Chat err = %v, want ErrLLMNoEligibleModel", err)
	}
	if _, err := g.Resolve(ctxFor("t1"), "code"); !errors.Is(err, iface.ErrLLMNoEligibleModel) {
		t.Fatalf("Resolve err = %v, want ErrLLMNoEligibleModel", err)
	}
	// A purpose nobody declares still resolves through default.
	info, err := g.Resolve(ctxFor("t1"), "summaries")
	if err != nil || info.UUID != "m-everyone" {
		t.Fatalf("Resolve undeclared purpose = %+v, %v", info, err)
	}
}

func TestGateway_RejectsNaNTemperatureAndUnserializableSchema(t *testing.T) {
	nan := math.NaN()
	req := iface.ChatRequest{Caller: "ok", Messages: []iface.ChatMessage{{Role: "user", Content: "x"}}, Options: iface.ChatOptions{Temperature: &nan}}
	if err := ValidateChatRequest(req); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Errorf("NaN temperature err = %v", err)
	}
	req = iface.ChatRequest{Caller: "ok", Messages: []iface.ChatMessage{{Role: "user", Content: "x"}},
		Tools: []iface.ChatToolSpec{{Name: "t", InputSchema: map[string]any{"bad": make(chan int)}}}}
	if err := ValidateChatRequest(req); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Errorf("unserializable schema err = %v", err)
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
