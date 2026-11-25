package jobs

import (
	"time"

	"github.com/orkestra/backend/internal/core/jobs/scheduler"
	"github.com/orkestra/backend/pkg/sdk/metrics"
)

// runMetrics adapts the process metrics collector to the scheduler's
// Metrics: orkestra_jobs_runs_total{job,status},
// orkestra_jobs_run_duration_seconds{job} and
// orkestra_jobs_lease_lost_total{job}, where job is a catalog key (closed at
// boot, ADR-0002). The collector counts abandoned and skipped runs without
// observing a duration: they never executed in this process.
type runMetrics struct{ c *metrics.Collector }

var _ scheduler.Metrics = runMetrics{}

func (m runMetrics) RunFinished(key, status string, d time.Duration) {
	m.c.RecordJobRun(key, status, d)
}

func (m runMetrics) LeaseLost(key string) { m.c.RecordJobLeaseLost(key) }
