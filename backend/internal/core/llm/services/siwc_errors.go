package services

import (
	"errors"

	"github.com/orkestra/backend/internal/core/llm/providers"
)

// SIWC classification identities live in providers (the OpenAI adapter
// classifies plan-route codes too, and providers cannot import services);
// these are the same values, re-exported for the services callers.
var (
	ErrSIWCRefreshTerminal = providers.ErrSIWCRefreshTerminal // never leaves the module: becomes iface.ErrLLMUserAccountReauth
	ErrSIWCInvalidUser     = providers.ErrSIWCInvalidUser
	ErrSIWCInvalidClient   = providers.ErrSIWCInvalidClient
	ErrSIWCExchangeFailed  = providers.ErrSIWCExchangeFailed
)

// Link-flow outcomes, produced and mapped inside services/handlers only.
var (
	ErrSIWCStateInvalid    = errors.New("llm siwc: state invalid, expired or already used")
	ErrSIWCDenied          = errors.New("llm siwc: the user denied the authorization")
	ErrSIWCIDTokenInvalid  = errors.New("llm siwc: id token did not validate")
	ErrSIWCCallbackInvalid = errors.New("llm siwc: pasted callback URL does not match the attempt")
)
