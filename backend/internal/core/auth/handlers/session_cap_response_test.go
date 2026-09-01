package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
)

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestWriteRefreshErr_SessionMaxAgeReached(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrSessionMaxAgeReached)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := decodeBody(t, rec)["code"]; got != "session_max_age_reached" {
		t.Errorf("code = %v, want session_max_age_reached — 'revoked' is inaccurate for a session that simply aged out, and the distinction matters to whoever reads the support ticket", got)
	}
}

// A rotation lost to a concurrent sibling is NOT a sign-out. Answering it
// on the 401 path made every client treat it as one, which is precisely
// how a multi-tab race turned into a forced re-login.
func TestWriteRefreshErr_RotationRacedIs409(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrRefreshRotationRaced)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 — a raced rotation must not look like an expired session", rec.Code)
	}
	if got := decodeBody(t, rec)["code"]; got != "refresh_rotation_raced" {
		t.Errorf("code = %v, want refresh_rotation_raced", got)
	}
}

func TestRefreshFailureOutcome_RotationRacedIsNotReplay(t *testing.T) {
	if got := refreshFailureOutcome(services.ErrRefreshRotationRaced); got != "rotation_raced" {
		t.Errorf("outcome = %q, want rotation_raced — logging a benign race as replay_detected buries the real replays", got)
	}
}

func TestWriteRefreshErr_EnforcementUnavailableIs503(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrSessionEnforcementUnavailable)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — a storage outage must not be reported as an authentication failure", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["code"] != "session_enforcement_unavailable" {
		t.Errorf("code = %v, want session_enforcement_unavailable", body["code"])
	}
	for _, v := range body {
		if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), "mongo") {
			t.Errorf("response leaks internals: %q", s)
		}
	}
}

func TestWriteRefreshErr_DegradedIsGenericLogout(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, &services.SessionRevocationDegradedError{})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if code, present := decodeBody(t, rec)["code"]; present {
		t.Errorf("code = %v, want none — a partially degraded cap logout must not claim a completely recorded cap expiry", code)
	}
}

func TestWriteRefreshErr_ReplayUnchanged(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrRefreshTokenReplay)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := decodeBody(t, rec)["code"]; got != "refresh_token_replay" {
		t.Errorf("code = %v, want refresh_token_replay", got)
	}
}

// Redux state cleanup is not a substitute for expiring the HttpOnly
// credential: the browser would keep presenting the dead cookie on every
// subsequent request. Enforcement unavailable is deliberately excluded —
// durable logout is not known to have completed there, and the client may
// legitimately retry once storage recovers.
func TestClearRefreshCookieOnTerminalRefreshErr(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantClear bool
	}{
		{"cap expiry clears", services.ErrSessionMaxAgeReached, true},
		{"degraded cap logout clears", &services.SessionRevocationDegradedError{}, true},
		{"enforcement unavailable keeps the cookie", services.ErrSessionEnforcementUnavailable, false},
		{"replay keeps today's behaviour", services.ErrRefreshTokenReplay, false},
		{"invalid token keeps today's behaviour", services.ErrInvalidRefreshToken, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Auth.Cookie.Name = logoutTestCookieName
			h := &AuthHandler{config: cfg}

			rec := httptest.NewRecorder()
			h.clearRefreshCookieOnTerminalRefreshErr(rec, logoutTestCookieName, tc.err)

			var cleared bool
			for _, c := range rec.Result().Cookies() {
				if c.Name == logoutTestCookieName && c.MaxAge < 0 {
					cleared = true
				}
			}
			if cleared != tc.wantClear {
				t.Errorf("cookie cleared = %v, want %v", cleared, tc.wantClear)
			}
		})
	}
}

// §4.9. The refresh-path outage code sits BESIDE session_enforcement_unavailable
// rather than reusing it: both are 503 and every client treats 503 identically,
// so the distinction costs nothing on the wire and buys the thing ADR-0017 D4
// argued for — whoever reads the support ticket can tell which subsystem failed.
// (The neighbouring branch keeping its own code is already pinned by
// TestWriteRefreshErr_EnforcementUnavailableIs503 above.)
func TestWriteRefreshErr_LookupUnavailable_Is503WithDistinctCode(t *testing.T) {
	rec := httptest.NewRecorder()
	// WRAPPED, never the bare sentinel: every emitting site returns
	// `fmt.Errorf("...: %w: %w", ErrRefreshLookupUnavailable, err)`, so the
	// bare sentinel is an input this handler never actually receives — and the
	// internals sweep below cannot fail against it, because the sentinel's own
	// message names no store. The wrap is what makes the sweep real.
	writeRefreshErr(rec, fmt.Errorf("refresh token lookup failed: %w: %w",
		services.ErrRefreshLookupUnavailable,
		errors.New("mongo: no reachable servers")))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — a 401 here is the Mongo-blip logout this change removes", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["code"] != "refresh_lookup_unavailable" {
		t.Fatalf("code = %v, want refresh_lookup_unavailable", body["code"])
	}
	for _, v := range body {
		if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), "mongo") {
			t.Errorf("response leaks internals: %q", s)
		}
	}
}

func TestRefreshFailureOutcome_LookupUnavailable_IsNotInvalidToken(t *testing.T) {
	if got := refreshFailureOutcome(services.ErrRefreshLookupUnavailable); got != "lookup_unavailable" {
		t.Fatalf("refreshFailureOutcome = %q, want lookup_unavailable — logging an outage as invalid_token is the misreading this change removes", got)
	}
}
