package handlers

// Task 9: pure-function coverage for mapServiceAccountAdminError, mirroring
// service_token_error_mapping_test.go's TestMapServiceTokenError. The admin
// handler itself is thin delegation to services.ServiceAccountService, which
// already has full unit coverage (Task 6) — this test only pins the
// error-to-HTTP-status mapping.

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
)

func TestMapServiceAccountAdminError(t *testing.T) {
	cases := []struct {
		in         error
		wantStatus int
	}{
		{services.ErrServiceAccountNotFound, http.StatusNotFound},
		{services.ErrAccountAlreadyExists, http.StatusConflict},
		{services.ErrTooManyActiveCredentials, http.StatusConflict},
		{services.ErrInvalidAccountName, http.StatusUnprocessableEntity},
		{repository.ErrServiceAccountCredentialNotFound, http.StatusNotFound},
		{services.ErrServiceAccountLookupUnavailable, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		err := mapServiceAccountAdminError(c.in)
		var se interface{ GetStatus() int }
		if !errors.As(err, &se) || se.GetStatus() != c.wantStatus {
			t.Errorf("map(%v) status = %v, want %d", c.in, err, c.wantStatus)
		}
	}
}

// TestServiceAccountUpdateNameHasMinLength is Item 8's regression test:
// the PATCH body's `name` field must declare minLength:"1" so
// huma validates {"name": ""} into a 422 rather than the handler
// silently treating it as a no-op-because-empty-string-is-falsy-ish
// rename. Asserted at the struct-tag level (no huma test harness exists
// in this package) — this is the exact tag huma's schema reflection
// reads to build the OpenAPI minLength constraint.
func TestServiceAccountUpdateNameHasMinLength(t *testing.T) {
	bodyField, ok := reflect.TypeOf(ServiceAccountUpdateRequest{}).FieldByName("Body")
	if !ok {
		t.Fatal("ServiceAccountUpdateRequest has no Body field")
	}
	nameField, ok := bodyField.Type.FieldByName("Name")
	if !ok {
		t.Fatal("ServiceAccountUpdateRequest.Body has no Name field")
	}
	if got := nameField.Tag.Get("minLength"); got != "1" {
		t.Errorf(`Name field minLength tag = %q, want "1" — {"name":""} must 422, not silently no-op`, got)
	}
}

// The 503 arm carries a machine-readable token, and it has to survive the
// wrap the service applies (`%w: %w` with the underlying cause) — a mapping
// that only matched the bare sentinel would answer a real outage with a 500.
// Huma's ErrorModel has no top-level `code` field, so the token goes in
// `detail`, the shape this tree already uses on huma routes
// (user/handlers/avatar_handler.go's avatar_storage_unavailable).
func TestMapServiceAccountAdminErrorLookupUnavailableCarriesCode(t *testing.T) {
	wrapped := fmt.Errorf("service account lookup failed: %w: %w",
		services.ErrServiceAccountLookupUnavailable, errors.New("mongo: no reachable servers"))

	var model *huma.ErrorModel
	if !errors.As(mapServiceAccountAdminError(wrapped), &model) {
		t.Fatalf("map(%v) is not a *huma.ErrorModel", wrapped)
	}
	if model.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — an unreadable directory is not a verdict on the account", model.Status)
	}
	if model.Detail != "service_account_lookup_unavailable" {
		t.Errorf("detail = %q, want the machine-readable code", model.Detail)
	}
	// The not-found answer must be untouched by the split.
	var nf *huma.ErrorModel
	if !errors.As(mapServiceAccountAdminError(services.ErrServiceAccountNotFound), &nf) {
		t.Fatal("not-found mapping is not a *huma.ErrorModel")
	}
	if nf.Status != http.StatusNotFound || nf.Detail != "service account not found" {
		t.Errorf("not-found mapping = %d %q, want 404 \"service account not found\"", nf.Status, nf.Detail)
	}
}
