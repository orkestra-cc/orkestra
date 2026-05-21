package models

import (
	"errors"
	"strconv"
	"time"
)

// DecayFn parameterises how a rule's contribution attenuates with age.
// Three functions are supported (decision tree D15): "none" (the
// activity counts at full strength regardless of age), "linear" (the
// contribution drops to 0 after WindowDays), and "exponential" (the
// contribution halves every HalfLifeDays).
//
// The struct is shared by ScoreProfile.DefaultDecay (inherited by
// rules that don't specify one) and ScoreRule.Decay (rule-level
// override). The engine in scoring/decay.go is the single
// authoritative implementation; this model just carries the params.
type DecayFn struct {
	Fn           string `bson:"fn" json:"fn"`                                         // "none" | "linear" | "exponential"
	WindowDays   *int   `bson:"windowDays,omitempty" json:"windowDays,omitempty"`     // linear: contribution → 0 after N days
	HalfLifeDays *int   `bson:"halfLifeDays,omitempty" json:"halfLifeDays,omitempty"` // exponential: contribution halves every N days
}

// ScoreRule is one scoring rule in a ScoreProfile.Rules array. A rule
// matches an activity when its ActivityKind selector matches the
// activity's kind AND every key/value in MatchPayload is satisfied
// against the activity's payload. On match the contribution is
// Points × decay(age), capped at Cap per-rule.
//
// ActivityKind is intentionally typed `any` to accept either a single
// ActivityKind string, a []ActivityKind slice, or the wildcard "*".
// The scoring engine type-switches on the value; BSON marshals the
// underlying primitive directly.
type ScoreRule struct {
	ActivityKind any            `bson:"activityKind" json:"activityKind"`
	MatchPayload map[string]any `bson:"matchPayload,omitempty" json:"matchPayload,omitempty"`
	Points       float64        `bson:"points" json:"points"`
	Decay        *DecayFn       `bson:"decay,omitempty" json:"decay,omitempty"`
	Cap          *float64       `bson:"cap,omitempty" json:"cap,omitempty"`
	WindowDays   *int           `bson:"windowDays,omitempty" json:"windowDays,omitempty"`
}

// ProfileFilter restricts a profile to a subset of persons. The
// scoring engine evaluates these against the Person record at
// recompute time and sets ScoreSnapshot.Applicable accordingly —
// snapshots are still emitted for filtered-out persons (with
// Value=0, Applicable=false) so the leaderboard can elect to
// exclude them without a missing-row ambiguity.
type ProfileFilter struct {
	TagsInclude        []string       `bson:"tagsInclude,omitempty" json:"tagsInclude,omitempty"`
	TagsExclude        []string       `bson:"tagsExclude,omitempty" json:"tagsExclude,omitempty"`
	CustomFieldFilters map[string]any `bson:"customFieldFilters,omitempty" json:"customFieldFilters,omitempty"`
}

// ScoreProfile is one named scoring configuration for a tenant.
// Multiple profiles run in parallel (decision D14) — typical tenants
// keep 2-6 profiles, one per audience or quality lane. The score is
// a pure function of (Activities, Profile, asOf), so any profile
// modification (Rules / DefaultDecay / Filters) invalidates the
// downstream snapshots: Save() bumps Version and ScoreService marks
// every snapshot for the profile as stale, to be recomputed by the
// next eager hit or the nightly job.
//
// Schema mirrors
// docs/plans/marketing-addon/schemas/marketing_score_profiles.md.
type ScoreProfile struct {
	UUID        string `bson:"uuid" json:"uuid"`
	TenantID    string `bson:"tenantId" json:"tenantId"`
	Name        string `bson:"name" json:"name"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`

	// Active=false profiles are kept on disk for audit (and so prior
	// snapshots remain interpretable) but are skipped by the eager
	// recompute hook and the nightly job.
	Active bool `bson:"active" json:"active"`

	Rules        []ScoreRule    `bson:"rules" json:"rules"`
	Filters      *ProfileFilter `bson:"filters,omitempty" json:"filters,omitempty"`
	DefaultDecay *DecayFn       `bson:"defaultDecay,omitempty" json:"defaultDecay,omitempty"`

	// Version increments on every save. Snapshots carry the version
	// they were computed against; the staleness check is simply
	// snapshot.ProfileVersion < profile.Version.
	Version int `bson:"version" json:"version"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
	CreatedBy string    `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	UpdatedBy string    `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}

// ErrInvalidScoreProfile is returned by Validate when the profile
// would not be safe to persist (empty name, no rules, malformed
// decay fn).
var ErrInvalidScoreProfile = errors.New("marketing: invalid score profile")

// Validate enforces the minimum invariants the service layer checks
// before persisting a profile. The repository is permissive — a
// fully-empty row would store fine — so this is the canonical gate.
//
// Rules:
//   - Name is a slug-like identifier, required.
//   - At least one rule is declared (an empty profile produces
//     zero-value snapshots; allow that only as a no-op edit case,
//     not a fresh create).
//   - Every Decay (rule-level or default) declares a known fn.
//
// The engine itself validates rule matchers at compute time, not
// here — a profile is allowed to reference an ActivityKind that
// will only fire when the corresponding addon ships.
func (p *ScoreProfile) Validate() error {
	if p == nil {
		return ErrInvalidScoreProfile
	}
	if p.Name == "" {
		return errors.Join(ErrInvalidScoreProfile, errors.New("name required"))
	}
	if len(p.Rules) == 0 {
		return errors.Join(ErrInvalidScoreProfile, errors.New("at least one rule required"))
	}
	if err := validateDecay(p.DefaultDecay); err != nil {
		return errors.Join(ErrInvalidScoreProfile, err)
	}
	for i, r := range p.Rules {
		if err := validateDecay(r.Decay); err != nil {
			return errors.Join(ErrInvalidScoreProfile, errors.New("rule "+strconv.Itoa(i)+": "+err.Error()))
		}
	}
	return nil
}

// BumpVersion increments Version and refreshes UpdatedAt. Called by
// ScoreProfileService.Save on every successful write so the staleness
// check against snapshots fires exactly once per edit.
func (p *ScoreProfile) BumpVersion(now time.Time) {
	p.Version++
	p.UpdatedAt = now
}

func validateDecay(d *DecayFn) error {
	if d == nil {
		return nil
	}
	switch d.Fn {
	case "", "none":
		return nil
	case "linear":
		if d.WindowDays == nil || *d.WindowDays <= 0 {
			return errors.New("linear decay requires positive windowDays")
		}
		return nil
	case "exponential":
		if d.HalfLifeDays == nil || *d.HalfLifeDays <= 0 {
			return errors.New("exponential decay requires positive halfLifeDays")
		}
		return nil
	default:
		return errors.New("unknown decay fn: " + d.Fn)
	}
}
