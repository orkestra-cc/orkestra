package errors

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// RecordErrorCode is the response transformer that reports a coded error
// response (*errcode.Error) to the request logger: its machine-readable code
// and whether it opted in as an expected unavailability
// (errcode.FeatureNotConfigured).
//
// The access-log middleware (shared/middleware.RequestLogger) wraps the whole
// handler chain and only sees the status, so it cannot tell an optional feature
// this installation deliberately left unconfigured from a real server fault.
// This transformer sits on the one place every Huma error is serialised and
// stores both facts in the request's ctxauth.RequestAnnotations holder, which
// the logger reads after the handler returns. The code is never interpreted:
// only the error's own opt-in flag downgrades the log level. It never alters
// the response body and is a no-op when no holder is installed (tests, mounts
// without the logger) or the value is not an *errcode.Error.
//
// Install it once on the shared huma.Config next to HumaErrorDetailPolicy.
func RecordErrorCode() huma.Transformer {
	return func(ctx huma.Context, _ string, v any) (any, error) {
		e, ok := v.(*errcode.Error)
		if !ok || e == nil {
			return v, nil
		}
		ann := ctxauth.RequestAnnotationsFrom(ctx.Context())
		ann.SetErrorCode(e.Code)
		if e.ExpectedUnavailable() {
			ann.MarkExpectedUnavailable()
		}
		return v, nil
	}
}
