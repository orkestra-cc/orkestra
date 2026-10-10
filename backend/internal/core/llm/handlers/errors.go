// Package handlers holds the HTTP surface of the llm module: the Tier-1
// admin endpoints over the catalog and the self endpoint over the gateway.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// validationDetails gives every validation sentinel the services wrap in
// iface.ErrLLMInvalidRequest its own client-facing sentence. The sentinel
// text is never sent: it is written for logs and may change.
var validationDetails = []struct {
	err    error
	detail string
}{
	{models.ErrInvalidName, "The name must be 1 to 64 letters, digits, spaces, dots, underscores or dashes, starting with a letter or digit."},
	{models.ErrInvalidProvider, "The provider is not one this installation supports."},
	{models.ErrBaseURLFixed, "This provider has a fixed endpoint; leave the base URL empty."},
	{models.ErrBaseURLRequired, "This provider needs a base URL."},
	{models.ErrBaseURLNotAllowed, "This provider takes no base URL."},
	{models.ErrSecretRequired, "This provider needs an API key."},
	{models.ErrInvalidModelID, "The model ID must be 1 to 128 letters, digits, dots, dashes, underscores, colons or slashes."},
	{models.ErrInvalidCredentialRef, "Choose an organization credential, or the user's own linked account without a credential."},
	{models.ErrUserAccountProvider, "A model billed to the user's linked account must use the OpenAI provider."},
	{models.ErrUserAccountCapabilities, "A model billed to the user's linked account cannot offer embeddings."},
	{models.ErrUserAccountDefaults, "A model billed to the user's linked account cannot set a temperature or an output limit."},
	{models.ErrInvalidPurposes, "Give 1 to 16 distinct purposes, each a lowercase key such as default or summarize."},
	{models.ErrInvalidAccess, "Access must be granted users only, or everyone in the organization."},
	{models.ErrInvalidCapabilities, "The model must offer chat or embeddings; embeddings need a dimension count above zero."},
	{models.ErrInvalidEffort, "The effort must be empty, low, medium or high."},
	{models.ErrInvalidReserve, "The reserved output tokens must be between 1 and 131072."},
	{services.ErrInvalidStatus, "The status must be active or disabled."},
	{services.ErrProviderMismatch, "The credential belongs to a different provider than the model."},
}

const invalidRequestDetail = "The request was rejected because a field is out of range or malformed."

func invalidDetail(err error) string {
	for _, v := range validationDetails {
		if errors.Is(err, v.err) {
			return v.detail
		}
	}
	return invalidRequestDetail
}

// MapError turns service and iface sentinels into the stable llm.* codes.
// Details are written sentences, never err.Error() (errquality R1).
// Unknown errors and tenant-scope wiring faults (a route mounted without
// the tenant context the repositories require) are the server's fault: a
// generic 500 whose cause the caller logs (see failure).
func MapError(err error) error {
	switch {
	case errors.Is(err, iface.ErrLLMInvalidRequest):
		return errcode.UnprocessableEntity(errcode.LLMInvalidRequest, invalidDetail(err))
	case errors.Is(err, services.ErrNotFound):
		return errcode.NotFound(errcode.LLMNotFound, "No such credential or model in this organization.")
	case errors.Is(err, services.ErrNameInUse):
		return errcode.Conflict(errcode.LLMNameInUse, "Another credential or model in this organization already has this name.")
	case errors.Is(err, services.ErrCredentialInUse):
		return errcode.Conflict(errcode.LLMCredentialInUse, "An active model still uses this credential; disable or delete the model first.")
	case errors.Is(err, services.ErrSecretKeyMissing):
		return errcode.ServiceUnavailable(errcode.LLMSecretKeyMissing, "API keys cannot be stored: this deployment has no secret encryption key.")
	case errors.Is(err, services.ErrEndpointNotAllowed):
		return errcode.UnprocessableEntity(errcode.LLMEndpointNotAllowed, "The base URL points to an address that is not allowed in this environment.")
	case errors.Is(err, services.ErrHostedDisabled):
		return errcode.UnprocessableEntity(errcode.LLMHostedDisabled, "Hosted providers are disabled; enable Allow hosted providers in the module settings first.")
	case errors.Is(err, services.ErrMockNotAllowed):
		return errcode.UnprocessableEntity(errcode.LLMMockNotAllowed, "The mock provider is not available in production-like environments.")
	case errors.Is(err, services.ErrGrantNotMember):
		return errcode.UnprocessableEntity(errcode.LLMGrantNotMember, "Every user granted access must be a member of this organization.")
	case errors.Is(err, iface.ErrLLMNotConfigured):
		return errcode.FeatureNotConfigured(errcode.LLMNotConfigured, "No language model is configured for this organization yet.")
	case errors.Is(err, iface.ErrLLMNoEligibleModel):
		return errcode.Forbidden(errcode.LLMNoEligibleModel, "No model you may use fits this request.")
	case errors.Is(err, iface.ErrLLMModelAccessDenied):
		return errcode.Forbidden(errcode.LLMModelAccessDenied, "You may not use the selected model.")
	case errors.Is(err, iface.ErrLLMCapabilityMismatch):
		return errcode.UnprocessableEntity(errcode.LLMCapabilityMismatch, "The request needs a capability the model does not offer.")
	case errors.Is(err, iface.ErrLLMProviderUnavailable):
		return errcode.ServiceUnavailable(errcode.LLMProviderUnavailable, "The model provider cannot be reached right now.")
	case errors.Is(err, tenantrepo.ErrTenantScopeMissing), errors.Is(err, tenantrepo.ErrTenantKindMismatch):
		return errcode.Internal("", "The organization context of this request could not be resolved.")
	}
	return errcode.Internal("", "The language model settings could not be processed.")
}

// failure maps err for the client and logs the cause of every server-side
// failure, which the response never carries. The expected not_configured
// 503 and every 4xx are outcomes, not faults, and are not logged.
func failure(ctx context.Context, logger *slog.Logger, op string, err error) error {
	mapped := MapError(err)
	var e *errcode.Error
	if !errors.As(mapped, &e) || e.Status < http.StatusInternalServerError || e.ExpectedUnavailable() {
		return mapped
	}
	level := slog.LevelWarn
	if e.Status == http.StatusInternalServerError {
		level = slog.LevelError
	}
	logger.Log(ctx, level, "llm: request failed", slog.String("op", op), slog.String("code", e.Code), slog.String("error", err.Error()))
	return mapped
}
