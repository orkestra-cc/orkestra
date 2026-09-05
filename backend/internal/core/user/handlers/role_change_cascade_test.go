package handlers

// M-13 / spec §4.6 D27 (as amended: D27 splits by direction).
//
// A system-role change used to flush no authorization cache and end no
// session, so a demoted administrator kept administrator verdicts for up
// to the 60s permission-cache TTL plus a whole access-token lifetime of
// stale `srole`.
//
// The role branch now does what the deactivate branch does, in the one
// order that is safe in BOTH directions: write, then best-effort
// invalidate, then terminate the sessions minted under the old role,
// then audit what was actually achieved.
//
// There is deliberately NO refusal. A role change is never made safer by
// refusing it: a refused DEMOTION recreates M-13 exactly — the demoted
// administrator keeps administrator verdicts permanently instead of for
// at most one cache TTL — and a refused PROMOTION only delays a harmless
// stale deny. Everything the handler could not achieve is reported in the
// audit row (`cache_invalidated`, `sessions_terminated`) instead.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ---------------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------------

// callTrace records the order in which the collaborators were reached,
// so a test can pin "write → invalidate → terminate → audit" rather than
// merely counting calls. Nil-safe: the shared fixtures carry an optional
// pointer to one and every other test in the package leaves it nil.
type callTrace struct {
	mu    sync.Mutex
	steps []string
}

func (c *callTrace) add(step string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, step)
}

func (c *callTrace) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.steps...)
}

// recordingInvalidator stands in for the authz service behind
// module.ServiceAuthzProvider. It implements iface.AuthzProvider *as
// well as* iface.AuthzCacheInvalidator and is registered through the
// former, exactly as authz/module.go registers the real service — so
// these tests also pin that the handler's interface-to-interface type
// assertion still resolves through the registered static type.
type recordingInvalidator struct {
	mu          sync.Mutex
	invalidated []string
	err         error
	trace       *callTrace
}

func (r *recordingInvalidator) InvalidateUserPermissions(_ context.Context, userUUID string) error {
	r.mu.Lock()
	r.invalidated = append(r.invalidated, userUUID)
	r.mu.Unlock()
	r.trace.add("invalidate")
	return r.err
}

func (r *recordingInvalidator) calls(userUUID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, got := range r.invalidated {
		if got == userUUID {
			n++
		}
	}
	return n
}

func (r *recordingInvalidator) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.invalidated)
}

// The AuthzProvider half. Unused by the handler — a call is a
// regression signal, not a legitimate path.
func (r *recordingInvalidator) HasPermission(context.Context, string, string, string) (bool, error) {
	panic("unused: HasPermission")
}
func (r *recordingInvalidator) GetEffectivePermissions(context.Context, string, string) ([]string, error) {
	panic("unused: GetEffectivePermissions")
}
func (r *recordingInvalidator) RegisterPermissions(context.Context, []iface.PermissionSpec) error {
	panic("unused: RegisterPermissions")
}

// bareAuthzProvider is an authz service that predates the invalidator
// seam: registered under the same key, satisfying iface.AuthzProvider
// and nothing more. The handler must report "nothing retired" rather
// than refuse the change.
type bareAuthzProvider struct{}

func (bareAuthzProvider) HasPermission(context.Context, string, string, string) (bool, error) {
	panic("unused: HasPermission")
}
func (bareAuthzProvider) GetEffectivePermissions(context.Context, string, string) ([]string, error) {
	panic("unused: GetEffectivePermissions")
}
func (bareAuthzProvider) RegisterPermissions(context.Context, []iface.PermissionSpec) error {
	panic("unused: RegisterPermissions")
}

// userStore is the seedable backing the package's fakeUserService never
// had: a tiny in-memory map wired into that fake's function fields, so a
// test can seed a user, change their role through the handler and read
// the persisted result back. Deliberately a wrapper — the package keeps
// exactly one fakeUserService.
type userStore struct {
	svc *fakeUserService

	mu      sync.Mutex
	rows    map[string]*iface.UserManagementResponse
	updates int
	admins  int64 // what CountActiveAdministrators reports

	trace *callTrace
}

func newUserStore(trace *callTrace) *userStore {
	s := &userStore{
		rows:   map[string]*iface.UserManagementResponse{},
		admins: 3, // quorum is never the thing under test here
		trace:  trace,
	}
	s.svc = &fakeUserService{
		getUserFn: func(_ context.Context, id string) (*iface.UserManagementResponse, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			row, ok := s.rows[id]
			if !ok {
				// Loud, never silent. An unseeded id would otherwise
				// leave `previous` nil, and the handler would sail past
				// the entire role branch with every assertion below
				// still "passing" for the wrong reason.
				return nil, services.ErrUserNotFound
			}
			copied := *row
			return &copied, nil
		},
		updateUserFn: func(_ context.Context, id string, in *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
			s.trace.add("write")
			s.mu.Lock()
			defer s.mu.Unlock()
			row, ok := s.rows[id]
			if !ok {
				return nil, services.ErrUserNotFound
			}
			s.updates++
			if in.Role != "" {
				row.Role = in.Role
			}
			if in.FullName != "" {
				row.FullName = in.FullName
			}
			if in.IsActive != nil {
				row.IsActive = *in.IsActive
			}
			copied := *row
			return &copied, nil
		},
		countActiveAdminsFn: func(context.Context, string) (int64, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.admins, nil
		},
	}
	return s
}

func (s *userStore) seed(uuid, role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[uuid] = &iface.UserManagementResponse{
		ID:       uuid,
		Email:    uuid + "@orkestra.local",
		FullName: uuid,
		Role:     role,
		IsActive: true,
	}
}

func (s *userStore) roleOf(uuid string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[uuid]
	if !ok {
		return ""
	}
	return row.Role
}

func (s *userStore) updateCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updates
}

type harnessCfg struct {
	invalidatorErr error
	terminatorErr  error
	noInvalidator  bool
	bareProvider   bool
	noTerminator   bool
	noRegistry     bool
}

type harnessOpt func(*harnessCfg)

// withInvalidationFailure simulates "cache configured but unavailable" —
// the one authz cache state that is an error.
func withInvalidationFailure(err error) harnessOpt {
	return func(c *harnessCfg) { c.invalidatorErr = err }
}

func withTerminationFailure(err error) harnessOpt {
	return func(c *harnessCfg) { c.terminatorErr = err }
}

// withoutInvalidator leaves module.ServiceAuthzProvider unregistered.
func withoutInvalidator() harnessOpt {
	return func(c *harnessCfg) { c.noInvalidator = true }
}

// withBareAuthzProvider registers an AuthzProvider that does not
// implement the invalidator seam.
func withBareAuthzProvider() harnessOpt {
	return func(c *harnessCfg) { c.bareProvider = true }
}

// withoutTerminator leaves module.ServiceAuthService unregistered — the
// auth module trimmed by a fork, or simply not yet initialised.
func withoutTerminator() harnessOpt {
	return func(c *harnessCfg) { c.noTerminator = true }
}

// withoutRegistry builds the handler with no ServiceRegistry at all.
func withoutRegistry() harnessOpt {
	return func(c *harnessCfg) { c.noRegistry = true }
}

type roleChangeHarness struct {
	h           *UserHandler
	users       *userStore
	invalidator *recordingInvalidator
	sessions    *recordingTerminator
	audit       *captureSink
	trace       *callTrace
}

func newRoleChangeHarness(t *testing.T, opts ...harnessOpt) *roleChangeHarness {
	t.Helper()

	cfg := harnessCfg{}
	for _, opt := range opts {
		opt(&cfg)
	}

	trace := &callTrace{}
	users := newUserStore(trace)
	sink := &captureSink{trace: trace}
	users.svc.sink = sink

	inv := &recordingInvalidator{err: cfg.invalidatorErr, trace: trace}
	term := &recordingTerminator{err: cfg.terminatorErr, trace: trace}

	h := NewUserHandler(users.svc)
	if !cfg.noRegistry {
		reg := module.NewServiceRegistry()
		switch {
		case cfg.bareProvider:
			reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(bareAuthzProvider{}))
		case !cfg.noInvalidator:
			// Registered through iface.AuthzProvider, byte-for-byte as
			// authz/module.go does it.
			reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(inv))
		}
		if !cfg.noTerminator {
			reg.Register(module.ServiceAuthService, iface.SessionTerminator(term))
		}
		h.SetServiceRegistry(reg)
	}

	return &roleChangeHarness{
		h:           h,
		users:       users,
		invalidator: inv,
		sessions:    term,
		audit:       sink,
		trace:       trace,
	}
}

// update runs the handler as a super_admin operator ("test-admin").
func (hn *roleChangeHarness) update(t *testing.T, id string, body iface.UpdateUserInput) error {
	t.Helper()
	_, err := hn.h.UpdateUser(adminCtx(), &UpdateUserRequest{ID: id, Body: body})
	return err
}

// requireSeeded fails before the interesting assertions if the fixture
// never arranged the state under test — the precondition that turns a
// vacuous pass into a failure.
func (hn *roleChangeHarness) requireSeeded(t *testing.T, id, role string) {
	t.Helper()
	if got := hn.users.roleOf(id); got != role {
		t.Fatalf("fixture: user %q seeded with role %q, want %q", id, got, role)
	}
}

func (hn *roleChangeHarness) terminations(userUUID string) int {
	n := 0
	for _, got := range hn.sessions.list() {
		if got == userUUID {
			n++
		}
	}
	return n
}

// roleChangedEvent returns the single user.role.changed audit row, and
// fails when there is not exactly one — an absent event would otherwise
// make every metadata assertion below vacuously true.
func (hn *roleChangeHarness) roleChangedEvent(t *testing.T) iface.AuditEvent {
	t.Helper()
	var found []iface.AuditEvent
	for _, ev := range hn.audit.events {
		if ev.Action == "user.role.changed" {
			found = append(found, ev)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one user.role.changed audit row, got %d (%v)", len(found), hn.audit.events)
	}
	return found[0]
}

func metaBool(t *testing.T, ev iface.AuditEvent, key string) bool {
	t.Helper()
	raw, ok := ev.Metadata[key]
	if !ok {
		t.Fatalf("audit metadata has no %q key: %v", key, ev.Metadata)
	}
	got, ok := raw.(bool)
	if !ok {
		t.Fatalf("audit metadata %q = %T(%v), want bool", key, raw, raw)
	}
	return got
}

// assertOrder checks that the named steps appear in the given order in
// the trace, each exactly once.
func assertOrder(t *testing.T, trace *callTrace, want ...string) {
	t.Helper()
	steps := trace.list()
	at := func(step string) int {
		idx := -1
		for i, got := range steps {
			if got == step {
				if idx >= 0 {
					t.Fatalf("step %q occurred more than once in %v", step, steps)
				}
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("step %q never happened; trace = %v", step, steps)
		}
		return idx
	}
	prev := -1
	for _, step := range want {
		idx := at(step)
		if idx <= prev {
			t.Fatalf("trace = %v, want the order %v", steps, want)
		}
		prev = idx
	}
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

// The whole of M-13, in one pass: the change lands, the cached verdicts
// are retired, the sessions minted under the old role end, and the audit
// row says all three happened — in that order.
func TestUpdateUser_RoleChangeRetiresCacheThenSessionsThenAudits(t *testing.T) {
	hn := newRoleChangeHarness(t)
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	if got := hn.users.roleOf("u-1"); got != "operator" {
		t.Fatalf("role = %q, want the demotion persisted", got)
	}
	if got := hn.invalidator.calls("u-1"); got != 1 {
		t.Fatalf("authz cache invalidated %d times, want 1", got)
	}
	if got := hn.terminations("u-1"); got != 1 {
		t.Fatalf("sessions terminated %d times, want 1 — a role change ends the sessions minted under the old role", got)
	}

	ev := hn.roleChangedEvent(t)
	if !metaBool(t, ev, "cache_invalidated") {
		t.Error("cache_invalidated must be true when the invalidation succeeded")
	}
	if !metaBool(t, ev, "sessions_terminated") {
		t.Error("sessions_terminated must be true when the teardown succeeded")
	}
	if ev.Metadata["from"] != "administrator" || ev.Metadata["to"] != "operator" {
		t.Errorf("from/to = %v/%v, want administrator/operator", ev.Metadata["from"], ev.Metadata["to"])
	}

	// The order is the mechanism, not an accident: invalidating BEFORE
	// the write would retire verdicts computed from the old role and
	// then leave the write free to be cached again from a concurrent
	// read.
	assertOrder(t, hn.trace, "write", "invalidate", "terminate", "audit")
}

// D27 as amended: a cache that cannot be retired NEVER refuses the
// change. Refusing a demotion is M-13 itself, permanently.
func TestUpdateUser_InvalidationFailureStillAppliesTheChange(t *testing.T) {
	cases := []struct {
		name       string
		seededRole string
		toRole     string
	}{
		// The dangerous direction: a stale cached verdict is an ALLOW.
		{name: "demotion", seededRole: "administrator", toRole: "operator"},
		// The harmless one: a stale cached verdict is a DENY. Refusing
		// it would still be wrong — it just costs less.
		{name: "promotion", seededRole: "operator", toRole: "administrator"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			hn := newRoleChangeHarness(t, withInvalidationFailure(errors.New("redis down")))
			hn.users.seed("u-1", c.seededRole)
			hn.requireSeeded(t, "u-1", c.seededRole)

			if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: c.toRole}); err != nil {
				t.Fatalf("the change must stand, got %v", err)
			}

			if got := hn.users.roleOf("u-1"); got != c.toRole {
				t.Fatalf("role = %q, want %q persisted despite the cache failure", got, c.toRole)
			}
			if got := hn.users.updateCalls(); got != 1 {
				t.Fatalf("UpdateUser called %d times, want 1 — the write must not be skipped", got)
			}
			if got := hn.invalidator.calls("u-1"); got != 1 {
				t.Fatalf("the invalidation must still be attempted, got %d calls", got)
			}
			if got := hn.terminations("u-1"); got != 1 {
				t.Fatalf("sessions terminated %d times, want 1 — termination does not depend on the cache", got)
			}

			ev := hn.roleChangedEvent(t)
			if metaBool(t, ev, "cache_invalidated") {
				t.Error("cache_invalidated must be false: nothing was retired")
			}
			if !metaBool(t, ev, "sessions_terminated") {
				t.Error("sessions_terminated must be true: the teardown succeeded")
			}
		})
	}
}

// An invalidator absent from the registry is reported, never refused —
// a fork that trims the authz module, or a boot order that has not
// registered it yet, must not lose the ability to demote anyone.
func TestUpdateUser_MissingInvalidatorStillAppliesTheChange(t *testing.T) {
	hn := newRoleChangeHarness(t, withoutInvalidator())
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator"}); err != nil {
		t.Fatalf("the change must stand, got %v", err)
	}

	if got := hn.users.roleOf("u-1"); got != "operator" {
		t.Fatalf("role = %q, want operator", got)
	}
	if got := hn.terminations("u-1"); got != 1 {
		t.Fatalf("sessions terminated %d times, want 1", got)
	}
	if metaBool(t, hn.roleChangedEvent(t), "cache_invalidated") {
		t.Error("cache_invalidated must be false when no invalidator is wired")
	}
}

// An authz service registered under the same key that does not satisfy
// the invalidator seam (an older fork build) takes the same path.
func TestUpdateUser_ProviderWithoutInvalidatorIsReportedNotRefused(t *testing.T) {
	hn := newRoleChangeHarness(t, withBareAuthzProvider())
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator"}); err != nil {
		t.Fatalf("the change must stand, got %v", err)
	}
	if got := hn.users.roleOf("u-1"); got != "operator" {
		t.Fatalf("role = %q, want operator", got)
	}
	if metaBool(t, hn.roleChangedEvent(t), "cache_invalidated") {
		t.Error("cache_invalidated must be false: the registered provider retires nothing")
	}
}

// With nothing wired at all the handler must not reach into a nil
// registry — module.ServiceRegistry.Get takes a lock on the receiver.
func TestUpdateUser_RoleChangeWithoutRegistryStillSucceeds(t *testing.T) {
	hn := newRoleChangeHarness(t, withoutRegistry())
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator"}); err != nil {
		t.Fatalf("the change must stand, got %v", err)
	}
	if got := hn.users.roleOf("u-1"); got != "operator" {
		t.Fatalf("role = %q, want operator", got)
	}
	ev := hn.roleChangedEvent(t)
	if metaBool(t, ev, "cache_invalidated") || metaBool(t, ev, "sessions_terminated") {
		t.Error("with no registry nothing was retired and nothing was terminated; the audit row must say so")
	}
}

// A non-role patch pays none of this.
func TestUpdateUser_NonRolePatchRetiresNothing(t *testing.T) {
	hn := newRoleChangeHarness(t)
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{FullName: "New Name"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got := hn.invalidator.total(); got != 0 {
		t.Errorf("a rename must not touch the authz cache, got %d invalidations", got)
	}
	if got := hn.terminations("u-1"); got != 0 {
		t.Errorf("a rename must not sign the user out, got %d terminations", got)
	}
}

// Neither does a patch that re-sends the role the user already holds.
func TestUpdateUser_UnchangedRoleRetiresNothing(t *testing.T) {
	hn := newRoleChangeHarness(t)
	hn.users.seed("u-1", "operator")
	hn.requireSeeded(t, "u-1", "operator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got := hn.invalidator.total(); got != 0 {
		t.Errorf("a no-op role patch must not touch the authz cache, got %d invalidations", got)
	}
	if got := hn.terminations("u-1"); got != 0 {
		t.Errorf("a no-op role patch must not sign the user out, got %d terminations", got)
	}
}

// Termination is best-effort exactly as it is on the deactivate path:
// the change stands and the shortfall is audited.
func TestUpdateUser_TerminationFailureIsAuditedNotFailed(t *testing.T) {
	hn := newRoleChangeHarness(t, withTerminationFailure(errors.New("redis down")))
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator"}); err != nil {
		t.Fatalf("a session-teardown failure must not fail the role change: %v", err)
	}
	if got := hn.terminations("u-1"); got != 1 {
		t.Fatalf("the teardown must still be attempted, got %d calls", got)
	}
	ev := hn.roleChangedEvent(t)
	if metaBool(t, ev, "sessions_terminated") {
		t.Error("sessions_terminated must be false when the teardown failed")
	}
	if !metaBool(t, ev, "cache_invalidated") {
		t.Error("cache_invalidated must still be true — the two are independent")
	}
}

// Edge case 17: with the auth module unwired the termination degrades to
// a no-op, the cache work still happens, and the row records the gap.
func TestUpdateUser_UnwiredTerminatorDegrades(t *testing.T) {
	hn := newRoleChangeHarness(t, withoutTerminator())
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator"}); err != nil {
		t.Fatalf("termination is best-effort: %v", err)
	}
	if got := hn.invalidator.calls("u-1"); got != 1 {
		t.Fatalf("the cache must still be retired, got %d invalidations", got)
	}
	if metaBool(t, hn.roleChangedEvent(t), "sessions_terminated") {
		t.Error("sessions_terminated must be false with no terminator wired")
	}
}

// Edge case 16: changing one's OWN role ends one's own sessions. The
// response is still 200 — the operator signs back in under the new role.
func TestUpdateUser_SelfRoleChangeEndsTheActorsOwnSessions(t *testing.T) {
	hn := newRoleChangeHarness(t)
	// adminCtx() signs the request as "test-admin".
	hn.users.seed("test-admin", "super_admin")
	hn.requireSeeded(t, "test-admin", "super_admin")

	if err := hn.update(t, "test-admin", iface.UpdateUserInput{Role: "administrator"}); err != nil {
		t.Fatalf("a self-demotion must still succeed: %v", err)
	}
	if got := hn.terminations("test-admin"); got != 1 {
		t.Fatalf("the actor's own sessions end too, got %d terminations", got)
	}
	if got := hn.invalidator.calls("test-admin"); got != 1 {
		t.Fatalf("the actor's own cached verdicts are retired too, got %d", got)
	}
}

// A patch that both deactivates and changes the role must tear the
// sessions down once, not twice, and report that one outcome.
func TestUpdateUser_DeactivationWithRoleChangeTerminatesOnce(t *testing.T) {
	hn := newRoleChangeHarness(t)
	hn.users.seed("u-1", "administrator")
	hn.requireSeeded(t, "u-1", "administrator")

	if err := hn.update(t, "u-1", iface.UpdateUserInput{Role: "operator", IsActive: boolPtr(false)}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got := hn.terminations("u-1"); got != 1 {
		t.Fatalf("sessions terminated %d times, want exactly 1", got)
	}
	if !metaBool(t, hn.roleChangedEvent(t), "sessions_terminated") {
		t.Error("sessions_terminated must report the one teardown that happened")
	}
}
