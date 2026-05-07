package auth

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/orkestra/backend/internal/core/auth/handlers"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/geoip"
	"github.com/orkestra/backend/internal/shared/iface"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type AuthModule struct {
	module.BaseModule

	// deviceTrust is a single non-tier-split collection so one handler
	// is reused across both operator and client mounts.
	deviceTrustHandler *handlers.DeviceTrustHandler

	// ADR-0003 PR-D: operator-tier handler instances bound to the
	// operator authTierBundle. Mounted under /v1/auth/operator/...
	// The operator AuthHandler also owns the single shared OAuth
	// callback URL — its tierDispatch map routes callbacks to the
	// matching tier's authService. webauthn handler stays nil when
	// passkeys are disabled at boot.
	operatorAuthHandler     *handlers.AuthHandler
	operatorPasswordHandler *handlers.PasswordAuthHandler
	operatorMFAHandler      *handlers.MFAHandler
	operatorWebAuthnHandler *handlers.WebAuthnHandler

	// ADR-0003 PR-D D-5: client-tier handler instances bound to the
	// client authTierBundle. Same shape as the operator block above but
	// tied to client_* collections + a JWT service that stamps
	// aud=client on every minted token, so /v1/auth/client/* requests
	// produce client-audience access + refresh tokens that only the
	// client host mux accepts.
	clientAuthHandler     *handlers.AuthHandler
	clientPasswordHandler *handlers.PasswordAuthHandler
	clientMFAHandler      *handlers.MFAHandler
	clientWebAuthnHandler *handlers.WebAuthnHandler
}

func NewModule() *AuthModule { return &AuthModule{} }

func (m *AuthModule) Name() string        { return "auth" }
func (m *AuthModule) DisplayName() string  { return "Authentication" }
func (m *AuthModule) Description() string  { return "OAuth 2.1, JWT, sessions, RBAC" }

func (m *AuthModule) Dependencies() []string { return []string{"user", "notification", "tenant", "authz"} }
func (m *AuthModule) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceUserService, module.ServiceTenantProvider}
}
func (m *AuthModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceNotificationSender}
}
func (m *AuthModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServiceAuthService,
		module.ServiceJWTService,
		module.ServicePasswordService,
		module.ServicePasswordAuthService,
		module.ServiceSessionRevocation,
	}
}

func (m *AuthModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "auth.self", Module: "auth", Description: "Edit your own password and sessions"},
		{Key: "auth.mfa.self", Module: "auth", Description: "Enroll, verify, and remove your own MFA factors"},
		{Key: "system.users.mfa_reset", Module: "auth", Description: "Admin: reset another user's MFA factors"},
	}
}

// ConfigSchema declares every OAuth provider setting as admin-manageable.
// Values are seeded from the listed env vars on first boot, then owned by
// the module_configs document in MongoDB. Secrets are encrypted at rest.
// The Group field drives the admin modal's tab rendering — fields in the
// same group land on the same tab, in declaration order.
func (m *AuthModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		// Google
		{Key: "googleClientId", Label: "Client ID", Group: "Google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_CLIENT_ID"},
		{Key: "googleClientSecret", Label: "Client Secret", Group: "Google", Type: module.FieldSecret, EnvVar: "OAUTH_GOOGLE_CLIENT_SECRET"},
		{Key: "googleRedirectURL", Label: "Redirect URL", Group: "Google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_REDIRECT_URL"},
		{Key: "googleAndroidClientId", Label: "Android Client ID", Group: "Google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_ANDROID_CLIENT_ID"},
		{Key: "googleIOSClientId", Label: "iOS Client ID", Group: "Google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_IOS_CLIENT_ID"},

		// Apple
		{Key: "appleClientId", Label: "Client ID", Group: "Apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_CLIENT_ID"},
		{Key: "appleTeamId", Label: "Team ID", Group: "Apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_TEAM_ID"},
		{Key: "appleKeyId", Label: "Key ID", Group: "Apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_KEY_ID"},
		{Key: "applePrivateKey", Label: ".p8 Key (PEM)", Group: "Apple", Description: "Inline PEM content of your Apple Sign-In .p8 key", Type: module.FieldSecret, EnvVar: "OAUTH_APPLE_PRIVATE_KEY"},
		{Key: "applePrivateKeyPath", Label: ".p8 Key Path", Group: "Apple", Description: "Filesystem path fallback if PEM is not inlined", Type: module.FieldString, EnvVar: "OAUTH_APPLE_PRIVATE_KEY_PATH"},
		{Key: "appleRedirectURL", Label: "Redirect URL", Group: "Apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_REDIRECT_URL"},
		{Key: "appleIOSClientId", Label: "iOS Client ID", Group: "Apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_IOS_CLIENT_ID"},
		{Key: "appleAndroidClientId", Label: "Android Client ID", Group: "Apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_ANDROID_CLIENT_ID"},

		// GitHub
		{Key: "githubClientId", Label: "Client ID", Group: "GitHub", Type: module.FieldString, EnvVar: "OAUTH_GITHUB_CLIENT_ID"},
		{Key: "githubClientSecret", Label: "Client Secret", Group: "GitHub", Type: module.FieldSecret, EnvVar: "OAUTH_GITHUB_CLIENT_SECRET"},
		{Key: "githubRedirectURL", Label: "Redirect URL", Group: "GitHub", Type: module.FieldString, EnvVar: "OAUTH_GITHUB_REDIRECT_URL"},

		// Discord
		{Key: "discordClientId", Label: "Client ID", Group: "Discord", Type: module.FieldString, EnvVar: "OAUTH_DISCORD_CLIENT_ID"},
		{Key: "discordClientSecret", Label: "Client Secret", Group: "Discord", Type: module.FieldSecret, EnvVar: "OAUTH_DISCORD_CLIENT_SECRET"},
		{Key: "discordRedirectURL", Label: "Redirect URL", Group: "Discord", Type: module.FieldString, EnvVar: "OAUTH_DISCORD_REDIRECT_URL"},

		// Registration — tier-aware site-wide signup policy. Read at
		// request time by AuthPolicyService; edits via the admin UI take
		// effect on the next signup with no restart.
		{
			Key: "registrationEnabledAdmin", Label: "Allow signups on operator console", Group: "Registration",
			Description: "When off, POST /v1/auth/operator/register returns 403. Operator accounts must be invited or created via /admin.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "registrationEnabledClient", Label: "Allow signups on client app", Group: "Registration",
			Description: "When off, POST /v1/auth/client/register returns 403. Tier-2 clients can no longer self-register.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "defaultRoleClient", Label: "Default role for new client signups", Group: "Registration",
			Description: "System role assigned to a Tier-2 client account on signup. Lower-privilege roles are recommended.",
			Type:        module.FieldEnum, Default: "operator",
			Options:     []string{"operator", "manager", "guest"},
		},
		{
			Key: "allowedEmailDomainsAdmin", Label: "Allowed email domains (operator)", Group: "Registration",
			Description: "Comma-separated allowlist (e.g. acme.com, ops.acme.com). Empty = any domain. Applied only to /v1/auth/operator/register.",
			Type:        module.FieldStringList,
		},
		{
			Key: "allowedEmailDomainsClient", Label: "Allowed email domains (client)", Group: "Registration",
			Description: "Comma-separated allowlist applied only to /v1/auth/client/register. Empty = any domain.",
			Type:        module.FieldStringList,
		},

		// Login & Sessions — per-surface kill switches + lockout policy.
		// Read at request time by AuthPolicyService; lockout values flow
		// into shared/errors.RateLimiter via SetAuthFailedConfig before
		// each login attempt so admin edits take effect on the next try.
		{
			Key: "loginEnabledAdmin", Label: "Allow logins on operator console", Group: "Login & Sessions",
			Description: "When off, POST /v1/auth/operator/login returns 403. Use during maintenance to lock out the operator console without taking the backend offline.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "loginEnabledClient", Label: "Allow logins on client app", Group: "Login & Sessions",
			Description: "When off, POST /v1/auth/client/login returns 403. Affects /v1/auth/client/* only.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "accountLockoutThreshold", Label: "Failed login attempts before lockout", Group: "Login & Sessions",
			Description: "Number of failed login attempts (per IP and per email) before the account is temporarily locked. Default 5.",
			Type:        module.FieldInt, Default: "5",
		},
		{
			Key: "accountLockoutDuration", Label: "Lockout duration", Group: "Login & Sessions",
			Description: "Go duration string (e.g. 15m, 1h) — how long an IP/email stays locked after exceeding the threshold. Default 15m.",
			Type:        module.FieldDuration, Default: "15m",
		},

		// Password Policy — site-wide rules enforced by passwordService.
		// ValidatePolicy on signup / change-password / reset. Defaults
		// match the legacy hardcoded behaviour (10..128 chars, no
		// complexity, HIBP on) so existing deployments observe no change
		// after the migration.
		{
			Key: "passwordMinLength", Label: "Minimum length", Group: "Password Policy",
			Description: "Minimum number of characters in a new password. Default 10. Recommend 12+.",
			Type:        module.FieldInt, Default: "10",
		},
		{
			Key: "passwordMaxLength", Label: "Maximum length", Group: "Password Policy",
			Description: "Upper bound on password length. Argon2id is not a bottleneck; raise this only if you have a concrete reason.",
			Type:        module.FieldInt, Default: "128",
		},
		{
			Key: "passwordRequireUpper", Label: "Require an uppercase letter", Group: "Password Policy",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "passwordRequireLower", Label: "Require a lowercase letter", Group: "Password Policy",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "passwordRequireDigit", Label: "Require a digit", Group: "Password Policy",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "passwordRequireSymbol", Label: "Require a symbol", Group: "Password Policy",
			Description: "Any non-alphanumeric character.",
			Type:        module.FieldBool, Default: "false",
		},
		{
			Key: "breachedPasswordCheck", Label: "Reject breached passwords (HIBP)", Group: "Password Policy",
			Description: "k-anonymity lookup against haveibeenpwned.com — only the first 5 chars of the SHA-1 hash leave the server. Disable for air-gapped deployments.",
			Type:        module.FieldBool, Default: "true",
		},

		// OAuth Providers — per-surface enable. The credential fields
		// stay where they are (one set per provider, shared across
		// audiences) but each provider can be exposed only on the
		// surfaces that should accept it. A provider that is configured
		// but disabled for a surface is filtered out of GET
		// /v1/auth/{tier}/providers and returns 403 oauth_disabled
		// from the start endpoints.
		{
			Key: "googleEnabledAdmin", Label: "Google on operator console", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "googleEnabledClient", Label: "Google on client app", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "appleEnabledAdmin", Label: "Apple on operator console", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "appleEnabledClient", Label: "Apple on client app", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "githubEnabledAdmin", Label: "GitHub on operator console", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "githubEnabledClient", Label: "GitHub on client app", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "discordEnabledAdmin", Label: "Discord on operator console", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},
		{
			Key: "discordEnabledClient", Label: "Discord on client app", Group: "OAuth Providers",
			Type: module.FieldBool, Default: "true",
		},

		// MFA — global feature flag + grace window. The privileged-role
		// list (super_admin / administrator / org_owner / org_admin) is
		// still hardcoded in services/mfa_policy.go; that's a follow-up
		// once we agree on UX for editing it. For today, operators can:
		//   - flip the master switch off in an emergency (existing
		//     enrollments stay intact; users can still verify
		//     voluntarily, but RoleRequiresMFA returns false)
		//   - tune how long a freshly-promoted admin has to enroll
		{
			Key: "mfaEnabled", Label: "Require MFA for privileged users", Group: "MFA",
			Description: "Master switch. When off, RoleRequiresMFA returns false — privileged users can sign in without a second factor. Existing TOTP/passkey enrollments are not deleted; users can still use them.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "mfaEnrollmentGraceDays", Label: "Enrollment grace period (days)", Group: "MFA",
			Description: "How many days a newly privileged user has to enroll a second factor before login returns 403 mfa_enrollment_required. Default 7.",
			Type:        module.FieldInt, Default: "7",
		},

		// Anti-abuse & Notifications — Tab 7. Operational guardrails on
		// top of the per-tier login/registration kill switches: who gets
		// emailed on suspicious logins, which IPs/countries are
		// allowed/blocked at the operator host, and when to retire stale
		// accounts. Read at request time by AuthPolicyService; admin
		// edits take effect immediately. The IP and geo gates are
		// scoped to the operator surface only — Tier-2 client traffic
		// is far broader and gating it by IP/country would lock real
		// customers out, while operator console access is already a
		// privileged surface where allow/blocklists make sense.
		{
			Key: "notifyUserOnNewDeviceLogin", Label: "Email user on first login from a new device", Group: "Anti-abuse & Notifications",
			Description: "When on, sends an auth.new_device_login transactional email the first time a user logs in from a (deviceId, userUUID) pair the system has not seen before. Helps users notice unauthorised access on the same day it happens.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "notifyAdminOnSuspiciousLogin", Label: "Email admins on high-risk login", Group: "Anti-abuse & Notifications",
			Description: "When on, every high-risk login (risk score ≥ 0.5) emails each address in the recipients list below in addition to notifying the user. Default off — recipients must be explicitly configured first.",
			Type:        module.FieldBool, Default: "false",
		},
		{
			Key: "suspiciousLoginRecipients", Label: "Suspicious-login admin recipients", Group: "Anti-abuse & Notifications",
			Description: "Comma-separated list of admin email addresses notified on high-risk logins. Empty disables the admin email half regardless of the toggle above.",
			Type:        module.FieldStringList,
		},
		{
			Key: "ipAllowlistAdmin", Label: "IP allowlist (operator console)", Group: "Anti-abuse & Notifications",
			Description: "Comma-separated list of CIDR ranges allowed to reach the operator host. Empty = open. Applied only to operator host traffic — the client API is unaffected. Example: 10.0.0.0/8, 192.0.2.5/32.",
			Type:        module.FieldStringList,
		},
		{
			Key: "ipBlocklistAdmin", Label: "IP blocklist (operator console)", Group: "Anti-abuse & Notifications",
			Description: "Comma-separated list of CIDR ranges denied at the operator host. Evaluated after the allowlist — a blocked entry rejects the request even if it also matches the allowlist.",
			Type:        module.FieldStringList,
		},
		{
			Key: "geoBlockCountries", Label: "Country blocklist", Group: "Anti-abuse & Notifications",
			Description: "Comma-separated ISO-3166-1 alpha-2 country codes (e.g. RU, KP) that cannot complete login on either tier. Requires the GeoIP resolver (AUTH_GEOIP_DB_PATH) — empty when GeoIP is disabled has no effect.",
			Type:        module.FieldStringList,
		},
		{
			Key: "inactiveAccountAutoDisableDays", Label: "Auto-disable inactive accounts after (days)", Group: "Anti-abuse & Notifications",
			Description: "Disables a user account when its lastLogin is older than the configured number of days. Checked at login time so a stale account is denied at the next attempt without a periodic job. 0 = disabled.",
			Type:        module.FieldInt, Default: "0",
		},

		// Sessions & Account — Phase 8 trivial toggles. Two existing
		// security behaviours surfaced as live-editable knobs.
		{
			Key: "revokeSessionsOnPasswordChange", Label: "Revoke sessions on password change", Group: "Sessions & Account",
			Description: "When on, a successful POST /v1/auth/{tier}/change-password also revokes the caller's current session id and every device-trust grant for the user. When off, password change leaves existing sessions alive (used for migrations or staged rollouts; not recommended in steady state). Default on.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "selfServiceAccountDeletionClient", Label: "Allow client users to self-delete (GDPR erase)", Group: "Sessions & Account",
			Description: "When on, Tier-2 client users can call POST /v1/me/dsr/erase to irreversibly wipe their personal data across every PII producer. When off (default), client tier returns 403 self_service_deletion_disabled and erasure must be triggered through the operator console. Operator-side erasure is unaffected.",
			Type:        module.FieldBool, Default: "false",
		},

		// OAuth signup allowance — Phase 9 small backlog. The OAuth
		// provider tabs above gate which buttons appear; this pair gates
		// what happens when an OAuth login arrives for an unknown email.
		// When off, the callback returns 403 oauth_signup_disabled
		// instead of provisioning a new account — useful when an
		// operator wants to allow existing users to sign in via OAuth
		// while keeping signups invitation-only.
		{
			Key: "oauthAllowSignupAdmin", Label: "Allow OAuth signups on operator console", Group: "OAuth Providers",
			Description: "When off, OAuth callbacks on the operator host that resolve to an unknown email return 403 instead of creating a new operator account. Existing users can still sign in.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "oauthAllowSignupClient", Label: "Allow OAuth signups on client app", Group: "OAuth Providers",
			Description: "When off, OAuth callbacks on the client host that resolve to an unknown email return 403 instead of creating a new client account.",
			Type:        module.FieldBool, Default: "true",
		},

		// MFA — admin-managed list of roles that mandate a second factor.
		// Phase 9 small backlog. Empty falls back to the legacy hardcoded
		// list (super_admin, administrator, org_owner, org_admin) so an
		// unset value preserves today's behaviour. Adding a role here is
		// security-sensitive — broaden carefully.
		{
			Key: "mfaRequiredForRoles", Label: "Roles that require MFA", Group: "MFA",
			Description: "Comma-separated list of role names that mandate a second factor. Recognised system roles: super_admin, administrator, developer, manager, operator, guest. Recognised org roles: org_owner, org_admin, org_member. Empty restores the built-in default (super_admin, administrator, org_owner, org_admin).",
			Type:        module.FieldStringList,
		},
		{
			Key: "recoveryCodesCount", Label: "Recovery codes issued on enrollment", Group: "MFA",
			Description: "Number of one-shot backup codes minted when a user confirms TOTP enrollment. Default 10. Range 1–50 — outside that the legacy default (10) is used.",
			Type:        module.FieldInt, Default: "10",
		},

		// OAuth account linking — Phase 10 of the auth-policy roadmap.
		// Today's flow auto-links an OAuth provider to an existing
		// account when the email matches. That's convenient but lets
		// an attacker who controls a verified email at the IdP take
		// over an existing Orkestra account whose owner used a
		// password. Operators in higher-assurance deployments turn
		// this off.
		{
			Key: "oauthAutoLinkByEmail", Label: "Auto-link OAuth provider to existing email account", Group: "OAuth Providers",
			Description: "When on (default), an OAuth callback for an existing Orkestra account (matched by email) attaches the provider to that user automatically. When off, the OAuth flow refuses with 403 oauth_link_disabled and the user must initiate linking from their account settings while authenticated. Recommended off for compliance-sensitive deployments.",
			Type:        module.FieldBool, Default: "true",
		},
	}
}

func (m *AuthModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		// Non-tier-split collections: security events are an audit log
		// keyed on userUUID alone, device-trust grants follow the user
		// record and the auth-path split does not need them per-tier.
		{Name: models.SecurityEventsCollection},
		{Name: models.DeviceTrustCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1, "deviceId": 1}},
			{Keys: map[string]int{"trustedUntil": 1}, ExpireAt: true},
		}},

		// ADR-0003 PR-D D-8: operator-tier and client-tier auth
		// collections are the only canonical storage. Each pair below
		// shares an identical IndexSpec set; only the collection name
		// differs.
		{Name: models.OperatorOAuthProvidersCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"userUuid": 1, "provider": 1}, Unique: true},
		}},
		{Name: models.ClientOAuthProvidersCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"userUuid": 1, "provider": 1}, Unique: true},
		}},
		{Name: models.OperatorRefreshTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			{Keys: map[string]int{"familyId": 1}},
		}},
		{Name: models.ClientRefreshTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			{Keys: map[string]int{"familyId": 1}},
		}},
		{Name: models.OperatorSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: models.ClientSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: models.OperatorEmailTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"tokenHash": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			{Keys: map[string]int{"expiresAt": 1}, TTL: 24 * time.Hour},
		}},
		{Name: models.ClientEmailTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"tokenHash": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			{Keys: map[string]int{"expiresAt": 1}, TTL: 24 * time.Hour},
		}},
		{Name: models.OperatorMFAFactorsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1, "type": 1}, Unique: true},
		}},
		{Name: models.ClientMFAFactorsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1, "type": 1}, Unique: true},
		}},
	}
}

func (m *AuthModule) Init(deps *module.Dependencies) error {
	cfg := deps.Config
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	// Device-trust is the only auth collection that stays single (not
	// tier-split) — the grant follows the user record and is reused
	// across both tier mounts.
	deviceTrustRepo := repository.NewDeviceTrustRepository(deps.DB)
	deviceTrustDuration := parseDurationEnv("AUTH_DEVICE_TRUST_DURATION", models.DeviceTrustDuration)
	deviceTrustSvc := services.NewDeviceTrustService(deviceTrustRepo, deviceTrustDuration, logger)

	// OAuth provider factory + live config resolver. Provider configs
	// live in the module_configs document, resolved per-request from
	// admin-managed values; secret rotations take effect without a
	// restart.
	providerFactory := services.NewOAuthProviderFactory(
		map[models.OAuthProvider]*services.OAuthProviderConfig{},
		deps.RedisAdapter,
	)
	oauthResolver := services.NewOAuthConfigResolver(deps.ConfigService)

	// Operator-audience JWT service. The environment is stamped into
	// the iss claim (orkestra.<env>) so a token minted in one
	// deployment is rejected by another even if the signing keys ever
	// overlap. The same key pair is reused by the client-audience JWT
	// service constructed below — only the aud claim differs.
	operatorJWT, err := services.NewJWTServiceWithAudience(
		cfg.Auth.JWT.PrivateKey,
		cfg.Auth.JWT.PublicKey,
		cfg.Server.Environment,
		services.AudienceOperator,
		cfg.Auth.JWT.AccessTokenExpiry,
		cfg.Auth.JWT.RefreshTokenExpiry,
	)
	if err != nil {
		return err
	}
	tenantProvider := module.MustGetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	operatorJWT.SetTenantProvider(tenantProvider)

	// OAuth state service + signed-state JWT secret (D-6). The HMAC
	// secret is derived from the JWT private key so every replica
	// agrees without an extra env var; rotation is implicit when the
	// JWT key pair rotates.
	redisStore := services.NewRedisOAuthStateStore(deps.RedisAdapter)
	oauthStateService := services.NewOAuthStateService(redisStore)
	var oauthStateSecret []byte
	if cfg.Auth.JWT.PrivateKey != nil {
		secret, err := services.DeriveOAuthStateSecret(cfg.Auth.JWT.PrivateKey)
		if err != nil {
			logger.Warn("auth: failed to derive OAuth state secret",
				slog.String("error", err.Error()))
		} else {
			oauthStateSecret = secret
		}
	}

	// First-admin claimer is shared between OAuth and password signup
	// so both tiers race-proof the first-user super_admin election
	// against the same atomic claimer.
	var firstAdminClaimer services.FirstAdminClaimer
	if c, ok := module.GetTyped[services.FirstAdminClaimer](deps.Services, module.ServiceFirstAdminClaimer); ok {
		firstAdminClaimer = c
	} else {
		logger.Warn("first-admin claimer not wired — signup flows will fall through to non-atomic first-user heuristic")
	}

	mfaChallengeSvc := services.NewMFAChallengeService(redisStore)

	// Session revocation list (Block D): Redis-backed set of revoked
	// `sid` claims checked on every authenticated request. Single
	// instance shared across both tiers since the sid namespace is
	// global.
	sessionRevocationSvc := services.NewSessionRevocationService(
		deps.RedisAdapter,
		cfg.Auth.JWT.AccessTokenExpiry,
		logger,
	)

	passwordSvc := services.NewPasswordService(logger, true)
	var notifier iface.NotificationSender
	if n, ok := module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender); ok {
		notifier = n
	}

	// Suspicious-login notifier shares one SecurityEventService
	// instance with the PII producer below so user-facing security
	// history and GDPR DSR export read the same rows. The policy
	// pointer below is constructed further down — wire it on the
	// notifier after `authPolicy` exists.
	securityEventSvc, securityEventErr := services.NewSecurityEventService(deps.DB)
	if securityEventErr != nil {
		logger.Warn("auth: security event service init failed; suspicious-login notifier disabled",
			slog.String("error", securityEventErr.Error()))
	}

	rateLimiter := sharederrors.NewRateLimiter()

	mfaIssuer := getEnvOrDefault("APP_NAME", "Orkestra")
	geoResolver := geoip.FromEnv(logger)
	velocityKmh := parseFloatEnv("AUTH_GEOIP_VELOCITY_THRESHOLD_KMH", services.DefaultImpossibleTravelVelocityKmh)

	// WebAuthn relying party — resolved once and shared across both
	// tier bundles so an env-misconfiguration produces a single
	// warning. Nil disables passkeys at boot; the per-tier bundles
	// inherit a nil webauthnSvc to match.
	rpID, rpOrigins := resolveWebAuthnRP(cfg.Server.FrontendURL)
	var webauthnRP *gowebauthn.WebAuthn
	if rpID != "" && len(rpOrigins) > 0 {
		wa, err := gowebauthn.New(&gowebauthn.Config{
			RPDisplayName: mfaIssuer,
			RPID:          rpID,
			RPOrigins:     rpOrigins,
		})
		if err != nil {
			logger.Warn("webauthn disabled — config invalid",
				slog.String("rpId", rpID),
				slog.String("error", err.Error()),
			)
		} else {
			webauthnRP = wa
			logger.Info("webauthn enabled",
				slog.String("rpId", rpID),
				slog.Int("rpOrigins", len(rpOrigins)),
			)
		}
	} else {
		logger.Info("webauthn disabled — WEBAUTHN_RP_ID/WEBAUTHN_RP_ORIGINS not set")
	}

	// Device-trust self-service handler is reused across both tier
	// mounts since the underlying collection is single.
	m.deviceTrustHandler = handlers.NewDeviceTrustHandler(deviceTrustSvc)

	// Shared admin-policy reader. Both tier bundles consume the same
	// instance — schema keys carry their own Admin/Client suffix so a
	// single ConfigService read disambiguates by audience.
	authPolicy := services.NewAuthPolicyService(deps.ConfigService)
	// Hand the policy to the (already-constructed) shared password
	// service so length / complexity / HIBP rules can be edited live
	// at /admin/modules/auth without a restart.
	passwordSvc.SetPolicy(authPolicy)

	// Suspicious-login notifier — constructed here (after authPolicy)
	// so the admin-email half can read notifyAdminOnSuspiciousLogin /
	// suspiciousLoginRecipients live on every OnLogin call.
	var suspiciousLoginNotifierSvc services.SuspiciousLoginNotifier
	if securityEventSvc != nil {
		suspiciousLoginNotifierSvc = services.NewSuspiciousLoginNotifier(services.NotifierConfig{
			Events:       securityEventSvc,
			Notifier:     notifier,
			AppName:      getEnvOrDefault("APP_NAME", "Orkestra"),
			SupportEmail: os.Getenv("SUPPORT_EMAIL"),
			FrontendURL:  cfg.Server.FrontendURL,
			Logger:       logger,
			Policy:       authPolicy,
		})
	}

	commonTierDeps := tierBundleDeps{
		db:                       deps.DB,
		logger:                   logger,
		tenantProvider:           tenantProvider,
		passwordService:          passwordSvc,
		mfaChallengeService:      mfaChallengeSvc,
		firstAdminClaimer:        firstAdminClaimer,
		deviceTrust:              deviceTrustSvc,
		suspiciousLoginNotifier:  suspiciousLoginNotifierSvc,
		notifier:                 notifier,
		rateLimiter:              rateLimiter,
		geoResolver:              geoResolver,
		velocityKmh:              velocityKmh,
		frontendURL:              cfg.Server.FrontendURL,
		requireEmailVerification: getBoolEnv("AUTH_REQUIRE_EMAIL_VERIFICATION", cfg.IsProductionLike()),
		appName:                  getEnvOrDefault("APP_NAME", "Orkestra"),
		supportEmail:             os.Getenv("SUPPORT_EMAIL"),
		mfaIssuer:                mfaIssuer,
		webauthnRP:               webauthnRP,
		authPolicy:               authPolicy,
	}

	// ADR-0003 PR-D D-9: per-audience refresh-cookie domains. Each
	// tier's handler trio gets the matching value so refresh cookies are
	// scoped to the host that minted them — operator tokens stay on
	// `console.*`, client tokens on `api.*`. Empty per-tier fields fall
	// back to the legacy single-host `Domain` so single-host deployments
	// keep working unchanged.
	operatorCookieDomain := cfg.Auth.Cookie.OperatorDomain
	if operatorCookieDomain == "" {
		operatorCookieDomain = cfg.Auth.Cookie.Domain
	}
	clientCookieDomain := cfg.Auth.Cookie.ClientDomain
	if clientCookieDomain == "" {
		clientCookieDomain = cfg.Auth.Cookie.Domain
	}

	// Operator tier — required after the D-8 cutover. The user module
	// always registers ServiceOperatorUserProvider, so a missing
	// provider here means the user module failed to init.
	operatorUser := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceOperatorUserProvider)
	opDeps := commonTierDeps
	opDeps.tier = tierOperator
	opDeps.userProvider = operatorUser
	opDeps.jwtService = operatorJWT
	if cfg.Server.Operator.FrontendURL != "" {
		opDeps.frontendURL = cfg.Server.Operator.FrontendURL
	}
	opBundle, err := buildAuthTierBundle(opDeps)
	if err != nil {
		return err
	}

	m.operatorPasswordHandler = handlers.NewPasswordAuthHandler(
		opBundle.passwordSvc,
		cfg.Auth.Cookie.Name,
		operatorCookieDomain,
		cfg.Auth.Cookie.Secure,
	)
	m.operatorPasswordHandler.SetSessionRevocation(sessionRevocationSvc)

	m.operatorAuthHandler = handlers.NewAuthHandler(
		opBundle.authService,
		providerFactory,
		oauthResolver,
		oauthStateService,
		opBundle.oauthProviderRepo,
		operatorJWT,
		cfg,
		operatorCookieDomain,
	)
	m.operatorAuthHandler.SetSessionRevocation(sessionRevocationSvc)
	m.operatorAuthHandler.SetStateSecret(oauthStateSecret)
	m.operatorAuthHandler.SetTier(services.AudienceOperator)
	m.operatorAuthHandler.SetPolicy(authPolicy)

	m.operatorMFAHandler = handlers.NewMFAHandler(
		opBundle.mfaSvc,
		mfaChallengeSvc,
		operatorJWT,
		operatorUser,
		opBundle.passwordSvc,
		cfg.Auth.Cookie.Name,
		operatorCookieDomain,
		cfg.Auth.Cookie.Secure,
	)
	m.operatorMFAHandler.SetDeviceTrust(deviceTrustSvc)
	m.operatorMFAHandler.SetPolicy(authPolicy)
	if opBundle.webauthnSvc != nil {
		m.operatorMFAHandler.SetWebAuthn(opBundle.webauthnSvc)
		m.operatorWebAuthnHandler = handlers.NewWebAuthnHandler(
			opBundle.webauthnSvc,
			mfaChallengeSvc,
			operatorJWT,
			operatorUser,
			opBundle.passwordSvc,
			cfg.Auth.Cookie.Name,
			operatorCookieDomain,
			cfg.Auth.Cookie.Secure,
		)
		m.operatorWebAuthnHandler.SetDeviceTrust(deviceTrustSvc)
		deps.Services.Register(module.ServiceWebAuthn, opBundle.webauthnSvc)
	}

	// Client tier — required after the D-8 cutover. Same expectation
	// as operator tier above. Mints aud=client tokens via the client-
	// audience JWT service so the client host mux's
	// RequireAudience("client") gate accepts them and the operator
	// mux rejects them. Reuses the same RS256 key pair as the operator
	// service — only the audience claim differs.
	clientUser := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceClientUserProvider)
	clientJWT, err := services.NewJWTServiceWithAudience(
		cfg.Auth.JWT.PrivateKey,
		cfg.Auth.JWT.PublicKey,
		cfg.Server.Environment,
		services.AudienceClient,
		cfg.Auth.JWT.AccessTokenExpiry,
		cfg.Auth.JWT.RefreshTokenExpiry,
	)
	if err != nil {
		return err
	}
	clientJWT.SetTenantProvider(tenantProvider)

	clDeps := commonTierDeps
	clDeps.tier = tierClient
	clDeps.userProvider = clientUser
	clDeps.jwtService = clientJWT
	if cfg.Server.Client.FrontendURL != "" {
		clDeps.frontendURL = cfg.Server.Client.FrontendURL
	}
	clBundle, err := buildAuthTierBundle(clDeps)
	if err != nil {
		return err
	}

	m.clientPasswordHandler = handlers.NewPasswordAuthHandler(
		clBundle.passwordSvc,
		cfg.Auth.Cookie.Name,
		clientCookieDomain,
		cfg.Auth.Cookie.Secure,
	)
	m.clientPasswordHandler.SetSessionRevocation(sessionRevocationSvc)

	m.clientAuthHandler = handlers.NewAuthHandler(
		clBundle.authService,
		providerFactory,
		oauthResolver,
		oauthStateService,
		clBundle.oauthProviderRepo,
		clientJWT,
		cfg,
		clientCookieDomain,
	)
	m.clientAuthHandler.SetSessionRevocation(sessionRevocationSvc)
	m.clientAuthHandler.SetStateSecret(oauthStateSecret)
	m.clientAuthHandler.SetTier(services.AudienceClient)
	m.clientAuthHandler.SetPolicy(authPolicy)

	m.clientMFAHandler = handlers.NewMFAHandler(
		clBundle.mfaSvc,
		mfaChallengeSvc,
		clientJWT,
		clientUser,
		clBundle.passwordSvc,
		cfg.Auth.Cookie.Name,
		clientCookieDomain,
		cfg.Auth.Cookie.Secure,
	)
	m.clientMFAHandler.SetDeviceTrust(deviceTrustSvc)
	m.clientMFAHandler.SetPolicy(authPolicy)
	if clBundle.webauthnSvc != nil {
		m.clientMFAHandler.SetWebAuthn(clBundle.webauthnSvc)
		m.clientWebAuthnHandler = handlers.NewWebAuthnHandler(
			clBundle.webauthnSvc,
			mfaChallengeSvc,
			clientJWT,
			clientUser,
			clBundle.passwordSvc,
			cfg.Auth.Cookie.Name,
			clientCookieDomain,
			cfg.Auth.Cookie.Secure,
		)
		m.clientWebAuthnHandler.SetDeviceTrust(deviceTrustSvc)
	}

	// ADR-0003 PR-D D-6: per-tier dispatcher map on the operator
	// AuthHandler — that's the instance that owns the single shared
	// OAuth callback URL registered with each provider. On every
	// callback it parses the signed-state JWT and looks the tier up in
	// this map to pick the AuthHandler whose authService should mint
	// the resulting tokens. Empty/unknown state.tier falls back to
	// operator (the receiver) so stray pre-cutover flows still resolve.
	tierDispatch := map[string]*handlers.AuthHandler{
		services.AudienceOperator: m.operatorAuthHandler,
		services.AudienceClient:   m.clientAuthHandler,
	}
	m.operatorAuthHandler.SetTierDispatch(tierDispatch)

	// Canonical service registrations. After the D-8 cutover the
	// operator-tier services back the canonical keys — they are the
	// default an unaware consumer (setup wizard, dev token, middleware
	// auto-refresh) gets. Audience-aware consumers (onboarding,
	// compliance audit sink) request the per-tier key directly.
	deps.Services.Register(module.ServiceAuthService, opBundle.authService)
	deps.Services.Register(module.ServiceJWTService, operatorJWT)
	// ADR-0003 PR-D D-10: per-tier JWT services published so audience-
	// aware consumers (dev token generator) can mint a token stamped
	// with the matching `aud` claim without poking at tier internals.
	deps.Services.Register(module.ServiceOperatorJWTService, operatorJWT)
	deps.Services.Register(module.ServiceClientJWTService, clientJWT)
	deps.Services.Register(module.ServicePasswordService, passwordSvc)
	deps.Services.Register(module.ServicePasswordAuthService, opBundle.passwordSvc)
	deps.Services.Register(module.ServiceSessionRevocation, sessionRevocationSvc)
	deps.Services.Register(module.ServiceOperatorAuthService, opBundle.authService)
	deps.Services.Register(module.ServiceOperatorPasswordAuthService, opBundle.passwordSvc)
	deps.Services.Register(module.ServiceClientAuthService, clBundle.authService)
	deps.Services.Register(module.ServiceClientPasswordAuthService, clBundle.passwordSvc)
	// Phase 7: publish the policy reader so non-auth callers (operator
	// IP-gate middleware, future admin tooling) can hit the live
	// admin-managed config without reaching into auth-module internals.
	deps.Services.Register(module.ServiceAuthPolicy, authPolicy)

	// Session-risk lookup: resolves the most recent risk score for a
	// sid against the auth_sessions collections. Sessions are tier-
	// scoped (operator_sessions vs client_sessions) but the sid
	// namespace is global, so the lookup tries operator first and
	// falls through to client. A nil error with score==0 is legitimate
	// (session absent, terminated, or scorer not yet populated) —
	// callers treat it as zero risk and fail open.
	operatorSessions := opBundle.authSessionRepo
	clientSessions := clBundle.authSessionRepo
	var sessionRiskLookup authMiddleware.SessionRiskLookup = func(ctx context.Context, sessionID string) (float64, error) {
		if sessionID == "" {
			return 0, nil
		}
		if session, err := operatorSessions.GetByUUID(ctx, sessionID); err != nil {
			return 0, err
		} else if session != nil {
			return session.RiskScore, nil
		}
		session, err := clientSessions.GetByUUID(ctx, sessionID)
		if err != nil {
			return 0, err
		}
		if session == nil {
			return 0, nil
		}
		return session.RiskScore, nil
	}
	deps.Services.Register(module.ServiceSessionRiskLookup, sessionRiskLookup)

	// Register one PII producer per tier with the DSR registry. Each
	// producer reports tier-correct collection names in the DSR audit
	// row. The registry tolerates missing producers — a deployment
	// without compliance just skips registration silently.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(
			opBundle.refreshTokenRepo, opBundle.authSessionRepo, opBundle.emailTokenRepo, opBundle.mfaFactorRepo,
			securityEventSvc, deviceTrustRepo,
			models.OperatorRefreshTokensCollection, models.OperatorSessionsCollection,
			models.OperatorEmailTokensCollection, models.OperatorMFAFactorsCollection,
		))
		reg.Register(services.NewPIIProducer(
			clBundle.refreshTokenRepo, clBundle.authSessionRepo, clBundle.emailTokenRepo, clBundle.mfaFactorRepo,
			securityEventSvc, deviceTrustRepo,
			models.ClientRefreshTokensCollection, models.ClientSessionsCollection,
			models.ClientEmailTokensCollection, models.ClientMFAFactorsCollection,
		))
	}

	return nil
}

func getBoolEnv(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1" || v == "yes"
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseFloatEnv reads key as a float64. Falls back to fallback on
// unset, empty, or malformed input. Malformed input is logged so ops
// can spot typos instead of running silently on the default.
func parseFloatEnv(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		slog.Default().Warn("auth: malformed float env var, using default",
			slog.String("key", key),
			slog.String("value", raw),
			slog.Float64("default", fallback))
		return fallback
	}
	return v
}

// parseDurationEnv reads key as a Go duration string (e.g. "168h",
// "30m"). Falls back to fallback on unset, empty, or malformed input.
// Logs a warning on malformed input so ops can spot the typo instead
// of silently running with the default.
func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Default().Warn("auth: malformed duration env var, using default",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("default", fallback.String()))
		return fallback
	}
	return d
}

// resolveWebAuthnRP derives the WebAuthn Relying Party ID and origin list
// from env vars, falling back to the deployment's frontend URL when only
// one or the other is set. RP ID must be the eTLD+1 host (no scheme, no
// port, no path) per the W3C spec; origins are the full URL the browser
// sees in the address bar. Returning empty values disables WebAuthn at
// boot — the caller logs and skips wiring.
func resolveWebAuthnRP(frontendURL string) (string, []string) {
	rpID := strings.TrimSpace(os.Getenv("WEBAUTHN_RP_ID"))
	originsCSV := strings.TrimSpace(os.Getenv("WEBAUTHN_RP_ORIGINS"))

	var origins []string
	if originsCSV != "" {
		for _, o := range strings.Split(originsCSV, ",") {
			if v := strings.TrimSpace(o); v != "" {
				origins = append(origins, v)
			}
		}
	}

	// Fallback: if either side is missing, parse the frontend URL.
	// FRONTEND_URL is already required for OAuth redirects so it's a safe
	// default for dev (http://localhost:8080 → rpID=localhost).
	if (rpID == "" || len(origins) == 0) && frontendURL != "" {
		if u, err := url.Parse(frontendURL); err == nil && u.Host != "" {
			if rpID == "" {
				rpID = u.Hostname() // strips port — rpID must not include it
			}
			if len(origins) == 0 {
				// scheme + host (with port if present) — what the browser sends
				origins = []string{u.Scheme + "://" + u.Host}
			}
		}
	}
	return rpID, origins
}

func (m *AuthModule) RegisterRoutes(ri *module.RouteInfo) {
	// ADR-0003 PR-D D-8: only audience-split mounts survive. The
	// operator AuthHandler also owns the single shared OAuth callback
	// URL (RegisterOAuthRoutes), since the IdP has one registered
	// redirect URI per provider; the callback's signed-state JWT
	// carries the audience tier and dispatches to the matching
	// authService.

	operatorProtectedAPI := humachi.New(ri.Operator.ProtectedRouter, ri.APIConfig)

	// Operator OAuth callback (the single dispatcher) + operator
	// tier-mountable routes (refresh / refresh-cookie / logout / me) +
	// per-audience OAuth start endpoints stamped with tier=operator.
	m.operatorAuthHandler.RegisterOAuthRoutes(ri.Operator.PublicAPI, operatorProtectedAPI, ri.Router, ri.Operator.ProtectedRouter)
	m.operatorAuthHandler.RegisterTierMountableRoutes(ri.Operator.PublicAPI, operatorProtectedAPI, ri.Router, handlers.OperatorMount)
	m.operatorAuthHandler.RegisterOAuthStartRoutes(ri.Operator.PublicAPI, handlers.OperatorMount)

	// Operator password auth: register/login/verify/reset/forgot are
	// public; change-password is protected and runs without an org
	// context (user self-service).
	m.operatorPasswordHandler.RegisterPublicRoutes(ri.Operator.PublicAPI, handlers.OperatorMount)
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.operatorPasswordHandler.RegisterProtectedRoutes(api, handlers.OperatorMount)
	})

	// Operator MFA endpoints split into four halves:
	//   - public: /v1/auth/operator/mfa/login/verify completes an in-
	//     flight login (caller has a challengeId, not yet a bearer).
	//   - protected (no step-up): enroll / status / verify.
	//   - protected (step-up): /v1/auth/operator/me/mfa/remove —
	//     dropping your own second factor is catastrophic, demand a
	//     <5min OTP proof.
	//   - admin (step-up): /v1/admin/users/{id}/mfa/reset — admin reset
	//     stays under /v1/admin/... since admin is operator-tier by
	//     definition.
	m.operatorMFAHandler.RegisterPublicRoutes(ri.Operator.PublicAPI, handlers.OperatorMount)
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.operatorMFAHandler.RegisterProtectedRoutes(api, handlers.OperatorMount)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
		api := humachi.New(r, ri.APIConfig)
		m.operatorMFAHandler.RegisterStepUpRoutes(api, handlers.OperatorMount)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.mfa_reset"))
		r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
		api := humachi.New(r, ri.APIConfig)
		m.operatorMFAHandler.RegisterAdminRoutes(api)
	})

	// Operator WebAuthn — public/protected/step-up halves mirror the
	// TOTP layout. Nil handler means passkeys are disabled at boot.
	if m.operatorWebAuthnHandler != nil {
		m.operatorWebAuthnHandler.RegisterPublicRoutes(ri.Operator.PublicAPI, handlers.OperatorMount)
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.operatorWebAuthnHandler.RegisterProtectedRoutes(api, handlers.OperatorMount)
		})
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
			api := humachi.New(r, ri.APIConfig)
			m.operatorWebAuthnHandler.RegisterStepUpRoutes(api, handlers.OperatorMount)
		})
	}

	// Device-trust self-service on the operator mount. Single non-tier-
	// split handler reused under both tier prefixes.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.deviceTrustHandler.RegisterRoutes(api, handlers.OperatorMount)
	})

	// ADR-0003 PR-D D-5: client-tier auth paths under
	// /v1/auth/client/... — mounted on the client host mux. Each
	// client-bound handler reads/writes through client_* collections
	// and mints aud=client tokens via the client-audience JWT service
	// constructed in Init. OAuth callbacks are NOT mounted here — the
	// operator dispatcher above owns the single shared callback URL
	// and dispatches client-tier flows back to the client authService
	// via the tierDispatch map. Admin paths stay operator-only.
	if ri.Client == nil {
		return
	}
	clientProtectedAPI := humachi.New(ri.Client.ProtectedRouter, ri.APIConfig)
	if ri.ClientRouter != nil {
		m.clientAuthHandler.RegisterTierMountableRoutes(ri.Client.PublicAPI, clientProtectedAPI, ri.ClientRouter, handlers.ClientMount)
		m.clientAuthHandler.RegisterOAuthStartRoutes(ri.Client.PublicAPI, handlers.ClientMount)
	}
	m.clientPasswordHandler.RegisterPublicRoutes(ri.Client.PublicAPI, handlers.ClientMount)
	ri.Client.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Client.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.clientPasswordHandler.RegisterProtectedRoutes(api, handlers.ClientMount)
	})
	m.clientMFAHandler.RegisterPublicRoutes(ri.Client.PublicAPI, handlers.ClientMount)
	ri.Client.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Client.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.clientMFAHandler.RegisterProtectedRoutes(api, handlers.ClientMount)
	})
	ri.Client.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Client.AuthMW.RequireGlobal())
		r.Use(ri.Client.AuthMW.RequireStepUp(5 * time.Minute))
		api := humachi.New(r, ri.APIConfig)
		m.clientMFAHandler.RegisterStepUpRoutes(api, handlers.ClientMount)
	})
	if m.clientWebAuthnHandler != nil {
		m.clientWebAuthnHandler.RegisterPublicRoutes(ri.Client.PublicAPI, handlers.ClientMount)
		ri.Client.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.clientWebAuthnHandler.RegisterProtectedRoutes(api, handlers.ClientMount)
		})
		ri.Client.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			r.Use(ri.Client.AuthMW.RequireStepUp(5 * time.Minute))
			api := humachi.New(r, ri.APIConfig)
			m.clientWebAuthnHandler.RegisterStepUpRoutes(api, handlers.ClientMount)
		})
	}
	ri.Client.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Client.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.deviceTrustHandler.RegisterRoutes(api, handlers.ClientMount)
	})
}

