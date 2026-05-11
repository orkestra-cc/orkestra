package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/iface"
)

// WebAuthnHandler binds the WebAuthn ceremony endpoints. It mirrors the
// shape of MFAHandler but for asymmetric-key passkeys instead of TOTP.
// Login completion (post-password) lives on a public endpoint that takes
// a loginChallengeId; the rest are protected and either run inside the
// caller's session (enroll/list/remove) or mint a stepped-up token (verify).
type WebAuthnHandler struct {
	wa           services.WebAuthnService
	mfaChallenges services.MFAChallengeService
	jwt          services.JWTService
	users        iface.UserProvider
	tokens       LoginTokenIssuer
	deviceTrust  services.DeviceTrustService // optional — Section C item #3
	cookieName   string
	cookieDomain string
	cookieSecure bool
}

// NewWebAuthnHandler wires the dependencies. WebAuthnService may be nil
// when the deployment hasn't configured an RP — the route registration
// is gated on that nil check, so the endpoints simply don't mount.
func NewWebAuthnHandler(
	wa services.WebAuthnService,
	mfaChallenges services.MFAChallengeService,
	jwt services.JWTService,
	users iface.UserProvider,
	tokens LoginTokenIssuer,
	cookieName, cookieDomain string,
	cookieSecure bool,
) *WebAuthnHandler {
	if cookieName == "" {
		cookieName = "access_token"
	}
	return &WebAuthnHandler{
		wa:           wa,
		mfaChallenges: mfaChallenges,
		jwt:          jwt,
		users:        users,
		tokens:       tokens,
		cookieName:   cookieName,
		cookieDomain: cookieDomain,
		cookieSecure: cookieSecure,
	}
}

// SetDeviceTrust wires the device-trust service so the login-finish
// endpoint can grant "remember this device" on a passkey-completed
// login. Optional — nil leaves the handler's trust-granting path
// inert. Section C item #3 of the 2026-04-24 auth roadmap.
func (h *WebAuthnHandler) SetDeviceTrust(dt services.DeviceTrustService) {
	h.deviceTrust = dt
}

// --- enroll ---

type webAuthnRegisterBeginResponse struct {
	Body struct {
		ChallengeID string          `json:"challengeId"`
		PublicKey   json.RawMessage `json:"publicKey" doc:"PublicKeyCredentialCreationOptions per W3C WebAuthn"`
	}
}

func (h *WebAuthnHandler) RegisterBegin(ctx context.Context, _ *struct{}) (*webAuthnRegisterBeginResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	user, err := h.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}
	chID, options, err := h.wa.BeginRegistration(ctx, user)
	if err != nil {
		return nil, mapWebAuthnError(err)
	}
	// CredentialCreation marshals to {publicKey: {...}, mediation: ...}
	// already; pass it through as RawMessage so the browser sees the
	// canonical W3C JSON shape without us hand-shaping it.
	raw, err := json.Marshal(options.Response)
	if err != nil {
		return nil, huma.Error500InternalServerError("encode webauthn options failed")
	}
	resp := &webAuthnRegisterBeginResponse{}
	resp.Body.ChallengeID = chID
	resp.Body.PublicKey = raw
	return resp, nil
}

type webAuthnRegisterFinishRequest struct {
	Body struct {
		ChallengeID         string          `json:"challengeId"`
		Name                string          `json:"name" doc:"User-supplied label, e.g. 'Yubikey 5C'"`
		AttestationResponse json.RawMessage `json:"attestationResponse" doc:"Raw PublicKeyCredential JSON returned by navigator.credentials.create()"`
	}
}

type webAuthnRegisterFinishResponse struct {
	Body struct {
		Success    bool   `json:"success"`
		Credential webAuthnCredentialPublic `json:"credential"`
	}
}

func (h *WebAuthnHandler) RegisterFinish(ctx context.Context, req *webAuthnRegisterFinishRequest) (*webAuthnRegisterFinishResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	user, err := h.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}
	if len(req.Body.AttestationResponse) == 0 {
		return nil, huma.Error400BadRequest("attestationResponse is required")
	}
	cred, err := h.wa.FinishRegistration(ctx, user, req.Body.ChallengeID, req.Body.Name, req.Body.AttestationResponse)
	if err != nil {
		return nil, mapWebAuthnError(err)
	}
	resp := &webAuthnRegisterFinishResponse{}
	resp.Body.Success = true
	resp.Body.Credential = toPublicCredential(*cred)
	return resp, nil
}

// --- list / remove ---

type webAuthnCredentialPublic struct {
	CredentialID string     `json:"credentialId" doc:"base64url"`
	Name         string     `json:"name"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	Transports   []string   `json:"transports,omitempty"`
	BackupState  bool       `json:"backupState,omitempty"`
	CloneWarning bool       `json:"cloneWarning,omitempty"`
}

type webAuthnListResponse struct {
	Body struct {
		Credentials []webAuthnCredentialPublic `json:"credentials"`
	}
}

func (h *WebAuthnHandler) List(ctx context.Context, _ *struct{}) (*webAuthnListResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	creds, err := h.wa.ListCredentials(ctx, userUUID)
	if err != nil {
		return nil, mapWebAuthnError(err)
	}
	resp := &webAuthnListResponse{}
	resp.Body.Credentials = make([]webAuthnCredentialPublic, 0, len(creds))
	for _, c := range creds {
		resp.Body.Credentials = append(resp.Body.Credentials, toPublicCredential(c))
	}
	return resp, nil
}

type webAuthnRemoveRequest struct {
	CredentialID string `path:"credentialId" doc:"base64url-encoded credential ID"`
}

type webAuthnRemoveResponse struct {
	Body struct {
		Success bool `json:"success"`
	}
}

func (h *WebAuthnHandler) Remove(ctx context.Context, req *webAuthnRemoveRequest) (*webAuthnRemoveResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	id, err := decodeCredentialID(req.CredentialID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid credentialId")
	}
	removed, err := h.wa.RemoveCredential(ctx, userUUID, id)
	if err != nil {
		return nil, huma.Error500InternalServerError("remove credential failed")
	}
	if !removed {
		return nil, huma.Error404NotFound("credential not found")
	}
	resp := &webAuthnRemoveResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- step-up verify (caller already authenticated) ---

type webAuthnVerifyBeginResponse struct {
	Body struct {
		ChallengeID string          `json:"challengeId"`
		PublicKey   json.RawMessage `json:"publicKey" doc:"PublicKeyCredentialRequestOptions per W3C WebAuthn"`
	}
}

func (h *WebAuthnHandler) VerifyBegin(ctx context.Context, _ *struct{}) (*webAuthnVerifyBeginResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	user, err := h.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}
	chID, options, err := h.wa.BeginAssertion(ctx, user, services.MFAPurposeWebAuthnVerify)
	if err != nil {
		return nil, mapWebAuthnError(err)
	}
	raw, err := json.Marshal(options.Response)
	if err != nil {
		return nil, huma.Error500InternalServerError("encode webauthn options failed")
	}
	resp := &webAuthnVerifyBeginResponse{}
	resp.Body.ChallengeID = chID
	resp.Body.PublicKey = raw
	return resp, nil
}

type webAuthnVerifyFinishRequest struct {
	Body struct {
		ChallengeID       string          `json:"challengeId"`
		AssertionResponse json.RawMessage `json:"assertionResponse" doc:"Raw PublicKeyCredential JSON returned by navigator.credentials.get()"`
	}
}

type webAuthnVerifyFinishResponse struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success     bool   `json:"success"`
		AccessToken string `json:"accessToken"`
		TokenType   string `json:"tokenType"`
		ExpiresIn   int64  `json:"expiresIn"`
	}
}

// VerifyFinish validates the assertion and mints a stepped-up access
// token. amr gets "otp" appended (the step-up middleware accepts either
// "otp" or "webauthn") and last_otp_at is set to now, so the next 5min
// of requests pass RequireStepUp without re-prompting.
func (h *WebAuthnHandler) VerifyFinish(ctx context.Context, req *webAuthnVerifyFinishRequest) (*webAuthnVerifyFinishResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	user, err := h.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}
	if len(req.Body.AssertionResponse) == 0 {
		return nil, huma.Error400BadRequest("assertionResponse is required")
	}
	if err := h.wa.FinishAssertion(ctx, user, req.Body.ChallengeID, services.MFAPurposeWebAuthnVerify, req.Body.AssertionResponse); err != nil {
		return nil, mapWebAuthnError(err)
	}

	amr := appendWebAuthn(priorAMRWithOTP(ctx))
	token, err := h.jwt.GenerateAccessTokenWithAMR(user, amr, time.Now().Unix())
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to mint stepped-up token")
	}
	resp := &webAuthnVerifyFinishResponse{}
	resp.Body.Success = true
	resp.Body.AccessToken = token
	resp.Body.TokenType = "Bearer"
	resp.Body.ExpiresIn = 15 * 60
	return resp, nil
}

// --- public login completion (paired with /v1/auth/login partial response) ---

type webAuthnLoginBeginRequest struct {
	Body struct {
		LoginChallengeID string `json:"loginChallengeId" doc:"mfaToken returned by /v1/auth/login"`
	}
}

type webAuthnLoginBeginResponse struct {
	Body struct {
		ChallengeID string          `json:"challengeId"`
		PublicKey   json.RawMessage `json:"publicKey"`
	}
}

func (h *WebAuthnHandler) LoginBegin(ctx context.Context, req *webAuthnLoginBeginRequest) (*webAuthnLoginBeginResponse, error) {
	if req.Body.LoginChallengeID == "" {
		return nil, huma.Error400BadRequest("loginChallengeId is required")
	}
	loginCh, err := h.mfaChallenges.Peek(ctx, req.Body.LoginChallengeID)
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid or expired login challenge")
	}
	if loginCh.Purpose != services.MFAPurposeLogin {
		return nil, huma.Error400BadRequest("challenge purpose mismatch")
	}
	user, err := h.users.GetUserByID(ctx, loginCh.UserUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}
	chID, options, err := h.wa.BeginAssertion(ctx, user, services.MFAPurposeWebAuthnLogin)
	if err != nil {
		return nil, mapWebAuthnError(err)
	}
	raw, err := json.Marshal(options.Response)
	if err != nil {
		return nil, huma.Error500InternalServerError("encode webauthn options failed")
	}
	resp := &webAuthnLoginBeginResponse{}
	resp.Body.ChallengeID = chID
	resp.Body.PublicKey = raw
	return resp, nil
}

type webAuthnLoginFinishRequest struct {
	Body struct {
		LoginChallengeID    string          `json:"loginChallengeId"`
		WebAuthnChallengeID string          `json:"webauthnChallengeId"`
		AssertionResponse   json.RawMessage `json:"assertionResponse"`
		TrustDevice         bool            `json:"trustDevice,omitempty" doc:"When true, grant this device a 30-day trust so subsequent logins can skip the MFA prompt"`
	}
}

type webAuthnLoginFinishResponse struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success      bool                               `json:"success"`
		AccessToken  string                             `json:"accessToken"`
		RefreshToken string                             `json:"refreshToken,omitempty"`
		TokenType    string                             `json:"tokenType"`
		ExpiresIn    int64                              `json:"expiresIn"`
		SessionID    string                             `json:"sessionId"`
		DeviceID     string                             `json:"deviceId,omitempty"`
		User         interface{}                        `json:"user,omitempty"`
	}
}

// LoginFinish validates the assertion against the WebAuthn challenge,
// then consumes the original login challenge and mints a full token
// pair — same shape as /v1/auth/mfa/login/verify but with the source
// AMR augmented by "otp" + "webauthn".
func (h *WebAuthnHandler) LoginFinish(ctx context.Context, req *webAuthnLoginFinishRequest) (*webAuthnLoginFinishResponse, error) {
	if req.Body.LoginChallengeID == "" || req.Body.WebAuthnChallengeID == "" {
		return nil, huma.Error400BadRequest("loginChallengeId and webauthnChallengeId are required")
	}
	if len(req.Body.AssertionResponse) == 0 {
		return nil, huma.Error400BadRequest("assertionResponse is required")
	}

	loginCh, err := h.mfaChallenges.Peek(ctx, req.Body.LoginChallengeID)
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid or expired login challenge")
	}
	if loginCh.Purpose != services.MFAPurposeLogin {
		return nil, huma.Error400BadRequest("challenge purpose mismatch")
	}

	user, err := h.users.GetUserByID(ctx, loginCh.UserUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}

	if err := h.wa.FinishAssertion(ctx, user, req.Body.WebAuthnChallengeID, services.MFAPurposeWebAuthnLogin, req.Body.AssertionResponse); err != nil {
		_, _ = h.mfaChallenges.IncrementAttempts(ctx, req.Body.LoginChallengeID)
		return nil, mapWebAuthnError(err)
	}

	// Both ceremonies passed — consume the login challenge so it can't be
	// reused with another factor.
	_, _ = h.mfaChallenges.Consume(ctx, req.Body.LoginChallengeID)

	amr := appendWebAuthn(appendOTP(loginCh.SourceAMR))
	tokens, err := h.tokens.IssueLoginTokens(ctx, user, loginCh.DeviceID, loginCh.Platform, loginCh.IPAddress, amr, time.Now().Unix())
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to mint login tokens")
	}

	// Section C item #3: if the user opted into "remember this device",
	// persist a 30-day trust grant tagged as webauthn-issued so the
	// next login can skip the passkey prompt. Best-effort — a grant
	// failure must not turn a successful login into an error.
	if req.Body.TrustDevice && h.deviceTrust != nil && loginCh.DeviceID != "" {
		_ = h.deviceTrust.MarkTrusted(ctx, services.MarkTrustedInput{
			UserUUID:    loginCh.UserUUID,
			DeviceID:    loginCh.DeviceID,
			Fingerprint: loginCh.Fingerprint,
			Platform:    loginCh.Platform,
			IPAddress:   loginCh.IPAddress,
			GrantedAMR:  "webauthn",
		})
	}

	resp := &webAuthnLoginFinishResponse{}
	resp.SetCookie = buildRefreshCookie(h.cookieName, tokens.RefreshToken, h.cookieDomain, h.cookieSecure)
	resp.Body.Success = true
	resp.Body.AccessToken = tokens.AccessToken
	resp.Body.TokenType = tokens.TokenType
	resp.Body.ExpiresIn = tokens.ExpiresIn
	resp.Body.SessionID = tokens.SessionID
	resp.Body.DeviceID = tokens.DeviceID
	resp.Body.User = tokens.User
	return resp, nil
}

// --- helpers ---

func toPublicCredential(c authModels.WebAuthnCredential) webAuthnCredentialPublic {
	return webAuthnCredentialPublic{
		CredentialID: base64.RawURLEncoding.EncodeToString(c.CredentialID),
		Name:         c.Name,
		CreatedAt:    c.CreatedAt,
		LastUsedAt:   c.LastUsedAt,
		Transports:   c.Transports,
		BackupState:  c.BackupState,
		CloneWarning: c.CloneWarning,
	}
}

// decodeCredentialID accepts both raw URL-encoded base64 (no padding,
// the canonical W3C wire format) and standard base64 with padding so
// older clients don't break if they send the wrong variant.
func decodeCredentialID(s string) ([]byte, error) {
	if id, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return id, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// appendWebAuthn adds "webauthn" to amr if not already present. Used
// alongside appendOTP so step-up tokens minted via passkey carry both
// markers — "otp" satisfies the existing middleware check, "webauthn"
// gives the audit trail enough fidelity to distinguish the factor.
func appendWebAuthn(source []string) []string {
	for _, v := range source {
		if v == "webauthn" {
			return source
		}
	}
	return append(source, "webauthn")
}

// mapWebAuthnError translates service-layer errors to HTTP status codes.
// Keep the wire format identical to the TOTP handler's mapMFAError so
// frontend error handling stays uniform.
func mapWebAuthnError(err error) error {
	switch {
	case errors.Is(err, services.ErrMFAInvalidCode):
		return huma.Error401Unauthorized("invalid webauthn challenge")
	case errors.Is(err, services.ErrMFAChallengeMismatch):
		return huma.Error400BadRequest("challenge does not match requested action")
	case errors.Is(err, services.ErrWebAuthnNoCredentials):
		return huma.Error400BadRequest("no webauthn credentials enrolled for this user")
	case errors.Is(err, services.ErrWebAuthnAssertion):
		return huma.Error401Unauthorized("webauthn assertion failed")
	default:
		return huma.Error400BadRequest("webauthn request failed")
	}
}

// --- registration ---

func (h *WebAuthnHandler) RegisterPublicRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-login-begin",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/webauthn/login/begin",
		Summary:     "Begin a passkey assertion to complete a paused login",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
	}, h.LoginBegin)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-login-finish",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/webauthn/login/finish",
		Summary:     "Finish a passkey assertion to complete a paused login",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
	}, h.LoginFinish)
}

func (h *WebAuthnHandler) RegisterProtectedRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-register-begin",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/webauthn/register/begin",
		Summary:     "Begin enrolling a new passkey",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.RegisterBegin)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-register-finish",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/webauthn/register/finish",
		Summary:     "Finish enrolling a new passkey",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.RegisterFinish)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-list",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/mfa/webauthn/credentials",
		Summary:     "List the current user's enrolled passkeys",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-verify-begin",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/webauthn/verify/begin",
		Summary:     "Begin a step-up assertion using a passkey",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.VerifyBegin)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-verify-finish",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/webauthn/verify/finish",
		Summary:     "Finish a step-up assertion; mints a stepped-up access token",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.VerifyFinish)
}

// RegisterStepUpRoutes mounts the credential-removal endpoint, which
// requires a fresh step-up — pulling a passkey is irreversible from the
// user's perspective (the authenticator hardware can only re-enroll).
func (h *WebAuthnHandler) RegisterStepUpRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-webauthn-remove",
		Method:      http.MethodDelete,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/mfa/webauthn/credentials/{credentialId}",
		Summary:     "Delete a passkey (requires fresh step-up)",
		Tags:        []string{"Authentication", "MFA", "WebAuthn"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.Remove)
}
