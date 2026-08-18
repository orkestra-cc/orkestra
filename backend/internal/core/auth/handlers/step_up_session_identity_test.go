package handlers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
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

type stepUpMFA struct{ services.MFAService }

func (stepUpMFA) Verify(context.Context, string, string) error { return nil }

type stepUpWebAuthn struct{ services.WebAuthnService }

func (stepUpWebAuthn) FinishAssertion(context.Context, *iface.User, string, services.MFAChallengePurpose, []byte) error {
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
		UserUUID:  userUUID,
		SessionID: "session-step-up",
		DeviceID:  "device-step-up",
		AMR:       []string{"pwd"},
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
		UserUUID:  user.UUID,
		SessionID: "session-pending-mfa",
		SourceAMR: []string{"pwd"},
		DeviceID:  "device-pending",
		Platform:  "web",
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
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	h := NewWebAuthnHandler(stepUpWebAuthn{}, challenges, jwt, &stepUpUsers{user: user}, tokens, "", "", false)
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
