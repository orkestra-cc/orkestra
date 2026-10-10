// backend/internal/core/llm/services/errors.go
package services

import "errors"

var (
	// ErrSecretKeyMissing: neither KMS nor LLM_SECRET_ENCRYPTION_KEY; the
	// handler maps it to llm.secret_key_missing (503).
	ErrSecretKeyMissing = errors.New("llm: no secret encryption key available")
	// ErrEnvelopeCorrupt: ciphertext does not open under its AAD — copied
	// between tenants/records/fields, truncated, or sealed with another key.
	ErrEnvelopeCorrupt = errors.New("llm: secret envelope cannot be opened")
	// ErrInvalidEnvelopeContext: Seal/Open called with an empty AAD part or
	// one containing the '|' separator (a caller bug, refused fail-closed).
	ErrInvalidEnvelopeContext = errors.New("llm: envelope context part is empty or contains '|'")
	ErrNotFound               = errors.New("llm: not found")
	ErrNameInUse              = errors.New("llm: name already in use")
	ErrCredentialInUse        = errors.New("llm: credential referenced by an active model")
	ErrEndpointNotAllowed     = errors.New("llm: endpoint not allowed")
	ErrHostedDisabled         = errors.New("llm: hosted providers are disabled")
	ErrMockNotAllowed         = errors.New("llm: mock provider is not allowed here")
	ErrGrantNotMember         = errors.New("llm: grantee is not a member of this organization")
	// ErrEmptySubject: a data-subject request without a user UUID; the
	// DSR queries would otherwise match every row with an empty actor.
	ErrEmptySubject = errors.New("llm: data-subject request without a subject")
	// ErrInvalidStatus, ErrProviderMismatch and ErrCredentialDisabled are
	// validation details, wrapped in iface.ErrLLMInvalidRequest by the
	// catalog. On the call path (NewOrgCredentialResolver) the last two are
	// wrapped in iface.ErrLLMProviderUnavailable instead: a credential
	// disabled after candidate selection is not the caller's mistake.
	ErrInvalidStatus    = errors.New("llm: status must be active or disabled")
	ErrProviderMismatch = errors.New("llm: the credential provider does not match the model provider")
	// ErrCredentialDisabled: a model that would end up active rides on a
	// disabled credential.
	ErrCredentialDisabled = errors.New("llm: the model's credential is disabled")
	// ErrModelDisabled: an admin prompt test on a model that is not active
	// (wrapped in iface.ErrLLMInvalidRequest).
	ErrModelDisabled = errors.New("llm: the model is disabled")
	// ErrInvalidUsageRange: the usage report window is empty, reversed or
	// longer than 366 days (wrapped in iface.ErrLLMInvalidRequest).
	ErrInvalidUsageRange = errors.New("llm: invalid usage report range")
	// ErrInvalidGroupBy: the usage report grouping is not model, user or
	// caller (wrapped in iface.ErrLLMInvalidRequest).
	ErrInvalidGroupBy = errors.New("llm: invalid usage report grouping")
	// ErrInvalidBudget: a budget limit, warn threshold or action out of
	// range (wrapped in iface.ErrLLMInvalidRequest).
	ErrInvalidBudget = errors.New("llm: invalid budget")
	// ErrBudgetRecipientNotMember: an extra budget recipient is not a
	// member of this organization (wrapped in iface.ErrLLMInvalidRequest).
	ErrBudgetRecipientNotMember = errors.New("llm: budget recipient is not a member of this organization")
	// ErrInvalidPrompt: an admin prompt test without text or over 4000
	// bytes (wrapped in iface.ErrLLMInvalidRequest).
	ErrInvalidPrompt = errors.New("llm: invalid test prompt")
)
