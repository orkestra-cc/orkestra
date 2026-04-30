package auth

import (
	"log/slog"

	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/geoip"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/mongo"
)

// audienceTier discriminates the three repository bindings the bundle
// builder produces. Constant strings (not the iface enum) so the auth
// package stays free of shared/module imports for this purpose.
type audienceTier string

const (
	tierLegacy   audienceTier = ""         // legacy auth_* collections; pre-cutover canonical
	tierOperator audienceTier = "operator" // operator_* collections; ADR-0003 PR-D
	tierClient   audienceTier = "client"   // client_* collections; ADR-0003 PR-D
)

// authTierBundle is the per-tier set of services PR-D's audience-split
// auth handlers consume. Each bundle is bound to its tier's session,
// refresh-token, oauth-provider, mfa-factor, and email-token
// collections (PR-B) and to the matching user provider (Operator/
// Client/legacy). Services genuinely shared across tiers
// (PasswordService, MFAChallengeService, JWTService, DeviceTrust,
// SecurityEventService) live outside the bundle and are injected via
// tierBundleDeps.
type authTierBundle struct {
	tier              audienceTier
	authSessionRepo   repository.AuthSessionRepository
	refreshTokenRepo  repository.RefreshTokenRepository
	oauthProviderRepo repository.OAuthProviderRepository
	mfaFactorRepo     repository.MFAFactorRepository
	emailTokenRepo    repository.EmailTokenRepository
	riskAssessment    services.RiskAssessmentService
	authService       services.AuthService
	passwordSvc       *services.PasswordAuthService
}

// tierBundleDeps carries the tier-shared singletons and per-tier user
// provider that buildAuthTierBundle needs. Pulled into a struct so the
// function signature stays manageable as PR-D's plumbing grows.
type tierBundleDeps struct {
	db                       *mongo.Database
	logger                   *slog.Logger
	tier                     audienceTier
	userProvider             iface.UserProvider
	tenantProvider           iface.TenantProvider
	authRepo                 repository.AuthRepository // legacy single-table; tier-shared
	jwtService               services.JWTService
	passwordService          services.PasswordService
	mfaChallengeService      services.MFAChallengeService
	firstAdminClaimer        services.FirstAdminClaimer
	deviceTrust              services.DeviceTrustService
	suspiciousLoginNotifier  services.SuspiciousLoginNotifier
	notifier                 iface.NotificationSender
	rateLimiter              *sharederrors.RateLimiter
	geoResolver              geoip.Resolver
	velocityKmh              float64
	frontendURL              string
	requireEmailVerification bool
	appName                  string
	supportEmail             string
}

// buildAuthTierBundle constructs the per-tier repos + RiskAssessment +
// AuthService + PasswordAuthService. The tier-bound repos are picked
// off d.tier; tierLegacy maps to the legacy auth_* collections so the
// same helper covers the canonical (pre-cutover) bundle and the
// operator/client bundles uniformly.
func buildAuthTierBundle(d tierBundleDeps) (*authTierBundle, error) {
	var (
		sessionRepo repository.AuthSessionRepository
		refreshRepo repository.RefreshTokenRepository
		oauthRepo   repository.OAuthProviderRepository
		mfaRepo     repository.MFAFactorRepository
		emailRepo   repository.EmailTokenRepository
	)
	switch d.tier {
	case tierOperator:
		sessionRepo = repository.NewOperatorAuthSessionRepository(d.db)
		refreshRepo = repository.NewOperatorRefreshTokenRepository(d.db)
		oauthRepo = repository.NewOperatorOAuthProviderRepository(d.db)
		mfaRepo = repository.NewOperatorMFAFactorRepository(d.db)
		emailRepo = repository.NewOperatorEmailTokenRepository(d.db)
	case tierClient:
		sessionRepo = repository.NewClientAuthSessionRepository(d.db)
		refreshRepo = repository.NewClientRefreshTokenRepository(d.db)
		oauthRepo = repository.NewClientOAuthProviderRepository(d.db)
		mfaRepo = repository.NewClientMFAFactorRepository(d.db)
		emailRepo = repository.NewClientEmailTokenRepository(d.db)
	default:
		sessionRepo = repository.NewAuthSessionRepository(d.db)
		refreshRepo = repository.NewRefreshTokenRepository(d.db)
		oauthRepo = repository.NewOAuthProviderRepository(d.db)
		mfaRepo = repository.NewMFAFactorRepository(d.db)
		emailRepo = repository.NewEmailTokenRepository(d.db)
	}

	risk := services.NewRiskAssessmentServiceWithGeoIP(sessionRepo, d.geoResolver, d.velocityKmh, d.logger)

	authSvc, err := services.NewAuthService(&services.AuthConfig{
		AuthRepo:            d.authRepo,
		UserService:         d.userProvider,
		TenantProvider:      d.tenantProvider,
		OAuthProviderRepo:   oauthRepo,
		RefreshTokenRepo:    refreshRepo,
		AuthSessionRepo:     sessionRepo,
		JWTService:          d.jwtService,
		MFAFactorRepo:       mfaRepo,
		MFAChallengeService: d.mfaChallengeService,
		FirstAdminClaimer:   d.firstAdminClaimer,
		RiskAssessment:      risk,
	})
	if err != nil {
		return nil, err
	}

	passSvc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService:              d.userProvider,
		TenantProvider:           d.tenantProvider,
		PasswordService:          d.passwordService,
		JWTService:               d.jwtService,
		EmailTokenRepo:           emailRepo,
		RefreshTokenRepo:         refreshRepo,
		AuthSessionRepo:          sessionRepo,
		MFAFactorRepo:            mfaRepo,
		MFAChallengeService:      d.mfaChallengeService,
		FirstAdminClaimer:        d.firstAdminClaimer,
		RiskAssessment:           risk,
		DeviceTrust:              d.deviceTrust,
		SuspiciousLoginNotifier:  d.suspiciousLoginNotifier,
		Notifier:                 d.notifier,
		RateLimiter:              d.rateLimiter,
		FrontendURL:              d.frontendURL,
		RequireEmailVerification: d.requireEmailVerification,
		AppName:                  d.appName,
		SupportEmail:             d.supportEmail,
		Logger:                   d.logger,
	})

	return &authTierBundle{
		tier:              d.tier,
		authSessionRepo:   sessionRepo,
		refreshTokenRepo:  refreshRepo,
		oauthProviderRepo: oauthRepo,
		mfaFactorRepo:     mfaRepo,
		emailTokenRepo:    emailRepo,
		riskAssessment:    risk,
		authService:       authSvc,
		passwordSvc:       passSvc,
	}, nil
}
