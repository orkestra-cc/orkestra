package services

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func seedModels(mods *memModels, grants *memGrants) {
	mods.rows["m-everyone"] = &models.Model{UUID: "m-everyone", TenantID: "t1", Name: "Everyone", Provider: "openai", Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: "c1"},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 20}}}
	mods.rows["m-granted"] = &models.Model{UUID: "m-granted", TenantID: "t1", Name: "Granted", Provider: "anthropic", Status: models.ModelStatusActive, Access: models.AccessGranted,
		Capabilities:  models.LLMModelCapabilities{Chat: true, Tools: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: "c2"},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 10}, {Purpose: "code", Priority: 1}}}
	mods.rows["m-disabled"] = &models.Model{UUID: "m-disabled", TenantID: "t1", Name: "Off", Provider: "openai", Status: models.ModelStatusDisabled, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Chat: true}, Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 1}}}
	mods.rows["m-embed"] = &models.Model{UUID: "m-embed", TenantID: "t1", Name: "Embed", Provider: "openai", Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities: models.LLMModelCapabilities{Embeddings: true, Dimensions: 1536}, Purposes: []models.LLMModelPurpose{{Purpose: "default", Priority: 5}}}
	grants.rows["m-granted"] = []string{"u1"}
}

func newResolverForTest() (*AccessResolver, *memModels, *memGrants) {
	mods := &memModels{rows: map[string]*models.Model{}}
	grants := &memGrants{rows: map[string][]string{}}
	return NewAccessResolver(mods, grants), mods, grants
}

func TestAccessResolver_Candidates(t *testing.T) {
	a, mods, grants := newResolverForTest()
	seedModels(mods, grants)
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
	a, mods, grants := newResolverForTest()
	if _, err := a.Candidates(ctxFor("t1"), "u1", "default", Need{Chat: true}); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("empty org err = %v", err)
	}
	seedModels(mods, grants)
	got, err := a.Candidates(ctxFor("t1"), "u2", "default", Need{Chat: true, StructuredOutput: true})
	if err != nil || len(got) != 0 {
		t.Fatalf("no eligible: got %v, %v (resolver returns empty, the gateway maps it)", ids(got), err)
	}
}

// Review Focus 5: the "my models" listing must never widen access.
func TestAccessResolver_UsableNeverWidensAccess(t *testing.T) {
	a, mods, grants := newResolverForTest()
	seedModels(mods, grants)
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
