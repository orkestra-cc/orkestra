package auth

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/orkestra/backend/internal/core/auth/handlers"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/internal/shared/config"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/geoip"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
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
	// operatorAdminUserAuthHandler hosts the admin endpoints that
	// inspect and manage another operator user's auth methods
	// (password, MFA, OAuth, email verification). Mounted under
	// /v1/admin/users/{userId}/... on the operator host mux only —
	// admin actions on Tier-2 client users live on the user module's
	// AdminClientUserHandler.
	operatorAdminUserAuthHandler *handlers.AdminUserAuthHandler
	// operatorSelfUserAuthHandler hosts the self-service security-center
	// endpoints under /v1/auth/operator/me/... (auth-methods aggregator,
	// session list/revoke, OAuth self-unlink). Drives the
	// frontend-admin /user/security page. Tier-bound to operator
	// because session + OAuth state lives in operator_* collections;
	// the client-tier mirror is a deliberate follow-up.
	operatorSelfUserAuthHandler *handlers.SelfUserAuthHandler

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

	// serviceAccountService owns the service-account lifecycle (Task
	// 6/7: create/list/get/update accounts, issue/revoke credentials,
	// Grant the client-credentials token exchange) and serviceTokenHandler
	// exposes Grant over HTTP at POST /v1/auth/token (Task 8).
	// serviceAccountAdminHandler exposes the same lifecycle methods over
	// the gated operator admin surface at /v1/admin/service-accounts
	// (Task 9). None of the three is registered in the ServiceRegistry —
	// nothing external consumes them.
	serviceAccountService      *services.ServiceAccountService
	serviceTokenHandler        *handlers.ServiceTokenHandler
	serviceAccountAdminHandler *handlers.ServiceAccountAdminHandler

	// cfg is captured at construction time so Init does not need
	// Dependencies.Config (Phase 1c). The Module interface contract has
	// no field for app-wide config; auth is the only consumer of
	// *config.Config and threads it in via the catalog factory.
	cfg *config.Config

	// Refresh-token retention sweep (ADR-0017 D7). One loop covering both
	// tiers by calling the two repositories, which are separate
	// instances. lifecycleMu/sweepCancel/sweepDone copy the logging
	// module's pattern so Start is idempotent, a stopped module can start
	// again, and no second ticker survives a hot enable/disable cycle.
	lifecycleMu sync.Mutex
	sweepCancel context.CancelFunc
	sweepDone   chan struct{}
	sweepTiers  []sweepTier
	sweepLease  *services.MaintenanceLease
	logger      *slog.Logger
}

// NewModule constructs an AuthModule bound to the live application config.
// cfg must be non-nil — Init dereferences it to read JWT keys, cookie
// settings, and the per-audience frontend URLs. The catalog factory in
// cmd/server/catalog.go threads main.go's cfg through a closure.
func NewModule(cfg *config.Config) *AuthModule { return &AuthModule{cfg: cfg} }

func (m *AuthModule) Name() string        { return "auth" }
func (m *AuthModule) DisplayName() string { return "Authentication" }
func (m *AuthModule) Description() string { return "OAuth 2.1, JWT, sessions, RBAC" }

// HotReloadConfig declares what has always been true: AuthPolicyService and
// the OAuth resolver read module config at request time, so a successful
// config write is live immediately and must persist needsRestart=false in
// the same atomic update (spec §4.1) instead of leaving a false restart
// banner in the admin UI.
func (m *AuthModule) HotReloadConfig() bool { return true }

func (m *AuthModule) Dependencies() []string {
	return []string{"user", "notification", "tenant", "authz"}
}
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
		// System: true on the four admin-user-credential keys so they
		// land in the authz `systemPermissionSet` and the
		// `super_admin` / `administrator` / `developer` shortcuts grant
		// them without requiring an explicit global binding. Without
		// this flag a freshly created administrator user (no binding
		// to the seeded administrator role) would 403 on the four
		// /v1/admin/users/{id}/{mfa-reset,send-password-reset,
		// resend-verification,oauth} endpoints — the legacy default
		// hid the bug because the first user is always super_admin
		// (wildcard).
		{Key: "system.users.mfa_reset", Module: "auth", Description: "Admin: reset another user's MFA factors", System: true},
		{Key: "system.users.password_reset", Module: "auth", Description: "Admin: trigger a password-reset email for another user", System: true},
		{Key: "system.users.email_verify_resend", Module: "auth", Description: "Admin: resend the email-verification mail for another user", System: true},
		{Key: "system.users.oauth_unlink", Module: "auth", Description: "Admin: unlink an OAuth identity (Google/Apple/GitHub/Discord) from another user", System: true},
		// System: true — operator-console-only, granted by the platform
		// role shortcuts (super_admin/administrator), excluded from org
		// roles by the seeder. Task 9.
		{Key: "auth.service_accounts.read", Module: "auth", Description: "Admin: list and inspect service accounts", System: true},
		{Key: "auth.service_accounts.manage", Module: "auth", Description: "Admin: create service accounts and issue, rotate, or revoke their client credentials", System: true},
	}
}

// NavItems declares the auth module's operator-console navigation. The
// sidebar is backend-declared (frontend renders GET /v1/navigation);
// ItemKey is set explicitly so a future rename cannot orphan persisted
// reorder overrides.
func (m *AuthModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Name: "Service Accounts", Icon: "key",
			Path: "/admin/service-accounts", MinRole: "administrator", Active: true,
			ItemKey: "auth-service-accounts"},
	}
}

// ConfigGroups gives the admin settings page a sectioned rail instead of one
// flat list. auth is by far the largest configuration surface in the base —
// 63 fields — and the four OAuth providers are declared as children of the
// single "OAuth Providers" node rather than as siblings of it, which is what
// the old flat Group labels made them look like.
func (m *AuthModule) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "registration", Label: "Registration", Order: 1,
			Description: "Who may create an account, and on which surface."},
		{Key: "login", Label: "Login & Sessions", Order: 2,
			Description: "Sign-in availability, lockout, and token lifetimes."},
		{Key: "password", Label: "Password Policy", Order: 3,
			Description: "Rules enforced on every password change, on both the operator console and the client app."},
		{Key: "mfa", Label: "MFA", Order: 4,
			Description: "Second-factor requirements and enrollment."},
		{Key: "oauth", Label: "OAuth Providers", Order: 5,
			Description: "Which providers are offered, and on which surface. Each provider's credentials appear once it is switched on."},
		{Key: "oauth.google", Label: "Google", Parent: "oauth", Order: 6},
		{Key: "oauth.apple", Label: "Apple", Parent: "oauth", Order: 7},
		{Key: "oauth.github", Label: "GitHub", Parent: "oauth", Order: 8},
		{Key: "oauth.discord", Label: "Discord", Parent: "oauth", Order: 9},
		{Key: "antiabuse", Label: "Anti-abuse & Notifications", Order: 10,
			Description: "Rate limiting, IP and country rules, and the alerts they raise."},
		{Key: "sessions", Label: "Sessions & Account", Order: 11},
	}
}

// ConfigSchema declares every OAuth provider setting as admin-manageable.
// Values are seeded from the listed env vars on first boot, then owned by
// the module_configs document in MongoDB. Secrets are encrypted at rest.
// The Group field is a ConfigGroup.Key (see ConfigGroups above) — it drives
// the admin settings page's nested rail, not a modal or a flat tab strip.
func (m *AuthModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		// Google — gated on either audience surface being enabled so the
		// credentials are dead weight (and hidden) on installs that don't
		// use Google sign-in.
		{Key: "googleClientId", Label: "Client ID", Group: "oauth.google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_CLIENT_ID",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "googleEnabledAdmin", In: []string{"true"}},
				{Key: "googleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "googleClientSecret", Label: "Client Secret", Group: "oauth.google", Type: module.FieldSecret, EnvVar: "OAUTH_GOOGLE_CLIENT_SECRET",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "googleEnabledAdmin", In: []string{"true"}},
				{Key: "googleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "googleRedirectURL", Label: "Redirect URL", Group: "oauth.google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_REDIRECT_URL",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "googleEnabledAdmin", In: []string{"true"}},
				{Key: "googleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "googleAndroidClientId", Label: "Android Client ID", Group: "oauth.google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_ANDROID_CLIENT_ID",
			Advanced:       true,
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "googleEnabledAdmin", In: []string{"true"}},
				{Key: "googleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "googleIOSClientId", Label: "iOS Client ID", Group: "oauth.google", Type: module.FieldString, EnvVar: "OAUTH_GOOGLE_IOS_CLIENT_ID",
			Advanced:       true,
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "googleEnabledAdmin", In: []string{"true"}},
				{Key: "googleEnabledClient", In: []string{"true"}},
			},
		},

		// Apple — same gating shape as Google.
		{Key: "appleClientId", Label: "Client ID", Group: "oauth.apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_CLIENT_ID",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "appleTeamId", Label: "Team ID", Group: "oauth.apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_TEAM_ID",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "appleKeyId", Label: "Key ID", Group: "oauth.apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_KEY_ID",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "applePrivateKey", Label: ".p8 Key (PEM)", Group: "oauth.apple", Description: "Inline PEM content of your Apple Sign-In .p8 key", Type: module.FieldSecret, EnvVar: "OAUTH_APPLE_PRIVATE_KEY",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "applePrivateKeyPath", Label: ".p8 Key Path", Group: "oauth.apple", Description: "Filesystem path fallback if PEM is not inlined", Type: module.FieldString, EnvVar: "OAUTH_APPLE_PRIVATE_KEY_PATH",
			Advanced:       true,
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "appleRedirectURL", Label: "Redirect URL", Group: "oauth.apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_REDIRECT_URL",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "appleIOSClientId", Label: "iOS Client ID", Group: "oauth.apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_IOS_CLIENT_ID",
			Advanced:       true,
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "appleAndroidClientId", Label: "Android Client ID", Group: "oauth.apple", Type: module.FieldString, EnvVar: "OAUTH_APPLE_ANDROID_CLIENT_ID",
			Advanced:       true,
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "appleEnabledAdmin", In: []string{"true"}},
				{Key: "appleEnabledClient", In: []string{"true"}},
			},
		},

		// GitHub — same gating shape as Google.
		{Key: "githubClientId", Label: "Client ID", Group: "oauth.github", Type: module.FieldString, EnvVar: "OAUTH_GITHUB_CLIENT_ID",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "githubEnabledAdmin", In: []string{"true"}},
				{Key: "githubEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "githubClientSecret", Label: "Client Secret", Group: "oauth.github", Type: module.FieldSecret, EnvVar: "OAUTH_GITHUB_CLIENT_SECRET",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "githubEnabledAdmin", In: []string{"true"}},
				{Key: "githubEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "githubRedirectURL", Label: "Redirect URL", Group: "oauth.github", Type: module.FieldString, EnvVar: "OAUTH_GITHUB_REDIRECT_URL",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "githubEnabledAdmin", In: []string{"true"}},
				{Key: "githubEnabledClient", In: []string{"true"}},
			},
		},

		// Discord — same gating shape as Google.
		{Key: "discordClientId", Label: "Client ID", Group: "oauth.discord", Type: module.FieldString, EnvVar: "OAUTH_DISCORD_CLIENT_ID",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "discordEnabledAdmin", In: []string{"true"}},
				{Key: "discordEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "discordClientSecret", Label: "Client Secret", Group: "oauth.discord", Type: module.FieldSecret, EnvVar: "OAUTH_DISCORD_CLIENT_SECRET",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "discordEnabledAdmin", In: []string{"true"}},
				{Key: "discordEnabledClient", In: []string{"true"}},
			},
		},
		{Key: "discordRedirectURL", Label: "Redirect URL", Group: "oauth.discord", Type: module.FieldString, EnvVar: "OAUTH_DISCORD_REDIRECT_URL",
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "discordEnabledAdmin", In: []string{"true"}},
				{Key: "discordEnabledClient", In: []string{"true"}},
			},
		},

		// Registration — tier-aware site-wide signup policy. Read at
		// request time by AuthPolicyService; edits via the admin UI take
		// effect on the next signup with no restart. Both surfaces ship
		// OFF by default: a fresh install must not accept self-service
		// signups until the super_admin explicitly opens them. The very
		// first account on a fresh install bypasses this kill switch
		// (see PasswordAuthService.Register) so the bootstrapping
		// operator is never locked out.
		{
			Key: "registrationEnabledAdmin", Label: "Allow signups on operator console", Group: "registration",
			Description: "OFF by default. When off, POST /v1/auth/operator/register returns 403 — operator accounts must be invited or created via /admin. The super_admin turns this on to allow self-service operator signups. The first account on a fresh install bypasses the switch so bootstrap is never blocked.",
			Type:        module.FieldBool, Default: "false",
		},
		{
			Key: "registrationEnabledClient", Label: "Allow signups on client app", Group: "registration",
			Description: "OFF by default. When off, POST /v1/auth/client/register returns 403 — Tier-2 clients cannot self-register until the super_admin enables it.",
			Type:        module.FieldBool, Default: "false",
		},
		{
			Key: "defaultRoleClient", Label: "Default role for new client signups", Group: "registration",
			Description: "System role assigned to a Tier-2 client account on signup. Lower-privilege roles are recommended.",
			Type:        module.FieldEnum, Default: "operator",
			Options: []string{"operator", "manager", "guest"},
		},
		{
			Key: "allowedEmailDomainsAdmin", Label: "Allowed email domains (operator)", Group: "registration",
			Description: "Comma-separated allowlist (e.g. acme.com, ops.acme.com). Empty = any domain. Applied only to /v1/auth/operator/register.",
			Type:        module.FieldStringList,
		},
		{
			Key: "allowedEmailDomainsClient", Label: "Allowed email domains (client)", Group: "registration",
			Description: "Comma-separated allowlist applied only to /v1/auth/client/register. Empty = any domain.",
			Type:        module.FieldStringList,
		},

		// Login & Sessions — per-surface kill switches + lockout policy.
		// Read at request time by AuthPolicyService. The account pair and
		// the address pair are read per attempt by the Redis attempt
		// counters (services/attempt_counter.go), so an admin edit takes
		// effect on the very next attempt, including one already inside
		// an open window.
		{
			Key: "loginEnabledAdmin", Label: "Allow logins on operator console", Group: "login",
			Description: "When off, POST /v1/auth/operator/login returns 403. Use during maintenance to lock out the operator console without taking the backend offline.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "loginEnabledClient", Label: "Allow logins on client app", Group: "login",
			Description: "When off, POST /v1/auth/client/login returns 403. Affects /v1/auth/client/* only.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "passwordLoginEnabledAdmin", Label: "Allow email/password sign-in on operator console", Group: "login",
			Description: "When off, the operator console accepts OAuth only: new password sign-ins, signups and reset requests on /v1/auth/operator/* are refused (403 auth.password_login_disabled), in-flight password logins cannot complete, and a password no longer counts as a credential for step-up re-authentication or OAuth-unlink checks. Sessions opened before the change are not revoked. Cannot be turned off unless at least one OAuth provider is fully configured for this surface and 'Auto-link OAuth provider to existing email account' is on.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "passwordLoginEnabledClient", Label: "Allow email/password sign-in on client app", Group: "login",
			Description: "When off, the client app accepts OAuth only: new password sign-ins, signups and reset requests on /v1/auth/client/* are refused (403 auth.password_login_disabled), in-flight password logins cannot complete, and a password no longer counts as a credential for step-up re-authentication or OAuth-unlink checks. Sessions opened before the change are not revoked. Cannot be turned off unless at least one OAuth provider is fully configured for this surface and 'Auto-link OAuth provider to existing email account' is on.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "accountLockoutThreshold", Label: "Failed login attempts before lockout", Group: "login",
			Description: "Number of failed login attempts (per IP and per email) before the account is temporarily locked. Default 5.",
			Type:        module.FieldInt, Default: "5",
		},
		{
			Key: "accountLockoutDuration", Label: "Lockout duration", Group: "login",
			Description: "Go duration string (e.g. 15m, 1h) — how long an IP/email stays locked after exceeding the threshold. Default 15m.",
			Type:        module.FieldDuration, Default: "15m",
		},
		{
			Key: "ipLockoutThreshold", Label: "Failed attempts from one address before lockout", Group: "login",
			Description: "Number of failed attempts from a single source address before that address is temporarily locked. Deliberately much higher than the per-account threshold — one office egress or VPN can be hundreds of people, and locking the address on five wrong passwords among them would take the whole office offline. Must be at least the per-account threshold. Default 100.",
			Type:        module.FieldInt, Default: "100",
		},
		{
			Key: "ipLockoutDuration", Label: "Address lockout duration", Group: "login",
			Description: "Go duration string (e.g. 15m, 1h) — how long a source address stays locked after exceeding its threshold. Default 15m.",
			Type:        module.FieldDuration, Default: "15m",
		},
		// Phase 3.1 — admin-managed token TTLs. Both are read live on
		// every mint so an admin edit takes effect on the next call.
		{
			Key: "accessTokenTTL", Label: "Access token lifetime", Group: "login",
			Description: "Go duration string — how long an issued access token stays valid. Shorter = tighter security but more refresh round-trips. Range 1m–24h. Default 15m.",
			Type:        module.FieldDuration, Default: "15m",
			// UX aid only: ModuleConfigFields.tsx gives feedback before
			// save. Enforcement is the server-side ValidateConfig above —
			// this pattern is deliberately stricter than the parser (it
			// rejects compound forms like 1h30m that the server accepts),
			// which is acceptable for a hint but must never be treated as
			// the contract.
			Pattern: "^[0-9]+(s|m|h|d)$",
		},
		{
			Key: "passwordResetTokenTTL", Label: "Password reset link lifetime", Group: "login",
			Description: "Go duration string — how long the link in the reset-password email stays valid. Range 5m–24h. Default 30m.",
			Type:        module.FieldDuration, Default: "30m",
			Pattern: "^[0-9]+(s|m|h|d)$",
		},
		// ADR-0017 D1 — absolute session cap. One field for both audience
		// tiers: the operator console and the client surface share one
		// value, following the loginEnabledAdmin/loginEnabledClient
		// precedent that per-tier splitting is added only when a need
		// appears. Anchored on session.StartedAt, so enabling it needs no
		// migration and on upgrade it signs out sessions older than the
		// cap because that is what the existing data already records.
		{
			Key: "sessionAbsoluteTTL", Label: "Maximum session age", Group: "login",
			Description: "Maximum lifetime of a session from login, independent of activity. " +
				"When it elapses the user must authenticate again. Range 1h–89d; " +
				"empty disables the cap. Default 720h (30 days).",
			Type: module.FieldDuration, Default: "720h",
			Pattern: "^[0-9]+(s|m|h|d)$",
		},
		// Phase 3.6 — restrict the MFA factor types users can enroll.
		// Empty list = all methods allowed (the legacy default so an
		// untouched deployment observes no change).
		{
			Key: "mfaMethods", Label: "Allowed MFA methods", Group: "mfa",
			Description: "Comma-separated list of factor types users may enroll. Empty = all allowed. Valid: totp, webauthn, backup_codes.",
			Type:        module.FieldStringList, Default: "",
		},

		// Password Policy — site-wide rules enforced by passwordService.
		// ValidatePolicy on signup / change-password / reset. Defaults
		// match the legacy hardcoded behaviour (10..128 chars, no
		// complexity, HIBP on) so existing deployments observe no change
		// after the migration.
		{
			Key: "passwordMinLength", Label: "Minimum length", Group: "password",
			Description: "Minimum number of characters in a new password. Default 10. Recommend 12+.",
			Type:        module.FieldInt, Default: "10",
		},
		{
			Key: "passwordMaxLength", Label: "Maximum length", Group: "password",
			Description: "Upper bound on password length. Argon2id is not a bottleneck; raise this only if you have a concrete reason.",
			Type:        module.FieldInt, Default: "128",
		},
		{
			Key: "passwordRequireUpper", Label: "Require an uppercase letter", Group: "password",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "passwordRequireLower", Label: "Require a lowercase letter", Group: "password",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "passwordRequireDigit", Label: "Require a digit", Group: "password",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "passwordRequireSymbol", Label: "Require a symbol", Group: "password",
			Description: "Any non-alphanumeric character.",
			Type:        module.FieldBool, Default: "false",
		},
		{
			Key: "breachedPasswordCheck", Label: "Reject breached passwords (HIBP)", Group: "password",
			Description: "k-anonymity lookup against haveibeenpwned.com — only the first 5 chars of the SHA-1 hash leave the server. Disable for air-gapped deployments.",
			Type:        module.FieldBool, Default: "true",
		},

		// OAuth Providers — per-surface enable. The credential fields
		// stay where they are (one set per provider, shared across
		// audiences) but each provider can be exposed only on the
		// surfaces that should accept it. A provider that is configured
		// but disabled for a surface is filtered out of GET
		// /v1/auth/{tier}/providers and returns 403 oauth_disabled
		// from the start endpoints. Every toggle ships OFF by default:
		// a fresh install exposes no social-login button until the
		// super_admin both configures the provider's credentials AND
		// flips its surface toggle on. (A provider with no client ID is
		// already filtered out regardless — this toggle is the explicit
		// second gate.)
		{
			Key: "googleEnabledAdmin", Label: "Google on operator console", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "googleEnabledClient", Label: "Google on client app", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "appleEnabledAdmin", Label: "Apple on operator console", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "appleEnabledClient", Label: "Apple on client app", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "githubEnabledAdmin", Label: "GitHub on operator console", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "githubEnabledClient", Label: "GitHub on client app", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "discordEnabledAdmin", Label: "Discord on operator console", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},
		{
			Key: "discordEnabledClient", Label: "Discord on client app", Group: "oauth",
			Type: module.FieldBool, Default: "false",
		},

		// MFA — global feature flag + grace window. The privileged-role
		// list (super_admin / administrator / org_owner / org_admin) is
		// still hardcoded in services/mfa_policy.go; that's a follow-up
		// once we agree on UX for editing it. The master switch ships
		// OFF by default: a fresh install's first account is super_admin
		// (a privileged role), so seeding mfaEnabled=true would block
		// that operator from the very config writes needed to finish
		// setup (e.g. SMTP) with an MFA prompt for a factor they never
		// enrolled. Operators turn it on once a second factor is
		// enrolled. For today, operators can:
		//   - flip the master switch on/off at runtime (existing
		//     enrollments stay intact; when off, users can still verify
		//     voluntarily, but RoleRequiresMFA returns false)
		//   - tune how long a freshly-promoted admin has to enroll
		{
			Key: "mfaEnabled", Label: "Require MFA for privileged users", Group: "mfa",
			Description: "Master switch, OFF by default so a fresh install's first super_admin can configure the platform without being blocked by an MFA prompt they never enrolled. Turn this ON only after enrolling a second factor (TOTP/passkey) — otherwise privileged users hit the enrollment grace window on their next login. When off, RoleRequiresMFA returns false; existing enrollments are not deleted and users can still verify voluntarily.",
			Type:        module.FieldBool, Default: "false",
		},
		{
			Key: "mfaEnrollmentGraceDays", Label: "Enrollment grace period (days)", Group: "mfa",
			Description: "How many days a newly privileged user has to enroll a second factor before login returns 403 mfa_enrollment_required. Default 7.",
			Type:        module.FieldInt, Default: "7",
		},

		// Anti-abuse & Notifications — the "antiabuse" group. Operational guardrails on
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
			Key: "notifyUserOnNewDeviceLogin", Label: "Email user on first login from a new device", Group: "antiabuse",
			Description: "When on, sends an auth.new_device_login transactional email the first time a user logs in from a (deviceId, userUUID) pair the system has not seen before. Helps users notice unauthorised access on the same day it happens.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "notifyAdminOnSuspiciousLogin", Label: "Email admins on high-risk login", Group: "antiabuse",
			Description: "When on, every high-risk login (risk score ≥ 0.5) emails each address in the recipients list below in addition to notifying the user. Default off — recipients must be explicitly configured first.",
			Type:        module.FieldBool, Default: "false",
		},
		{
			Key: "suspiciousLoginRecipients", Label: "Suspicious-login admin recipients", Group: "antiabuse",
			Description: "Comma-separated list of admin email addresses notified on high-risk logins. Empty disables the admin email half regardless of the toggle above.",
			Type:        module.FieldStringList,
		},
		{
			Key: "ipAllowlistAdmin", Label: "IP allowlist (operator console)", Group: "antiabuse",
			Description: "Comma-separated list of CIDR ranges allowed to reach the operator host. Empty = open. Applied only to operator host traffic — the client API is unaffected. Example: 10.0.0.0/8, 192.0.2.5/32.",
			Type:        module.FieldStringList,
		},
		{
			Key: "ipBlocklistAdmin", Label: "IP blocklist (operator console)", Group: "antiabuse",
			Description: "Comma-separated list of CIDR ranges denied at the operator host. Evaluated after the allowlist — a blocked entry rejects the request even if it also matches the allowlist.",
			Type:        module.FieldStringList,
		},
		{
			Key: "geoBlockCountries", Label: "Country blocklist", Group: "antiabuse",
			Description: "Comma-separated ISO-3166-1 alpha-2 country codes (e.g. RU, KP) that cannot complete login on either tier. Requires the GeoIP resolver (AUTH_GEOIP_DB_PATH) — empty when GeoIP is disabled has no effect.",
			Type:        module.FieldStringList,
		},
		{
			Key: "inactiveAccountAutoDisableDays", Label: "Auto-disable inactive accounts after (days)", Group: "antiabuse",
			Description: "Disables a user account when its lastLogin is older than the configured number of days. Checked at login time so a stale account is denied at the next attempt without a periodic job. 0 = disabled.",
			Type:        module.FieldInt, Default: "0", Advanced: true,
		},

		// Sessions & Account — Phase 8 trivial toggles. Two existing
		// security behaviours surfaced as live-editable knobs.
		{
			Key: "revokeSessionsOnPasswordChange", Label: "Revoke sessions on password change", Group: "sessions",
			Description: "When on, a successful POST /v1/auth/{tier}/change-password also revokes the caller's current session id and every device-trust grant for the user. When off, password change leaves existing sessions alive (used for migrations or staged rollouts; not recommended in steady state). Default on.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "selfServiceAccountDeletionClient", Label: "Allow client users to self-delete (GDPR erase)", Group: "sessions",
			Description: "When on, Tier-2 client users can call POST /v1/me/dsr/erase to irreversibly wipe their personal data across every PII producer. When off (default), client tier returns 403 self_service_deletion_disabled and erasure must be triggered through the operator console. Operator-side erasure is unaffected.",
			Type:        module.FieldBool, Default: "false",
		},

		// OAuth signup allowance — Phase 9 small backlog. The eight
		// per-provider enable toggles live in this same "oauth" parent
		// group, declared above; they gate which buttons appear, and the
		// credentials in the "oauth.<provider>" child groups DependsOn
		// them. This pair is a different axis: it gates what happens when
		// an OAuth login arrives for an unknown email.
		// When off, the callback returns 403 oauth_signup_disabled
		// instead of provisioning a new account — useful when an
		// operator wants to allow existing users to sign in via OAuth
		// while keeping signups invitation-only.
		{
			Key: "oauthAllowSignupAdmin", Label: "Allow OAuth signups on operator console", Group: "oauth",
			Description: "When off, OAuth callbacks on the operator host that resolve to an unknown email return 403 instead of creating a new operator account. Existing users can still sign in.",
			Type:        module.FieldBool, Default: "true",
		},
		{
			Key: "oauthAllowSignupClient", Label: "Allow OAuth signups on client app", Group: "oauth",
			Description: "When off, OAuth callbacks on the client host that resolve to an unknown email return 403 instead of creating a new client account.",
			Type:        module.FieldBool, Default: "true",
		},

		// MFA — admin-managed list of roles that mandate a second factor.
		// Phase 9 small backlog. Empty falls back to the legacy hardcoded
		// list (super_admin, administrator, org_owner, org_admin) so an
		// unset value preserves today's behaviour. Adding a role here is
		// security-sensitive — broaden carefully.
		{
			Key: "mfaRequiredForRoles", Label: "Roles that require MFA", Group: "mfa",
			Description: "Comma-separated list of role names that mandate a second factor. Recognised system roles: super_admin, administrator, developer, manager, operator, guest. Recognised org roles: org_owner, org_admin, org_member. Empty restores the built-in default (super_admin, administrator, org_owner, org_admin).",
			Type:        module.FieldStringList,
		},
		{
			Key: "recoveryCodesCount", Label: "Recovery codes issued on enrollment", Group: "mfa",
			Description: "Number of one-shot backup codes minted when a user confirms TOTP enrollment. Default 10. Range 1–50 — outside that the legacy default (10) is used.",
			Type:        module.FieldInt, Default: "10", Advanced: true,
		},

		// OAuth account linking — Phase 10 of the auth-policy roadmap.
		// Today's flow auto-links an OAuth provider to an existing
		// account when the email matches. That's convenient but lets
		// an attacker who controls a verified email at the IdP take
		// over an existing Orkestra account whose owner used a
		// password. Operators in higher-assurance deployments turn
		// this off.
		{
			Key: "oauthAutoLinkByEmail", Label: "Auto-link OAuth provider to existing email account", Group: "oauth",
			Description: "When on (default), an OAuth callback for an existing Orkestra account (matched by email) attaches the provider to that user automatically. When off, the OAuth flow refuses with 403 oauth_link_disabled and the user must initiate linking from their account settings while authenticated. Recommended off for compliance-sensitive deployments.",
			Type:        module.FieldBool, Default: "true", Advanced: true,
		},
	}
}

// sessionExpiryEpochFloor is the timestamp below which an `expiresAt` is
// treated as "not really set" rather than "expired in the year 1".
//
// A zero time.Time marshals to BSON as 0001-01-01T00:00:00Z, which is in
// the past for every conceivable clock, so a bare TTL index deletes such a
// document on the TTL monitor's next 60-second pass. Any real retention
// deadline this code writes is `now + models.AuthSessionRetention`, i.e.
// decades above this floor, so the boundary can never exclude a legitimate
// row. Kept as a package-level var so the index spec and the test that pins
// it cannot drift apart.
var sessionExpiryEpochFloor = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// sessionRetentionPartialFilter scopes the session TTL indexes to documents
// that carry a plausible retention deadline, so a zero `expiresAt` is
// structurally undeletable by Mongo's TTL monitor. See the comment on the
// operator-sessions index for why that matters. ADR-0017 D7.
//
// Returned fresh per call: IndexSpec.PartialFilter is a map, and handing the
// same mutable instance to two collection specs would let any future mutation
// of one silently rewrite the other.
func sessionRetentionPartialFilter() map[string]any {
	return map[string]any{"expiresAt": map[string]any{"$gt": sessionExpiryEpochFloor}}
}

func (m *AuthModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		// Non-tier-split collections: security events are an audit log
		// keyed on userUUID alone, device-trust grants follow the user
		// record and the auth-path split does not need them per-tier.
		{Name: models.SecurityEventsCollection, Indexes: []module.IndexSpec{
			// Per-user activity timeline: the admin and self pages
			// both list "recent events for this user", sorted by
			// timestamp desc — compound (userUuid, timestamp desc).
			{Keys: map[string]int{"userUuid": 1, "timestamp": -1}},
			// EventType filter for the future "show only login_failed"
			// affordance + cross-user analytics queries.
			{Keys: map[string]int{"eventType": 1, "timestamp": -1}},
		}},
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
			// Serves the sweep's sorted, limited selection. Deliberately
			// NOT a TTL index: deletion at expiry is semantically safe,
			// but Mongo's TTL monitor cannot provide the bounded
			// per-cycle progress and backlog telemetry the first cleanup
			// of an upgraded installation requires. ADR-0017 D7.
			{OrderedKeys: []module.IndexKey{{Field: "expiresAt", Direction: 1}, {Field: "uuid", Direction: 1}}},
		}},
		{Name: models.ClientRefreshTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			{Keys: map[string]int{"familyId": 1}},
			// Serves the sweep's sorted, limited selection. Deliberately
			// NOT a TTL index: deletion at expiry is semantically safe,
			// but Mongo's TTL monitor cannot provide the bounded
			// per-cycle progress and backlog telemetry the first cleanup
			// of an upgraded installation requires. ADR-0017 D7.
			{OrderedKeys: []module.IndexKey{{Field: "expiresAt", Direction: 1}, {Field: "uuid", Direction: 1}}},
		}},
		{Name: models.OperatorRefreshTokenFamiliesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"familyId": 1}, Unique: true},
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		}},
		{Name: models.ClientRefreshTokenFamiliesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"familyId": 1}, Unique: true},
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		}},
		{Name: models.OperatorSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			// expiresAt is the retention deadline, so ExpireAt (delete AT
			// the timestamp) is the exact expression of the intent —
			// not TTL, which would add a second offset on top. ADR-0017 D7.
			//
			// The partial filter makes the delete safe BY CONSTRUCTION.
			// AuthSessionDoc.ExpiresAt is `bson:"expiresAt"` with no
			// omitempty, so a zero value serialises as a year-1 date and
			// the TTL monitor would delete that row on its very next pass
			// — irreversibly, for a session that has not expired at all.
			// Excluding pre-2000 timestamps removes the whole class:
			// such a row is simply not in the index, so the monitor never
			// considers it. This replaces a prose runbook ("count them
			// first, in both collections, in both environments, and
			// remember to") with a structural guarantee.
			{
				Keys:          map[string]int{"expiresAt": 1},
				ExpireAt:      true,
				PartialFilter: sessionRetentionPartialFilter(),
			},
		}},
		{Name: models.ClientSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{
				Keys:          map[string]int{"expiresAt": 1},
				ExpireAt:      true,
				PartialFilter: sessionRetentionPartialFilter(),
			},
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
		{Name: models.ServiceAccountCredentialsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"clientId": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
		}},
	}
}

func (m *AuthModule) Init(deps *module.Dependencies) error {
	cfg := m.cfg
	if cfg == nil {
		return fmt.Errorf("auth: NewModule was called without *config.Config — check cmd/server/catalog.go")
	}
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
	// Platform-default Tier-1 tenant resolver (iface.DefaultTenantProvider,
	// tenant module PR 3). Optional: the tenant module's Init runs before
	// auth's in the topological order, so it's already registered by now,
	// but a missing key is tolerated (loadMemberships falls through to the
	// owner-first rule). Wired ONLY on the operator-audience service —
	// serviceJWT and clientJWT below deliberately do not get it, so a
	// Tier-2 client-portal token can never consult the internal platform
	// default.
	if dp, ok := module.GetTyped[iface.DefaultTenantProvider](deps.Services, module.ServiceDefaultTenantProvider); ok {
		operatorJWT.SetDefaultTenantProvider(dp)
	}

	// OAuth state service + signed-state JWT secret (D-6). The HMAC
	// secret is derived from the JWT private key so every replica
	// agrees without an extra env var; rotation is implicit when the
	// JWT key pair rotates.
	atomicRedis, ok := deps.RedisAdapter.(services.AtomicTakeRedisClient)
	if !ok {
		return fmt.Errorf("auth: Redis adapter lacks atomic GETDEL support")
	}
	// EVAL is required for the same reason GETDEL is: the attempt
	// counters are the only brute-force bound on every anonymous auth
	// surface, and the script is what makes their count/TTL/healing
	// atomic. A client that cannot run it would leave the platform with
	// no lockout at all, which must be a boot failure, not a silent
	// degradation.
	scriptRedis, ok := deps.RedisAdapter.(services.ScriptRedisClient)
	if !ok {
		return fmt.Errorf("auth: Redis adapter lacks EVAL support")
	}
	attemptCounter := services.NewRedisAttemptCounter(scriptRedis, logger)
	redisStore := services.NewRedisOAuthStateStore(atomicRedis)
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
	// global. The TTL argument is deprecated and ignored — entries live
	// for the fixed maximum access-token lifetime plus clock skew, which
	// is the only window that survives a live policy change in either
	// direction (ADR-0017 D5).
	sessionRevocationSvc := services.NewSessionRevocationService(
		deps.RedisAdapter,
		0,
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
	// Operator-only break-glass (spec §4.2): read once at boot, handed to
	// the ONE decision path allowed to see it. The WARN repeats on every
	// boot while the variable is set so a forgotten override stays loud.
	if cfg.Auth.OperatorPasswordLoginBreakGlass {
		authPolicy.SetOperatorBreakGlass(true)
		logger.Warn("auth: operator password-login BREAK-GLASS override is ACTIVE — " +
			"persisted policy is bypassed for operator login (and its MFA continuation) only; " +
			"repair the OAuth configuration, then unset AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS and restart")
	}
	// Hand the policy to the (already-constructed) shared password
	// service so length / complexity / HIBP rules can be edited live
	// at /admin/modules/auth without a restart.
	passwordSvc.SetPolicy(authPolicy)
	// Phase 3.1: hand the same policy to the operator JWT service so
	// accessTokenTTL is read live on every mint. The client JWT
	// service is constructed later in this function — we wire it
	// inline at that site.
	operatorJWT.SetPolicy(authPolicy)

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

	// auth_security_events is non-tier-split — one repo instance shared
	// by both operator + client tier bundles. Phase 2.1 of the
	// core-completion epic: RecordSecurityEvent now persists.
	securityEventRepo := repository.NewSecurityEventRepository(deps.DB)

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
		attemptCounter:           attemptCounter,
		geoResolver:              geoResolver,
		velocityKmh:              velocityKmh,
		frontendURL:              cfg.Server.FrontendURL,
		requireEmailVerification: getBoolEnv("AUTH_REQUIRE_EMAIL_VERIFICATION", cfg.IsProductionLike()),
		appName:                  getEnvOrDefault("APP_NAME", "Orkestra"),
		supportEmail:             os.Getenv("SUPPORT_EMAIL"),
		mfaIssuer:                mfaIssuer,
		webauthnRP:               webauthnRP,
		authPolicy:               authPolicy,
		securityEventRepo:        securityEventRepo,
	}

	// ADR-0003 PR-D D-9: per-audience refresh-cookie domains. Each
	// tier's handler trio gets the matching value so refresh cookies are
	// scoped to the host that minted them — operator tokens stay on
	// `console.*`, client tokens on `api.*`. An empty value mints the
	// cookie without a Domain attribute (scoped to the minting host).
	operatorCookieDomain := cfg.Auth.Cookie.OperatorDomain
	clientCookieDomain := cfg.Auth.Cookie.ClientDomain

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
	m.operatorAuthHandler.SetSPAURL(opDeps.frontendURL)
	// Avatar pipeline: hand the blob store so /me + login + refresh +
	// session-poll resolve uploaded avatars to a fresh presigned GET.
	// Without this, oauth_*/uploaded users see initials in the navbar
	// because the raw User.Avatar field is empty. The handler gets it
	// for GET /v1/auth/operator/me; the service gets it for every code
	// path that builds a UserManagementResponse for the wire.
	if store, ok := module.GetTyped[blob.Store](deps.Services, module.ServiceBlobStore); ok {
		m.operatorAuthHandler.SetBlobStore(store)
		opBundle.authService.SetBlobStore(store)
		opBundle.passwordSvc.SetBlobStore(store)
	}

	// User-security plan Phase 1: hand the revocation store to the
	// auth service so RevokeUserSession / RevokeAllUserSessionsExcept
	// push to the same Redis set the AuthMiddleware consults on every
	// authenticated request. Without this, in-flight access tokens
	// would stay valid until the per-token TTL ticked over.
	opBundle.authService.SetSessionRevocation(sessionRevocationSvc)
	// Same store for the password service: ResetPassword / ChangePassword
	// push every evicted sid so a credential change kills access tokens
	// already in flight instead of waiting out their TTL.
	opBundle.passwordSvc.SetSessionRevocation(sessionRevocationSvc)

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
	m.operatorMFAHandler.SetAuditRecorder(opBundle.authService)
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
		m.operatorWebAuthnHandler.SetPolicy(authPolicy)
		deps.Services.Register(module.ServiceWebAuthn, opBundle.webauthnSvc)
	}

	// Admin user-auth handler — operator-tier only. Reuses the operator
	// auth service for the GET aggregator + OAuth unlink, and the
	// operator password-auth service (which satisfies
	// iface.AdminAuthInviter via structural typing) for the
	// send-password-reset / resend-verification routes.
	m.operatorAdminUserAuthHandler = handlers.NewAdminUserAuthHandler(opBundle.authService, opBundle.passwordSvc, securityEventRepo)

	// Self-service security-center handler — operator tier this
	// iteration. Wired to the operator authService + mfaSvc so reads
	// + revokes hit operator_sessions / operator_mfa_factors. The
	// route gates (RequireGlobal vs RequireGlobal+RequireStepUp(5m))
	// are applied in RegisterRoutes.
	m.operatorSelfUserAuthHandler = handlers.NewSelfUserAuthHandler(opBundle.authService, opBundle.mfaSvc)

	// Service-audience JWT service (Task 8). Mirrors the operatorJWT
	// construction above verbatim — same key pair, same environment, same
	// TTLs — only the audience claim differs (aud="service" instead of
	// "operator"), so a service-account token cannot be replayed against
	// either the operator or client host mux (RequireAudience rejects
	// cross-audience tokens).
	serviceJWT, err := services.NewJWTServiceWithAudience(
		cfg.Auth.JWT.PrivateKey,
		cfg.Auth.JWT.PublicKey,
		cfg.Server.Environment,
		services.AudienceService,
		cfg.Auth.JWT.AccessTokenExpiry,
		cfg.Auth.JWT.RefreshTokenExpiry,
	)
	if err != nil {
		return fmt.Errorf("service jwt: %w", err)
	}
	serviceJWT.SetTenantProvider(tenantProvider)
	serviceJWT.SetPolicy(authPolicy)

	// operatorUser must additionally satisfy ServiceAccountLister
	// (Task 1) — the user module's ServiceOperatorUserProvider always
	// implements it (additive-only rule), so a failed assertion here
	// means the user module's provider changed underneath auth. Fail
	// loud at boot, not at the first /admin/service-accounts request.
	saLister, ok := operatorUser.(iface.ServiceAccountLister)
	if !ok {
		return fmt.Errorf("auth: operator user provider does not implement iface.ServiceAccountLister")
	}
	saCredRepo := repository.NewServiceAccountCredentialRepository(deps.DB)
	// serviceJWT (services.JWTService) already declares both methods
	// ServiceTokenMinter requires, so it satisfies the minter seam
	// structurally — no assertion needed.
	m.serviceAccountService = services.NewServiceAccountService(
		saCredRepo, operatorUser, saLister, passwordSvc, serviceJWT, rateLimiter)
	// securityEventRepo/authPolicy already exist in scope from the
	// operator-tier wiring above — thread them into the service the
	// same way the sibling AdminUserAuthHandler receives its deps (see
	// NewAdminUserAuthHandler below) and the way Login refreshes its
	// lockout config from authPolicy.
	m.serviceAccountService.SetSecurityEventRepo(securityEventRepo)
	m.serviceAccountService.SetPolicy(authPolicy)
	m.serviceTokenHandler = handlers.NewServiceTokenHandler(m.serviceAccountService)
	m.serviceAccountAdminHandler = handlers.NewServiceAccountAdminHandler(m.serviceAccountService)

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
	// Phase 3.1: live accessTokenTTL lookup for the client tier too.
	clientJWT.SetPolicy(authPolicy)

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
	m.clientAuthHandler.SetSPAURL(clDeps.frontendURL)
	if store, ok := module.GetTyped[blob.Store](deps.Services, module.ServiceBlobStore); ok {
		m.clientAuthHandler.SetBlobStore(store)
		clBundle.authService.SetBlobStore(store)
		clBundle.passwordSvc.SetBlobStore(store)
	}
	clBundle.authService.SetSessionRevocation(sessionRevocationSvc)
	clBundle.passwordSvc.SetSessionRevocation(sessionRevocationSvc)

	// §4.7: the unlink guards count usable links through the same strict
	// one-read resolver the web flow uses; the closure keeps the resolver
	// type out of the services package.
	providerUsability := func(ctx context.Context, audience services.PolicyAudience, p iface.OAuthProvider) (bool, error) {
		_, ok, err := oauthResolver.OAuthWebProviderUsable(ctx, audience, models.OAuthProvider(string(p)))
		if err != nil {
			return false, err
		}
		return ok, nil
	}
	opBundle.authService.SetProviderUsability(providerUsability)
	clBundle.authService.SetProviderUsability(providerUsability)

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
	m.clientMFAHandler.SetAuditRecorder(clBundle.authService)
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
		m.clientWebAuthnHandler.SetPolicy(authPolicy)
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
	// default an unaware consumer (setup wizard, dev token) gets.
	// Audience-aware consumers (onboarding, compliance audit sink)
	// request the per-tier key directly.
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

	// MFA-enrollment lookup: reports whether a user has any TOTP or
	// WebAuthn factor on the tier the caller's token was minted for.
	// Consumed by AuthMiddleware.RequireStepUp to split step-up failures
	// into MFA / password-reconfirm / enroll-first buckets. Tier
	// resolution: audience claim picks the matching mfa_factors
	// collection; empty/unknown audience falls back to operator (today's
	// canonical tier) so legacy single-aud tokens keep working.
	operatorMFA := opBundle.mfaFactorRepo
	clientMFA := clBundle.mfaFactorRepo
	mfaEnrollmentLookup := func(ctx context.Context, audience, userUUID string) (bool, error) {
		if userUUID == "" {
			return false, nil
		}
		repo := operatorMFA
		if audience == "client" {
			repo = clientMFA
		}
		if repo == nil {
			return false, nil
		}
		if totp, err := repo.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP); err == nil && totp != nil {
			return true, nil
		} else if err != nil && !stderrors.Is(err, repository.ErrMFAFactorNotFound) {
			return false, err
		}
		if wa, err := repo.FindByUserAndType(ctx, userUUID, models.MFAFactorWebAuthn); err == nil && wa != nil && len(wa.WebAuthnCredentials) > 0 {
			return true, nil
		} else if err != nil && !stderrors.Is(err, repository.ErrMFAFactorNotFound) {
			return false, err
		}
		return false, nil
	}
	deps.Services.Register(module.ServiceMFAEnrollmentLookup, authMiddleware.MFAEnrollmentLookup(mfaEnrollmentLookup))

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

	// Maintenance wiring (ADR-0017 D7). A Redis adapter that cannot
	// satisfy the lease contract disables the sweep rather than running
	// it unelected — every replica sweeping would make the per-cycle
	// bound meaningless. Nothing here can fail Init or Start: the sweep
	// is maintenance, never an authentication dependency.
	m.logger = logger
	if lease, ok := deps.RedisAdapter.(services.LeaseRedisClient); ok {
		m.sweepLease = services.NewMaintenanceLease(lease, tokenSweepLeaseKey, logger)
		// The two names are literals, not derived from audienceTier:
		// they are the closed Prometheus label set of ADR-0017 D8, and
		// a rename of the internal enum must not silently retag every
		// sweep series as "unknown".
		m.sweepTiers = []sweepTier{
			{name: "operator", repo: opBundle.refreshTokenRepo},
			{name: "client", repo: clBundle.refreshTokenRepo},
		}
	} else {
		logger.Warn("auth: Redis adapter does not support the maintenance lease; refresh-token sweep disabled")
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
	d, ok := utils.ParseDuration(raw)
	if !ok || d <= 0 {
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

	// Service-account client-credentials grant (Task 8): public, single
	// operator-tier path (no per-audience mount split — service accounts
	// are an operator-tier concept).
	m.serviceTokenHandler.RegisterPublicRoutes(ri.Operator.PublicAPI)

	// Service-account admin surface (Task 9): gated
	// /v1/admin/service-accounts routes, operator-tier only. Read group
	// has no step-up (list/get are read-only); manage group requires a
	// fresh <5min step-up because every route there either creates a
	// credential-bearing account or mints/revokes a credential.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("auth.service_accounts.read"))
		m.serviceAccountAdminHandler.RegisterReadRoutes(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("auth.service_accounts.manage"))
		r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
		m.serviceAccountAdminHandler.RegisterManageRoutes(humachi.New(r, ri.APIConfig))
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
		// Tier-aware client-user MFA reset — same gate, different
		// path, different handler instance. Mounted on the operator
		// router because admins act from the operator console.
		m.clientMFAHandler.RegisterClientAdminRoutes(api)
	})

	// Admin user-auth surface — four endpoints under
	// /v1/admin/users/{userId}/... each gated by its own permission so
	// the audit trail stays per-action. Step-up applies only to OAuth
	// unlink (credential removal); password-reset / resend-verification
	// dispatch a notification but do not read or remove a secret.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.admin"))
		api := humachi.New(r, ri.APIConfig)
		m.operatorAdminUserAuthHandler.RegisterReadAuthMethodsRoute(api)
		// Phase 2.3: per-user audit timeline lives behind the same gate
		// (reading audit rows is incidental to user administration).
		m.operatorAdminUserAuthHandler.RegisterSecurityEventsRoute(api)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.password_reset"))
		api := humachi.New(r, ri.APIConfig)
		m.operatorAdminUserAuthHandler.RegisterPasswordResetRoute(api)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.email_verify_resend"))
		api := humachi.New(r, ri.APIConfig)
		m.operatorAdminUserAuthHandler.RegisterResendVerificationRoute(api)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.oauth_unlink"))
		r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
		api := humachi.New(r, ri.APIConfig)
		m.operatorAdminUserAuthHandler.RegisterOAuthUnlinkRoute(api)
	})

	// Self-service security-center surface — operator tier this
	// iteration. Read endpoints under RequireGlobal(); destructive
	// endpoints (OAuth unlink, session revoke, revoke-all) under
	// RequireGlobal()+RequireStepUp(5m) so a fresh MFA proof is
	// required for credential / session removal.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.operatorSelfUserAuthHandler.RegisterReadRoutes(api, handlers.OperatorMount)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
		api := humachi.New(r, ri.APIConfig)
		m.operatorSelfUserAuthHandler.RegisterStepUpRoutes(api, handlers.OperatorMount)
		// Linking a new OAuth identity adds a credential, same shape
		// as unlinking removes one — apply the same RequireStepUp(5m)
		// gate so a hijacked session can't silently attach a
		// persistence vector.
		m.operatorAuthHandler.RegisterOAuthLinkRoute(api, handlers.OperatorMount)
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
		// Client-tier web logins complete HERE (spec §4.10): the
		// operator-host callback relays them because only this host can set
		// the client refresh cookie and verify the state cookie it set at
		// start.
		m.clientAuthHandler.RegisterOAuthRelayRoute(ri.ClientRouter)
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
