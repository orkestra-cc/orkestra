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
	authHandler        *handlers.AuthHandler
	passwordHandler    *handlers.PasswordAuthHandler
	mfaHandler         *handlers.MFAHandler
	webauthnHandler    *handlers.WebAuthnHandler    // optional — nil when WebAuthn isn't configured
	deviceTrustHandler *handlers.DeviceTrustHandler // Section C item #3

	// ADR-0003 PR-D D-4: operator-tier handler instances bound to the
	// operator authTierBundle (D-2). Mounted under /v1/auth/operator/...
	// in RegisterRoutes alongside the legacy /v1/auth/... mounts; the
	// legacy mounts are removed by D-8. These fields stay nil when the
	// operator user provider isn't registered (PR-B's
	// USER_TIER_SPLIT_ENABLED gate is false), in which case
	// RegisterRoutes silently skips the operator mounts and only the
	// legacy paths are reachable. webauthn handler stays nil when
	// passkeys are disabled at boot, mirroring the legacy nil-gate.
	operatorAuthHandler     *handlers.AuthHandler
	operatorPasswordHandler *handlers.PasswordAuthHandler
	operatorMFAHandler      *handlers.MFAHandler
	operatorWebAuthnHandler *handlers.WebAuthnHandler

	// ADR-0003 PR-D D-5: client-tier handler instances bound to the
	// client authTierBundle. Same shape as the operator block above but
	// tied to client_* collections + a JWT service that stamps
	// aud=client on every minted token, so /v1/auth/client/* requests
	// produce client-audience access + refresh tokens that only the
	// client host mux accepts. Nil when ServiceClientUserProvider is
	// not registered — the client mounts are silently skipped and only
	// the operator + legacy paths are reachable.
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
	}
}

func (m *AuthModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.OAuthProvidersCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"userUuid": 1, "provider": 1}, Unique: true},
		}},
		{Name: models.RefreshTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			// Block C: family lookup for RevokeFamily / replay detection.
			{Keys: map[string]int{"familyId": 1}},
		}},
		{Name: models.AuthSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: models.SecurityEventsCollection},
		{Name: models.EmailTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"tokenHash": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			// TTL: documents are removed 24h after ExpiresAt (max TTL for both verify and reset).
			{Keys: map[string]int{"expiresAt": 1}, TTL: 24 * time.Hour},
		}},
		{Name: models.MFAFactorsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1, "type": 1}, Unique: true},
		}},
		// Device trust grants (Section C item #3): "remember this
		// device for 30 days" rows that let a returning user skip the
		// MFA prompt on login. ExpireAt reaps rows when trustedUntil
		// passes; (userUuid, deviceId) non-unique because we keep
		// revoked history for audit — the repo enforces one ACTIVE
		// row per pair on insert.
		{Name: models.DeviceTrustCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1, "deviceId": 1}},
			{Keys: map[string]int{"trustedUntil": 1}, ExpireAt: true},
		}},

		// ADR-0003 PR-B: tier-split auth collections. Mirror the
		// legacy auth_* indexes so the tier-aware repos can be a
		// drop-in replacement once USER_TIER_SPLIT_ENABLED flips. Each
		// pair below shares an identical IndexSpec set with its legacy
		// sibling; only the collection name differs.
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

	// Repositories
	authRepo, err := repository.NewAuthRepository(deps.DB)
	if err != nil {
		return err
	}
	oauthProviderRepo := repository.NewOAuthProviderRepository(deps.DB)
	refreshTokenRepo := repository.NewRefreshTokenRepository(deps.DB)
	authSessionRepo := repository.NewAuthSessionRepository(deps.DB)
	emailTokenRepo := repository.NewEmailTokenRepository(deps.DB)
	mfaFactorRepo := repository.NewMFAFactorRepository(deps.DB)
	deviceTrustRepo := repository.NewDeviceTrustRepository(deps.DB)
	// Device-trust service (Section C item #3). Duration is env-
	// overridable via AUTH_DEVICE_TRUST_DURATION (Go duration string,
	// e.g. "168h" for 7 days). Unset falls back to 30 days.
	deviceTrustDuration := parseDurationEnv("AUTH_DEVICE_TRUST_DURATION", models.DeviceTrustDuration)
	deviceTrustSvc := services.NewDeviceTrustService(deviceTrustRepo, deviceTrustDuration, logger)

	// OAuth provider factory + live config resolver.
	//
	// Provider configs live in the module_configs document and are resolved
	// per-request by OAuthConfigResolver — seeded on first boot from the
	// OAUTH_* env vars declared in ConfigSchema(), then owned by the admin
	// UI at /admin/modules. The factory itself holds an empty map; each
	// handler call passes a freshly resolved config through CreateProvider's
	// override parameter so secret rotations take effect without a restart.
	providerFactory := services.NewOAuthProviderFactory(
		map[models.OAuthProvider]*services.OAuthProviderConfig{},
		deps.RedisAdapter,
	)
	oauthResolver := services.NewOAuthConfigResolver(deps.ConfigService)

	// JWT service. The tenant provider is wired in so that token issuance
	// can embed the user's current memberships in the JWT. The environment
	// is stamped into the iss claim (orkestra.<env>) so a token minted in
	// one deployment is rejected by another even if the signing keys ever
	// overlap. Keys themselves should differ per environment — this claim
	// is defense in depth.
	jwtService := services.NewJWTService(
		cfg.Auth.JWT.PrivateKey,
		cfg.Auth.JWT.PublicKey,
		cfg.Server.Environment,
		cfg.Auth.JWT.AccessTokenExpiry,
		cfg.Auth.JWT.RefreshTokenExpiry,
	)
	tenantProvider := module.MustGetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	jwtService.SetTenantProvider(tenantProvider)

	// OAuth state service
	redisStore := services.NewRedisOAuthStateStore(deps.RedisAdapter)
	oauthStateService := services.NewOAuthStateService(redisStore)

	// ADR-0003 PR-D D-6: derive the HMAC secret used to sign OAuth
	// state JWTs from the deployment's JWT private key. Every replica
	// of the monolith reaches the same secret without an extra env
	// var, and the secret rotates implicitly whenever the JWT key
	// pair rotates. nil private key means JWT issuance itself is
	// disabled — leave the state secret nil and let the OAuth start
	// endpoints surface the misconfiguration with a 500.
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

	// Get user service from registry
	userService := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceUserService)

	// Auth service
	// First-admin claimer is shared between OAuth and password signup. The
	// lookup is lifted here from its earlier spot in the password section
	// so both services see the same instance.
	var firstAdminClaimer services.FirstAdminClaimer
	if c, ok := module.GetTyped[services.FirstAdminClaimer](deps.Services, module.ServiceFirstAdminClaimer); ok {
		firstAdminClaimer = c
	} else {
		logger.Warn("first-admin claimer not wired — signup flows will fall through to non-atomic first-user heuristic")
	}

	// MFA challenge store is needed by both the auth service (OAuth login
	// partial response) and the password auth service (password login
	// partial response), so build it before either consumer.
	mfaChallengeSvc := services.NewMFAChallengeService(redisStore)

	// Section C item #1: login-risk scorer. Reads session history through
	// the auth_sessions repo to compute new_device/new_ip/rapid_ip_change
	// factors. Shared by the OAuth and password login paths so both surface
	// a consistent score on the SessionDoc.RiskScore field.
	// Section C item #4: GeoIP-backed impossible-travel detection.
	// FromEnv returns a real resolver when AUTH_GEOIP_DB_PATH points
	// at a MaxMind .mmdb (and the MaxMind library is linked in via a
	// follow-up commit), or a NoopResolver otherwise. The scorer
	// silently skips the impossible_travel factor when the resolver
	// is noop — other factors still compute.
	geoResolver := geoip.FromEnv(logger)
	velocityKmh := parseFloatEnv("AUTH_GEOIP_VELOCITY_THRESHOLD_KMH", services.DefaultImpossibleTravelVelocityKmh)
	riskAssessmentSvc := services.NewRiskAssessmentServiceWithGeoIP(authSessionRepo, geoResolver, velocityKmh, logger)

	authService, err := services.NewAuthService(&services.AuthConfig{
		AuthRepo:            authRepo,
		UserService:         userService,
		TenantProvider:      tenantProvider,
		OAuthProviderRepo:   oauthProviderRepo,
		RefreshTokenRepo:    refreshTokenRepo,
		AuthSessionRepo:     authSessionRepo,
		JWTService:          jwtService,
		MFAFactorRepo:       mfaFactorRepo,
		MFAChallengeService: mfaChallengeSvc,
		FirstAdminClaimer:   firstAdminClaimer,
		RiskAssessment:      riskAssessmentSvc,
	})
	if err != nil {
		return err
	}

	// Session revocation list (Block D): Redis-backed set of revoked `sid`
	// claims checked on every authenticated request. Populated on logout,
	// password reset, and admin kill-session so a stolen access token stops
	// working instantly instead of staying valid until its TTL expires.
	sessionRevocationSvc := services.NewSessionRevocationService(
		deps.RedisAdapter,
		cfg.Auth.JWT.AccessTokenExpiry,
		logger,
	)

	// Auth handler. The legacy mount stamps an empty tier on the state
	// JWT so callbacks for /v1/auth/oauth/login flows self-handle on
	// this handler's authService instead of dispatching, preserving
	// pre-D-6 behaviour for any in-flight legacy flows.
	m.authHandler = handlers.NewAuthHandler(
		authService,
		providerFactory,
		oauthResolver,
		oauthStateService,
		oauthProviderRepo,
		jwtService,
		cfg,
	)
	m.authHandler.SetSessionRevocation(sessionRevocationSvc)
	m.authHandler.SetStateSecret(oauthStateSecret)

	// Password auth service — depends on notification module (optional).
	passwordSvc := services.NewPasswordService(logger, true)
	var notifier iface.NotificationSender
	if n, ok := module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender); ok {
		notifier = n
	}

	// Section C item #5: suspicious-login notifier. Shares a single
	// SecurityEventService instance with the PII producer below so
	// user-facing security history + GDPR DSR export read the same
	// rows. A nil NotificationSender disables only the email half;
	// the security-event recording still fires whenever a login is
	// scored. Construction may fail if the Mongo indexes on
	// auth_security_events can't be built — the notifier stays nil
	// in that case and logs fall through to a zero-impact no-op.
	securityEventSvc, securityEventErr := services.NewSecurityEventService(deps.DB)
	if securityEventErr != nil {
		logger.Warn("auth: security event service init failed; suspicious-login notifier disabled",
			slog.String("error", securityEventErr.Error()))
	}
	var suspiciousLoginNotifierSvc services.SuspiciousLoginNotifier
	if securityEventSvc != nil {
		suspiciousLoginNotifierSvc = services.NewSuspiciousLoginNotifier(services.NotifierConfig{
			Events:       securityEventSvc,
			Notifier:     notifier,
			AppName:      getEnvOrDefault("APP_NAME", "Orkestra"),
			SupportEmail: os.Getenv("SUPPORT_EMAIL"),
			FrontendURL:  cfg.Server.FrontendURL,
			Logger:       logger,
		})
	}

	rateLimiter := sharederrors.NewRateLimiter()

	passwordAuthSvc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService:              userService,
		TenantProvider:           tenantProvider,
		PasswordService:          passwordSvc,
		JWTService:               jwtService,
		EmailTokenRepo:           emailTokenRepo,
		RefreshTokenRepo:         refreshTokenRepo,
		AuthSessionRepo:          authSessionRepo,
		MFAFactorRepo:            mfaFactorRepo,
		MFAChallengeService:      mfaChallengeSvc,
		FirstAdminClaimer:        firstAdminClaimer,
		RiskAssessment:           riskAssessmentSvc,
		DeviceTrust:              deviceTrustSvc,
		SuspiciousLoginNotifier:  suspiciousLoginNotifierSvc,
		Notifier:                 notifier,
		RateLimiter:              rateLimiter,
		FrontendURL:              cfg.Server.FrontendURL,
		RequireEmailVerification: getBoolEnv("AUTH_REQUIRE_EMAIL_VERIFICATION", cfg.IsProductionLike()),
		AppName:                  getEnvOrDefault("APP_NAME", "Orkestra"),
		SupportEmail:             os.Getenv("SUPPORT_EMAIL"),
		Logger:                   logger,
	})

	m.passwordHandler = handlers.NewPasswordAuthHandler(
		passwordAuthSvc,
		cfg.Auth.Cookie.Name,
		cfg.Auth.Cookie.Domain,
		cfg.Auth.Cookie.Secure,
	)
	m.passwordHandler.SetSessionRevocation(sessionRevocationSvc)

	// MFA orchestrator — issuer drives the TOTP provisioning URI label.
	// Borrows the password service's argon2id hasher for backup-code
	// storage so we don't ship a second hasher.
	mfaIssuer := getEnvOrDefault("APP_NAME", "Orkestra")

	// WebAuthn relying party — resolved once and reused by both the
	// legacy module-level WebAuthn wiring and the ADR-0003 PR-D D-4
	// per-tier authTierBundle, so an env-misconfiguration produces a
	// single warning rather than two and the tier bundles silently
	// match the legacy "passkeys disabled" decision. Nil when neither
	// `WEBAUTHN_RP_ID`/`WEBAUTHN_RP_ORIGINS` nor `FRONTEND_URL` resolve
	// to a valid RP.
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

	mfaSvc := services.NewMFAService(mfaFactorRepo, mfaChallengeSvc, passwordSvc, mfaIssuer, logger)
	mfaSvc.SetDeviceTrust(deviceTrustSvc)

	m.mfaHandler = handlers.NewMFAHandler(
		mfaSvc,
		mfaChallengeSvc,
		jwtService,
		userService,
		passwordAuthSvc,
		cfg.Auth.Cookie.Name,
		cfg.Auth.Cookie.Domain,
		cfg.Auth.Cookie.Secure,
	)
	m.mfaHandler.SetDeviceTrust(deviceTrustSvc)

	// Device-trust self-service endpoints (Section C item #3): list +
	// revoke the caller's active "remember this device 30d" grants.
	// Granting happens on the MFA login-verify endpoints above.
	m.deviceTrustHandler = handlers.NewDeviceTrustHandler(deviceTrustSvc)

	// WebAuthn — wires the legacy WebAuthnService against the legacy
	// mfa_factors collection, off the hoisted webauthnRP. nil RP means
	// the deployment hasn't configured one (or env was malformed) and
	// the legacy module's passkey routes simply don't mount; the
	// frontend hides the passkeys UI based on /v1/auth/me/mfa returning
	// webauthnCredentials=0.
	if webauthnRP != nil {
		webauthnSvc := services.NewWebAuthnService(webauthnRP, mfaFactorRepo, mfaChallengeSvc, logger)
		m.mfaHandler.SetWebAuthn(webauthnSvc)
		m.webauthnHandler = handlers.NewWebAuthnHandler(
			webauthnSvc,
			mfaChallengeSvc,
			jwtService,
			userService,
			passwordAuthSvc,
			cfg.Auth.Cookie.Name,
			cfg.Auth.Cookie.Domain,
			cfg.Auth.Cookie.Secure,
		)
		m.webauthnHandler.SetDeviceTrust(deviceTrustSvc)
		deps.Services.Register(module.ServiceWebAuthn, webauthnSvc)
		passwordAuthSvc.SetWebAuthnAvailability(webauthnAvailabilityChecker{svc: webauthnSvc})
		authService.SetWebAuthnAvailability(webauthnAvailabilityChecker{svc: webauthnSvc})
	}

	// Register services for main.go middleware setup
	deps.Services.Register(module.ServiceAuthService, authService)
	deps.Services.Register(module.ServiceJWTService, jwtService)
	deps.Services.Register(module.ServicePasswordService, passwordSvc)
	deps.Services.Register(module.ServicePasswordAuthService, passwordAuthSvc)
	deps.Services.Register(module.ServiceSessionRevocation, sessionRevocationSvc)

	// ADR-0003 PR-D D-2/D-4: tier-aware auth/password-auth services + per-tier
	// MFA + WebAuthn services. Each bundle wraps its own AuthService +
	// PasswordAuthService + MFAService + (optional) WebAuthnService bound
	// to the matching tier's session/refresh/oauth/mfa/email-token repos
	// (PR-B) and the matching OperatorUserProvider / ClientUserProvider.
	// Registered alongside the legacy ServiceAuthService /
	// ServicePasswordAuthService keys so PR-D's per-audience handlers
	// (D-4 operator paths, D-5 client paths) can pull the audience-
	// matching service without touching the legacy wiring. The legacy
	// bundle stays canonical until D-8 deletes the legacy auth_*
	// collections. Shared singletons (passwordSvc, jwtService,
	// mfaChallengeSvc, deviceTrustSvc, suspiciousLoginNotifierSvc,
	// rateLimiter, webauthnRP, etc.) are reused across all tiers — only
	// the repos and risk scorer differ.
	//
	// `webauthnRP` is sourced from the same env-resolved relying party
	// used to build the legacy WebAuthn service above. It's pulled out
	// of the module-level wiring via the local `webauthnRP` var the
	// passkey block left behind; nil here means passkeys are disabled
	// at boot and the per-tier bundles get a nil webauthnSvc to match.
	commonTierDeps := tierBundleDeps{
		db:                       deps.DB,
		logger:                   logger,
		tenantProvider:           tenantProvider,
		authRepo:                 authRepo,
		jwtService:               jwtService,
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
	}

	if operatorUser, ok := module.GetTyped[iface.UserProvider](deps.Services, module.ServiceOperatorUserProvider); ok {
		opDeps := commonTierDeps
		opDeps.tier = tierOperator
		opDeps.userProvider = operatorUser
		opBundle, err := buildAuthTierBundle(opDeps)
		if err != nil {
			return err
		}
		deps.Services.Register(module.ServiceOperatorAuthService, opBundle.authService)
		deps.Services.Register(module.ServiceOperatorPasswordAuthService, opBundle.passwordSvc)

		// ADR-0003 PR-D D-4: operator-tier handler instances. Bound to
		// the operator bundle's services so /v1/auth/operator/* requests
		// read and write through operator_* collections. The OAuth
		// surface still uses the legacy authService until D-6 introduces
		// state-encoded tier dispatch — `m.operatorAuthHandler` exists
		// only for the tier-mountable subset (refresh/refresh-cookie/
		// logout/me).
		m.operatorPasswordHandler = handlers.NewPasswordAuthHandler(
			opBundle.passwordSvc,
			cfg.Auth.Cookie.Name,
			cfg.Auth.Cookie.Domain,
			cfg.Auth.Cookie.Secure,
		)
		m.operatorPasswordHandler.SetSessionRevocation(sessionRevocationSvc)

		m.operatorAuthHandler = handlers.NewAuthHandler(
			opBundle.authService,
			providerFactory,
			oauthResolver,
			oauthStateService,
			opBundle.oauthProviderRepo,
			jwtService,
			cfg,
		)
		m.operatorAuthHandler.SetSessionRevocation(sessionRevocationSvc)
		m.operatorAuthHandler.SetStateSecret(oauthStateSecret)
		m.operatorAuthHandler.SetTier(services.AudienceOperator)

		m.operatorMFAHandler = handlers.NewMFAHandler(
			opBundle.mfaSvc,
			mfaChallengeSvc,
			jwtService,
			operatorUser,
			opBundle.passwordSvc,
			cfg.Auth.Cookie.Name,
			cfg.Auth.Cookie.Domain,
			cfg.Auth.Cookie.Secure,
		)
		m.operatorMFAHandler.SetDeviceTrust(deviceTrustSvc)
		if opBundle.webauthnSvc != nil {
			m.operatorMFAHandler.SetWebAuthn(opBundle.webauthnSvc)
			m.operatorWebAuthnHandler = handlers.NewWebAuthnHandler(
				opBundle.webauthnSvc,
				mfaChallengeSvc,
				jwtService,
				operatorUser,
				opBundle.passwordSvc,
				cfg.Auth.Cookie.Name,
				cfg.Auth.Cookie.Domain,
				cfg.Auth.Cookie.Secure,
			)
			m.operatorWebAuthnHandler.SetDeviceTrust(deviceTrustSvc)
		}
	} else {
		logger.Warn("auth: operator user provider not registered — operator-tier auth bundle skipped (ADR-0003 PR-D)")
	}

	if clientUser, ok := module.GetTyped[iface.UserProvider](deps.Services, module.ServiceClientUserProvider); ok {
		// ADR-0003 PR-D D-5: client-tier JWT service. Mints access +
		// refresh tokens with aud=client so the client host mux's
		// RequireAudience("client") gate accepts them and the operator
		// mux rejects them. Reuses the same RS256 key pair as the
		// operator service — only the audience claim differs. Tenant
		// provider is wired in identically so client tokens still
		// embed the user's memberships.
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
		clBundle, err := buildAuthTierBundle(clDeps)
		if err != nil {
			return err
		}
		deps.Services.Register(module.ServiceClientAuthService, clBundle.authService)
		deps.Services.Register(module.ServiceClientPasswordAuthService, clBundle.passwordSvc)

		// ADR-0003 PR-D D-5: client-tier handler instances. Same shape
		// as the operator block above; the only differences are the
		// bound services (clBundle.* read/write through client_*
		// collections) and the JWT service the AuthHandler holds (so
		// refresh + me responses carry aud=client). OAuth flows stay
		// legacy-only until D-6 — `m.clientAuthHandler` exposes only
		// the tier-mountable subset (refresh/refresh-cookie/logout/me).
		m.clientPasswordHandler = handlers.NewPasswordAuthHandler(
			clBundle.passwordSvc,
			cfg.Auth.Cookie.Name,
			cfg.Auth.Cookie.Domain,
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
		)
		m.clientAuthHandler.SetSessionRevocation(sessionRevocationSvc)
		m.clientAuthHandler.SetStateSecret(oauthStateSecret)
		m.clientAuthHandler.SetTier(services.AudienceClient)

		m.clientMFAHandler = handlers.NewMFAHandler(
			clBundle.mfaSvc,
			mfaChallengeSvc,
			clientJWT,
			clientUser,
			clBundle.passwordSvc,
			cfg.Auth.Cookie.Name,
			cfg.Auth.Cookie.Domain,
			cfg.Auth.Cookie.Secure,
		)
		m.clientMFAHandler.SetDeviceTrust(deviceTrustSvc)
		if clBundle.webauthnSvc != nil {
			m.clientMFAHandler.SetWebAuthn(clBundle.webauthnSvc)
			m.clientWebAuthnHandler = handlers.NewWebAuthnHandler(
				clBundle.webauthnSvc,
				mfaChallengeSvc,
				clientJWT,
				clientUser,
				clBundle.passwordSvc,
				cfg.Auth.Cookie.Name,
				cfg.Auth.Cookie.Domain,
				cfg.Auth.Cookie.Secure,
			)
			m.clientWebAuthnHandler.SetDeviceTrust(deviceTrustSvc)
		}
	} else {
		logger.Warn("auth: client user provider not registered — client-tier auth bundle skipped (ADR-0003 PR-D)")
	}

	// ADR-0003 PR-D D-6: wire the per-tier dispatcher map on the
	// callback-owning AuthHandler. The legacy /v1/auth/... handler
	// owns the single OAuth callback URL registered with each
	// provider; on every callback it parses the signed-state JWT
	// and looks the tier up in this map to pick the AuthHandler
	// whose authService should mint the resulting tokens. The map
	// only contains entries for tiers whose user provider was
	// registered — a missing entry falls back to the legacy
	// authService so pre-cutover deployments keep working.
	tierDispatch := map[string]*handlers.AuthHandler{}
	if m.operatorAuthHandler != nil {
		tierDispatch[services.AudienceOperator] = m.operatorAuthHandler
	}
	if m.clientAuthHandler != nil {
		tierDispatch[services.AudienceClient] = m.clientAuthHandler
	}
	m.authHandler.SetTierDispatch(tierDispatch)

	// Session-risk lookup: resolves the most recent risk score for a sid
	// against the auth_sessions collection. Consumed post-InitAll by the
	// HTTP middleware's RequireLowRisk gate and the Cedar shadow
	// evaluator's principal.risk_score attribute. Registered as a plain
	// function (not an interface) so consumers on either side of the
	// auth/authz split bind to the same concrete closure without
	// importing auth's types. A nil error with score==0 is legitimate
	// (session absent, terminated, or scorer not yet populated) —
	// callers treat it as zero risk and fail open.
	var sessionRiskLookup authMiddleware.SessionRiskLookup = func(ctx context.Context, sessionID string) (float64, error) {
		if sessionID == "" {
			return 0, nil
		}
		session, err := authSessionRepo.GetByUUID(ctx, sessionID)
		if err != nil {
			return 0, err
		}
		if session == nil {
			return 0, nil
		}
		return session.RiskScore, nil
	}
	deps.Services.Register(module.ServiceSessionRiskLookup, sessionRiskLookup)

	// Register the auth PII producer with the DSR registry pre-created in
	// main.go. Registers even when the registry is absent so the main
	// path stays uniform — the helper is a no-op when the key is missing.
	// Reuses the SecurityEventService instance hoisted above for the
	// suspicious-login notifier so the user-facing security history and
	// the DSR export read the same collection.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(refreshTokenRepo, authSessionRepo, emailTokenRepo, mfaFactorRepo, securityEventSvc, deviceTrustRepo))
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

// webauthnAvailabilityChecker adapts the WebAuthnService to the smaller
// HasWebAuthnCredentials interface that the password / OAuth login services
// consume. The narrow shape keeps service-to-service coupling minimal —
// the login services don't need to know about register/verify ceremonies.
type webauthnAvailabilityChecker struct {
	svc services.WebAuthnService
}

func (c webauthnAvailabilityChecker) HasWebAuthnCredentials(ctx context.Context, userUUID string) bool {
	if c.svc == nil {
		return false
	}
	ok, _ := c.svc.HasCredentials(ctx, userUUID)
	return ok
}

func (m *AuthModule) RegisterRoutes(ri *module.RouteInfo) {
	// Auth has both public and protected routes. Mount the legacy
	// /v1/auth/... layout first, then layer the ADR-0003 PR-D D-4
	// /v1/auth/operator/... mounts on top when the operator bundle is
	// available. Both share the same chi router (ri.Operator) so requests
	// hitting either path land on the operator host mux; only the bound
	// services + JWT audience claims differ.
	protectedAPI := humachi.New(ri.Operator.ProtectedRouter, ri.APIConfig)
	m.authHandler.RegisterRoutes(ri.Operator.PublicAPI, protectedAPI, ri.Router, ri.Operator.ProtectedRouter)

	// Password auth endpoints: register/login/verify/reset/forgot live on the
	// public API; change-password is protected and runs without an org
	// context (it's a user self-service flow).
	if m.passwordHandler != nil {
		m.passwordHandler.RegisterPublicRoutes(ri.Operator.PublicAPI, handlers.LegacyMount)
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.passwordHandler.RegisterProtectedRoutes(api, handlers.LegacyMount)
		})
	}

	// MFA endpoints split into four halves:
	//   - public: /v1/auth/mfa/login/verify completes an in-flight login
	//     (the caller has a challengeId, not yet a bearer token).
	//   - protected (no step-up): enroll/status/verify — RequireGlobal()
	//     so the caller is already authenticated with a primary factor.
	//   - protected (step-up): /v1/auth/me/mfa/remove — dropping your own
	//     second factor is catastrophic, so demand a <5min OTP proof.
	//   - admin (step-up): /v1/admin/users/{id}/mfa/reset — resetting
	//     another user's MFA lets the admin enroll their own device, so
	//     step-up here gates the same move.
	if m.mfaHandler != nil {
		m.mfaHandler.RegisterPublicRoutes(ri.Operator.PublicAPI, handlers.LegacyMount)
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.mfaHandler.RegisterProtectedRoutes(api, handlers.LegacyMount)
		})
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
			api := humachi.New(r, ri.APIConfig)
			m.mfaHandler.RegisterStepUpRoutes(api, handlers.LegacyMount)
		})
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.mfa_reset"))
			r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
			api := humachi.New(r, ri.APIConfig)
			m.mfaHandler.RegisterAdminRoutes(api)
		})
	}

	// WebAuthn endpoints split into three halves, mirroring the TOTP layout:
	//   - public: /v1/auth/mfa/webauthn/login/{begin,finish} complete a
	//     paused password/OAuth login that the partial response flagged with
	//     webauthnAvailable=true.
	//   - protected (no step-up): register/verify/list — RequireGlobal()
	//     so the caller is authenticated with a primary factor.
	//   - protected (step-up): DELETE credentials — pulling a passkey is
	//     irreversible so demand a <5min OTP/WebAuthn proof first.
	if m.webauthnHandler != nil {
		m.webauthnHandler.RegisterPublicRoutes(ri.Operator.PublicAPI, handlers.LegacyMount)
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.webauthnHandler.RegisterProtectedRoutes(api, handlers.LegacyMount)
		})
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
			api := humachi.New(r, ri.APIConfig)
			m.webauthnHandler.RegisterStepUpRoutes(api, handlers.LegacyMount)
		})
	}

	// Device-trust self-service (Section C item #3):
	//   GET    /v1/auth/me/devices/trust          — list active grants
	//   DELETE /v1/auth/me/devices/trust/{id}     — revoke one
	//   DELETE /v1/auth/me/devices/trust          — revoke all
	// All three are protected self-service: RequireGlobal() gates on
	// authenticated session, no org context needed.
	if m.deviceTrustHandler != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.deviceTrustHandler.RegisterRoutes(api, handlers.LegacyMount)
		})
	}

	// ADR-0003 PR-D D-4: operator-tier auth paths under
	// /v1/auth/operator/... — mounted on the same operator host mux as
	// the legacy paths above. Each operator-bound handler reads/writes
	// through the operator authTierBundle's tier-stamped collections so
	// USER_TIER_SPLIT_ENABLED-flipped operator users can authenticate
	// via the new prefix without depending on the legacy auth_*
	// collections. The legacy mounts above stay live as a parallel
	// surface until D-8 deletes them. OAuth flows (start/callback) are
	// not yet split per tier — D-6 introduces state-encoded tier
	// dispatch and the per-audience start endpoints — so the operator
	// AuthHandler exposes only the tier-mountable subset (refresh/
	// refresh-cookie/logout/me).
	if m.operatorAuthHandler != nil {
		m.operatorAuthHandler.RegisterTierMountableRoutes(ri.Operator.PublicAPI, protectedAPI, ri.Router, handlers.OperatorMount)
		// ADR-0003 PR-D D-6: per-audience OAuth start endpoints. The
		// signed-state JWT minted here carries `tier=operator` so the
		// shared callback dispatches the resulting flow to the operator
		// authService. Mobile ID-token endpoints under this prefix mint
		// aud=operator tokens via the operator authService directly.
		m.operatorAuthHandler.RegisterOAuthStartRoutes(ri.Operator.PublicAPI, handlers.OperatorMount)
	}
	if m.operatorPasswordHandler != nil {
		m.operatorPasswordHandler.RegisterPublicRoutes(ri.Operator.PublicAPI, handlers.OperatorMount)
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.operatorPasswordHandler.RegisterProtectedRoutes(api, handlers.OperatorMount)
		})
	}
	if m.operatorMFAHandler != nil {
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
		// Admin reset stays on the legacy /v1/admin/... path — there's
		// no /admin/operator equivalent because admin is operator-tier
		// by definition.
	}
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
	if m.deviceTrustHandler != nil {
		// device_trust is a single (non-tier-split) collection so the
		// legacy handler instance is reused — only the path prefix
		// differs per tier mount. Adds /v1/auth/operator/me/devices/trust.
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.deviceTrustHandler.RegisterRoutes(api, handlers.OperatorMount)
		})
	}

	// ADR-0003 PR-D D-5: client-tier auth paths under /v1/auth/client/...
	// — mounted on the client host mux (ri.Client.PublicAPI /
	// ri.Client.ProtectedRouter / ri.ClientRouter). Each client-bound
	// handler reads/writes through the client authTierBundle's tier-
	// stamped collections (client_*) and mints aud=client tokens via
	// the client-audience JWT service constructed in Init. The legacy
	// /v1/auth/* mounts on the operator mux remain live as the canonical
	// surface until D-8 deletes them. OAuth flows are not yet split per
	// tier — D-6 introduces state-encoded tier dispatch — so the client
	// AuthHandler exposes only the tier-mountable subset (refresh/
	// refresh-cookie/logout/me), and admin paths stay operator-only.
	if ri.Client == nil {
		return
	}
	clientProtectedAPI := humachi.New(ri.Client.ProtectedRouter, ri.APIConfig)
	if m.clientAuthHandler != nil && ri.ClientRouter != nil {
		m.clientAuthHandler.RegisterTierMountableRoutes(ri.Client.PublicAPI, clientProtectedAPI, ri.ClientRouter, handlers.ClientMount)
		// ADR-0003 PR-D D-6: per-audience OAuth start endpoints. The
		// signed-state JWT minted here carries `tier=client` so the
		// shared callback (mounted on the operator host mux at the
		// OAuth provider's registered redirect URI) dispatches the
		// resulting flow to the client authService and stamps
		// aud=client on every minted token. Mobile ID-token endpoints
		// under this prefix similarly mint client-audience tokens.
		m.clientAuthHandler.RegisterOAuthStartRoutes(ri.Client.PublicAPI, handlers.ClientMount)
	}
	if m.clientPasswordHandler != nil {
		m.clientPasswordHandler.RegisterPublicRoutes(ri.Client.PublicAPI, handlers.ClientMount)
		ri.Client.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.clientPasswordHandler.RegisterProtectedRoutes(api, handlers.ClientMount)
		})
	}
	if m.clientMFAHandler != nil {
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
	}
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
	if m.deviceTrustHandler != nil {
		ri.Client.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.deviceTrustHandler.RegisterRoutes(api, handlers.ClientMount)
		})
	}
}

