package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/core/authz/services"
)

func TestAuthzInternalErrorDoesNotLeakPolicyEngineText(t *testing.T) {
	t.Parallel()
	err := authzInternalError(context.Background(), "create the role", errors.New("cedar evaluator: missing entity"))
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 500 {
		t.Fatalf("want 500 huma.StatusError, got %T (%v)", err, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "cedar") {
		t.Fatalf("policy-engine text reached the client: %q", err.Error())
	}
}

func TestMapCreateBindingErrorPreservesKnownClientErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"role inactive", services.ErrRoleInactive, 409},
		{"system role in tenant", services.ErrSystemRoleNotGrantableInTenant, 400},
		{"tenant role globally", services.ErrTenantRoleNotGrantableGlobally, 400},
		{"insufficient grant permissions", services.ErrInsufficientPermissionsToGrant, 403},
		{"missing granter", services.ErrGranterRequired, 400},
		{"unknown role", repository.ErrNotFound, 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapCreateBindingError(context.Background(), tt.err)
			se, ok := err.(huma.StatusError)
			if !ok || se.GetStatus() != tt.want {
				t.Fatalf("want %d huma.StatusError, got %T (%v)", tt.want, err, err)
			}
			if strings.Contains(err.Error(), "authz:") {
				t.Fatalf("service diagnostic reached the client: %q", err)
			}
		})
	}
}

func TestMapCreateBindingErrorKeepsUnknownFailuresInternal(t *testing.T) {
	t.Parallel()
	err := mapCreateBindingError(context.Background(), errors.New("cedar evaluator: missing entity"))
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 500 {
		t.Fatalf("want 500 huma.StatusError, got %T (%v)", err, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "cedar") {
		t.Fatalf("policy-engine text reached the client: %q", err)
	}
}
