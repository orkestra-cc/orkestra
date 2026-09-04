package handlers

// The step-up mints re-issue the caller's amr under the user's CURRENT MFA
// epoch with last_otp_at=now, so any marker copied forward from the raw
// claim comes back with fresh authority. That is how a credential removal
// stops taking effect on the caller's own token — D16's headline property —
// so each of these is a driver, not a pin.

import (
	"context"
	"testing"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
)

func amrCtx(amr []string) context.Context {
	return context.WithValue(context.Background(), "claims", &authModels.JWTClaims{AMR: amr})
}

func hasMarker(amr []string, want string) bool {
	for _, v := range amr {
		if v == want {
			return true
		}
	}
	return false
}

// I-1. The reviewer's trace: a user self-removes their factor (the epoch
// moves, D16 spares their session, the resolver strips "otp" on the next
// request), then posts one password reconfirm. Before the fix the reconfirm
// handed the marker straight back at the CURRENT epoch — passing RequireMFA,
// RequireStepUp, the enrolment gate, Cedar, and the personal-tenant
// impersonation bypass this PR had just closed.
//
// ConfirmPassword's own enrolled-factor refusal cannot cover this: it
// refuses callers who HAVE a factor, and by this point the factor is gone.
func TestPriorAMRFromCtx_DoesNotLaunderARemovedFactorsMarker(t *testing.T) {
	got := priorAMRFromCtx(amrCtx([]string{"pwd", "otp"}))
	if hasMarker(got, "otp") {
		t.Fatalf("priorAMRFromCtx = %v — a reauth mint must not carry a second-factor marker", got)
	}
	if !hasMarker(got, "pwd") {
		t.Fatalf("priorAMRFromCtx = %v — the base marker describes how the session began and must survive", got)
	}
}

// Every epoch-governed marker, not just "otp": a deleted passkey and a
// device-trust grant are re-minted with the same fresh authority.
func TestPriorAMRFromCtx_StripsEveryEpochGovernedMarker(t *testing.T) {
	got := priorAMRFromCtx(amrCtx([]string{
		"pwd", "otp", "webauthn", "mfa", authModels.DeviceTrustAMR,
	}))
	if len(got) != 1 || got[0] != "pwd" {
		t.Fatalf("priorAMRFromCtx = %v, want [pwd]", got)
	}
}

// The endpoint must keep working for the population it exists to serve: a
// caller with no second factor at all.
func TestPriorAMRFromCtx_LeavesABaseOnlyClaimAlone(t *testing.T) {
	got := priorAMRFromCtx(amrCtx([]string{"oauth"}))
	if len(got) != 1 || got[0] != "oauth" {
		t.Fatalf("priorAMRFromCtx = %v, want [oauth] unchanged", got)
	}
}

// Minor 2 / ruling R15, first half. The caller has just proven a REAL TOTP
// factor, so the mint is legitimate — but a passkey they deleted must not
// ride along on it and come back current.
func TestPriorAMRWithOTP_DoesNotLaunderAWebAuthnMarkerThroughATOTPVerify(t *testing.T) {
	got := priorAMRWithOTP(amrCtx([]string{"pwd", "webauthn"}))
	if hasMarker(got, "webauthn") {
		t.Fatalf("priorAMRWithOTP = %v — a TOTP verify proves nothing about a passkey", got)
	}
	if !hasMarker(got, "otp") {
		t.Fatalf("priorAMRWithOTP = %v — the factor actually verified must be stamped", got)
	}
	if !hasMarker(got, "pwd") {
		t.Fatalf("priorAMRWithOTP = %v — base markers survive", got)
	}
}

func TestPriorAMRWithOTP_StripsDeviceTrustAndStaleMFAMarkers(t *testing.T) {
	got := priorAMRWithOTP(amrCtx([]string{"oauth", "mfa", authModels.DeviceTrustAMR}))
	if hasMarker(got, "mfa") || hasMarker(got, authModels.DeviceTrustAMR) {
		t.Fatalf("priorAMRWithOTP = %v — only the verified factor may be re-minted", got)
	}
	if len(got) != 2 || got[0] != "oauth" || got[1] != "otp" {
		t.Fatalf("priorAMRWithOTP = %v, want [oauth otp]", got)
	}
}

// "reauth" is not epoch-governed — a password is not an MFA credential —
// so a caller who reconfirmed and then verified a factor keeps both facts.
func TestPriorAMRWithOTP_KeepsReauth(t *testing.T) {
	got := priorAMRWithOTP(amrCtx([]string{"pwd", "reauth"}))
	if !hasMarker(got, "reauth") {
		t.Fatalf("priorAMRWithOTP = %v — the epoch does not govern a password reconfirm", got)
	}
}

// Minor 2, second half: the passkey step-up composes the two helpers, and
// the result must name the factor actually asserted and nothing else.
// VerifyFinish mints appendWebAuthn(priorAMRWithOTP(ctx)).
func TestVerifyFinishAMR_CarriesOnlyTheAssertedFactor(t *testing.T) {
	got := appendWebAuthn(priorAMRWithOTP(amrCtx([]string{"pwd", "otp", authModels.DeviceTrustAMR})))
	want := []string{"pwd", "otp", "webauthn"}
	if len(got) != len(want) {
		t.Fatalf("amr = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("amr = %v, want %v", got, want)
		}
	}
}

// The other appendWebAuthn call site (LoginFinish) is safe by construction
// rather than by a strip: SourceAMR is stamped at login as exactly ["pwd"]
// or ["oauth"]. Pinned so a future producer that widens it is caught here
// rather than by re-deriving the laundering bug.
func TestLoginFinishAMR_SourceAMRShapeCarriesNoEpochGovernedMarker(t *testing.T) {
	for _, source := range [][]string{{"pwd"}, {"oauth"}} {
		if authModels.HasEpochBoundAMR(source) {
			t.Fatalf("SourceAMR %v now carries an epoch-governed marker — "+
				"appendWebAuthn(appendOTP(SourceAMR)) needs the same strip priorAMRWithOTP applies", source)
		}
	}
}

// The mint must never write through to the caller's claims: other consumers
// on the same request read that slice, and the resolved MFA authority is
// derived from it.
func TestPriorAMRHelpers_DoNotMutateTheClaim(t *testing.T) {
	claims := &authModels.JWTClaims{AMR: []string{"pwd", "otp"}}
	ctx := context.WithValue(context.Background(), "claims", claims)

	_ = appendWebAuthn(priorAMRWithOTP(ctx))
	_ = priorAMRFromCtx(ctx)

	if len(claims.AMR) != 2 || claims.AMR[0] != "pwd" || claims.AMR[1] != "otp" {
		t.Fatalf("claims.AMR = %v — the helpers must not write through to the token's own claim", claims.AMR)
	}
}
