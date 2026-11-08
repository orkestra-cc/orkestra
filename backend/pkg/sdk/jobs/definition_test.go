package jobs

import (
	"context"
	"testing"
	"time"
)

func validDef() Definition {
	return Definition{
		Name: "sweep", Title: "Sweep", Schedule: Every(time.Hour),
		Run: func(context.Context, *Run) error { return nil },
	}
}

func TestDefinitionValidate(t *testing.T) {
	if err := validDef().Normalized().Validate(); err != nil {
		t.Fatalf("minimal def should be valid: %v", err)
	}
	cases := map[string]func(d *Definition){
		"bad name":      func(d *Definition) { d.Name = "Bad-Name" },
		"empty title":   func(d *Definition) { d.Title = "" },
		"nil run":       func(d *Definition) { d.Run = nil },
		"timeout 7h":    func(d *Definition) { d.Timeout = 7 * time.Hour },
		"attempts 11":   func(d *Definition) { d.Retry.MaxAttempts = 11 },
		"zero schedule": func(d *Definition) { d.Schedule = Schedule{} },
		"backoff>max":   func(d *Definition) { d.Retry.Backoff = 2 * time.Hour; d.Retry.MaxBackoff = time.Hour },
	}
	for name, mut := range cases {
		d := validDef()
		mut(&d)
		if err := d.Normalized().Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestNormalizedDefaults(t *testing.T) {
	d := validDef().Normalized()
	if d.Timeout != 10*time.Minute || d.Retry.MaxAttempts != 1 || d.Retry.Backoff != time.Minute || d.Retry.MaxBackoff != time.Hour {
		t.Fatalf("defaults wrong: %+v", d)
	}
}

func TestRetryDelay(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 10, Backoff: time.Minute, MaxBackoff: time.Hour}
	j := func() float64 { return 0.5 }
	for attempt, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 8: time.Hour, 40: time.Hour} {
		if got := p.Delay(attempt, j); got != want {
			t.Errorf("attempt %d: got %s want %s", attempt, got, want)
		}
	}
	if got := p.Delay(1, func() float64 { return 0 }); got != 48*time.Second {
		t.Errorf("jitter 0: got %s", got)
	}
}
