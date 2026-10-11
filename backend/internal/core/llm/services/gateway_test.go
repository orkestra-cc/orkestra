package services

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"strings"
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

// Final review I2: Options.MaxOutputTokens is bounded like every other
// output limit (1-131072); an unbounded value would inflate the
// reservation.
func TestGateway_MaxOutputTokensIsBounded(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want bool
	}{{1, true}, {models.MaxOutputTokens, true}, {models.MaxOutputTokens + 1, false}, {1_000_000_000, false}, {0, false}} {
		n := tc.n
		req := iface.ChatRequest{Caller: "ok", Messages: []iface.ChatMessage{{Role: "user", Content: "x"}}, Options: iface.ChatOptions{MaxOutputTokens: &n}}
		err := ValidateChatRequest(req)
		if tc.want && err != nil {
			t.Errorf("maxOutputTokens %d rejected: %v", n, err)
		}
		if !tc.want && !errors.Is(err, iface.ErrLLMInvalidRequest) {
			t.Errorf("maxOutputTokens %d err = %v, want ErrLLMInvalidRequest", n, err)
		}
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

func TestGateway_ChatRoutesThroughRouter(t *testing.T) {
	g, _ := newGatewayWithModels(t)
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": ok("hi", 2, 1)})
	g.SetRouter(env.r)
	res, err := g.Chat(ctxFor("t1"), iface.ChatRequest{Caller: "test", Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}}, nil)
	if err != nil || res.Model.UUID != "m-everyone" || res.Usage.OutputTokens != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if ev := singleLedger(t, env.store); ev.UserUUID != "u-admin" || ev.ModelUUID != "m-everyone" {
		t.Fatalf("ledger = %+v", ev)
	}
	eres, err := g.Embed(ctxFor("t1"), iface.EmbedRequest{Caller: "test", Inputs: []string{"hi"}})
	if err != nil || eres.Model.UUID != "m-embed" {
		t.Fatalf("embed = %+v, %v", eres, err)
	}
}

// PR 1 carry-over (T7.b): instructions, tool descriptions and the response
// schema are bounded like messages and tool schemas.
func TestGateway_BoundsInstructionsDescriptionsAndResponseSchema(t *testing.T) {
	g := newGatewayForTest(t)
	msg := []iface.ChatMessage{{Role: "user", Content: "x"}}
	big := strings.Repeat("a", maxInstructionsBytes+1)
	cases := map[string]iface.ChatRequest{
		"instructions":     {Caller: "ok", Messages: msg, Instructions: big},
		"tool description": {Caller: "ok", Messages: msg, Tools: []iface.ChatToolSpec{{Name: "f", Description: strings.Repeat("d", maxToolDescriptionBytes+1), InputSchema: map[string]any{}}}},
		"response schema":  {Caller: "ok", Messages: msg, ResponseFormat: &iface.JSONSchemaFormat{Name: "out", Schema: map[string]any{"description": strings.Repeat("s", maxToolSchemaBytes)}}},
		"response NaN":     {Caller: "ok", Messages: msg, ResponseFormat: &iface.JSONSchemaFormat{Name: "out", Schema: map[string]any{"x": math.NaN()}}},
		"response name":    {Caller: "ok", Messages: msg, ResponseFormat: &iface.JSONSchemaFormat{Name: strings.Repeat("n", 65), Schema: map[string]any{}}},
	}
	for name, req := range cases {
		if _, err := g.Chat(ctxFor("t1"), req, nil); !errors.Is(err, iface.ErrLLMInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrLLMInvalidRequest", name, err)
		}
	}
	okReq := iface.ChatRequest{Caller: "ok", Messages: msg, Instructions: strings.Repeat("a", maxInstructionsBytes),
		ResponseFormat: &iface.JSONSchemaFormat{Name: "out", Schema: map[string]any{"type": "object"}}}
	if err := ValidateChatRequest(okReq); err != nil {
		t.Fatalf("within bounds rejected: %v", err)
	}
}

// Ruling 5.1: listing and Resolve show a user_account model only when the
// caller's selected account is usable and its plan catalog lists the
// model; Chat keeps the classified per-call error.
func TestGateway_PlanModelsFollowTheSelectedAccount(t *testing.T) {
	g, mods := newGatewayWithModels(t)
	for _, m := range []models.Model{userAccountModel("m-plan", "gpt-5.6-terra"), userAccountModel("m-off-plan", "gpt-not-in-plan")} {
		m.Name, m.Purposes = m.UUID, []models.LLMModelPurpose{{Purpose: "siwc", Priority: 1}}
		if m.UUID == "m-off-plan" {
			m.Purposes[0].Priority = 0 // would win Resolve if it were not filtered
		}
		mods.rows[m.UUID] = &m
	}
	ctx := ctxFor("t1")
	listed := func() map[string]bool {
		t.Helper()
		list, err := g.ListUsable(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, m := range list {
			out[m.UUID] = true
		}
		return out
	}
	// No accounts wired: never listed.
	if got := listed(); got["m-plan"] || got["m-off-plan"] || !got["m-everyone"] {
		t.Fatalf("unwired list = %v", got)
	}

	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"gpt-5.6-terra"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	g.SetAccounts(h.svc)
	// Wired, nothing linked: hidden; Resolve finds nothing for the purpose.
	if got := listed(); got["m-plan"] || got["m-off-plan"] {
		t.Fatalf("unlinked list = %v", got)
	}
	if _, err := g.Resolve(ctx, "siwc"); !errors.Is(err, iface.ErrLLMNoEligibleModel) {
		t.Fatalf("unlinked Resolve err = %v", err)
	}

	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	if got := listed(); !got["m-plan"] || got["m-off-plan"] || !got["m-everyone"] {
		t.Fatalf("linked list = %v", got)
	}
	if info, err := g.Resolve(ctx, "siwc"); err != nil || info.UUID != "m-plan" {
		t.Fatalf("linked Resolve = %+v, %v", info, err)
	}
	// The catalog is cached per account: listing twice reads it once.
	var plans int
	oidc.locked(func() { plans = oidc.planCalls })
	listed()
	oidc.locked(func() {
		if oidc.planCalls != plans {
			t.Errorf("plan catalog re-read within the cache window (%d → %d)", plans, oidc.planCalls)
		}
	})

	// needs_reauth: hidden again, while Chat still says why.
	h.repo.edit(acc.UUID, func(a *models.UserAccount) { a.Status = models.AccountStatusNeedsReauth })
	if got := listed(); got["m-plan"] {
		t.Fatalf("needs_reauth list = %v", got)
	}
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": ok("x", 1, 1)})
	catalog, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	env.r.creds = NewCredentialResolver(catalog, h.svc)
	g.SetRouter(env.r)
	req := iface.ChatRequest{Caller: "test", Purpose: "siwc", Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}}
	if _, err := g.Chat(ctx, req, nil); !errors.Is(err, iface.ErrLLMUserAccountReauth) {
		t.Fatalf("Chat err = %v", err)
	}
}

// The audit sink compliance pushes into the gateway reaches the accounts
// service too (account link/unlink events).
func TestGateway_ForwardsAuditSinkToAccounts(t *testing.T) {
	g := newGatewayForTest(t)
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"gpt-5.6-terra"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	g.SetAccounts(h.svc)
	sink := &syncAudit{}
	g.SetAuditSink(sink)
	h.linkAccount(t, ctxFor("t1"), "oaiapp_1", "c")
	if len(sink.actions()) == 0 {
		t.Fatal("no account audit event reached the gateway's sink")
	}
}

// TenantsWithActiveModels makes the in-memory repo an ActiveModelTenants.
func (m *memModels) TenantsWithActiveModels(context.Context) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range m.rows {
		if r.Status == models.ModelStatusActive && !seen[r.TenantID] {
			seen[r.TenantID] = true
			out = append(out, r.TenantID)
		}
	}
	sort.Strings(out)
	return out, nil
}

// The availability probe runs with no user or org in context (a module
// health check): it looks across orgs, ignores grants and follows the
// live rules and the purpose fallback routing uses.
func TestGateway_HasLiveModel(t *testing.T) {
	g, mods := newGatewayWithModels(t)
	ctx := context.Background()
	tools := iface.LLMCapabilities{Chat: true, Tools: true}
	if _, err := g.HasLiveModel(ctx, "support", tools); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("unwired probe err = %v", err)
	}
	g.SetActiveModelTenants(mods)
	for _, tc := range []struct {
		purpose string
		need    iface.LLMCapabilities
		want    bool
	}{
		// support is declared by no model: the default purpose serves it,
		// and m-granted (tools, granted to u1 only) counts.
		{"support", tools, true},
		{"code", tools, true},
		{"default", iface.LLMCapabilities{Embeddings: true}, true},
		{"default", iface.LLMCapabilities{Chat: true, StructuredOutput: true}, false},
	} {
		if ok, err := g.HasLiveModel(ctx, tc.purpose, tc.need); err != nil || ok != tc.want {
			t.Fatalf("HasLiveModel(%s, %+v) = %v, %v; want %v", tc.purpose, tc.need, ok, err, tc.want)
		}
	}

	// A model that is not live (disabled) does not count.
	mods.rows["m-granted"].Status = models.ModelStatusDisabled
	if ok, err := g.HasLiveModel(ctx, "support", tools); err != nil || ok {
		t.Fatalf("with the tools model disabled = %v, %v", ok, err)
	}
	// Any org will do.
	other := *mods.rows["m-granted"]
	other.UUID, other.TenantID, other.Status = "m-t2", "t2", models.ModelStatusActive
	other.CredentialRef = models.LLMCredentialRef{Kind: models.CredentialKindUserAccount}
	mods.rows[other.UUID] = &other
	if ok, err := g.HasLiveModel(ctx, "support", tools); err != nil || !ok {
		t.Fatalf("with a tools model in another org = %v, %v", ok, err)
	}
	if _, err := g.HasLiveModel(ctx, "Bad Purpose", tools); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("bad purpose err = %v", err)
	}
}
