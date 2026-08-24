package module

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestMapConfigServiceError_CodeBearingErrorUsesEnvelope(t *testing.T) {
	t.Parallel()
	err := mapConfigServiceError(
		fmt.Errorf("update: %w", &ConfigValidationError{
			Field: "provisioning.internal.mode", Message: "must be manual or single", Code: "tenant.internal_mode_invalid",
		}),
		func(e error) error { return huma.Error400BadRequest(e.Error()) },
	)
	env, ok := err.(*configValidationHTTPError)
	if !ok {
		t.Fatalf("expected *configValidationHTTPError, got %T", err)
	}
	if env.Status != http.StatusUnprocessableEntity || env.Code != "tenant.internal_mode_invalid" {
		t.Fatalf("bad envelope: %+v", env)
	}
	if env.GetStatus() != http.StatusUnprocessableEntity {
		t.Fatalf("GetStatus mismatch")
	}
}

func TestMapConfigServiceError_CodelessErrorKeepsHuma422(t *testing.T) {
	t.Parallel()
	err := mapConfigServiceError(
		&ConfigValidationError{Field: "f", Message: "bad"},
		func(e error) error { return e },
	)
	if _, isEnv := err.(*configValidationHTTPError); isEnv {
		t.Fatalf("codeless validator must keep the legacy Huma 422, got envelope")
	}
	var se huma.StatusError
	if !errors.As(err, &se) || se.GetStatus() != http.StatusUnprocessableEntity {
		t.Fatalf("expected Huma 422, got %v", err)
	}
}

func TestMapConfigServiceError_OtherErrorUsesFallback(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	err := mapConfigServiceError(sentinel, func(e error) error { return fmt.Errorf("fb: %w", e) })
	if !errors.Is(err, sentinel) {
		t.Fatalf("fallback not applied: %v", err)
	}
}
