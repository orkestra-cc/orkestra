package scheduler

import (
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/pkg/sdk/jobs"
)

// Effective is how a job actually runs once the operator override is applied
// over its definition.
type Effective struct {
	Enabled  bool
	Schedule jobs.Schedule
	Location *time.Location
	// Fingerprint identifies Enabled, Schedule and, for a cron schedule
	// only, Location, e.g. "on|cron 0 2 * * *|Europe/Rome" or
	// "on|every 1h0m0s": an interval or manual schedule does not depend on
	// the zone, so a zone change leaves its due time alone. The state
	// document stores the fingerprint its nextRunAt was computed from; a
	// difference means the due time is stale.
	Fingerprint string
}

// EffectiveOf applies st's override (if any) to entry's definition. Enabled
// comes from the override, else from DisabledByDefault; the schedule from the
// override, except on a Manual definition, which ignores schedule overrides;
// the location from the override timezone, else defLoc (nil means UTC). An
// override that does not parse is an error.
func EffectiveOf(entry jobs.Entry, st *models.State, defLoc *time.Location) (Effective, error) {
	def := entry.Def
	eff := Effective{Enabled: !def.DisabledByDefault, Schedule: def.Schedule, Location: defLoc}
	if eff.Location == nil {
		eff.Location = time.UTC
	}
	if st != nil && st.Override != nil {
		o := st.Override
		if o.Enabled != nil {
			eff.Enabled = *o.Enabled
		}
		if o.Schedule != nil && def.Schedule.Kind() != jobs.ScheduleManual {
			s, err := ScheduleOf(*o.Schedule)
			if err != nil {
				return Effective{}, fmt.Errorf("jobs: %s: override: %w", entry.Key, err)
			}
			eff.Schedule = s
		}
		if o.Timezone != "" {
			loc, err := time.LoadLocation(o.Timezone)
			if err != nil {
				return Effective{}, fmt.Errorf("jobs: %s: override timezone %q: %w", entry.Key, o.Timezone, err)
			}
			eff.Location = loc
		}
	}
	state := "on"
	if !eff.Enabled {
		state = "off"
	}
	eff.Fingerprint = state + "|" + eff.Schedule.String()
	if eff.Schedule.Kind() == jobs.ScheduleCron {
		eff.Fingerprint += "|" + eff.Location.String()
	}
	return eff, nil
}

// ScheduleOf turns a persisted schedule spec ("every" or "cron") into a
// validated schedule.
func ScheduleOf(spec models.ScheduleSpec) (jobs.Schedule, error) {
	var s jobs.Schedule
	switch jobs.ScheduleKind(spec.Kind) {
	case jobs.ScheduleEvery:
		s = jobs.Every(time.Duration(spec.EverySeconds) * time.Second)
	case jobs.ScheduleCron:
		s = jobs.Cron(spec.Cron)
	default:
		return jobs.Schedule{}, fmt.Errorf("jobs: schedule kind %q must be every or cron", spec.Kind)
	}
	if err := s.Validate(); err != nil {
		return jobs.Schedule{}, err
	}
	return s, nil
}

// NextRun is the next due time strictly after `after`, or nil when the job is
// disabled, manual or its schedule never fires again.
func NextRun(eff Effective, after time.Time) *time.Time {
	if !eff.Enabled {
		return nil
	}
	t, ok := eff.Schedule.Next(after, eff.Location)
	if !ok {
		return nil
	}
	return &t
}
