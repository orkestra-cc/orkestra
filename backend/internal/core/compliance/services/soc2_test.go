package services

import "testing"

// TestPercent pins the MFA/coverage ratio helper, including the empty-set
// convention: 0/0 reports 100% (a population with no privileged users is
// vacuously fully covered, not a 0% finding).
func TestPercent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		covered int64
		total   int64
		want    float64
	}{
		{"empty population is 100%", 0, 0, 100.0},
		{"nonzero covered with zero total is 100%", 5, 0, 100.0},
		{"half covered", 1, 2, 50.0},
		{"fully covered", 2, 2, 100.0},
		{"none covered", 0, 4, 0.0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := percent(tc.covered, tc.total); got != tc.want {
				t.Fatalf("percent(%d, %d) = %v; want %v", tc.covered, tc.total, got, tc.want)
			}
		})
	}
}
