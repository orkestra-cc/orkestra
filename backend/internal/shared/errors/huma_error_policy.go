package errors

import (
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// HumaErrorDetailPolicy is the response transformer that decides what a Huma
// error body may carry. Install it once on the shared huma.Config
// (cmd/server/main.go) and every API built from that config — both audience
// surfaces and every module sub-router — inherits it.
//
// The problem it closes: huma.NewError serialises every wrapped error into
// `errors[].message` as err.Error(), and the auth handlers alone pass an
// infrastructure error into huma.Error5xx(msg, err) at some twenty-five
// sites. A Redis or Mongo outage therefore answered anonymous callers with
// dial errors naming hosts and ports. A bare (non-StatusError) error returned
// from a handler takes the same path: Huma wraps it as a 500 whose detail is
// the error text.
//
// The rule, deliberately narrow:
//   - status >= 500 AND productionLike → the `errors[]` items are withheld
//     and logged at WARN with the operation and path; the handler's own
//     `detail` survives, since it was written for the caller.
//   - everything else is untouched: a 4xx explains the caller's own input or
//     credential and is already written for them, and Huma's 422 validation
//     errors carry the field locations a client needs.
//   - in development the wrapped error stays in the body, because that is
//     where the text is read by the person who can act on it.
//
// It is a Transformer rather than a huma.NewError override so it sees the
// request (operation id, path) for the log line, and so it also covers the
// bare-error wrap, which never passes through NewError's caller.
func HumaErrorDetailPolicy(productionLike bool, logger *slog.Logger) huma.Transformer {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx huma.Context, status string, v any) (any, error) {
		em, ok := v.(*huma.ErrorModel)
		if !ok || em == nil || em.Status < 500 || len(em.Errors) == 0 || !productionLike {
			return v, nil
		}
		msgs := make([]string, 0, len(em.Errors))
		for _, d := range em.Errors {
			if d != nil && d.Message != "" {
				msgs = append(msgs, d.Message)
			}
		}
		opID := ""
		if op := ctx.Operation(); op != nil {
			opID = op.OperationID
		}
		logger.WarnContext(ctx.Context(), "http error details withheld from the client",
			slog.Int("status", em.Status),
			slog.String("operation", opID),
			slog.String("path", ctx.URL().Path),
			slog.String("detail", em.Detail),
			slog.String("errors", strings.Join(msgs, "; ")),
		)
		em.Errors = nil
		return em, nil
	}
}
