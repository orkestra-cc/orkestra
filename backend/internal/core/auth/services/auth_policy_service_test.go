package services

import (
	"context"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// stubReader satisfies the configValueReader interface used by
// AuthPolicyService. Tests inject keyed values directly so no Mongo
// or Redis is required.
type stubReader struct {
	values map[string]string
}

func (s *stubReader) GetValue(_ context.Context, _, key string) string {
	if s == nil {
		return ""
	}
	return s.values[key]
}

func newPolicy(values map[string]string) *AuthPolicyService {
	return &AuthPolicyService{cs: &stubReader{values: values}}
}

func TestRegistrationAllowed_NilService_DefaultsTrue(t *testing.T) {
	var p *AuthPolicyService
	if !p.RegistrationAllowed(context.Background(), PolicyAudienceOperator) {
		t.Fatalf("nil policy must allow registration")
	}
}

func TestRegistrationAllowed_NoConfig_DefaultsTrue(t *testing.T) {
	p := newPolicy(nil)
	if !p.RegistrationAllowed(context.Background(), PolicyAudienceOperator) {
		t.Fatalf("empty config must allow registration")
	}
	if !p.RegistrationAllowed(context.Background(), PolicyAudienceClient) {
		t.Fatalf("empty config must allow client registration")
	}
}

func TestRegistrationAllowed_PerAudience(t *testing.T) {
	p := newPolicy(map[string]string{
		"registrationEnabledAdmin":  "false",
		"registrationEnabledClient": "true",
	})
	if p.RegistrationAllowed(context.Background(), PolicyAudienceOperator) {
		t.Fatalf("operator registration should be disabled")
	}
	if !p.RegistrationAllowed(context.Background(), PolicyAudienceClient) {
		t.Fatalf("client registration should remain enabled")
	}
}

func TestRegistrationAllowed_Truthy(t *testing.T) {
	for _, v := range []string{"true", "1", "yes"} {
		p := newPolicy(map[string]string{"registrationEnabledClient": v})
		if !p.RegistrationAllowed(context.Background(), PolicyAudienceClient) {
			t.Fatalf("value %q should be truthy", v)
		}
	}
	for _, v := range []string{"false", "0", "no", "off"} {
		p := newPolicy(map[string]string{"registrationEnabledClient": v})
		if p.RegistrationAllowed(context.Background(), PolicyAudienceClient) {
			t.Fatalf("value %q should be falsy", v)
		}
	}
}

func TestEmailDomainAllowed_EmptyAllowlist(t *testing.T) {
	p := newPolicy(nil)
	if !p.EmailDomainAllowed(context.Background(), PolicyAudienceOperator, "anyone@example.com") {
		t.Fatalf("empty allowlist should permit any domain")
	}
}

func TestEmailDomainAllowed_PerAudience(t *testing.T) {
	p := newPolicy(map[string]string{
		"allowedEmailDomainsAdmin":  "Acme.com,  ops.acme.com ",
		"allowedEmailDomainsClient": "client.io",
	})
	ctx := context.Background()

	cases := []struct {
		audience PolicyAudience
		email    string
		want     bool
	}{
		{PolicyAudienceOperator, "alice@acme.com", true},     // case-insensitive match
		{PolicyAudienceOperator, "bob@OPS.ACME.COM", true},   // case-insensitive match
		{PolicyAudienceOperator, "evil@attacker.com", false}, // not in allowlist
		{PolicyAudienceOperator, "noatsign", false},          // malformed
		{PolicyAudienceOperator, "trailing@", false},         // empty domain
		{PolicyAudienceOperator, "sub.alice@sub.acme.com", false}, // exact match only
		{PolicyAudienceClient, "user@client.io", true},
		{PolicyAudienceClient, "user@acme.com", false}, // different audience
	}
	for _, tc := range cases {
		got := p.EmailDomainAllowed(ctx, tc.audience, tc.email)
		if got != tc.want {
			t.Errorf("EmailDomainAllowed(%s, %q) = %v, want %v", tc.audience, tc.email, got, tc.want)
		}
	}
}

func TestAllowedEmailDomains_TrimmingAndDotPrefix(t *testing.T) {
	p := newPolicy(map[string]string{
		"allowedEmailDomainsAdmin": " .acme.com , ,  beta.io  ",
	})
	got := p.AllowedEmailDomains(context.Background(), PolicyAudienceOperator)
	want := []string{"acme.com", "beta.io"}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d (%v)", len(want), len(got), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("entry %d: got %q, want %q", i, got[i], v)
		}
	}
}

func TestLoginAllowed_NilService_DefaultsTrue(t *testing.T) {
	var p *AuthPolicyService
	if !p.LoginAllowed(context.Background(), PolicyAudienceOperator) {
		t.Fatalf("nil policy must allow login")
	}
}

func TestLoginAllowed_PerAudience(t *testing.T) {
	p := newPolicy(map[string]string{
		"loginEnabledAdmin":  "false",
		"loginEnabledClient": "true",
	})
	if p.LoginAllowed(context.Background(), PolicyAudienceOperator) {
		t.Fatalf("operator login should be disabled")
	}
	if !p.LoginAllowed(context.Background(), PolicyAudienceClient) {
		t.Fatalf("client login should remain enabled")
	}
}

func TestLockoutThreshold(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want int
	}{
		{"unset falls back", nil, 5},
		{"empty falls back", map[string]string{"accountLockoutThreshold": ""}, 5},
		{"valid value", map[string]string{"accountLockoutThreshold": "10"}, 10},
		{"non-numeric falls back", map[string]string{"accountLockoutThreshold": "many"}, 5},
		{"zero rejected", map[string]string{"accountLockoutThreshold": "0"}, 5},
		{"negative rejected", map[string]string{"accountLockoutThreshold": "-3"}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPolicy(tc.set)
			if got := p.LockoutThreshold(context.Background()); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLockoutDuration(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want time.Duration
	}{
		{"unset falls back", nil, 15 * time.Minute},
		{"empty falls back", map[string]string{"accountLockoutDuration": ""}, 15 * time.Minute},
		{"valid 30m", map[string]string{"accountLockoutDuration": "30m"}, 30 * time.Minute},
		{"valid 1h", map[string]string{"accountLockoutDuration": "1h"}, time.Hour},
		{"malformed falls back", map[string]string{"accountLockoutDuration": "forever"}, 15 * time.Minute},
		{"zero falls back", map[string]string{"accountLockoutDuration": "0s"}, 15 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPolicy(tc.set)
			if got := p.LockoutDuration(context.Background()); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPasswordPolicy_NilService_LegacyDefaults(t *testing.T) {
	var p *AuthPolicyService
	pp := p.PasswordPolicy(context.Background())
	if pp.MinLength != 10 || pp.MaxLength != 128 {
		t.Fatalf("nil policy must return legacy 10..128, got %d..%d", pp.MinLength, pp.MaxLength)
	}
	if !pp.BreachedPasswordCheck {
		t.Fatalf("HIBP must default to true")
	}
	if pp.RequireUpper || pp.RequireLower || pp.RequireDigit || pp.RequireSymbol {
		t.Fatalf("complexity flags must default to false")
	}
}

func TestPasswordPolicy_OverrideAndSwap(t *testing.T) {
	p := newPolicy(map[string]string{
		"passwordMinLength":     "16",
		"passwordMaxLength":     "64",
		"passwordRequireUpper":  "true",
		"passwordRequireSymbol": "yes",
		"breachedPasswordCheck": "false",
	})
	pp := p.PasswordPolicy(context.Background())
	if pp.MinLength != 16 || pp.MaxLength != 64 {
		t.Errorf("min/max: got %d..%d, want 16..64", pp.MinLength, pp.MaxLength)
	}
	if !pp.RequireUpper || !pp.RequireSymbol {
		t.Errorf("complexity flags not honored: %+v", pp)
	}
	if pp.RequireLower || pp.RequireDigit {
		t.Errorf("unset complexity flags should remain false: %+v", pp)
	}
	if pp.BreachedPasswordCheck {
		t.Errorf("HIBP should be off")
	}
}

func TestPasswordPolicy_MalformedFallsBackThenSwapsRange(t *testing.T) {
	// Malformed/zero values are ignored; an inverted range is swapped
	// so callers always see a usable [min, max].
	p := newPolicy(map[string]string{
		"passwordMinLength": "200",
		"passwordMaxLength": "20",
	})
	pp := p.PasswordPolicy(context.Background())
	if pp.MinLength != 20 || pp.MaxLength != 200 {
		t.Errorf("inverted range should be swapped, got %d..%d", pp.MinLength, pp.MaxLength)
	}

	p2 := newPolicy(map[string]string{
		"passwordMinLength": "garbage",
		"passwordMaxLength": "0",
	})
	pp2 := p2.PasswordPolicy(context.Background())
	if pp2.MinLength != 10 || pp2.MaxLength != 128 {
		t.Errorf("malformed values should fall back to legacy defaults, got %d..%d", pp2.MinLength, pp2.MaxLength)
	}
}

func TestOAuthProviderEnabled_NilService_DefaultsTrue(t *testing.T) {
	var p *AuthPolicyService
	for _, prov := range []string{"google", "apple", "github", "discord"} {
		if !p.OAuthProviderEnabled(context.Background(), PolicyAudienceOperator, prov) {
			t.Errorf("nil policy must allow %q", prov)
		}
	}
}

func TestOAuthProviderEnabled_UnknownProviderRejected(t *testing.T) {
	// An unknown provider name has no schema key — must always return
	// false rather than fall through to a permissive default.
	var p *AuthPolicyService
	if p.OAuthProviderEnabled(context.Background(), PolicyAudienceOperator, "facebook") {
		t.Fatalf("unknown provider must return false even with nil policy")
	}
	p2 := newPolicy(map[string]string{"googleEnabledAdmin": "true"})
	if p2.OAuthProviderEnabled(context.Background(), PolicyAudienceOperator, "wizard") {
		t.Fatalf("unknown provider must return false even with policy wired")
	}
}

func TestOAuthProviderEnabled_PerAudience(t *testing.T) {
	p := newPolicy(map[string]string{
		"googleEnabledAdmin":  "false",
		"googleEnabledClient": "true",
		"appleEnabledAdmin":   "true",
		"appleEnabledClient":  "false",
	})
	ctx := context.Background()
	cases := []struct {
		audience PolicyAudience
		provider string
		want     bool
	}{
		{PolicyAudienceOperator, "google", false},
		{PolicyAudienceClient, "google", true},
		{PolicyAudienceOperator, "apple", true},
		{PolicyAudienceClient, "apple", false},
		// Unset providers default to true so existing deployments
		// preserve behaviour after the schema migration.
		{PolicyAudienceOperator, "github", true},
		{PolicyAudienceClient, "discord", true},
	}
	for _, tc := range cases {
		got := p.OAuthProviderEnabled(ctx, tc.audience, tc.provider)
		if got != tc.want {
			t.Errorf("audience=%s provider=%s: got %v, want %v", tc.audience, tc.provider, got, tc.want)
		}
	}
}

func TestOAuthProviderEnabled_CaseInsensitiveProvider(t *testing.T) {
	p := newPolicy(map[string]string{"googleEnabledAdmin": "false"})
	ctx := context.Background()
	for _, name := range []string{"google", "Google", "GOOGLE", " google "} {
		if p.OAuthProviderEnabled(ctx, PolicyAudienceOperator, name) {
			t.Errorf("expected %q to be disabled (case/space-insensitive)", name)
		}
	}
}

func TestMFAEnabled_NilService_DefaultsTrue(t *testing.T) {
	var p *AuthPolicyService
	if !p.MFAEnabled(context.Background()) {
		t.Fatalf("nil policy must report MFA enabled")
	}
}

func TestMFAEnabled_OffPath(t *testing.T) {
	p := newPolicy(map[string]string{"mfaEnabled": "false"})
	if p.MFAEnabled(context.Background()) {
		t.Fatalf("mfaEnabled=false must report MFA disabled")
	}
}

func TestMFAGraceWindow(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want time.Duration
	}{
		{"unset falls back to 7d", nil, 7 * 24 * time.Hour},
		{"empty falls back to 7d", map[string]string{"mfaEnrollmentGraceDays": ""}, 7 * 24 * time.Hour},
		{"3 days", map[string]string{"mfaEnrollmentGraceDays": "3"}, 3 * 24 * time.Hour},
		{"30 days", map[string]string{"mfaEnrollmentGraceDays": "30"}, 30 * 24 * time.Hour},
		{"non-numeric falls back", map[string]string{"mfaEnrollmentGraceDays": "abc"}, 7 * 24 * time.Hour},
		{"zero rejected", map[string]string{"mfaEnrollmentGraceDays": "0"}, 7 * 24 * time.Hour},
		{"negative rejected", map[string]string{"mfaEnrollmentGraceDays": "-5"}, 7 * 24 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPolicy(tc.set)
			if got := p.MFAGraceWindow(context.Background()); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMFARequired_KillSwitchSuppressesPrivilegedRole(t *testing.T) {
	admin := &userModels.User{Role: SystemRoleAdministrator}
	// Sanity: with no policy, privileged role still requires MFA.
	var nilPolicy *AuthPolicyService
	if !nilPolicy.MFARequired(admin, nil) {
		t.Fatalf("nil policy: administrator must require MFA")
	}
	// With mfaEnabled=false, the kill switch suppresses the requirement.
	p := newPolicy(map[string]string{"mfaEnabled": "false"})
	if p.MFARequired(admin, nil) {
		t.Fatalf("mfaEnabled=false: administrator must NOT require MFA")
	}
	// With mfaEnabled=true, behaves like the legacy free function.
	p2 := newPolicy(map[string]string{"mfaEnabled": "true"})
	if !p2.MFARequired(admin, nil) {
		t.Fatalf("mfaEnabled=true: administrator must require MFA")
	}
}

func TestMFAGraceExpired_UsesConfiguredWindow(t *testing.T) {
	now := time.Now()
	// User started grace 5 days ago.
	started := now.Add(-5 * 24 * time.Hour)
	user := &userModels.User{MFAGraceStartedAt: &started}

	// Default 7-day window: not expired.
	var nilPolicy *AuthPolicyService
	if nilPolicy.MFAGraceExpired(context.Background(), user, now) {
		t.Fatalf("legacy 7-day window: 5d-old grace must not be expired")
	}
	// Tighten to 3 days: expired.
	tight := newPolicy(map[string]string{"mfaEnrollmentGraceDays": "3"})
	if !tight.MFAGraceExpired(context.Background(), user, now) {
		t.Fatalf("3-day window: 5d-old grace must be expired")
	}
	// Loosen to 30 days: not expired.
	loose := newPolicy(map[string]string{"mfaEnrollmentGraceDays": "30"})
	if loose.MFAGraceExpired(context.Background(), user, now) {
		t.Fatalf("30-day window: 5d-old grace must not be expired")
	}
}

func TestMFAGraceExpiresAt_NilUser(t *testing.T) {
	p := newPolicy(map[string]string{"mfaEnrollmentGraceDays": "10"})
	if got := p.MFAGraceExpiresAt(context.Background(), nil); !got.IsZero() {
		t.Errorf("nil user must return zero time, got %v", got)
	}
	user := &userModels.User{} // no grace start
	if got := p.MFAGraceExpiresAt(context.Background(), user); !got.IsZero() {
		t.Errorf("user without grace start must return zero time, got %v", got)
	}
}

// TestMFARequired_OrgRoleStillRequiresMFA pins behaviour: even when only
// an org_owner membership grants the privilege, the kill switch must
// still suppress the requirement — guards against a regression where
// the kill switch was bypassed via the role-list path.
func TestMFARequired_OrgRoleHonoursKillSwitch(t *testing.T) {
	user := &userModels.User{Role: "operator"} // non-privileged system role
	memberships := []authModels.OrgMembership{
		{Roles: []string{OrgRoleOwner}},
	}
	if !(*AuthPolicyService)(nil).MFARequired(user, memberships) {
		t.Fatalf("nil policy: org_owner must require MFA via legacy path")
	}
	p := newPolicy(map[string]string{"mfaEnabled": "false"})
	if p.MFARequired(user, memberships) {
		t.Fatalf("mfaEnabled=false: org_owner must NOT require MFA")
	}
}

// Phase 7: Anti-abuse & Notifications policy accessors.

func TestNotifyUserOnNewDeviceLogin_DefaultsTrue(t *testing.T) {
	var p *AuthPolicyService
	if !p.NotifyUserOnNewDeviceLogin(context.Background()) {
		t.Fatalf("nil policy must default to notify=true")
	}
	if !newPolicy(nil).NotifyUserOnNewDeviceLogin(context.Background()) {
		t.Fatalf("empty config must default to notify=true")
	}
	off := newPolicy(map[string]string{"notifyUserOnNewDeviceLogin": "false"})
	if off.NotifyUserOnNewDeviceLogin(context.Background()) {
		t.Fatalf("explicit false must disable")
	}
}

func TestNotifyAdminOnSuspiciousLogin_DefaultsFalse(t *testing.T) {
	var p *AuthPolicyService
	if p.NotifyAdminOnSuspiciousLogin(context.Background()) {
		t.Fatalf("nil policy must default to false (no admin emails)")
	}
	on := newPolicy(map[string]string{"notifyAdminOnSuspiciousLogin": "yes"})
	if !on.NotifyAdminOnSuspiciousLogin(context.Background()) {
		t.Fatalf("yes must enable admin emails")
	}
}

func TestSuspiciousLoginRecipients_LowercasedAndTrimmed(t *testing.T) {
	p := newPolicy(map[string]string{
		"suspiciousLoginRecipients": "Alice@Example.com,  ,bob@beta.io  ",
	})
	got := p.SuspiciousLoginRecipients(context.Background())
	want := []string{"alice@example.com", "bob@beta.io"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d (%v)", len(got), len(want), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("entry %d: got %q want %q", i, got[i], v)
		}
	}
}

func TestSuspiciousLoginRecipients_NilWhenUnset(t *testing.T) {
	if got := newPolicy(nil).SuspiciousLoginRecipients(context.Background()); len(got) != 0 {
		t.Fatalf("unset must return empty, got %v", got)
	}
}

func TestIPLists_TrimAndDropEmpty(t *testing.T) {
	p := newPolicy(map[string]string{
		"ipAllowlistAdmin": " 10.0.0.0/8 ,, 192.0.2.5/32",
		"ipBlocklistAdmin": "203.0.113.0/24",
	})
	allow := p.IPAllowlistOperator(context.Background())
	block := p.IPBlocklistOperator(context.Background())
	if len(allow) != 2 || allow[0] != "10.0.0.0/8" || allow[1] != "192.0.2.5/32" {
		t.Errorf("allow: got %v", allow)
	}
	if len(block) != 1 || block[0] != "203.0.113.0/24" {
		t.Errorf("block: got %v", block)
	}
}

func TestGeoBlockCountries_UppercasedAndTrimmed(t *testing.T) {
	p := newPolicy(map[string]string{
		"geoBlockCountries": " ru, kp ,, Cn ",
	})
	got := p.GeoBlockCountries(context.Background())
	want := []string{"RU", "KP", "CN"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("entry %d: got %q want %q", i, got[i], v)
		}
	}
}

func TestCountryBlocked(t *testing.T) {
	p := newPolicy(map[string]string{"geoBlockCountries": "ru, kp"})
	ctx := context.Background()
	cases := []struct {
		input string
		want  bool
	}{
		{"RU", true},
		{"ru", true}, // case-insensitive
		{" RU ", true},
		{"US", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := p.CountryBlocked(ctx, tc.input); got != tc.want {
			t.Errorf("CountryBlocked(%q) = %v want %v", tc.input, got, tc.want)
		}
	}
	// Nil policy and empty list both fail open.
	var nilP *AuthPolicyService
	if nilP.CountryBlocked(ctx, "RU") {
		t.Errorf("nil policy must not block")
	}
	if newPolicy(nil).CountryBlocked(ctx, "RU") {
		t.Errorf("empty list must not block")
	}
}

func TestInactiveAccountAutoDisableDays(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want int
	}{
		{"unset → 0 (disabled)", nil, 0},
		{"empty → 0", map[string]string{"inactiveAccountAutoDisableDays": ""}, 0},
		{"valid 30", map[string]string{"inactiveAccountAutoDisableDays": "30"}, 30},
		{"non-numeric → 0", map[string]string{"inactiveAccountAutoDisableDays": "abc"}, 0},
		{"zero → 0", map[string]string{"inactiveAccountAutoDisableDays": "0"}, 0},
		{"negative → 0", map[string]string{"inactiveAccountAutoDisableDays": "-5"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPolicy(tc.set)
			if got := p.InactiveAccountAutoDisableDays(context.Background()); got != tc.want {
				t.Errorf("got %d want %d", got, tc.want)
			}
		})
	}
}

// Phase 8: trivial toggles.

func TestRevokeSessionsOnPasswordChange_DefaultsTrue(t *testing.T) {
	var p *AuthPolicyService
	if !p.RevokeSessionsOnPasswordChange(context.Background()) {
		t.Fatalf("nil policy must default to true (preserve current behaviour)")
	}
	if !newPolicy(nil).RevokeSessionsOnPasswordChange(context.Background()) {
		t.Fatalf("empty config must default to true")
	}
	off := newPolicy(map[string]string{"revokeSessionsOnPasswordChange": "false"})
	if off.RevokeSessionsOnPasswordChange(context.Background()) {
		t.Fatalf("explicit false must opt out")
	}
}

func TestSelfServiceAccountDeletionClient_DefaultsFalse(t *testing.T) {
	var p *AuthPolicyService
	if p.SelfServiceAccountDeletionClient(context.Background()) {
		t.Fatalf("nil policy must default to false (operator-only deletion)")
	}
	on := newPolicy(map[string]string{"selfServiceAccountDeletionClient": "yes"})
	if !on.SelfServiceAccountDeletionClient(context.Background()) {
		t.Fatalf("yes must enable the client surface")
	}
}

func TestDefaultClientRole_FallbackAndOverride(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want string
	}{
		{"unset falls back to operator", nil, "operator"},
		{"empty falls back to operator", map[string]string{"defaultRoleClient": ""}, "operator"},
		{"valid manager", map[string]string{"defaultRoleClient": "manager"}, "manager"},
		{"valid guest", map[string]string{"defaultRoleClient": "guest"}, "guest"},
		{"invalid role rejected", map[string]string{"defaultRoleClient": "super_admin"}, "operator"},
		{"unknown role rejected", map[string]string{"defaultRoleClient": "wizard"}, "operator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPolicy(tc.set)
			if got := p.DefaultClientRole(context.Background()); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
