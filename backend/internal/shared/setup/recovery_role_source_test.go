package setup

// D28, setup half. The finalizer recovery gate compares the caller's
// system role as the DATABASE holds it, never the `srole` JWT claim.
//
// The claim is minted at login and can be a whole access-token lifetime
// stale. This gate is the one that hands an operator the ability to seize
// an in-progress setup binding, so a demoted super_admin holding a
// still-valid token must not be able to claim recovery, and a freshly
// promoted one must not be blocked by a token that predates the promotion.
//
// Every fixture below makes the claim and the database row DISAGREE, so a
// revert to `ctxauth.GetSystemRole` flips the assertion.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// emptyBinding is the coordinator state that routes every caller through
// the recovery-eligibility check — the branch the role decides.
func emptyBinding() *fakeFinalizationStore { return &fakeFinalizationStore{rec: nil} }

func TestFinalizationAccess_StaleSuperAdminClaimCannotClaimRecovery(t *testing.T) {
	users := (&fakeLifecycleUsers{
		count:  1,
		states: map[string]iface.UserLifecycleState{"demoted-1": iface.UserLifecycleActive},
	}).withRoles("demoted-1", "administrator") // the truth: demoted
	svc := NewService(users, &stubAdmin{}, emptyBinding(), nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	// The token still says super_admin.
	ctx := testkit.NewIdentity("demoted-1", "demoted@example.com", "super_admin").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body.CanClaimRecovery {
		t.Error("a stale super_admin claim must not unlock recovery once the database says otherwise")
	}
	if resp.Body.Reason != reasonRecoveryRequiresSuperAdmin {
		t.Errorf("Reason = %q, want %q", resp.Body.Reason, reasonRecoveryRequiresSuperAdmin)
	}
	// The privacy short-circuit still holds: a caller the database does
	// not consider a super_admin never has their lifecycle read.
	for _, uuid := range users.calls {
		if uuid == "demoted-1" {
			t.Errorf("the caller's own lifecycle was looked up despite a non-super_admin database role; calls=%v", users.calls)
		}
	}
}

func TestFinalizationAccess_FreshlyPromotedSuperAdminMayClaimRecovery(t *testing.T) {
	users := (&fakeLifecycleUsers{
		count:  1,
		states: map[string]iface.UserLifecycleState{"promoted-1": iface.UserLifecycleActive},
	}).withRoles("promoted-1", "super_admin") // the truth: promoted
	svc := NewService(users, &stubAdmin{}, emptyBinding(), nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	// The token predates the promotion.
	ctx := testkit.NewIdentity("promoted-1", "promoted@example.com", "administrator").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Body.CanClaimRecovery {
		t.Errorf("the database role must grant recovery even when the claim predates it (reason=%q)", resp.Body.Reason)
	}
}

// A role-lookup failure is fail-closed, exactly like the lifecycle lookup:
// it leaves through the error return and surfaces as 503, never as an
// authorization outcome and never as a fallback to the claim.
func TestFinalizationAccess_RoleLookupFailureIs503NeverAClaimFallback(t *testing.T) {
	users := &fakeLifecycleUsers{count: 1, roleErr: errors.New("mongo: connection refused")}
	svc := NewService(users, &stubAdmin{}, emptyBinding(), nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	ctx := testkit.NewIdentity("super-1", "super@example.com", "super_admin").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err == nil {
		t.Fatalf("expected an error, got %+v", resp)
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
}

// A caller whose row is gone is a FACT, not a failure — the same way the
// lifecycle provider reports a missing user as UserLifecycleMissing. It
// refuses recovery cleanly instead of reporting an outage.
func TestFinalizationAccess_MissingCallerRowRefusesRecoveryWithoutA503(t *testing.T) {
	users := &fakeLifecycleUsers{count: 1, roleErr: iface.ErrUserNotFound}
	svc := NewService(users, &stubAdmin{}, emptyBinding(), nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	ctx := testkit.NewIdentity("ghost-1", "ghost@example.com", "super_admin").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body.CanClaimRecovery {
		t.Error("a caller with no database row must not claim recovery")
	}
	if resp.Body.Reason != reasonRecoveryRequiresSuperAdmin {
		t.Errorf("Reason = %q, want %q", resp.Body.Reason, reasonRecoveryRequiresSuperAdmin)
	}
}

// The bound-administrator path never needs the caller's role at all, so it
// must not pay for the lookup — and a database that cannot answer it must
// not break a finalize the binding alone authorizes.
func TestFinalizationAccess_BoundCallerNeedsNoRoleLookup(t *testing.T) {
	users := &fakeLifecycleUsers{
		count:   1,
		states:  map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
		roleErr: errors.New("mongo: connection refused"),
	}
	store := &fakeFinalizationStore{rec: &systeminit.FinalizationRecord{AdminUUID: "admin-1"}}
	svc := NewService(users, &stubAdmin{}, store, nil, nil, nil, discardLogger())
	h := NewHandler(svc, config.CookieConfig{})

	ctx := testkit.NewIdentity("admin-1", "admin@example.com", "administrator").ContextFor(context.Background(), "")
	resp, err := h.FinalizationAccess(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Body.CanFinalize {
		t.Errorf("the bound administrator must still be able to finalize, got %+v", resp.Body)
	}
	if len(users.roleCalls) != 0 {
		t.Errorf("the bound-caller path must not read the caller's role; roleCalls=%v", users.roleCalls)
	}
}

// The finalize POST shares the same seam, so it inherits the same rule.
func TestFinalize_StaleSuperAdminClaimCannotSeizeAnEmptyBinding(t *testing.T) {
	fx := newSagaFixture(nil, map[string]iface.UserLifecycleState{"demoted-1": iface.UserLifecycleActive})
	fx.users.withRoles("demoted-1", "administrator")

	// The context still carries the pre-demotion super_admin claim, so a
	// regression that reads `srole` here would seize the binding.
	ctx := testkit.NewIdentity("demoted-1", "demoted@example.com", "super_admin").ContextFor(context.Background(), "")
	_, err := fx.svc.Finalize(ctx, "demoted-1", testInput(true))
	if !errors.Is(err, ErrRecoveryRequiresSuperAdmin) {
		t.Fatalf("Finalize = %v, want ErrRecoveryRequiresSuperAdmin", err)
	}
	if got := fx.log.only("store.ClaimRecovery"); len(got) != 0 {
		t.Errorf("a stale claim must not seize the binding; recovery claims = %v", got)
	}
}
