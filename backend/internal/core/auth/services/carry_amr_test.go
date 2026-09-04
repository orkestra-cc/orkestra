package services

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const testLastOTPAt int64 = 1700000000

// M-2, second half: refresh copied amr and last_otp_at forward verbatim,
// so a session whose factor had been removed kept minting MFA-satisfied
// tokens for as long as it lived.
func TestCarryAMR_KeepsMarkersOnlyWhenTheEpochMatches(t *testing.T) {
	amr, lastOTP := carryAMR([]string{"pwd", "otp"}, testLastOTPAt, 3, 3)
	if !contains(amr, "otp") {
		t.Fatal("a matching epoch must keep the otp marker")
	}
	if lastOTP != testLastOTPAt {
		t.Fatalf("LastOTPAt = %d, want %d carried when a marker survives", lastOTP, testLastOTPAt)
	}

	amr, lastOTP = carryAMR([]string{"pwd", "otp"}, testLastOTPAt, 3, 4)
	if contains(amr, "otp") {
		t.Fatal("a stale epoch must drop the otp marker")
	}
	if !contains(amr, "pwd") {
		t.Fatal("base markers are kept regardless — they describe how the session began")
	}
	if lastOTP != 0 {
		t.Fatalf("LastOTPAt = %d, want 0 when no MFA marker survives", lastOTP)
	}
}

// "reauth" is a five-minute proof of presence, never a session property.
// It is ALWAYS dropped, whatever the epoch says — and dropping it must not
// be mistaken for "a marker survived", so the freshness stamp goes too.
func TestCarryAMR_AlwaysDropsReauth(t *testing.T) {
	for _, epochs := range [][2]int{{0, 0}, {2, 2}, {2, 5}} {
		amr, lastOTP := carryAMR([]string{"pwd", "reauth"}, testLastOTPAt, epochs[0], epochs[1])
		if contains(amr, "reauth") {
			t.Fatalf("epochs %v: reauth must never survive a refresh", epochs)
		}
		if !contains(amr, "pwd") {
			t.Fatalf("epochs %v: dropping reauth must not take pwd with it", epochs)
		}
		if lastOTP != 0 {
			t.Fatalf("epochs %v: LastOTPAt = %d, want 0 — reauth is not a surviving marker", epochs, lastOTP)
		}
	}
}

func TestCarryAMR_KeepsBaseMarkers(t *testing.T) {
	amr, _ := carryAMR([]string{"oauth"}, testLastOTPAt, 1, 9)
	if !contains(amr, "oauth") {
		t.Fatal("oauth describes how the session began and always survives")
	}
}

// device_trust is epoch-governed: the trust was granted on the strength of
// a factor, so it dies with it. It is the marker the middleware and the
// refresh path used to disagree about, which is why the set now lives in
// auth/models and both read it from there.
func TestCarryAMR_DeviceTrustFollowsTheEpoch(t *testing.T) {
	amr, lastOTP := carryAMR([]string{"pwd", models.DeviceTrustAMR}, testLastOTPAt, 1, 2)
	if contains(amr, models.DeviceTrustAMR) {
		t.Fatal("device_trust is an epoch-governed marker and dies with a stale epoch")
	}
	if lastOTP != 0 {
		t.Fatalf("LastOTPAt = %d, want 0", lastOTP)
	}

	amr, lastOTP = carryAMR([]string{"pwd", models.DeviceTrustAMR}, testLastOTPAt, 2, 2)
	if !contains(amr, models.DeviceTrustAMR) {
		t.Fatal("a matching epoch keeps device_trust")
	}
	if lastOTP != testLastOTPAt {
		t.Fatalf("LastOTPAt = %d, want it carried when device_trust survives", lastOTP)
	}
}

// Order is preserved and nothing is invented: the output is the input
// minus the markers the rules remove.
func TestCarryAMR_PreservesOrderAndAddsNothing(t *testing.T) {
	amr, _ := carryAMR([]string{"pwd", "otp", "reauth", models.DeviceTrustAMR}, testLastOTPAt, 1, 1)
	want := []string{"pwd", "otp", models.DeviceTrustAMR}
	if len(amr) != len(want) {
		t.Fatalf("amr = %v, want %v", amr, want)
	}
	for i := range want {
		if amr[i] != want[i] {
			t.Fatalf("amr = %v, want %v", amr, want)
		}
	}
}

// The shipped refresh paths read `prior` off the REFRESH token's claims,
// which carry no amr at all (GenerateEnhancedRefreshToken omits it). Pins
// that the empty case is well behaved rather than a nil-slice surprise, so
// the recomputation is behaviour-neutral on the paths as they stand today.
func TestCarryAMR_EmptyPriorIsTheShippedRefreshShape(t *testing.T) {
	amr, lastOTP := carryAMR(nil, testLastOTPAt, 0, 4)
	if len(amr) != 0 {
		t.Fatalf("amr = %v, want empty", amr)
	}
	if lastOTP != 0 {
		t.Fatalf("LastOTPAt = %d, want 0", lastOTP)
	}
}

// --- The refresh paths themselves -------------------------------------------

// A refreshed token must carry the user's CURRENT epoch, not the epoch the
// session was minted under, or it would be stale the moment it was signed
// and its holder would be locked out of every MFA gate.
//
// This is not vacuous even though the refresh token carries no amr: the
// mint reads mfae off the *iface.User the refresh already loaded, so the
// value is whatever the user document says at rotation time.
func TestRefresh_MintsUnderTheCurrentEpoch(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{
		UUID: "u-epoch-rotation", Email: "epoch-rotation@example.com",
		Role: "operator", IsActive: true, MFAEpoch: 2,
	}
	env.users.seed(user)
	token := seedRefreshCarryingAuthTime(t, env, user, time.Now().Add(-time.Hour).Unix())

	// The factor is removed while the session is live: the user document
	// moves on. gateUserFake stores the pointer, so this is the same
	// mutation a BumpMFAEpoch would produce.
	user.MFAEpoch = 5

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	claims, err := env.jwt.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.MFAEpoch != 5 {
		t.Fatalf("refreshed MFAEpoch = %d, want the user's current 5", claims.MFAEpoch)
	}
	if contains(claims.AMR, "otp") {
		t.Fatal("no MFA marker may survive a rotation across an epoch change")
	}
}

// Same rule on the non-rotating bootstrap mint (/session), which is the
// path a client that never rotates would otherwise use to keep a stale
// epoch alive indefinitely.
func TestMintAccessTokenFromRefresh_MintsUnderTheCurrentEpoch(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{
		UUID: "u-epoch-bootstrap", Email: "epoch-bootstrap@example.com",
		Role: "operator", IsActive: true, MFAEpoch: 1,
	}
	env.users.seed(user)
	token := seedRefreshCarryingAuthTime(t, env, user, time.Now().Add(-time.Hour).Unix())
	user.MFAEpoch = 4

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("MintAccessTokenFromRefresh: %v", err)
	}
	claims, err := env.jwt.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.MFAEpoch != 4 {
		t.Fatalf("bootstrap MFAEpoch = %d, want the user's current 4", claims.MFAEpoch)
	}
	if contains(claims.AMR, "otp") {
		t.Fatal("no MFA marker may survive a bootstrap mint across an epoch change")
	}
}
