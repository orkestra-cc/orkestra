package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/types"
	"github.com/orkestra/backend/internal/shared/utils"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	authService       services.AuthService
	oauthFactory      services.OAuthProviderFactory
	oauthResolver     *services.OAuthConfigResolver
	oauthStateService services.OAuthStateService
	oauthProviderRepo repository.OAuthProviderRepository
	jwtService        services.JWTService
	sessionRevocation services.SessionRevocationService
	config            *config.Config

	// cookieDomain scopes refresh-token cookies minted by this handler
	// instance — operator-tier handlers carry the operator host's domain
	// (`console.*`), client-tier handlers carry the client host's
	// (`api.*`). ADR-0003 PR-D D-9 split: handlers no longer share a
	// single cfg.Auth.Cookie.Domain — that field is the legacy single-
	// host fallback, resolved at construction time in module.go.
	cookieDomain string

	// ADR-0003 PR-D D-6: state-encoded tier dispatch.
	//
	// tier identifies which audience this handler instance was wired
	// for. Stamped into the signed-state JWT every time this instance
	// initiates an OAuth login so the single shared callback can
	// dispatch the resulting flow to the matching tier's authService.
	// Empty on the legacy /v1/auth/oauth/login mount: callbacks for
	// states without a tier claim self-handle on the legacy authService
	// instead of delegating, preserving pre-D-6 behaviour for any
	// /v1/auth/oauth/login flows that were started before the cutover.
	tier string
	// stateSecret signs (and validates) the OAuth state JWT. Derived
	// once at module init from the JWT private key so every replica
	// agrees on the secret. Empty disables OAuth start endpoints — the
	// handler returns a configuration error rather than minting an
	// unsigned state.
	stateSecret []byte
	// tierDispatch routes a callback's tier-stamped state to the
	// AuthHandler instance bound to that tier. Set only on the
	// handler that owns the callback routes (the legacy / operator-mux
	// instance); a nil map means "no dispatch — handle every callback
	// locally" (legacy single-tier deployments). When the state JWT's
	// tier claim is non-empty and matches a key in this map, the
	// callback delegates the user-creation/token-issuance step to the
	// matching handler so the tokens are minted by the audience's
	// authService and stamped with the audience's JWT aud claim.
	tierDispatch map[string]*AuthHandler

	// policy resolves admin-managed login policy. Nil = legacy "always
	// allow" semantics. Currently consulted only for the LoginAllowed
	// kill switch on OAuth start endpoints; later phases will plumb
	// this into MFA / session-limit decisions.
	policy *services.AuthPolicyService

	// blobStore is consumed by GetCurrentUser to mint fresh presigned
	// GET URLs for uploaded avatars. Optional — when nil, uploaded
	// avatars fall back to whatever URL was stored on the user
	// document (likely empty for fresh uploads).
	blobStore blob.Store
}

// SetBlobStore wires the object-storage handle used to mint presigned
// GET URLs for uploaded avatars on the /me response path. Optional;
// leaving it nil degrades uploaded avatars to "" (the SPA will render
// initials).
func (h *AuthHandler) SetBlobStore(s blob.Store) {
	h.blobStore = s
}

// SetSessionRevocation wires the revoked-session store so logout can
// invalidate the current session's sid instantly instead of waiting for
// the access-token TTL. Optional — nil falls back to refresh-token
// invalidation only (the pre-revocation behavior).
func (h *AuthHandler) SetSessionRevocation(s services.SessionRevocationService) {
	h.sessionRevocation = s
}

// SetTier records which audience this AuthHandler issues OAuth flows
// for. Operator/client tier handlers stamp this value on the state JWT
// their start endpoints mint; the callback decodes the same value to
// dispatch back to the matching authService.
func (h *AuthHandler) SetTier(t string) {
	h.tier = t
}

// SetPolicy wires the admin-managed auth policy reader. Optional —
// nil leaves the kill switch always-allow (legacy behaviour).
func (h *AuthHandler) SetPolicy(p *services.AuthPolicyService) {
	h.policy = p
}

// loginAllowed returns false when the kill switch is active for this
// handler's tier. Empty tier (legacy mount) inherits operator semantics
// so a kill on the operator surface also pauses the legacy callback.
func (h *AuthHandler) loginAllowed(ctx context.Context) bool {
	if h.policy == nil {
		return true
	}
	return h.policy.LoginAllowed(ctx, h.policyAudience())
}

// policyAudience maps the handler's tier string to the PolicyAudience
// enum AuthPolicyService consumes. Empty tier inherits operator
// semantics — same as loginAllowed.
func (h *AuthHandler) policyAudience() services.PolicyAudience {
	if h.tier == services.AudienceClient {
		return services.PolicyAudienceClient
	}
	return services.PolicyAudienceOperator
}

// oauthProviderAllowed combines two checks: the kill switch on the
// surface, and the per-provider per-surface enable flag. Both must
// be true. Returns nil on success, a 403 codedError otherwise so
// callers can return it directly.
func (h *AuthHandler) oauthProviderAllowed(ctx context.Context, provider string) error {
	if !h.loginAllowed(ctx) {
		return &codedError{
			Status: http.StatusForbidden,
			Title:  "Forbidden",
			Detail: "Login is temporarily disabled for this surface. Contact an administrator.",
			Code:   "login_disabled",
		}
	}
	if h.policy != nil && !h.policy.OAuthProviderEnabled(ctx, h.policyAudience(), provider) {
		return &codedError{
			Status: http.StatusForbidden,
			Title:  "Forbidden",
			Detail: "This OAuth provider is not enabled for this surface. Contact an administrator.",
			Code:   "oauth_provider_disabled",
		}
	}
	return nil
}

// SetStateSecret wires the HMAC secret used to sign and validate the
// OAuth state JWT. Must be the same secret on every replica that may
// receive a callback for a flow initiated elsewhere — derived from
// shared key material in the auth module's Init.
func (h *AuthHandler) SetStateSecret(s []byte) {
	h.stateSecret = s
}

// SetTierDispatch wires the per-tier dispatcher map consulted on every
// OAuth callback. Only the handler instance that owns the callback
// routes (the legacy / operator-mux instance) needs this set; tier-
// specific handlers leave it nil and never receive a callback they
// must dispatch.
func (h *AuthHandler) SetTierDispatch(d map[string]*AuthHandler) {
	h.tierDispatch = d
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(
	authService services.AuthService,
	oauthFactory services.OAuthProviderFactory,
	oauthResolver *services.OAuthConfigResolver,
	oauthStateService services.OAuthStateService,
	oauthProviderRepo repository.OAuthProviderRepository,
	jwtService services.JWTService,
	config *config.Config,
	cookieDomain string,
) *AuthHandler {
	return &AuthHandler{
		authService:       authService,
		oauthFactory:      oauthFactory,
		oauthResolver:     oauthResolver,
		oauthStateService: oauthStateService,
		oauthProviderRepo: oauthProviderRepo,
		jwtService:        jwtService,
		config:            config,
		cookieDomain:      cookieDomain,
	}
}

// currentSessionID returns the JWT sid claim from the request context, or
// "" when the caller is unauthenticated or carrying a pre-sid token.
func currentSessionID(ctx context.Context) string {
	sid, _ := middleware.GetSessionID(ctx)
	return sid
}

// oauthSignupDisabled is a thin errors.Is wrapper kept inline so each
// provider's callback can branch on the policy outcome without
// duplicating the import. Phase 9 of the auth-policy roadmap.
func oauthSignupDisabled(err error) bool {
	return errors.Is(err, services.ErrOAuthSignupDisabled)
}

// redirectOAuthSignupDisabled bounces the caller back to the frontend
// callback URL with success=false + error=oauth_signup_disabled. The
// SPA renders a friendly "Sign-up is currently invitation-only" page
// instead of a generic 500. Falls through to a plain 403 when the
// frontend URL isn't configured.
func redirectOAuthSignupDisabled(w http.ResponseWriter, r *http.Request, frontendURL string) {
	if frontendURL == "" {
		http.Error(w, "OAuth signup is disabled", http.StatusForbidden)
		return
	}
	dest := fmt.Sprintf("%s/auth/callback?success=false&error=oauth_signup_disabled", frontendURL)
	http.Redirect(w, r, dest, http.StatusFound)
}

// resolveProvider fetches the current config for an OAuth provider from the
// resolver and constructs a provider instance. Returns a 400 equivalent error
// when the provider is not configured in the admin panel.
func (h *AuthHandler) resolveProvider(ctx context.Context, p models.OAuthProvider) (services.OAuthProviderInterface, *services.OAuthProviderConfig, error) {
	cfg, ok := h.oauthResolver.Get(ctx, p)
	if !ok {
		return nil, nil, fmt.Errorf("oauth provider %q is not configured", p)
	}
	provider, err := h.oauthFactory.CreateProvider(p, cfg)
	if err != nil {
		return nil, nil, err
	}
	return provider, cfg, nil
}

// GetAuthPolicyRequest is the empty input for the public policy endpoint —
// declared as a struct so Huma generates the correct zero-arg operation.
type GetAuthPolicyRequest struct{}

// GetAuthPolicyResponse returns the slice of admin-managed auth policy
// the unauthenticated SPA needs to know about so it can hide signup /
// login CTAs and surface password requirements without round-tripping
// through a 403. Audience is implicit in the route prefix.
type GetAuthPolicyResponse struct {
	Body struct {
		RegistrationEnabled   bool `json:"registrationEnabled" doc:"Whether self-service signup is currently accepted on this surface"`
		LoginEnabled          bool `json:"loginEnabled" doc:"Whether interactive login is currently accepted on this surface"`
		OAuthSignupAllowed    bool `json:"oauthSignupAllowed" doc:"Whether an unknown email returning from an OAuth flow may be auto-provisioned on this surface"`
		MFAEnabled            bool `json:"mfaEnabled" doc:"Whether the master MFA switch is on — a user MFA settings page should render an inert badge when this is false"`
		PasswordMinLength     int  `json:"passwordMinLength" doc:"Minimum password length the signup form should advertise"`
		PasswordMaxLength     int  `json:"passwordMaxLength" doc:"Maximum password length the signup form should accept"`
		PasswordRequireUpper  bool `json:"passwordRequireUpper" doc:"Whether the signup form should advertise an uppercase requirement"`
		PasswordRequireLower  bool `json:"passwordRequireLower" doc:"Whether the signup form should advertise a lowercase requirement"`
		PasswordRequireDigit  bool `json:"passwordRequireDigit" doc:"Whether the signup form should advertise a digit requirement"`
		PasswordRequireSymbol bool `json:"passwordRequireSymbol" doc:"Whether the signup form should advertise a symbol requirement"`
	}
}

// GetAuthPolicy returns the public-facing slice of the admin-managed
// auth policy for this handler's audience. Public endpoint — no auth
// required — so the unauthenticated login + signup pages can render a
// maintenance banner / hide the CTA before the user types anything.
//
// Read-through: the same AuthPolicyService that gates Register / Login
// already provides nil-safe defaults (registration on, login on,
// passwordMinLength 10). This handler just projects those values into a
// shape the SPA can consume.
func (h *AuthHandler) GetAuthPolicy(ctx context.Context, _ *GetAuthPolicyRequest) (*GetAuthPolicyResponse, error) {
	resp := &GetAuthPolicyResponse{}
	audience := h.policyAudience()
	resp.Body.RegistrationEnabled = h.policy.RegistrationAllowed(ctx, audience)
	resp.Body.LoginEnabled = h.policy.LoginAllowed(ctx, audience)
	resp.Body.OAuthSignupAllowed = h.policy.OAuthAllowSignup(ctx, audience)
	resp.Body.MFAEnabled = h.policy.MFAEnabled(ctx)
	pp := h.policy.PasswordPolicy(ctx)
	resp.Body.PasswordMinLength = pp.MinLength
	resp.Body.PasswordMaxLength = pp.MaxLength
	resp.Body.PasswordRequireUpper = pp.RequireUpper
	resp.Body.PasswordRequireLower = pp.RequireLower
	resp.Body.PasswordRequireDigit = pp.RequireDigit
	resp.Body.PasswordRequireSymbol = pp.RequireSymbol
	return resp, nil
}

// ListOAuthProvidersRequest is the empty input for the providers endpoint —
// declared as a struct so Huma generates the correct zero-arg operation.
type ListOAuthProvidersRequest struct{}

// ListOAuthProvidersResponse returns only providers that currently have a
// client ID configured. The login UI uses this to decide which social
// buttons to render; never lists a provider the backend can't actually serve.
type ListOAuthProvidersResponse struct {
	Body struct {
		Providers []string `json:"providers" doc:"Provider names that are fully configured and ready to accept logins"`
	}
}

// ListOAuthProviders returns the set of OAuth providers configured in the
// admin panel. Public endpoint — no auth required — because it's used by
// the unauthenticated login screen. The result is filtered by the
// handler's audience: a provider that's configured but disabled for
// this surface (per the OAuth Providers tab on /admin/modules/auth)
// is omitted so the login UI never offers a button it can't honor.
func (h *AuthHandler) ListOAuthProviders(ctx context.Context, _ *ListOAuthProvidersRequest) (*ListOAuthProvidersResponse, error) {
	configured := h.oauthResolver.ConfiguredProviders(ctx)
	resp := &ListOAuthProvidersResponse{}
	resp.Body.Providers = make([]string, 0, len(configured))
	for _, p := range configured {
		if h.policy != nil && !h.policy.OAuthProviderEnabled(ctx, h.policyAudience(), string(p)) {
			continue
		}
		resp.Body.Providers = append(resp.Body.Providers, string(p))
	}
	return resp, nil
}

// OAuth Login Request
type OAuthLoginRequest struct {
	Body struct {
		Provider models.OAuthProvider `json:"provider" enum:"google,apple,discord,github" doc:"OAuth provider name"`
	}
}

// OAuth Login Response
type OAuthLoginResponse struct {
	Body struct {
		AuthURL string `json:"authUrl" doc:"URL to redirect the user for OAuth authentication"`
		State   string `json:"state" doc:"OAuth state parameter for security"`
	}
}

// InitiateOAuthLogin handles the OAuth login initiation
func (h *AuthHandler) InitiateOAuthLogin(ctx context.Context, req *OAuthLoginRequest) (*OAuthLoginResponse, error) {
	logger := slog.Default()
	logger.Debug("InitiateOAuthLogin called",
		slog.String("provider", string(req.Body.Provider)),
		slog.String("tier", h.tier),
	)

	if err := h.oauthProviderAllowed(ctx, string(req.Body.Provider)); err != nil {
		return nil, err
	}

	if len(h.stateSecret) == 0 {
		// ADR-0003 PR-D D-6: every monolith-issued state JWT must be
		// signable. A missing secret is a wiring bug — surface it
		// loudly rather than minting an unsigned state.
		logger.Error("oauth state secret not configured")
		return nil, huma.Error500InternalServerError("OAuth not available", nil)
	}

	// Backend always determines frontend redirect URL automatically
	var frontendRedirectURL string
	if rawRequest, ok := ctx.Value("http_request").(*http.Request); ok {
		origin := rawRequest.Header.Get("Origin")
		if origin != "" {
			frontendRedirectURL = origin + "/auth/callback"
		} else {
			// Fallback to configured frontend URL
			frontendRedirectURL = h.config.Server.FrontendURL + "/auth/callback"
		}
	} else {
		// Fallback to configured frontend URL
		frontendRedirectURL = h.config.Server.FrontendURL + "/auth/callback"
	}

	// Extract device info from context (set by device middleware)
	var deviceInfo *models.DeviceInfo
	if di := ctx.Value("deviceInfo"); di != nil {
		if d, ok := di.(*types.DeviceInfo); ok {
			// Convert types.DeviceInfo to models.DeviceInfo
			deviceInfo = &models.DeviceInfo{
				DeviceID:    d.DeviceID,
				DeviceType:  d.DeviceType,
				Platform:    d.Platform,
				UserAgent:   d.UserAgent,
				Fingerprint: d.Fingerprint,
			}
		}
	}

	// ADR-0003 PR-D D-6: state becomes a signed JWT carrying the tier
	// claim. The CSRF nonce inside the JWT doubles as the Redis key
	// used to look up the per-flow side data on callback, so the JWT
	// itself stays small while the existing one-time-use Redis
	// semantics carry over.
	csrf, err := services.GenerateOAuthCSRF()
	if err != nil {
		logger.Error("Failed to generate OAuth CSRF nonce", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to create OAuth state", err)
	}
	signedState, err := services.SignOAuthStateToken(h.stateSecret, h.tier, csrf, 10*time.Minute)
	if err != nil {
		logger.Error("Failed to sign OAuth state", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to create OAuth state", err)
	}

	stateRequest := &services.StoreOAuthStateRequest{
		Provider:       req.Body.Provider,
		Tier:           h.tier,
		State:          csrf,
		RedirectURI:    frontendRedirectURL,
		DeviceInfo:     deviceInfo,
		ExpiryDuration: 10 * time.Minute,
	}

	if _, err := h.oauthStateService.StoreOAuthState(ctx, stateRequest); err != nil {
		logger.Error("Failed to create OAuth state", slog.String("error", err.Error()))
		return nil, huma.Error400BadRequest("Failed to create OAuth state", err)
	}

	// Create OAuth provider from live admin-panel config.
	provider, _, err := h.resolveProvider(ctx, req.Body.Provider)
	if err != nil {
		logger.Error("Failed to create OAuth provider", slog.String("error", err.Error()))
		return nil, huma.Error400BadRequest("OAuth provider not configured", err)
	}

	backendCallbackURL := h.oauthResolver.RedirectURL(ctx, req.Body.Provider)
	if backendCallbackURL == "" {
		return nil, huma.Error400BadRequest("OAuth provider redirect URL not configured", nil)
	}

	authURL := provider.GetAuthURL(signedState, "", backendCallbackURL)

	return &OAuthLoginResponse{
		Body: struct {
			AuthURL string `json:"authUrl" doc:"URL to redirect the user for OAuth authentication"`
			State   string `json:"state" doc:"OAuth state parameter for security"`
		}{
			AuthURL: authURL,
			State:   signedState,
		},
	}, nil
}

// OAuthLinkRequest is path-only — the provider is encoded in the
// URL so the start endpoint mirrors the unlink-self surface
// (DELETE /me/oauth/{provider}).
type OAuthLinkRequest struct {
	Provider string `path:"provider" enum:"google,apple,discord,github" doc:"OAuth provider name"`
}

// InitiateOAuthLink kicks off the self-service "add a sign-in
// provider" flow. Authenticated; the caller's userUUID is stamped
// into the signed-state JWT (Mode=link, LinkUserUUID=caller) so the
// shared callback can bind the new identity without trusting any
// query-string parameter or fresh login result. Returns the
// IdP-redirect URL the SPA should `window.location.assign` to.
func (h *AuthHandler) InitiateOAuthLink(ctx context.Context, req *OAuthLinkRequest) (*OAuthLoginResponse, error) {
	logger := slog.Default()

	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}

	provider := models.OAuthProvider(strings.ToLower(req.Provider))
	switch provider {
	case models.OAuthProviderGoogle, models.OAuthProviderApple, models.OAuthProviderGitHub, models.OAuthProviderDiscord:
	default:
		return nil, huma.Error400BadRequest("unsupported provider")
	}
	if err := h.oauthProviderAllowed(ctx, string(provider)); err != nil {
		return nil, err
	}
	if len(h.stateSecret) == 0 {
		logger.Error("oauth state secret not configured")
		return nil, huma.Error500InternalServerError("OAuth not available", nil)
	}

	// SPA-side redirect target — same shape as InitiateOAuthLogin so
	// the browser-facing post-callback path stays consistent. Linking
	// flows redirect to /user/security?tab=oauth on success/failure;
	// that targeting happens in the callback's link branch below, not
	// here.
	var frontendRedirectURL string
	if rawRequest, ok := ctx.Value("http_request").(*http.Request); ok {
		origin := rawRequest.Header.Get("Origin")
		if origin != "" {
			frontendRedirectURL = origin + "/user/security"
		} else {
			frontendRedirectURL = h.config.Server.FrontendURL + "/user/security"
		}
	} else {
		frontendRedirectURL = h.config.Server.FrontendURL + "/user/security"
	}

	csrf, err := services.GenerateOAuthCSRF()
	if err != nil {
		logger.Error("Failed to generate OAuth CSRF nonce", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to create OAuth state", err)
	}
	signedState, err := services.SignOAuthLinkStateToken(h.stateSecret, h.tier, csrf, userUUID, 10*time.Minute)
	if err != nil {
		logger.Error("Failed to sign OAuth link state", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to create OAuth state", err)
	}

	stateRequest := &services.StoreOAuthStateRequest{
		Provider:       provider,
		Tier:           h.tier,
		State:          csrf,
		RedirectURI:    frontendRedirectURL,
		ExpiryDuration: 10 * time.Minute,
		Mode:           services.OAuthStateModeLink,
		LinkUserUUID:   userUUID,
	}
	if _, err := h.oauthStateService.StoreOAuthState(ctx, stateRequest); err != nil {
		logger.Error("Failed to create OAuth link state", slog.String("error", err.Error()))
		return nil, huma.Error400BadRequest("Failed to create OAuth state", err)
	}

	providerSvc, _, err := h.resolveProvider(ctx, provider)
	if err != nil {
		logger.Error("Failed to create OAuth provider", slog.String("error", err.Error()))
		return nil, huma.Error400BadRequest("OAuth provider not configured", err)
	}
	backendCallbackURL := h.oauthResolver.RedirectURL(ctx, provider)
	if backendCallbackURL == "" {
		return nil, huma.Error400BadRequest("OAuth provider redirect URL not configured", nil)
	}
	authURL := providerSvc.GetAuthURL(signedState, "", backendCallbackURL)

	return &OAuthLoginResponse{
		Body: struct {
			AuthURL string `json:"authUrl" doc:"URL to redirect the user for OAuth authentication"`
			State   string `json:"state" doc:"OAuth state parameter for security"`
		}{
			AuthURL: authURL,
			State:   signedState,
		},
	}, nil
}

// finishOAuthLinkRedirect drives the link-mode branch of every
// provider callback. Calls SelfLinkOAuthFromCallback on the
// dispatched-target authService and renders a single neutral
// 302 → /user/security?tab=oauth&link=<status>&provider=<x>. Caller
// is responsible for having already validated the state token and
// dispatched to the correct tier.
func (h *AuthHandler) finishOAuthLinkRedirect(
	w http.ResponseWriter,
	r *http.Request,
	target *AuthHandler,
	provider models.OAuthProvider,
	userInfo map[string]interface{},
	oauthTokens *models.OAuthProviderTokens,
	linkUserUUID string,
) {
	logger := slog.Default()
	frontendURL := target.config.Server.FrontendURL
	if frontendURL == "" {
		frontendURL = h.config.Server.FrontendURL
	}
	frontendURL = strings.TrimRight(frontendURL, "/")

	redirect := func(status, code string) {
		base := frontendURL + "/user/security?tab=oauth&link=" + url.QueryEscape(status) + "&provider=" + url.QueryEscape(string(provider))
		if code != "" {
			base += "&code=" + url.QueryEscape(code)
		}
		http.Redirect(w, r, base, http.StatusFound)
	}

	if err := target.authService.SelfLinkOAuthFromCallback(r.Context(), linkUserUUID, userModels.OAuthProvider(provider), userInfo, oauthTokens); err != nil {
		switch {
		case errors.Is(err, services.ErrOAuthLinkClaimedByOther):
			logger.Info("oauth link refused: identity already claimed",
				slog.String("userUUID", linkUserUUID),
				slog.String("provider", string(provider)))
			redirect("failed", "already_linked")
			return
		case errors.Is(err, services.ErrOAuthLinkAlreadyExists):
			logger.Info("oauth link refused: provider already attached",
				slog.String("userUUID", linkUserUUID),
				slog.String("provider", string(provider)))
			redirect("failed", "duplicate_provider")
			return
		case errors.Is(err, services.ErrOAuthLinkInvalidUserInfo):
			logger.Warn("oauth link refused: incomplete provider userinfo",
				slog.String("userUUID", linkUserUUID),
				slog.String("provider", string(provider)))
			redirect("failed", "invalid_userinfo")
			return
		default:
			logger.Error("oauth link failed",
				slog.String("userUUID", linkUserUUID),
				slog.String("provider", string(provider)),
				slog.String("error", err.Error()))
			redirect("failed", "internal")
			return
		}
	}
	redirect("success", "")
}

// finishOAuthMFAPartialRedirect handles the partial-response branch
// of the OAuth callback: when evaluateMFAForOAuth in the auth service
// determines the user must complete a second factor before tokens are
// minted, HandleOAuthCallbackWithLinking returns a TokenResponse with
// RequiresMFA=true and no Access/RefreshToken. The callback must NOT
// SetRefreshTokenCookie in that case — passing an empty value would
// overwrite any previously valid cookie with an unusable one and leave
// the SPA stuck on /auth/callback?success=true with no working session.
//
// Instead, redirect to /auth/callback with requiresMfa=true plus the
// challenge id; SocialAuthCallback on the SPA reads those params and
// routes to /mfa/verify (matching the password login MFA-partial path).
// Returns true when the partial branch was hit and the caller should
// stop processing.
func (h *AuthHandler) finishOAuthMFAPartialRedirect(
	w http.ResponseWriter,
	r *http.Request,
	target *AuthHandler,
	tokenResponse *models.TokenResponse,
	provider string,
) bool {
	if tokenResponse == nil || !tokenResponse.RequiresMFA {
		return false
	}
	frontendURL := strings.TrimRight(target.config.Server.FrontendURL, "/")
	qs := url.Values{}
	qs.Set("requiresMfa", "true")
	qs.Set("provider", provider)
	if tokenResponse.MFAToken != "" {
		qs.Set("mfaToken", tokenResponse.MFAToken)
	}
	if tokenResponse.WebAuthnAvailable {
		qs.Set("webauthnAvailable", "true")
	}
	if tokenResponse.User != nil {
		qs.Set("user_id", tokenResponse.User.ID)
		qs.Set("email", tokenResponse.User.Email)
	}
	http.Redirect(w, r, frontendURL+"/auth/callback?"+qs.Encode(), http.StatusFound)
	return true
}

// resolveOAuthMFAPartialRedirect is the Huma-handler counterpart of
// finishOAuthMFAPartialRedirect — same contract, but returns the
// OAuthCallbackResponse the GitHub Huma callback can hand back to the
// framework. (nil, false) means the caller should fall through to the
// regular full-token path.
func (h *AuthHandler) resolveOAuthMFAPartialRedirect(
	target *AuthHandler,
	tokenResponse *models.TokenResponse,
	provider string,
) (*OAuthCallbackResponse, bool) {
	if tokenResponse == nil || !tokenResponse.RequiresMFA {
		return nil, false
	}
	frontendURL := strings.TrimRight(target.config.Server.FrontendURL, "/")
	qs := url.Values{}
	qs.Set("requiresMfa", "true")
	qs.Set("provider", provider)
	if tokenResponse.MFAToken != "" {
		qs.Set("mfaToken", tokenResponse.MFAToken)
	}
	if tokenResponse.WebAuthnAvailable {
		qs.Set("webauthnAvailable", "true")
	}
	if tokenResponse.User != nil {
		qs.Set("user_id", tokenResponse.User.ID)
		qs.Set("email", tokenResponse.User.Email)
	}
	resp := &OAuthCallbackResponse{Status: 302}
	resp.Headers.Location = frontendURL + "/auth/callback?" + qs.Encode()
	return resp, true
}

// resolveOAuthLinkRedirect is the Huma-handler counterpart to
// finishOAuthLinkRedirect. Same link-mode contract; returns an
// OAuthCallbackResponse the Huma handler can return directly so the
// link branch composes inside the existing GitHub / Apple Huma
// surfaces.
func (h *AuthHandler) resolveOAuthLinkRedirect(
	ctx context.Context,
	target *AuthHandler,
	provider models.OAuthProvider,
	userInfo map[string]interface{},
	oauthTokens *models.OAuthProviderTokens,
	linkUserUUID string,
) (*OAuthCallbackResponse, error) {
	frontendURL := target.config.Server.FrontendURL
	if frontendURL == "" {
		frontendURL = h.config.Server.FrontendURL
	}
	frontendURL = strings.TrimRight(frontendURL, "/")
	build := func(status, code string) string {
		base := frontendURL + "/user/security?tab=oauth&link=" + url.QueryEscape(status) + "&provider=" + url.QueryEscape(string(provider))
		if code != "" {
			base += "&code=" + url.QueryEscape(code)
		}
		return base
	}
	resp := &OAuthCallbackResponse{Status: 302}

	if err := target.authService.SelfLinkOAuthFromCallback(ctx, linkUserUUID, userModels.OAuthProvider(provider), userInfo, oauthTokens); err != nil {
		switch {
		case errors.Is(err, services.ErrOAuthLinkClaimedByOther):
			resp.Headers.Location = build("failed", "already_linked")
		case errors.Is(err, services.ErrOAuthLinkAlreadyExists):
			resp.Headers.Location = build("failed", "duplicate_provider")
		case errors.Is(err, services.ErrOAuthLinkInvalidUserInfo):
			resp.Headers.Location = build("failed", "invalid_userinfo")
		default:
			slog.Default().Error("oauth link failed",
				slog.String("userUUID", linkUserUUID),
				slog.String("provider", string(provider)),
				slog.String("error", err.Error()))
			resp.Headers.Location = build("failed", "internal")
		}
		return resp, nil
	}
	resp.Headers.Location = build("success", "")
	return resp, nil
}

// resolveStateForCallback validates the signed-state JWT presented to a
// callback handler, looks up the matching Redis side-data row, and
// returns the (cross-checked) state info. Returns ErrInvalidStateToken
// equivalents as a generic 400-style error so callers can render a
// single neutral message regardless of the failure mode.
//
// Tier-cross-check: if the JWT tier claim is non-empty it must match
// the Redis row's tier — otherwise an attacker who races a legitimate
// flow could swap their own pre-stored row in. Empty tier on either
// side is treated as legacy and only legacy-on-both-sides is accepted.
func (h *AuthHandler) resolveStateForCallback(ctx context.Context, raw string) (*services.OAuthStateInfo, *services.OAuthStateClaims, error) {
	if len(h.stateSecret) == 0 {
		return nil, nil, fmt.Errorf("oauth state secret not configured")
	}
	claims, err := services.ValidateOAuthStateToken(h.stateSecret, raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid OAuth state: %w", err)
	}
	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, claims.CSRF)
	if err != nil {
		return nil, nil, fmt.Errorf("OAuth state not found or expired: %w", err)
	}
	if stateInfo.Tier != claims.Tier {
		return nil, nil, fmt.Errorf("OAuth state tier mismatch")
	}
	// Cross-check the link-mode pair (mode + linkUserUUID) — if either
	// side stamped a link flow but the other didn't, treat it as a
	// forged state (an attacker who tampered with one half in
	// isolation). Both empty (login) or both populated (link with
	// matching UUIDs) is fine.
	if stateInfo.Mode != claims.Mode || stateInfo.LinkUserUUID != claims.LinkUserUUID {
		return nil, nil, fmt.Errorf("OAuth state mode mismatch")
	}
	return stateInfo, claims, nil
}

// dispatchTarget picks the AuthHandler that should drive token issuance
// for a callback whose state-JWT tier claim is `tier`. Returns the
// receiver itself when no dispatch is needed (tier empty, no map, or no
// matching key) so legacy /v1/auth/oauth/* flows keep using the
// callback handler's own authService.
func (h *AuthHandler) dispatchTarget(tier string) *AuthHandler {
	if tier == "" || h.tierDispatch == nil {
		return h
	}
	if target, ok := h.tierDispatch[tier]; ok && target != nil {
		return target
	}
	return h
}

// OAuth Callback Request
type OAuthCallbackRequest struct {
	Code  string `query:"code" doc:"Authorization code from OAuth provider"`
	State string `query:"state" doc:"OAuth state parameter"`
}

// OAuth Callback Response with redirect
type OAuthCallbackResponse struct {
	Headers struct {
		Location string `header:"Location"`
	}
	Status int `status:"302"`
}

// Token Response (for non-redirect endpoints)
type TokenResponse struct {
	Body models.TokenResponse
}

// HandleGoogleCallback handles Google OAuth callback
// func (h *AuthHandler) HandleGoogleCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
// 	// Validate state
// 	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, req.State)
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
// 	}

// 	// Create Google OAuth service
// 	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderGoogle, nil)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get OAuth provider", err)
// 	}

// 	// Exchange code for tokens - must use same redirect URI as initial auth request
// 	backendBaseURL := "https://erpb.blacklab.cc" // TODO: Make this configurable
// 	backendCallbackURL := backendBaseURL + "/v1/auth/oauth/google/callback"
// 	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
// 		Code:        req.Code,
// 		RedirectURI: backendCallbackURL,
// 	})
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Failed to exchange code", err)
// 	}

// 	// Get user info from provider
// 	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get user info", err)
// 	}

// 	// Create or update user
// 	user := &models.User{
// 		UUID:          userInfo.ProviderID,
// 		Email:         userInfo.Email,
// 		Username:      userInfo.Email,
// 		FullName:      userInfo.Name,
// 		Avatar:        userInfo.Picture,
// 		EmailVerified: userInfo.EmailVerified,
// 		IsActive:      true,
// 		Role:          "viewer", // Default role
// 	}

// 	// Generate tokens
// 	tokenResponse, err := h.authService.GenerateEnhancedTokenPair(ctx, user, stateInfo.DeviceInfo, stateInfo.SecurityContext)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to generate tokens", err)
// 	}

// 	// Redirect to frontend with tokens
// 	frontendURL := h.config.Server.FrontendURL
// 	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&access_token=%s&token_type=Bearer&expires_in=%d&user_id=%s&email=%s&provider=google",
// 		frontendURL,
// 		url.QueryEscape(tokenResponse.AccessToken),
// 		tokenResponse.ExpiresIn,
// 		url.QueryEscape(user.UUID),
// 		url.QueryEscape(user.Email))

// 	resp := &OAuthCallbackResponse{
// 		Status: 302,
// 	}
// 	resp.Headers.Location = redirectURL

// 	return resp, nil
// }

// HandleGoogleCallbackHTTP handles Google OAuth callback with proper HTTP redirect
func (h *AuthHandler) HandleGoogleCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract query parameters
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if state == "" || code == "" {
		logger.Warn("Missing state or code parameter in OAuth callback")
		http.Error(w, "Missing state or code parameter", http.StatusBadRequest)
		return
	}

	// ADR-0003 PR-D D-6: validate the signed-state JWT and dispatch
	// to the tier-bound AuthHandler for token issuance. dispatchTarget
	// returns the receiver when no tier was stamped (legacy flow) so
	// existing /v1/auth/oauth/login callbacks keep self-handling.
	stateInfo, claims, err := h.resolveStateForCallback(ctx, state)
	if err != nil {
		logger.Warn("Invalid OAuth state", slog.String("error", err.Error()))
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}
	target := h.dispatchTarget(claims.Tier)

	// Create Google OAuth provider from live admin-panel config.
	provider, _, err := h.resolveProvider(ctx, models.OAuthProviderGoogle)
	if err != nil {
		logger.Error("Failed to create OAuth provider", slog.String("error", err.Error()))
		http.Error(w, "Google OAuth not configured", http.StatusInternalServerError)
		return
	}

	// Exchange code for tokens
	backendCallbackURL := h.oauthResolver.RedirectURL(ctx, models.OAuthProviderGoogle)

	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		logger.Error("Failed to exchange code for tokens", slog.String("error", err.Error()))
		http.Error(w, "Failed to exchange code", http.StatusBadRequest)
		return
	}

	// Get user info from provider
	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		logger.Error("Failed to get user info", slog.String("error", err.Error()))
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int(tokenResp.ExpiresIn),
		Scopes:       tokenResp.Scope,
		IDToken:      tokenResp.IDToken,
	}

	// Link mode (state.Mode=="link"): the user is already
	// authenticated; bind the new identity instead of minting tokens.
	if claims.Mode == services.OAuthStateModeLink {
		h.finishOAuthLinkRedirect(w, r, target, models.OAuthProviderGoogle, userInfoMap, oauthTokens, claims.LinkUserUUID)
		return
	}

	// Use enhanced auth service for proper user creation and token management
	tokenResponse, err := target.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderGoogle, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		if oauthSignupDisabled(err) {
			redirectOAuthSignupDisabled(w, r, target.config.Server.FrontendURL)
			return
		}
		logger.Error("Failed to process OAuth callback", slog.String("error", err.Error()))
		http.Error(w, "Failed to process OAuth callback", http.StatusInternalServerError)
		return
	}
	// MFA-partial path: privileged user with an enrolled factor — no
	// tokens were minted. Redirect to /auth/callback with the challenge
	// id so the SPA routes to /mfa/verify, and skip the cookie write
	// (an empty refresh token would clobber any previously valid one).
	if h.finishOAuthMFAPartialRedirect(w, r, target, tokenResponse, "google") {
		return
	}

	// Set only refresh token as secure HttpOnly cookie
	// Use cookie configuration from environment
	cookieName := target.config.Auth.Cookie.Name // Set from COOKIE_NAME env var
	cookieDomain := target.cookieDomain          // ADR-0003 PR-D D-9: per-tier
	isSecure := target.config.Auth.Cookie.Secure // Set from COOKIE_SECURE env var

	// Set only refresh token in cookie (7 days expiry)
	// Access token will be sent in the redirect URL for the client to store
	utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days for refresh token

	// Redirect to frontend without access token (refresh token is in cookie, access token will be fetched via /auth/session)
	frontendURL := target.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=google",
		frontendURL,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// HandleDiscordCallbackHTTP handles Discord OAuth callback with proper HTTP redirect
func (h *AuthHandler) HandleDiscordCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract query parameters
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if state == "" || code == "" {
		http.Error(w, "Missing state or code parameter", http.StatusBadRequest)
		return
	}

	// ADR-0003 PR-D D-6: validate signed state + dispatch by tier.
	stateInfo, claims, err := h.resolveStateForCallback(ctx, state)
	if err != nil {
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}
	target := h.dispatchTarget(claims.Tier)

	// Create Discord OAuth provider from live admin-panel config.
	provider, _, err := h.resolveProvider(ctx, models.OAuthProviderDiscord)
	if err != nil {
		http.Error(w, "Discord OAuth not configured", http.StatusInternalServerError)
		return
	}

	// Exchange code for tokens
	backendCallbackURL := h.oauthResolver.RedirectURL(ctx, models.OAuthProviderDiscord)
	tokenResponse, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		http.Error(w, "Failed to exchange code for token", http.StatusInternalServerError)
		return
	}

	// Get user info
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResponse.AccessToken,
		RefreshToken: tokenResponse.RefreshToken,
		TokenType:    tokenResponse.TokenType,
		ExpiresIn:    int(tokenResponse.ExpiresIn),
		Scopes:       tokenResponse.Scope,
	}

	if claims.Mode == services.OAuthStateModeLink {
		h.finishOAuthLinkRedirect(w, r, target, models.OAuthProviderDiscord, userInfoMap, oauthTokens, claims.LinkUserUUID)
		return
	}

	// Use enhanced auth service for proper user creation and token management
	authTokenResponse, err := target.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderDiscord, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		http.Error(w, "Failed to process OAuth callback", http.StatusInternalServerError)
		return
	}
	if h.finishOAuthMFAPartialRedirect(w, r, target, authTokenResponse, "discord") {
		return
	}

	// Set only refresh token as secure HttpOnly cookie
	// Use cookie configuration from environment
	cookieName := target.config.Auth.Cookie.Name // Set from COOKIE_NAME env var
	cookieDomain := target.cookieDomain          // ADR-0003 PR-D D-9: per-tier
	isSecure := target.config.Auth.Cookie.Secure // Set from COOKIE_SECURE env var

	// Set only refresh token in cookie (7 days expiry)
	// Access token will be sent in the redirect URL for the client to store
	utils.SetRefreshTokenCookie(w, cookieName, authTokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days for refresh token

	// Redirect to frontend without access token (refresh token is in cookie, access token will be fetched via /auth/session)
	frontendURL := target.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=discord",
		frontendURL,
		url.QueryEscape(authTokenResponse.User.ID),
		url.QueryEscape(authTokenResponse.User.Email))

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// HandleAppleCallbackHTTP handles Apple OAuth callback with proper HTTP redirect
func (h *AuthHandler) HandleAppleCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	utils.AuthDebugFlow("HandleAppleCallbackHTTP")
	ctx := r.Context()

	// Parse form data (Apple uses POST with form data, not query parameters)
	if err := r.ParseForm(); err != nil {
		utils.AuthDebugError("parse_form", err)
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	// Extract form parameters
	state := r.FormValue("state")
	code := r.FormValue("code")
	idToken := r.FormValue("id_token")

	// Debug logging with sensitive data protection
	utils.AuthDebugPresence("state", state)
	utils.AuthDebugPresence("code", code)
	utils.AuthDebugPresence("id_token", idToken)

	if code == "" {
		utils.AuthDebugError("validation", fmt.Errorf("missing authorization code"))
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	var stateInfo *services.OAuthStateInfo
	var claims *services.OAuthStateClaims
	target := h

	if state == "" {
		// SECURITY: In production, state parameter is REQUIRED to prevent CSRF attacks
		if h.config.IsProductionLike() {
			utils.AuthDebugError("security", fmt.Errorf("missing state parameter in production - possible CSRF attack"))
			http.Error(w, "Missing state parameter - authentication rejected for security", http.StatusBadRequest)
			return
		}

		// Development only: Allow fallback with warning (for testing Apple Sign In configuration issues)
		if idToken == "" {
			utils.AuthDebugError("security", fmt.Errorf("missing both state and id_token"))
			http.Error(w, "Missing security parameters", http.StatusBadRequest)
			return
		}

		// Log security warning for development
		utils.AuthDebug("SECURITY WARNING: State parameter missing - this would fail in production!")
		utils.AuthDebug("Fix Apple Service ID configuration to include state parameter")

		// Create minimal state info for development testing only
		stateInfo = &services.OAuthStateInfo{
			State:       "apple-dev-fallback",
			Provider:    models.OAuthProviderApple,
			RedirectURI: h.config.Server.FrontendURL + "/auth/callback",
			DeviceInfo:  nil,
			SecurityContext: &models.SecurityContext{
				IPAddress: utils.GetClientIP(r),
				Timestamp: time.Now(),
			},
		}
	} else {
		// ADR-0003 PR-D D-6: validate signed state + dispatch by tier.
		var err error
		stateInfo, claims, err = h.resolveStateForCallback(ctx, state)
		if err != nil {
			utils.AuthDebugError("state_validation", err)
			http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
			return
		}
		target = h.dispatchTarget(claims.Tier)
		utils.AuthDebug("OAuth state validated - Provider: %s, Tier: %s", stateInfo.Provider, claims.Tier)
	}

	// Create Apple OAuth provider from live admin-panel config.
	utils.AuthDebug("Creating Apple OAuth provider")
	provider, _, err := h.resolveProvider(ctx, models.OAuthProviderApple)
	if err != nil {
		utils.AuthDebugError("create_provider", err)
		http.Error(w, "Apple OAuth not configured", http.StatusInternalServerError)
		return
	}

	// Exchange code for tokens
	backendCallbackURL := h.oauthResolver.RedirectURL(ctx, models.OAuthProviderApple)
	utils.AuthDebug("Exchanging code for tokens")

	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		utils.AuthDebugError("exchange_code", err)
		http.Error(w, "Failed to exchange code", http.StatusBadRequest)
		return
	}
	utils.AuthDebug("Code exchanged successfully")

	// Get user info from Apple ID token (Apple doesn't provide a user info endpoint)
	utils.AuthDebug("Validating Apple ID token")
	userInfo, err := provider.ValidateIDToken(ctx, &services.IDTokenValidationRequest{
		IDToken:     tokenResp.IDToken,
		AccessToken: tokenResp.AccessToken,
		Audience:    provider.GetClientID(),
	})
	if err != nil {
		utils.AuthDebugError("validate_id_token", err)
		http.Error(w, "Failed to validate ID token", http.StatusInternalServerError)
		return
	}
	utils.AuthDebugPresence("user_email", userInfo.Email)

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int(tokenResp.ExpiresIn),
		Scopes:       tokenResp.Scope,
		IDToken:      tokenResp.IDToken,
	}
	utils.AuthDebug("OAuth tokens prepared - has access: %v, has refresh: %v",
		tokenResp.AccessToken != "", tokenResp.RefreshToken != "")

	if claims != nil && claims.Mode == services.OAuthStateModeLink {
		h.finishOAuthLinkRedirect(w, r, target, models.OAuthProviderApple, userInfoMap, oauthTokens, claims.LinkUserUUID)
		return
	}

	// Use enhanced auth service for proper user creation and token management
	utils.AuthDebug("Processing OAuth callback with linking")
	tokenResponse, err := target.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderApple, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		utils.AuthDebugError("oauth_callback", err)
		http.Error(w, "Failed to process OAuth callback", http.StatusInternalServerError)
		return
	}
	if h.finishOAuthMFAPartialRedirect(w, r, target, tokenResponse, "apple") {
		return
	}
	// Set only refresh token as secure HttpOnly cookie
	// Use cookie configuration from environment
	cookieName := target.config.Auth.Cookie.Name // Set from COOKIE_NAME env var
	cookieDomain := target.cookieDomain          // ADR-0003 PR-D D-9: per-tier
	isSecure := target.config.Auth.Cookie.Secure // Set from COOKIE_SECURE env var

	// Set only refresh token in cookie (7 days expiry)
	// Access token will be sent in the redirect URL for the client to store
	utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days for refresh token

	// Redirect to frontend without access token (refresh token is in cookie, access token will be fetched via /auth/session)
	frontendURL := target.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=apple",
		frontendURL,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// HandleAppleCallback handles Apple OAuth callback
func (h *AuthHandler) HandleAppleCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
	// Similar to Google callback
	stateInfo, claims, err := h.resolveStateForCallback(ctx, req.State)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
	}
	target := h.dispatchTarget(claims.Tier)

	provider, _, err := h.resolveProvider(ctx, models.OAuthProviderApple)
	if err != nil {
		return nil, huma.Error500InternalServerError("Apple OAuth not configured", err)
	}

	// Exchange code for tokens - must use same redirect URI as initial auth request
	backendCallbackURL := h.oauthResolver.RedirectURL(ctx, models.OAuthProviderApple)
	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        req.Code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to exchange code", err)
	}

	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user info", err)
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int(tokenResp.ExpiresIn),
		IDToken:      tokenResp.IDToken,
	}

	// Use enhanced auth service for proper user creation and token management
	tokenResponse, err := target.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderApple, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to process OAuth callback", err)
	}

	// Redirect to frontend with tokens (Note: Huma handlers can't set cookies directly)
	frontendURL := target.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&access_token=%s&token_type=Bearer&expires_in=%d&user_id=%s&email=%s&provider=apple",
		frontendURL,
		url.QueryEscape(tokenResponse.AccessToken),
		tokenResponse.ExpiresIn,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	resp := &OAuthCallbackResponse{
		Status: 302,
	}
	resp.Headers.Location = redirectURL

	return resp, nil
}

// HandleDiscordCallback handles Discord OAuth callback
// func (h *AuthHandler) HandleDiscordCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
// 	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, req.State)
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
// 	}

// 	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderDiscord, nil)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get OAuth provider", err)
// 	}

// 	// Exchange code for tokens - must use same redirect URI as initial auth request
// 	backendBaseURL := "https://erpb.blacklab.cc" // TODO: Make this configurable
// 	backendCallbackURL := backendBaseURL + "/v1/auth/oauth/discord/callback"
// 	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
// 		Code:        req.Code,
// 		RedirectURI: backendCallbackURL,
// 	})
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Failed to exchange code", err)
// 	}

// 	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get user info", err)
// 	}

// 	// Convert userInfo to map for enhanced auth service
// 	userInfoMap := map[string]interface{}{
// 		"email":          userInfo.Email,
// 		"name":           userInfo.Name,
// 		"picture":        userInfo.Picture,
// 		"provider_id":    userInfo.ProviderID,
// 		"email_verified": userInfo.EmailVerified,
// 	}

// 	// Use enhanced auth service for proper user creation and token management
// 	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderDiscord, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to process OAuth callback", err)
// 	}

// 	// Redirect to frontend with tokens (Note: Huma handlers can't set cookies directly)
// 	frontendURL := h.config.Server.FrontendURL
// 	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&access_token=%s&token_type=Bearer&expires_in=%d&user_id=%s&email=%s&provider=discord",
// 		frontendURL,
// 		url.QueryEscape(tokenResponse.AccessToken),
// 		tokenResponse.ExpiresIn,
// 		url.QueryEscape(tokenResponse.User.ID),
// 		url.QueryEscape(tokenResponse.User.Email))

// 	resp := &OAuthCallbackResponse{
// 		Status: 302,
// 	}
// 	resp.Headers.Location = redirectURL

// 	return resp, nil
// }

// HandleGitHubCallback handles GitHub OAuth callback
func (h *AuthHandler) HandleGitHubCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
	stateInfo, claims, err := h.resolveStateForCallback(ctx, req.State)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
	}
	target := h.dispatchTarget(claims.Tier)

	provider, _, err := h.resolveProvider(ctx, models.OAuthProviderGitHub)
	if err != nil {
		return nil, huma.Error500InternalServerError("GitHub OAuth not configured", err)
	}

	// Exchange code for tokens - must use same redirect URI as initial auth request
	backendCallbackURL := h.oauthResolver.RedirectURL(ctx, models.OAuthProviderGitHub)
	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        req.Code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to exchange code", err)
	}

	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user info", err)
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		Scopes:      tokenResp.Scope,
	}

	if claims.Mode == services.OAuthStateModeLink {
		return h.resolveOAuthLinkRedirect(ctx, target, models.OAuthProviderGitHub, userInfoMap, oauthTokens, claims.LinkUserUUID)
	}

	// Use enhanced auth service for proper user creation and token management
	tokenResponse, err := target.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderGitHub, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to process OAuth callback", err)
	}
	if resp, ok := h.resolveOAuthMFAPartialRedirect(target, tokenResponse, "github"); ok {
		return resp, nil
	}

	// Redirect to frontend without access token (access token will be fetched via /auth/session)
	// Note: Huma handlers can't set cookies directly, so refresh token handling may need adjustment
	frontendURL := target.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=github",
		frontendURL,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	resp := &OAuthCallbackResponse{
		Status: 302,
	}
	resp.Headers.Location = redirectURL

	return resp, nil
}

// Refresh Token Request
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" doc:"Refresh token to exchange for new tokens"`
}

// RefreshTokensResponse simplified response with token in header
type RefreshTokensResponse struct {
	Headers struct {
		XNewAccessToken string `header:"X-New-Access-Token" doc:"New access token"`
	}
	Body struct {
		TokenType string `json:"tokenType"`
		ExpiresIn int64  `json:"expiresIn"`
		Success   bool   `json:"success"`
	}
}

// RefreshTokens handles token refresh
func (h *AuthHandler) RefreshTokens(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokensResponse, error) {
	logger := slog.Default()

	// Extract device info and IP address from request context
	deviceInfo := middleware.GetDeviceInfo(ctx)
	var ipAddress string
	if deviceInfo != nil {
		ipAddress = deviceInfo.IP
	} else {
		ipAddress = "unknown" // Fallback if device info is not available
	}

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	var refreshToken string

	// First, try to get refresh token from cookie if available (Huma context doesn't have direct HTTP request access)
	// Check if we have access to the raw HTTP request from context
	if rawRequest, ok := ctx.Value("http_request").(*http.Request); ok {
		cookieName := h.config.Auth.Cookie.Name
		if cookieToken, err := utils.GetRefreshTokenFromCookieByName(rawRequest, cookieName); err == nil {
			refreshToken = cookieToken
		}
	}

	// If no token from cookie, use token from request body
	if refreshToken == "" && req.RefreshToken != "" {
		refreshToken = req.RefreshToken
	}

	// If no token found in either place
	if refreshToken == "" {
		return nil, huma.Error401Unauthorized("No refresh token provided", nil)
	}

	// Validate and refresh tokens with risk assessment
	tokenResponse, err := h.authService.RefreshTokensWithRiskAssessment(ctx, refreshToken, securityCtx)
	if err != nil {
		logger.Warn("Token refresh failed", slog.String("error", err.Error()))
		if errors.Is(err, services.ErrRefreshTokenReplay) {
			return nil, huma.Error401Unauthorized("refresh_token_replay: session revoked", err)
		}
		return nil, huma.Error401Unauthorized("Invalid refresh token", err)
	}

	return &RefreshTokensResponse{
		Headers: struct {
			XNewAccessToken string `header:"X-New-Access-Token" doc:"New access token"`
		}{
			XNewAccessToken: tokenResponse.AccessToken,
		},
		Body: struct {
			TokenType string `json:"tokenType"`
			ExpiresIn int64  `json:"expiresIn"`
			Success   bool   `json:"success"`
		}{
			TokenType: "Bearer",
			ExpiresIn: tokenResponse.ExpiresIn,
			Success:   true,
		},
	}, nil
}

// RefreshTokensWithHeaderHTTP handles token refresh with access token in X-New-Access-Token header
func (h *AuthHandler) RefreshTokensWithHeaderHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract device info and IP address from request
	ipAddress := utils.GetClientIP(r)

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Extract refresh token from cookie or request body
	var refreshToken string
	var tokenSource string

	// First, try to get refresh token from cookie (using configured cookie name)
	cookieName := h.config.Auth.Cookie.Name
	if cookieToken, err := utils.GetRefreshTokenFromCookieByName(r, cookieName); err == nil {
		refreshToken = cookieToken
		tokenSource = "cookie"
	} else {
		// If no token from cookie, try parsing request body
		var req RefreshTokenRequest
		if r.Header.Get("Content-Type") == "application/json" {
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.RefreshToken != "" {
				refreshToken = req.RefreshToken
				tokenSource = "request_body"
			}
		}
	}

	// If no token found in either place
	if refreshToken == "" {
		http.Error(w, "No refresh token provided", http.StatusUnauthorized)
		return
	}

	// Validate and refresh tokens with risk assessment
	tokenResponse, err := h.authService.RefreshTokensWithRiskAssessment(ctx, refreshToken, securityCtx)
	if err != nil {
		logger.Warn("Token refresh failed", slog.String("error", err.Error()))
		writeRefreshErr(w, err)
		return
	}

	// Set new refresh token as cookie if we got the original from a cookie
	if tokenSource == "cookie" {
		cookieDomain := h.cookieDomain
		isSecure := h.config.Auth.Cookie.Secure
		utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days
	}

	// Set the access token in the X-New-Access-Token header
	w.Header().Set("X-New-Access-Token", tokenResponse.AccessToken)

	// Return minimal JSON response
	w.Header().Set("Content-Type", "application/json")
	response := struct {
		TokenType string `json:"tokenType"`
		ExpiresIn int64  `json:"expiresIn"`
		Success   bool   `json:"success"`
	}{
		TokenType: "Bearer",
		ExpiresIn: tokenResponse.ExpiresIn,
		Success:   true,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// GetSessionHTTP handles session initialization for web clients after OAuth callback
// It uses the refresh token from cookie to generate a fresh access token
func (h *AuthHandler) GetSessionHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract device info and IP address from request
	ipAddress := utils.GetClientIP(r)

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Extract refresh token from cookie. Try every candidate the browser
	// sent under this name — when multiple cookies share the name (e.g. a
	// stale Path=/auth cookie from a prior deployment plus the current
	// Path=/ one), `r.Cookie()` returns only the first match which may be
	// the stale rotated token. Trying each one in order and stopping at
	// the first that successfully refreshes avoids tripping the
	// family-replay guard on every page refresh.
	cookieName := h.config.Auth.Cookie.Name
	candidates := utils.GetAllRefreshTokensFromCookies(r, cookieName)
	if len(candidates) == 0 {
		// Bootstrap probe: no refresh cookie means the browser has never
		// authenticated (or has logged out). This is a normal state at app
		// startup, not an auth failure, so return 200 with
		// authenticated:false. 401 stays reserved for "cookie present but
		// refresh rejected" (expired, revoked, replay) — a real error the
		// client should surface. The frontend's getSession query treats
		// authenticated:false the same as a null session.
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(struct {
			Authenticated bool `json:"authenticated"`
			Success       bool `json:"success"`
		}{Authenticated: false, Success: false}); err != nil {
			logger.Error("Failed to encode unauthenticated session response", slog.String("error", err.Error()))
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	var tokenResponse *models.TokenResponse
	var lastErr error
	for _, candidate := range candidates {
		resp, err := h.authService.RefreshTokensWithRiskAssessment(ctx, candidate, securityCtx)
		if err == nil {
			tokenResponse = resp
			break
		}
		lastErr = err
	}
	if tokenResponse == nil {
		logger.Warn("Token refresh failed",
			slog.String("error", lastErr.Error()),
			slog.Int("candidatesTried", len(candidates)),
		)
		writeRefreshErr(w, lastErr)
		return
	}

	// Set new refresh token as cookie
	cookieDomain := h.cookieDomain
	isSecure := h.config.Auth.Cookie.Secure
	utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days

	// Return the access token and user info in the response body for Redux storage
	w.Header().Set("Content-Type", "application/json")
	response := struct {
		AccessToken    string                             `json:"accessToken"`
		TokenType      string                             `json:"tokenType"`
		ExpiresIn      int64                              `json:"expiresIn"`
		User           *userModels.UserManagementResponse `json:"user"`
		OAuthProviders []models.OAuthProviderInfo         `json:"oauthProviders,omitempty"`
		Authenticated  bool                               `json:"authenticated"`
		Success        bool                               `json:"success"`
	}{
		AccessToken:    tokenResponse.AccessToken,
		TokenType:      "Bearer",
		ExpiresIn:      tokenResponse.ExpiresIn,
		User:           tokenResponse.User,
		OAuthProviders: tokenResponse.OAuthProviders,
		Authenticated:  true,
		Success:        true,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// MobileGoogleAuthRequest represents the request from mobile app with Google tokens
type MobileGoogleAuthRequest struct {
	Body struct {
		IDToken     string `json:"id_token" form:"id_token" doc:"Google ID token from mobile app"`
		AccessToken string `json:"access_token,omitempty" form:"access_token" doc:"Google access token from mobile app"`
	}
}

// MobileGoogleAuthResponse represents the response to mobile app with JWT tokens
type MobileGoogleAuthResponse struct {
	Body struct {
		AccessToken  string `json:"access_token" doc:"JWT access token for API access"`
		RefreshToken string `json:"refresh_token" doc:"JWT refresh token for token renewal"`
		TokenType    string `json:"token_type" doc:"Token type (Bearer)"`
		ExpiresIn    int64  `json:"expires_in" doc:"Access token expiration time in seconds"`
		User         struct {
			ID            string `json:"id" doc:"User ID"`
			Email         string `json:"email" doc:"User email"`
			Name          string `json:"name" doc:"User full name"`
			Avatar        string `json:"avatar,omitempty" doc:"User avatar URL"`
			EmailVerified bool   `json:"email_verified" doc:"Email verification status"`
		} `json:"user" doc:"User information"`
	}
}

// HandleMobileGoogleAuth handles Google authentication from mobile apps
func (h *AuthHandler) HandleMobileGoogleAuth(ctx context.Context, req *MobileGoogleAuthRequest) (*MobileGoogleAuthResponse, error) {
	logger := slog.Default()

	if err := h.oauthProviderAllowed(ctx, "google"); err != nil {
		return nil, err
	}

	// Extract device info from context
	var deviceInfo *models.DeviceInfo
	var ipAddress string = "unknown"
	if di := ctx.Value("deviceInfo"); di != nil {
		if d, ok := di.(*types.DeviceInfo); ok {
			deviceInfo = &models.DeviceInfo{
				DeviceID:    d.DeviceID,
				DeviceType:  d.DeviceType,
				Platform:    d.Platform,
				UserAgent:   d.UserAgent,
				Fingerprint: d.Fingerprint,
			}
			ipAddress = d.IP // Get IP from types.DeviceInfo
		}
	}
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Get Google OAuth provider from live admin-panel config.
	provider, _, err := h.resolveProvider(ctx, models.OAuthProviderGoogle)
	if err != nil {
		logger.Error("Failed to create Google OAuth provider", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Google OAuth not configured", err)
	}

	// Validate ID token and get user info. The audience is the platform-specific
	// client ID registered in Google Console for the mobile app.
	audience := h.oauthResolver.MobileAudience(ctx, models.OAuthProviderGoogle, "android")
	validationRequest := &services.IDTokenValidationRequest{
		IDToken:     req.Body.IDToken,
		AccessToken: req.Body.AccessToken,
		Audience:    audience,
	}

	userInfo, err := provider.ValidateIDToken(ctx, validationRequest)
	if err != nil {
		logger.Warn("ID token validation failed", slog.String("error", err.Error()))
		return nil, huma.Error401Unauthorized("Invalid Google ID token", err)
	}

	// Convert userInfo to map for auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
		"given_name":     userInfo.GivenName,
		"family_name":    userInfo.FamilyName,
	}

	// Store OAuth provider tokens if we have an access token
	var oauthTokens *models.OAuthProviderTokens
	if req.Body.AccessToken != "" {
		oauthTokens = &models.OAuthProviderTokens{
			AccessToken: req.Body.AccessToken,
			TokenType:   "Bearer",
		}
	}

	// Use auth service to handle user creation/update and generate JWT tokens
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(
		ctx,
		models.OAuthProviderGoogle,
		userInfoMap,
		oauthTokens,
		securityCtx,
		deviceInfo,
	)
	if err != nil {
		logger.Error("Failed to process OAuth callback", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to process authentication", err)
	}

	// Prepare response
	response := &MobileGoogleAuthResponse{}
	response.Body.AccessToken = tokenResponse.AccessToken
	response.Body.RefreshToken = tokenResponse.RefreshToken
	response.Body.TokenType = "Bearer"
	response.Body.ExpiresIn = tokenResponse.ExpiresIn
	response.Body.User.ID = tokenResponse.User.ID
	response.Body.User.Email = tokenResponse.User.Email
	response.Body.User.Name = tokenResponse.User.FullName // Use FullName instead of Name
	response.Body.User.Avatar = tokenResponse.User.Avatar
	response.Body.User.EmailVerified = tokenResponse.User.EmailVerified

	return response, nil
}

// MobileAppleAuthRequest represents the request from mobile app with Apple ID token
type MobileAppleAuthRequest struct {
	Body struct {
		IDToken     string `json:"id_token" form:"id_token" doc:"Apple ID token from mobile app"`
		AccessToken string `json:"access_token,omitempty" form:"access_token" doc:"Apple access token from mobile app (optional)"`
	}
}

// MobileAppleAuthResponse represents the response to mobile app with JWT tokens
// Reuses the same structure as Google for consistency
type MobileAppleAuthResponse = MobileGoogleAuthResponse

// HandleMobileAppleAuth handles Apple authentication from mobile apps
func (h *AuthHandler) HandleMobileAppleAuth(ctx context.Context, req *MobileAppleAuthRequest) (*MobileAppleAuthResponse, error) {
	logger := slog.Default()

	if err := h.oauthProviderAllowed(ctx, "apple"); err != nil {
		return nil, err
	}

	// Extract device info from context
	var deviceInfo *models.DeviceInfo
	var ipAddress string = "unknown"
	if di := ctx.Value("deviceInfo"); di != nil {
		if d, ok := di.(*types.DeviceInfo); ok {
			deviceInfo = &models.DeviceInfo{
				DeviceID:    d.DeviceID,
				DeviceType:  d.DeviceType,
				Platform:    d.Platform,
				UserAgent:   d.UserAgent,
				Fingerprint: d.Fingerprint,
			}
			ipAddress = d.IP // Get IP from types.DeviceInfo
		}
	}
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Get Apple OAuth provider from live admin-panel config.
	provider, _, err := h.resolveProvider(ctx, models.OAuthProviderApple)
	if err != nil {
		logger.Error("Failed to create Apple OAuth provider", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Apple OAuth not configured", err)
	}

	// Determine audience based on platform — falls back to the web client ID
	// when the device platform is unknown or the platform-specific ID isn't set.
	var platform string
	if deviceInfo != nil {
		platform = deviceInfo.Platform
	}
	audience := h.oauthResolver.MobileAudience(ctx, models.OAuthProviderApple, platform)

	// Validate ID token and get user info
	validationRequest := &services.IDTokenValidationRequest{
		IDToken:     req.Body.IDToken,
		AccessToken: req.Body.AccessToken,
		Audience:    audience,
	}

	userInfo, err := provider.ValidateIDToken(ctx, validationRequest)
	if err != nil {
		logger.Warn("ID token validation failed", slog.String("error", err.Error()))
		return nil, huma.Error401Unauthorized("Invalid Apple ID token", err)
	}

	// Convert userInfo to map for auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
		"given_name":     userInfo.GivenName,
		"family_name":    userInfo.FamilyName,
	}

	// Store OAuth provider tokens if we have an access token
	var oauthTokens *models.OAuthProviderTokens
	if req.Body.AccessToken != "" {
		oauthTokens = &models.OAuthProviderTokens{
			AccessToken: req.Body.AccessToken,
			TokenType:   "Bearer",
		}
	}

	// Use auth service to handle user creation/update and generate JWT tokens
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(
		ctx,
		models.OAuthProviderApple,
		userInfoMap,
		oauthTokens,
		securityCtx,
		deviceInfo,
	)
	if err != nil {
		logger.Error("Failed to process OAuth callback", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to process authentication", err)
	}

	// Prepare response
	response := &MobileAppleAuthResponse{}
	response.Body.AccessToken = tokenResponse.AccessToken
	response.Body.RefreshToken = tokenResponse.RefreshToken
	response.Body.TokenType = "Bearer"
	response.Body.ExpiresIn = tokenResponse.ExpiresIn
	response.Body.User.ID = tokenResponse.User.ID
	response.Body.User.Email = tokenResponse.User.Email
	response.Body.User.Name = tokenResponse.User.FullName // Use FullName instead of Name
	response.Body.User.Avatar = tokenResponse.User.Avatar
	response.Body.User.EmailVerified = tokenResponse.User.EmailVerified

	return response, nil
}

// RefreshTokensHTTP handles token refresh with cookie support (raw HTTP handler)
func (h *AuthHandler) RefreshTokensHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract device info and IP address from request
	ipAddress := utils.GetClientIP(r)

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Extract refresh token from cookie or request body. Try every cookie
	// candidate so a stale Path=/auth cookie (from a prior deployment) does
	// not mask the current Path=/ cookie — see GetSessionHTTP for the full
	// rationale. Stop at the first candidate that successfully refreshes.
	var tokenSource string
	var tokenResponse *models.TokenResponse
	var lastErr error
	cookieName := h.config.Auth.Cookie.Name
	candidates := utils.GetAllRefreshTokensFromCookies(r, cookieName)
	for _, candidate := range candidates {
		resp, err := h.authService.RefreshTokensWithRiskAssessment(ctx, candidate, securityCtx)
		if err == nil {
			tokenResponse = resp
			tokenSource = "cookie"
			break
		}
		lastErr = err
	}

	if tokenResponse == nil {
		var req RefreshTokenRequest
		if r.Header.Get("Content-Type") == "application/json" {
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.RefreshToken != "" {
				resp, err := h.authService.RefreshTokensWithRiskAssessment(ctx, req.RefreshToken, securityCtx)
				if err == nil {
					tokenResponse = resp
					tokenSource = "request_body"
				} else {
					lastErr = err
				}
			}
		}
	}

	if tokenResponse == nil {
		if lastErr == nil {
			http.Error(w, "No refresh token provided", http.StatusUnauthorized)
			return
		}
		logger.Warn("Token refresh failed",
			slog.String("error", lastErr.Error()),
			slog.Int("candidatesTried", len(candidates)),
		)
		writeRefreshErr(w, lastErr)
		return
	}

	// Set new refresh token as cookie if we got the original from a cookie
	if tokenSource == "cookie" {
		cookieDomain := h.cookieDomain
		isSecure := h.config.Auth.Cookie.Secure
		utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tokenResponse); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// Logout Request
type LogoutRequest struct {
	RefreshToken string `json:"refreshToken,omitempty" doc:"Refresh token to invalidate"`
	AllDevices   bool   `json:"allDevices" doc:"Logout from all devices"`
}

// Logout Response
type LogoutResponse struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

// LogoutHTTP handles user logout with proper cookie clearing (raw HTTP handler)
func (h *AuthHandler) LogoutHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Try to get user UUID from context first (if authenticated via middleware)
	userUUIDVal := ctx.Value("userUUID")
	if userUUIDVal == nil {
		// Fallback to userID for backward compatibility
		userUUIDVal = ctx.Value("userID")
	}

	var userUUID string
	var ok bool

	// If no user context (likely because auth middleware failed), try to extract from refresh token
	if userUUIDVal == nil {
		cookieName := h.config.Auth.Cookie.Name
		refreshToken, err := utils.GetRefreshTokenFromCookieByName(r, cookieName)
		if err != nil || refreshToken == "" {
			// Still clear the cookie even if we can't find it
			cookieDomain := h.cookieDomain
			isSecure := h.config.Auth.Cookie.Secure
			utils.ClearRefreshTokenCookie(w, cookieName, cookieDomain, isSecure)

			// Return success - user is effectively logged out
			w.Header().Set("Content-Type", "application/json")
			response := struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}{
				Success: true,
				Message: "Successfully logged out",
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		// Parse refresh token to get user UUID
		refreshClaims, err := h.jwtService.ParseUnverifiedClaims(refreshToken)
		if err != nil || refreshClaims.UserUUID == "" {
			// Still clear the cookie
			cookieDomain := h.cookieDomain
			isSecure := h.config.Auth.Cookie.Secure
			utils.ClearRefreshTokenCookie(w, cookieName, cookieDomain, isSecure)

			// Return success
			w.Header().Set("Content-Type", "application/json")
			response := struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}{
				Success: true,
				Message: "Successfully logged out",
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		userUUID = refreshClaims.UserUUID
	} else {
		userUUID, ok = userUUIDVal.(string)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Parse request body for logout options
	var req LogoutRequest
	if r.Header.Get("Content-Type") == "application/json" {
		json.NewDecoder(r.Body).Decode(&req) // Ignore errors, use defaults
	}

	// Get refresh token from cookie to terminate specific session
	cookieName := h.config.Auth.Cookie.Name
	refreshToken, _ := utils.GetRefreshTokenFromCookieByName(r, cookieName)

	// Terminate sessions based on request
	if req.AllDevices {
		err := h.authService.TerminateAllSessionsByUUID(ctx, userUUID)
		if err != nil {
			logger.Error("Failed to terminate all sessions", slog.String("error", err.Error()))
			http.Error(w, "Failed to logout", http.StatusInternalServerError)
			return
		}
	} else {
		// Terminate current session based on refresh token or device ID
		if refreshToken != "" {
			// Parse refresh token to get device ID
			refreshClaims, err := h.jwtService.ParseUnverifiedClaims(refreshToken)
			if err == nil && refreshClaims.DeviceID != "" {
				h.authService.TerminateSessionByUUID(ctx, userUUID, refreshClaims.DeviceID)
			}
		} else if req.RefreshToken != "" {
			// Use refresh token from request body if provided
			refreshClaims, err := h.jwtService.ParseUnverifiedClaims(req.RefreshToken)
			if err == nil && refreshClaims.DeviceID != "" {
				h.authService.TerminateSessionByUUID(ctx, userUUID, refreshClaims.DeviceID)
			}
		}
	}

	// Revoke the current access token's sid so the caller cannot keep using
	// it for the remainder of its TTL. Refresh-token invalidation above only
	// prevents new tokens from being minted — without this call, a stolen
	// bearer still works until its exp ticks over. Best-effort: Redis errors
	// log but do not fail the logout response.
	if h.sessionRevocation != nil {
		if sid := currentSessionID(ctx); sid != "" {
			if err := h.sessionRevocation.Revoke(ctx, sid, "logout"); err != nil {
				logger.Warn("logout: failed to revoke session id",
					slog.String("sid", sid),
					slog.String("error", err.Error()))
			}
		}
	}

	// Clear the refresh token cookie
	cookieDomain := h.cookieDomain
	isSecure := h.config.Auth.Cookie.Secure
	utils.ClearRefreshTokenCookie(w, cookieName, cookieDomain, isSecure)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{
		Success: true,
		Message: "Successfully logged out",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// Logout handles user logout (Huma handler - deprecated, use LogoutHTTP instead)
func (h *AuthHandler) Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error) {
	// Get user from context
	userUUID := ctx.Value("userUUID").(string)

	if req.AllDevices {
		err := h.authService.TerminateAllSessionsByUUID(ctx, userUUID)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to logout", err)
		}
	} else if req.RefreshToken != "" {
		// Terminate specific session
		claims, err := h.jwtService.ParseUnverifiedClaims(req.RefreshToken)
		if err == nil && claims.DeviceID != "" {
			err = h.authService.TerminateSessionByUUID(ctx, userUUID, claims.DeviceID)
			if err != nil {
				return nil, huma.Error500InternalServerError("Failed to logout", err)
			}
		}
	}

	if h.sessionRevocation != nil {
		if sid := currentSessionID(ctx); sid != "" {
			_ = h.sessionRevocation.Revoke(ctx, sid, "logout")
		}
	}

	return &LogoutResponse{
		Body: struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{
			Success: true,
			Message: "Successfully logged out",
		},
	}, nil
}

// GetCurrentUser Response
type GetCurrentUserResponse struct {
	Body CurrentUserResponse `json:"body"`
}

// CurrentUserResponse includes user data with OAuth providers
type CurrentUserResponse struct {
	userModels.UserManagementResponse
	OAuthProviders []models.OAuthProviderInfo `json:"oauthProviders,omitempty"`
}

// UpdateCurrentUserInput is the request body for PATCH /v1/auth/{tier}/me.
// Self-service preference surface — strictly allowlisted: only the
// language field is accepted today. Adding a new mutable preference
// (theme, notification opt-ins, …) means adding a field here AND
// honoring it explicitly in UpdateCurrentUser; the underlying
// UpdateUserInput shape is wider but is NOT pass-through.
type UpdateCurrentUserInput struct {
	Body struct {
		// Language is the user's preferred BCP-47 language tag. The
		// allowlist mirrors UpdateUserInput.Language in the SDK iface —
		// extend the enum and validate tags in lockstep when adding a
		// new locale.
		Language string `json:"language,omitempty" enum:"en,it" validate:"omitempty,oneof=en it"`
	}
}

// UpdateCurrentUser writes self-service preferences for the calling
// user. Currently exposes only `language`; the response shape matches
// GET /v1/auth/{tier}/me so the SPA can replace its cached user
// document with the response without an extra round-trip.
func (h *AuthHandler) UpdateCurrentUser(ctx context.Context, in *UpdateCurrentUserInput) (*GetCurrentUserResponse, error) {
	userUUIDValue := ctx.Value("userUUID")
	if userUUIDValue == nil {
		return nil, huma.Error401Unauthorized("Authentication required", nil)
	}
	userUUID, ok := userUUIDValue.(string)
	if !ok {
		return nil, huma.Error401Unauthorized("Invalid authentication context", nil)
	}

	if in.Body.Language != "" {
		if err := h.authService.UpdateLanguageByUUID(ctx, userUUID, in.Body.Language); err != nil {
			return nil, huma.Error500InternalServerError("Failed to update preferences", err)
		}
	}

	return h.GetCurrentUser(ctx, &struct{}{})
}

// GetCurrentUser returns the current authenticated user
func (h *AuthHandler) GetCurrentUser(ctx context.Context, _ *struct{}) (*GetCurrentUserResponse, error) {
	userUUIDValue := ctx.Value("userUUID")
	if userUUIDValue == nil {
		return nil, huma.Error401Unauthorized("Authentication required", nil)
	}

	userUUID, ok := userUUIDValue.(string)
	if !ok {
		return nil, huma.Error401Unauthorized("Invalid authentication context", nil)
	}

	user, err := h.authService.GetUserByUUID(ctx, userUUID)
	if err != nil {
		return nil, huma.Error404NotFound("User not found", err)
	}

	// Fetch OAuth provider information
	oauthProviders, err := h.oauthProviderRepo.GetByUserUUID(ctx, userUUID)
	if err != nil {
		// OAuth providers are optional data - continue without them
		oauthProviders = []*models.OAuthProviderDoc{}
	}

	// Convert OAuth providers to response format
	oauthProvidersInfo := models.ConvertOAuthProvidersToInfo(oauthProviders)

	// Rebuild Avatar via the canonical resolver so /me always returns
	// a fresh presigned GET for uploaded source (1h TTL, Redis-cached
	// to ~50min), the current OAuth provider picture for oauth_*, or
	// "" for initials. Legacy users without an AvatarSource keep
	// whatever the document carries.
	avatarURL := user.Avatar
	if user.AvatarSource != "" {
		avatarURL = blob.ResolveAvatarURL(ctx, user, h.blobStore)
	}

	return &GetCurrentUserResponse{
		Body: CurrentUserResponse{
			UserManagementResponse: userModels.UserManagementResponse{
				ID:            user.UUID,
				Email:         user.Email,
				Username:      user.Username,
				FullName:      user.FullName,
				Avatar:        avatarURL,
				AvatarSource:  user.AvatarSource,
				Role:          user.Role,
				IsActive:      user.IsActive,
				EmailVerified: user.EmailVerified,
				LastLogin:     user.LastLogin,
				CreatedAt:     user.CreatedAt,
				UpdatedAt:     user.UpdatedAt,
				Language:      user.Language,
			},
			OAuthProviders: oauthProvidersInfo,
		},
	}, nil
}

// RegisterOAuthRoutes mounts the OAuth callback endpoints. Only the
// dispatcher AuthHandler (operator-mux instance) calls this — the IdP
// has a single registered redirect URI per provider so there is exactly
// one callback per provider regardless of how many audiences exist. On
// every callback the signed-state JWT carries the audience tier and
// dispatchTarget routes the resulting token issuance to the matching
// authService (ADR-0003 PR-D D-6).
func (h *AuthHandler) RegisterOAuthRoutes(publicAPI huma.API, _ huma.API, router chi.Router, _ chi.Router) {
	// OAuth callbacks — raw HTTP handlers for proper redirects. Hosted
	// once on the operator host mux; the dispatched target's config
	// owns cookie domain + frontend redirect.
	router.Get("/v1/auth/oauth/google/callback", h.HandleGoogleCallbackHTTP)
	router.Get("/v1/auth/oauth/discord/callback", h.HandleDiscordCallbackHTTP)
	router.Post("/v1/auth/oauth/apple/callback", h.HandleAppleCallbackHTTP)

	// Session initialization for web clients after OAuth callback — raw
	// HTTP handler for cookies.
	router.Get("/v1/auth/session", h.GetSessionHTTP)

	// GitHub callback uses Huma for consistency with the existing
	// implementation.
	huma.Register(publicAPI, huma.Operation{
		OperationID: "github-oauth-callback",
		Method:      http.MethodGet,
		Path:        "/v1/auth/oauth/github/callback",
		Summary:     "GitHub OAuth callback",
		Description: "Handle GitHub OAuth callback and exchange code for tokens",
		Tags:        []string{"Authentication"},
	}, h.HandleGitHubCallback)
}

// RegisterOAuthStartRoutes mounts the OAuth start endpoints under the
// given mount prefix. This is the surface that stamps the AuthHandler's
// tier into the signed-state JWT, so per-tier mounts must use a handler
// instance whose tier matches the audience: the operator-bound handler
// for /v1/auth/operator/... and the client-bound handler for
// /v1/auth/client/.... Callbacks are not mounted here — they live on
// the dispatcher AuthHandler exactly once (RegisterOAuthRoutes) so the
// OAuth provider only needs to register a single redirect URI per
// provider regardless of how many audiences exist.
func (h *AuthHandler) RegisterOAuthStartRoutes(publicAPI huma.API, mount RouteMount) {
	huma.Register(publicAPI, huma.Operation{
		OperationID: mount.OpIDPrefix + "list-oauth-providers",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/providers",
		Summary:     "List configured OAuth providers",
		Description: "Returns the set of OAuth providers that currently have a client ID configured in the admin panel.",
		Tags:        []string{"Authentication"},
	}, h.ListOAuthProviders)

	huma.Register(publicAPI, huma.Operation{
		OperationID: mount.OpIDPrefix + "get-auth-policy",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/policy",
		Summary:     "Read the public auth policy for this surface",
		Description: "Public endpoint that exposes the slice of admin-managed auth policy the SPA needs before the user authenticates: whether registration / login are accepted on this surface, and the password minimum length to advertise. Audience is implicit in the path prefix.",
		Tags:        []string{"Authentication"},
	}, h.GetAuthPolicy)

	huma.Register(publicAPI, huma.Operation{
		OperationID: mount.OpIDPrefix + "initiate-oauth-login",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/oauth/login",
		Summary:     "Initiate OAuth login",
		Description: "Start the OAuth authentication flow for a specific provider. The returned state encodes the audience tier so the single shared callback can dispatch the resulting session to the matching authService.",
		Tags:        []string{"Authentication"},
	}, h.InitiateOAuthLogin)

	huma.Register(publicAPI, huma.Operation{
		OperationID: mount.OpIDPrefix + "mobile-google-auth",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/google/mobile",
		Summary:     "Authenticate with Google from mobile app",
		Description: "Validate Google ID token from mobile app and return JWT tokens. Tokens are minted with the audience matching this mount.",
		Tags:        []string{"Authentication", "Mobile"},
	}, h.HandleMobileGoogleAuth)

	huma.Register(publicAPI, huma.Operation{
		OperationID: mount.OpIDPrefix + "mobile-apple-auth",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/apple/mobile",
		Summary:     "Authenticate with Apple from mobile app",
		Description: "Validate Apple ID token from mobile app and return JWT tokens. Tokens are minted with the audience matching this mount.",
		Tags:        []string{"Authentication", "Mobile"},
	}, h.HandleMobileAppleAuth)
}

// RegisterOAuthLinkRoute mounts the authenticated "add a sign-in
// provider" start endpoint at POST /v1/auth{prefix}/me/oauth/link/{provider}.
// Caller wires `RequireGlobal()` (and optionally `RequireStepUp(5m)`)
// around the API instance — link mode adds a credential, so the same
// fresh-MFA gate the unlink path uses applies here.
func (h *AuthHandler) RegisterOAuthLinkRoute(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-oauth-link-init",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/oauth/link/{provider}",
		Summary:     "Initiate OAuth flow to add a sign-in provider to the current account",
		Description: "Returns a redirect URL the SPA should navigate to. The signed-state JWT carries the caller's userUUID so the shared callback can bind the new identity without a fresh login. The callback redirects back to /user/security?tab=oauth&link=success|failed.",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.InitiateOAuthLink)
}

// RegisterTierMountableRoutes mounts the audience-aware session routes
// — refresh, refresh-cookie, logout, me — under the prefix described by
// mount. Each tier (legacy, operator, client) calls this with a distinct
// AuthHandler instance bound to that tier's authService + JWT issuer so
// the issued tokens carry the matching aud claim. The raw HTTP refresh
// + logout handlers are mounted on the supplied chi.Router (the tier's
// host mux); /v1/auth{prefix}/me is mounted on protectedAPI so the
// surrounding RequireGlobal()/auth chain runs.
func (h *AuthHandler) RegisterTierMountableRoutes(publicAPI huma.API, protectedAPI huma.API, router chi.Router, mount RouteMount) {
	_ = publicAPI // reserved for future tier-mountable public huma routes (D-6 may add per-tier OAuth start endpoints).

	router.Post("/v1/auth"+mount.PathPrefix+"/refresh", h.RefreshTokensWithHeaderHTTP)
	router.Post("/v1/auth"+mount.PathPrefix+"/refresh-cookie", h.RefreshTokensHTTP)
	router.Post("/v1/auth"+mount.PathPrefix+"/logout", h.LogoutHTTP)

	huma.Register(protectedAPI, huma.Operation{
		OperationID: mount.OpIDPrefix + "get-current-user",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/me",
		Summary:     "Get current user",
		Description: "Get information about the currently authenticated user",
		Tags:        []string{"Authentication"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, h.GetCurrentUser)

	huma.Register(protectedAPI, huma.Operation{
		OperationID: mount.OpIDPrefix + "update-current-user",
		Method:      http.MethodPatch,
		Path:        "/v1/auth" + mount.PathPrefix + "/me",
		Summary:     "Update current user preferences",
		Description: "Self-service preference update for the calling user. Currently exposes only the `language` field (BCP-47 tag, allowlisted to `en`/`it`); the response mirrors GET /me so the SPA can replace its cached user document with the response.",
		Tags:        []string{"Authentication", "Self-Service"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, h.UpdateCurrentUser)
}

// writeRefreshErr writes a JSON 401 for a refresh-flow error, distinguishing
// replay detection (code:"refresh_token_replay") from generic failures so
// the frontend can show a "you've been signed out for security reasons"
// banner instead of a neutral "please sign in again".
func writeRefreshErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	body := map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "Unauthorized",
		"detail": "Invalid refresh token",
	}
	if errors.Is(err, services.ErrRefreshTokenReplay) {
		body["code"] = "refresh_token_replay"
		body["detail"] = "refresh token reuse detected — session revoked"
	}
	_ = json.NewEncoder(w).Encode(body)
}
