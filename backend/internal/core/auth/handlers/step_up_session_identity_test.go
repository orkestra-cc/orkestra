package handlers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type stepUpUsers struct {
	iface.UserProvider
	user *iface.User
}

func (u *stepUpUsers) GetUserByID(context.Context, string) (*iface.User, error) {
	return u.user, nil
}

func (u *stepUpUsers) UpdateUserLastLogin(context.Context, string) error { return nil }

// ClearFailedLogins is now called on a successful ConfirmPasswordWithSecurity
// (Task 9 fix round, Finding 2 — a success clears the durable counter too,
// not just the AttemptCounter's email scope). This fixture only ever
// embeds a nil iface.UserProvider for the methods it doesn't override, so
// without this the success path in
// TestStepUpSessionIdentity_PasswordConfirmPreservesSID panics on a nil
// method value instead of exercising the session-identity assertion the
// test is actually about.
func (u *stepUpUsers) ClearFailedLogins(context.Context, string) error { return nil }

type stepUpMFA struct{ services.MFAService }

func (stepUpMFA) Verify(context.Context, string, string) error { return nil }

type stepUpWebAuthn struct{ services.WebAuthnService }

func (stepUpWebAuthn) FinishAssertion(context.Context, *iface.User, string, services.MFAChallengePurpose, []byte) error {
	return nil
}

type oneWinnerWebAuthn struct {
	services.WebAuthnService
	used sync.Map
}

func (w *oneWinnerWebAuthn) FinishAssertion(_ context.Context, _ *iface.User, challengeID string, _ services.MFAChallengePurpose, _ []byte) error {
	if _, loaded := w.used.LoadOrStore(challengeID, struct{}{}); loaded {
		return services.ErrMFAInvalidCode
	}
	return nil
}

type stepUpRefreshRepo struct {
	repository.RefreshTokenRepository
	created *authModels.RefreshTokenDoc
}

func (r *stepUpRefreshRepo) CreateRefreshToken(_ context.Context, doc *authModels.RefreshTokenDoc) error {
	copy := *doc
	r.created = &copy
	return nil
}

type stepUpSessionRepo struct {
	repository.AuthSessionRepository
	created *authModels.AuthSessionDoc
}

type countingLoginTokenIssuer struct{ calls atomic.Int32 }

func (i *countingLoginTokenIssuer) IssueLoginTokensForSession(_ context.Context, _ *iface.User, in services.LoginTokenContext, _ []string, _ int64) (*authModels.TokenResponse, error) {
	i.calls.Add(1)
	return &authModels.TokenResponse{AccessToken: "winner", TokenType: "Bearer", SessionID: in.SessionID}, nil
}

func (i *countingLoginTokenIssuer) EmitBreakGlassUsed(context.Context, string, string, string, string) {
}

// passwordLoginOnPolicy mirrors production wiring for these completion
// fixtures: both handlers get a policy service, and it says password login
// is on for the operator surface. Without one, a password-sourced challenge
// fails closed at the spec §4.3 re-check (nil policy = outage), which is the
// intended behaviour but not what these session-identity tests are about.
func passwordLoginOnPolicy() *services.AuthPolicyService {
	return services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"})
}

func (r *stepUpSessionRepo) GetDeviceSessionHistory(context.Context, string, string, int) ([]*authModels.AuthSessionDoc, error) {
	return nil, nil
}

func (r *stepUpSessionRepo) CreateSession(_ context.Context, doc *authModels.AuthSessionDoc) error {
	copy := *doc
	r.created = &copy
	return nil
}

func newPendingSessionTokenIssuer(t *testing.T, jwt services.JWTService, user *iface.User) (*services.PasswordAuthService, *stepUpRefreshRepo, *stepUpSessionRepo) {
	t.Helper()
	refresh := &stepUpRefreshRepo{}
	sessions := &stepUpSessionRepo{}
	svc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService:      &stepUpUsers{user: user},
		JWTService:       jwt,
		RefreshTokenRepo: refresh,
		AuthSessionRepo:  sessions,
	})
	return svc, refresh, sessions
}

func newStepUpJWT(t *testing.T) services.JWTService {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	jwt, err := services.NewJWTServiceWithAudience(key, &key.PublicKey, "test", services.AudienceOperator, 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	return jwt
}

func stepUpContext(userUUID string) context.Context {
	ctx := context.WithValue(context.Background(), "userUUID", userUUID)
	return context.WithValue(ctx, "claims", &authModels.JWTClaims{
		UserUUID:    userUUID,
		SessionID:   "session-step-up",
		DeviceID:    "device-step-up",
		IPAddress:   "198.51.100.24",
		Fingerprint: "fingerprint-step-up",
		RiskScore:   0.42,
		AMR:         []string{"pwd"},
	})
}

func assertStepUpSession(t *testing.T, jwt services.JWTService, token, wantAMR string) {
	t.Helper()
	claims, err := jwt.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.SessionID != "session-step-up" {
		t.Errorf("sid = %q, want session-step-up", claims.SessionID)
	}
	if claims.DeviceID != "device-step-up" {
		t.Errorf("did = %q, want device-step-up", claims.DeviceID)
	}
	if claims.IPAddress != "198.51.100.24" {
		t.Errorf("ip = %q, want 198.51.100.24", claims.IPAddress)
	}
	if claims.Fingerprint != "fingerprint-step-up" {
		t.Errorf("fp = %q, want fingerprint-step-up", claims.Fingerprint)
	}
	if claims.RiskScore != 0.42 {
		t.Errorf("risk = %v, want 0.42", claims.RiskScore)
	}
	found := false
	for _, method := range claims.AMR {
		if method == wantAMR {
			found = true
		}
	}
	if !found {
		t.Errorf("amr = %v, want %q", claims.AMR, wantAMR)
	}
	if claims.LastOTPAt == 0 {
		t.Error("last_otp_at was not refreshed")
	}
}

func TestStepUpSessionIdentity_TOTPPreservesSID(t *testing.T) {
	jwt := newStepUpJWT(t)
	user := &iface.User{UUID: "step-up-user", Email: "step-up@example.com", Role: "operator", IsActive: true}
	h := NewMFAHandler(stepUpMFA{}, nil, jwt, &stepUpUsers{user: user}, nil, "", "", false)
	req := &MFAVerifyRequest{}
	req.Body.Code = "123456"

	response, err := h.Verify(stepUpContext(user.UUID), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	assertStepUpSession(t, jwt, response.Body.AccessToken, "otp")
}

func TestStepUpSessionIdentity_WebAuthnPreservesSID(t *testing.T) {
	jwt := newStepUpJWT(t)
	user := &iface.User{UUID: "step-up-user", Email: "step-up@example.com", Role: "operator", IsActive: true}
	h := NewWebAuthnHandler(stepUpWebAuthn{}, nil, jwt, &stepUpUsers{user: user}, nil, "", "", false)
	req := &webAuthnVerifyFinishRequest{}
	req.Body.ChallengeID = "challenge-step-up"
	req.Body.AssertionResponse = json.RawMessage(`{"id":"credential"}`)

	response, err := h.VerifyFinish(stepUpContext(user.UUID), req)
	if err != nil {
		t.Fatalf("VerifyFinish: %v", err)
	}
	assertStepUpSession(t, jwt, response.Body.AccessToken, "webauthn")
}

func TestStepUpSessionIdentity_PasswordConfirmPreservesSID(t *testing.T) {
	jwt := newStepUpJWT(t)
	passwords := services.NewPasswordService(nil, false)
	hash, err := passwords.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	user := &iface.User{
		UUID:         "step-up-user",
		Email:        "step-up@example.com",
		Role:         "operator",
		IsActive:     true,
		PasswordHash: hash,
	}
	svc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService:     &stepUpUsers{user: user},
		PasswordService: passwords,
		JWTService:      jwt,
		// PR 3 §4.6: the reconfirm now reads the per-surface password
		// policy, and an unreadable one is an outage (503) rather than a
		// silent allow — so this fixture must carry the same Policy +
		// Audience pair production wiring always supplies. Empty values =
		// key absent = password login enabled, which is the state this
		// test is about: a SUCCESSFUL reconfirm preserving the session
		// identity.
		Policy:   services.NewAuthPolicyServiceForTest(nil),
		Audience: services.PolicyAudienceOperator,
	})
	h := NewPasswordAuthHandler(svc, "", "", false)
	req := &PasswordConfirmRequest{}
	req.Body.Password = "correct-horse-battery"

	response, err := h.PasswordConfirm(stepUpContext(user.UUID), req)
	if err != nil {
		t.Fatalf("PasswordConfirm: %v", err)
	}
	assertStepUpSession(t, jwt, response.Body.AccessToken, "reauth")
}

func TestStepUpSessionIdentity_MissingSIDFailsClosed(t *testing.T) {
	ctx := context.WithValue(context.Background(), "userUUID", "legacy-user")
	ctx = context.WithValue(ctx, "claims", &authModels.JWTClaims{UserUUID: "legacy-user", DeviceID: "legacy-device"})

	t.Run("totp", func(t *testing.T) {
		req := &MFAVerifyRequest{}
		req.Body.Code = "123456"
		_, err := (&MFAHandler{}).Verify(ctx, req)
		if got := statusOf(t, err); got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})
	t.Run("webauthn", func(t *testing.T) {
		req := &webAuthnVerifyFinishRequest{}
		req.Body.AssertionResponse = json.RawMessage(`{"id":"credential"}`)
		_, err := (&WebAuthnHandler{}).VerifyFinish(ctx, req)
		if got := statusOf(t, err); got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})
	t.Run("password confirm", func(t *testing.T) {
		req := &PasswordConfirmRequest{}
		req.Body.Password = "correct-horse-battery"
		_, err := (&PasswordAuthHandler{}).PasswordConfirm(ctx, req)
		if got := statusOf(t, err); got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})
}

func TestStepUpSessionIdentity_TOTPLoginCompletionPreservesPendingSID(t *testing.T) {
	jwt := newStepUpJWT(t)
	user := &iface.User{UUID: "pending-user", Email: "pending@example.com", Role: "operator", IsActive: true}
	tokens, refresh, sessions := newPendingSessionTokenIssuer(t, jwt, user)
	challenges := services.NewMFAChallengeService(services.NewMemoryOAuthStateStore())
	challenge, err := challenges.BeginLogin(context.Background(), services.LoginChallengeInput{
		UserUUID:    user.UUID,
		SessionID:   "session-pending-mfa",
		SourceAMR:   []string{"oauth"},
		DeviceID:    "device-pending",
		DeviceType:  "mobile",
		Platform:    "web",
		Fingerprint: "fingerprint-pending",
		RiskScore:   0.73,
		RiskFactors: []string{"new_device", "proxy"},
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	h := NewMFAHandler(stepUpMFA{}, challenges, jwt, &stepUpUsers{user: user}, tokens, "", "", false)
	req := &MFALoginVerifyRequest{}
	req.Body.ChallengeID = challenge.ID
	req.Body.Code = "123456"

	response, err := h.LoginVerify(context.Background(), req)
	if err != nil {
		t.Fatalf("LoginVerify: %v", err)
	}
	claims, err := jwt.ValidateAccessToken(response.Body.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	for name, got := range map[string]string{
		"access claim": claims.SessionID,
		"response":     response.Body.SessionID,
		"refresh row":  refresh.created.SessionUUID,
		"session row":  sessions.created.UUID,
	} {
		if got != "session-pending-mfa" {
			t.Errorf("%s sid = %q, want session-pending-mfa", name, got)
		}
	}
	if got := refresh.created.DeviceType; got != "mobile" {
		t.Errorf("refresh device type = %q, want mobile", got)
	}
	if got := refresh.created.Fingerprint; got != "fingerprint-pending" {
		t.Errorf("refresh fingerprint = %q, want fingerprint-pending", got)
	}
	if got := refresh.created.RiskScore; got != 0.73 {
		t.Errorf("refresh risk = %v, want 0.73", got)
	}
	if got := sessions.created.LoginMethod; got != "oauth" {
		t.Errorf("session login method = %q, want oauth", got)
	}
	if !sessions.created.MFACompleted {
		t.Error("completed OAuth+MFA session is not marked MFACompleted")
	}
	if got := sessions.created.DeviceInfo.Fingerprint; got != "fingerprint-pending" {
		t.Errorf("session fingerprint = %q, want fingerprint-pending", got)
	}
	if got := sessions.created.RiskScore; got != 0.73 {
		t.Errorf("session risk = %v, want 0.73", got)
	}
	if got := claims.Fingerprint; got != "fingerprint-pending" {
		t.Errorf("completed access-token fingerprint = %q, want fingerprint-pending", got)
	}
	if got := claims.RiskScore; got != 0.73 {
		t.Errorf("completed access-token risk = %v, want 0.73", got)
	}
	if !containsString(claims.AMR, "oauth") || !containsString(claims.AMR, "otp") || containsString(claims.AMR, "pwd") {
		t.Errorf("completed access-token amr = %v, want oauth+otp without pwd", claims.AMR)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestStepUpSessionIdentity_WebAuthnLoginCompletionPreservesPendingSID(t *testing.T) {
	jwt := newStepUpJWT(t)
	user := &iface.User{UUID: "pending-user", Email: "pending@example.com", Role: "operator", IsActive: true}
	tokens, refresh, sessions := newPendingSessionTokenIssuer(t, jwt, user)
	challenges := services.NewMFAChallengeService(services.NewMemoryOAuthStateStore())
	loginChallenge, err := challenges.BeginLogin(context.Background(), services.LoginChallengeInput{
		UserUUID:  user.UUID,
		SessionID: "session-pending-webauthn",
		SourceAMR: []string{"pwd"},
		DeviceID:  "device-pending",
		Platform:  "web",
		Audience:  "operator",
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	h := NewWebAuthnHandler(stepUpWebAuthn{}, challenges, jwt, &stepUpUsers{user: user}, tokens, "", "", false)
	h.SetPolicy(passwordLoginOnPolicy())
	req := &webAuthnLoginFinishRequest{}
	req.Body.LoginChallengeID = loginChallenge.ID
	req.Body.WebAuthnChallengeID = "webauthn-pending"
	req.Body.AssertionResponse = json.RawMessage(`{"id":"credential"}`)

	response, err := h.LoginFinish(context.Background(), req)
	if err != nil {
		t.Fatalf("LoginFinish: %v", err)
	}
	claims, err := jwt.ValidateAccessToken(response.Body.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	for name, got := range map[string]string{
		"access claim": claims.SessionID,
		"response":     response.Body.SessionID,
		"refresh row":  refresh.created.SessionUUID,
		"session row":  sessions.created.UUID,
	} {
		if got != "session-pending-webauthn" {
			t.Errorf("%s sid = %q, want session-pending-webauthn", name, got)
		}
	}
}

func TestMFALoginVerify_ConcurrentReplayMintsExactlyOnce(t *testing.T) {
	user := &iface.User{UUID: "concurrent-user", Email: "concurrent@example.com", Role: "operator", IsActive: true}
	challenges := services.NewMFAChallengeService(services.NewMemoryOAuthStateStore())
	challenge, err := challenges.BeginLogin(context.Background(), services.LoginChallengeInput{
		UserUUID: user.UUID, SessionID: "session-concurrent", SourceAMR: []string{"pwd"}, Audience: "operator",
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	issuer := &countingLoginTokenIssuer{}
	h := NewMFAHandler(stepUpMFA{}, challenges, newStepUpJWT(t), &stepUpUsers{user: user}, issuer, "", "", false)
	h.SetPolicy(passwordLoginOnPolicy())

	const callers = 24
	start := make(chan struct{})
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := &MFALoginVerifyRequest{}
			req.Body.ChallengeID = challenge.ID
			req.Body.Code = "123456"
			if _, err := h.LoginVerify(context.Background(), req); err == nil {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Errorf("successful responses = %d, want 1", got)
	}
	if got := issuer.calls.Load(); got != 1 {
		t.Errorf("token issuance calls = %d, want 1", got)
	}
}

func TestWebAuthnVerifyFinish_ReplayedCeremonyCannotMintAgain(t *testing.T) {
	jwt := newStepUpJWT(t)
	user := &iface.User{UUID: "step-up-user", Email: "step-up@example.com", Role: "operator", IsActive: true}
	h := NewWebAuthnHandler(&oneWinnerWebAuthn{}, nil, jwt, &stepUpUsers{user: user}, nil, "", "", false)
	req := &webAuthnVerifyFinishRequest{}
	req.Body.ChallengeID = "ceremony-step-up-one-winner"
	req.Body.AssertionResponse = json.RawMessage(`{"id":"credential"}`)

	if _, err := h.VerifyFinish(stepUpContext(user.UUID), req); err != nil {
		t.Fatalf("first VerifyFinish: %v", err)
	}
	if _, err := h.VerifyFinish(stepUpContext(user.UUID), req); statusOf(t, err) != http.StatusUnauthorized {
		t.Fatalf("replayed VerifyFinish = %v, want 401", err)
	}
}

func TestWebAuthnLoginFinish_OneCeremonyCannotCompleteDifferentPendingLogins(t *testing.T) {
	user := &iface.User{UUID: "pending-user", Email: "pending@example.com", Role: "operator", IsActive: true}
	challenges := services.NewMFAChallengeService(services.NewMemoryOAuthStateStore())
	pendingIDs := make([]string, 0, 2)
	for _, sid := range []string{"pending-session-a", "pending-session-b"} {
		challenge, err := challenges.BeginLogin(context.Background(), services.LoginChallengeInput{
			UserUUID: user.UUID, SessionID: sid, SourceAMR: []string{"pwd"}, Audience: "operator",
		})
		if err != nil {
			t.Fatalf("BeginLogin(%s): %v", sid, err)
		}
		pendingIDs = append(pendingIDs, challenge.ID)
	}
	issuer := &countingLoginTokenIssuer{}
	h := NewWebAuthnHandler(&oneWinnerWebAuthn{}, challenges, newStepUpJWT(t), &stepUpUsers{user: user}, issuer, "", "", false)
	h.SetPolicy(passwordLoginOnPolicy())

	start := make(chan struct{})
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, pendingID := range pendingIDs {
		pendingID := pendingID
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := &webAuthnLoginFinishRequest{}
			req.Body.LoginChallengeID = pendingID
			req.Body.WebAuthnChallengeID = "shared-ceremony-one-winner"
			req.Body.AssertionResponse = json.RawMessage(`{"id":"credential"}`)
			if _, err := h.LoginFinish(context.Background(), req); err == nil {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Errorf("successful login completions = %d, want 1", got)
	}
	if got := issuer.calls.Load(); got != 1 {
		t.Errorf("token issuance calls = %d, want 1", got)
	}
}
