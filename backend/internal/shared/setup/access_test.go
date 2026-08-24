package setup

// Task 5.4: the read-only finalizer access probe. These tests exercise
// evaluateAccess directly (the shared seam Task 5.5's finalize POST also
// uses) and the FinalizationAccess HTTP handler built on top of it.
//
// fakeLifecycleUsers plays the role of "fake lifecycle provider" the task
// brief calls for: it satisfies both iface.UserProvider (embedded, panics
// on anything not overridden) and iface.UserLifecycleStateProvider, mirroring
// how the real user-module service is wired in production — NewService
// derives Service.lifecycle from a type assertion on the users argument,
// never a separate constructor parameter (see service.go).
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeLifecycleUsers is the fake lifecycle-capable UserProvider used across
// this file. `calls` records every UUID looked up (in order) so tests can
// assert evaluateAccess's short-circuit behavior (e.g. it must never
// consult a non-super_admin caller's own lifecycle state).
type fakeLifecycleUsers struct {
	iface.UserProvider
	count    int64
	countErr error
	states   map[string]iface.UserLifecycleState
	err      error
	calls    []string
}

func (f *fakeLifecycleUsers) GetUserCount(_ context.Context, _ *iface.UserFilters) (int64, error) {
	return f.count, f.countErr
}

func (f *fakeLifecycleUsers) UserLifecycleState(_ context.Context, userUUID string) (iface.UserLifecycleState, error) {
	f.calls = append(f.calls, userUUID)
	if f.err != nil {
		return "", f.err
	}
	if st, ok := f.states[userUUID]; ok {
		return st, nil
	}
	return iface.UserLifecycleMissing, nil
}

var _ iface.UserLifecycleStateProvider = (*fakeLifecycleUsers)(nil)

// assertOnlyThreeKeys proves a FinalizationAccess value serializes to
// exactly {canFinalize, canClaimRecovery, reason} — the load-bearing check
// that the response never leaks the bound administrator's identity, since
// any leaked field (uuid/email/name/state) would show up as a fourth key.
func assertOnlyThreeKeys(t *testing.T, access FinalizationAccess) {
	t.Helper()
	data, err := json.Marshal(access)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	want := []string{"canFinalize", "canClaimRecovery", "reason"}
	if len(m) != len(want) {
		t.Fatalf("response body has %d keys, want exactly %d: %s", len(m), len(want), data)
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("missing expected key %q; body=%s", k, data)
		}
	}
}

// --- 1. bound == caller, active -> CanFinalize ---

func TestEvaluateAccess_BoundEqualsCallerActive_CanFinalize(t *testing.T) {
	users := &fakeLifecycleUsers{states: map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive}}
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{AdminUUID: "admin-1", Revision: 3}}
	svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

	access, rec, err := svc.evaluateAccess(context.Background(), "admin-1", "administrator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !access.CanFinalize {
		t.Errorf("CanFinalize = false, want true")
	}
	if access.CanClaimRecovery {
		t.Errorf("CanClaimRecovery = true, want false")
	}
	if access.Reason != "" {
		t.Errorf("Reason = %q, want empty", access.Reason)
	}
	if rec == nil || rec.AdminUUID != "admin-1" {
		t.Errorf("expected the coordinator record to be returned, got %+v", rec)
	}
	if store.mutatorCalls != 0 {
		t.Errorf("evaluateAccess must not mutate the store; recorded %d mutator calls", store.mutatorCalls)
	}
}

// --- 2. bound != caller, active -> bound_to_another_admin, no identity leak ---

func TestEvaluateAccess_BoundDiffersFromCallerActive_BoundToAnotherAdmin(t *testing.T) {
	users := &fakeLifecycleUsers{states: map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive}}
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{AdminUUID: "admin-1"}}
	svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

	access, _, err := svc.evaluateAccess(context.Background(), "someone-else", "administrator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access.CanFinalize {
		t.Errorf("CanFinalize = true, want false")
	}
	if access.CanClaimRecovery {
		t.Errorf("CanClaimRecovery = true, want false")
	}
	if access.Reason != reasonBoundToAnotherAdmin {
		t.Errorf("Reason = %q, want %q", access.Reason, reasonBoundToAnotherAdmin)
	}
	if store.mutatorCalls != 0 {
		t.Errorf("evaluateAccess must not mutate the store; recorded %d mutator calls", store.mutatorCalls)
	}
	assertOnlyThreeKeys(t, access)
}

// --- 3. empty/missing/deleted/inactive binding + active super_admin -> CanClaimRecovery ---

func TestEvaluateAccess_UnusableBinding_ActiveSuperAdmin_CanClaimRecovery(t *testing.T) {
	cases := []struct {
		name       string
		rec        *systeminit.FinalizationRecord
		boundState iface.UserLifecycleState
	}{
		{name: "empty binding, nil record", rec: nil},
		{name: "empty binding, empty AdminUUID", rec: &systeminit.FinalizationRecord{AdminUUID: ""}},
		{name: "missing bound admin", rec: &systeminit.FinalizationRecord{AdminUUID: "ghost"}, boundState: iface.UserLifecycleMissing},
		{name: "deleted bound admin", rec: &systeminit.FinalizationRecord{AdminUUID: "gone"}, boundState: iface.UserLifecycleDeleted},
		{name: "inactive bound admin", rec: &systeminit.FinalizationRecord{AdminUUID: "sleepy"}, boundState: iface.UserLifecycleInactive},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			users := &fakeLifecycleUsers{states: map[string]iface.UserLifecycleState{"super-1": iface.UserLifecycleActive}}
			if c.rec != nil && c.rec.AdminUUID != "" {
				users.states[c.rec.AdminUUID] = c.boundState
			}
			store := &fakeFinalizationStore{rec: c.rec}
			svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

			access, _, err := svc.evaluateAccess(context.Background(), "super-1", "super_admin")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !access.CanClaimRecovery {
				t.Errorf("CanClaimRecovery = false, want true (reason=%q)", access.Reason)
			}
			if access.CanFinalize {
				t.Errorf("CanFinalize = true, want false")
			}
			if access.Reason != "" {
				t.Errorf("Reason = %q, want empty", access.Reason)
			}
			if store.mutatorCalls != 0 {
				t.Errorf("evaluateAccess must not mutate the store; recorded %d mutator calls", store.mutatorCalls)
			}
		})
	}
}

// --- 4. same unusable-binding states + a lower system role -> recovery_requires_super_admin ---

func TestEvaluateAccess_UnusableBinding_LowerRole_RecoveryRequiresSuperAdmin(t *testing.T) {
	cases := []struct {
		name       string
		rec        *systeminit.FinalizationRecord
		boundState iface.UserLifecycleState
	}{
		{name: "empty binding", rec: nil},
		{name: "missing bound admin", rec: &systeminit.FinalizationRecord{AdminUUID: "ghost"}, boundState: iface.UserLifecycleMissing},
		{name: "deleted bound admin", rec: &systeminit.FinalizationRecord{AdminUUID: "gone"}, boundState: iface.UserLifecycleDeleted},
		{name: "inactive bound admin", rec: &systeminit.FinalizationRecord{AdminUUID: "sleepy"}, boundState: iface.UserLifecycleInactive},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			users := &fakeLifecycleUsers{states: map[string]iface.UserLifecycleState{}}
			if c.rec != nil && c.rec.AdminUUID != "" {
				users.states[c.rec.AdminUUID] = c.boundState
			}
			store := &fakeFinalizationStore{rec: c.rec}
			svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

			access, _, err := svc.evaluateAccess(context.Background(), "caller-1", "administrator")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if access.CanClaimRecovery {
				t.Errorf("CanClaimRecovery = true, want false")
			}
			if access.CanFinalize {
				t.Errorf("CanFinalize = true, want false")
			}
			if access.Reason != reasonRecoveryRequiresSuperAdmin {
				t.Errorf("Reason = %q, want %q", access.Reason, reasonRecoveryRequiresSuperAdmin)
			}
			for _, uuid := range users.calls {
				if uuid == "caller-1" {
					t.Errorf("evaluateAccess looked up the caller's own lifecycle despite a non-super_admin role; calls=%v", users.calls)
				}
			}
		})
	}
}

// --- 5. lifecycle lookup error -> propagated, recovery never granted ---

func TestEvaluateAccess_LifecycleLookupError_NeverGrantsRecovery(t *testing.T) {
	lookupErr := errors.New("mongo: connection refused")

	t.Run("bound admin lookup error", func(t *testing.T) {
		users := &fakeLifecycleUsers{err: lookupErr}
		store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{AdminUUID: "admin-1"}}
		svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

		access, rec, err := svc.evaluateAccess(context.Background(), "admin-1", "super_admin")
		if err == nil {
			t.Fatalf("expected error, got nil (access=%+v)", access)
		}
		if access != (FinalizationAccess{}) {
			t.Errorf("expected zero-value access on error, got %+v", access)
		}
		if rec != nil {
			t.Errorf("expected nil record on error, got %+v", rec)
		}
		if access.CanClaimRecovery {
			t.Errorf("CanClaimRecovery must never be true on a lookup error")
		}
	})

	t.Run("caller lifecycle lookup error during recovery check", func(t *testing.T) {
		users := &fakeLifecycleUsers{err: lookupErr}
		store := &fakeFinalizationStore{rec: nil} // empty binding
		svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

		access, _, err := svc.evaluateAccess(context.Background(), "super-1", "super_admin")
		if err == nil {
			t.Fatalf("expected error, got nil (access=%+v)", access)
		}
		if access.CanClaimRecovery {
			t.Errorf("CanClaimRecovery must never be true on a lookup error")
		}
	})

	t.Run("coordinator read error", func(t *testing.T) {
		users := &fakeLifecycleUsers{}
		store := &fakeFinalizationStore{getErr: lookupErr}
		svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

		access, _, err := svc.evaluateAccess(context.Background(), "super-1", "super_admin")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if access.CanClaimRecovery {
			t.Errorf("CanClaimRecovery must never be true on a lookup error")
		}
	})
}

// --- 6. the probe performs zero coordinator mutations ---

func TestEvaluateAccess_NeverMutatesStore(t *testing.T) {
	scenarios := []struct {
		name       string
		rec        *systeminit.FinalizationRecord
		callerUUID string
		callerRole string
		states     map[string]iface.UserLifecycleState
	}{
		{
			name: "bound == caller, active", callerUUID: "a", callerRole: "administrator",
			rec:    &systeminit.FinalizationRecord{AdminUUID: "a"},
			states: map[string]iface.UserLifecycleState{"a": iface.UserLifecycleActive},
		},
		{
			name: "bound != caller, active", callerUUID: "b", callerRole: "administrator",
			rec:    &systeminit.FinalizationRecord{AdminUUID: "a"},
			states: map[string]iface.UserLifecycleState{"a": iface.UserLifecycleActive},
		},
		{
			name: "empty binding, super_admin caller", callerUUID: "s", callerRole: "super_admin",
			rec:    nil,
			states: map[string]iface.UserLifecycleState{"s": iface.UserLifecycleActive},
		},
		{
			name: "empty binding, lower-role caller", callerUUID: "c", callerRole: "administrator",
			rec: nil,
		},
	}
	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			users := &fakeLifecycleUsers{states: sc.states}
			store := &fakeFinalizationStore{rec: sc.rec}
			svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())

			if _, _, err := svc.evaluateAccess(context.Background(), sc.callerUUID, sc.callerRole); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if store.mutatorCalls != 0 {
				t.Errorf("evaluateAccess must be read-only; recorded %d mutator calls", store.mutatorCalls)
			}
		})
	}
}

// --- 7. JSON round-trip: no identity fields, for every reachable shape ---

func TestFinalizationAccess_JSONRoundTrip_NoIdentityFields(t *testing.T) {
	cases := []FinalizationAccess{
		{CanFinalize: true},
		{Reason: reasonBoundToAnotherAdmin},
		{CanClaimRecovery: true},
		{Reason: reasonRecoveryRequiresSuperAdmin},
	}
	for _, access := range cases {
		assertOnlyThreeKeys(t, access)
	}
}

// --- Handler-level: phase gating, 503 mapping, success shape ---

func TestFinalizationAccessHandler_LifecycleLookupError_Returns503(t *testing.T) {
	users := &fakeLifecycleUsers{count: 1, err: errors.New("mongo down")}
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{AdminUUID: "admin-1"}}
	svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	ctx := testkit.NewIdentity("admin-1", "admin@example.com", "administrator").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err == nil {
		t.Fatalf("expected error, got response %+v", resp)
	}

	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error does not implement huma.StatusError: %v", err)
	}
	if se.GetStatus() != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", se.GetStatus(), http.StatusServiceUnavailable)
	}

	var ee *errcode.Error
	if !errors.As(err, &ee) {
		t.Fatalf("error does not carry an *errcode.Error: %v", err)
	}
	if ee.Code != errcode.SetupFinalizerStateUnavailable {
		t.Errorf("code = %q, want %q", ee.Code, errcode.SetupFinalizerStateUnavailable)
	}
	if ee.Detail == "mongo down" {
		t.Errorf("detail leaks the raw lookup error: %q", ee.Detail)
	}
}

func TestFinalizationAccessHandler_PhaseComplete_Returns409_NoLifecycleLookup(t *testing.T) {
	completedAt := time.Now().UTC()
	users := &fakeLifecycleUsers{count: 1}
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{AdminUUID: "admin-1", CompletedAt: &completedAt}}
	svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	ctx := testkit.NewIdentity("admin-1", "admin@example.com", "administrator").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err == nil {
		t.Fatalf("expected error, got response %+v", resp)
	}

	var ee *errcode.Error
	if !errors.As(err, &ee) {
		t.Fatalf("error does not carry an *errcode.Error: %v", err)
	}
	if ee.Status != http.StatusConflict {
		t.Errorf("status = %d, want %d", ee.Status, http.StatusConflict)
	}
	if ee.Code != errcode.SetupAlreadyCompleted {
		t.Errorf("code = %q, want %q", ee.Code, errcode.SetupAlreadyCompleted)
	}
	if ee.Detail != "Setup is already complete." {
		t.Errorf("detail = %q, want fixed string", ee.Detail)
	}
	// Once setup is complete, the bound admin's lifecycle no longer matters
	// — evaluateAccess must never run, so no lookup should have occurred.
	if len(users.calls) != 0 {
		t.Errorf("expected zero lifecycle lookups once phase is complete, got %d: %v", len(users.calls), users.calls)
	}
}

func TestFinalizationAccessHandler_Success_SetsCacheControl(t *testing.T) {
	users := &fakeLifecycleUsers{count: 1, states: map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive}}
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{AdminUUID: "admin-1"}}
	svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	ctx := testkit.NewIdentity("admin-1", "admin@example.com", "administrator").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CacheControl != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", resp.CacheControl, "no-store")
	}
	if !resp.Body.CanFinalize {
		t.Errorf("expected CanFinalize=true, got %+v", resp.Body)
	}
}

func TestFinalizationAccessHandler_NoCallerIdentity_Returns401(t *testing.T) {
	svc := NewService(&fakeLifecycleUsers{count: 1}, &stubAdmin{}, &fakeFinalizationStore{}, nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	_, err := h.FinalizationAccess(context.Background(), &struct{}{})
	if err == nil {
		t.Fatalf("expected error")
	}
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error does not implement huma.StatusError: %v", err)
	}
	if se.GetStatus() != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", se.GetStatus(), http.StatusUnauthorized)
	}
}
