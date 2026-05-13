package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// WebAuthn challenge purposes. Reuses the existing Redis-backed
// MFAChallengeService — only the purpose tag differs from TOTP, the
// PendingSecret field carries the JSON-encoded webauthn.SessionData
// the library produced at Begin time.
const (
	MFAPurposeWebAuthnRegister MFAChallengePurpose = "webauthn_register"
	MFAPurposeWebAuthnVerify   MFAChallengePurpose = "webauthn_verify" // self-service step-up
	MFAPurposeWebAuthnLogin    MFAChallengePurpose = "webauthn_login"  // password→passkey login completion
)

// ErrWebAuthnNoCredentials is returned when a verify/login flow runs but
// the user has not enrolled any passkeys yet. Callers convert to 400.
var ErrWebAuthnNoCredentials = errors.New("user has no webauthn credentials")

// ErrWebAuthnAssertion is returned when the authenticator's response fails
// validation — bad challenge, signature mismatch, or sign-count regression
// past the clone-warning threshold. Callers convert to 401.
var ErrWebAuthnAssertion = errors.New("webauthn assertion failed")

// WebAuthnService orchestrates the W3C ceremonies (registration + login)
// and persists credentials on the existing MFA factor row. The ceremonies
// are two-step (Begin → Finish) because the browser must call
// navigator.credentials.create()/get() between the two halves.
type WebAuthnService interface {
	BeginRegistration(ctx context.Context, user *userModels.User) (challengeID string, options *protocol.CredentialCreation, err error)
	FinishRegistration(ctx context.Context, user *userModels.User, challengeID, name string, attestationJSON []byte) (*authModels.WebAuthnCredential, error)

	BeginAssertion(ctx context.Context, user *userModels.User, purpose MFAChallengePurpose) (challengeID string, options *protocol.CredentialAssertion, err error)
	FinishAssertion(ctx context.Context, user *userModels.User, challengeID string, expectedPurpose MFAChallengePurpose, assertionJSON []byte) error

	ListCredentials(ctx context.Context, userUUID string) ([]authModels.WebAuthnCredential, error)
	RemoveCredential(ctx context.Context, userUUID string, credentialID []byte) (bool, error)
	HasCredentials(ctx context.Context, userUUID string) (bool, error)
}

type webAuthnService struct {
	wa         *webauthn.WebAuthn
	factors    repository.MFAFactorRepository
	challenges MFAChallengeService
	logger     *slog.Logger
}

// NewWebAuthnService builds the orchestrator. The supplied *webauthn.WebAuthn
// must already be configured with the deployment's RP ID + origins; pass nil
// to disable WebAuthn entirely (the module wiring uses this when the env
// vars are unset rather than panicking at boot).
func NewWebAuthnService(
	wa *webauthn.WebAuthn,
	factors repository.MFAFactorRepository,
	challenges MFAChallengeService,
	logger *slog.Logger,
) WebAuthnService {
	if logger == nil {
		logger = slog.Default()
	}
	return &webAuthnService{
		wa:         wa,
		factors:    factors,
		challenges: challenges,
		logger:     logger,
	}
}

func (s *webAuthnService) BeginRegistration(ctx context.Context, user *userModels.User) (string, *protocol.CredentialCreation, error) {
	if s.wa == nil {
		return "", nil, errors.New("webauthn not configured")
	}
	if user == nil || user.UUID == "" {
		return "", nil, errors.New("user is required")
	}

	creds, _ := s.loadCredentials(ctx, user.UUID)
	wu := &webAuthnUser{user: user, credentials: creds}

	// ExcludeCredentials prevents the same authenticator from being
	// registered twice — the browser refuses the prompt if a matching
	// credential is already present. Critical for the per-credential
	// "name" field (otherwise the user could quietly re-enroll under a
	// new name and we'd lose track of which physical device is which).
	excludeOpts := []webauthn.RegistrationOption{
		webauthn.WithExclusions(webauthn.Credentials(toLibCredentials(creds)).CredentialDescriptors()),
	}

	options, sessionData, err := s.wa.BeginRegistration(wu, excludeOpts...)
	if err != nil {
		return "", nil, fmt.Errorf("begin webauthn registration: %w", err)
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return "", nil, fmt.Errorf("marshal webauthn session: %w", err)
	}

	ch, err := s.challenges.Begin(ctx, user.UUID, MFAPurposeWebAuthnRegister, string(sessionJSON))
	if err != nil {
		return "", nil, err
	}
	return ch.ID, options, nil
}

func (s *webAuthnService) FinishRegistration(ctx context.Context, user *userModels.User, challengeID, name string, attestationJSON []byte) (*authModels.WebAuthnCredential, error) {
	if s.wa == nil {
		return nil, errors.New("webauthn not configured")
	}
	if user == nil || user.UUID == "" {
		return nil, errors.New("user is required")
	}
	if name == "" {
		// Fall back to a generic label so the settings UI never shows an
		// empty row. Users can rename via re-enrollment if it matters.
		name = "Passkey"
	}

	ch, err := s.challenges.Peek(ctx, challengeID)
	if err != nil {
		return nil, ErrMFAInvalidCode
	}
	if ch.UserUUID != user.UUID || ch.Purpose != MFAPurposeWebAuthnRegister {
		return nil, ErrMFAChallengeMismatch
	}

	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(ch.PendingSecret), &sessionData); err != nil {
		return nil, fmt.Errorf("unmarshal webauthn session: %w", err)
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(attestationJSON)
	if err != nil {
		_, _ = s.challenges.IncrementAttempts(ctx, challengeID)
		return nil, fmt.Errorf("%w: parse attestation: %v", ErrWebAuthnAssertion, err)
	}

	creds, _ := s.loadCredentials(ctx, user.UUID)
	wu := &webAuthnUser{user: user, credentials: creds}

	libCred, err := s.wa.CreateCredential(wu, sessionData, parsed)
	if err != nil {
		_, _ = s.challenges.IncrementAttempts(ctx, challengeID)
		return nil, fmt.Errorf("%w: %v", ErrWebAuthnAssertion, err)
	}

	// Verified — consume the challenge so its session data can't be reused.
	_, _ = s.challenges.Consume(ctx, challengeID)

	now := time.Now()
	stored := authModels.WebAuthnCredential{
		CredentialID:    libCred.ID,
		PublicKey:       libCred.PublicKey,
		AttestationType: libCred.AttestationType,
		AAGUID:          libCred.Authenticator.AAGUID,
		SignCount:       libCred.Authenticator.SignCount,
		Transports:      transportsToStrings(libCred.Transport),
		UserVerified:    libCred.Flags.UserVerified,
		BackupEligible:  libCred.Flags.BackupEligible,
		BackupState:     libCred.Flags.BackupState,
		Name:            name,
		CreatedAt:       now,
	}
	if err := s.factors.AppendWebAuthnCredential(ctx, user.UUID, stored); err != nil {
		return nil, fmt.Errorf("persist webauthn credential: %w", err)
	}

	s.logger.Info("webauthn credential registered",
		slog.String("userUUID", user.UUID),
		slog.String("credentialName", name),
		slog.Int("totalCredentials", len(creds)+1),
	)
	return &stored, nil
}

// BeginAssertion handles both step-up (verify) and login-completion
// ceremonies. The purpose discriminates so a verify challenge can't be
// satisfied by a login response (or vice-versa) — the two paths mint
// tokens with different shapes downstream.
func (s *webAuthnService) BeginAssertion(ctx context.Context, user *userModels.User, purpose MFAChallengePurpose) (string, *protocol.CredentialAssertion, error) {
	if s.wa == nil {
		return "", nil, errors.New("webauthn not configured")
	}
	if user == nil || user.UUID == "" {
		return "", nil, errors.New("user is required")
	}
	if purpose != MFAPurposeWebAuthnVerify && purpose != MFAPurposeWebAuthnLogin {
		return "", nil, ErrMFAChallengeMismatch
	}

	creds, err := s.loadCredentials(ctx, user.UUID)
	if err != nil || len(creds) == 0 {
		return "", nil, ErrWebAuthnNoCredentials
	}
	wu := &webAuthnUser{user: user, credentials: creds}

	options, sessionData, err := s.wa.BeginLogin(wu)
	if err != nil {
		return "", nil, fmt.Errorf("begin webauthn login: %w", err)
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return "", nil, fmt.Errorf("marshal webauthn session: %w", err)
	}
	ch, err := s.challenges.Begin(ctx, user.UUID, purpose, string(sessionJSON))
	if err != nil {
		return "", nil, err
	}
	return ch.ID, options, nil
}

func (s *webAuthnService) FinishAssertion(ctx context.Context, user *userModels.User, challengeID string, expectedPurpose MFAChallengePurpose, assertionJSON []byte) error {
	if s.wa == nil {
		return errors.New("webauthn not configured")
	}
	if user == nil || user.UUID == "" {
		return errors.New("user is required")
	}

	ch, err := s.challenges.Peek(ctx, challengeID)
	if err != nil {
		return ErrMFAInvalidCode
	}
	if ch.UserUUID != user.UUID || ch.Purpose != expectedPurpose {
		return ErrMFAChallengeMismatch
	}

	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(ch.PendingSecret), &sessionData); err != nil {
		return fmt.Errorf("unmarshal webauthn session: %w", err)
	}

	parsed, err := protocol.ParseCredentialRequestResponseBytes(assertionJSON)
	if err != nil {
		_, _ = s.challenges.IncrementAttempts(ctx, challengeID)
		return fmt.Errorf("%w: parse assertion: %v", ErrWebAuthnAssertion, err)
	}

	creds, err := s.loadCredentials(ctx, user.UUID)
	if err != nil || len(creds) == 0 {
		return ErrWebAuthnNoCredentials
	}
	wu := &webAuthnUser{user: user, credentials: creds}

	libCred, err := s.wa.ValidateLogin(wu, sessionData, parsed)
	if err != nil {
		_, _ = s.challenges.IncrementAttempts(ctx, challengeID)
		return fmt.Errorf("%w: %v", ErrWebAuthnAssertion, err)
	}

	// Verified — consume the challenge before the sign-count update so a
	// crash between persist and consume falls on the safe side (challenge
	// stays alive, can be retried, sign-count not yet bumped).
	_, _ = s.challenges.Consume(ctx, challengeID)

	now := time.Now()
	updated, err := s.factors.UpdateWebAuthnCredential(ctx, user.UUID, libCred.ID, libCred.Authenticator.SignCount, now, libCred.Authenticator.CloneWarning)
	if err != nil {
		// Soft-fail: the assertion was valid, we just couldn't write the
		// counter. Future logins will detect the regression but this one
		// stays valid — refusing here would lock out users on a Mongo
		// blip after an otherwise-successful auth.
		s.logger.Warn("webauthn sign-count update failed",
			slog.String("userUUID", user.UUID),
			slog.String("error", err.Error()),
		)
	} else if !updated {
		s.logger.Warn("webauthn credential vanished mid-assertion",
			slog.String("userUUID", user.UUID),
		)
	}
	if libCred.Authenticator.CloneWarning {
		// CloneWarning = sign count regressed. Don't fail the request —
		// it might be a legitimate authenticator firmware quirk — but
		// emit a structured warning so an operator can investigate.
		s.logger.Warn("webauthn clone-warning",
			slog.String("userUUID", user.UUID),
			slog.Uint64("authenticatorCount", uint64(libCred.Authenticator.SignCount)),
		)
	}
	return nil
}

func (s *webAuthnService) ListCredentials(ctx context.Context, userUUID string) ([]authModels.WebAuthnCredential, error) {
	return s.loadCredentials(ctx, userUUID)
}

func (s *webAuthnService) RemoveCredential(ctx context.Context, userUUID string, credentialID []byte) (bool, error) {
	removed, err := s.factors.RemoveWebAuthnCredential(ctx, userUUID, credentialID)
	if err != nil {
		return false, err
	}
	if removed {
		s.logger.Info("webauthn credential removed",
			slog.String("userUUID", userUUID),
			slog.Int("credentialIDLen", len(credentialID)),
		)
	}
	return removed, nil
}

func (s *webAuthnService) HasCredentials(ctx context.Context, userUUID string) (bool, error) {
	creds, err := s.loadCredentials(ctx, userUUID)
	if err != nil {
		return false, err
	}
	return len(creds) > 0, nil
}

func (s *webAuthnService) loadCredentials(ctx context.Context, userUUID string) ([]authModels.WebAuthnCredential, error) {
	doc, err := s.factors.FindByUserAndType(ctx, userUUID, authModels.MFAFactorWebAuthn)
	if err != nil {
		if errors.Is(err, repository.ErrMFAFactorNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return doc.WebAuthnCredentials, nil
}

// --- adapters between our stored shape and the go-webauthn library ---

// webAuthnUser bridges *userModels.User into the webauthn.User interface
// the library expects. WebAuthnID must be a stable, opaque user handle —
// we use the UUID bytes which are already 36 chars (well under the 64-byte
// max) and never reused across users.
type webAuthnUser struct {
	user        *userModels.User
	credentials []authModels.WebAuthnCredential
}

func (u *webAuthnUser) WebAuthnID() []byte   { return []byte(u.user.UUID) }
func (u *webAuthnUser) WebAuthnName() string { return u.user.Email }
func (u *webAuthnUser) WebAuthnDisplayName() string {
	if u.user.FullName != "" {
		return u.user.FullName
	}
	return u.user.Email
}
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return toLibCredentials(u.credentials)
}

func toLibCredentials(creds []authModels.WebAuthnCredential) []webauthn.Credential {
	out := make([]webauthn.Credential, len(creds))
	for i, c := range creds {
		out[i] = webauthn.Credential{
			ID:              c.CredentialID,
			PublicKey:       c.PublicKey,
			AttestationType: c.AttestationType,
			Transport:       stringsToTransports(c.Transports),
			Flags: webauthn.CredentialFlags{
				UserPresent:    true,
				UserVerified:   c.UserVerified,
				BackupEligible: c.BackupEligible,
				BackupState:    c.BackupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:       c.AAGUID,
				SignCount:    c.SignCount,
				CloneWarning: c.CloneWarning,
			},
		}
	}
	return out
}

func transportsToStrings(t []protocol.AuthenticatorTransport) []string {
	out := make([]string, len(t))
	for i, v := range t {
		out[i] = string(v)
	}
	return out
}

func stringsToTransports(s []string) []protocol.AuthenticatorTransport {
	out := make([]protocol.AuthenticatorTransport, len(s))
	for i, v := range s {
		out[i] = protocol.AuthenticatorTransport(v)
	}
	return out
}
