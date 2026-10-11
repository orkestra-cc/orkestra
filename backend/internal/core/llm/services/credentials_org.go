package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// CredentialResolver turns a candidate model into the decrypted material
// its provider call needs, plus the credential identity used for the
// breaker key and the ledger. It runs inside the call path only; the
// returned secret must not outlive the provider call or reach a log.
type CredentialResolver interface {
	Resolve(ctx context.Context, m models.Model, userUUID string) (providers.Credential, string, error)
}

// orgCredentialSource is what the org resolver needs from the catalog
// (*CatalogService satisfies it).
type orgCredentialSource interface {
	GetCredential(ctx context.Context, id string) (*models.Credential, error)
	OpenCredentialSecret(ctx context.Context, c *models.Credential) (string, error)
}

type orgCredentialResolver struct{ catalog orgCredentialSource }

// NewOrgCredentialResolver resolves kind=org models through the catalog
// and the vault. kind=user_account answers iface.ErrLLMUserAccountRequired
// until PR 3 brings linked accounts.
func NewOrgCredentialResolver(catalog orgCredentialSource) CredentialResolver {
	return orgCredentialResolver{catalog: catalog}
}

func (r orgCredentialResolver) Resolve(ctx context.Context, m models.Model, _ string) (providers.Credential, string, error) {
	switch m.CredentialRef.Kind {
	case models.CredentialKindOrg:
	case models.CredentialKindUserAccount:
		return providers.Credential{}, "", iface.ErrLLMUserAccountRequired
	default:
		return providers.Credential{}, "", fmt.Errorf("%w: unknown credential kind", iface.ErrLLMProviderUnavailable)
	}
	c, err := r.catalog.GetCredential(ctx, m.CredentialRef.CredentialUUID)
	switch {
	case errors.Is(err, ErrNotFound):
		// Deleted between candidate selection and the call.
		return providers.Credential{}, "", fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrCredentialDisabled)
	case err != nil:
		return providers.Credential{}, "", fmt.Errorf("%w: read credential: %w", iface.ErrLLMProviderUnavailable, err)
	case c.Status != models.CredentialStatusActive:
		return providers.Credential{}, "", fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrCredentialDisabled)
	case c.Provider != m.Provider:
		return providers.Credential{}, "", fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrProviderMismatch)
	}
	secret, err := r.catalog.OpenCredentialSecret(ctx, c)
	if err != nil {
		// Vault errors carry no plaintext; the cause stays a sentinel.
		return providers.Credential{}, "", fmt.Errorf("%w: open credential: %w", iface.ErrLLMProviderUnavailable, err)
	}
	return providers.Credential{Kind: models.CredentialKindOrg, Secret: secret, BaseURL: c.BaseURL}, c.UUID, nil
}
