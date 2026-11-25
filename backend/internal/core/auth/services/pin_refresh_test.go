package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// seedPINRefresh seeds a refresh row exactly as a PIN sign-in writes it.
func seedPINRefresh(t *testing.T, e *orchestrationEnv, user *iface.User, loginMethod string) string {
	t.Helper()
	token, err := e.jwt.GenerateEnhancedRefreshToken(user,
		&authModels.DeviceInfo{DeviceID: "tab-1"},
		&authModels.SecurityContext{SessionID: "sess-pin", AuthTime: time.Now().Add(-time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	hash := utils.HashRefreshToken(token)
	e.refresh.seedRefreshDoc(hash, &authModels.RefreshTokenDoc{
		UUID: uuid.NewString(), UserUUID: user.UUID, Token: hash, SessionUUID: "sess-pin",
		DeviceID: "tab-1", FamilyID: "fam-pin", LoginMethod: loginMethod,
		ExpiresAt: time.Now().Add(time.Hour), IssuedAt: time.Now(), CreatedAt: time.Now(),
	})
	return token
}

// pinSessionCheckerFunc adapts a func to PINSessionChecker.
type pinSessionCheckerFunc func(ctx context.Context, userUUID, deviceID string) error

func (f pinSessionCheckerFunc) CheckPINSession(ctx context.Context, userUUID, deviceID string) error {
	return f(ctx, userUUID, deviceID)
}

func pinRefreshEnv(t *testing.T) (*orchestrationEnv, *iface.User) {
	t.Helper()
	env := newOrchestrationEnv(t)
	env.auth.(*authService).SetPolicy(NewAuthPolicyServiceForTest(map[string]string{
		"pinAccessTokenTTL": "5m", "pinRefreshTokenTTL": "8h",
	}))
	env.auth.(*authService).SetPINSessionChecker(pinSessionCheckerFunc(func(context.Context, string, string) error { return nil }))
	user := &iface.User{UUID: "u-pin-refresh", Email: "pin@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	return env, user
}

// The refresh JWT carries no amr, so without the row's LoginMethod a
// refreshed PIN session would come back looking like a password session.
func TestRefresh_PINSessionStaysAPINSession(t *testing.T) {
	env, user := pinRefreshEnv(t)
	token := seedPINRefresh(t, env, user, authModels.LoginMethodPIN)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	claims, err := env.jwt.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.AMR) != 1 || claims.AMR[0] != authModels.AMRPIN || claims.LastOTPAt != 0 {
		t.Fatalf("refreshed amr=%v last_otp_at=%d, want [pin] and 0", claims.AMR, claims.LastOTPAt)
	}
	if resp.ExpiresIn != 300 || resp.RefreshExpiresIn != int64((8*time.Hour).Seconds()) {
		t.Fatalf("expiresIn=%d refreshExpiresIn=%d, want the PIN lifetimes", resp.ExpiresIn, resp.RefreshExpiresIn)
	}
	next := env.refresh.byHash[utils.HashRefreshToken(resp.RefreshToken)]
	if next == nil || next.LoginMethod != authModels.LoginMethodPIN {
		t.Fatalf("rotated row lost the PIN login method: %+v", next)
	}
	if d := time.Until(next.ExpiresAt); d < 8*time.Hour-time.Minute || d > 8*time.Hour {
		t.Fatalf("rotated row expires in %v, want the 8h PIN idle timeout", d)
	}
	rc, err := env.jwt.ValidateRefreshToken(resp.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(time.Unix(rc.ExpiresAt, 0)); d > 8*time.Hour+time.Minute {
		t.Fatalf("refresh JWT outlives the PIN idle timeout: %v", d)
	}

	// The non-rotating bootstrap mint re-asserts the marker too.
	again := seedPINRefresh(t, env, user, authModels.LoginMethodPIN)
	boot, err := env.auth.MintAccessTokenFromRefresh(context.Background(), again, nil)
	if err != nil {
		t.Fatal(err)
	}
	bc, _ := env.jwt.ValidateAccessToken(boot.AccessToken)
	if len(bc.AMR) != 1 || bc.AMR[0] != authModels.AMRPIN || boot.ExpiresIn != 300 {
		t.Fatalf("bootstrap amr=%v expiresIn=%d", bc.AMR, boot.ExpiresIn)
	}
}

func TestRefresh_NonPINSessionUnchanged(t *testing.T) {
	env, user := pinRefreshEnv(t)
	token := seedPINRefresh(t, env, user, "")
	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := env.jwt.ValidateAccessToken(resp.AccessToken)
	if authModels.HasPINAMR(claims.AMR) || resp.ExpiresIn != int64((15*time.Minute).Seconds()) {
		t.Fatalf("a non-PIN rotation changed: amr=%v expiresIn=%d", claims.AMR, resp.ExpiresIn)
	}
	next := env.refresh.byHash[utils.HashRefreshToken(resp.RefreshToken)]
	if next.LoginMethod != "" || time.Until(next.ExpiresAt) < 6*24*time.Hour {
		t.Fatalf("non-PIN row: loginMethod=%q expires in %v", next.LoginMethod, time.Until(next.ExpiresAt))
	}
}

// A PIN session re-validates on every refresh: once the checker refuses
// (PIN removed, device revoked, user no longer eligible) neither the
// rotation nor the bootstrap mint issues a token, and the checker sees the
// session's user and device.
func TestRefresh_PINSessionRevalidated(t *testing.T) {
	env, user := pinRefreshEnv(t)
	var gotUser, gotDevice string
	env.auth.(*authService).SetPINSessionChecker(pinSessionCheckerFunc(func(_ context.Context, u, d string) error {
		gotUser, gotDevice = u, d
		return ErrRefreshSessionInactive
	}))
	token := seedPINRefresh(t, env, user, authModels.LoginMethodPIN)
	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("rotation = %v, want ErrRefreshSessionInactive", err)
	}
	if gotUser != user.UUID || gotDevice != "tab-1" {
		t.Fatalf("checker saw user=%q device=%q", gotUser, gotDevice)
	}
	again := seedPINRefresh(t, env, user, authModels.LoginMethodPIN)
	if _, err := env.auth.MintAccessTokenFromRefresh(context.Background(), again, nil); !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("bootstrap mint = %v, want ErrRefreshSessionInactive", err)
	}

	// Without a checker a PIN session cannot refresh at all.
	env.auth.(*authService).SetPINSessionChecker(nil)
	third := seedPINRefresh(t, env, user, authModels.LoginMethodPIN)
	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), third, nil); !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("unwired rotation = %v, want ErrRefreshSessionInactive", err)
	}
}
