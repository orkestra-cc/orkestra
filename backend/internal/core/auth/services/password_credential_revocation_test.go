package services

// Security regression tests for what a password change / reset actually
// invalidates.
//
// The two flows were asymmetric and both incomplete:
//
//   - ResetPassword — the "my account was compromised" recovery flow —
//     revoked refresh tokens only. The attacker's access token stayed
//     valid for its full TTL, their session doc stayed IsActive, and
//     their device-trust grant survived, letting them skip the MFA
//     prompt on the next login.
//   - ChangePassword revoked the CALLER's own session id and nothing
//     else, so the user who just changed their password was signed out
//     of the tab they were sitting in while every other device — the
//     ones a password change exists to evict — stayed live.
//
// Both now route through revokeSessionsAfterCredentialChange, which
// closes every pathway: refresh tokens, session docs, the Redis sid
// revocation set, and device-trust grants. ChangePassword keeps only the
// caller's own session, which it may do because the caller just proved
// knowledge of the current password.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- fakes ------------------------------------------------------------

// revocationRefreshRepo records which revoke primitive was called for
// whom. Embedding the interface keeps the fake to the methods this test
// actually drives; anything else panics loudly if a future change starts
// calling it.
type revocationRefreshRepo struct {
	repository.RefreshTokenRepository
	mu             sync.Mutex
	revokedByUser  []string
	revokedBySess  []string
	createdSession []string
	byUserErr      error
	bySessionErr   error
	reasons        []string
	tokens         []*authModels.RefreshTokenDoc
	listErr        error
	sessionErrors  map[string]error
}

func (r *revocationRefreshRepo) RevokeTokensByUser(_ context.Context, userUUID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revokedByUser = append(r.revokedByUser, userUUID)
	r.reasons = append(r.reasons, reason)
	if r.byUserErr == nil {
		for _, token := range r.tokens {
			if token.UserUUID == userUUID {
				token.IsRevoked = true
				token.RevokedReason = reason
			}
		}
	}
	return r.byUserErr
}

func (r *revocationRefreshRepo) RevokeTokensBySession(_ context.Context, sessionUUID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revokedBySess = append(r.revokedBySess, sessionUUID)
	r.reasons = append(r.reasons, reason)
	if err := r.sessionErrors[sessionUUID]; err != nil {
		return err
	}
	if r.bySessionErr != nil {
		return r.bySessionErr
	}
	for _, token := range r.tokens {
		if token.SessionUUID == sessionUUID {
			token.IsRevoked = true
			token.RevokedReason = reason
		}
	}
	return nil
}

func (r *revocationRefreshRepo) GetActiveTokensByUser(_ context.Context, userUUID string) ([]*authModels.RefreshTokenDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	var tokens []*authModels.RefreshTokenDoc
	for _, token := range r.tokens {
		if token.UserUUID == userUUID && !token.IsRevoked && token.ExpiresAt.After(time.Now()) {
			cp := *token
			tokens = append(tokens, &cp)
		}
	}
	return tokens, nil
}

func (r *revocationRefreshRepo) CreateRefreshToken(_ context.Context, doc *authModels.RefreshTokenDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createdSession = append(r.createdSession, doc.SessionUUID)
	return nil
}

func (r *revocationRefreshRepo) snapshot() (byUser, bySess []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.revokedByUser...), append([]string(nil), r.revokedBySess...)
}

// revocationDeviceTrust records RevokeAllByUser.
type revocationDeviceTrust struct {
	DeviceTrustService
	mu      sync.Mutex
	revoked []string
	reasons []string
	err     error
}

func (d *revocationDeviceTrust) RevokeAllByUser(_ context.Context, userUUID, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.revoked = append(d.revoked, userUUID)
	d.reasons = append(d.reasons, reason)
	return d.err
}

type failingCredentialSessions struct {
	*fakeAuthSessionRepo
	listErr      error
	terminateErr error
	attempted    []string
}

type credentialSIDRevocation struct {
	*fakeSessionRevocation
	reasons   []string
	sidErrors map[string]error
}

func (r *credentialSIDRevocation) Revoke(ctx context.Context, sid, reason string) error {
	r.reasons = append(r.reasons, reason)
	err := r.fakeSessionRevocation.Revoke(ctx, sid, reason)
	if sidErr := r.sidErrors[sid]; sidErr != nil {
		return sidErr
	}
	return err
}

func (r *failingCredentialSessions) GetActiveSessionsByUser(ctx context.Context, userUUID string) ([]*authModels.AuthSessionDoc, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.fakeAuthSessionRepo.GetActiveSessionsByUser(ctx, userUUID)
}

func (r *failingCredentialSessions) TerminateSession(ctx context.Context, sid string) error {
	r.attempted = append(r.attempted, sid)
	if r.terminateErr != nil {
		return r.terminateErr
	}
	return r.fakeAuthSessionRepo.TerminateSession(ctx, sid)
}

func (d *revocationDeviceTrust) revokedList() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.revoked...)
}

// revocationEmailTokens serves a single reset token by hash.
type revocationEmailTokens struct {
	doc  *authModels.EmailTokenDoc
	used []string
}

func (e *revocationEmailTokens) Create(context.Context, *authModels.EmailTokenDoc) error { return nil }

func (e *revocationEmailTokens) GetByHash(_ context.Context, hash string) (*authModels.EmailTokenDoc, error) {
	if e.doc != nil && e.doc.TokenHash == hash {
		return e.doc, nil
	}
	return nil, errNotFound
}

func (e *revocationEmailTokens) MarkUsed(_ context.Context, hash string) error {
	e.used = append(e.used, hash)
	return nil
}

func (e *revocationEmailTokens) InvalidateByUserAndPurpose(context.Context, string, string) error {
	return nil
}

func (e *revocationEmailTokens) DeleteAllByUser(context.Context, string) (int64, error) {
	return 0, nil
}

// --- harness ----------------------------------------------------------

type credentialRevocationEnv struct {
	t        *testing.T
	svc      *PasswordAuthService
	users    *gateUserFake
	refresh  *revocationRefreshRepo
	sessions *fakeAuthSessionRepo
	revoker  *credentialSIDRevocation
	trust    *revocationDeviceTrust
	tokens   *revocationEmailTokens
	pwd      PasswordService
}

func newCredentialRevocationEnv(t *testing.T) *credentialRevocationEnv {
	t.Helper()
	env := &credentialRevocationEnv{
		t:        t,
		users:    newGateUserFake(),
		refresh:  &revocationRefreshRepo{},
		sessions: newFakeAuthSessionRepo(),
		revoker:  &credentialSIDRevocation{fakeSessionRevocation: &fakeSessionRevocation{}},
		trust:    &revocationDeviceTrust{},
		tokens:   &revocationEmailTokens{},
		pwd:      NewPasswordService(silentLogger(), false),
	}
	env.svc = NewPasswordAuthService(PasswordAuthConfig{
		UserService:      env.users,
		TenantProvider:   gateTenantProvider{},
		PasswordService:  env.pwd,
		EmailTokenRepo:   env.tokens,
		RefreshTokenRepo: env.refresh,
		AuthSessionRepo:  env.sessions,
		DeviceTrust:      env.trust,
		Logger:           silentLogger(),
	})
	env.svc.SetSessionRevocation(env.revoker)
	return env
}

// userWithSessions provisions a user holding a real argon2id hash plus
// the given active sessions.
func (e *credentialRevocationEnv) userWithSessions(password string, sids ...string) *iface.User {
	e.t.Helper()
	hash, err := e.pwd.Hash(password)
	if err != nil {
		e.t.Fatalf("hash: %v", err)
	}
	u := &iface.User{
		UUID:         "u-1",
		Email:        "victim@example.com",
		Role:         "operator",
		IsActive:     true,
		PasswordHash: hash,
	}
	e.users.seed(u)
	for _, sid := range sids {
		e.sessions.seed(&authModels.AuthSessionDoc{
			UUID:      sid,
			UserUUID:  u.UUID,
			IsActive:  true,
			ExpiresAt: time.Now().Add(24 * time.Hour),
		})
	}
	return u
}

// seedResetToken registers a usable reset token and returns its raw form.
func (e *credentialRevocationEnv) seedResetToken(userUUID string) string {
	e.t.Helper()
	raw, hash, err := generateEmailToken()
	if err != nil {
		e.t.Fatalf("generateEmailToken: %v", err)
	}
	e.tokens.doc = &authModels.EmailTokenDoc{
		UserUUID:  userUUID,
		TokenHash: hash,
		Purpose:   authModels.EmailTokenPurposeResetPassword,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	return raw
}

// --- ResetPassword ----------------------------------------------------

func TestResetPassword_RevokesEveryCredentialPathway(t *testing.T) {
	env := newCredentialRevocationEnv(t)
	user := env.userWithSessions("correct-horse-battery", "sess-attacker", "sess-owner")
	raw := env.seedResetToken(user.UUID)

	if err := env.svc.ResetPassword(context.Background(), raw, "brand-new-passphrase"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	byUser, _ := env.refresh.snapshot()
	if !contains(byUser, user.UUID) {
		t.Error("refresh tokens must be revoked on password reset")
	}

	// Every session doc must be terminated — a reset is a recovery
	// action, there is no session worth preserving.
	for _, sid := range []string{"sess-attacker", "sess-owner"} {
		if !contains(env.sessions.terminated, sid) {
			t.Errorf("session %s must be terminated on password reset", sid)
		}
		if !contains(env.revoker.revokedList(), sid) {
			t.Errorf("session %s must be pushed to the sid revocation set so in-flight access tokens die", sid)
		}
	}

	if !contains(env.trust.revokedList(), user.UUID) {
		t.Error("device-trust grants must be revoked on password reset, otherwise the attacker keeps skipping MFA")
	}
}

// --- ChangePassword ---------------------------------------------------

func TestChangePassword_RevokesOtherSessionsAndKeepsCaller(t *testing.T) {
	env := newCredentialRevocationEnv(t)
	user := env.userWithSessions("correct-horse-battery", "sess-caller", "sess-other")

	err := env.svc.ChangePassword(context.Background(), ChangePasswordInput{
		UserUUID:   user.UUID,
		CurrentSID: "sess-caller",
		Current:    "correct-horse-battery",
		New:        "brand-new-passphrase",
	})
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if !contains(env.sessions.terminated, "sess-other") {
		t.Error("other devices must be signed out by a password change")
	}
	if !contains(env.revoker.revokedList(), "sess-other") {
		t.Error("the other session's sid must be revoked so its access token dies immediately")
	}

	// The caller just proved the current password — signing them out of
	// their own tab is the behaviour this replaces, not the goal.
	if contains(env.sessions.terminated, "sess-caller") {
		t.Error("the caller's own session must survive their own password change")
	}
	if contains(env.revoker.revokedList(), "sess-caller") {
		t.Error("the caller's own sid must not be revoked")
	}

	if !contains(env.trust.revokedList(), user.UUID) {
		t.Error("device-trust grants must be revoked on password change")
	}
}

func TestChangePassword_WrongCurrentPasswordRevokesNothing(t *testing.T) {
	env := newCredentialRevocationEnv(t)
	user := env.userWithSessions("correct-horse-battery", "sess-caller", "sess-other")

	err := env.svc.ChangePassword(context.Background(), ChangePasswordInput{
		UserUUID:   user.UUID,
		CurrentSID: "sess-caller",
		Current:    "wrong-password",
		New:        "brand-new-passphrase",
	})
	if err == nil {
		t.Fatal("a wrong current password must be rejected")
	}

	if len(env.sessions.terminated) != 0 {
		t.Errorf("a failed password change must not terminate sessions, got %v", env.sessions.terminated)
	}
	if len(env.revoker.revokedList()) != 0 {
		t.Errorf("a failed password change must not revoke sids, got %v", env.revoker.revokedList())
	}
	if len(env.trust.revokedList()) != 0 {
		t.Error("a failed password change must not drop device trust")
	}
}

func TestCredentialRevocation_ReportsFailuresAndContinues(t *testing.T) {
	for _, stage := range []string{"list", "refresh_session", "terminate", "sid", "refresh_user", "trust"} {
		t.Run(stage, func(t *testing.T) {
			env := newCredentialRevocationEnv(t)
			user := env.userWithSessions("correct-horse-battery", "sess-one", "sess-two")
			sessions := &failingCredentialSessions{fakeAuthSessionRepo: env.sessions}
			env.svc.authSessionRepo = sessions
			outage := errors.New("injected teardown failure")
			switch stage {
			case "list":
				sessions.listErr = outage
			case "refresh_session":
				env.refresh.bySessionErr = outage
			case "terminate":
				sessions.terminateErr = outage
			case "sid":
				env.revoker.err = outage
			case "refresh_user":
				env.refresh.byUserErr = outage
			case "trust":
				env.trust.err = outage
			}
			// Failure status must not depend on whether a logger is wired.
			env.svc.logger = nil
			result := env.svc.revokeSessionsAfterCredentialChange(context.Background(), user.UUID, "password_reset", "", "password_reset")
			if result.Complete {
				t.Fatal("a failed teardown stage must report incomplete")
			}
			wantRevoked := 0
			if stage == "refresh_user" || stage == "trust" {
				wantRevoked = 2
			}
			if result.Revoked != wantRevoked {
				t.Fatalf("Revoked=%d, want %d completed per-session revocations", result.Revoked, wantRevoked)
			}
			byUser, bySession := env.refresh.snapshot()
			if len(byUser) != 1 || len(env.trust.revokedList()) != 1 {
				t.Fatal("by-user refresh sweep and device trust must still be attempted")
			}
			if stage != "list" && (len(bySession) != 2 || len(sessions.attempted) != 2 || len(env.revoker.revokedList()) != 2) {
				t.Fatal("failure must not stop later stages or later sessions")
			}
		})
	}
}
