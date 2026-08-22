package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// The cap and the session-retention window are coupled: retention must
// never be able to delete the anchor of a session still inside the cap.
// A strict inequality is required — at equality Mongo's TTL monitor can
// delete the row at the exact boundary, before the refresh path reads it,
// turning an expired session into a compatibility miss. Changing either
// constant must break the build. ADR-0017 D7.
func TestSessionAbsoluteTTLLeavesRetentionMargin(t *testing.T) {
	if MaxSessionAbsoluteTTL+SessionRetentionSafetyMargin > models.AuthSessionRetention {
		t.Fatalf("cap %v + margin %v exceeds retention %v — retention could delete a live session's anchor",
			MaxSessionAbsoluteTTL, SessionRetentionSafetyMargin, models.AuthSessionRetention)
	}
	if SessionRetentionSafetyMargin <= 0 {
		t.Fatal("the margin must be positive; equality races Mongo's TTL monitor at the cap boundary")
	}
	if DefaultSessionAbsoluteTTL > MaxSessionAbsoluteTTL || DefaultSessionAbsoluteTTL < MinSessionAbsoluteTTL {
		t.Fatalf("default %v outside the accepted range [%v, %v]", DefaultSessionAbsoluteTTL, MinSessionAbsoluteTTL, MaxSessionAbsoluteTTL)
	}
}

// The stub's map lookup distinguishes "key present with empty value" (ok=true,
// v="") from "key absent" (ok=false) exactly the way GetRawValue distinguishes
// an operator-cleared field from one the document never had — see stubReader's
// GetRawValue in auth_policy_service_test.go.
func TestSessionAbsoluteTTL_PolicyResolution(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want time.Duration
	}{
		{"absent uses the 30-day default", nil, DefaultSessionAbsoluteTTL},
		{"empty disables the cap", map[string]string{"sessionAbsoluteTTL": ""}, 0},
		{"blank disables the cap", map[string]string{"sessionAbsoluteTTL": "   "}, 0},
		{"explicit value wins", map[string]string{"sessionAbsoluteTTL": "48h"}, 48 * time.Hour},
		{"day suffix parses", map[string]string{"sessionAbsoluteTTL": "7d"}, 7 * 24 * time.Hour},
		{"legacy malformed uses the default", map[string]string{"sessionAbsoluteTTL": "forever"}, DefaultSessionAbsoluteTTL},
		{"legacy below minimum clamps up", map[string]string{"sessionAbsoluteTTL": "5m"}, MinSessionAbsoluteTTL},
		{"legacy above maximum clamps down", map[string]string{"sessionAbsoluteTTL": "365d"}, MaxSessionAbsoluteTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := newPolicy(tc.set).SessionAbsoluteTTL(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionAbsoluteTTL_NilPolicyUsesDefault(t *testing.T) {
	var p *AuthPolicyService
	got, err := p.SessionAbsoluteTTL(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != DefaultSessionAbsoluteTTL {
		t.Errorf("nil policy = %v, want the secure-by-default %v", got, DefaultSessionAbsoluteTTL)
	}
}

// A config-read FAILURE must not be read as "the operator said nothing".
//
// GetRawValue used to collapse err != nil into ("", false), which
// SessionAbsoluteTTL treated as "key absent" and answered with the 30-day
// default. A deployment that had deliberately cleared sessionAbsoluteTTL to
// disable the cap therefore got the cap back for the duration of any
// module_configs read failure — and the consequence is not a degraded read
// but an IRREVERSIBLE logout of every session older than 30 days that happened
// to refresh in that window. Every other failure on this path fails closed to
// 503; this one silently substituted a different policy.
func TestSessionAbsoluteTTL_ReadErrorDoesNotApplyTheDefault(t *testing.T) {
	boom := errors.New("module_configs unreachable")
	// The operator's stored state says "disabled" — but the read fails, so
	// the service cannot see it. The wrong answer here is the default.
	p := &AuthPolicyService{cs: &stubReader{
		values: map[string]string{"sessionAbsoluteTTL": ""},
		rawErr: boom,
	}}

	got, err := p.SessionAbsoluteTTL(context.Background())
	if !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("err = %v, want ErrSessionEnforcementUnavailable — a failed read is an outage, and the handler maps this sentinel to 503", err)
	}
	if got == DefaultSessionAbsoluteTTL {
		t.Errorf("returned the %v default on a failed read — that re-arms a cap the operator disabled and signs out every session older than it", DefaultSessionAbsoluteTTL)
	}
}

// The same failure, one layer up: the enforcement helper must propagate it
// rather than let a zero duration read as "cap disabled, carry on".
func TestSessionWithinAbsoluteCap_PolicyReadErrorFailsClosed(t *testing.T) {
	sessions := newGateSessionRepo()
	// Seeded YOUNG on purpose: under the swallowed-error behaviour the
	// helper would take the 30-day default, find this session inside it,
	// and return nil — indistinguishable from success. The assertion is
	// therefore about the policy read alone, not about the anchor.
	sessions.seedSession(&models.AuthSessionDoc{
		UUID: "sid-1", UserUUID: "u-1", StartedAt: time.Now().Add(-time.Hour),
	})
	svc := &authService{
		authSessionRepo: sessions,
		policy:          &AuthPolicyService{cs: &stubReader{rawErr: errors.New("module_configs unreachable")}},
	}

	err := svc.sessionWithinAbsoluteCap(context.Background(), "sid-1")
	if !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("err = %v, want ErrSessionEnforcementUnavailable — an unreadable policy is an outage, and answering nil here mints credentials on a cap the service could not evaluate", err)
	}
}
