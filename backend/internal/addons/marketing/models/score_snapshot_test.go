package models

import "testing"

// TestScoreSnapshotIsStaleAgainst pins the staleness predicate the
// ScoreService relies on. Two paths must both return true: an
// explicit Stale flag, or a ProfileVersion drift that hasn't been
// flipped yet by InvalidateProfile.
func TestScoreSnapshotIsStaleAgainst(t *testing.T) {
	cases := []struct {
		name           string
		snapVersion    int
		snapStaleFlag  bool
		profileVersion int
		want           bool
	}{
		{"fresh", 3, false, 3, false},
		{"explicit-stale", 3, true, 3, true},
		{"version-drifted", 2, false, 3, true},
		{"both-stale-and-drifted", 1, true, 3, true},
		{"snapshot-newer-than-profile", 5, false, 3, false}, // defensive — should never happen, but predicate doesn't panic
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &ScoreSnapshot{ProfileVersion: c.snapVersion, Stale: c.snapStaleFlag}
			if got := s.IsStaleAgainst(c.profileVersion); got != c.want {
				t.Errorf("IsStaleAgainst(%d) = %v, want %v (snap v=%d stale=%v)",
					c.profileVersion, got, c.want, c.snapVersion, c.snapStaleFlag)
			}
		})
	}
}
