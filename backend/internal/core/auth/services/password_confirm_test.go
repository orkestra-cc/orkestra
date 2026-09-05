package services

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Tests for PasswordAuthService.ConfirmPassword — the reauth bypass used
// by RequireStepUp's password_confirm_required path. Reuses the in-memory
// gatesEnv fixture so the wiring of UserService / PasswordService / JWT
// matches the production code paths.

func TestConfirmPassword_HappyPath(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("alice@example.com", "correct-horse-battery")

	res, err := env.auth.ConfirmPassword(context.Background(), u.UUID, "correct-horse-battery", []string{"pwd"}, "203.0.113.10", "session-confirm", "device-confirm")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if res == nil || res.AccessToken == "" {
		t.Fatal("expected access token in result")
	}
	if res.TokenType != "Bearer" {
		t.Errorf("token type = %q, want Bearer", res.TokenType)
	}
	// Parse the freshly-minted token and assert it carries amr += "reauth"
	// plus a non-zero last_otp_at. ValidateAccessToken runs the same path
	// the middleware would.
	claims, err := env.jwt.ValidateAccessToken(res.AccessToken)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if claims.LastOTPAt == 0 {
		t.Error("LastOTPAt must be stamped on a reauth token")
	}
	seen := map[string]bool{}
	for _, v := range claims.AMR {
		seen[v] = true
	}
	if !seen["pwd"] || !seen["reauth"] {
		t.Errorf("amr = %v, want [pwd reauth]", claims.AMR)
	}
}

func TestConfirmPassword_WrongPasswordReturnsInvalidCreds(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("alice@example.com", "correct-horse-battery")

	_, err := env.auth.ConfirmPassword(context.Background(), u.UUID, "wrong-password", []string{"pwd"}, "203.0.113.10", "session-confirm", "device-confirm")
	if !stderrors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestConfirmPassword_NoPasswordHashIsUnavailable(t *testing.T) {
	// Pure-OAuth user (no password hash) cannot reconfirm via password.
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := activeUser("oauth-only@example.com", "")
	env.users.seed(u)

	_, err := env.auth.ConfirmPassword(context.Background(), u.UUID, "anything", nil, "", "session-confirm", "device-confirm")
	if !stderrors.Is(err, ErrPasswordConfirmUnavailable) {
		t.Fatalf("err = %v, want ErrPasswordConfirmUnavailable", err)
	}
}

func TestConfirmPassword_TOTPEnrolledRefuses(t *testing.T) {
	// A user with TOTP enrolled must use the MFA path, not password
	// reconfirm — the middleware should never route them here, but a
	// crafted direct call must still be refused.
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	repo := newFakeFactorRepo()
	env.auth.mfaFactorRepo = repo
	u := env.hashedUser("with-totp@example.com", "correct-horse-battery")
	_ = repo.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:     "factor-1",
		UserUUID: u.UUID,
		Type:     models.MFAFactorTOTP,
	})

	_, err := env.auth.ConfirmPassword(context.Background(), u.UUID, "correct-horse-battery", []string{"pwd"}, "", "session-confirm", "device-confirm")
	if !stderrors.Is(err, ErrPasswordConfirmUnavailable) {
		t.Fatalf("err = %v, want ErrPasswordConfirmUnavailable", err)
	}
}

func TestConfirmPassword_WebAuthnEnrolledRefuses(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	repo := newFakeFactorRepo()
	env.auth.mfaFactorRepo = repo
	u := env.hashedUser("with-passkey@example.com", "correct-horse-battery")
	_ = repo.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:     "factor-wa",
		UserUUID: u.UUID,
		Type:     models.MFAFactorWebAuthn,
		WebAuthnCredentials: []models.WebAuthnCredential{
			{CredentialID: []byte("cred-1")},
		},
	})

	_, err := env.auth.ConfirmPassword(context.Background(), u.UUID, "correct-horse-battery", []string{"pwd"}, "", "session-confirm", "device-confirm")
	if !stderrors.Is(err, ErrPasswordConfirmUnavailable) {
		t.Fatalf("err = %v, want ErrPasswordConfirmUnavailable", err)
	}
}

func TestConfirmPassword_EmptyArgsRejected(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	if _, err := env.auth.ConfirmPassword(context.Background(), "", "x", nil, "", "session-confirm", "device-confirm"); !stderrors.Is(err, ErrInvalidCredentials) {
		t.Errorf("empty userUUID: err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := env.auth.ConfirmPassword(context.Background(), "u-1", "", nil, "", "session-confirm", "device-confirm"); !stderrors.Is(err, ErrInvalidCredentials) {
		t.Errorf("empty password: err = %v, want ErrInvalidCredentials", err)
	}
}

func TestMergeAMRWithReauth_AppendsAndDedupes(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{"pwd", "reauth"}},
		{"pwd-only", []string{"pwd"}, []string{"pwd", "reauth"}},
		{"oauth", []string{"oauth"}, []string{"oauth", "reauth"}},
		{"already-has-reauth", []string{"pwd", "reauth"}, []string{"pwd", "reauth"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mergeAMRWithReauth(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("len = %d, want %d (got %v want %v)", len(got), len(c.want), got, c.want)
			}
			for i, v := range got {
				if v != c.want[i] {
					t.Errorf("idx %d: got %q, want %q (got %v want %v)", i, v, c.want[i], got, c.want)
				}
			}
		})
	}
}

// PR 3 §4.6: a password that is not an accepted credential cannot be a
// proof of presence either. Same 409 branch as "no password hash";
// break-glass is ignored; an unreadable policy is a 503, not a guess.
func TestConfirmPassword_MethodDisabledIsUnavailable(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
	env.policy.SetOperatorBreakGlass(true) // must be invisible here
	u := env.hashedUser("op@example.com", "correct horse battery staple")
	_, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct horse battery staple",
		[]string{"pwd"}, &models.DeviceInfo{}, &models.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()})
	if !stderrors.Is(err, ErrPasswordConfirmUnavailable) {
		t.Fatalf("want ErrPasswordConfirmUnavailable, got %v", err)
	}
}

func TestConfirmPassword_PolicyOutageIs503Shaped(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("op@example.com", "correct horse battery staple")
	env.policy.cs = &stubReader{rawErr: stderrors.New("mongo down")}
	_, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct horse battery staple",
		[]string{"pwd"}, &models.DeviceInfo{}, &models.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()})
	if !stderrors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
	}
}

// Spec D11: a password reconfirm PROVES A FACTOR, it does not create a
// session, so the stepped-up token it mints must carry the session's
// original auth_time — never now().
//
// This asserts the property at a real mint rather than at the seam that
// feeds it. handlers.currentSessionSecurity is covered separately, but a
// helper returning the right value proves nothing about a consumer that
// overwrites it: `security.AuthTime = time.Now().Unix()` added to any of
// the three step-up mints (this one, MFAHandler.Verify,
// WebAuthnHandler.VerifyFinish) would ship green against the seam test
// alone — and, once the first-factor enrolment gate lands, would hand a
// no-factor user fresh proof of presence for an enrolment they never
// interactively authenticated for.
//
// The two handler mints take their context from that same seam and pass
// it through unmodified, so this is the funnel-level assertion for all
// three.
func TestConfirmPassword_CarriesTheSessionsAuthTimeUnchanged(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("alice@example.com", "correct-horse-battery")
	origin := time.Now().Add(-2 * time.Hour).Unix()

	res, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct-horse-battery",
		[]string{"pwd"}, &models.DeviceInfo{DeviceID: "device-confirm"},
		&models.SecurityContext{SessionID: "session-confirm", Timestamp: time.Now(), AuthTime: origin})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	claims, err := env.jwt.ValidateAccessToken(res.AccessToken)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if claims.AuthTime != origin {
		t.Fatalf("AuthTime = %d, want the session's original %d — a proven factor is not a new session", claims.AuthTime, origin)
	}
	// And the proof itself IS fresh: last_otp_at moves, auth_time does not.
	// Without this the test would also pass if the mint dropped both.
	if claims.LastOTPAt == 0 {
		t.Error("LastOTPAt must still be stamped — the reconfirm is a fresh proof")
	}
}

// M-1's second half (spec §4.3 D19). The enrolled-factor refusal above
// catches a user who HAS a stronger factor. It cannot catch the user this
// obligation exists for: one whose role REQUIRES a second factor and who
// has not enrolled one yet. Before D19 that user could mint a `reauth`
// token and satisfy every freshness gate with a password alone — exactly
// what the obligation is there to prevent.
//
// gateTenantProvider returns no memberships, so this case is the
// system-role half of the decision; the membership half is the next test.
func TestConfirmPassword_RefusesAnMFAObligatedUser(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("admin@example.com", "correct-horse-battery")
	u.Role = SystemRoleAdministrator // the fake stores the pointer

	_, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct-horse-battery",
		[]string{"pwd"}, &models.DeviceInfo{}, &models.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()})
	if !stderrors.Is(err, ErrPasswordConfirmEnrollmentRequired) {
		t.Fatalf("err = %v, want ErrPasswordConfirmEnrollmentRequired", err)
	}
}

// obligatedByMembership is the tenant provider for the half of the
// decision the system role cannot make: a plain `operator` who holds
// org_owner in some tenant is MFA-obligated through that membership.
// D19 says memberships are resolved the way completeLogin resolves them
// — through loadMembershipsAsAuthModel — and this is the test that fails
// if the refusal reads the system role alone.
type obligatedByMembership struct{ gateTenantProvider }

func (obligatedByMembership) ListUserMemberships(context.Context, string) ([]iface.TenantMembership, error) {
	return []iface.TenantMembership{{
		TenantUUID: "tenant-1",
		TenantKind: "internal",
		Roles:      []string{OrgRoleOwner},
	}}, nil
}

func TestConfirmPassword_RefusesAUserObligatedByAMembership(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	env.auth.tenantProvider = obligatedByMembership{}
	u := env.hashedUser("owner@example.com", "correct-horse-battery")
	if u.Role == SystemRoleAdministrator {
		t.Fatal("fixture drift: this case must be obligated by the MEMBERSHIP, not the system role")
	}

	_, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct-horse-battery",
		[]string{"pwd"}, &models.DeviceInfo{}, &models.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()})
	if !stderrors.Is(err, ErrPasswordConfirmEnrollmentRequired) {
		t.Fatalf("err = %v, want ErrPasswordConfirmEnrollmentRequired", err)
	}
}

// The other side of D19: the endpoint's whole population is non-obligated
// password users, and they must still be served. A refusal that fired for
// everyone would close the only step-up path those users have.
//
// ⚠️ BOUND, not a TDD driver — green before D19 existed. It fails only if
// the refusal is later widened past the obligation. Ruling R17.
func TestConfirmPassword_StillServesANonObligatedUser(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("plain@example.com", "correct-horse-battery")

	res, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct-horse-battery",
		[]string{"pwd"}, &models.DeviceInfo{}, &models.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("a non-obligated password user must still be served: %v", err)
	}
	if res == nil || res.AccessToken == "" {
		t.Fatal("expected a stepped-up access token")
	}
}

// The master switch is part of "obliged": MFARequired reads mfaEnabled
// first, so on an install with MFA off nobody is obligated and the
// reconfirm keeps working for an administrator too. Without this, turning
// MFA off would lock every admin out of step-up on a no-factor install.
//
// ⚠️ BOUND, not a TDD driver — green before D19 existed, and the reason it
// is here is that the obvious wrong implementation (RoleRequiresMFA
// directly, bypassing the policy) would turn it red. Ruling R17.
func TestConfirmPassword_MFADisabledLeavesTheAdminServed(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{"mfaEnabled": "false"}, nil)
	u := env.hashedUser("admin@example.com", "correct-horse-battery")
	u.Role = SystemRoleAdministrator

	if _, err := env.auth.ConfirmPasswordWithSecurity(context.Background(), u.UUID, "correct-horse-battery",
		[]string{"pwd"}, &models.DeviceInfo{}, &models.SecurityContext{SessionID: "sid-1", Timestamp: time.Now()}); err != nil {
		t.Fatalf("mfaEnabled=false means nobody is obligated: %v", err)
	}
}
