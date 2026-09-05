package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// LoginTokenIssuer is the subset of PasswordAuthService the MFA login verify
// endpoint needs to mint and persist a full token pair. Kept as a local
// interface so the MFA handler doesn't import the whole password service.
type LoginTokenIssuer interface {
	IssueLoginTokensForSession(ctx context.Context, user *iface.User, in services.LoginTokenContext, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error)
	// EmitBreakGlassUsed records a rescued password authentication (spec
	// §4.2). On the interface — not an optional assertion — so a fake
	// cannot silently drop the audit contract.
	EmitBreakGlassUsed(ctx context.Context, audience, userUUID, sessionID, ip string)
}

// passwordLoginDecider is the one-method slice of AuthPolicyService the
// completion re-check consumes; tests inject a fake.
type passwordLoginDecider interface {
	PasswordLoginDecision(ctx context.Context, audience services.PolicyAudience) (services.PasswordAuthDecision, error)
}

// recheckPasswordChallenge enforces spec §4.3's pending-challenge rule on
// both completion endpoints, BEFORE the factor is verified (a disabled
// login must not burn attempt budget or probe factor state):
//
//   - challenge not password-sourced → untouched, (false, nil);
//   - empty/unknown audience → pre-toggle in-flight challenge: consumed,
//     401 (rollout waits one challenge TTL before exposing the switch);
//   - decision error → 503 auth.policy_unavailable WITHOUT consuming, so
//     a transient outage is retryable inside the original TTL;
//   - !Allowed → atomically consumed, 403 auth.password_login_disabled;
//   - Allowed → (BreakGlassUsed, nil); the one-winner Consume stays with
//     the caller's existing flow.
//
// A nil decider on a password-sourced challenge is missing wiring — an
// outage, never a pass (G4).
func recheckPasswordChallenge(ctx context.Context, policy passwordLoginDecider, challenges services.MFAChallengeService, ch *services.MFAChallenge) (bool, error) {
	if !passwordSourcedChallenge(ch) {
		return false, nil
	}
	var audience services.PolicyAudience
	switch ch.Audience {
	case string(services.PolicyAudienceOperator):
		audience = services.PolicyAudienceOperator
	case string(services.PolicyAudienceClient):
		audience = services.PolicyAudienceClient
	default:
		_, _ = challenges.Consume(ctx, ch.ID)
		return false, huma.Error401Unauthorized("invalid or expired challenge")
	}
	if policy == nil {
		return false, errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
	decision, err := policy.PasswordLoginDecision(ctx, audience)
	if err != nil {
		return false, errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
	if !decision.Allowed {
		_, _ = challenges.Consume(ctx, ch.ID)
		return false, errcode.Forbidden(errcode.AuthPasswordLoginDisabled,
			"Email/password sign-in was disabled while this login was in flight. Start over with a configured sign-in provider.")
	}
	return decision.BreakGlassUsed, nil
}

// passwordSourcedChallenge reports whether the login challenge was minted
// by a password login (either provenance marker; both are stamped today —
// password_auth_service.go completeLogin, auth_service.go OAuth branch).
func passwordSourcedChallenge(ch *services.MFAChallenge) bool {
	if ch.LoginMethod == "password" {
		return true
	}
	for _, v := range ch.SourceAMR {
		if v == "pwd" {
			return true
		}
	}
	return false
}

// adminAuthRecorder is the narrow slice of AuthService MFAHandler needs
// to log admin-on-user MFA actions to the audit pipeline. Kept as an
// interface so tests can substitute a capturing fake without pulling in
// the full AuthService surface.
type adminAuthRecorder interface {
	RecordAdminAuthEvent(ctx context.Context, eventType, actorUUID, targetUUID string, fields map[string]interface{})
}

// MFAHandler binds the MFA service to its HTTP surface. All endpoints live
// under /v1/auth/mfa or /v1/auth/me/mfa and require an authenticated user
// (no org context needed, so RequireGlobal() is the correct gate).
type MFAHandler struct {
	mfa         services.MFAService
	challenges  services.MFAChallengeService
	jwt         services.JWTService
	users       iface.UserProvider
	tokens      LoginTokenIssuer
	webauthn    services.WebAuthnService    // optional — populated when WebAuthn is configured
	deviceTrust services.DeviceTrustService // optional — Section C item #3
	policy      *services.AuthPolicyService // optional — admin-managed mfaEnabled + grace-window source
	recorder    adminAuthRecorder           // optional — emits admin_mfa_reset to the audit pipeline
	// sessions ends the sessions a credential change invalidated (spec
	// §4.3 D16). Typed services.AuthService, not iface.SessionTerminator,
	// because two of the three call sites need
	// RevokeAllUserSessionsExcept and that interface declares only
	// TerminateAllSessionsByUUID (ruling R5). Per tier: the operator
	// handler gets the operator authService, the client handler the
	// client one — same module, so this is not a cross-module import.
	// Optional; nil degrades to "the epoch alone ends MFA authority".
	sessions services.AuthService
	// verifyAttempts + audience are the OUTER attempt cap on the
	// authenticated verify route (spec §4.3 D20, M-3). The per-challenge
	// counter inside MFAChallengeService bounds ONE challenge; this bounds
	// the caller, per (audience, user), across challenges. Both fields are
	// set together by SetVerifyAttemptCounter so the key can never be built
	// for the wrong tier. Optional: a nil counter leaves the route uncapped,
	// which is what it was before D20.
	verifyAttempts services.AttemptCounter
	audience       services.PolicyAudience
	cookieName     string
	cookieDomain   string
	cookieSecure   bool
}

// SetAuditRecorder wires the admin-auth event recorder. Mirrors the
// other optional setters on this handler — nil-tolerant. Called from
// auth/module.go::Init after both AuthService and MFAHandler are
// constructed.
func (h *MFAHandler) SetAuditRecorder(r adminAuthRecorder) {
	h.recorder = r
}

// SetSessionTerminator wires the tier's own auth service so a credential
// change can end the sessions it invalidated. See the struct field for why
// the type is the full AuthService rather than iface.SessionTerminator.
func (h *MFAHandler) SetSessionTerminator(s services.AuthService) {
	h.sessions = s
}

// SetVerifyAttemptCounter wires the outer MFA-verify cap and the audience
// its key is scoped to (spec §4.3 D20). The two arrive together on purpose:
// AttemptKeyMFAVerify puts the audience in the key, so an operator handler
// wired with the client audience would share one lockout scope across both
// tiers — locking an operator would lock the client account that happens to
// carry the same UUID. module.go wires each tier's handler from that tier's
// own bundle audience, which is derived from the same `tier` value that
// picks the tier's collections.
func (h *MFAHandler) SetVerifyAttemptCounter(c services.AttemptCounter, audience services.PolicyAudience) {
	h.verifyAttempts = c
	h.audience = audience
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
// hardcoded 7-day fallback), honours the master mfaEnabled flag, and lets
// LoginVerify re-evaluate a password-sourced challenge's per-surface
// policy at completion time (spec §4.3). Wired unconditionally in
// module.go. Nil still falls back to the legacy hardcoded values for the
// MFA-enabled / grace reads, but it makes every PASSWORD-SOURCED
// completion fail closed with 503 auth.policy_unavailable — see decider
// and recheckPasswordChallenge above.
func (h *MFAHandler) SetPolicy(p *services.AuthPolicyService) {
	h.policy = p
}

// decider adapts the handler's concrete policy pointer to the helper's
// interface, mapping a nil pointer to a nil INTERFACE so missing wiring
// takes the fail-closed 503 branch instead of a typed-nil surprise.
func (h *MFAHandler) decider() passwordLoginDecider {
	if h.policy == nil {
		return nil
	}
	return h.policy
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
	codes, replaced, err := h.mfa.ConfirmEnrollment(ctx, userUUID, req.Body.ChallengeID, req.Body.Code)
	// D16: a replacement destroyed the old secret, so it is a removal and
	// carries a removal's consequences. A first enrolment invalidates
	// nothing and revokes nothing. The service reports which happened —
	// it cannot see the caller's sid to act on it itself.
	//
	// Checked BEFORE the error is mapped, and deliberately so: the service
	// reports replaced=true alongside an error when the old secret was
	// destroyed but the new one failed to persist. Those sessions were
	// authorised by a factor that no longer exists whether or not the
	// enrolment completed, so returning first would have made the
	// `replaced` value dead on exactly the path that needs it most.
	if replaced {
		revokeSessionsExceptCurrent(ctx, h.sessions, userUUID, "totp_factor_replaced")
	}
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
	// the handler performs the removal and then ends what the removed
	// factor authorised everywhere else (D16). The service already bumped
	// the epoch, which is what closes the caller's OWN token.
	err := h.mfa.RemoveFactor(ctx, userUUID, userUUID)
	if errors.Is(err, services.ErrMFANotEnrolled) {
		// Nothing existed, so nothing was destroyed: no consequences.
		return nil, mapMFAError(err)
	}
	// Everything below follows the DESTRUCTION, not the success of the
	// call — the same rule the service's own deferred epoch bump obeys.
	// RemoveFactor deletes the TOTP row before the WebAuthn one, so any
	// other error means at least one credential is already gone, and the
	// sessions it authorised must end regardless.
	//
	// RemoveFactor removes EVERY factor (D15), so on the success path the
	// caller is now factor-less: restart their enrolment grace window or
	// an MFA-obliged user has just locked themselves out permanently — see
	// resetMFAGraceClock. On the partial-failure path a factor may still
	// survive and the restart is merely redundant; a needless restart
	// moves a countdown, a missed one costs an administrator their account.
	resetMFAGraceClock(ctx, h.users, userUUID, "self_mfa_removed")
	revokeSessionsExceptCurrent(ctx, h.sessions, userUUID, "self_mfa_removed")
	if err != nil {
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
	device, security, ok := currentSessionSecurity(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}

	// Spec §4.3 D20 — peek BEFORE verifying, so a locked caller costs a
	// Redis read and nothing more, and so a lock cannot extend itself on
	// every probe. A counter error reads as "not locked": the whole
	// AttemptCounter contract is fail-open (attempt_counter.go), because a
	// Redis outage must not take step-up down for every user at once.
	key := services.AttemptKeyMFAVerify(h.audience, userUUID)
	if h.verifyAttempts != nil {
		if v, err := h.verifyAttempts.Locked(ctx, key, services.MFAVerifyLimit); err == nil && v.Locked {
			return nil, lockoutError(v.RetryAfter)
		}
	}

	var verifyErr error
	if req.Body.UseBackup {
		verifyErr = h.mfa.VerifyBackupCode(ctx, userUUID, req.Body.Code)
	} else {
		verifyErr = h.mfa.Verify(ctx, userUUID, req.Body.Code)
	}
	if verifyErr != nil {
		// ONE failure per REQUEST, whatever the verification compared
		// internally: VerifyBackupCode walks the whole hashed list and
		// returns a single ErrMFAInvalidCode, so charging per comparison
		// would lock a user out on their first backup-code attempt.
		//
		// Only a rejected CREDENTIAL is charged. "Not enrolled" and a
		// wrapped store error are refusals, not guesses — charging them
		// would let a degraded backend, or an account with no factor,
		// burn its own budget without an attacker ever trying a code.
		// Every wrong-code branch in mfaService returns ErrMFAInvalidCode
		// (step mismatch, replay, a lost CAS race, empty input), so
		// nothing that IS a guess escapes this. ErrMFAMethodDisabled is
		// not in the picture at all: only BeginEnrollment produces it,
		// never Verify or VerifyBackupCode.
		if h.verifyAttempts != nil && errors.Is(verifyErr, services.ErrMFAInvalidCode) {
			_, _ = h.verifyAttempts.RecordFailure(ctx, key, services.MFAVerifyLimit)
		}
		return nil, mapMFAError(verifyErr)
	}
	if h.verifyAttempts != nil {
		_ = h.verifyAttempts.Reset(ctx, key)
	}

	user, err := h.users.GetUserByID(ctx, userUUID)
	if err != nil || services.ValidateTokenEligibleUser(user) != nil {
		return nil, huma.Error401Unauthorized("user not found")
	}
	amr := priorAMRWithOTP(ctx)
	lastOTPAt := nowUnix()
	token, err := h.jwt.GenerateAccessTokenForSessionWithAMR(user, device, security, amr, lastOTPAt)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to mint stepped-up token")
	}

	resp := &MFAVerifyResponse{}
	resp.Body.Success = true
	resp.Body.AccessToken = token
	resp.Body.TokenType = "Bearer"
	resp.Body.ExpiresIn = int64(h.jwt.AccessTokenTTL(ctx).Seconds())
	return resp, nil
}

// priorAMRWithOTP returns the caller's existing amr, stripped of every
// epoch-governed marker, plus "otp" — the factor this mint just verified.
// When the prior token has no amr (dev tokens, tokens minted before Block A)
// we default to ["pwd","otp"] so the resulting token still looks coherent.
// The "claims" context key is populated by AuthMiddleware.setUserContext.
//
// The strip is the same rule priorAMRFromCtx applies, one door over: this
// mint stamps the user's CURRENT epoch and last_otp_at=now, so any marker
// copied from the raw claim is re-issued with fresh authority. The caller
// has just proven a real factor, so the risk is narrower than the password
// reconfirm's — but the laundered marker is a DIFFERENT one: a deleted
// passkey's "webauthn" would otherwise survive a TOTP verify and come back
// current. Carrying only what was actually verified keeps the claim an
// honest record of this ceremony.
//
// "reauth" and the base markers survive: the epoch governs MFA
// credentials, and neither a password reconfirm nor "how the session began"
// is one.
func priorAMRWithOTP(ctx context.Context) []string {
	var prior []string
	if claims, ok := ctx.Value("claims").(*authModels.JWTClaims); ok && claims != nil {
		prior = authModels.WithoutEpochBoundAMR(claims.AMR)
	}
	if len(prior) == 0 {
		prior = []string{"pwd"}
	}
	// No dedup needed: the strip above removed "otp" if it was there, and
	// both branches produce a fresh slice, so the append never writes
	// through to the caller's claims.
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

// AdminReset removes every MFA factor another user holds (TOTP + WebAuthn,
// D15), ends every session they hold, and starts a fresh grace window so
// they must re-enroll within the policy deadline. The route is mounted
// (auth/module.go) behind RequireSystemPermission("system.users.mfa_reset")
// — the permission this module declares — plus RequireStepUp(5m), so the
// caller must hold the permission AND have proved a second factor, or a
// password reconfirm, within the last five minutes. It is NOT behind
// RequireMFA: step-up is the stricter of the two and subsumes it.
//
// This is the one path that terminates ALL sessions rather than sparing
// one: the caller is not the target, so there is no session of the
// caller's to preserve, and an operator resetting someone's MFA is
// recovering an account they have reason to believe is compromised.
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
			// Nothing existed to reset, so nothing changed: a 404, and no
			// audit row for a state change that did not happen.
			return nil, huma.Error404NotFound("target user has no MFA factor to reset")
		}
		// R14: a removal can now fail PART-WAY (one factor row deleted,
		// the other not), and this branch used to return 500 before the
		// recorder ever ran — so the one outcome an operator most needs
		// to see left no trace at all. Record it, then fail.
		//
		// A DISTINCT event type, not admin_mfa_reset with a metadata flag:
		// recordAuthEvent hardcodes Success/Outcome to success for every
		// auth event in the tree, so a failure filed under the success
		// type is indexed as a successful reset and a SOC2 evidence query
		// filtering on outcome would never surface it. Until that
		// pipeline-wide defect is fixed, the type is the only field that
		// can carry the truth.
		//
		// The metadata carries a CLASSIFIED kind, never err.Error(): this
		// map is serialised to GET /v1/admin/audit-events, and an
		// unbounded Mongo driver string can echo namespaces, filter
		// fragments and server addresses into an API response. The detail
		// goes to the log, which is where the other degradation paths in
		// this package already put it.
		slog.Default().Error("auth: admin MFA reset failed; the target account may be half-reset",
			slog.String("target_uuid", req.UserID),
			slog.String("actor_uuid", actorUUID),
			slog.String("error", err.Error()))
		// The consequences follow the DESTRUCTION, not the success of the
		// call — the rule the service's deferred epoch bump already obeys,
		// and which the session half used to break. A part-way removal
		// (one row deleted, the other not) bumped the epoch yet left every
		// other session of the target's alive, which is exactly the state
		// this branch exists to prevent.
		//
		// Restart the grace clock too. When the removal WAS part-way, this
		// is the only pass that can still stamp it: the operator's retry
		// finds no factor row left and answers 404, which by design applies
		// no consequences at all. This branch is also reached when nothing
		// was destroyed — RemoveFactor can fail on either of its two
		// lookups, before any delete — so the stamp is not conditional on
		// destruction the way the epoch bump is. That is deliberate and
		// inert rather than merely tolerable: the clock is read only for a
		// user who has NO factor, so restarting it for a target whose
		// factors all survived changes nothing they will ever meet, while
		// getting it wrong in the other direction is the lockout this whole
		// helper exists to prevent. The handler cannot tell the two apart
		// anyway — `destroyed` lives inside RemoveFactor and is not
		// reported outward.
		resetMFAGraceClock(ctx, h.users, req.UserID, "admin_mfa_reset_failed")
		terminated := h.terminateAllSessions(ctx, req.UserID)
		h.recordAdminResetEvent(ctx, "admin_mfa_reset_failed", actorUUID, req.UserID, map[string]interface{}{
			"outcome":             "failed",
			"error_kind":          "removal_failed",
			"sessions_terminated": terminated,
		})
		return nil, huma.Error500InternalServerError("failed to reset MFA factor")
	}
	resetMFAGraceClock(ctx, h.users, req.UserID, "admin_mfa_reset")
	// D16: RemoveFactor already bumped the target's MFA epoch, which ends
	// the MFA authority of every token they hold — including ones already
	// in flight. This ends the sessions themselves. A failure is recorded
	// and NOT fatal: what survives it is ordinary session access, the same
	// exposure as any degraded revocation, and telling the operator the
	// reset failed would send them chasing a factor that is already gone.
	terminated := h.terminateAllSessions(ctx, req.UserID)
	h.recordAdminResetEvent(ctx, "admin_mfa_reset", actorUUID, req.UserID, map[string]interface{}{
		"sessions_terminated": terminated,
	})
	resp := &MFAAdminResetResponse{}
	resp.Body.Success = true
	return resp, nil
}

// terminateAllSessions ends every session the target holds and reports
// whether it succeeded, for the audit row. This is the one path that spares
// nothing: the caller is not the target, so there is no session of the
// caller's to preserve. Nil-tolerant and never fatal — the epoch already
// ended the target's MFA authority, so what survives a failure here is
// ordinary session access.
func (h *MFAHandler) terminateAllSessions(ctx context.Context, targetUUID string) bool {
	if h.sessions == nil {
		slog.Default().Warn("auth: session terminator not wired; admin MFA reset left the target's sessions signed in",
			slog.String("target_uuid", targetUUID))
		return false
	}
	if err := h.sessions.TerminateAllSessionsByUUID(ctx, targetUUID); err != nil {
		slog.Default().Warn("auth: admin MFA reset could not terminate the target's sessions; their MFA authority ended anyway via the epoch",
			slog.String("target_uuid", targetUUID),
			slog.String("error", err.Error()))
		return false
	}
	return true
}

// recordAdminResetEvent writes one row through the audit pipeline: slog +
// auth_security_events + compliance via the AuthService recorder. eventType
// is admin_mfa_reset or admin_mfa_reset_failed — see AdminReset's failure
// branch for why the outcome has to live in the type. Nil-tolerant — a
// build without the audit wiring still succeeds at the user-facing
// operation. fields must carry classified values only; it is serialised to
// GET /v1/admin/audit-events.
func (h *MFAHandler) recordAdminResetEvent(ctx context.Context, eventType, actorUUID, targetUUID string, fields map[string]interface{}) {
	if h.recorder == nil {
		return
	}
	h.recorder.RecordAdminAuthEvent(ctx, eventType, actorUUID, targetUUID, fields)
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
	if ch.SessionID == "" {
		return nil, huma.Error401Unauthorized("invalid or expired challenge")
	}

	// Spec §4.3: a password-sourced challenge must still be allowed NOW
	// (before the factor is verified, so a disabled login can neither
	// burn attempt budget nor probe factor state — deviation 6).
	rescued, err := recheckPasswordChallenge(ctx, h.decider(), h.challenges, ch)
	if err != nil {
		return nil, err
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

	// Verified — atomically claim the challenge. Concurrent requests may
	// both verify the same factor, but only the GETDEL winner may mint.
	ch, err = h.challenges.Consume(ctx, req.Body.ChallengeID)
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid or expired challenge")
	}

	user, err := h.users.GetUserByID(ctx, ch.UserUUID)
	if err != nil || services.ValidateTokenEligibleUser(user) != nil {
		return nil, huma.Error401Unauthorized("user not found")
	}

	amr := appendOTP(ch.SourceAMR)
	tokens, err := h.tokens.IssueLoginTokensForSession(ctx, user, services.LoginTokenContext{
		SessionID: ch.SessionID, DeviceID: ch.DeviceID, DeviceType: ch.DeviceType,
		Platform: ch.Platform, IPAddress: ch.IPAddress, Fingerprint: ch.Fingerprint,
		UserAgent: ch.UserAgent, LoginMethod: ch.LoginMethod, RiskScore: ch.RiskScore,
		RiskFactors: append([]string(nil), ch.RiskFactors...), TrustLevel: ch.TrustLevel,
		MFACompleted: true,
	}, amr, time.Now().Unix())
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to mint login tokens")
	}

	// One rescued-login audit event per winning completion (spec §4.2):
	// either the initial password check or this completion's decision
	// used the break-glass. Only the Consume winner reaches this line.
	if rescued || ch.BreakGlassUsed {
		h.tokens.EmitBreakGlassUsed(ctx, ch.Audience, ch.UserUUID, ch.SessionID, ch.IPAddress)
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
	resp.SetCookie = buildRefreshCookie(h.cookieName, tokens.RefreshToken, h.cookieDomain, h.cookieSecure,
		int(h.jwt.RefreshTokenTTL().Seconds()))
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
	case errors.Is(err, services.ErrMFAMethodDisabled):
		// Phase 3.6 — admin restricted this factor type via the
		// mfaMethods allow-list. The frontend should redirect the user
		// to a still-allowed type instead of retrying.
		return huma.Error403Forbidden("mfa_method_disabled: this factor type is not allowed by policy")
	default:
		slog.Default().Error("unmapped mfa auth error", slog.String("error", err.Error()))
		return errcode.Internal(errcode.AuthUnavailable,
			"MFA verification failed for an unexpected reason. The failure has been logged for an administrator.")
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

// RegisterProtectedRoutes mounts the MFA endpoints a plain bearer is
// enough for: reading your own enrollment status, and verifying a code you
// already hold a factor for. Creating or replacing a factor is NOT here —
// see RegisterEnrolmentRoutes.
func (h *MFAHandler) RegisterProtectedRoutes(api huma.API, mount RouteMount) {
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

// RegisterEnrolmentRoutes mounts the endpoints that CREATE or REPLACE a
// second factor. The caller wires the enrolment-proof gate around this API
// instance — auth/module.go's enrolmentGate helper, which resolves it off the
// surface's RoleMiddleware through module.EnrolmentProofGate and substitutes
// an always-refuse middleware when that assertion fails. Both halves of the ceremony are gated, not
// just the first: the factor set can change between begin and confirm, so a
// begin that passed the gate must not license a confirm that would not.
//
// H-2/H-3: these lived in RegisterProtectedRoutes, under RequireGlobal()
// alone, which let a stolen session-only bearer replace the victim's TOTP
// secret (EnrollConfirm deletes the existing factor after validating a code
// for the NEW one) or add a passkey of its own.
func (h *MFAHandler) RegisterEnrolmentRoutes(api huma.API, mount RouteMount) {
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
