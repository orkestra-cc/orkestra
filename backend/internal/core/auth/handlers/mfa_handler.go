package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// LoginTokenIssuer is the subset of PasswordAuthService the MFA login verify
// endpoint needs to mint and persist a full token pair. Kept as a local
// interface so the MFA handler doesn't import the whole password service.
type LoginTokenIssuer interface {
	IssueLoginTokens(ctx context.Context, user *userModels.User, deviceID, platform, ip string, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error)
}

// MFAHandler binds the MFA service to its HTTP surface. All endpoints live
// under /v1/auth/mfa or /v1/auth/me/mfa and require an authenticated user
// (no org context needed, so RequireGlobal() is the correct gate).
type MFAHandler struct {
	mfa          services.MFAService
	challenges   services.MFAChallengeService
	jwt          services.JWTService
	users        iface.UserProvider
	tokens       LoginTokenIssuer
	webauthn     services.WebAuthnService    // optional — populated when WebAuthn is configured
	deviceTrust  services.DeviceTrustService // optional — Section C item #3
	policy       *services.AuthPolicyService // optional — admin-managed mfaEnabled + grace-window source
	cookieName   string
	cookieDomain string
	cookieSecure bool
}

// NewMFAHandler wires the dependencies. Cookie config is needed by the
// login-verify endpoint which issues a refreshed token pair.
func NewMFAHandler(
	mfa services.MFAService,
	challenges services.MFAChallengeService,
	jwt services.JWTService,
	users iface.UserProvider,
	tokens LoginTokenIssuer,
	cookieName, cookieDomain string,
	cookieSecure bool,
) *MFAHandler {
	if cookieName == "" {
		cookieName = "access_token"
	}
	return &MFAHandler{
		mfa:          mfa,
		challenges:   challenges,
		jwt:          jwt,
		users:        users,
		tokens:       tokens,
		cookieName:   cookieName,
		cookieDomain: cookieDomain,
		cookieSecure: cookieSecure,
	}
}

// SetWebAuthn lets the wiring layer attach the WebAuthn service after
// construction so MFAStatus can report passkey count alongside TOTP
// state. Optional — nil keeps the legacy TOTP-only response shape.
func (h *MFAHandler) SetWebAuthn(wa services.WebAuthnService) {
	h.webauthn = wa
}

// SetDeviceTrust wires the "remember this device" service so the
// login-verify endpoint can honor a trustDevice=true request body.
// Optional — nil leaves the handler's trust-granting path inert.
func (h *MFAHandler) SetDeviceTrust(dt services.DeviceTrustService) {
	h.deviceTrust = dt
}

// SetPolicy wires the admin-managed AuthPolicyService so the Status
// endpoint reports the configured grace deadline (instead of the
// hardcoded 7-day fallback) and honours the master mfaEnabled flag.
// Optional — nil falls back to legacy hardcoded values.
func (h *MFAHandler) SetPolicy(p *services.AuthPolicyService) {
	h.policy = p
}

// --- Enrollment ---

type MFAEnrollBeginResponse struct {
	Body struct {
		ChallengeID     string `json:"challengeId"`
		Secret          string `json:"secret"`
		ProvisioningURI string `json:"provisioningUri"`
	}
}

func (h *MFAHandler) EnrollBegin(ctx context.Context, _ *struct{}) (*MFAEnrollBeginResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	user, err := h.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}
	begin, err := h.mfa.BeginEnrollment(ctx, user)
	if err != nil {
		return nil, mapMFAError(err)
	}
	resp := &MFAEnrollBeginResponse{}
	resp.Body.ChallengeID = begin.ChallengeID
	resp.Body.Secret = begin.SecretBase32
	resp.Body.ProvisioningURI = begin.ProvisioningURI
	return resp, nil
}

type MFAEnrollConfirmRequest struct {
	Body struct {
		ChallengeID string `json:"challengeId" doc:"Challenge ID returned by /enroll/begin"`
		Code        string `json:"code" doc:"6-digit TOTP code from the authenticator app"`
	}
}

type MFAEnrollConfirmResponse struct {
	Body struct {
		Success     bool     `json:"success"`
		BackupCodes []string `json:"backupCodes"`
	}
}

func (h *MFAHandler) EnrollConfirm(ctx context.Context, req *MFAEnrollConfirmRequest) (*MFAEnrollConfirmResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	codes, err := h.mfa.ConfirmEnrollment(ctx, userUUID, req.Body.ChallengeID, req.Body.Code)
	if err != nil {
		return nil, mapMFAError(err)
	}
	resp := &MFAEnrollConfirmResponse{}
	resp.Body.Success = true
	resp.Body.BackupCodes = codes
	return resp, nil
}

// --- Status ---

type MFAStatusResponse struct {
	Body struct {
		Status               string `json:"status"`
		Type                 string `json:"type,omitempty"`
		BackupCodesRemaining int    `json:"backupCodesRemaining"`
		// RequiresMFA is true when the caller's role (system or org-scoped)
		// obligates enrollment. False means the banner/countdown should be
		// hidden regardless of enrollment status.
		RequiresMFA bool `json:"requiresMfa"`
		// GraceExpiresAt is the deadline by which a user whose role requires
		// MFA must enroll. Present only when the grace clock has started —
		// absent before the first privileged login. Populated from the user
		// record's MFAGraceStartedAt so it survives page reloads (unlike the
		// one-shot field in the login response).
		GraceExpiresAt *time.Time `json:"graceExpiresAt,omitempty"`
		// WebAuthnCredentials is the count of enrolled passkeys; the
		// settings UI uses this to decide whether to render the passkeys
		// card and to compose the per-credential management list (the
		// per-credential metadata lives at /v1/auth/me/mfa/webauthn/credentials).
		WebAuthnCredentials int `json:"webauthnCredentials"`
	}
}

func (h *MFAHandler) Status(ctx context.Context, _ *struct{}) (*MFAStatusResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	snap, err := h.mfa.Status(ctx, userUUID)
	if err != nil {
		return nil, mapMFAError(err)
	}
	resp := &MFAStatusResponse{}
	resp.Body.Status = string(snap.Status)
	resp.Body.Type = string(snap.Type)
	resp.Body.BackupCodesRemaining = snap.BackupCodesRemaining

	// Role-based MFA requirement + grace deadline. Best-effort: the user
	// lookup and policy check can each fail independently (bad claims,
	// deleted user). Absent fields default to "not required / no deadline",
	// which is the correct fallback — don't pester users with a banner
	// when the backend can't confirm they actually need MFA.
	user, err := h.users.GetUserByID(ctx, userUUID)
	if err == nil && user != nil {
		var memberships []authModels.OrgMembership
		if claims, ok := ctx.Value("claims").(*authModels.JWTClaims); ok && claims != nil {
			memberships = claims.Memberships
		}
		if h.policy.MFARequired(user, memberships) {
			resp.Body.RequiresMFA = true
			if deadline := h.policy.MFAGraceExpiresAt(ctx, user); !deadline.IsZero() {
				resp.Body.GraceExpiresAt = &deadline
			}
		}
	}

	// Best-effort WebAuthn credential count. Same defensive pattern as the
	// role check above — a service-layer failure must not blank the TOTP
	// status the user has already.
	if h.webauthn != nil {
		if creds, err := h.webauthn.ListCredentials(ctx, userUUID); err == nil {
			resp.Body.WebAuthnCredentials = len(creds)
			// Promote status to "enrolled" if a passkey is present even when
			// no TOTP factor exists — avoids the banner showing "not_required"
			// for a user who has only registered passkeys.
			if len(creds) > 0 && resp.Body.Status == string(authModels.MFAStatusNotRequired) {
				resp.Body.Status = string(authModels.MFAStatusEnrolled)
			}
		}
	}
	return resp, nil
}

// --- Remove ---

type MFARemoveRequest struct {
	Body struct{}
}

type MFARemoveResponse struct {
	Body struct {
		Success bool `json:"success"`
	}
}

func (h *MFAHandler) Remove(ctx context.Context, req *MFARemoveRequest) (*MFARemoveResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	// Freshness of the step-up is enforced by RequireStepUp middleware;
	// the handler only performs the removal.
	if err := h.mfa.RemoveFactor(ctx, userUUID, userUUID); err != nil {
		return nil, mapMFAError(err)
	}
	resp := &MFARemoveResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- Regenerate backup codes ---

type MFARegenerateBackupCodesRequest struct {
	Body struct{}
}

type MFARegenerateBackupCodesResponse struct {
	Body struct {
		Codes []string `json:"codes"`
	}
}

// RegenerateBackupCodes destroys the user's existing backup codes
// and returns a freshly generated set exactly once. The route is
// gated by RequireStepUp(5m) — the action is irreversible and any
// captured plaintext code is revoked the moment the new set lands.
// Returns 400 mfa_not_enrolled when the user has no TOTP factor.
func (h *MFAHandler) RegenerateBackupCodes(ctx context.Context, req *MFARegenerateBackupCodesRequest) (*MFARegenerateBackupCodesResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	codes, err := h.mfa.RegenerateBackupCodes(ctx, userUUID)
	if err != nil {
		return nil, mapMFAError(err)
	}
	resp := &MFARegenerateBackupCodesResponse{}
	resp.Body.Codes = codes
	return resp, nil
}

// --- Verify (self-service step-up) ---

type MFAVerifyRequest struct {
	Body struct {
		ChallengeID string `json:"challengeId,omitempty" doc:"Optional — reserved for Block B login flow"`
		Code        string `json:"code" doc:"6-digit TOTP code or a backup code"`
		UseBackup   bool   `json:"useBackup,omitempty" doc:"Set true to consume a backup code instead of TOTP"`
	}
}

type MFAVerifyResponse struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success     bool   `json:"success"`
		AccessToken string `json:"accessToken"`
		TokenType   string `json:"tokenType"`
		ExpiresIn   int64  `json:"expiresIn"`
	}
}

// Verify mints a new access token annotated with amr:["pwd","otp"] (or
// ["oauth","otp"]) and last_otp_at=now. Block A only supports the
// self-service path where the caller already has a valid "pwd" or "oauth"
// token; the Block B login path will supply a challengeId tied to a
// partially-authenticated session.
func (h *MFAHandler) Verify(ctx context.Context, req *MFAVerifyRequest) (*MFAVerifyResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}

	if req.Body.UseBackup {
		if err := h.mfa.VerifyBackupCode(ctx, userUUID, req.Body.Code); err != nil {
			return nil, mapMFAError(err)
		}
	} else {
		if err := h.mfa.Verify(ctx, userUUID, req.Body.Code); err != nil {
			return nil, mapMFAError(err)
		}
	}

	user, err := h.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}

	amr := priorAMRWithOTP(ctx)
	lastOTPAt := nowUnix()
	token, err := h.jwt.GenerateAccessTokenWithAMR(user, amr, lastOTPAt)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to mint stepped-up token")
	}

	resp := &MFAVerifyResponse{}
	resp.Body.Success = true
	resp.Body.AccessToken = token
	resp.Body.TokenType = "Bearer"
	resp.Body.ExpiresIn = 15 * 60 // mirrors jwtService.accessExpiry
	return resp, nil
}

// priorAMRWithOTP returns the caller's existing amr plus "otp", deduplicated.
// When the prior token has no amr (dev tokens, tokens minted before Block A)
// we default to ["pwd","otp"] so the resulting token still looks coherent.
// The "claims" context key is populated by AuthMiddleware.setUserContext.
func priorAMRWithOTP(ctx context.Context) []string {
	var prior []string
	if claims, ok := ctx.Value("claims").(*authModels.JWTClaims); ok && claims != nil {
		prior = claims.AMR
	}
	if len(prior) == 0 {
		prior = []string{"pwd"}
	}
	for _, v := range prior {
		if v == "otp" {
			return prior
		}
	}
	return append(prior, "otp")
}

func nowUnix() int64 {
	return time.Now().Unix()
}

// --- admin reset (another user's factor) ---

type MFAAdminResetRequest struct {
	UserID string `path:"userId" doc:"UUID of the user whose factor should be deleted"`
}

type MFAAdminResetResponse struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// AdminReset removes another user's MFA factor and starts a fresh grace
// window so they must re-enroll within the policy deadline. Consumes the
// system.users.mfa_reset permission declared by the auth module and is
// itself gated by RequireMFA — an admin can't reset another user's MFA
// without having completed their own second factor first.
func (h *MFAHandler) AdminReset(ctx context.Context, req *MFAAdminResetRequest) (*MFAAdminResetResponse, error) {
	actorUUID, _ := ctx.Value("userUUID").(string)
	if actorUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if req.UserID == "" {
		return nil, huma.Error400BadRequest("userId is required")
	}
	// Reset flow: delete the factor, then restart the grace clock so the
	// target has a bounded window to re-enroll. Ordering matters — if the
	// delete fails, we don't want a half-applied state.
	if err := h.mfa.RemoveFactor(ctx, req.UserID, actorUUID); err != nil {
		if errors.Is(err, services.ErrMFANotEnrolled) {
			return nil, huma.Error404NotFound("target user has no MFA factor to reset")
		}
		return nil, huma.Error500InternalServerError("failed to reset MFA factor")
	}
	if err := h.users.ResetMFAGrace(ctx, req.UserID); err != nil {
		// Grace stamp is best-effort — the factor is already gone, so the
		// target will be gated by their next privileged login regardless.
		// We log via the mfa service on the delete path; nothing more here.
		_ = err
	}
	resp := &MFAAdminResetResponse{}
	resp.Body.Success = true
	return resp, nil
}

// --- public login-verify (completes password/OAuth login after MFA) ---

type MFALoginVerifyRequest struct {
	Body struct {
		ChallengeID string `json:"challengeId" doc:"Challenge ID returned by /v1/auth/login or an OAuth flow"`
		Code        string `json:"code" doc:"6-digit TOTP code or backup code"`
		UseBackup   bool   `json:"useBackup,omitempty" doc:"Set true to consume a backup code instead of TOTP"`
		TrustDevice bool   `json:"trustDevice,omitempty" doc:"When true, grant this device a 30-day trust so subsequent logins can skip the MFA prompt"`
	}
}

type MFALoginVerifyResponse struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success      bool        `json:"success"`
		AccessToken  string      `json:"accessToken"`
		RefreshToken string      `json:"refreshToken,omitempty"`
		TokenType    string      `json:"tokenType"`
		ExpiresIn    int64       `json:"expiresIn"`
		SessionID    string      `json:"sessionId"`
		DeviceID     string      `json:"deviceId,omitempty"`
		User         interface{} `json:"user,omitempty"`
	}
}

// LoginVerify is the public companion to POST /v1/auth/login. It accepts the
// challengeId the login endpoint returned when the user had an enrolled MFA
// factor, validates a TOTP or backup code, then mints a full token pair
// with amr = (sourceAMR ∪ {"otp"}) and last_otp_at = now.
func (h *MFAHandler) LoginVerify(ctx context.Context, req *MFALoginVerifyRequest) (*MFALoginVerifyResponse, error) {
	if req.Body.ChallengeID == "" || req.Body.Code == "" {
		return nil, huma.Error400BadRequest("challengeId and code are required")
	}

	// Peek first — we don't want to destroy a valid challenge on a typo
	// and we still need its payload if verification succeeds.
	ch, err := h.challenges.Peek(ctx, req.Body.ChallengeID)
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid or expired challenge")
	}
	if ch.Purpose != services.MFAPurposeLogin {
		return nil, huma.Error400BadRequest("challenge purpose mismatch")
	}

	if req.Body.UseBackup {
		if err := h.mfa.VerifyBackupCode(ctx, ch.UserUUID, req.Body.Code); err != nil {
			_, _ = h.challenges.IncrementAttempts(ctx, req.Body.ChallengeID)
			return nil, mapMFAError(err)
		}
	} else {
		if err := h.mfa.Verify(ctx, ch.UserUUID, req.Body.Code); err != nil {
			_, _ = h.challenges.IncrementAttempts(ctx, req.Body.ChallengeID)
			return nil, mapMFAError(err)
		}
	}

	// Verified — consume the challenge so it can't be reused.
	_, _ = h.challenges.Consume(ctx, req.Body.ChallengeID)

	user, err := h.users.GetUserByID(ctx, ch.UserUUID)
	if err != nil || user == nil {
		return nil, huma.Error401Unauthorized("user not found")
	}

	amr := appendOTP(ch.SourceAMR)
	tokens, err := h.tokens.IssueLoginTokens(ctx, user, ch.DeviceID, ch.Platform, ch.IPAddress, amr, time.Now().Unix())
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to mint login tokens")
	}

	// Section C item #3: if the user opted into "remember this device",
	// persist a 30-day trust grant so the next login from the same
	// (deviceID, fingerprint) can skip the MFA prompt. Best-effort —
	// a grant failure must not turn a successful login into an error.
	if req.Body.TrustDevice && h.deviceTrust != nil && ch.DeviceID != "" {
		_ = h.deviceTrust.MarkTrusted(ctx, services.MarkTrustedInput{
			UserUUID:    ch.UserUUID,
			DeviceID:    ch.DeviceID,
			Fingerprint: ch.Fingerprint,
			Platform:    ch.Platform,
			IPAddress:   ch.IPAddress,
			GrantedAMR:  "otp",
		})
	}

	resp := &MFALoginVerifyResponse{}
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

// appendOTP returns source with "otp" appended, deduplicating. A nil source
// produces ["pwd","otp"] as a safety default — no live code path hits this
// since both login sources populate SourceAMR, but it keeps the token's
// amr coherent should a future caller forget to set it.
func appendOTP(source []string) []string {
	if len(source) == 0 {
		return []string{"pwd", "otp"}
	}
	for _, v := range source {
		if v == "otp" {
			return source
		}
	}
	return append(source, "otp")
}

// --- error mapping ---

func mapMFAError(err error) error {
	switch {
	case errors.Is(err, services.ErrMFAInvalidCode):
		return huma.Error401Unauthorized("invalid mfa code")
	case errors.Is(err, services.ErrMFAChallengeMismatch):
		return huma.Error400BadRequest("challenge does not match requested action")
	case errors.Is(err, services.ErrMFANotEnrolled):
		return huma.Error400BadRequest("mfa is not enrolled for this user")
	default:
		return huma.Error400BadRequest("mfa request failed")
	}
}

// --- registration ---

// RegisterPublicRoutes mounts endpoints that complete an in-flight login
// and therefore cannot require a bearer token. Only the login-verify path
// lives here; self-service step-up uses the protected endpoint instead.
// See RouteMount for path/operation-ID prefix semantics.
func (h *MFAHandler) RegisterPublicRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-login-verify",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/login/verify",
		Summary:     "Complete a login by verifying a TOTP or backup code",
		Tags:        []string{"Authentication", "MFA"},
	}, h.LoginVerify)
}

func (h *MFAHandler) RegisterProtectedRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-enroll-begin",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/enroll/begin",
		Summary:     "Begin MFA (TOTP) enrollment",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.EnrollBegin)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-enroll-confirm",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/enroll/confirm",
		Summary:     "Confirm MFA enrollment and receive backup codes",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.EnrollConfirm)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-status",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/mfa",
		Summary:     "Return the current user's MFA enrollment status",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.Status)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-verify",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/verify",
		Summary:     "Verify a TOTP or backup code; returns a stepped-up access token",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.Verify)
}

// RegisterStepUpRoutes mounts endpoints that demand a *fresh* MFA proof.
// The caller wires RequireStepUp(5m) around this API instance — see
// auth/module.go.
func (h *MFAHandler) RegisterStepUpRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-remove",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/mfa/remove",
		Summary:     "Remove the current user's MFA factor (requires fresh step-up)",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.Remove)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-regenerate-backup-codes",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/mfa/backup-codes/regenerate",
		Summary:     "Regenerate the current user's MFA backup codes (requires fresh step-up)",
		Description: "Destroys the existing backup-code set and returns a freshly generated list exactly once. Old codes stop working immediately.",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.RegenerateBackupCodes)
}

// RegisterAdminRoutes mounts the admin-scoped reset endpoint. The caller
// must chain RequireSystemPermission + RequireStepUp around this API
// instance before invocation — see auth/module.go for the wiring. The
// admin surface is operator-tier-only by definition (Tier-1 internal
// console only) so it doesn't take a RouteMount parameter.
func (h *MFAHandler) RegisterAdminRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "mfa-admin-reset",
		Method:      http.MethodPost,
		Path:        "/v1/admin/users/{userId}/mfa/reset",
		Summary:     "Admin: delete an operator user's MFA factor and restart their enrollment grace",
		Tags:        []string{"Administration", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.AdminReset)
}

// RegisterClientAdminRoutes mounts the same AdminReset action under the
// /v1/admin/client-users path so an operator (mounted on the operator
// host) can reset a Tier-2 client user's MFA factor. Callers wire the
// **client-tier** MFAHandler instance here so the reset operates against
// client_mfa_factors and the client UserService — preventing an
// operator-tier handler from accidentally targeting client tables.
// Same RequireSystemPermission + RequireStepUp gating as the operator
// admin route.
func (h *MFAHandler) RegisterClientAdminRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "mfa-admin-reset-client",
		Method:      http.MethodPost,
		Path:        "/v1/admin/client-users/{userId}/mfa/reset",
		Summary:     "Admin: delete a Tier-2 client user's MFA factor and restart their enrollment grace",
		Tags:        []string{"Administration", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.AdminReset)
}
