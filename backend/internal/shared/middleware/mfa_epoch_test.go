package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- Doubles ----------------------------------------------------------------

// epochStore is the MFAEpochLookup double. It counts reads (so the
// no-lookup and memoisation properties are observable), answers per-tier so
// a lookup that ignored the audience is visible, and can be mutated
// mid-test to model a factor being removed under a live session.
type epochStore struct {
	operator map[string]int
	client   map[string]int
	err      error
	reads    atomic.Int64
	// lastAudience records what the middleware passed, so a test can prove
	// the tier argument reaches the lookup at all.
	lastAudience atomic.Value
}

func (e *epochStore) lookup() MFAEpochLookup {
	return func(_ context.Context, audience, userUUID string) (int, error) {
		e.reads.Add(1)
		e.lastAudience.Store(audience)
		if e.err != nil {
			return 0, e.err
		}
		table := e.operator
		if audience == "client" {
			table = e.client
		}
		epoch, ok := table[userUUID]
		if !ok {
			// Absent user is an error, never epoch 0 — 0 would match a
			// token with no mfae claim and turn a failed lookup into a pass.
			return 0, iface.ErrUserNotFound
		}
		return epoch, nil
	}
}

func (e *epochStore) reads64() int64 { return e.reads.Load() }

// operatorEpochs is the one-line constructor for the common case: a single
// operator-tier user at a known epoch.
func operatorEpochs(userUUID string, epoch int) *epochStore {
	return &epochStore{operator: map[string]int{userUUID: epoch}}
}

func erroringEpochs() *epochStore {
	return &epochStore{err: errors.New("mongo down")}
}

// --- Harness ----------------------------------------------------------------

// runThroughPerimeter drives one request through setUserContext — the real
// place the MFA authority is resolved — and then through the supplied
// gate(s). Nothing here stubs the stash: the tests exercise exactly the
// path RequireAuth takes in production.
//
// The downstream handler writes 204, not 200: a gate that neither called
// next nor wrote anything would leave the recorder's zero value behind and
// satisfy a 200 assertion.
func runThroughPerimeter(t *testing.T, m *AuthMiddleware, claims *authModels.JWTClaims, gates ...func(http.Handler) http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/anything", nil)
	rec := httptest.NewRecorder()

	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for i := len(gates) - 1; i >= 0; i-- {
		h = gates[i](h)
	}
	m.setUserContext(rec, req, claims, h)
	return rec
}

// newEpochMiddleware wires the minimal middleware plus the epoch lookup.
// The step-up policy is wired ON deliberately: RequireMFA short-circuits
// to a pass-through when the mfaEnabled master switch is off, and the
// SHIPPED DEFAULT of that switch is false (auth/module.go). A RequireMFA
// test that left the policy nil would exercise a branch production reaches
// only when an operator has turned MFA on — and one that wired a policy
// reporting "off" would assert nothing at all.
func newEpochMiddleware(t *testing.T, store *epochStore) *AuthMiddleware {
	t.Helper()
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	m.SetStepUpPolicy(&fakeStepUpPolicy{})
	if store != nil {
		m.SetMFAEpochLookup(store.lookup())
	}
	return m
}

func runMFAGate(t *testing.T, claims *authModels.JWTClaims, store *epochStore) *httptest.ResponseRecorder {
	t.Helper()
	m := newEpochMiddleware(t, store)
	return runThroughPerimeter(t, m, claims, m.RequireMFA())
}

func runStepUpGate(t *testing.T, claims *authModels.JWTClaims, store *epochStore, maxAge time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	m := newEpochMiddleware(t, store)
	return runThroughPerimeter(t, m, claims, m.RequireStepUp(maxAge))
}

func runEnrolmentGateWithEpoch(t *testing.T, claims *authModels.JWTClaims, store *epochStore, hasFactor bool, maxAge time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	m := newEpochMiddleware(t, store)
	m.SetMFAEnrollmentLookup(enrolmentLookupFactor(hasFactor))
	return runThroughPerimeter(t, m, claims, m.RequireEnrolmentProof(maxAge))
}

// perimeterContext returns the context setUserContext built, so a test can
// ask the package functions Cedar calls (IsMFAEnrolled, GetAMR) what they
// see on a request that passed through no MFA-aware gate at all.
func perimeterContext(t *testing.T, claims *authModels.JWTClaims, store *epochStore) context.Context {
	t.Helper()
	m := newEpochMiddleware(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	rec := httptest.NewRecorder()
	var seen context.Context
	m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Context()
	}))
	if seen == nil {
		t.Fatalf("perimeter refused the request (status %d); the harness needs a passing one", rec.Code)
	}
	return seen
}

// personalTenantMiddleware wires the shape tryImpersonationBypass needs: an
// authz provider that grants system.tenants.admin and a self-serve external
// tenant, which is the canonical "personal tenant" the bypass gates on MFA.
func personalTenantMiddleware(t *testing.T, store *epochStore) *AuthMiddleware {
	t.Helper()
	tenants := &fakeTenantProvider{tenants: map[string]*iface.Tenant{
		"tenant-P": {
			UUID:          "tenant-P",
			Kind:          iface.TenantKindExternal,
			Name:          "alice",
			Slug:          "alice",
			SignupChannel: iface.SignupChannelSelfServe,
		},
	}}
	m := newTestMiddleware(&fakeAuthz{allow: true}, tenants, nil)
	if store != nil {
		m.SetMFAEpochLookup(store.lookup())
	}
	return m
}

// impersonationAllowed reports whether the personal-tenant bypass admitted
// the caller.
func impersonationAllowed(t *testing.T, claims *authModels.JWTClaims, store *epochStore) bool {
	t.Helper()
	m := personalTenantMiddleware(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	req.Header.Set(TenantIDHeader, "tenant-P")
	rec := httptest.NewRecorder()
	called := false
	m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	return called
}

func staleClaims(userUUID string) *authModels.JWTClaims {
	return &authModels.JWTClaims{
		UserUUID:  userUUID,
		Audience:  "operator",
		AMR:       []string{"pwd", "otp"},
		LastOTPAt: time.Now().Unix(),
		MFAEpoch:  1,
	}
}

// --- D16: the epoch is enforced ---------------------------------------------

// The epoch is the whole point of D16: a token minted under a factor the
// user has since removed must lose its MFA authority IMMEDIATELY, in the
// session it was minted for, without a refresh.
func TestMFAEpoch_StaleTokenLosesEveryGate(t *testing.T) {
	users := operatorEpochs("u-1", 2) // the user has moved on

	t.Run("RequireMFA", func(t *testing.T) {
		// sendMFARequired's code is "step_up_required" (title "mfa
		// required") — the frontend branches on the code alone and both
		// gates drive the same modal. Pinned byte-for-byte in
		// coded_error_golden_test.go; there is no "mfa_required" code.
		assertCodedError(t, runMFAGate(t, staleClaims("u-1"), users), http.StatusUnauthorized, "step_up_required")
	})
	t.Run("RequireStepUp", func(t *testing.T) {
		assertCodedError(t, runStepUpGate(t, staleClaims("u-1"), users, 5*time.Minute), http.StatusUnauthorized, "step_up_required")
	})
	t.Run("RequireEnrolmentProof", func(t *testing.T) {
		rec := runEnrolmentGateWithEpoch(t, staleClaims("u-1"), users, true /*hasFactor*/, 5*time.Minute)
		assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
	})
	t.Run("IsMFAEnrolled", func(t *testing.T) {
		ctx := perimeterContext(t, staleClaims("u-1"), users)
		if IsMFAEnrolled(ctx) {
			t.Fatal("Cedar's principal.mfa_enrolled must read false for a stale epoch")
		}
		// GetAMR keeps returning the literal claim: it is the token's own
		// record of how the session authenticated, and the Cedar principal
		// stamps it as such.
		if amr, _ := GetAMR(ctx); len(amr) != 2 {
			t.Fatalf("GetAMR = %v, want the unmodified claim", amr)
		}
	})
	t.Run("impersonation bypass", func(t *testing.T) {
		if impersonationAllowed(t, staleClaims("admin-1"), operatorEpochs("admin-1", 4)) {
			t.Fatal("a stale epoch must not satisfy the personal-tenant impersonation bypass")
		}
	})
}

func TestMFAEpoch_CurrentTokenPassesEveryGate(t *testing.T) {
	current := func() *authModels.JWTClaims {
		c := staleClaims("u-1")
		c.MFAEpoch = 2
		return c
	}
	users := operatorEpochs("u-1", 2)

	if rec := runMFAGate(t, current(), users); rec.Code != http.StatusNoContent {
		t.Fatalf("RequireMFA = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if rec := runStepUpGate(t, current(), users, 5*time.Minute); rec.Code != http.StatusNoContent {
		t.Fatalf("RequireStepUp = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if rec := runEnrolmentGateWithEpoch(t, current(), users, true, 5*time.Minute); rec.Code != http.StatusNoContent {
		t.Fatalf("RequireEnrolmentProof = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if !IsMFAEnrolled(perimeterContext(t, current(), users)) {
		t.Fatal("a current epoch must keep principal.mfa_enrolled true")
	}
	if !impersonationAllowed(t, func() *authModels.JWTClaims {
		c := current()
		c.UserUUID = "admin-1"
		return c
	}(), operatorEpochs("admin-1", 2)) {
		t.Fatal("a current epoch must still satisfy the impersonation bypass")
	}
}

// Edge case 12: every user document that predates the field reads as 0 and
// matches every pre-deploy token, so the deploy downgrades nobody.
func TestMFAEpoch_ZeroOnBothSidesMatches(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", Audience: "operator",
		AMR: []string{"pwd", "otp"}, LastOTPAt: time.Now().Unix(),
		// no MFAEpoch — a token minted before the claim shipped
	}
	if rec := runMFAGate(t, claims, operatorEpochs("u-1", 0)); rec.Code != http.StatusNoContent {
		t.Fatalf("a pre-deploy token against a pre-deploy user = %d, want 204", rec.Code)
	}
}

// The other half of edge case 12: a deployment that has not wired the
// lookup at all behaves exactly as it did before the epoch existed.
func TestMFAEpoch_UnwiredLookupKeepsMarkers(t *testing.T) {
	if rec := runMFAGate(t, staleClaims("u-1"), nil /*no lookup wired*/); rec.Code != http.StatusNoContent {
		t.Fatalf("an unwired lookup must keep the pre-epoch behaviour, got %d", rec.Code)
	}
}

// FAIL CLOSED on a lookup error: "not current" is the safe reading. A
// degraded store must never be the reason a removed factor keeps working.
func TestMFAEpoch_LookupErrorReadsAsStale(t *testing.T) {
	assertCodedError(t, runMFAGate(t, staleClaims("u-1"), erroringEpochs()), http.StatusUnauthorized, "step_up_required")
}

// A user the tier's provider does not know is the same answer as an
// outage. This is the case a tier-blind lookup produces for EVERY token of
// the other tier, which is why it must not be permissive.
func TestMFAEpoch_UnknownUserReadsAsStale(t *testing.T) {
	assertCodedError(t, runMFAGate(t, staleClaims("ghost"), operatorEpochs("u-1", 1)), http.StatusUnauthorized, "step_up_required")
}

// The audience reaches the lookup, and a client-tier token is resolved
// against the CLIENT table. The regression this guards is an outage, not a
// weakening: a middleware that resolved every UUID against operator_users
// would miss every client user, fail closed, and strip MFA authority from
// the entire client tier on every request.
func TestMFAEpoch_LookupIsTierAware(t *testing.T) {
	store := &epochStore{
		operator: map[string]int{"u-1": 7}, // same UUID, different tier, different epoch
		client:   map[string]int{"u-1": 3},
	}
	claims := staleClaims("u-1")
	claims.Audience = "client"
	claims.MFAEpoch = 3 // current for the CLIENT user, stale for the operator one

	if rec := runMFAGate(t, claims, store); rec.Code != http.StatusNoContent {
		t.Fatalf("a client token = %d, want 204 — the lookup resolved the wrong tier's table", rec.Code)
	}
	if got, _ := store.lastAudience.Load().(string); got != "client" {
		t.Fatalf("lookup saw audience %q, want \"client\"", got)
	}
}

// A token with NO MFA marker must cost no database read at all — this is
// the common request path, on every authenticated request.
func TestMFAEpoch_NoMarkersCostsNoLookup(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	store := operatorEpochs("u-1", 0)
	_ = runStepUpGate(t, claims, store, 5*time.Minute)
	if n := store.reads64(); n != 0 {
		t.Fatalf("%d user lookups for a token with no MFA markers, want 0", n)
	}
}

// The resolver is memoised per request: three gates on one route must not
// mean three reads. The claims are CURRENT so all three gates actually run
// and consult the authority — a stale token would short-circuit at the
// first gate and prove nothing.
func TestMFAEpoch_ResolverIsMemoisedPerRequest(t *testing.T) {
	claims := staleClaims("u-1")
	claims.MFAEpoch = 1
	store := operatorEpochs("u-1", 1)

	m := newEpochMiddleware(t, store)
	m.SetMFAEnrollmentLookup(enrolmentLookupFactor(true))
	rec := runThroughPerimeter(t, m, claims,
		m.RequireMFA(), m.RequireStepUp(5*time.Minute), m.RequireEnrolmentProof(5*time.Minute))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("chained gates = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if n := store.reads64(); n != 1 {
		t.Fatalf("%d lookups across 3 chained gates, want 1", n)
	}
}

// The handler-level proof of D16, with fakes rather than a live stack: the
// SAME token that passed a step-up route is refused on the very next
// request once the factor behind it is removed. No refresh, no logout, no
// revocation write — the epoch alone ends it.
func TestEpoch_SteppedUpTokenDiesOnTheNextRequestAfterRemoval(t *testing.T) {
	store := operatorEpochs("u-1", 4)
	m := newEpochMiddleware(t, store)
	token := staleClaims("u-1") // one token, reused verbatim across both calls
	token.MFAEpoch = 4

	if rec := runThroughPerimeter(t, m, token, m.RequireStepUp(5*time.Minute)); rec.Code != http.StatusNoContent {
		t.Fatalf("precondition: step-up route = %d, want 204", rec.Code)
	}

	// Removing the factor is exactly one thing on the user document.
	store.operator["u-1"] = 5

	rec := runThroughPerimeter(t, m, token, m.RequireStepUp(5*time.Minute))
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

// RequireMFA — and only RequireMFA — honours the mfaEnabled master switch,
// whose shipped default is false. With MFA globally off the gate is a
// pass-through, so a stale epoch changes nothing there. Pinned so the
// limit of D16's reach through this one gate stays deliberate: the
// step-up, enrolment, impersonation and Cedar consumers do NOT consult the
// switch and enforce the epoch regardless.
func TestMFAEpoch_MasterSwitchOffLeavesRequireMFAOpen(t *testing.T) {
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	m.SetStepUpPolicy(&fakeStepUpPolicy{mfaDisabled: true})
	m.SetMFAEpochLookup(operatorEpochs("u-1", 9).lookup())

	if rec := runThroughPerimeter(t, m, staleClaims("u-1"), m.RequireMFA()); rec.Code != http.StatusNoContent {
		t.Fatalf("master switch off = %d, want 204 pass-through", rec.Code)
	}
	// …while the step-up gate, which ignores the switch, still refuses.
	m2 := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	m2.SetStepUpPolicy(&fakeStepUpPolicy{mfaDisabled: true})
	m2.SetMFAEpochLookup(operatorEpochs("u-1", 9).lookup())
	assertCodedError(t, runThroughPerimeter(t, m2, staleClaims("u-1"), m2.RequireStepUp(5*time.Minute)),
		http.StatusUnauthorized, "step_up_required")
}

// --- M-1: the session-long gate stops accepting a password reconfirm ------

// "reauth" is a password reconfirm, not a second factor. It must satisfy
// RequireStepUp (a presence proof) and NOT RequireMFA (a second-factor
// gate) — the whole of M-1.
func TestRequireMFA_RejectsAReauthOnlyToken(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", Audience: "operator",
		AMR: []string{"pwd", "reauth"}, LastOTPAt: time.Now().Unix(),
	}
	assertCodedError(t, runMFAGate(t, claims, operatorEpochs("u-1", 0)), http.StatusUnauthorized, "step_up_required")

	// The same token still satisfies RequireStepUp — narrowing the
	// session-long predicate must not collaterally break the freshness
	// gates. (TestRequireStepUp_ReauthAMRSatisfiesGate pins this too, from
	// the pre-epoch harness; this asserts it survives the resolver.)
	if rec := runStepUpGate(t, claims, operatorEpochs("u-1", 0), 5*time.Minute); rec.Code != http.StatusNoContent {
		t.Fatalf("reauth must still satisfy RequireStepUp, got %d", rec.Code)
	}
}

// The intended collateral of M-1, stated so it cannot be discovered in
// production: an operator who satisfied step-up with a PASSWORD RECONFIRM
// can no longer impersonate a personal tenant. Only a real second factor
// opens the most sensitive surface an operator has.
func TestImpersonationBypass_ReauthNoLongerAdmitsAPersonalTenant(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "admin-1", Audience: "operator",
		SystemRole: "administrator",
		AMR:        []string{"pwd", "reauth"}, LastOTPAt: time.Now().Unix(),
	}
	if impersonationAllowed(t, claims, operatorEpochs("admin-1", 0)) {
		t.Fatal("a password reconfirm must no longer open a personal tenant (M-1)")
	}
}

// The epoch does not govern "reauth": a password is not an MFA credential,
// so removing a factor must not invalidate a reconfirm that is still fresh.
func TestMFAEpoch_DoesNotStripReauth(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", Audience: "operator",
		AMR: []string{"pwd", "reauth"}, LastOTPAt: time.Now().Unix(), MFAEpoch: 1,
	}
	rec := runStepUpGate(t, claims, operatorEpochs("u-1", 9), 5*time.Minute)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("a stale epoch must not invalidate a fresh password reconfirm, got %d", rec.Code)
	}
}

// device_trust rides the epoch even though it never satisfies a gate on its
// own: the trust was granted on the strength of a factor, so it dies with
// it. Without this the trust annotation would keep costing a lookup — and
// keep its partner marker alive — after the factor was gone.
func TestMFAEpoch_StripsDeviceTrustAlongsideItsFactor(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", Audience: "operator",
		AMR: []string{"pwd", "otp", authModels.DeviceTrustAMR}, LastOTPAt: time.Now().Unix(), MFAEpoch: 1,
	}
	ctx := perimeterContext(t, claims, operatorEpochs("u-1", 2))
	if IsMFAEnrolled(ctx) {
		t.Fatal("a stale epoch must strip otp and device_trust together")
	}
}
