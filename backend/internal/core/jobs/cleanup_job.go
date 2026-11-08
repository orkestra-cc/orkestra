package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdkjobs "github.com/orkestra/backend/pkg/sdk/jobs"
)

// cleanupJobName is the module's own job: jobs.runs_cleanup (spec J13).
const cleanupJobName = "runs_cleanup"

// Jobs contributes runs_cleanup, the framework's first consumer: once a day
// it deletes the run records older than runRetentionDays.
func (m *Module) Jobs() []sdkjobs.Definition {
	return []sdkjobs.Definition{{
		Name:        cleanupJobName,
		Title:       "Delete old job runs",
		Description: "Deletes the run history older than the module's run retention (runRetentionDays, default 30 days).",
		Schedule:    sdkjobs.Every(24 * time.Hour),
		Timeout:     30 * time.Minute,
		Run:         m.runCleanup,
	}}
}

func (m *Module) runCleanup(ctx context.Context, run *sdkjobs.Run) error {
	runs := m.runs
	if runs == nil {
		return sdkjobs.Permanent(errors.New("jobs: run repository not initialised"))
	}
	days := m.retentionDays()
	cutoff := m.now().Add(-time.Duration(days) * 24 * time.Hour)
	n, err := runs.DeleteOlderThan(ctx, cutoff)
	run.Detail("deleted", n)
	run.Detail("retentionDays", days)
	if err != nil {
		return fmt.Errorf("jobs: delete runs older than %d days: %w", days, err)
	}
	return nil
}
