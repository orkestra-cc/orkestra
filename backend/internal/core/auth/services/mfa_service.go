package services

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// BackupCodeCount is the number of one-shot recovery codes issued on
// successful enrollment. Ten is the community norm; enough that a user can
// lose a few and still recover, few enough that printed storage is practical.
const BackupCodeCount = 10

// ErrMFANotEnrolled is returned by Verify/VerifyBackupCode/RemoveFactor when
// the user has not yet completed enrollment. Callers convert it to 400/404.
var ErrMFANotEnrolled = errors.New("mfa not enrolled")

// ErrMFAInvalidCode is returned when a supplied TOTP code or backup code is
// rejected. Caller should convert to 401 and optionally increment attempts.
var ErrMFAInvalidCode = errors.New("invalid mfa code")

// ErrMFAMethodDisabled is returned by BeginEnrollment (TOTP) and by
// the WebAuthn register-begin path when the admin-managed mfaMethods
// allow-list excludes the requested factor type. Handlers map this to
// 403 with body code mfa_method_disabled. Phase 3.6 of the
// auth-policy roadmap.
var ErrMFAMethodDisabled = errors.New("mfa method not allowed by policy")

// ErrMFAChallengeMismatch is returned when a challenge's purpose doesn't
// match the caller's flow (e.g. an enroll challenge supplied to /verify).
var ErrMFAChallengeMismatch = errors.New("mfa challenge purpose mismatch")

// MFAEnrollmentBegin describes the payload handed back to the client when it
// kicks off enrollment. The secret is shown once, never persisted in plain.
type MFAEnrollmentBegin struct {
	ChallengeID     string
	SecretBase32    string
	ProvisioningURI string
}

// MFAStatusSnapshot is the small public view of a user's MFA state returned
// by GET /v1/auth/me/mfa. Block A reports only Enrolled/NotRequired — the
// role-based required states arrive in Block B.
type MFAStatusSnapshot struct {
	Status               models.MFAStatus
	Type                 models.MFAFactorType
	BackupCodesRemaining int
	LastUsedAt           *time.Time
}

// MFAService orchestrates enrollment, verification, and recovery for TOTP.
// It holds no state of its own — everything lives in Mongo (factor rows)
// or Redis (short-lived challenges), so horizontal scale is free.
type MFAService interface {
	BeginEnrollment(ctx context.Context, user *iface.User) (*MFAEnrollmentBegin, error)
	// ConfirmEnrollment persists the pending TOTP secret and returns the
	// one-shot backup codes. `replaced` reports whether the confirm
	// destroyed an existing TOTP secret rather than adding a first one —
	// a replacement is a REMOVAL, so the caller's other sessions must end
	// (spec §4.3 D16). The service cannot do that itself: it has no
	// session collaborator and no way to learn the caller's session id,
	// which lives on the request claims. So it reports the fact outward
	// and MFAHandler.EnrollConfirm acts on it.
	ConfirmEnrollment(ctx context.Context, userUUID, challengeID, code string) (codes []string, replaced bool, err error)

	Verify(ctx context.Context, userUUID, code string) error
	VerifyBackupCode(ctx context.Context, userUUID, code string) error

	RemoveFactor(ctx context.Context, userUUID, actorUUID string) error
	// RegenerateBackupCodes replaces the user's existing backup codes
	// with a freshly generated set, persists their hashes (atomic
	// $set, not append), and returns the plaintext codes exactly
	// once. Callers must apply the step-up middleware — destroying
	// the existing codes is irreversible. Returns ErrMFANotEnrolled
	// when no TOTP factor exists for the user.
	RegenerateBackupCodes(ctx context.Context, userUUID string) ([]string, error)
	Status(ctx context.Context, userUUID string) (*MFAStatusSnapshot, error)
	// SetDeviceTrust wires the "remember this device" service so
	// factor removal (and the admin-reset variant on top of it)
	// can revoke every trust grant the user holds. Optional — nil
	// leaves the revoke step inert. Section C item #3.
	SetDeviceTrust(dt DeviceTrustService)
	// SetPolicy wires the admin-managed policy reader so backup-code
	// generation can honour the recoveryCodesCount toggle (Phase 10
	// of the auth-policy roadmap). Nil falls back to the legacy
	// hardcoded BackupCodeCount.
	SetPolicy(p *AuthPolicyService)
	// SetAuditSink wires the persistent audit sink so backup-code
	// regenerate and every credential change emit an
	// auth_security_events row. Nil falls back to the legacy slog-only
	// audit lane. Phase 2.2 of the core-completion epic.
	SetAuditSink(sink SecurityEventSink)
	// SetEpochBumper wires the MFA epoch counter (spec §4.3 D16). It is
	// on the INTERFACE, not just the struct, because module.go holds an
	// MFAService interface value — a setter that existed only on the
	// concrete type would be unreachable from the wiring. Optional: a
	// fork's user provider may predate iface.MFAEpochBumper, in which
	// case the epoch never moves and the platform degrades to session
	// revocation alone, which is what it had before. Never fatal.
	SetEpochBumper(b iface.MFAEpochBumper)
	// SetFactorAddedNotifier wires the "a second factor was added to your
	// account" email. Optional — nil sends nothing.
	SetFactorAddedNotifier(n *FactorAddedNotifier)
}

// SecurityEventSink is the narrow interface mfaService uses to emit
// audit rows. Implemented by *authService via RecordSelfAuthEvent.
// Kept here rather than in auth_service.go so mfaService doesn't have
// to import the full AuthService surface.
type SecurityEventSink interface {
	RecordSelfAuthEvent(ctx context.Context, eventType, userUUID string, fields map[string]interface{})
}

type mfaService struct {
	factors     repository.MFAFactorRepository
	challenges  MFAChallengeService
	passwords   PasswordService    // reused for argon2id hashing of backup codes
	deviceTrust DeviceTrustService // optional — see SetDeviceTrust
	issuer      string
	logger      *slog.Logger
	policy      *AuthPolicyService // optional — Phase 10 backup-code count
	// auditSink emits audit rows via authService. Optional; nil keeps
	// the legacy slog-only behaviour so minimal builds still work.
	auditSink SecurityEventSink
	// epoch moves User.MFAEpoch on every removal or replacement, which
	// is what ends the MFA authority already minted into live tokens —
	// including the caller's own. Optional; see SetEpochBumper.
	epoch iface.MFAEpochBumper
	// factorAdded announces an added factor by email. Optional.
	factorAdded *FactorAddedNotifier
}

// SetDeviceTrust wires the optional device-trust service. Called
// post-construction from module.go so the construction graph stays
// free of a cross-service dependency.
func (s *mfaService) SetDeviceTrust(dt DeviceTrustService) { s.deviceTrust = dt }

// SetPolicy wires the optional auth-policy reader. Phase 10 of the
// auth-policy roadmap — used today only by backup-code generation
// (recoveryCodesCount). Safe to call multiple times.
func (s *mfaService) SetPolicy(p *AuthPolicyService) { s.policy = p }

// SetAuditSink wires the optional persistent audit sink so MFA actions
// (backup-codes regenerate) land in auth_security_events. Nil keeps
// the legacy slog-only behaviour.
func (s *mfaService) SetAuditSink(sink SecurityEventSink) { s.auditSink = sink }

// SetEpochBumper wires the optional MFA epoch counter. See the interface
// doc for why absence degrades rather than fails.
func (s *mfaService) SetEpochBumper(b iface.MFAEpochBumper) { s.epoch = b }

// SetFactorAddedNotifier wires the optional factor-added email.
func (s *mfaService) SetFactorAddedNotifier(n *FactorAddedNotifier) { s.factorAdded = n }

// bumpEpoch moves the user's MFA epoch. Called by every removal or
// replacement and by no addition.
func (s *mfaService) bumpEpoch(ctx context.Context, userUUID string) {
	bumpMFAEpoch(ctx, s.epoch, s.logger, userUUID)
}

// applyRemovalConsequences is the pair of service-level halves every
// credential destruction owes: end the MFA authority already minted into
// live tokens, and drop the "remember this device" grants whose amr
// annotation named the factor that just went away.
//
// It exists as one call rather than two so the removal path and the
// replacement path cannot drift on which of them they remember to do —
// and so a partial failure can apply both from a single deferred call.
// Neither half can fail the caller: the credential is already gone, and
// reporting a completed destruction as not having happened is worse than
// either degradation (see bumpMFAEpoch).
//
// The third half of the rule — ending the caller's other sessions — is
// the handler's, because only it can see which session the request
// arrived on.
func (s *mfaService) applyRemovalConsequences(ctx context.Context, userUUID, trustReason string) {
	s.bumpEpoch(ctx, userUUID)
	if s.deviceTrust == nil {
		return
	}
	if err := s.deviceTrust.RevokeAllByUser(ctx, userUUID, trustReason); err != nil {
		s.logger.Warn("device_trust: revoke after a credential removal failed",
			slog.String("user_uuid", userUUID),
			slog.String("reason", trustReason),
			slog.String("error", err.Error()))
	}
}

// NewMFAService builds the service. `issuer` ends up as the label prefix in
// the TOTP provisioning URI — authenticator apps show it above the 6-digit
// code so the user can tell which account a code is for.
func NewMFAService(
	factors repository.MFAFactorRepository,
	challenges MFAChallengeService,
	passwords PasswordService,
	issuer string,
	logger *slog.Logger,
) MFAService {
	if issuer == "" {
		issuer = "Orkestra"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &mfaService{
		factors:    factors,
		challenges: challenges,
		passwords:  passwords,
		issuer:     issuer,
		logger:     logger,
	}
}

func (s *mfaService) BeginEnrollment(ctx context.Context, user *iface.User) (*MFAEnrollmentBegin, error) {
	if user == nil || user.UUID == "" {
		return nil, fmt.Errorf("user is required")
	}

	// Phase 3.6: respect the admin-managed mfaMethods allow-list. An
	// empty list (the default) means "all methods allowed" so existing
	// deployments observe no change. ErrMFAMethodDisabled is mapped to
	// 403 mfa_method_disabled at the handler boundary.
	if s.policy != nil && !s.policy.MFAMethodAllowed(ctx, string(models.MFAFactorTOTP)) {
		return nil, ErrMFAMethodDisabled
	}

	// Calling begin twice before confirm invalidates the prior pending secret
	// — the new challenge issues a new secret. We rely on Redis TTL to
	// expire the old one; no explicit cleanup required.
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.issuer,
		AccountName: user.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1, // widest authenticator compatibility
	})
	if err != nil {
		return nil, fmt.Errorf("generate totp secret: %w", err)
	}

	secretBase32 := key.Secret()
	ch, err := s.challenges.Begin(ctx, user.UUID, MFAPurposeEnroll, secretBase32)
	if err != nil {
		return nil, err
	}
	return &MFAEnrollmentBegin{
		ChallengeID:     ch.ID,
		SecretBase32:    secretBase32,
		ProvisioningURI: key.URL(),
	}, nil
}

func (s *mfaService) ConfirmEnrollment(ctx context.Context, userUUID, challengeID, code string) ([]string, bool, error) {
	if userUUID == "" || challengeID == "" || code == "" {
		return nil, false, fmt.Errorf("userUUID, challengeID, and code are required")
	}

	// Peek before consuming so we don't destroy a valid challenge on a typo.
	ch, err := s.challenges.Peek(ctx, challengeID)
	if err != nil {
		return nil, false, ErrMFAInvalidCode
	}
	if ch.UserUUID != userUUID || ch.Purpose != MFAPurposeEnroll {
		return nil, false, ErrMFAChallengeMismatch
	}

	if !validateTOTP(ch.PendingSecret, code) {
		attempts, _ := s.challenges.IncrementAttempts(ctx, challengeID)
		_ = attempts
		return nil, false, ErrMFAInvalidCode
	}

	// Consume the challenge now — verified, about to persist the factor.
	_, _ = s.challenges.Consume(ctx, challengeID)

	// If a factor already exists for this user+type (enrollment repeated),
	// replace it. The unique index on (userUuid, type) guarantees we never
	// have duplicates in flight.
	//
	// A replacement is a REMOVAL of the old secret, so it carries the same
	// consequences (§4.3 D16) — and they are applied HERE, the instant the
	// old row is gone, not after the new one is persisted. Three returns
	// sit between this point and the Insert (encrypt, backup codes, the
	// write itself); taking any of them with the consequences still
	// pending would leave the account holding no TOTP factor while every
	// live token still carried amr:["pwd","otp"], every device-trust grant
	// stood, and no session had been revoked — M-2 surviving inside the
	// fix for M-2. Worse, the caller's retry would find no existing row,
	// report replaced=false, and never bump the epoch at all.
	//
	// Over-bumping is harmless by construction: the counter is monotone
	// and no ADDITION ever depends on its value, so the cost of a bump on
	// a replacement that then fails to persist is one extra re-login.
	//
	// RequireEnrolmentProof has already guaranteed a fresh proof of
	// presence, so reaching this line is a deliberate act by the account
	// holder.
	//
	// A failed delete is returned rather than swallowed: the unique index
	// would reject the Insert below anyway, so continuing only trades a
	// clear error for a confusing one. Nothing was destroyed on that
	// branch, so nothing is applied.
	replaced := false
	if existing, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP); err == nil && existing != nil {
		if err := s.factors.Delete(ctx, existing.UUID); err != nil {
			return nil, false, fmt.Errorf("replace existing totp factor: %w", err)
		}
		replaced = true
		s.applyRemovalConsequences(ctx, userUUID, models.DeviceTrustRevokedOnMFAReplace)
		emitCredentialEvent(ctx, s.auditSink, s.logger, "self_mfa_factor_replaced", userUUID, map[string]interface{}{
			"factorType": string(models.MFAFactorTOTP),
		})
	}

	secretEnc, err := utils.EncryptMFASecret(ch.PendingSecret)
	if err != nil {
		return nil, replaced, fmt.Errorf("encrypt totp secret: %w", err)
	}

	plaintextCodes, hashedCodes, err := s.generateBackupCodes(s.recoveryCodesCount(ctx))
	if err != nil {
		return nil, replaced, fmt.Errorf("generate backup codes: %w", err)
	}

	now := time.Now()
	doc := &models.MFAFactorDoc{
		UUID:              uuid.NewString(),
		UserUUID:          userUUID,
		Type:              models.MFAFactorTOTP,
		SecretEnc:         secretEnc,
		VerifiedAt:        &now,
		CreatedAt:         now,
		BackupCodesHashed: hashedCodes,
	}
	if err := s.factors.Insert(ctx, doc); err != nil {
		return nil, replaced, fmt.Errorf("persist mfa factor: %w", err)
	}

	// D13/D16: only the REPLACEMENT branch invalidates anything, and it
	// already did so above, at the moment of destruction. A first
	// enrolment is a pure addition — authority proven by a factor that
	// still exists stays valid, so the epoch does not move, no trust grant
	// becomes a lie, and its event belongs here, after the new row is
	// safely persisted: nothing happened until it was.
	if !replaced {
		emitCredentialEvent(ctx, s.auditSink, s.logger, "self_mfa_enrolled", userUUID, map[string]interface{}{
			"factorType": string(models.MFAFactorTOTP),
		})
	}
	// Announced even on a replacement: the old secret is gone, but a NEW
	// one now exists, and it is the addition an attacker would perform.
	s.factorAdded.NotifyFactorAdded(ctx, userUUID, string(models.MFAFactorTOTP), replaced)

	s.logger.Info("mfa enrollment confirmed",
		slog.String("userUUID", userUUID),
		slog.String("factorUUID", doc.UUID),
		slog.Bool("replaced", replaced),
	)
	return plaintextCodes, replaced, nil
}

func (s *mfaService) Verify(ctx context.Context, userUUID, code string) error {
	if userUUID == "" || code == "" {
		return ErrMFAInvalidCode
	}
	factor, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP)
	if err != nil {
		if errors.Is(err, repository.ErrMFAFactorNotFound) {
			return ErrMFANotEnrolled
		}
		return err
	}
	secret, err := utils.DecryptMFASecret(factor.SecretEnc)
	if err != nil {
		return fmt.Errorf("decrypt totp secret: %w", err)
	}
	now := time.Now()
	step, ok := matchTOTPStep(secret, code, now)
	if !ok {
		return ErrMFAInvalidCode
	}
	// Replay guard: reject any step we've already consumed (including the
	// current one on a second attempt within the 30s window). CAS via the
	// repository ensures concurrent verifies can't both win.
	if factor.LastUsedStep > 0 && step <= factor.LastUsedStep {
		return ErrMFAInvalidCode
	}
	advanced, err := s.factors.AdvanceLastUsedStep(ctx, factor.UUID, step, now)
	if err != nil {
		return fmt.Errorf("advance totp step: %w", err)
	}
	if !advanced {
		// Another verify won the race — the code is already spent.
		return ErrMFAInvalidCode
	}
	return nil
}

func (s *mfaService) VerifyBackupCode(ctx context.Context, userUUID, code string) error {
	if userUUID == "" || code == "" {
		return ErrMFAInvalidCode
	}
	factor, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP)
	if err != nil {
		if errors.Is(err, repository.ErrMFAFactorNotFound) {
			return ErrMFANotEnrolled
		}
		return err
	}
	normalised := normaliseBackupCode(code)
	for _, hashed := range factor.BackupCodesHashed {
		ok, err := s.passwords.Verify(normalised, hashed)
		if err != nil {
			continue
		}
		if ok {
			removed, err := s.factors.ConsumeBackupCode(ctx, userUUID, hashed)
			if err != nil {
				return err
			}
			if !removed {
				// Another request consumed it first — treat as invalid so
				// the same code can't succeed twice across a race.
				return ErrMFAInvalidCode
			}
			_ = s.factors.UpdateLastUsed(ctx, factor.UUID, time.Now())
			return nil
		}
	}
	return ErrMFAInvalidCode
}

// RemoveFactor deletes EVERY MFA credential the user holds — the TOTP row
// and the WebAuthn row — and returns ErrMFANotEnrolled only when neither
// exists.
//
// It used to delete the TOTP row alone, which made "remove MFA" a lie for
// anyone who also had a passkey and made the admin reset answer 404 for a
// passkey-only target: an operator could not recover such an account at
// all (D15). A WebAuthn row with no credentials is not a factor, matching
// what MFAEnrollmentLookup (auth/module.go) already reports — the two
// must agree since the enrolment gate consumes that lookup.
//
// Both rows are looked up before either is deleted, and a failure of
// either delete is returned rather than swallowed: reporting success on
// a half-reset account would tell an operator the target is recoverable
// when it is not (the admin handler audits that 500 and answers it).
//
// A partial failure still applies the removal consequences, because they
// follow the DESTRUCTION of a credential, not the success of the whole
// call. Returning the WebAuthn delete's error with the epoch unmoved
// would leave a half-reset account — TOTP row gone, device trust intact,
// every session live and fully MFA-authorised — which is the exact
// scenario the admin path's failure audit row exists to make visible.
func (s *mfaService) RemoveFactor(ctx context.Context, userUUID, actorUUID string) error {
	totpFactor, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP)
	if err != nil && !errors.Is(err, repository.ErrMFAFactorNotFound) {
		return err
	}
	waFactor, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorWebAuthn)
	if err != nil && !errors.Is(err, repository.ErrMFAFactorNotFound) {
		return err
	}
	hasWebAuthn := waFactor != nil && len(waFactor.WebAuthnCredentials) > 0
	if totpFactor == nil && !hasWebAuthn {
		return ErrMFANotEnrolled
	}

	// Section C item #3: removing the MFA factor(s) also invalidates
	// every "remember this device" grant the user holds. A trust row
	// carries an amr annotation that claims a factor was verified —
	// once that factor is gone, the annotation is a lie. User-initiated
	// removal and admin reset both flow through here; the revoke reason
	// distinguishes them (actorUUID != userUUID indicates admin). Resolved
	// up front so the success path and the partial-failure path below
	// cannot disagree about it.
	reason := models.DeviceTrustRevokedOnMFARemove
	if actorUUID != "" && actorUUID != userUUID {
		reason = models.DeviceTrustRevokedOnAdminReset
	}

	// destroyed says whether any credential row is already gone. The
	// deferred call is what makes the rule unconditional — if a credential
	// was destroyed, the epoch moves and the trust grants go, success or
	// not — and it fires exactly once per call regardless of how many rows
	// were deleted, which is the property TestRemoveFactor_RevokesDeviceTrustOnce
	// pins. Nothing deleted (either lookup failed, or the TOTP delete
	// itself failed) means nothing to invalidate, so the flag stays false.
	destroyed := false
	defer func() {
		if destroyed {
			s.applyRemovalConsequences(ctx, userUUID, reason)
		}
	}()

	if totpFactor != nil {
		if err := s.factors.Delete(ctx, totpFactor.UUID); err != nil {
			return err
		}
		destroyed = true
	}
	// Gated on the row's existence, not on hasWebAuthn (which is the
	// not-enrolled predicate, zero-credential rows included): an empty
	// WebAuthn row is not a factor, but it is still a row, and
	// RemoveWebAuthnCredential already treats a credential-less row as
	// garbage to collect. Leaving it behind on "remove everything" would
	// make this the one path that grows a permanent orphan instead.
	if waFactor != nil {
		if err := s.factors.Delete(ctx, waFactor.UUID); err != nil {
			return err
		}
		destroyed = true
	}

	// D13: the SELF removal gets a self_mfa_removed row. The admin variant
	// deliberately does not — the handler records admin_mfa_reset for it,
	// and emitting both would file one reset twice in the audit timeline
	// under two different actors.
	if actorUUID == "" || actorUUID == userUUID {
		emitCredentialEvent(ctx, s.auditSink, s.logger, "self_mfa_removed", userUUID, map[string]interface{}{
			"totp":     totpFactor != nil,
			"webauthn": hasWebAuthn,
		})
	}

	s.logger.Info("mfa factors removed",
		slog.String("userUUID", userUUID),
		slog.String("actorUUID", actorUUID),
		slog.Bool("totp", totpFactor != nil),
		slog.Bool("webauthn", hasWebAuthn),
	)
	return nil
}

// RegenerateBackupCodes replaces the user's existing backup-code
// hash list with a fresh set, returning the plaintext exactly once.
// The repo's $set replace is atomic — old codes stop working the
// instant the write lands, so a stolen code race is bounded by the
// regeneration latency itself. Returns ErrMFANotEnrolled when the
// user has no TOTP factor (the caller has nothing to replace).
func (s *mfaService) RegenerateBackupCodes(ctx context.Context, userUUID string) ([]string, error) {
	if userUUID == "" {
		return nil, ErrMFANotEnrolled
	}
	if _, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP); err != nil {
		if errors.Is(err, repository.ErrMFAFactorNotFound) {
			return nil, ErrMFANotEnrolled
		}
		return nil, err
	}
	plaintext, hashed, err := s.generateBackupCodes(s.recoveryCodesCount(ctx))
	if err != nil {
		return nil, fmt.Errorf("generate backup codes: %w", err)
	}
	if err := s.factors.ReplaceBackupCodes(ctx, userUUID, hashed); err != nil {
		if errors.Is(err, repository.ErrMFAFactorNotFound) {
			return nil, ErrMFANotEnrolled
		}
		return nil, fmt.Errorf("persist backup codes: %w", err)
	}
	if s.auditSink != nil {
		s.auditSink.RecordSelfAuthEvent(ctx, "self_backup_codes_regenerated", userUUID, map[string]interface{}{
			"count": len(plaintext),
		})
	} else {
		// Legacy slog-only fallback so minimal builds without the audit
		// sink wired still leave a log breadcrumb.
		s.logger.Info("self_auth_action",
			"event", "self_backup_codes_regenerated",
			"userUUID", userUUID,
			"count", len(plaintext),
		)
	}
	return plaintext, nil
}

func (s *mfaService) Status(ctx context.Context, userUUID string) (*MFAStatusSnapshot, error) {
	factor, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP)
	if err != nil {
		if errors.Is(err, repository.ErrMFAFactorNotFound) {
			// Block B will return required_pending_enrollment / grace here
			// once role-based MFA requirements land. For now only the two
			// determinate states are populated.
			return &MFAStatusSnapshot{Status: models.MFAStatusNotRequired}, nil
		}
		return nil, err
	}
	return &MFAStatusSnapshot{
		Status:               models.MFAStatusEnrolled,
		Type:                 factor.Type,
		BackupCodesRemaining: len(factor.BackupCodesHashed),
		LastUsedAt:           factor.LastUsedAt,
	}, nil
}

// validateTOTP accepts the current step ± 1 to absorb 30s of clock skew each
// side. Kept for the enrollment-confirm path which has no factor row yet
// (nothing to advance). Login/step-up use matchTOTPStep for replay guard.
func validateTOTP(secret, code string) bool {
	valid, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false
	}
	return valid
}

// matchTOTPStep returns the step index (unix / period) whose generated code
// matches the supplied value within the ±1 skew window. Callers use the
// returned step to advance LastUsedStep and prevent replay. ok=false means
// no match in the window.
func matchTOTPStep(secret, code string, now time.Time) (int64, bool) {
	const period = 30
	current := now.Unix() / period
	for _, offset := range [...]int64{0, -1, 1} {
		step := current + offset
		stepTime := time.Unix(step*period, 0)
		candidate, err := totp.GenerateCodeCustom(secret, stepTime, totp.ValidateOpts{
			Period:    period,
			Skew:      0,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			continue
		}
		if subtleConstantTimeEq(candidate, code) {
			return step, true
		}
	}
	return 0, false
}

// subtleConstantTimeEq is a length-safe constant-time compare used by the
// TOTP matcher. stdlib's subtle.ConstantTimeCompare requires equal length;
// we want a consistent "not equal" without short-circuiting on length.
func subtleConstantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		// Still run a comparison so the branch doesn't leak length info
		// beyond what the code-length convention already reveals.
		var x byte
		for i := 0; i < len(a); i++ {
			x |= a[i] ^ a[i] //nolint:gocritic // dupSubExpr: intentional self-XOR keeps loop timing symmetric with the equal-length branch below.
		}
		_ = x
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// recoveryCodesCount returns the configured number of one-shot
// recovery codes to issue at enrollment time. Falls back to the
// legacy BackupCodeCount when the policy is unwired or returns a
// value outside the safe range (1..50). The upper bound prevents a
// misedit from generating thousands of codes on every enrollment.
func (s *mfaService) recoveryCodesCount(ctx context.Context) int {
	if s.policy == nil {
		return BackupCodeCount
	}
	n := s.policy.RecoveryCodesCount(ctx)
	if n < 1 || n > 50 {
		return BackupCodeCount
	}
	return n
}

// generateBackupCodes returns (plaintext, hashed) pairs. Plaintext is shown
// to the user exactly once; only the argon2id hash is persisted.
func (s *mfaService) generateBackupCodes(n int) ([]string, []string, error) {
	codes := make([]string, 0, n)
	hashes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		code, err := generateBackupCode()
		if err != nil {
			return nil, nil, err
		}
		hashed, err := s.passwords.Hash(normaliseBackupCode(code))
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, hashed)
	}
	return codes, hashes, nil
}

// generateBackupCode produces an 8-char uppercase base32 code formatted as
// two dash-separated groups (e.g. "ABCD-EFGH"). Dashes are stripped at
// verification time so the user can type with or without them.
func generateBackupCode() (string, error) {
	buf := make([]byte, 5) // 40 bits → 8 base32 chars
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	enc = strings.ToUpper(enc)
	if len(enc) < 8 {
		return "", fmt.Errorf("unexpected backup code length %d", len(enc))
	}
	return enc[:4] + "-" + enc[4:8], nil
}

func normaliseBackupCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}
