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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
)

// statusOf extracts the HTTP status code from a Huma error or our
// codedError envelope. Both implement huma.StatusError.
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
		// Optional: when the case maps to a codedError, assert the
		// machine-readable code field. Empty skips the check.
		wantSlug string
	}{
		{"InvalidCredentials → 401", services.ErrInvalidCredentials, http.StatusUnauthorized, ""},
		{"EmailNotVerified → 403 email_not_verified", services.ErrEmailNotVerified, http.StatusForbidden, "email_not_verified"},
		{"AccountLocked → 429", services.ErrAccountLocked, http.StatusTooManyRequests, ""},
		{"UserInactive → 403", services.ErrUserInactive, http.StatusForbidden, ""},
		{"PasswordReused → 400", services.ErrPasswordReused, http.StatusBadRequest, ""},
		{"NotificationDown → 503", services.ErrNotificationDown, http.StatusServiceUnavailable, ""},
		{"MFAEnrollmentRequired → 403", services.ErrMFAEnrollmentRequired, http.StatusForbidden, ""},
		{"RegistrationDisabled → 403 registration_disabled", services.ErrRegistrationDisabled, http.StatusForbidden, "registration_disabled"},
		{"EmailDomainNotAllowed → 403 email_domain_not_allowed", services.ErrEmailDomainNotAllowed, http.StatusForbidden, "email_domain_not_allowed"},
		{"LoginDisabled → 403 login_disabled", services.ErrLoginDisabled, http.StatusForbidden, "login_disabled"},
		{"CountryBlocked → 403 country_blocked", services.ErrCountryBlocked, http.StatusForbidden, "country_blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := mapPasswordError(tc.in)
			if got := statusOf(t, out); got != tc.wantCode {
				t.Errorf("status = %d, want %d", got, tc.wantCode)
			}
			if tc.wantSlug != "" {
				ce, ok := out.(*codedError)
				if !ok {
					t.Fatalf("expected *codedError for slug case, got %T", out)
				}
				if ce.Code != tc.wantSlug {
					t.Errorf("code = %q, want %q", ce.Code, tc.wantSlug)
				}
			}
		})
	}
}

func TestMapPasswordError_PolicyValidationGroup(t *testing.T) {
	// Every password-policy validation error maps to a 400 carrying the
	// underlying error message verbatim — the SPA renders the localized
	// reason directly. Spot-check the 8 errors as one group rather than
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

func TestMapPasswordError_UnknownErrorFallsTo400(t *testing.T) {
	// Anything not in the switch should fall to the generic 400 — the
	// service caller doesn't get to leak arbitrary internal text. Also
	// guards against the silent-no-match drift mode.
	custom := errors.New("totally unexpected internal error")
	out := mapPasswordError(custom)
	if got := statusOf(t, out); got != http.StatusBadRequest {
		t.Errorf("unknown err: status = %d, want 400", got)
	}
	if out.Error() == "totally unexpected internal error" {
		t.Errorf("unknown err: must NOT leak the internal message verbatim, got %q", out.Error())
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
		{"unknown → 400 fallback", errors.New("???"), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, mapMFAError(tc.in)); got != tc.wantCode {
				t.Errorf("status = %d, want %d", got, tc.wantCode)
			}
		})
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

// ===== oauthSignupDisabled =====

func TestOAuthSignupDisabled_MatchesSentinel(t *testing.T) {
	if !oauthSignupDisabled(services.ErrOAuthSignupDisabled) {
		t.Errorf("must match the wrapped sentinel via errors.Is")
	}
	if oauthSignupDisabled(errors.New("some other error")) {
		t.Errorf("must NOT match unrelated errors")
	}
	if oauthSignupDisabled(nil) {
		t.Errorf("nil error must NOT match")
	}
}

// ===== redirectOAuthSignupDisabled =====

func TestRedirectOAuthSignupDisabled_BouncesToFrontendURL(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cb", nil)
	redirectOAuthSignupDisabled(rec, req, "https://app.example.com")
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	want := "https://app.example.com/auth/callback?success=false&error=oauth_signup_disabled"
	if loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

func TestRedirectOAuthSignupDisabled_NoFrontendURLFallsTo403(t *testing.T) {
	// When the frontend URL isn't configured we can't bounce the user
	// usefully — fall back to a plain 403 so the operator sees the
	// failure in their access log instead of a confusing 200.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cb", nil)
	redirectOAuthSignupDisabled(rec, req, "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
