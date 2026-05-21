package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

func intPtr(i int) *int { return &i }

// nearly is a numerical tolerance comparison for the exponential decay
// branch, where mathematical exactness is undermined by float64
// representation.
func nearly(a, b float64) bool {
	const eps = 1e-9
	return math.Abs(a-b) < eps
}

// TestApplyDecayNone — the nil and explicit "none" forms both return
// 1 at every age.
func TestApplyDecayNone(t *testing.T) {
	cases := []struct {
		name  string
		decay *models.DecayFn
		age   time.Duration
	}{
		{"nil", nil, 0},
		{"nil-far-past", nil, 10000 * 24 * time.Hour},
		{"empty-fn", &models.DecayFn{Fn: ""}, 30 * 24 * time.Hour},
		{"explicit-none", &models.DecayFn{Fn: "none"}, 30 * 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyDecay(c.decay, c.age); got != 1 {
				t.Errorf("applyDecay(%+v, %v) = %v, want 1", c.decay, c.age, got)
			}
		})
	}
}

// TestApplyDecayLinear hits every meaningful age bucket:
//
//	age = 0          → 1.0 (full strength)
//	age = window/2   → 0.5 (half)
//	age = window     → 0   (at threshold the contribution dies)
//	age > window     → 0   (clamped, no negative)
func TestApplyDecayLinear(t *testing.T) {
	d := &models.DecayFn{Fn: "linear", WindowDays: intPtr(180)}
	cases := []struct {
		name string
		age  time.Duration
		want float64
	}{
		{"zero-age", 0, 1.0},
		{"half-window", 90 * 24 * time.Hour, 0.5},
		{"at-window", 180 * 24 * time.Hour, 0.0},
		{"past-window", 365 * 24 * time.Hour, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := applyDecay(d, c.age)
			if !nearly(got, c.want) {
				t.Errorf("linear decay at %v: got %v, want %v", c.age, got, c.want)
			}
		})
	}
}

// TestApplyDecayExponential hits the half-life lattice — values at
// 0, 1×, 2×, 3× half-life — and a long-tail point. The function
// asymptotes toward 0 but never reaches it; we check the lattice
// exactly and the tail loosely.
func TestApplyDecayExponential(t *testing.T) {
	d := &models.DecayFn{Fn: "exponential", HalfLifeDays: intPtr(90)}
	cases := []struct {
		name string
		age  time.Duration
		want float64
	}{
		{"zero-age", 0, 1.0},
		{"one-halflife", 90 * 24 * time.Hour, 0.5},
		{"two-halflife", 180 * 24 * time.Hour, 0.25},
		{"three-halflife", 270 * 24 * time.Hour, 0.125},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := applyDecay(d, c.age)
			if !nearly(got, c.want) {
				t.Errorf("exponential decay at %v: got %v, want %v", c.age, got, c.want)
			}
		})
	}
	// Long tail: at 10× half-life the value is 2^-10 ≈ 0.000977.
	// Important property: still > 0, not clamped.
	got := applyDecay(d, 900*24*time.Hour)
	if got <= 0 || got > 0.01 {
		t.Errorf("exponential decay at 10× halflife: got %v, expected (0, 0.01)", got)
	}
}

// TestApplyDecayMisconfigured covers the defensive runtime fallbacks.
// Validate() at the model layer rejects these on save, but the engine
// must not panic if a corrupted profile slips through (e.g. raw mongo
// edit by an operator).
func TestApplyDecayMisconfigured(t *testing.T) {
	cases := []struct {
		name  string
		decay *models.DecayFn
	}{
		{"linear-no-window", &models.DecayFn{Fn: "linear"}},
		{"linear-zero-window", &models.DecayFn{Fn: "linear", WindowDays: intPtr(0)}},
		{"linear-negative-window", &models.DecayFn{Fn: "linear", WindowDays: intPtr(-1)}},
		{"exp-no-halflife", &models.DecayFn{Fn: "exponential"}},
		{"exp-zero-halflife", &models.DecayFn{Fn: "exponential", HalfLifeDays: intPtr(0)}},
		{"unknown-fn", &models.DecayFn{Fn: "polynomial"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyDecay(c.decay, 30*24*time.Hour); got != 1 {
				t.Errorf("misconfigured decay %s should fall back to 1, got %v", c.name, got)
			}
		})
	}
}

// TestApplyDecayNegativeAge — a future-of-asOf activity yields age < 0;
// the engine filters such activities out earlier, but the helper must
// defend against clock skew via clamp-to-zero.
func TestApplyDecayNegativeAge(t *testing.T) {
	d := &models.DecayFn{Fn: "linear", WindowDays: intPtr(180)}
	if got := applyDecay(d, -24*time.Hour); got != 1 {
		t.Errorf("negative age should clamp to zero (decay=1), got %v", got)
	}
}
