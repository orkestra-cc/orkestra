package services

import (
	"context"
	"fmt"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// CredentialRefresher is the optional second step of a CredentialResolver
// (ruling 5.2): after the provider rejected the resolved credential before
// any output, rotate it once and resolve the same model again.
type CredentialRefresher interface {
	Refresh(ctx context.Context, m models.Model, userUUID string, rejected providers.Credential) (providers.Credential, string, error)
}

// CompositeCredentialResolver dispatches on the model's credential kind
// (ruling 2.13): kind=org to the org resolver (catalog + vault), kind
// user_account to the caller's selected ChatGPT registration.
type CompositeCredentialResolver struct {
	org  CredentialResolver
	user *userAccountResolver // nil: linked accounts are not wired
}

var (
	_ CredentialResolver  = (*CompositeCredentialResolver)(nil)
	_ CredentialRefresher = (*CompositeCredentialResolver)(nil)
)

// NewCredentialResolver builds the router's resolver. accounts may be nil:
// kind=user_account then answers iface.ErrLLMUserAccountRequired, as in PR 2.
func NewCredentialResolver(catalog orgCredentialSource, accounts AccountCredentialSource) *CompositeCredentialResolver {
	r := &CompositeCredentialResolver{org: NewOrgCredentialResolver(catalog)}
	if accounts != nil {
		r.user = &userAccountResolver{accounts: accounts}
	}
	return r
}

func (r *CompositeCredentialResolver) Resolve(ctx context.Context, m models.Model, userUUID string) (providers.Credential, string, error) {
	if m.CredentialRef.Kind != models.CredentialKindUserAccount {
		return r.org.Resolve(ctx, m, userUUID)
	}
	if r.user == nil {
		return providers.Credential{}, "", iface.ErrLLMUserAccountRequired
	}
	return r.user.Resolve(ctx, m, userUUID)
}

// Refresh is defined for user_account models only: an org API key the
// provider rejected is the operator's to rotate, never retried.
func (r *CompositeCredentialResolver) Refresh(ctx context.Context, m models.Model, userUUID string, rejected providers.Credential) (providers.Credential, string, error) {
	if m.CredentialRef.Kind != models.CredentialKindUserAccount || r.user == nil {
		return providers.Credential{}, "", fmt.Errorf("%w: credential kind %s cannot be refreshed", iface.ErrLLMProviderUnavailable, m.CredentialRef.Kind)
	}
	return r.user.Refresh(ctx, m, userUUID, rejected)
}
