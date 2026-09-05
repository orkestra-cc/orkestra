package handlers

// Defect (a), found in live testing on staging 2026-09-04: a verdict 401
// that does not name itself.
//
// The operator console's error interceptor (`baseQueryWithRetry`) reads a
// 401 with NO top-level `code` as the one 401 shape that is not a verdict —
// a JWT signing-key rotation, after which every unexpired bearer validates
// as plain "invalid" — and answers it by running `performRefresh` once. So
// every codeless verdict 401 rotated the caller's refresh cookie. Typed
// quickly they raced: one 13-second burst of mistyped enrolment codes
// produced 26 of these 401s, 44 `409 refresh_rotation_raced` answers, and
// then a dead session.
//
// The rule these tests pin: a 401 a handler returns as a VERDICT on a
// credential the caller submitted must carry an errcode. A 401 about the
// BEARER (missing, expired, or a user row that is gone) deliberately does
// not — that one really is the session's own state, and rotating is the
// right answer to it.
//
// The assertions are on the emitted ERROR — status plus the machine-readable
// code off the *errcode.Error envelope — never on the detail string, which
// is human copy and may be reworded without touching the contract.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// failingReconfirmUsers is stepUpUsers plus the one method a REJECTED
// reconfirm reaches that a successful one does not: recordVerifyFailure
// charges the durable per-account counter before the handler answers.
// Without it the embedded nil UserProvider panics and the test never gets
// to look at the error it is about.
type failingReconfirmUsers struct{ *stepUpUsers }

func (failingReconfirmUsers) RecordFailedLogin(context.Context, string, *time.Time) error {
	return nil
}

// TestVerdict401sCarryTheirCode covers every credential-rejection arm of the
// three auth mappers. Reverting any one of them to a bare
// huma.Error401Unauthorized fails here, because assertStatusAndCode requires
// an *errcode.Error before it can read a code at all.
func TestVerdict401sCarryTheirCode(t *testing.T) {
	cases := []struct {
		name string
		got  error
		want string
	}{
		{
			// The arm the incident ran through: /mfa/verify and
			// /mfa/enroll/confirm both answer wrong codes here.
			name: "mapMFAError: a rejected TOTP or backup code",
			got:  mapMFAError(services.ErrMFAInvalidCode),
			want: errcode.AuthMFACodeInvalid,
		},
		{
			// Two arms, two codes, because they are two situations: an
			// unreadable challenge is not a rejected signature.
			name: "mapWebAuthnError: an unreadable challenge",
			got:  mapWebAuthnError(services.ErrMFAInvalidCode),
			want: errcode.AuthWebAuthnChallengeInvalid,
		},
		{
			name: "mapWebAuthnError: a rejected assertion",
			got:  mapWebAuthnError(services.ErrWebAuthnAssertion),
			want: errcode.AuthWebAuthnAssertionFailed,
		},
		{
			// Reached from change-password (protected — this is the one
			// that was rotating) and from login (public, excluded from the
			// console's rotation arm, and carrying the same neutral code so
			// the pair never becomes an existence oracle).
			name: "mapPasswordError: a rejected password",
			got:  mapPasswordError(services.ErrInvalidCredentials),
			want: errcode.AuthInvalidCredentials,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStatusAndCode(t, tc.got, http.StatusUnauthorized, tc.want)
		})
	}
}

// The route-level half for the endpoint the incident actually hit. The
// mapper test above pins the arm; this pins that /mfa/enroll/confirm really
// reaches it, so a future refactor cannot route around the code.
func TestMFAEnrollConfirm_WrongCodeNamesItself(t *testing.T) {
	user := enrolUser()
	h := newEnrolHandler(t, services.NewMemoryOAuthStateStore(),
		services.NewMemoryAttemptCounter(), services.PolicyAudienceOperator, user)
	challengeID := beginEnrolment(t, h, user.UUID)

	_, err := h.EnrollConfirm(stepUpContext(user.UUID), enrolConfirmRequest(challengeID, "000000"))
	assertStatusAndCode(t, err, http.StatusUnauthorized, errcode.AuthMFACodeInvalid)
}

// And the route-level half for the password reconfirm, whose 401 is built in
// the handler rather than in mapPasswordError. A wrong password there used to
// rotate the session too — /me/password-confirm is not in the console's
// AUTH_ENDPOINT_PATHS allowlist.
func TestPasswordConfirm_WrongPasswordNamesItself(t *testing.T) {
	jwt := newStepUpJWT(t)
	passwords := services.NewPasswordService(nil, false)
	hash, err := passwords.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	user := &iface.User{
		UUID:         "reconfirming-user",
		Email:        "reconfirm@example.com",
		Role:         "operator",
		IsActive:     true,
		PasswordHash: hash,
	}
	svc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService:     failingReconfirmUsers{&stepUpUsers{user: user}},
		PasswordService: passwords,
		JWTService:      jwt,
		Policy:          services.NewAuthPolicyServiceForTest(nil),
		Audience:        services.PolicyAudienceOperator,
	})
	h := NewPasswordAuthHandler(svc, "", "", false)

	req := &PasswordConfirmRequest{}
	req.Body.Password = "not-the-password"
	_, confirmErr := h.PasswordConfirm(stepUpContext(user.UUID), req)
	assertStatusAndCode(t, confirmErr, http.StatusUnauthorized, errcode.AuthInvalidCredentials)
}
