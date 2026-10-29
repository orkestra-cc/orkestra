package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/authz/cedar"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// A check about a user other than the caller (a module asking whether the
// user it is about to link holds a key in the tenant) must build the Cedar
// principal from THAT user's bindings. Before the fix the caller's request
// tenant_roles were stamped: an org_owner asking about an org_viewer got
// Cedar's allow on a `.self` key the viewer does not hold, which is a
// divergence in shadow mode and a wrong grant under enforce.

func newThirdPartySvc(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	svc, repo := newTier1Service(t, staticRoleLookup("")) // no platform role
	eng, err := cedar.New("development")
	if err != nil {
		t.Fatalf("cedar engine: %v", err)
	}
	svc.cedarEngine = eng
	repo.seedRole("role-owner", "org_owner", true, []string{"*"}, "")
	repo.seedRole("role-member", "org_member", true, []string{"people.self"}, "")
	repo.seedRole("role-viewer", "org_viewer", true, []string{"people.read"}, "")
	return svc, repo
}

// callerCtx is a request by an org_owner of tenant-1 with an MFA session.
func callerCtx() context.Context {
	ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "caller")
	ctx = context.WithValue(ctx, ctxauth.KeyTenantID, "tenant-1")
	ctx = context.WithValue(ctx, ctxauth.KeyTenantRoles, []string{"org_owner"})
	ctx = ctxauth.WithTenantKind(ctx, "internal")
	return middleware.WithSessionID(ctx, "sess-caller")
}

func TestShadowEvaluate_ThirdPartyUsesTheTargetsRoles(t *testing.T) {
	svc, repo := newThirdPartySvc(t)
	repo.seedBinding("b-viewer", "target", "tenant-1", "role-viewer")

	decision, ok := svc.shadowEvaluate(callerCtx(), "target", "tenant-1", "people.self", false)
	if !ok {
		t.Fatalf("evaluation should succeed, errors: %+v", decision.Errors)
	}
	if decision.Allowed {
		t.Fatalf("an org_viewer target must not get people.self from the caller's org_owner role (matched=%q)", decision.MatchedPolicy)
	}

	// Positive control: the target's own member role permits it, through
	// the member policy rather than the caller's owner one.
	repo.seedBinding("b-member", "target", "tenant-1", "role-member")
	decision, ok = svc.shadowEvaluate(callerCtx(), "target", "tenant-1", "people.self", true)
	if !ok || !decision.Allowed || !decisionDecidedBy(decision, "tenant_roles.org_member.read_self_own") {
		t.Fatalf("member target: ok=%v allowed=%v reasons=%v", ok, decision.Allowed, decision.Reasons)
	}
	if decisionDecidedBy(decision, "tenant_roles.org_owner.all_in_tenant") {
		t.Fatalf("the caller's owner role leaked into the target's principal: %v", decision.Reasons)
	}
}

// Only catalog roles count: a tenant's custom role may carry a catalog name
// (names are unique per tenant only) and must not fire that name's permit.
func TestBoundRoleNames_CatalogRolesOnly(t *testing.T) {
	svc, repo := newThirdPartySvc(t)
	repo.seedRole("role-fake-owner", "org_owner", false, []string{"people.read"}, "tenant-1")
	repo.seedBinding("b-fake", "target", "tenant-1", "role-fake-owner")
	repo.seedBinding("b-owner-2", "target", "tenant-2", "role-owner") // another tenant

	got, err := svc.boundRoleNames(callerCtx(), "target", "tenant-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("roles = %v, %v; want none", got, err)
	}
	decision, ok := svc.shadowEvaluate(callerCtx(), "target", "tenant-1", "people.delete", false)
	if !ok || decision.Allowed {
		t.Fatalf("a custom role named org_owner fired the owner permit: ok=%v reasons=%v", ok, decision.Reasons)
	}
}

// The request roles belong to the request tenant: a check about the caller
// in another tenant takes the caller's bindings there, and keeps the session.
func TestShadowEvaluate_CallerInAnotherTenantUsesBindings(t *testing.T) {
	svc, repo := newThirdPartySvc(t)
	repo.seedBinding("b-viewer", "caller", "tenant-2", "role-viewer")
	looked := 0
	svc.SetSessionRiskLookup(func(context.Context, string) (float64, error) {
		looked++
		return 0, nil
	})
	decision, ok := svc.shadowEvaluate(callerCtx(), "caller", "tenant-2", "people.self", false)
	if !ok || decision.Allowed {
		t.Fatalf("the request tenant's org_owner leaked into tenant-2: ok=%v reasons=%v", ok, decision.Reasons)
	}
	if looked != 1 {
		t.Fatalf("a check about the caller keeps the session signals (risk lookups=%d)", looked)
	}
}

func TestShadowEvaluate_SelfCheckKeepsTheRequestRoles(t *testing.T) {
	svc, _ := newThirdPartySvc(t) // the caller has no bindings in the fake
	decision, ok := svc.shadowEvaluate(callerCtx(), "caller", "tenant-1", "people.self", true)
	if !ok || !decision.Allowed || !decisionDecidedBy(decision, "tenant_roles.org_owner.all_in_tenant") {
		t.Fatalf("self check: ok=%v allowed=%v reasons=%v", ok, decision.Allowed, decision.Reasons)
	}
}

func TestShadowEvaluate_ThirdPartyCarriesNoSessionSignals(t *testing.T) {
	svc, repo := newThirdPartySvc(t)
	repo.seedBinding("b-member", "target", "tenant-1", "role-member")
	looked := 0
	svc.SetSessionRiskLookup(func(context.Context, string) (float64, error) {
		looked++
		return 0.9, nil
	})
	if _, ok := svc.shadowEvaluate(callerCtx(), "target", "tenant-1", "people.self", true); !ok {
		t.Fatal("evaluation should succeed")
	}
	if looked != 0 {
		t.Fatalf("the caller's session risk was looked up for another user (%d calls)", looked)
	}
	if _, ok := svc.shadowEvaluate(callerCtx(), "caller", "tenant-1", "people.self", true); !ok || looked != 1 {
		t.Fatalf("self check must still read the session risk (calls=%d)", looked)
	}
}

func TestShadowEvaluate_ThirdPartyRoleLookupFailureIsNotOK(t *testing.T) {
	svc, repo := newThirdPartySvc(t)
	repo.listBindingsErr = errors.New("mongo down")
	if _, ok := svc.shadowEvaluate(callerCtx(), "target", "tenant-1", "people.self", false); ok {
		t.Fatal("a failed role lookup must not produce a load-bearing decision")
	}
}
