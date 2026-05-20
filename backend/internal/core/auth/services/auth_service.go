package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// defaultSlogger is the default logger factory used by handleRefreshReplay.
// Exposed as a var so tests can swap it for a capture-friendly logger
// without touching the global slog.Default().
func defaultSlogger() *slog.Logger { return slog.Default() }

var (
	ErrCannotRemoveLastOAuthLink = errors.New("cannot remove the last OAuth link")
	ErrOAuthLinkNotFound         = errors.New("OAuth link not found")
	ErrOAuthLinkAlreadyExists    = errors.New("OAuth link already exists")
	ErrUserMigrationRequired     = errors.New("user migration to UUID required")
	ErrInvalidRefreshToken       = errors.New("invalid refresh token")
	// ErrRefreshTokenReplay signals that a rotated refresh token was used
	// again after its successor already existed — a textbook replay attack.
	// The whole family is revoked before this error is returned.
	ErrRefreshTokenReplay = errors.New("refresh token replay detected — session revoked")
	// ErrOAuthSignupDisabled signals that an OAuth callback resolved to
	// an unknown email but the audience-scoped oauthAllowSignup policy
	// is off, so we refuse to provision a new account. Translated to
	// 403 oauth_signup_disabled at the handler boundary.
	ErrOAuthSignupDisabled = errors.New("oauth signup disabled by policy")
	// ErrOAuthLinkDisabled signals that the OAuth callback resolved to
	// an existing account by email but oauthAutoLinkByEmail is off.
	// The user must initiate linking from their account settings while
	// already authenticated. Translated to 403 oauth_link_disabled at
	// the handler boundary.
	ErrOAuthLinkDisabled = errors.New("oauth account linking by email is disabled")
	// ErrLastCredentialRemoval signals that an admin tried to unlink an
	// OAuth identity that would leave the target with no usable login
	// method (no password set AND it was their only OAuth provider).
	// Translated to 409 last_credential at the handler boundary so the
	// UI can prompt the operator to send a password-reset first.
	ErrLastCredentialRemoval = errors.New("cannot remove the user's only remaining credential")
	// ErrAdminSelfAction signals that an admin tried to invoke an
	// admin-on-user action against their own account (MFA reset, OAuth
	// unlink) — actions where lock-out risk is real and the admin
	// should be using the self-service flow instead. Translated to 409
	// self_action at the handler boundary.
	ErrAdminSelfAction = errors.New("admin cannot perform this action on their own account")
	// ErrCannotRevokeCurrent signals that the user tried to revoke the
	// session they are calling from. The existing logout endpoint is
	// the right tool for that — revoking the current session through
	// the self-service surface would race the user's own response.
	// Translated to 409 cannot_revoke_current at the handler boundary.
	ErrCannotRevokeCurrent = errors.New("cannot revoke the current session — use logout instead")
	// ErrSessionNotFound signals that the requested session UUID does
	// not exist or does not belong to the calling user. We collapse
	// "doesn't exist" and "not yours" to one error so the response
	// doesn't leak the existence of someone else's session UUID.
	ErrSessionNotFound = errors.New("session not found")
	// ErrOAuthLinkClaimedByOther signals that the OAuth identity the
	// caller is trying to link is already attached to a different
	// account. Returned by SelfLinkOAuthFromCallback so the callback
	// surface can render an "already in use" error rather than
	// silently re-binding the identity to the new user. Translated to
	// 409 oauth_already_linked at the redirect query layer.
	ErrOAuthLinkClaimedByOther = errors.New("OAuth identity already linked to another account")
	// ErrOAuthLinkInvalidUserInfo signals that the OAuth provider's
	// userInfo response was missing fields required to build a link
	// (typically providerID or email). Distinct from ErrOAuthLinkNotFound
	// — that's "the link doesn't exist", this is "the link can't be
	// built from what we got back".
	ErrOAuthLinkInvalidUserInfo = errors.New("OAuth provider returned incomplete user info")
)

// AuthService extends the basic auth service with UUID support and OAuth linking
type AuthService interface {
	// UUID-based user operations
	GetUserByUUID(ctx context.Context, uuid string) (*userModels.User, error)
	UpdateUserByUUID(ctx context.Context, uuid string, update *userModels.User) error
	UpdateLastLoginByUUID(ctx context.Context, uuid string) error
	DeleteUserByUUID(ctx context.Context, uuid string) error
	// UpdateLanguageByUUID writes the user's preferred BCP-47 language
	// tag. Self-service surface — callers MUST validate the tag against
	// the supported-language allowlist before invoking; the underlying
	// UpdateUserInput re-validates via struct tags, but the service trusts
	// the caller has already rejected the unknown values.
	UpdateLanguageByUUID(ctx context.Context, uuid, language string) error

	// OAuth Link Management
	AddOAuthLink(ctx context.Context, userUUID string, link models.LinkOAuthProviderInput) error
	RemoveOAuthLink(ctx context.Context, userUUID string, input models.UnlinkOAuthProviderInput) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, input models.SetPrimaryOAuthProviderInput) error
	GetOAuthLinks(ctx context.Context, userUUID string) (*models.OAuthLinksResponse, error)
	// AdminUnlinkOAuth removes a linked OAuth identity from another
	// user's account. Enforces two safeguards in addition to the route
	// gate: rejects self-action (actorUUID == targetUUID) and rejects
	// removing the user's only remaining credential (no password +
	// single OAuth link). Both safeguards live here so any caller —
	// HTTP handler, future CLI, internal job — gets the same checks.
	AdminUnlinkOAuth(ctx context.Context, actorUUID, targetUUID string, provider userModels.OAuthProvider) error
	// SelfUnlinkOAuth removes one OAuth identity from the caller's own
	// account. Mirrors AdminUnlinkOAuth's last-credential safeguard so
	// a user cannot accidentally remove their only login method, but
	// drops the self-action check (self-action IS the point here). The
	// HTTP handler is gated by RequireStepUp(5m) — credential removal
	// demands a fresh MFA proof.
	SelfUnlinkOAuth(ctx context.Context, userUUID string, provider userModels.OAuthProvider) error
	// SelfLinkOAuthFromCallback binds a freshly-completed OAuth flow
	// to an authenticated user. Called from the OAuth callback when
	// the signed-state JWT carries Mode=link. Rejects with
	// ErrOAuthLinkClaimedByOther when the (provider, providerID) pair
	// is already attached to a different user; with
	// ErrOAuthLinkAlreadyExists when the user already has the same
	// provider linked.
	SelfLinkOAuthFromCallback(ctx context.Context, userUUID string, provider userModels.OAuthProvider, userInfo map[string]interface{}, oauthTokens *models.OAuthProviderTokens) error
	// GetUserAuthMethods aggregates the user's auth state across the
	// user record, MFA factor row(s), and linked OAuth providers into
	// a single view consumed by the admin profile card and by the
	// self-service /user/security page. The HTTP gate (admin
	// permission vs. RequireGlobal) is applied at the handler.
	GetUserAuthMethods(ctx context.Context, targetUUID string) (*models.AuthMethodsView, error)
	// ListUserSessions returns the active sessions for the user. The
	// IsCurrent flag is NOT populated here — the HTTP handler stamps
	// it by comparing each SessionInfo.SessionID against the JWT sid.
	ListUserSessions(ctx context.Context, userUUID string) (*models.SessionsResponse, error)
	// RevokeUserSession revokes one session by UUID for the user.
	// Rejects revoke-current with ErrCannotRevokeCurrent so the
	// caller's response can complete; returns ErrSessionNotFound when
	// the session doesn't exist or doesn't belong to the user.
	RevokeUserSession(ctx context.Context, userUUID, sessionUUID, currentSid string) error
	// RevokeAllUserSessionsExcept revokes every active session for
	// the user except the one matching currentSid. Returns the count
	// of revoked sessions.
	RevokeAllUserSessionsExcept(ctx context.Context, userUUID, currentSid string) (int, error)

	// Account consolidation
	ConsolidateAccountsByEmail(ctx context.Context, email string) (*userModels.User, error)
	FindAccountsForConsolidation(ctx context.Context, email string) ([]*userModels.User, error)

	// Enhanced session management (UUID-based)
	GetUserSessionsByUUID(ctx context.Context, userUUID string) (*models.SessionsResponse, error)
	GetSessionCountByUUID(ctx context.Context, userUUID string) (int, error)
	RenameSessionByUUID(ctx context.Context, userUUID string, deviceID, deviceName string) error
	TerminateSessionByUUID(ctx context.Context, userUUID string, deviceID string) error
	TerminateAllSessionsByUUID(ctx context.Context, userUUID string) error

	// Enhanced token management with UUID and risk assessment
	GenerateEnhancedTokenPair(ctx context.Context, user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenResponse, error)
	RefreshTokensWithRiskAssessment(ctx context.Context, refreshToken string, securityCtx *models.SecurityContext) (*models.TokenResponse, error)
	ValidateTokenWithRiskAssessment(ctx context.Context, token string, securityCtx *models.SecurityContext) (*models.TokenValidationResult, error)

	// Risk assessment and security
	AssessLoginRisk(ctx context.Context, userUUID string, securityCtx *models.SecurityContext) (*models.RiskAssessment, error)
	RecordSecurityEvent(ctx context.Context, event *models.SecurityEvent) error
	GetSecurityEvents(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error)

	// Migration utilities
	MigrateUserToUUID(ctx context.Context, userID primitive.ObjectID) (*userModels.User, error)
	MigrateAllUsersToUUID(ctx context.Context) (int, error)
	ConvertOAuthLinksToNewFormat(ctx context.Context, userUUID string) error

	// SetWebAuthnAvailability wires the optional passkey-availability checker
	// post-construction. The OAuth login branch consumes it to populate the
	// partial response's WebAuthnAvailable hint. Mirrors the same setter on
	// PasswordAuthService — both login paths must surface the field uniformly.
	SetWebAuthnAvailability(c HasWebAuthnCredentials)

	// SetSessionRevocation wires the Redis-backed sid revocation store
	// post-construction so RevokeUserSession / RevokeAllUserSessionsExcept
	// can push revoked sids into the set the AuthMiddleware consults on
	// every authenticated request. Optional — when nil the persisted
	// session+refresh-token state is still updated, but in-flight access
	// tokens stay valid until their TTL ticks over.
	SetSessionRevocation(rev SessionRevocationService)

	// SetPolicy wires the admin-managed policy reader. Optional — the
	// OAuth login MFA evaluation falls back to legacy hardcoded values
	// when the receiver is nil.
	SetPolicy(p *AuthPolicyService)

	// SetAudience wires the surface this service serves so policy
	// reads can fetch audience-scoped values (oauthAllowSignup, etc.)
	// without callers having to thread the audience through every
	// signature. Empty falls back to operator semantics.
	SetAudience(a PolicyAudience)

	// Enhanced OAuth callback handling with account linking
	HandleOAuthCallbackWithLinking(ctx context.Context, provider models.OAuthProvider, userInfo map[string]interface{}, oauthTokens *models.OAuthProviderTokens, securityCtx *models.SecurityContext, deviceInfo *models.DeviceInfo) (*models.TokenResponse, error)
}

// AuthConfig holds configuration for the auth service
type AuthConfig struct {
	UserService         iface.UserProvider
	TenantProvider      iface.TenantProvider // used by MFA policy evaluation on OAuth login
	OAuthProviderRepo   repository.OAuthProviderRepository
	RefreshTokenRepo    repository.RefreshTokenRepository
	AuthSessionRepo     repository.AuthSessionRepository
	JWTService          JWTService
	MFAFactorRepo       repository.MFAFactorRepository // nil disables MFA gating (tests/minimal)
	MFAChallengeService MFAChallengeService            // required alongside MFAFactorRepo
	FirstAdminClaimer   FirstAdminClaimer              // race-proofs first-user super_admin on OAuth signup
	// RiskAssessment scores login + refresh attempts against the user's
	// session history. Nil falls back to a zero-score stub so tests and
	// minimal deploys don't need the full scorer plumbed. Section C item
	// #1 of the 2026-04-24 auth roadmap.
	RiskAssessment RiskAssessmentService
}

type authService struct {
	userService         iface.UserProvider
	tenantProvider      iface.TenantProvider
	oauthProviderRepo   repository.OAuthProviderRepository
	refreshTokenRepo    repository.RefreshTokenRepository
	authSessionRepo     repository.AuthSessionRepository
	jwtService          JWTService
	mfaFactorRepo       repository.MFAFactorRepository
	mfaChallengeService MFAChallengeService
	firstAdminClaimer   FirstAdminClaimer
	riskAssessment      RiskAssessmentService
	// webauthnAvailability is wired post-construction so the OAuth login
	// branch can flag passkey availability on the partial response. Nil
	// when WebAuthn is disabled — falls back to TOTP-only behavior.
	webauthnAvailability HasWebAuthnCredentials
	// policy is the admin-managed kill switches + grace-window source
	// for the OAuth login MFA evaluation. Nil falls back to legacy
	// hardcoded values (mfaEnabled=true, grace=7d).
	policy *AuthPolicyService
	// audience names the surface this service serves. Used so OAuth
	// signup gating can read the audience-scoped oauthAllowSignup
	// policy without needing the caller to pass it explicitly.
	audience PolicyAudience
	// sessionRevocation is the Redis-backed sid revocation store
	// shared across all tiers. Wired post-construction via
	// SetSessionRevocation; nil disables the in-flight access-token
	// kill (the persisted state still updates, just no immediate
	// effect on outstanding bearer tokens).
	sessionRevocation SessionRevocationService
}

// SetWebAuthnAvailability wires the optional checker. Mirrors the same
// setter on PasswordAuthService so both login paths surface
// WebAuthnAvailable identically.
func (s *authService) SetWebAuthnAvailability(c HasWebAuthnCredentials) {
	s.webauthnAvailability = c
}

// SetPolicy wires the admin-managed AuthPolicyService so the OAuth
// login path honours the same MFA kill switch + grace window the
// password login path consults.
func (s *authService) SetPolicy(p *AuthPolicyService) {
	s.policy = p
}

// SetAudience records the surface this service serves so policy
// reads can fetch audience-scoped knobs.
func (s *authService) SetAudience(a PolicyAudience) {
	s.audience = a
}

// SetSessionRevocation wires the Redis-backed sid revocation store.
func (s *authService) SetSessionRevocation(rev SessionRevocationService) {
	s.sessionRevocation = rev
}

// NewAuthService creates a new auth service
func NewAuthService(config *AuthConfig) (AuthService, error) {
	return &authService{
		userService:         config.UserService,
		tenantProvider:      config.TenantProvider,
		oauthProviderRepo:   config.OAuthProviderRepo,
		refreshTokenRepo:    config.RefreshTokenRepo,
		authSessionRepo:     config.AuthSessionRepo,
		jwtService:          config.JWTService,
		mfaFactorRepo:       config.MFAFactorRepo,
		mfaChallengeService: config.MFAChallengeService,
		firstAdminClaimer:   config.FirstAdminClaimer,
		riskAssessment:      config.RiskAssessment,
	}, nil
}

// Placeholder implementations for now - to be fully implemented later

func (s *authService) GetUserByUUID(ctx context.Context, uuid string) (*userModels.User, error) {
	userModel, err := s.userService.GetUserByID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	return userModel, nil
}

func (s *authService) UpdateUserByUUID(ctx context.Context, uuid string, update *userModels.User) error {
	// Convert user model to update input
	updateInput := &userModels.UpdateUserInput{
		FullName: update.FullName,
		Avatar:   update.Avatar,
		Role:     update.Role,
	}
	_, err := s.userService.UpdateUser(ctx, uuid, updateInput)
	return err
}

func (s *authService) UpdateLastLoginByUUID(ctx context.Context, uuid string) error {
	return s.userService.UpdateUserLastLogin(ctx, uuid)
}

func (s *authService) UpdateLanguageByUUID(ctx context.Context, uuid, language string) error {
	_, err := s.userService.UpdateUser(ctx, uuid, &userModels.UpdateUserInput{Language: language})
	return err
}

func (s *authService) DeleteUserByUUID(ctx context.Context, uuid string) error {
	return s.userService.DeleteUser(ctx, uuid)
}

func (s *authService) AddOAuthLink(ctx context.Context, userUUID string, linkInput models.LinkOAuthProviderInput) error {
	// This method handles OAuth flow input to add a new link
	// TODO: Implement OAuth provider exchange to get provider ID and user info
	// For now, this is a placeholder that indicates the OAuth flow is not yet fully implemented
	return fmt.Errorf("OAuth link addition via OAuth flow not yet implemented - provider: %s", linkInput.Provider)
}

func (s *authService) RemoveOAuthLink(ctx context.Context, userUUID string, input models.UnlinkOAuthProviderInput) error {
	// Get user's OAuth links to find the one to remove
	links, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return err
	}

	// Find the link for this provider
	var targetProviderID string
	for _, link := range links {
		if userModels.OAuthProvider(input.Provider) == link.Provider {
			targetProviderID = link.ProviderID
			break
		}
	}

	if targetProviderID == "" {
		return fmt.Errorf("OAuth link for provider %s not found", input.Provider)
	}

	return s.userService.RemoveOAuthLinkFromUser(ctx, userUUID, userModels.OAuthProvider(input.Provider), targetProviderID)
}

func (s *authService) SetPrimaryOAuthLink(ctx context.Context, userUUID string, input models.SetPrimaryOAuthProviderInput) error {
	// Get user's OAuth links to find the one to set as primary
	links, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return err
	}

	// Find the link for this provider
	var targetProviderID string
	for _, link := range links {
		if userModels.OAuthProvider(input.Provider) == link.Provider {
			targetProviderID = link.ProviderID
			break
		}
	}

	if targetProviderID == "" {
		return fmt.Errorf("OAuth link for provider %s not found", input.Provider)
	}

	return s.userService.SetPrimaryOAuthLink(ctx, userUUID, userModels.OAuthProvider(input.Provider), targetProviderID)
}

func (s *authService) GetOAuthLinks(ctx context.Context, userUUID string) (*models.OAuthLinksResponse, error) {
	links, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return nil, err
	}

	// Convert user OAuth links to auth OAuth links
	authLinks := make([]models.OAuthLink, len(links))
	for i, link := range links {
		authLinks[i] = models.OAuthLink{
			Provider:   models.OAuthProvider(link.Provider),
			ProviderID: link.ProviderID,
			Email:      link.Email,
			LinkedAt:   link.LinkedAt,
			IsActive:   link.IsActive,
			IsPrimary:  link.IsPrimary,
			OAuthData:  link.OAuthData,
			LastUsed:   link.LastUsed,
		}
	}

	return &models.OAuthLinksResponse{
		Links:       authLinks,
		CanUnlink:   len(authLinks) > 1, // Can only unlink if more than one link exists
		RequiresMFA: false,              // TODO: Implement MFA logic
	}, nil
}

// AdminUnlinkOAuth removes one linked OAuth identity from another
// user's account. The HTTP handler is gated by
// RequireSystemPermission("system.users.oauth_unlink") + RequireStepUp,
// but the lock-out and self-action invariants are enforced here so a
// future caller (CLI, internal job) cannot bypass them.
//
// Safeguards (in this order):
//  1. actorUUID != targetUUID — admins must not unlink their own
//     identities through the admin path; the self-service flow exists
//     for that and carries different audit semantics.
//  2. Last-credential lockout — refuse when the target has no usable
//     password AND this is their only OAuth link. The operator should
//     send a password-reset first, then unlink.
//
// The provider-specific subject ID is resolved from the target's
// User.OAuthLinks rather than from the OAuth provider repo so this
// path stays free of tier-specific repo plumbing.
func (s *authService) AdminUnlinkOAuth(ctx context.Context, actorUUID, targetUUID string, provider userModels.OAuthProvider) error {
	if actorUUID == "" || targetUUID == "" {
		return fmt.Errorf("admin unlink: actorUUID and targetUUID are required")
	}
	if actorUUID == targetUUID {
		return ErrAdminSelfAction
	}

	target, err := s.userService.GetUserByID(ctx, targetUUID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrOAuthLinkNotFound
	}

	links, err := s.userService.GetUserOAuthLinks(ctx, targetUUID)
	if err != nil {
		return err
	}
	providerID, locked, found := wouldLockOutOAuthUnlink(target, links, provider)
	if !found {
		return ErrOAuthLinkNotFound
	}
	if locked {
		return ErrLastCredentialRemoval
	}

	if err := s.userService.RemoveOAuthLinkFromUser(ctx, targetUUID, provider, providerID); err != nil {
		return err
	}

	slog.Info("admin_auth_action",
		"event", "admin_oauth_unlink",
		"actorUUID", actorUUID,
		"targetUUID", targetUUID,
		"provider", string(provider),
	)
	return nil
}

// wouldLockOutOAuthUnlink computes the shared lockout decision used by
// AdminUnlinkOAuth and SelfUnlinkOAuth. Returns the matched providerID
// (empty when no link is found), a `locked` flag set when removing the
// link would leave the user with no usable credential (no password
// AND single active OAuth link), and a `found` flag distinguishing
// "provider not linked" (404) from a benign mismatch.
func wouldLockOutOAuthUnlink(target *userModels.User, links []userModels.OAuthLink, provider userModels.OAuthProvider) (providerID string, locked bool, found bool) {
	if target == nil {
		return "", false, false
	}
	activeCount := 0
	for _, link := range links {
		if link.IsActive {
			activeCount++
		}
		if link.Provider == provider && providerID == "" {
			providerID = link.ProviderID
		}
	}
	found = providerID != ""
	if !found {
		return "", false, false
	}
	locked = target.PasswordHash == "" && activeCount <= 1
	return providerID, locked, true
}

// SelfUnlinkOAuth removes one OAuth identity from the caller's own
// account. Reuses the shared wouldLockOutOAuthUnlink helper so a user
// cannot accidentally lock themselves out — but skips the self-action
// guard, since self-action is the entire point. The HTTP handler
// gates this with RequireStepUp(5m); credential removal demands a
// fresh MFA proof.
func (s *authService) SelfUnlinkOAuth(ctx context.Context, userUUID string, provider userModels.OAuthProvider) error {
	if userUUID == "" {
		return fmt.Errorf("self unlink: userUUID is required")
	}
	user, err := s.userService.GetUserByID(ctx, userUUID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrOAuthLinkNotFound
	}
	links, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return err
	}
	providerID, locked, found := wouldLockOutOAuthUnlink(user, links, provider)
	if !found {
		return ErrOAuthLinkNotFound
	}
	if locked {
		return ErrLastCredentialRemoval
	}
	if err := s.userService.RemoveOAuthLinkFromUser(ctx, userUUID, provider, providerID); err != nil {
		return err
	}
	slog.Info("self_auth_action",
		"event", "self_oauth_unlink",
		"userUUID", userUUID,
		"provider", string(provider),
	)
	return nil
}

// SelfLinkOAuthFromCallback binds an OAuth identity returned by a
// completed OAuth flow to an authenticated user. Driven by the
// link-mode branch of the shared OAuth callback (state.Mode=="link";
// state.LinkUserUUID names this user). Mirrors the safeguards in
// HandleOAuthCallbackWithLinking but never mints tokens — the user
// is already authenticated.
//
// Safeguards:
//  1. providerID + email must be present in userInfo; reject with
//     ErrOAuthLinkInvalidUserInfo otherwise.
//  2. (provider, providerID) must not already be claimed by a
//     different user — return ErrOAuthLinkClaimedByOther.
//  3. The user must not already have a link for this provider —
//     return ErrOAuthLinkAlreadyExists.
//
// On success the link is appended to User.OAuthLinks (the embedded
// slice that powers self/admin OAuth-unlink) and a corresponding
// row is written to the auth-side oauth_provider_repo so existing
// provider-side lookups (login resolution) see the new identity.
func (s *authService) SelfLinkOAuthFromCallback(
	ctx context.Context,
	userUUID string,
	provider userModels.OAuthProvider,
	userInfo map[string]interface{},
	oauthTokens *models.OAuthProviderTokens,
) error {
	if userUUID == "" {
		return fmt.Errorf("self link: userUUID is required")
	}
	user, err := s.userService.GetUserByID(ctx, userUUID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}

	providerID, _ := userInfo["provider_id"].(string)
	email, _ := userInfo["email"].(string)
	if providerID == "" || email == "" {
		return ErrOAuthLinkInvalidUserInfo
	}

	// Already-claimed-by-another check: keyed on the provider-side
	// index, the source of truth for "who owns this providerID". The
	// repo speaks the auth-side OAuthProvider type, so cross from
	// user-side to auth-side at the boundary.
	authProvider := models.OAuthProvider(provider)
	if existing, _ := s.oauthProviderRepo.GetByProviderAndID(ctx, authProvider, providerID); existing != nil {
		if existing.UserUUID != userUUID {
			return ErrOAuthLinkClaimedByOther
		}
		// Already linked to this same user — make the call idempotent
		// so a refresh / replay doesn't surface as a hard error.
		return nil
	}

	// Already-on-this-user check: walk the embedded array. Picking up
	// here means a row exists on the user without a matching provider
	// repo doc — defensive, treat as duplicate.
	existingLinks, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return err
	}
	for _, l := range existingLinks {
		if l.Provider == provider {
			return ErrOAuthLinkAlreadyExists
		}
	}

	now := time.Now()
	name, _ := userInfo["name"].(string)
	picture, _ := userInfo["picture"].(string)
	emailVerified, _ := userInfo["email_verified"].(bool)
	metadata := map[string]interface{}{
		"name":           name,
		"picture":        picture,
		"email_verified": emailVerified,
	}

	link := userModels.OAuthLink{
		Provider:   provider,
		ProviderID: providerID,
		Email:      email,
		LinkedAt:   now,
		IsActive:   true,
		IsPrimary:  false,
		LastUsed:   &now,
		OAuthData:  metadata,
	}
	if err := s.userService.AddOAuthLinkToUser(ctx, userUUID, link); err != nil {
		return fmt.Errorf("persist user link: %w", err)
	}

	// Mirror to the provider-side index so future logins by this
	// identity resolve back to the right user. Best-effort — a write
	// failure here leaves the embedded link in place; admin can
	// re-link if needed. Tokens are stored unencrypted only when the
	// caller chose not to provide encryption material; the canonical
	// HandleOAuthCallbackWithLinking does encrypt — we keep the
	// surface minimal here since these tokens are only used as a
	// linking artifact, not for ongoing API access.
	providerDoc := &models.OAuthProviderDoc{
		UUID:       models.GenerateUUIDv7(),
		UserUUID:   userUUID,
		Provider:   authProvider,
		ProviderID: providerID,
		Email:      email,
		IsPrimary:  false,
		LinkedAt:   now,
		Metadata:   metadata,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if oauthTokens != nil {
		providerDoc.Scopes = oauthTokens.Scopes
	}
	if err := s.oauthProviderRepo.CreateOAuthProvider(ctx, providerDoc); err != nil {
		slog.Warn("self_oauth_link: provider repo write failed (link still attached to user)",
			"userUUID", userUUID,
			"provider", string(provider),
			"error", err.Error(),
		)
	}

	slog.Info("self_auth_action",
		"event", "self_oauth_link",
		"userUUID", userUUID,
		"provider", string(provider),
	)
	return nil
}

// GetUserAuthMethods aggregates a single user's authentication state
// (password, MFA factors, OAuth identities, email verification) into
// the AuthMethodsView the admin card consumes. Reads from three
// sources: the user record (PasswordHash, MFAGraceStartedAt,
// OAuthLinks, EmailVerified, LastLogin), the tier's mfa_factors
// collection (TOTP + WebAuthn rows for the user), and — implicitly —
// the policy reader for MFARequired/MFAGraceExpiresAt.
//
// MFA factor reads are best-effort: a transient repo error must not
// blank the password / OAuth half of the response. The caller can
// retry the whole call to refresh the MFA section if it surfaces as
// empty when it shouldn't.
func (s *authService) GetUserAuthMethods(ctx context.Context, targetUUID string) (*models.AuthMethodsView, error) {
	if targetUUID == "" {
		return nil, fmt.Errorf("get auth methods: targetUUID is required")
	}
	user, err := s.userService.GetUserByID(ctx, targetUUID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrOAuthLinkNotFound
	}

	view := &models.AuthMethodsView{
		HasUsablePassword: user.PasswordHash != "",
		PasswordUpdatedAt: user.PasswordUpdatedAt,
		EmailVerified:     user.EmailVerified,
		LastLoginAt:       user.LastLogin,
		MFAGraceStartedAt: user.MFAGraceStartedAt,
		MFAFactors:        []models.MFAFactorView{},
		OAuthProviders:    []models.OAuthProviderView{},
	}

	// MFARequired starts from the role-only check — covers the system
	// roles (super_admin / administrator) without needing a tenant
	// lookup. If the role alone doesn't trigger it and a tenant
	// provider is wired, fold in the user's tenant memberships so an
	// org_owner / org_admin in any tenant flips the requirement on.
	view.MFARequired = RoleRequiresMFA(user, nil)
	if !view.MFARequired && s.tenantProvider != nil {
		if mems, mErr := s.tenantProvider.ListUserMemberships(ctx, targetUUID); mErr == nil {
			view.MFARequired = RoleRequiresMFA(user, ifaceMembershipsToAuth(mems))
		}
	}
	if s.policy != nil {
		if deadline := s.policy.MFAGraceExpiresAt(ctx, user); !deadline.IsZero() {
			view.MFAGraceExpiresAt = &deadline
		}
	}

	if s.mfaFactorRepo != nil {
		if totp, err := s.mfaFactorRepo.FindByUserAndType(ctx, targetUUID, models.MFAFactorTOTP); err == nil && totp != nil {
			view.MFAFactors = append(view.MFAFactors, models.MFAFactorView{
				Type:                 string(models.MFAFactorTOTP),
				EnrolledAt:           totp.VerifiedAt,
				LastUsedAt:           totp.LastUsedAt,
				BackupCodesRemaining: len(totp.BackupCodesHashed),
			})
		}
		if wa, err := s.mfaFactorRepo.FindByUserAndType(ctx, targetUUID, models.MFAFactorWebAuthn); err == nil && wa != nil && len(wa.WebAuthnCredentials) > 0 {
			creds := make([]models.WebAuthnCredentialSummary, 0, len(wa.WebAuthnCredentials))
			for _, c := range wa.WebAuthnCredentials {
				creds = append(creds, models.WebAuthnCredentialSummary{
					CredentialID: encodeCredentialID(c.CredentialID),
					Name:         c.Name,
					CreatedAt:    c.CreatedAt,
					LastUsedAt:   c.LastUsedAt,
				})
			}
			view.MFAFactors = append(view.MFAFactors, models.MFAFactorView{
				Type:        string(models.MFAFactorWebAuthn),
				EnrolledAt:  wa.VerifiedAt,
				LastUsedAt:  wa.LastUsedAt,
				Credentials: creds,
			})
		}
	}

	for _, link := range user.OAuthLinks {
		if !link.IsActive {
			continue
		}
		view.OAuthProviders = append(view.OAuthProviders, models.OAuthProviderView{
			Provider:   string(link.Provider),
			Email:      link.Email,
			LinkedAt:   link.LinkedAt,
			LastUsedAt: link.LastUsed,
			IsPrimary:  link.IsPrimary,
		})
	}

	return view, nil
}

// ifaceMembershipsToAuth converts the iface.TenantMembership slice
// returned by TenantProvider.ListUserMemberships into the
// authModels.OrgMembership shape that RoleRequiresMFA consumes. Only
// the fields the policy reads are copied; everything else is dropped.
func ifaceMembershipsToAuth(mems []iface.TenantMembership) []models.OrgMembership {
	out := make([]models.OrgMembership, 0, len(mems))
	for _, m := range mems {
		out = append(out, models.OrgMembership{
			TenantUUID: m.TenantUUID,
			TenantKind: m.TenantKind,
			Roles:      m.Roles,
		})
	}
	return out
}

// encodeCredentialID renders the raw WebAuthn credential ID bytes as
// the same base64url-no-padding string the W3C JSON wire format uses,
// so the admin UI can deep-link to "remove this credential" later
// without reshaping IDs at the boundary.
func encodeCredentialID(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *authService) ConsolidateAccountsByEmail(ctx context.Context, email string) (*userModels.User, error) {
	userResponse, err := s.userService.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	// Convert user response to auth model
	return convertUserResponseToAuthModel(userResponse), nil
}

func (s *authService) FindAccountsForConsolidation(ctx context.Context, email string) ([]*userModels.User, error) {
	userResponse, err := s.userService.GetUserByEmail(ctx, email)
	if err != nil {
		return []*userModels.User{}, nil
	}
	return []*userModels.User{convertUserResponseToAuthModel(userResponse)}, nil
}

// GetUserSessionsByUUID lists active (isActive && !expired) sessions
// for the user, sorted most-recent-first. The IsCurrent flag is left
// unset — the HTTP handler stamps it by comparing each
// SessionInfo.SessionID against the JWT sid claim.
func (s *authService) GetUserSessionsByUUID(ctx context.Context, userUUID string) (*models.SessionsResponse, error) {
	if userUUID == "" {
		return nil, fmt.Errorf("get user sessions: userUUID is required")
	}
	if s.authSessionRepo == nil {
		return &models.SessionsResponse{Sessions: []models.SessionInfo{}}, nil
	}
	docs, err := s.authSessionRepo.GetActiveSessionsByUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	out := make([]models.SessionInfo, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		out = append(out, models.SessionInfo{
			SessionID:    d.UUID,
			DeviceID:     d.DeviceID,
			DeviceName:   d.DeviceInfo.DeviceName,
			DeviceType:   d.DeviceInfo.DeviceType,
			Platform:     d.DeviceInfo.Platform,
			Location:     d.Location,
			IPAddress:    d.IPAddress,
			LastActivity: d.LastActivity,
			CreatedAt:    d.CreatedAt,
			ExpiresAt:    d.ExpiresAt,
			RiskScore:    d.RiskScore,
		})
	}
	return &models.SessionsResponse{
		Sessions:    out,
		ActiveCount: len(out),
	}, nil
}

// ListUserSessions is the explicitly-named alias used by the
// self-service handler. Same contract as GetUserSessionsByUUID.
func (s *authService) ListUserSessions(ctx context.Context, userUUID string) (*models.SessionsResponse, error) {
	return s.GetUserSessionsByUUID(ctx, userUUID)
}

func (s *authService) GetSessionCountByUUID(ctx context.Context, userUUID string) (int, error) {
	if userUUID == "" || s.authSessionRepo == nil {
		return 0, nil
	}
	docs, err := s.authSessionRepo.GetActiveSessionsByUser(ctx, userUUID)
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}

func (s *authService) RenameSessionByUUID(ctx context.Context, userUUID string, deviceID, deviceName string) error {
	if s.authSessionRepo == nil {
		return fmt.Errorf("session repo unavailable")
	}
	return s.authSessionRepo.RenameDevice(ctx, userUUID, deviceID, deviceName)
}

// TerminateSessionByUUID terminates every active session matching
// (userUUID, deviceID). Used by the existing logout-by-device path
// (auth_handler.LogoutHTTP). Coordinates the three-step revoke per
// matching session so refresh tokens are revoked, the session doc is
// flipped, and the sid is pushed into the Redis revocation set.
func (s *authService) TerminateSessionByUUID(ctx context.Context, userUUID string, deviceID string) error {
	if userUUID == "" || deviceID == "" {
		return fmt.Errorf("terminate session: userUUID and deviceID required")
	}
	if s.authSessionRepo == nil {
		return fmt.Errorf("session repo unavailable")
	}
	sessions, err := s.authSessionRepo.GetActiveSessionsByUser(ctx, userUUID)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess == nil || sess.DeviceID != deviceID {
			continue
		}
		if err := s.revokeSessionInternal(ctx, sess.UUID, "user_logout"); err != nil {
			return err
		}
	}
	// Sweep any stale rows the per-session loop didn't touch (older
	// duplicates, expired-but-still-active rows that the repo's
	// "active" predicate filtered out).
	return s.authSessionRepo.TerminateSessionByDevice(ctx, userUUID, deviceID)
}

// TerminateAllSessionsByUUID terminates every active session for the
// user. Drives /v1/auth/logout?allDevices=true. Best-effort revoke
// across each session, then a final TerminateAllUserSessions sweep so
// any rows missed by the per-session loop still flip isActive=false.
func (s *authService) TerminateAllSessionsByUUID(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return fmt.Errorf("terminate all sessions: userUUID required")
	}
	if s.authSessionRepo == nil {
		return fmt.Errorf("session repo unavailable")
	}
	sessions, err := s.authSessionRepo.GetActiveSessionsByUser(ctx, userUUID)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		if err := s.revokeSessionInternal(ctx, sess.UUID, "user_logout_all"); err != nil {
			return err
		}
	}
	return s.authSessionRepo.TerminateAllUserSessions(ctx, userUUID)
}

// RevokeUserSession revokes one session by UUID for the user.
// Rejects revoke-current with ErrCannotRevokeCurrent (the user has
// the existing logout for that). Returns ErrSessionNotFound when
// the session doesn't exist or doesn't belong to the calling user —
// the two are collapsed so the response can't be used to probe for
// other users' session UUIDs.
func (s *authService) RevokeUserSession(ctx context.Context, userUUID, sessionUUID, currentSid string) error {
	if userUUID == "" || sessionUUID == "" {
		return fmt.Errorf("revoke session: userUUID and sessionUUID required")
	}
	if currentSid != "" && sessionUUID == currentSid {
		return ErrCannotRevokeCurrent
	}
	if s.authSessionRepo == nil {
		return fmt.Errorf("session repo unavailable")
	}
	sess, err := s.authSessionRepo.GetByUUID(ctx, sessionUUID)
	if err != nil {
		return err
	}
	if sess == nil || sess.UserUUID != userUUID {
		return ErrSessionNotFound
	}
	if err := s.revokeSessionInternal(ctx, sessionUUID, "user_self_revoke"); err != nil {
		return err
	}
	slog.Info("self_auth_action",
		"event", "self_session_revoke",
		"userUUID", userUUID,
		"sessionUUID", sessionUUID,
	)
	return nil
}

// RevokeAllUserSessionsExcept revokes every active session for the
// user except the one matching currentSid so the caller's request
// can complete. Returns the count of sessions revoked.
func (s *authService) RevokeAllUserSessionsExcept(ctx context.Context, userUUID, currentSid string) (int, error) {
	if userUUID == "" {
		return 0, fmt.Errorf("revoke all sessions: userUUID required")
	}
	if s.authSessionRepo == nil {
		return 0, nil
	}
	sessions, err := s.authSessionRepo.GetActiveSessionsByUser(ctx, userUUID)
	if err != nil {
		return 0, err
	}
	revoked := 0
	for _, sess := range sessions {
		if sess == nil || sess.UUID == currentSid {
			continue
		}
		if err := s.revokeSessionInternal(ctx, sess.UUID, "user_self_revoke_others"); err != nil {
			return revoked, err
		}
		revoked++
	}
	slog.Info("self_auth_action",
		"event", "self_session_revoke_all",
		"userUUID", userUUID,
		"revoked", revoked,
	)
	return revoked, nil
}

// revokeSessionInternal performs the three-step revocation for a
// single session: revoke its refresh tokens → flip the session doc
// to inactive → push the sid into the Redis revocation set so
// in-flight access tokens are rejected by AuthMiddleware on the
// next request. The Redis push is best-effort (fail-open on outage)
// — the persisted state still updates so reauth via cookie is
// blocked even when Redis is unavailable.
func (s *authService) revokeSessionInternal(ctx context.Context, sessionUUID, reason string) error {
	if sessionUUID == "" {
		return nil
	}
	if s.refreshTokenRepo != nil {
		if err := s.refreshTokenRepo.RevokeTokensBySession(ctx, sessionUUID, reason); err != nil {
			return fmt.Errorf("revoke refresh tokens: %w", err)
		}
	}
	if s.authSessionRepo != nil {
		if err := s.authSessionRepo.TerminateSession(ctx, sessionUUID); err != nil {
			// Tolerate the repo's "session not found" sentinel —
			// the row may already be terminated by a concurrent
			// caller. Anything else is a real failure.
			if err.Error() != "session not found" {
				return fmt.Errorf("terminate session doc: %w", err)
			}
		}
	}
	if s.sessionRevocation != nil {
		_ = s.sessionRevocation.Revoke(ctx, sessionUUID, reason)
	}
	return nil
}

func (s *authService) GenerateEnhancedTokenPair(ctx context.Context, user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenResponse, error) {
	// MFA gating for OAuth-resolved users. Mirrors the password login path:
	// privileged users with an enrolled factor receive a partial response
	// and must call /v1/auth/mfa/login/verify; without a factor, the grace
	// window applies.
	if resp, handled, err := s.evaluateMFAForOAuth(ctx, user, deviceInfo, securityCtx); handled {
		return resp, err
	}

	fmt.Printf("[AUTH_DEBUG] ==> GenerateEnhancedTokenPair called for user: %s\n", user.UUID)
	fmt.Printf("[AUTH_DEBUG] Token creation source: OAuth Login Flow\n")
	fmt.Printf("[AUTH_DEBUG] Current active tokens for user before creation: querying database...\n")

	// Check current active tokens for debugging
	if existingTokens, err := s.refreshTokenRepo.GetActiveTokensByUser(ctx, user.UUID); err == nil {
		fmt.Printf("[AUTH_DEBUG] User currently has %d active refresh tokens\n", len(existingTokens))
		for i, token := range existingTokens {
			fmt.Printf("[AUTH_DEBUG]   Token %d: UUID=%s, DeviceID=%s, CreatedAt=%s\n",
				i+1, token.UUID, token.DeviceID, token.CreatedAt.Format("2006-01-02 15:04:05"))
		}
	} else {
		fmt.Printf("[AUTH_DEBUG] WARNING: Failed to query existing tokens: %v\n", err)
	}

	// Generate JWT tokens. OAuth logins get amr:["oauth"] so the token
	// encodes how the user authenticated — aligns with password logins'
	// amr:["pwd"] (see password_auth_service.completeLogin) and enables
	// RequireMFA / step-up middleware to distinguish primary factors.
	fmt.Printf("[AUTH_DEBUG] Generating JWT access token...\n")
	accessToken, err := s.jwtService.GenerateAccessTokenWithAMR(user, []string{"oauth"}, 0)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to generate access token: %v\n", err)
		return nil, err
	}
	fmt.Printf("[AUTH_DEBUG] Access token generated successfully (length: %d)\n", len(accessToken))

	fmt.Printf("[AUTH_DEBUG] Generating JWT refresh token...\n")
	refreshToken, err := s.jwtService.GenerateRefreshToken(user)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to generate refresh token: %v\n", err)
		return nil, err
	}
	fmt.Printf("[AUTH_DEBUG] Refresh token generated successfully (length: %d)\n", len(refreshToken))

	sessionID := uuid.New().String()
	fmt.Printf("[AUTH_DEBUG] Generated session ID: %s\n", sessionID)

	// Create refresh token record in database for persistence and tracking
	now := time.Now()

	// Handle nil security context safely
	ipAddress := "unknown"
	if securityCtx != nil && securityCtx.IPAddress != "" {
		ipAddress = securityCtx.IPAddress
	}

	// Extract device information safely with fallbacks
	deviceID := "default-device"
	deviceType := "web"
	platform := "web"
	fingerprint := "default-fingerprint"

	if deviceInfo != nil {
		fmt.Printf("[AUTH_DEBUG] Device info found: ID=%s, Type=%s, Platform=%s, Fingerprint=%s\n",
			deviceInfo.DeviceID, deviceInfo.DeviceType, deviceInfo.Platform, deviceInfo.Fingerprint)

		// Use extracted device ID or fallback to default
		if deviceInfo.DeviceID != "" {
			deviceID = deviceInfo.DeviceID
		}

		// Use extracted device type or fallback to web
		if deviceInfo.DeviceType != "" {
			deviceType = deviceInfo.DeviceType
		}

		// Use extracted platform or fallback to web
		if deviceInfo.Platform != "" {
			platform = deviceInfo.Platform
		}

		// Use extracted fingerprint or fallback to default
		if deviceInfo.Fingerprint != "" {
			fingerprint = deviceInfo.Fingerprint
		}
	} else {
		fmt.Printf("[AUTH_DEBUG] No device info found, using defaults\n")
	}

	fmt.Printf("[AUTH_DEBUG] Final device values: ID=%s, Type=%s, Platform=%s, Fingerprint=%s\n",
		deviceID, deviceType, platform, fingerprint)

	// Revoke existing active tokens for this device to enforce single-token-per-device
	// This prevents token accumulation when users login multiple times from the same device
	fmt.Printf("[AUTH_DEBUG] Revoking existing tokens for device: %s\n", deviceID)
	if err := s.refreshTokenRepo.RevokeTokensByDevice(ctx, user.UUID, deviceID, "new_login"); err != nil {
		fmt.Printf("[AUTH_DEBUG] WARNING: Failed to revoke existing device tokens: %v\n", err)
		// Continue with token creation - don't fail the login for cleanup failures
	}

	// Fresh login → fresh family. Every subsequent refresh inherits this ID
	// via RotateWithFamily so a replay of any descendant kills the lineage.
	familyID := uuid.New().String()

	refreshTokenRecord := &models.RefreshTokenDoc{
		UUID:         models.GenerateUUIDv7(),
		UserUUID:     user.UUID,
		Token:        refreshToken, // Repository layer will hash this for storage
		SessionUUID:  sessionID,
		DeviceID:     deviceID,
		DeviceType:   deviceType,
		Platform:     platform,
		Fingerprint:  fingerprint,
		IPAddress:    ipAddress,
		RiskScore:    0.1, // Low risk for OAuth
		IssuedAt:     now,
		ExpiresAt:    now.Add(7 * 24 * time.Hour), // 7 days
		LastActivity: now,
		IsRevoked:    false,
		CreatedAt:    now,
		UpdatedAt:    now,
		FamilyID:     familyID,
	}

	// Store refresh token in database
	fmt.Printf("[AUTH_DEBUG] Storing refresh token in database...\n")
	err = s.refreshTokenRepo.CreateRefreshToken(ctx, refreshTokenRecord)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to store refresh token: %v\n", err)
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}
	fmt.Printf("[AUTH_DEBUG] Refresh token stored in database successfully\n")

	// Update user's last login timestamp
	fmt.Printf("[AUTH_DEBUG] Updating user's last login timestamp...\n")
	err = s.UpdateLastLoginByUUID(ctx, user.UUID)
	if err != nil {
		// Log error but don't fail the authentication
		fmt.Printf("[AUTH_DEBUG] WARNING: Failed to update last login timestamp: %v\n", err)
	} else {
		fmt.Printf("[AUTH_DEBUG] Last login timestamp updated successfully\n")
	}

	fmt.Printf("[AUTH_DEBUG] Creating token response...\n")

	// Fetch OAuth provider information for complete user data (similar to /auth/me endpoint)
	oauthProviders, err := s.oauthProviderRepo.GetByUserUUID(ctx, user.UUID)
	if err != nil {
		// Log the error but don't fail the request - OAuth providers are optional data
		fmt.Printf("[AUTH_DEBUG] Warning: Failed to fetch OAuth providers for user %s: %v\n", user.UUID, err)
		oauthProviders = []*models.OAuthProviderDoc{}
	}

	// Convert OAuth providers to response format
	oauthProvidersInfo := models.ConvertOAuthProvidersToInfo(oauthProviders)

	// Create user response
	userResponse := user.ToResponse()

	tokenResponse := &models.TokenResponse{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		TokenType:      "Bearer",
		ExpiresIn:      900, // 15 minutes
		SessionID:      sessionID,
		DeviceID:       deviceInfo.DeviceID,
		User:           userResponse,
		OAuthProviders: oauthProvidersInfo,
	}

	fmt.Printf("[AUTH_DEBUG] Token response created - Session ID: %s, Device ID: %s\n", sessionID, deviceInfo.DeviceID)
	fmt.Printf("[AUTH_DEBUG] <== GenerateEnhancedTokenPair completed successfully\n")
	return tokenResponse, nil
}

// RefreshTokensWithRiskAssessment rotates a refresh token atomically and
// detects replay. Flow:
//  1. JWT structural validation.
//  2. Look up the row (including revoked rows — replay detection can't see
//     rotated rows otherwise).
//  3. If the row is revoked with reason "rotated", the legitimate client
//     has already advanced past this token. That's a replay — kill the
//     whole family, log, and return ErrRefreshTokenReplay.
//  4. If the row is revoked for any other reason (logout / password
//     change / role change), just reject as invalid.
//  5. Mint a new access + refresh pair and persist the new row via
//     RotateWithFamily, which atomically marks the old row rotated.
//     A CAS failure means another caller beat us to the rotation —
//     treat that as replay too.
func (s *authService) RefreshTokensWithRiskAssessment(ctx context.Context, refreshToken string, securityCtx *models.SecurityContext) (*models.TokenResponse, error) {
	// 1. Validate JWT structure and extract claims.
	claims, err := s.jwtService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	// 2. Look up the row — unfiltered so replay detection can see rotated rows.
	hashedToken := utils.HashRefreshToken(refreshToken)
	tokenDoc, err := s.refreshTokenRepo.GetByTokenAny(ctx, hashedToken)
	if err != nil {
		return nil, fmt.Errorf("failed to validate refresh token: %w", err)
	}
	if tokenDoc == nil {
		return nil, ErrInvalidRefreshToken
	}
	if time.Now().After(tokenDoc.ExpiresAt) {
		return nil, ErrInvalidRefreshToken
	}

	// 3+4. Already-revoked branches.
	if tokenDoc.IsRevoked {
		if tokenDoc.RevokedReason == models.RevokeReasonRotated {
			s.handleRefreshReplay(ctx, tokenDoc, securityCtx, "rotated_token_reused")
			return nil, ErrRefreshTokenReplay
		}
		return nil, ErrInvalidRefreshToken
	}

	// Load the user for JWT claim population.
	userModel, err := s.userService.GetUserByID(ctx, claims.UserUUID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	user := convertUserModelToAuthModel(userModel)

	// 5. Mint new JWT tokens. The access token carries forward the caller's
	// prior amr — they haven't completed a new factor, we're just rolling
	// the session forward. (last_otp_at is not elevated either.)
	amr := claims.AMR
	lastOTPAt := claims.LastOTPAt
	newAccess, err := s.jwtService.GenerateAccessTokenWithAMR(user, amr, lastOTPAt)
	if err != nil {
		return nil, fmt.Errorf("failed to mint access token: %w", err)
	}
	newRefresh, err := s.jwtService.GenerateRefreshToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to mint refresh token: %w", err)
	}

	now := time.Now()
	newSessionID := tokenDoc.SessionUUID // preserve the session — rotation is within one session
	newDoc := &models.RefreshTokenDoc{
		UUID:         models.GenerateTimeOrderedUUID(),
		UserUUID:     tokenDoc.UserUUID,
		Token:        newRefresh, // repo layer hashes on insert
		SessionUUID:  newSessionID,
		DeviceID:     tokenDoc.DeviceID,
		DeviceName:   tokenDoc.DeviceName,
		DeviceType:   tokenDoc.DeviceType,
		Platform:     tokenDoc.Platform,
		AppVersion:   tokenDoc.AppVersion,
		Fingerprint:  tokenDoc.Fingerprint,
		IPAddress:    nonEmpty(securityCtxIP(securityCtx), tokenDoc.IPAddress),
		RiskScore:    tokenDoc.RiskScore,
		IssuedAt:     now,
		ExpiresAt:    now.Add(7 * 24 * time.Hour),
		LastActivity: now,
		IsRevoked:    false,
		CreatedAt:    now,
		UpdatedAt:    now,
		FamilyID:     tokenDoc.FamilyID, // inherit: rotation doesn't start a new family
	}

	if err := s.refreshTokenRepo.RotateWithFamily(ctx, hashedToken, newDoc); err != nil {
		if errors.Is(err, repository.ErrTokenAlreadyRotated) {
			// Concurrency: another caller rotated between our Get and our
			// CAS, or the client retried. Either way, only one caller holds
			// the legitimate chain — kill the family.
			s.handleRefreshReplay(ctx, tokenDoc, securityCtx, "rotation_cas_lost")
			return nil, ErrRefreshTokenReplay
		}
		return nil, fmt.Errorf("failed to rotate refresh token: %w", err)
	}

	// Refresh user OAuth provider list for the response (matches the
	// existing GenerateEnhancedTokenPair shape so clients see no diff).
	oauthProviders, _ := s.oauthProviderRepo.GetByUserUUID(ctx, user.UUID)
	oauthProvidersInfo := models.ConvertOAuthProvidersToInfo(oauthProviders)

	return &models.TokenResponse{
		AccessToken:    newAccess,
		RefreshToken:   newRefresh,
		TokenType:      "Bearer",
		ExpiresIn:      900,
		SessionID:      newSessionID,
		DeviceID:       tokenDoc.DeviceID,
		User:           user.ToResponse(),
		OAuthProviders: oauthProvidersInfo,
	}, nil
}

// handleRefreshReplay kills the family and logs a structured warning. The
// FamilyID can be empty on pre-Block-C rows — RevokeFamily short-circuits
// to a no-op in that case so we don't accidentally revoke unrelated rows.
func (s *authService) handleRefreshReplay(ctx context.Context, doc *models.RefreshTokenDoc, securityCtx *models.SecurityContext, kind string) {
	revoked, err := s.refreshTokenRepo.RevokeFamily(ctx, doc.FamilyID, models.RevokeReasonReplayDetected)
	ip := securityCtxIP(securityCtx)
	logger := slogDefault()
	logger.Warn("refresh_token_replay",
		"userUUID", doc.UserUUID,
		"sessionId", doc.SessionUUID,
		"deviceId", doc.DeviceID,
		"familyId", doc.FamilyID,
		"ip", ip,
		"revokedCount", revoked,
		"kind", kind,
		"revokeErr", errToString(err),
	)
}

// securityCtxIP safely extracts an IP from a possibly-nil context.
func securityCtxIP(sc *models.SecurityContext) string {
	if sc == nil {
		return ""
	}
	return sc.IPAddress
}

// nonEmpty returns the first non-empty argument, or "".
func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// slogDefault wraps slog.Default() so test code can swap the logger via a
// package variable if we ever need to assert on log output. Today it's a
// passthrough — no behavioural change.
var slogDefault = defaultSlogger

func (s *authService) ValidateTokenWithRiskAssessment(ctx context.Context, token string, securityCtx *models.SecurityContext) (*models.TokenValidationResult, error) {
	claims, err := s.jwtService.ValidateAccessToken(token)
	if err != nil {
		return &models.TokenValidationResult{
			Valid: false,
			Error: err,
		}, err
	}

	return &models.TokenValidationResult{
		Valid:     true,
		Claims:    claims,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0),
		RiskScore: 0.0,
	}, nil
}

func (s *authService) AssessLoginRisk(ctx context.Context, userUUID string, securityCtx *models.SecurityContext) (*models.RiskAssessment, error) {
	if s.riskAssessment == nil {
		// Degenerate deployments (tests, setup-time first admin) run
		// without a scorer wired. Returning a zero assessment keeps the
		// surface consistent without forcing every caller to nil-check.
		return &models.RiskAssessment{
			Score:      0.0,
			Level:      RiskLevelLow,
			Factors:    []models.RiskFactor{},
			AssessedAt: time.Now(),
		}, nil
	}
	return s.riskAssessment.AssessLoginRisk(ctx, userUUID, securityCtx)
}

func (s *authService) RecordSecurityEvent(ctx context.Context, event *models.SecurityEvent) error {
	return nil // Placeholder
}

func (s *authService) GetSecurityEvents(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error) {
	return []*models.SecurityEvent{}, nil
}

func (s *authService) MigrateUserToUUID(ctx context.Context, userID primitive.ObjectID) (*userModels.User, error) {
	return nil, fmt.Errorf("migration not yet implemented")
}

func (s *authService) MigrateAllUsersToUUID(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("migration not yet implemented")
}

func (s *authService) ConvertOAuthLinksToNewFormat(ctx context.Context, userUUID string) error {
	return fmt.Errorf("OAuth link conversion not yet implemented")
}

func (s *authService) HandleOAuthCallbackWithLinking(ctx context.Context, provider models.OAuthProvider, userInfo map[string]interface{}, oauthTokens *models.OAuthProviderTokens, securityCtx *models.SecurityContext, deviceInfo *models.DeviceInfo) (*models.TokenResponse, error) {
	fmt.Printf("[AUTH_DEBUG] ==> HandleOAuthCallbackWithLinking called\n")
	fmt.Printf("[AUTH_DEBUG] Provider: %s\n", provider)

	// First extract email for validation and later use
	email, _ := userInfo["email"].(string)
	fmt.Printf("[AUTH_DEBUG] Extracted email from user info: %s\n", email)
	if email == "" {
		fmt.Printf("[AUTH_DEBUG] ERROR: Email is empty\n")
		return nil, errors.New("email required from OAuth provider")
	}

	// Extract provider-specific user ID FIRST to check for existing provider
	providerID, _ := userInfo["provider_id"].(string)
	if providerID == "" {
		// Fallback to sub claim for OpenID Connect providers
		providerID, _ = userInfo["sub"].(string)
	}
	fmt.Printf("[AUTH_DEBUG] Provider ID extracted: %s\n", providerID)

	// Check if this OAuth provider is already linked
	fmt.Printf("[AUTH_DEBUG] Checking for existing OAuth provider link...\n")
	fmt.Printf("[AUTH_DEBUG] Query: Provider=%s, ProviderID=%s\n", provider, providerID)

	existingProvider, err := s.oauthProviderRepo.GetByProviderAndID(ctx, provider, providerID)
	if err != nil && err.Error() != "failed to find OAuth provider: mongo: no documents in result" {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to check existing provider: %v\n", err)
		// Don't fail the auth, just log the error
	}

	var user *userModels.User

	if existingProvider != nil {
		// Provider exists - fetch the user by UserUUID from the provider record
		fmt.Printf("[AUTH_DEBUG] OAuth provider already linked\n")
		fmt.Printf("[AUTH_DEBUG] Existing link - UUID: %s, UserUUID: %s, Primary: %v\n",
			existingProvider.UUID, existingProvider.UserUUID, existingProvider.IsPrimary)

		// CRITICAL FIX: Fetch the actual user using the provider's UserUUID
		fmt.Printf("[AUTH_DEBUG] Fetching user by UUID: %s\n", existingProvider.UserUUID)
		userModel, err := s.userService.GetUserByID(ctx, existingProvider.UserUUID)
		if err != nil {
			fmt.Printf("[AUTH_DEBUG] ERROR: Failed to get user for existing provider: %v\n", err)
			return nil, fmt.Errorf("failed to get user for existing provider: %w", err)
		}
		user = userModel
		fmt.Printf("[AUTH_DEBUG] User fetched successfully - UUID: %s, Email: %s\n", user.UUID, user.Email)
	} else {
		// No existing provider - check if user exists by email
		fmt.Printf("[AUTH_DEBUG] No existing provider link found, checking for user by email: %s\n", email)
		userResponse, err := s.userService.GetUserByEmail(ctx, email)
		if err != nil {
			// Signup gates. Two toggles must both allow the new account:
			// the audience-scoped registration kill switch (the umbrella
			// "Allow signups" toggle, shared with password signup) and
			// the OAuth-specific gate. Unlike the password Register()
			// path there is no first-user bypass here — operators
			// bootstrap a fresh install via the password flow, which
			// retains its own kill-switch bypass for that case.
			if s.policy != nil {
				if !s.policy.RegistrationAllowed(ctx, s.audience) {
					fmt.Printf("[AUTH_DEBUG] Registration disabled by policy for audience %q, refusing OAuth signup\n", s.audience)
					return nil, ErrOAuthSignupDisabled
				}
				if !s.policy.OAuthAllowSignup(ctx, s.audience) {
					fmt.Printf("[AUTH_DEBUG] OAuth signup disabled by policy for audience %q, refusing to create user\n", s.audience)
					return nil, ErrOAuthSignupDisabled
				}
			}
			fmt.Printf("[AUTH_DEBUG] User not found in database, creating new user\n")
			// Create new user via UserService
			newUUID := models.GenerateUUIDv7()
			fmt.Printf("[AUTH_DEBUG] Generated new UUID for user: %s\n", newUUID)

			// Atomic first-admin claim (replaces the former count-based race).
			// If the sentinel is already taken by another concurrent signup,
			// fall through to the tier-default role: "guest" (lowest system
			// role) for operator-tier signups so a fresh OAuth callback
			// can't grant itself elevated privileges by default; for
			// client-tier signups, the admin-configurable defaultRoleClient
			// (falls back to "operator" when unset, matching today's
			// password-path behaviour).
			role := "guest"
			if s.audience == PolicyAudienceClient && s.policy != nil {
				role = s.policy.DefaultClientRole(ctx)
			}
			claimed := false
			if s.firstAdminClaimer != nil {
				c, err := s.firstAdminClaimer.ClaimFirstAdmin(ctx, newUUID)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: first-admin claim failed: %v; defaulting to %q\n", err, role)
				} else if c {
					claimed = true
					role = "super_admin"
					fmt.Printf("[AUTH_DEBUG] First-admin sentinel claimed; assigning 'super_admin' role\n")
				}
			}

			createInput := &userModels.CreateUserInput{
				UUID:     newUUID,
				Email:    email,
				FullName: userInfo["name"].(string),
				Role:     role,
			}
			fmt.Printf("[AUTH_DEBUG] Creating new user - Name: %s, Role: %s\n", createInput.FullName, createInput.Role)

			userModel, err := s.userService.CreateUserFromOAuth(ctx, createInput)
			if err != nil {
				fmt.Printf("[AUTH_DEBUG] ERROR: Failed to create user: %v\n", err)
				if claimed && s.firstAdminClaimer != nil {
					if relErr := s.firstAdminClaimer.Release(ctx, newUUID); relErr != nil {
						fmt.Printf("[AUTH_DEBUG] WARNING: first-admin sentinel rollback failed: %v — sentinel is orphaned\n", relErr)
					}
				}
				return nil, fmt.Errorf("failed to create user: %w", err)
			}
			user = convertUserModelToAuthModel(userModel)
			fmt.Printf("[AUTH_DEBUG] New user created successfully with UUID: %s\n", user.UUID)
		} else {
			// Phase 10: oauthAutoLinkByEmail gate. The callback found an
			// existing Orkestra account by email — auto-linking the
			// OAuth provider to it is convenient but lets an attacker
			// who controls a matching IdP email hijack a password
			// account. When off, refuse here so account linking must
			// happen from an authenticated settings page instead.
			if s.policy != nil && !s.policy.OAuthAutoLinkByEmail(ctx) {
				fmt.Printf("[AUTH_DEBUG] OAuth auto-link disabled by policy, refusing to attach provider to existing account\n")
				return nil, ErrOAuthLinkDisabled
			}
			user = convertUserResponseToAuthModel(userResponse)
			fmt.Printf("[AUTH_DEBUG] Existing user found with UUID: %s\n", user.UUID)
		}
	}

	// Link OAuth provider (if not already linked)
	fmt.Printf("[AUTH_DEBUG] ==> Starting OAuth provider linking process\n")

	if existingProvider != nil {
		fmt.Printf("[AUTH_DEBUG] OAuth provider already linked\n")
		fmt.Printf("[AUTH_DEBUG] Existing link - UUID: %s, UserUUID: %s, Primary: %v\n",
			existingProvider.UUID, existingProvider.UserUUID, existingProvider.IsPrimary)

		// Update OAuth tokens if provided
		if oauthTokens != nil {
			fmt.Printf("[AUTH_DEBUG] Updating OAuth tokens for existing provider\n")

			var encryptedAccessToken, encryptedRefreshToken string
			var accessTokenExpiresAt, refreshTokenExpiresAt *time.Time
			var tokenScopes []string

			if oauthTokens.AccessToken != "" {
				encryptedAccessToken, err = utils.EncryptOAuthToken(oauthTokens.AccessToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt access token: %v\n", err)
				} else {
					if oauthTokens.ExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.ExpiresIn) * time.Second)
						accessTokenExpiresAt = &expiresAt
					}
				}
			}

			if oauthTokens.RefreshToken != "" {
				encryptedRefreshToken, err = utils.EncryptOAuthToken(oauthTokens.RefreshToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt refresh token: %v\n", err)
				} else {
					if oauthTokens.RefreshTokenExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.RefreshTokenExpiresIn) * time.Second)
						refreshTokenExpiresAt = &expiresAt
					}
				}
			}

			tokenScopes = oauthTokens.Scopes

			// Update existing provider with new tokens
			existingProvider.AccessToken = encryptedAccessToken
			existingProvider.RefreshToken = encryptedRefreshToken
			existingProvider.AccessTokenExpiresAt = accessTokenExpiresAt
			existingProvider.RefreshTokenExpiresAt = refreshTokenExpiresAt
			existingProvider.TokenStatus = "active"
			existingProvider.LastTokenRefresh = &time.Time{}
			*existingProvider.LastTokenRefresh = time.Now()
			existingProvider.Scopes = tokenScopes
			existingProvider.UpdatedAt = time.Now()

			// Update tokens in database
			err = s.oauthProviderRepo.UpdateOAuthTokens(ctx, existingProvider.UUID,
				encryptedAccessToken, encryptedRefreshToken,
				accessTokenExpiresAt, refreshTokenExpiresAt, tokenScopes)
			if err != nil {
				fmt.Printf("[AUTH_DEBUG] WARNING: Failed to update OAuth tokens in database: %v\n", err)
			} else {
				fmt.Printf("[AUTH_DEBUG] OAuth tokens updated successfully - Access: %v, Refresh: %v\n",
					encryptedAccessToken != "", encryptedRefreshToken != "")
			}
		}

		// Update last used timestamp
		fmt.Printf("[AUTH_DEBUG] Updating last used timestamp for provider\n")
		err = s.oauthProviderRepo.UpdateLastUsed(ctx, existingProvider.UUID)
		if err != nil {
			fmt.Printf("[AUTH_DEBUG] WARNING: Failed to update last used: %v\n", err)
		}
	} else {
		fmt.Printf("[AUTH_DEBUG] No existing provider link found, creating new link\n")

		// Check if this is the first provider for the user
		userProviders, err := s.oauthProviderRepo.GetByUserUUID(ctx, user.UUID)
		isPrimary := len(userProviders) == 0
		fmt.Printf("[AUTH_DEBUG] This will be primary provider: %v (user has %d existing providers)\n", isPrimary, len(userProviders))

		// Extract additional provider metadata
		picture, _ := userInfo["picture"].(string)
		locale, _ := userInfo["locale"].(string)

		metadata := make(map[string]interface{})
		if picture != "" {
			metadata["picture"] = picture
		}
		if locale != "" {
			metadata["locale"] = locale
		}

		// Encrypt OAuth tokens if provided
		var encryptedAccessToken, encryptedRefreshToken string
		var accessTokenExpiresAt, refreshTokenExpiresAt *time.Time
		var tokenStatus string = "active"
		var tokenScopes []string

		if oauthTokens != nil {
			if oauthTokens.AccessToken != "" {
				var err error
				encryptedAccessToken, err = utils.EncryptOAuthToken(oauthTokens.AccessToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt access token: %v\n", err)
					// Continue without storing access token rather than failing auth
				} else {
					fmt.Printf("[AUTH_DEBUG] Access token encrypted successfully\n")
					if oauthTokens.ExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.ExpiresIn) * time.Second)
						accessTokenExpiresAt = &expiresAt
					}
				}
			}

			if oauthTokens.RefreshToken != "" {
				var err error
				encryptedRefreshToken, err = utils.EncryptOAuthToken(oauthTokens.RefreshToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt refresh token: %v\n", err)
					// Continue without storing refresh token rather than failing auth
				} else {
					fmt.Printf("[AUTH_DEBUG] Refresh token encrypted successfully\n")
					if oauthTokens.RefreshTokenExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.RefreshTokenExpiresIn) * time.Second)
						refreshTokenExpiresAt = &expiresAt
					}
				}
			}

			tokenScopes = oauthTokens.Scopes
			fmt.Printf("[AUTH_DEBUG] OAuth tokens prepared - Access: %v, Refresh: %v, Scopes: %v\n",
				encryptedAccessToken != "", encryptedRefreshToken != "", tokenScopes)
		}

		// Create new OAuth provider link with encrypted tokens
		newProvider := &models.OAuthProviderDoc{
			UUID:                  models.GenerateUUIDv7(),
			UserUUID:              user.UUID,
			Provider:              provider,
			ProviderID:            providerID,
			Email:                 email,
			IsPrimary:             isPrimary,
			LinkedAt:              time.Now(),
			AccessToken:           encryptedAccessToken,
			RefreshToken:          encryptedRefreshToken,
			AccessTokenExpiresAt:  accessTokenExpiresAt,
			RefreshTokenExpiresAt: refreshTokenExpiresAt,
			TokenStatus:           tokenStatus,
			Scopes:                tokenScopes,
			Metadata:              metadata,
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		}

		fmt.Printf("[AUTH_DEBUG] Creating OAuth provider link...\n")
		fmt.Printf("[AUTH_DEBUG] Provider details - UUID: %s, UserUUID: %s, Provider: %s, Primary: %v\n",
			newProvider.UUID, newProvider.UserUUID, newProvider.Provider, newProvider.IsPrimary)

		err = s.oauthProviderRepo.CreateOAuthProvider(ctx, newProvider)
		if err != nil {
			fmt.Printf("[AUTH_DEBUG] ERROR: Failed to create OAuth provider link: %v\n", err)
			// Don't fail the auth, continue without provider linking
		} else {
			fmt.Printf("[AUTH_DEBUG] OAuth provider linked successfully - UUID: %s\n", newProvider.UUID)
			fmt.Printf("[AUTH_DEBUG] Provider metadata saved: %v\n", metadata)
		}
	}

	fmt.Printf("[AUTH_DEBUG] <== OAuth provider linking process completed\n")

	// Use provided device info or create defaults
	if deviceInfo != nil {
		fmt.Printf("[AUTH_DEBUG] Using device info from OAuth state - DeviceID: %s, Fingerprint: %s\n",
			deviceInfo.DeviceID, deviceInfo.Fingerprint)
	} else {
		// Fallback to creating minimal device info with a new device ID
		deviceInfo = &models.DeviceInfo{
			DeviceID: uuid.New().String(),
		}
		fmt.Printf("[AUTH_DEBUG] No device info provided, generated device ID: %s\n", deviceInfo.DeviceID)
	}

	fmt.Printf("[AUTH_DEBUG] Calling GenerateEnhancedTokenPair to create JWT tokens\n")
	tokenResponse, err := s.GenerateEnhancedTokenPair(ctx, user, deviceInfo, securityCtx)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to generate token pair: %v\n", err)
		return nil, err
	}
	fmt.Printf("[AUTH_DEBUG] <== HandleOAuthCallbackWithLinking completed successfully\n")
	return tokenResponse, nil
}

// Helper functions for type conversion between user and auth models

// convertUserModelToAuthModel is deprecated - we now use userModels.User directly
// This function just returns the same user model
func convertUserModelToAuthModel(userModel *userModels.User) *userModels.User {
	return userModel
}

// convertUserResponseToAuthModel converts a UserManagementResponse to user.User
func convertUserResponseToAuthModel(userResponse *userModels.UserManagementResponse) *userModels.User {
	if userResponse == nil {
		return nil
	}

	user := &userModels.User{
		UUID:          userResponse.ID,
		Email:         userResponse.Email,
		Username:      userResponse.Username,
		FullName:      userResponse.FullName,
		Avatar:        userResponse.Avatar,
		Role:          userResponse.Role,
		IsActive:      userResponse.IsActive,
		EmailVerified: userResponse.EmailVerified,
		LastLogin:     userResponse.LastLogin,
		CreatedAt:     userResponse.CreatedAt,
		UpdatedAt:     userResponse.UpdatedAt,
	}

	// OAuth links are not included in UserManagementResponse
	// They would need to be fetched separately if needed

	return user
}

// evaluateMFAForOAuth applies the Block B decision tree to an OAuth-resolved
// user before any token is minted. Returns (response, handled, error):
//   - handled=true means evaluateMFAForOAuth produced the final result and
//     GenerateEnhancedTokenPair should return immediately
//   - handled=false means MFA is not required or not wired; caller proceeds
//     with the normal full-token issuance path
func (s *authService) evaluateMFAForOAuth(ctx context.Context, user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenResponse, bool, error) {
	if user == nil || s.mfaFactorRepo == nil || s.mfaChallengeService == nil {
		return nil, false, nil
	}
	memberships := s.loadMembershipsAsAuthModel(ctx, user.UUID)
	if !s.policy.MFARequired(user, memberships) {
		return nil, false, nil
	}

	hasTOTP := false
	factor, err := s.mfaFactorRepo.FindByUserAndType(ctx, user.UUID, models.MFAFactorTOTP)
	if err == nil && factor != nil {
		hasTOTP = true
	} else if err != nil && !errors.Is(err, repository.ErrMFAFactorNotFound) {
		return nil, true, err
	}
	hasWebAuthn := false
	if s.webauthnAvailability != nil {
		hasWebAuthn = s.webauthnAvailability.HasWebAuthnCredentials(ctx, user.UUID)
	}
	if hasTOTP || hasWebAuthn {
		in := LoginChallengeInput{
			UserUUID:  user.UUID,
			SourceAMR: []string{"oauth"},
		}
		if deviceInfo != nil {
			in.DeviceID = deviceInfo.DeviceID
			in.Platform = deviceInfo.Platform
			in.Fingerprint = deviceInfo.Fingerprint
		}
		if securityCtx != nil {
			in.IPAddress = securityCtx.IPAddress
		}
		ch, err := s.mfaChallengeService.BeginLogin(ctx, in)
		if err != nil {
			return nil, true, err
		}
		return &models.TokenResponse{
			RequiresMFA:       true,
			MFAToken:          ch.ID,
			WebAuthnAvailable: hasWebAuthn,
			User:              user.ToResponse(),
		}, true, nil
	}

	// Privileged user without a factor → grace window.
	now := time.Now()
	if s.policy.MFAGraceExpired(ctx, user, now) {
		return nil, true, ErrMFAEnrollmentRequired
	}
	if user.MFAGraceStartedAt == nil {
		_ = s.userService.StartMFAGraceIfUnset(ctx, user.UUID)
	}
	// caller continues to issue a full token — the completeLogin branch
	// for the token response enrichment happens in the OAuth handler layer.
	return nil, false, nil
}

// loadMembershipsAsAuthModel mirrors the password service helper — converts
// tenant memberships into the lightweight auth-model shape RoleRequiresMFA
// consumes. Returns nil on error; RoleRequiresMFA then falls back to the
// system-role check alone.
func (s *authService) loadMembershipsAsAuthModel(ctx context.Context, userUUID string) []models.TenantMembership {
	if s.tenantProvider == nil {
		return nil
	}
	list, err := s.tenantProvider.ListUserMemberships(ctx, userUUID)
	if err != nil || len(list) == 0 {
		return nil
	}
	out := make([]models.TenantMembership, 0, len(list))
	for _, m := range list {
		out = append(out, models.TenantMembership{TenantUUID: m.TenantUUID, TenantKind: m.TenantKind, Roles: m.Roles})
	}
	return out
}
