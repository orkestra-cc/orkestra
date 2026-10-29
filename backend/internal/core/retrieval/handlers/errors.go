// Package handlers holds the HTTP surface of the retrieval module: the
// tier-1 admin endpoints over the corpora, the indexing state and the
// search log.
package handlers

import (
	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// mapError turns a retrieval service error into the client-facing
// *errcode.Error. The code comes from services.AuditReason, the one table
// the audit trail also records, so the two cannot drift; this switch only
// picks the status and the sentence. The details are fixed sentences: the
// wrapped error text is never sent, so the handler logs the cause and the
// caller gets only the code and its sentence.
func mapError(err error) error {
	switch code := services.AuditReason(err); code {
	case errcode.RetrievalNotConfigured:
		return errcode.FeatureNotConfigured(code,
			"The vector engine is not configured on this installation.")
	case errcode.RetrievalUnavailable:
		return errcode.ServiceUnavailable(code,
			"The vector engine is unavailable. Try again shortly.")
	case errcode.RetrievalCorpusNotFound:
		return errcode.NotFound(code,
			"No retrieval state exists for this corpus.")
	case errcode.RetrievalCorpusRegistered:
		return errcode.Conflict(code,
			"The corpus is still registered. Unregister it before dropping its state.")
	case errcode.RetrievalCorpusUnavailable:
		return errcode.ServiceUnavailable(code,
			"The module that owns this corpus is not running.")
	case errcode.RetrievalForbidden:
		return errcode.Forbidden(code,
			"You may not access this corpus.")
	case errcode.RetrievalInvalidFilter:
		return errcode.UnprocessableEntity(code,
			"The filter does not fit this corpus.")
	case errcode.RetrievalInvalidRequest:
		return errcode.UnprocessableEntity(code,
			"The search request is not valid.")
	case errcode.RetrievalLocalityViolation:
		return errcode.Conflict(code,
			"An inference endpoint breaks the locality rule of this corpus.")
	case errcode.RetrievalVisibilityCheckFailed:
		return errcode.ServiceUnavailable(code,
			"The source visibility check could not run. Try again shortly.")
	default:
		return errcode.Internal(errcode.RetrievalInternal,
			"The retrieval request could not be completed.")
	}
}
