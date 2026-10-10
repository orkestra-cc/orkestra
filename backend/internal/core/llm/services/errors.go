// backend/internal/core/llm/services/errors.go
package services

import "errors"

var (
	// ErrSecretKeyMissing: neither KMS nor LLM_SECRET_ENCRYPTION_KEY; the
	// handler maps it to llm.secret_key_missing (503).
	ErrSecretKeyMissing = errors.New("llm: no secret encryption key available")
	// ErrEnvelopeCorrupt: ciphertext does not open under its AAD — copied
	// between tenants/records/fields, truncated, or sealed with another key.
	ErrEnvelopeCorrupt    = errors.New("llm: secret envelope cannot be opened")
	ErrNotFound           = errors.New("llm: not found")
	ErrNameInUse          = errors.New("llm: name already in use")
	ErrCredentialInUse    = errors.New("llm: credential referenced by an active model")
	ErrEndpointNotAllowed = errors.New("llm: endpoint not allowed")
	ErrHostedDisabled     = errors.New("llm: hosted providers are disabled")
	ErrMockNotAllowed     = errors.New("llm: mock provider is not allowed here")
	ErrGrantNotMember     = errors.New("llm: grantee is not a member of this organization")
	// ErrInvalidStatus and ErrProviderMismatch are validation details,
	// always wrapped in iface.ErrLLMInvalidRequest.
	ErrInvalidStatus    = errors.New("llm: status must be active or disabled")
	ErrProviderMismatch = errors.New("llm: the credential provider does not match the model provider")
)
