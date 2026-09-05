package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/core/authz/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
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
		{"binding exists", services.ErrBindingExists, 409},
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

// --- role writes (D21 / H-4) ---

// TestMapRoleWriteErrorPreservesKnownClientErrors is the sibling of the
// binding table above, for the mapper createRole and updateRole share.
// It pins the WIRE CONTRACT the operator console branches on: the two
// D21 refusals must be 422s carrying their specific codes, not the
// codeless 500 the update path's old default: arm produced.
func TestMapRoleWriteErrorPreservesKnownClientErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		want     int
		wantCode string
	}{
		{"unknown role", repository.ErrNotFound, 404, ""},
		{"system role immutable", services.ErrSystemRoleImmutable, 403, ""},
		{"blank name", services.ErrRoleNameRequired, 400, ""},
		{"empty permission list", services.ErrRolePermissionsRequired, 400, ""},
		{"missing actor", services.ErrGranterRequired, 400, ""},
		{"unknown permission", services.ErrUnknownPermission, 422, errcode.AuthzPermissionUnknown},
		{"platform key in a tenant role", services.ErrSystemPermissionInCustomRole, 422, errcode.AuthzSystemPermissionForbidden},
		{"cascade refusal", services.ErrInsufficientPermissionsToGrant, 403, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapRoleWriteError(context.Background(), "update the role", tt.err)
			se, ok := err.(huma.StatusError)
			if !ok || se.GetStatus() != tt.want {
				t.Fatalf("want %d huma.StatusError, got %T (%v)", tt.want, err, err)
			}
			if strings.Contains(err.Error(), "authz:") {
				t.Fatalf("service diagnostic reached the client: %q", err)
			}

			var coded *errcode.Error
			hasCode := errors.As(err, &coded)
			if tt.wantCode == "" {
				if hasCode && coded.Code != "" {
					t.Fatalf("did not expect a wire code, got %q", coded.Code)
				}
				return
			}
			if !hasCode {
				t.Fatalf("want *errcode.Error carrying %q, got %T", tt.wantCode, err)
			}
			if coded.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", coded.Code, tt.wantCode)
			}
		})
	}
}

func TestMapRoleWriteErrorKeepsUnknownFailuresInternal(t *testing.T) {
	t.Parallel()
	err := mapRoleWriteError(context.Background(), "create the role", errors.New("cedar evaluator: missing entity"))
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 500 {
		t.Fatalf("want 500 huma.StatusError, got %T (%v)", err, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "cedar") {
		t.Fatalf("policy-engine text reached the client: %q", err)
	}
}

// A bare sentinel carries no key, so the detail must fall back to a
// neutral subject rather than render an empty pair of quotes.
func TestMapRoleWriteErrorRendersAKeylessRefusalReadably(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{services.ErrUnknownPermission, services.ErrSystemPermissionInCustomRole} {
		err := mapRoleWriteError(context.Background(), "update the role", sentinel)
		if strings.Contains(err.Error(), `""`) {
			t.Errorf("empty quotes in the detail for %v: %q", sentinel, err)
		}
	}
}

// TestRoleActorNeverAcceptsThePlatformSentinel guards the D21 waiver at
// the HTTP boundary. The authz service treats the literal "system" as a
// cascade bypass; a token subject that spelled it would inherit that
// bypass over the wire. Not reachable today — subjects are
// uuid.NewString() — which is exactly why the guard needs a test: without
// one, a refactor that inlines ctxauth.GetUserUUID back into the two role
// handlers makes the waiver spellable again and CI stays green.
func TestRoleActorNeverAcceptsThePlatformSentinel(t *testing.T) {
	t.Parallel()

	// Pins the literal this test hard-codes against the service's own
	// notion of "reserved", so renaming the sentinel fails here rather
	// than silently making the case below vacuous.
	if !services.IsReservedActor("system") {
		t.Fatal(`services.IsReservedActor("system") is false — the sentinel moved; update this test with it`)
	}

	tests := []struct {
		name    string
		subject string
		present bool
		want    string
	}{
		{"the reserved sentinel is refused", "system", true, ""},
		{"an ordinary subject passes through", "3f1a9c02-0b4d-4a77-9f2e-6c81d5b0a913", true, "3f1a9c02-0b4d-4a77-9f2e-6c81d5b0a913"},
		{"an unauthenticated context yields no actor", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.present {
				ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, tt.subject)
			}
			if got := roleActor(ctx); got != tt.want {
				t.Fatalf("roleActor = %q, want %q", got, tt.want)
			}
		})
	}
}
