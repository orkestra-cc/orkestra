package services

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// seedModels seeds two active credentials of t1 (c1 openai, c2 anthropic)
// and four models riding on them.
func seedModels(mods *memModels, grants *memGrants, creds *memCreds) {
	creds.rows["c1"] = &models.Credential{UUID: "c1", TenantID: "t1", Name: "OpenAI", Provider: "openai", Status: models.CredentialStatusActive}
	creds.rows["c2"] = &models.Credential{UUID: "c2", TenantID: "t1", Name: "Anthropic", Provider: "anthropic", Status: models.CredentialStatusActive}
	c1 := models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: "c1"}
	mods.rows["m-everyone"] = &models.Model{UUID: "m-everyone", TenantID: "t1", Name: "Everyone", Provider: "openai", Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: c1,
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 20}}}
	mods.rows["m-granted"] = &models.Model{UUID: "m-granted", TenantID: "t1", Name: "Granted", Provider: "anthropic", Status: models.ModelStatusActive, Access: models.AccessGranted,
		Capabilities:  models.LLMModelCapabilities{Chat: true, Tools: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: "c2"},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 10}, {Purpose: "code", Priority: 1}}}
	mods.rows["m-disabled"] = &models.Model{UUID: "m-disabled", TenantID: "t1", Name: "Off", Provider: "openai", Status: models.ModelStatusDisabled, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Chat: true}, CredentialRef: c1, Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 1}}}
	mods.rows["m-embed"] = &models.Model{UUID: "m-embed", TenantID: "t1", Name: "Embed", Provider: "openai", Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Embeddings: true, Dimensions: 1536}, CredentialRef: c1, Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 5}}}
	grants.rows["m-granted"] = []string{"u1"}
}

// resolverEnv is a resolver over in-memory repos whose live config the test
// can change between calls.
type resolverEnv struct {
	a      *AccessResolver
	mods   *memModels
	grants *memGrants
	creds  *memCreds
	cfg    CatalogConfig
}

func newResolverEnv() *resolverEnv {
	e := &resolverEnv{
		mods:   &memModels{rows: map[string]*models.Model{}},
		grants: &memGrants{rows: map[string][]string{}},
		creds:  &memCreds{rows: map[string]*models.Credential{}},
		cfg:    CatalogConfig{AllowHosted: true},
	}
	e.a = NewAccessResolver(e.mods, e.grants, e.creds, func() CatalogConfig { return e.cfg })
	return e
}

func newResolverForTest() (*AccessResolver, *memModels, *memGrants, *memCreds) {
	e := newResolverEnv()
	return e.a, e.mods, e.grants, e.creds
}

func TestAccessResolver_Candidates(t *testing.T) {
	a, mods, grants, creds := newResolverForTest()
	seedModels(mods, grants, creds)
	ctx := ctxFor("t1")

	// u1 has a grant: granted model first (priority 10 < 20); disabled and embed-only excluded for chat.
	got, err := a.Candidates(ctx, "u1", "default", Need{Chat: true})
	if err != nil || len(got) != 2 || got[0].UUID != "m-granted" || got[1].UUID != "m-everyone" {
		t.Fatalf("u1 candidates = %v, %v", ids(got), err)
	}
	// u2 has no grant: only everyone.
	got, _ = a.Candidates(ctx, "u2", "default", Need{Chat: true})
	if len(got) != 1 || got[0].UUID != "m-everyone" {
		t.Fatalf("u2 candidates = %v", ids(got))
	}
	// No user (job): only everyone.
	got, _ = a.Candidates(ctx, "", "default", Need{Chat: true})
	if len(got) != 1 || got[0].UUID != "m-everyone" {
		t.Fatalf("job candidates = %v", ids(got))
	}
	// Tools needed: only the granted anthropic model, and only for u1.
	got, _ = a.Candidates(ctx, "u1", "default", Need{Chat: true, Tools: true})
	if len(got) != 1 || got[0].UUID != "m-granted" {
		t.Fatalf("tools candidates = %v", ids(got))
	}
	if got, _ := a.Candidates(ctx, "u2", "default", Need{Chat: true, Tools: true}); len(got) != 0 {
		t.Fatalf("u2 tools candidates = %v, want none", ids(got))
	}
	// Unknown purpose falls back to default.
	got, _ = a.Candidates(ctx, "u1", "summaries", Need{Chat: true})
	if len(got) != 2 {
		t.Fatalf("fallback candidates = %v", ids(got))
	}
	// Specific purpose wins and uses its own priority.
	got, _ = a.Candidates(ctx, "u1", "code", Need{Chat: true})
	if len(got) != 1 || got[0].UUID != "m-granted" {
		t.Fatalf("code candidates = %v", ids(got))
	}
	// Embeddings need.
	got, _ = a.Candidates(ctx, "", "default", Need{Embeddings: true})
	if len(got) != 1 || got[0].UUID != "m-embed" {
		t.Fatalf("embed candidates = %v", ids(got))
	}
}

func TestAccessResolver_NotConfiguredVsNoEligible(t *testing.T) {
	a, mods, grants, creds := newResolverForTest()
	if _, err := a.Candidates(ctxFor("t1"), "u1", "default", Need{Chat: true}); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("empty org err = %v", err)
	}
	seedModels(mods, grants, creds)
	got, err := a.Candidates(ctxFor("t1"), "u2", "default", Need{Chat: true, StructuredOutput: true})
	if err != nil || len(got) != 0 {
		t.Fatalf("no eligible: got %v, %v (resolver returns empty, the gateway maps it)", ids(got), err)
	}
}

// Spec Routing step 2: fall back to "default" only when the purpose has no
// model in the org, never because the caller cannot use, or the model cannot
// serve, the purpose that does exist.
func TestAccessResolver_PurposeFallbackOnlyWhenPurposeHasNoModels(t *testing.T) {
	a, mods, grants, creds := newResolverForTest()
	seedModels(mods, grants, creds)
	ctx := ctxFor("t1")

	// "code" is declared by m-granted only, and only u1 holds the grant.
	got, err := a.Candidates(ctx, "u2", "code", Need{Chat: true})
	if err != nil || len(got) != 0 {
		t.Fatalf("u2 on a purpose it cannot use = %v, %v; want empty (no silent move to default)", ids(got), err)
	}
	if got, _ := a.Candidates(ctx, "", "code", Need{Chat: true}); len(got) != 0 {
		t.Fatalf("job on code = %v, want empty", ids(got))
	}

	// "chatonly" is declared by m-everyone, which has no tools: a tools
	// request must not be moved onto the default models (m-granted has tools).
	mods.rows["m-everyone"].Purposes = append(mods.rows["m-everyone"].Purposes, models.LLMModelPurpose{Purpose: "chatonly", Priority: 1})
	got, err = a.Candidates(ctx, "u1", "chatonly", Need{Chat: true, Tools: true})
	if err != nil || len(got) != 0 {
		t.Fatalf("tools on a purpose without tool models = %v, %v; want empty", ids(got), err)
	}
	got, _ = a.Candidates(ctx, "u1", "chatonly", Need{Chat: true})
	if len(got) != 1 || got[0].UUID != "m-everyone" {
		t.Fatalf("chatonly without tools = %v", ids(got))
	}

	// A purpose no active model declares falls back to default; a purpose
	// declared only by a disabled model counts as undeclared.
	got, _ = a.Candidates(ctx, "u1", "nobody-declares-this", Need{Chat: true})
	if len(got) != 2 || got[0].UUID != "m-granted" {
		t.Fatalf("undeclared purpose = %v, want the default models", ids(got))
	}
	mods.rows["m-disabled"].Purposes = append(mods.rows["m-disabled"].Purposes, models.LLMModelPurpose{Purpose: "legacy", Priority: 1})
	got, _ = a.Candidates(ctx, "u1", "legacy", Need{Chat: true})
	if len(got) != 2 {
		t.Fatalf("purpose of a disabled model = %v, want default fallback", ids(got))
	}
}

// Review Focus 5: the "my models" listing must never widen access.
func TestAccessResolver_UsableNeverWidensAccess(t *testing.T) {
	a, mods, grants, creds := newResolverForTest()
	seedModels(mods, grants, creds)
	ctx := ctxFor("t1")

	// No user in context: only the models open to everyone.
	got, err := a.Usable(ctx, "")
	if err != nil || len(got) != 2 || got[0].UUID != "m-embed" || got[1].UUID != "m-everyone" {
		t.Fatalf("anonymous usable = %v, %v", ids(got), err)
	}
	// A user without grants: no "granted" model.
	got, _ = a.Usable(ctx, "u2")
	for _, m := range got {
		if m.UUID == "m-granted" {
			t.Fatalf("u2 must not see the granted model: %v", ids(got))
		}
	}
	if len(got) != 2 {
		t.Fatalf("u2 usable = %v", ids(got))
	}
	// A granted user sees it, in name order.
	got, _ = a.Usable(ctx, "u1")
	if len(got) != 3 || got[0].UUID != "m-embed" || got[1].UUID != "m-everyone" || got[2].UUID != "m-granted" {
		t.Fatalf("u1 usable = %v", ids(got))
	}
	// A grant on a disabled model does not resurrect it.
	grants.rows["m-disabled"] = []string{"u1"}
	got, _ = a.Usable(ctx, "u1")
	for _, m := range got {
		if m.UUID == "m-disabled" {
			t.Fatalf("disabled model must stay unusable: %v", ids(got))
		}
	}
}

func ids(ms []models.Model) []string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.UUID)
	}
	return out
}

// I1: allow_hosted is a privacy opt-in read on every operation. Turning it
// off takes the hosted models out of every listing and every route at once,
// without anyone editing them; the mock provider likewise in production.
func TestAccessResolver_HostedAndMockFollowTheLiveConfig(t *testing.T) {
	e := newResolverEnv()
	seedModels(e.mods, e.grants, e.creds)
	e.creds.rows["c-oll"] = &models.Credential{UUID: "c-oll", TenantID: "t1", Name: "Local", Provider: models.ProviderOllama, Status: models.CredentialStatusActive}
	e.creds.rows["c-mock"] = &models.Credential{UUID: "c-mock", TenantID: "t1", Name: "Mock", Provider: models.ProviderMock, Status: models.CredentialStatusActive}
	e.mods.rows["m-local"] = &models.Model{UUID: "m-local", TenantID: "t1", Name: "Local", Provider: models.ProviderOllama, Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Chat: true}, CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: "c-oll"},
		Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 30}}}
	e.mods.rows["m-mock"] = &models.Model{UUID: "m-mock", TenantID: "t1", Name: "Mock", Provider: models.ProviderMock, Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Chat: true}, CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: "c-mock"},
		Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 40}}}
	e.mods.rows["m-sub"] = &models.Model{UUID: "m-sub", TenantID: "t1", Name: "Subscription", Provider: models.ProviderOpenAI, Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Chat: true}, CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindUserAccount},
		Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 50}}}
	ctx := ctxFor("t1")

	got, _ := e.a.Usable(ctx, "u1")
	if len(got) != 6 {
		t.Fatalf("allow_hosted on: usable = %v, want all six active models", ids(got))
	}

	e.cfg = CatalogConfig{AllowHosted: false}
	got, err := e.a.Usable(ctx, "u1")
	if err != nil || len(got) != 2 || got[0].UUID != "m-local" || got[1].UUID != "m-mock" {
		t.Fatalf("allow_hosted off: usable = %v, %v; want only the local and mock models", ids(got), err)
	}
	got, _ = e.a.Candidates(ctx, "u1", "default", Need{Chat: true})
	if len(got) != 2 || got[0].UUID != "m-local" {
		t.Fatalf("allow_hosted off: candidates = %v", ids(got))
	}
	// The purpose only a hosted model declares now counts as undeclared and
	// falls back to default, which only the local models serve.
	got, _ = e.a.Candidates(ctx, "u1", "code", Need{Chat: true})
	if len(got) != 2 || got[0].UUID != "m-local" {
		t.Fatalf("allow_hosted off: code candidates = %v", ids(got))
	}

	e.cfg = CatalogConfig{AllowHosted: false, ProductionLike: true}
	got, _ = e.a.Usable(ctx, "u1")
	if len(got) != 1 || got[0].UUID != "m-local" {
		t.Fatalf("production, allow_hosted off: usable = %v, want only ollama", ids(got))
	}
	// Nothing left but hosted/mock models is an org with nothing configured.
	delete(e.mods.rows, "m-local")
	if _, err := e.a.Candidates(ctx, "u1", "default", Need{Chat: true}); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("only gated models left: err = %v, want ErrLLMNotConfigured", err)
	}
}

// I1: a model whose org credential is disabled or gone is not usable, even
// though the model itself is still active.
func TestAccessResolver_CredentialMustBeActive(t *testing.T) {
	e := newResolverEnv()
	seedModels(e.mods, e.grants, e.creds)
	ctx := ctxFor("t1")

	e.creds.rows["c2"].Status = models.CredentialStatusDisabled
	got, _ := e.a.Usable(ctx, "u1")
	for _, m := range got {
		if m.UUID == "m-granted" {
			t.Fatalf("a model on a disabled credential is still usable: %v", ids(got))
		}
	}
	got, _ = e.a.Candidates(ctx, "u1", "default", Need{Chat: true})
	if len(got) != 1 || got[0].UUID != "m-everyone" {
		t.Fatalf("candidates with c2 disabled = %v", ids(got))
	}

	delete(e.creds.rows, "c1")
	got, _ = e.a.Usable(ctx, "u1")
	if len(got) != 0 {
		t.Fatalf("models on a missing or disabled credential = %v, want none", ids(got))
	}
	if _, err := e.a.Candidates(ctx, "u1", "default", Need{Chat: true}); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("no live model left: err = %v", err)
	}

	// A credential of another org with the same UUID does not count.
	e.creds.rows["c1"] = &models.Credential{UUID: "c1", TenantID: "t2", Name: "OpenAI", Provider: "openai", Status: models.CredentialStatusActive}
	if got, _ := e.a.Usable(ctx, "u1"); len(got) != 0 {
		t.Fatalf("a foreign org's credential made models usable: %v", ids(got))
	}
}
