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

	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
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
		getUserFn: targetRow(&iface.UserManagementResponse{ID: "target-1", Role: "operator", IsActive: true}),
		getUserByIDFn: func(context.Context, string) (*iface.User, error) {
			return nil, boom // the CALLER's row is unreadable
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
		getUserByIDFn: callerRow("administrator"),
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
		getUserByIDFn: func(context.Context, string) (*iface.User, error) {
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

// --- the synthetic dev-token exception (ruling P31) --------------------
//
// POST /dev/token mints `sub = dev-<role>-<unix>` with no database row.
// D28's "a lookup miss is a 500" would take the documented local flow
// down — `./scripts/devtoken.sh administrator` then POST /v1/users, which
// always names a Role. The carve-out is the one the authz evaluator
// already makes for the same identities, guarded three ways.
//
// Every test below asserts the guard it names by REMOVING exactly that
// condition and nothing else, so none of them can be satisfied by a
// neighbouring refusal.

// fakePlatform is a module.PlatformInfo whose only meaningful answer is
// the environment classification the dev-token exception gates on.
type fakePlatform struct{ productionLike bool }

func (f fakePlatform) IsProduction() bool     { return f.productionLike }
func (f fakePlatform) IsStaging() bool        { return f.productionLike }
func (f fakePlatform) IsDevelopment() bool    { return !f.productionLike }
func (f fakePlatform) IsProductionLike() bool { return f.productionLike }
func (f fakePlatform) GetEnvironment() string {
	if f.productionLike {
		return "production"
	}
	return "development"
}
func (f fakePlatform) FrontendURL() string { return "http://localhost:8080" }

var _ module.PlatformInfo = fakePlatform{}

// devTokenHandler builds a handler whose store holds NO row for the
// synthetic caller — exactly the production shape, since /dev/token
// writes none.
func devTokenHandler(productionLike bool) (*UserHandler, *bool) {
	created := false
	svc := &fakeUserService{
		getUserByIDFn: func(context.Context, string) (*iface.User, error) {
			return nil, services.ErrUserNotFound
		},
		createUserFn: func(context.Context, *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
			created = true
			return &iface.UserManagementResponse{ID: "new-1"}, nil
		},
	}
	h := NewUserHandler(svc)
	h.SetPlatform(fakePlatform{productionLike: productionLike})
	return h, &created
}

func devCreate(h *UserHandler, actorUUID, claimedRole, assign string) error {
	_, err := h.CreateUser(claimCtx(actorUUID, claimedRole), &CreateUserRequest{
		Body: iface.CreateUserInput{Email: "new@orkestra.local", FullName: "New", Role: assign},
	})
	return err
}

func TestCreateUser_DevTokenCallerResolvesInDevelopment(t *testing.T) {
	t.Parallel()
	h, created := devTokenHandler(false)

	if err := devCreate(h, "dev-administrator-1757000000", "administrator", "operator"); err != nil {
		t.Fatalf("the documented dev-token flow must work in development: %v", err)
	}
	if !*created {
		t.Error("the create never reached the service")
	}
}

// Guard 1 removed, and only guard 1: same identity, same claim, same
// assignment, production-like deployment.
func TestCreateUser_DevTokenCallerIsInertWhenProductionLike(t *testing.T) {
	t.Parallel()
	h, created := devTokenHandler(true)

	err := devCreate(h, "dev-administrator-1757000000", "administrator", "operator")
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if *created {
		t.Error("a production-like deployment must not let a claim decide the caller's role")
	}
}

// A handler with no platform wired must behave as production-like: the
// exception is opt-in wiring, never a default.
func TestCreateUser_DevTokenCallerIsInertWithoutAPlatform(t *testing.T) {
	t.Parallel()
	h, _ := devTokenHandler(false)
	h.SetPlatform(nil)

	err := devCreate(h, "dev-administrator-1757000000", "administrator", "operator")
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
}

// Guard 2 removed, and only guard 2: a real UUID in development still has
// to have a row.
func TestCreateUser_NonDevUUIDGetsNoExceptionInDevelopment(t *testing.T) {
	t.Parallel()
	h, created := devTokenHandler(false)

	err := devCreate(h, "3f2504e0-4f89-11d3-9a0c-0305e82c3301", "administrator", "operator")
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if *created {
		t.Error("only the dev- prefix opens the exception")
	}
}

// Guard 3 removed, and only guard 3: a dev- principal whose claim names
// no real system role gets no role at all.
func TestCreateUser_DevTokenWithAnUnknownRoleClaimIsRefused(t *testing.T) {
	t.Parallel()
	h, created := devTokenHandler(false)

	// Not a 500: the exception simply does not apply, the store answers
	// "no row", and that is the lookup failure.
	err := devCreate(h, "dev-wizard-1757000000", "wizard", "operator")
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if *created {
		t.Error("an unrecognised srole must not become a caller role")
	}
}

// The exception hands over the CLAIMED role, not a blanket pass: a dev
// token minted as `operator` still cannot mint an administrator.
func TestCreateUser_DevTokenStillObeysTheTierLadder(t *testing.T) {
	t.Parallel()
	h, created := devTokenHandler(false)

	err := devCreate(h, "dev-operator-1757000000", "operator", "administrator")
	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if *created {
		t.Error("the tier ladder still applies to a dev-token caller")
	}
}

func TestUpdateUser_DevTokenCallerResolvesInDevelopment(t *testing.T) {
	t.Parallel()
	users := newUserStore(nil)
	users.seed("target-1", "operator") // no row for the dev principal
	h := NewUserHandler(users.svc)
	h.SetPlatform(fakePlatform{})

	if err := h.updateAsDevToken("dev-administrator-1757000000", "administrator", "manager"); err != nil {
		t.Fatalf("the documented dev-token flow must work in development: %v", err)
	}
	if got := users.roleOf("target-1"); got != "manager" {
		t.Errorf("target role = %q, want manager", got)
	}
}

func TestUpdateUser_DevTokenCallerIsInertWhenProductionLike(t *testing.T) {
	t.Parallel()
	users := newUserStore(nil)
	users.seed("target-1", "operator")
	h := NewUserHandler(users.svc)
	h.SetPlatform(fakePlatform{productionLike: true})

	err := h.updateAsDevToken("dev-administrator-1757000000", "administrator", "manager")
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if got := users.roleOf("target-1"); got != "operator" {
		t.Errorf("target role = %q, want it unchanged", got)
	}
}

// updateAsDevToken is the one-liner both UpdateUser dev-token cases share.
func (h *UserHandler) updateAsDevToken(actorUUID, claimedRole, assign string) error {
	_, err := h.UpdateUser(claimCtx(actorUUID, claimedRole), &UpdateUserRequest{
		ID:   "target-1",
		Body: iface.UpdateUserInput{Role: assign},
	})
	return err
}

// --- the refusal is audited -------------------------------------------

// Every other refusal on this path lands an audit row; a 500 the operator
// cannot distinguish from a 403 in the SOC2 trail is a gap, not a
// simplification.
func TestUpdateUser_CallerRoleLookupFailureIsAudited(t *testing.T) {
	t.Parallel()
	sink := &captureSink{}
	svc := &fakeUserService{
		sink:      sink,
		getUserFn: targetRow(&iface.UserManagementResponse{ID: "target-1", Role: "operator", IsActive: true}),
		getUserByIDFn: func(context.Context, string) (*iface.User, error) {
			return nil, errors.New("mongo: connection refused")
		},
	}
	h := NewUserHandler(svc)

	if err := h.updateAsDevToken("admin-1", "super_admin", "administrator"); err == nil {
		t.Fatal("expected a refusal")
	}
	ev := findAudit(sink, "user.update.refused")
	if ev == nil {
		t.Fatalf("no user.update.refused event; got %v", sink.events)
	}
	if got, _ := ev.Metadata["code"].(string); got != errcode.UserRoleLookupUnavailable {
		t.Errorf("metadata.code = %q, want %q", got, errcode.UserRoleLookupUnavailable)
	}
	if ev.Outcome != "denied" {
		t.Errorf("outcome = %q, want denied", ev.Outcome)
	}
}

func TestCreateUser_CallerRoleLookupFailureIsAudited(t *testing.T) {
	t.Parallel()
	sink := &captureSink{}
	svc := &fakeUserService{
		sink: sink,
		getUserByIDFn: func(context.Context, string) (*iface.User, error) {
			return nil, errors.New("mongo: connection refused")
		},
	}
	h := NewUserHandler(svc)

	if err := devCreate(h, "admin-1", "super_admin", "operator"); err == nil {
		t.Fatal("expected a refusal")
	}
	ev := findAudit(sink, "user.create.refused")
	if ev == nil {
		t.Fatalf("no user.create.refused event; got %v", sink.events)
	}
	if got, _ := ev.Metadata["code"].(string); got != errcode.UserRoleLookupUnavailable {
		t.Errorf("metadata.code = %q, want %q", got, errcode.UserRoleLookupUnavailable)
	}
}

// --- the client-tier twin ---------------------------------------------
//
// PATCH /v1/admin/client-users/{id} is the same documented local flow and
// its caller is the same synthetic operator, so it shares the carve-out
// through the same devTokenSystemRole helper. Both directions, so the
// exception cannot silently widen on that surface either.

func TestUpdateClientUserAdmin_DevTokenCallerResolvesInDevelopment(t *testing.T) {
	t.Parallel()
	h, clients, operators, _ := newClientAdminHarness(t)
	clients.seed("client-target", "operator")
	h.SetPlatform(fakePlatform{})

	ctx := operatorCtx("dev-administrator-1757000000", "administrator")
	if err := patchClientRole(t, h, ctx, "client-target", "manager"); err != nil {
		t.Fatalf("the documented dev-token flow must work in development: %v", err)
	}
	if got := clients.roleOf("client-target"); got != "manager" {
		t.Errorf("target role = %q, want manager", got)
	}
	if n := len(operators.lookups()); n != 0 {
		t.Errorf("a resolved dev principal must not hit the operator store; %d lookups", n)
	}
}

func TestUpdateClientUserAdmin_DevTokenCallerIsInertWhenProductionLike(t *testing.T) {
	t.Parallel()
	h, clients, _, _ := newClientAdminHarness(t)
	clients.seed("client-target", "operator")
	h.SetPlatform(fakePlatform{productionLike: true})

	ctx := operatorCtx("dev-administrator-1757000000", "administrator")
	err := patchClientRole(t, h, ctx, "client-target", "manager")
	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if got := clients.roleOf("client-target"); got != "operator" {
		t.Errorf("target role = %q, want it unchanged", got)
	}
}
