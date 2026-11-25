package jobs

import (
	"errors"
	"fmt"
	"time"
)

// ScheduleKind says how a job is triggered.
type ScheduleKind string

const (
	ScheduleEvery  ScheduleKind = "every"
	ScheduleCron   ScheduleKind = "cron"
	ScheduleManual ScheduleKind = "manual"
)

// MinEvery is the smallest interval an Every schedule accepts.
const MinEvery = time.Minute

// Schedule is when a job runs. Build one with Every, Cron or Manual; the zero
// value is invalid.
type Schedule struct {
	kind  ScheduleKind
	every time.Duration
	cron  *CronExpr
	raw   string
	err   error
}

// Every runs the job at a fixed interval (at least one minute).
func Every(d time.Duration) Schedule { return Schedule{kind: ScheduleEvery, every: d} }

// Cron runs the job on a 5-field cron expression. A parse error is kept and
// reported by Validate.
func Cron(expr string) Schedule {
	c, err := ParseCron(expr)
	return Schedule{kind: ScheduleCron, cron: c, raw: expr, err: err}
}

// Manual never runs on its own; the job only runs when triggered.
func Manual() Schedule { return Schedule{kind: ScheduleManual} }

// Kind returns the schedule kind ("" for the zero value).
func (s Schedule) Kind() ScheduleKind { return s.kind }

// Every returns the interval of an every schedule, else 0.
func (s Schedule) Every() time.Duration { return s.every }

// Expr returns the cron expression as given, else "".
func (s Schedule) Expr() string { return s.raw }

// Validate reports why the schedule cannot be used.
func (s Schedule) Validate() error {
	switch s.kind {
	case ScheduleEvery:
		if s.every < MinEvery {
			return fmt.Errorf("jobs: every %s is below the minimum of %s", s.every, MinEvery)
		}
	case ScheduleCron:
		if s.err != nil {
			return s.err
		}
		probe := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, ok := s.cron.Next(probe, time.UTC); !ok {
			return fmt.Errorf("jobs: cron %q never matches", s.raw)
		}
	case ScheduleManual:
	default:
		return errors.New("jobs: schedule is not set (use Every, Cron or Manual)")
	}
	return nil
}

// Next returns the next run strictly after `after`. For every schedules loc is
// ignored; manual and invalid schedules return false.
func (s Schedule) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	switch s.kind {
	case ScheduleEvery:
		if s.every < MinEvery {
			return time.Time{}, false
		}
		return after.Add(s.every), true
	case ScheduleCron:
		if s.cron == nil {
			return time.Time{}, false
		}
		return s.cron.Next(after, loc)
	}
	return time.Time{}, false
}

func (s Schedule) String() string {
	switch s.kind {
	case ScheduleEvery:
		return "every " + s.every.String()
	case ScheduleCron:
		return "cron " + s.raw
	case ScheduleManual:
		return "manual"
	}
	return "unset"
}
