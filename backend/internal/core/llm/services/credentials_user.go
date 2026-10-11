package services

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// userAccountResolver resolves kind=user_account models to the caller's
// selected ChatGPT registration (ruling 2.13): the access token of the
// selected, active, plan-granted account, and only for a model the
// account's plan catalog lists. The returned credential identity is the
// account UUID (breaker key and ledger).
type userAccountResolver struct{ accounts AccountCredentialSource }

func (r userAccountResolver) Resolve(ctx context.Context, m models.Model, userUUID string) (providers.Credential, string, error) {
	if userUUID == "" {
		// A job with no user in context has no account to bill.
		return providers.Credential{}, "", iface.ErrLLMUserAccountRequired
	}
	cred, err := r.accounts.CredentialFor(ctx, userUUID)
	if err != nil {
		return providers.Credential{}, "", err
	}
	return r.fromAccount(ctx, m, cred)
}

// Refresh rotates the token the provider rejected before any output (an
// inference 401, ruling 5.2) and resolves m again with the new one. When
// the stored set has already moved on (a concurrent refresh), that set is
// used without another rotation. A terminal refresh answers
// iface.ErrLLMUserAccountReauth (the account is then needs_reauth).
func (r userAccountResolver) Refresh(ctx context.Context, m models.Model, userUUID string, rejected providers.Credential) (providers.Credential, string, error) {
	if userUUID == "" {
		return providers.Credential{}, "", iface.ErrLLMUserAccountRequired
	}
	cred, err := r.accounts.CredentialFor(ctx, userUUID)
	if err != nil {
		return providers.Credential{}, "", err
	}
	if subtle.ConstantTimeCompare([]byte(cred.AccessToken), []byte(rejected.Secret)) == 1 {
		if cred, err = r.accounts.ForceRefresh(ctx, cred); err != nil {
			return providers.Credential{}, "", err
		}
	}
	return r.fromAccount(ctx, m, cred)
}

func (r userAccountResolver) fromAccount(ctx context.Context, m models.Model, cred *AccountCredential) (providers.Credential, string, error) {
	if m.Provider != models.ProviderOpenAI {
		return providers.Credential{}, "", fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, ErrProviderMismatch)
	}
	slugs, err := r.accounts.PlanModels(ctx, cred)
	if err != nil {
		return providers.Credential{}, "", err
	}
	if !slices.Contains(slugs, m.ModelID) {
		return providers.Credential{}, "", fmt.Errorf("%w: model %s is not in the linked account catalog", iface.ErrLLMCapabilityMismatch, m.ModelID)
	}
	// BaseURL "": the adapter's default origin, the one the plan catalog
	// was read from.
	return providers.Credential{Kind: models.CredentialKindUserAccount, Secret: cred.AccessToken}, cred.Account.UUID, nil
}

// planCatalogFor is what listing needs (ruling 5.1): the selected
// account's plan catalog, or an error when the caller has no usable
// account (none linked, not selected, needs_reauth, plan disabled, or the
// catalog cannot be read right now).
func planCatalogFor(ctx context.Context, accounts AccountCredentialSource, userUUID string) ([]string, error) {
	if accounts == nil || userUUID == "" {
		return nil, iface.ErrLLMUserAccountRequired
	}
	cred, err := accounts.CredentialFor(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	return accounts.PlanModels(ctx, cred)
}

// accountClassified reports whether err is one of the linked-account
// outcomes a consumer classifies on its own (not an availability fault).
func accountClassified(err error) bool {
	return errors.Is(err, iface.ErrLLMUserAccountReauth) || errors.Is(err, iface.ErrLLMUserAccountRequired) ||
		errors.Is(err, iface.ErrLLMPlanUsageDisabled) || errors.Is(err, iface.ErrLLMCapabilityMismatch)
}
