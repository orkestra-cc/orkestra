// Package errcode provides the lightweight error-code contract that
// admin-facing HTTP handlers use to surface a stable machine-readable
// reason alongside the human-fallback message.
//
// Handlers return *errcode.Error from their failure paths; the SPA
// reads response.code and translates via t(`errors.${code}`), falling
// back to response.detail when a handler hasn't been migrated yet. The
// type implements huma.StatusError so Huma serializes the struct
// verbatim — JSON tags here are the wire contract.
//
// Codes live in codes.go and follow <module>.<situation> snake_case —
// see backend/CLAUDE.md "Error-code contract" for the convention and
// codes_test.go for the golden-file lock that fails CI on a silent
// rename.
package errcode

import "net/http"

// Error is the response envelope for code-bearing handler failures.
// JSON tags are the wire format consumed by the SPA — do not rename
// fields here without rolling out the corresponding frontend reader
// change.
type Error struct {
	Status int    `json:"status"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail"`
	Code   string `json:"code,omitempty"`

	// Headers are copied onto the HTTP response by Huma via the
	// huma.HeadersError interface and are NEVER serialised into the
	// body — the {status,title,detail,code} envelope is a frozen wire
	// contract. Retry-After on a 429 is the motivating case.
	Headers http.Header `json:"-"`
}

// Error implements the error interface using the human-readable
// detail so wrapped errors retain their narrative when logged.
func (e *Error) Error() string { return e.Detail }

// GetStatus implements huma.StatusError so Huma uses the configured
// HTTP status instead of falling back to 500.
func (e *Error) GetStatus() int { return e.Status }

// GetHeaders implements huma.HeadersError. Returns a non-nil, possibly
// empty Header so callers never have to nil-check.
func (e *Error) GetHeaders() http.Header {
	if e.Headers == nil {
		return http.Header{}
	}
	return e.Headers
}

// WithHeader attaches one response header and returns the receiver so it
// composes with the named builders:
//
//	errcode.TooManyRequests(errcode.AuthTooManyAttempts, detail).
//	    WithHeader("Retry-After", "15")
func (e *Error) WithHeader(k, v string) *Error {
	if e.Headers == nil {
		e.Headers = http.Header{}
	}
	e.Headers.Set(k, v)
	return e
}

// New constructs an Error with an arbitrary status. Prefer the named
// builders (Conflict, NotFound, …) at call sites — New exists for
// statuses without a dedicated builder.
func New(status int, code, detail string) *Error {
	return &Error{Status: status, Title: http.StatusText(status), Detail: detail, Code: code}
}

// BadRequest returns a 400 with the given code + detail.
func BadRequest(code, detail string) *Error { return New(http.StatusBadRequest, code, detail) }

// Unauthorized returns a 401.
func Unauthorized(code, detail string) *Error { return New(http.StatusUnauthorized, code, detail) }

// Forbidden returns a 403.
func Forbidden(code, detail string) *Error { return New(http.StatusForbidden, code, detail) }

// NotFound returns a 404.
func NotFound(code, detail string) *Error { return New(http.StatusNotFound, code, detail) }

// Conflict returns a 409 — the most common code-bearing failure mode
// (duplicate resource, last-credential lockout, self-action refusal).
func Conflict(code, detail string) *Error { return New(http.StatusConflict, code, detail) }

// UnprocessableEntity returns a 422 — for semantically-invalid input
// that survived schema validation (business-rule violations).
func UnprocessableEntity(code, detail string) *Error {
	return New(http.StatusUnprocessableEntity, code, detail)
}

// TooManyRequests returns a 429 with the given code + detail.
func TooManyRequests(code, detail string) *Error {
	return New(http.StatusTooManyRequests, code, detail)
}

// ServiceUnavailable returns a 503 — the dependency or configuration a
// request needs is absent or down. Use it for faults an operator can
// fix (missing keys, unconfigured mailer), and say which one in detail.
func ServiceUnavailable(code, detail string) *Error {
	return New(http.StatusServiceUnavailable, code, detail)
}

// Internal returns a 500 — the server hit a state it does not model.
// The detail must stay a written sentence, never the underlying error
// text: log the cause, tell the caller what failed.
func Internal(code, detail string) *Error { return New(http.StatusInternalServerError, code, detail) }
