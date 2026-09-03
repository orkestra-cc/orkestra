package services

// Phase 11 integration tests: policy gates wired into PasswordAuthService
// (Login + Register + ChangePassword) and AuthService (OAuth callback).
// Each test exercises the actual call path — the policy reader is wired
// to a stub config service, side effects are observed on in-memory fakes.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// totpGenerateNow is a small adapter around totp.GenerateCode so the
// recovery-codes test can confirm enrollment without copying the
// production validator's algorithm choice. Returns the current 6-digit
// code for the given base32 secret.
func totpGenerateNow(secretBase32 string) (string, error) {
	return totp.GenerateCode(secretBase32, time.Now())
}

// gatesEnv bundles every dependency the password / OAuth flows need so
// tests can assemble one in two lines and reach into the field they
// care about.
type gatesEnv struct {
	t            *testing.T
	users        *gateUserFake
	refresh      *gateRefreshRepo
	sessions     *gateSessionRepo
	geo          *gateGeoResolver
	notifier     *gateNotifier
	pwd          PasswordService
	jwt          JWTService
	policy       *AuthPolicyService
	claimer      *gateClaimer
	tenant       gateTenantProvider
	auth         *PasswordAuthService
	authAudience PolicyAudience
	// emailTokens + audit back the §4.3 reset paths and the break-glass
	// audit assertions; both are inert for every pre-existing case.
	emailTokens *gateEmailTokenRepo
	audit       *gateAuditSink
}

// gatesOption tweaks the PasswordAuthConfig newGatesEnv assembles, just
// before the service is built. The login-lockout fixtures use it to wire
// an AttemptCounter and a Verify-counting PasswordService; every
// pre-existing caller passes none and keeps the counter-less service,
// which is exactly the documented fail-open path.
type gatesOption func(*PasswordAuthConfig)

// newGatesEnv assembles a wired PasswordAuthService against in-memory
// fakes. policyValues seeds the auth-policy reader.
func newGatesEnv(t *testing.T, audience PolicyAudience, policyValues map[string]string, geoByIP map[string]string, opts ...gatesOption) *gatesEnv {
	t.Helper()
	// HIBP off for every env built here. ValidatePolicy hands the decision
	// to the POLICY toggle as soon as a policy is wired (see
	// password_service.go), so the constructor's hibpEnabled=false below is
	// not enough on its own: without this seed every Register / reset in
	// this file would reach api.pwnedpasswords.com over the network. Seeded
	// UNDER the caller's keys, so a case that wants the check on still can.
	values := map[string]string{"breachedPasswordCheck": "false"}
	for k, v := range policyValues {
		values[k] = v
	}
	policy := &AuthPolicyService{cs: &stubReader{values: values}}
	pwd := NewPasswordService(silentLogger(), false /* HIBP off via policy */)
	pwd.SetPolicy(policy)

	jwt, err := NewJWTServiceWithAudience(testRSAKey(), &testRSAKey().PublicKey, "test", string(audience), 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	tenant := gateTenantProvider{}
	jwt.SetTenantProvider(tenant)

	env := &gatesEnv{
		t:            t,
		users:        newGateUserFake(),
		refresh:      newGateRefreshRepo(),
		sessions:     newGateSessionRepo(),
		geo:          newGateGeoResolver(geoByIP),
		notifier:     &gateNotifier{configured: true},
		pwd:          pwd,
		jwt:          jwt,
		policy:       policy,
		claimer:      newGateClaimer(),
		tenant:       tenant,
		authAudience: audience,
		emailTokens:  &gateEmailTokenRepo{},
		audit:        &gateAuditSink{},
	}
	authCfg := PasswordAuthConfig{
		UserService:              env.users,
		TenantProvider:           env.tenant,
		PasswordService:          env.pwd,
		JWTService:               env.jwt,
		EmailTokenRepo:           env.emailTokens,
		RefreshTokenRepo:         env.refresh,
		AuthSessionRepo:          env.sessions,
		MFAFactorRepo:            nil, // no MFA in the gate paths
		MFAChallengeService:      nil,
		FirstAdminClaimer:        env.claimer,
		Notifier:                 env.notifier,
		FrontendURL:              "https://app.example.com",
		RequireEmailVerification: false,
		AppName:                  "Orkestra",
		Logger:                   silentLogger(),
		Policy:                   policy,
		Audience:                 audience,
		GeoResolver:              env.geo,
	}
	for _, opt := range opts {
		opt(&authCfg)
	}
	env.auth = NewPasswordAuthService(authCfg)
	env.auth.SetAuditSink(env.audit)
	return env
}

// hashedUser provisions a user with a real argon2id hash so Login() /
// ChangePassword() can verify the password without faking the
// PasswordService.
func (e *gatesEnv) hashedUser(email, password string) *iface.User {
	hash, err := e.pwd.Hash(password)
	if err != nil {
		e.t.Fatalf("hash: %v", err)
	}
	u := activeUser(email, hash)
	e.users.seed(u)
	return u
}

// ===== Login gates =====

func TestLogin_LoginDisabled_ReturnsErrLoginDisabled(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"loginEnabledAdmin": "false",
	}, nil)
	env.hashedUser("alice@example.com", "correct-horse-battery")
	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "alice@example.com", Password: "correct-horse-battery", IP: "203.0.113.10",
	})
	if !errors.Is(err, ErrLoginDisabled) {
		t.Fatalf("got %v, want ErrLoginDisabled", err)
	}
}

func TestLogin_CountryBlocked_ReturnsErrCountryBlocked(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"geoBlockCountries": "RU, KP",
	}, map[string]string{"5.5.5.5": "RU"})
	env.hashedUser("bob@example.com", "correct-horse-battery")

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "bob@example.com", Password: "correct-horse-battery", IP: "5.5.5.5",
	})
	if !errors.Is(err, ErrCountryBlocked) {
		t.Fatalf("got %v, want ErrCountryBlocked", err)
	}

	// Sanity: a non-blocked country still passes the gate. (Will fail
	// later for unrelated reasons since we only seeded one user — we
	// just want to confirm the gate didn't fire.)
	_, err2 := env.auth.Login(context.Background(), LoginInput{
		Email: "bob@example.com", Password: "correct-horse-battery", IP: "1.1.1.1",
	})
	if errors.Is(err2, ErrCountryBlocked) {
		t.Fatalf("gate must not fire for non-blocked country, got ErrCountryBlocked")
	}
}

func TestLogin_InactiveAutoDisable_FlipsIsActiveAndDenies(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"inactiveAccountAutoDisableDays": "30",
	}, nil)
	u := env.hashedUser("stale@example.com", "correct-horse-battery")
	old := time.Now().Add(-90 * 24 * time.Hour)
	u.LastLogin = &old

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "stale@example.com", Password: "correct-horse-battery", IP: "1.1.1.1",
	})
	// Auto-disable fires, then the IsActive check returns
	// ErrInvalidCredentials so we don't leak the existence of the
	// account. Also assert UpdateUser was called with IsActive=false.
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v, want ErrInvalidCredentials after auto-disable", err)
	}
	if u.IsActive {
		t.Fatalf("user must be flipped to inactive, got IsActive=true")
	}
	if got := len(env.users.updateUserCalls); got != 1 {
		t.Fatalf("expected 1 UpdateUser call (auto-disable), got %d", got)
	}
	if env.users.updateUserCalls[0].IsActive == nil || *env.users.updateUserCalls[0].IsActive {
		t.Fatalf("UpdateUser must set IsActive=false, got %+v", env.users.updateUserCalls[0])
	}
}

func TestLogin_RecentLastLogin_DoesNotAutoDisable(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"inactiveAccountAutoDisableDays": "30",
	}, nil)
	u := env.hashedUser("fresh@example.com", "correct-horse-battery")
	recent := time.Now().Add(-7 * 24 * time.Hour)
	u.LastLogin = &recent

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "fresh@example.com", Password: "correct-horse-battery", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("login should succeed for a 7d-old lastLogin under 30d threshold, got %v", err)
	}
	if !u.IsActive {
		t.Fatalf("recent user must stay active")
	}
	if len(env.users.updateUserCalls) != 0 {
		t.Fatalf("auto-disable must not fire for recent lastLogin")
	}
}

func TestLogin_NeverLoggedIn_DoesNotAutoDisable(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"inactiveAccountAutoDisableDays": "30",
	}, nil)
	u := env.hashedUser("never@example.com", "correct-horse-battery")
	u.LastLogin = nil // brand-new account

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "never@example.com", Password: "correct-horse-battery", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("brand-new account must not trip auto-disable, got %v", err)
	}
	if !u.IsActive {
		t.Fatalf("user without prior login must stay active")
	}
}

// ===== Register gates =====

func TestRegister_RegistrationDisabled_ReturnsError(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceClient, map[string]string{
		"registrationEnabledClient": "false",
	}, nil)
	// Seed one existing user so the first-user bypass doesn't kick in.
	env.users.seed(activeUser("seed@example.com", "x"))

	_, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "new@example.com", Password: "correct-horse-battery", FullName: "New", IP: "1.1.1.1",
	})
	if !errors.Is(err, ErrRegistrationDisabled) {
		t.Fatalf("got %v, want ErrRegistrationDisabled", err)
	}
	// No user was created.
	for _, u := range env.users.createdUsers {
		if u.Email == "new@example.com" {
			t.Fatalf("ErrRegistrationDisabled must abort before user creation")
		}
	}
}

func TestRegister_FirstUserBypassesRegistrationKillSwitch(t *testing.T) {
	// The bootstrap bypass is OPERATOR-only (audit H-1): the operator who runs
	// the very first registration on a fresh install must not be locked out by a
	// kill switch flipped before any account exists.
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"registrationEnabledAdmin": "false",
	}, nil)
	// users.count starts at 0 → first-user bypass should let this through.
	_, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "first@example.com", Password: "correct-horse-battery", FullName: "First", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("first operator user must bypass the kill switch, got %v", err)
	}
}

// TestRegister_ClientFirstUser_DoesNotBypassKillSwitch is the H-1 regression:
// the first-admin sentinel is global but the client user count is tier-scoped,
// so an anonymous client register on a fresh install must NOT bypass the client
// registration kill switch (otherwise it could also seize the global super_admin
// seat and brick the operator bootstrap).
func TestRegister_ClientFirstUser_DoesNotBypassKillSwitch(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceClient, map[string]string{
		"registrationEnabledClient": "false",
	}, nil)
	// Zero client users — pre-fix this would have bypassed the kill switch.
	_, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "first@example.com", Password: "correct-horse-battery", FullName: "First", IP: "1.1.1.1",
	})
	if !errors.Is(err, ErrRegistrationDisabled) {
		t.Fatalf("client first user must NOT bypass the kill switch, got %v", err)
	}
}

// TestRegister_ClientFirstUser_NeverClaimsSuperAdmin asserts a client-tier
// registration never wins the global first-admin seat even when registration is
// open and the sentinel is unclaimed.
func TestRegister_ClientFirstUser_NeverClaimsSuperAdmin(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceClient, map[string]string{
		"registrationEnabledClient": "true",
	}, nil)
	// Zero client users, claimer unclaimed — pre-fix this would mint super_admin.
	u, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "first@example.com", Password: "correct-horse-battery", FullName: "First", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Role == "super_admin" {
		t.Fatalf("client-tier registration must never be granted super_admin")
	}
}

func TestRegister_EmailDomainNotAllowed_ReturnsError(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceClient, map[string]string{
		"allowedEmailDomainsClient": "acme.com,partner.io",
	}, nil)
	env.users.seed(activeUser("seed@example.com", "x")) // skip first-user bypass

	_, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "outsider@otherco.com", Password: "correct-horse-battery", FullName: "X", IP: "1.1.1.1",
	})
	if !errors.Is(err, ErrEmailDomainNotAllowed) {
		t.Fatalf("got %v, want ErrEmailDomainNotAllowed", err)
	}

	// In-allowlist domain still works.
	_, err = env.auth.Register(context.Background(), RegisterInput{
		Email: "ok@acme.com", Password: "correct-horse-battery", FullName: "Y", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("acme.com should be allowed, got %v", err)
	}
}

func TestRegister_OperatorDefaultRoleGuest(t *testing.T) {
	// Non-first operator-tier password signup must land as "guest"
	// (lowest system role) so a fresh registration can't silently grant
	// itself elevated privileges. First-admin sentinel covers the
	// "first account on a fresh install" case separately.
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	env.users.seed(activeUser("seed@example.com", "x"))
	env.claimer.claimed = map[string]bool{"seed": true} // next claim returns false

	u, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "newop@example.com", Password: "correct-horse-battery", FullName: "New", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Role != "guest" {
		t.Fatalf("expected role=guest for non-first operator-tier signup, got %q", u.Role)
	}
}

func TestRegister_DefaultRoleClient_AppliedFromPolicy(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceClient, map[string]string{
		"defaultRoleClient": "guest",
	}, nil)
	// Pre-seed so the registrant doesn't claim first-admin.
	env.users.seed(activeUser("seed@example.com", "x"))
	env.claimer.claimed = map[string]bool{"seed": true} // ensures next claim returns false

	u, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "newclient@example.com", Password: "correct-horse-battery", FullName: "Client", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Role != "guest" {
		t.Fatalf("expected role=guest from defaultRoleClient policy, got %q", u.Role)
	}
}

// ===== ChangePassword toggle =====

func TestChangePassword_RevokeOnPasswordChangeOff_SkipsDeviceTrustRevoke(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"revokeSessionsOnPasswordChange": "false",
	}, nil)
	u := env.hashedUser("dt@example.com", "correct-horse-battery")
	dt := &recordingDeviceTrust{}
	env.auth.deviceTrust = dt

	if err := env.auth.ChangePassword(context.Background(), ChangePasswordInput{UserUUID: u.UUID, Current: "correct-horse-battery", New: "new-correct-horse-pw"}); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if dt.revokeCalls != 0 {
		t.Fatalf("toggle off must skip device-trust revoke, got %d calls", dt.revokeCalls)
	}
}

func TestChangePassword_RevokeOnPasswordChangeOn_DefaultRevokes(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil) // default = true
	u := env.hashedUser("dt2@example.com", "correct-horse-battery")
	dt := &recordingDeviceTrust{}
	env.auth.deviceTrust = dt

	if err := env.auth.ChangePassword(context.Background(), ChangePasswordInput{UserUUID: u.UUID, Current: "correct-horse-battery", New: "new-correct-horse-pw"}); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if dt.revokeCalls != 1 {
		t.Fatalf("default-on must call device-trust revoke exactly once, got %d", dt.revokeCalls)
	}
}

func TestShouldRevokeOnPasswordChange_Accessor(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"revokeSessionsOnPasswordChange": "false",
	}, nil)
	if env.auth.ShouldRevokeOnPasswordChange(context.Background()) {
		t.Fatalf("toggle off must propagate to the public accessor")
	}
	envOn := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	if !envOn.auth.ShouldRevokeOnPasswordChange(context.Background()) {
		t.Fatalf("default policy must report should-revoke=true")
	}
}

// recordingDeviceTrust implements DeviceTrustService with just enough
// to observe RevokeAllByUser calls. Other methods panic so a refactor
// that starts to depend on them surfaces immediately.
type recordingDeviceTrust struct {
	revokeCalls int
}

func (r *recordingDeviceTrust) MarkTrusted(context.Context, MarkTrustedInput) error {
	panic("not used")
}
func (r *recordingDeviceTrust) IsTrusted(context.Context, string, string, string) (bool, *authModels.DeviceTrustDoc, error) {
	panic("not used")
}
func (r *recordingDeviceTrust) ListActive(context.Context, string) ([]*authModels.DeviceTrustDoc, error) {
	panic("not used")
}
func (r *recordingDeviceTrust) RevokeByDevice(context.Context, string, string, string) error {
	panic("not used")
}
func (r *recordingDeviceTrust) RevokeAllByUser(_ context.Context, _ string, _ string) error {
	r.revokeCalls++
	return nil
}

// ===== New-device-login email gate =====

func TestNewDeviceLogin_EmailFiresWhenDeviceUnseen(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil) // notify-default = true
	env.hashedUser("nd@example.com", "correct-horse-battery")

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "nd@example.com", Password: "correct-horse-battery", IP: "1.1.1.1", DeviceID: "device-A",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	// Notifier must have received the new-device template.
	if got := len(env.notifier.sends); got != 1 {
		t.Fatalf("expected 1 new-device email, got %d", got)
	}
	if env.notifier.sends[0].TemplateID != "auth.new_device_login" {
		t.Fatalf("template = %q, want auth.new_device_login", env.notifier.sends[0].TemplateID)
	}
}

func TestNewDeviceLogin_EmailSuppressedByPolicy(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"notifyUserOnNewDeviceLogin": "false",
	}, nil)
	env.hashedUser("nd2@example.com", "correct-horse-battery")

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "nd2@example.com", Password: "correct-horse-battery", IP: "1.1.1.1", DeviceID: "device-A",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(env.notifier.sends) != 0 {
		t.Fatalf("policy off must suppress new-device email, got %d sends", len(env.notifier.sends))
	}
}

func TestNewDeviceLogin_KnownDeviceDoesNotEmail(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("nd3@example.com", "correct-horse-battery")
	// Pre-load device history so the (user, device) pair is recognised.
	env.sessions.deviceHistory[u.UUID+"|device-A"] = []*authModels.AuthSessionDoc{{
		UUID: "prior-session", UserUUID: u.UUID, DeviceID: "device-A",
	}}

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "nd3@example.com", Password: "correct-horse-battery", IP: "1.1.1.1", DeviceID: "device-A",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(env.notifier.sends) != 0 {
		t.Fatalf("known device must not trigger new-device email, got %d sends", len(env.notifier.sends))
	}
}

// ===== MFA recovery codes count =====

func TestMFAEnrollment_RecoveryCodesCount_HonoursPolicy(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want int
	}{
		{"unset → legacy 10", nil, BackupCodeCount},
		{"valid 6", map[string]string{"recoveryCodesCount": "6"}, 6},
		{"valid 25", map[string]string{"recoveryCodesCount": "25"}, 25},
		{"out-of-range high → legacy 10", map[string]string{"recoveryCodesCount": "100"}, BackupCodeCount},
		{"zero → legacy 10", map[string]string{"recoveryCodesCount": "0"}, BackupCodeCount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// MFAService encrypts the TOTP secret at confirm time —
			// requires MFA_SECRET_ENCRYPTION_KEY in env. Set per
			// sub-test so each run is hermetic.
			t.Setenv("MFA_SECRET_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
			factors := newFakeFactorRepo()
			challenges := newFakeMFAChallenge()
			policy := &AuthPolicyService{cs: &stubReader{values: tc.set}}
			pwd := NewPasswordService(silentLogger(), false)
			pwd.SetPolicy(policy)

			svc := NewMFAService(factors, challenges, pwd, "Orkestra", silentLogger())
			svc.SetPolicy(policy)

			user := activeUser("mfa@example.com", "x")
			begin, err := svc.BeginEnrollment(context.Background(), user)
			if err != nil {
				t.Fatalf("BeginEnrollment: %v", err)
			}
			code := mustGenerateTOTPNow(t, begin.SecretBase32)

			plain, err := svc.ConfirmEnrollment(context.Background(), user.UUID, begin.ChallengeID, code)
			if err != nil {
				t.Fatalf("ConfirmEnrollment: %v", err)
			}
			if got := len(plain); got != tc.want {
				t.Fatalf("issued %d recovery codes, want %d", got, tc.want)
			}
		})
	}
}

// ===== AuthService OAuth gates =====

func TestOAuthCallback_SignupDisabled_ReturnsErr(t *testing.T) {
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"oauthAllowSignupAdmin": "false",
	})
	// Email is NOT in the user fake → falls to the new-user branch.
	_, err := env.auth.HandleOAuthCallbackWithLinking(
		context.Background(),
		authModels.OAuthProviderGoogle,
		map[string]any{"id": "g-99", "email": "newcomer@example.com", "name": "New", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	if !errors.Is(err, ErrOAuthSignupDisabled) {
		t.Fatalf("got %v, want ErrOAuthSignupDisabled", err)
	}
}

func TestOAuthCallback_OperatorDefaultRoleGuest(t *testing.T) {
	// Non-first OAuth signup on the operator surface lands as "guest"
	// (lowest system role) so a fresh OAuth callback can't silently
	// grant itself elevated privileges. First-admin sentinel claim
	// upgrades the very first account to super_admin — covered
	// elsewhere; here we want the non-first path. Abort the OAuth flow
	// right after CreateUserFromOAuth captures the role so downstream
	// token-issuance fakes (which panic) don't run.
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
	env.users.seed(activeUser("seed@example.com", "x"))
	env.claimer.claimed = map[string]bool{"seed": true} // next claim returns false
	env.users.createFromOAuthAbortErr = errors.New("stop here, role captured")
	_, _ = env.auth.HandleOAuthCallbackWithLinking(
		context.Background(),
		authModels.OAuthProviderGoogle,
		map[string]any{"id": "g-200", "email": "joiner@example.com", "name": "Joiner", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	created := env.users.byEmail["joiner@example.com"]
	if created == nil {
		t.Fatalf("OAuth signup did not persist the new user before abort")
	}
	if created.Role != "guest" {
		t.Fatalf("operator-tier OAuth signup must default to role=guest, got %q", created.Role)
	}
}

func TestOAuthCallback_ClientDefaultRoleReadsPolicy(t *testing.T) {
	// Tier-2 OAuth signup must honour the admin-configurable
	// defaultRoleClient — mirrors the password Register() path so the
	// two surfaces agree on the role for a new tier-2 account.
	env := newOAuthGatesEnv(t, PolicyAudienceClient, map[string]string{
		"defaultRoleClient": "guest",
	})
	env.users.seed(activeUser("seed-client@example.com", "x"))
	env.claimer.claimed = map[string]bool{"seed": true} // next claim returns false
	env.users.createFromOAuthAbortErr = errors.New("stop here, role captured")
	_, _ = env.auth.HandleOAuthCallbackWithLinking(
		context.Background(),
		authModels.OAuthProviderGoogle,
		map[string]any{"id": "g-300", "email": "client-joiner@example.com", "name": "Client", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	created := env.users.byEmail["client-joiner@example.com"]
	if created == nil {
		t.Fatalf("OAuth signup did not persist the new user before abort")
	}
	if created.Role != "guest" {
		t.Fatalf("client-tier OAuth signup must read defaultRoleClient (=guest), got %q", created.Role)
	}
}

func TestOAuthCallback_NewUser_RequiresVerifiedEmail(t *testing.T) {
	// §4.4: an unlinked identity is matched or created only when the IdP
	// vouches for the address. claim_true still lands EmailVerified=true so
	// the user is not asked to confirm what the IdP confirmed; false or
	// missing is refused BEFORE the email lookup and creates nothing.
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
	env.users.seed(activeUser("seed-ev@example.com", "x"))
	env.claimer.claimed = map[string]bool{"seed": true}
	env.users.createFromOAuthAbortErr = errors.New("stop here, flag captured")

	_, _ = env.auth.HandleOAuthCallbackWithLinking(
		context.Background(), authModels.OAuthProviderGoogle,
		map[string]any{"provider_id": "g-verified", "email": "verified@example.com", "name": "V", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	created := env.users.byEmail["verified@example.com"]
	if created == nil || !created.EmailVerified {
		t.Fatalf("a verified IdP email must create a verified user: %+v", created)
	}

	for name, claim := range map[string]map[string]any{
		"claim_false":   {"provider_id": "g-unverified", "email": "unverified@example.com", "name": "U", "email_verified": false},
		"claim_missing": {"provider_id": "g-missing", "email": "missing@example.com", "name": "M"},
		"claim_string":  {"provider_id": "g-string", "email": "string@example.com", "name": "S", "email_verified": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
			_, err := env.auth.HandleOAuthCallbackWithLinking(
				context.Background(), authModels.OAuthProviderGoogle, claim,
				nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
			)
			if !errors.Is(err, ErrOAuthEmailUnverified) {
				t.Fatalf("err = %v, want ErrOAuthEmailUnverified", err)
			}
			if env.users.getByEmailCalls != 0 {
				t.Fatalf("GetUserByEmail was called %d times; must be 0 before the verified check", env.users.getByEmailCalls)
			}
			if len(env.users.createdUsers) != 0 {
				t.Fatal("no user may be created")
			}
		})
	}
}

func TestOAuthCallback_UnverifiedEmail_SameAnswerForKnownAndUnknownAccount(t *testing.T) {
	// The refusal must not be an account-existence oracle: identical error,
	// zero lookups, whether or not a local account with that email exists.
	for name, seedKnown := range map[string]bool{"known": true, "unknown": false} {
		t.Run(name, func(t *testing.T) {
			env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
			if seedKnown {
				env.users.seed(activeUser("probe@example.com", "x"))
			}
			_, err := env.auth.HandleOAuthCallbackWithLinking(
				context.Background(), authModels.OAuthProviderGoogle,
				map[string]any{"provider_id": "g-probe", "email": "probe@example.com", "name": "P", "email_verified": false},
				nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
			)
			if !errors.Is(err, ErrOAuthEmailUnverified) || env.users.getByEmailCalls != 0 {
				t.Fatalf("err = %v, lookups = %d", err, env.users.getByEmailCalls)
			}
		})
	}
}

func TestOAuthCallback_AutoLinkPolicyUnavailable_FailsClosedBeforeLookup(t *testing.T) {
	for name, reader := range map[string]*stubReader{
		"read failure":     {rawErr: errors.New("mongo down")},
		"missing document": {requiredMissing: true},
		"malformed value":  {values: map[string]string{"oauthAutoLinkByEmail": "treu"}},
	} {
		t.Run(name, func(t *testing.T) {
			env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
			env.policy.cs = reader
			env.users.seed(activeUser("existing@example.com", "x"))
			_, err := env.auth.HandleOAuthCallbackWithLinking(
				context.Background(), authModels.OAuthProviderGoogle,
				map[string]any{"provider_id": "g-existing", "email": "existing@example.com", "name": "E", "email_verified": true},
				nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
			)
			if !errors.Is(err, ErrAuthPolicyUnavailable) {
				t.Fatalf("err = %v, want ErrAuthPolicyUnavailable", err)
			}
			if env.users.getByEmailCalls != 0 || len(env.users.createdUsers) != 0 {
				t.Fatalf("lookups = %d, created = %d; must both be 0", env.users.getByEmailCalls, len(env.users.createdUsers))
			}
		})
	}
}

func TestOAuthCallback_NilPolicy_FailsClosed(t *testing.T) {
	// A service wired without a policy cannot establish the auto-link
	// rule; it must not fall open to the legacy "always link".
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
	env.auth.SetPolicy(nil)
	env.users.seed(activeUser("existing@example.com", "x"))
	_, err := env.auth.HandleOAuthCallbackWithLinking(
		context.Background(), authModels.OAuthProviderGoogle,
		map[string]any{"provider_id": "g-existing", "email": "existing@example.com", "name": "E", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	if !errors.Is(err, ErrAuthPolicyUnavailable) || env.users.getByEmailCalls != 0 {
		t.Fatalf("err = %v, lookups = %d", err, env.users.getByEmailCalls)
	}
}

func TestOAuthCallback_RegistrationDisabled_ReturnsErr(t *testing.T) {
	// The umbrella "Allow signups on operator console" toggle must also
	// gate the OAuth new-user branch — not just the password Register()
	// path. Audience-scoped: operator surface reads
	// registrationEnabledAdmin.
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"registrationEnabledAdmin": "false",
	})
	_, err := env.auth.HandleOAuthCallbackWithLinking(
		context.Background(),
		authModels.OAuthProviderGoogle,
		map[string]any{"id": "g-100", "email": "newcomer2@example.com", "name": "New2", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	if !errors.Is(err, ErrOAuthSignupDisabled) {
		t.Fatalf("got %v, want ErrOAuthSignupDisabled", err)
	}
}

func TestOAuthCallback_AutoLinkDisabled_ReturnsErr(t *testing.T) {
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, map[string]string{
		"oauthAutoLinkByEmail": "false",
	})
	// Pre-seed a user with this email — the OAuth flow finds them by
	// email and would normally auto-link. The toggle must refuse.
	env.users.seed(activeUser("existing@example.com", "x"))

	_, err := env.auth.HandleOAuthCallbackWithLinking(
		context.Background(),
		authModels.OAuthProviderGoogle,
		map[string]any{"id": "g-existing", "email": "existing@example.com", "name": "Existing", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	if !errors.Is(err, ErrOAuthLinkDisabled) {
		t.Fatalf("got %v, want ErrOAuthLinkDisabled", err)
	}
	if env.users.getByEmailCalls != 1 {
		t.Fatalf("lookups = %d, want exactly 1 (policy is read BEFORE the lookup, the refusal comes after it)", env.users.getByEmailCalls)
	}
}

// oauthGatesEnv mirrors gatesEnv but wires AuthService instead of
// PasswordAuthService. Reuses the same fakes.
type oauthGatesEnv struct {
	users    *gateUserFake
	refresh  *gateRefreshRepo
	sessions *gateSessionRepo
	policy   *AuthPolicyService
	auth     AuthService
	claimer  *gateClaimer
}

func newOAuthGatesEnv(t *testing.T, audience PolicyAudience, policyValues map[string]string) *oauthGatesEnv {
	t.Helper()
	if policyValues == nil {
		policyValues = map[string]string{}
	}
	policy := &AuthPolicyService{cs: &stubReader{values: policyValues}}
	users := newGateUserFake()
	refresh := newGateRefreshRepo()
	sessions := newGateSessionRepo()
	jwt, err := NewJWTServiceWithAudience(testRSAKey(), &testRSAKey().PublicKey, "test", string(audience), 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})

	claimer := newGateClaimer()
	authSvc, err := NewAuthService(&AuthConfig{
		UserService:         users,
		TenantProvider:      gateTenantProvider{},
		OAuthProviderRepo:   &oauthRepoStub{},
		RefreshTokenRepo:    refresh,
		AuthSessionRepo:     sessions,
		JWTService:          jwt,
		MFAFactorRepo:       nil,
		MFAChallengeService: nil,
		FirstAdminClaimer:   claimer,
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	authSvc.SetPolicy(policy)
	authSvc.SetAudience(audience)
	return &oauthGatesEnv{
		users: users, refresh: refresh, sessions: sessions, policy: policy, auth: authSvc, claimer: claimer,
	}
}

// oauthRepoStub satisfies repository.OAuthProviderRepository. The
// gate tests exercise GetByProviderAndID early in the flow (returns
// nil to mean "not yet linked"); everything else returns no-op
// success since the test path returns before the repo writes anything
// meaningful.
type oauthRepoStub struct{}

func (oauthRepoStub) CreateOAuthProvider(context.Context, *authModels.OAuthProviderDoc) error {
	return nil
}
func (oauthRepoStub) LinkOAuthProvider(context.Context, string, *authModels.OAuthLink) error {
	return nil
}
func (oauthRepoStub) GetByProviderAndID(context.Context, authModels.OAuthProvider, string) (*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (oauthRepoStub) GetByUserUUID(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (oauthRepoStub) GetPrimaryProvider(context.Context, string) (*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (oauthRepoStub) UpdateLastUsed(context.Context, string) error { return nil }
func (oauthRepoStub) SetPrimaryProvider(context.Context, string, authModels.OAuthProvider) error {
	return nil
}
func (oauthRepoStub) UpdateRefreshToken(context.Context, string, string) error { return nil }
func (oauthRepoStub) UpdateOAuthTokens(context.Context, string, string, string, *time.Time, *time.Time, []string) error {
	return nil
}
func (oauthRepoStub) UpdateMetadata(context.Context, string, map[string]interface{}) error {
	return nil
}
func (oauthRepoStub) UnlinkProvider(context.Context, string, authModels.OAuthProvider) error {
	return nil
}
func (oauthRepoStub) DeleteProvider(context.Context, string) error { return nil }
func (oauthRepoStub) FindByEmail(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (oauthRepoStub) ConsolidateProviders(context.Context, string, string) error { return nil }

// ===== helpers =====

// fakeMFAChallenge is an in-memory MFAChallengeService for the
// recovery-codes test. Single map keyed by challenge id since the
// production MFAChallenge struct covers both enroll + login.
type fakeMFAChallenge struct {
	ch map[string]*MFAChallenge
}

func newFakeMFAChallenge() *fakeMFAChallenge {
	return &fakeMFAChallenge{ch: map[string]*MFAChallenge{}}
}

func (f *fakeMFAChallenge) Begin(_ context.Context, userUUID string, purpose MFAChallengePurpose, pendingSecret string) (*MFAChallenge, error) {
	id := userUUID + "-" + string(purpose)
	c := &MFAChallenge{
		ID: id, UserUUID: userUUID, Purpose: purpose, PendingSecret: pendingSecret,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	f.ch[id] = c
	return c, nil
}

func (f *fakeMFAChallenge) BeginLogin(_ context.Context, in LoginChallengeInput) (*MFAChallenge, error) {
	id := in.UserUUID + "-login"
	c := &MFAChallenge{
		ID: id, UserUUID: in.UserUUID, Purpose: MFAPurposeLogin,
		SessionID: in.SessionID, DeviceID: in.DeviceID, Platform: in.Platform, IPAddress: in.IPAddress,
		Fingerprint: in.Fingerprint, SourceAMR: in.SourceAMR,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	f.ch[id] = c
	return c, nil
}

func (f *fakeMFAChallenge) Peek(_ context.Context, id string) (*MFAChallenge, error) {
	c, ok := f.ch[id]
	if !ok {
		return nil, errNotFound
	}
	return c, nil
}

func (f *fakeMFAChallenge) Consume(_ context.Context, id string) (*MFAChallenge, error) {
	c, ok := f.ch[id]
	if !ok {
		return nil, errNotFound
	}
	delete(f.ch, id)
	return c, nil
}

func (f *fakeMFAChallenge) IncrementAttempts(_ context.Context, id string) (int, error) {
	c, ok := f.ch[id]
	if !ok {
		return 0, errNotFound
	}
	c.Attempts++
	return c.Attempts, nil
}

// mustGenerateTOTPNow returns a TOTP code valid right now for the given
// base32 secret. Reuses the same library the production validator
// uses so the algorithm stays in lock-step.
func mustGenerateTOTPNow(t *testing.T, secretBase32 string) string {
	t.Helper()
	code, err := totpGenerateNow(secretBase32)
	if err != nil {
		t.Fatalf("totp generate: %v", err)
	}
	return code
}

// suppress lint when the iface package is unused after pruning.
var _ iface.NotificationSender = (*gateNotifier)(nil)

// --- PR 3 §4.3: per-surface password-method gates ---

func passwordOff(surface string) map[string]string {
	return map[string]string{"passwordLoginEnabled" + surface: "false"}
}

func TestLogin_PasswordMethodGate(t *testing.T) {
	t.Run("off refuses before the user lookup, per audience", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		// Deliberately NO user seeded: the gate must answer before
		// GetUserForAuth, so the outcome cannot be ErrInvalidCredentials.
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "who@example.com", Password: "pw", IP: "203.0.113.9"})
		if !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
	})
	t.Run("other audience unaffected", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Admin"), nil)
		u := env.hashedUser("c@example.com", "correct horse battery staple")
		u.EmailVerified = true
		resp, err := env.auth.Login(context.Background(), LoginInput{Email: "c@example.com", Password: "correct horse battery staple"})
		if err != nil || resp == nil || resp.AccessToken == "" {
			t.Fatalf("client login with only Admin off must succeed, got (%v, %v)", resp, err)
		}
	})
	t.Run("policy outage is 503-shaped, never open", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "a@example.com", Password: "pw"})
		if !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("nil policy is an outage, not legacy-allow", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
		env.auth.policy = nil
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "a@example.com", Password: "pw"})
		if !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("break-glass rescues operator login and audits once", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		env.policy.SetOperatorBreakGlass(true)
		u := env.hashedUser("op@example.com", "correct horse battery staple")
		u.EmailVerified = true
		resp, err := env.auth.Login(context.Background(), LoginInput{Email: "op@example.com", Password: "correct horse battery staple", IP: "203.0.113.9"})
		if err != nil || resp == nil || resp.AccessToken == "" {
			t.Fatalf("rescued login must mint tokens, got (%v, %v)", resp, err)
		}
		got := env.audit.byAction("auth.policy.break_glass_used")
		if len(got) != 1 {
			t.Fatalf("want exactly one break-glass event, got %d", len(got))
		}
		e := got[0]
		if e.ActorUserID != u.UUID || e.IPAddress != "203.0.113.9" {
			t.Errorf("event must carry user UUID + source IP, got %+v", e)
		}
		if e.Metadata["audience"] != "operator" || e.Metadata["sessionId"] != resp.SessionID {
			t.Errorf("event must carry audience + session id, got %+v", e.Metadata)
		}
		if e.ActorEmail != "" {
			t.Errorf("event must not carry a full email, got %q", e.ActorEmail)
		}
	})
	t.Run("break-glass never rescues client login", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		env.policy.SetOperatorBreakGlass(true)
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "c@example.com", Password: "pw"})
		if !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
	})
	t.Run("failed break-glass attempt claims nothing", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		env.policy.SetOperatorBreakGlass(true)
		u := env.hashedUser("op@example.com", "correct horse battery staple")
		u.EmailVerified = true
		_, err := env.auth.Login(context.Background(), LoginInput{Email: "op@example.com", Password: "WRONG", IP: "203.0.113.9"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("want ErrInvalidCredentials, got %v", err)
		}
		if n := len(env.audit.byAction("auth.policy.break_glass_used")); n != 0 {
			t.Fatalf("failed attempts must not claim the override was used, got %d events", n)
		}
	})
}

func TestRegister_PasswordMethodGate(t *testing.T) {
	// Password follows this file's existing idiom ("correct-horse-battery")
	// rather than the full xkcd passphrase, which is a known-breached
	// string: newGatesEnv seeds breachedPasswordCheck=false so Register's
	// ValidatePolicy never calls HIBP, and the idiom keeps the case honest
	// for any env that re-enables the check.
	in := RegisterInput{Email: "new@example.com", Password: "correct-horse-battery", FullName: "New User"}

	t.Run("off refuses, per audience, break-glass ignored", func(t *testing.T) {
		for _, tc := range []struct {
			aud     PolicyAudience
			surface string
		}{{PolicyAudienceOperator, "Admin"}, {PolicyAudienceClient, "Client"}} {
			env := newGatesEnv(t, tc.aud, passwordOff(tc.surface), nil)
			env.policy.SetOperatorBreakGlass(true)
			// A non-first signup: seed one existing user so the operator
			// bootstrap branch cannot fire.
			env.users.seed(&iface.User{UUID: "existing", Email: "e@example.com", IsActive: true})
			_, err := env.auth.Register(context.Background(), in)
			if !errors.Is(err, ErrPasswordLoginDisabled) {
				t.Fatalf("%s: want ErrPasswordLoginDisabled, got %v", tc.aud, err)
			}
		}
	})
	t.Run("operator first-user bootstrap bypasses the gate", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		u, err := env.auth.Register(context.Background(), in)
		if err != nil || u == nil {
			t.Fatalf("first operator signup must bypass the method gate, got (%v, %v)", u, err)
		}
		if u.Role != "super_admin" {
			t.Fatalf("first user must claim super_admin, got %q", u.Role)
		}
	})
	t.Run("RegisterInitialAdmin bootstrap stays reachable with password off", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceOperator, passwordOff("Admin"), nil)
		resp, err := env.auth.RegisterInitialAdmin(context.Background(), "root@example.com", "correct-horse-battery", "Root", "203.0.113.9")
		if err != nil || resp == nil || resp.AccessToken == "" {
			t.Fatalf("setup-wizard bootstrap is an explicit G2 exception; got (%v, %v)", resp, err)
		}
	})
	t.Run("empty Tier-2 collection gets no bypass", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		if _, err := env.auth.Register(context.Background(), in); !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
	})
	t.Run("policy outage refuses non-bootstrap signups", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		if _, err := env.auth.Register(context.Background(), in); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}

func TestForgotPassword_PasswordMethodGate(t *testing.T) {
	t.Run("off refuses before the lookup, identically for any email", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		env.policy.SetOperatorBreakGlass(true) // must be invisible here
		known := env.hashedUser("known@example.com", "correct horse battery staple")
		known.IsActive = true
		for _, email := range []string{"known@example.com", "unknown@example.com"} {
			err := env.auth.ForgotPassword(context.Background(), email, "203.0.113.9")
			if !errors.Is(err, ErrPasswordLoginDisabled) {
				t.Fatalf("%s: want ErrPasswordLoginDisabled, got %v", email, err)
			}
		}
		if n := len(env.emailTokens.created); n != 0 {
			t.Fatalf("no reset token may be minted under the gate, got %d", n)
		}
	})
	t.Run("on keeps the generic swallow for known and unknown email", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		known := env.hashedUser("known@example.com", "correct horse battery staple")
		known.IsActive = true
		for _, email := range []string{"known@example.com", "unknown@example.com"} {
			if err := env.auth.ForgotPassword(context.Background(), email, "203.0.113.9"); err != nil {
				t.Fatalf("%s: account-specific outcomes must stay swallowed, got %v", email, err)
			}
		}
	})
	t.Run("policy outage propagates", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		if err := env.auth.ForgotPassword(context.Background(), "a@example.com", ""); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}

func TestAdminTriggerPasswordReset_PasswordMethodGate(t *testing.T) {
	t.Run("off refuses; break-glass ignored", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, passwordOff("Client"), nil)
		env.policy.SetOperatorBreakGlass(true)
		u := env.hashedUser("victim@example.com", "correct horse battery staple")
		if err := env.auth.AdminTriggerPasswordReset(context.Background(), u.UUID); !errors.Is(err, ErrPasswordLoginDisabled) {
			t.Fatalf("want ErrPasswordLoginDisabled, got %v", err)
		}
		if n := len(env.emailTokens.created); n != 0 {
			t.Fatalf("no reset token may be minted under the gate, got %d", n)
		}
	})
	t.Run("on still works", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		u := env.hashedUser("victim@example.com", "correct horse battery staple")
		if err := env.auth.AdminTriggerPasswordReset(context.Background(), u.UUID); err != nil {
			t.Fatalf("want success, got %v", err)
		}
		if n := len(env.emailTokens.created); n != 1 {
			t.Fatalf("want one reset token, got %d", n)
		}
	})
	t.Run("policy outage propagates", func(t *testing.T) {
		env := newGatesEnv(t, PolicyAudienceClient, nil, nil)
		env.policy.cs = &stubReader{rawErr: errors.New("mongo down")}
		if err := env.auth.AdminTriggerPasswordReset(context.Background(), "any"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}

// TestOpenRoutes_UnaffectedByPasswordMethodGate pins the OPEN column of the
// §4.3 verdict table: the routes that manage or verify an existing
// credential never consult the per-surface method gate, on either surface.
//
// Each subtest calls the service method under a password-off policy with
// minimal / invalid input and asserts ONLY that neither policy sentinel
// came back. The specific error (invalid token, wrong password) is
// pre-existing behaviour that belongs to those routes' own tests and is
// deliberately not asserted here — over-asserting it would make this test
// fail for reasons that have nothing to do with the gate.
//
// The token-redeeming routes are called with an empty token: the shared
// gateEmailTokenRepo panics in GetByHash by design, and an empty token
// short-circuits in lookupEmailToken. That is still the right probe — on
// every gated route the gate sits AHEAD of the token/user lookup (see
// ForgotPassword), so a gate on these routes would fire here too.
func TestOpenRoutes_UnaffectedByPasswordMethodGate(t *testing.T) {
	for _, tier := range []struct {
		name     string
		audience PolicyAudience
		surface  string
	}{
		{"operator", PolicyAudienceOperator, "Admin"},
		{"client", PolicyAudienceClient, "Client"},
	} {
		t.Run(tier.name, func(t *testing.T) {
			env := newGatesEnv(t, tier.audience, passwordOff(tier.surface), nil)
			u := env.hashedUser("open@example.com", "correct horse battery staple")
			unverified := env.hashedUser("pending@example.com", "correct horse battery staple")
			unverified.EmailVerified = false

			for _, c := range []struct {
				route string
				run   func() error
			}{
				{"reset-password", func() error {
					return env.auth.ResetPassword(context.Background(), "", "another correct horse staple")
				}},
				{"accept-invite", func() error {
					return env.auth.ConsumeInvite(context.Background(), "", "another correct horse staple")
				}},
				{"verify-email", func() error {
					return env.auth.VerifyEmail(context.Background(), "")
				}},
				{"verify-email/resend", func() error {
					return env.auth.ResendVerification(context.Background(), unverified.Email, "203.0.113.9")
				}},
				{"change-password", func() error {
					return env.auth.ChangePassword(context.Background(), ChangePasswordInput{
						UserUUID: u.UUID,
						Current:  "the wrong current password",
						New:      "another correct horse staple",
					})
				}},
			} {
				t.Run(c.route, func(t *testing.T) {
					err := c.run()
					if errors.Is(err, ErrPasswordLoginDisabled) || errors.Is(err, ErrAuthPolicyUnavailable) {
						t.Fatalf("open route must never consult the password-method gate, got %v", err)
					}
				})
			}
		})
	}
}
