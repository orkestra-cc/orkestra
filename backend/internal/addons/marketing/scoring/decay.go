// Package scoring is the pure marketing-addon score engine. Public
// entry point is Engine.Compute(activities, profile, asOf), which is
// a side-effect-free function of its inputs — no DB calls, no
// time.Now() reads, no logging of state. That purity is what makes
// time-travel (recompute with a past asOf) trivial: pass a past
// instant and the result is what the score would have been then.
//
// The package is split by concern to keep the engine.go file legible:
//
//	decay.go      — none / linear / exponential decay functions
//	match.go      — rule kind selector + Mongo-style payload matcher
//	correction.go — corrected_by set derivation
//	engine.go     — public Compute entry point, orchestrates the rest
package scoring

import (
	"math"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// applyDecay computes the decay coefficient in [0, 1] for an activity
// of the given age, under the supplied DecayFn. Returns 1 when d is
// nil (no decay declared at the rule level and no DefaultDecay on
// the profile).
//
// Decay branches (decision D15 / schema doc §rule):
//
//	none        f(age) = 1
//	linear      f(age) = max(0, 1 - age / window)
//	exponential f(age) = 2 ** (-age / half-life)
//
// All three branches saturate non-negatively — a future-of-asOf
// activity (age < 0) is filtered out by the engine before reaching
// this function, but the helper still clamps to 0 defensively in
// case of clock skew on imported timestamps.
func applyDecay(d *models.DecayFn, age time.Duration) float64 {
	if age < 0 {
		age = 0
	}
	if d == nil {
		return 1
	}
	switch d.Fn {
	case "", "none":
		return 1
	case "linear":
		if d.WindowDays == nil || *d.WindowDays <= 0 {
			// Misconfigured rule — treat as no decay rather than
			// dropping the contribution entirely. Validate at the
			// model level catches this on profile save; this is the
			// defensive runtime fallback.
			return 1
		}
		window := time.Duration(*d.WindowDays) * 24 * time.Hour
		if age >= window {
			return 0
		}
		return 1 - float64(age)/float64(window)
	case "exponential":
		if d.HalfLifeDays == nil || *d.HalfLifeDays <= 0 {
			return 1
		}
		halfLife := time.Duration(*d.HalfLifeDays) * 24 * time.Hour
		ratio := float64(age) / float64(halfLife)
		return math.Pow(2, -ratio)
	default:
		// Unknown decay fn — same defensive choice as the misconfigured
		// linear/exponential branches. Validate at the model level
		// rejects unknown fns on profile save.
		return 1
	}
}
