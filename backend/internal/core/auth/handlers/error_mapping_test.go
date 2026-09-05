package handlers

// Phase 13: pure-function coverage for the handler-side helpers that
// have grown across the auth-policy roadmap. mapPasswordError is the
// most error-prone — every new ErrXxx in the service layer requires
// a matching case here, and a missing or mistyped code/title/detail
// silently lands as a generic 400. mapMFAError is similar, smaller.
// priorAMRWithOTP / appendOTP are tiny helpers but core to the AMR
// claim downstream middleware reads on every step-up check.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// statusOf extracts the HTTP status code from a Huma error or our
// *errcode.Error envelope. Both implement huma.StatusError.
func statusOf(t *testing.T, err error) int {
	t.Helper()
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected huma.StatusError, got %T (%v)", err, err)
	}
	return se.GetStatus()
}

func TestMapPasswordError_KnownCodes(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode int
		// Optional: when the case maps to an *errcode.Error, assert the
		// machine-readable code field. Empty skips the check.
		wantSlug string
	}{
		{"InvalidCredentials → 401", services.ErrInvalidCredentials, http.StatusUnauthorized, ""},
		{"EmailNotVerified → 403 auth.email_not_verified", services.ErrEmailNotVerified, http.StatusForbidden, errcode.AuthEmailNotVerified},
		{"AccountLocked → 429 auth.too_many_attempts", services.ErrAccountLocked, http.StatusTooManyRequests, errcode.AuthTooManyAttempts},
		{"UserInactive → 403", services.ErrUserInactive, http.StatusForbidden, ""},
		{"PasswordReused → 400", services.ErrPasswordReused, http.StatusBadRequest, ""},
		{"NotificationDown → 503", services.ErrNotificationDown, http.StatusServiceUnavailable, ""},
		{"MFAEnrollmentRequired → 403", services.ErrMFAEnrollmentRequired, http.StatusForbidden, ""},
		// D19: the reconfirm's own refusal for an MFA-OBLIGATED caller.
		// The code is the middleware's unprefixed envelope code, not an
		// errcode const (ruling R8) — the SPA switches on one value for
		// one situation, and it already handles this one.
		{"PasswordConfirmEnrollmentRequired → 403 mfa_enrollment_required", services.ErrPasswordConfirmEnrollmentRequired, http.StatusForbidden, "mfa_enrollment_required"},
		{"RegistrationDisabled → 403 auth.registration_disabled", services.ErrRegistrationDisabled, http.StatusForbidden, errcode.AuthRegistrationDisabled},
		{"EmailDomainNotAllowed → 403 auth.email_domain_not_allowed", services.ErrEmailDomainNotAllowed, http.StatusForbidden, errcode.AuthEmailDomainNotAllowed},
		{"LoginDisabled → 403 auth.login_disabled", services.ErrLoginDisabled, http.StatusForbidden, errcode.AuthLoginDisabled},
		{"CountryBlocked → 403 auth.country_blocked", services.ErrCountryBlocked, http.StatusForbidden, errcode.AuthCountryBlocked},
		{"password login disabled", services.ErrPasswordLoginDisabled, http.StatusForbidden, errcode.AuthPasswordLoginDisabled},
		{"policy unavailable", services.ErrAuthPolicyUnavailable, http.StatusServiceUnavailable, errcode.AuthPolicyUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := mapPasswordError(tc.in)
			if got := statusOf(t, out); got != tc.wantCode {
				t.Errorf("status = %d, want %d", got, tc.wantCode)
			}
			if tc.wantSlug != "" {
				var ce *errcode.Error
				if !errors.As(out, &ce) {
					t.Fatalf("expected *errcode.Error for slug case, got %T", out)
				}
				if ce.Code != tc.wantSlug {
					t.Errorf("code = %q, want %q", ce.Code, tc.wantSlug)
				}
			}
		})
	}
}

// A 429 with no Retry-After tells the caller to guess. Every lockout
// answer carries one, and it is never below 1 second (a "come back in
// 0 seconds" is an invitation to hot-loop).
func TestMapPasswordError_AccountLockedCarriesRetryAfter(t *testing.T) {
	err := mapPasswordError(services.ErrAccountLocked)

	var ce *errcode.Error
	if !errors.As(err, &ce) {
		t.Fatalf("want *errcode.Error, got %T", err)
	}
	ra := ce.GetHeaders().Get("Retry-After")
	if ra == "" {
		t.Fatal("Retry-After missing")
	}
	n, convErr := strconv.Atoi(ra)
	if convErr != nil || n < 1 {
		t.Fatalf("Retry-After = %q, want an integer >= 1", ra)
	}
}

func TestMapPasswordError_PolicyValidationGroup(t *testing.T) {
	// Every password-policy validation error maps to a 400 carrying a
	// written, human-safe sentence describing which rule failed (no
	// longer err.Error() itself — see mapPasswordError's per-case
	// literals). Spot-check the 8 errors as one group rather than
	// inflating the table above.
	policyErrs := []error{
		services.ErrPasswordTooShort,
		services.ErrPasswordTooLong,
		services.ErrPasswordContainsEmail,
		services.ErrPasswordBreached,
		services.ErrPasswordMissingUpper,
		services.ErrPasswordMissingLower,
		services.ErrPasswordMissingDigit,
		services.ErrPasswordMissingSymbol,
	}
	for _, e := range policyErrs {
		out := mapPasswordError(e)
		if got := statusOf(t, out); got != http.StatusBadRequest {
			t.Errorf("%v: status = %d, want 400", e, got)
		}
		if out.Error() == "" {
			t.Errorf("%v: huma error must carry a non-empty detail", e)
		}
	}
}

func TestMapPasswordError_JWTKeysNotLoadedIsUnavailable(t *testing.T) {
	err := mapPasswordError(services.ErrJWTKeysNotLoaded)
	if got := statusOf(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", got)
	}
	var ce *errcode.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *errcode.Error, got %T", err)
	}
	if ce.Code != errcode.AuthJWTNotConfigured {
		t.Fatalf("code = %q, want %q", ce.Code, errcode.AuthJWTNotConfigured)
	}
	if !strings.Contains(strings.ToLower(ce.Detail), "sign") &&
		!strings.Contains(strings.ToLower(ce.Detail), "key") {
		t.Fatalf("detail must name the cause, got %q", ce.Detail)
	}
}

func TestMapPasswordError_UnknownErrorIsServerFault(t *testing.T) {
	err := mapPasswordError(errors.New("something the handler has never seen"))
	if got := statusOf(t, err); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — an unnamed error is not the caller's fault", got)
	}
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Code != errcode.AuthUnavailable {
		t.Fatalf("want code %q, got %#v", errcode.AuthUnavailable, err)
	}
}

func TestErrorMapping_OAuthInvalidCredentialsStaysNeutral(t *testing.T) {
	email := "inactive@example.com"
	userUUID := "user-123"
	for _, tc := range []struct {
		name string
		in   error
	}{
		{
			name: "inactive user maps like invalid OAuth authentication",
			in:   fmt.Errorf("inactive account email=%s userUUID=%s: %w", email, userUUID, services.ErrInvalidCredentials),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := mapOAuthError(tc.in)
			if got := statusOf(t, out); got != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", got, http.StatusUnauthorized)
			}
			body, err := json.Marshal(out)
			if err != nil {
				t.Fatalf("marshal mapped error: %v", err)
			}
			for _, forbidden := range []string{"inactive", email, userUUID} {
				if strings.Contains(string(body), forbidden) {
					t.Errorf("OAuth error body leaks %q: %s", forbidden, body)
				}
			}
		})
	}
}

func TestMapOAuthError_NewSentinels(t *testing.T) {
	cases := []struct {
		in       error
		wantCode int
		wantSlug string
	}{
		{services.ErrOAuthEmailUnverified, http.StatusForbidden, errcode.AuthOAuthEmailUnverified},
		{services.ErrAuthPolicyUnavailable, http.StatusServiceUnavailable, errcode.AuthPolicyUnavailable},
		{services.ErrInvalidCredentials, http.StatusUnauthorized, ""},
		{errors.New("anything else"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		err := mapOAuthError(tc.in)
		if got := statusOf(t, err); got != tc.wantCode {
			t.Errorf("%v → %d, want %d", tc.in, got, tc.wantCode)
		}
		if tc.wantSlug != "" {
			var e *errcode.Error
			if !errors.As(err, &e) || e.Code != tc.wantSlug {
				t.Errorf("%v → %v, want code %s", tc.in, err, tc.wantSlug)
			}
		}
	}
}

func TestMapMFAError_KnownCodes(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode int
	}{
		{"InvalidCode → 401", services.ErrMFAInvalidCode, http.StatusUnauthorized},
		{"ChallengeMismatch → 400", services.ErrMFAChallengeMismatch, http.StatusBadRequest},
		{"NotEnrolled → 400", services.ErrMFANotEnrolled, http.StatusBadRequest},
		{"unknown → 500 server fault", errors.New("???"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, mapMFAError(tc.in)); got != tc.wantCode {
				t.Errorf("status = %d, want %d", got, tc.wantCode)
			}
		})
	}
}

func TestMapMFAError_UnknownErrorIsServerFault(t *testing.T) {
	err := mapMFAError(errors.New("something the MFA handler has never seen"))
	if got := statusOf(t, err); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — an unnamed error is not the caller's fault", got)
	}
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Code != errcode.AuthUnavailable {
		t.Fatalf("want code %q, got %#v", errcode.AuthUnavailable, err)
	}
}

func TestMapWebAuthnError_UnknownErrorIsServerFault(t *testing.T) {
	err := mapWebAuthnError(errors.New("something the webauthn handler has never seen"))
	if got := statusOf(t, err); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — an unnamed error is not the caller's fault", got)
	}
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Code != errcode.AuthUnavailable {
		t.Fatalf("want code %q, got %#v", errcode.AuthUnavailable, err)
	}
}

// ===== priorAMRWithOTP =====

func TestPriorAMRWithOTP_DefaultsToPwdPlusOTP(t *testing.T) {
	// No claims in context → the helper assumes password login (the most
	// common preceding factor) and stamps "otp" as the second factor.
	out := priorAMRWithOTP(context.Background())
	if len(out) != 2 || out[0] != "pwd" || out[1] != "otp" {
		t.Errorf("default = %v, want [pwd otp]", out)
	}
}

func TestPriorAMRWithOTP_AppendsToExistingClaim(t *testing.T) {
	claims := &authModels.JWTClaims{AMR: []string{"oauth"}}
	ctx := context.WithValue(context.Background(), "claims", claims)
	out := priorAMRWithOTP(ctx)
	if len(out) != 2 || out[0] != "oauth" || out[1] != "otp" {
		t.Errorf("got %v, want [oauth otp]", out)
	}
}

func TestPriorAMRWithOTP_IdempotentWhenOTPAlreadyPresent(t *testing.T) {
	// A token that already carries "otp" must NOT have a second one
	// appended — duplicate factors break some downstream "amr contains"
	// checks. Pass back the existing slice unchanged.
	claims := &authModels.JWTClaims{AMR: []string{"pwd", "otp"}}
	ctx := context.WithValue(context.Background(), "claims", claims)
	out := priorAMRWithOTP(ctx)
	if len(out) != 2 {
		t.Errorf("expected unchanged length 2, got %d (%v)", len(out), out)
	}
	otpCount := 0
	for _, v := range out {
		if v == "otp" {
			otpCount++
		}
	}
	if otpCount != 1 {
		t.Errorf("expected exactly one 'otp', got %d", otpCount)
	}
}

// ===== appendOTP =====

func TestAppendOTP_EmptySourceDefaults(t *testing.T) {
	out := appendOTP(nil)
	if len(out) != 2 || out[0] != "pwd" || out[1] != "otp" {
		t.Errorf("empty source: got %v, want [pwd otp]", out)
	}
}

func TestAppendOTP_PreservesExistingFactors(t *testing.T) {
	out := appendOTP([]string{"oauth"})
	if len(out) != 2 || out[0] != "oauth" || out[1] != "otp" {
		t.Errorf("got %v, want [oauth otp]", out)
	}
}

func TestAppendOTP_IdempotentOnExistingOTP(t *testing.T) {
	src := []string{"pwd", "otp"}
	out := appendOTP(src)
	if len(out) != 2 {
		t.Errorf("idempotent call must not extend the slice, got %v", out)
	}
}

// ===== currentSessionID =====

func TestCurrentSessionID_EmptyContextReturnsEmptyString(t *testing.T) {
	if got := currentSessionID(context.Background()); got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

// ===== not-found / notification-down classification (spec §8 #18(c)) =====
//
// The three admin/self mappers used to classify these two cases by
// err.Error() == "<literal>". That matched only because the sentinels'
// messages are literally those strings and the producers return them
// unwrapped — so the next fmt.Errorf("...: %w", …) anywhere on the path
// would have turned a 404 into a 500 (and a 503 into a 500) silently, with
// no test to catch it. These tests present the sentinels WRAPPED, which is
// exactly the input the string compare cannot see.

// TestMappersClassifyWrappedUserNotFound is the regression test for the
// three drop-ins: a wrapped iface.ErrUserNotFound must still be a 404.
func TestMappersClassifyWrappedUserNotFound(t *testing.T) {
	wrapped := fmt.Errorf("read auth methods: %w", iface.ErrUserNotFound)

	cases := []struct {
		name string
		out  error
	}{
		{"mapAdminUserAuthError", mapAdminUserAuthError(wrapped)},
		{"mapAdminInviterError", mapAdminInviterError(wrapped, "failed to send password reset email")},
		{"mapSelfAuthError", mapSelfAuthError(wrapped)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, tc.out); got != http.StatusNotFound {
				t.Errorf("status = %d, want %d — a wrapped iface.ErrUserNotFound must classify as not-found", got, http.StatusNotFound)
			}
		})
	}
}

// TestMappersClassifyBareUserNotFound is the other half: the shape that
// already worked (the sentinel returned unwrapped, which is what
// user/services does today) keeps answering 404. Together with the test
// above it says the classification widened and nothing moved.
func TestMappersClassifyBareUserNotFound(t *testing.T) {
	cases := []struct {
		name string
		out  error
	}{
		{"mapAdminUserAuthError", mapAdminUserAuthError(iface.ErrUserNotFound)},
		{"mapAdminInviterError", mapAdminInviterError(iface.ErrUserNotFound, "failed to send password reset email")},
		{"mapSelfAuthError", mapSelfAuthError(iface.ErrUserNotFound)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, tc.out); got != http.StatusNotFound {
				t.Errorf("status = %d, want %d", got, http.StatusNotFound)
			}
		})
	}
}

// TestMappersDoNotClassifyLookalikeNotFound is the bound. A different
// module's own "user not found" error — same message, different identity —
// is NOT the SDK sentinel and must now surface as a 500 rather than being
// mistaken for one. This is the behaviour the string compare could not
// express, and the only intentional change in the three mappers' outputs.
func TestMappersDoNotClassifyLookalikeNotFound(t *testing.T) {
	lookalike := errors.New("user not found")

	cases := []struct {
		name string
		out  error
	}{
		{"mapAdminUserAuthError", mapAdminUserAuthError(lookalike)},
		{"mapAdminInviterError", mapAdminInviterError(lookalike, "failed to send password reset email")},
		{"mapSelfAuthError", mapSelfAuthError(lookalike)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, tc.out); got != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d — only iface.ErrUserNotFound's identity may mean not-found", got, http.StatusInternalServerError)
			}
		})
	}
}

// TestMapAdminInviterErrorClassifiesWrappedNotificationDown covers the
// compare that sat beside the not-found one in mapAdminInviterError and
// has the same shape: services.ErrNotificationDown, wrapped, must still be
// the 503 that tells an operator the invite could not be delivered.
func TestMapAdminInviterErrorClassifiesWrappedNotificationDown(t *testing.T) {
	for _, in := range []error{
		services.ErrNotificationDown,
		fmt.Errorf("admin send invite: %w", services.ErrNotificationDown),
	} {
		out := mapAdminInviterError(in, "failed to send invite email")
		if got := statusOf(t, out); got != http.StatusServiceUnavailable {
			t.Errorf("map(%v) status = %d, want %d", in, got, http.StatusServiceUnavailable)
		}
	}
}

// TestMappersKeepUnrelatedErrorsAt500 is the negative that stops the three
// mappers from becoming "everything is a 404".
func TestMappersKeepUnrelatedErrorsAt500(t *testing.T) {
	boom := errors.New("mongo: no reachable servers")

	cases := []struct {
		name string
		out  error
	}{
		{"mapAdminUserAuthError", mapAdminUserAuthError(boom)},
		{"mapAdminInviterError", mapAdminInviterError(boom, "failed to send password reset email")},
		{"mapSelfAuthError", mapSelfAuthError(boom)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, tc.out); got != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", got, http.StatusInternalServerError)
			}
		})
	}
}
