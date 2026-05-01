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
	// history and GDPR DSR export read the same rows.
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
	}

	// Operator tier — required after the D-8 cutover. The user module
	// always registers ServiceOperatorUserProvider, so a missing
	// provider here means the user module failed to init.
	operatorUser := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceOperatorUserProvider)
	opDeps := commonTierDeps
	opDeps.tier = tierOperator
	opDeps.userProvider = operatorUser
	opDeps.jwtService = operatorJWT
	opBundle, err := buildAuthTierBundle(opDeps)
	if err != nil {
		return err
	}

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
		operatorJWT,
		cfg,
	)
	m.operatorAuthHandler.SetSessionRevocation(sessionRevocationSvc)
	m.operatorAuthHandler.SetStateSecret(oauthStateSecret)
	m.operatorAuthHandler.SetTier(services.AudienceOperator)

	m.operatorMFAHandler = handlers.NewMFAHandler(
		opBundle.mfaSvc,
		mfaChallengeSvc,
		operatorJWT,
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
			operatorJWT,
			operatorUser,
			opBundle.passwordSvc,
			cfg.Auth.Cookie.Name,
			cfg.Auth.Cookie.Domain,
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
	clBundle, err := buildAuthTierBundle(clDeps)
	if err != nil {
		return err
	}

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
	deps.Services.Register(module.ServicePasswordService, passwordSvc)
	deps.Services.Register(module.ServicePasswordAuthService, opBundle.passwordSvc)
	deps.Services.Register(module.ServiceSessionRevocation, sessionRevocationSvc)
	deps.Services.Register(module.ServiceOperatorAuthService, opBundle.authService)
	deps.Services.Register(module.ServiceOperatorPasswordAuthService, opBundle.passwordSvc)
	deps.Services.Register(module.ServiceClientAuthService, clBundle.authService)
	deps.Services.Register(module.ServiceClientPasswordAuthService, clBundle.passwordSvc)

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

