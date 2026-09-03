package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/utils"
)

// oauthCallbackParams are the IdP-supplied parameters every provider
// callback reads: GET query for Google/Discord/GitHub, form-post for Apple.
type oauthCallbackParams struct {
	State    string
	Code     string
	IdPError string
}

func queryCallbackParams(r *http.Request) oauthCallbackParams {
	q := r.URL.Query()
	return oauthCallbackParams{State: q.Get("state"), Code: q.Get("code"), IdPError: q.Get("error")}
}

func formCallbackParams(r *http.Request) (oauthCallbackParams, error) {
	if err := r.ParseForm(); err != nil {
		return oauthCallbackParams{}, err
	}
	return oauthCallbackParams{State: r.FormValue("state"), Code: r.FormValue("code"), IdPError: r.FormValue("error")}, nil
}

// oauthExchange turns an authorization code into the provider's userinfo
// map plus the tokens to store. Each provider supplies one; everything
// else about a callback is shared by completeOAuthCallback.
type oauthExchange func(ctx context.Context, prov services.OAuthProviderInterface, cfg *services.OAuthProviderConfig, code string) (map[string]interface{}, *models.OAuthProviderTokens, error)

// exchangeWithUserInfo is the code-exchange + userinfo-endpoint path
// Google, Discord and GitHub share. The redirect URI presented at exchange
// is the provider's backend callback from the SAME resolved config the
// usability check answered with.
func exchangeWithUserInfo() oauthExchange {
	return func(ctx context.Context, prov services.OAuthProviderInterface, cfg *services.OAuthProviderConfig, code string) (map[string]interface{}, *models.OAuthProviderTokens, error) {
		tok, err := prov.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{Code: code, RedirectURI: cfg.AdditionalConfig["redirect_url"]})
		if err != nil {
			return nil, nil, err
		}
		info, err := prov.GetUserInfo(ctx, tok.AccessToken)
		if err != nil {
			return nil, nil, err
		}
		return userInfoMap(info), providerTokens(tok), nil
	}
}

// exchangeAppleIDToken is Apple's path: no userinfo endpoint, the identity
// comes from the ID token returned by the exchange.
func exchangeAppleIDToken() oauthExchange {
	return func(ctx context.Context, prov services.OAuthProviderInterface, cfg *services.OAuthProviderConfig, code string) (map[string]interface{}, *models.OAuthProviderTokens, error) {
		tok, err := prov.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{Code: code, RedirectURI: cfg.AdditionalConfig["redirect_url"]})
		if err != nil {
			return nil, nil, err
		}
		info, err := prov.ValidateIDToken(ctx, &services.IDTokenValidationRequest{IDToken: tok.IDToken, AccessToken: tok.AccessToken, Audience: prov.GetClientID()})
		if err != nil {
			return nil, nil, err
		}
		return userInfoMap(info), providerTokens(tok), nil
	}
}

// userInfoMap is the shape HandleOAuthCallbackWithLinking consumes. The
// email_verified bit is copied as a Go bool — the service refuses anything
// else for an unlinked identity — and it survives the relay's JSON round
// trip as a bool.
func userInfoMap(info *services.UserInfo) map[string]interface{} {
	return map[string]interface{}{
		"email":          info.Email,
		"name":           info.Name,
		"picture":        info.Picture,
		"provider_id":    info.ProviderID,
		"email_verified": info.EmailVerified,
	}
}

func providerTokens(tok *services.TokenResponse) *models.OAuthProviderTokens {
	return &models.OAuthProviderTokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		ExpiresIn:    int(tok.ExpiresIn),
		Scopes:       tok.Scope,
		IDToken:      tok.IDToken,
	}
}

// completeOAuthCallback is the ONE implementation behind every provider's
// web callback, on the operator host. Its order is the spec's
// trust-before-destination rule (§4.10 v4.3):
//
//  1. Prove the state is ours, one-shot (atomic take), for THIS provider,
//     and bound to this browser — or, for a client-tier login, deferrable
//     to the relay on the client API host. Until then there is no trusted
//     tier, hence no SPA to send anyone to: every failure is a terminal
//     generic 400 with no redirect.
//  2. Dispatch to the tier-bound handler; evict the nonce cookie if it
//     lives on this host.
//  3. Only now interpret the IdP's `error`, require the code, resolve the
//     provider strictly from one config read, and exchange. Every failure
//     from here redirects to the tier SPA with an allowlisted coarse code;
//     raw text stays in sanitized log fields.
//  4. Link mode returns through its own builder. An operator/legacy login
//     completes inline (finishOAuthCompletion). A client-tier login is
//     handed to the client API host through a one-shot relay record — the
//     refresh cookie for api.* can only be set there, and the browser
//     binding can only be verified there.
func (h *AuthHandler) completeOAuthCallback(w http.ResponseWriter, r *http.Request, provider models.OAuthProvider, params oauthCallbackParams, exchange oauthExchange) {
	ctx := r.Context()
	logger := slog.Default()

	if params.State == "" {
		logger.Warn("oauth callback rejected", slog.String("provider", string(provider)), slog.String("outcome", "missing_state"))
		writeCallbackRejection(w, "Invalid OAuth state")
		return
	}
	res, err := h.resolveStateForCallback(ctx, params.State, provider)
	if err != nil {
		logger.Warn("oauth callback rejected", slog.String("provider", string(provider)), slog.String("outcome", "invalid_state"))
		writeCallbackRejection(w, "Invalid OAuth state")
		return
	}

	target := h.dispatchTarget(res.claims.Tier)
	linkMode := res.claims.Mode == services.OAuthStateModeLink

	// A deferred flow needs somewhere to go. With no client surface there
	// is no destination to trust: terminal 400, before the IdP code is
	// spent on an exchange nobody can complete. (Like a failed relay store,
	// this is the one shape that cannot clear the start-host cookie; it
	// expires on its own Max-Age.)
	if res.bindingDeferred {
		if _, ok := h.relayCompleteURL("probe"); !ok {
			logger.Warn("oauth callback rejected", slog.String("provider", string(provider)), slog.String("outcome", "relay_unavailable"))
			writeCallbackRejection(w, "Invalid OAuth state")
			return
		}
	} else {
		// The nonce cookie lives on this host: evict it on every terminal
		// outcome from here on. A deferred flow's cookie lives on the
		// client API host and is cleared by the relay endpoint.
		w.Header().Add("Set-Cookie", clearOAuthStateCookie(h.config.Auth.Cookie.Secure))
	}

	// relay hands the flow — success or failure — to the client API host.
	// Every terminal outcome of a valid client-tier state goes through it,
	// so the deferred binding is always verified and the start-host cookie
	// always cleared before the browser reaches the client SPA.
	relay := func(rec *services.OAuthRelayRecord) {
		rec.Tier, rec.Provider, rec.CSRF, rec.Mode, rec.LinkUserUUID = res.claims.Tier, provider, res.claims.CSRF, res.claims.Mode, res.claims.LinkUserUUID
		id, err := h.oauthStateService.StoreOAuthRelay(ctx, rec)
		if err != nil {
			// Terminal on THIS host: nothing was minted and the one-shot
			// state is spent, but the start-host cookie cannot be cleared
			// from here — it expires on its own Max-Age.
			logger.Warn("oauth callback rejected", slog.String("provider", string(provider)), slog.String("outcome", "relay_store_failed"))
			writeCallbackRejection(w, "Invalid OAuth state")
			return
		}
		dest, _ := h.relayCompleteURL(id)
		logger.Info("oauth callback relayed to the client api host",
			slog.String("provider", string(provider)), slog.Bool("failure", rec.FailureCode != ""))
		h.writeRelayRedirect(w, r, dest)
	}

	fail := func(loginCode, linkCode, outcome string) {
		logger.Warn("oauth callback failed",
			slog.String("provider", string(provider)),
			slog.String("tier", res.claims.Tier),
			slog.Bool("link_mode", linkMode),
			slog.Bool("relayed", res.bindingDeferred),
			slog.String("outcome", outcome))
		switch {
		case linkMode:
			target.writeOAuthLinkRedirect(w, r, provider, false, linkCode)
		case res.bindingDeferred:
			relay(&services.OAuthRelayRecord{FailureCode: loginCode})
		default:
			target.writeOAuthLoginRedirect(w, r, oauthLoginFailure(provider, loginCode))
		}
	}

	if params.IdPError != "" {
		logger.Info("oauth callback denied by provider",
			slog.String("provider", string(provider)),
			slog.String("idp_error", sanitizeIdPError(params.IdPError)))
		fail(OAuthCallbackErrAccessDenied, oauthLinkCodeAccessDenied, "idp_denied")
		return
	}
	if params.Code == "" {
		fail(OAuthCallbackErrLoginFailed, oauthLinkCodeInternal, "missing_code")
		return
	}

	cfg, usable, err := h.oauthResolver.OAuthWebProviderUsable(ctx, target.policyAudience(), provider)
	if err != nil {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "config_unavailable")
		return
	}
	if !usable {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "provider_unusable")
		return
	}
	prov, err := h.oauthFactory.CreateProvider(provider, cfg)
	if err != nil {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "provider_construct_failed")
		return
	}
	userInfo, oauthTokens, err := exchange(ctx, prov, cfg, params.Code)
	if err != nil {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "exchange_failed")
		return
	}

	if linkMode {
		h.finishOAuthLinkRedirect(w, r, target, provider, userInfo, oauthTokens, res.claims.LinkUserUUID)
		return
	}

	if res.bindingDeferred {
		relay(&services.OAuthRelayRecord{
			UserInfo:        userInfo,
			Tokens:          oauthTokens,
			SecurityContext: res.info.SecurityContext,
			DeviceInfo:      res.info.DeviceInfo,
		})
		return
	}

	h.finishOAuthCompletion(w, r, target, provider, userInfo, oauthTokens, res.info.SecurityContext, res.info.DeviceInfo)
}

// writeCallbackRejection is the ONLY writer of a terminal 400 in the
// callback/relay flow. The request URL of such a response carries state,
// code or relay, so it gets the same no-store / no-referrer headers as a
// redirect. The body is one neutral sentence whatever the reason.
func writeCallbackRejection(w http.ResponseWriter, detail string) {
	setCallbackRedirectHeaders(w)
	http.Error(w, detail, http.StatusBadRequest)
}

// finishOAuthCompletion is the application half of a login: it runs on the
// host that owns the target tier's cookie — the operator host for
// operator/legacy flows, the client API host (relay endpoint) for client
// flows. Everything it writes is the target tier's: authService, cookie
// name/domain/secure, refresh TTL, SPA URL.
func (h *AuthHandler) finishOAuthCompletion(
	w http.ResponseWriter,
	r *http.Request,
	target *AuthHandler,
	provider models.OAuthProvider,
	userInfo map[string]interface{},
	oauthTokens *models.OAuthProviderTokens,
	securityCtx *models.SecurityContext,
	deviceInfo *models.DeviceInfo,
) {
	ctx := r.Context()
	tokenResponse, err := target.authService.HandleOAuthCallbackWithLinking(ctx, provider, userInfo, oauthTokens, securityCtx, deviceInfo)
	if err != nil {
		code, outcome := oauthLoginErrorCode(err)
		slog.Default().Warn("oauth callback failed",
			slog.String("provider", string(provider)),
			slog.String("tier", target.tier),
			slog.String("outcome", outcome))
		target.writeOAuthLoginRedirect(w, r, oauthLoginFailure(provider, code))
		return
	}
	if tokenResponse.RequiresMFA {
		target.writeOAuthLoginRedirect(w, r, oauthLoginMFA(provider, tokenResponse.MFAToken, tokenResponse.WebAuthnAvailable))
		return
	}
	utils.SetRefreshTokenCookie(w, target.config.Auth.Cookie.Name, tokenResponse.RefreshToken,
		refreshCookieMaxAge(target.jwtService), target.cookieDomain, target.config.Auth.Cookie.Secure)
	target.writeOAuthLoginRedirect(w, r, oauthLoginSuccess(provider))
}

// HandleOAuthRelayCompleteHTTP is the client API host's half of a
// client-tier web login (GET /v1/auth/client/oauth/complete?relay=<id>).
// It runs on the host that set the state cookie at start, so the browser
// binding the operator-host callback had to defer is verified HERE and is
// required. Every refusal is a terminal 400 with no redirect and no token:
// the record was the trust, and it has just been consumed.
func (h *AuthHandler) HandleOAuthRelayCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.Default()
	reject := func(outcome string) {
		logger.Warn("oauth relay rejected", slog.String("tier", h.tier), slog.String("outcome", outcome))
		writeCallbackRejection(w, "Invalid OAuth relay")
	}

	id := r.URL.Query().Get("relay")
	if id == "" {
		reject("missing_relay")
		return
	}
	rec, err := h.oauthStateService.TakeOAuthRelay(ctx, id) // atomic one-shot
	if err != nil {
		reject("relay_missing_or_replayed")
		return
	}
	if rec.Tier != h.tier || rec.Mode == services.OAuthStateModeLink {
		reject("relay_tier_or_mode_mismatch")
		return
	}
	if err := verifyRelayBinding(r, rec.CSRF); err != nil {
		reject("relay_unbound")
		return
	}
	// Bound. The cookie this host set at start is spent; clear it on every
	// outcome below.
	w.Header().Add("Set-Cookie", clearOAuthStateCookie(h.config.Auth.Cookie.Secure))
	if rec.FailureCode != "" {
		logger.Warn("oauth callback failed",
			slog.String("provider", string(rec.Provider)),
			slog.String("tier", h.tier),
			slog.Bool("relayed", true),
			slog.String("outcome", "relayed_failure"))
		h.writeOAuthLoginRedirect(w, r, oauthLoginFailure(rec.Provider, rec.FailureCode))
		return
	}
	h.finishOAuthCompletion(w, r, h, rec.Provider, rec.UserInfo, rec.Tokens, rec.SecurityContext, rec.DeviceInfo)
}

// RegisterOAuthRelayRoute mounts the relay endpoint on the CLIENT host mux.
// Only the client-tier handler registers it, and only when a client
// surface exists; it is not part of the OpenAPI document — the browser is
// redirected to it, no client ever calls it.
func (h *AuthHandler) RegisterOAuthRelayRoute(router chi.Router) {
	router.Get(oauthRelayCompletePath, h.HandleOAuthRelayCompleteHTTP)
}
