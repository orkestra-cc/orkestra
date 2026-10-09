package handlers

// Mobile sign-in (spec §4.10 D35): a server-issued nonce bound to a
// client-held PKCE verifier.
//
//	POST /v1/auth/{tier}/{google,apple}/mobile/begin  {code_challenge} → {nonce}
//	POST /v1/auth/{tier}/{google,apple}/mobile        {id_token, code_verifier} → tokens
//
// The backend mints and holds the nonce against a challenge the app
// commits to before it ever sees the nonce, takes the record atomically
// at completion and checks the verifier in constant time. A token
// exfiltrated on its own is worthless without the verifier; a token
// minted for another app or flow has no record; a replay finds none.
// Every miss is one opaque 401 and a taken record is not restored — a
// failed completion burns the challenge, as the web relay does.

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"regexp"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	authutils "github.com/orkestra/backend/internal/core/auth/utils"
	"github.com/orkestra/backend/internal/shared/types"
)

// MobileAuthBeginRequest commits the app's PKCE challenge.
type MobileAuthBeginRequest struct {
	Body struct {
		CodeChallenge string `json:"code_challenge" doc:"base64url S256 of a verifier the app keeps; 43 characters"`
	}
}

// MobileAuthBeginResponse carries the nonce the app hands to the platform
// sign-in SDK (Apple takes its SHA-256 hex).
type MobileAuthBeginResponse struct {
	Body struct {
		Nonce string `json:"nonce" doc:"Nonce for the platform sign-in SDK. Google: pass as is. Apple: pass the SHA-256 hex of it, as the SDK requires."`
	}
}

// s256Challenge is a base64url-encoded SHA-256: exactly 43 unpadded chars.
var s256Challenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// mobileNonceKey is the store key for a nonce the backend issued: the
// prefix plus the SHA-256 hex of the nonce, which is also the value Sign in
// with Apple places in the ID token.
func mobileNonceKey(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return services.MobileNonceKeyPrefix + hex.EncodeToString(sum[:])
}

// mobileRecordKeyFor derives the store key from the token's nonce claim:
// Google carries the nonce verbatim (hash it); Apple carries the hash the
// SDK was given (use it as is).
func mobileRecordKeyFor(provider models.OAuthProvider, nonceClaim string) string {
	if provider == models.OAuthProviderApple {
		return services.MobileNonceKeyPrefix + nonceClaim
	}
	return mobileNonceKey(nonceClaim)
}

const mobileRefusal = "Invalid ID token or challenge"

func (h *AuthHandler) HandleMobileGoogleBegin(ctx context.Context, req *MobileAuthBeginRequest) (*MobileAuthBeginResponse, error) {
	return h.beginMobileAuth(ctx, models.OAuthProviderGoogle, req)
}

func (h *AuthHandler) HandleMobileAppleBegin(ctx context.Context, req *MobileAuthBeginRequest) (*MobileAuthBeginResponse, error) {
	return h.beginMobileAuth(ctx, models.OAuthProviderApple, req)
}

func (h *AuthHandler) beginMobileAuth(ctx context.Context, provider models.OAuthProvider, req *MobileAuthBeginRequest) (*MobileAuthBeginResponse, error) {
	logger := slog.Default()
	if err := h.oauthProviderAllowed(ctx, string(provider)); err != nil {
		return nil, err
	}
	if !s256Challenge.MatchString(req.Body.CodeChallenge) {
		return nil, huma.Error400BadRequest("code_challenge must be the base64url S256 of the verifier", nil)
	}
	nonce, err := services.GenerateOAuthCSRF()
	if err != nil {
		logger.Error("mobile oauth begin failed", slog.String("provider", string(provider)), slog.String("outcome", "nonce_generation_failed"))
		return nil, huma.Error500InternalServerError("OAuth not available", nil)
	}
	rec := &services.MobileNonceRecord{Provider: provider, Tier: h.tier, CodeChallenge: req.Body.CodeChallenge}
	if err := h.oauthStateService.StoreMobileNonce(ctx, mobileNonceKey(nonce), rec); err != nil {
		logger.Error("mobile oauth begin failed", slog.String("provider", string(provider)), slog.String("outcome", "record_store_failed"))
		return nil, huma.Error500InternalServerError("OAuth not available", nil)
	}
	res := &MobileAuthBeginResponse{}
	res.Body.Nonce = nonce
	return res, nil
}

// HandleMobileGoogleAuth completes Google sign-in from a mobile app.
func (h *AuthHandler) HandleMobileGoogleAuth(ctx context.Context, req *MobileGoogleAuthRequest) (*MobileGoogleAuthResponse, error) {
	return h.completeMobileAuth(ctx, models.OAuthProviderGoogle, req.Body.IDToken, req.Body.CodeVerifier)
}

// HandleMobileAppleAuth completes Apple sign-in from a mobile app.
func (h *AuthHandler) HandleMobileAppleAuth(ctx context.Context, req *MobileAppleAuthRequest) (*MobileAppleAuthResponse, error) {
	return h.completeMobileAuth(ctx, models.OAuthProviderApple, req.Body.IDToken, req.Body.CodeVerifier)
}

// mobileDeviceInfo reads the device info the middleware stashed, with the
// IP it carries; "unknown" when absent.
func mobileDeviceInfo(ctx context.Context) (*models.DeviceInfo, string) {
	if di := ctx.Value("deviceInfo"); di != nil {
		if d, ok := di.(*types.DeviceInfo); ok {
			return &models.DeviceInfo{
				DeviceID:    d.DeviceID,
				DeviceType:  d.DeviceType,
				Platform:    d.Platform,
				UserAgent:   d.UserAgent,
				Fingerprint: d.Fingerprint,
			}, d.IP
		}
	}
	return nil, "unknown"
}

func (h *AuthHandler) completeMobileAuth(ctx context.Context, provider models.OAuthProvider, idToken, codeVerifier string) (*MobileGoogleAuthResponse, error) {
	logger := slog.Default()
	if err := h.oauthProviderAllowed(ctx, string(provider)); err != nil {
		return nil, err
	}
	// Every miss is the same answer: the caller must not learn which
	// check failed. The outcome goes to the log.
	refuse := func(outcome string) error {
		logger.Warn("mobile oauth failed", slog.String("provider", string(provider)), slog.String("outcome", outcome))
		return huma.Error401Unauthorized(mobileRefusal, nil)
	}
	deviceInfo, ipAddress := mobileDeviceInfo(ctx)
	securityCtx := &models.SecurityContext{IPAddress: ipAddress, Timestamp: time.Now()}

	prov, _, err := h.resolveProvider(ctx, provider)
	if err != nil {
		logger.Error("mobile oauth failed", slog.String("provider", string(provider)), slog.String("outcome", "provider_unavailable"))
		return nil, huma.Error500InternalServerError("OAuth not available", nil)
	}
	// The platform comes from the device, for both providers (the Google
	// handler used to hardcode "android").
	platform := ""
	if deviceInfo != nil {
		platform = deviceInfo.Platform
	}
	audience := h.oauthResolver.MobileAudience(ctx, provider, platform)
	// Signature, required exp, issuer, audience (an empty expected
	// audience is an ERROR inside the validator, never a skip).
	userInfo, err := prov.ValidateIDToken(ctx, &services.IDTokenValidationRequest{IDToken: idToken, Audience: audience})
	if err != nil {
		return nil, refuse("invalid_id_token")
	}
	if userInfo.Nonce == "" {
		return nil, refuse("missing_nonce")
	}
	if codeVerifier == "" {
		return nil, refuse("missing_verifier")
	}
	// GETDEL: one completion wins; a failed one has burned the record.
	rec, err := h.oauthStateService.TakeMobileNonce(ctx, mobileRecordKeyFor(provider, userInfo.Nonce))
	if err != nil || rec == nil {
		return nil, refuse("no_record")
	}
	if rec.Provider != provider || rec.Tier != h.tier {
		return nil, refuse("record_mismatch")
	}
	if subtle.ConstantTimeCompare([]byte(authutils.GenerateCodeChallenge(codeVerifier)), []byte(rec.CodeChallenge)) != 1 {
		return nil, refuse("verifier_mismatch")
	}

	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
		"given_name":     userInfo.GivenName,
		"family_name":    userInfo.FamilyName,
	}
	// No provider tokens: the client-supplied access_token used to be
	// stored unverified, and an ID-token sign-in has none to vouch for.
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, provider, userInfoMap, nil, securityCtx, deviceInfo)
	if err != nil {
		logOAuthAuthenticationFailure(provider, oauthErrorResponseFor(err).outcome)
		return nil, mapOAuthError(err)
	}
	response := &MobileGoogleAuthResponse{}
	response.Body.AccessToken = tokenResponse.AccessToken
	response.Body.RefreshToken = tokenResponse.RefreshToken
	response.Body.TokenType = "Bearer"
	response.Body.ExpiresIn = tokenResponse.ExpiresIn
	response.Body.User.ID = tokenResponse.User.ID
	response.Body.User.Email = tokenResponse.User.Email
	response.Body.User.Name = tokenResponse.User.FullName
	response.Body.User.Avatar = tokenResponse.User.Avatar
	response.Body.User.EmailVerified = tokenResponse.User.EmailVerified
	return response, nil
}
