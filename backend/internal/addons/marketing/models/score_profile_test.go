package models

import (
	"errors"
	"testing"
	"time"
)

func intPtr(i int) *int { return &i }

// TestValidateDecayBranches covers each branch of validateDecay so a
// regression introducing a new decay fn that forgets to require its
// params fails loudly.
func TestValidateDecayBranches(t *testing.T) {
	cases := []struct {
		name    string
		decay   *DecayFn
		wantErr bool
	}{
		{"nil", nil, false},
		{"none-empty-fn", &DecayFn{Fn: ""}, false},
		{"none-explicit", &DecayFn{Fn: "none"}, false},
		{"linear-with-window", &DecayFn{Fn: "linear", WindowDays: intPtr(180)}, false},
		{"linear-missing-window", &DecayFn{Fn: "linear"}, true},
		{"linear-zero-window", &DecayFn{Fn: "linear", WindowDays: intPtr(0)}, true},
		{"linear-negative-window", &DecayFn{Fn: "linear", WindowDays: intPtr(-1)}, true},
		{"exp-with-halflife", &DecayFn{Fn: "exponential", HalfLifeDays: intPtr(90)}, false},
		{"exp-missing-halflife", &DecayFn{Fn: "exponential"}, true},
		{"exp-zero-halflife", &DecayFn{Fn: "exponential", HalfLifeDays: intPtr(0)}, true},
		{"unknown-fn", &DecayFn{Fn: "polynomial"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateDecay(c.decay)
			if (err != nil) != c.wantErr {
				t.Errorf("validateDecay(%+v) err=%v, wantErr=%v", c.decay, err, c.wantErr)
			}
		})
	}
}

// TestScoreProfileValidateHappyPath confirms a minimally complete
// profile passes Validate.
func TestScoreProfileValidateHappyPath(t *testing.T) {
	p := &ScoreProfile{
		Name: "hot_lead",
		Rules: []ScoreRule{
			{ActivityKind: string(KindMeetingHeld), Points: 50},
		},
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate happy path: unexpected error %v", err)
	}
}

// TestScoreProfileValidateRejections walks the gate matrix.
func TestScoreProfileValidateRejections(t *testing.T) {
	cases := []struct {
		name    string
		profile *ScoreProfile
	}{
		{
			name:    "nil-receiver",
			profile: nil,
		},
		{
			name: "empty-name",
			profile: &ScoreProfile{
				Name:  "",
				Rules: []ScoreRule{{ActivityKind: "*", Points: 1}},
			},
		},
		{
			name: "no-rules",
			profile: &ScoreProfile{
				Name:  "empty_profile",
				Rules: nil,
			},
		},
		{
			name: "bad-default-decay",
			profile: &ScoreProfile{
				Name:         "bad_default",
				Rules:        []ScoreRule{{ActivityKind: "*", Points: 1}},
				DefaultDecay: &DecayFn{Fn: "linear"}, // missing windowDays
			},
		},
		{
			name: "bad-rule-decay",
			profile: &ScoreProfile{
				Name: "bad_rule",
				Rules: []ScoreRule{
					{ActivityKind: "*", Points: 1, Decay: &DecayFn{Fn: "exponential"}},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.profile.Validate()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidScoreProfile) {
				t.Errorf("expected ErrInvalidScoreProfile in chain, got %v", err)
			}
		})
	}
}

// TestScoreProfileBumpVersion: every Save bumps the version + sets
// UpdatedAt to the supplied clock. The staleness check on snapshots
// relies on monotonic version increase.
func TestScoreProfileBumpVersion(t *testing.T) {
	p := &ScoreProfile{Version: 3}
	when := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	p.BumpVersion(when)
	if p.Version != 4 {
		t.Errorf("Version after bump = %d, want 4", p.Version)
	}
	if !p.UpdatedAt.Equal(when) {
		t.Errorf("UpdatedAt = %v, want %v", p.UpdatedAt, when)
	}
}
