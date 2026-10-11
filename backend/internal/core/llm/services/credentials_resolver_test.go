package services

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func userAccountModel(uuid, modelID string) models.Model {
	m := model(uuid, models.ProviderOpenAI, "")
	m.ModelID = modelID
	m.Capabilities = models.LLMModelCapabilities{Chat: true, Streaming: true}
	m.CredentialRef = models.LLMCredentialRef{Kind: models.CredentialKindUserAccount}
	return m
}

func TestResolver_UserAccountPaths(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"gpt-5.6-terra"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	r := NewCredentialResolver(svc, h.svc)
	m := userAccountModel("m", "gpt-5.6-terra")
	ctx := ctxFor("t1")

	if _, _, err := r.Resolve(ctx, m, ""); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("job err = %v", err)
	}
	if _, _, err := r.Resolve(ctx, m, "u-admin"); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("unlinked err = %v", err)
	}
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	cred, uuid, err := r.Resolve(ctx, m, "u-admin")
	if err != nil || cred.Kind != models.CredentialKindUserAccount || cred.Secret != "at-c" || cred.BaseURL != "" || uuid != acc.UUID {
		t.Fatalf("resolve = kind %q base %q uuid %q (want %q) %v", cred.Kind, cred.BaseURL, uuid, acc.UUID, err)
	}
	miss := m
	miss.ModelID = "gpt-not-in-plan"
	if _, _, err := r.Resolve(ctx, miss, "u-admin"); !errors.Is(err, iface.ErrLLMCapabilityMismatch) {
		t.Fatalf("catalog miss err = %v", err)
	}
	// Another user of the org has no account of their own.
	if _, _, err := r.Resolve(ctxForUser("t1", "u2"), m, "u2"); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("other user err = %v", err)
	}
	h.repo.edit(acc.UUID, func(a *models.UserAccount) { a.Status = models.AccountStatusNeedsReauth })
	if _, _, err := r.Resolve(ctx, m, "u-admin"); !errors.Is(err, iface.ErrLLMUserAccountReauth) {
		t.Fatalf("needs_reauth err = %v", err)
	}
	h.repo.edit(acc.UUID, func(a *models.UserAccount) { a.Status = models.AccountStatusPlanDisabled })
	if _, _, err := r.Resolve(ctx, m, "u-admin"); !errors.Is(err, iface.ErrLLMPlanUsageDisabled) {
		t.Fatalf("plan_disabled err = %v", err)
	}

	// The org path is the PR 2 resolver, unchanged.
	c := openAICredential(t, svc, ctx, "Key")
	if cred, id, err := r.Resolve(ctx, model("o", models.ProviderOpenAI, c.UUID), "u-admin"); err != nil || cred.Kind != models.CredentialKindOrg || id != c.UUID {
		t.Fatalf("org resolve = %q %q %v", cred.Kind, id, err)
	}
	// Without linked accounts wired, user_account keeps the PR 2 answer.
	if _, _, err := NewCredentialResolver(svc, nil).Resolve(ctx, m, "u-admin"); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("unwired err = %v", err)
	}
}

// Ruling 5.2: Refresh rotates the rejected token once; when the stored set
// already moved on it is reused without another rotation; a terminal
// refresh answers reauth and flags the account.
func TestResolver_UserAccountRefresh(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"gpt-5.6-terra"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	h.svc.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	r := NewCredentialResolver(svc, h.svc)
	m := userAccountModel("m", "gpt-5.6-terra")
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")

	first, _, err := r.Resolve(ctx, m, "u-admin")
	if err != nil {
		t.Fatal(err)
	}
	next, id, err := r.Refresh(ctx, m, "u-admin", first)
	if err != nil || next.Secret == first.Secret || next.Secret != "at-r1" || id != acc.UUID {
		t.Fatalf("refresh = %v (rotated %v, id ok %v)", err, next.Secret != first.Secret, id == acc.UUID)
	}
	// A second caller still holding the old token gets the rotated set.
	again, _, err := r.Refresh(ctx, m, "u-admin", first)
	if err != nil || again.Secret != next.Secret {
		t.Fatalf("stale holder: %v (same %v)", err, again.Secret == next.Secret)
	}
	oidc.locked(func() {
		if oidc.refreshes != 1 {
			t.Errorf("refreshes = %d, want 1", oidc.refreshes)
		}
		oidc.refreshErr = providers.ClassifySIWCCode("invalid_grant")
	})
	if _, _, err := r.Refresh(ctx, m, "u-admin", next); !errors.Is(err, iface.ErrLLMUserAccountReauth) {
		t.Fatalf("terminal refresh err = %v", err)
	}
	if row, _ := h.repo.row(acc.UUID); row.Status != models.AccountStatusNeedsReauth {
		t.Fatalf("status = %q", row.Status)
	}
	// An org model is never refreshed.
	if _, _, err := r.Refresh(ctx, model("o", models.ProviderOpenAI, "c1"), "u-admin", providers.Credential{}); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("org refresh err = %v", err)
	}
}
