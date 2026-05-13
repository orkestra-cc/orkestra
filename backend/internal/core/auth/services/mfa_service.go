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
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/utils"
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
	BeginEnrollment(ctx context.Context, user *userModels.User) (*MFAEnrollmentBegin, error)
	ConfirmEnrollment(ctx context.Context, userUUID, challengeID, code string) ([]string, error)

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
}

type mfaService struct {
	factors     repository.MFAFactorRepository
	challenges  MFAChallengeService
	passwords   PasswordService    // reused for argon2id hashing of backup codes
	deviceTrust DeviceTrustService // optional — see SetDeviceTrust
	issuer      string
	logger      *slog.Logger
	policy      *AuthPolicyService // optional — Phase 10 backup-code count
}

// SetDeviceTrust wires the optional device-trust service. Called
// post-construction from module.go so the construction graph stays
// free of a cross-service dependency.
func (s *mfaService) SetDeviceTrust(dt DeviceTrustService) { s.deviceTrust = dt }

// SetPolicy wires the optional auth-policy reader. Phase 10 of the
// auth-policy roadmap — used today only by backup-code generation
// (recoveryCodesCount). Safe to call multiple times.
func (s *mfaService) SetPolicy(p *AuthPolicyService) { s.policy = p }

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

func (s *mfaService) BeginEnrollment(ctx context.Context, user *userModels.User) (*MFAEnrollmentBegin, error) {
	if user == nil || user.UUID == "" {
		return nil, fmt.Errorf("user is required")
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

func (s *mfaService) ConfirmEnrollment(ctx context.Context, userUUID, challengeID, code string) ([]string, error) {
	if userUUID == "" || challengeID == "" || code == "" {
		return nil, fmt.Errorf("userUUID, challengeID, and code are required")
	}

	// Peek before consuming so we don't destroy a valid challenge on a typo.
	ch, err := s.challenges.Peek(ctx, challengeID)
	if err != nil {
		return nil, ErrMFAInvalidCode
	}
	if ch.UserUUID != userUUID || ch.Purpose != MFAPurposeEnroll {
		return nil, ErrMFAChallengeMismatch
	}

	if !validateTOTP(ch.PendingSecret, code) {
		attempts, _ := s.challenges.IncrementAttempts(ctx, challengeID)
		_ = attempts
		return nil, ErrMFAInvalidCode
	}

	// Consume the challenge now — verified, about to persist the factor.
	_, _ = s.challenges.Consume(ctx, challengeID)

	// If a factor already exists for this user+type (enrollment repeated),
	// replace it. The unique index on (userUuid, type) guarantees we never
	// have duplicates in flight.
	if existing, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP); err == nil && existing != nil {
		_ = s.factors.Delete(ctx, existing.UUID)
	}

	secretEnc, err := utils.EncryptMFASecret(ch.PendingSecret)
	if err != nil {
		return nil, fmt.Errorf("encrypt totp secret: %w", err)
	}

	plaintextCodes, hashedCodes, err := s.generateBackupCodes(s.recoveryCodesCount(ctx))
	if err != nil {
		return nil, fmt.Errorf("generate backup codes: %w", err)
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
		return nil, fmt.Errorf("persist mfa factor: %w", err)
	}

	s.logger.Info("mfa enrollment confirmed",
		slog.String("userUUID", userUUID),
		slog.String("factorUUID", doc.UUID),
	)
	return plaintextCodes, nil
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

func (s *mfaService) RemoveFactor(ctx context.Context, userUUID, actorUUID string) error {
	factor, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP)
	if err != nil {
		if errors.Is(err, repository.ErrMFAFactorNotFound) {
			return ErrMFANotEnrolled
		}
		return err
	}
	if err := s.factors.Delete(ctx, factor.UUID); err != nil {
		return err
	}
	// Section C item #3: removing the MFA factor also invalidates every
	// "remember this device" grant the user holds. A trust row carries
	// an amr annotation that claims the factor was verified — once the
	// factor is gone, that annotation is a lie. User-initiated removal
	// and admin reset both flow through here; the revoke reason
	// distinguishes them (actorUUID != userUUID indicates admin).
	if s.deviceTrust != nil {
		reason := models.DeviceTrustRevokedOnMFARemove
		if actorUUID != "" && actorUUID != userUUID {
			reason = models.DeviceTrustRevokedOnAdminReset
		}
		if err := s.deviceTrust.RevokeAllByUser(ctx, userUUID, reason); err != nil {
			s.logger.Warn("device_trust: revoke on factor removal failed",
				slog.String("user_uuid", userUUID),
				slog.String("error", err.Error()))
		}
	}
	s.logger.Info("mfa factor removed",
		slog.String("userUUID", userUUID),
		slog.String("actorUUID", actorUUID),
		slog.String("factorUUID", factor.UUID),
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
	s.logger.Info("self_auth_action",
		"event", "self_backup_codes_regenerated",
		"userUUID", userUUID,
		"count", len(plaintext),
	)
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
