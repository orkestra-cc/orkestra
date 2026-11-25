package services

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TestTokenPairHonoursPerMintTTLOverride pins the PIN method's lifetimes:
// a SecurityContext override reaches both exp claims and ExpiresIn, is
// clamped into the bounds, and leaves the configured lifetimes untouched
// for every other mint.
func TestTokenPairHonoursPerMintTTLOverride(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", 15*time.Minute, 7*24*time.Hour)
	user := &iface.User{UUID: "u-1", Email: "a@b.com", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "tab-1"}

	cases := []struct {
		name                    string
		access, refresh         time.Duration
		wantAccess, wantRefresh time.Duration
	}{
		{"no override", 0, 0, 15 * time.Minute, 7 * 24 * time.Hour},
		{"pin lifetimes", 5 * time.Minute, 12 * time.Hour, 5 * time.Minute, 12 * time.Hour},
		{"clamped", 10 * time.Second, 30 * 24 * time.Hour, MinAccessTokenTTL, MaxPINRefreshTokenTTL},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := time.Now()
			pair, err := svc.GenerateTokenPairWithAMR(user, device,
				&authModels.SecurityContext{SessionID: "s-1", AccessTTL: c.access, RefreshTTL: c.refresh},
				[]string{authModels.AMRPIN}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if pair.ExpiresIn != int64(c.wantAccess.Seconds()) {
				t.Fatalf("ExpiresIn=%d, want %d", pair.ExpiresIn, int64(c.wantAccess.Seconds()))
			}
			ac, err := svc.ValidateAccessToken(pair.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			rc, err := svc.ValidateRefreshToken(pair.RefreshToken)
			if err != nil {
				t.Fatal(err)
			}
			within := func(exp int64, want time.Duration) bool {
				d := time.Unix(exp, 0).Sub(before)
				return d > want-5*time.Second && d <= want+5*time.Second
			}
			if !within(ac.ExpiresAt, c.wantAccess) {
				t.Fatalf("access exp %v after mint, want %v", time.Unix(ac.ExpiresAt, 0).Sub(before), c.wantAccess)
			}
			if !within(rc.ExpiresAt, c.wantRefresh) {
				t.Fatalf("refresh exp %v after mint, want %v", time.Unix(rc.ExpiresAt, 0).Sub(before), c.wantRefresh)
			}
			if len(ac.AMR) != 1 || ac.AMR[0] != authModels.AMRPIN {
				t.Fatalf("amr=%v", ac.AMR)
			}
		})
	}
}

// A re-mint that only adds a marker to a PIN session (step-up, password
// reconfirm) keeps the PIN access lifetime instead of the default.
func TestReMintOfAPINSessionKeepsThePINLifetime(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewJWTService(priv, &priv.PublicKey, "test", time.Hour, 7*24*time.Hour)
	svc.SetPolicy(NewAuthPolicyServiceForTest(map[string]string{"pinAccessTokenTTL": "2m"}))
	user := &iface.User{UUID: "u-1", Email: "a@b.com", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "tab-1"}
	for _, c := range []struct {
		amr  []string
		want time.Duration
	}{
		{[]string{authModels.AMRPIN, authModels.AMRReauth}, 2 * time.Minute},
		{[]string{authModels.AMRPassword, authModels.AMRReauth}, time.Hour},
	} {
		before := time.Now()
		tok, err := svc.GenerateAccessTokenForSessionWithAMR(user, device, &authModels.SecurityContext{SessionID: "s-1"}, c.amr, time.Now().Unix())
		if err != nil {
			t.Fatal(err)
		}
		claims, err := svc.ValidateAccessToken(tok)
		if err != nil {
			t.Fatal(err)
		}
		if d := time.Unix(claims.ExpiresAt, 0).Sub(before); d < c.want-5*time.Second || d > c.want+5*time.Second {
			t.Fatalf("amr %v: lifetime %v, want %v", c.amr, d, c.want)
		}
	}
}
