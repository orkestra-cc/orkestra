package metrics

import (
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func jobDurationSamples(t *testing.T, c *Collector, job string) uint64 {
	t.Helper()
	hist, ok := c.jobsRunDuration.WithLabelValues(job).(prometheus.Histogram)
	if !ok {
		t.Fatal("jobsRunDuration observer is not a prometheus.Histogram")
	}
	var m dto.Metric
	if err := hist.Write(&m); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return m.GetHistogram().GetSampleCount()
}

// Runs that never executed in this process (abandoned, skipped) are counted
// but not timed: their zero duration would drag the histogram down.
func TestJobRunsCountedAndTimed(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordJobRun("jobs.runs_cleanup", "succeeded", 2*time.Second)
	c.RecordJobRun("jobs.runs_cleanup", "failed", 500*time.Millisecond)
	c.RecordJobRun("jobs.runs_cleanup", "abandoned", 0)
	c.RecordJobRun("jobs.runs_cleanup", "skipped", 0)
	c.RecordJobRun("jobs.runs_cleanup", "exploded", time.Second) // not a status

	for status, want := range map[string]float64{"succeeded": 1, "failed": 1, "abandoned": 1, "skipped": 1, "unknown": 1} {
		if got := testutil.ToFloat64(c.jobsRuns.WithLabelValues("jobs.runs_cleanup", status)); got != want {
			t.Errorf("status=%s runs = %v, want %v", status, got, want)
		}
	}
	if got := jobDurationSamples(t, c, "jobs.runs_cleanup"); got != 3 {
		t.Errorf("duration samples = %d, want 3 (succeeded, failed, unknown)", got)
	}
}

func TestJobLeaseLostCounted(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordJobLeaseLost("crm.digest")
	c.RecordJobLeaseLost("crm.digest")
	if got := testutil.ToFloat64(c.jobsLeaseLost.WithLabelValues("crm.digest")); got != 2 {
		t.Errorf("lease lost = %v, want 2", got)
	}
	var nilC *Collector
	nilC.RecordJobRun("x.y", "succeeded", time.Second) // nil-safe
	nilC.RecordJobLeaseLost("x.y")
}
