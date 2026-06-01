package services

import (
	"testing"

	"github.com/orkestra/backend/internal/addons/compliance/models"
)

// TestDefaultActorType pins the inference rule: if the caller omits ActorType,
// fall back to "user" when a userID is present, otherwise "system". This is
// the contract consumers rely on so every emit site doesn't have to set the
// field explicitly.
func TestDefaultActorType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		given  string
		userID string
		want   string
	}{
		{"explicit user is preserved", models.ActorTypeUser, "", models.ActorTypeUser},
		{"explicit system is preserved", models.ActorTypeSystem, "u-1", models.ActorTypeSystem},
		{"explicit anonymous is preserved", models.ActorTypeAnonymous, "", models.ActorTypeAnonymous},
		{"empty with user id infers user", "", "u-1", models.ActorTypeUser},
		{"empty without user id infers system", "", "", models.ActorTypeSystem},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := defaultActorType(tc.given, tc.userID)
			if got != tc.want {
				t.Fatalf("defaultActorType(%q, %q) = %q; want %q",
					tc.given, tc.userID, got, tc.want)
			}
		})
	}
}

// TestDefaultOutcome pins the success-by-default rule. Consumers can emit
// with an empty Outcome for the hot-path successful case; failure paths
// must set it explicitly so the choice is deliberate.
func TestDefaultOutcome(t *testing.T) {
	t.Parallel()

	if got := defaultOutcome(""); got != models.OutcomeSuccess {
		t.Fatalf("empty outcome should default to success; got %q", got)
	}
	if got := defaultOutcome(models.OutcomeFailure); got != models.OutcomeFailure {
		t.Fatalf("explicit failure should be preserved; got %q", got)
	}
	if got := defaultOutcome(models.OutcomeDenied); got != models.OutcomeDenied {
		t.Fatalf("explicit denied should be preserved; got %q", got)
	}
}
