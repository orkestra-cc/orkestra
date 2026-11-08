package jobs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const (
	defaultTimeout    = 10 * time.Minute
	maxTimeout        = 6 * time.Hour
	defaultAttempts   = 1
	maxAttempts       = 10
	defaultBackoff    = time.Minute
	defaultMaxBackoff = time.Hour
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// RetryPolicy says how a failed run is retried. Delays double from Backoff up
// to MaxBackoff, with jitter.
type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
	MaxBackoff  time.Duration
}

// Delay is the wait before retrying after the given failed attempt (>= 1):
// Backoff*2^(attempt-1) capped at MaxBackoff, scaled by 0.8..1.2 using jitter
// (a function returning a value in [0,1)).
func (p RetryPolicy) Delay(attempt int, jitter func() float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := p.Backoff
	for i := 1; i < attempt && d < p.MaxBackoff; i++ {
		d *= 2
	}
	if p.MaxBackoff > 0 && d > p.MaxBackoff {
		d = p.MaxBackoff
	}
	j := 0.5
	if jitter != nil {
		j = jitter()
	}
	return time.Duration(float64(d) * (0.8 + 0.4*j))
}

// Definition is a background job a module contributes through
// module.HasJobs. The core jobs module schedules and runs it.
type Definition struct {
	// Name is unique within the module: lower snake case. The job key is
	// "<module>.<Name>".
	Name        string
	Title       string
	Description string
	Schedule    Schedule
	// DisabledByDefault keeps a new job off until an operator enables it.
	DisabledByDefault bool
	// Timeout bounds one attempt (default 10m, max 6h).
	Timeout time.Duration
	Retry   RetryPolicy
	// SkipMissed drops runs missed while the process was down instead of
	// running one catch-up.
	SkipMissed bool
	// Run does the work. It must honour ctx cancellation.
	Run func(ctx context.Context, run *Run) error
}

// Normalized returns a copy with defaults filled in.
func (d Definition) Normalized() Definition {
	if d.Timeout == 0 {
		d.Timeout = defaultTimeout
	}
	if d.Retry.MaxAttempts == 0 {
		d.Retry.MaxAttempts = defaultAttempts
	}
	if d.Retry.Backoff == 0 {
		d.Retry.Backoff = defaultBackoff
	}
	if d.Retry.MaxBackoff == 0 {
		d.Retry.MaxBackoff = defaultMaxBackoff
	}
	return d
}

// Validate reports why the definition cannot be used. Call it on a Normalized
// definition.
func (d Definition) Validate() error {
	if !nameRE.MatchString(d.Name) {
		return fmt.Errorf("jobs: name %q must match %s", d.Name, nameRE)
	}
	if d.Title == "" {
		return fmt.Errorf("jobs: %s: title is required", d.Name)
	}
	if err := d.Schedule.Validate(); err != nil {
		return fmt.Errorf("jobs: %s: %w", d.Name, err)
	}
	if d.Run == nil {
		return fmt.Errorf("jobs: %s: run is required", d.Name)
	}
	if d.Timeout <= 0 || d.Timeout > maxTimeout {
		return fmt.Errorf("jobs: %s: timeout %s must be in (0, %s]", d.Name, d.Timeout, maxTimeout)
	}
	if d.Retry.MaxAttempts < 1 || d.Retry.MaxAttempts > maxAttempts {
		return fmt.Errorf("jobs: %s: max attempts %d must be 1..%d", d.Name, d.Retry.MaxAttempts, maxAttempts)
	}
	if d.Retry.Backoff <= 0 || d.Retry.MaxBackoff <= 0 || d.Retry.Backoff > d.Retry.MaxBackoff {
		return errors.New("jobs: " + d.Name + ": backoff must be positive and not above max backoff")
	}
	return nil
}
