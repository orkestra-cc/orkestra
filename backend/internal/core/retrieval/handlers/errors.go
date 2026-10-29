// Package handlers holds the HTTP surface of the retrieval module: the
// tier-1 admin endpoints over the corpora, the indexing state and the
// search log.
package handlers

import (
	"errors"

	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// mapError turns a retrieval service error into the client-facing
// *errcode.Error. The details are fixed sentences: the wrapped error text
// is never sent, so the handler logs the cause and the caller gets only
// the code and its sentence.
func mapError(err error) error {
	switch {
	case errors.Is(err, iface.ErrRetrievalNotConfigured):
		return errcode.FeatureNotConfigured(errcode.RetrievalNotConfigured,
			"The vector engine is not configured on this installation.")
	case errors.Is(err, iface.ErrRetrievalUnavailable):
		return errcode.ServiceUnavailable(errcode.RetrievalUnavailable,
			"The vector engine is unavailable. Try again shortly.")
	case errors.Is(err, iface.ErrRetrievalUnknownCorpus),
		errors.Is(err, services.ErrCorpusNotFound):
		return errcode.NotFound(errcode.RetrievalCorpusNotFound,
			"No retrieval state exists for this corpus.")
	case errors.Is(err, services.ErrCorpusRegistered):
		return errcode.Conflict(errcode.RetrievalCorpusRegistered,
			"The corpus is still registered. Unregister it before dropping its state.")
	case errors.Is(err, iface.ErrRetrievalCorpusUnavailable):
		return errcode.ServiceUnavailable(errcode.RetrievalCorpusUnavailable,
			"The module that owns this corpus is not running.")
	case errors.Is(err, iface.ErrRetrievalForbidden):
		return errcode.Forbidden(errcode.RetrievalForbidden,
			"You may not access this corpus.")
	case errors.Is(err, iface.ErrRetrievalInvalidFilter):
		return errcode.UnprocessableEntity(errcode.RetrievalInvalidFilter,
			"The filter does not fit this corpus.")
	case errors.Is(err, iface.ErrRetrievalInvalidRequest):
		return errcode.UnprocessableEntity(errcode.RetrievalInvalidRequest,
			"The search request is not valid.")
	case errors.Is(err, iface.ErrRetrievalLocality):
		return errcode.Conflict(errcode.RetrievalLocalityViolation,
			"An inference endpoint breaks the locality rule of this corpus.")
	case errors.Is(err, iface.ErrRetrievalVisibilityCheck):
		return errcode.ServiceUnavailable(errcode.RetrievalVisibilityCheckFailed,
			"The source visibility check could not run. Try again shortly.")
	default:
		return errcode.Internal(errcode.RetrievalInternal,
			"The retrieval request could not be completed.")
	}
}
