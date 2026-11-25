package services

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestOrgCredentialResolver(t *testing.T) {
	svc, creds, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	c := openAICredential(t, svc, ctx, "Key")
	r := NewOrgCredentialResolver(svc)
	m := model("m1", models.ProviderOpenAI, c.UUID)

	cred, credUUID, err := r.Resolve(ctx, m, "u")
	if err != nil || cred.Secret != "sk-live-abcdef" || cred.Kind != models.CredentialKindOrg || cred.BaseURL != c.BaseURL || cred.BaseURL == "" || credUUID != c.UUID {
		t.Fatalf("resolve = %+v %q %v", cred, credUUID, err)
	}

	ua := m
	ua.CredentialRef = models.LLMCredentialRef{Kind: models.CredentialKindUserAccount}
	if _, _, err := r.Resolve(ctx, ua, "u"); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("user_account err = %v", err)
	}

	missing := model("m2", models.ProviderOpenAI, "nope")
	if _, _, err := r.Resolve(ctx, missing, "u"); !errors.Is(err, iface.ErrLLMProviderUnavailable) || !errors.Is(err, ErrCredentialDisabled) {
		t.Fatalf("missing credential err = %v", err)
	}

	mismatch := model("m3", models.ProviderAnthropic, c.UUID)
	if _, _, err := r.Resolve(ctx, mismatch, "u"); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("provider mismatch err = %v", err)
	}

	// Another org cannot resolve t1's credential.
	if _, _, err := r.Resolve(ctxFor("t2"), m, "u"); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("cross-tenant err = %v", err)
	}

	creds.rows[c.UUID].Status = models.CredentialStatusDisabled
	_, _, err = r.Resolve(ctx, m, "u")
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || !errors.Is(err, ErrCredentialDisabled) {
		t.Fatalf("disabled err = %v", err)
	}
	if errorCodeOf(err) != "credential_disabled" {
		t.Fatalf("code = %q", errorCodeOf(err))
	}
}
