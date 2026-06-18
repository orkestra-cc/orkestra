package services

import (
	"context"
	"testing"
	"time"
)

// TestRetentionCutoff pins the window arithmetic, including the default that
// guards against a zero/negative config (which would otherwise make the cutoff
// "now" and reap every tombstone immediately).
func TestRetentionCutoff(t *testing.T) {
	t.Parallel()

	const day = 24 * time.Hour
	cases := []struct {
		name      string
		years     int
		wantYears int // effective years after defaulting
	}{
		{"explicit 3 years", 3, 3},
		{"zero defaults to 5", 0, 5},
		{"negative defaults to 5", -2, 5},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := retentionCutoff(tc.years)
			want := time.Now().UTC().Add(-time.Duration(tc.wantYears) * 365 * day)
			// Allow a generous skew for the time between the two Now() reads.
			if diff := got.Sub(want); diff > time.Minute || diff < -time.Minute {
				t.Fatalf("retentionCutoff(%d) off by %v (got %v, want ~%v)", tc.years, diff, got, want)
			}
			if !got.Before(time.Now().UTC()) {
				t.Fatalf("retentionCutoff(%d) = %v is not in the past", tc.years, got)
			}
		})
	}
}

// TestRunOnceDisabledIsNoOp pins that the retention job is inert while
// auto-cleanup is off — it returns (0, nil) and never touches the DB (the nil
// db/dsr here would panic if the disabled guard didn't short-circuit first).
func TestRunOnceDisabledIsNoOp(t *testing.T) {
	t.Parallel()

	loaded := 0
	svc := &RetentionService{
		loadConfig: func() RetentionConfig {
			loaded++
			return RetentionConfig{Enabled: false, Years: 5}
		},
	}
	n, err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce disabled returned error: %v", err)
	}
	if n != 0 {
		t.Fatalf("RunOnce disabled purged %d; want 0", n)
	}
	if loaded != 1 {
		t.Fatalf("RunOnce should read config fresh each run; loaded=%d", loaded)
	}
}
