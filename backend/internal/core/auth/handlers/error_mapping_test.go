package handlers

// Phase 13: pure-function coverage for the handler-side helpers that
// have grown across the auth-policy roadmap. mapPasswordError is the
// most error-prone — every new ErrXxx in the service layer requires
// a matching case here, and a missing or mistyped code/title/detail
// silently lands as a generic 400. mapMFAError is similar, smaller.
// priorAMRWithOTP / appendOTP are tiny helpers but core to the AMR
// claim downstream middleware reads on every step-up check.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
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
		{"AccountLocked → 429", services.ErrAccountLocked, http.StatusTooManyRequests, ""},
		{"UserInactive → 403", services.ErrUserInactive, http.StatusForbidden, ""},
		{"PasswordReused → 400", services.ErrPasswordReused, http.StatusBadRequest, ""},
		{"NotificationDown → 503", services.ErrNotificationDown, http.StatusServiceUnavailable, ""},
		{"MFAEnrollmentRequired → 403", services.ErrMFAEnrollmentRequired, http.StatusForbidden, ""},
		{"RegistrationDisabled → 403 auth.registration_disabled", services.ErrRegistrationDisabled, http.StatusForbidden, errcode.AuthRegistrationDisabled},
		{"EmailDomainNotAllowed → 403 auth.email_domain_not_allowed", services.ErrEmailDomainNotAllowed, http.StatusForbidden, errcode.AuthEmailDomainNotAllowed},
		{"LoginDisabled → 403 auth.login_disabled", services.ErrLoginDisabled, http.StatusForbidden, errcode.AuthLoginDisabled},
		{"CountryBlocked → 403 auth.country_blocked", services.ErrCountryBlocked, http.StatusForbidden, errcode.AuthCountryBlocked},
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

func TestErrorMapping_WriteOAuthCallbackErrorStaysNeutralAndSanitized(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	marker := "oauth-sensitive-marker"
	for _, tc := range []struct {
		name        string
		err         error
		wantStatus  int
		wantBody    string
		wantOutcome string
	}{
		{
			name:        "invalid credentials",
			err:         fmt.Errorf("%s: %w", marker, services.ErrInvalidCredentials),
			wantStatus:  http.StatusUnauthorized,
			wantBody:    invalidOAuthAuthenticationDetail,
			wantOutcome: "invalid_credentials",
		},
		{
			name:        "internal error",
			err:         errors.New(marker),
			wantStatus:  http.StatusInternalServerError,
			wantBody:    "Failed to process OAuth callback",
			wantOutcome: "internal_error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			rec := httptest.NewRecorder()
			writeOAuthCallbackError(rec, authModels.OAuthProviderGoogle, tc.err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.wantBody) {
				t.Fatalf("body = %q, want %q", body, tc.wantBody)
			}
			if strings.Contains(rec.Body.String(), marker) || strings.Contains(logs.String(), marker) {
				t.Errorf("OAuth callback leaked marker in response/logs: body=%q logs=%q", rec.Body.String(), logs.String())
			}
			if !strings.Contains(logs.String(), `"msg":"oauth_authentication_failed"`) ||
				!strings.Contains(logs.String(), `"provider":"google"`) ||
				!strings.Contains(logs.String(), `"outcome":"`+tc.wantOutcome+`"`) {
				t.Errorf("sanitized OAuth log = %q, want stable category/provider/outcome", logs.String())
			}
		})
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
