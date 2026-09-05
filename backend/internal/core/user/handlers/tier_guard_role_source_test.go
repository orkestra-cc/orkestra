package handlers

// D28. The operator-tier role guards read the CALLER's role from the
// database, never from the `srole` JWT claim.
//
// The claim can be up to one access-token lifetime stale — precisely the
// window the role-change propagation (M-13) exists to close. Trusting it
// in the guard that decides whether a caller may assign a role would put
// that hole straight back: a demoted administrator would keep handing out
// administrator roles until their last access token expired.
//
// Every fixture below makes the claim and the database row DISAGREE, so a
// revert to `ctxauth.GetSystemRole` flips the assertion rather than
// leaving it vacuously true.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// claimCtx stamps an actor UUID plus a `srole` claim onto the context,
// exactly as AuthMiddleware does. Tests here deliberately pass a claim
// that contradicts the seeded database row.
func claimCtx(actorUUID, claimedRole string) context.Context {
	ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, actorUUID)
	return context.WithValue(ctx, ctxauth.KeySystemRole, claimedRole)
}

// --- UpdateUser -------------------------------------------------------

func TestUpdateUser_CallerRoleComesFromTheDatabaseNotTheClaim(t *testing.T) {
	t.Parallel()
	users := newUserStore(nil)
	users.seed("actor-1", "administrator") // the truth
	users.seed("target-1", "operator")
	h := NewUserHandler(users.svc)

	// The claim says super_admin; the row says administrator. An
	// administrator may not mint a super_admin.
	_, err := h.UpdateUser(claimCtx("actor-1", "super_admin"), &UpdateUserRequest{
		ID:   "target-1",
		Body: iface.UpdateUserInput{Role: "super_admin"},
	})

	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if got := users.roleOf("target-1"); got != "operator" {
		t.Errorf("target role = %q, want it unchanged at %q", got, "operator")
	}
	if n := users.updateCalls(); n != 0 {
		t.Errorf("the guard must refuse before the write; %d update calls", n)
	}
}

// The inverse: a stale claim must not COST a caller a right the database
// grants them either. Without this the guard could "pass" by refusing
// everything.
func TestUpdateUser_DatabaseRoleGrantsWhatTheStaleClaimWouldRefuse(t *testing.T) {
	t.Parallel()
	users := newUserStore(nil)
	users.seed("actor-1", "super_admin") // promoted; the claim predates it
	users.seed("target-1", "operator")
	h := NewUserHandler(users.svc)

	if _, err := h.UpdateUser(claimCtx("actor-1", "manager"), &UpdateUserRequest{
		ID:   "target-1",
		Body: iface.UpdateUserInput{Role: "administrator"},
	}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got := users.roleOf("target-1"); got != "administrator" {
		t.Errorf("target role = %q, want %q", got, "administrator")
	}
}

// A lookup failure is a 500, NEVER a fallback to the claim: falling back
// makes the claim authoritative again exactly when the database cannot
// contradict it.
func TestUpdateUser_CallerRoleLookupFailureIsInternalNeverAClaimFallback(t *testing.T) {
	t.Parallel()
	boom := errors.New("mongo: connection refused")
	wrote := false
	svc := &fakeUserService{
		getUserFn: func(_ context.Context, id string) (*iface.UserManagementResponse, error) {
			if id == "actor-1" {
				return nil, boom
			}
			return &iface.UserManagementResponse{ID: id, Role: "operator", IsActive: true}, nil
		},
		updateUserFn: func(_ context.Context, id string, _ *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
			wrote = true
			return &iface.UserManagementResponse{ID: id, Role: "administrator"}, nil
		},
	}
	h := NewUserHandler(svc)

	_, err := h.UpdateUser(claimCtx("actor-1", "super_admin"), &UpdateUserRequest{
		ID:   "target-1",
		Body: iface.UpdateUserInput{Role: "administrator"},
	})
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if wrote {
		t.Error("an unresolvable caller role must refuse before the write")
	}
}

// A caller row that simply is not there is the same fail-closed 500: an
// absent row is not evidence of any role.
func TestUpdateUser_MissingCallerRowIsInternal(t *testing.T) {
	t.Parallel()
	users := newUserStore(nil)
	users.seed("target-1", "operator") // actor-1 deliberately unseeded
	h := NewUserHandler(users.svc)

	_, err := h.UpdateUser(claimCtx("actor-1", "super_admin"), &UpdateUserRequest{
		ID:   "target-1",
		Body: iface.UpdateUserInput{Role: "administrator"},
	})
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if n := users.updateCalls(); n != 0 {
		t.Errorf("a caller whose role cannot be resolved must not write; %d update calls", n)
	}
}

// A patch that assigns no role never resolves a caller role at all — the
// read is on the guarded path only. The store has no row for the actor,
// so an unconditional caller lookup would surface as a 500 here.
func TestUpdateUser_ProfileOnlyPatchDoesNotResolveTheCallerRole(t *testing.T) {
	t.Parallel()
	users := newUserStore(nil)
	users.seed("target-1", "operator") // no actor row on purpose
	h := NewUserHandler(users.svc)

	if _, err := h.UpdateUser(claimCtx("actor-1", "super_admin"), &UpdateUserRequest{
		ID:   "target-1",
		Body: iface.UpdateUserInput{FullName: "Renamed"},
	}); err != nil {
		t.Fatalf("a profile-only patch must not need the caller's role: %v", err)
	}
}

// --- CreateUser -------------------------------------------------------

func TestCreateUser_CallerRoleComesFromTheDatabaseNotTheClaim(t *testing.T) {
	t.Parallel()
	created := false
	svc := &fakeUserService{
		getUserFn: func(_ context.Context, id string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: id, Role: "administrator", IsActive: true}, nil
		},
		createUserFn: func(context.Context, *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
			created = true
			return &iface.UserManagementResponse{ID: "new-1"}, nil
		},
	}
	h := NewUserHandler(svc)

	_, err := h.CreateUser(claimCtx("actor-1", "super_admin"), &CreateUserRequest{
		Body: iface.CreateUserInput{Email: "new@orkestra.local", FullName: "New", Role: "super_admin"},
	})
	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if created {
		t.Error("the guard must refuse before the create")
	}
}

func TestCreateUser_CallerRoleLookupFailureIsInternal(t *testing.T) {
	t.Parallel()
	created := false
	svc := &fakeUserService{
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return nil, errors.New("mongo: connection refused")
		},
		createUserFn: func(context.Context, *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
			created = true
			return &iface.UserManagementResponse{ID: "new-1"}, nil
		},
	}
	h := NewUserHandler(svc)

	_, err := h.CreateUser(claimCtx("actor-1", "super_admin"), &CreateUserRequest{
		Body: iface.CreateUserInput{Email: "new@orkestra.local", FullName: "New", Role: "operator"},
	})
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if created {
		t.Error("an unresolvable caller role must refuse before the create")
	}
}
