package module

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// configValidationHTTPError is the SDK-local twin of the internal errcode
// envelope: {status,title,detail,code}. pkg/sdk must not import
// internal/shared/errcode (SDK self-containment), so the wire shape is
// reproduced here. It implements huma.StatusError, so Huma serializes the
// struct verbatim with the configured status.
type configValidationHTTPError struct {
	Status int    `json:"status"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail"`
	Code   string `json:"code,omitempty"`
}

func (e *configValidationHTTPError) Error() string  { return e.Detail }
func (e *configValidationHTTPError) GetStatus() int { return e.Status }

// mapConfigServiceError is the single mapper for the three module-admin
// mutation surfaces (UpdateModule, UpdateEnvironment, SetActiveEnvironment).
// Code-bearing validation errors become the stable 422 envelope; validators
// without a code keep their pre-existing text-only Huma 422; anything else
// goes to the per-surface fallback (which the callers keep divergent on
// purpose — changing a fallback status is out of scope).
func mapConfigServiceError(err error, fallback func(error) error) error {
	var invalid *ConfigValidationError
	if errors.As(err, &invalid) {
		if invalid.Code != "" {
			return &configValidationHTTPError{
				Status: http.StatusUnprocessableEntity,
				Title:  http.StatusText(http.StatusUnprocessableEntity),
				Detail: invalid.Error(),
				Code:   invalid.Code,
			}
		}
		return huma.Error422UnprocessableEntity(invalid.Error())
	}
	return fallback(err)
}
