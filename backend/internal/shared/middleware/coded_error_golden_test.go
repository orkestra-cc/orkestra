package middleware

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// TestCodedErrorEnvelopes_Golden is the byte-level contract for every
// hand-built coded error envelope this middleware writes.
//
// There are TEN of them — the nine spec §8 #18(d) enumerates plus
// sendTokenVerificationUnavailable, added by §4.10 — and they are the only
// responses in this package that carry a flat top-level `code`.
// sendErrorResponse is deliberately NOT here: it routes through
// errorManager and emits no top-level code at all (§3.C), and putting it
// behind the same writer would be a wire change.
//
// The table is a GOLDEN one in the strict sense: `wantBody` is the exact
// byte string the pre-refactor emitters produced, captured off an
// httptest.ResponseRecorder before writeCodedError existed (the capture run
// is recorded in the batch-3 workspace as
// logs/b1b-golden-capture-pre-refactor.log). It is not a re-derivation of
// what the current code does — which is the whole point. `json.Encoder`
// sorts map keys, so the field order below is alphabetical regardless of
// the order the literals are written in, and Encode appends the trailing
// newline.
//
// Every axis §8 #18(d) names is pinned: status (401 ×6, 403, 402, 503 ×2),
// the WWW-Authenticate scheme (Bearer ×4, MFA ×3, absent ×3 — asserted as
// an absent header, not an empty one), the errors[] array (present with a
// per-site location + value ×8, absent ×2), and the extra top-level fields
// (none ×5, maxAgeSeconds ×2, riskScore+riskThreshold ×1,
// capability+tenantId ×1).
//
// If a change to an envelope is INTENDED, this test is the place the wire
// change becomes visible and deliberate: update the literal here in the
// same commit and say so. It failing on a refactor means the refactor was
// not behaviour-neutral.
func TestCodedErrorEnvelopes_Golden(t *testing.T) {
	m := &AuthMiddleware{}
	// None of the ten emitters reads the request (several do not even take
	// one); it is supplied only because the signatures ask for it.
	r := httptest.NewRequest(http.MethodGet, "/v1/admin/anything", nil)

	const (
		ctJSON = "application/json"
		maxAge = 300 * time.Second
	)

	cases := []struct {
		name        string
		emit        func(w http.ResponseWriter)
		wantStatus  int
		wantHeaders map[string]string
		wantBody    string
	}{
		{
			name:       "sendSessionRevoked/revoked",
			emit:       func(w http.ResponseWriter) { m.sendSessionRevoked(w, r, "logout_all") },
			wantStatus: http.StatusUnauthorized,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `Bearer error="session_revoked"`,
			},
			wantBody: `{"code":"session_revoked","detail":"this session has been revoked; please sign in again","errors":[{"location":"require_auth","message":"session revoked","value":"SESSION_REVOKED"}],"status":401,"title":"session revoked","type":"about:blank"}` + "\n",
		},
		{
			name:       "sendSessionRevoked/session_max_age",
			emit:       func(w http.ResponseWriter) { m.sendSessionRevoked(w, r, models.RevokeReasonSessionMaxAge) },
			wantStatus: http.StatusUnauthorized,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `Bearer error="session_max_age_reached"`,
			},
			wantBody: `{"code":"session_max_age_reached","detail":"this session reached its maximum age; please sign in again","errors":[{"location":"require_auth","message":"session maximum age reached","value":"SESSION_MAX_AGE_REACHED"}],"status":401,"title":"session maximum age reached","type":"about:blank"}` + "\n",
		},
		{
			name:       "sendAccessTokenExpired",
			emit:       func(w http.ResponseWriter) { m.sendAccessTokenExpired(w) },
			wantStatus: http.StatusUnauthorized,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `Bearer error="access_token_expired"`,
			},
			wantBody: `{"code":"access_token_expired","detail":"the access token has expired; refresh it and retry","errors":[{"location":"require_auth","message":"access token expired","value":"ACCESS_TOKEN_EXPIRED"}],"status":401,"title":"access token expired","type":"about:blank"}` + "\n",
		},
		{
			// The one emitter whose errors[0].value is NOT
			// strings.ToUpper(code) — HIGH_RISK_SESSION against a
			// step_up_required code. It is why `value` is a parameter of
			// writeCodedError rather than something derived from `code`.
			name:       "sendRiskStepUp",
			emit:       func(w http.ResponseWriter) { m.sendRiskStepUp(w, r, 0.7, 0.85) },
			wantStatus: http.StatusUnauthorized,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `MFA error="step_up_required"`,
			},
			wantBody: `{"code":"step_up_required","detail":"this action requires a fresh second-factor verification due to elevated session risk","errors":[{"location":"require_low_risk","message":"step-up required — high risk session","value":"HIGH_RISK_SESSION"}],"riskScore":0.85,"riskThreshold":0.7,"status":401,"title":"step-up authentication required","type":"about:blank"}` + "\n",
		},
		{
			name:       "sendStepUpRequired",
			emit:       func(w http.ResponseWriter) { m.sendStepUpRequired(w, r, maxAge) },
			wantStatus: http.StatusUnauthorized,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `MFA error="step_up_required"`,
			},
			wantBody: `{"code":"step_up_required","detail":"this action requires a fresh second-factor verification","errors":[{"location":"require_step_up","message":"step-up required","value":"STEP_UP_REQUIRED"}],"maxAgeSeconds":300,"status":401,"title":"step-up authentication required","type":"about:blank"}` + "\n",
		},
		{
			name:       "sendPasswordConfirmRequired",
			emit:       func(w http.ResponseWriter) { m.sendPasswordConfirmRequired(w, r, maxAge) },
			wantStatus: http.StatusUnauthorized,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `Bearer error="password_confirm_required"`,
			},
			wantBody: `{"code":"password_confirm_required","detail":"this action requires a fresh password reconfirm because no second factor is enrolled","errors":[{"location":"require_step_up","message":"password confirm required","value":"PASSWORD_CONFIRM_REQUIRED"}],"maxAgeSeconds":300,"status":401,"title":"password reconfirm required","type":"about:blank"}` + "\n",
		},
		{
			// One of the two emitters with NO errors[] and no
			// WWW-Authenticate. Supplying either would be a wire change,
			// so their absence is pinned here, not merely unasserted.
			name:        "sendPolicyUnavailable",
			emit:        func(w http.ResponseWriter) { m.sendPolicyUnavailable(w) },
			wantStatus:  http.StatusServiceUnavailable,
			wantHeaders: map[string]string{"Content-Type": ctJSON},
			wantBody:    `{"code":"auth.policy_unavailable","detail":"the sign-in policy could not be evaluated; try again shortly","status":503,"title":"sign-in policy unavailable","type":"about:blank"}` + "\n",
		},
		{
			name:        "sendTokenVerificationUnavailable",
			emit:        func(w http.ResponseWriter) { m.sendTokenVerificationUnavailable(w) },
			wantStatus:  http.StatusServiceUnavailable,
			wantHeaders: map[string]string{"Content-Type": ctJSON},
			wantBody:    `{"code":"token_verification_unavailable","detail":"access tokens cannot be verified right now; try again shortly","status":503,"title":"token verification unavailable","type":"about:blank"}` + "\n",
		},
		{
			name:       "sendMFAEnrollmentRequired",
			emit:       func(w http.ResponseWriter) { m.sendMFAEnrollmentRequired(w, r) },
			wantStatus: http.StatusForbidden,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `Bearer error="mfa_enrollment_required"`,
			},
			wantBody: `{"code":"mfa_enrollment_required","detail":"your role requires a second factor; enroll one before performing this action","errors":[{"location":"require_step_up","message":"mfa enrollment required","value":"MFA_ENROLLMENT_REQUIRED"}],"status":403,"title":"mfa enrollment required","type":"about:blank"}` + "\n",
		},
		{
			name:       "sendMFARequired",
			emit:       func(w http.ResponseWriter) { m.sendMFARequired(w, r) },
			wantStatus: http.StatusUnauthorized,
			wantHeaders: map[string]string{
				"Content-Type":     ctJSON,
				"Www-Authenticate": `MFA error="step_up_required"`,
			},
			wantBody: `{"code":"step_up_required","detail":"this action requires a second authentication factor","errors":[{"location":"require_mfa","message":"mfa required","value":"STEP_UP_REQUIRED"}],"status":401,"title":"mfa required","type":"about:blank"}` + "\n",
		},
		{
			// 402, no WWW-Authenticate, and two extra top-level fields.
			name:        "sendCapabilityRequiredResponse",
			emit:        func(w http.ResponseWriter) { m.sendCapabilityRequiredResponse(w, r, "reports.export", "tenant-7") },
			wantStatus:  http.StatusPaymentRequired,
			wantHeaders: map[string]string{"Content-Type": ctJSON},
			wantBody:    `{"capability":"reports.export","code":"capability_required","detail":"tenant is not entitled to this capability","errors":[{"location":"require_capability","message":"capability required","value":"CAPABILITY_REQUIRED"}],"status":402,"tenantId":"tenant-7","title":"capability required","type":"about:blank"}` + "\n",
		},
	}

	// The count is asserted, not just iterated: an eleventh emitter added
	// without a row here would otherwise slip through silently. Ten
	// emitters, eleven rows — sendSessionRevoked has two code branches.
	if len(cases) != 11 {
		t.Fatalf("golden table has %d rows, want 11 (ten emitters, sendSessionRevoked twice)", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.emit(rec)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Body.String(); got != tc.wantBody {
				t.Errorf("body mismatch\n got: %q\nwant: %q", got, tc.wantBody)
			}

			got := rec.Header()
			gotKeys := make([]string, 0, len(got))
			for k := range got {
				gotKeys = append(gotKeys, k)
			}
			sort.Strings(gotKeys)
			wantKeys := make([]string, 0, len(tc.wantHeaders))
			for k := range tc.wantHeaders {
				wantKeys = append(wantKeys, k)
			}
			sort.Strings(wantKeys)
			if len(gotKeys) != len(wantKeys) {
				t.Fatalf("header keys = %v, want exactly %v", gotKeys, wantKeys)
			}
			for i, k := range wantKeys {
				if gotKeys[i] != k {
					t.Fatalf("header keys = %v, want exactly %v", gotKeys, wantKeys)
				}
				if v := got.Get(k); v != tc.wantHeaders[k] {
					t.Errorf("header %s = %q, want %q", k, v, tc.wantHeaders[k])
				}
			}
		})
	}
}
