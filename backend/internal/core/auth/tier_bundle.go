package auth

import (
	"context"
	"fmt"
	"log/slog"

	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/geoip"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/mongo"
)

// audienceTier discriminates the per-tier repository bindings the
// bundle builder produces. Constant strings (not the iface enum) so
// the auth package stays free of shared/module imports for this
// purpose.
type audienceTier string

const (
	tierOperator audienceTier = "operator" // operator_* collections; ADR-0003 PR-D
	tierClient   audienceTier = "client"   // client_* collections; ADR-0003 PR-D
)

// authTierBundle is the per-tier set of services PR-D's audience-split
// auth handlers consume. Each bundle is bound to its tier's session,
// refresh-token, oauth-provider, mfa-factor, and email-token
// collections (PR-B) and to the matching user provider (Operator/
// Client). Services genuinely shared across tiers
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
	// mfaSvc and webauthnSvc are tier-bound MFA orchestrators that read
	// and write through the bundle's tier-specific mfaFactorRepo. PR-D
	// D-4 wires per-audience MFA handler instances off these so an
	// operator user's TOTP enrollment lands in operator_mfa_factors and
	// a client user's lands in client_mfa_factors. webauthnSvc is nil
	// when the deployment hasn't configured a WebAuthn RP — same
	// gating semantics as the legacy module.go branch.
	mfaSvc      services.MFAService
	webauthnSvc services.WebAuthnService
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
	// mfaIssuer is the TOTP provisioning URI label (Authenticator app
	// row name). Tier-shared because users see the same issuer string
	// regardless of which tier they sit in.
	mfaIssuer string
	// webauthnRP is the configured WebAuthn Relying Party. Nil when
	// passkeys are disabled at boot — buildAuthTierBundle then leaves
	// webauthnSvc nil to match the legacy module.go nil-gating.
	webauthnRP *gowebauthn.WebAuthn
	// authPolicy is the shared policy reader; both bundles consume the
	// same instance. Nil = legacy "always-on" semantics.
	authPolicy *services.AuthPolicyService
}

// buildAuthTierBundle constructs the per-tier repos + RiskAssessment +
// AuthService + PasswordAuthService. The tier-bound repos are picked
// off d.tier; only operator and client tiers are valid (ADR-0003 PR-D
// D-8 removed the legacy single-table tier).
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
		return nil, fmt.Errorf("buildAuthTierBundle: unknown tier %q (expected %q or %q)", d.tier, tierOperator, tierClient)
	}

	risk := services.NewRiskAssessmentServiceWithGeoIP(sessionRepo, d.geoResolver, d.velocityKmh, d.logger)

	authSvc, err := services.NewAuthService(&services.AuthConfig{
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
	// Hand the shared admin-policy reader to the OAuth login path so it
	// honours the same mfaEnabled / grace-window the password login path
	// already consults via PasswordAuthService.
	authSvc.SetPolicy(d.authPolicy)
	policyAudienceForBundle := services.PolicyAudienceOperator
	if d.tier == tierClient {
		policyAudienceForBundle = services.PolicyAudienceClient
	}
	authSvc.SetAudience(policyAudienceForBundle)

	policyAudience := policyAudienceForBundle
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
		Policy:                   d.authPolicy,
		Audience:                 policyAudience,
		GeoResolver:              d.geoResolver,
	})

	mfaSvc := services.NewMFAService(mfaRepo, d.mfaChallengeService, d.passwordService, d.mfaIssuer, d.logger)
	mfaSvc.SetDeviceTrust(d.deviceTrust)
	// Phase 10: hand the policy to the MFA service so backup-code
	// generation honours the recoveryCodesCount toggle live.
	mfaSvc.SetPolicy(d.authPolicy)

	var webauthnSvc services.WebAuthnService
	if d.webauthnRP != nil {
		webauthnSvc = services.NewWebAuthnService(d.webauthnRP, mfaRepo, d.mfaChallengeService, d.logger)
		// Mirror the legacy wiring: when WebAuthn is enabled the
		// password/OAuth login services need to know whether a given
		// user has any passkeys to surface "use passkey" on the
		// partial login response. Wiring on the *handler* (status
		// endpoint, login-finish) happens in module.go after handler
		// construction since SetWebAuthn lives on the handler, not
		// the MFAService.
		passSvc.SetWebAuthnAvailability(tierWebAuthnAvailability{svc: webauthnSvc})
		authSvc.SetWebAuthnAvailability(tierWebAuthnAvailability{svc: webauthnSvc})
	}

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
		mfaSvc:            mfaSvc,
		webauthnSvc:       webauthnSvc,
	}, nil
}

// tierWebAuthnAvailability adapts the per-tier WebAuthnService to the
// HasWebAuthnCredentials interface the password/OAuth login services
// consume. Mirrors the legacy webauthnAvailabilityChecker in module.go,
// duplicated here to keep the bundle self-contained — both shapes
// expose the same single method.
type tierWebAuthnAvailability struct {
	svc services.WebAuthnService
}

func (a tierWebAuthnAvailability) HasWebAuthnCredentials(ctx context.Context, userUUID string) bool {
	if a.svc == nil {
		return false
	}
	ok, _ := a.svc.HasCredentials(ctx, userUUID)
	return ok
}
