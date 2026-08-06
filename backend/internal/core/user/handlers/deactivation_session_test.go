package handlers

// Security regression test: deactivating a user must end their sessions.
//
// Flipping isActive=false emitted a `user.deactivated` audit row and
// nothing else. The auth refresh paths now refuse a deactivated user, so
// the account dies within one access-token TTL — but for the case this
// exists to serve (offboarding someone, or cutting off a compromised
// account) "within 15 minutes" is not the same as "now". The handler
// therefore also asks the auth module to tear the sessions down, which
// revokes refresh tokens, flips the session docs, and pushes every sid
// into the Redis revocation set so in-flight bearers stop working on
// their next request.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// recordingTerminator captures the users whose sessions were torn down.
type recordingTerminator struct {
	mu         sync.Mutex
	terminated []string
	err        error
}

func (r *recordingTerminator) TerminateAllSessionsByUUID(_ context.Context, userUUID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.terminated = append(r.terminated, userUUID)
	return r.err
}

func (r *recordingTerminator) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.terminated...)
}

// handlerWithTerminator builds a UserHandler whose lazy registry lookup
// resolves to the supplied terminator.
func handlerWithTerminator(svc *fakeUserService, term iface.SessionTerminator) *UserHandler {
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceAuthService, term)
	h := NewUserHandler(svc)
	h.SetServiceRegistry(reg)
	return h
}

func deactivatingService() *fakeUserService {
	active := true
	return &fakeUserService{
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: active}, nil
		},
		updateUserFn: func(_ context.Context, _ string, in *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
			if in.IsActive != nil {
				active = *in.IsActive
			}
			return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: active}, nil
		},
	}
}

func boolPtr(b bool) *bool { return &b }

func TestUpdateUser_DeactivationTerminatesSessions(t *testing.T) {
	term := &recordingTerminator{}
	h := handlerWithTerminator(deactivatingService(), term)

	_, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
		ID:   "u1",
		Body: iface.UpdateUserInput{IsActive: boolPtr(false)},
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	if got := term.list(); len(got) != 1 || got[0] != "u1" {
		t.Errorf("deactivation must terminate the user's sessions, got %v", got)
	}
}

func TestUpdateUser_ActivationDoesNotTerminateSessions(t *testing.T) {
	term := &recordingTerminator{}
	h := handlerWithTerminator(deactivatingService(), term)

	_, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
		ID:   "u1",
		Body: iface.UpdateUserInput{IsActive: boolPtr(true)},
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	if got := term.list(); len(got) != 0 {
		t.Errorf("re-activating a user must not sign them out, got %v", got)
	}
}

func TestUpdateUser_UnrelatedPatchDoesNotTerminateSessions(t *testing.T) {
	term := &recordingTerminator{}
	h := handlerWithTerminator(deactivatingService(), term)

	_, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
		ID:   "u1",
		Body: iface.UpdateUserInput{FullName: "Renamed"},
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	if got := term.list(); len(got) != 0 {
		t.Errorf("a rename must not sign the user out, got %v", got)
	}
}

func TestUpdateUser_TerminationFailureDoesNotFailTheRequest(t *testing.T) {
	// The account is already disabled in the database by this point, and
	// the refresh paths refuse it. Failing the response would tell the
	// operator the deactivation did not happen, which is worse.
	term := &recordingTerminator{err: errors.New("redis down")}
	h := handlerWithTerminator(deactivatingService(), term)

	if _, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
		ID:   "u1",
		Body: iface.UpdateUserInput{IsActive: boolPtr(false)},
	}); err != nil {
		t.Fatalf("a session-teardown failure must not fail the deactivation: %v", err)
	}
}

func TestUpdateUser_DeactivationWithoutRegistryStillSucceeds(t *testing.T) {
	// Nothing wired (tests, a fork that trims the auth module): the
	// deactivation must still apply.
	h := NewUserHandler(deactivatingService())

	if _, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
		ID:   "u1",
		Body: iface.UpdateUserInput{IsActive: boolPtr(false)},
	}); err != nil {
		t.Fatalf("UpdateUser without a service registry: %v", err)
	}
}
