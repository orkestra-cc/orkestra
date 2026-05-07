package services

import (
	"context"
	"strconv"
	"strings"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/module"
)

// Defaults applied when the corresponding ConfigSchema key is unset
// or invalid. These match the values that were hardcoded before the
// admin-managed Login & Sessions tab was introduced.
const (
	defaultLockoutThreshold       = 5
	defaultLockoutDuration        = 15 * time.Minute
	defaultPasswordMinLength      = 10
	defaultPasswordMaxLength      = 128
	defaultBreachedPasswordCheck  = true
)

// PolicyAudience names the surface a policy lookup is being performed for.
// "operator" applies to /v1/auth/operator/* (Tier-1 console), "client" to
// /v1/auth/client/* (Tier-2 client SPA / API). The two surfaces share the
// same auth module config document but each schema key has an Admin/Client
// suffix so a single ConfigService read resolves the right value.
type PolicyAudience string

const (
	PolicyAudienceOperator PolicyAudience = "operator"
	PolicyAudienceClient   PolicyAudience = "client"
)

// configValueReader is the slice of ModuleConfigService the policy
// service depends on. Defined as an interface so tests can stub it
// without standing up Mongo+Redis.
type configValueReader interface {
	GetValue(ctx context.Context, moduleName, key string) string
}

// AuthPolicyService resolves admin-managed authentication policy at
// request time from the auth module's config document. Reads go through
// ModuleConfigService's Redis cache (30s TTL), so calling this on every
// signup is cheap. A nil receiver is valid — every accessor returns the
// schema default, which preserves current behaviour for consumers that
// haven't been wired to the service yet.
type AuthPolicyService struct {
	cs configValueReader
}

// NewAuthPolicyService binds the service to the live ConfigService.
// Passing nil is supported and makes every accessor fall through to its
// permissive default (matching pre-policy behaviour).
func NewAuthPolicyService(cs *module.ModuleConfigService) *AuthPolicyService {
	if cs == nil {
		return &AuthPolicyService{}
	}
	return &AuthPolicyService{cs: cs}
}

// RegistrationAllowed reports whether self-service signup is currently
// enabled for the given audience. Defaults to true when the value is
// unset so existing deployments preserve current behaviour after the
// schema migration.
func (s *AuthPolicyService) RegistrationAllowed(ctx context.Context, audience PolicyAudience) bool {
	if s == nil || s.cs == nil {
		return true
	}
	key := "registrationEnabledAdmin"
	if audience == PolicyAudienceClient {
		key = "registrationEnabledClient"
	}
	v := s.cs.GetValue(ctx, "auth", key)
	if v == "" {
		return true
	}
	return v == "true" || v == "1" || v == "yes"
}

// AllowedEmailDomains returns the lowercased domain allowlist for the
// audience, or an empty slice when the allowlist is not configured (any
// domain accepted). Domains are returned without leading dots.
func (s *AuthPolicyService) AllowedEmailDomains(ctx context.Context, audience PolicyAudience) []string {
	if s == nil || s.cs == nil {
		return nil
	}
	key := "allowedEmailDomainsAdmin"
	if audience == PolicyAudienceClient {
		key = "allowedEmailDomainsClient"
	}
	raw := s.cs.GetValue(ctx, "auth", key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		d := strings.ToLower(strings.TrimSpace(p))
		d = strings.TrimPrefix(d, ".")
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// EmailDomainAllowed checks an email's domain against the audience's
// allowlist. Returns true when the allowlist is empty (any domain) or
// when the email's domain matches one of the configured entries.
// Subdomain matches are exact — "sub.acme.com" does not match an
// "acme.com" entry — so operators add every accepted host explicitly.
func (s *AuthPolicyService) EmailDomainAllowed(ctx context.Context, audience PolicyAudience, email string) bool {
	allow := s.AllowedEmailDomains(ctx, audience)
	if len(allow) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range allow {
		if d == domain {
			return true
		}
	}
	return false
}

// LoginAllowed reports whether interactive login is currently enabled
// for the given audience. Defaults to true when unset so existing
// deployments preserve current behaviour after the schema migration.
func (s *AuthPolicyService) LoginAllowed(ctx context.Context, audience PolicyAudience) bool {
	if s == nil || s.cs == nil {
		return true
	}
	key := "loginEnabledAdmin"
	if audience == PolicyAudienceClient {
		key = "loginEnabledClient"
	}
	v := s.cs.GetValue(ctx, "auth", key)
	if v == "" {
		return true
	}
	return v == "true" || v == "1" || v == "yes"
}

// LockoutThreshold returns the number of failed login attempts (per
// IP and per email) before lockout kicks in. Falls back to 5 when
// unset or invalid.
func (s *AuthPolicyService) LockoutThreshold(ctx context.Context) int {
	if s == nil || s.cs == nil {
		return defaultLockoutThreshold
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "accountLockoutThreshold"))
	if v == "" {
		return defaultLockoutThreshold
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultLockoutThreshold
	}
	return n
}

// LockoutDuration returns how long an IP/email stays locked after
// exceeding the threshold. Falls back to 15m when unset or invalid.
func (s *AuthPolicyService) LockoutDuration(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return defaultLockoutDuration
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "accountLockoutDuration"))
	if v == "" {
		return defaultLockoutDuration
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultLockoutDuration
	}
	return d
}

// PasswordPolicy is the materialised view of the password-policy
// config keys. Resolved once per ValidatePolicy call so a single
// request reads consistent values across the length and complexity
// checks even if an admin edits the document mid-flight.
type PasswordPolicy struct {
	MinLength             int
	MaxLength             int
	RequireUpper          bool
	RequireLower          bool
	RequireDigit          bool
	RequireSymbol         bool
	BreachedPasswordCheck bool
}

// PasswordPolicy returns the live password policy. Falls back to the
// pre-policy hardcoded defaults when the service is nil or the
// underlying ConfigService is missing — preserving today's behaviour
// for any consumer that hasn't been wired to the policy yet.
func (s *AuthPolicyService) PasswordPolicy(ctx context.Context) PasswordPolicy {
	pp := PasswordPolicy{
		MinLength:             defaultPasswordMinLength,
		MaxLength:             defaultPasswordMaxLength,
		BreachedPasswordCheck: defaultBreachedPasswordCheck,
	}
	if s == nil || s.cs == nil {
		return pp
	}
	if v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "passwordMinLength")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			pp.MinLength = n
		}
	}
	if v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "passwordMaxLength")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pp.MaxLength = n
		}
	}
	// Defensive ordering — a malformed pair where min > max would let
	// every password fail length validation. Swap so callers always
	// see a usable range.
	if pp.MinLength > pp.MaxLength {
		pp.MinLength, pp.MaxLength = pp.MaxLength, pp.MinLength
	}
	pp.RequireUpper = readBool(s.cs.GetValue(ctx, "auth", "passwordRequireUpper"), false)
	pp.RequireLower = readBool(s.cs.GetValue(ctx, "auth", "passwordRequireLower"), false)
	pp.RequireDigit = readBool(s.cs.GetValue(ctx, "auth", "passwordRequireDigit"), false)
	pp.RequireSymbol = readBool(s.cs.GetValue(ctx, "auth", "passwordRequireSymbol"), false)
	pp.BreachedPasswordCheck = readBool(s.cs.GetValue(ctx, "auth", "breachedPasswordCheck"), defaultBreachedPasswordCheck)
	return pp
}

// readBool parses a config string the same way RegistrationAllowed/
// LoginAllowed do, with an explicit default for unset/empty values.
func readBool(v string, def bool) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	switch v {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return def
}

// MFAEnabled reports whether the master MFA switch is on. Defaults to
// true when unset so existing deployments preserve behaviour after the
// schema migration. The accessor is nil-safe.
func (s *AuthPolicyService) MFAEnabled(ctx context.Context) bool {
	if s == nil || s.cs == nil {
		return true
	}
	return readBool(s.cs.GetValue(ctx, "auth", "mfaEnabled"), true)
}

// MFAGraceWindow returns the configured grace period a newly privileged
// user has to enroll a second factor. Falls back to the legacy 7-day
// hardcoded window when unset, malformed, or non-positive.
func (s *AuthPolicyService) MFAGraceWindow(ctx context.Context) time.Duration {
	const def = MFAEnrollmentGraceWindow
	if s == nil || s.cs == nil {
		return def
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "mfaEnrollmentGraceDays"))
	if v == "" {
		return def
	}
	days, err := strconv.Atoi(v)
	if err != nil || days < 1 {
		return def
	}
	return time.Duration(days) * 24 * time.Hour
}

// MFARequired combines the master mfaEnabled flag with the legacy
// privileged-role check. Callers replace direct uses of the
// RoleRequiresMFA free function with this so the kill switch is
// centrally honoured. Nil receiver inherits legacy behaviour
// (master switch on, role list unchanged).
func (s *AuthPolicyService) MFARequired(user *userModels.User, memberships []authModels.OrgMembership) bool {
	if s != nil && s.cs != nil {
		// Use a Background context — this method is called from hot
		// paths that already cache the policy read via the live
		// service. Keeping the call cheap matters more than honouring
		// per-request cancellation for a config lookup.
		if !s.MFAEnabled(context.Background()) {
			return false
		}
	}
	return RoleRequiresMFA(user, memberships)
}

// MFAGraceExpired wraps GraceExpired with the configured grace window.
// Nil receiver falls back to the legacy 7-day constant.
func (s *AuthPolicyService) MFAGraceExpired(ctx context.Context, user *userModels.User, now time.Time) bool {
	if user == nil || user.MFAGraceStartedAt == nil {
		return false
	}
	return now.Sub(*user.MFAGraceStartedAt) > s.MFAGraceWindow(ctx)
}

// MFAGraceExpiresAt returns the absolute deadline a user must enroll
// by, computed against the configured grace window. Zero time when the
// grace clock hasn't started.
func (s *AuthPolicyService) MFAGraceExpiresAt(ctx context.Context, user *userModels.User) time.Time {
	if user == nil || user.MFAGraceStartedAt == nil {
		return time.Time{}
	}
	return user.MFAGraceStartedAt.Add(s.MFAGraceWindow(ctx))
}

// OAuthProviderEnabled reports whether the given provider is exposed
// on the audience's surface. Defaults to true when unset so existing
// deployments preserve behaviour after the schema migration. Unknown
// provider names always return false — admin-managed lookups do not
// fall through to "allow" for typos.
func (s *AuthPolicyService) OAuthProviderEnabled(ctx context.Context, audience PolicyAudience, provider string) bool {
	suffix := "Admin"
	if audience == PolicyAudienceClient {
		suffix = "Client"
	}
	var key string
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "google":
		key = "googleEnabled" + suffix
	case "apple":
		key = "appleEnabled" + suffix
	case "github":
		key = "githubEnabled" + suffix
	case "discord":
		key = "discordEnabled" + suffix
	default:
		return false
	}
	if s == nil || s.cs == nil {
		return true
	}
	return readBool(s.cs.GetValue(ctx, "auth", key), true)
}

// NotifyUserOnNewDeviceLogin reports whether the user should be
// emailed the first time we see a (deviceId, userUUID) pair. Default
// true so deployments that don't touch the toggle still inherit the
// safer-by-default posture.
func (s *AuthPolicyService) NotifyUserOnNewDeviceLogin(ctx context.Context) bool {
	if s == nil || s.cs == nil {
		return true
	}
	return readBool(s.cs.GetValue(ctx, "auth", "notifyUserOnNewDeviceLogin"), true)
}

// NotifyAdminOnSuspiciousLogin reports whether admins should be
// emailed in addition to the user when the risk scorer flags a login
// as high-risk. Default false so the admin email half stays inert
// until recipients are explicitly configured.
func (s *AuthPolicyService) NotifyAdminOnSuspiciousLogin(ctx context.Context) bool {
	if s == nil || s.cs == nil {
		return false
	}
	return readBool(s.cs.GetValue(ctx, "auth", "notifyAdminOnSuspiciousLogin"), false)
}

// SuspiciousLoginRecipients returns the lowercased list of admin email
// addresses to notify on high-risk login. Empty when the list is
// unset — the admin-email half short-circuits in that case so a stray
// toggle without a recipient list never silently swallows alerts.
func (s *AuthPolicyService) SuspiciousLoginRecipients(ctx context.Context) []string {
	if s == nil || s.cs == nil {
		return nil
	}
	return splitTrimLower(s.cs.GetValue(ctx, "auth", "suspiciousLoginRecipients"))
}

// IPAllowlistOperator returns the configured CIDR allowlist for the
// operator host. The middleware compiles these strings to net.IPNet
// values; empty here means the allowlist is open (every IP is
// accepted unless caught by the blocklist).
func (s *AuthPolicyService) IPAllowlistOperator(ctx context.Context) []string {
	if s == nil || s.cs == nil {
		return nil
	}
	return splitTrim(s.cs.GetValue(ctx, "auth", "ipAllowlistAdmin"))
}

// IPBlocklistOperator returns the configured CIDR blocklist for the
// operator host. Evaluated after the allowlist — a blocked entry
// rejects regardless of allowlist membership.
func (s *AuthPolicyService) IPBlocklistOperator(ctx context.Context) []string {
	if s == nil || s.cs == nil {
		return nil
	}
	return splitTrim(s.cs.GetValue(ctx, "auth", "ipBlocklistAdmin"))
}

// GeoBlockCountries returns the uppercased ISO-3166-1 alpha-2 country
// codes that should be denied at login time. Returns an empty slice
// when unset or when the policy or its config-service is missing — the
// caller treats that as "no geo gate".
func (s *AuthPolicyService) GeoBlockCountries(ctx context.Context) []string {
	if s == nil || s.cs == nil {
		return nil
	}
	raw := splitTrim(s.cs.GetValue(ctx, "auth", "geoBlockCountries"))
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		c := strings.ToUpper(r)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// CountryBlocked reports whether the given ISO country code is on the
// configured blocklist. Empty input or empty list always returns
// false (= not blocked) so an unresolved IP doesn't lock anyone out.
func (s *AuthPolicyService) CountryBlocked(ctx context.Context, country string) bool {
	c := strings.ToUpper(strings.TrimSpace(country))
	if c == "" {
		return false
	}
	for _, b := range s.GeoBlockCountries(ctx) {
		if b == c {
			return true
		}
	}
	return false
}

// RevokeSessionsOnPasswordChange reports whether a successful password
// change should also revoke the caller's session id + device-trust
// grants. Defaults to true so today's behaviour is preserved when the
// toggle isn't configured.
func (s *AuthPolicyService) RevokeSessionsOnPasswordChange(ctx context.Context) bool {
	if s == nil || s.cs == nil {
		return true
	}
	return readBool(s.cs.GetValue(ctx, "auth", "revokeSessionsOnPasswordChange"), true)
}

// SelfServiceAccountDeletionClient reports whether Tier-2 client users
// can call /v1/me/dsr/erase. Defaults to false so today's behaviour
// (operator-only erasure) is preserved unless an admin explicitly
// opens the surface up.
func (s *AuthPolicyService) SelfServiceAccountDeletionClient(ctx context.Context) bool {
	if s == nil || s.cs == nil {
		return false
	}
	return readBool(s.cs.GetValue(ctx, "auth", "selfServiceAccountDeletionClient"), false)
}

// InactiveAccountAutoDisableDays returns the configured stale-login
// threshold in days. 0 (or unset / negative / malformed) means the
// inactive-account-disable feature is off.
func (s *AuthPolicyService) InactiveAccountAutoDisableDays(ctx context.Context) int {
	if s == nil || s.cs == nil {
		return 0
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "inactiveAccountAutoDisableDays"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// splitTrim splits a comma-separated config value, trims whitespace,
// and drops empty entries. Used for stringList accessors that don't
// need case folding.
func splitTrim(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// splitTrimLower is splitTrim plus ToLower — used for case-insensitive
// list values like email addresses where Acme.com and acme.com must
// compare equal.
func splitTrimLower(raw string) []string {
	out := splitTrim(raw)
	for i := range out {
		out[i] = strings.ToLower(out[i])
	}
	return out
}

// DefaultClientRole returns the system role assigned to a new Tier-2
// client signup. Falls back to "operator" (today's hard-coded default
// in PasswordAuthService.Register) when unset or invalid.
func (s *AuthPolicyService) DefaultClientRole(ctx context.Context) string {
	const fallback = "operator"
	if s == nil || s.cs == nil {
		return fallback
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "defaultRoleClient"))
	switch v {
	case "operator", "manager", "guest":
		return v
	}
	return fallback
}
