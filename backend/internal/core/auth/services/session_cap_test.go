package services

import (
	"context"
	"testing"
	"time"
)

// The cap and the session-retention window are coupled: retention must
// never be able to delete the anchor of a session still inside the cap.
// A strict inequality is required — at equality Mongo's TTL monitor can
// delete the row at the exact boundary, before the refresh path reads it,
// turning an expired session into a compatibility miss. Changing either
// constant must break the build. ADR-0017 D7.
func TestSessionAbsoluteTTLLeavesRetentionMargin(t *testing.T) {
	if MaxSessionAbsoluteTTL+SessionRetentionSafetyMargin > AuthSessionRetention {
		t.Fatalf("cap %v + margin %v exceeds retention %v — retention could delete a live session's anchor",
			MaxSessionAbsoluteTTL, SessionRetentionSafetyMargin, AuthSessionRetention)
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
			if got := newPolicy(tc.set).SessionAbsoluteTTL(context.Background()); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionAbsoluteTTL_NilPolicyUsesDefault(t *testing.T) {
	var p *AuthPolicyService
	if got := p.SessionAbsoluteTTL(context.Background()); got != DefaultSessionAbsoluteTTL {
		t.Errorf("nil policy = %v, want the secure-by-default %v", got, DefaultSessionAbsoluteTTL)
	}
}
