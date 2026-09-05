package handlers

// D29 / M-17. The Tier-2 client-user PATCH
// (`PATCH /v1/admin/client-users/{id}`) runs the same role guards as its
// operator-tier twin: `canAssignRole` against the caller's DATABASE role
// (D28) and `serviceAccountRoleAllowed` against the target's kind.
//
// Two things make this endpoint different from the operator PATCH, and
// both are load-bearing for every fixture below.
//
//  1. The ACTOR is an operator; the TARGET is a client user. They live in
//     different collections behind different services, so the caller's
//     role must be read from `module.ServiceOperatorUserProvider`.
//     Reading it from the handler's own client-tier service would miss on
//     every real request and turn D28's "a lookup failure is a 500" into
//     a total outage of the endpoint — which is why
//     TestUpdateClientUserAdmin_LegitimateOperatorCallerStillSucceeds and
//     TestUpdateClientUserAdmin_ActorRoleComesFromTheOperatorStoreNotTheClientStore
//     exist and why the client store below deliberately does not know the
//     actor's id.
//
//  2. The last-administrator quorum stays OPERATOR-only. A client user is
//     never a platform administrator, so demoting one can never strand
//     the platform without one.
//
// Every fixture is id-aware. A fake that answered one row for every id
// would hand the actor the TARGET's role, and an escalation test would
// then pass through the wrong guard with the same status and the same
// error code — the exact failure mode `actorAnd` was introduced to fix.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// operatorStore is an id-aware iface.UserProvider standing in for the
// operator-tier user service that the client PATCH resolves the CALLER
// from. Only GetUserByID is implemented; the embedded nil interface makes
// any other call panic loudly rather than quietly returning a zero value.
type operatorStore struct {
	iface.UserProvider

	mu    sync.Mutex
	rows  map[string]*iface.User
	err   error
	calls []string
}

func newOperatorStore() *operatorStore {
	return &operatorStore{rows: map[string]*iface.User{}}
}

func (o *operatorStore) seed(uuid, role string) *operatorStore {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.rows[uuid] = &iface.User{UUID: uuid, Email: uuid + "@orkestra.local", Role: role, IsActive: true}
	return o
}

// failWith makes every lookup fail — the transient-outage case that must
// never fall back to the `srole` claim.
func (o *operatorStore) failWith(err error) *operatorStore {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.err = err
	return o
}

func (o *operatorStore) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, id)
	if o.err != nil {
		return nil, o.err
	}
	row, ok := o.rows[id]
	if !ok {
		return nil, iface.ErrUserNotFound
	}
	copied := *row
	return &copied, nil
}

func (o *operatorStore) lookups() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...)
}

// seedKind seeds a client-store row that also carries a Kind
// discriminator — the machine-principal case userStore.seed does not
// cover.
func (s *userStore) seedKind(uuid, role, kind string) {
	s.seed(uuid, role)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[uuid].Kind = kind
}

// newClientAdminHarness wires the handler the way user/module.go does:
// the CLIENT-tier user service holds the target, and the registry holds
// the OPERATOR-tier provider the caller is resolved from.
//
// The client store is deliberately seeded with the target only. An actor
// id is never seeded there, so any implementation that resolves the
// caller from the client service gets ErrUserNotFound and 500s — which is
// the outage this task exists to avoid.
func newClientAdminHarness(t *testing.T) (*AdminClientUserHandler, *userStore, *operatorStore, *captureSink) {
	t.Helper()
	clients := newUserStore(nil)
	sink := &captureSink{}
	clients.svc.sink = sink
	operators := newOperatorStore()
	h, reg := newAdminHandler(clients.svc)
	reg.Register(module.ServiceOperatorUserProvider, operators)
	return h, clients, operators, sink
}

// operatorCtx stamps an operator principal onto the request context, as
// AuthMiddleware does. claimedRole is the `srole` claim — the value the
// guards must NOT consult.
func operatorCtx(actorUUID, claimedRole string) context.Context {
	ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, actorUUID)
	ctx = context.WithValue(ctx, ctxauth.KeyUserEmail, actorUUID+"@orkestra.local")
	return context.WithValue(ctx, ctxauth.KeySystemRole, claimedRole)
}

func patchClientRole(t *testing.T, h *AdminClientUserHandler, ctx context.Context, targetID, role string) error {
	t.Helper()
	_, err := h.UpdateClientUserAdmin(ctx, &UpdateClientUserAdminRequest{
		ID:   targetID,
		Body: UpdateClientUserAdminBody{Role: role},
	})
	return err
}

// findAudit returns the first event with the given action, or nil.
func findAudit(sink *captureSink, action string) *iface.AuditEvent {
	for i := range sink.events {
		if sink.events[i].Action == action {
			return &sink.events[i]
		}
	}
	return nil
}

// --- the outage guard -------------------------------------------------

// TestUpdateClientUserAdmin_LegitimateOperatorCallerStillSucceeds is the
// single most important test in this file. The guards resolve the caller
// from the operator provider; if they ever resolve it from the handler's
// own client-tier service instead, EVERY call to this endpoint 500s from
// the moment it deploys, because an operator has no row in the client
// collection.
func TestUpdateClientUserAdmin_LegitimateOperatorCallerStillSucceeds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		actorRole string
		assign    string
	}{
		{name: "a super_admin may assign a lower tier", actorRole: "super_admin", assign: "manager"},
		// Equal-tier assignment is allowed; the prohibition is on strict
		// elevation only.
		{name: "an administrator may assign an equal tier", actorRole: "administrator", assign: "administrator"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, clients, operators, _ := newClientAdminHarness(t)
			operators.seed("op-actor", c.actorRole)
			clients.seed("client-target", "operator")

			if err := patchClientRole(t, h, operatorCtx("op-actor", c.actorRole), "client-target", c.assign); err != nil {
				t.Fatalf("a legitimate operator caller must still be able to patch a client user's role: %v", err)
			}
			if got := clients.roleOf("client-target"); got != c.assign {
				t.Errorf("target role = %q, want %q", got, c.assign)
			}
			if got := operators.lookups(); len(got) != 1 || got[0] != "op-actor" {
				t.Errorf("operator lookups = %v, want exactly one for the ACTOR", got)
			}
		})
	}
}

// TestUpdateClientUserAdmin_ActorRoleComesFromTheOperatorStoreNotTheClientStore
// pins WHICH store answers for the caller. The two stores are seeded with
// contradicting rows for the same id, so a handler reading the wrong one
// flips the verdict rather than producing the same answer twice.
func TestUpdateClientUserAdmin_ActorRoleComesFromTheOperatorStoreNotTheClientStore(t *testing.T) {
	t.Parallel()
	h, clients, operators, _ := newClientAdminHarness(t)
	operators.seed("shared-id", "administrator") // the truth: an administrator
	clients.seed("shared-id", "super_admin")     // a decoy row in the wrong tier
	clients.seed("client-target", "operator")

	err := patchClientRole(t, h, operatorCtx("shared-id", "super_admin"), "client-target", "super_admin")

	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if got := clients.roleOf("client-target"); got != "operator" {
		t.Errorf("target role = %q, want it unchanged at %q", got, "operator")
	}
}

// --- canAssignRole ----------------------------------------------------

func TestUpdateClientUserAdmin_RefusesEscalation(t *testing.T) {
	t.Parallel()
	h, clients, operators, sink := newClientAdminHarness(t)
	operators.seed("client-actor", "administrator") // the truth
	clients.seed("client-target", "operator")

	// The claim says super_admin and the row says administrator, so this
	// test also falls if the guard reverts to reading `srole` — a claim
	// matching the row would leave it passing either way.
	err := patchClientRole(t, h, operatorCtx("client-actor", "super_admin"), "client-target", "super_admin")

	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if got := clients.roleOf("client-target"); got != "operator" {
		t.Errorf("target role = %q, want it unchanged at %q", got, "operator")
	}
	if n := clients.updateCalls(); n != 0 {
		t.Errorf("the guard must refuse before the write; %d update calls", n)
	}

	ev := findAudit(sink, "user.update.refused")
	if ev == nil {
		t.Fatalf("a refused client role change must emit user.update.refused, like the operator handler; got %+v", sink.events)
	}
	if ev.ResourceType != "client_user" {
		t.Errorf("ResourceType = %q, want %q", ev.ResourceType, "client_user")
	}
	if ev.Outcome != "denied" {
		t.Errorf("Outcome = %q, want %q", ev.Outcome, "denied")
	}
	if ev.ActorUserID != "client-actor" {
		t.Errorf("ActorUserID = %q, want %q", ev.ActorUserID, "client-actor")
	}
	if got := ev.Metadata["attempted"]; got != "role_escalation" {
		t.Errorf("metadata.attempted = %v, want %q", got, "role_escalation")
	}
	if got := ev.Metadata["code"]; got != errcode.UserRoleEscalationForbidden {
		t.Errorf("metadata.code = %v, want %q", got, errcode.UserRoleEscalationForbidden)
	}
	if got := ev.Metadata["from"]; got != "operator" {
		t.Errorf("metadata.from = %v, want %q", got, "operator")
	}
	if got := ev.Metadata["to"]; got != "super_admin" {
		t.Errorf("metadata.to = %v, want %q", got, "super_admin")
	}
}

// TestUpdateClientUserAdmin_MissingActorRefusesEveryRoleAssignment mirrors
// the operator tier: no authenticated principal is a degraded gate, not a
// broken database, so it is a fail-closed 403 rather than a 500 — and the
// operator store is never consulted, because there is no id to look up.
func TestUpdateClientUserAdmin_MissingActorRefusesEveryRoleAssignment(t *testing.T) {
	t.Parallel()
	h, clients, operators, _ := newClientAdminHarness(t)
	clients.seed("client-target", "operator")

	err := patchClientRole(t, h, context.Background(), "client-target", "guest")

	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if n := clients.updateCalls(); n != 0 {
		t.Errorf("the guard must refuse before the write; %d update calls", n)
	}
	if got := operators.lookups(); len(got) != 0 {
		t.Errorf("operator lookups = %v, want none — there is no principal to resolve", got)
	}
}

// --- the lookup fails closed ------------------------------------------

// TestUpdateClientUserAdmin_CallerLookupFailureIsInternalNeverAClaimFallback
// pins D28's rule on this tier too: when the caller's row cannot be read,
// the request is refused with a 500. Falling back to the `srole` claim
// would make it authoritative again exactly when the database cannot
// contradict it — and every fixture here carries a super_admin claim that
// WOULD have allowed the assignment.
func TestUpdateClientUserAdmin_CallerLookupFailureIsInternalNeverAClaimFallback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		arm   func(*operatorStore)
		wired bool
	}{
		{
			name:  "the operator store is down",
			arm:   func(o *operatorStore) { o.failWith(errors.New("mongo: connection refused")) },
			wired: true,
		},
		{
			name:  "the caller has no operator row",
			arm:   func(o *operatorStore) {}, // seeded with nothing
			wired: true,
		},
		{
			// The provider is unwired (the user module always registers
			// it, so this is a "cannot happen" that must still fail
			// closed rather than sail past the guard).
			name:  "the operator provider is not registered",
			arm:   func(o *operatorStore) {},
			wired: false,
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			clients := newUserStore(nil)
			clients.seed("client-target", "operator")
			h, reg := newAdminHandler(clients.svc)
			operators := newOperatorStore()
			c.arm(operators)
			if c.wired {
				reg.Register(module.ServiceOperatorUserProvider, operators)
			}

			err := patchClientRole(t, h, operatorCtx("op-actor", "super_admin"), "client-target", "super_admin")

			assertStatus(t, err, http.StatusInternalServerError)
			assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
			if got := clients.roleOf("client-target"); got != "operator" {
				t.Errorf("target role = %q, want it unchanged at %q", got, "operator")
			}
			if n := clients.updateCalls(); n != 0 {
				t.Errorf("the guard must refuse before the write; %d update calls", n)
			}
		})
	}
}

// --- serviceAccountRoleAllowed ----------------------------------------

// TestUpdateClientUserAdmin_RefusesAServiceAccountRole seeds the caller as
// a super_admin ON PURPOSE: that clears the escalation guard outright, so
// the only thing that can produce the refusal is the service-account
// guard. The metadata assertion pins which guard fired — the two share a
// status and an error code, and a test that checked only those would pass
// with the service-account guard deleted.
func TestUpdateClientUserAdmin_RefusesAServiceAccountRole(t *testing.T) {
	t.Parallel()
	h, clients, operators, sink := newClientAdminHarness(t)
	operators.seed("client-actor", "super_admin")
	clients.seedKind("client-target", "guest", iface.UserKindService)

	err := patchClientRole(t, h, operatorCtx("client-actor", "super_admin"), "client-target", "administrator")

	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if got := clients.roleOf("client-target"); got != "guest" {
		t.Errorf("target role = %q, want it unchanged at %q", got, "guest")
	}
	if n := clients.updateCalls(); n != 0 {
		t.Errorf("the guard must refuse before the write; %d update calls", n)
	}
	ev := findAudit(sink, "user.update.refused")
	if ev == nil {
		t.Fatalf("a refused client role change must emit user.update.refused; got %+v", sink.events)
	}
	if got := ev.Metadata["attempted"]; got != "service_account_privileged_role" {
		t.Fatalf("metadata.attempted = %v, want %q — the refusal came from the WRONG guard", got, "service_account_privileged_role")
	}
	if ev.ResourceType != "client_user" {
		t.Errorf("ResourceType = %q, want %q", ev.ResourceType, "client_user")
	}
}

// A machine principal may still be moved between non-privileged roles.
func TestUpdateClientUserAdmin_AllowsANonPrivilegedServiceAccountRole(t *testing.T) {
	t.Parallel()
	h, clients, operators, _ := newClientAdminHarness(t)
	operators.seed("client-actor", "super_admin")
	clients.seedKind("client-target", "guest", iface.UserKindService)

	if err := patchClientRole(t, h, operatorCtx("client-actor", "super_admin"), "client-target", "operator"); err != nil {
		t.Fatalf("a non-privileged role on a service account must be allowed: %v", err)
	}
	if got := clients.roleOf("client-target"); got != "operator" {
		t.Errorf("target role = %q, want %q", got, "operator")
	}
}

// --- the quorum stays operator-only -----------------------------------

// TestUpdateClientUserAdmin_NoLastAdminQuorum pins the deliberate
// asymmetry. A client user is never a platform administrator, so the
// last-administrator quorum must not be ported to this tier: the store
// reports ZERO remaining platform administrators and the demotion must
// still go through, and CountActiveAdministrators must never be called.
func TestUpdateClientUserAdmin_NoLastAdminQuorum(t *testing.T) {
	t.Parallel()
	h, clients, operators, _ := newClientAdminHarness(t)
	operators.seed("client-actor", "super_admin")
	clients.seed("client-target", "administrator")
	counted := false
	clients.svc.countActiveAdminsFn = func(context.Context, string) (int64, error) {
		counted = true
		return 0, nil // demoting the target would strand the platform
	}

	if err := patchClientRole(t, h, operatorCtx("client-actor", "super_admin"), "client-target", "guest"); err != nil {
		t.Fatalf("the client PATCH must not run the platform quorum check: %v", err)
	}
	if counted {
		t.Error("CountActiveAdministrators was consulted; the quorum is operator-only")
	}
	if got := clients.roleOf("client-target"); got != "guest" {
		t.Errorf("target role = %q, want %q", got, "guest")
	}
}

// --- the guard is scoped to the role branch ---------------------------

// TestUpdateClientUserAdmin_ProfileOnlyPatchRunsNoRoleGuards pins that a
// patch carrying no role costs no caller lookup — and therefore keeps
// working with no operator provider wired at all.
func TestUpdateClientUserAdmin_ProfileOnlyPatchRunsNoRoleGuards(t *testing.T) {
	t.Parallel()
	clients := newUserStore(nil)
	clients.seed("client-target", "operator")
	h, _ := newAdminHandler(clients.svc) // registry deliberately empty

	if _, err := h.UpdateClientUserAdmin(context.Background(), &UpdateClientUserAdminRequest{
		ID:   "client-target",
		Body: UpdateClientUserAdminBody{FullName: "Renamed"},
	}); err != nil {
		t.Fatalf("a profile-only patch must not run the role guards: %v", err)
	}
	if got := clients.roleOf("client-target"); got != "operator" {
		t.Errorf("target role = %q, want it unchanged", got)
	}
}

// --- the audit row shape ----------------------------------------------

// TestUpdateClientUserAdmin_RoleChangedAuditRowCarriesThePropagationFlags
// pins that one action name has one metadata contract regardless of tier.
// `user.role.changed` carries cache_invalidated and sessions_terminated on
// the operator tier (M-13); on the client tier both are literally false —
// neither happens — and `propagation` says WHY, so a reader never reads a
// client-tier false as an operator-tier "we tried and failed".
func TestUpdateClientUserAdmin_RoleChangedAuditRowCarriesThePropagationFlags(t *testing.T) {
	t.Parallel()
	h, clients, operators, sink := newClientAdminHarness(t)
	operators.seed("client-actor", "super_admin")
	clients.seed("client-target", "operator")

	if err := patchClientRole(t, h, operatorCtx("client-actor", "super_admin"), "client-target", "manager"); err != nil {
		t.Fatalf("err = %v", err)
	}

	ev := findAudit(sink, "user.role.changed")
	if ev == nil {
		t.Fatalf("a successful client role change must emit user.role.changed; got %+v", sink.events)
	}
	if ev.ResourceType != "client_user" {
		t.Errorf("ResourceType = %q, want %q", ev.ResourceType, "client_user")
	}
	for key, want := range map[string]any{
		"from":                "operator",
		"to":                  "manager",
		"cache_invalidated":   false,
		"sessions_terminated": false,
		"propagation":         clientTierNoPropagation,
	} {
		got, ok := ev.Metadata[key]
		if !ok {
			t.Errorf("metadata is missing %q; the operator and client tiers must agree on this action's contract", key)
			continue
		}
		if got != want {
			t.Errorf("metadata[%q] = %v, want %v", key, got, want)
		}
	}
}

// --- the create and invite paths (ruling P32) -------------------------
//
// A canAssignRole guard on the PATCH that can be sidestepped by CREATING
// or INVITING a client user at a role above your own is not a fix, it is
// the appearance of one: M-17 would stay reachable as a two-step while
// the finding is retired. Both create paths carry the same required
// `oneof=super_admin … guest` body field, so both get the same guard.
//
// serviceAccountRoleAllowed is deliberately NOT applied here, and the
// reason is structural rather than an omission: iface.CreateUserInput.Kind
// is `json:"-"` and neither handler ever sets it, so a create can only
// ever produce a human principal and the guard could not fire. The
// operator tier's CreateUser makes exactly the same call. It is pinned
// below by TestClientAdminCreatePathsNeverMintAServiceAccount, so the day
// someone plumbs Kind through either body, that test breaks and the guard
// becomes required.

// authorisedOperator registers a super_admin operator caller on reg and
// returns the context to call with. Used by the create/invite tests that
// predate these guards: their subject is the hasher / inviter / service
// error path, not authorisation, so they need a caller that passes.
func authorisedOperator(reg *module.ServiceRegistry) context.Context {
	reg.Register(module.ServiceOperatorUserProvider, newOperatorStore().seed("op-admin", "super_admin"))
	return operatorCtx("op-admin", "super_admin")
}

// recordingHasher counts ValidatePolicy/Hash so a test can prove the
// guard refused BEFORE the deliberately-expensive argon2id hash ran.
type recordingHasher struct {
	fakePasswordHasher
	hashed int
}

func (r *recordingHasher) Hash(plaintext string) (string, error) {
	r.hashed++
	return r.fakePasswordHasher.Hash(plaintext)
}

func TestCreateClientUserAdmin_RefusesEscalation(t *testing.T) {
	t.Parallel()
	created := 0
	svc := &fakeUserService{
		createUserWithPasswordFn: func(context.Context, *iface.CreateUserInput) (*iface.User, error) {
			created++
			return &iface.User{UUID: "nope"}, nil
		},
		markEmailVerifiedFn: func(context.Context, string) error { return nil },
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "nope"}, nil
		},
	}
	sink := &captureSink{}
	svc.sink = sink
	h, reg := newAdminHandler(svc)
	operators := newOperatorStore().seed("op-actor", "administrator")
	reg.Register(module.ServiceOperatorUserProvider, operators)
	hasher := &recordingHasher{}
	reg.Register(module.ServicePasswordService, hasher)

	_, err := h.CreateClientUserAdmin(operatorCtx("op-actor", "super_admin"), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{
			Email: "victim@b.c", FullName: "x", Role: "super_admin", Password: "Hunter2!Hunter2!",
		},
	})

	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if created != 0 {
		t.Errorf("the guard must refuse before the write; %d creates", created)
	}
	if hasher.hashed != 0 {
		t.Errorf("the guard must refuse before the argon2id hash; %d hashes", hasher.hashed)
	}
	ev := findAudit(sink, "user.create.refused")
	if ev == nil {
		t.Fatalf("a refused client create must emit user.create.refused, like the operator handler; got %+v", sink.events)
	}
	if ev.ResourceType != "client_user" {
		t.Errorf("ResourceType = %q, want %q", ev.ResourceType, "client_user")
	}
	if ev.Outcome != "denied" {
		t.Errorf("Outcome = %q, want %q", ev.Outcome, "denied")
	}
	if got := ev.Metadata["attempted"]; got != "role_escalation" {
		t.Errorf("metadata.attempted = %v, want %q", got, "role_escalation")
	}
	if got := ev.Metadata["to"]; got != "super_admin" {
		t.Errorf("metadata.to = %v, want %q", got, "super_admin")
	}
	if got := ev.Metadata["email"]; got != "victim@b.c" {
		t.Errorf("metadata.email = %v, want %q", got, "victim@b.c")
	}
}

// The outage guard for the create path: the caller is resolved from the
// operator provider, and the client store does not know that id.
func TestCreateClientUserAdmin_LegitimateOperatorCallerStillSucceeds(t *testing.T) {
	t.Parallel()
	svc := &fakeUserService{
		createUserWithPasswordFn: func(_ context.Context, in *iface.CreateUserInput) (*iface.User, error) {
			return &iface.User{UUID: "new-uuid", Email: in.Email}, nil
		},
		markEmailVerifiedFn: func(context.Context, string) error { return nil },
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "new-uuid"}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	operators := newOperatorStore().seed("op-actor", "administrator")
	reg.Register(module.ServiceOperatorUserProvider, operators)
	reg.Register(module.ServicePasswordService, &fakePasswordHasher{})

	resp, err := h.CreateClientUserAdmin(operatorCtx("op-actor", "administrator"), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{
			Email: "a@b.c", FullName: "x", Role: "manager", Password: "Hunter2!Hunter2!",
		},
	})
	if err != nil {
		t.Fatalf("a legitimate operator caller must still be able to create a client user: %v", err)
	}
	if resp.Body.ID != "new-uuid" {
		t.Errorf("body.ID = %q", resp.Body.ID)
	}
	if got := operators.lookups(); len(got) != 1 || got[0] != "op-actor" {
		t.Errorf("operator lookups = %v, want exactly one for the ACTOR", got)
	}
}

func TestCreateClientUserAdmin_CallerLookupFailureIsInternal(t *testing.T) {
	t.Parallel()
	created := 0
	svc := &fakeUserService{
		createUserWithPasswordFn: func(context.Context, *iface.CreateUserInput) (*iface.User, error) {
			created++
			return &iface.User{UUID: "nope"}, nil
		},
		markEmailVerifiedFn: func(context.Context, string) error { return nil },
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "nope"}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	reg.Register(module.ServiceOperatorUserProvider,
		newOperatorStore().failWith(errors.New("mongo: connection refused")))
	reg.Register(module.ServicePasswordService, &fakePasswordHasher{})

	// The claim says super_admin and WOULD have allowed this.
	_, err := h.CreateClientUserAdmin(operatorCtx("op-actor", "super_admin"), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{
			Email: "a@b.c", FullName: "x", Role: "super_admin", Password: "Hunter2!Hunter2!",
		},
	})

	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if created != 0 {
		t.Errorf("the guard must refuse before the write; %d creates", created)
	}
}

func TestInviteClientUserAdmin_RefusesEscalation(t *testing.T) {
	t.Parallel()
	created := 0
	svc := &fakeUserService{
		createUserFn: func(context.Context, *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
			created++
			return &iface.UserManagementResponse{ID: "nope"}, nil
		},
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "nope"}, nil
		},
	}
	sink := &captureSink{}
	svc.sink = sink
	h, reg := newAdminHandler(svc)
	reg.Register(module.ServiceOperatorUserProvider, newOperatorStore().seed("op-actor", "manager"))
	// NO inviter registered. The refusal must be the 403, not the 503:
	// authorisation is decided before the auth module is even consulted,
	// so a role escalation cannot be masked by a degraded dependency.

	_, err := h.InviteClientUserAdmin(operatorCtx("op-actor", "super_admin"), &InviteClientUserAdminRequest{
		Body: InviteClientUserAdminBody{Email: "victim@b.c", FullName: "x", Role: "administrator"},
	})

	assertStatus(t, err, http.StatusForbidden)
	assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	if created != 0 {
		t.Errorf("the guard must refuse before the write; %d creates", created)
	}
	ev := findAudit(sink, "user.create.refused")
	if ev == nil {
		t.Fatalf("a refused client invite must emit user.create.refused; got %+v", sink.events)
	}
	if ev.ResourceType != "client_user" {
		t.Errorf("ResourceType = %q, want %q", ev.ResourceType, "client_user")
	}
	if got := ev.Metadata["attempted"]; got != "role_escalation" {
		t.Errorf("metadata.attempted = %v, want %q", got, "role_escalation")
	}
	if got := ev.Metadata["to"]; got != "administrator" {
		t.Errorf("metadata.to = %v, want %q", got, "administrator")
	}
}

// The outage guard for the invite path.
func TestInviteClientUserAdmin_LegitimateOperatorCallerStillSucceeds(t *testing.T) {
	t.Parallel()
	svc := &fakeUserService{
		createUserFn: func(_ context.Context, in *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "u1", Email: in.Email}, nil
		},
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "u1"}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	operators := newOperatorStore().seed("op-actor", "administrator")
	reg.Register(module.ServiceOperatorUserProvider, operators)
	reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
		sendInviteFn: func(context.Context, string, string) error { return nil },
	})

	resp, err := h.InviteClientUserAdmin(operatorCtx("op-actor", "administrator"), &InviteClientUserAdminRequest{
		Body: InviteClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "manager"},
	})
	if err != nil {
		t.Fatalf("a legitimate operator caller must still be able to invite a client user: %v", err)
	}
	if resp.Body.ID != "u1" {
		t.Errorf("body.ID = %q", resp.Body.ID)
	}
	if got := operators.lookups(); len(got) != 1 || got[0] != "op-actor" {
		t.Errorf("operator lookups = %v, want exactly one for the ACTOR", got)
	}
}

func TestInviteClientUserAdmin_CallerLookupFailureIsInternal(t *testing.T) {
	t.Parallel()
	created := 0
	svc := &fakeUserService{
		createUserFn: func(context.Context, *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
			created++
			return &iface.UserManagementResponse{ID: "nope"}, nil
		},
		getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
			return &iface.UserManagementResponse{ID: "nope"}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	// Provider deliberately NOT registered.
	reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
		sendInviteFn: func(context.Context, string, string) error { return nil },
	})

	_, err := h.InviteClientUserAdmin(operatorCtx("op-actor", "super_admin"), &InviteClientUserAdminRequest{
		Body: InviteClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "super_admin"},
	})

	assertStatus(t, err, http.StatusInternalServerError)
	assertErrCode(t, err, errcode.UserRoleLookupUnavailable)
	if created != 0 {
		t.Errorf("the guard must refuse before the write; %d creates", created)
	}
}

// TestClientAdminCreatePathsNeverMintAServiceAccount is why
// serviceAccountRoleAllowed is absent from the two create paths rather
// than forgotten. iface.CreateUserInput.Kind is `json:"-"`, so no request
// body can set it, and neither handler sets it itself — the guard could
// never fire and no test could falsify it. If this ever fails, Kind has
// become reachable and BOTH create paths need the guard the PATCH has.
func TestClientAdminCreatePathsNeverMintAServiceAccount(t *testing.T) {
	t.Parallel()
	t.Run("create", func(t *testing.T) {
		t.Parallel()
		var seen *iface.CreateUserInput
		svc := &fakeUserService{
			createUserWithPasswordFn: func(_ context.Context, in *iface.CreateUserInput) (*iface.User, error) {
				seen = in
				return &iface.User{UUID: "u1"}, nil
			},
			markEmailVerifiedFn: func(context.Context, string) error { return nil },
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1"}, nil
			},
		}
		h, reg := newAdminHandler(svc)
		ctx := authorisedOperator(reg)
		reg.Register(module.ServicePasswordService, &fakePasswordHasher{})
		if _, err := h.CreateClientUserAdmin(ctx, &CreateClientUserAdminRequest{
			Body: CreateClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "operator", Password: "Hunter2!Hunter2!"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
		if seen == nil || seen.Kind != "" {
			t.Fatalf("Kind = %q, want empty — a machine principal is unreachable from this body", seen.Kind)
		}
	})
	t.Run("invite", func(t *testing.T) {
		t.Parallel()
		var seen *iface.CreateUserInput
		svc := &fakeUserService{
			createUserFn: func(_ context.Context, in *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
				seen = in
				return &iface.UserManagementResponse{ID: "u1"}, nil
			},
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1"}, nil
			},
		}
		h, reg := newAdminHandler(svc)
		ctx := authorisedOperator(reg)
		reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
			sendInviteFn: func(context.Context, string, string) error { return nil },
		})
		if _, err := h.InviteClientUserAdmin(ctx, &InviteClientUserAdminRequest{
			Body: InviteClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "operator"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
		if seen == nil || seen.Kind != "" {
			t.Fatalf("Kind = %q, want empty — a machine principal is unreachable from this body", seen.Kind)
		}
	})
}
