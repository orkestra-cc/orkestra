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

	"github.com/orkestra-cc/orkestra-sdk/iface"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
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
	rateLimiter  *sharederrors.RateLimiter
	pwd          PasswordService
	jwt          JWTService
	policy       *AuthPolicyService
	claimer      *gateClaimer
	tenant       gateTenantProvider
	auth         *PasswordAuthService
	authAudience PolicyAudience
}

// newGatesEnv assembles a wired PasswordAuthService against in-memory
// fakes. policyValues seeds the auth-policy reader.
func newGatesEnv(t *testing.T, audience PolicyAudience, policyValues map[string]string, geoByIP map[string]string) *gatesEnv {
	t.Helper()
	if policyValues == nil {
		policyValues = map[string]string{}
	}
	policy := &AuthPolicyService{cs: &stubReader{values: policyValues}}
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
		rateLimiter:  sharederrors.NewRateLimiter(),
		pwd:          pwd,
		jwt:          jwt,
		policy:       policy,
		claimer:      newGateClaimer(),
		tenant:       tenant,
		authAudience: audience,
	}
	env.auth = NewPasswordAuthService(PasswordAuthConfig{
		UserService:              env.users,
		TenantProvider:           env.tenant,
		PasswordService:          env.pwd,
		JWTService:               env.jwt,
		EmailTokenRepo:           nil, // not exercised
		RefreshTokenRepo:         env.refresh,
		AuthSessionRepo:          env.sessions,
		MFAFactorRepo:            nil, // no MFA in the gate paths
		MFAChallengeService:      nil,
		FirstAdminClaimer:        env.claimer,
		Notifier:                 env.notifier,
		RateLimiter:              env.rateLimiter,
		FrontendURL:              "https://app.example.com",
		RequireEmailVerification: false,
		AppName:                  "Orkestra",
		Logger:                   silentLogger(),
		Policy:                   policy,
		Audience:                 audience,
		GeoResolver:              env.geo,
	})
	t.Cleanup(env.rateLimiter.Close)
	return env
}

// hashedUser provisions a user with a real argon2id hash so Login() /
// ChangePassword() can verify the password without faking the
// PasswordService.
func (e *gatesEnv) hashedUser(email, password string) *userModels.User {
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
	env := newGatesEnv(t, PolicyAudienceClient, map[string]string{
		"registrationEnabledClient": "false",
	}, nil)
	// users.count starts at 0 → first-user bypass should let this through.
	_, err := env.auth.Register(context.Background(), RegisterInput{
		Email: "first@example.com", Password: "correct-horse-battery", FullName: "First", IP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("first user must bypass the kill switch, got %v", err)
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

	if err := env.auth.ChangePassword(context.Background(), u.UUID, "correct-horse-battery", "new-correct-horse-pw"); err != nil {
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

	if err := env.auth.ChangePassword(context.Background(), u.UUID, "correct-horse-battery", "new-correct-horse-pw"); err != nil {
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
		map[string]any{"id": "g-99", "email": "newcomer@example.com", "name": "New"},
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
		map[string]any{"id": "g-200", "email": "joiner@example.com", "name": "Joiner"},
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
		map[string]any{"id": "g-300", "email": "client-joiner@example.com", "name": "Client"},
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
		map[string]any{"id": "g-100", "email": "newcomer2@example.com", "name": "New2"},
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
		map[string]any{"id": "g-existing", "email": "existing@example.com", "name": "Existing"},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	if !errors.Is(err, ErrOAuthLinkDisabled) {
		t.Fatalf("got %v, want ErrOAuthLinkDisabled", err)
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
		DeviceID: in.DeviceID, Platform: in.Platform, IPAddress: in.IPAddress,
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
